package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
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

func (c *importConfirmClient) ConfirmOriginACLUpdate(_ context.Context, _ string, admit func() bool) (string, error) {
	if !admit() {
		return "", &provider.TEOError{Kind: provider.TEOErrorCanceled, NotSent: true}
	}
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

func TestEdgeOneUnsavedProtectionAcrossConfigChanges(t *testing.T) {
	for _, operation := range []string{"retain", "remove", "reset"} {
		t.Run(operation, func(t *testing.T) {
			e := newTestEnv(t)
			ctx := context.Background()
			id := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-protection")
			zone := "zone-protection"
			create := e.do(t, http.MethodPost, "/api/edgeone/sites",
				edgeOneSiteBody(zone, []int{id}, "TCP", "443", "ipv4"))
			if create.Code != http.StatusCreated {
				t.Fatalf("create site: %d %s", create.Code, create.Body.String())
			}
			settings := e.do(t, http.MethodPut, "/api/settings", `{"dns_enabled":"false","edgeone_enabled":"true"}`)
			if settings.Code != http.StatusOK {
				t.Fatalf("enable EdgeOne: %d %s", settings.Code, settings.Body.String())
			}
			exported := e.do(t, http.MethodPost, "/api/config/export", "")
			if exported.Code != http.StatusOK {
				t.Fatalf("export: %d %s", exported.Code, exported.Body.String())
			}
			e.execRaw(t, `CREATE TRIGGER fixture_transition_failure BEFORE INSERT ON edgeone_origin_transitions
				BEGIN SELECT RAISE(ABORT, 'fixture transition insert failure'); END`)

			state := e.snapshot()
			current := netip.MustParsePrefix("203.0.113.0/24")
			pending := netip.MustParsePrefix("198.51.100.0/24")
			teo := &importConfirmClient{
				snapshot: provider.OriginACLSnapshot{
					ZoneID: zone, Status: "online",
					Current: provider.OriginACLVersion{Version: "v1", Prefixes: []netip.Prefix{current}},
					Next:    &provider.OriginACLVersion{Version: "v2", Prefixes: []netip.Prefix{pending}},
				},
				started: make(chan struct{}),
				release: make(chan struct{}),
			}
			close(teo.release)
			state.TEO = teo
			var rules []config.RuleInfo
			for i, prefix := range []netip.Prefix{current, pending} {
				rules = append(rules, config.RuleInfo{
					Protocol: "TCP", Port: "443", CidrBlock: prefix.String(), Action: "ACCEPT",
					Description: "[" + state.Config.EdgeOneTag + "] edgeone", PolicyIndex: strconv.Itoa(i),
				})
			}
			state.Providers = []provider.Provider{importCoveredTarget{
				id: id, snapshot: provider.RuleSnapshot{Revision: "1", Rules: rules},
			}}
			engine := syncer.New(e.runtime)
			e.deps.Syncer = engine
			engine.SetEdgeOneTransitionStore(e.store)
			engine.SetStableConfig(e.deps.ConfigCoordinator().WithStableConfig)
			go engine.Run()
			t.Cleanup(func() { stopSyncerBounded(t, engine, 3*time.Second) })
			deadline := time.Now().Add(3 * time.Second)
			for engine.Status().EdgeOne.LastCompletedAt == nil {
				if time.Now().After(deadline) {
					t.Fatal("initial EdgeOne round did not complete")
				}
				time.Sleep(time.Millisecond)
			}
			stopSyncerBounded(t, engine, 3*time.Second)

			row, err := e.store.GetEdgeOneTransition(ctx, zone)
			before := engine.EdgeOneRuntimeStatus()
			if err != nil || row != nil || len(before) != 1 ||
				!slices.Equal(before[0].ProtectedCIDRs, []string{pending.String()}) || before[0].TransitionError == "" {
				t.Fatalf("fixture needs protection only in memory: %+v, row=%+v err=%v", before, row, err)
			}
			select {
			case <-teo.started:
				t.Fatal("confirmation sent without committing unknown")
			default:
			}

			var response *httptest.ResponseRecorder
			switch operation {
			case "retain":
				response = e.do(t, http.MethodPost, "/api/config/import", exported.Body.String())
			case "remove":
				response = e.do(t, http.MethodPost, "/api/config/import", validV4Bundle())
			case "reset":
				response = e.do(t, http.MethodPost, "/api/config/reset", "{}")
			}
			if response.Code != http.StatusOK {
				t.Fatalf("configuration change: %d %s", response.Code, response.Body.String())
			}

			row, err = e.store.GetEdgeOneTransition(ctx, zone)
			if err != nil || row != nil {
				t.Fatalf("configuration change persisted runtime protection: row=%+v err=%v", row, err)
			}
			if len(engine.RoundCoverage()) != 0 {
				t.Fatal("configuration change kept old coverage")
			}
			response = e.do(t, http.MethodGet, "/api/edgeone/status", "")
			var status syncer.EdgeOneStatus
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &status) != nil {
				t.Fatalf("read status: %d %s", response.Code, response.Body.String())
			}
			if operation == "retain" {
				if len(status.Sites) != 1 || status.Sites[0].LastQueryAt != nil ||
					!slices.Equal(status.Sites[0].ProtectedCIDRs, []string{pending.String()}) {
					t.Fatalf("same-site import must retain only pending protection: %+v", status)
				}
			} else if len(status.Sites) != 0 || len(engine.EdgeOneRuntimeStatus()) != 0 {
				t.Fatalf("removed site protection survived %s: %+v", operation, status)
			}
		})
	}
}
