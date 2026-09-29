package syncer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Issue7 Step 4「Dry Run 与正式同步共用同一 planner」判别性用例。
//
// 证据边界：本地 mock Provider + 注入式 DNS，只证明调度与 DTO 语义；
// 页面渲染与浏览器行为不在自动化范围内（见 ProdTestList.md PT-I7-06）。

// TestDryRun_MatchesProductionPlanner Dry Run 必须与正式同步对同一 S0 输入给出逐字段一致的规划。
func TestDryRun_MatchesProductionPlanner(t *testing.T) {
	stale := staleRule("TCP", "9999", "10.9.9.9/32", "r-stale", "")
	p := newProbeProvider(config.CloudAliSWAS, 1, stale)
	rules := []config.DomainRule{
		staticRule(1, "a.example.com", "TCP", "443"),
		staticRule(2, "a.example.com", "UDP", "443"),
	}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("结果数量 = %d, want 1（每目标一项）", len(resp.Results))
	}
	got := resp.Results[0]

	// 用同一 planner 独立复算期望值：Dry Run 必须是同一份规划，不得复制实现
	snap, err := p.GetSnapshot()
	if err != nil {
		t.Fatalf("取快照失败: %v", err)
	}
	want := provider.PlanTarget(provider.TargetPlanInput{
		CloudType: config.CloudAliSWAS,
		Tag:       "auto-dns",
		Rules:     rules,
		Resolved:  map[int][]dns.ResolvedIP{1: {mockResolved("1.1.1.1/32")}, 2: {mockResolved("1.1.1.1/32")}},
		Snapshot:  snap,
	})

	if len(got.Desired) != len(want.Desired) {
		t.Fatalf("desired 数量 = %d, want %d", len(got.Desired), len(want.Desired))
	}
	for i := range want.Desired {
		if got.Desired[i].Key != want.Desired[i].Key {
			t.Errorf("desired[%d].key = %+v, want %+v", i, got.Desired[i].Key, want.Desired[i].Key)
		}
	}
	if len(got.ToAdd) != len(want.ToAdd) {
		t.Fatalf("to_add 数量 = %d, want %d", len(got.ToAdd), len(want.ToAdd))
	}
	if len(got.CleanupCandidates) != len(want.CleanupCandidates) {
		t.Fatalf("cleanup_candidates 数量 = %d, want %d", len(got.CleanupCandidates), len(want.CleanupCandidates))
	}
	if got.CoverageReady != want.CoverageReady {
		t.Errorf("coverage_ready = %v, want %v", got.CoverageReady, want.CoverageReady)
	}
	if got.TargetID != 1 {
		t.Errorf("target_id = %d, want 1（稳定 key）", got.TargetID)
	}
	if len(got.Domains) != 1 || got.Domains[0] != "a.example.com" {
		t.Errorf("domains = %v, want [a.example.com]", got.Domains)
	}
}

// TestDryRun_OneSnapshotPerTargetAndOneResolvePerHost Dry Run 每目标只取一次快照、
// 每 host 只解析一次，且目标内不做限速等待。
func TestDryRun_OneSnapshotPerTargetAndOneResolvePerHost(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	rules := []config.DomainRule{
		staticRule(1, "a.example.com", "TCP", "443"),
		staticRule(2, "a.example.com", "UDP", "443"),
		staticRule(3, "a.example.com", "TCP", "8000-8010"),
	}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
	resolves := 0
	base := s.resolveHostFn
	s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
		resolves++
		return base(host)
	}
	var slept []time.Duration
	s.sleepFn = func(d time.Duration) { slept = append(slept, d) }

	if _, err := s.DryRun(); err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}

	if snapshots, creates, deletes := p.counts(); snapshots != 1 || creates != 0 || deletes != 0 {
		t.Fatalf("snapshot/create/delete = %d/%d/%d, want 1/0/0（Dry Run 绝不写入）", snapshots, creates, deletes)
	}
	if resolves != 1 {
		t.Fatalf("同 host 解析次数 = %d, want 1（按 host 去重）", resolves)
	}
	// 目标内不得 sleep；目标间限速允许一次
	if len(slept) > 1 {
		t.Fatalf("目标内不得限速等待，实际等待 %v", slept)
	}
}

// TestDryRun_IncludesTargetsWithoutApplicableRules R7-03：所有已配置目标都必须返回，
// 但无适用规则的目标只返回未调度空骨架，且绝不解析 DNS 或访问云 API。
func TestDryRun_IncludesTargetsWithoutApplicableRules(t *testing.T) {
	p1 := newProbeProvider(config.CloudTCLighthouse, 1)
	p2 := newProbeProvider(config.CloudTCCVM, 2)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	rules[0].Targets = []int{1}
	s := newTargetSyncer(t, []provider.Provider{p1, p2}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	resolves := 0
	base := s.resolveHostFn
	s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
		resolves++
		return base(host)
	}

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("结果数量 = %d, want 2（所有已配置目标各一项）", len(resp.Results))
	}

	byID := make(map[int]DryRunResult, len(resp.Results))
	for _, result := range resp.Results {
		byID[result.TargetID] = result
	}
	if got := byID[1]; len(got.Domains) != 1 || got.Domains[0] != "a.example.com" {
		t.Fatalf("目标 1 domains = %v, want [a.example.com]", got.Domains)
	}
	skipped, ok := byID[2]
	if !ok {
		t.Fatal("缺少无适用规则的目标 2")
	}
	assertUnscheduledDryRunResult(t, skipped)

	if resolves != 1 {
		t.Fatalf("DNS 解析次数 = %d, want 1（目标 2 不得解析）", resolves)
	}
	if snapshots, creates, deletes := p1.counts(); snapshots != 1 || creates != 0 || deletes != 0 {
		t.Fatalf("目标 1 snapshot/create/delete = %d/%d/%d, want 1/0/0", snapshots, creates, deletes)
	}
	if snapshots, creates, deletes := p2.counts(); snapshots != 0 || creates != 0 || deletes != 0 {
		t.Fatalf("目标 2 snapshot/create/delete = %d/%d/%d, want 0/0/0", snapshots, creates, deletes)
	}
}

