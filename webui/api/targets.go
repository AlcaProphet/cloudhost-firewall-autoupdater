package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func (d *Deps) handleGetTargets(w http.ResponseWriter, r *http.Request) {
	targets, err := d.Store.GetTargets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, targets)
}

func (d *Deps) handleAddTarget(w http.ResponseWriter, r *http.Request) {
	var t config.TargetConfig
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := d.Store.AddTarget(t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.notifyReload()
	writeJSON(w, http.StatusCreated, map[string]string{"message": "添加成功"})
}

func (d *Deps) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var n int
	if _, err := fmt.Sscanf(id, "%d", &n); err != nil {
		writeError(w, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	var t config.TargetConfig
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := d.Store.UpdateTarget(n, t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

func (d *Deps) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var n int
	if _, err := fmt.Sscanf(id, "%d", &n); err != nil {
		writeError(w, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	if err := d.Store.DeleteTarget(n); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// testConnectionReq 测试连接请求
type testConnectionReq struct {
	CloudType  string `json:"cloud_type"`
	Region     string `json:"region"`
	ResourceID string `json:"resource_id"`
}

func (d *Deps) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var req testConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	// 从 Store 读取凭据
	settings, err := d.Store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取凭据失败")
		return
	}
	// 凭据空值快速失败：避免暴露 SDK 原始报错
	if strings.HasPrefix(req.CloudType, "tc_") && (settings["tc_access_id"] == "" || settings["tc_access_key"] == "") {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "腾讯云凭据未配置，请先在全局设置中填写"})
		return
	}
	if strings.HasPrefix(req.CloudType, "ali_") && (settings["ali_access_id"] == "" || settings["ali_access_key"] == "") {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "阿里云凭据未配置，请先在全局设置中填写"})
		return
	}
	provider.SetCredentials(
		settings["tc_access_id"], settings["tc_access_key"],
		settings["ali_access_id"], settings["ali_access_key"],
	)

	// 创建临时 Provider 测试连通性
	cfg := config.TargetConfig{
		CloudType:  config.CloudType(req.CloudType),
		Region:     req.Region,
		ResourceID: req.ResourceID,
	}
	pool := provider.NewClientPool()
	p, err := provider.NewProvider(cfg, 0, pool)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	rules, err := p.GetRules()
	if err != nil {
		slog.Warn("测试连接失败", "provider", p.Name(), "error", err)
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("连接成功，当前 %d 条规则", len(rules)),
	})
}
