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
			[]string{"10.0.0.4/32", "10.0.0.6/32", "10.0.0.8/32", "10.0.0.10/32"}),
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

	s.runScheduledRound(st, false, true)

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
	p.createVisible = false
	site := step3Site("zone-a", "TCP", "80,443")
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step3State(t, p, site, step3Settings(true, 0), teo)
	s := step3Syncer(t, st, newStep3CompressionStore())

	s.runScheduledRound(st, false, true)

	if _, creates, deletes := p.counts(); creates != 1 || deletes != 0 {
		t.Fatalf("另一 TAG 等价行不得计 witness: create=%d delete=%d", creates, deletes)
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundFailed {
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

func TestBuild9S3FormalRiskBlockedNoWrite(t *testing.T) {
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

	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("风险阻断必须零写入: create=%d delete=%d", creates, deletes)
	}
	if summary := step3RoundSummary(s); summary.Outcome != RoundPartial {
		t.Fatalf("风险阻断应为 partial: %+v", summary)
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
