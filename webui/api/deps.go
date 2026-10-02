package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/app"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// Syncer 同步引擎接口（避免 api 包直接依赖 syncer 包的具体实现细节）。
//
// Issue6 A5 / 批次 8（2026-09-27 用户裁决，解释 A）：接口**不再**暴露
// `Pause()`/`Resume()`——pause/resume 的运行时写入口唯一属于配置变更协调器
// （handler 先写 DB，再由协调器在 commit 后发布新 RuntimeState）。移除接口成员后，
// 「handler 在协调器之外二次改写运行时开关」从**运行时断言**升级为**编译期不可能**。
//
// 注意：`syncer.Syncer` 上的 `Pause()`/`Resume()` 实现方法**必须保留**
// （Build6 明确要求，且 `syncer` 包内测试继续使用），删除的只是 API 侧的接口成员。
type Syncer interface {
	Status() syncer.SyncStatus
	TriggerSync()
	DryRun() (syncer.DryRunResponse, error)
	// Runtime 返回运行时状态管理器：只读操作（连接测试、资源扫描）据此取得一次完整快照。
	//
	// 依 2026-09-27 用户裁决（解释 A）**保留**该接口成员；`syncer` 侧实现同样保留。
	Runtime() *syncer.RuntimeManager
	// ApplyState 无失败地一次性发布完整运行时状态（协调器 commit 后调用）
	ApplyState(state *syncer.RuntimeState)
}

// EventSubscriber 事件订阅接口（用于 SSE 推送）
type EventSubscriber interface {
	SubscribeChan() (<-chan notifier.Event, func())
}

// Deps API handler 共享依赖
type Deps struct {
	Store          *config.Store
	Syncer         Syncer                 // 可为 nil（无配置时）
	EventBus       EventSubscriber        // 可为 nil
	Runtime        *syncer.RuntimeManager // 可为 nil（只读快照来源）
	Alerts         *AlertManager          // 可为 nil（无告警接线时）
	LogBroadcaster *LogBroadcaster        // 可为 nil

	// Coord 配置变更协调器（Build6 §12.4 完整形态）。未显式注入时按需惰性创建：
	// createRuntime 为 false 时只做纯写入，不做运行时发布（最小接线/测试场景）。
	Coord     *ConfigCoordinator
	coordOnce sync.Once
	// createRuntime 标记惰性协调器是否应构造并发布运行时状态。
	createRuntime bool

	// Health 运行健康来源（Build7 Step 4）：唯一 OperationalHealth 计算源，
	// 同时提供配置保存后的唤醒入口。可为 nil（未接线时 operational 端点返回 503）。
	Health OperationalHealthSource

	// Push 是 Uptime Kuma Push 心跳循环的唤醒入口（Build7 Step 5）；可为 nil（未接线）。
	// 配置保存在 commit 后唤醒它一次，使其立即按新配置首发或停止。
	Push PushWaker

	// ShutdownCh 服务器级 shutdown 信号：由 webui.Server 拥有并关闭，
	// 两类 SSE handler（/api/sync/events、/api/logs/stream）据此主动退出；
	// 只读、永不写入，handler 退出后由既有 defer unsubscribe() 取消订阅。
	ShutdownCh <-chan struct{}
}

// SetRuntimeWiring 注入运行时状态与告警管理器（run.go 在启动时调用一次）。
//
// 同时使惰性创建的协调器具备「事务内构造候选 + commit 后无失败发布」的能力。
func (d *Deps) SetRuntimeWiring(runtime *syncer.RuntimeManager, alerts *AlertManager) {
	d.Runtime = runtime
	d.Alerts = alerts
	d.createRuntime = runtime != nil
}

// coordinator 返回配置变更协调器；未注入时惰性创建（测试与最小接线场景）。
func (d *Deps) coordinator() *ConfigCoordinator {
	d.coordOnce.Do(func() {
		if d.Coord != nil {
			return
		}
		if !d.createRuntime && d.Runtime == nil {
			// 无运行时接线：只提交事务，不做发布
			d.Coord = NewConfigCoordinator(d.Store, nil, nil)
			return
		}
		d.Coord = NewConfigCoordinator(d.Store, d.buildCandidate, d.applyCandidate)
	})
	return d.Coord
}

