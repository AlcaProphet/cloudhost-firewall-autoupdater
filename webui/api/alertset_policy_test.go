package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// ─── Build7 Step 3：三个触发开关是邮件/Webhook 共用的全局策略 ───

// policyRuntime 构造带指定渠道与触发开关的运行时配置（SMTP/Webhook 指向本地假 SMTP）。
func policyRuntime(t *testing.T, emailOn, webhookOn bool, policy config.AlertPolicyConfig, smtpAddr string) config.RuntimeConfig {
	t.Helper()
	host, port, err := splitHostPortForTest(smtpAddr)
	if err != nil {
		t.Fatalf("解析假 SMTP 地址失败: %v", err)
	}
	rc := config.RuntimeConfig{
		Credentials: config.Credentials{}, Tag: "auto-dns", Interval: 5 * time.Minute,
		DNS: "223.5.5.5", DNSTimeout: 10 * time.Second, DNSFailThreshold: 5,
		LogLevel: "info", SyncEnabled: true, Theme: "light",
		Policy: policy,
		Email: config.AlertEmailConfig{Security: "auto_starttls",
			Enabled: emailOn, Host: host, Port: port, Username: "", Password: "",
			FromAddr: "f@example.com", ToAddr: "t@example.com",
			Subject: "[FWAlizer] 告警通知", Body: "FWAlizer 检测到运行异常，请检查同步日志。",
		},
		Webhook: config.AlertWebhookConfig{Enabled: webhookOn, URL: "http://127.0.0.1:1/hook", Channel: "dingtalk"},
	}
	return rc
}

// TestBuildAlertSetSubscriptionMatrix 只有「渠道开启 + 对应触发开启」才产生订阅。
//
// 红灯：修复前 BuildAlertSet 只看渠道开关，三个触发开关完全不影响订阅。
func TestBuildAlertSetSubscriptionMatrix(t *testing.T) {
	allOff := config.DefaultAlertPolicy()
	dnsOn := config.DefaultAlertPolicy()
	dnsOn.DNSFailedEnabled = true
	syncOn := config.DefaultAlertPolicy()
	syncOn.SyncErrorEnabled = true

	cases := []struct {
		name      string
		emailOn   bool
		webhookOn bool
		policy    config.AlertPolicyConfig
		wantEmail bool
		wantHook  bool
	}{
		{"全部关闭", false, false, allOff, false, false},
		{"仅邮件渠道开启但无触发", true, false, allOff, false, false},
		{"仅 Webhook 渠道开启但无触发", false, true, allOff, false, false},
		{"渠道开启且 DNS 触发开启", true, true, dnsOn, true, true},
		{"渠道开启且同步触发开启", true, true, syncOn, true, true},
		{"渠道关闭但触发开启", false, false, dnsOn, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := policyRuntime(t, tc.emailOn, tc.webhookOn, tc.policy, "127.0.0.1:1")
			set := BuildAlertSet(rc)
			if (set.email != nil) != tc.wantEmail {
				t.Errorf("email 订阅 = %v, want %v", set.email != nil, tc.wantEmail)
			}
			if (set.webhook != nil) != tc.wantHook {
				t.Errorf("webhook 订阅 = %v, want %v", set.webhook != nil, tc.wantHook)
			}
		})
	}
}

// TestAlertManagerPolicyControlsActualDelivery 真实总线行为：
// 未开启的触发条件不得产生任何 SMTP 连接；开启的触发条件必须投递。
func TestAlertManagerPolicyControlsActualDelivery(t *testing.T) {
	addr, rec := startAPIFakeSMTP(t, true)

	// 只开启 DNS 触发：同步失败事件不得发送
	dnsOnly := config.DefaultAlertPolicy()
	dnsOnly.DNSFailedEnabled = true
	bus := notifier.NewEventBus()
	mgr := NewAlertManager(bus)
	mgr.Apply(BuildAlertSet(policyRuntime(t, true, false, dnsOnly, addr)))

	bus.Publish(notifier.Event{
		Type: notifier.EventSyncError, Timestamp: time.Now(),
		Data: map[string]any{"provider": "tc_lighthouse(lhins-1)", "domain": "a.example.com", "error": "boom"},
	})
	waitForNoSMTPData(t, rec, 300*time.Millisecond)
	if data := rec.Data(); data != "" {
		t.Errorf("未开启的同步触发不得发送邮件: %q", data)
	}

	// 开启的 DNS 触发必须发送
	bus.Publish(notifier.Event{
		Type: notifier.EventDNSFailed, Timestamp: time.Now(),
		Data: map[string]any{"domain": "b.example.com", "error": "dns boom"},
	})
	waitForSMTPData(t, rec, 5*time.Second)
	subject, payload := decodeAPIMail(t, rec.Data())
	if !strings.Contains(payload, "b.example.com") || !strings.Contains(payload, "dns boom") {
		t.Errorf("DNS 触发邮件缺少事件详情: %q", payload)
	}
	if subject != "[FWAlizer] 告警通知 - DNS 解析失败" {
		t.Errorf("DNS 触发邮件主题缺少固定后缀: %q", subject)
	}
}

