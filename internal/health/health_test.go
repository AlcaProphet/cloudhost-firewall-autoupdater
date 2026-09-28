package health

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// ─── Build7 §7.2：唯一 OperationalHealth 计算源的判别性用例 ───

// stubPinger 可配置的 SQLite 探活桩：支持立即失败、立即成功与阻塞到 ctx 超时。
//
// 字段由 mu 保护：监督器在后台 goroutine 中调用 PingContext，测试会读取调用次数。
type stubPinger struct {
	mu    sync.Mutex
	err   error
	block bool
	calls int
}

func (p *stubPinger) PingContext(ctx context.Context) error {
	p.mu.Lock()
	p.calls++
	block := p.block
	err := p.err
	p.mu.Unlock()

	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

// Calls 返回探活调用次数
func (p *stubPinger) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// SetErr 设置探活错误
func (p *stubPinger) SetErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

// SetBlock 设置是否阻塞到 ctx 超时
func (p *stubPinger) SetBlock(block bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.block = block
}

// healthTestDeps 可变夹具：字段由 mu 保护，运行中可通过 set* 方法安全变更。
type healthTestDeps struct {
	mu       sync.Mutex
	pinger   *stubPinger
	status   syncer.SyncStatus
	policy   config.AlertPolicyConfig
	interval time.Duration
	now      time.Time
}

// setRunning 运行中调整主循环状态
func (d *healthTestDeps) setRunning(running bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.Running = running
}

// setOperationalEnabled 运行中调整第三触发开关
func (d *healthTestDeps) setOperationalEnabled(enabled bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.policy.OperationalErrorEnabled = enabled
}

// setup 在启动监督器之前完成一次性字段配置
func (d *healthTestDeps) setup(fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fn()
}

func newHealthTestDeps() *healthTestDeps {
	return &healthTestDeps{
		pinger:   &stubPinger{},
		status:   syncer.SyncStatus{Running: true, Enabled: true, ProcessStartedAt: time.Now().Add(-time.Minute)},
		policy:   config.DefaultAlertPolicy(),
		interval: 5 * time.Minute,
		now:      time.Now(),
	}
}

func (d *healthTestDeps) checker() *Checker {
	return New(Deps{
		Pinger: d.pinger,
		Status: func() syncer.SyncStatus {
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.status
		},
		Policy: func() config.AlertPolicyConfig {
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.policy
		},
		Interval: func() time.Duration {
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.interval
		},
		Now: func() time.Time {
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.now
		},
		PingTimeout: 100 * time.Millisecond,
	})
}

// baseStatus 返回一个“健康”的状态基线
func baseStatus(now time.Time) syncer.SyncStatus {
	last := now.Add(-time.Minute)
	finished := now.Add(-time.Minute)
	return syncer.SyncStatus{
		Running: true, Enabled: true,
		LastSync:         &last,
		LastSuccess:      &finished,
		LastRound:        &syncer.RoundSummary{FinishedAt: finished, Total: 1, OK: 1, Outcome: syncer.RoundSuccess},
		ProcessStartedAt: now.Add(-10 * time.Minute),
	}
}

func TestEvaluateHealthyBaseline(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })

	res := d.checker().Evaluate(context.Background())
	if !res.Healthy {
		t.Fatalf("基线必须是健康: %+v", res)
	}
	if len(res.Reasons) != 0 {
		t.Errorf("健康时不得有原因: %+v", res.Reasons)
	}
	if res.CheckedAt.IsZero() {
		t.Errorf("CheckedAt 必须填写")
	}
}

