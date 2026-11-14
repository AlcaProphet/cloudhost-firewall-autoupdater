package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

type edgeStatusSyncer struct {
	*stubSyncer
	status   syncer.SyncStatus
	runtime  []syncer.EdgeOneSiteStatus
	coverage map[string]syncer.SiteCoverage
}

func (s *edgeStatusSyncer) Status() syncer.SyncStatus { return s.status }
func (s *edgeStatusSyncer) EdgeOneRuntimeStatus() []syncer.EdgeOneSiteStatus {
	return s.runtime
}
func (s *edgeStatusSyncer) RoundCoverage() map[string]syncer.SiteCoverage { return s.coverage }

func TestEdgeOneStatusReadsLocalStateOnly(t *testing.T) {
	e := newTestEnv(t)
	targetID := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-status")
	withTxForTest(t, e.store, func(ctx context.Context, tx *sql.Tx) error {
		return e.store.AddEdgeOneSiteTx(ctx, tx, config.EdgeOneSite{
			ZoneID: "zone-a", TargetIDs: []int{targetID}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		})
	})
	prefix := netip.MustParsePrefix("203.0.113.0/24")
	if err := e.store.SaveEdgeOneObservation(context.Background(), config.EdgeOneTransition{
		ZoneID: "zone-a", BaseVersion: "v1", PendingVersion: "v2",
		PendingCIDRs: []netip.Prefix{prefix}, ConfirmationState: config.EdgeOneConfirmationUnknown,
	}); err != nil {
		t.Fatalf("预置确认状态失败: %v", err)
	}
	if ok, err := e.store.SetEdgeOneConfirmation(context.Background(), "zone-a", "v2", []netip.Prefix{prefix}, config.EdgeOneConfirmationUnknown, "", time.Now().UTC()); err != nil || !ok {
		t.Fatalf("提交 unknown 失败: %v %v", ok, err)
	}
	targets, _ := e.store.GetTargets()
	key := config.TargetIdentity(targets[0])
	snap := syncer.EdgeOneSiteStatus{
		ZoneID: "zone-a", CurrentVersion: "v1", PendingVersion: "v2",
		LastQueryRequestID: "req-query", ConfirmationState: config.EdgeOneConfirmationUnknown,
	}
	spy := &edgeStatusSyncer{
		stubSyncer: &stubSyncer{enabled: true},
		status: syncer.SyncStatus{
			Enabled: true,
			DNS:     syncer.SourceStatus{Enabled: true, Effective: true},
			EdgeOne: syncer.SourceStatus{Enabled: true, Effective: true},
		},
		runtime: []syncer.EdgeOneSiteStatus{snap},
		coverage: map[string]syncer.SiteCoverage{
			"zone-a\x00" + key: {ZoneID: "zone-a", TargetKey: key, Complete: true},
		},
	}
	e.deps.Syncer = spy

	w := e.do(t, http.MethodGet, "/api/edgeone/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("缺少 no-store: %v", w.Header())
	}
	var got syncer.EdgeOneStatus
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !got.Enabled || !got.Effective || len(got.Sites) != 1 {
		t.Fatalf("响应 = %+v", got)
	}
	site := got.Sites[0]
	if site.ZoneID != "zone-a" || site.CurrentVersion != "v1" || site.PendingVersion != "v2" ||
		site.ConfirmationState != config.EdgeOneConfirmationUnknown || site.LastQueryRequestID != "req-query" ||
		site.LastConfirmationErr != syncer.EdgeOneUnknownConfirmationDiagnostic {
		t.Fatalf("站点状态 = %+v", site)
	}
	if len(site.Coverage) != 1 || !site.Coverage[0].Complete {
		t.Fatalf("覆盖状态 = %+v", site.Coverage)
	}
	// Step 10 旧行 protected 为空时，状态只读展示 pending 兼容保护。
	if len(site.ProtectedCIDRs) != 1 || site.ProtectedCIDRs[0] != prefix.String() ||
		site.TransitionBaseVersion != "v1" || site.TransitionPendingVersion != "v2" || site.TransitionUpdatedAt == nil {
		t.Fatalf("过渡保护状态 = %+v", site)
	}
}

func TestEdgeOneStatusNilDependency(t *testing.T) {
	d := &Deps{}
	w := httptest.NewRecorder()
	d.handleGetEdgeOneStatus(w, httptest.NewRequest(http.MethodGet, "/api/edgeone/status", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("缺失依赖状态码 = %d, want 503", w.Code)
	}
}
