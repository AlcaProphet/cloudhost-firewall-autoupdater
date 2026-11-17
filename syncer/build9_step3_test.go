package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件验证 Build9 Step 3 的正式目标链接线：压缩新增、S1/S2 证明、借用与
// 清理。全部使用本地 Provider/TEO 替身与临时状态，不访问真实云。

type step3CompressionStore struct {
	states    map[string]config.LighthouseCompressionState
	loadErr   error
	saveErr   error
	deleteErr error
	saves     int
	deletes   int
}

func newStep3CompressionStore() *step3CompressionStore {
	return &step3CompressionStore{states: make(map[string]config.LighthouseCompressionState)}
}

func (s *step3CompressionStore) LoadLighthouseCompressionState(c config.LighthouseCompression) (config.LighthouseCompressionState, bool, error) {
	if s.loadErr != nil {
		return config.LighthouseCompressionState{}, false, s.loadErr
	}
	state, ok := s.states[c.Identity()]
	return state, ok, nil
}

func (s *step3CompressionStore) SaveLighthouseCompressionState(state config.LighthouseCompressionState) error {
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.states[state.Identity()] = state
	return nil
}

func (s *step3CompressionStore) DeleteLighthouseCompressionState(c config.LighthouseCompression) error {
	s.deletes++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.states, c.Identity())
	return nil
}

func step3Site(zone string, protocol, ports string) config.EdgeOneSite {
	return config.EdgeOneSite{ZoneID: zone, TargetIDs: []int{0}, Protocol: protocol, Ports: ports, AddressFamily: "ipv4"}
}

func step3State(t *testing.T, p *targetProbeProvider, site config.EdgeOneSite, settings config.LighthouseCompression, teo provider.OriginACLClient) *RuntimeState {
	t.Helper()
	st := step8State(t, false, true, time.Hour, time.Hour, p, []config.EdgeOneSite{site})
	st.Config.SyncEnabled = false
	st.Config.LighthouseCompression = []config.LighthouseCompression{settings}
	st.TEO = teo
	return st
}

func step3Syncer(t *testing.T, st *RuntimeState, store LighthouseCompressionStore) *Syncer {
	t.Helper()
	s := New(NewRuntimeManager(st))
	s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
	s.SetEdgeOneTransitionStore(newStep10Store())
	if store != nil {
		s.SetLighthouseCompressionStore(store)
	}
	s.sleepFn = func(time.Duration) {}
	s.resolveHostFn = step8Resolve(st)
	return s
}

func step3Settings(enabled bool, limit int64) config.LighthouseCompression {
	return config.LighthouseCompression{
		CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "sg-step8",
		Enabled: enabled, ExtraIPv4Allow: limit,
	}
}

func step3RoundSummary(s *Syncer) RoundSummary {
	if status := s.Status().LastRound; status != nil {
		return *status
	}
	return RoundSummary{}
}

func TestBuild9S3FormalCompressionAddAndProof(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	site := step3Site("zone-a", "TCP", "80,443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if snapshots, creates, deletes := p.counts(); creates != 1 || deletes != 0 || snapshots < 2 {
		t.Fatalf("正式压缩应一次新增零删除: snapshot=%d create=%d delete=%d", snapshots, creates, deletes)
	}
	if p.ruleCount() != 1 {
		t.Fatalf("S1 后应有一条压缩行: %+v", p.rules)
	}
	summary := step3RoundSummary(s)
	if summary.Outcome != RoundSuccess || summary.Added != 1 || summary.Deleted != 0 {
		t.Fatalf("目标结果错误: %+v", summary)
	}
}

func TestBuild9S3FormalS1MissingBlocksCleanupAndConfirm(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	p.createVisible = false
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "v2", []string{"203.0.113.0/24"}),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	st.Config.SyncEnabled = true
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 1 || deletes != 0 {
		t.Fatalf("S1 缺压缩行必须零删除: create=%d delete=%d", creates, deletes)
	}
	if _, confirms := teo.counts(); confirms != 0 {
		t.Fatalf("S1 缺压缩行必须零 TEO 确认: confirms=%d", confirms)
	}
	summary := step3RoundSummary(s)
	if summary.Outcome != RoundFailed || summary.Deleted != 0 {
		t.Fatalf("S1 缺压缩行目标应失败: %+v", summary)
	}
}

func TestBuild9S3FormalBorrowPeak100To96(t *testing.T) {
	rules := make([]config.RuleInfo, 0, 96)
	rules = append(rules,
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.0/32", Action: "ACCEPT", Description: "[edgeone]"},
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.2/32", Action: "ACCEPT", Description: "[edgeone]"},
	)
	for i := 0; i < 4; i++ {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "ALL", CidrBlock: fmt.Sprintf("10.0.0.%d/32", 12+i*2),
			Action: "ACCEPT", Description: "[edgeone]",
		})
	}
	for len(rules) < 96 {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)),
			Action: "ACCEPT", Description: "manual",
		})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	site := step3Site("zone-a", "TCP", "1-65535")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"10.0.0.0/32", "10.0.0.2/32"}, "v2",
			[]string{"10.0.0.4/32", "10.0.0.6/32", "10.0.0.8/32", "10.0.0.10/32"}),
	}}
	st := step3State(t, p, site, step3Settings(true, 8), teo)
	st.Config.SyncEnabled = false
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 1 || deletes != 1 {
		t.Fatalf("借用应一次新增一次清理: create=%d delete=%d", creates, deletes)
	}
	if sizes := p.deleteSizes(); len(sizes) != 1 || sizes[0] != 4 {
		t.Fatalf("应删除 4 条旧压缩残留: %v", sizes)
	}
	if p.ruleCount() != 96 {
		t.Fatalf("S2 应恢复到 96 条: %d", p.ruleCount())
	}
	summary := step3RoundSummary(s)
	if summary.Outcome != RoundSuccess || summary.Added != 4 || summary.Deleted != 4 {
		t.Fatalf("借用结果错误: %+v", summary)
	}
}

