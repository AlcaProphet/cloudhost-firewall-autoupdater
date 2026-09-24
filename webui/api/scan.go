package api

import (
	"log/slog"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// scanResourcesReq 扫描资源请求（固定两字段，Build6 §12.9）
type scanResourcesReq struct {
	CloudType string `json:"cloud_type"`
	Region    string `json:"region"`
}

// handleScanResources 扫描指定云厂商+地域的资源列表并持久化（供添加目标时自动补全）
func (d *Deps) handleScanResources(w http.ResponseWriter, r *http.Request) {
	var req scanResourcesReq
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	// 与目标 CRUD 复用同一组基础校验（cloud_type 枚举 + region Trim 后非空）
	ct, err := config.NormalizeCloudType(req.CloudType)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	region, err := config.NormalizeRegion(req.Region)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	// 只取一次运行时快照，并使用其中的不可变凭据与 ClientPool：
	// 不再从 Store 零散读取密钥，也不覆盖同步正在使用的凭据（Build6 §12.3 第 6 条）。
	state := d.runtimeSnapshot()
	if state == nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "运行时状态尚未就绪，请稍后重试"})
		return
	}
	if ready, hint := providerCredentialsReady(ct, state.Config.Credentials); !ready {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": hint})
		return
	}

	// 扫描资源：使用快照中的 pool，同一请求全程只用一个快照
	resources, err := provider.ScanResources(ct, region, state.Pool)
	if err != nil {
		slog.Warn("扫描资源失败", "cloud_type", ct, "region", region, "error", err)
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	// 持久化（覆盖式）
	scanned := make([]config.ScannedResource, 0, len(resources))
	for _, res := range resources {
		scanned = append(scanned, config.ScannedResource{
			CloudType:    string(ct),
			Region:       res.Region,
			ResourceID:   res.ResourceID,
			ResourceName: res.Name,
		})
	}
	if err := d.Store.ReplaceScannedResources(string(ct), region, scanned); err != nil {
		writeInternalError(w, "保存扫描结果失败", err)
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
		writeInternalError(w, "读取扫描结果失败", err)
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
		writeInternalError(w, "清理扫描结果失败", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "清理成功"})
}
