package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// 三个写入入口严格拒绝模式错误；完整业务快照及已发布对象均保持。
func TestSMTPSecurityStrictEntrypoints(t *testing.T) {
	for _, mode := range []string{"missing", "null", "", "STARTTLS", " starttls ", "mode-secret"} {
		for _, endpoint := range []string{"/api/alerts", "/api/alerts/test-email", "/api/config/import"} {
			t.Run(endpoint+"/"+mode, func(t *testing.T) {
				e, unchanged := i8111Guard(t)
				method, body := http.MethodPost, validBundle()
				if endpoint == "/api/alerts" {
					method, body = http.MethodPut, alertsBody("smtp.invalid", "secret", "", "dingtalk")
				}
				if endpoint == "/api/alerts/test-email" {
					body = testEmailBody(t, "127.0.0.1:1", "secret", "subject", "body")
				}
				replacement := ""
				if mode != "missing" {
					value, err := json.Marshal(mode)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "null" {
						value = []byte("null")
					}
					replacement = `"security":` + string(value) + `,`
				}
				body = strings.Replace(body, `"security":"auto_starttls",`, replacement, 1)
				w := e.do(t, method, endpoint, body)
				if w.Code != http.StatusBadRequest {
					t.Fatalf("未拒绝模式: %d %s", w.Code, w.Body.String())
				}
				if endpoint == "/api/config/import" && (mode == "missing" || mode == "null") && !strings.Contains(w.Body.String(), "alerts.email.security") {
					t.Fatal("缺少字段路径诊断")
				}
				if strings.Contains(w.Body.String(), "mode-secret") {
					t.Fatal("非法模式原值泄露")
				}
				unchanged(t)
			})
		}
	}
}

func TestSMTPSecuritySavePublishAndBundle(t *testing.T) {
	for _, mode := range []string{"auto_starttls", "starttls", "implicit_tls"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEnv(t)
			body := strings.Replace(alertsBody("smtp.invalid", "secret", "", "dingtalk"), `"security":"auto_starttls"`, `"security":"`+mode+`"`, 1)
			w := e.do(t, http.MethodPut, "/api/alerts", body)
			if w.Code != 200 {
				t.Fatalf("保存失败: %s", w.Body.String())
			}
			cfg, err := e.store.GetAlertEmail()
			if err != nil || cfg.Security != mode {
				t.Fatalf("持久化模式丢失: %+v %v", cfg, err)
			}
			if getAlertsV3(t, e).Email.Security != mode || e.snapshot().Config.Email.Security != mode {
				t.Fatal("GET/运行时丢失模式")
			}
			exported := e.do(t, http.MethodPost, "/api/config/export", `{}`)
			var bundle struct {
				Alerts struct {
					Email struct {
						Security string `json:"security"`
					} `json:"email"`
				} `json:"alerts"`
			}
			if err := json.Unmarshal(exported.Body.Bytes(), &bundle); err != nil {
				t.Fatal(err)
			}
			if exported.Code != 200 || bundle.Alerts.Email.Security != mode {
				t.Fatalf("导出丢失模式: %s", exported.Body.String())
			}
			imported := e.do(t, http.MethodPost, "/api/config/import", exported.Body.String())
			if imported.Code != 200 || getAlertsV3(t, e).Email.Security != mode || e.snapshot().Config.Email.Security != mode {
				t.Fatalf("往返丢失模式: %s", imported.Body.String())
			}
		})
	}
}

// 服务器只提供明文SMTP：旧模式成功，强制模式不得进入DATA。
// 保存后运行时/新订阅必须使用新模式；旧订阅持有旧配置。
func TestSMTPSecurityHotReloadNotifier(t *testing.T) {
	e := newTestEnv(t)
	addr, record := startAPIFakeSMTP(t, true)
	base := testEmailBody(t, addr, "secret", "subject", "body")
	var email map[string]any
	if err := json.Unmarshal([]byte(base), &email); err != nil {
		t.Fatal(err)
	}
	email["enabled"] = true
	save := func(mode string) {
		email["security"] = mode
		payload := map[string]any{"email": email, "policy": map[string]any{"dns_failed_enabled": true, "sync_error_enabled": true, "operational_error_enabled": true, "health_timeout": "10m"}, "webhook": config.AlertWebhookConfig{}, "uptime_kuma_push": map[string]any{"enabled": false, "url": "", "interval": "60s"}}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if w := e.do(t, http.MethodPut, "/api/alerts", string(data)); w.Code != 200 {
			t.Fatalf("保存失败: %s", w.Body.String())
		}
	}
	save("auto_starttls")
	old := e.alerts.Current().email
	save("starttls")
	current := e.alerts.Current().email
	if old == current {
		t.Fatal("保存未替换通知器")
	}
	event := notifier.Event{Type: notifier.EventSyncError, Timestamp: time.Now()}
	if err := current.OnEvent(event); err == nil || !strings.Contains(err.Error(), "STARTTLS 失败") {
		t.Fatalf("新通知器漏传模式: %v", err)
	}
	if record.Data() != "" {
		t.Fatal("拒绝后仍发送DATA")
	}
	if err := old.OnEvent(event); err != nil {
		t.Fatalf("旧通知器配置被修改: %v", err)
	}
	if record.Data() == "" {
		t.Fatal("旧兼容模式未完成发送")
	}
}

