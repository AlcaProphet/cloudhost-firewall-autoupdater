package syncer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// ─── Build6 Step 5：不可变运行时状态与单一调度控制语义 ───

// syncCountProvider 可计数、可逐轮阻塞的 Provider。
//
// 用「调用计数 + 进入信号 + 放行 channel」精确控制轮次边界，替代 time.Sleep
// 猜竞态：测试可以明确知道某一轮何时开始、何时结束，以及调度在轮次进行中
// 收到了什么状态变化。
type syncCountProvider struct {
	cloudType   config.CloudType
	targetIndex int

	calls    atomic.Int32
	started  chan struct{} // 每次进入 GetRules 发送一次（带缓冲，不阻塞生产路径）
	release  chan struct{} // 每次 GetRules 等待一次放行
	blockAll bool          // 为 false 时不阻塞（用于只关心触发次数的用例）
}

func newCountingProvider(ct config.CloudType, blockAll bool) *syncCountProvider {
	return &syncCountProvider{
		cloudType: ct,
		started:   make(chan struct{}, 64),
		release:   make(chan struct{}, 64),
		blockAll:  blockAll,
	}
}

func (p *syncCountProvider) Name() string                          { return "counting" }
func (p *syncCountProvider) CloudType() config.CloudType           { return p.cloudType }
func (p *syncCountProvider) TargetIndex() int                      { return p.targetIndex }
func (p *syncCountProvider) ConvertPorts(port string) []string     { return []string{port} }
func (p *syncCountProvider) CreateRules([]config.RuleAction) error { return nil }
func (p *syncCountProvider) DeleteRules([]config.RuleInfo) error   { return nil }

func (p *syncCountProvider) GetRules() ([]config.RuleInfo, error) {
	p.calls.Add(1)
	if p.blockAll {
		p.started <- struct{}{}
		<-p.release
	}
	return nil, nil
}

// waitForCalls 等待 Provider 调用次数达到 want（超时失败，不使用固定 sleep 断言时序）
func waitForCalls(t *testing.T, p *syncCountProvider, want int32, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p.calls.Load() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s（GetRules 调用次数 = %d, want >= %d）", msg, p.calls.Load(), want)
}

// gatedRule 构造一条引用 targetIndex=0 的 localhost 规则
func gatedRule() config.DomainRule {
	return config.DomainRule{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}}
}

// gatedState 构造带指定开关与间隔的运行时状态
func gatedState(t *testing.T, enabled bool, interval time.Duration) *RuntimeState {
	t.Helper()
	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: interval, DNS: "8.8.8.8", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: enabled, Theme: "light",
		DomainRules: []config.DomainRule{gatedRule()},
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	return st
}

// stateWithProvider 基于当前状态构造替换状态并沿用既有 Provider 列表
func stateWithProvider(t *testing.T, s *Syncer, enabled bool, interval time.Duration) *RuntimeState {
	t.Helper()
	st := gatedState(t, enabled, interval)
	if prev := s.runtime.Snapshot(); prev != nil {
		st.Providers = prev.Providers
	}
	return st
}

// newControlSyncer 构造 SyncEnabled=false 起步、单 Provider 的 Syncer
func newControlSyncer(t *testing.T, p provider.Provider, interval time.Duration) *Syncer {
	t.Helper()
	st := gatedState(t, false, interval)
	st.Providers = []provider.Provider{p}
	return New(NewRuntimeManager(st))
}

// startRun 启动 Run 并在清理时 Stop/Wait
func startRun(t *testing.T, s *Syncer) {
	t.Helper()
	go s.Run()
	t.Cleanup(func() {
		s.Stop()
		s.Wait()
	})
}

