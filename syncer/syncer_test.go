package syncer

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// stubProvider 测试用模拟 Provider（GetRules 返回空，可计数调用次数）
type stubProvider struct {
	cloudType   config.CloudType
	targetIndex int
	block       chan struct{} // 非 nil 时 GetRules 阻塞等待释放（用于并发测试）
	getRulesNum atomic.Int32  // GetRules 调用次数
}

func (m *stubProvider) Name() string                { return "stub" }
func (m *stubProvider) CloudType() config.CloudType { return m.cloudType }
func (m *stubProvider) TargetIndex() int            { return m.targetIndex }
func (m *stubProvider) GetRules() ([]config.RuleInfo, error) {
	m.getRulesNum.Add(1)
	if m.block != nil {
		<-m.block
	}
	return nil, nil
}

// GetSnapshot 测试 mock：复用 GetRules 的既有行为，Revision 固定为非空值。
func (m *stubProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	rules, err := m.GetRules()
	return provider.RuleSnapshot{Rules: rules, Revision: "1"}, err
}
func (m *stubProvider) CreateRules(_ provider.RuleSnapshot, rules []config.RuleAction) (provider.CreateResult, error) {
	return provider.CreateResult{Written: len(rules)}, nil
}
func (m *stubProvider) DeleteRules(provider.RuleSnapshot, []config.RuleInfo) (provider.DeleteResult, error) {
	return provider.DeleteResult{}, nil
}
func (m *stubProvider) ConvertPorts(port string) []string {
	return portconv.Parse(port)
}

// TestDryRun_EmptyConfig 无 providers 无规则 → Warnings 两条、Results 为空数组
func TestDryRun_EmptyConfig(t *testing.T) {
	cfg := &config.Config{Tag: "auto-dns"}
	s := newSyncer(t, cfg, nil)

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	if len(resp.Warnings) != 2 {
		t.Errorf("Warnings 数量 = %d, want 2（目标与规则各一条）", len(resp.Warnings))
	}
	if resp.Results == nil || len(resp.Results) != 0 {
		t.Errorf("Results 应为空数组, got %v", resp.Results)
	}
}

// TestDryRun_Detail 单域名规则 → 目标级结果的目标字段与 to_add 明细
// 使用 CVM 云类型（限速 200ms），避免 Lighthouse/SWAS 的 5s 间隔拖慢测试
func TestDryRun_Detail(t *testing.T) {
	p := &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0}
	cfg := &config.Config{
		Tag: "auto-dns",
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Comment: "测试", Targets: []int{0}},
		},
	}
	s := newSyncer(t, cfg, []provider.Provider{p})

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("Results 数量 = %d, want 1", len(resp.Results))
	}
	r := resp.Results[0]
	if len(r.Domains) != 1 || r.Domains[0] != "localhost" {
		t.Errorf("Domains = %v, want [localhost]", r.Domains)
	}
	if r.TargetID != 0 {
		t.Errorf("TargetID = %d, want 0（稳定 key）", r.TargetID)
	}
	if r.Error != "" {
		t.Fatalf("不应有错误: %s", r.Error)
	}
	// localhost 应解析出 IPv4（EnableIPv6=false 过滤 IPv6），期望 1 条待添加
	if len(r.ToAdd) != 1 {
		t.Fatalf("ToAdd 数量 = %d, want 1", len(r.ToAdd))
	}
	ca := r.ToAdd[0]
	if ca.Cidr != "127.0.0.1/32" {
		t.Errorf("ToAdd[0].Cidr = %s, want 127.0.0.1/32", ca.Cidr)
	}
	if ca.Protocol != "TCP" || ca.Port != "443" {
		t.Errorf("ToAdd[0] 协议/端口 = %s/%s, want TCP/443", ca.Protocol, ca.Port)
	}
	if ca.Desc != "[auto-dns] 测试" {
		t.Errorf("ToAdd[0].Desc = %s, want [auto-dns] 测试", ca.Desc)
	}
	if len(r.CleanupCandidates) != 0 {
		t.Errorf("CleanupCandidates 数量 = %d, want 0", len(r.CleanupCandidates))
	}
	if len(r.Desired) != 1 {
		t.Errorf("Desired 数量 = %d, want 1", len(r.Desired))
	}
}

