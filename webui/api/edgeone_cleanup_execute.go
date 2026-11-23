package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// cleanupEngine 的任务仅由同步引擎 Run 执行，覆盖正式轮和 TEO 确认互斥。
type cleanupEngine interface {
	QueueMaintenance(func()) error
	MaintenanceAllowed() bool
	WaitMaintenanceCloud(config.CloudType)
	FinishMaintenanceCloud(config.CloudType)
	InvalidateTargetCoverage(string)
}

type cleanupConfirmRequest struct {
	PreviewID      string `json:"preview_id"`
	DisableEdgeOne *bool  `json:"disable_edgeone"`
}

// CleanupTargetResult 分开记录云确认计数和最终快照事实，空指针明确未知。
type CleanupTargetResult struct {
	TargetIDs        []int            `json:"target_ids"`
	CloudType        config.CloudType `json:"cloud_type"`
	Region           string           `json:"region"`
	ResourceID       string           `json:"resource_id"`
	Status           string           `json:"status"`
	ReasonCode       string           `json:"reason_code,omitempty"`
	AuthorizedRows   int              `json:"authorized_rows"`
	ConfirmedDeleted int              `json:"confirmed_deleted"`
	AbsentRows       *int             `json:"absent_rows"`
	RemainingRows    *int             `json:"remaining_rows"`
	ReadbackVerified bool             `json:"readback_verified"`
	OutcomeUnknown   bool             `json:"outcome_unknown"`
	RequestsSent     int              `json:"requests_sent"`
}

// CleanupOperation 进程内任务结果，重启后通过新预览重建事实，不自动续删。
type CleanupOperation struct {
	OperationID    string                `json:"operation_id"`
	PreviewID      string                `json:"preview_id"`
	Tag            string                `json:"tag"`
	Status         string                `json:"status"`
	ReasonCode     string                `json:"reason_code,omitempty"`
	DisableEdgeOne bool                  `json:"disable_edgeone"`
	SwitchAction   string                `json:"switch_action"`
	CreatedAt      time.Time             `json:"created_at"`
	FinishedAt     *time.Time            `json:"finished_at"`
	Cleared        bool                  `json:"cleared"`
	Targets        []CleanupTargetResult `json:"targets"`
}

type cleanupOperations struct {
	mu        sync.Mutex
	byID      map[string]CleanupOperation
	byPreview map[string]string
}

func (d *Deps) cleanupOperationRegistry() *cleanupOperations {
	d.cleanupOperationsOnce.Do(func() {
		d.cleanupOperations = &cleanupOperations{byID: make(map[string]CleanupOperation), byPreview: make(map[string]string)}
	})
	return d.cleanupOperations
}

func copyCleanupOperation(op CleanupOperation) CleanupOperation {
	op.Targets = append([]CleanupTargetResult{}, op.Targets...)
	for i := range op.Targets {
		op.Targets[i].TargetIDs = append([]int{}, op.Targets[i].TargetIDs...)
	}
	return op
}

func (ops *cleanupOperations) save(op CleanupOperation) {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	ops.byID[op.OperationID] = copyCleanupOperation(op)
}

func (d *Deps) handleEdgeOneCleanupConfirm(w http.ResponseWriter, r *http.Request) {
	var req cleanupConfirmRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	if strings.TrimSpace(req.PreviewID) == "" || req.DisableEdgeOne == nil {
		writeRequestError(w, badRequest("必须提供 preview_id 和明确的 disable_edgeone 布尔值"))
		return
	}
	status, response := d.acceptCleanup(req)
	writeJSON(w, status, response)
}

