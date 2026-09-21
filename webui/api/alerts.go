package api

import (
	"encoding/json"
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	webhookCfg, err := d.Store.GetAlertWebhook()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, alertsResponse{Email: emailCfg, Webhook: webhookCfg})
}

func (d *Deps) handlePutAlerts(w http.ResponseWriter, r *http.Request) {
	var req alertsResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Email != nil {
		if err := d.Store.SaveAlertEmail(req.Email); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.Webhook != nil {
		if err := d.Store.SaveAlertWebhook(req.Webhook); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	d.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"message": "保存成功"})
}