// TestControl_FalseToTrueTriggersRoundImmediately false → true：立即触发一轮。
func TestControl_FalseToTrueTriggersRoundImmediately(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	s := newControlSyncer(t, p, time.Hour)
	startRun(t, s)

	select {
	case <-p.started:
		t.Fatal("暂停启动不应自动同步")
	case <-time.After(200 * time.Millisecond):
	}

	s.ApplyState(stateWithProvider(t, s, true, time.Hour))
	waitForCalls(t, p, 1, "false → true 必须立即触发一轮同步")

	time.Sleep(120 * time.Millisecond)
	if got := p.calls.Load(); got != 1 {
		t.Errorf("false → true 只应立即触发一轮, GetRules = %d, want 1", got)
	}
}

// TestControl_TrueToTrueOnlyResetsTicker true → true：立即窗口内不得额外同步，
// 但必须按新 interval 到期触发一轮。
func TestControl_TrueToTrueOnlyResetsTicker(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	s := newControlSyncer(t, p, time.Hour)
	startRun(t, s)

	// 先进入启用态：false → true 立即一轮
	s.ApplyState(stateWithProvider(t, s, true, time.Hour))
	waitForCalls(t, p, 1, "false → true 未触发首轮")
	before := p.calls.Load()

	// true → true 换用很短的新 interval
	s.ApplyState(stateWithProvider(t, s, true, 150*time.Millisecond))

	// 新 interval 未到期前不得额外触发
	time.Sleep(50 * time.Millisecond)
	if got := p.calls.Load(); got != before {
		t.Fatalf("true → true 不得额外立即同步: %d → %d", before, got)
	}

	// 新 interval 到期必须触发一轮
	waitForCalls(t, p, before+1, "true → true 未按新 interval 触发同步")
}

// TestControl_TrueToFalseFinishesCurrentRound true → false：
// 开关关闭前已开始的轮次必须完成，之后不再启动新一轮。
func TestControl_TrueToFalseFinishesCurrentRound(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, true)
	st := gatedState(t, true, time.Hour) // 直接以启用态启动 Run
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))
	startRun(t, s)

	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("启用态启动未开始首轮")
	}

	// 轮次进行中关闭开关
	s.ApplyState(stateWithProvider(t, s, false, time.Hour))

	// 当前轮必须被允许完成：放行后计数归零，说明本轮确实走完（calls 不回退）
	p.release <- struct{}{}
	waitForCalls(t, p, 1, "当前轮未完成")
	time.Sleep(150 * time.Millisecond)
	if got := p.calls.Load(); got != 1 {
		t.Fatalf("关闭开关后不得启动新一轮: GetRules = %d, want 1", got)
	}

	// 暂停状态下再次投递状态与触发均不得启动新一轮
	s.ApplyState(stateWithProvider(t, s, false, 10*time.Millisecond))
	s.TriggerSync()
	time.Sleep(200 * time.Millisecond)
	if got := p.calls.Load(); got != 1 {
		t.Fatalf("暂停后排队 trigger/状态通知不得启动新一轮: GetRules = %d, want 1", got)
	}
}

// TestQueuedTriggerNotRunWhilePaused 暂停期间排队的 trigger 在恢复前不得执行，
// 恢复后允许执行（语义：消费前重新检查 enabled）。
func TestQueuedTriggerNotRunWhilePaused(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	s := newControlSyncer(t, p, time.Hour)
	startRun(t, s)

	// 暂停状态下排队 trigger
	s.TriggerSync()
	time.Sleep(200 * time.Millisecond)
	if got := p.calls.Load(); got != 0 {
		t.Fatalf("暂停期间 trigger 不得执行: GetRules = %d, want 0", got)
	}

	// 恢复：false → true 立即一轮（排队 trigger 至多使 calls 增加，不会导致持续轮次）
	s.Resume()
	waitForCalls(t, p, 1, "恢复后应立即执行一轮")
}

