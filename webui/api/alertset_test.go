package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/app"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/health"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// alertReloadSubscriber 告警订阅测试替身：记录事件、进入回调时发信号，
// 并可在回调内阻塞以精确制造「Apply 之前的在途通知」。
//
// 用 channel 屏障替代 sleep 猜时序（Build6 §12.16）。
type alertReloadSubscriber struct {
	mu      sync.Mutex
	events  []notifier.Event
	entered chan struct{}
	done    chan struct{}
	block   chan struct{} // 非 nil 时每次回调先等待其关闭
}

func newAlertReloadSubscriber(buffer int) *alertReloadSubscriber {
	return &alertReloadSubscriber{
		entered: make(chan struct{}, buffer),
		done:    make(chan struct{}, buffer),
	}
}

func (s *alertReloadSubscriber) OnEvent(ev notifier.Event) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
	select {
	case s.done <- struct{}{}:
	default:
	}
	return nil
}

func (s *alertReloadSubscriber) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// waitSignal 在给定时限内等待信号，超时即判定断言失败。
func waitSignal(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(msg)
	}
}

// alertConfig 构造带启用告警的运行时配置（SMTP/Webhook 指向必然不可达的地址）。
//
// Build7 §2.1 起：只有「渠道开启 + 对应触发开启」才产生订阅，因此本夹具默认
// 开启前两个触发条件，用于验证渠道构造与构造阶段零副作用；纯策略矩阵由
// alertset_policy_test.go 覆盖。
func alertConfig(emailEnabled, webhookEnabled bool) config.RuntimeConfig {
	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: 5 * time.Minute, DNS: "223.5.5.5", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		Policy: config.AlertPolicyConfig{
			DNSFailedEnabled: true, SyncErrorEnabled: true,
			HealthTimeout: 10 * time.Minute, HealthTimeoutText: "10m",
		},
	}
	if emailEnabled {
		rc.Email = config.AlertEmailConfig{
			Enabled: true, Host: "127.0.0.1", Port: "1", Username: "u", Password: "smtp-password-secret",
			FromAddr: "f@example.com", ToAddr: "t@example.com",
		}
	}
	if webhookEnabled {
		rc.Webhook = config.AlertWebhookConfig{Enabled: true, URL: "http://127.0.0.1:1/hook-secret", Channel: "dingtalk"}
	}
	return rc
}

// buildStateFromConfig 由运行时配置构造候选完整状态
func buildStateFromConfig(t *testing.T, rc config.RuntimeConfig) (*syncer.RuntimeState, error) {
	t.Helper()
	return syncer.BuildRuntimeState(nil, rc, syncer.BreakerReset)
}

// TestBuildAlertSetSendsNothing 候选告警集合的构造阶段必须零网络副作用：
// 构造完成后事件总线上不得出现任何告警订阅者。
func TestBuildAlertSetSendsNothing(t *testing.T) {
	bus := notifier.NewEventBus()

	set := BuildAlertSet(alertConfig(true, true))
	if set.email == nil || set.webhook == nil {
		t.Fatalf("启用的渠道必须构造出 subscriber: %+v", set)
	}

	// 构造阶段没有 Apply，因此发布事件不得触发任何发送
	done := make(chan struct{})
	go func() {
		defer close(done)
		bus.Publish(notifier.Event{Type: notifier.EventSyncError, Timestamp: time.Now()})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("构造阶段不应触发任何网络发送（Publish 阻塞）")
	}

	// 安全日志元数据只含收件人与渠道名，不含密码/URL
	if set.emailToAddr != "t@example.com" || set.webhookChannel != "dingtalk" {
		t.Errorf("安全日志元数据错误: %+v", set)
	}
}

// TestBuildAlertSetRespectsDisabledFlags 禁用的渠道不得构造 subscriber。
func TestBuildAlertSetRespectsDisabledFlags(t *testing.T) {
	set := BuildAlertSet(alertConfig(false, false))
	if set.email != nil || set.webhook != nil {
		t.Errorf("禁用渠道不得构造 subscriber: %+v", set)
	}
}

