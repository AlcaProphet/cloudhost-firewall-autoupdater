package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// ─── GET /api/alerts：四个完整对象（Build7 §4.6） ───
//
// health_timeout 与 interval 在 HTTP 层以时长文本表达（"10m" / "60s"），
// 与 SQLite 和 version 3 配置包保持一致；领域层同时持有解析后的 Duration。

// alertPolicyResponse 触发策略响应
type alertPolicyResponse struct {
	DNSFailedEnabled        bool   `json:"dns_failed_enabled"`
	SyncErrorEnabled        bool   `json:"sync_error_enabled"`
	OperationalErrorEnabled bool   `json:"operational_error_enabled"`
	HealthTimeout           string `json:"health_timeout"`
}

// alertPushResponse Uptime Kuma Push 响应
type alertPushResponse struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"`
	Interval string `json:"interval"`
}

// alertsResponse 告警配置响应结构：四个对象都必须出现
type alertsResponse struct {
	Policy         alertPolicyResponse        `json:"policy"`
	Email          *config.AlertEmailConfig   `json:"email"`
	Webhook        *config.AlertWebhookConfig `json:"webhook"`
	UptimeKumaPush alertPushResponse          `json:"uptime_kuma_push"`
}

// toPolicyResponse 把领域策略转换为响应对象
func toPolicyResponse(p config.AlertPolicyConfig) alertPolicyResponse {
	return alertPolicyResponse{
		DNSFailedEnabled:        p.DNSFailedEnabled,
		SyncErrorEnabled:        p.SyncErrorEnabled,
		OperationalErrorEnabled: p.OperationalErrorEnabled,
		HealthTimeout:           p.HealthTimeoutText,
	}
}

// toPushResponse 把领域 Push 配置转换为响应对象
func toPushResponse(p config.UptimeKumaPushConfig) alertPushResponse {
	return alertPushResponse{Enabled: p.Enabled, URL: p.URL, Interval: p.IntervalText}
}

func (d *Deps) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	snapshot, err := d.Store.LoadAlertsSnapshot(r.Context())
	if err != nil {
		writeInternalError(w, "读取告警配置失败", err)
		return
	}
	policy, emailCfg := &snapshot.Policy, &snapshot.Email
	webhookCfg, pushCfg := &snapshot.Webhook, &snapshot.UptimeKumaPush

	// 空库/未配置时补齐固定默认值：前端表单会把 GET 结果原样回传，
	// 而 PUT 要求每个字段都是合法值（Build7 §4.6）
	if policy.HealthTimeoutText == "" {
		*policy = config.DefaultAlertPolicy()
	}
	if emailCfg.Security == "" {
		emailCfg.Security = config.DefaultSMTPSecurity
	}
	if emailCfg.Port == "" {
		emailCfg.Port = config.DefaultAlertPort
	}
	if emailCfg.Subject == "" {
		emailCfg.Subject = config.DefaultEmailSubject
	}
	if emailCfg.Body == "" {
		emailCfg.Body = config.DefaultEmailBody
	}
	if pushCfg.IntervalText == "" {
		*pushCfg = config.DefaultUptimeKumaPush()
	}

	// 响应包含 SMTP 密码、Webhook URL 与 Push URL（既有敏感对象边界），
	// 因此必须禁止任何缓存（Build7 §4.6）
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, alertsResponse{
		Policy:         toPolicyResponse(*policy),
		Email:          emailCfg,
		Webhook:        webhookCfg,
		UptimeKumaPush: toPushResponse(*pushCfg),
	})
}

// ─── PUT /api/alerts：四个对象及全部子字段都必须出现（Build7 §4.6） ───

// alertPolicyRequest 触发策略请求体：每个子字段都用指针做 presence 检查
type alertPolicyRequest struct {
	DNSFailedEnabled        *bool   `json:"dns_failed_enabled"`
	SyncErrorEnabled        *bool   `json:"sync_error_enabled"`
	OperationalErrorEnabled *bool   `json:"operational_error_enabled"`
	HealthTimeout           *string `json:"health_timeout"`
}

// toConfig 转换触发策略请求（缺失字段与 null 均拒绝）
func (req *alertPolicyRequest) toConfig() (config.AlertPolicyConfig, error) {
	if req == nil {
		return config.AlertPolicyConfig{}, badRequest("policy 字段缺失")
	}
	if req.DNSFailedEnabled == nil || req.SyncErrorEnabled == nil ||
		req.OperationalErrorEnabled == nil || req.HealthTimeout == nil {
		return config.AlertPolicyConfig{}, badRequest("policy 的每个子字段都必须出现")
	}
	return config.AlertPolicyConfig{
		DNSFailedEnabled:        *req.DNSFailedEnabled,
		SyncErrorEnabled:        *req.SyncErrorEnabled,
		OperationalErrorEnabled: *req.OperationalErrorEnabled,
		HealthTimeoutText:       *req.HealthTimeout,
	}, nil
}

