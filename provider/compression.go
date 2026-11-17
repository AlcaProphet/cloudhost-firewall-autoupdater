package provider

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
)

// 本文件是 Build9 Step 2 的 Lighthouse EdgeOne IPv4 策略压缩纯规划器。
//
// 纯函数：不访问网络、数据库、时钟或日志。正式同步与 Dry Run 后续共用同一输入/输出。

// 压缩规划稳定阻断原因码（Step 2 只产出结果，Step 3 决定目标 outcome）。
const (
	CompressionBlockedUnsupportedPlatform  = "unsupported_platform"
	CompressionBlockedSnapshotIncomplete   = "snapshot_incomplete"
	CompressionBlockedInvalidPort          = "invalid_port_expression"
	CompressionBlockedHardQuota            = "hard_quota_exceeded"
	CompressionBlockedCurrentAdd           = "current_add_exceeds_normal_budget"
	CompressionBlockedReserveNeedsPending  = "reserve_requires_pending"
	CompressionBlockedPendingExceedsExcess = "pending_increment_exceeds_excess"
	CompressionBlockedCleanupUntrusted     = "cleanup_evidence_untrusted"
	CompressionBlockedCleanupInsufficient  = "cleanup_insufficient"
	CompressionBlockedRiskLimit            = "risk_limit_exceeded"
	CompressionBlockedNoCandidate          = "no_compliant_candidate"
	CompressionBlockedLossyForbidden       = "lossy_forbidden"
	CompressionBlockedDNSIncomplete        = "dns_incomplete_lossy_blocked"
	CompressionBlockedDNSConflict          = "dns_conflict"
	CompressionBlockedDNSHit               = "dns_hit_no_candidate"
	CompressionBlockedOppositeAction       = "opposite_action_candidate"
	CompressionBlockedCanceled             = "planning_canceled"
	CompressionBlockedIncomplete           = "planning_incomplete"
)

const (
	CompressionModeDisabled    = "disabled"
	CompressionModeNormal      = "normal"
	CompressionModeBorrow      = "borrow"
	CompressionModeCleanupOnly = "cleanup_only"
	CompressionModeBlocked     = "blocked"
)

const (
	compressionNormalCap = 96
	compressionHardCap   = 100
	compressionReserved  = 4
)

// CompressionConfig 是一个物理 Lighthouse 实例的压缩开关与显式额度。
type CompressionConfig struct {
	Enabled        bool
	ExtraIPv4Limit int64
}

// CompressionAuthorization 保留原始站点、版本、CIDR、协议与端口来源。
type CompressionAuthorization struct {
	ZoneID      string
	Version     string
	VersionKind string // current / next / both
	Prefix      string
	Protocol    string
	PortItem    string
}

// CompressionRow 是一条计划中的 Lighthouse 物理行。
type CompressionRow struct {
	Protocol      string
	Port          string
	CIDR          string
	Action        string
	Description   string
	ServesCurrent bool
	ServesNext    bool
	Reused        bool
	Covered       []CompressionAuthorization
	Lossy         bool
}

// CompressionRepresentation 是一组物理行及其覆盖证明。
type CompressionRepresentation struct {
	Rows         []CompressionRow
	AddRows      []config.RuleAction
	ReusedRows   []config.RuleInfo
	Witnesses    []CompressionWitness
	CurrentAdds  int
	PendingExtra int
	ExtraIPv4    uint64
	MaxExtraRun  uint64
	Exact        bool
}

// CompressionWitness 记录一条原始授权由某条物理行覆盖的端口部分。
type CompressionWitness struct {
	Authorization CompressionAuthorization
	RowIndex      int
	Port          string
	Lossy         bool
}

// CompressionCapacity 是物理行预算与借用判定。
type CompressionCapacity struct {
	Mode                string
	S0Rows              int
	OtherNewRows        int
	AddRows             int
	CurrentAdds         int
	PendingExtra        int
	NormalCap           int
	Reserved            int
	HardCap             int
	NormalBudget        int
	Peak                int
	ExpectedCleanupRows int
	ExpectedAfter       int
	Borrowed            int
	Reasons             []CompressionBlocker
}

