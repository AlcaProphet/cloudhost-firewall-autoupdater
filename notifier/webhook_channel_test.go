package notifier

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
)

// TestI8113InvalidChannelNoSend 非法渠道先于限流/HTTP 拒绝，不相关事件仍忽略。
func TestI8113InvalidChannelNoSend(t *testing.T) {
	for _, channel := range []string{"", "wecom", "channel-secret"} {
		t.Run(channel, func(t *testing.T) {
			const urlSecret = "https://host-secret.invalid/path-secret?token=query-secret"
			transportErr := errors.New("transport-secret")
			calls := 0
			n := NewWebhookNotifier(urlSecret, channel)
			n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, transportErr })}
			if err := n.OnEvent(Event{Type: EventSyncComplete}); err != nil {
				t.Fatal("不相关事件应忽略")
			}
			for _, full := range []bool{false, true} {
				l := NewInFlightLimiter(1)
				n.SetInFlightLimiter(l)
				if full {
					release, ok := l.Acquire()
					if !ok {
						t.Fatal("无法占用名额")
					}
					defer release()
				}
				for _, event := range []EventType{EventDNSFailed, EventSyncError, EventOperationalUnhealthy} {
					err := n.OnEvent(Event{Type: event})
					if err == nil || err.Error() != "Webhook 发送失败: channel=unknown category=invalid_channel" {
						t.Fatalf("非法渠道返回: %v", err)
					}
					for _, secret := range []string{"host-secret", "path-secret", "query-secret", "transport-secret", "channel-secret", "wecom"} {
						if strings.Contains(err.Error(), secret) {
							t.Fatalf("错误泄漏: %v", err)
						}
					}
					if errors.Is(err, transportErr) {
						t.Fatal("保留了原始错误链")
					}
				}
				if !full {
					release, ok := l.Acquire()
					if !ok {
						t.Fatal("非法渠道占用了在途名额")
					}
					release()
				}
			}
			if calls != 0 {
				t.Fatalf("非法渠道发起 HTTP: %d", calls)
			}
		})
	}
}

// TestI8113InvalidChannelEventBusWarning 真实异步总线只输出安全类别，虚拟时间等待回调完成。
func TestI8113InvalidChannelEventBusWarning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := captureLogs(t)
		n := NewWebhookNotifier("https://host-secret.invalid/path-secret?token=query-secret", "channel-secret")
		calls := 0
		n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("transport-secret") })}
		bus := NewEventBus()
		bus.Subscribe(EventDNSFailed, n)
		bus.Publish(Event{Type: EventDNSFailed})
		synctest.Wait()
		warnings := logs.all()
		if len(warnings) != 1 || !strings.Contains(warnings[0], "channel=unknown category=invalid_channel") {
			t.Fatalf("总线安全诊断: %v", warnings)
		}
		for _, secret := range []string{"host-secret", "path-secret", "query-secret", "channel-secret", "transport-secret"} {
			if strings.Contains(warnings[0], secret) {
				t.Fatalf("WARN 泄漏: %v", warnings)
			}
		}
		if calls != 0 {
			t.Fatal("非法渠道发起 HTTP")
		}
	})
}