// alertEmailRequest 邮件告警请求体：每个子字段都用指针做 presence 检查
type alertEmailRequest struct {
	Enabled  *bool   `json:"enabled"`
	Host     *string `json:"host"`
	Port     *string `json:"port"`
	Security *string `json:"security"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	FromAddr *string `json:"from_addr"`
	ToAddr   *string `json:"to_addr"`
	Subject  *string `json:"subject"`
	Body     *string `json:"body"`
}

// toConfig 校验并转换邮件告警请求（缺失字段与 null 均拒绝）
func (req *alertEmailRequest) toConfig() (config.AlertEmailConfig, error) {
	if req == nil {
		return config.AlertEmailConfig{}, badRequest("email 字段缺失")
	}
	if req.Enabled == nil || req.Host == nil || req.Port == nil || req.Security == nil || req.Username == nil ||
		req.Password == nil || req.FromAddr == nil || req.ToAddr == nil ||
		req.Subject == nil || req.Body == nil {
		return config.AlertEmailConfig{}, badRequest("email 的每个子字段都必须出现")
	}
	return config.AlertEmailConfig{
		Enabled:  *req.Enabled,
		Host:     *req.Host,
		Port:     *req.Port,
		Security: *req.Security,
		Username: *req.Username,
		Password: *req.Password,
		FromAddr: *req.FromAddr,
		ToAddr:   *req.ToAddr,
		Subject:  *req.Subject,
		Body:     *req.Body,
	}, nil
}

// alertWebhookRequest Webhook 告警请求体：每个子字段都用指针做 presence 检查
type alertWebhookRequest struct {
	Enabled *bool   `json:"enabled"`
	URL     *string `json:"url"`
	Channel *string `json:"channel"`
}

// toConfig 校验并转换 Webhook 告警请求（缺失字段与 null 均拒绝）
func (req *alertWebhookRequest) toConfig() (config.AlertWebhookConfig, error) {
	if req == nil {
		return config.AlertWebhookConfig{}, badRequest("webhook 字段缺失")
	}
	if req.Enabled == nil || req.URL == nil || req.Channel == nil {
		return config.AlertWebhookConfig{}, badRequest("webhook 的每个子字段都必须出现")
	}
	return config.AlertWebhookConfig{
		Enabled: *req.Enabled,
		URL:     *req.URL,
		Channel: *req.Channel,
	}, nil
}

// alertPushRequest Uptime Kuma Push 请求体：每个子字段都用指针做 presence 检查
type alertPushRequest struct {
	Enabled  *bool   `json:"enabled"`
	URL      *string `json:"url"`
	Interval *string `json:"interval"`
}

// toConfig 校验并转换 Push 请求（缺失字段与 null 均拒绝）
func (req *alertPushRequest) toConfig() (config.UptimeKumaPushConfig, error) {
	if req == nil {
		return config.UptimeKumaPushConfig{}, badRequest("uptime_kuma_push 字段缺失")
	}
	if req.Enabled == nil || req.URL == nil || req.Interval == nil {
		return config.UptimeKumaPushConfig{}, badRequest("uptime_kuma_push 的每个子字段都必须出现")
	}
	return config.UptimeKumaPushConfig{
		Enabled:      *req.Enabled,
		URL:          *req.URL,
		IntervalText: *req.Interval,
	}, nil
}

// alertsRequest PUT /api/alerts 请求体：四个对象都必需，
// 不能用 null 表示“不改”（Build7 §4.6）
type alertsRequest struct {
	Policy         *alertPolicyRequest  `json:"policy"`
	Email          *alertEmailRequest   `json:"email"`
	Webhook        *alertWebhookRequest `json:"webhook"`
	UptimeKumaPush *alertPushRequest    `json:"uptime_kuma_push"`
}

func (d *Deps) handlePutAlerts(w http.ResponseWriter, r *http.Request) {
	var req alertsRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}

	policyCfg, err := req.Policy.toConfig()
	if err != nil {
		writeRequestError(w, err)
		return
	}
	policyCfg, err = config.NormalizeAlertPolicy(policyCfg)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	emailCfg, err := req.Email.toConfig()
	if err != nil {
		writeRequestError(w, err)
		return
	}
	emailCfg, err = config.NormalizeAlertEmail(emailCfg)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	webhookCfg, err := req.Webhook.toConfig()
	if err != nil {
		writeRequestError(w, err)
		return
	}
	webhookCfg, err = config.NormalizeAlertWebhook(webhookCfg)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	pushCfg, err := req.UptimeKumaPush.toConfig()
	if err != nil {
		writeRequestError(w, err)
		return
	}
	pushCfg, err = config.NormalizeUptimeKumaPush(pushCfg)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	// 四个部分必须在同一个事务内覆盖保存，任一步失败全部回滚（Build7 §4.6）
	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return d.Store.ReplaceBusinessAlertsTx(ctx, tx, policyCfg, emailCfg, webhookCfg, pushCfg)
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "保存成功"})
}
