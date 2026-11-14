package syncer

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func TestStep15SourceStatusRecordsEdgeOneSuccess(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	s.runScheduledRound(st, false, true)
	status := s.Status()
	if !status.EdgeOne.Enabled || !status.EdgeOne.Effective ||
		status.EdgeOne.LastOutcome != SourceOutcomeSuccess ||
		status.EdgeOne.LastStartedAt == nil || status.EdgeOne.LastCompletedAt == nil ||
		status.EdgeOne.IntervalSeconds <= 0 {
		t.Fatalf("EdgeOne 来源结果 = %+v", status.EdgeOne)
	}
	if status.DNS.Enabled || status.DNS.LastOutcome != "" {
		t.Fatalf("DNS 禁用时不得伪造来源结果：%+v", status.DNS)
	}
}

func TestStep15SourceStatusRecordsBlockedOnOverlap(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, newStep10Store(), map[string]string{"home.example": "203.0.113.7/32"})
	out := s.runScheduledRound(st, true, true)
	if out.Preflight.State != PreflightStateOverlap {
		t.Fatalf("夹具必须重叠：%+v", out.Preflight)
	}
	status := s.Status()
	if status.DNS.LastOutcome != SourceOutcomeBlocked || status.EdgeOne.LastOutcome != SourceOutcomeBlocked {
		t.Fatalf("重叠必须同时标记两侧 blocked：dns=%+v edge=%+v", status.DNS, status.EdgeOne)
	}
	if status.DNS.LastCompletedAt == nil || status.EdgeOne.LastCompletedAt == nil {
		t.Fatal("被阻断来源也必须记录本轮完成时间")
	}
}

func TestStep15SourceStatusRecordsIncomplete(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step10TEO{describeFn: func() (provider.OriginACLSnapshot, error) {
		return provider.OriginACLSnapshot{}, errors.New("fixture: describe failed")
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	s.runScheduledRound(st, false, true)
	status := s.Status()
	if status.EdgeOne.LastOutcome != SourceOutcomeIncomplete {
		t.Fatalf("EdgeOne 查询失败应记录 incomplete：%+v", status.EdgeOne)
	}
}

func TestStep15SourceStatusRecordsTargetFailure(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	p.createVisible = false
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "v2", []string{"198.51.100.0/24"}),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	s.runScheduledRound(st, false, true)
	status := s.Status()
	if status.EdgeOne.LastOutcome != SourceOutcomeFailed {
		t.Fatalf("目标 failed 应进入来源结果：%+v", status.EdgeOne)
	}
	if status.EdgeOne.IntervalSeconds != int64((time.Hour)/time.Second) {
		t.Fatalf("来源间隔 = %d", status.EdgeOne.IntervalSeconds)
	}
}

func TestStep15SourceStatusSeparatesPendingCache(t *testing.T) {
	// 仅 DNS 到期且复用有效 EdgeOne 缓存时，EdgeOne 来源不应被重写为 success。
	store := newStep10Store()
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, map[string]string{"home.example": "192.0.2.10/32"})
	// 先跑一轮 EdgeOne，写入缓存与来源结果。
	s.runScheduledRound(st, false, true)
	before := s.Status().EdgeOne
	// 再跑仅 DNS 到期轮：缓存可复用，EdgeOne 来源结论保持。
	s.runScheduledRound(st, true, false)
	after := s.Status().EdgeOne
	if before.LastCompletedAt == nil || after.LastCompletedAt == nil ||
		!before.LastCompletedAt.Equal(*after.LastCompletedAt) ||
		after.LastOutcome != before.LastOutcome {
		t.Fatalf("仅 DNS 到期不得重写 EdgeOne 来源结果：before=%+v after=%+v", before, after)
	}
	if _, confirms := teo.counts(); confirms != 0 {
		t.Fatalf("无待确认版本时确认调用应为 0，实际 %d", confirms)
	}
}

func TestStep15DiagnosticsDoNotExposeSDKPayload(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	secret := "AKIDsecretSecretKey raw sdk payload"
	teo := &step10TEO{describeFn: func() (provider.OriginACLSnapshot, error) {
		return provider.OriginACLSnapshot{}, errors.New(secret)
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	s.runScheduledRound(st, false, true)
	status := s.EdgeOneRuntimeStatus()
	if len(status) != 1 {
		t.Fatalf("状态 = %+v", status)
	}
	if strings.Contains(status[0].LastQueryError, secret) || strings.Contains(s.Status().EdgeOne.LastOutcome, secret) {
		t.Fatalf("诊断泄露 SDK/凭据载荷：%+v", status[0])
	}
}
