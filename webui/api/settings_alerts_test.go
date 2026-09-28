package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// TestGetSettingsReturnsOnlyEditableKeys GET 只返回 11 个可编辑键并补齐默认值
func TestGetSettingsReturnsOnlyEditableKeys(t *testing.T) {
	e := newTestEnv(t)
	for k, v := range map[string]string{"webui_port": "61234", "sync_enabled": "false", "unknown_key": "x"} {
		if err := e.store.SetSetting(k, v); err != nil {
			t.Fatalf("预置残留键失败: %v", err)
		}
	}

	w := e.do(t, http.MethodGet, "/api/settings", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v; body=%s", err, w.Body.String())
	}
	if len(got) != len(settingsEditableKeys) {
		t.Errorf("返回键数 = %d, want %d: %+v", len(got), len(settingsEditableKeys), got)
	}
	for k := range got {
		if !settingsEditableKeys[k] {
			t.Errorf("不应返回非可编辑键 %q", k)
		}
	}
	want := map[string]string{
		"tag": "auto-dns", "interval": "5m", "dns": "223.5.5.5", "dns_timeout": "10s",
		"dns_fail_threshold": "5", "log_level": "info", "theme": "light",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("默认值 %s = %q, want %q", k, got[k], v)
		}
	}
	for _, k := range []string{"tc_access_id", "tc_access_key", "ali_access_id", "ali_access_key"} {
		if got[k] != "" {
			t.Errorf("未配置凭据 %s 应为空字符串，实际 %q", k, got[k])
		}
	}
}

// TestPutSettingsValidationErrors 逐字段非法值 → 400 且零写入零 reload
func TestPutSettingsValidationErrors(t *testing.T) {
	bodies := map[string]string{
		"tag 含方括号":       `{"tag":"[bad]"}`,
		"tag 空白":         `{"tag":"   "}`,
		"tag 49 字符":      `{"tag":"` + strings.Repeat("a", 49) + `"}`,
		"interval 不可解析":  `{"interval":"abc"}`,
		"interval 零值":    `{"interval":"0s"}`,
		"interval 负值":    `{"interval":"-5m"}`,
		"dns 端口越界":       `{"dns":"8.8.8.8:0"}`,
		"dns 未加括号 IPv6":  `{"dns":"2001:db8::1"}`,
		"dns 空白":         `{"dns":"  "}`,
		"dns_timeout 负值": `{"dns_timeout":"-1s"}`,
		"threshold 零":    `{"dns_fail_threshold":"0"}`,
		"threshold 非整数":  `{"dns_fail_threshold":"x"}`,
		"log_level 未知":   `{"log_level":"trace"}`,
		"theme 未知":       `{"theme":"blue"}`,
		"空对象":            `{}`,
		"未知字段":           `{"extra":"x"}`,
		"webui_port":     `{"webui_port":"61234"}`,
		"sync_enabled":   `{"sync_enabled":"false"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			w := e.do(t, http.MethodPut, "/api/settings", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if settings, err := e.store.GetSettings(); err != nil || len(settings) != 0 {
				t.Errorf("非法输入不应写库: %v %v", settings, err)
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法输入不应触发运行时更新，实际 %d", got)
			}
		})
	}
}

// TestPutSettingsTransactionRollback 多键 settings 中途失败必须完整回滚且不 reload
func TestPutSettingsTransactionRollback(t *testing.T) {
	e := newTestEnv(t)
	e.execRaw(t, `CREATE TRIGGER fail_settings BEFORE INSERT ON settings BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)

	w := e.do(t, http.MethodPut, "/api/settings", `{"tag":"new-tag","interval":"7m","dns":"1.1.1.1"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	settings, err := e.store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("部分写入必须完整回滚: %+v", settings)
	}
	if got := e.applyCount(); got != 0 {
		t.Errorf("失败事务不应触发运行时更新，实际 %d", got)
	}
}

// TestPutSettingsSuccessOneReload 合法多键更新只触发一次运行时更新
func TestPutSettingsSuccessOneReload(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPut, "/api/settings", `{"tag":"my-tag","interval":"7m","dns_timeout":"3s"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("运行时更新次数 = %d, want 1", got)
	}
	settings, _ := e.store.GetSettings()
	if settings["tag"] != "my-tag" || settings["interval"] != "7m" || settings["dns_timeout"] != "3s" {
		t.Errorf("设置未正确保存: %+v", settings)
	}
}

// TestGetAlertsDefaultsOnEmptyDB 空库时补齐 port/channel 默认值
func TestGetAlertsDefaultsOnEmptyDB(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodGet, "/api/alerts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	var got alertsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析失败: %v; body=%s", err, w.Body.String())
	}
	if got.Email == nil || got.Webhook == nil {
		t.Fatalf("两个对象都必须返回: %s", w.Body.String())
	}
	if got.Email.Port != "587" {
		t.Errorf("邮件端口默认值 = %q, want 587", got.Email.Port)
	}
	if got.Webhook.Channel != "dingtalk" {
		t.Errorf("Webhook 渠道默认值 = %q, want dingtalk", got.Webhook.Channel)
	}
}