// TestStaleTriggerAfterPauseDoesNotAddRound 暂停生效后排队的 trigger 在恢复后
// 不得额外多跑一轮（Build6 §12.5「排队中的 trigger 在消费前也重新检查 enabled」）。
//
// 用状态发布钩子（SetStateAppliedHook）作为确定性屏障：hook 在 Run 消费控制消息
// 之后触发，因此可以精确区分「暂停已生效」与「暂停已提交但循环相位尚未翻转」。
// 「恢复本身恰好触发一轮（false → true）」是共同前提，若陈旧 trigger 也被执行，
// 计数会多 1 —— 因此本用例能确定性捕获「移除 enabled 复查」这类回归。
func TestStaleTriggerAfterPauseDoesNotAddRound(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	s := newControlSyncer(t, p, time.Hour)

	applied := make(chan struct{}, 64)
	s.SetStateAppliedHook(func(*RuntimeState) {
		select {
		case applied <- struct{}{}:
		default:
		}
	})
	// waitApplied 等待 Run 完成一次状态应用；若其间已积压多条通知则视为已应用
	waitApplied := func(msg string) {
		t.Helper()
		select {
		case <-applied:
		case <-time.After(5 * time.Second):
			t.Fatal(msg)
		}
	}

	startRun(t, s)

	// 进入启用态并完成第一轮
	s.ApplyState(stateWithProvider(t, s, true, time.Hour))
	waitForCalls(t, p, 1, "false → true 未触发首轮")
	waitApplied("启用态未应用")
	time.Sleep(100 * time.Millisecond)
	baseline := p.calls.Load()
	if baseline != 1 {
		t.Fatalf("前置条件：启用后应恰好 1 轮，实际 %d", baseline)
	}

	// 暂停并等待其真正生效
	s.ApplyState(stateWithProvider(t, s, false, time.Hour))
	waitApplied("暂停未应用")
	time.Sleep(100 * time.Millisecond)
	if got := p.calls.Load(); got != baseline {
		t.Fatalf("暂停本身不得触发同步: %d → %d", baseline, got)
	}

	// 暂停已生效后排队的 trigger 必须被丢弃
	s.TriggerSync()
	time.Sleep(200 * time.Millisecond)
	if got := p.calls.Load(); got != baseline {
		t.Fatalf("暂停生效期间的 trigger 不得执行: %d → %d", baseline, got)
	}

	// 恢复：只应因 false → true 触发一轮；陈旧 trigger 不得再补一轮
	s.Resume()
	waitApplied("恢复未应用")
	time.Sleep(600 * time.Millisecond)
	if got := p.calls.Load(); got != baseline+1 {
		t.Fatalf("恢复后应恰好新增 1 轮（陈旧 trigger 必须被丢弃）: %d → %d", baseline, got)
	}
}

// TestStopIdempotent Stop 必须幂等：重复与并发调用均不得 panic，Wait 可多次安全返回。
func TestStopIdempotent(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	st := gatedState(t, true, time.Hour)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))
	go s.Run()

	deadline := time.Now().Add(5 * time.Second)
	for !s.Status().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	s.Stop()
	s.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Stop()
			s.Wait()
		}()
	}
	wg.Wait()
}

// TestNoNewRoundAfterStop 停止开始后，即使收到状态变化通知也不得启动新一轮。
func TestNoNewRoundAfterStop(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	st := gatedState(t, true, time.Hour)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))
	go s.Run()
	waitForCalls(t, p, 1, "启用态启动未执行首轮")

	s.Stop()
	s.Wait()
	before := p.calls.Load()

	s.ApplyState(stateWithProvider(t, s, true, 10*time.Millisecond))
	time.Sleep(200 * time.Millisecond)
	if got := p.calls.Load(); got != before {
		t.Fatalf("停止后不得启动新一轮: GetRules = %d → %d", before, got)
	}
}

