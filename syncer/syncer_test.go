package syncer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
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
func (m *stubProvider) CreateRules(rules []config.RuleAction) error { return nil }
func (m *stubProvider) DeleteRules(rules []config.RuleInfo) error   { return nil }
func (m *stubProvider) ConvertPorts(port string) []string {
	return portconv.Parse(port)
}

// localResolver 测试用解析器：解析 localhost（遵循现有 TestResolve_Localhost 惯例，走 hosts/DNS）
func localResolver(t *testing.T) *dns.Resolver {
	t.Helper()
	return dns.NewResolver("8.8.8.8:53", 10*time.Second)
}

// TestDryRun_EmptyConfig 无 providers 无规则 → Warnings 两条、Results 为空数组
func TestDryRun_EmptyConfig(t *testing.T) {
	cfg := &config.Config{Tag: "auto-dns"}
	s := New(cfg, nil, localResolver(t))

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

// TestDryRun_Detail 单域名规则 → ToAdd/ToDelete 明细数组与字段
// 使用 CVM 云类型（限速 200ms），避免 Lighthouse/SWAS 的 5s 间隔拖慢测试
func TestDryRun_Detail(t *testing.T) {
	p := &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0}
	cfg := &config.Config{
		Tag: "auto-dns",
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Comment: "测试", Targets: []int{0}},
		},
	}
	s := New(cfg, []provider.Provider{p}, localResolver(t))

	resp, err := s.DryRun()
	if err != nil {
		t.Fatalf("DryRun 失败: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("Results 数量 = %d, want 1", len(resp.Results))
	}
	r := resp.Results[0]
	if r.Domain != "localhost" {
		t.Errorf("Domain = %s, want localhost", r.Domain)
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
	if len(r.ToDelete) != 0 {
		t.Errorf("ToDelete 数量 = %d, want 0", len(r.ToDelete))
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
	s := New(cfg, []provider.Provider{p}, localResolver(t))

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
	return New(cfg, []provider.Provider{p}, localResolver(t)), p
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

	s.Pause()
	time.Sleep(200 * time.Millisecond)
	s.TriggerSync() // 暂停期间 trigger 信号不触发同步（等待主循环处理 pause 后进入 waitForResume）
	time.Sleep(300 * time.Millisecond)
	n1 := p.getRulesNum.Load()
	if n1 != n0 {
		t.Errorf("暂停后不应再同步, GetRules = %d → %d", n0, n1)
	}
	if s.IsEnabled() {
		t.Error("Pause 后 IsEnabled 应为 false")
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

// TestReload_SyncEnabledSync 热重载开关同步：Reload(false) 门控生效 → Reload(true) 恢复
func TestReload_SyncEnabledSync(t *testing.T) {
	s, p := newGateSyncer(t, true)

	go s.Run()
	time.Sleep(400 * time.Millisecond)
	n0 := p.getRulesNum.Load()
	if n0 < 1 {
		t.Fatalf("启动后应已同步, GetRules = %d", n0)
	}

	// 热重载关闭同步（携带 DomainRules，保证恢复后可计数）
	pausedCfg := &config.Config{
		Tag:         "auto-dns",
		Interval:    time.Hour,
		SyncEnabled: false,
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}},
		},
	}
	s.Reload(pausedCfg)
	time.Sleep(300 * time.Millisecond)
	if s.IsEnabled() {
		t.Error("Reload(false) 后 IsEnabled 应为 false")
	}
	s.TriggerSync()
	time.Sleep(300 * time.Millisecond)
	if n := p.getRulesNum.Load(); n != n0 {
		t.Errorf("暂停后 trigger 不应触发同步, GetRules = %d → %d", n0, n)
	}

	// 热重载开启同步（携带 DomainRules）
	pausedCfg.SyncEnabled = true
	s.Reload(pausedCfg)
	time.Sleep(600 * time.Millisecond)
	if !s.IsEnabled() {
		t.Error("Reload(true) 后 IsEnabled 应为 true")
	}
	if n := p.getRulesNum.Load(); n <= n0 {
		t.Errorf("Reload(true) 后应立即执行一次同步, GetRules = %d → %d", n0, n)
	}

	s.Stop()
	s.Wait()
}

// TestIsEnabled 状态镜像正确性
func TestIsEnabled(t *testing.T) {
	s1 := New(&config.Config{SyncEnabled: false}, nil, localResolver(t))
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

	s2 := New(&config.Config{SyncEnabled: true}, nil, localResolver(t))
	if !s2.IsEnabled() {
		t.Error("SyncEnabled=true 初始化应 IsEnabled()==true")
	}
}

// ─── Build4 Step 1：计数链路测试 ───

// countingProvider 测试用 Provider：记录 CreateRules/DeleteRules 成功收到的规则数量
// （内嵌 stubProvider 继承 Name/CloudType/TargetIndex/GetRules/ConvertPorts）
type countingProvider struct {
	*stubProvider
	created atomic.Int32
	deleted atomic.Int32
}

func (m *countingProvider) CreateRules(rules []config.RuleAction) error {
	m.created.Add(int32(len(rules)))
	return nil
}

func (m *countingProvider) DeleteRules(rules []config.RuleInfo) error {
	m.deleted.Add(int32(len(rules)))
	return nil
}

// TestRetrySync_Counts 空云端规则 + 单域名规则 → 全部新增，added=1 deleted=0
func TestRetrySync_Counts(t *testing.T) {
	p := &countingProvider{stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0}}
	cfg := &config.Config{Tag: "auto-dns"}
	s := New(cfg, []provider.Provider{p}, localResolver(t))

	resolved, err := s.resolver.Resolve(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	resolved = filterIPv4(resolved) // 与 syncDomain 实际执行路径一致（LookupIPAddr 对 localhost 同时返回 127.0.0.1 与 ::1，过滤后恒为 1 条 IPv4）
	added, deleted, err := s.retrySync(p, config.DomainRule{
		Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0},
	}, resolved, cfg.Tag)
	if err != nil {
		t.Fatalf("retrySync 失败: %v", err)
	}
	if added != 1 || deleted != 0 {
		t.Errorf("计数 = added:%d deleted:%d, want 1/0", added, deleted)
	}
	if p.created.Load() != 1 || p.deleted.Load() != 0 {
		t.Errorf("Provider 调用 = created:%d deleted:%d, want 1/0", p.created.Load(), p.deleted.Load())
	}
}

