package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// alertsResponse 告警配置响应结构
type alertsResponse struct {
	Email   *config.AlertEmailConfig   `json:"email"`
	Webhook *config.AlertWebhookConfig `json:"webhook"`
}

func (d *Deps) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	emailCfg, err := d.Store.GetAlertEmail()
	if err != nil {
		writeInternalError(w, "读取告警配置失败", err)
		return
	}
	webhookCfg, err := d.Store.GetAlertWebhook()
	if err != nil {
		writeInternalError(w, "读取告警配置失败", err)
		return
	}

	// 空库/未配置时补齐固定默认值（Build6 §3.1）：前端表单会把 GET 结果原样回传，
	// 而 PUT 要求端口与渠道类型正确，缺省值必须是合法值
	if emailCfg.Port == "" {
		emailCfg.Port = "587"
	}
	if webhookCfg.Channel == "" {
		webhookCfg.Channel = "dingtalk"
	}

	writeJSON(w, http.StatusOK, alertsResponse{Email: emailCfg, Webhook: webhookCfg})
}

// alertEmailRequest 邮件告警请求体：每个子字段都用指针做 presence 检查
type alertEmailRequest struct {
	Enabled  *bool   `json:"enabled"`
	Host     *string `json:"host"`
	Port     *string `json:"port"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	FromAddr *string `json:"from_addr"`
	ToAddr   *string `json:"to_addr"`
}

// toConfig 校验并转换邮件告警请求（缺失字段与 null 均拒绝）
func (req *alertEmailRequest) toConfig() (config.AlertEmailConfig, error) {
	if req == nil {
		return config.AlertEmailConfig{}, badRequest("email 字段缺失")
	}
	if req.Enabled == nil || req.Host == nil || req.Port == nil || req.Username == nil ||
		req.Password == nil || req.FromAddr == nil || req.ToAddr == nil {
		return config.AlertEmailConfig{}, badRequest("email 的每个子字段都必须出现")
	}
	return config.AlertEmailConfig{
		Enabled:  *req.Enabled,
		Host:     *req.Host,
		Port:     *req.Port,
		Username: *req.Username,
		Password: *req.Password,
		FromAddr: *req.FromAddr,
		ToAddr:   *req.ToAddr,
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

// alertsRequest PUT /api/alerts 请求体：email 与 webhook 两个对象都必需，
// 不能用 null 表示“不改”（Build6 §12.9）
type alertsRequest struct {
	Email   *alertEmailRequest   `json:"email"`
	Webhook *alertWebhookRequest `json:"webhook"`
}

func (d *Deps) handlePutAlerts(w http.ResponseWriter, r *http.Request) {
	var req alertsRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
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

	// 邮件与 Webhook 必须在同一个事务内覆盖保存，不能只保存其中一半
	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if serr := d.Store.SaveAlertEmailTx(ctx, tx, &emailCfg); serr != nil {
			return serr
		}
		return d.Store.SaveAlertWebhookTx(ctx, tx, &webhookCfg)
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "保存成功"})
}
