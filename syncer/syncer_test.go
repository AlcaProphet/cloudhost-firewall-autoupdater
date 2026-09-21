package syncer

import (
	"context"
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

func (m *stubProvider) Name() string                     { return "stub" }
func (m *stubProvider) CloudType() config.CloudType      { return m.cloudType }
func (m *stubProvider) TargetIndex() int                 { return m.targetIndex }
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
	}, resolved)
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
