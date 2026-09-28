package syncer

import (
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestSyncStatusCarriesRoundAndProcessTimes Build7 §7.2：
// SyncStatus 必须暴露 ProcessStartedAt（进程启动近似）与 RoundStartedAt
// （当前轮次开始时间，无轮次时为 nil），供唯一健康计算源使用。
//
// 用例刻意使用「空 Provider/空规则」的 idle 轮次：不触发任何 DNS 或云调用。
func TestSyncStatusCarriesRoundAndProcessTimes(t *testing.T) {
	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	s := New(NewRuntimeManager(st))

	initial := s.Status()
	if initial.ProcessStartedAt.IsZero() {
		t.Errorf("初始状态的 ProcessStartedAt 不得为零值: %+v", initial)
	}
	if time.Since(initial.ProcessStartedAt) > time.Minute {
		t.Errorf("ProcessStartedAt 与当前时间偏差过大: %v", initial.ProcessStartedAt)
	}
	if initial.RoundStartedAt != nil {
		t.Errorf("尚无轮次时 RoundStartedAt 必须为 nil: %v", *initial.RoundStartedAt)
	}

	// 轮次开始（与 Run 主循环共用同一门控）
	if !s.beginRound() {
		t.Fatal("未请求停止时 beginRound 必须放行")
	}
	during := s.Status()
	if during.RoundStartedAt == nil {
		t.Fatalf("轮次进行中 RoundStartedAt 必须非 nil: %+v", during)
	}
	if time.Since(*during.RoundStartedAt) > time.Minute {
		t.Errorf("RoundStartedAt 与当前时间偏差过大: %v", *during.RoundStartedAt)
	}
	if during.RoundStartedAt.Before(during.ProcessStartedAt) {
		t.Errorf("RoundStartedAt 不得早于 ProcessStartedAt")
	}

	// 轮次结束：必须清空 RoundStartedAt 并记录整轮结论
	s.syncAll()
	after := s.Status()
	if after.RoundStartedAt != nil {
		t.Errorf("轮次结束后 RoundStartedAt 必须回到 nil: %v", *after.RoundStartedAt)
	}
	if after.LastSync == nil {
		t.Errorf("轮次结束后 last_sync 必须被刷新")
	}
	if after.LastRound == nil || after.LastRound.Outcome != RoundIdle {
		t.Errorf("空目标/空规则必须是 idle 轮次: %+v", after.LastRound)
	}
	if after.ProcessStartedAt.IsZero() {
		t.Errorf("ProcessStartedAt 必须在整个生命周期内保持非零")
	}
}
