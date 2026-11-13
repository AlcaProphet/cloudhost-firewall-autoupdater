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

type pauseConfirmClient struct {
	snapshot provider.OriginACLSnapshot
	waiting  chan struct{}
	release  chan struct{}
	checked  chan bool
}

func (c *pauseConfirmClient) DescribeOriginACL(context.Context, string) (provider.OriginACLSnapshot, error) {
	return c.snapshot, nil
}

func (c *pauseConfirmClient) ConfirmOriginACLUpdate(_ context.Context, _ string, admit func() bool) (string, error) {
	close(c.waiting)
	<-c.release
	allowed := admit()
	c.checked <- allowed
	if !allowed {
		return "", &provider.TEOError{Kind: provider.TEOErrorCanceled, NotSent: true}
	}
	return "req-after-pause", nil
}

func TestEdgeOneConfirmationPauseAfterUnknownCommit(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	id := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-pause")
	site := config.EdgeOneSite{ZoneID: "zone-pause", TargetIDs: []int{id}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"}
	withTxForTest(t, e.store, func(ctx context.Context, tx *sql.Tx) error {
		return e.store.AddEdgeOneSiteTx(ctx, tx, site)
	})
	w := e.do(t, http.MethodPut, "/api/settings", `{"dns_enabled":"false","edgeone_enabled":"true"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("启用 EdgeOne 失败：%d %s", w.Code, w.Body.String())
	}

	state := e.snapshot()
	prefix := netip.MustParsePrefix("203.0.113.0/24")
	teo := &pauseConfirmClient{
		snapshot: provider.OriginACLSnapshot{
			ZoneID: site.ZoneID, Status: "online",
			Current: provider.OriginACLVersion{Version: "v1", Prefixes: []netip.Prefix{prefix}},
			Next:    &provider.OriginACLVersion{Version: "v2", Prefixes: []netip.Prefix{prefix}},
		},
		waiting: make(chan struct{}),
		release: make(chan struct{}),
		checked: make(chan bool, 1),
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
	case <-teo.waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("正式轮未进入确认发送等待")
	}
	row, err := e.store.GetEdgeOneTransition(ctx, site.ZoneID)
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationUnknown {
		t.Fatalf("确认前未提交 unknown：%+v %v", row, err)
	}
	w = e.do(t, http.MethodPost, "/api/sync/pause", "")
	if w.Code != http.StatusOK || s.IsEnabled() || e.snapshot() == state {
		t.Fatalf("暂停未提交并发布新状态：%d %s enabled=%v", w.Code, w.Body.String(), s.IsEnabled())
	}
	close(teo.release)
	select {
	case allowed := <-teo.checked:
		if allowed {
			t.Fatal("暂停提交后仍取得确认发送准入")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("正式轮未完成发送准入检查")
	}
	s.Stop()
	s.Wait()
	row, err = e.store.GetEdgeOneTransition(ctx, site.ZoneID)
	if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationNotAttempted || row.ConfirmationRequestID != "" {
		t.Fatalf("明确未发送的本次记录未恢复：%+v %v", row, err)
	}
}