// acceptCleanup 串行关联预览与任务，响应写出时已释放任务锁。
func (d *Deps) acceptCleanup(req cleanupConfirmRequest) (int, any) {
	ops := d.cleanupOperationRegistry()
	ops.mu.Lock()
	defer ops.mu.Unlock()
	if id, ok := ops.byPreview[req.PreviewID]; ok {
		return http.StatusOK, copyCleanupOperation(ops.byID[id])
	}
	engine, ok := d.Syncer.(cleanupEngine)
	if !ok || !engine.MaintenanceAllowed() || d.cleanupShuttingDown() {
		return http.StatusServiceUnavailable, map[string]string{"error": "清理引擎不可用", "reason_code": "engine_unavailable"}
	}
	id, err := newCleanupPreviewID()
	if err != nil {
		slog.Error("生成操作标识失败", "error", err)
		return http.StatusInternalServerError, map[string]string{"error": "生成操作标识失败"}
	}
	var record CleanupPreviewRecord
	var reason string
	d.coordinator().WithStableConfig(func() bool {
		record, reason = d.cleanupPreviewRegistry().Take(req.PreviewID, d.runtimeSnapshot())
		return reason == ""
	})
	if reason != "" {
		return http.StatusConflict, map[string]string{"error": "请重新生成清理预览", "reason_code": reason}
	}
	op := CleanupOperation{OperationID: id, PreviewID: record.ID, Tag: record.Tag, Status: "queued", DisableEdgeOne: *req.DisableEdgeOne, SwitchAction: "unchanged", CreatedAt: time.Now(), Targets: []CleanupTargetResult{}}
	// 终态保留一小时；活动任务从不被清除。
	for key, previous := range ops.byID {
		if previous.FinishedAt != nil && time.Since(*previous.FinishedAt) > cleanupPreviewRetention {
			delete(ops.byPreview, previous.PreviewID)
			delete(ops.byID, key)
		}
	}
	ops.byID[id] = op
	ops.byPreview[record.ID] = id
	if err := engine.QueueMaintenance(func() { d.executeCleanup(engine, record, op) }); err != nil {
		op.Status = "rejected"
		op.ReasonCode = "engine_unavailable"
		for _, target := range record.Targets[len(op.Targets):] {
			op.Targets = append(op.Targets, CleanupTargetResult{TargetIDs: target.TargetIDs, CloudType: target.CloudType, Region: target.Region, ResourceID: target.ResourceID, Status: "unverified", ReasonCode: op.ReasonCode, AuthorizedRows: len(cleanupAuthorized(target))})
		}
		now := time.Now()
		op.FinishedAt = &now
		ops.byID[id] = op
		return http.StatusServiceUnavailable, op
	}
	return http.StatusAccepted, op
}

func (d *Deps) handleEdgeOneCleanupResult(w http.ResponseWriter, r *http.Request) {
	ops := d.cleanupOperationRegistry()
	ops.mu.Lock()
	op, ok := ops.byID[r.PathValue("operation_id")]
	op = copyCleanupOperation(op)
	ops.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "清理操作不存在，请重新预览")
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (d *Deps) cleanupShuttingDown() bool {
	select {
	case <-d.ShutdownCh:
		return true
	default:
		return false
	}
}

// cleanupRecordReason 必须在协调器稳定区内调用，只检查本地不可变配置。
func (d *Deps) cleanupRecordReason(engine cleanupEngine, record CleanupPreviewRecord) string {
	if !engine.MaintenanceAllowed() || d.cleanupShuttingDown() {
		return "service_stopping"
	}
	if !d.cleanupPreviewRegistry().now().Before(record.ExpiresAt) {
		return CleanupPreviewExpired
	}
	if _, reason := d.cleanupPreviewRegistry().Lookup(record.ID); reason == CleanupPreviewInvalidated {
		return reason
	}
	state := d.runtimeSnapshot()
	if record.Scope == cleanupScopeAll {
		ids, err := cleanupAllTargetIDs(state)
		if err != nil || !slices.Equal(ids, record.SelectedTargetIDs) {
			d.cleanupPreviewRegistry().Invalidate(record.ID)
			return CleanupPreviewConfigChanged
		}
	}
	digest, _, err := cleanupScopeDigest(state, record.SelectedTargetIDs)
	if err != nil || digest != record.Digest {
		d.cleanupPreviewRegistry().Invalidate(record.ID)
		return CleanupPreviewConfigChanged
	}
	return ""
}

func (d *Deps) cleanupAdmission(engine cleanupEngine, record CleanupPreviewRecord) string {
	reason := ""
	d.coordinator().WithStableConfig(func() bool { reason = d.cleanupRecordReason(engine, record); return reason == "" })
	return reason
}

func cleanupAuthorized(target CleanupPreviewTarget) []config.RuleInfo {
	rows := []config.RuleInfo{}
	if target.Status != cleanupTargetRead {
		return rows
	}
	for _, row := range target.Rows {
		if row.Status == provider.FirewallCleanupDeletable {
			rows = append(rows, row.Rule)
		}
	}
	return rows
}

