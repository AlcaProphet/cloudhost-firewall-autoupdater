package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// syncStatusProviderMode 控制状态 API 端到端测试中的 Provider 行为。
type syncStatusProviderMode int

const (
	syncStatusSuccess syncStatusProviderMode = iota
	syncStatusFailed
	syncStatusPartial
)

// syncStatusProvider 是可切换行为的并发安全 Provider 测试替身。
// Syncer 的轮次在 goroutine 中读取行为，因此模式与调用计数必须由同一把锁保护。
type syncStatusProvider struct {
	mu    sync.Mutex
	mode  syncStatusProviderMode
	calls int
}

func (p *syncStatusProvider) Name() string                { return "sync-status" }
func (p *syncStatusProvider) CloudType() config.CloudType { return config.CloudTCCVM }
func (p *syncStatusProvider) TargetIndex() int            { return 0 }

func (p *syncStatusProvider) GetRules() ([]config.RuleInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++

	switch p.mode {
	case syncStatusFailed:
		return nil, errors.New("permission denied")
	case syncStatusPartial:
		return nil, nil
	default:
		return []config.RuleInfo{{
			Protocol: "TCP", Port: "443", Action: "ACCEPT",
			CidrBlock: "127.0.0.1/32", Description: "[auto-dns]",
		}}, nil
	}
}

func (p *syncStatusProvider) CreateRules(rules []config.RuleAction) (provider.CreateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.mode == syncStatusPartial {
		return provider.CreateResult{Skipped: len(rules)}, nil
	}
	return provider.CreateResult{Written: len(rules)}, nil
}

func (p *syncStatusProvider) DeleteRules([]config.RuleInfo) error {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return nil
}

func (p *syncStatusProvider) ConvertPorts(port string) []string { return []string{port} }

func (p *syncStatusProvider) setMode(mode syncStatusProviderMode) {
	p.mu.Lock()
	p.mode = mode
	p.mu.Unlock()
}

func (p *syncStatusProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestSyncStatusEndpointRoundValues 通过真实 Syncer、RuntimeManager、Server 与 HTTP
// 端点验证 A3/A18 的值级合同，而不只验证 JSON 字段名存在。
func TestSyncStatusEndpointRoundValues(t *testing.T) {
	p := &syncStatusProvider{}
	state := newSyncStatusState(t, false, []config.DomainRule{syncStatusRule()}, p)
	engine := syncer.New(syncer.NewRuntimeManager(state))
	events, unsubscribe := engine.EventBus().SubscribeChan()
	t.Cleanup(unsubscribe)

	server := newTestServer(t, freeTCPPort(t))
	server.SetSyncer(engine, engine.EventBus())
	_, port := startTestServer(t, server)
	statusURL := fmt.Sprintf("http://127.0.0.1:%d/api/sync/status", port)

	initial := getSyncStatus(t, statusURL)
	if initial.Enabled {
		t.Fatal("初始 enabled = true, want false")
	}
	if initial.LastSync != nil || initial.LastSuccess != nil || initial.LastRound != nil {
		t.Fatalf("初始历史字段 = last_sync:%v last_success:%v last_round:%v, want 全部 null",
			initial.LastSync, initial.LastSuccess, initial.LastRound)
	}

	go engine.Run()
	t.Cleanup(func() { stopSyncStatusEngine(t, engine) })

	// false → true 会立即执行一轮；EventSyncComplete 是状态写入完成后的确定性屏障。
	engine.Resume()
	waitForSyncComplete(t, events)
	success := getSyncStatus(t, statusURL)
	assertRoundStatus(t, success, syncer.RoundSummary{
		Total: 1, OK: 1, Outcome: syncer.RoundSuccess,
	})
	if success.LastSuccess == nil || !success.LastSuccess.Equal(*success.LastSync) {
		t.Fatalf("success 后 last_success=%v last_sync=%v, want 相等且非 null", success.LastSuccess, success.LastSync)
	}
	lastSuccess := *success.LastSuccess

	p.setMode(syncStatusFailed)
	engine.TriggerSync()
	waitForSyncComplete(t, events)
	failed := getSyncStatus(t, statusURL)
	assertRoundStatus(t, failed, syncer.RoundSummary{
		Total: 1, Failed: 1, Outcome: syncer.RoundFailed,
	})
	assertLastSuccessPreserved(t, failed, lastSuccess)
	if !failed.LastSync.After(*success.LastSync) {
		t.Errorf("failed 后 last_sync=%v, want 晚于 success 的 %v", failed.LastSync, success.LastSync)
	}

	p.setMode(syncStatusPartial)
	engine.TriggerSync()
	waitForSyncComplete(t, events)
	partial := getSyncStatus(t, statusURL)
	assertRoundStatus(t, partial, syncer.RoundSummary{
		Total: 1, Skipped: 1, Outcome: syncer.RoundPartial,
	})
	assertLastSuccessPreserved(t, partial, lastSuccess)
	if !partial.LastSync.After(*failed.LastSync) {
		t.Errorf("partial 后 last_sync=%v, want 晚于 failed 的 %v", partial.LastSync, failed.LastSync)
	}

	// RuntimeState 发布后不可修改：基于当前快照深拷贝配置，再发布无规则的新状态。
	current := engine.Runtime().Snapshot()
	idleState := *current
	idleState.Config = current.Config.DeepCopy()
	idleState.Config.DomainRules = nil
	idleState.Providers = append([]provider.Provider(nil), current.Providers...)
	engine.ApplyState(&idleState)
	engine.TriggerSync()
	waitForSyncComplete(t, events)
	idle := getSyncStatus(t, statusURL)
	assertRoundStatus(t, idle, syncer.RoundSummary{Outcome: syncer.RoundIdle})
	assertLastSuccessPreserved(t, idle, lastSuccess)
	if !idle.LastSync.After(*partial.LastSync) {
		t.Errorf("idle 后 last_sync=%v, want 晚于 partial 的 %v", idle.LastSync, partial.LastSync)
	}

	// 用状态应用回调确认 Run 已进入暂停相位，再验证 TriggerSync 不制造新轮次。
	applied := make(chan bool, 2)
	engine.SetStateAppliedHook(func(state *syncer.RuntimeState) {
		applied <- state.Config.SyncEnabled
	})
	engine.Pause()
	waitForSyncDisabled(t, applied)

	callsBefore := p.callCount()
	pausedBefore := getSyncStatus(t, statusURL)
	engine.TriggerSync()
	pausedAfter := getSyncStatus(t, statusURL)
	if pausedAfter.Enabled {
		t.Fatal("暂停后 enabled = true, want false")
	}
	if p.callCount() != callsBefore {
		t.Errorf("暂停 TriggerSync 后 Provider 调用数 = %d, want %d", p.callCount(), callsBefore)
	}
	assertSameSyncHistory(t, pausedAfter, pausedBefore)
}

func newSyncStatusState(t *testing.T, enabled bool, rules []config.DomainRule, p provider.Provider) *syncer.RuntimeState {
	t.Helper()
	state, err := syncer.BuildRuntimeState(nil, config.RuntimeConfig{
		Tag:              "auto-dns",
		Interval:         time.Hour,
		DNS:              "8.8.8.8",
		DNSTimeout:       10 * time.Second,
		DNSFailThreshold: 5,
		LogLevel:         "info",
		SyncEnabled:      enabled,
		Theme:            "light",
		DomainRules:      rules,
	}, syncer.BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	state.Providers = []provider.Provider{p}
	return state
}

func syncStatusRule() config.DomainRule {
	return config.DomainRule{
		Host: "localhost", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0},
	}
}

func getSyncStatus(t *testing.T, url string) syncer.SyncStatus {
	t.Helper()
	body, code := httpGet(t, url)
	if code != http.StatusOK {
		t.Fatalf("GET /api/sync/status 状态码 = %d, want 200; body=%s", code, body)
	}

	// 先验证字段实际存在，避免 typed unmarshal 把缺失字段静默还原为零值。
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatalf("解析 /api/sync/status 字段失败: %v; body=%s", err, body)
	}
	for _, field := range []string{"running", "enabled", "last_sync", "last_success", "last_round"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("/api/sync/status 缺少字段 %q; body=%s", field, body)
		}
	}

	var status syncer.SyncStatus
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatalf("解析 /api/sync/status 值失败: %v; body=%s", err, body)
	}
	return status
}