// TestSyncRoundUsesSingleSnapshot 同步轮次只使用开始时的一份快照：
// 本轮在 Describe 阻塞期间替换运行时状态，本轮仍必须使用旧 TAG，下一轮用新 TAG。
func TestSyncRoundUsesSingleSnapshot(t *testing.T) {
	p := &fakeTagProvider{
		stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0},
		blockOn:      1,
		blocked:      make(chan struct{}, 1),
		release:      make(chan struct{}),
	}
	s := newTagSnapshotSyncer(t, "auto-dns", p)

	roundDone := make(chan struct{})
	go func() {
		defer close(roundDone)
		s.syncAll()
	}()

	waitSignal(t, p.blocked, "本轮未进入首次 Describe")
	// 轮次进行中替换状态：新 TAG 只能从下一轮生效
	s.ApplyState(newRoundState(t, s, newRoundConfig("new-tag")))

	close(p.release)
	waitSignal(t, roundDone, "同步轮次未结束")

	if got := p.createdDescs(); len(got) != 1 || got[0] != "[auto-dns] 测试" {
		t.Errorf("本轮新增描述 = %v, want [[auto-dns] 测试]（必须使用本轮快照）", got)
	}

	// 下一轮使用新快照
	s.syncAll()
	created := p.createdDescs()
	if len(created) != 2 || created[1] != "[new-tag] 测试" {
		t.Errorf("下一轮新增描述 = %v, want 第二轮使用 [new-tag] 测试", created)
	}
}

// TestDryRunUsesSingleSnapshot Dry Run 只取一次完整快照（不受并发状态替换影响）。
func TestDryRunUsesSingleSnapshot(t *testing.T) {
	p := &fakeTagProvider{
		stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0},
		blockOn:      1,
		blocked:      make(chan struct{}, 1),
		release:      make(chan struct{}),
	}
	s := newTagSnapshotSyncer(t, "auto-dns", p)

	type result struct {
		resp DryRunResponse
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := s.DryRun()
		resCh <- result{resp: resp, err: err}
	}()

	waitSignal(t, p.blocked, "Dry Run 未进入首次 Describe")
	s.ApplyState(newRoundState(t, s, newRoundConfig("new-tag")))
	close(p.release)

	got := <-resCh
	if got.err != nil {
		t.Fatalf("DryRun 失败: %v", got.err)
	}
	if len(got.resp.Results) != 1 {
		t.Fatalf("Results 数量 = %d, want 1", len(got.resp.Results))
	}
	if len(got.resp.Results[0].ToAdd) != 1 || got.resp.Results[0].ToAdd[0].Desc != "[auto-dns] 测试" {
		t.Errorf("Dry Run 必须使用开始时快照的 TAG: %+v", got.resp.Results[0].ToAdd)
	}
}

// TestDryRunNotBlockedByPause Dry Run 不受同步暂停开关限制。
func TestDryRunNotBlockedByPause(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	s := newControlSyncer(t, p, time.Hour) // 初始 SyncEnabled=false

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("暂停状态下 DryRun 失败: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("暂停状态下 DryRun 应正常执行, Results = %+v", resp.Results)
	}
}