func TestBuild9S3FormalBorrowPeak100To97Blocked(t *testing.T) {
	rules := make([]config.RuleInfo, 0, 96)
	rules = append(rules,
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.0/32", Action: "ACCEPT", Description: "[edgeone]"},
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.2/32", Action: "ACCEPT", Description: "[edgeone]"},
	)
	for i := 0; i < 3; i++ {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "ALL", CidrBlock: fmt.Sprintf("10.0.0.%d/32", 12+i*2),
			Action: "ACCEPT", Description: "[edgeone]",
		})
	}
	for len(rules) < 96 {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)),
			Action: "ACCEPT", Description: "manual",
		})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	site := step3Site("zone-a", "TCP", "1-65535")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"10.0.0.0/32", "10.0.0.2/32"}, "v2",
			[]string{"10.0.0.0/32", "10.0.0.2/32", "10.0.0.4/32", "10.0.0.6/32", "10.0.0.8/32", "10.0.0.10/32"}),
	}}
	st := step3State(t, p, site, step3Settings(true, 8), teo)
	st.Config.SyncEnabled = false
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("借用证据不足必须零新增零删除: create=%d delete=%d", creates, deletes)
	}
	summary := step3RoundSummary(s)
	if summary.Outcome != RoundPartial || summary.Added != 0 || summary.Deleted != 0 {
		t.Fatalf("借用阻断应为 partial: %+v", summary)
	}
}

func TestBuild9S3FormalS2MissingCoverageKeepsCounts(t *testing.T) {
	rules := make([]config.RuleInfo, 0, 96)
	rules = append(rules,
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.0/32", Action: "ACCEPT", Description: "[edgeone]"},
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.2/32", Action: "ACCEPT", Description: "[edgeone]"},
	)
	for i := 0; i < 4; i++ {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "ALL", CidrBlock: fmt.Sprintf("10.0.0.%d/32", 12+i*2),
			Action: "ACCEPT", Description: "[edgeone]",
		})
	}
	for len(rules) < 96 {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)),
			Action: "ACCEPT", Description: "manual",
		})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	p.afterDelete = func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, rule := range p.rules {
			if rule.CidrBlock == "10.0.0.10/32" {
				p.rules = append(p.rules[:i], p.rules[i+1:]...)
				return
			}
		}
	}
	site := step3Site("zone-a", "TCP", "1-65535")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"10.0.0.0/32", "10.0.0.2/32"}, "v2",
			[]string{"10.0.0.4/32", "10.0.0.6/32", "10.0.0.8/32", "10.0.0.10/32"}),
	}}
	st := step3State(t, p, site, step3Settings(true, 8), teo)
	st.Config.SyncEnabled = true
	s := step3Syncer(t, st, newStep3CompressionStore())

	var observed map[string]any
	cancel := s.bus.SubscribeIngress(notifier.EventSyncError, func(event notifier.Event) {
		observed, _ = event.Data["compression"].(map[string]any)
	})
	defer cancel()
	s.runScheduledRound(st, false, true)
	if observed["stage"] != "s2" || observed["actual_rows"] != 95 || observed["peak"] != 100 || observed["failure"] == "" {
		t.Fatalf("S2 失败应保留实际读数、规划峰值与原因: %+v", observed)
	}

	if _, confirms := teo.counts(); confirms != 0 {
		t.Fatalf("S2 缺覆盖必须零确认: confirms=%d", confirms)
	}
	summary := step3RoundSummary(s)
	if summary.Outcome != RoundFailed || summary.Deleted != 4 {
		t.Fatalf("S2 缺覆盖应失败并保留实际删除计数: %+v", summary)
	}
}

func TestBuild9S3FormalDisabledReplacementClearsPending(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: "203.0.0.0/16", Action: "ACCEPT", Description: "[edgeone]"},
	)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	settings := step3Settings(false, 0)
	store := newStep3CompressionStore()
	store.states[settings.Identity()] = config.LighthouseCompressionState{
		CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "sg-step8",
		PendingReview: true, PendingReason: "disabled", PendingSince: "2026-11-17T00:00:00Z",
	}
	st := step3State(t, p, site, settings, teo)
	s := step3Syncer(t, st, store)

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 1 || deletes != 1 {
		t.Fatalf("关闭后应补精确覆盖并删除旧压缩行: create=%d delete=%d", creates, deletes)
	}
	if len(p.createdDesc) == 0 || p.hasRule("203.0.0.0/16", "443") {
		t.Fatalf("关闭后不得保留压缩行: %+v", p.rules)
	}
	if p.ruleCount() != 1 {
		t.Fatalf("替换后应只剩精确行: %d", p.ruleCount())
	}
	if _, found, _ := store.LoadLighthouseCompressionState(settings); found {
		t.Fatal("可信回读满足后应清除待复查")
	}
}