// CompressionPortRisk 是一个协议端口组的额外放行摘要。
type CompressionPortRisk struct {
	Protocol    string
	Port        string
	ExtraIPv4   uint64
	MaxExtraRun uint64
	Ranges      []string
}

// CompressionRisk 是整个实例的风险额度核算。
type CompressionRisk struct {
	Limit         int64
	ExtraIPv4     uint64
	MaxExtraRun   uint64
	PerPort       []CompressionPortRisk
	OldResidue    uint64
	ManualIgnored bool
	Complete      bool
}

// CompressionBlocker 是稳定、可展示的阻断原因。
type CompressionBlocker struct {
	Code     string
	Message  string
	ZoneID   string
	Protocol string
	Port     string
	Prefix   string
}

// LighthouseCompressionInput 是纯规划器的全部输入。
type LighthouseCompressionInput struct {
	CloudType     config.CloudType
	Config        CompressionConfig
	AssumeEnabled bool
	EdgeOneTag    string
	OtherTag      string
	Snapshot      RuleSnapshot
	Sites         []EdgeOneSitePlanInput

	OtherNewRows       int
	PendingFingerprint string
	// AssessCleanup 对同一个 S0 与传入候选模拟新版生效后的整批安全清理。
	// 必须是纯函数，且只读取候选。无证据时返回零值。
	AssessCleanup func(candidate CompressionRepresentation) CompressionCleanupEvidence
	// Protected 是本轮仍有效的逐协议端口过渡授权，仅用于峰值风险计算。
	Protected []CompressionAuthorization
	// PostActivationProtected comes from the same transition rules as the live round.
	PostActivationProtected []CompressionAuthorization
	CleanupKeepKeys         []FunctionalKey

	DNSEnabled   bool
	DNSAddresses []netip.Addr
	DNSComplete  bool
	DNSConflict  bool

	MaxStates int
}

// CompressionCleanupEvidence 绑定本轮 S0 和待更新版本，行号为 S0.Rules 下标。
type CompressionCleanupEvidence struct {
	SnapshotRevision   string
	PendingFingerprint string
	ReleasableRows     []int
}

// LighthouseCompressionPlan 是纯规划器输出。
type LighthouseCompressionPlan struct {
	Enabled    bool
	Assumed    bool
	Lossless   CompressionRepresentation
	Lossy      *CompressionRepresentation
	Selected   *CompressionRepresentation
	Capacity   CompressionCapacity
	Risk       CompressionRisk
	Blockers   []CompressionBlocker
	Warnings   []CompressionBlocker
	Incomplete bool
}