// TestApplyAlertSetSwapsSubscriptions Apply 必须取消旧订阅并安装新订阅。
func TestApplyAlertSetSwapsSubscriptions(t *testing.T) {
	bus := notifier.NewEventBus()
	manager := NewAlertManager(bus)

	manager.Apply(BuildAlertSet(alertConfig(true, false)))
	first := manager.Current()
	if first.email == nil || first.webhook != nil {
		t.Fatalf("首次 Apply 订阅集合错误: %+v", first)
	}

	// 换成只启用 webhook：旧的邮件订阅必须被取消
	manager.Apply(BuildAlertSet(alertConfig(false, true)))
	second := manager.Current()
	if second.email != nil || second.webhook == nil {
		t.Fatalf("二次 Apply 订阅集合错误: %+v", second)
	}
}

// TestAlertManagerApplyIsIdempotent 重复 Apply 同一集合不 panic 且集合保持一致。
func TestAlertManagerApplyIsIdempotent(t *testing.T) {
	bus := notifier.NewEventBus()
	manager := NewAlertManager(bus)
	set := BuildAlertSet(alertConfig(true, true))

	for i := 0; i < 5; i++ {
		manager.Apply(set)
	}
	current := manager.Current()
	if current.email == nil || current.webhook == nil {
		t.Errorf("重复 Apply 后集合错误: %+v", current)
	}
}

// TestAlertManagerWithoutBusIsSafe bus 为 nil 时 Apply/LogStatus 均安全（最小接线场景）。
func TestAlertManagerWithoutBusIsSafe(t *testing.T) {
	manager := NewAlertManager(nil)
	manager.Apply(BuildAlertSet(alertConfig(true, true)))
	manager.LogStatus("已启用")
}

// TestApplyCandidateOrderAndEffects 发布副作用：日志级别与 RuntimeState 都在 apply 返回前生效。
func TestApplyCandidateOrderAndEffects(t *testing.T) {
	e := newTestEnv(t)
	e.deps.LogBroadcaster = NewLogBroadcaster("info")

	app.SetLogLevel("info")
	before := app.LogLevelVar.Level()

	rc := alertConfig(true, true)
	rc.LogLevel = "error"
	rc.Tag = "published-tag"
	rc.Theme = "dark"
	state, err := buildStateFromConfig(t, rc)
	if err != nil {
		t.Fatalf("构造候选失败: %v", err)
	}
	candidate := Candidate{State: state, Alerts: BuildAlertSet(rc)}

	e.deps.applyCandidate(candidate)

	if got := app.LogLevelVar.Level(); got != slog.LevelError {
		t.Errorf("日志级别未应用: %v → %v", before, got)
	}
	if st := e.snapshot(); st == nil || st.Config.Tag != "published-tag" || st.Config.Theme != "dark" {
		t.Errorf("RuntimeState 未发布: %+v", st)
	}
	if current := e.alerts.Current(); current.email == nil || current.webhook == nil {
		t.Errorf("告警集合未应用: %+v", current)
	}
	// 收尾：恢复全局日志级别，避免影响同包其它用例
	app.SetLogLevel("info")
}

// TestApplyCandidateWithoutRuntimeIsNoop 无 Syncer/Runtime 时 applyCandidate 不得 panic。
func TestApplyCandidateWithoutRuntimeIsNoop(t *testing.T) {
	d := &Deps{}
	d.applyCandidate(Candidate{})
}

// TestCoordinatorBuildsCandidateBeforeCommit 候选必须在 commit 之前构造，
// 且 builder 看到的是本事务已写入、尚未提交的数据。
func TestCoordinatorBuildsCandidateBeforeCommit(t *testing.T) {
	e := newTestEnv(t)

	var observedTag string
	commitFinished := false
	var appliedCount atomic.Int32
	coord := NewConfigCoordinator(e.store, func(snapshot *config.BusinessSnapshot, _ syncer.BreakerPolicy) (Candidate, error) {
		if commitFinished {
			t.Errorf("候选构造不得发生在 commit 之后")
		}
		observedTag = snapshot.Settings["tag"]
		state, err := buildStateFromConfig(t, snapshot.ToRuntimeConfig())
		if err != nil {
			return Candidate{}, err
		}
		return Candidate{State: state, Alerts: BuildAlertSet(snapshot.ToRuntimeConfig())}, nil
	}, func(c Candidate) {
		if c.State == nil {
			t.Errorf("apply 收到空候选")
		}
		appliedCount.Add(1)
		commitFinished = true
	})

	err := coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		return e.store.SetSettingTx(ctx, tx, "tag", "in-transaction")
	})
	if err != nil {
		t.Fatalf("Mutate 失败: %v", err)
	}
	if observedTag != "in-transaction" {
		t.Errorf("候选构造必须看到事务内已写入的数据: %q", observedTag)
	}
	if appliedCount.Load() != 1 || !commitFinished {
		t.Errorf("commit 后必须恰好 apply 一次: %d", appliedCount.Load())
	}

	settings, _ := e.store.GetSettings()
	if settings["tag"] != "in-transaction" {
		t.Errorf("设置未提交: %+v", settings)
	}
}