// TestGetAlertsExposesPolicySwitches 页面必须能读到三个触发开关与 health_timeout。
func TestGetAlertsExposesPolicySwitches(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodGet, "/api/alerts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, fragment := range []string{`"dns_failed_enabled"`, `"sync_error_enabled"`, `"operational_error_enabled"`} {
		if !strings.Contains(body, fragment) {
			t.Errorf("GET /api/alerts 缺少触发开关 %s: %s", fragment, body)
		}
	}
}

// TestAlertManagerPolicyHotReloadSwitchesSubscriptions 策略热重载必须
// 「按旧集合取消订阅、按新集合安装订阅」：切换后只有新开启的触发条件生效。
func TestAlertManagerPolicyHotReloadSwitchesSubscriptions(t *testing.T) {
	addr, rec := startAPIFakeSMTP(t, true)
	bus := notifier.NewEventBus()
	mgr := NewAlertManager(bus)

	dnsOnly := config.DefaultAlertPolicy()
	dnsOnly.DNSFailedEnabled = true
	mgr.Apply(BuildAlertSet(policyRuntime(t, true, false, dnsOnly, addr)))

	// 先确认 DNS 触发在旧策略下可用（同时占用假 SMTP 记录）
	bus.Publish(notifier.Event{Type: notifier.EventDNSFailed, Timestamp: time.Now(), Data: map[string]any{"domain": "old.example.com"}})
	waitForSMTPData(t, rec, 5*time.Second)

	// 切换到只开启同步触发
	syncOnly := config.DefaultAlertPolicy()
	syncOnly.SyncErrorEnabled = true
	mgr.Apply(BuildAlertSet(policyRuntime(t, true, false, syncOnly, addr)))

	// 复位记录：DNS 触发必须已取消订阅（不再发送）
	rec.setData("")
	bus.Publish(notifier.Event{Type: notifier.EventDNSFailed, Timestamp: time.Now(), Data: map[string]any{"domain": "after.example.com"}})
	waitForNoSMTPData(t, rec, 300*time.Millisecond)
	if data := rec.Data(); data != "" {
		t.Errorf("热重载后旧触发条件仍会发送: %q", data)
	}

	// 新开启的同步触发必须发送
	bus.Publish(notifier.Event{
		Type: notifier.EventSyncError, Timestamp: time.Now(),
		Data: map[string]any{"provider": "tc_lighthouse(lhins-9)", "domain": "new.example.com", "error": "boom"},
	})
	waitForSMTPData(t, rec, 5*time.Second)
	if subject, data := decodeAPIMail(t, rec.Data()); !strings.Contains(data, "new.example.com") || subject != "[FWAlizer] 告警通知 - 同步失败" {
		t.Errorf("热重载后新触发条件未生效: %q", data)
	}
}

// TestOperationalTriggerSubscribesAndDelivers 第三触发条件（运行健康异常）：
// 开启后才订阅事件，并能通过邮件渠道投递（固定后缀与展示名）。
func TestOperationalTriggerSubscribesAndDelivers(t *testing.T) {
	addr, rec := startAPIFakeSMTP(t, true)

	// 未开启第三触发：事件不投递
	off := config.DefaultAlertPolicy()
	bus := notifier.NewEventBus()
	mgr := NewAlertManager(bus)
	mgr.Apply(BuildAlertSet(policyRuntime(t, true, false, off, addr)))
	bus.Publish(notifier.Event{
		Type: notifier.EventOperationalUnhealthy, Timestamp: time.Now(),
		Data: map[string]any{"reasons": []string{"同步引擎未运行"}},
	})
	waitForNoSMTPData(t, rec, 300*time.Millisecond)
	if data := rec.Data(); data != "" {
		t.Errorf("未开启第三触发时不得发送: %q", data)
	}

	// 开启第三触发：事件必须投递，且主题/详情使用固定文案
	on := config.DefaultAlertPolicy()
	on.OperationalErrorEnabled = true
	mgr.Apply(BuildAlertSet(policyRuntime(t, true, false, on, addr)))
	bus.Publish(notifier.Event{
		Type: notifier.EventOperationalUnhealthy, Timestamp: time.Now(),
		Data: map[string]any{"reasons": []string{"同步引擎未运行"}},
	})
	waitForSMTPData(t, rec, 5*time.Second)
	subject, payload := decodeAPIMail(t, rec.Data())
	if subject != "[FWAlizer] 告警通知 - 运行健康异常" {
		t.Errorf("运行健康异常邮件主题缺少固定后缀: %q", subject)
	}
	if !strings.Contains(payload, "事件类型：运行健康异常") {
		t.Errorf("运行健康异常邮件详情缺少展示名: %q", payload)
	}
}