// PlanLighthouseCompression 计算一个物理 Lighthouse 实例的压缩计划。
func PlanLighthouseCompression(ctx context.Context, in LighthouseCompressionInput) LighthouseCompressionPlan {
	plan := LighthouseCompressionPlan{Enabled: in.Config.Enabled, Assumed: in.AssumeEnabled}
	if !in.Config.Enabled && !in.AssumeEnabled {
		plan.Capacity = CompressionCapacity{Mode: CompressionModeDisabled, NormalCap: compressionNormalCap, Reserved: compressionReserved, HardCap: compressionHardCap}
		return plan
	}
	if in.CloudType != "" && in.CloudType != config.CloudTCLighthouse {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedUnsupportedPlatform, Message: "策略压缩仅支持腾讯云 Lighthouse"})
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	if in.Config.ExtraIPv4Limit < 0 || in.Config.ExtraIPv4Limit > config.MaxLighthouseExtraIPv4 {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedRiskLimit, Message: "额外放行 IPv4 额度必须是 0～2^32 的整数"})
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	if in.DNSConflict {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedDNSConflict, Message: "DNS 地址落入 EdgeOne 回源网段，正式同步已阻断"})
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	if strings.TrimSpace(in.Snapshot.Revision) == "" {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "Lighthouse S0 缺少 FirewallVersion"})
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	if ctx != nil && ctx.Err() != nil {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedCanceled, Message: "规划被取消"})
		plan.Incomplete = true
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}

	auths, blockers, warnings := buildCompressionAuthorizations(in, nil)
	plan.Blockers = append(plan.Blockers, blockers...)
	plan.Warnings = append(plan.Warnings, warnings...)
	if len(plan.Blockers) > 0 {
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	hasAuthorizations := len(auths) > 0
	planner := newCompressionPlanner(in, auths)
	external := compressionExternalKeys(in.Snapshot, in.EdgeOneTag, in.OtherTag)
	if len(external) > 0 {
		auths, _, _ = buildCompressionAuthorizations(in, external)
	}
	var exactRows []CompressionRow
	for _, group := range auths {
		for _, prefix := range group.prefixes {
			exactRows = append(exactRows, planner.rows(group, prefix)...)
		}
	}
	plan.Lossless = planner.representation(exactRows)
	plan.Capacity = planner.capacity(plan.Lossless)
	plan.Risk = planner.risk.summarize(in.Config.ExtraIPv4Limit, plan.Lossless)
	if ctx != nil && ctx.Err() != nil {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedCanceled, Message: "规划被取消"})
		plan.Incomplete = true
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	plan.Blockers = append(plan.Blockers, compressionDropConflicts(in.Snapshot.Rules, in.Sites)...)
	plan.Blockers = append(plan.Blockers, compressionCrossTagConflicts(in)...)
	if len(plan.Blockers) > 0 {
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	opposite := planner.hitsDrop(exactRows)
	if plan.Risk.ExtraIPv4 > uint64(in.Config.ExtraIPv4Limit) {
		// Recovery may replace existing coverage within normal capacity.
		// S1 checks the actual peak sets and S2 determines eligibility for confirmation.
		if !opposite && planner.risk.oldExtra.count() > uint64(in.Config.ExtraIPv4Limit) {
			if (plan.Capacity.Mode == CompressionModeNormal || plan.Capacity.Mode == CompressionModeCleanupOnly) &&
				!planner.risk.expandsExtra(plan.Lossless) {
				plan.Selected = &plan.Lossless
				return plan
			}
			if len(auths) > 0 && in.Config.ExtraIPv4Limit > 0 {
				rep, capacity, incomplete, blockers := planner.planLossy(ctx, auths)
				if rep != nil {
					plan.Lossy, plan.Selected = rep, rep
					plan.Capacity = capacity
					plan.Risk = planner.risk.summarize(in.Config.ExtraIPv4Limit, *rep)
					return plan
				}
				plan.Incomplete = incomplete
				plan.Blockers = append(plan.Blockers, blockers...)
			}
		}
		plan.Blockers = append(plan.Blockers, CompressionBlocker{
			Code:    CompressionBlockedRiskLimit,
			Message: fmt.Sprintf("额外放行 %d 个 IPv4，超过额度 %d", plan.Risk.ExtraIPv4, in.Config.ExtraIPv4Limit),
		})
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}

	if !hasAuthorizations {
		plan.Blockers = append(plan.Blockers, plan.Capacity.Reasons...)
		return plan
	}
	if !opposite && plan.Capacity.Mode != CompressionModeBlocked {
		plan.Selected = &plan.Lossless
		return plan
	}

	// 仅无损容量不足时考虑扩大授权范围。
	if opposite && plan.Capacity.Mode != CompressionModeBlocked {
		plan.Blockers = append(plan.Blockers, compressionOppositeAction)
	} else if len(auths) == 0 {
		plan.Blockers = append(plan.Blockers, plan.Capacity.Reasons...)
	} else if in.Config.ExtraIPv4Limit == 0 {
		plan.Blockers = append(plan.Blockers, plan.Capacity.Reasons...)
		plan.Blockers = append(plan.Blockers, CompressionBlocker{
			Code: CompressionBlockedLossyForbidden, Message: "额外放行额度为 0，只允许无损压缩",
		})
	} else {
		rep, capacity, incomplete, blockers := planner.planLossy(ctx, auths)
		if rep != nil {
			plan.Lossy, plan.Selected = rep, rep
			plan.Capacity = capacity
			plan.Risk = planner.risk.summarize(in.Config.ExtraIPv4Limit, *rep)
			return plan
		}
		plan.Incomplete = incomplete
		plan.Blockers = append(plan.Blockers, blockers...)
		if !incomplete {
			plan.Blockers = append(plan.Blockers, plan.Capacity.Reasons...)
		}
	}
	plan.Capacity.Mode = CompressionModeBlocked
	return plan
}