// ─── Step 1（R5-03）：同步轮次 TAG 快照测试 ───
//
// 固定口径（Build6 Step 1）：syncAll 捕获本轮 TAG 并沿
// syncAll → syncDomain → syncDomainInternal → retrySync 显式传参；
// 一轮同步的 OwnedRules 筛选、描述生成与全部重试只使用本轮快照 TAG，新 TAG 从下一轮开始生效。
//
// 交错说明：当前生产 Run 循环同步执行 syncAll（Reload 为阻塞式 channel 发送），
// 因此"轮次进行中替换配置"由测试构造：Run 循环作为真实写入方，轮次在独立 goroutine 中显式调用。

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

	// 幂等错误注入（规则已存在 / 已不存在）：返回错误但不应计数、不应重试
	idempotentCreateErr error
	idempotentDeleteErr error
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

func (m *fakeTagProvider) CreateRules(rules []config.RuleAction) error {
	n := m.createCalls.Add(1)
	if m.idempotentCreateErr != nil {
		return m.idempotentCreateErr
	}
	m.mu.Lock()
	for _, r := range rules {
		m.created = append(m.created, r.Description)
	}
	m.mu.Unlock()
	if m.failCreateOn == n {
		return errors.New("InternalError: 模拟可重试写入失败")
	}
	return nil
}

func (m *fakeTagProvider) DeleteRules(rules []config.RuleInfo) error {
	m.deleteCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idempotentDeleteErr != nil {
		return m.idempotentDeleteErr
	}
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
	return nil
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

func (m *fakeTagProvider) deletedDescs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.deleted...)
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

// newTagSnapshotSyncer 构造 TAG 快照测试用 Syncer（SyncEnabled=false 使 Run 只作为配置写入方）
func newTagSnapshotSyncer(t *testing.T, tagStr string, p provider.Provider) *Syncer {
	t.Helper()
	return New(newRoundConfig(tagStr), []provider.Provider{p}, localResolver(t))
}

// startConfigWriter 启动真实 Run 循环并通过真实 Reload 路径替换配置
func startConfigWriter(t *testing.T, s *Syncer) {
	t.Helper()
	go s.Run()
	t.Cleanup(func() {
		s.Stop()
		s.Wait()
	})
}

// reloadAndWait 触发真实 Reload 并等待 Run 循环完成锁内替换
func reloadAndWait(t *testing.T, s *Syncer, cfg *config.Config) {
	t.Helper()
	s.Reload(cfg)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		cur := s.cfg
		s.mu.RUnlock()
		if cur == cfg {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待 Run 循环应用 Reload 配置超时")
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
	reloadAndWait(t, s, newRoundConfig("new-tag"))

	close(p.release)
	waitSignal(t, roundDone, "同步轮次未结束")

	if got := p.deletedDescs(); len(got) != 1 || got[0] != "[auto-dns] 测试" {
		t.Errorf("本轮删除描述 = %v, want [[auto-dns] 测试]（OwnedRules 必须使用本轮旧 TAG）", got)
	}
	if got := p.createdDescs(); len(got) != 1 || got[0] != "[auto-dns] 测试" {
		t.Errorf("本轮新增描述 = %v, want [[auto-dns] 测试]（描述生成必须使用本轮旧 TAG）", got)
	}
}

// TestRetrySync_TagSnapshotAcrossRetry 首次写入返回可重试错误，在受控重试边界（第二次 Describe 阻塞）替换 TAG：
// 重试轮的 OwnedRules 与描述必须仍使用本轮旧 TAG
func TestRetrySync_TagSnapshotAcrossRetry(t *testing.T) {
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
	reloadAndWait(t, s, newRoundConfig("new-tag"))

	// 模拟云端出现旧 TAG 规则：重试轮若使用旧 TAG 必须识别并按描述精确删除
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
	if got := p.deletedDescs(); len(got) != 1 || got[0] != "[auto-dns] 测试" {
		t.Errorf("重试轮删除描述 = %v, want [[auto-dns] 测试]", got)
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

	reloadAndWait(t, s, newRoundConfig("new-tag"))
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
				s.Reload(newRoundConfig("tag-a"))
				return
			}
			s.Reload(newRoundConfig("tag-b"))
		}(i)
	}
	wg.Wait()
}