// buildCandidate 在事务内构造候选运行时状态与候选告警集合。
//
// 只构造本地对象与 SDK client，不访问云 API、DNS 上游、SMTP、Webhook
// 或任何其他外部网络（Build6 §12.4）。policy 由协调器给定：普通变更
// BreakerPreserve 保留既有 DNS 熔断失败计数；完整导入 BreakerReset 清空
// （Build6 §12.3 第 7 条、Issue6 A8）。
func (d *Deps) buildCandidate(snapshot *config.BusinessSnapshot, policy syncer.BreakerPolicy) (Candidate, error) {
	rc := snapshot.ToRuntimeConfig()

	var previous *syncer.RuntimeState
	if d.Runtime != nil {
		previous = d.Runtime.Snapshot()
	}
	state, err := syncer.BuildRuntimeState(previous, rc, policy)
	if err != nil {
		// 未注册云类型属请求侧错误；其余构造失败按内部错误处理
		if errors.Is(err, syncer.ErrUnknownCloudType) {
			return Candidate{}, fmt.Errorf("%w: %v", ErrRuntimeCandidateUnsupported, err)
		}
		return Candidate{}, fmt.Errorf("%w: %v", ErrInternalBuild, err)
	}
	return Candidate{State: state, Alerts: BuildAlertSet(rc)}, nil
}

// applyCandidate 在 commit 之后按固定顺序执行无失败发布：
// 日志级别 → 告警集合 → RuntimeState → 运行健康监督器唤醒 → Uptime Kuma Push 唤醒。
//
// 该函数不返回 error：commit 之后不得再存在可失败出口，否则会出现
// “接口报错但数据库已经改变”（Build6 §3.5）。内部全部为无返回的 setter/锁内替换。
// 任何取到新运行时状态的操作都必然已经看到新日志级别与新告警集合。
func (d *Deps) applyCandidate(candidate Candidate) {
	if candidate.State == nil {
		return
	}
	level := candidate.State.Config.LogLevel

	// 1) 日志级别（stdout 与 WebUI 日志流保持同一级别）
	app.SetLogLevel(level)
	if d.LogBroadcaster != nil {
		d.LogBroadcaster.SetLevel(app.ParseLogLevel(level))
	} else {
		slog.Warn("日志流不可用，仅更新 stdout 日志级别")
	}

	// 2) 告警集合（取消旧订阅 → 安装新订阅）
	if d.Alerts != nil {
		d.Alerts.Apply(candidate.Alerts)
	}

	// 3) 先发布完整 RuntimeState，再唤醒读取共享快照的 Health/Push，避免消费旧配置。
	if d.Syncer != nil {
		// Syncer 在收到新状态后按固定顺序重新读取生效中的告警集合并输出安全日志
		d.Syncer.ApplyState(candidate.State)
	} else {
		if d.Runtime != nil {
			d.Runtime.Apply(candidate.State)
		}
		if d.Alerts != nil {
			d.Alerts.LogStatus("已更新")
		}
	}

	// 4) 运行健康监督配置：唤醒一次检查，使新策略（health_timeout 与第三开关）
	//    立即生效，而不必等待最长 30 秒的周期（Build7 §7.3）。
	if d.Health != nil {
		d.Health.Wake()
	}

	// 5) Uptime Kuma Push 配置：唤醒心跳循环，使其按新 URL/interval 立即首发或停止。
	if d.Push != nil {
		d.Push.Wake()
	}
}

// runtimeSnapshot 返回当前完整运行时状态的共享指针。
//
// 连接测试与资源扫描在请求开始时各取一次，并在该请求全程只使用这一份快照；
// 快照发布后不可修改，因此并发状态替换不会影响正在执行的请求。
func (d *Deps) runtimeSnapshot() *syncer.RuntimeState {
	if d.Runtime == nil {
		return nil
	}
	return d.Runtime.Snapshot()
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
	// 运行健康（Build7 Step 4）：/api/health 仍由服务器注册且保持静态存活语义
	mux.HandleFunc("GET /api/health/operational", d.handleOperationalHealth)
	// 实时日志流
	mux.HandleFunc("GET /api/logs/stream", d.handleLogStream)
	// 设置 + 配置导入导出
	mux.HandleFunc("GET /api/settings", d.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", d.handlePutSettings)
	mux.HandleFunc("POST /api/config/reset", d.handleConfigReset)
	// 配置导出固定为 POST（旧 GET 端点已删除，Build6 §3.3）
	mux.HandleFunc("POST /api/config/export", d.handleConfigExport)
	mux.HandleFunc("POST /api/config/import", d.handleConfigImport)
	// 告警配置
	mux.HandleFunc("GET /api/alerts", d.handleGetAlerts)
	mux.HandleFunc("PUT /api/alerts", d.handlePutAlerts)
	// 测试邮件（Build7 Step 2）：使用请求表单值，不写库、不 Apply、不改变订阅
	mux.HandleFunc("POST /api/alerts/test-email", d.handleTestEmail)
}

// writeJSON 写入 JSON 响应；普通 JSON API 的成功与错误响应统一禁止 HTTP 缓存。
//
// Issue6 A14：Encode 的返回值必须处理。响应头与状态码此时已发出，**不得**再伪造
// 第二个 HTTP 错误响应（参照 export.go 的既有先例），只记录安全日志——错误文本
// 可能包含响应内容片段，因此只记错误本身，不记 data。
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Warn("写出 JSON 响应失败", "status", status, "error", err)
	}
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
