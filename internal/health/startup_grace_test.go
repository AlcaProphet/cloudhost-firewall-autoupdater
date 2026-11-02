package health

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

func TestI8R20StartupGraceValue(t *testing.T) {
	// 独立合同锚点，不能用被测常量推导预期。
	if DefaultStartupGrace != 10*time.Second {
		t.Fatalf("启动宽限 = %v，合同要求 10s", DefaultStartupGrace)
	}
}

func TestI8R20StartupGraceDefaultBoundary(t *testing.T) {
	// 直接走生产 New/Evaluate 默认接线，不使用显式传入默认常量的公共夹具。
	// ProcessStartedAt 的生产基准是 Syncer 构造时间；固定时钟无需真实等待。
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for _, fallback := range []struct {
		name  string
		grace time.Duration
	}{
		{name: "omitted"},
		{name: "negative", grace: -time.Second},
	} {
		t.Run(fallback.name, func(t *testing.T) {
			for _, mode := range []struct {
				name    string
				enabled bool
			}{
				{name: "enabled", enabled: true},
				{name: "paused"},
			} {
				t.Run(mode.name, func(t *testing.T) {
					for _, tc := range []struct {
						name       string
						age        time.Duration
						started    bool
						running    bool
						sqliteFail bool
						wantReason string
					}{
						{name: "before_10s", age: 10*time.Second - time.Nanosecond},
						{name: "exactly_10s", age: 10 * time.Second},
						{name: "after_10s", age: 10*time.Second + time.Nanosecond, wantReason: ReasonSyncerStopped},
						{name: "started_then_stopped", age: time.Second, started: true, wantReason: ReasonSyncerStopped},
						{name: "running_after_10s", age: 11 * time.Second, started: true, running: true},
						{name: "sqlite_failure_inside_grace", age: time.Second, sqliteFail: true, wantReason: ReasonSQLite},
					} {
						t.Run(tc.name, func(t *testing.T) {
							status := syncer.SyncStatus{
								Enabled:          mode.enabled,
								Running:          tc.running,
								ProcessStartedAt: now.Add(-tc.age),
							}
							if tc.started {
								started := now.Add(-tc.age)
								status.StartedAt = &started
							}
							pinger := &stubPinger{}
							if tc.sqliteFail {
								pinger.SetErr(errors.New("测试 SQLite 探活失败"))
							}
							deps := Deps{
								Pinger:   pinger,
								Status:   func() syncer.SyncStatus { return status },
								Policy:   config.DefaultAlertPolicy,
								Interval: func() time.Duration { return 5 * time.Minute },
								Now:      func() time.Time { return now },
							}
							if fallback.grace < 0 {
								deps.StartupGrace = fallback.grace
							}
							got := New(deps).Evaluate(context.Background())
							wantReasons := []string{}
							if tc.wantReason != "" {
								wantReasons = append(wantReasons, tc.wantReason)
							}
							wantHealthy := len(wantReasons) == 0
							if got.Healthy != wantHealthy || !slices.Equal(got.Reasons, wantReasons) {
								t.Fatalf("I8R20_BEHAVIOR: age=%v，healthy=%v reasons=%v；want healthy=%v reasons=%v",
									tc.age, got.Healthy, got.Reasons, wantHealthy, wantReasons)
							}
						})
					}
				})
			}
		})
	}
}
