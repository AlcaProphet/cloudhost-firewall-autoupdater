package syncer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
	// 首次单目标既无前序读取，也无下一目标，不得产生末尾等待。
	if len(slept) != 0 {
		t.Fatalf("首次单目标不得限速等待，实际等待 %v", slept)
	}
}

// dryRunCooldownProbe 记录快照调用与结束时间，复用既有零写入计数夹具。
type dryRunCooldownProbe struct {
	*targetProbeProvider
	events     *[]string
	startedAt  time.Time
	finishedAt time.Time
}

func (p *dryRunCooldownProbe) GetSnapshot() (provider.RuleSnapshot, error) {
	p.startedAt = time.Now()
	*p.events = append(*p.events, fmt.Sprintf("read:%d", p.TargetIndex()))
	snapshot, err := p.targetProbeProvider.GetSnapshot()
	p.finishedAt = time.Now()
	return snapshot, err
}

// newDryRunCooldownSyncer 用 20 条同域名规则证明等待与读取次数不随规则数增长。
func newDryRunCooldownSyncer(t *testing.T, clouds []config.CloudType, failFirst bool, targets []int) (*Syncer, *[]string, []*dryRunCooldownProbe, *int) {
	t.Helper()
	events := []string{}
	ps := []provider.Provider{}
	probes := []*dryRunCooldownProbe{}
	for i, ct := range clouds {
		p := &dryRunCooldownProbe{targetProbeProvider: newProbeProvider(ct, i+1), events: &events}
		if failFirst && i == 0 {
			p.snapshotErrs = []error{errors.New("合成快照读取失败")}
		}
		ps = append(ps, p)
		probes = append(probes, p)
	}
	rules := make([]config.DomainRule, 20)
	for i := range rules {
		rules[i] = staticRule(i+1, "a.example.com", "TCP", fmt.Sprint(8000+i))
		rules[i].Targets = targets
	}
	s := newTargetSyncer(t, ps, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
	resolves := 0
	base := s.resolveHostFn
	s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) { resolves++; return base(host) }
	s.sleepFn = func(d time.Duration) {
		if d <= 0 || d > 5*time.Second {
			t.Fatalf("等待时间越界: %v", d)
		}
		events = append(events, "wait")
	}
	return s, &events, probes, &resolves
}

// TestDryRun_CooldownScheduling 验证等待位置、成功/失败与跨调用语义；
// sleep 接缝只记录等待，时间间隔另由结束时间与真实时钟用例验证。
func TestDryRun_CooldownScheduling(t *testing.T) {
	for _, tc := range []struct {
		name      string
		clouds    []config.CloudType
		failFirst bool
		targets   []int
		runs      int
		want      []string
	}{
		{"单目标20规则无末尾等待", []config.CloudType{config.CloudTCLighthouse}, false, nil, 1, []string{"read:1"}},
		{"同平台仅下一目标读取前等待", []config.CloudType{config.CloudTCLighthouse, config.CloudTCLighthouse}, false, nil, 1, []string{"read:1", "wait", "read:2"}},
		{"不同平台无相互等待", []config.CloudType{config.CloudTCLighthouse, config.CloudAliECS}, false, nil, 1, []string{"read:1", "read:2"}},
		{"四平台独立冷却", []config.CloudType{config.CloudTCLighthouse, config.CloudTCCVM, config.CloudAliSWAS, config.CloudAliECS}, false, nil, 1, []string{"read:1", "read:2", "read:3", "read:4"}},
		{"失败无末尾等待", []config.CloudType{config.CloudTCLighthouse}, true, nil, 1, []string{"read:1"}},
		{"失败后下一目标仍限速", []config.CloudType{config.CloudTCLighthouse, config.CloudTCLighthouse}, true, nil, 1, []string{"read:1", "wait", "read:2"}},
		{"成功冷却跨连续调用", []config.CloudType{config.CloudTCLighthouse}, false, nil, 2, []string{"read:1", "wait", "read:1"}},
		{"失败冷却跨连续调用", []config.CloudType{config.CloudTCLighthouse}, true, nil, 2, []string{"read:1", "wait", "read:1"}},
		{"无适用规则不读取不等待", []config.CloudType{config.CloudTCLighthouse}, false, []int{99}, 2, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, events, ps, resolves := newDryRunCooldownSyncer(t, tc.clouds, tc.failFirst, tc.targets)
			for run := 0; run < tc.runs; run++ {
				resp, err := s.DryRun()
				if err != nil {
					t.Fatal(err)
				}
				if len(resp.Results) != len(ps) {
					t.Fatalf("结果数=%d，want %d", len(resp.Results), len(ps))
				}
				for i, result := range resp.Results {
					if result.TargetID != i+1 {
						t.Fatalf("结果顺序变化: %+v", resp.Results)
					}
					wantError := tc.failFirst && i == 0 && run == 0 && tc.targets == nil
					if (result.Error != "") != wantError {
						t.Fatalf("读取错误语义变化: %+v", result)
					}
					if tc.targets != nil {
						assertUnscheduledDryRunResult(t, result)
					}
				}
			}
			if !reflect.DeepEqual(*events, tc.want) {
				t.Fatalf("调用顺序=%v，want %v", *events, tc.want)
			}
			expectedResolves := len(ps) * tc.runs
			for _, p := range ps {
				expectedReads := tc.runs
				if tc.targets != nil {
					expectedReads = 0
					expectedResolves = 0
				}
				n, c, d := p.counts()
				if n != expectedReads || c != 0 || d != 0 {
					t.Fatalf("目标%d snapshot/create/delete=%d/%d/%d，want %d/0/0", p.TargetIndex(), n, c, d, expectedReads)
				}
				if expectedReads == 0 {
					continue
				}
				// 独立写出当前既定间隔，不能让生产错误缩短间隔后测试同步变绿。
				interval := 200 * time.Millisecond
				if p.CloudType() == config.CloudTCLighthouse || p.CloudType() == config.CloudAliSWAS {
					interval = 5 * time.Second
				}
				if s.dryRunNextRead[p.CloudType()].Before(p.finishedAt.Add(interval)) {
					t.Fatalf("目标%d 读取结束后的冷却不足 %v", p.TargetIndex(), interval)
				}
			}
			if *resolves != expectedResolves {
				t.Fatalf("DNS次数=%d，want %d（每目标每host一次）", *resolves, expectedResolves)
			}
			if tc.targets != nil && len(s.dryRunNextRead) != 0 {
				t.Fatalf("无适用规则不得创建冷却: %v", s.dryRunNextRead)
			}
		})
	}
}

