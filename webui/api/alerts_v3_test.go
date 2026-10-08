package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// 本文件是 Build7 Step 1 的 API 判别性测试：version 3 唯一协议、v1/2 拒绝、
// 告警四对象严格契约、默认全部关闭、reset 归位与事务失败零写入零发布。
//
// 刻意使用 HTTP 断言 + 既有 Store 读取方法（email/webhook），使同一批用例
// 在 Step 1 实施前的代码上也能编译，并因**目标原因**失败（旧代码只认 version 2
// 且只接受两对象 PUT）。

// alertsV3 是 GET /api/alerts 的 v3 响应形状（测试本地定义，避免依赖生产 DTO）
type alertsV3 struct {
	Policy struct {
		DNSFailedEnabled        bool   `json:"dns_failed_enabled"`
		SyncErrorEnabled        bool   `json:"sync_error_enabled"`
		OperationalErrorEnabled bool   `json:"operational_error_enabled"`
		HealthTimeout           string `json:"health_timeout"`
	} `json:"policy"`
	Email struct {
		Enabled  bool   `json:"enabled"`
		Host     string `json:"host"`
		Port     string `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		FromAddr string `json:"from_addr"`
		ToAddr   string `json:"to_addr"`
		Subject  string `json:"subject"`
		Body     string `json:"body"`
	} `json:"email"`
	Webhook struct {
		Enabled bool   `json:"enabled"`
		URL     string `json:"url"`
		Channel string `json:"channel"`
	} `json:"webhook"`
	UptimeKumaPush struct {
		Enabled  bool   `json:"enabled"`
		URL      string `json:"url"`
		Interval string `json:"interval"`
	} `json:"uptime_kuma_push"`
}

// getAlertsV3 读取并解析 GET /api/alerts
func getAlertsV3(t *testing.T, e *testEnv) alertsV3 {
	t.Helper()
	w := e.do(t, http.MethodGet, "/api/alerts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/alerts 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got alertsV3
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析告警响应失败: %v; body=%s", err, w.Body.String())
	}
	return got
}

// v3Bundle 构造带指定 policy/push/email 片段的完整 version 3 配置包。
func v3Bundle(policyJSON, emailJSON, monitoringJSON string) string {
	return `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
		validBundleSettings() + `,` +
		`"alerts":{"policy":` + policyJSON + `,"email":` + emailJSON + `,` +
		`"webhook":{"enabled":false,"url":"","channel":"dingtalk"}},` +
		`"monitoring":` + monitoringJSON + `}`
}

// v3DefaultPolicyJSON 是默认策略片段
const v3DefaultPolicyJSON = `{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false,"health_timeout":"10m"}`

// v3DefaultEmailJSON 是默认邮件片段（含 Build7 主题/正文）
const v3DefaultEmailJSON = `{"enabled":false,"host":"","port":"587","username":"","password":"","from_addr":"","to_addr":"","subject":"[FWAlizer] 告警通知","body":"FWAlizer 检测到运行异常，请检查同步日志。"}`

// TestExportVersion3StructureAndFilename 空库导出必须是完整 version 3 结构：
// policy/email/webhook + monitoring、默认全部关闭、默认主题正文与时长、v3 附件名。
func TestExportVersion3StructureAndFilename(t *testing.T) {
	e := newTestEnv(t)

	w := e.do(t, http.MethodPost, "/api/config/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("导出状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("导出 Cache-Control = %q, want no-store", cc)
	}
	if !version3FilenamePattern.MatchString(w.Header().Get("Content-Disposition")) {
		t.Errorf("导出附件名 = %q, want fwalizer-config-v3-*.json", w.Header().Get("Content-Disposition"))
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析导出失败: %v", err)
	}
	var version int
	if err := json.Unmarshal(raw["version"], &version); err != nil {
		t.Fatalf("解析 version 失败: %v", err)
	}
	if version != 3 {
		t.Fatalf("version = %d, want 3（version 3 是唯一协议）", version)
	}

	var alerts struct {
		Policy struct {
			DNSFailedEnabled        bool   `json:"dns_failed_enabled"`
			SyncErrorEnabled        bool   `json:"sync_error_enabled"`
			OperationalErrorEnabled bool   `json:"operational_error_enabled"`
			HealthTimeout           string `json:"health_timeout"`
		} `json:"policy"`
		Email struct {
			Subject string `json:"subject"`
			Body    string `json:"body"`
		} `json:"email"`
	}
	if err := json.Unmarshal(raw["alerts"], &alerts); err != nil {
		t.Fatalf("解析 alerts 失败（必须包含 policy）: %v; alerts=%s", err, raw["alerts"])
	}
	if alerts.Policy.DNSFailedEnabled || alerts.Policy.SyncErrorEnabled || alerts.Policy.OperationalErrorEnabled {
		t.Errorf("导出默认策略必须三个触发开关全部关闭: %+v", alerts.Policy)
	}
	if alerts.Policy.HealthTimeout != "10m" {
		t.Errorf("导出 health_timeout = %q, want 10m", alerts.Policy.HealthTimeout)
	}
	if alerts.Email.Subject != "[FWAlizer] 告警通知" || alerts.Email.Body != "FWAlizer 检测到运行异常，请检查同步日志。" {
		t.Errorf("导出必须补齐默认主题/正文: %+v", alerts.Email)
	}

	var monitoring struct {
		UptimeKumaPush struct {
			Enabled  bool   `json:"enabled"`
			URL      string `json:"url"`
			Interval string `json:"interval"`
		} `json:"uptime_kuma_push"`
	}
	if err := json.Unmarshal(raw["monitoring"], &monitoring); err != nil {
		t.Fatalf("解析 monitoring 失败（必须存在）: %v; monitoring=%s", err, raw["monitoring"])
	}
	push := monitoring.UptimeKumaPush
	if push.Enabled || push.URL != "" || push.Interval != "60s" {
		t.Errorf("导出默认 Push 必须关闭/空 URL/60s: %+v", push)
	}
}

// TestImportRejectsVersion1And2AndOthers version 1/2 与其他版本必须直接拒绝（400、零写入、零发布）。
func TestImportRejectsVersion1And2AndOthers(t *testing.T) {
	cases := map[string]string{
		"version 1": `{"version":1,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
			validBundleSettings() + `,` + validBundleAlerts() + `,` + validBundleMonitoring() + `}`,
		"version 2": `{"version":2,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
			validBundleSettings() + `,` + validBundleAlerts() + `,` + validBundleMonitoring() + `}`,
		"version 4": `{"version":4,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
			validBundleSettings() + `,` + validBundleAlerts() + `,` + validBundleMonitoring() + `}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			e.seedTarget(t, "tc_lighthouse", "ap-guangzhou", "keep-me")

			w := e.do(t, http.MethodPost, "/api/config/import", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法版本不应触发运行时更新，实际 %d", got)
			}
			targets, err := e.store.GetTargets()
			if err != nil {
				t.Fatalf("GetTargets 失败: %v", err)
			}
			if len(targets) != 1 || targets[0].ResourceID != "keep-me" {
				t.Errorf("非法版本不应改动目标: %+v", targets)
			}
		})
	}
}

