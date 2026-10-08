package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func i8113JSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// TestI8113EmptyChannelRoundTrip 未选择状态走真实 GET/PUT/导出/导入/reset，并保持 version 3。
func TestI8113EmptyChannelRoundTrip(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(map[bool]string{false: "默认行", true: "缺行"}[absent], func(t *testing.T) {
			e := newTestEnv(t)
			if absent {
				e.execRaw(t, "DELETE FROM alert_webhook")
			}
			got := getAlertsV3(t, e)
			if got.Webhook.Channel != "" || got.Webhook.Enabled || got.Webhook.URL != "" {
				t.Fatalf("GET 未保留未选择状态: %+v", got.Webhook)
			}
			before := e.applyCount()
			w := e.do(t, http.MethodPut, "/api/alerts", i8113JSON(t, got))
			if w.Code != 200 || e.applyCount() != before+1 {
				t.Fatalf("关闭空渠道保存: %d %s", w.Code, w.Body.String())
			}
			w = e.do(t, http.MethodPost, "/api/config/export", "")
			if w.Code != 200 {
				t.Fatalf("导出: %d", w.Code)
			}
			body := w.Body.String()
			var b bundleV3
			if err := json.Unmarshal([]byte(body), &b); err != nil {
				t.Fatal(err)
			}
			if b.Version != 3 || b.Alerts.Webhook.Channel != "" || b.Alerts.Webhook.Enabled {
				t.Fatalf("导出未保留未选择状态: %+v", b.Alerts.Webhook)
			}
			before = e.applyCount()
			w = e.do(t, http.MethodPost, "/api/config/import", body)
			if w.Code != 200 || e.applyCount() != before+1 {
				t.Fatalf("空渠道往返: %d %s", w.Code, w.Body.String())
			}
			if next := getAlertsV3(t, e); !reflect.DeepEqual(got, next) {
				t.Fatal("往返改变告警配置")
			}
			// 三种历史合法渠道仍可导入；关闭允许草稿 URL，重新启用不丢失值。
			for _, channel := range []string{"dingtalk", "feishu", "slack"} {
				b.Alerts.Webhook.Channel = channel
				b.Alerts.Webhook.URL = "https://example.invalid/keep"
				w = e.do(t, http.MethodPost, "/api/config/import", i8113JSON(t, b))
				if w.Code != 200 {
					t.Fatalf("原有 version 3 渠道 %s: %d", channel, w.Code)
				}
				got = getAlertsV3(t, e)
				for _, enabled := range []bool{true, false, true} {
					got.Webhook.Enabled = enabled
					w = e.do(t, http.MethodPut, "/api/alerts", i8113JSON(t, got))
					if w.Code != 200 {
						t.Fatalf("切换开关: %d", w.Code)
					}
					if next := getAlertsV3(t, e); next.Webhook != got.Webhook {
						t.Fatal("切换开关丢失渠道或 URL")
					}
				}
			}
			w = e.do(t, http.MethodPost, "/api/config/reset", "{}")
			if w.Code != 200 {
				t.Fatalf("reset: %d", w.Code)
			}
			got = getAlertsV3(t, e)
			if got.Webhook.Channel != "" || got.Webhook.Enabled || got.Webhook.URL != "" {
				t.Fatalf("reset 未归位: %+v", got.Webhook)
			}
		})
	}
}

// TestI8113ChannelValidationAtomicity 关闭可空不等于字段可缺失/null，拒绝必须零写入零发布。
func TestI8113ChannelValidationAtomicity(t *testing.T) {
	cases := []struct{ name, hook string }{
		{"启用空渠道", `{"enabled":true,"url":"https://example.invalid/keep","channel":""}`},
		{"启用空白渠道", `{"enabled":true,"url":"https://example.invalid/keep","channel":"  "}`},
		{"未知渠道关闭", `{"enabled":false,"url":"","channel":"wecom"}`},
		{"未知渠道开启", `{"enabled":true,"url":"https://example.invalid/keep","channel":"wecom"}`},
		{"缺渠道", `{"enabled":false,"url":""}`},
		{"null渠道", `{"enabled":false,"url":"","channel":null}`},
		{"错误类型", `{"enabled":false,"url":"","channel":false}`},
		{"启用无URL", `{"enabled":true,"url":"","channel":"slack"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, unchanged := i8111Guard(t)
			var form map[string]json.RawMessage
			w := e.do(t, http.MethodGet, "/api/alerts", "")
			if err := json.Unmarshal(w.Body.Bytes(), &form); err != nil {
				t.Fatal(err)
			}
			form["webhook"] = json.RawMessage(tc.hook)
			put := i8113JSON(t, form)
			// 固定完整旧版配置包夹具；JSON 结构替换，不依赖导出格式。
			var b map[string]json.RawMessage
			if err := json.Unmarshal([]byte(validBundle()), &b); err != nil {
				t.Fatal(err)
			}
			var alerts map[string]json.RawMessage
			if err := json.Unmarshal(b["alerts"], &alerts); err != nil {
				t.Fatal(err)
			}
			alerts["webhook"] = json.RawMessage(tc.hook)
			b["alerts"] = json.RawMessage(i8113JSON(t, alerts))
			for _, req := range []struct{ method, path, body string }{{http.MethodPut, "/api/alerts", put}, {http.MethodPost, "/api/config/import", i8113JSON(t, b)}} {
				w = e.do(t, req.method, req.path, req.body)
				if w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("%s 拒绝: %d %s", req.path, w.Code, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), "webhook") {
					t.Fatalf("缺字段引导: %s", w.Body.String())
				}
				unchanged(t)
			}
		})
	}
}
