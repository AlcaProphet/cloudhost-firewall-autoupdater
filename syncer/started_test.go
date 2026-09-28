package syncer

import (
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// newStartedTestSyncer 构造空 Provider/空规则的 Syncer（idle 轮次，不访问 DNS 或云 API）。
func newStartedTestSyncer(t *testing.T) *Syncer {
	t.Helper()
	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: 10 * time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	return New(NewRuntimeManager(st))
}

// TestStartedSignalClosesAfterRunBegins Build7 Step 7：
// Started() 必须在 Run 把 running=true 置为可见之后关闭一次，且 StartedAt 随之写入；
// Run 退出后 StartedAt 保留（供健康判定区分「已启动后停止」）。
func TestStartedSignalClosesAfterRunBegins(t *testing.T) {
	s := newStartedTestSyncer(t)

	if got := s.Status(); got.StartedAt != nil || got.Running {
		t.Fatalf("Run 之前必须处于未运行且未启动状态: %+v", got)
	}
	select {
	case <-s.Started():
		t.Fatalf("Run 之前不得关闭 Started 信号")
	default:
	}

	go s.Run()

	select {
	case <-s.Started():
	case <-time.After(5 * time.Second):
		t.Fatalf("Run 之后 5 秒内必须关闭 Started 信号")
	}

	// 读到通道关闭即必须能看到 running=true（channel close 的 happens-before 保证）
	after := s.Status()
	if !after.Running {
		t.Errorf("Started 关闭时必须已置 running=true: %+v", after)
	}
	if after.StartedAt == nil {
		t.Fatalf("Started 关闭时必须已写入 StartedAt: %+v", after)
	}
	if time.Since(*after.StartedAt) > time.Minute {
		t.Errorf("StartedAt 与当前时间偏差过大: %v", *after.StartedAt)
	}

	s.Stop()
	s.Wait()

	stopped := s.Status()
	if stopped.Running {
		t.Errorf("Stop 并退出后 running 必须为 false: %+v", stopped)
	}
	if stopped.StartedAt == nil {
		t.Errorf("已启动过的 StartedAt 必须保留，供「已启动后停止」判定: %+v", stopped)
	}
}

// TestStartedNeverClosesWhenStopPrecedesRun Build7 Step 7：
// Stop 先于 Run（吸收态）时 Run 被拒绝，Started() 永不关闭，StartedAt 保持 nil；
// 调用方（run.go）因此必须使用有界等待。
func TestStartedNeverClosesWhenStopPrecedesRun(t *testing.T) {
	s := newStartedTestSyncer(t)
	s.Stop()

	done := make(chan struct{})
	go func() {
		s.Run() // 吸收态：WARN 后立即返回，不取得生命周期所有权
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Stop 之后的 Run 必须立即返回")
	}
	select {
	case <-s.Started():
		t.Fatalf("Stop 先于 Run 时不得关闭 Started 信号")
	case <-time.After(100 * time.Millisecond):
	}
	if got := s.Status(); got.StartedAt != nil {
		t.Errorf("从未进入运行态时 StartedAt 必须为 nil: %+v", got)
	}
}