// TestDryRun_CooldownSurvivesStateChange 冷却属于 Syncer，热更新/零规则不能清空它。
func TestDryRun_CooldownSurvivesStateChange(t *testing.T) {
	s, events, _, _ := newDryRunCooldownSyncer(t, []config.CloudType{config.CloudTCLighthouse}, false, nil)
	if _, err := s.DryRun(); err != nil {
		t.Fatal(err)
	}
	deadline := s.dryRunNextRead[config.CloudTCLighthouse]
	old := s.runtime.Snapshot()
	emptyConfig := old.Config.DeepCopy()
	emptyConfig.DomainRules = nil
	empty, err := BuildRuntimeState(old, emptyConfig, BreakerPreserve)
	if err != nil {
		t.Fatal(err)
	}
	empty.Providers = old.Providers
	s.ApplyState(empty)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	assertUnscheduledDryRunResult(t, resp.Results[0])
	if !s.dryRunNextRead[config.CloudTCLighthouse].Equal(deadline) {
		t.Fatal("零规则期间冷却被改写")
	}
	next, err := BuildRuntimeState(empty, old.Config, BreakerPreserve)
	if err != nil {
		t.Fatal(err)
	}
	next.Providers = old.Providers
	s.ApplyState(next)
	if _, err := s.DryRun(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*events, []string{"read:1", "wait", "read:1"}) {
		t.Fatalf("状态替换丢失冷却: %v", *events)
	}
}

// TestDryRun_CooldownElapsed 已消耗时间抵扣冷却，并由真实时钟证明剩余等待发生在读取前。
func TestDryRun_CooldownElapsed(t *testing.T) {
	t.Run("DNS阶段已消耗冷却", func(t *testing.T) {
		s, events, _, _ := newDryRunCooldownSyncer(t, []config.CloudType{config.CloudTCLighthouse}, false, nil)
		if _, err := s.DryRun(); err != nil {
			t.Fatal(err)
		}
		base := s.resolveHostFn
		s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
			// 推进冷却到已过期，确定性模拟慢DNS；不靠固定Sleep猜测时序。
			s.dryRunNextRead = map[config.CloudType]time.Time{config.CloudTCLighthouse: time.Now().Add(-time.Second)}
			return base(host)
		}
		if _, err := s.DryRun(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*events, []string{"read:1", "read:1"}) {
			t.Fatalf("已过期仍等待: %v", *events)
		}
	})
	t.Run("真实时钟仅等待剩余时间", func(t *testing.T) {
		s, _, ps, _ := newDryRunCooldownSyncer(t, []config.CloudType{config.CloudTCCVM}, false, nil)
		if _, err := s.DryRun(); err != nil {
			t.Fatal(err)
		}
		// 当前200ms冷却已消耗大部分，仅剩20ms；测试不要求精确运行耗时。
		remaining := 20 * time.Millisecond
		deadline := time.Now().Add(remaining)
		s.dryRunNextRead[config.CloudTCCVM] = deadline
		var waited time.Duration
		s.sleepFn = func(d time.Duration) { waited = d; time.Sleep(d) }
		if _, err := s.DryRun(); err != nil {
			t.Fatal(err)
		}
		if waited < 0 || waited > remaining {
			t.Fatalf("未按剩余时间等待: %v", waited)
		}
		if ps[0].startedAt.Before(deadline) {
			t.Fatalf("云读取早于冷却截止时间: %v < %v", ps[0].startedAt, deadline)
		}
	})
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