// TestPutAlertsRequiresAllFields 四个对象与全部子字段必需（Build7 §4.6）
func TestPutAlertsRequiresAllFields(t *testing.T) {
	email := `{"enabled":false,"host":"h","port":"587","username":"u","password":"p","from_addr":"f","to_addr":"t","subject":"s","body":"b"}`
	webhook := `{"enabled":false,"url":"","channel":"dingtalk"}`
	policy := `{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"10m"}`
	push := `{"enabled":false,"url":"","interval":"60s"}`

	valid := `{"policy":` + policy + `,"email":` + email + `,"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`
	cases := map[string]string{
		"缺 policy":                `{"email":` + email + `,"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`,
		"缺 email":                 `{"policy":` + policy + `,"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`,
		"缺 webhook":               `{"policy":` + policy + `,"email":` + email + `,"uptime_kuma_push":` + push + `}`,
		"缺 uptime_kuma_push":      `{"policy":` + policy + `,"email":` + email + `,"webhook":` + webhook + `}`,
		"policy 为 null":           `{"policy":null,"email":` + email + `,"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`,
		"email 为 null":            `{"policy":` + policy + `,"email":null,"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`,
		"webhook 为 null":          `{"policy":` + policy + `,"email":` + email + `,"webhook":null,"uptime_kuma_push":` + push + `}`,
		"push 为 null":             `{"policy":` + policy + `,"email":` + email + `,"webhook":` + webhook + `,"uptime_kuma_push":null}`,
		"缺 email.port":            `{"policy":` + policy + `,"email":{"enabled":false,"host":"h","username":"u","password":"p","from_addr":"f","to_addr":"t","subject":"s","body":"b"},"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`,
		"缺 email.subject":         `{"policy":` + policy + `,"email":{"enabled":false,"host":"h","port":"587","username":"u","password":"p","from_addr":"f","to_addr":"t","body":"b"},"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`,
		"缺 email.body":            `{"policy":` + policy + `,"email":{"enabled":false,"host":"h","port":"587","username":"u","password":"p","from_addr":"f","to_addr":"t","subject":"s"},"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`,
		"缺 webhook.channel":       `{"policy":` + policy + `,"email":` + email + `,"webhook":{"enabled":false,"url":""},"uptime_kuma_push":` + push + `}`,
		"缺 policy.health_timeout": `{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false},"email":` + email + `,"webhook":` + webhook + `,"uptime_kuma_push":` + push + `}`,
		"缺 push.interval":         `{"policy":` + policy + `,"email":` + email + `,"webhook":` + webhook + `,"uptime_kuma_push":{"enabled":false,"url":""}}`,
		"空对象":                     `{}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			w := e.do(t, http.MethodPut, "/api/alerts", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			// 新库已存在默认行（port=587/默认主题正文），因此用「未被写入的字段」判定零写入
			if email, _ := e.store.GetAlertEmail(); email.Host != "" || email.Password != "" || email.Enabled {
				t.Errorf("非法输入不应写库: %+v", email)
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法输入不应触发运行时更新，实际 %d", got)
			}
		})
	}

	// 合法请求作为对照
	e := newTestEnv(t)
	if w := e.do(t, http.MethodPut, "/api/alerts", valid); w.Code != http.StatusOK {
		t.Fatalf("合法请求应通过: %d %s", w.Code, w.Body.String())
	}
}

// TestPutAlertsStrictDecoding 四对象请求体仍走严格解码：未知字段、尾随 JSON、多个顶层值都拒绝
func TestPutAlertsStrictDecoding(t *testing.T) {
	valid := alertsBody("h", "p", "", "dingtalk")
	cases := map[string]string{
		"未知字段":    strings.TrimSuffix(valid, "}") + `,"extra":1}`,
		"尾随 JSON": valid + `{"x":1}`,
		"多个顶层值":   valid + ` 1`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			if w := e.do(t, http.MethodPut, "/api/alerts", body); w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法输入不应触发运行时更新，实际 %d", got)
			}
		})
	}
}

// TestPutAlertsValidation 端口/渠道/URL/主题/时长校验（Build7 §4.5）
func TestPutAlertsValidation(t *testing.T) {
	email := func(over string) string { return over }
	webhook := `{"enabled":false,"url":"","channel":"dingtalk"}`
	policy := `{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"10m"}`
	push := `{"enabled":false,"url":"","interval":"60s"}`
	wrap := func(e, w, p, k string) string {
		return `{"policy":` + p + `,"email":` + e + `,"webhook":` + w + `,"uptime_kuma_push":` + k + `}`
	}
	baseEmail := `{"enabled":false,"host":"h","port":"587","username":"u","password":"p","from_addr":"f","to_addr":"t","subject":"s","body":"b"}`

	cases := map[string]string{
		"端口为 0":      wrap(email(`{"enabled":false,"host":"h","port":"0","username":"u","password":"p","from_addr":"f","to_addr":"t","subject":"s","body":"b"}`), webhook, policy, push),
		"端口非整数":      wrap(email(`{"enabled":false,"host":"h","port":"abc","username":"u","password":"p","from_addr":"f","to_addr":"t","subject":"s","body":"b"}`), webhook, policy, push),
		"启用但 host 空": wrap(email(`{"enabled":true,"host":"","port":"587","username":"u","password":"p","from_addr":"f","to_addr":"t","subject":"s","body":"b"}`), webhook, policy, push),
		"启用但 from 空": wrap(email(`{"enabled":true,"host":"h","port":"587","username":"u","password":"p","from_addr":"","to_addr":"t","subject":"s","body":"b"}`), webhook, policy, push),
		"启用但 to 空":   wrap(email(`{"enabled":true,"host":"h","port":"587","username":"u","password":"p","from_addr":"f","to_addr":"","subject":"s","body":"b"}`), webhook, policy, push),
		"主题为空":       wrap(email(`{"enabled":false,"host":"h","port":"587","username":"u","password":"p","from_addr":"f","to_addr":"t","subject":"  ","body":"b"}`), webhook, policy, push),
		"渠道未知":       wrap(baseEmail, `{"enabled":false,"url":"","channel":"wecom"}`, policy, push),
		"渠道为空":       wrap(baseEmail, `{"enabled":false,"url":"","channel":""}`, policy, push),
		"启用但 URL 为空": wrap(baseEmail, `{"enabled":true,"url":"","channel":"dingtalk"}`, policy, push),
		"启用但非 http":  wrap(baseEmail, `{"enabled":true,"url":"ftp://example.com","channel":"dingtalk"}`, policy, push),
		"启用但无 host":  wrap(baseEmail, `{"enabled":true,"url":"https://","channel":"dingtalk"}`, policy, push),
		"health_timeout 为 0": wrap(baseEmail, webhook,
			`{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"0s"}`, push),
		"health_timeout 非法": wrap(baseEmail, webhook,
			`{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"abc"}`, push),
		"push 间隔过小":      wrap(baseEmail, webhook, policy, `{"enabled":false,"url":"","interval":"19s"}`),
		"push 启用但 URL 空": wrap(baseEmail, webhook, policy, `{"enabled":true,"url":"","interval":"60s"}`),
		"push 启用但非 http": wrap(baseEmail, webhook, policy, `{"enabled":true,"url":"ftp://kuma/x","interval":"60s"}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			if w := e.do(t, http.MethodPut, "/api/alerts", body); w.Code != http.StatusBadRequest {
				t.Errorf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法输入不应触发运行时更新，实际 %d", got)
			}
		})
	}
}