// CompressionCapacityInput 是容量判定的纯输入。
type CompressionCapacityInput struct {
	S0Rows              int
	OtherNewRows        int
	CurrentAdds         int
	PendingExtra        int
	HasPendingVersion   bool
	PendingFingerprint  string
	ExpectedCleanupRows int
	CleanupFingerprint  string
	CleanupTrusted      bool
}

// EvaluateCompressionCapacity 按 Build9 冻结规则计算 normal/borrow/blocked。
//
// “清理后 ≤ 原有占用 − 4”按固定 100 条硬上限解释为 ≤96（100−4）；
// 仅 pending 增量可借用，服务当前版的新增不得借用。
func EvaluateCompressionCapacity(in CompressionCapacityInput) CompressionCapacity {
	out := CompressionCapacity{
		Mode:                CompressionModeNormal,
		S0Rows:              in.S0Rows,
		OtherNewRows:        in.OtherNewRows,
		CurrentAdds:         in.CurrentAdds,
		PendingExtra:        in.PendingExtra,
		AddRows:             in.CurrentAdds + in.PendingExtra,
		NormalCap:           compressionNormalCap,
		Reserved:            compressionReserved,
		HardCap:             compressionHardCap,
		NormalBudget:        max(0, compressionNormalCap-in.S0Rows-in.OtherNewRows),
		ExpectedCleanupRows: in.ExpectedCleanupRows,
	}
	out.Peak = in.S0Rows + in.OtherNewRows + out.AddRows
	if in.OtherNewRows == 0 && out.AddRows == 0 {
		if in.S0Rows > compressionNormalCap {
			out.Mode = CompressionModeCleanupOnly
			out.Reasons = append(out.Reasons, CompressionBlocker{Code: "existing_over_normal_cap", Message: "实例已超过常态 96 条上限，只允许清理，不允许普通新增"})
		}
		out.ExpectedAfter = out.Peak
		return out
	}
	if out.Peak > compressionHardCap {
		out.Mode = CompressionModeBlocked
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedHardQuota, Message: fmt.Sprintf("写入峰值 %d 超过硬上限 %d", out.Peak, compressionHardCap)})
		return out
	}
	if in.OtherNewRows > max(0, compressionNormalCap-in.S0Rows) {
		out.Mode = CompressionModeBlocked
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedCurrentAdd, Message: "DDNS 与其他来源新增超过常态预算"})
		return out
	}
	if out.AddRows <= out.NormalBudget {
		out.ExpectedAfter = out.Peak - in.ExpectedCleanupRows
		if out.ExpectedAfter < 0 {
			out.ExpectedAfter = 0
		}
		return out
	}
	if in.CurrentAdds > out.NormalBudget {
		out.Mode = CompressionModeBlocked
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedCurrentAdd, Message: fmt.Sprintf("服务当前版新增 %d 条超过常态预算 %d 条", in.CurrentAdds, out.NormalBudget)})
		return out
	}
	if !in.HasPendingVersion {
		out.Mode = CompressionModeBlocked
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedReserveNeedsPending, Message: "常态预算不足且没有可观察的待更新版，不能借用 4 条预留"})
		return out
	}
	excess := out.Peak - compressionNormalCap
	if in.PendingExtra < excess {
		out.Mode = CompressionModeBlocked
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedPendingExceedsExcess, Message: "借用需求超过仅为待更新版覆盖所需的增量"})
		return out
	}
	if !in.CleanupTrusted || strings.TrimSpace(in.PendingFingerprint) == "" ||
		in.CleanupFingerprint != in.PendingFingerprint || in.ExpectedCleanupRows < 0 || in.ExpectedCleanupRows > in.S0Rows {
		out.Mode = CompressionModeBlocked
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedCleanupUntrusted, Message: "预计安全清理证据不完整或与待更新指纹不一致"})
		return out
	}
	out.ExpectedAfter = out.Peak - in.ExpectedCleanupRows
	if out.ExpectedAfter > compressionNormalCap {
		out.Mode = CompressionModeBlocked
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedCleanupInsufficient, Message: fmt.Sprintf("预计安全清理后仍有 %d 条，超过常态上限 %d", out.ExpectedAfter, compressionNormalCap)})
		return out
	}
	out.Mode = CompressionModeBorrow
	out.Borrowed = excess
	return out
}

