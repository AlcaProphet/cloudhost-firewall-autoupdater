package syncer

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// ─── Issue6 A13 / I8-R04：状态消费回调与普通轮次 ───

// stateAppliedHookFixture 复用真实 Run 与目标链，只将 DNS 和云端替换为内存夹具。
// Provider 新增后能返回新规则，避免把覆盖验证失败的轮次作为测试成功前提。
func stateAppliedHookFixture(t *testing.T, enabled bool, interval time.Duration) (*Syncer, *targetProbeProvider, func()) {
	t.Helper()
	p := newProbeProvider(config.CloudTCCVM, 0)
	state := gatedState(t, enabled, interval)
	state.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(state))
	s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
		return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
	}
	// Wait 只保证稳定阻塞；Run 可能正阻塞于真实目标冷却，不能据此宣称轮次已结束。
	// 先等待正式整轮完成事件，再 Wait，既推动虚拟时间也让后续状态 hook 完成。
	events, cancel := s.EventBus().SubscribeChan()
	t.Cleanup(cancel)
	waitRound := func() {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case event := <-events:
				if event.Type == notifier.EventSyncComplete {
					synctest.Wait()
					return
				}
			case <-deadline:
				t.Fatal("5 秒虚拟时间内未收到本次整轮完成事件")
			}
		}
	}
	return s, p, waitRound
}

func stateAppliedHookSnapshotCalls(p *targetProbeProvider) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotCalls
}

// stateAppliedHookWantRounds 在稳定点核对实际执行：本夹具每轮恰好读取 S0/S1。
// 无清理候选，不需要 S2；同时检查成功汇总与在途标记，不能只看触发次数。
func stateAppliedHookWantRounds(t *testing.T, s *Syncer, p *targetProbeProvider, want int) {
	t.Helper()
	if got := stateAppliedHookSnapshotCalls(p); got != 2*want {
		t.Fatalf("快照调用次数 = %d, want %d（%d 轮的 S0/S1）", got, 2*want, want)
	}
	status := s.Status()
	if status.RoundStartedAt != nil {
		t.Fatal("稳定点仍有在途轮次")
	}
	if want == 0 {
		if status.LastRound != nil || status.LastSync != nil {
			t.Fatal("暂停启动不应有已完成轮次")
		}
		return
	}
	if status.LastRound == nil || status.LastRound.Outcome != RoundSuccess || status.LastRound.Total != 1 {
		t.Fatalf("最近实际轮次应为单目标成功，得到 %+v", status.LastRound)
	}
}

// TestStateAppliedHookNotFiredByOrdinaryRounds 保护启动基线、定时轮和手动轮。
// 所有读取都在 synctest.Wait 或有界 Stop/Wait 之后；不以短暂 sleep 猜测执行进度。
func TestStateAppliedHookNotFiredByOrdinaryRounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, p, waitRound := stateAppliedHookFixture(t, true, 10*time.Second)
		var hooks atomic.Int32
		s.SetStateAppliedHook(func(*RuntimeState) { hooks.Add(1) })
		startRun(t, s) // 失败路径也由现有有界清理回收 Run。
		waitRound()
		stateAppliedHookWantRounds(t, s, p, 1)
		if got := hooks.Load(); got != 0 {
			t.Fatalf("构造时已记录初始状态，启动 hook 次数 = %d, want 0", got)
		}

		for i := 0; i < 6; i++ {
			synctest.Sleep(10 * time.Second)
			waitRound()
			stateAppliedHookWantRounds(t, s, p, i+2)
			if got := hooks.Load(); got != 0 {
				t.Fatalf("定时普通轮 hook 次数 = %d, want 0", got)
			}
		}
		for i := 0; i < 3; i++ {
			s.TriggerSync()
			waitRound()
			stateAppliedHookWantRounds(t, s, p, i+8)
			if got := hooks.Load(); got != 0 {
				t.Fatalf("手动普通轮 hook 次数 = %d, want 0", got)
			}
		}

		stopRunBounded(t, s, 10*time.Second)
		stateAppliedHookWantRounds(t, s, p, 10)
		if got := hooks.Load(); got != 0 || s.Status().Running {
			t.Fatalf("退出后 hook 次数 = %d, want 0；Run 必须已退出", got)
		}
	})
}