func waitForSyncComplete(t *testing.T, events <-chan notifier.Event) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.Type == notifier.EventSyncComplete {
				return
			}
		case <-deadline.C:
			t.Fatal("未在限期内收到 sync:complete 事件")
		}
	}
}

func waitForSyncDisabled(t *testing.T, applied <-chan bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case enabled := <-applied:
			if !enabled {
				return
			}
		case <-deadline.C:
			t.Fatal("Run 未在限期内应用暂停状态")
		}
	}
}

func assertRoundStatus(t *testing.T, status syncer.SyncStatus, want syncer.RoundSummary) {
	t.Helper()
	if status.LastSync == nil || status.LastRound == nil {
		t.Fatalf("last_sync=%v last_round=%v, want 均非 null", status.LastSync, status.LastRound)
	}
	got := status.LastRound
	if !status.LastSync.Equal(got.FinishedAt) {
		t.Errorf("last_sync=%v last_round.finished_at=%v, want 相等", status.LastSync, got.FinishedAt)
	}
	if got.Total != want.Total || got.OK != want.OK || got.Changed != want.Changed ||
		got.Failed != want.Failed || got.Skipped != want.Skipped ||
		got.Added != want.Added || got.Deleted != want.Deleted || got.Outcome != want.Outcome {
		t.Errorf("last_round=%+v, want counts/outcome=%+v", *got, want)
	}
	if got.Total != got.OK+got.Changed+got.Failed+got.Skipped {
		t.Errorf("last_round 不变量被破坏: %+v", *got)
	}
}

func assertLastSuccessPreserved(t *testing.T, status syncer.SyncStatus, want time.Time) {
	t.Helper()
	if status.LastSuccess == nil || !status.LastSuccess.Equal(want) {
		t.Errorf("last_success=%v, want 保留 %v", status.LastSuccess, want)
	}
}

func assertSameSyncHistory(t *testing.T, got, want syncer.SyncStatus) {
	t.Helper()
	if got.LastSync == nil || want.LastSync == nil || !got.LastSync.Equal(*want.LastSync) {
		t.Errorf("暂停 TriggerSync 改变 last_sync: before=%v after=%v", want.LastSync, got.LastSync)
	}
	if got.LastSuccess == nil || want.LastSuccess == nil || !got.LastSuccess.Equal(*want.LastSuccess) {
		t.Errorf("暂停 TriggerSync 改变 last_success: before=%v after=%v", want.LastSuccess, got.LastSuccess)
	}
	if got.LastRound == nil || want.LastRound == nil || *got.LastRound != *want.LastRound {
		t.Errorf("暂停 TriggerSync 改变 last_round: before=%+v after=%+v", want.LastRound, got.LastRound)
	}
}

func stopSyncStatusEngine(t *testing.T, engine *syncer.Syncer) {
	t.Helper()
	engine.Stop()
	done := make(chan struct{})
	go func() {
		engine.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Syncer 未在限期内停止")
	}
}