type compressionDrop struct {
	protocol string
	port     compressionPortSpan
	address  ipv4Range
}

type compressionPlanner struct {
	in       LighthouseCompressionInput
	risk     compressionRiskModel
	owned    map[string]config.RuleInfo
	drops    []compressionDrop
	locators map[FunctionalKey]int
	pending  bool
}

func newCompressionPlanner(in LighthouseCompressionInput, groups []compressionAuthGroup) compressionPlanner {
	p := compressionPlanner{in: in, risk: buildCompressionRiskModel(in, groups), owned: make(map[string]config.RuleInfo), drops: compressionDrops(in.Snapshot.Rules)}
	for _, site := range in.Sites {
		p.pending = p.pending || site.Snapshot.Next != nil
	}
	if p.pending && in.AssessCleanup != nil {
		p.locators = make(map[FunctionalKey]int)
	}
	for _, rule := range in.Snapshot.Rules {
		if p.locators != nil {
			keys, _ := SnapshotRuleKeys(rule)
			for _, key := range keys {
				p.locators[key]++
			}
		}
		cidr, ok := CanonicalCIDR(rule.CidrBlock)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(rule.Action), "ACCEPT") && tag.IsOwned(rule.Description, in.EdgeOneTag) {
			key := compressionReuseKey(rule.Protocol, rule.Port, cidr)
			if previous, found := p.owned[key]; !found || compareCompressionRules(rule, previous) < 0 {
				p.owned[key] = rule
			}
		}
	}
	return p
}

var compressionOppositeAction = CompressionBlocker{
	Code: CompressionBlockedOppositeAction, Message: "候选与 S0 的相反动作重叠",
}

func compressionDrops(rules []config.RuleInfo) []compressionDrop {
	var drops []compressionDrop
	for _, rule := range rules {
		if !strings.EqualFold(strings.TrimSpace(rule.Action), "DROP") {
			continue
		}
		addresses, ports := compressionRuleGrant(rule)
		protocols := compressionProtocols(rule.Protocol)
		if canonicalProtocol(rule.Protocol) == "ALL" {
			protocols = []string{"TCP", "UDP", "ICMP"}
			ports = []compressionPortSpan{allCompressionPorts}
		} else if canonicalProtocol(rule.Protocol) == "ICMP" {
			protocols = []string{"ICMP"}
			ports = []compressionPortSpan{allCompressionPorts}
		}
		for _, protocol := range protocols {
			for _, port := range ports {
				for _, address := range addresses {
					drops = append(drops, compressionDrop{protocol, port, address})
				}
			}
		}
	}
	return drops
}

func compressionHitsDrop(drops []compressionDrop, protocol, cidr, port string) bool {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	address, ok := prefixIPv4Range(prefix)
	if !ok {
		return false
	}
	ports, _ := compressionPortSpans(port)
	for _, drop := range drops {
		if protocol != drop.protocol || address.Start > drop.address.End || drop.address.Start > address.End {
			continue
		}
		for _, span := range ports {
			if span.overlaps(drop.port) {
				return true
			}
		}
	}
	return false
}

