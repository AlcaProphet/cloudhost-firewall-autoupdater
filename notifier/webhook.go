package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// webhookTimeout 单次 Webhook 请求上限。
//
// 非导出变量仅为测试接缝（Issue6 §六.4 F8）：默认值保持既有 10s 不变。
var webhookTimeout = 10 * time.Second

// WebhookNotifier Webhook 告警（支持钉钉/飞书/Slack 格式）
type WebhookNotifier struct {
	url     string
	channel string
	client  *http.Client
	// limiter 为渠道在途限流器（由 AlertManager 注入；nil 表示不限流）
	limiter *InFlightLimiter
}

// NewWebhookNotifier 创建 Webhook 通知器
func NewWebhookNotifier(url, channel string) *WebhookNotifier {
	if channel == "" {
		channel = "dingtalk"
	}
	return &WebhookNotifier{
		url:     url,
		channel: channel,
		client:  &http.Client{Timeout: webhookTimeout},
	}
}

// SetInFlightLimiter 注入渠道在途限流器（实现 limitedNotifier）
func (n *WebhookNotifier) SetInFlightLimiter(l *InFlightLimiter) { n.limiter = l }

// ChannelName 返回渠道名（用于安全日志；不暴露 URL）
func (n *WebhookNotifier) ChannelName() string { return n.channel }

// OnEvent 实现 Subscriber 接口
func (n *WebhookNotifier) OnEvent(event Event) error {
	if event.Type != EventSyncError && event.Type != EventDNSFailed {
		return nil
	}

	// 在途上限：满载丢弃最新通知
	if n.limiter != nil {
		release, ok := n.limiter.Acquire()
		if !ok {
			logDropped(n.ChannelName(), event, InFlightLimit)
			return nil
		}
		defer release()
	}

	content := fmt.Sprintf("[FWAlizer] %s\n%s", event.Type, formatEventBody(event))
	var payload map[string]any
	switch n.channel {
	case "feishu":
		payload = map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": content},
		}
	case "slack":
		payload = map[string]any{"text": content}
	default: // dingtalk
		payload = map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": content},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := n.client.Post(n.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("Webhook 发送失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("Webhook 返回状态码: %d", resp.StatusCode)
	}
	return nil
}
