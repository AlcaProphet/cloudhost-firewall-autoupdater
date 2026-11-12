package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// i9r04Body 使用原始 JSON 检查正文存在且为字符串，避免零值掩盖缺失/null。
func i9r04Body(t *testing.T, wire []byte, path ...string) string {
	t.Helper()
	raw := json.RawMessage(wire)
	for _, key := range path {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		var ok bool
		raw, ok = object[key]
		if !ok {
			t.Fatalf("缺少字段 %s", key)
		}
	}
	if len(raw) == 0 || raw[0] != '"' {
		t.Fatalf("正文必须是显式字符串，实际 %s", raw)
	}
	var body string
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func i9r04JSON(t *testing.T, value any) string {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(wire)
}

func i9r04Export(t *testing.T, e *testEnv) string {
	t.Helper()
	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出状态码=%d, body=%s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func i9r04CheckReads(t *testing.T, e *testEnv, want string) string {
	t.Helper()
	before := e.applyCount()
	w := e.do(t, http.MethodGet, "/api/alerts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET 状态码=%d", w.Code)
	}
	if got := i9r04Body(t, w.Body.Bytes(), "email", "body"); got != want {
		t.Errorf("GET 正文=%q, want %q", got, want)
	}
	exported := i9r04Export(t, e)
	if got := i9r04Body(t, []byte(exported), "alerts", "email", "body"); got != want {
		t.Errorf("导出正文=%q, want %q", got, want)
	}
	if e.applyCount() != before {
		t.Fatal("GET/导出不得发布运行时配置")
	}
	return exported
}

// TestI9R04EmailBodyRoundTrip 覆盖正式 PUT、运行时、GET、导出/导入和重新打开数据库。
func TestI9R04EmailBodyRoundTrip(t *testing.T) {
	bodies := []struct{ name, text string }{
		{"empty", ""}, {"whitespace", " \t\n"},
		{"multiline", "第一行\n第二行  \n"}, {"default", config.DefaultEmailBody},
	}
	for _, enabled := range []bool{false, true} {
		for _, body := range bodies {
			t.Run(fmt.Sprintf("enabled_%v/%s", enabled, body.name), func(t *testing.T) {
				e := newTestEnv(t)
				data := getAlertsV3(t, e)
				data.Email.Enabled = enabled
				data.Email.Body = body.text
				data.Email.Host = "smtp.example.test"
				data.Email.FromAddr = "from@example.test"
				data.Email.ToAddr = "to@example.test"
				w := e.do(t, http.MethodPut, "/api/alerts", i9r04JSON(t, data))
				if w.Code != http.StatusOK {
					t.Fatalf("PUT 状态码=%d, body=%s", w.Code, w.Body.String())
				}
				checkSaved := func() {
					t.Helper()
					got, err := e.store.GetAlertEmail()
					if err != nil {
						t.Fatal(err)
					}
					if got.Body != body.text || e.snapshot().Config.Email.Body != body.text {
						t.Fatal("数据库或运行时改变了正文")
					}
				}
				checkReopen := func() {
					t.Helper()
					reopened, err := config.OpenStore(e.dbPath)
					if err != nil {
						t.Fatal(err)
					}
					got, readErr := reopened.GetAlertEmail()
					closeErr := reopened.Close()
					if readErr != nil || closeErr != nil {
						t.Fatalf("重开数据库读取=%v, 关闭=%v", readErr, closeErr)
					}
					if got.Body != body.text {
						t.Fatalf("启动迁移改变正文=%q", got.Body)
					}
				}
				checkSaved()
				checkReopen()
				exported := i9r04CheckReads(t, e, body.text)
				if e.applyCount() != 1 {
					t.Fatal("PUT 应只发布一次")
				}
				w = e.do(t, http.MethodPost, "/api/config/import", exported)
				if w.Code != http.StatusOK {
					t.Fatalf("导入状态码=%d, body=%s", w.Code, w.Body.String())
				}
				checkSaved()
				checkReopen()
				i9r04CheckReads(t, e, body.text)
				if e.applyCount() != 2 {
					t.Fatal("导入应只再发布一次")
				}
			})
		}
	}
}

// TestI9R04EmailBodyDefaults 缺行必须在补其他字段之前识别，且不改变 Store 零值合同。
func TestI9R04EmailBodyDefaults(t *testing.T) {
	for _, mode := range []string{"fresh", "absent", "reset"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEnv(t)
			switch mode {
			case "absent":
				e.execRaw(t, "DELETE FROM alert_email")
			case "reset":
				data := getAlertsV3(t, e)
				data.Email.Body = ""
				if w := e.do(t, http.MethodPut, "/api/alerts", i9r04JSON(t, data)); w.Code != http.StatusOK {
					t.Fatalf("PUT 状态码=%d", w.Code)
				}
				if w := e.do(t, http.MethodPost, "/api/config/reset", "{}"); w.Code != http.StatusOK {
					t.Fatalf("reset 状态码=%d", w.Code)
				}
			}
			exported := i9r04CheckReads(t, e, config.DefaultEmailBody)
			if mode == "absent" {
				got, err := e.store.GetAlertEmail()
				if err != nil || *got != (config.AlertEmailConfig{}) {
					t.Fatalf("读取不得改变无行零值合同: cfg=%+v, err=%v", got, err)
				}
			}
			if w := e.do(t, http.MethodPost, "/api/config/import", exported); w.Code != http.StatusOK {
				t.Fatalf("默认配置导出包不能导入: %d", w.Code)
			}
		})
	}
}