var dryRunResultArrayFields = []string{
	"domains",
	"desired",
	"satisfied_by_owned",
	"satisfied_by_external",
	"to_add",
	"cleanup_candidates",
	"cleanup_deferred",
	"dns_errors",
	"unsupported",
	"conflicts",
}

// requireJSONArray 检查字段存在且 JSON 值是非 null 数组，并返回数组元素供上层继续检查。
func requireJSONArray(obj map[string]json.RawMessage, field string) ([]json.RawMessage, error) {
	raw, ok := obj[field]
	if !ok {
		return nil, fmt.Errorf("缺少字段 %q", field)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("字段 %q 不得为 null", field)
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("字段 %q 必须是数组: %w", field, err)
	}
	if items == nil {
		return nil, fmt.Errorf("字段 %q 不得解码为 nil 数组", field)
	}
	return items, nil
}

// validateDryRunArrayJSON 对 Dry Run JSON 的顶层与目标层数组合同做结构化检查。
// 使用 RawMessage 保留字段缺失、null、[] 与错误 JSON 类型之间的差异。
func validateDryRunArrayJSON(raw []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("解码 Dry Run 响应: %w", err)
	}
	if top == nil {
		return fmt.Errorf("Dry Run 响应必须是 JSON 对象")
	}

	results, err := requireJSONArray(top, "results")
	if err != nil {
		return err
	}
	if _, err := requireJSONArray(top, "warnings"); err != nil {
		return err
	}

	for i, rawResult := range results {
		var result map[string]json.RawMessage
		if err := json.Unmarshal(rawResult, &result); err != nil {
			return fmt.Errorf("results[%d] 必须是对象: %w", i, err)
		}
		if result == nil {
			return fmt.Errorf("results[%d] 必须是非 null 对象", i)
		}
		for _, field := range dryRunResultArrayFields {
			if _, err := requireJSONArray(result, field); err != nil {
				return fmt.Errorf("results[%d]: %w", i, err)
			}
		}
	}
	return nil
}

func assertDryRunArraysJSON(t *testing.T, resp DryRunResponse) {
	t.Helper()
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("序列化 Dry Run 响应失败: %v", err)
	}
	if err := validateDryRunArrayJSON(raw); err != nil {
		t.Fatalf("Dry Run 数组 JSON 合同不满足: %v; body=%s", err, raw)
	}
}

// TestDryRun_ArraysNeverNull 所有数组字段必须存在、序列化为 JSON array，且不得为 null。
func TestDryRun_ArraysNeverNull(t *testing.T) {
	t.Run("有适用规则目标", func(t *testing.T) {
		p := newProbeProvider(config.CloudTCLighthouse, 1)
		rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
		s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

		resp, err := s.DryRun()
		if err != nil {
			t.Fatalf("DryRun 失败: %v", err)
		}
		assertDryRunArraysJSON(t, resp)
	})

	t.Run("无适用规则目标骨架", func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 2)
		rule := staticRule(1, "a.example.com", "TCP", "443")
		rule.Targets = []int{1}
		s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{rule}, nil)

		resp, err := s.DryRun()
		if err != nil {
			t.Fatalf("DryRun 失败: %v", err)
		}
		if len(resp.Results) != 1 {
			t.Fatalf("结果数量 = %d, want 1", len(resp.Results))
		}
		assertUnscheduledDryRunResult(t, resp.Results[0])
		assertDryRunArraysJSON(t, resp)
	})

	t.Run("零目标零结果", func(t *testing.T) {
		s := newTargetSyncer(t, nil, nil, nil)
		resp, err := s.DryRun()
		if err != nil {
			t.Fatalf("DryRun 失败: %v", err)
		}
		if len(resp.Results) != 0 {
			t.Fatalf("结果数量 = %d, want 0", len(resp.Results))
		}
		assertDryRunArraysJSON(t, resp)
	})
}