func TestBuild9S3FormalDisabledProtectedResidualKeepsPending(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: "203.0.0.0/16", Action: "ACCEPT", Description: "[edgeone]"},
	)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "v2", []string{"203.0.113.0/24"}),
	}}
	settings := step3Settings(false, 0)
	store := newStep3CompressionStore()
	store.states[settings.Identity()] = config.LighthouseCompressionState{
		CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "sg-step8",
		PendingReview: true, PendingReason: "disabled", PendingSince: "2026-11-17T00:00:00Z",
	}
	st := step3State(t, p, site, settings, teo)
	s := step3Syncer(t, st, store)
	// 让过渡保护覆盖旧宽行；没有可信释放证据时不得删除。
	s.SetEdgeOneTransitionStore(&step3StaticTransitionStore{rows: map[string]config.EdgeOneTransition{
		"zone-a": {
			ZoneID: "zone-a", BaseVersion: "v1", PendingVersion: "v2",
			PendingCIDRs:      []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")},
			ProtectedCIDRs:    []netip.Prefix{netip.MustParsePrefix("203.0.0.0/16")},
			ConfirmationState: config.EdgeOneConfirmationAccepted,
		},
	}})

	s.runScheduledRound(st, false, true)

	if _, _, deletes := p.counts(); deletes != 0 {
		t.Fatalf("受保护残留不得删除: deletes=%d", deletes)
	}
	if _, found, _ := store.LoadLighthouseCompressionState(settings); !found {
		t.Fatal("未满足收窄条件时不得清除待复查")
	}
}

type step3StaticTransitionStore struct {
	rows map[string]config.EdgeOneTransition
}

func (s *step3StaticTransitionStore) GetEdgeOneTransition(_ context.Context, zone string) (*config.EdgeOneTransition, error) {
	row, ok := s.rows[zone]
	if !ok {
		return nil, nil
	}
	cp := row
	return &cp, nil
}
func (s *step3StaticTransitionStore) ListEdgeOneTransitions(context.Context) ([]config.EdgeOneTransition, error) {
	out := make([]config.EdgeOneTransition, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, row)
	}
	return out, nil
}
func (s *step3StaticTransitionStore) SaveEdgeOneObservation(context.Context, config.EdgeOneTransition) error {
	return errors.New("unused")
}
func (s *step3StaticTransitionStore) SaveEdgeOneProtectedObservation(context.Context, config.EdgeOneTransition, bool) error {
	return errors.New("unused")
}
func (s *step3StaticTransitionStore) DeleteEdgeOneTransition(context.Context, string) error {
	return errors.New("unused")
}
func (s *step3StaticTransitionStore) SetEdgeOneConfirmation(context.Context, string, string, []netip.Prefix, string, string, time.Time) (bool, error) {
	return false, errors.New("unused")
}
func (s *step3StaticTransitionStore) RestoreUnsentEdgeOneConfirmation(context.Context, string, string, []netip.Prefix, time.Time) (bool, error) {
	return false, errors.New("unused")
}

func TestBuild9S3FormalS1PartialPortBlocks(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "80", CidrBlock: "203.0.113.0/24", Action: "ACCEPT", Description: "[edgeone]"},
	)
	p.createVisible = false
	site := step3Site("zone-a", "TCP", "80,443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 1 || deletes != 0 {
		t.Fatalf("S1 缺端口必须零删除: create=%d delete=%d", creates, deletes)
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundFailed {
		t.Fatalf("S1 缺端口目标应失败: %+v", summary)
	}
}

func TestBuild9S3FormalOtherTagEquivalentNotWitness(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "80,443", CidrBlock: "203.0.113.0/24", Action: "ACCEPT", Description: "[auto-dns]"},
	)
	site := step3Site("zone-a", "TCP", "80,443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("另一 TAG 等价行不得计 witness 或重复新增: create=%d delete=%d", creates, deletes)
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundPartial {
		t.Fatalf("另一 TAG 等价不得完成覆盖: %+v", summary)
	}
}

func TestBuild9S3FormalOppositeActionBlocks(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: "203.0.113.0/24", Action: "DROP", Description: "manual"},
	)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("相反动作必须零新增零删除: create=%d delete=%d", creates, deletes)
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundPartial {
		t.Fatalf("相反动作应阻断为 partial: %+v", summary)
	}
}

func TestBuild9S3FormalRestartKeepsPendingAtFullQuota(t *testing.T) {
	rules := []config.RuleInfo{{Protocol: "TCP", Port: "443", CidrBlock: "203.0.0.0/16", Action: "ACCEPT", Description: "[edgeone]"}}
	for len(rules) < 100 {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)),
			Action: "ACCEPT", Description: "manual",
		})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	site := step3Site("zone-a", "TCP", "443")
	snapshots := []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}
	teo := &step10TEO{snapshots: snapshots}
	settings := step3Settings(false, 0)
	store := newStep3CompressionStore()
	store.states[settings.Identity()] = config.LighthouseCompressionState{
		CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "sg-step8",
		PendingReview: true, PendingReason: "disabled", PendingSince: "2026-11-17T00:00:00Z",
	}
	st := step3State(t, p, site, settings, teo)
	s := step3Syncer(t, st, store)
	s.runScheduledRound(st, false, true)
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("满配额关闭态应零写入: create=%d delete=%d", creates, deletes)
	}

	st2 := step3State(t, p, site, settings, teo)
	s2 := step3Syncer(t, st2, store)
	s2.runScheduledRound(st2, false, true)
	if _, creates2, deletes2 := p.counts(); creates2 != 0 || deletes2 != 0 {
		t.Fatalf("重启后不得产生新写入: create=%d delete=%d", creates2, deletes2)
	}
	if _, found, _ := store.LoadLighthouseCompressionState(settings); !found {
		t.Fatal("满配额时重启后不得清除待复查")
	}
}

