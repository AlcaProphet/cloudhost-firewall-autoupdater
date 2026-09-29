package syncer

import (
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// ─── Issue6 A18：整轮成功/失败汇总；Issue6 A11：skipped 进入汇总 ───

// roundFakeProvider 可配置每次 GetRules/CreateRules 的返回值。
type roundFakeProvider struct {
	cloudType   config.CloudType
	targetIndex int

	rules       []config.RuleInfo
	getRulesErr error

	createResult provider.CreateResult
	createErr    error

	getNum    atomic.Int32
	createNum atomic.Int32
}

func (p *roundFakeProvider) Name() string                { return "round-fake" }
func (p *roundFakeProvider) CloudType() config.CloudType { return p.cloudType }
func (p *roundFakeProvider) TargetIndex() int            { return p.targetIndex }
func (p *roundFakeProvider) GetRules() ([]config.RuleInfo, error) {
	p.getNum.Add(1)
	if p.getRulesErr != nil {
		return nil, p.getRulesErr
	}
	return p.rules, nil
}

// GetSnapshot 测试 mock：复用 GetRules 的既有行为，Revision 固定为非空值。
func (p *roundFakeProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	rules, err := p.GetRules()
	return provider.RuleSnapshot{Rules: rules, Revision: "1"}, err
}
func (p *roundFakeProvider) CreateRules(_ provider.RuleSnapshot, rules []config.RuleAction) (provider.CreateResult, error) {
	p.createNum.Add(1)
	if p.createErr != nil {
		return p.createResult, p.createErr
	}
	// 默认：全部写入成功，并模拟云端生效（后续 S1 快照可见），
	// 否则新增后覆盖验证必然失败（Issue7 §4.5）。
	if p.createResult == (provider.CreateResult{}) {
		p.materialize(rules)
		return provider.CreateResult{Written: len(rules)}, nil
	}
	return p.createResult, nil
}

// materialize 把已写入的期望规则变成后续快照可见的云端规则。
func (p *roundFakeProvider) materialize(rules []config.RuleAction) {
	for _, r := range rules {
		p.rules = append(p.rules, config.RuleInfo{
			Protocol: r.Protocol, Port: r.Port, CidrBlock: r.CidrBlock, Ipv6CidrBlock: r.Ipv6CidrBlock,
			Action: r.Action, Description: r.Description, RuleID: "created",
		})
	}
}

// TestRoundSummary_FailedUnitKeepsConfirmedCounts failed 与 added/deleted 正交：
// 单元仍归 failed，但整轮汇总必须保留云端已确认写入量且维持分类不变量。
func TestRoundSummary_FailedUnitKeepsConfirmedCounts(t *testing.T) {
	p := &roundFakeProvider{
		cloudType:    config.CloudTCCVM,
		createResult: provider.CreateResult{Written: 1},
		createErr:    errors.New("permission denied"),
	}
	sum := runOneRound(t, p, []config.DomainRule{tcpRule()}, true)

	if sum.Outcome != RoundFailed || sum.Total != 1 || sum.Failed != 1 || sum.Added != 1 {
		t.Errorf("汇总 = %+v, want failed/total=1/failed=1/added=1", sum)
	}
	if sum.Total != sum.OK+sum.Changed+sum.Failed+sum.Skipped {
		t.Errorf("不变量被破坏: %+v", sum)
	}
}

// TestSyncErrorCarriesConfirmedCounts 错误事件必须携带与目标结果相同的已确认计数。
func TestSyncErrorCarriesConfirmedCounts(t *testing.T) {
	p := &roundFakeProvider{
		cloudType:    config.CloudTCCVM,
		createResult: provider.CreateResult{Written: 1},
		createErr:    errors.New("permission denied"),
	}
	rules := []config.DomainRule{{ID: 1, Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT"}}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"example.com": "1.2.3.4/32"})
	events := make(chan notifier.Event, 1)
	s.bus.Subscribe(notifier.EventSyncError, roundEventSink{ch: events})

	res := s.syncTarget(s.runtime.Snapshot(), p, rules)
	if res.outcome != TargetFailed || res.added != 1 || res.deleted != 0 {
		t.Fatalf("targetResult = %+v, want outcome=failed added=1 deleted=0", res)
	}
	select {
	case ev := <-events:
		if ev.Data["added"] != 1 || ev.Data["deleted"] != 0 {
			t.Errorf("错误事件计数 = added:%v deleted:%v, want 1/0", ev.Data["added"], ev.Data["deleted"])
		}
	case <-time.After(time.Second):
		t.Fatal("未收到 sync:error 事件")
	}
}

func (p *roundFakeProvider) DeleteRules(provider.RuleSnapshot, []config.RuleInfo) (provider.DeleteResult, error) {
	return provider.DeleteResult{}, nil
}
func (p *roundFakeProvider) ConvertPorts(port string) []string { return []string{port} }

// runOneRound 在真实 Run goroutine 中执行恰好一轮并返回汇总。
//
// 通过订阅 EventSyncComplete 取得真实事件负载，既验证事件内容又避免读取内部字段。
func runOneRound(t *testing.T, p provider.Provider, rules []config.DomainRule, enabled bool) RoundSummary {
	t.Helper()

	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: enabled, Theme: "light",
		DomainRules: rules,
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	events := make(chan notifier.Event, 8)
	s.EventBus().Subscribe(notifier.EventSyncComplete, roundEventSink{ch: events})

	go s.Run()
	t.Cleanup(func() { stopRunBounded(t, s, 10*time.Second) })

	select {
	case ev := <-events:
		sum, err := summaryFromEvent(ev)
		if err != nil {
			t.Fatalf("解析 EventSyncComplete 失败: %v", err)
		}
		return sum
	case <-time.After(10 * time.Second):
		t.Fatal("未在限期内收到 sync:complete 事件")
		return RoundSummary{}
	}
}

// roundEventSink 把事件转发到 channel（实现 notifier.Subscriber）。
type roundEventSink struct{ ch chan notifier.Event }

func (s roundEventSink) OnEvent(ev notifier.Event) error {
	select {
	case s.ch <- ev:
	default:
	}
	return nil
}

// summaryFromEvent 从事件负载还原整轮汇总（证明事件里确实带有同一份汇总）。
func summaryFromEvent(ev notifier.Event) (RoundSummary, error) {
	var sum RoundSummary
	if ev.Type != notifier.EventSyncComplete {
		return sum, errors.New("事件类型不是 sync:complete")
	}
	// duration 字符串必须继续存在（兼容合同）
	if _, ok := ev.Data["duration"]; !ok {
		return sum, errors.New("EventSyncComplete.Data 缺少既有 duration 字段")
	}
	get := func(k string) (int, error) {
		v, ok := ev.Data[k]
		if !ok {
			return 0, errors.New("缺少字段 " + k)
		}
		switch n := v.(type) {
		case int:
			return n, nil
		case int64:
			return int(n), nil
		default:
			return 0, errors.New("字段类型不是整数: " + k)
		}
	}
	var err error
	if sum.Total, err = get("total"); err != nil {
		return sum, err
	}
	if sum.OK, err = get("ok"); err != nil {
		return sum, err
	}
	if sum.Changed, err = get("changed"); err != nil {
		return sum, err
	}
	if sum.Failed, err = get("failed"); err != nil {
		return sum, err
	}
	if sum.Skipped, err = get("skipped"); err != nil {
		return sum, err
	}
	if sum.Added, err = get("added"); err != nil {
		return sum, err
	}
	if sum.Deleted, err = get("deleted"); err != nil {
		return sum, err
	}
	outcome, ok := ev.Data["outcome"].(string)
	if !ok {
		return sum, errors.New("缺少 outcome 字段")
	}
	sum.Outcome = RoundOutcome(outcome)
	return sum, nil
}

func tcpRule() config.DomainRule {
	return config.DomainRule{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}}
}

// TestRoundSummary_Idle 无适用规则 → idle，且 last_success 不被刷新。
func TestRoundSummary_Idle(t *testing.T) {
	p := &roundFakeProvider{cloudType: config.CloudTCCVM}
	sum := runOneRound(t, p, []config.DomainRule{}, true)

	if sum.Outcome != RoundIdle {
		t.Errorf("outcome = %q, want idle", sum.Outcome)
	}
	if sum.Total != 0 {
		t.Errorf("total = %d, want 0", sum.Total)
	}
	if sum.Total != sum.OK+sum.Changed+sum.Failed+sum.Skipped {
		t.Errorf("不变量被破坏: total=%d ok=%d changed=%d failed=%d skipped=%d",
			sum.Total, sum.OK, sum.Changed, sum.Failed, sum.Skipped)
	}
	if p.getNum.Load() != 0 {
		t.Errorf("无适用规则时不得调用云 API，实际 GetRules = %d", p.getNum.Load())
	}
}

// TestRoundSummary_SuccessNoChange 期望规则已存在 → ok=1、changed=0 → success。
func TestRoundSummary_SuccessNoChange(t *testing.T) {
	// 现有的本工具规则与期望完全一致 → Diff 不产生增删
	p := &roundFakeProvider{
		cloudType: config.CloudTCCVM,
		rules: []config.RuleInfo{{
			Protocol: "TCP", Port: "443", Action: "ACCEPT",
			CidrBlock: "127.0.0.1/32", Description: "[auto-dns]",
		}},
	}
	sum := runOneRound(t, p, []config.DomainRule{tcpRule()}, true)

	if sum.Outcome != RoundSuccess {
		t.Errorf("outcome = %q, want success", sum.Outcome)
	}
	if sum.Total != 1 || sum.OK != 1 || sum.Changed != 0 || sum.Failed != 0 || sum.Skipped != 0 {
		t.Errorf("汇总 = %+v, want total=1 ok=1 changed=0 failed=0 skipped=0", sum)
	}
	if p.createNum.Load() != 0 {
		t.Errorf("无变更时不得调用 CreateRules，实际 %d", p.createNum.Load())
	}
}

// TestRoundSummary_ChangedCountsAsChanged 成功且发生新增 → changed=1 而非 ok。
//
// 这是 F4 裁决「ok 只计成功且无变更，另加 changed」的判别性用例。
func TestRoundSummary_ChangedCountsAsChanged(t *testing.T) {
	p := &roundFakeProvider{cloudType: config.CloudTCCVM}
	sum := runOneRound(t, p, []config.DomainRule{tcpRule()}, true)

	if sum.Outcome != RoundSuccess {
		t.Errorf("outcome = %q, want success（成功且有变更仍是 success）", sum.Outcome)
	}
	if sum.Total != 1 || sum.OK != 0 || sum.Changed != 1 {
		t.Errorf("汇总 = %+v, want total=1 ok=0 changed=1", sum)
	}
	if sum.Added != 1 {
		t.Errorf("added = %d, want 1", sum.Added)
	}
}

// TestRoundSummary_ProviderErrorIsFailed 云调用失败 → failed=1 → failed，且不刷新 last_success。
func TestRoundSummary_ProviderErrorIsFailed(t *testing.T) {
	p := &roundFakeProvider{
		cloudType:   config.CloudTCCVM,
		getRulesErr: errors.New("some unexpected failure"),
	}
	sum := runOneRound(t, p, []config.DomainRule{tcpRule()}, true)

	if sum.Outcome != RoundFailed {
		t.Errorf("outcome = %q, want failed", sum.Outcome)
	}
	if sum.Total != 1 || sum.Failed != 1 {
		t.Errorf("汇总 = %+v, want total=1 failed=1", sum)
	}
}

// TestRoundSummary_OnlySkippedIsPartial 平台能力限制（planner 级 unsupported）→ partial。
//
// 判别 A11 + A18 的目标级口径：SWAS 无法表达 DROP，规划阶段即列为 unsupported，
// 既不计入 to_add 也不计作成功；目标结果为 partial、added=0、failed=0。
func TestRoundSummary_OnlySkippedIsPartial(t *testing.T) {
	p := &roundFakeProvider{cloudType: config.CloudAliSWAS}
	rule := tcpRule()
	rule.Action = "DROP"
	sum := runOneRound(t, p, []config.DomainRule{rule}, true)

	if sum.Outcome != RoundPartial {
		t.Errorf("outcome = %q, want partial", sum.Outcome)
	}
	if sum.Total != 1 || sum.Skipped != 1 || sum.Failed != 0 {
		t.Errorf("汇总 = %+v, want total=1 skipped=1 failed=0", sum)
	}
	if sum.Added != 0 {
		t.Errorf("added = %d, want 0（跳过绝不计作新增成功）", sum.Added)
	}
}

// TestTargetSyncCompleteCarriesSkippedDetails 目标级完成事件必须携带
// 可结构化消费的规则与原因，而不只是 skipped 整数（Issue7 §7.2）。
func TestTargetSyncCompleteCarriesSkippedDetails(t *testing.T) {
	p := &roundFakeProvider{cloudType: config.CloudAliSWAS}
	rules := []config.DomainRule{{ID: 1, Host: "example.com", Protocol: "TCP", Ports: "443", Action: "DROP"}}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"example.com": "1.2.3.4/32"})
	events := make(chan notifier.Event, 1)
	s.bus.Subscribe(notifier.EventTargetSyncComplete, roundEventSink{ch: events})

	res := s.syncTarget(s.runtime.Snapshot(), p, rules)
	if res.outcome != TargetPartial {
		t.Fatalf("SWAS + DROP 必须为 partial，实际 %q", res.outcome)
	}

	select {
	case ev := <-events:
		if got := ev.Data["skipped"]; got != 1 {
			t.Fatalf("skipped = %#v, want 1", got)
		}
		details, ok := ev.Data["skipped_details"].([]provider.RuleChange)
		if !ok || len(details) != 1 {
			t.Fatalf("skipped_details = %#v, want 1 条结构化详情", ev.Data["skipped_details"])
		}
		detail := details[0]
		if detail.Protocol != "TCP" || detail.Port != "443" || detail.Action != "DROP" || detail.Cidr != "1.2.3.4/32" || detail.SkipReason == "" {
			t.Errorf("详情 = %+v, want 完整规则与非空原因", detail)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到 target:sync_complete 事件")
	}
}

// TestRoundSummary_InvariantAcrossMixedUnits 多单元混合时不变式恒成立。
func TestRoundSummary_InvariantAcrossMixedUnits(t *testing.T) {
	// 两个目标：一个正常写入，一个持续失败
	okP := &roundFakeProvider{cloudType: config.CloudTCCVM, targetIndex: 0}
	badP := &roundFakeProvider{
		cloudType: config.CloudTCLighthouse, targetIndex: 1,
		getRulesErr: errors.New("RequestLimitExceeded"),
	}
	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		// 该规则必须同时适用于两个目标，否则第二个 Provider 没有适用规则
		DomainRules: []config.DomainRule{{
			Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0, 1},
		}},
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	st.Providers = []provider.Provider{okP, badP}
	s := New(NewRuntimeManager(st))

	events := make(chan notifier.Event, 8)
	s.EventBus().Subscribe(notifier.EventSyncComplete, roundEventSink{ch: events})
	go s.Run()
	t.Cleanup(func() { stopRunBounded(t, s, 15*time.Second) })

	select {
	case ev := <-events:
		sum, err := summaryFromEvent(ev)
		if err != nil {
			t.Fatalf("解析事件失败: %v", err)
		}
		if sum.Total != 2 {
			t.Fatalf("total = %d, want 2（两个 Provider × 一条适用规则）", sum.Total)
		}
		if sum.Total != sum.OK+sum.Changed+sum.Failed+sum.Skipped {
			t.Errorf("不变量被破坏: total=%d ok=%d changed=%d failed=%d skipped=%d",
				sum.Total, sum.OK, sum.Changed, sum.Failed, sum.Skipped)
		}
		if sum.Outcome != RoundFailed {
			t.Errorf("存在失败单元时 outcome = %q, want failed", sum.Outcome)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("未收到 sync:complete 事件")
	}
}

// TestRetrySync_AddedCountsOnlyWritten added 必须只累计 Provider 报告的 Written。
//
// 判别 A11：修复前 retrySync 以 len(diff.ToAdd) 累加，Provider 明确跳过的期望规则
// （SWAS 无法表达 DROP）会被虚增为「新增成功」，且每轮重复出现、永不收敛。
func TestRetrySync_AddedCountsOnlyWritten(t *testing.T) {
	p := &roundFakeProvider{
		cloudType: config.CloudTCCVM,
		// 期望 2 条：1 条真实写入、1 条明确跳过
		createResult: provider.CreateResult{Written: 1, Skipped: 1},
	}
	s := &Syncer{}

	added, deleted, skipped, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, nil, "auto-dns")
	if err != nil {
		t.Fatalf("retrySync 失败: %v", err)
	}
	// 注意：本用例的假 Provider 的 GetRules 返回空，Diff 不产生 to_add，因此
	// CreateRules 根本不会被调用（added/skipped 均为 0）。这本身就说明
	// 「added 只在真的发起写入时累计」；下面的断言覆盖显式跳过与真实写入两条路径。
	if added != 0 {
		t.Errorf("added = %d, want 0（必须跟随 Provider 的 Written，而不是 diff.ToAdd 长度）", added)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0（没有 to_add 时不得凭空产生跳过计数）", skipped)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}

	// 真正触发写入：假 Provider 返回空规则集 + 期望 1 条 → to_add=1
	// 此时 added 必须等于 Provider 报告的 Written（0），而不是 diff.ToAdd 长度（1）
	s = &Syncer{}
	p2 := &roundFakeProvider{
		cloudType:    config.CloudTCCVM,
		createResult: provider.CreateResult{Written: 0, Skipped: 1},
	}
	added, deleted, skipped, err = s.retrySync(p2, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
	}, []dns.ResolvedIP{{IP: net.ParseIP("1.2.3.4")}}, "auto-dns")
	if err != nil {
		t.Fatalf("retrySync 失败: %v", err)
	}
	if p2.createNum.Load() == 0 {
		t.Fatal("用例前提：必须真正调用过 CreateRules")
	}
	if added != 0 {
		t.Errorf("added = %d, want 0（必须跟随 Provider 的 Written，而不是 len(diff.ToAdd)）", added)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

// TestRetrySync_SkippedCountsDryRunSkipsWithoutToAdd 全部规则都无法实施时 skipped 仍必须报告。
//
// 判别 A11（批次 6 收尾）：SWAS DROP 规则在 Diff 阶段就被识别为 skipped，
// 因此 diff.ToAdd 为空、CreateRules 根本不会被调用；修复前这种「全 DROP」场景
// 的返回值既不是 added 也无法表达 skipped，计数只能靠 Provider 内部，外部不可见。
func TestRetrySync_SkippedCountsDryRunSkipsWithoutToAdd(t *testing.T) {
	p := &roundFakeProvider{cloudType: config.CloudAliSWAS}
	s := &Syncer{}

	// SWAS + DROP：期望规则无法表达，进入 diff.Skipped 而不是 to_add
	added, deleted, skipped, err := s.retrySync(p, config.DomainRule{
		Host: "example.com", Protocol: "TCP", Ports: "443", Action: "DROP",
	}, []dns.ResolvedIP{{IP: net.ParseIP("1.2.3.4")}}, "auto-dns")
	if err != nil {
		t.Fatalf("retrySync 失败: %v", err)
	}
	if added != 0 {
		t.Errorf("added = %d, want 0（DROP 无法实施，绝不计作新增）", added)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1（Diff 阶段识别的无法实施规则必须计入）", skipped)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
	if p.createNum.Load() != 0 {
		t.Errorf("全部规则都无法实施时不得调用 CreateRules，实际 %d 次", p.createNum.Load())
	}
}

// TestDryRunDoesNotListSWASDropAsToAdd Dry Run 不得把 SWAS DROP 伪装成普通 to_add。
func TestDryRunDoesNotListSWASDropAsToAdd(t *testing.T) {
	p := &roundFakeProvider{cloudType: config.CloudAliSWAS}
	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		DomainRules: []config.DomainRule{{
			Host: "localhost", Protocol: "TCP", Ports: "443", Action: "DROP", Targets: []int{0},
		}},
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("Results 数量 = %d, want 1", len(resp.Results))
	}
	r := resp.Results[0]
	if r.Error != "" {
		t.Fatalf("不应有错误: %s", r.Error)
	}
	if len(r.ToAdd) != 0 {
		t.Errorf("SWAS DROP 不得出现在 to_add（修复前会伪装成普通待添加）: %+v", r.ToAdd)
	}
	if len(r.Unsupported) != 1 {
		t.Fatalf("unsupported 数量 = %d, want 1", len(r.Unsupported))
	}
	if r.Unsupported[0].Code != provider.IssueUnsupportedAction {
		t.Errorf("unsupported[0].Code = %q, want %q", r.Unsupported[0].Code, provider.IssueUnsupportedAction)
	}
	if !strings.Contains(r.Unsupported[0].Message, "DROP") {
		t.Errorf("跳过原因应说明 DROP 限制，实际 %q", r.Unsupported[0].Message)
	}
	if len(r.CleanupCandidates) != 0 {
		t.Errorf("CleanupCandidates 数量 = %d, want 0", len(r.CleanupCandidates))
	}
}
