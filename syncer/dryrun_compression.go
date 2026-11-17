package syncer

import (
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件只构造 Dry Run 的压缩只读预览；不写库、不写缓存、不发事件。

// dryRunCompressionForUnit 按正式同一输入填充 execution_units[].compression。
//
// cctx 的 Plan 已由与正式相同的 buildCompressionPlan 计算。disabled 时再以
// 假设启用副本生成只读测算，实际计划仍保持关闭态。
func dryRunCompressionForUnit(state *RuntimeState, exec targetExecution, baseInput provider.TargetPlanInput, cctx *compressionContext) DryRunCompression {
	out := emptyDryRunCompression()
	if cctx == nil {
		return out
	}
	snapshot := baseInput.Snapshot
	out.Applicable = true
	out.Enabled = cctx.Enabled
	out.PendingReview = cctx.Pending.PendingReview
	out.PendingKnown = cctx.PendingKnown
	out.S0Rows = len(snapshot.Rules)
	out.RiskLimit = int64Ptr(cctx.riskLimit())
	if cctx.Coverage.RiskKnown {
		out.CurrentExtraIPv4 = uint64Ptr(cctx.Coverage.ExtraIPv4)
		out.ExtraIPv4 = out.CurrentExtraIPv4
		out.MaxExtraRun = uint64Ptr(cctx.Coverage.MaxExtraRun)
		out.PerPort = append([]provider.CompressionPortRisk{}, cctx.Coverage.PerPort...)
	}
	out.ReservedRestored = boolPtr(len(snapshot.Rules) <= 96)
	current := cctx
	if !cctx.Enabled && len(cctx.Eligible) > 0 {
		out.Assumed = true
		assumed := *cctx
		assumed.Enabled = true
		if exec.edgeActive {
			assumed.BaseSites = compressionBaseSites(exec.edgeSites, true)
			baseInput.EdgeOneSites = edgeOneInputsForSites(assumed.BaseSites, exec.edgeData)
		}
		assumed.planAttempt(state, exec, snapshot, provider.PlanTarget(baseInput))
		current = &assumed
	}
	out.Mode = current.Plan.Capacity.Mode
	out.OtherNewRows = current.Plan.Capacity.OtherNewRows
	out.AddRows = len(current.compressionAddActions())
	out.RiskLimit = int64Ptr(current.riskLimit())
	out.Blockers = append([]provider.CompressionBlocker{}, current.Blockers...)
	out.Warnings = append([]provider.CompressionBlocker{}, current.Plan.Warnings...)
	out.Rows = dryRunCompressionRows(current.Selected)
	if !edgePreviewComplete(exec.edgeSites, exec.edgeData) || current.Plan.Incomplete ||
		current.Plan.Capacity.HardCap == 0 || strings.TrimSpace(snapshot.Revision) == "" {
		if !edgePreviewComplete(exec.edgeSites, exec.edgeData) && len(out.Blockers) == 0 {
			out.Blockers = append(out.Blockers, provider.CompressionBlocker{Code: provider.CompressionBlockedSnapshotIncomplete, Message: "站点输入不完整"})
		}
		return out
	}
	out.Peak = intPtr(current.Plan.Capacity.Peak)
	out.Borrowed = intPtr(current.Plan.Capacity.Borrowed)
	out.ExpectedCleanup = intPtr(current.Plan.Capacity.ExpectedCleanupRows)
	if current.Plan.Capacity.Mode != provider.CompressionModeBlocked || current.Plan.Capacity.ExpectedAfter > 0 {
		out.ExpectedAfter = intPtr(current.Plan.Capacity.ExpectedAfter)
	}
	out.NormalBudget = intPtr(current.Plan.Capacity.NormalBudget)
	if current.Plan.Risk.Complete {
		out.ExtraIPv4 = uint64Ptr(current.Plan.Risk.ExtraIPv4)
		out.MaxExtraRun = uint64Ptr(current.Plan.Risk.MaxExtraRun)
		out.PerPort = append([]provider.CompressionPortRisk{}, current.Plan.Risk.PerPort...)
	}
	return out
}

func dryRunCompressionRows(rep *provider.CompressionRepresentation) []DryRunCompressionRow {
	out := []DryRunCompressionRow{}
	if rep == nil {
		return out
	}
	for _, row := range rep.Rows {
		out = append(out, dryRunCompressionRow(row))
	}
	return out
}

// dryRunUnknown 在快照读取失败时返回非 null 的未知压缩骨架。
func (c *compressionContext) dryRunUnknown() DryRunCompression {
	out := emptyDryRunCompression()
	if c == nil {
		return out
	}
	out.Applicable = true
	out.Enabled = c.Enabled
	out.PendingReview = c.Pending.PendingReview
	out.PendingKnown = c.PendingKnown
	out.RiskLimit = int64Ptr(c.Settings.ExtraIPv4Allow)
	return out
}
