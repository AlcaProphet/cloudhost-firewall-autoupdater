package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

type importConfirmClient struct {
	snapshot provider.OriginACLSnapshot
	started  chan struct{}
	release  chan struct{}
}

func (c *importConfirmClient) DescribeOriginACL(context.Context, string) (provider.OriginACLSnapshot, error) {
	return c.snapshot, nil
}

func (c *importConfirmClient) ConfirmOriginACLUpdate(context.Context, string) (string, error) {
	close(c.started)
	<-c.release
	return "req-before-import", nil
}

// 只提供覆盖验证所需的 Provider 方法；其余调用会让测试失败。
type importCoveredTarget struct {
	provider.Provider
	id       int
	snapshot provider.RuleSnapshot
}

func (p importCoveredTarget) Name() string                { return "import-test-target" }
func (p importCoveredTarget) CloudType() config.CloudType { return config.CloudTCCVM }
func (p importCoveredTarget) TargetIndex() int            { return p.id }
func (p importCoveredTarget) GetSnapshot() (provider.RuleSnapshot, error) {
	return p.snapshot, nil
}

func TestEdgeOneLateConfirmationAfterConfigImport(t *testing.T) {
	for _, keepSite := range []bool{true, false} {
		name := "removed_site"
		if keepSite {
			name = "retained_site"
		}
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			ctx := context.Background()
			id := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-import")
			site := config.EdgeOneSite{ZoneID: "zone-import", TargetIDs: []int{id}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"}
			withTxForTest(t, e.store, func(ctx context.Context, tx *sql.Tx) error {
				return e.store.AddEdgeOneSiteTx(ctx, tx, site)
			})
			w := e.do(t, http.MethodPut, "/api/settings", `{"dns_enabled":"false","edgeone_enabled":"true"}`)
			if w.Code != http.StatusOK {
				t.Fatalf("启用 EdgeOne 失败: %d %s", w.Code, w.Body.String())
			}
			export := e.do(t, http.MethodPost, "/api/config/export", "")
			if export.Code != http.StatusOK {
				t.Fatalf("导出失败: %d %s", export.Code, export.Body.String())
			}
			bundle := validV4Bundle()
			if keepSite {
				bundle = export.Body.String()
			}

			state := e.snapshot()
			prefix := netip.MustParsePrefix("203.0.113.0/24")
			teo := &importConfirmClient{
				snapshot: provider.OriginACLSnapshot{
					ZoneID: site.ZoneID, Status: "online", RequestID: "req-describe",
					Current: provider.OriginACLVersion{Version: "v1", Prefixes: []netip.Prefix{prefix}},
					Next:    &provider.OriginACLVersion{Version: "v2", Prefixes: []netip.Prefix{prefix}},
				},
				started: make(chan struct{}),
				release: make(chan struct{}),
			}
			state.TEO = teo
			state.Providers = []provider.Provider{importCoveredTarget{
				id: id,
				snapshot: provider.RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{{
					Protocol: "TCP", Port: "443", CidrBlock: prefix.String(), Action: "ACCEPT",
					Description: "[" + state.Config.EdgeOneTag + "] edgeone", PolicyIndex: "0",
				}}},
			}}
			s := syncer.New(e.runtime)
			e.deps.Syncer = s
			s.SetEdgeOneTransitionStore(e.store)
			s.SetStableConfig(e.deps.ConfigCoordinator().WithStableConfig)
			go s.Run()
			defer func() {
				s.Stop()
				select {
				case <-teo.release:
				default:
					close(teo.release)
				}
				s.Wait()
			}()

			select {
			case <-teo.started:
			case <-time.After(3 * time.Second):
				t.Fatal("正式轮未进入站点确认")
			}
			row, err := e.store.GetEdgeOneTransition(ctx, site.ZoneID)
			if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationUnknown {
				t.Fatalf("请求前未提交 unknown: %+v / %v", row, err)
			}
			if len(s.EdgeOneRuntimeStatus()) != 1 || len(s.RoundCoverage()) != 1 {
				t.Fatal("导入前必须存在站点状态和覆盖证明")
			}

			w = e.do(t, http.MethodPost, "/api/config/import", bundle)
			if w.Code != http.StatusOK || e.snapshot() == state || s.Generation() == 0 {
				t.Fatalf("在途确认期间导入未提交并发布新代次: %d %s", w.Code, w.Body.String())
			}
			s.Stop()
			close(teo.release)
			s.Wait()

			if statuses := s.EdgeOneRuntimeStatus(); len(statuses) != 0 || len(s.RoundCoverage()) != 0 {
				t.Fatalf("旧回包恢复了已清除的运行态: %+v / %+v", statuses, s.RoundCoverage())
			}
			row, err = e.store.GetEdgeOneTransition(ctx, site.ZoneID)
			if err != nil {
				t.Fatal(err)
			}
			if keepSite {
				if row == nil || row.ConfirmationState != config.EdgeOneConfirmationUnknown || row.ConfirmationRequestID != "" {
					t.Fatalf("同站点导入应保留 unknown: %+v", row)
				}
			} else if row != nil {
				t.Fatalf("旧回包恢复了已剪枝的确认记录: %+v", row)
			}
		})
	}
}