func (d *Deps) executeCleanup(engine cleanupEngine, record CleanupPreviewRecord, op CleanupOperation) {
	ops := d.cleanupOperationRegistry()
	op.Status = "running"
	ops.save(op)
	defer func() {
		for _, target := range record.Targets[len(op.Targets):] {
			op.Targets = append(op.Targets, CleanupTargetResult{TargetIDs: target.TargetIDs, CloudType: target.CloudType, Region: target.Region, ResourceID: target.ResourceID, Status: "unverified", ReasonCode: op.ReasonCode, AuthorizedRows: len(cleanupAuthorized(target))})
		}
		now := time.Now()
		op.FinishedAt = &now
		op.Status = "completed"
		op.Cleared = len(op.Targets) == len(record.Targets) && len(op.Targets) > 0
		for _, target := range op.Targets {
			if !target.ReadbackVerified || target.RemainingRows == nil || *target.RemainingRows != 0 {
				op.Cleared = false
			}
		}
		ops.save(op)
		slog.Info("EdgeOne TAG 手动清理完成", "operation_id", op.OperationID, "cleared", op.Cleared, "reason_code", op.ReasonCode)
	}()
	if reason := d.cleanupAdmission(engine, record); reason != "" {
		op.ReasonCode = reason
		return
	}
	if op.DisableEdgeOne {
		// 协调器事务先提交开关，失败则整个操作零删除。HTTP 取消不传播到已受理任务。
		err := d.coordinator().Mutate(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			if reason := d.cleanupRecordReason(engine, record); reason != "" {
				return fmt.Errorf("%s", reason)
			}
			return d.Store.SetSettingTx(ctx, tx, "edgeone_enabled", "false")
		})
		if err != nil {
			op.SwitchAction = "failed"
			op.ReasonCode = "disable_edgeone_failed"
			return
		}
		op.SwitchAction = "disabled"
		ops.save(op)
	}
	for _, target := range record.Targets {
		result := d.executeCleanupTarget(engine, record, target)
		op.Targets = append(op.Targets, result)
		ops.save(op)
		slog.Info("EdgeOne TAG 手动清理目标结果", "operation_id", op.OperationID, "cloud_type", result.CloudType, "region", result.Region, "resource_id", result.ResourceID, "confirmed_deleted", result.ConfirmedDeleted, "absent_rows", cleanupCountLogValue(result.AbsentRows), "remaining_rows", cleanupCountLogValue(result.RemainingRows), "reason_code", result.ReasonCode)
	}
}