// TestI9R04ImportEmptyBody 独立构造空正文导入包，避免导出端替换掩盖导入行为。
func TestI9R04ImportEmptyBody(t *testing.T) {
	e := newTestEnv(t)
	var bundle bundleV4
	if err := json.Unmarshal([]byte(i9r04Export(t, e)), &bundle); err != nil {
		t.Fatal(err)
	}
	bundle.Alerts.Email.Body = ""
	w := e.do(t, http.MethodPost, "/api/config/import", i9r04JSON(t, bundle))
	if w.Code != http.StatusOK {
		t.Fatalf("导入状态码=%d, body=%s", w.Code, w.Body.String())
	}
	got, err := e.store.GetAlertEmail()
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "" || e.snapshot().Config.Email.Body != "" {
		t.Fatal("导入未保留显式空正文")
	}
	i9r04CheckReads(t, e, "")
	if e.applyCount() != 1 {
		t.Fatal("导入应只发布一次")
	}
}

// TestI9R04EmptyBodyMail 空用户正文仍追加系统内容，不隐式插入默认文案。
func TestI9R04EmptyBodyMail(t *testing.T) {
	e := newTestEnv(t)
	addr, rec := startAPIFakeSMTP(t, true)
	host, port, err := splitHostPortForTest(addr)
	if err != nil {
		t.Fatal(err)
	}
	data := getAlertsV3(t, e)
	data.Email.Enabled, data.Email.Body = true, ""
	data.Email.Host, data.Email.Port = host, port
	data.Email.FromAddr, data.Email.ToAddr = "from@example.test", "to@example.test"
	data.Policy.DNSFailedEnabled = true
	if w := e.do(t, http.MethodPut, "/api/alerts", i9r04JSON(t, data)); w.Code != http.StatusOK {
		t.Fatalf("PUT 状态码=%d", w.Code)
	}
	set := BuildAlertSet(e.snapshot().Config)
	if set.email == nil {
		t.Fatal("未构造邮件订阅")
	}
	if err := set.email.OnEvent(notifier.Event{
		Type: notifier.EventDNSFailed, Timestamp: time.Now(),
		Data: map[string]any{"domain": "example.test", "error": "fixture"},
	}); err != nil {
		t.Fatal(err)
	}
	_, body := decodeAPIMail(t, rec.Data())
	if strings.Contains(body, config.DefaultEmailBody) || !strings.HasPrefix(body, "\r\n\r\n事件类型：DNS 解析失败") {
		t.Fatalf("自动邮件未保留空用户正文/系统详情: %q", body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(i9r04JSON(t, data.Email)), &payload); err != nil {
		t.Fatal(err)
	}
	delete(payload, "enabled")
	w := e.do(t, http.MethodPost, "/api/alerts/test-email", i9r04JSON(t, payload))
	var result testEmailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || !result.Success {
		t.Fatalf("测试邮件状态码=%d, body=%s", w.Code, w.Body.String())
	}
	_, body = decodeAPIMail(t, rec.Data())
	if strings.Contains(body, config.DefaultEmailBody) || !strings.HasPrefix(body, "\r\n\r\n"+notifier.EmailTestNotice) {
		t.Fatalf("测试邮件未保留空用户正文/测试说明: %q", body)
	}
	if e.applyCount() != 1 {
		t.Fatal("测试邮件不得发布配置")
	}
}
