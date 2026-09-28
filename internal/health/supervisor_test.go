package health

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// ─── Build7 §7.3：30 秒内部监督器的边沿语义 ───

// recordingPublisher 记录发布的事件（测试替身）
type recordingPublisher struct {
	mu     sync.Mutex
	events []notifier.Event
}

func (p *recordingPublisher) Publish(ev notifier.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func (p *recordingPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

func (p *recordingPublisher) last() (notifier.Event, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		return notifier.Event{}, false
	}
	return p.events[len(p.events)-1], true
}

// startSupervisor 以短 tick 启动监督器并返回依赖夹具
func startSupervisor(t *testing.T, d *healthTestDeps, pub *recordingPublisher, tick time.Duration) *Supervisor {
	t.Helper()
	s := NewSupervisor(SupervisorDeps{
		Checker:  d.checker(),
		Bus:      pub,
		Interval: tick,
	})
	go s.Run()
	t.Cleanup(func() { s.Stop() })
	return s
}

// waitForChecks 等待至少 n 次健康检查（通过 Pinger 调用次数观测，避免固定 sleep）
func waitForChecks(t *testing.T, d *healthTestDeps, n int, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if d.pinger.Calls() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待 %d 次健康检查超时（当前 %d）", n, d.pinger.Calls())
}

// TestSupervisorPublishesOncePerUnhealthyEpisode 健康→异常只发布一次，持续异常不刷屏。
func TestSupervisorPublishesOncePerUnhealthyEpisode(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.status.Running = false
	d.setOperationalEnabled(true)
	pub := &recordingPublisher{}

	startSupervisor(t, d, pub, 20*time.Millisecond)
	waitForChecks(t, d, 5, 5*time.Second)
	if got := pub.count(); got != 1 {
		t.Fatalf("持续异常期间事件数 = %d, want 1", got)
	}
	ev, _ := pub.last()
	if ev.Type != notifier.EventOperationalUnhealthy {
		t.Errorf("事件类型 = %q, want %q", ev.Type, notifier.EventOperationalUnhealthy)
	}
	reasons, ok := ev.Data["reasons"].([]string)
	if !ok || len(reasons) == 0 || reasons[0] != ReasonSyncerStopped {
		t.Errorf("事件必须携带稳定原因数组: %+v", ev.Data)
	}
	if _, ok := ev.Data["checked_at"]; !ok {
		t.Errorf("事件必须携带检查时间: %+v", ev.Data)
	}
}

// TestSupervisorReasonChangeDoesNotRepublish 原因变化只更新日志，不重复发布。
func TestSupervisorReasonChangeDoesNotRepublish(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.status.Running = false
	d.setOperationalEnabled(true)
	pub := &recordingPublisher{}

	startSupervisor(t, d, pub, 20*time.Millisecond)
	waitForChecks(t, d, 2, 5*time.Second)

	// 追加第二个原因（SQLite 失败）
	d.pinger.SetErr(errTestPing)
	waitForChecks(t, d, 4, 5*time.Second)
	if got := pub.count(); got != 1 {
		t.Fatalf("原因变化不得重复发布，事件数 = %d, want 1", got)
	}
}

// TestSupervisorRecoveryThenNewEpisode 恢复只写 INFO（不发事件），再次异常可重新发布一次。
func TestSupervisorRecoveryThenNewEpisode(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.status.Running = false
	d.setOperationalEnabled(true)
	pub := &recordingPublisher{}

	startSupervisor(t, d, pub, 20*time.Millisecond)
	waitForChecks(t, d, 2, 5*time.Second)
	if got := pub.count(); got != 1 {
		t.Fatalf("首次异常应发布一次，实际 %d", got)
	}

	// 恢复：不得再发布
	d.setRunning(true)
	waitForChecks(t, d, 4, 5*time.Second)
	if got := pub.count(); got != 1 {
		t.Fatalf("恢复不得发布事件，实际 %d", got)
	}

	// 再次异常：必须重新发布一次
	d.setRunning(false)
	waitForChecks(t, d, 7, 5*time.Second)
	if got := pub.count(); got != 2 {
		t.Fatalf("恢复后再次异常必须重新发布一次，实际 %d", got)
	}
}

// TestSupervisorSwitchOffNoPublishAndTurnOnPublishesOnce 第三开关只控制是否发布；
// 从关闭变为开启且当前已异常时必须补发一次。
func TestSupervisorSwitchOffNoPublishAndTurnOnPublishesOnce(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.status.Running = false
	d.setOperationalEnabled(false)
	pub := &recordingPublisher{}

	s := startSupervisor(t, d, pub, 20*time.Millisecond)
	waitForChecks(t, d, 3, 5*time.Second)
	if got := pub.count(); got != 0 {
		t.Fatalf("开关关闭时不得发布事件，实际 %d", got)
	}

	// 打开开关并唤醒：下一次检查必须补发一次当前异常
	d.setOperationalEnabled(true)
	s.Wake()
	waitForChecks(t, d, 5, 5*time.Second)
	if got := pub.count(); got != 1 {
		t.Fatalf("开启开关后必须补发一次，实际 %d", got)
	}

	// 继续异常：不得再次发布
	waitForChecks(t, d, 7, 5*time.Second)
	if got := pub.count(); got != 1 {
		t.Fatalf("补发后持续异常不得重复发布，实际 %d", got)
	}
}

// TestSupervisorWakeTriggersImmediateCheck 保存配置后必须立即唤醒检查，不必等满周期。
func TestSupervisorWakeTriggersImmediateCheck(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	pub := &recordingPublisher{}

	s := startSupervisor(t, d, pub, time.Hour)
	waitForChecks(t, d, 1, 5*time.Second)

	before := d.pinger.Calls()
	s.Wake()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.pinger.Calls() > before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Wake 后必须立即检查一次（当前 %d，之前 %d）", d.pinger.Calls(), before)
}

// TestSupervisorStopIsBoundedAndSilentAfterStop shutdown 必须停止监督、不制造伪异常、不泄漏 goroutine。
func TestSupervisorStopIsBoundedAndSilentAfterStop(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.status.Running = false
	d.setOperationalEnabled(true)
	pub := &recordingPublisher{}

	s := NewSupervisor(SupervisorDeps{Checker: d.checker(), Bus: pub, Interval: 10 * time.Millisecond})
	go s.Run()
	waitForChecks(t, d, 2, 5*time.Second)

	start := time.Now()
	s.Stop()
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Stop 必须有界返回，实际 %v", elapsed)
	}
	countAfterStop := pub.count()
	time.Sleep(100 * time.Millisecond)
	if got := pub.count(); got != countAfterStop {
		t.Errorf("shutdown 后不得再发布事件: %d → %d", countAfterStop, got)
	}

	// 重复 Stop 幂等
	s.Stop()
}

// TestSupervisorRequiresHealthyReasonsArray 事件 reasons 必须是非空稳定原因数组。
func TestSupervisorRequiresHealthyReasonsArray(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.status.Running = false
	d.setOperationalEnabled(true)
	pub := &recordingPublisher{}

	startSupervisor(t, d, pub, 20*time.Millisecond)
	waitForChecks(t, d, 2, 5*time.Second)

	ev, ok := pub.last()
	if !ok {
		t.Fatal("必须已发布事件")
	}
	reasons, _ := ev.Data["reasons"].([]string)
	joined := strings.Join(reasons, "; ")
	if strings.Contains(joined, "boom") || strings.Contains(joined, "/") {
		t.Errorf("原因数组不得泄露底层错误或路径: %q", joined)
	}
}

// errTestPing 是测试用探活错误
var errTestPing = testPingError{}

type testPingError struct{}

func (testPingError) Error() string { return "test ping failure" }