func (d *Deps) executeCleanupTarget(engine cleanupEngine, record CleanupPreviewRecord, target CleanupPreviewTarget) CleanupTargetResult {
	authorized := cleanupAuthorized(target)
	out := CleanupTargetResult{TargetIDs: target.TargetIDs, CloudType: target.CloudType, Region: target.Region, ResourceID: target.ResourceID, Status: "unverified", AuthorizedRows: len(authorized)}
	if target.Status != cleanupTargetRead {
		out.ReasonCode = "preview_target_unread"
		return out
	}
	engine.WaitMaintenanceCloud(target.CloudType)
	defer engine.FinishMaintenanceCloud(target.CloudType)
	if reason := d.cleanupAdmission(engine, record); reason != "" {
		out.ReasonCode = reason
		return out
	}
	p, ok := cleanupProviderForTargets(d.runtimeSnapshot(), target.TargetIDs)
	if !ok || p.CloudType() != target.CloudType {
		out.ReasonCode = "provider_missing"
		return out
	}
	snapshot, err := p.GetSnapshot()
	if err != nil {
		out.ReasonCode = "snapshot_failed"
		return out
	}
	rows, _, err := provider.ValidateFirewallCleanup(target.CloudType, snapshot, record.Tag, target.Revision, authorized)
	if err != nil {
		out.ReasonCode = "snapshot_changed"
		d.cleanupReadback(&out, target, record.Tag, authorized, snapshot)
		return out
	}
	var unknownBatch []config.RuleInfo
	for len(rows) > 0 {
		n := len(rows)
		if target.CloudType == config.CloudAliECS {
			n = min(n, 100)
		}
		batch := rows[:n]
		if reason := d.cleanupAdmission(engine, record); reason != "" {
			out.ReasonCode = reason
			break
		}
		// 从准入到交给 SDK 之间不等待、不持配置锁或 SQL 事务。
		out.RequestsSent++
		identity := config.TargetIdentity(config.TargetConfig{CloudType: target.CloudType, Region: target.Region, ResourceID: target.ResourceID})
		engine.InvalidateTargetCoverage(identity)
		result, deleteErr := p.DeleteRules(snapshot, batch)
		out.ConfirmedDeleted += result.Deleted
		if deleteErr != nil && !isCleanupIdempotent(deleteErr) {
			out.ReasonCode = "delete_failed"
			requestID, unknown := provider.FirewallCleanupErrorInfo(deleteErr)
			out.OutcomeUnknown = unknown
			if unknown {
				unknownBatch = append([]config.RuleInfo{}, batch...)
			}
			slog.Warn("EdgeOne TAG 手动删除未确认", "preview_id", record.ID, "provider", p.Name(), "request_id", requestID, "outcome_unknown", unknown)
			break
		}
		rows = rows[n:]
		if len(rows) > 0 {
			engine.FinishMaintenanceCloud(target.CloudType)
			engine.WaitMaintenanceCloud(target.CloudType)
			if reason := d.cleanupAdmission(engine, record); reason != "" {
				out.ReasonCode = reason
				break
			}
			snapshot, err = p.GetSnapshot()
			if err != nil {
				out.ReasonCode = "batch_snapshot_failed"
				break
			}
			rows, _, err = provider.ValidateFirewallCleanup(target.CloudType, snapshot, record.Tag, target.Revision, rows)
			if err != nil {
				out.ReasonCode = "snapshot_changed"
				break
			}
		}
	}
	// 已发出的请求无论成功、失败或超时均回读；只读收尾不被 Stop/配置变化取消。
	readback, err := p.GetSnapshot()
	if err != nil {
		out.Status = "unverified"
		out.ReasonCode = "readback_failed"
		return out
	}
	if out.OutcomeUnknown {
		out.OutcomeUnknown = false
		for _, row := range unknownBatch {
			if cleanupRulePresent(target.CloudType, row, readback) {
				out.OutcomeUnknown = true
				break
			}
		}
	}
	d.cleanupReadback(&out, target, record.Tag, authorized, readback)
	return out
}

func (d *Deps) cleanupReadback(out *CleanupTargetResult, target CleanupPreviewTarget, tagValue string, authorized []config.RuleInfo, snapshot provider.RuleSnapshot) {
	if (target.CloudType == config.CloudTCLighthouse || target.CloudType == config.CloudTCCVM) && strings.TrimSpace(snapshot.Revision) == "" {
		return
	}
	assessment := provider.AssessFirewallCleanup(target.CloudType, snapshot, tagValue)
	remaining := assessment.MatchedCount()
	absent := 0
	for _, old := range authorized {
		if !cleanupRulePresent(target.CloudType, old, snapshot) {
			absent++
		}
	}

	out.RemainingRows = &remaining
	out.AbsentRows = &absent
	out.ReadbackVerified = true
	switch {
	case remaining == 0:
		out.Status = "cleared"
		out.OutcomeUnknown = false
	case out.OutcomeUnknown:
		out.Status = "unknown"
	default:
		out.Status = "remaining"
	}
}

// 幂等码沿用正式同步分类，不因消息内容扩大阿里云兼容集合。
func isCleanupIdempotent(err error) bool { return syncer.FirewallDeleteIsIdempotent(err) }

func cleanupCountLogValue(count *int) any {
	if count == nil {
		return "unknown"
	}
	return *count
}

// cleanupRulePresent 不以 CVM 的可漂移索引或腾讯描述变化推导物理规则已消失。
// 阿里云优先比较稳定 ID，缺 ID 的同值行仍保守视为可见。
func cleanupRulePresent(ct config.CloudType, old config.RuleInfo, snapshot provider.RuleSnapshot) bool {
	for _, current := range snapshot.Rules {
		if ct == config.CloudAliSWAS || ct == config.CloudAliECS {
			if strings.TrimSpace(current.RuleID) == strings.TrimSpace(old.RuleID) {
				return true
			}
			missingID := strings.TrimSpace(current.RuleID) == ""
			current.RuleID = old.RuleID
			if missingID && current == old {
				return true
			}
			continue
		}
		current.PolicyIndex = old.PolicyIndex
		current.Description = old.Description
		if current == old {
			return true
		}
	}
	return false
}