// TestValidateDryRunArrayJSON_RejectsInvalidShapes 负向控制证明检查器能拒绝旧字符串断言会漏掉的
// null、字段缺失与错误类型，而不是只让当前正确输出通过。
func TestValidateDryRunArrayJSON_RejectsInvalidShapes(t *testing.T) {
	validTarget := func() map[string]any {
		result := make(map[string]any, len(dryRunResultArrayFields))
		for _, field := range dryRunResultArrayFields {
			result[field] = []any{}
		}
		return result
	}
	marshal := func(t *testing.T, value any) []byte {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("构造负向 JSON 失败: %v", err)
		}
		return raw
	}

	for _, field := range []string{"results", "warnings"} {
		for _, mutation := range []struct {
			name  string
			apply func(map[string]any)
		}{
			{name: "null", apply: func(obj map[string]any) { obj[field] = nil }},
			{name: "缺失", apply: func(obj map[string]any) { delete(obj, field) }},
			{name: "对象", apply: func(obj map[string]any) { obj[field] = map[string]any{} }},
		} {
			t.Run("顶层_"+field+"_"+mutation.name, func(t *testing.T) {
				response := map[string]any{"results": []any{validTarget()}, "warnings": []any{}}
				mutation.apply(response)
				if err := validateDryRunArrayJSON(marshal(t, response)); err == nil {
					t.Fatalf("字段 %s 为%s时必须拒绝", field, mutation.name)
				}
			})
		}
	}

	for _, field := range dryRunResultArrayFields {
		for _, mutation := range []struct {
			name  string
			apply func(map[string]any)
		}{
			{name: "null", apply: func(obj map[string]any) { obj[field] = nil }},
			{name: "缺失", apply: func(obj map[string]any) { delete(obj, field) }},
			{name: "对象", apply: func(obj map[string]any) { obj[field] = map[string]any{} }},
		} {
			t.Run("目标层_"+field+"_"+mutation.name, func(t *testing.T) {
				target := validTarget()
				mutation.apply(target)
				response := map[string]any{"results": []any{target}, "warnings": []any{}}
				if err := validateDryRunArrayJSON(marshal(t, response)); err == nil {
					t.Fatalf("字段 %s 为%s时必须拒绝", field, mutation.name)
				}
			})
		}
	}

	t.Run("目标项为null", func(t *testing.T) {
		response := map[string]any{"results": []any{nil}, "warnings": []any{}}
		if err := validateDryRunArrayJSON(marshal(t, response)); err == nil {
			t.Fatal("results 中的 null 目标项必须拒绝")
		}
	})
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

// TestDryRun_CoverageReadyCapabilityBoundary 直接验证输出布尔值，避免同源 planner 对比掩盖误报。
func TestDryRun_CoverageReadyCapabilityBoundary(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed_%t", mixed), func(t *testing.T) {
			stale := staleRule("TCP", "9999", "10.9.9.9/32", "stale", "")
			p := newProbeProvider(config.CloudAliSWAS, 1, stale)
			drop := staticRule(1, "probe.example", "TCP", "443")
			drop.Action = "DROP"
			rules := []config.DomainRule{drop}
			if mixed {
				rules = append(rules, staticRule(2, "probe.example", "TCP", "443"))
				p.rules = append(p.rules, staleRule("TCP", "443", "1.1.1.1/32", "covered", ""))
			}
			s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"probe.example": "1.1.1.1/32"})
			resp, err := s.DryRun()
			if err != nil || len(resp.Results) != 1 {
				t.Fatalf("DryRun: %+v %v", resp, err)
			}
			got := resp.Results[0]
			if got.CoverageReady != mixed || len(got.Unsupported) != 1 || len(got.ToAdd) != 0 || len(got.CleanupCandidates) != 1 {
				t.Fatalf("输出契约不符: %+v", got)
			}
			_, creates, deletes := p.counts()
			if creates != 0 || deletes != 0 {
				t.Fatalf("Dry Run 不得写入: %d/%d", creates, deletes)
			}
			state := s.runtime.Snapshot()
			round := newDNSRound(state)
			res := s.syncTarget(state, p, rules, round)
			round.finish()
			if res.outcome != TargetPartial || res.cleanupDeferred != 1 {
				t.Fatalf("正式目标必须 partial 并保留残留: %+v %v", res, err)
			}
			_, _, deletes = p.counts()
			if deletes != 0 {
				t.Fatalf("unsupported 必须零删除: %d", deletes)
			}
		})
	}
}