// TestDryRun_Concurrent 并发调用：第二个返回 ErrDryRunInProgress
func TestDryRun_Concurrent(t *testing.T) {
	release := make(chan struct{})
	p := &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0, block: release}
	cfg := &config.Config{
		Tag: "auto-dns",
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}},
		},
	}
	s := newSyncer(t, cfg, []provider.Provider{p})

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.DryRun()
	}()
	time.Sleep(200 * time.Millisecond) // 确保第一个 DryRun 已持有锁

	_, err := s.DryRun()
	if err != ErrDryRunInProgress {
		t.Errorf("并发第二个 DryRun 应返回 ErrDryRunInProgress, got %v", err)
	}
	close(release)
	<-done
}

// ─── Step 8：暂停门控测试 ───

// newGateSyncer 构造门控测试用 Syncer（单目标单规则，Interval 取 1h 避免 ticker 干扰计数）
func newGateSyncer(t *testing.T, syncEnabled bool) (*Syncer, *stubProvider) {
	t.Helper()
	p := &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0}
	cfg := &config.Config{
		Tag:         "auto-dns",
		Interval:    time.Hour,
		SyncEnabled: syncEnabled,
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}},
		},
	}
	return newSyncer(t, cfg, []provider.Provider{p}), p
}

// TestRun_DisabledStartup SyncEnabled=false 启动：不执行同步 → Resume 后执行一次
func TestRun_DisabledStartup(t *testing.T) {
	s, p := newGateSyncer(t, false)

	go s.Run()
	time.Sleep(300 * time.Millisecond)
	if n := p.getRulesNum.Load(); n != 0 {
		t.Errorf("禁用启动不应执行同步, GetRules 调用次数 = %d, want 0", n)
	}
	if s.IsEnabled() {
		t.Error("IsEnabled 应为 false")
	}

	s.Resume()
	time.Sleep(600 * time.Millisecond) // 等待恢复后 syncAll 完成（限速 200ms + 解析）
	if n := p.getRulesNum.Load(); n < 1 {
		t.Errorf("Resume 后应执行一次同步, GetRules 调用次数 = %d, want >= 1", n)
	}
	if !s.IsEnabled() {
		t.Error("Resume 后 IsEnabled 应为 true")
	}

	s.Stop()
	s.Wait()
}

// TestPauseResume_Flow 正常运行 → Pause 暂停（ticker/trigger 均不触发）→ Resume 恢复
func TestPauseResume_Flow(t *testing.T) {
	s, p := newGateSyncer(t, true)

	go s.Run()
	time.Sleep(400 * time.Millisecond) // 启动即同步
	n0 := p.getRulesNum.Load()
	if n0 < 1 {
		t.Fatalf("启动后应已同步, GetRules 调用次数 = %d, want >= 1", n0)
	}

	pausedCfg := &config.Config{
		Tag:         "auto-dns",
		Interval:    time.Hour,
		SyncEnabled: false,
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}},
		},
	}
	s.ApplyState(newRoundState(t, s, pausedCfg))
	time.Sleep(200 * time.Millisecond)
	s.TriggerSync() // 暂停期间的排队 trigger 在消费前复查开关，不得启动新一轮
	time.Sleep(300 * time.Millisecond)
	n1 := p.getRulesNum.Load()
	if n1 != n0 {
		t.Errorf("暂停后不应再同步, GetRules = %d → %d", n0, n1)
	}
	if s.IsEnabled() {
		t.Error("暂停后 IsEnabled 应为 false")
	}

	s.Resume()
	time.Sleep(600 * time.Millisecond)
	n2 := p.getRulesNum.Load()
	if n2 <= n1 {
		t.Errorf("Resume 后应恢复同步, GetRules = %d → %d", n1, n2)
	}

	s.Stop()
	s.Wait()
}

