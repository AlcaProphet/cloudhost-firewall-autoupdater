package syncer

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// ─── Issue6 A20：Stop 后不得启动任何新轮次 ───
//
// 四处 syncAll 调用点（Run 入口启动轮、ticker、trigger、false→true 恢复轮）
// 统一走 beginRound() 硬门控。以下用例提供确定性判别，不依赖概率次数。

// TestBeginRoundGateAfterStop 门控谓词单元断言：
// Stop 之后 isStopped() 必须为 true，而 IsEnabled() **仍为 true**——
// 证明 stop 门控与 A6 的 enabled 门控是两个独立概念、不得合并。
func TestBeginRoundGateAfterStop(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	st := gatedState(t, true, time.Hour)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	if s.isStopped() {
		t.Fatal("Stop 前 isStopped() 必须为 false")
	}
	if !s.IsEnabled() {
		t.Fatal("前置条件：初始状态必须为启用")
	}

	s.Stop()

	if !s.isStopped() {
		t.Error("Stop 后 isStopped() 必须为 true")
	}
	// Issue6 A20「必须保持」：A6 的 enabled 守卫继续存在且与 stopped 门控并列、不合并
	if !s.IsEnabled() {
		t.Error("Stop 不得改变 IsEnabled()：开关真值由 SQLite 与已发布运行时状态决定")
	}
	if s.beginRound() {
		t.Error("Stop 后 beginRound() 必须返回 false（新轮次被硬门控拦截）")
	}
}

// TestStopBeforeRunWaitReturns Stop 在 Run 从未取得生命周期所有权时，必须直接完成
// Syncer 生命周期，使 Wait 有界返回。修复前 doneCh 只由 Run 关闭，本用例会超时。
func TestStopBeforeRunWaitReturns(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	st := gatedState(t, true, time.Hour)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	s.Stop()

	waited := make(chan struct{})
	go func() {
		s.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("Run 从未启动时，Stop 后 Wait 必须有界返回")
	}

	// Stop 是吸收态：生命周期已完成后，首次 Run 也必须立即拒绝，不能短暂进入 running。
	runReturned := make(chan struct{})
	go func() {
		s.Run()
		close(runReturned)
	}()
	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("Stop 后的首次 Run 必须有界返回")
	}
	if s.Status().Running {
		t.Error("Stop 后的首次 Run 不得进入 running 状态")
	}
	if got := p.calls.Load(); got != 0 {
		t.Fatalf("Stop 后的首次 Run 不得调用 Provider：GetRules = %d, want 0", got)
	}
}

// TestWaitBeforeRunAndStopBlocksUntilStop 固定 Wait 的调用合同：尚未 Run 且尚未 Stop 时
// 继续等待；Stop 才把未启动实例推进到终止态，并广播释放全部 Wait 调用者。
func TestWaitBeforeRunAndStopBlocksUntilStop(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	st := gatedState(t, true, time.Hour)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	const waiters = 4
	waited := make(chan struct{}, waiters)
	for range waiters {
		go func() {
			s.Wait()
			waited <- struct{}{}
		}()
	}

	select {
	case <-waited:
		t.Fatal("Run/Stop 均未发生时 Wait 不得提前返回")
	case <-time.After(100 * time.Millisecond):
	}

	s.Stop()
	for i := 0; i < waiters; i++ {
		select {
		case <-waited:
		case <-time.After(time.Second):
			t.Fatalf("Stop 后第 %d/%d 个 Wait 未有界返回", i+1, waiters)
		}
	}
}

// TestStopBeforeRunStartsNoRound Stop() 先于 go Run()：不得启动任何轮次。
//
// 这是 100% 确定性的红灯证据：修复前 Run 入口的启动轮位于循环之外、stop 检查之前，
// 因此「启动即收到 SIGTERM」（run.go 的 go s.Run() / s.Stop() 调度顺序）会照常
// 执行一整轮真实云写入（Describe → Diff → Create/Delete）。
func TestStopBeforeRunStartsNoRound(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	st := gatedState(t, true, time.Hour)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	s.Stop()

	done := make(chan struct{})
	go func() {
		s.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 后 Run 必须立即退出（不得启动启动轮）")
	}

	if got := p.calls.Load(); got != 0 {
		t.Fatalf("Stop 后启动 Run 不得调用任何 Provider：GetRules = %d, want 0", got)
	}
}