func TestBuild9S3FormalDeleteFailureKeepsDeferred(t *testing.T) {
	rules := make([]config.RuleInfo, 0, 96)
	rules = append(rules,
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.0/32", Action: "ACCEPT", Description: "[edgeone]"},
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.2/32", Action: "ACCEPT", Description: "[edgeone]"},
	)
	for i := 0; i < 4; i++ {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "ALL", CidrBlock: fmt.Sprintf("10.0.0.%d/32", 12+i*2),
			Action: "ACCEPT", Description: "[edgeone]",
		})
	}
	for len(rules) < 96 {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)),
			Action: "ACCEPT", Description: "manual",
		})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	p.deleteErr = errors.New("fixture delete failure")
	site := step3Site("zone-a", "TCP", "1-65535")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"10.0.0.0/32", "10.0.0.2/32"}, "v2",
			[]string{"10.0.0.4/32", "10.0.0.6/32", "10.0.0.8/32", "10.0.0.10/32"}),
	}}
	st := step3State(t, p, site, step3Settings(true, 8), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	summary := step3RoundSummary(s)
	if summary.Outcome != RoundSuccess || summary.Added != 4 || summary.Deleted != 0 || summary.CleanupDeferred == 0 {
		t.Fatalf("删除失败应成功且保留 cleanup_deferred: %+v", summary)
	}
	if p.ruleCount() != 100 {
		t.Fatalf("删除失败后旧行必须保留: %d", p.ruleCount())
	}
}

func TestBuild9S3FormalConcurrentAddCleansWithoutNewWrites(t *testing.T) {
	rules := make([]config.RuleInfo, 0, 96)
	rules = append(rules,
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.0/32", Action: "ACCEPT", Description: "[edgeone]"},
		config.RuleInfo{Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.2/32", Action: "ACCEPT", Description: "[edgeone]"},
	)
	for i := 0; i < 4; i++ {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "ALL", CidrBlock: fmt.Sprintf("10.0.0.%d/32", 12+i*2),
			Action: "ACCEPT", Description: "[edgeone]",
		})
	}
	for len(rules) < 96 {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)),
			Action: "ACCEPT", Description: "manual",
		})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	addedOnce := false
	p.afterDelete = func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if addedOnce {
			return
		}
		addedOnce = true
		p.rules = append(p.rules, config.RuleInfo{
			Protocol: "TCP", Port: "ALL", CidrBlock: "10.0.0.20/32", Action: "ACCEPT", Description: "[edgeone]",
		})
	}
	site := step3Site("zone-a", "TCP", "1-65535")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"10.0.0.0/32", "10.0.0.2/32"}, "v2",
			[]string{"10.0.0.4/32", "10.0.0.6/32", "10.0.0.8/32", "10.0.0.10/32"}),
		step11Snapshot("v1", []string{"10.0.0.0/32", "10.0.0.2/32"}, "v2",
			[]string{"10.0.0.4/32", "10.0.0.6/32", "10.0.0.8/32", "10.0.0.10/32"}),
	}}
	st := step3State(t, p, site, step3Settings(true, 8), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())
	s.runScheduledRound(st, false, true)
	if p.ruleCount() != 97 {
		t.Fatalf("并发新增后应实际保留 97 条: %d", p.ruleCount())
	}
	_, createsBefore, deletesBefore := p.counts()
	s.runScheduledRound(st, false, true)
	_, createsAfter, deletesAfter := p.counts()
	if createsAfter != createsBefore {
		t.Fatalf("实际仍高于常态上限时不得新增: create %d→%d", createsBefore, createsAfter)
	}
	if p.ruleCount() != 96 {
		t.Fatalf("第二轮应通过安全清理回到 96 条: %d", p.ruleCount())
	}
	if deletesAfter < deletesBefore {
		t.Fatalf("并发残留应只通过受控清理减少: delete %d→%d", deletesBefore, deletesAfter)
	}
}

func TestBuild9S3FormalCompressedSiteStillConfirms(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	site := step3Site("zone-a", "TCP", "443")
	snap := step11Snapshot("v1", []string{"203.0.113.0/24"}, "v2", []string{"203.0.113.0/24"})
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{snap, snap}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	st.Config.SyncEnabled = true
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, confirms := teo.counts(); confirms != 1 {
		t.Fatalf("压缩站点完整证明后应确认一次: confirms=%d", confirms)
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundSuccess {
		t.Fatalf("确认成功不应改变目标结果: %+v", summary)
	}
}

func TestBuild9S3FormalCleanupBatchSubset(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.0/24", Action: "ACCEPT", Description: "[edgeone]"},
		config.RuleInfo{Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.0/25", Action: "ACCEPT", Description: "[edgeone]"},
	)
	site := step3Site("zone-a", "TCP", "80")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"10.0.0.0/25"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 256), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if sizes := p.deleteSizes(); len(sizes) != 1 || sizes[0] != 1 {
		t.Fatalf("整批失去覆盖时只提交一个候选: %v", sizes)
	}
	if p.ruleCount() != 1 {
		t.Fatalf("应保留仍承担覆盖的窄行: %+v", p.rules)
	}
}

func TestBuild9S3FormalPendingClearFailureKeepsState(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: "203.0.0.0/16", Action: "ACCEPT", Description: "[edgeone]"},
	)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	settings := step3Settings(false, 0)
	store := newStep3CompressionStore()
	store.states[settings.Identity()] = config.LighthouseCompressionState{
		CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "sg-step8",
		PendingReview: true, PendingReason: "disabled", PendingSince: "2026-11-17T00:00:00Z",
	}
	store.deleteErr = errors.New("fixture state failure")
	st := step3State(t, p, site, settings, teo)
	s := step3Syncer(t, st, store)

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 1 || deletes != 1 {
		t.Fatalf("替换写入不应被状态删除失败回滚: create=%d delete=%d", creates, deletes)
	}
	if _, found, _ := store.LoadLighthouseCompressionState(settings); !found {
		t.Fatal("状态删除失败时必须保留待复查")
	}
}