// TestApplyState_SyncEnabledSync 状态替换开关同步：ApplyState(false) 门控生效 → ApplyState(true) 恢复
func TestApplyState_SyncEnabledSync(t *testing.T) {
	s, p := newGateSyncer(t, true)

	go s.Run()
	time.Sleep(400 * time.Millisecond)
	n0 := p.getRulesNum.Load()
	if n0 < 1 {
		t.Fatalf("启动后应已同步, GetRules = %d", n0)
	}

	// 状态替换关闭同步（携带 DomainRules，保证恢复后可计数）
	pausedCfg := &config.Config{
		Tag:         "auto-dns",
		Interval:    time.Hour,
		SyncEnabled: false,
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}},
		},
	}
	s.ApplyState(newRoundState(t, s, pausedCfg))
	time.Sleep(300 * time.Millisecond)
	if s.IsEnabled() {
		t.Error("ApplyState(false) 后 IsEnabled 应为 false")
	}
	s.TriggerSync()
	time.Sleep(300 * time.Millisecond)
	if n := p.getRulesNum.Load(); n != n0 {
		t.Errorf("暂停后 trigger 不应触发同步, GetRules = %d → %d", n0, n)
	}

	// 状态替换开启同步（携带 DomainRules）
	pausedCfg.SyncEnabled = true
	s.ApplyState(newRoundState(t, s, pausedCfg))
	time.Sleep(600 * time.Millisecond)
	if !s.IsEnabled() {
		t.Error("ApplyState(true) 后 IsEnabled 应为 true")
	}
	if n := p.getRulesNum.Load(); n <= n0 {
		t.Errorf("ApplyState(true) 后应立即执行一次同步, GetRules = %d → %d", n0, n)
	}

	s.Stop()
	s.Wait()
}

// TestIsEnabled 状态镜像正确性
func TestIsEnabled(t *testing.T) {
	s1 := newSyncer(t, &config.Config{SyncEnabled: false}, nil)
	if s1.IsEnabled() {
		t.Error("SyncEnabled=false 初始化应 IsEnabled()==false")
	}
	s1.Resume()
	if !s1.IsEnabled() {
		t.Error("Resume 后应 IsEnabled()==true")
	}
	s1.Pause()
	if s1.IsEnabled() {
		t.Error("Pause 后应 IsEnabled()==false")
	}

	s2 := newSyncer(t, &config.Config{SyncEnabled: true}, nil)
	if !s2.IsEnabled() {
		t.Error("SyncEnabled=true 初始化应 IsEnabled()==true")
	}
}

// ─── Step 1（R5-03）：同步轮次 TAG 快照测试 ───
//
// 当前目标链：syncAll 捕获完整 RuntimeState，沿 runRound → syncTarget 传递；
// planner 的 TAG 所有权、描述生成与全部重试都使用本轮快照，新 TAG 从下一轮生效。
//
// 交错说明：测试在独立 goroutine 中调用 syncAll，同时由配置写入方发布新 RuntimeState。

// fakeTagProvider 测试用 Provider：
// 可控云端规则列表、受控 GetRules 阻塞点、可让指定次数 CreateRules 返回可重试错误，并记录写入
type fakeTagProvider struct {
	*stubProvider

	mu      sync.Mutex
	rules   []config.RuleInfo // 云端当前规则（GetRules 返回的副本）
	created []string          // CreateRules 收到的描述（按调用顺序）
	deleted []string          // DeleteRules 收到的描述（按调用顺序）

	getCalls atomic.Int32
	blockOn  int32         // >0 时在第 N 次 GetRules 上阻塞（受控交错点）
	blocked  chan struct{} // 进入阻塞前通知一次
	release  chan struct{} // 关闭后放行阻塞的 GetRules

	createCalls  atomic.Int32
	deleteCalls  atomic.Int32
	failCreateOn int32 // >0 时第 N 次 CreateRules 返回可重试错误

}

func (m *fakeTagProvider) GetRules() ([]config.RuleInfo, error) {
	n := m.getCalls.Add(1)
	if m.blockOn == n && m.blocked != nil {
		m.blocked <- struct{}{}
		<-m.release
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]config.RuleInfo(nil), m.rules...), nil
}

// GetSnapshot 测试 mock：复用 GetRules 的既有行为，Revision 固定为非空值。
func (m *fakeTagProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	rules, err := m.GetRules()
	return provider.RuleSnapshot{Rules: rules, Revision: "1"}, err
}

func (m *fakeTagProvider) CreateRules(_ provider.RuleSnapshot, rules []config.RuleAction) (provider.CreateResult, error) {
	n := m.createCalls.Add(1)
	m.mu.Lock()
	for _, r := range rules {
		m.created = append(m.created, r.Description)
	}
	m.mu.Unlock()
	if m.failCreateOn == n {
		return provider.CreateResult{}, errors.New("InternalError: 模拟可重试写入失败")
	}
	return provider.CreateResult{Written: len(rules)}, nil
}

