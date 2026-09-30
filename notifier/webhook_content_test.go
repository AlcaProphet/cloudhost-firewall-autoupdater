package notifier

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ─── Build7 Step 7：Webhook 与邮件共用固定详情渲染器 ───

// captureWebhook 构造一个用可替换 Transport 捕获请求体的 WebhookNotifier。
func captureWebhook(t *testing.T, channel string) (*WebhookNotifier, *[]string) {
	t.Helper()
	bodies := &[]string{}
	n := NewWebhookNotifier("https://example.invalid/hook?token=secret", channel)
	n.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		*bodies = append(*bodies, string(data))
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(map[string]string{"dingtalk": `{"errcode":0}`, "feishu": `{"code":0,"StatusCode":0}`, "slack": "ok"}[channel])),
			Header:     make(http.Header),
		}, nil
	})}
	return n, bodies
}

// webhookText 从渠道载荷中取出实际发送的纯文本内容。
func webhookText(t *testing.T, channel, body string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("解析 %s 载荷失败: %v; body=%s", channel, err, body)
	}
	switch channel {
	case "feishu":
		content, _ := payload["content"].(map[string]any)
		text, _ := content["text"].(string)
		return text
	case "slack":
		text, _ := payload["text"].(string)
		return text
	default: // dingtalk
		text, _ := payload["text"].(map[string]any)
		content, _ := text["content"].(string)
		return content
	}
}

// TestWebhookContentIsDeterministicAndUnified Build7 Step 7：
//   - 同一事件连续渲染 20 次必须逐字节一致（修复前遍历 map，顺序随机）；
//   - 详情块与邮件同构：事件类型/时间/Provider/域名/错误 + 运行健康异常的可选「原因」行；
//   - 未固定字段（checked_at 等）不得出现。
func TestWebhookContentIsDeterministicAndUnified(t *testing.T) {
	event := Event{
		Type:      EventOperationalUnhealthy,
		Timestamp: time.Date(2026, 9, 28, 12, 34, 56, 0, time.Local),
		Data: map[string]any{
			"checked_at": "2026-09-28T12:34:56Z",
			"reasons":    []string{"最近一轮同步失败", "SQLite 检查失败"},
		},
	}

	for _, channel := range []string{"dingtalk", "feishu", "slack"} {
		t.Run(channel, func(t *testing.T) {
			n, bodies := captureWebhook(t, channel)

			const runs = 20
			for i := 0; i < runs; i++ {
				if err := n.OnEvent(event); err != nil {
					t.Fatalf("第 %d 次发送失败: %v", i+1, err)
				}
			}
			if len(*bodies) != runs {
				t.Fatalf("请求次数 = %d, want %d", len(*bodies), runs)
			}
			first := (*bodies)[0]
			for i, body := range *bodies {
				if body != first {
					t.Fatalf("同一事件第 %d 次渲染与首次不一致（详情顺序必须稳定）:\n首次: %s\n本次: %s", i+1, first, body)
				}
			}

			text := webhookText(t, channel, first)
			for _, fragment := range []string{
				"[FWAlizer] operational:unhealthy",
				"事件类型：运行健康异常",
				"时间：2026-09-28 12:34:56",
				"Provider：-", "域名：-", "错误：-",
				"原因：最近一轮同步失败; SQLite 检查失败",
			} {
				if !strings.Contains(text, fragment) {
					t.Fatalf("%s 详情块缺少 %q: %q", channel, fragment, text)
				}
			}
			if strings.Contains(text, "checked_at") {
				t.Errorf("%s 详情块不得遍历 map 输出未固定字段: %q", channel, text)
			}

			// 固定顺序：事件类型 → 时间 → Provider → 域名 → 错误 → 原因
			last := -1
			for _, fragment := range []string{
				"事件类型：运行健康异常", "时间：2026-09-28 12:34:56",
				"Provider：-", "域名：-", "错误：-", "原因：",
			} {
				idx := strings.Index(text, fragment)
				if idx < 0 {
					t.Fatalf("%s 详情块缺少 %q: %q", channel, fragment, text)
				}
				if idx < last {
					t.Errorf("%s 详情顺序不稳定: %q 出现在 %q 之前", channel, fragment, text[:last])
				}
				last = idx
			}
		})
	}
}

// TestWebhookSyncEventUsesFixedBlockWithoutExtraKeys 同步失败事件的 Webhook 文本
// 必须与邮件同构：固定 5 行详情块、不含 added/deleted 等未固定键、不含原因行。
func TestWebhookSyncEventUsesFixedBlockWithoutExtraKeys(t *testing.T) {
	n, bodies := captureWebhook(t, "dingtalk")
	event := Event{
		Type:      EventSyncError,
		Timestamp: time.Date(2026, 9, 28, 12, 34, 56, 0, time.Local),
		Data: map[string]any{
			"provider": "tc_lighthouse(lhins-1)", "domain": "example.com",
			"error": "boom", "added": 2, "deleted": 1,
		},
	}
	if err := n.OnEvent(event); err != nil {
		t.Fatalf("同步失败事件发送失败: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("请求次数 = %d, want 1", len(*bodies))
	}
	text := webhookText(t, "dingtalk", (*bodies)[0])
	for _, fragment := range []string{
		"[FWAlizer] sync:error", "事件类型：同步失败",
		"Provider：tc_lighthouse(lhins-1)", "域名：example.com", "错误：boom",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("同步失败详情块缺少 %q: %q", fragment, text)
		}
	}
	for _, absent := range []string{"added", "deleted", "原因："} {
		if strings.Contains(text, absent) {
			t.Errorf("同步失败详情块不得包含 %q: %q", absent, text)
		}
	}
}