// TestStateAppliedHookFiresOncePerApplyState 对每份串行提交、已被 Run 消费的新状态各回调一次。
// 控制通知允许合并，不将此测试解释为快速连续 ApplyState 的逐次回调保证。
// 回调内记录一致观察，稳定点再断言，避免计数先增长、状态尚未写入的窗口。
// 计数与观察还需原子操作/互斥锁保护：Wait 不能替代后续回调与先前读取之间的同步。
func TestStateAppliedHookFiresOncePerApplyState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, p, waitRound := stateAppliedHookFixture(t, false, time.Hour)
		type observation struct {
			state        *RuntimeState
			enabled      bool
			snapshots    int
			roundReady   bool
			roundRunning bool
		}
		var observations []observation
		var observationMu sync.Mutex
		readObservations := func() []observation {
			observationMu.Lock()
			defer observationMu.Unlock()
			return append([]observation(nil), observations...)
		}
		s.SetStateAppliedHook(func(state *RuntimeState) {
			status := s.Status()
			observationMu.Lock()
			observations = append(observations, observation{
				state: state, enabled: s.IsEnabled(), snapshots: stateAppliedHookSnapshotCalls(p),
				roundReady:   status.LastRound != nil && status.LastRound.Outcome == RoundSuccess,
				roundRunning: status.RoundStartedAt != nil,
			})
			observationMu.Unlock()
		})
		startRun(t, s)
		synctest.Wait()
		stateAppliedHookWantRounds(t, s, p, 0)
		if records := readObservations(); len(records) != 0 {
			t.Fatalf("暂停启动 hook 次数 = %d, want 0", len(records))
		}

		// 每次先等待真实消费，再提交下一份；hook 内的记录必须对应本次状态和完整轮数。
		apply := func(next *RuntimeState, wantHooks, wantRounds int) {
			t.Helper()
			before := stateAppliedHookSnapshotCalls(p)
			s.ApplyState(next)
			if 2*wantRounds > before {
				waitRound()
			} else {
				synctest.Wait()
			}
			stateAppliedHookWantRounds(t, s, p, wantRounds)
			records := readObservations()
			if len(records) != wantHooks {
				t.Fatalf("已消费新状态 hook 次数 = %d, want %d", len(records), wantHooks)
			}
			got := records[wantHooks-1]
			if got.state != next || got.enabled != next.Config.SyncEnabled {
				t.Fatal("hook 必须观察本次状态指针及其同步开关")
			}
			if got.snapshots != 2*wantRounds || !got.roundReady || got.roundRunning {
				t.Fatalf("hook 必须位于调度决策及本次恢复轮完成之后，观察 %+v", got)
			}
		}
		reapply := func(next *RuntimeState, wantHooks, wantRounds int) {
			t.Helper()
			s.ApplyState(next)
			synctest.Wait()
			stateAppliedHookWantRounds(t, s, p, wantRounds)
			if records := readObservations(); len(records) != wantHooks {
				t.Fatalf("同一已消费指针重复通知 hook 次数 = %d, want %d", len(records), wantHooks)
			}
		}

		first := stateWithProvider(t, s, true, time.Hour)
		apply(first, 1, 1)
		reapply(first, 1, 1)
		for i := 0; i < 3; i++ {
			s.TriggerSync()
			waitRound()
			stateAppliedHookWantRounds(t, s, p, i+2)
			if records := readObservations(); len(records) != 1 {
				t.Fatalf("手动普通轮 hook 次数 = %d, want 1", len(records))
			}
		}

		// 新指针但仍启用、间隔不变：应有新回调，不应额外起轮。
		second := stateWithProvider(t, s, true, time.Hour)
		second.Config.Theme = "dark"
		apply(second, 2, 4)
		paused := stateWithProvider(t, s, false, time.Hour)
		apply(paused, 3, 4)
		reapply(paused, 3, 4)
		resumed := stateWithProvider(t, s, true, time.Hour)
		apply(resumed, 4, 5)
		reapply(resumed, 4, 5)

		stopRunBounded(t, s, 10*time.Second)
		stateAppliedHookWantRounds(t, s, p, 5)
		if records := readObservations(); len(records) != 4 || s.Status().Running {
			t.Fatalf("退出后 hook 次数 = %d, want 4；Run 必须已退出", len(records))
		}
	})
}