// TestCoordinatorNoApplyOnCandidateError 候选构造失败：不提交、不 apply、数据库回滚。
func TestCoordinatorNoApplyOnCandidateError(t *testing.T) {
	e := newTestEnv(t)

	applied := 0
	coord := NewConfigCoordinator(e.store, func(*config.BusinessSnapshot, syncer.BreakerPolicy) (Candidate, error) {
		return Candidate{}, errors.New("candidate failed")
	}, func(Candidate) {
		applied++
	})

	err := coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		return e.store.SetSettingTx(ctx, tx, "tag", "should-rollback")
	})
	if err == nil {
		t.Fatal("候选构造失败必须返回错误")
	}
	if applied != 0 {
		t.Errorf("候选构造失败不得 apply，实际 %d", applied)
	}
	settings, _ := e.store.GetSettings()
	if len(settings) != 0 {
		t.Errorf("候选构造失败必须回滚: %+v", settings)
	}
}

// TestCoordinatorWithoutRuntimeIsPureWrite 未注入运行时接线的协调器只提交事务、不做发布。
func TestCoordinatorWithoutRuntimeIsPureWrite(t *testing.T) {
	e := newTestEnv(t)
	coord := NewConfigCoordinator(e.store, nil, nil)

	if err := coord.Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		return e.store.SetSettingTx(ctx, tx, "tag", "pure-write")
	}); err != nil {
		t.Fatalf("纯写入 Mutate 失败: %v", err)
	}
	settings, _ := e.store.GetSettings()
	if settings["tag"] != "pure-write" {
		t.Errorf("纯写入未生效: %+v", settings)
	}
}

// TestAlertManagerEnabledChannelsMetadata 安全日志元数据来自运行时配置，不含密钥本身。
func TestAlertManagerEnabledChannelsMetadata(t *testing.T) {
	manager := NewAlertManager(nil)
	manager.Apply(BuildAlertSet(alertConfig(true, true)))

	emailOn, webhookOn, toAddr, channel := manager.enabledChannels()
	if !emailOn || !webhookOn {
		t.Fatalf("渠道启用状态错误: %v %v", emailOn, webhookOn)
	}
	if toAddr != "t@example.com" || channel != "dingtalk" {
		t.Errorf("安全日志元数据错误: to=%q channel=%q", toAddr, channel)
	}
	if toAddr == "smtp-password-secret" || channel == "http://127.0.0.1:1/hook-secret" {
		t.Errorf("安全日志元数据不得包含密钥或 URL")
	}
}

