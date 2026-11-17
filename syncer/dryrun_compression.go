package syncer

import (
	"sort"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件只构造 Dry Run 的压缩只读预览；不写库、不写缓存、不发事件。

// dryRunCompressionForUnit 按正式同一输入填充 execution_units[].compression。
//
// cctx 的 Plan 已由与正式相同的 buildCompressionPlan 计算。disabled 时再以
// 假设启用副本生成只读测算，实际计划仍保持关闭态。
func (s *Syncer) dryRunCompressionForUnit(
	state *RuntimeState,
	exec targetExecution,
	snapshot provider.RuleSnapshot,
	basePlan provider.TargetPlan,
	cctx *compressionContext,
	baseAddRows int,
	baseAddLimit int,
) DryRunCompression {
	out := emptyDryRunCompression()
	if cctx == nil {
		return out
	}
	out.Applicable = true
	out.Enabled = cctx.Enabled
	out.PendingReview = cctx.Pending.PendingReview
	out.PendingKnown = cctx.PendingKnown
	out.S0Rows = len(snapshot.Rules)
	out.OtherNewRows = baseAddRows
	out.NormalCap = 96
	out.Reserved = 4
	out.HardCap = 100
	out.RiskLimit = int64Ptr(cctx.Settings.ExtraIPv4Allow)
	if baseAddLimit >= 0 {
		out.NormalBudget = intPtr(max(0, baseAddLimit))
	}
	out.ResidualRows = dryRunCompressionResidualRows(snapshot, cctx)

	if !cctx.Enabled {
		out.Mode = provider.CompressionModeDisabled
		if baseAddLimit >= 0 {
			out.NormalBudget = intPtr(max(0, 100-len(snapshot.Rules)))
		}
		coverage := provider.CompressionCoverageFromRules(snapshot, cctx.EdgeOneTag, cctx.All,
			cctx.Input.Protected, cctx.Settings.ExtraIPv4Allow)
		if coverage.RiskKnown {
			out.ExtraIPv4 = uint64Ptr(coverage.ExtraIPv4)
			out.MaxExtraRun = uint64Ptr(coverage.MaxExtraRun)
			out.PerPort = append([]provider.CompressionPortRisk{}, coverage.PerPort...)
			out.ReservedRestored = boolPtr(len(snapshot.Rules) <= 96 && coverage.ExtraIPv4 == 0)
		} else {
			out.ReservedRestored = nil
		}
		if len(cctx.Eligible) > 0 {
			assumed := *cctx
			assumed.Enabled = true
			assumed.Settings.Enabled = true
			assumed.BaseSites = compressionBaseSites(exec.edgeSites, true)
			s.buildCompressionPlan(state, exec, &assumed, snapshot, baseAddRows)
			out.Assumed = true
			out.Mode = assumed.Plan.Capacity.Mode
			out.AddRows = len(assumed.compressionAddActions())
			out.Peak = intPtr(assumed.Plan.Capacity.Peak)
			out.Borrowed = intPtr(assumed.Plan.Capacity.Borrowed)
			out.ExpectedCleanup = intPtr(assumed.Plan.Capacity.ExpectedCleanupRows)
			out.ExpectedAfter = intPtr(assumed.Plan.Capacity.ExpectedAfter)
			out.NormalBudget = intPtr(assumed.Plan.Capacity.NormalBudget)
			out.RiskLimit = int64Ptr(assumed.Plan.Risk.Limit)
			out.Blockers = append([]provider.CompressionBlocker{}, assumed.Plan.Blockers...)
			out.Warnings = append([]provider.CompressionBlocker{}, assumed.Plan.Warnings...)
			out.PerPort = append([]provider.CompressionPortRisk{}, assumed.Plan.Risk.PerPort...)
			if assumed.Plan.Risk.Complete {
				out.ExtraIPv4 = uint64Ptr(assumed.Plan.Risk.ExtraIPv4)
				out.MaxExtraRun = uint64Ptr(assumed.Plan.Risk.MaxExtraRun)
			}
			out.Rows = dryRunCompressionRows(assumed.Plan.Selected)
		}
		return out
	}

	out.Mode = cctx.Plan.Capacity.Mode
	out.AddRows = len(cctx.compressionAddActions())
	out.Peak = intPtr(cctx.Plan.Capacity.Peak)
	out.Borrowed = intPtr(cctx.Plan.Capacity.Borrowed)
	out.ExpectedCleanup = intPtr(cctx.Plan.Capacity.ExpectedCleanupRows)
	out.ExpectedAfter = intPtr(cctx.Plan.Capacity.ExpectedAfter)
	out.NormalBudget = intPtr(cctx.Plan.Capacity.NormalBudget)
	out.RiskLimit = int64Ptr(cctx.Plan.Risk.Limit)
	out.Blockers = append([]provider.CompressionBlocker{}, cctx.Plan.Blockers...)
	out.Warnings = append([]provider.CompressionBlocker{}, cctx.Plan.Warnings...)
	out.PerPort = append([]provider.CompressionPortRisk{}, cctx.Plan.Risk.PerPort...)
	if cctx.Plan.Risk.Complete {
		out.ExtraIPv4 = uint64Ptr(cctx.Plan.Risk.ExtraIPv4)
		out.MaxExtraRun = uint64Ptr(cctx.Plan.Risk.MaxExtraRun)
	}
	out.Rows = dryRunCompressionRows(cctx.Plan.Selected)
	out.ReservedRestored = dryRunReservedRestored(snapshot, &cctx.Plan.Risk)
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

// dryRunCompressionResidualRows 统计当前快照中属于压缩范围的 EdgeOne 行。
func dryRunCompressionResidualRows(snapshot provider.RuleSnapshot, cctx *compressionContext) int {
	if cctx == nil {
		return 0
	}
	count := 0
	for _, rule := range snapshot.Rules {
		if compressionScopeRule(rule, cctx.EdgeOneTag) {
			count++
		}
	}
	return count
}

func dryRunReservedRestored(snapshot provider.RuleSnapshot, risk *provider.CompressionRisk) *bool {
	if risk != nil && !risk.Complete {
		return nil
	}
	restored := len(snapshot.Rules) <= 96
	if risk != nil {
		restored = restored && risk.ExtraIPv4 == 0
	}
	return boolPtr(restored)
}

// dryRunCompressionCandidates 返回压缩清理预览候选（只读，不证明可删）。
func dryRunCompressionCandidates(snapshot provider.RuleSnapshot, cctx *compressionContext, edgeTag string) []provider.RuleChange {
	out := []provider.RuleChange{}
	if cctx == nil {
		return out
	}
	retained := cctx.compressionSelectedRows()
	for _, rule := range snapshot.Rules {
		if !compressionScopeRule(rule, edgeTag) {
			continue
		}
		row := provider.CompressionRow{Protocol: strings.ToUpper(strings.TrimSpace(rule.Protocol)), Port: rule.Port, CIDR: rule.CidrBlock}
		skip := false
		for _, item := range retained {
			if sameCompressionPhysical(row, item) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		out = append(out, provider.RuleChangeFromInfo(rule))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cidr != out[j].Cidr {
			return out[i].Cidr < out[j].Cidr
		}
		if out[i].Protocol != out[j].Protocol {
			return out[i].Protocol < out[j].Protocol
		}
		return out[i].Port < out[j].Port
	})
	return out
}

func sameCompressionPhysical(a provider.CompressionRow, b provider.CompressionRow) bool {
	return a.Protocol == b.Protocol && a.Port == b.Port && a.CIDR == b.CIDR
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

// dryRunCompressionAwareRules 从 base 清理预览中剔除由压缩预览接管的行。
func dryRunCompressionAwareRules(rules []config.RuleInfo, cctx *compressionContext) []config.RuleInfo {
	if cctx == nil {
		return rules
	}
	out := make([]config.RuleInfo, 0, len(rules))
	for _, rule := range rules {
		if compressionScopeRule(rule, cctx.EdgeOneTag) {
			continue
		}
		out = append(out, rule)
	}
	return out
}