func TestSMTPSecurityTestEmailUsesUnsavedMode(t *testing.T) {
	e, unchanged := i8111Guard(t)
	addr, record := startAPIFakeSMTP(t, true)
	body := strings.Replace(testEmailBody(t, addr, "secret", "subject", "body"), `"security":"auto_starttls"`, `"security":"starttls"`, 1)
	w := e.do(t, http.MethodPost, "/api/alerts/test-email", body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"success":false`) || !strings.Contains(w.Body.String(), "STARTTLS 失败") {
		t.Fatalf("未使用表单模式: %s", w.Body.String())
	}
	if record.Data() != "" {
		t.Fatal("未加密发送DATA")
	}
	unchanged(t)
}

// 随机端口也必须按表单/已发布模式先发送 TLS ClientHello，不能根据端口猜测。
func TestSMTPSecurityImplicitEntrypoints(t *testing.T) {
	for _, entry := range []string{"test-email", "automatic"} {
		t.Run(entry, func(t *testing.T) {
			e, unchanged := i8111Guard(t)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			observed := make(chan byte, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer func() {
					if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
						t.Error(err)
					}
				}()
				if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
					t.Error(err)
					return
				}
				var first [1]byte
				if _, err := io.ReadFull(conn, first[:]); err != nil {
					t.Error("未收到 TLS ClientHello")
					return
				}
				observed <- first[0]
			}()
			t.Cleanup(func() {
				if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
					t.Error(err)
				}
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("服务器未退出")
				}
			})
			host, port, err := net.SplitHostPort(ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			if entry == "test-email" {
				body := strings.Replace(testEmailBody(t, ln.Addr().String(), "secret", "subject", "body"), `"security":"auto_starttls"`, `"security":"implicit_tls"`, 1)
				w := e.do(t, http.MethodPost, "/api/alerts/test-email", body)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "SMTP TLS 握手失败") {
					t.Fatalf("测试入口未传递隐式模式: %s", w.Body.String())
				}
				unchanged(t)
			} else {
				rc := alertConfig(true, false)
				rc.Email.Host, rc.Email.Port, rc.Email.Security = host, port, "implicit_tls"
				subscriber := BuildAlertSet(rc).email
				err := subscriber.OnEvent(notifier.Event{Type: notifier.EventSyncError, Timestamp: time.Now()})
				if err == nil || !strings.Contains(err.Error(), "SMTP TLS 握手失败") {
					t.Fatalf("自动入口未传递隐式模式: %v", err)
				}
			}
			select {
			case first := <-observed:
				if first != 0x16 {
					t.Fatalf("首字节不是TLS握手: %x", first)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("无TLS握手证据")
			}
		})
	}
}

func TestSMTPSecurityDefaultExportWithoutRow(t *testing.T) {
	e := newTestEnv(t)
	e.execRaw(t, "DELETE FROM alert_email")
	exported := e.do(t, http.MethodPost, "/api/config/export", `{}`)
	var bundle struct {
		Alerts struct {
			Email config.AlertEmailConfig `json:"email"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(exported.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if exported.Code != 200 || bundle.Alerts.Email.Security != config.DefaultSMTPSecurity || bundle.Alerts.Email.Port != "587" {
		t.Fatalf("无行导出默认值异常: %s", exported.Body.String())
	}
	if imported := e.do(t, http.MethodPost, "/api/config/import", exported.Body.String()); imported.Code != 200 {
		t.Fatalf("默认导出不能导入: %s", imported.Body.String())
	}
}
