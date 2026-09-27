package api

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// countingImportProvider 导入调度测试用 Provider：只计数调用，不访问任何网络。
//
// targetIndex 用原子值保存：导入会重建目标并分配新的数据库 ID，包装器在发布候选状态前
// 同步更新它，使快照内的规则（targets = 新 DB ID）能匹配到本 stub。
type countingImportProvider struct {
	calls       atomic.Int32
	targetIndex atomic.Int32
}

func (p *countingImportProvider) Name() string                { return "counting-import" }
func (p *countingImportProvider) CloudType() config.CloudType { return config.CloudTCCVM }
func (p *countingImportProvider) TargetIndex() int            { return int(p.targetIndex.Load()) }
func (p *countingImportProvider) CreateRules(rules []config.RuleAction) (provider.CreateResult, error) {
	return provider.CreateResult{Written: len(rules)}, nil
}
func (p *countingImportProvider) DeleteRules([]config.RuleInfo) error { return nil }
func (p *countingImportProvider) ConvertPorts(port string) []string   { return portconv.Parse(port) }

func (p *countingImportProvider) GetRules() ([]config.RuleInfo, error) {
	p.calls.Add(1)
	return nil, nil
}

// stubProviderSyncer 包装真实 Syncer：发布前把候选 Provider 列表替换为计数 stub，
// 使导入路径的调度断言不访问真实云 API（候选构造本身不发网络请求）。
type stubProviderSyncer struct {
	*syncer.Syncer
	stub *countingImportProvider
}

func (s *stubProviderSyncer) ApplyState(state *syncer.RuntimeState) {
	if len(state.Config.Targets) > 0 {
		s.stub.targetIndex.Store(int32(state.Config.Targets[0].ID))
	}
	state.Providers = []provider.Provider{s.stub}
	s.Syncer.ApplyState(state)
}

// stopSyncerBounded 在有界时间内停止并等待同步引擎退出（测试清理必须有界）。
func stopSyncerBounded(t *testing.T, s *syncer.Syncer, timeout time.Duration) {
	t.Helper()
	s.Stop()
	done := make(chan struct{})
	go func() {
		s.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("同步引擎在 %s 内未退出（测试清理必须有界）", timeout)
	}
}

