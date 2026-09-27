package notifier

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestWebhookSendErrorDoesNotExposeSecrets 验证 OnEvent 返回给调用方的错误不携带
// Webhook URL 或 transport 底层错误，也不允许通过 Unwrap 重新取得底层错误。
//
// 判别性：修复前使用 %w 包装 *url.Error，错误文本包含完整 URL，且 errors.Is
// 可以重新取得底层错误。
func TestWebhookSendErrorDoesNotExposeSecrets(t *testing.T) {
	const (
		hostSecret    = "host-secret.invalid"
		pathSecret    = "path-secret"
		querySecret   = "query-secret"
		errorSecret   = "transport-secret"
		channelSecret = "channel-secret"
	)
	transportErr := errors.New(errorSecret)
	n := NewWebhookNotifier("https://"+hostSecret+"/"+pathSecret+"?token="+querySecret, channelSecret)
	n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, transportErr
	})}

	err := n.OnEvent(Event{Type: EventSyncError, Timestamp: time.Now()})
	if err == nil {
		t.Fatal("transport 失败必须返回错误")
	}
	if !strings.Contains(err.Error(), "channel=unknown") || !strings.Contains(err.Error(), "category=transport") {
		t.Errorf("错误必须只保留安全渠道名和固定类别；实际: %q", err)
	}
	for _, secret := range []string{hostSecret, pathSecret, querySecret, errorSecret, channelSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("返回错误泄漏敏感哨兵 %q: %q", secret, err)
		}
	}
	if errors.Is(err, transportErr) {
		t.Fatal("返回错误不得 Unwrap 到可能携带敏感值的底层错误")
	}
}

func TestWebhookErrorCategory(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "canceled", err: context.Canceled, want: "canceled"},
		{name: "deadline", err: context.DeadlineExceeded, want: "timeout"},
		{name: "network", err: &net.DNSError{Err: "lookup failed"}, want: "network"},
		{name: "transport", err: errors.New("transport failed"), want: "transport"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := webhookErrorCategory(tt.err); got != tt.want {
				t.Errorf("webhookErrorCategory() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestWebhookEventBusWarningDoesNotExposeSecrets 验证生产调用链：真实 EventBus
// 异步调用 WebhookNotifier 后写出的 WARN 也不得包含 URL 或底层错误哨兵。
func TestWebhookEventBusWarningDoesNotExposeSecrets(t *testing.T) {
	const (
		hostSecret  = "bus-host-secret.invalid"
		pathSecret  = "bus-path-secret"
		querySecret = "bus-query-secret"
		errorSecret = "bus-transport-secret"
	)
	logs := captureLogs(t)
	n := NewWebhookNotifier("https://"+hostSecret+"/"+pathSecret+"?signature="+querySecret, "feishu")
	n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(errorSecret)
	})}

	bus := NewEventBus()
	bus.Subscribe(EventDNSFailed, n)
	bus.Publish(Event{Type: EventDNSFailed, Timestamp: time.Now()})

	deadline := time.Now().Add(5 * time.Second)
	var warning string
	for time.Now().Before(deadline) {
		for _, message := range logs.all() {
			if strings.Contains(message, "事件处理失败") {
				warning = message
				break
			}
		}
		if warning != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if warning == "" {
		t.Fatalf("未观察到 EventBus 的失败 WARN；实际日志: %v", logs.all())
	}
	if !strings.Contains(warning, "channel=feishu") || !strings.Contains(warning, "category=transport") {
		t.Errorf("WARN 必须保留安全渠道名和固定类别；实际: %q", warning)
	}
	for _, secret := range []string{hostSecret, pathSecret, querySecret, errorSecret} {
		if strings.Contains(warning, secret) {
			t.Errorf("WARN 泄漏敏感哨兵 %q: %q", secret, warning)
		}
	}
}