func (p compressionPlanner) hitsDrop(rows []CompressionRow) bool {
	for _, row := range rows {
		if compressionHitsDrop(p.drops, row.Protocol, row.CIDR, row.Port) {
			return true
		}
	}
	return false
}

func (p compressionPlanner) rows(group compressionAuthGroup, prefix netip.Prefix) []CompressionRow {
	address, _ := prefixIPv4Range(prefix)
	cidr := prefix.String()
	rows := make([]CompressionRow, 0, len(group.ports))
	for _, port := range group.ports {
		row := CompressionRow{
			Protocol: port.protocol, Port: port.expression, CIDR: cidr, Action: "ACCEPT",
			Description: RenderDescription(config.CloudTCLighthouse, p.in.EdgeOneTag, ""),
			Lossy:       !group.addresses.containsPrefix(prefix),
		}
		for _, source := range group.sources {
			sourceRange := source.grant.addresses[0]
			if source.ref.Protocol != port.protocol || address.Start > sourceRange.Start || address.End < sourceRange.End {
				continue
			}
			for _, span := range port.spans {
				if span.overlaps(source.grant.ports) {
					row.Covered = append(row.Covered, source.ref)
					row.ServesCurrent = row.ServesCurrent || source.ref.VersionKind == "current"
					row.ServesNext = row.ServesNext || source.ref.VersionKind == "next"
					break
				}
			}
		}
		sort.Slice(row.Covered, func(i, j int) bool {
			a, b := row.Covered[i], row.Covered[j]
			if a.Prefix != b.Prefix {
				return a.Prefix < b.Prefix
			}
			if a.PortItem != b.PortItem {
				return a.PortItem < b.PortItem
			}
			if a.ZoneID != b.ZoneID {
				return a.ZoneID < b.ZoneID
			}
			if a.Version != b.Version {
				return a.Version < b.Version
			}
			return a.VersionKind < b.VersionKind
		})
		_, row.Reused = p.owned[compressionReuseKey(row.Protocol, row.Port, row.CIDR)]
		rows = append(rows, row)
	}
	return rows
}

func (p compressionPlanner) representation(rows []CompressionRow) CompressionRepresentation {
	rep := CompressionRepresentation{
		Rows:    sortCompressionRows(append([]CompressionRow{}, rows...)),
		AddRows: []config.RuleAction{}, ReusedRows: []config.RuleInfo{}, Witnesses: []CompressionWitness{},
		Exact: true,
	}
	var extra ipv4Set
	for i, row := range rep.Rows {
		rep.Exact = rep.Exact && !row.Lossy
		spans, _ := compressionPortSpans(row.Port)
		if row.Lossy {
			addresses := ipv4SetFromPrefixes([]netip.Prefix{netip.MustParsePrefix(row.CIDR)})
			extra = extra.union(p.risk.candidateExtra(compressionWirePort{row.Protocol, row.Port, spans}, addresses))
		}
		for _, ref := range row.Covered {
			original, _ := compressionPortSpans(ref.PortItem)
			var covered []string
			for _, span := range spans {
				if span.overlaps(original[0]) {
					part := compressionPortSpan{max(span.first, original[0].first), min(span.last, original[0].last)}
					covered = append(covered, part.String())
				}
			}
			rep.Witnesses = append(rep.Witnesses, CompressionWitness{
				Authorization: ref, RowIndex: i, Port: strings.Join(covered, ","), Lossy: row.Lossy,
			})
		}
		if row.Reused {
			rep.ReusedRows = append(rep.ReusedRows, p.owned[compressionReuseKey(row.Protocol, row.Port, row.CIDR)])
			continue
		}
		rep.AddRows = append(rep.AddRows, config.RuleAction{
			Protocol: row.Protocol, Port: row.Port, CidrBlock: row.CIDR, Action: row.Action, Description: row.Description,
		})
		if row.ServesNext && !row.ServesCurrent {
			rep.PendingExtra++
		} else {
			rep.CurrentAdds++
		}
	}
	rep.ExtraIPv4, rep.MaxExtraRun = extra.count(), extra.maxRun()
	return rep
}