// TestPutAlertsRollbackKeepsBothOld 邮件与 Webhook 必须同事务：任一阶段失败完整回滚
func TestPutAlertsRollbackKeepsBothOld(t *testing.T) {
	e := newTestEnv(t)
	if w := e.do(t, http.MethodPut, "/api/alerts", alertsBody("old.example.com", "old-pass", "https://old.example.com/hook", "dingtalk")); w.Code != http.StatusOK {
		t.Fatalf("预置旧告警失败: %d %s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Fatalf("预置应触发一次运行时更新，实际 %d", got)
	}

	// 制造 webhook 写入失败：email 的写入必须随之回滚
	e.execRaw(t, `CREATE TRIGGER fail_webhook BEFORE INSERT ON alert_webhook BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)
	w := e.do(t, http.MethodPut, "/api/alerts", alertsBody("new.example.com", "new-pass", "https://new.example.com/hook", "feishu"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}

	email, err := e.store.GetAlertEmail()
	if err != nil {
		t.Fatalf("GetAlertEmail 失败: %v", err)
	}
	if email.Host != "old.example.com" || email.Password != "old-pass" {
		t.Errorf("邮件告警必须随 webhook 失败一起回滚: %+v", email)
	}
	webhook, err := e.store.GetAlertWebhook()
	if err != nil {
		t.Fatalf("GetAlertWebhook 失败: %v", err)
	}
	if webhook.URL != "https://old.example.com/hook" || webhook.Channel != "dingtalk" {
		t.Errorf("Webhook 告警不应被部分更新: %+v", webhook)
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("失败事务不应触发运行时更新，实际 %d", got)
	}
}

// TestPutAlertsSuccessOneReload 合法的双告警保存只触发一次运行时更新
func TestPutAlertsSuccessOneReload(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPut, "/api/alerts", alertsBody("smtp.example.com", "pw", "https://example.com/hook", "feishu"))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("运行时更新次数 = %d, want 1", got)
	}
	webhook, _ := e.store.GetAlertWebhook()
	if webhook.Channel != "feishu" {
		t.Errorf("渠道未保存: %+v", webhook)
	}
}

// TestConfigResetStrictBody 只接受单一空对象，且清空全部业务表并一次 reload
func TestConfigResetStrictBody(t *testing.T) {
	// null 尤其关键：encoding/json 把 null 解码到结构体不报错，必须被显式拒绝（Issue6 A10）
	for _, body := range []string{
		`{"unknown":1}`, `{"webui_port":"1"}`, ``,
		`null`, `  null  `, `[]`, `[{}]`, `1`, `"str"`, `true`,
	} {
		e := newTestEnv(t)
		e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1")
		w := e.do(t, http.MethodPost, "/api/config/reset", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%q 状态码 = %d, want 400", body, w.Code)
		}
		if targets, _ := e.store.GetTargets(); len(targets) != 1 {
			t.Errorf("body=%q 被拒绝后数据应保留", body)
		}
		if got := e.applyCount(); got != 0 {
			t.Errorf("body=%q 被拒绝不应触发运行时更新，实际 %d", body, got)
		}
	}

	e := newTestEnv(t)
	e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1")
	e.seedRule(t, config.DomainRule{Host: "a.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT"})
	if err := e.store.SetSetting("tag", "x"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	if err := e.store.SaveAlertEmail(&config.AlertEmailConfig{Enabled: true, Host: "h", Port: "587"}); err != nil {
		t.Fatalf("预置邮件告警失败: %v", err)
	}
	if err := e.store.SaveAlertWebhook(&config.AlertWebhookConfig{Enabled: true, URL: "https://example.com/h"}); err != nil {
		t.Fatalf("预置 Webhook 失败: %v", err)
	}
	if err := e.store.AddSyncLog(config.SyncLog{Timestamp: time.Now(), Result: "success"}); err != nil {
		t.Fatalf("预置同步日志失败: %v", err)
	}
	if err := e.store.ReplaceScannedResources("tc_lighthouse", "ap-guangzhou", []config.ScannedResource{{ResourceID: "r"}}); err != nil {
		t.Fatalf("预置扫描结果失败: %v", err)
	}

	w := e.do(t, http.MethodPost, "/api/config/reset", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("清空成功应触发一次运行时更新，实际 %d", got)
	}
	if targets, _ := e.store.GetTargets(); len(targets) != 0 {
		t.Errorf("目标未清空: %+v", targets)
	}
	if rules, _ := e.store.GetRules(); len(rules) != 0 {
		t.Errorf("规则未清空: %+v", rules)
	}
	if settings, _ := e.store.GetSettings(); len(settings) != 0 {
		t.Errorf("设置未清空: %+v", settings)
	}
	if logs, _ := e.store.GetSyncLogs(10); len(logs) != 0 {
		t.Errorf("同步日志未清空: %+v", logs)
	}
	if email, _ := e.store.GetAlertEmail(); email.Enabled || email.Host != "" {
		t.Errorf("邮件告警未清空: %+v", email)
	}
	if webhook, _ := e.store.GetAlertWebhook(); webhook.Enabled || webhook.URL != "" {
		t.Errorf("Webhook 未清空: %+v", webhook)
	}
	if scanned, _ := e.store.GetScannedResources("tc_lighthouse"); len(scanned) != 0 {
		t.Errorf("扫描结果未清空: %+v", scanned)
	}
}

// TestPauseResumeThroughCoordinator pause/resume 先提交 DB，再经协调器发布一次运行时状态
func TestPauseResumeThroughCoordinator(t *testing.T) {
	e := newTestEnv(t)
	e.deps.Syncer = &stubSyncer{enabled: true, runtime: e.runtime}
	if w := e.do(t, http.MethodPost, "/api/sync/pause", ""); w.Code != http.StatusOK {
		t.Fatalf("pause 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if settings, _ := e.store.GetSettings(); settings["sync_enabled"] != "false" {
		t.Errorf("sync_enabled 应写为 false: %+v", settings)
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("pause 应触发一次运行时更新，实际 %d", got)
	}

	if w := e.do(t, http.MethodPost, "/api/sync/resume", ""); w.Code != http.StatusOK {
		t.Fatalf("resume 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if settings, _ := e.store.GetSettings(); settings["sync_enabled"] != "true" {
		t.Errorf("sync_enabled 应写为 true: %+v", settings)
	}
	if got := e.applyCount(); got != 2 {
		t.Errorf("resume 后累计运行时更新 = %d, want 2", got)
	}
}

// TestScanResourcesValidation 扫描请求复用 cloud_type/region 基础校验
func TestScanResourcesValidation(t *testing.T) {
	cases := map[string]string{
		"未知 cloud_type": `{"cloud_type":"aws_ec2","region":"gz"}`,
		"region 空白":     `{"cloud_type":"tc_cvm","region":"  "}`,
		"提交内部字段":        `{"cloud_type":"tc_cvm","region":"gz","id":1}`,
		"未知字段":          `{"cloud_type":"tc_cvm","region":"gz","extra":1}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			if w := e.do(t, http.MethodPost, "/api/scan-resources", body); w.Code != http.StatusBadRequest {
				t.Errorf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestTestConnectionValidation 连接测试复用目标基础校验，且缺凭据时不暴露 SDK 报错
func TestTestConnectionValidation(t *testing.T) {
	e := newTestEnv(t)
	resetEnvRuntimeForTest(t, e, config.RuntimeConfig{Tag: "auto-dns", Interval: time.Minute, DNS: "1.1.1.1"})

	if w := e.do(t, http.MethodPost, "/api/test-connection", `{"cloud_type":"aws","region":"gz","resource_id":"x"}`); w.Code != http.StatusBadRequest {
		t.Errorf("未知 cloud_type 状态码 = %d, want 400", w.Code)
	}
	w := e.do(t, http.MethodPost, "/api/test-connection", `{"cloud_type":"tc_lighthouse","region":"gz","resource_id":"x"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "凭据未配置") {
		t.Errorf("缺凭据应返回安全提示: %d %s", w.Code, w.Body.String())
	}
}

// TestTestConnectionUsesRuntimeSnapshotCredentials 连接测试只使用运行时快照的凭据：
// 数据库里的凭据不生效（不再零散读 Store），且不覆盖同步凭据。
func TestTestConnectionUsesRuntimeSnapshotCredentials(t *testing.T) {
	e := newTestEnv(t)
	// 数据库里有凭据，但运行时快照的凭据为空 → 必须按「未配置」快速失败
	if err := e.store.SetSetting("tc_access_id", "AKID-from-db"); err != nil {
		t.Fatalf("预置凭据失败: %v", err)
	}
	resetEnvRuntimeForTest(t, e, config.RuntimeConfig{Tag: "auto-dns", Interval: time.Minute, DNS: "1.1.1.1"})

	w := e.do(t, http.MethodPost, "/api/test-connection", `{"cloud_type":"tc_lighthouse","region":"gz","resource_id":"x"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "凭据未配置") {
		t.Errorf("连接测试必须只用运行时快照凭据: %d %s", w.Code, w.Body.String())
	}
}

// TestScanUsesRuntimeSnapshotCredentials 资源扫描同样只使用运行时快照（不读 Store、不写全局凭据）。
func TestScanUsesRuntimeSnapshotCredentials(t *testing.T) {
	e := newTestEnv(t)
	if err := e.store.SetSetting("ali_access_id", "LTAI-from-db"); err != nil {
		t.Fatalf("预置凭据失败: %v", err)
	}
	if err := e.store.SetSetting("ali_access_key", "aks-from-db"); err != nil {
		t.Fatalf("预置凭据失败: %v", err)
	}
	resetEnvRuntimeForTest(t, e, config.RuntimeConfig{Tag: "auto-dns", Interval: time.Minute, DNS: "1.1.1.1"})

	w := e.do(t, http.MethodPost, "/api/scan-resources", `{"cloud_type":"ali_ecs","region":"cn-hangzhou"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "凭据未配置") {
		t.Errorf("资源扫描必须只用运行时快照凭据: %d %s", w.Code, w.Body.String())
	}
}