func (m *fakeTagProvider) DeleteRules(_ provider.RuleSnapshot, rules []config.RuleInfo) (provider.DeleteResult, error) {
	m.deleteCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range rules {
		m.deleted = append(m.deleted, r.Description)
		// 模拟云端状态推进：删除成功的规则从云端列表移除
		for i, cur := range m.rules {
			if cur.Description == r.Description && cur.Port == r.Port && cur.CidrBlock == r.CidrBlock {
				m.rules = append(m.rules[:i], m.rules[i+1:]...)
				break
			}
		}
	}
	return provider.DeleteResult{Deleted: len(rules), Resolved: len(rules)}, nil
}

func (m *fakeTagProvider) setRules(rules []config.RuleInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = rules
}

func (m *fakeTagProvider) createdDescs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.created...)
}

// testRuntimeConfig 把测试用 Config 映射为完整运行时配置（补齐同步所需的默认值）
func testRuntimeConfig(cfg *config.Config) config.RuntimeConfig {
	rc := config.RuntimeConfig{
		Tag:              cfg.Tag,
		Interval:         cfg.Interval,
		DNS:              cfg.DNS,
		DNSTimeout:       cfg.DNSTimeout,
		DNSFailThreshold: cfg.DNSFailThreshold,
		LogLevel:         cfg.LogLevel,
		SyncEnabled:      cfg.SyncEnabled,
		Theme:            cfg.Theme,
		DomainRules:      config.DeepCopyRules(cfg.DomainRules),
		Targets:          append([]config.TargetConfig(nil), cfg.Targets...),
	}
	if rc.Interval == 0 {
		rc.Interval = time.Hour
	}
	if rc.DNSFailThreshold == 0 {
		rc.DNSFailThreshold = 5
	}
	if rc.DNSTimeout == 0 {
		rc.DNSTimeout = 10 * time.Second
	}
	return rc
}

// newSyncer 用给定配置与 Provider 构造 Syncer（内部发布一份初始运行时状态）
func newSyncer(t *testing.T, cfg *config.Config, providers []provider.Provider) *Syncer {
	t.Helper()
	rc := testRuntimeConfig(cfg)
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造初始运行时状态失败: %v", err)
	}
	if providers != nil {
		st.Providers = providers
	}
	return New(NewRuntimeManager(st))
}

// newRoundConfig 构造替换用配置：同一目标与规则，仅 TAG 不同
func newRoundConfig(tagStr string) *config.Config {
	return &config.Config{
		Tag:         tagStr,
		Interval:    time.Hour,
		SyncEnabled: false,
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Comment: "测试", Targets: []int{0}},
		},
	}
}

// newRoundState 用给定 Config 构造替换用运行时状态（保留当前 Provider 列表）
func newRoundState(t *testing.T, s *Syncer, cfg *config.Config) *RuntimeState {
	t.Helper()
	st, err := BuildRuntimeState(s.runtime.Snapshot(), testRuntimeConfig(cfg), BreakerPreserve)
	if err != nil {
		t.Fatalf("构造候选运行时状态失败: %v", err)
	}
	// 测试用 Config 不含 Targets，Provider 由调用方以 stub 形式注入，这里沿用旧列表
	if prev := s.runtime.Snapshot(); prev != nil {
		st.Providers = prev.Providers
	}
	return st
}

// newTagSnapshotSyncer 构造 TAG 快照测试用 Syncer（SyncEnabled=false 使 Run 只作为状态写入方）
func newTagSnapshotSyncer(t *testing.T, tagStr string, p provider.Provider) *Syncer {
	t.Helper()
	return newSyncer(t, newRoundConfig(tagStr), []provider.Provider{p})
}

// startConfigWriter 启动真实 Run 循环并通过真实 ApplyState 路径替换运行时状态
func startConfigWriter(t *testing.T, s *Syncer) {
	t.Helper()
	go s.Run()
	t.Cleanup(func() {
		stopRunBounded(t, s, 10*time.Second)
	})
}