// TestAlertManagerApplyBoundaryInFlightAndNewSubscriptions Step 7「告警热重载边界」：
//
//   - Apply 之前已经取得 EventBus 快照的在途通知必须允许正常完成（不取消、不等待）；
//   - Apply 之后的新事件只投递给新订阅集合，旧订阅者不再收到任何事件。
func TestAlertManagerApplyBoundaryInFlightAndNewSubscriptions(t *testing.T) {
	bus := notifier.NewEventBus()
	manager := NewAlertManager(bus)

	old := newAlertReloadSubscriber(4)
	old.block = make(chan struct{})
	// Build7 §6.3：只有策略开启的事件类型才会被订阅，因此手工构造的集合必须显式带上 events
	reloadEvents := []notifier.EventType{notifier.EventSyncError}
	manager.Apply(alertSet{email: old, events: reloadEvents, emailToAddr: "old@example.com"})

	// 旧订阅已安装：第一次发布进入旧订阅者回调并阻塞在那里，形成真正的「在途通知」
	bus.Publish(notifier.Event{Type: notifier.EventSyncError, Timestamp: time.Now()})
	waitSignal(t, old.entered, "旧订阅者未收到 Apply 之前发布的事件")

	// 热重载：取消旧订阅、安装新订阅（在途通知仍阻塞中）
	fresh := newAlertReloadSubscriber(4)
	manager.Apply(alertSet{email: fresh, events: reloadEvents, emailToAddr: "new@example.com"})

	// Apply 之后的新发布只能投递给新订阅者
	bus.Publish(notifier.Event{Type: notifier.EventSyncError, Timestamp: time.Now()})
	waitSignal(t, fresh.done, "新订阅者未收到热重载后的事件")
	if got := fresh.count(); got != 1 {
		t.Fatalf("新订阅者事件数 = %d, want 1", got)
	}
	if got := old.count(); got != 0 {
		t.Fatalf("在途通知被阻塞期间旧订阅者不应完成，实际 %d", got)
	}

	// 放行在途通知：旧订阅者的这一次通知必须允许完成
	close(old.block)
	waitSignal(t, old.done, "热重载不得取消 Apply 之前的在途通知")
	if got := old.count(); got != 1 {
		t.Fatalf("在途通知应恰好完成一次，实际 %d", got)
	}

	// 在途通知完成后再发布：旧订阅者不得再收到任何事件
	bus.Publish(notifier.Event{Type: notifier.EventSyncError, Timestamp: time.Now()})
	waitSignal(t, fresh.done, "新订阅者未收到第二次事件")
	time.Sleep(100 * time.Millisecond) // 给可能存在的陈旧快照投递留出窗口
	if got := fresh.count(); got != 2 {
		t.Errorf("新订阅者事件数 = %d, want 2", got)
	}
	if got := old.count(); got != 1 {
		t.Errorf("热重载后旧订阅者不得再收到事件，实际 %d", got)
	}

	// 安全日志元数据只含新收件人
	if _, _, toAddr, _ := manager.enabledChannels(); toAddr != "new@example.com" {
		t.Errorf("热重载后安全日志元数据未更新: %q", toAddr)
	}
}

// runtimeWakeProbe 在唤醒调用内读取共享快照，确定性检查发布顺序。
type runtimeWakeProbe struct{ fn func() }

func (p runtimeWakeProbe) Wake() { p.fn() }
func (p runtimeWakeProbe) Evaluate(context.Context) health.Result {
	return health.Result{Healthy: true}
}

// TestApplyCandidatePublishesBeforeWake 覆盖真实 Syncer 与最小 Runtime 两条发布分支。
func TestApplyCandidatePublishesBeforeWake(t *testing.T) {
	for _, real := range []bool{false, true} {
		name := "Runtime"
		if real {
			name = "Syncer"
		}
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { app.SetLogLevel("info") })
			old := &syncer.RuntimeState{Config: config.RuntimeConfig{
				LogLevel:       "info",
				Policy:         config.DefaultAlertPolicy(),
				UptimeKumaPush: config.DefaultUptimeKumaPush(),
			}}
			next := &syncer.RuntimeState{Config: config.RuntimeConfig{
				LogLevel:       "info",
				Policy:         config.AlertPolicyConfig{HealthTimeout: 7 * time.Minute, OperationalErrorEnabled: true},
				UptimeKumaPush: config.UptimeKumaPushConfig{Enabled: true, URL: "http://127.0.0.1/api/push/new", Interval: time.Hour},
			}}
			rt := syncer.NewRuntimeManager(old)
			d := &Deps{Runtime: rt}
			if real {
				s := syncer.New(rt)
				d.Syncer = s
				t.Cleanup(s.Stop)
				if s.Runtime() != rt {
					t.Fatal("manager mismatch")
				}
			}
			order := []string{}
			probe := func(label string) runtimeWakeProbe {
				return runtimeWakeProbe{fn: func() {
					order = append(order, label)
					got := rt.Snapshot()
					if got != next {
						t.Errorf("%s Wake sees old state: enabled=%v timeout=%v", label, got.Config.UptimeKumaPush.Enabled, got.Config.Policy.HealthTimeout)
					} else if !got.Config.UptimeKumaPush.Enabled || got.Config.UptimeKumaPush.URL != next.Config.UptimeKumaPush.URL ||
						got.Config.UptimeKumaPush.Interval != time.Hour || got.Config.Policy.HealthTimeout != 7*time.Minute ||
						!got.Config.Policy.OperationalErrorEnabled {
						t.Error("candidate fields mismatch")
					}
				}}
			}
			d.Health = probe("Health")
			d.Push = probe("Push")
			d.applyCandidate(Candidate{State: next})
			if len(order) != 2 || order[0] != "Health" || order[1] != "Push" {
				t.Fatalf("order=%v", order)
			}
			if rt.Snapshot() != next {
				t.Fatal("final publication missing")
			}
		})
	}
}