func TestBuild9S3FormalRestartFromSQLite(t *testing.T) {
	rules := []config.RuleInfo{{Protocol: "TCP", Port: "443", CidrBlock: "203.0.0.0/16", Action: "ACCEPT", Description: "[edgeone]"}}
	for len(rules) < 100 {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)),
			Action: "ACCEPT", Description: "manual",
		})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	settings := step3Settings(false, 0)
	path := filepath.Join(t.TempDir(), "step3-restart.db")
	store, err := config.OpenStore(path)
	if err != nil {
		t.Fatalf("打开临时 SQLite 失败: %v", err)
	}
	defer store.Close()
	if err := store.SaveLighthouseCompressionState(config.LighthouseCompressionState{
		CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "sg-step8",
		PendingReview: true, PendingReason: "disabled", PendingSince: "2026-11-17T00:00:00Z",
	}); err != nil {
		t.Fatalf("保存待复查失败: %v", err)
	}

	st := step3State(t, p, site, settings, teo)
	s := step3Syncer(t, st, store)
	s.runScheduledRound(st, false, true)
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("满配额关闭态应零写入: create=%d delete=%d", creates, deletes)
	}
	if _, found, err := store.LoadLighthouseCompressionState(settings); err != nil || !found {
		t.Fatalf("SQLite 待复查应保留: found=%v err=%v", found, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭临时库失败: %v", err)
	}

	reopened, err := config.OpenStore(path)
	if err != nil {
		t.Fatalf("重启打开 SQLite 失败: %v", err)
	}
	defer reopened.Close()
	if _, found, err := reopened.LoadLighthouseCompressionState(settings); err != nil || !found {
		t.Fatalf("重启后待复查丢失: found=%v err=%v", found, err)
	}
	st2 := step3State(t, p, site, settings, teo)
	s2 := step3Syncer(t, st2, reopened)
	s2.runScheduledRound(st2, false, true)
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("重启后不得产生新写入: create=%d delete=%d", creates, deletes)
	}
}

func TestBuild9S3FormalNarrowingReplacement(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0,
		config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: "203.0.0.0/16", Action: "ACCEPT", Description: "[edgeone]"},
	)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 1 || deletes != 1 {
		t.Fatalf("收窄应先新增精确规则再清理宽规则: create=%d delete=%d", creates, deletes)
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundSuccess || p.hasRule("203.0.0.0/16", "443") {
		t.Fatalf("收窄后应保留精确覆盖: %+v", summary)
	}
}

func TestBuild9S3FormalHardQuotaBlocked(t *testing.T) {
	rules := make([]config.RuleInfo, 0, 97)
	for i := 0; i < 97; i++ {
		rules = append(rules, config.RuleInfo{
			Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", i),
			Action: "ACCEPT", Description: "manual",
		})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	site := step3Site("zone-a", "TCP", "80,443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("硬配额阻断必须零写入: create=%d delete=%d", creates, deletes)
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundPartial {
		t.Fatalf("硬配额阻断应为 partial: %+v", summary)
	}
}

func TestBuild9S3FormalDNSContinuesWhenCompressionBlocked(t *testing.T) {
	rules := []config.RuleInfo{{
		Protocol: "TCP", Port: "443", CidrBlock: "203.0.0.0/16", Action: "ACCEPT", Description: "[edgeone]",
	}}
	for len(rules) < 95 {
		rules = append(rules, config.RuleInfo{Protocol: "TCP", Port: "9999",
			CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)), Action: "ACCEPT", Description: "manual"})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{site})
	st.Config.SyncEnabled = false
	st.Config.LighthouseCompression = []config.LighthouseCompression{step3Settings(true, 0)}
	st.TEO = teo
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, true, true)

	created := p.createdDesc
	if len(created) != 1 || !strings.Contains(created[0], "[auto-dns]") {
		t.Fatalf("压缩阻断时 DNS 新增必须继续: %+v", created)
	}
	if p.hasRule("203.0.113.0/24", "443") {
		t.Fatal("压缩阻断时不得新增压缩行")
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundPartial {
		t.Fatalf("压缩阻断应为 partial: %+v", summary)
	}
}

func TestBuild9S3FormalVersionMismatchReplans(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	p.createErrs = []error{errors.New("firewallversionmismatch")}
	site := step3Site("zone-a", "TCP", "443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if creates := p.createCalls; creates != 2 {
		t.Fatalf("版本竞争应重新新增: create=%d", creates)
	}
	if snapshots := p.snapshotCalls; snapshots < 3 {
		t.Fatalf("版本竞争后应重新 Describe 并核验: snapshot=%d", snapshots)
	}
	if p.ruleCount() != 1 {
		t.Fatalf("第二次 attempt 应完成压缩新增: %+v", p.rules)
	}
}