// applyStateAndWait 触发真实 ApplyState 并等待 Run 循环消费通知
func applyStateAndWait(t *testing.T, s *Syncer, cfg *config.Config) {
	t.Helper()
	s.ApplyState(newRoundState(t, s, cfg))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := s.runtime.Snapshot()
		if st != nil && st.Config.Tag == cfg.Tag {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待运行时状态替换超时")
}

// waitSignal 等待受控交错信号（超时失败，不依赖固定 sleep）
func waitSignal(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(msg)
	}
}

// TestSyncRound_TagSnapshotDuringReload 本轮首次 Describe 期间通过真实 Reload 替换 TAG：
// 本轮的 OwnedRules 筛选与描述生成必须仍使用旧 TAG
func TestSyncRound_TagSnapshotDuringReload(t *testing.T) {
	p := &fakeTagProvider{
		stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0},
		rules: []config.RuleInfo{
			{Protocol: "TCP", Port: "443", CidrBlock: "10.0.0.1/32", Action: "ACCEPT", Description: "[auto-dns] 测试"},
		},
		blockOn: 1,
		blocked: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	s := newTagSnapshotSyncer(t, "auto-dns", p)
	startConfigWriter(t, s)

	roundDone := make(chan struct{})
	go func() {
		defer close(roundDone)
		s.syncAll()
	}()

	// 等本轮进入首次 Describe 并阻塞（此刻本轮快照已取得）
	waitSignal(t, p.blocked, "本轮未进入首次 Describe")

	// 轮次进行中替换 TAG（新 TAG 只能从下一轮生效）
	applyStateAndWait(t, s, newRoundConfig("new-tag"))

	close(p.release)
	waitSignal(t, roundDone, "同步轮次未结束")

	// 夹具缺少可删除定位，使用清理候选数观察 TAG 归属。
	// 若本轮误用新 TAG，这条 [auto-dns] 规则会被判为 External，候选数将变为 0。
	if sum := s.Status().LastRound; sum == nil || sum.CleanupCandidates != 1 {
		t.Errorf("本轮清理候选 = %+v, want 1（OwnedRules 必须使用本轮旧 TAG）", sum)
	}
	if got := p.createdDescs(); len(got) != 1 || got[0] != "[auto-dns] 测试" {
		t.Errorf("本轮新增描述 = %v, want [[auto-dns] 测试]（描述生成必须使用本轮旧 TAG）", got)
	}
}

// TestSyncRound_TagSnapshotAcrossRetry 首次写入返回可重试错误，在受控重试边界（第二次 Describe 阻塞）替换 TAG：
// 重试轮的 OwnedRules 与描述必须仍使用本轮旧 TAG
func TestSyncRound_TagSnapshotAcrossRetry(t *testing.T) {
	p := &fakeTagProvider{
		stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0},
		blockOn:      2, // 第 1 次 Describe 正常返回，重试轮 Describe 阻塞
		blocked:      make(chan struct{}, 1),
		release:      make(chan struct{}),
		failCreateOn: 1, // 第 1 次写入返回可重试错误
	}
	s := newTagSnapshotSyncer(t, "auto-dns", p)
	startConfigWriter(t, s)

	roundDone := make(chan struct{})
	go func() {
		defer close(roundDone)
		s.syncAll()
	}()

	// 等重试轮进入 Describe（已完成退避）
	waitSignal(t, p.blocked, "重试轮未进入 Describe")
	applyStateAndWait(t, s, newRoundConfig("new-tag"))

	// 模拟云端出现旧 TAG 陈旧规则：重试必须按 TAG 归属识别为清理候选
	p.setRules([]config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "10.0.0.1/32", Action: "ACCEPT", Description: "[auto-dns] 测试"},
	})

	close(p.release)
	waitSignal(t, roundDone, "同步轮次未结束")

	created := p.createdDescs()
	if len(created) != 2 {
		t.Fatalf("CreateRules 调用次数 = %d, want 2（首次失败 + 重试成功）", len(created))
	}
	for i, desc := range created {
		if desc != "[auto-dns] 测试" {
			t.Errorf("第 %d 次新增描述 = %q, want %q（重试必须使用本轮旧 TAG）", i+1, desc, "[auto-dns] 测试")
		}
	}
	// 夹具缺少可删除定位，以清理候选数证明重试轮仍使用本轮旧 TAG。
	if sum := s.Status().LastRound; sum == nil || sum.CleanupCandidates != 1 {
		t.Errorf("重试轮清理候选 = %+v, want 1（重试必须使用本轮旧 TAG）", sum)
	}
}