// TestStopInjectedDuringRoundBlocksNextRound 确定性 trigger/stop 交错：
//
// hook 在「stop 门控通过、syncAll 之前」触发；hook 内调用 Stop()，因此下一次门控
// 判定必然看到已停止。断言 Provider 调用次数恒为 1（只保留当轮，不启动新轮）。
func TestStopInjectedDuringRoundBlocksNextRound(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, true)
	st := gatedState(t, true, 20*time.Millisecond)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	var witnessCalls atomic.Int32
	var hookRuns atomic.Int32

	s.SetBeforeRoundHook(func() {
		// 记录门控通过那一刻已发生的云调用次数，并在轮次开始前注入 Stop()
		witnessCalls.Store(p.calls.Load())
		hookRuns.Add(1)
		s.Stop()
	})

	go s.Run()
	t.Cleanup(func() {
		// 若用例提前失败，释放可能仍阻塞在 GetRules 的轮次，保证清理有界
		select {
		case <-p.started:
		default:
		}
		p.release <- struct{}{}
		stopRunBounded(t, s, 10*time.Second)
	})

	// 等待第一轮真正进入 Provider（证明门控确实放行了当轮）
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("第一轮未进入 Provider：门控不应拦截停止前已开始的轮次")
	}
	// 放行第一轮，让它正常完成
	p.release <- struct{}{}

	// 等待 Run 退出：Stop 已在 hook 内注入，所有后续门控都必须拒绝
	exited := make(chan struct{})
	go func() {
		s.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		// 兜底：再放行一次，避免 ticker 触发的第二轮一直阻塞
		p.release <- struct{}{}
		select {
		case <-exited:
			t.Fatal("Stop 后仍启动了新轮次并进入 Provider（第二轮被阻塞）")
		case <-time.After(3 * time.Second):
			t.Fatal("Run 在 Stop 后未退出")
		}
	}

	if got := hookRuns.Load(); got != 1 {
		t.Errorf("beginRound 通过次数 = %d, want 1（Stop 后门控必须拒绝后续轮次）", got)
	}
	if got := witnessCalls.Load(); got != 0 {
		t.Errorf("门控通过时已发生的云调用 = %d, want 0（门控位于 syncAll 之前）", got)
	}
	if got := p.calls.Load(); got != 1 {
		t.Fatalf("Provider 调用次数 = %d, want 1（只保留 Stop 前已开始的当轮）", got)
	}
}

// TestRunSecondCallRejectedWithoutPanic Run 的第二/并发第二次调用必须被拒绝且有界返回。
//
// 修复前第二次 Run 的 defer close(s.doneCh) 会对同一 channel 关闭两次而 panic。
func TestRunSecondCallRejectedWithoutPanic(t *testing.T) {
	p := newCountingProvider(config.CloudTCCVM, false)
	st := gatedState(t, true, time.Hour)
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))

	firstExited := make(chan struct{})
	go func() {
		s.Run()
		close(firstExited)
	}()

	waitForRunning(t, s, 5*time.Second)

	// 首个 Run 仍在运行时的第二次调用：必须立即返回（不得阻塞、不得 panic）
	for i := 0; i < 3; i++ {
		returned := make(chan struct{})
		go func() {
			s.Run()
			close(returned)
		}()
		select {
		case <-returned:
		case <-time.After(3 * time.Second):
			stopRunBounded(t, s, 5*time.Second)
			t.Fatal("第二次 Run 必须在有界时间内返回（不得等待首个 Run 结束）")
		}
	}

	stopRunBounded(t, s, 10*time.Second)

	select {
	case <-firstExited:
	case <-time.After(5 * time.Second):
		t.Fatal("首个 Run 未退出")
	}

	// 首个 Run 已退出之后再调用：同样必须被拒绝且有界返回
	afterExit := make(chan struct{})
	go func() {
		s.Run()
		close(afterExit)
	}()
	select {
	case <-afterExit:
	case <-time.After(3 * time.Second):
		t.Fatal("首个 Run 退出后的第二次 Run 必须有界返回")
	}
}

// waitForRunning 等待 Syncer 进入 running 状态。
func waitForRunning(t *testing.T, s *Syncer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.Status().Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Syncer 未进入 running 状态")
}