// TestImportVersion3RequiresPolicyAndMonitoring 缺失 policy/monitoring 时必须指出具体字段。
func TestImportVersion3RequiresPolicyAndMonitoring(t *testing.T) {
	push := `{"enabled":false,"url":"","interval":"60s"}`
	cases := map[string]struct {
		body string
		want string
	}{
		"缺 alerts.policy": {
			body: `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
				validBundleSettings() + `,"alerts":{"email":` + v3DefaultEmailJSON + `,"webhook":{"enabled":false,"url":"","channel":"dingtalk"}},"monitoring":{"uptime_kuma_push":` + push + `}}`,
			want: "alerts.policy",
		},
		"缺 monitoring": {
			body: `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
				validBundleSettings() + `,"alerts":{"policy":` + v3DefaultPolicyJSON + `,"email":` + v3DefaultEmailJSON + `,"webhook":{"enabled":false,"url":"","channel":"dingtalk"}}}`,
			want: "monitoring",
		},
		"缺 uptime_kuma_push": {
			body: `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
				validBundleSettings() + `,"alerts":{"policy":` + v3DefaultPolicyJSON + `,"email":` + v3DefaultEmailJSON + `,"webhook":{"enabled":false,"url":"","channel":"dingtalk"}},"monitoring":{}}`,
			want: "uptime_kuma_push",
		},
		"缺 health_timeout": {
			body: `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
				validBundleSettings() + `,"alerts":{"policy":{"dns_failed_enabled":false,"sync_error_enabled":false,"operational_error_enabled":false},"email":` + v3DefaultEmailJSON + `,"webhook":{"enabled":false,"url":"","channel":"dingtalk"}},"monitoring":{"uptime_kuma_push":` + push + `}}`,
			want: "health_timeout",
		},
		"缺 email.subject": {
			body: `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
				validBundleSettings() + `,"alerts":{"policy":` + v3DefaultPolicyJSON + `,"email":{"enabled":false,"host":"","port":"587","username":"","password":"","from_addr":"","to_addr":"","body":"b"},"webhook":{"enabled":false,"url":"","channel":"dingtalk"}},"monitoring":{"uptime_kuma_push":` + push + `}}`,
			want: "subject",
		},
		"monitoring 为 null": {
			body: `{"version":3,"metadata":{"exported_at":"2026-09-22T08:00:00Z"},"targets":[],"rules":[],` +
				validBundleSettings() + `,"alerts":{"policy":` + v3DefaultPolicyJSON + `,"email":` + v3DefaultEmailJSON + `,"webhook":{"enabled":false,"url":"","channel":"dingtalk"}},"monitoring":null}`,
			want: "monitoring",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			w := e.do(t, http.MethodPost, "/api/config/import", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Errorf("错误文案应指出缺失字段 %q: %s", tc.want, w.Body.String())
			}
			if got := e.applyCount(); got != 0 {
				t.Errorf("非法配置包不应触发运行时更新，实际 %d", got)
			}
		})
	}
}

// TestImportVersion3WritesPolicyEmailPush version 3 导入必须写入策略、主题正文与 Push，
// 并且导出可无损读回。
func TestImportVersion3WritesPolicyEmailPush(t *testing.T) {
	e := newTestEnv(t)

	policy := `{"dns_failed_enabled":true,"sync_error_enabled":false,"operational_error_enabled":true,"health_timeout":"25m"}`
	email := `{"enabled":true,"host":"smtp.v3","port":"2525","username":"u","password":"pw-v3","from_addr":"f@v3","to_addr":"t@v3","subject":"导入主题","body":"导入正文"}`
	monitoring := `{"uptime_kuma_push":{"enabled":true,"url":"https://kuma.example.com/api/push/tok-v3","interval":"45s"}}`

	w := e.do(t, http.MethodPost, "/api/config/import", v3Bundle(policy, email, monitoring))
	if w.Code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	got := getAlertsV3(t, e)
	if !got.Policy.DNSFailedEnabled || got.Policy.SyncErrorEnabled || !got.Policy.OperationalErrorEnabled {
		t.Errorf("触发策略未按导入值生效: %+v", got.Policy)
	}
	if got.Policy.HealthTimeout != "25m" {
		t.Errorf("health_timeout = %q, want 25m", got.Policy.HealthTimeout)
	}
	if got.Email.Subject != "导入主题" || got.Email.Body != "导入正文" || got.Email.Host != "smtp.v3" {
		t.Errorf("邮件主题/正文/凭据未按导入值生效: %+v", got.Email)
	}
	if !got.UptimeKumaPush.Enabled || got.UptimeKumaPush.URL != "https://kuma.example.com/api/push/tok-v3" ||
		got.UptimeKumaPush.Interval != "45s" {
		t.Errorf("Push 配置未按导入值生效: %+v", got.UptimeKumaPush)
	}

	// 协调器候选必须携带策略与 Push（Build7 §4.6）：不仅是 SQLite，运行时状态也要同步
	st := e.snapshot()
	if st == nil {
		t.Fatal("导入后必须已发布运行时状态")
	}
	if !st.Config.Policy.DNSFailedEnabled || st.Config.Policy.HealthTimeoutText != "25m" {
		t.Errorf("RuntimeState 未携带策略: %+v", st.Config.Policy)
	}
	if !st.Config.UptimeKumaPush.Enabled || st.Config.UptimeKumaPush.IntervalText != "45s" {
		t.Errorf("RuntimeState 未携带 Push: %+v", st.Config.UptimeKumaPush)
	}
	if st.Config.Email.Subject != "导入主题" || st.Config.Email.Body != "导入正文" {
		t.Errorf("RuntimeState 未携带邮件主题/正文: %+v", st.Config.Email)
	}

	// 导出必须包含同一份值
	exported := e.do(t, http.MethodPost, "/api/config/export", "")
	if exported.Code != http.StatusOK {
		t.Fatalf("导出状态码 = %d, want 200", exported.Code)
	}
	if !strings.Contains(exported.Body.String(), `"health_timeout": "25m"`) {
		t.Errorf("导出未携带导入的 health_timeout: %s", exported.Body.String())
	}
	if !strings.Contains(exported.Body.String(), `"interval": "45s"`) {
		t.Errorf("导出未携带导入的 Push interval: %s", exported.Body.String())
	}
	if !strings.Contains(exported.Body.String(), "导入主题") {
		t.Errorf("导出未携带导入的邮件主题: %s", exported.Body.String())
	}
}

// TestPutAlertsFourObjectsRoundTrip 四对象 PUT 必须一次事务保存并只发布一次运行时。
func TestPutAlertsFourObjectsRoundTrip(t *testing.T) {
	e := newTestEnv(t)

	body := `{"policy":{"dns_failed_enabled":true,"sync_error_enabled":true,"operational_error_enabled":false,"health_timeout":"30m"},` +
		`"email":{"enabled":true,"host":"smtp.put","port":"2525","username":"u","password":"pw","from_addr":"f@x","to_addr":"t@x","subject":"主题PUT","body":"正文PUT"},` +
		`"webhook":{"enabled":true,"url":"https://hook.example.com/put","channel":"slack"},` +
		`"uptime_kuma_push":{"enabled":true,"url":"https://kuma.example.com/api/push/tok-put","interval":"20s"}}`
	if w := e.do(t, http.MethodPut, "/api/alerts", body); w.Code != http.StatusOK {
		t.Fatalf("PUT 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != 1 {
		t.Errorf("合法变更必须只发布一次，实际 %d", got)
	}

	got := getAlertsV3(t, e)
	if !got.Policy.DNSFailedEnabled || !got.Policy.SyncErrorEnabled || got.Policy.OperationalErrorEnabled {
		t.Errorf("策略未保存: %+v", got.Policy)
	}
	if got.Policy.HealthTimeout != "30m" {
		t.Errorf("health_timeout = %q, want 30m", got.Policy.HealthTimeout)
	}
	if !got.Email.Enabled || got.Email.Subject != "主题PUT" || got.Email.Body != "正文PUT" || got.Email.Port != "2525" {
		t.Errorf("邮件未保存: %+v", got.Email)
	}
	if !got.Webhook.Enabled || got.Webhook.URL != "https://hook.example.com/put" || got.Webhook.Channel != "slack" {
		t.Errorf("Webhook 未保存: %+v", got.Webhook)
	}
	if !got.UptimeKumaPush.Enabled || got.UptimeKumaPush.Interval != "20s" {
		t.Errorf("Push 未保存: %+v", got.UptimeKumaPush)
	}

	// 协调器候选必须携带策略与 Push（Build7 §4.6）
	st := e.snapshot()
	if st == nil {
		t.Fatal("PUT 后必须已发布运行时状态")
	}
	if !st.Config.Policy.SyncErrorEnabled || st.Config.Policy.HealthTimeoutText != "30m" {
		t.Errorf("RuntimeState 未携带策略: %+v", st.Config.Policy)
	}
	if !st.Config.UptimeKumaPush.Enabled || st.Config.UptimeKumaPush.IntervalText != "20s" {
		t.Errorf("RuntimeState 未携带 Push: %+v", st.Config.UptimeKumaPush)
	}

	// GET 必须禁止缓存（响应含 SMTP 密码与 Push URL）
	w := e.do(t, http.MethodGet, "/api/alerts", "")
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("GET /api/alerts Cache-Control = %q, want no-store", cc)
	}
}

// TestPutAlertsRollbackOnPushWriteFailure 第四步写入失败必须完整回滚：四个对象全部保持旧值、零发布。
func TestPutAlertsRollbackOnPushWriteFailure(t *testing.T) {
	e := newTestEnv(t)

	old := alertsBody("old.example.com", "old-pass", "https://old.example.com/hook", "dingtalk")
	if w := e.do(t, http.MethodPut, "/api/alerts", old); w.Code != http.StatusOK {
		t.Fatalf("预置旧告警失败: %d %s", w.Code, w.Body.String())
	}
	before := e.applyCount()

	// 让 uptime_kuma_push 的写入失败（四对象保存在同一事务内）
	e.execRaw(t, `CREATE TRIGGER fail_push BEFORE INSERT ON uptime_kuma_push BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)

	body := `{"policy":{"dns_failed_enabled":true,"sync_error_enabled":true,"operational_error_enabled":true,"health_timeout":"99m"},` +
		`"email":{"enabled":false,"host":"new.example.com","port":"587","username":"u","password":"new-pass","from_addr":"f@x","to_addr":"t@x","subject":"新主题","body":"新正文"},` +
		`"webhook":{"enabled":false,"url":"","channel":"dingtalk"},` +
		`"uptime_kuma_push":{"enabled":false,"url":"","interval":"60s"}}`
	w := e.do(t, http.MethodPut, "/api/alerts", body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if got := e.applyCount(); got != before {
		t.Errorf("失败事务不得发布运行时，applyCount = %d, want %d", got, before)
	}

	after := getAlertsV3(t, e)
	if after.Email.Host != "old.example.com" || after.Email.Password != "old-pass" {
		t.Errorf("邮件必须随 Push 写入失败一起回滚: %+v", after.Email)
	}
	if after.Policy.DNSFailedEnabled || after.Policy.HealthTimeout != "10m" {
		t.Errorf("策略必须随 Push 写入失败一起回滚: %+v", after.Policy)
	}
}