func TestBuild9S3FormalOnlyDueSourceWrites(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dns, edge bool
		wantTag   string
	}{
		{"only DNS due", true, false, "[auto-dns]"},
		{"only EdgeOne due", false, true, "[edgeone]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProbeProvider(config.CloudTCLighthouse, 0,
				step11Rule("198.51.100.0/24", "443"),
				config.RuleInfo{CidrBlock: "192.0.2.11/32", Port: "443", Protocol: "TCP", Action: "ACCEPT", Description: "[auto-dns]"},
			)
			st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{step3Site("zone-a", "TCP", "443")})
			st.Config.LighthouseCompression = []config.LighthouseCompression{step3Settings(true, 0)}
			st.TEO = &step10TEO{snapshots: []provider.OriginACLSnapshot{step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil)}}
			s := step3Syncer(t, st, newStep3CompressionStore())
			s.runScheduledRound(st, tc.dns, tc.edge)
			_, creates, deletes := p.counts()
			if creates != 1 || deletes != 0 || len(p.createdDesc) != 1 || !strings.HasPrefix(p.createdDesc[0], tc.wantTag) ||
				!p.hasRule("192.0.2.11/32", "443") || !p.hasRule("198.51.100.0/24", "443") {
				t.Fatalf("只准入到期来源，应保留另一来源及未满足完整覆盖清理门的规则: creates=%v deletes=%d rules=%v", p.createdDesc, deletes, p.rules)
			}
		})
	}
	// Show the ownership filter when S1 does have complete base coverage
	// and the regular planner returns an unrelated DNS deletion candidate.
	t.Run("EdgeOne due with covered DNS and stale DDNS", func(t *testing.T) {
		p := newProbeProvider(config.CloudTCLighthouse, 0,
			config.RuleInfo{CidrBlock: "192.0.2.10/32", Port: "443", Protocol: "TCP", Action: "ACCEPT", Description: "[auto-dns]"},
			config.RuleInfo{CidrBlock: "192.0.2.11/32", Port: "443", Protocol: "TCP", Action: "ACCEPT", Description: "[auto-dns]"},
			step11Rule("198.51.100.0/24", "443"),
		)
		st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{step3Site("zone-a", "TCP", "443")})
		st.Config.LighthouseCompression = []config.LighthouseCompression{step3Settings(true, 2)}
		st.TEO = &step10TEO{snapshots: []provider.OriginACLSnapshot{step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil)}}
		s := step3Syncer(t, st, newStep3CompressionStore())
		s.runScheduledRound(st, false, true)
		_, creates, deletes := p.counts()
		if creates != 1 || deletes != 1 || !p.hasRule("192.0.2.11/32", "443") || p.hasRule("198.51.100.0/24", "443") {
			t.Fatalf("只能新增和清理 EdgeOne TAG: create=%d delete=%d rules=%v", creates, deletes, p.rules)
		}
	})
}

func TestBuild9S3FormalNormalCapForDNSOnlyInstance(t *testing.T) {
	var rules []config.RuleInfo
	for i := 0; i < 96; i++ {
		rules = append(rules, config.RuleInfo{CidrBlock: fmt.Sprintf("198.51.100.%d/32", i),
			Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: "manual"})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	st := step8State(t, true, false, time.Hour, time.Hour, p, nil)
	st.Config.LighthouseCompression = []config.LighthouseCompression{step3Settings(true, 0)}
	s := step3Syncer(t, st, newStep3CompressionStore())
	preview, err := s.DryRun()
	if err != nil || preview.ExecutionUnits[0].PlannedAdd != 0 {
		t.Fatalf("模拟中 DDNS 也不得使用预留: %+v err=%v", preview.ExecutionUnits, err)
	}
	s.runScheduledRound(st, true, false)
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 || p.ruleCount() != 96 {
		t.Fatalf("无绑定站点时启用实例也应保持常态上限: create=%d delete=%d rows=%d", creates, deletes, p.ruleCount())
	}
}

type step3ReadbackDropProvider struct {
	*targetProbeProvider
	drop config.RuleInfo
}

func (p *step3ReadbackDropProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	snapshot, err := p.targetProbeProvider.GetSnapshot()
	if p.snapshotCalls >= 2 {
		snapshot.Rules = append(snapshot.Rules, p.drop)
	}
	return snapshot, err
}

func TestBuild9S3FormalS1WildcardDropFreezesCleanup(t *testing.T) {
	for _, protocol := range []string{"ALL", "TCP"} {
		t.Run(protocol, func(t *testing.T) {
			p := newProbeProvider(config.CloudTCLighthouse, 0, step11Rule("198.51.100.0/24", "443"))
			wrapped := &step3ReadbackDropProvider{p, config.RuleInfo{CidrBlock: "203.0.113.0/24",
				Protocol: protocol, Port: "", Action: "DROP", Description: "manual"}}
			snap := step11Snapshot("v1", []string{"203.0.113.0/24"}, "v2", []string{"203.0.113.0/24"})
			teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{snap, snap}}
			st := step3State(t, p, step3Site("zone-a", "TCP", "443"), step3Settings(true, 256), teo)
			st.Config.SyncEnabled = true
			st.Providers[0] = wrapped
			s := step3Syncer(t, st, newStep3CompressionStore())
			s.runScheduledRound(st, false, true)
			_, creates, deletes := p.counts()
			_, confirms := teo.counts()
			if creates != 1 || deletes != 0 || confirms != 0 || step3RoundSummary(s).Outcome != RoundFailed {
				t.Fatalf("S1 通配 DROP 应冻结删除和确认: create=%d delete=%d confirm=%d", creates, deletes, confirms)
			}
		})
	}
}

func TestBuild9S3FormalManualExactCoverage(t *testing.T) {
	for _, ports := range []string{"80", "80,443"} {
		t.Run(ports, func(t *testing.T) {
			manual := config.RuleInfo{CidrBlock: "203.0.113.0/24", Port: "80", Protocol: "TCP", Action: "ACCEPT", Description: "manual"}
			p := newProbeProvider(config.CloudTCLighthouse, 0, manual)
			snap := step11Snapshot("v1", []string{"203.0.113.0/24"}, "v2", []string{"203.0.113.0/24"})
			teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{snap, snap}}
			st := step3State(t, p, step3Site("zone-a", "TCP", ports), step3Settings(true, 0), teo)
			st.Config.SyncEnabled = true
			s := step3Syncer(t, st, newStep3CompressionStore())
			s.runScheduledRound(st, false, true)
			_, creates, deletes := p.counts()
			_, confirms := teo.counts()
			want := 0
			if ports == "80,443" {
				want = 1
			}
			if creates != want || deletes != 0 || confirms != 1 || p.rules[0] != manual ||
				want == 1 && p.rules[1].Port != "443" {
				t.Fatalf("手工规则仅补精确覆盖，应只新增欠缺的 443: create=%d delete=%d confirm=%d rules=%+v", creates, deletes, confirms, p.rules)
			}
		})
	}
}

