package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 唯一 sentinel：任何出现在错误响应或日志里都判定为泄露
const (
	sentinelTCAccessID   = "AKIDsentinelTcId0001"
	sentinelTCAccessKey  = "sentinelTcKey0002"
	sentinelAliAccessID  = "LTAIsentinelAliId0003"
	sentinelAliAccessKey = "sentinelAliKey0004"
	sentinelSMTPPassword = "sentinelSmtpPassword0005"
	sentinelWebhookURL   = "https://sentinel-webhook.invalid/hook0006"
)

var allSentinels = []string{
	sentinelTCAccessID, sentinelTCAccessKey,
	sentinelAliAccessID, sentinelAliAccessKey,
	sentinelSMTPPassword, sentinelWebhookURL,
}

// seedSentinels 把唯一 sentinel 写入云凭据与告警配置
func seedSentinels(t *testing.T, e *testEnv) {
	t.Helper()
	w := e.do(t, http.MethodPut, "/api/settings",
		settingsWithSentinels(sentinelTCAccessID, sentinelTCAccessKey, sentinelAliAccessID, sentinelAliAccessKey))
	if w.Code != http.StatusOK {
		t.Fatalf("预置凭据 sentinel 失败: %d %s", w.Code, w.Body.String())
	}
	w = e.do(t, http.MethodPut, "/api/alerts",
		alertsBody("smtp.example.com", sentinelSMTPPassword, sentinelWebhookURL, "dingtalk"))
	if w.Code != http.StatusOK {
		t.Fatalf("预置告警 sentinel 失败: %d %s", w.Code, w.Body.String())
	}
}

// assertNoSentinels 断言文本中不出现任何 sentinel
func assertNoSentinels(t *testing.T, label, text string) {
	t.Helper()
	for _, s := range allSentinels {
		if strings.Contains(text, s) {
			t.Errorf("%s 泄露敏感值 %q: %s", label, s, text)
		}
	}
}

// TestErrorResponsesAndLogsDoNotLeakSentinels 新增的 400/409/413/500 响应与服务日志都不得出现敏感值
func TestErrorResponsesAndLogsDoNotLeakSentinels(t *testing.T) {
	e := newTestEnv(t)
	seedSentinels(t, e)

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	// 400：领域校验错误
	w := e.do(t, http.MethodPut, "/api/settings", `{"interval":"not-a-duration"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	assertNoSentinels(t, "400 响应", w.Body.String())

	// 409：目标被规则引用
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-1")
	e.seedRule(t, config.DomainRule{Host: "a.example.com", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{id}})
	w = e.do(t, http.MethodDelete, pathWithID("/api/targets/", id), "")
	if w.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	assertNoSentinels(t, "409 响应", w.Body.String())

	// 413：body 超限
	big := `{"cloud_type":"tc_cvm","region":"gz","resource_id":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
	w = e.do(t, http.MethodPost, "/api/targets", big)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, want 413", w.Code)
	}
	assertNoSentinels(t, "413 响应", w.Body.String())

	// 500：底层写入失败
	e.execRaw(t, `CREATE TRIGGER fail_target_insert BEFORE INSERT ON targets BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)
	w = e.do(t, http.MethodPost, "/api/targets", `{"cloud_type":"tc_cvm","region":"gz","resource_id":"x"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	assertNoSentinels(t, "500 响应", w.Body.String())
	assertNoSentinels(t, "服务端日志", logBuf.String())
}

// TestDocumentedGetContractsStillReturnSecrets 已文档化的 GET 契约按用户确认保留（§12.16 的 sentinel 范围限定为 Step 5 导入导出）
func TestDocumentedGetContractsStillReturnSecrets(t *testing.T) {
	e := newTestEnv(t)
	seedSentinels(t, e)

	settingsBody := e.do(t, http.MethodGet, "/api/settings", "").Body.String()
	if !strings.Contains(settingsBody, sentinelTCAccessKey) || !strings.Contains(settingsBody, sentinelAliAccessKey) {
		t.Errorf("GET /api/settings 按 §12.9 应返回 11 个可编辑键（含凭据）供表单回显: %s", settingsBody)
	}
	alertsBodyText := e.do(t, http.MethodGet, "/api/alerts", "").Body.String()
	if !strings.Contains(alertsBodyText, sentinelSMTPPassword) || !strings.Contains(alertsBodyText, sentinelWebhookURL) {
		t.Errorf("GET /api/alerts 应返回告警表单所需的完整对象: %s", alertsBodyText)
	}
	// version 2 导出是完整敏感快照，也是**唯一**允许包含这些 sentinel 的 HTTP 响应
	// （Build6 §3.6、§12.16）；其余响应与日志都不允许出现。
	exportBody := e.do(t, http.MethodPost, "/api/config/export", "").Body.String()
	for _, secret := range allSentinels {
		if !strings.Contains(exportBody, secret) {
			t.Errorf("version 2 导出应包含完整敏感快照，缺少 %q", secret)
		}
	}
}

// TestValidationErrorsDoNotEchoValues 领域校验错误只指出字段，不回显原始值
func TestValidationErrorsDoNotEchoValues(t *testing.T) {
	e := newTestEnv(t)
	secret := "AKIDsentinelValueEcho"
	w := e.do(t, http.MethodPut, "/api/settings", `{"tc_access_id":"`+secret+`","theme":"blue"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Errorf("校验错误不得回显原始值: %s", w.Body.String())
	}
	// 校验失败时不落库：连合法的 tc_access_id 也不能写入
	if settings, _ := e.store.GetSettings(); len(settings) != 0 {
		t.Errorf("校验失败不应留下部分写入: %+v", settings)
	}
}