func TestEvaluateSQLiteFailureAndTimeout(t *testing.T) {
	// 探活失败
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.pinger.SetErr(errors.New("disk I/O error"))
	res := d.checker().Evaluate(context.Background())
	if res.Healthy || len(res.Reasons) != 1 || res.Reasons[0] != ReasonSQLite {
		t.Fatalf("SQLite 探活失败必须 unhealthy 且原因为 %q: %+v", ReasonSQLite, res)
	}

	// 探活阻塞：必须在 2 秒上限内（测试接缝 100ms）返回
	d2 := newHealthTestDeps()
	d2.status = baseStatus(d2.now)
	d2.pinger.SetBlock(true)
	start := time.Now()
	res2 := d2.checker().Evaluate(context.Background())
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("SQLite 探活必须在有界时间内返回，实际 %v", elapsed)
	}
	if res2.Healthy || res2.Reasons[0] != ReasonSQLite {
		t.Fatalf("探活超时必须按 SQLite 失败处理: %+v", res2)
	}
}

func TestEvaluateSyncerNotRunning(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.status.Running = false

	res := d.checker().Evaluate(context.Background())
	if res.Healthy {
		t.Fatalf("主循环未运行必须 unhealthy: %+v", res)
	}
	if len(res.Reasons) != 1 || res.Reasons[0] != ReasonSyncerStopped {
		t.Errorf("原因应为 %q: %+v", ReasonSyncerStopped, res.Reasons)
	}
}

func TestEvaluatePausedSkipsRoundChecks(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.status.Enabled = false
	// 暂停时这些都会导致 unhealthy，但必须被跳过
	failed := d.now.Add(-time.Hour)
	d.status.LastRound = &syncer.RoundSummary{FinishedAt: failed, Total: 1, Failed: 1, Outcome: syncer.RoundFailed}
	stale := d.now.Add(-24 * time.Hour)
	d.status.LastSync = &stale
	roundStart := d.now.Add(-time.Hour)
	d.status.RoundStartedAt = &roundStart

	res := d.checker().Evaluate(context.Background())
	if !res.Healthy {
		t.Fatalf("暂停时只检查 SQLite 与主循环，必须健康: %+v", res)
	}
}

func TestEvaluateRoundTimeout(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	started := d.now.Add(-11 * time.Minute)
	d.status.RoundStartedAt = &started

	res := d.checker().Evaluate(context.Background())
	if res.Healthy || len(res.Reasons) != 1 || res.Reasons[0] != ReasonRoundTimeout {
		t.Fatalf("轮次超时必须 unhealthy: %+v", res)
	}

	// 未超时则健康
	d2 := newHealthTestDeps()
	d2.status = baseStatus(d2.now)
	fresh := d2.now.Add(-time.Minute)
	d2.status.RoundStartedAt = &fresh
	if res := d2.checker().Evaluate(context.Background()); !res.Healthy {
		t.Fatalf("在途轮次未超时必须健康: %+v", res)
	}
}

func TestEvaluateFailedAndPartialUnhealthyUntilOverridden(t *testing.T) {
	failed := baseStatus
	_ = failed

	for _, outcome := range []syncer.RoundOutcome{syncer.RoundFailed, syncer.RoundPartial} {
		d := newHealthTestDeps()
		d.setup(func() { d.status = baseStatus(d.now) })
		d.status.LastRound = &syncer.RoundSummary{FinishedAt: d.now.Add(-time.Minute), Total: 1, Outcome: outcome}

		res := d.checker().Evaluate(context.Background())
		if res.Healthy {
			t.Fatalf("最近一轮 %s 必须 unhealthy: %+v", outcome, res)
		}
		want := ReasonLastRoundFailed
		if outcome == syncer.RoundPartial {
			want = ReasonLastRoundPartial
		}
		if len(res.Reasons) != 1 || res.Reasons[0] != want {
			t.Errorf("原因应为 %q: %+v", want, res.Reasons)
		}
	}

	// success / idle 覆盖
	for _, outcome := range []syncer.RoundOutcome{syncer.RoundSuccess, syncer.RoundIdle} {
		d := newHealthTestDeps()
		d.setup(func() { d.status = baseStatus(d.now) })
		d.status.LastRound = &syncer.RoundSummary{FinishedAt: d.now.Add(-time.Minute), Total: 1, Outcome: outcome}
		if res := d.checker().Evaluate(context.Background()); !res.Healthy {
			t.Errorf("最近一轮 %s 必须视为健康: %+v", outcome, res)
		}
	}
}