func TestBuild9S3FormalDisabledProtectedOverRiskBlocksConfirmation(t *testing.T) {
	broad := step11Rule("203.0.0.0/16", "443")
	exact := step11Rule("203.0.113.0/24", "443")
	p := newProbeProvider(config.CloudTCLighthouse, 0, broad, exact)
	snap := step11Snapshot("v1", []string{exact.CidrBlock}, "v2", []string{exact.CidrBlock})
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{snap, snap}}
	settings := step3Settings(false, 9)
	store := newStep3CompressionStore()
	store.states[settings.Identity()] = config.LighthouseCompressionState{
		CloudType: settings.CloudType, Region: settings.Region, ResourceID: settings.ResourceID, PendingReview: true,
	}
	st := step3State(t, p, step3Site("zone-a", "TCP", "443"), settings, teo)
	st.Config.SyncEnabled = true
	s := step3Syncer(t, st, store)
	var observed map[string]any
	cancel := s.bus.SubscribeIngress(notifier.EventTargetSyncComplete, func(event notifier.Event) {
		observed, _ = event.Data["compression"].(map[string]any)
	})
	defer cancel()
	s.runScheduledRound(st, false, true)
	_, creates, deletes := p.counts()
	_, confirms := teo.counts()
	if creates != 0 || deletes != 0 || confirms != 0 || step3RoundSummary(s).Outcome != RoundPartial {
		t.Fatalf("关闭态受保护超额时须保留规则并冻结确认: create=%d delete=%d confirm=%d", creates, deletes, confirms)
	}
	if observed["risk_limit"] != int64(0) || observed["risk_extra_ipv4"] != uint64(65280) ||
		observed["pending_review"] != true || observed["protected_active"] != true {
		t.Fatalf("应记录残留、保护和待复查: %+v", observed)
	}
}

func TestBuild9S3FormalBorrowOldOnlyAndRestoreAfterRestart(t *testing.T) {
	current := []string{"10.0.0.0/32", "10.0.0.2/32", "10.0.0.4/32", "10.0.0.6/32"}
	next := []string{"10.0.0.8/32", "10.0.0.10/32", "10.0.0.12/32", "10.0.0.14/32"}
	update := step11Snapshot("v1", current, "v2", next)
	site := step3Site("zone-a", "TCP", "1-65535")
	for _, keepHistory := range []bool{false, true} {
		t.Run(fmt.Sprintf("historical protection=%v", keepHistory), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "borrow.db")
			store, err := config.OpenStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			rules := make([]config.RuleInfo, 0, 96)
			for _, cidr := range current {
				rules = append(rules, step11Rule(cidr, "ALL"))
			}
			if keepHistory {
				row := config.EdgeOneTransition{ZoneID: "zone-a", BaseVersion: "v1", PendingVersion: "v2",
					PendingCIDRs: update.Next.Prefixes, ConfirmationState: config.EdgeOneConfirmationNotAttempted}
				row.ProtectedCIDRs = append(append([]netip.Prefix{}, row.PendingCIDRs...), netip.MustParsePrefix(current[1]),
					netip.MustParsePrefix(current[2]), netip.MustParsePrefix(current[3]))
				if err := store.SaveEdgeOneProtectedObservation(context.Background(), row, false); err != nil {
					t.Fatal(err)
				}
			}
			for len(rules) < 96 {
				rules = append(rules, config.RuleInfo{CidrBlock: fmt.Sprintf("198.51.100.%d/32", len(rules)),
					Protocol: "TCP", Port: "9999", Action: "ACCEPT", Description: "manual"})
			}
			p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
			teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{update, update, update}}
			st := step3State(t, p, site, step3Settings(true, 0), teo)
			st.Config.SyncEnabled = true
			s := step3Syncer(t, st, store)
			s.SetEdgeOneTransitionStore(store)
			preview, err := s.DryRun()
			if err != nil {
				t.Fatal(err)
			}
			c := preview.ExecutionUnits[0].Compression
			expected := 96
			if keepHistory {
				expected = 99
			}
			if c.Peak == nil || *c.Peak != 100 || c.ExpectedAfter == nil || *c.ExpectedAfter != expected {
				t.Fatalf("应按新版生效后剩余保护推演: %+v", c)
			}
			s.runScheduledRound(st, false, true)
			_, creates, deletes := p.counts()
			_, confirms := teo.counts()
			if keepHistory {
				if creates != 0 || deletes != 0 || confirms != 0 || p.ruleCount() != 96 {
					t.Fatalf("99 条余量不允许借用: creates=%d deletes=%d confirms=%d rows=%d", creates, deletes, confirms, p.ruleCount())
				}
				return
			}
			if creates != 1 || deletes != 0 || confirms != 1 || p.ruleCount() != 100 {
				t.Fatalf("换版等待时应借用 4 条且旧版仍保留: creates=%d deletes=%d confirms=%d rows=%d", creates, deletes, confirms, p.ruleCount())
			}
			row, err := store.GetEdgeOneTransition(context.Background(), "zone-a")
			if err != nil || row == nil || row.ConfirmationState != config.EdgeOneConfirmationAccepted {
				t.Fatalf("重启前应已持久化确认状态: %+v err=%v", row, err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := config.OpenStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			st = step3State(t, p, site, step3Settings(true, 0), &step10TEO{snapshots: []provider.OriginACLSnapshot{
				step11Snapshot("v2", next, "", nil),
			}})
			st.Config.SyncEnabled = true
			s = step3Syncer(t, st, reopened)
			s.SetEdgeOneTransitionStore(reopened)
			s.runScheduledRound(st, false, true)
			_, creates, deletes = p.counts()
			summary := step3RoundSummary(s)
			if creates != 1 || deletes != 1 || p.ruleCount() != 96 ||
				summary.Added != 0 || summary.Deleted != 4 || summary.Outcome != RoundSuccess {
				t.Fatalf("重启后可信生效须只删 4 条: creates=%d deletes=%d rows=%d summary=%+v", creates, deletes, p.ruleCount(), summary)
			}
		})
	}
}