// TestResetRestoresAlertDefaults reset 之后必须回到「全部关闭 + 10m/60s + 默认主题正文」。
func TestResetRestoresAlertDefaults(t *testing.T) {
	e := newTestEnv(t)

	body := `{"policy":{"dns_failed_enabled":true,"sync_error_enabled":true,"operational_error_enabled":true,"health_timeout":"30m"},` +
		`"email":{"enabled":true,"host":"smtp.put","port":"2525","username":"u","password":"pw","from_addr":"f@x","to_addr":"t@x","subject":"主题","body":"正文"},` +
		`"webhook":{"enabled":true,"url":"https://hook.example.com/x","channel":"feishu"},` +
		`"uptime_kuma_push":{"enabled":true,"url":"https://kuma.example.com/api/push/t","interval":"45s"}}`
	if w := e.do(t, http.MethodPut, "/api/alerts", body); w.Code != http.StatusOK {
		t.Fatalf("预置失败: %d %s", w.Code, w.Body.String())
	}

	if w := e.do(t, http.MethodPost, "/api/config/reset", `{}`); w.Code != http.StatusOK {
		t.Fatalf("reset 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	got := getAlertsV3(t, e)
	if got.Policy.DNSFailedEnabled || got.Policy.SyncErrorEnabled || got.Policy.OperationalErrorEnabled {
		t.Errorf("reset 后触发开关必须全部关闭: %+v", got.Policy)
	}
	if got.Policy.HealthTimeout != "10m" {
		t.Errorf("reset 后 health_timeout = %q, want 10m", got.Policy.HealthTimeout)
	}
	if got.Email.Enabled || got.Email.Host != "" || got.Email.Password != "" {
		t.Errorf("reset 后邮件必须关闭且清空凭据: %+v", got.Email)
	}
	if got.Email.Subject != "[FWAlizer] 告警通知" || got.Email.Body != "FWAlizer 检测到运行异常，请检查同步日志。" {
		t.Errorf("reset 后主题/正文必须回到默认: %+v", got.Email)
	}
	if got.Webhook.Enabled || got.Webhook.URL != "" || got.Webhook.Channel != "" {
		t.Errorf("reset 后 Webhook 必须回到默认: %+v", got.Webhook)
	}
	if got.UptimeKumaPush.Enabled || got.UptimeKumaPush.URL != "" || got.UptimeKumaPush.Interval != "60s" {
		t.Errorf("reset 后 Push 必须回到默认: %+v", got.UptimeKumaPush)
	}
}