// ─── Step 1（R5-03）：既有语义不退化测试（显式传入本轮 TAG） ───

// resolveLocalhostIPv4 解析 localhost 并过滤 IPv6（与 syncDomain 实际执行路径一致）
func resolveLocalhostIPv4(t *testing.T, s *Syncer) []dns.ResolvedIP {
	t.Helper()
	resolved, err := s.resolver.Resolve(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return filterIPv4(resolved)
}

// TestRetrySync_IdempotentErrorsNotCounted 幂等错误（规则已存在 / 已不存在）视为成功：不计数、不重试
func TestRetrySync_IdempotentErrorsNotCounted(t *testing.T) {
	p := &fakeTagProvider{
		stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0},
		rules: []config.RuleInfo{
			{Protocol: "TCP", Port: "443", CidrBlock: "10.0.0.1/32", Action: "ACCEPT", Description: "[auto-dns] 测试"},
		},
		idempotentCreateErr: errors.New("FirewallRulesExist: 规则已存在"),
		idempotentDeleteErr: errors.New("FirewallRulesNotFound: 规则已不存在"),
	}
	s := newTagSnapshotSyncer(t, "auto-dns", p)

	added, deleted, err := s.retrySync(p, config.DomainRule{
		Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Comment: "测试", Targets: []int{0},
	}, resolveLocalhostIPv4(t, s), "auto-dns")
	if err != nil {
		t.Fatalf("幂等错误不应返回失败: %v", err)
	}
	if added != 0 || deleted != 0 {
		t.Errorf("幂等跳过不应计数 = added:%d deleted:%d, want 0/0", added, deleted)
	}
	if p.createCalls.Load() != 1 || p.deleteCalls.Load() != 1 {
		t.Errorf("幂等错误不应触发重试 = create:%d delete:%d, want 1/1", p.createCalls.Load(), p.deleteCalls.Load())
	}
}

// TestRetrySync_EmptyCommentDesc 空 comment 的描述沿用既有语义：仅 "[TAG]"，无尾随空格
func TestRetrySync_EmptyCommentDesc(t *testing.T) {
	p := &fakeTagProvider{stubProvider: &stubProvider{cloudType: config.CloudTCCVM, targetIndex: 0}}
	s := newTagSnapshotSyncer(t, "auto-dns", p)

	added, deleted, err := s.retrySync(p, config.DomainRule{
		Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0},
	}, resolveLocalhostIPv4(t, s), "auto-dns")
	if err != nil {
		t.Fatalf("retrySync 失败: %v", err)
	}
	if added != 1 || deleted != 0 {
		t.Errorf("计数 = added:%d deleted:%d, want 1/0", added, deleted)
	}
	if got := p.createdDescs(); len(got) != 1 || got[0] != "[auto-dns]" {
		t.Errorf("空 comment 描述 = %v, want [[auto-dns]]", got)
	}
}

// TestTruncateDesc_TagPrefixPreserved 描述截断保持既有语义：按云厂商上限截断且 [TAG] 前缀完整
func TestTruncateDesc_TagPrefixPreserved(t *testing.T) {
	long := "[auto-dns] " + strings.Repeat("很", 60)

	swas := truncateDesc(long, config.CloudAliSWAS)
	if n := len([]rune(swas)); n != 50 {
		t.Errorf("SWAS 截断长度 = %d, want 50", n)
	}
	if !strings.HasPrefix(swas, "[auto-dns]") {
		t.Errorf("SWAS 截断后 TAG 前缀不完整: %q", swas)
	}

	lighthouse := truncateDesc(long, config.CloudTCLighthouse)
	if n := len([]rune(lighthouse)); n != 64 {
		t.Errorf("Lighthouse 截断长度 = %d, want 64", n)
	}
	if !strings.HasPrefix(lighthouse, "[auto-dns]") {
		t.Errorf("Lighthouse 截断后 TAG 前缀不完整: %q", lighthouse)
	}

	if got := truncateDesc(long, config.CloudTCCVM); got != long {
		t.Errorf("CVM 不应截断: got %q", got)
	}
	short := "[auto-dns] 短描述"
	if got := truncateDesc(short, config.CloudAliSWAS); got != short {
		t.Errorf("未超长描述不应截断: got %q", got)
	}
}