func TestBuild9S3FormalNarrowingWithinOneAvailableSlot(t *testing.T) {
	rules := []config.RuleInfo{step11Rule("10.0.0.0/28", "443")}
	for len(rules) < 95 {
		rules = append(rules, config.RuleInfo{CidrBlock: fmt.Sprintf("198.51.100.%d/32", len(rules)),
			Protocol: "TCP", Port: "9999", Action: "ACCEPT", Description: "manual"})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	snap := step11Snapshot("v1", []string{"10.0.0.0/32", "10.0.0.2/32"}, "", nil)
	st := step3State(t, p, step3Site("zone-a", "TCP", "443"), step3Settings(true, 2),
		&step10TEO{snapshots: []provider.OriginACLSnapshot{snap, snap}})
	s := step3Syncer(t, st, newStep3CompressionStore())
	preview, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	unit := preview.ExecutionUnits[0]
	if unit.Compression.Peak == nil || *unit.Compression.Peak != 96 || len(unit.Compression.Rows) != 1 ||
		unit.Compression.Rows[0].CIDR != "10.0.0.0/30" {
		t.Fatalf("只剩一个常态名额时应选择仍在旧放行范围内的 /30: %+v", unit)
	}
	s.runScheduledRound(st, false, true)
	_, creates, deletes := p.counts()
	if creates != 1 || deletes != 1 || p.hasRule("10.0.0.0/28", "443") ||
		!p.hasRule("10.0.0.0/30", "443") || step3RoundSummary(s).Outcome != RoundSuccess {
		t.Fatalf("应在一个名额内收窄到 /30: create=%d delete=%d rules=%v", creates, deletes, p.rules)
	}
}

func TestBuild9S3FormalCleanupOnlyWithExistingCoverageAtFullQuota(t *testing.T) {
	rules := []config.RuleInfo{step11Rule("203.0.113.0/25", "443"), step11Rule("203.0.113.128/25", "443")}
	for i := 0; i < 4; i++ {
		rules = append(rules, step11Rule(fmt.Sprintf("198.51.100.%d/32", i), "443"))
	}
	for len(rules) < 100 {
		rules = append(rules, config.RuleInfo{CidrBlock: fmt.Sprintf("192.0.2.%d/32", len(rules)),
			Protocol: "TCP", Port: "9999", Action: "ACCEPT", Description: "manual"})
	}
	p := newProbeProvider(config.CloudTCLighthouse, 0, rules...)
	current := []string{rules[0].CidrBlock, rules[1].CidrBlock}
	snap := step11Snapshot("v1", current, "v2", current)
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{snap, snap, snap}}
	st := step3State(t, p, step3Site("zone-a", "TCP", "443"), step3Settings(true, 0), teo)
	st.Config.SyncEnabled = true
	s := step3Syncer(t, st, newStep3CompressionStore())
	preview, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	unit := preview.ExecutionUnits[0]
	if unit.PlannedAdd != 0 || unit.Compression.Mode != provider.CompressionModeCleanupOnly ||
		unit.Compression.ExpectedAfter == nil || *unit.Compression.ExpectedAfter != 96 || len(unit.CleanupDeletable) != 4 {
		t.Fatalf("无替代名额也可只清理不承担覆盖的规则: %+v", unit)
	}
	s.runScheduledRound(st, false, true)
	_, creates, deletes := p.counts()
	_, confirms := teo.counts()
	summary := step3RoundSummary(s)
	if creates != 0 || deletes != 1 || confirms != 1 || p.ruleCount() != 96 || summary.Deleted != 4 ||
		summary.Outcome != RoundSuccess || !p.hasRule(current[0], "443") || !p.hasRule(current[1], "443") {
		t.Fatalf("纯清理应保留两条已采用的取证规则并恢复预留: create=%d delete=%d confirm=%d summary=%+v", creates, deletes, confirms, summary)
	}
}

func TestBuild9S3FormalS0FailureDiagnostic(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 0)
	p.snapshotErrs = []error{errors.New("fixture: S0 failed")}
	snap := step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil)
	st := step3State(t, p, step3Site("zone-a", "TCP", "443"), step3Settings(true, 0),
		&step10TEO{snapshots: []provider.OriginACLSnapshot{snap}})
	s := step3Syncer(t, st, newStep3CompressionStore())
	var observed map[string]any
	cancel := s.bus.SubscribeIngress(notifier.EventSyncError, func(event notifier.Event) {
		observed, _ = event.Data["compression"].(map[string]any)
	})
	defer cancel()
	s.runScheduledRound(st, false, true)
	if observed["attempt"] != 1 || observed["stage"] != "s0" || observed["failure"] != "s0_read_failed" ||
		observed["actual_rows"] != nil || observed["peak"] != nil {
		t.Fatalf("没有读到 S0 时应记录本次失败并保留未知值: %+v", observed)
	}
}