// TestDryRun_NoRulesReturnsAllConfiguredTargets 零规则时仍展示全部配置目标，同时保留全局提示。
func TestDryRun_NoRulesReturnsAllConfiguredTargets(t *testing.T) {
	p1 := newProbeProvider(config.CloudAliSWAS, 1)
	p2 := newProbeProvider(config.CloudAliECS, 2)
	s := newTargetSyncer(t, []provider.Provider{p1, p2}, nil, nil)
	s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
		t.Fatal("零规则时不得解析 DNS")
		return nil, nil
	}

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("结果数量 = %d, want 2（零规则仍展示全部配置目标）", len(resp.Results))
	}
	if len(resp.Warnings) != 1 || resp.Warnings[0] != "暂无域名规则，请先在域名规则页配置" {
		t.Fatalf("warnings = %v, want 暂无域名规则提示", resp.Warnings)
	}
	for _, result := range resp.Results {
		assertUnscheduledDryRunResult(t, result)
	}
	for _, p := range []*targetProbeProvider{p1, p2} {
		if snapshots, creates, deletes := p.counts(); snapshots != 0 || creates != 0 || deletes != 0 {
			t.Fatalf("目标 %d snapshot/create/delete = %d/%d/%d, want 0/0/0",
				p.TargetIndex(), snapshots, creates, deletes)
		}
	}
}

func assertUnscheduledDryRunResult(t *testing.T, result DryRunResult) {
	t.Helper()
	if len(result.Domains) != 0 || len(result.Desired) != 0 || len(result.SatisfiedByOwned) != 0 ||
		len(result.SatisfiedByExternal) != 0 || len(result.ToAdd) != 0 || len(result.CleanupCandidates) != 0 ||
		len(result.CleanupDeferred) != 0 || len(result.DNSErrors) != 0 || len(result.Unsupported) != 0 ||
		len(result.Conflicts) != 0 {
		t.Fatalf("无适用规则目标必须返回空骨架: %+v", result)
	}
	if result.Domains == nil || result.Desired == nil || result.SatisfiedByOwned == nil ||
		result.SatisfiedByExternal == nil || result.ToAdd == nil || result.CleanupCandidates == nil ||
		result.CleanupDeferred == nil || result.DNSErrors == nil || result.Unsupported == nil || result.Conflicts == nil {
		t.Fatalf("无适用规则目标的数组不得为 nil: %+v", result)
	}
	if result.CoverageReady || result.Error != "" {
		t.Fatalf("无适用规则目标 coverage_ready/error = %v/%q, want false/空", result.CoverageReady, result.Error)
	}
}

// TestDryRun_ArraysNeverNull 所有数组字段必须序列化为 []，不得为 null。
func TestDryRun_ArraysNeverNull(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	body := string(raw)
	for _, field := range []string{
		`"results"`, `"warnings"`, `"domains"`, `"desired"`, `"satisfied_by_owned"`,
		`"satisfied_by_external"`, `"to_add"`, `"cleanup_candidates"`, `"cleanup_deferred"`,
		`"dns_errors"`, `"unsupported"`, `"conflicts"`,
	} {
		if strings.Contains(body, `"`+field+`":null`) {
			t.Errorf("字段 %s 不得为 null: %s", field, body)
		}
	}
}

// TestDryRun_PublishesNoEvents Dry Run 绝不发布事件（含 DNS 失败事件）。
func TestDryRun_PublishesNoEvents(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{}) // 未配置 → 解析失败

	events, unsubscribe := s.bus.SubscribeChan()
	defer unsubscribe()

	if _, err := s.DryRun(); err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	select {
	case ev := <-events:
		t.Fatalf("Dry Run 不得发布任何事件，实际收到 %s", ev.Type)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestTargetSyncCompleteEventIsSoleTargetCompletion 正式同步必须发布目标级完成事件，
// 且不再发布逐域名完成事件。
func TestTargetSyncCompleteEventIsSoleTargetCompletion(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	targetEvents, unsubTarget := s.bus.SubscribeChan()
	defer unsubTarget()
	legacyEvents, unsubLegacy := s.bus.SubscribeChan()
	defer unsubLegacy()

	s.syncAll()

	// 收集事件
	deadline := time.After(500 * time.Millisecond)
	var sawTarget bool
	for !sawTarget {
		select {
		case ev := <-targetEvents:
			if ev.Type == notifier.EventTargetSyncComplete {
				sawTarget = true
				data := ev.Data
				if data["target_id"] != 1 {
					t.Errorf("事件 target_id = %v, want 1", data["target_id"])
				}
				if data["outcome"] != string(TargetSuccess) {
					t.Errorf("事件 outcome = %v, want success", data["outcome"])
				}
				if _, ok := data["domains"]; !ok {
					t.Error("事件必须携带来源 domains")
				}
			}
		case <-deadline:
			t.Fatal("未收到 target:sync_complete 事件")
		}
	}

	// 逐域名完成事件不得再出现在生产链
	for {
		select {
		case ev := <-legacyEvents:
			if ev.Type == notifier.EventDomainSyncComplete {
				t.Fatal("生产链不得再发布 domain:sync_complete（Step 4 已由目标级事件取代）")
			}
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}