// waitProviderCalls 等待 Provider 调用次数达到 want。
func waitProviderCalls(t *testing.T, p *countingImportProvider, want int32, msg string) {
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

// bundleSettingsSync 构造 version 2 的 settings JSON 片段（可指定 sync_enabled）。
func bundleSettingsSync(syncEnabled bool) string {
	value := "false"
	if syncEnabled {
		value = "true"
	}
	return `{"credentials":{"tencent":{"secret_id":"","secret_key":""},` +
		`"aliyun":{"access_key_id":"","access_key_secret":""}},` +
		`"tag":"auto-dns","interval":"5m","dns":"223.5.5.5","dns_timeout":"10s",` +
		`"dns_fail_threshold":5,"log_level":"info","sync_enabled":` + value + `,"theme":"light"}`
}

// importBundleWithSyncEnabled 构造一份带单个目标与单条规则的合法配置包。
func importBundleWithSyncEnabled(syncEnabled bool) string {
	return bundleWithSettings(
		`[{"export_id":1,"cloud_type":"tc_cvm","region":"gz","resource_id":"sg-1"}]`,
		`[{"host":"localhost","protocol":"TCP","ports":"443","action":"ACCEPT",`+
			`"target_export_ids":[1],"comment":"","enable_ipv6":false}]`,
		bundleSettingsSync(syncEnabled),
	)
}

// TestConfigImportSyncEnabledRuntimeConsistency Step 7「导入 sync_enabled 的运行时一致性」：
//
// 用真实 API 导入 + 真实 Syncer 验证端到端接线（协调器 commit 后只发布一次）：
//   - 导入 sync_enabled=false：运行时真值立即为暂停，且不再启动新同步轮次；
//   - 导入 sync_enabled=true：运行时真值立即为开启，并立即启动一轮（false → true 语义）。
//
// 用 SetStateAppliedHook 作为确定性屏障，确保暂停已真正被 Run 消费，避免两次导入的
// 控制通知被合并而把 false→true 观测成 true→true。
func TestConfigImportSyncEnabledRuntimeConsistency(t *testing.T) {
	e := newTestEnv(t)

	stub := &countingImportProvider{}
	initial, err := syncer.BuildRuntimeState(nil, config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "223.5.5.5", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		DomainRules: []config.DomainRule{
			{Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}},
		},
	}, syncer.BreakerReset)
	if err != nil {
		t.Fatalf("构造初始运行时状态失败: %v", err)
	}
	initial.Providers = []provider.Provider{stub}

	s := &stubProviderSyncer{Syncer: syncer.New(syncer.NewRuntimeManager(initial)), stub: stub}
	e.deps.Syncer = s
	e.deps.Runtime = s.Runtime()
	e.deps.Coord = NewConfigCoordinator(e.store, e.deps.buildCandidate, e.deps.applyCandidate)

	go s.Run()
	t.Cleanup(func() { stopSyncerBounded(t, s.Syncer, 10*time.Second) })

	waitProviderCalls(t, stub, 1, "启用态启动未执行首轮")
	if !s.IsEnabled() {
		t.Fatal("前置条件：初始运行时状态必须为启用")
	}

	// 屏障：每次 Run 完成调度决策后通知一次
	applied := make(chan struct{}, 8)
	s.SetStateAppliedHook(func(*syncer.RuntimeState) {
		select {
		case applied <- struct{}{}:
		default:
		}
	})
	waitApplied := func(msg string) {
		t.Helper()
		select {
		case <-applied:
		case <-time.After(5 * time.Second):
			t.Fatal(msg)
		}
	}

	// 导入暂停态
	if w := e.do(t, http.MethodPost, "/api/config/import", importBundleWithSyncEnabled(false)); w.Code != http.StatusOK {
		t.Fatalf("导入（暂停）状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	waitApplied("导入的暂停状态未被 Run 消费")
	if s.IsEnabled() {
		t.Fatal("导入 sync_enabled=false 后运行时真值必须为暂停")
	}
	if settings, _ := e.store.GetSettings(); settings["sync_enabled"] != "false" {
		t.Fatalf("导入后 SQLite sync_enabled = %q, want false", settings["sync_enabled"])
	}
	before := stub.calls.Load()
	time.Sleep(300 * time.Millisecond) // 覆盖多个调度机会；间隔为 5m，只有错误启动的轮次才会命中
	if got := stub.calls.Load(); got != before {
		t.Fatalf("导入暂停后不得启动新轮次: GetRules = %d → %d", before, got)
	}

	// 导入开启态：必须立即启动一轮
	if w := e.do(t, http.MethodPost, "/api/config/import", importBundleWithSyncEnabled(true)); w.Code != http.StatusOK {
		t.Fatalf("导入（开启）状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	waitApplied("导入的开启状态未被 Run 消费")
	if !s.IsEnabled() {
		t.Fatal("导入 sync_enabled=true 后运行时真值必须为开启")
	}
	if settings, _ := e.store.GetSettings(); settings["sync_enabled"] != "true" {
		t.Fatalf("导入后 SQLite sync_enabled = %q, want true", settings["sync_enabled"])
	}
	waitProviderCalls(t, stub, before+1, "导入开启后必须立即启动一轮")
}

// TestConfigImportResetsDNSBreakerOrdinaryChangePreserves Step 7「DNS 阈值更新和 breaker 策略」
// （Build6 §12.3 第 7 条 / Issue6 A8）：
//
//   - 普通配置变更（settings）沿用 BreakerPreserve：既有 DNS 熔断失败计数必须保留；
//   - 完整配置导入使用 BreakerReset：新建 breaker，失败计数清空。
//
// 观测方式只用导出的 IsOpen：阈值设为 2 并预置 2 次失败（已熔断）。
// 若导入沿用保留策略，导入后的 breaker 仍会在阈值 2 下熔断，本用例即失败。
func TestConfigImportResetsDNSBreakerOrdinaryChangePreserves(t *testing.T) {
	e := newTestEnv(t)

	// 夹具：数据库中的阈值与初始发布状态一致（=2）
	if err := e.store.SetSetting("dns_fail_threshold", "2"); err != nil {
		t.Fatalf("预置 dns_fail_threshold 失败: %v", err)
	}
	initial, err := syncer.BuildRuntimeState(nil, config.RuntimeConfig{
		Tag: "auto-dns", Interval: 5 * time.Minute, DNS: "223.5.5.5", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 2, LogLevel: "info", SyncEnabled: true, Theme: "light",
	}, syncer.BreakerReset)
	if err != nil {
		t.Fatalf("构造初始状态失败: %v", err)
	}
	initial.Breaker.RecordFailure("probe.test")
	initial.Breaker.RecordFailure("probe.test")
	if !initial.Breaker.IsOpen("probe.test") {
		t.Fatal("前置条件：阈值 2 且 2 次失败必须已熔断")
	}
	e.runtime.Apply(initial)

	// 普通变更：必须保留计数（副本仍处于熔断状态）
	if w := e.do(t, http.MethodPut, "/api/settings", `{"theme":"dark"}`); w.Code != http.StatusOK {
		t.Fatalf("settings 变更状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	preserved := e.snapshot()
	if preserved == nil {
		t.Fatal("普通变更后运行时状态为空")
	}
	if preserved.Breaker == initial.Breaker {
		t.Error("普通变更必须复制 breaker 而不是复用同一实例")
	}
	if !preserved.Breaker.IsOpen("probe.test") {
		t.Error("普通变更必须保留既有 DNS 熔断失败计数（BreakerPreserve）")
	}

	// 完整导入：必须新建 breaker 并清空计数
	settings := `{"credentials":{"tencent":{"secret_id":"","secret_key":""},` +
		`"aliyun":{"access_key_id":"","access_key_secret":""}},` +
		`"tag":"auto-dns","interval":"5m","dns":"223.5.5.5","dns_timeout":"10s",` +
		`"dns_fail_threshold":2,"log_level":"info","sync_enabled":true,"theme":"light"}`
	body := bundleWithSettings(`[]`, `[]`, settings)
	if w := e.do(t, http.MethodPost, "/api/config/import", body); w.Code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	reset := e.snapshot()
	if reset == nil {
		t.Fatal("导入后运行时状态为空")
	}
	if reset.Breaker.IsOpen("probe.test") {
		t.Error("完整导入必须清空 DNS 熔断失败计数（BreakerReset）")
	}
}