// TestSyncRound_NextRoundUsesNewTag 新 TAG 只从下一轮同步开始生效
func TestSyncRound_NextRoundUsesNewTag(t *testing.T) {
	p := &fakeTagProvider{stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0}}
	s := newTagSnapshotSyncer(t, "auto-dns", p)
	startConfigWriter(t, s)

	s.syncAll()
	if got := p.createdDescs(); len(got) != 1 || got[0] != "[auto-dns] 测试" {
		t.Fatalf("第一轮新增描述 = %v, want [[auto-dns] 测试]", got)
	}

	applyStateAndWait(t, s, newRoundConfig("new-tag"))
	s.syncAll()

	created := p.createdDescs()
	if len(created) != 2 || created[1] != "[new-tag] 测试" {
		t.Errorf("第二轮新增描述 = %v, want 第二轮使用 [new-tag] 测试", created)
	}
}

// TestSyncRound_ConcurrentReloadStress 轮次与真实 Reload 并发（无严格交错点）：
// 依赖 race detector 发现未被锁保护的配置读取；本用例不断言时序，修复后不依赖交错即可通过
func TestSyncRound_ConcurrentReloadStress(t *testing.T) {
	p := &fakeTagProvider{stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0}}
	s := newTagSnapshotSyncer(t, "auto-dns", p)
	startConfigWriter(t, s)

	const rounds = 10
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.syncAll()
		}()
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				s.ApplyState(newRoundState(t, s, newRoundConfig("tag-a")))
				return
			}
			s.ApplyState(newRoundState(t, s, newRoundConfig("tag-b")))
		}(i)
	}
	wg.Wait()
}

// ─── Step 7：部分写入计数、Provider 快照隔离、描述/TAG 边界 ───

// TestSyncRoundUsesSingleProviderSnapshot Step 7「Provider/Resolver/TAG/Config 完整单轮快照」：
// 本轮 Describe 阻塞期间把 Provider 列表整体替换为另一个 Provider，
// 本轮必须继续使用开始时的 Provider（旧快照），下一轮才使用新 Provider。
func TestSyncRoundUsesSingleProviderSnapshot(t *testing.T) {
	pA := &fakeTagProvider{
		stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0},
		blockOn:      1,
		blocked:      make(chan struct{}, 1),
		release:      make(chan struct{}),
	}
	pB := &fakeTagProvider{stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0}}
	s := newTagSnapshotSyncer(t, "auto-dns", pA)

	roundDone := make(chan struct{})
	go func() {
		defer close(roundDone)
		s.syncAll()
	}()

	waitSignal(t, pA.blocked, "本轮未进入 Provider A 的首次 Describe")

	// 轮次进行中替换 Provider 列表：新 Provider 只能从下一轮生效
	next, err := BuildRuntimeState(s.runtime.Snapshot(), testRuntimeConfig(newRoundConfig("auto-dns")), BreakerPreserve)
	if err != nil {
		t.Fatalf("构造替换状态失败: %v", err)
	}
	next.Providers = []provider.Provider{pB}
	s.ApplyState(next)

	close(pA.release)
	waitSignal(t, roundDone, "同步轮次未结束")

	if got := pA.getCalls.Load(); got != 2 {
		t.Errorf("Provider A 快照调用 = %d, want 2（一轮 = S0 + S1）", got)
	}
	if got := pA.createCalls.Load(); got != 1 {
		t.Errorf("本轮必须继续使用快照中的 Provider A 完成写入，CreateRules 调用 = %d, want 1", got)
	}
	if got := pB.getCalls.Load(); got != 0 {
		t.Errorf("本轮不得使用替换后的 Provider B：GetRules 调用 = %d, want 0", got)
	}
	if got := pB.createCalls.Load(); got != 0 {
		t.Errorf("本轮不得使用替换后的 Provider B 写入：CreateRules 调用 = %d, want 0", got)
	}

	// 下一轮必须使用新 Provider B
	s.syncAll()
	if got := pB.getCalls.Load(); got == 0 {
		t.Errorf("下一轮必须使用新 Provider B")
	}
	if got := pA.getCalls.Load(); got != 2 {
		t.Errorf("Provider A 不得再被新轮次使用：快照调用 = %d, want 2", got)
	}
}