// TestRuntimeManagerConcurrentSnapshotApply 并发 Snapshot/Apply 无竞态且始终读到完整状态。
func TestRuntimeManagerConcurrentSnapshotApply(t *testing.T) {
	m := NewRuntimeManager(gatedState(t, true, time.Hour))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				m.Apply(gatedState(t, i%2 == 0, time.Duration(j+1)*time.Second))
			}
		}(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				got := m.Snapshot()
				if got == nil {
					t.Errorf("快照不得为 nil")
					return
				}
				if got.Config.DNSFailThreshold != 5 || got.Pool == nil || got.Breaker == nil || got.Resolver == nil {
					t.Errorf("快照不完整: %+v", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestBreakerPolicyPreserveVsReset 普通变更保留熔断失败计数，完整导入允许清空。
func TestBreakerPolicyPreserveVsReset(t *testing.T) {
	old := dns.NewCircuitBreaker(3)
	// 制造 2 次连续失败（未达阈值）
	old.RecordFailure("a.com")
	old.RecordFailure("a.com")

	prev := &RuntimeState{Config: config.RuntimeConfig{DNSFailThreshold: 3}, Breaker: old}
	rc := config.RuntimeConfig{DNSFailThreshold: 3}

	preserved, err := BuildRuntimeState(prev, rc, BreakerPreserve)
	if err != nil {
		t.Fatalf("BreakerPreserve 构造失败: %v", err)
	}
	if preserved.Breaker == old {
		t.Errorf("BreakerPreserve 必须复制而不是复用同一实例")
	}
	if preserved.Breaker.IsOpen("a.com") {
		t.Errorf("失败计数 2 < 阈值 3，不应熔断（计数必须保留）")
	}
	preserved.Breaker.RecordFailure("a.com")
	if !preserved.Breaker.IsOpen("a.com") {
		t.Errorf("复制出的 breaker 必须已持有 2 次失败计数")
	}
	if old.IsOpen("a.com") {
		t.Errorf("复制出的 breaker 不得反向影响原实例")
	}

	reset, err := BuildRuntimeState(prev, rc, BreakerReset)
	if err != nil {
		t.Fatalf("BreakerReset 构造失败: %v", err)
	}
	if reset.Breaker.IsOpen("a.com") {
		t.Errorf("BreakerReset 必须清空失败计数")
	}
	reset.Breaker.RecordFailure("a.com")
	reset.Breaker.RecordFailure("a.com")
	if reset.Breaker.IsOpen("a.com") {
		t.Errorf("重置后的计数应从 0 重新开始（2 < 3）")
	}
}

// TestBreakerPreserveAppliesNewThreshold 阈值变化必须线程安全地进入候选状态。
func TestBreakerPreserveAppliesNewThreshold(t *testing.T) {
	old := dns.NewCircuitBreaker(5)
	old.RecordFailure("a.com")
	old.RecordFailure("a.com")

	prev := &RuntimeState{Config: config.RuntimeConfig{DNSFailThreshold: 5}, Breaker: old}
	next, err := BuildRuntimeState(prev, config.RuntimeConfig{DNSFailThreshold: 2}, BreakerPreserve)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if !next.Breaker.IsOpen("a.com") {
		t.Errorf("新阈值 2 必须立即生效且保留既有 2 次失败计数")
	}
}

// TestBuildRuntimeStateUnknownCloudTypeFails 未注册云类型必须返回可判定为请求侧错误的结果。
func TestBuildRuntimeStateUnknownCloudTypeFails(t *testing.T) {
	rc := config.RuntimeConfig{
		Targets: []config.TargetConfig{{CloudType: config.CloudType("unknown_type"), Region: "r", ResourceID: "i"}},
	}
	if _, err := BuildRuntimeState(nil, rc, BreakerReset); err == nil {
		t.Fatal("未注册云类型必须构造失败")
	}
}

// TestBuildRuntimeStateDelegatesCredentialsToPool 凭据必须进入 pool 且不残留全局状态。
func TestBuildRuntimeStateDelegatesCredentialsToPool(t *testing.T) {
	rc := config.RuntimeConfig{
		Credentials: config.Credentials{
			TencentSecretID: "AKIDx", TencentSecretKey: "sk",
			AliyunAccessKeyID: "LTAI", AliyunAccessKeySecret: "aks",
		},
		Targets: []config.TargetConfig{{ID: 1, CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lhins-a"}},
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	creds := st.Pool.Credentials()
	if creds.TencentSecretID != "AKIDx" || creds.AliyunAccessKeySecret != "aks" {
		t.Errorf("凭据未进入 ClientPool: %+v", creds)
	}
	if len(st.Providers) != 1 || st.Providers[0].TargetIndex() != 1 {
		t.Errorf("Provider 列表未按目标构造: %+v", st.Providers)
	}
}

// TestBuildRuntimeStateDeepCopiesConfig 发布到运行时的配置必须是深拷贝：
// 修改候选构造入参不得影响已发布状态。
func TestBuildRuntimeStateDeepCopiesConfig(t *testing.T) {
	rc := config.RuntimeConfig{
		DomainRules: []config.DomainRule{{ID: 1, Host: "a.com", Targets: []int{1, 2}}},
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	rc.DomainRules[0].Host = "mutated"
	rc.DomainRules[0].Targets[0] = 99

	if st.Config.DomainRules[0].Host != "a.com" || st.Config.DomainRules[0].Targets[0] != 1 {
		t.Errorf("发布后的配置被构造入参污染: %+v", st.Config.DomainRules)
	}
}