func (p compressionPlanner) capacity(rep CompressionRepresentation) CompressionCapacity {
	in := p.in
	input := CompressionCapacityInput{
		S0Rows: len(in.Snapshot.Rules), OtherNewRows: in.OtherNewRows,
		CurrentAdds: len(rep.AddRows) - rep.PendingExtra, PendingExtra: rep.PendingExtra,
		HasPendingVersion: p.pending, PendingFingerprint: in.PendingFingerprint,
	}
	capacity := EvaluateCompressionCapacity(input)
	if len(capacity.Reasons) == 0 || capacity.Reasons[0].Code != CompressionBlockedCleanupUntrusted ||
		strings.TrimSpace(in.PendingFingerprint) == "" || in.AssessCleanup == nil {
		return capacity
	}
	evidence := in.AssessCleanup(rep)
	if evidence.SnapshotRevision != in.Snapshot.Revision || evidence.PendingFingerprint != in.PendingFingerprint {
		return capacity
	}
	retained := make(map[config.RuleInfo]bool, len(rep.ReusedRows))
	for _, row := range rep.Rows {
		if row.Reused && row.ServesNext {
			retained[p.owned[compressionReuseKey(row.Protocol, row.Port, row.CIDR)]] = true
		}
	}
	seen := make(map[int]bool, len(evidence.ReleasableRows))
	for _, index := range evidence.ReleasableRows {
		if index < 0 || index >= len(in.Snapshot.Rules) || seen[index] {
			return capacity
		}
		rule := in.Snapshot.Rules[index]
		keys, known := SnapshotRuleKeys(rule)
		if !tag.IsOwned(rule.Description, in.EdgeOneTag) || retained[rule] || !known ||
			slices.ContainsFunc(keys, func(key FunctionalKey) bool { return p.locators[key] != 1 }) {
			return capacity
		}
		seen[index] = true
	}
	input.CleanupTrusted = true
	input.CleanupFingerprint = evidence.PendingFingerprint
	input.ExpectedCleanupRows = len(seen)
	return EvaluateCompressionCapacity(input)
}

// compressionReuseKey 只比较 IPv4 允许行的端口语义，ReusedRows 始终保留 S0 原字段。
func compressionReuseKey(protocol, port, cidr string) string {
	spans, ok := compressionPortSpans(port)
	if !ok {
		return strings.ToUpper(strings.TrimSpace(protocol)) + "|" + strings.TrimSpace(port) + "|" + cidr
	}
	slices.SortFunc(spans, func(a, b compressionPortSpan) int { return a.first - b.first })
	var merged []compressionPortSpan
	for _, span := range spans {
		if len(merged) == 0 || span.first > merged[len(merged)-1].last+1 {
			merged = append(merged, span)
		} else {
			last := &merged[len(merged)-1].last
			*last = max(*last, span.last)
		}
	}
	parts := make([]string, len(merged))
	for i, span := range merged {
		parts[i] = span.String()
	}
	return strings.ToUpper(strings.TrimSpace(protocol)) + "|" + strings.Join(parts, ",") + "|" + cidr
}

func compareCompressionRules(a, b config.RuleInfo) int {
	return cmp.Or(
		cmp.Compare(a.Description, b.Description),
		cmp.Compare(a.Protocol, b.Protocol),
		cmp.Compare(a.Port, b.Port),
		cmp.Compare(a.CidrBlock, b.CidrBlock),
		cmp.Compare(a.Ipv6CidrBlock, b.Ipv6CidrBlock),
		cmp.Compare(a.Action, b.Action),
	)
}

func sortCompressionRows(rows []CompressionRow) []CompressionRow {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Protocol != rows[j].Protocol {
			return rows[i].Protocol < rows[j].Protocol
		}
		if rows[i].Port != rows[j].Port {
			return rows[i].Port < rows[j].Port
		}
		return rows[i].CIDR < rows[j].CIDR
	})
	return rows
}