func TestEvaluateScheduleStallAndStartupGrace(t *testing.T) {
	// 调度停滞：now - last_sync > interval + health_timeout
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	stale := d.now.Add(-(d.interval + d.policy.HealthTimeout + time.Minute))
	d.status.LastSync = &stale
	res := d.checker().Evaluate(context.Background())
	if res.Healthy || len(res.Reasons) != 1 || res.Reasons[0] != ReasonScheduleStalled {
		t.Fatalf("调度停滞必须 unhealthy: %+v", res)
	}

	// 启动宽限：无 last_sync 且未超过 health_timeout → 健康
	d2 := newHealthTestDeps()
	d2.status = baseStatus(d2.now)
	d2.status.LastSync = nil
	d2.status.LastRound = nil
	d2.status.ProcessStartedAt = d2.now.Add(-time.Minute)
	if res := d2.checker().Evaluate(context.Background()); !res.Healthy {
		t.Fatalf("启动宽限内必须健康: %+v", res)
	}

	// 超过宽限：启动后尚无完成轮次
	d3 := newHealthTestDeps()
	d3.status = baseStatus(d3.now)
	d3.status.LastSync = nil
	d3.status.LastRound = nil
	d3.status.ProcessStartedAt = d3.now.Add(-11 * time.Minute)
	res3 := d3.checker().Evaluate(context.Background())
	if res3.Healthy || len(res3.Reasons) != 1 || res3.Reasons[0] != ReasonNoCompletedRound {
		t.Fatalf("启动后超出宽限必须 unhealthy: %+v", res3)
	}

	// 有在途轮次时不判调度停滞
	d4 := newHealthTestDeps()
	d4.status = baseStatus(d4.now)
	d4.status.LastSync = &stale
	inRound := d4.now.Add(-time.Minute)
	d4.status.RoundStartedAt = &inRound
	res4 := d4.checker().Evaluate(context.Background())
	if !res4.Healthy {
		t.Fatalf("在途轮次未超时时不得因调度停滞报异常: %+v", res4)
	}
}

func TestEvaluateMultipleReasonsFixedOrderAndDedup(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.pinger.SetErr(errors.New("boom"))
	d.status.Running = false
	started := d.now.Add(-time.Hour)
	d.status.RoundStartedAt = &started
	d.status.LastRound = &syncer.RoundSummary{FinishedAt: d.now.Add(-time.Minute), Total: 1, Outcome: syncer.RoundFailed}

	res := d.checker().Evaluate(context.Background())
	want := []string{ReasonSQLite, ReasonSyncerStopped, ReasonRoundTimeout, ReasonLastRoundFailed}
	if len(res.Reasons) != len(want) {
		t.Fatalf("原因数量 = %d, want %d: %+v", len(res.Reasons), len(want), res.Reasons)
	}
	for i := range want {
		if res.Reasons[i] != want[i] {
			t.Errorf("原因顺序必须固定: got %v, want %v", res.Reasons, want)
			break
		}
	}
	if res.Healthy {
		t.Errorf("多原因时必须 unhealthy")
	}
	// 去重：同一原因不得出现两次
	seen := map[string]bool{}
	for _, r := range res.Reasons {
		if seen[r] {
			t.Errorf("原因重复: %v", res.Reasons)
		}
		seen[r] = true
	}
}

func TestEvaluateDoesNotExposeUnderlyingError(t *testing.T) {
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	d.pinger.SetErr(errors.New("disk I/O error at /var/lib/fwalizer/config.db"))
	res := d.checker().Evaluate(context.Background())
	for _, reason := range res.Reasons {
		if strings.Contains(reason, "disk I/O error") || strings.Contains(reason, "/var/lib") {
			t.Errorf("稳定原因不得泄露底层错误或路径: %q", reason)
		}
	}
}
