package api

import (
	"encoding/json"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// Syncer 同步引擎接口（避免 api 包直接依赖 syncer 包的具体实现）
type Syncer interface {
	Status() syncer.SyncStatus
	TriggerSync()
	DryRun() (syncer.DryRunResponse, error)
	Pause()  // 暂停同步
	Resume() // 恢复同步
}

// EventSubscriber 事件订阅接口（用于 SSE 推送）
type EventSubscriber interface {
	SubscribeChan() (<-chan notifier.Event, func())
}

// Deps API handler 共享依赖
type Deps struct {
	Store        *config.Store
	Syncer       Syncer          // 可为 nil（无配置时）
	EventBus     EventSubscriber // 可为 nil
	LogBroadcaster *LogBroadcaster // 可为 nil
	ReloadFunc   func()
}

// Register 注册所有 API 路由到 mux
func (d *Deps) Register(mux *http.ServeMux) {
	// 目标管理
	mux.HandleFunc("GET /api/targets", d.handleGetTargets)
	mux.HandleFunc("POST /api/targets", d.handleAddTarget)
	mux.HandleFunc("PUT /api/targets/{id}", d.handleUpdateTarget)
	mux.HandleFunc("DELETE /api/targets/{id}", d.handleDeleteTarget)
	mux.HandleFunc("POST /api/test-connection", d.handleTestConnection)
	// 地域数据（前端自动补全数据源）
	mux.HandleFunc("GET /api/zones", d.handleGetZones)
	// 资源扫描（凭据卡片扫描 + 添加目标自动补全）
	mux.HandleFunc("POST /api/scan-resources", d.handleScanResources)
	mux.HandleFunc("GET /api/scanned-resources", d.handleGetScannedResources)
	mux.HandleFunc("DELETE /api/scanned-resources", d.handleDeleteScannedResources)
	// 规则管理
	mux.HandleFunc("GET /api/rules", d.handleGetRules)
	mux.HandleFunc("POST /api/rules", d.handleAddRule)
	mux.HandleFunc("PUT /api/rules/{id}", d.handleUpdateRule)
	mux.HandleFunc("DELETE /api/rules/{id}", d.handleDeleteRule)
	// 同步
	mux.HandleFunc("GET /api/sync/status", d.handleSyncStatus)
	mux.HandleFunc("POST /api/sync/trigger", d.handleSyncTrigger)
	mux.HandleFunc("POST /api/sync/dryrun", d.handleSyncDryRun)
	mux.HandleFunc("POST /api/sync/pause", d.handleSyncPause)
	mux.HandleFunc("POST /api/sync/resume", d.handleSyncResume)
	mux.HandleFunc("GET /api/sync/events", d.handleSyncEvents)
	mux.HandleFunc("GET /api/sync/logs", d.handleGetSyncLogs)
	mux.HandleFunc("DELETE /api/sync/logs", d.handleClearSyncLogs)
	// 实时日志流
	mux.HandleFunc("GET /api/logs/stream", d.handleLogStream)
	// 设置 + 配置导入导出
	mux.HandleFunc("GET /api/settings", d.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", d.handlePutSettings)
	mux.HandleFunc("POST /api/config/reset", d.handleConfigReset)
	mux.HandleFunc("GET /api/config/export", d.handleConfigExport)
	mux.HandleFunc("POST /api/config/import", d.handleConfigImport)
	// 告警配置
	mux.HandleFunc("GET /api/alerts", d.handleGetAlerts)
	mux.HandleFunc("PUT /api/alerts", d.handlePutAlerts)
}

// notifyReload 触发配置重载
func (d *Deps) notifyReload() {
	if d.ReloadFunc != nil {
		d.ReloadFunc()
	}
}

// writeJSON 写入 JSON 响应
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
