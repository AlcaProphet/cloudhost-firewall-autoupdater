package syncer

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// ─── Issue6 A13：普通轮次不得重复触发「告警已更新」 ───

// TestStateAppliedHookNotFiredByOrdinaryRounds 无配置变化的多轮同步不得增长 hook 次数。
//
// 判别性：修复前 `logAppliedState` 在每次 select 返回后**无条件**调用，
// 因此 ticker/trigger 的每一轮都会多触发一次「告警已更新」日志；
// 修复后只在 Run 真正消费到新状态指针时触发。
func TestStateAppliedHookNotFiredByOrdinaryRounds(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	// 极短间隔：让 ticker 在观察窗口内触发多轮
	st := gatedState(t, true, 20*time.Millisecond)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	var hooks atomic.Int32
	s.SetStateAppliedHook(func(*RuntimeState) { hooks.Add(1) })

	startRun(t, s)

	// 等到至少跑完几轮
	waitForCalls(t, p, 3, "未在限期内跑满多轮")
	rounds := p.calls.Load()
	after := hooks.Load()

	// 再观察若干轮：hook 次数必须保持不变
	waitForCalls(t, p, rounds+3, "未在限期内继续跑轮")
	time.Sleep(150 * time.Millisecond)

	if got := hooks.Load(); got != after {
		t.Fatalf("无配置变化时 hook 次数不得增长: %d → %d（修复前随轮次增长）", after, got)
	}
	if after > 1 {
		t.Fatalf("启动基线已消费的状态只应触发一次 hook，实际 %d 次", after)
	}
}

// TestStateAppliedHookFiresOncePerApplyState ApplyState 一次只增加一次 hook，
// 且此刻 IsEnabled() 已反映新状态（hook 仍是「已被 Run 消费」的屏障）。
func TestStateAppliedHookFiresOncePerApplyState(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	st := gatedState(t, false, time.Hour) // 暂停起步，避免启动轮干扰计数
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	var hooks atomic.Int32
	enabledAtHook := atomic.Bool{}
	s.SetStateAppliedHook(func(*RuntimeState) {
		hooks.Add(1)
		enabledAtHook.Store(s.IsEnabled())
	})

	startRun(t, s)
	// 暂停态没有轮次：hook 不应被触发
	time.Sleep(200 * time.Millisecond)
	if got := hooks.Load(); got != 0 {
		t.Fatalf("暂停且无状态变化时 hook 不得触发，实际 %d 次", got)
	}

	// ApplyState 一次：hook 恰好 +1
	s.ApplyState(stateWithProvider(t, s, true, time.Hour))
	deadline := time.Now().Add(5 * time.Second)
	for hooks.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := hooks.Load(); got != 1 {
		t.Fatalf("ApplyState 一次后 hook 次数 = %d, want 1", got)
	}
	if !enabledAtHook.Load() {
		t.Error("hook 触发时 IsEnabled() 必须已反映新状态（hook 是消费屏障）")
	}

	// 再等若干轮：hook 不得继续增长
	waitForCalls(t, p, 1, "恢复后未执行首轮")
	before := hooks.Load()
	time.Sleep(200 * time.Millisecond)
	if got := hooks.Load(); got != before {
		t.Errorf("普通轮次不得再触发 hook: %d → %d", before, got)
	}
}
