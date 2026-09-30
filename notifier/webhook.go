package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// webhookTimeout 单次 Webhook 请求上限。
//
// 非导出变量仅为测试接缝（Issue6 §六.4 F8）：默认值保持既有 10s 不变。
var webhookTimeout = 10 * time.Second

// webhookResponseLimit 限制第三方响应体大小；额外读一字节区分恰好上限和超限。
const webhookResponseLimit = 16 * 1024

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
func (n *WebhookNotifier) ChannelName() string {
	switch n.channel {
	case "dingtalk", "feishu", "slack":
		return n.channel
	default:
		return "unknown"
	}
}

// webhookErrorCategory 把底层错误收敛为固定类别。
// 只用底层错误做类型判别，调用方得到的错误不包装也不输出它。
func webhookErrorCategory(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}

	// http.Client 会把 RoundTripper 错误包装成 *url.Error；先取内部错误，
	// 避免把所有 transport 错误都因 *url.Error 实现 net.Error 而误归为 network。
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout"
		}
		return "network"
	}
	return "transport"
}

// OnEvent 实现 Subscriber 接口
func (n *WebhookNotifier) OnEvent(event Event) error {
	if event.Type != EventSyncError && event.Type != EventDNSFailed && event.Type != EventOperationalUnhealthy {
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

	// 详情块与邮件共用同一固定渲染器（Build7 Step 7）：顺序稳定、缺失写 "-"、
	// operational 事件追加固定「原因」行；首行保留事件类型标识便于检索。
	content := fmt.Sprintf("[FWAlizer] %s\n%s", event.Type, formatEventDetails(event))
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
		return fmt.Errorf("Webhook 发送失败: channel=%s category=%s", n.ChannelName(), webhookErrorCategory(err))
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Webhook 响应体关闭失败", "channel", n.ChannelName(), "category", "response_close")
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Webhook 发送失败: channel=%s category=http_status status=%d", n.ChannelName(), resp.StatusCode)
	}
	response, err := io.ReadAll(io.LimitReader(resp.Body, webhookResponseLimit+1))
	if err != nil {
		return fmt.Errorf("Webhook 发送失败: channel=%s category=response_read", n.ChannelName())
	}
	if len(response) > webhookResponseLimit {
		return fmt.Errorf("Webhook 发送失败: channel=%s category=response_too_large", n.ChannelName())
	}
	return validateWebhookResponse(n.ChannelName(), resp.StatusCode, response)
}

// validateWebhookResponse 仅把明确的渠道成功响应视为成功，不输出平台原文。
func validateWebhookResponse(channel string, status int, body []byte) error {
	invalid := func() error {
		return fmt.Errorf("Webhook 发送失败: channel=%s category=invalid_response", channel)
	}
	if channel == "slack" {
		if status == http.StatusOK && strings.TrimSpace(string(body)) == "ok" {
			return nil
		}
		return invalid()
	}
	var keys []string
	switch channel {
	case "dingtalk":
		keys = []string{"errcode"}
	case "feishu":
		// code 为当前字段，StatusCode 仅兼容历史响应；同时出现时必须全部为零。
		keys = []string{"code", "StatusCode"}
	default:
		return invalid()
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return invalid()
	}
	found := false
	for _, key := range keys {
		raw, exists := fields[key]
		if !exists {
			continue
		}
		found = true
		// 指针明确区分零值与 null，RawMessage 保留字段是否存在及精确字段名。
		var code *int64
		if err := json.Unmarshal(raw, &code); err != nil || code == nil {
			return invalid()
		}
		if *code != 0 {
			return fmt.Errorf("Webhook 发送失败: channel=%s category=business_response code=%d", channel, *code)
		}
	}
	if !found {
		return invalid()
	}
	return nil
}
