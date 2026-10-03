package api

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// 本测试使用真实管理器 Apply 与生产 OnEvent，名额手工占满以隔离网络。
type p315SafeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *p315SafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *p315SafeBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }
func TestP315ManagerReloadAggregation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out p315SafeBuffer
		oldLog := slog.Default()
		oldOutput, oldFlags := log.Writer(), log.Flags()
		slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
		defer func() {
			slog.SetDefault(oldLog)
			log.SetOutput(oldOutput)
			log.SetFlags(oldFlags)
		}()
		manager := NewAlertManager(notifier.NewEventBus())
		for _, l := range []*notifier.InFlightLimiter{manager.emailLimiter, manager.webhookLimiter} {
			for i := 0; i < 4; i++ {
				release, ok := l.Acquire()
				if !ok {
					t.Fatal("fill")
				}
				defer release()
			}
		}
		cfg := alertConfig(true, true)
		manager.Apply(BuildAlertSet(cfg))
		first := manager.Current()
		event := notifier.Event{Type: notifier.EventDNSFailed, Data: map[string]any{"error": "payload-secret"}}
		send := func(s notifier.Subscriber) {
			if err := s.OnEvent(event); err != nil {
				t.Fatal(err)
			}
		}
		send(first.email)
		send(first.webhook)
		// 关闭、重新开启以及连续更换平台都不应刷新丢弃日志窗口。
		manager.Apply(BuildAlertSet(alertConfig(false, false)))
		for i := 0; i < 20; i++ {
			cfg.Webhook.Channel = "feishu"
			manager.Apply(BuildAlertSet(cfg))
			fresh := manager.Current()
			send(fresh.email)
			send(fresh.webhook)
		}
		// 退订前已取得旧快照的回调仍与新实例共享计数。
		send(first.email)
		send(first.webhook)
		if got := strings.Count(out.String(), "level=WARN"); got != 2 {
			t.Fatalf("热重载产生 %d 条首报，期望 2：%s", got, out.String())
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		logs := out.String()
		if got := strings.Count(logs, "level=WARN"); got != 4 {
			t.Fatalf("汇总条数=%d，期望4：%s", got, logs)
		}
		if strings.Count(logs, "dropped=21") != 2 || strings.Count(logs, "dropped=1 ") != 2 {
			t.Fatalf("丢弃计数不连续：%s", logs)
		}
		if !strings.Contains(logs, "channel=mixed") || !strings.Contains(logs, "dingtalk=1 feishu=20") {
			t.Fatalf("平台混合被错误标记：%s", logs)
		}
		for _, secret := range []string{"smtp-password-secret", "hook-secret", "payload-secret"} {
			if strings.Contains(logs, secret) {
				t.Fatal("泄漏", secret)
			}
		}
		time.Sleep(90 * time.Second)
		synctest.Wait()
		if strings.Count(out.String(), "level=WARN") != 4 {
			t.Fatal("空日志循环")
		}
	})
}

// 关闭订阅后仍汇总此前丢弃的尾批；新事件不会调用已关闭的渠道。
func TestP315ManagerDisabledTail(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out p315SafeBuffer
		oldLog := slog.Default()
		oldOutput, oldFlags := log.Writer(), log.Flags()
		slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
		defer func() {
			slog.SetDefault(oldLog)
			log.SetOutput(oldOutput)
			log.SetFlags(oldFlags)
		}()
		bus := notifier.NewEventBus()
		manager := NewAlertManager(bus)
		for _, l := range []*notifier.InFlightLimiter{manager.emailLimiter, manager.webhookLimiter} {
			for i := 0; i < notifier.InFlightLimit; i++ {
				release, ok := l.Acquire()
				if !ok {
					t.Fatal("fill")
				}
				defer release()
			}
		}
		manager.Apply(BuildAlertSet(alertConfig(true, true)))
		for i := 0; i < 3; i++ {
			bus.Publish(notifier.Event{Type: notifier.EventDNSFailed})
		}
		synctest.Wait()
		manager.Apply(BuildAlertSet(alertConfig(false, false)))
		for i := 0; i < 10; i++ {
			bus.Publish(notifier.Event{Type: notifier.EventDNSFailed})
		}
		synctest.Wait()
		if got := strings.Count(out.String(), "level=WARN"); got != 2 {
			t.Fatalf("first WARN=%d want 2: %s", got, out.String())
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		logs := out.String()
		if strings.Count(logs, "level=WARN") != 4 || strings.Count(logs, "dropped=2 ") != 2 {
			t.Fatalf("disabled tail lost or counted new events: %s", logs)
		}
		time.Sleep(90 * time.Second)
		synctest.Wait()
		if out.String() != logs {
			t.Fatalf("disabled channel continued logging: %s", out.String())
		}
	})
}
