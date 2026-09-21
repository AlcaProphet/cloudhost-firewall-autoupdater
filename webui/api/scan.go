package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// scanResourcesReq 扫描资源请求
type scanResourcesReq struct {
	CloudType string `json:"cloud_type"`
	Region    string `json:"region"`
}

// handleScanResources 扫描指定云厂商+地域的资源列表并持久化（供添加目标时自动补全）
func (d *Deps) handleScanResources(w http.ResponseWriter, r *http.Request) {
	var req scanResourcesReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.CloudType == "" || req.Region == "" {
		writeError(w, http.StatusBadRequest, "cloud_type 与 region 不能为空")
		return
	}

	// 从 Store 读取凭据（与 handleTestConnection 一致的凭据校验）
	settings, err := d.Store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取凭据失败")
		return
	}
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

	// 扫描资源
	pool := provider.NewClientPool()
	resources, err := provider.ScanResources(config.CloudType(req.CloudType), req.Region, pool)
	if err != nil {
		slog.Warn("扫描资源失败", "cloud_type", req.CloudType, "region", req.Region, "error", err)
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	// 持久化（覆盖式）
	scanned := make([]config.ScannedResource, 0, len(resources))
	for _, res := range resources {
		scanned = append(scanned, config.ScannedResource{
			CloudType:    req.CloudType,
			Region:       res.Region,
			ResourceID:   res.ResourceID,
			ResourceName: res.Name,
		})
	}
	if err := d.Store.ReplaceScannedResources(req.CloudType, req.Region, scanned); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"resources": scanned,
		"count":     len(scanned),
	})
}

// handleGetScannedResources 获取某云厂商的扫描结果（资源 ID 自动补全数据源）
func (d *Deps) handleGetScannedResources(w http.ResponseWriter, r *http.Request) {
	cloudType := r.URL.Query().Get("cloud_type")
	if cloudType == "" {
		writeError(w, http.StatusBadRequest, "缺少 cloud_type 参数")
		return
	}
	resources, err := d.Store.GetScannedResources(cloudType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resources)
}

// handleDeleteScannedResources 清理某云厂商的扫描结果
func (d *Deps) handleDeleteScannedResources(w http.ResponseWriter, r *http.Request) {
	cloudType := r.URL.Query().Get("cloud_type")
	if cloudType == "" {
		writeError(w, http.StatusBadRequest, "缺少 cloud_type 参数")
		return
	}
	if err := d.Store.DeleteScannedResources(cloudType); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "清理成功"})
}
