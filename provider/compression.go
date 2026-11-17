package provider

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/portconv"
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

// CompressionWitness 证明一条原始授权被某条物理行覆盖。
type CompressionWitness struct {
	Authorization CompressionAuthorization
	RowIndex      int
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
	Snapshot      RuleSnapshot
	Sites         []EdgeOneSitePlanInput

	OtherNewRows        int
	HasPendingVersion   bool
	PendingFingerprint  string
	ExpectedCleanupRows int
	CleanupFingerprint  string
	CleanupTrusted      bool

	DNSEnabled   bool
	DNSAddresses []netip.Addr
	DNSComplete  bool
	DNSConflict  bool

	MaxStates int
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

	auths, blockers, warnings := buildCompressionAuthorizations(in)
	plan.Blockers = append(plan.Blockers, blockers...)
	plan.Warnings = append(plan.Warnings, warnings...)
	if len(plan.Blockers) > 0 {
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	if len(auths) == 0 {
		plan.Capacity = EvaluateCompressionCapacity(CompressionCapacityInput{
			S0Rows: len(in.Snapshot.Rules), OtherNewRows: in.OtherNewRows,
		})
		return plan
	}

	lossless, exact, perr := buildCompressionRepresentation(in, auths, nil)
	if perr != "" {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedInvalidPort, Message: perr})
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	plan.Lossless = lossless
	capacity := evaluateRepresentationCapacity(in, lossless, exact)
	risk := computeCompressionRisk(in, auths, lossless)
	plan.Capacity = capacity
	plan.Risk = risk
	if capacity.Mode != CompressionModeBlocked && risk.ExtraIPv4 <= uint64(in.Config.ExtraIPv4Limit) {
		plan.Selected = &plan.Lossless
		return plan
	}

	// 无损已超额或风险超限。风险超限时分有损只会增加风险，直接阻断。
	if risk.ExtraIPv4 > uint64(in.Config.ExtraIPv4Limit) {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedRiskLimit, Message: fmt.Sprintf("额外放行 %d 个 IPv4，超过额度 %d", risk.ExtraIPv4, in.Config.ExtraIPv4Limit)})
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	if in.Config.ExtraIPv4Limit == 0 {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedLossyForbidden, Message: "额外放行额度为 0，只允许无损压缩"})
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}

	lossy, incomplete, lblockers := planLossyCompression(ctx, in, auths)
	plan.Blockers = append(plan.Blockers, lblockers...)
	if incomplete {
		plan.Incomplete = true
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	if lossy == nil {
		if len(plan.Blockers) == 0 {
			plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedNoCandidate, Message: "没有满足容量与风险额度的有损候选"})
		}
		plan.Capacity.Mode = CompressionModeBlocked
		return plan
	}
	plan.Lossy = lossy
	plan.Capacity = evaluateRepresentationCapacity(in, *lossy, false)
	plan.Risk = computeCompressionRisk(in, auths, *lossy)
	if plan.Capacity.Mode != CompressionModeBlocked && plan.Risk.ExtraIPv4 <= uint64(in.Config.ExtraIPv4Limit) {
		plan.Selected = plan.Lossy
		return plan
	}
	if len(plan.Capacity.Reasons) > 0 {
		plan.Blockers = append(plan.Blockers, plan.Capacity.Reasons...)
	}
	if plan.Risk.ExtraIPv4 > uint64(in.Config.ExtraIPv4Limit) {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedRiskLimit, Message: fmt.Sprintf("额外放行 %d 个 IPv4，超过额度 %d", plan.Risk.ExtraIPv4, in.Config.ExtraIPv4Limit)})
	}
	if len(plan.Blockers) == 0 {
		plan.Blockers = append(plan.Blockers, CompressionBlocker{Code: CompressionBlockedNoCandidate, Message: "压缩计划未通过容量或风险校验"})
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
		NormalBudget:        maxInt(0, compressionNormalCap-in.S0Rows-in.OtherNewRows),
		ExpectedCleanupRows: in.ExpectedCleanupRows,
	}
	out.Peak = in.S0Rows + in.OtherNewRows + out.AddRows
	if out.AddRows == 0 {
		if in.S0Rows+in.OtherNewRows > compressionNormalCap {
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
	excess := out.AddRows - out.NormalBudget
	if in.PendingExtra < excess {
		out.Mode = CompressionModeBlocked
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedPendingExceedsExcess, Message: "借用需求超过仅为待更新版覆盖所需的增量"})
		return out
	}
	if !in.CleanupTrusted || (in.PendingFingerprint != "" && in.CleanupFingerprint != in.PendingFingerprint) {
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

func evaluateRepresentationCapacity(in LighthouseCompressionInput, rep CompressionRepresentation, exact bool) CompressionCapacity {
	return EvaluateCompressionCapacity(CompressionCapacityInput{
		S0Rows:              len(in.Snapshot.Rules),
		OtherNewRows:        in.OtherNewRows,
		CurrentAdds:         rep.CurrentAdds,
		PendingExtra:        rep.PendingExtra,
		HasPendingVersion:   in.HasPendingVersion,
		PendingFingerprint:  in.PendingFingerprint,
		ExpectedCleanupRows: in.ExpectedCleanupRows,
		CleanupFingerprint:  in.CleanupFingerprint,
		CleanupTrusted:      in.CleanupTrusted,
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// authKey 是一个协议端口授权项。
type authKey struct {
	Protocol string
	Port     string
}

func (k authKey) String() string { return k.Protocol + "|" + k.Port }

// buildCompressionAuthorizations 从站点快照构建 IPv4 授权并按“前缀的端口集合”分组。
func buildCompressionAuthorizations(in LighthouseCompressionInput) ([]compressionAuthGroup, []CompressionBlocker, []CompressionBlocker) {
	var blockers []CompressionBlocker
	var warnings []CompressionBlocker
	// prefix -> authKey -> refs
	type refKey struct {
		zone, version, kind string
	}
	byPrefix := make(map[netip.Prefix]map[authKey]map[refKey]bool)
	for _, source := range in.Sites {
		site := source.Site
		zone := strings.TrimSpace(site.ZoneID)
		if zone == "" {
			blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "站点配置缺少 ZoneId"})
			continue
		}
		if source.UnavailableReason != "" || source.Snapshot.ZoneID == "" || strings.TrimSpace(source.Snapshot.Current.Version) == "" {
			blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "站点 " + zone + " 快照不可用", ZoneID: zone})
			continue
		}
		if source.Snapshot.ZoneID != zone {
			blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "站点 " + zone + " 快照 ZoneId 不一致", ZoneID: zone})
			continue
		}
		if !OriginACLSnapshotPrefixesValid(source.Snapshot) {
			blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "站点 " + zone + " 快照包含非法或缺失网段", ZoneID: zone})
			continue
		}
		family := strings.ToLower(strings.TrimSpace(site.AddressFamily))
		if family == "ipv6" {
			continue
		}
		if family != "ipv4" && family != "dual" {
			blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "站点 " + zone + " 地址族无效", ZoneID: zone})
			continue
		}
		protocols := []string{strings.ToUpper(strings.TrimSpace(site.Protocol))}
		if protocols[0] == "TCP+UDP" {
			protocols = []string{"TCP", "UDP"}
		}
		if protocols[0] == "ICMP" {
			warnings = append(warnings, CompressionBlocker{Code: "icmp_not_compressed", Message: "站点 " + zone + " 为 ICMP，沿用原规划，不进入压缩", ZoneID: zone})
			continue
		}
		items := portconv.Parse(site.Ports)
		if len(items) == 0 {
			blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedInvalidPort, Message: "站点 " + zone + " 端口为空", ZoneID: zone})
			continue
		}
		addVersion := func(version *OriginACLVersion, kind string) {
			if version == nil {
				return
			}
			for _, prefix := range version.Prefixes {
				if !prefix.IsValid() || !prefix.Addr().Is4() {
					continue
				}
				prefix = prefix.Masked()
				if byPrefix[prefix] == nil {
					byPrefix[prefix] = make(map[authKey]map[refKey]bool)
				}
				for _, proto := range protocols {
					for _, item := range items {
						key := authKey{Protocol: proto, Port: strings.ToUpper(strings.TrimSpace(item))}
						if byPrefix[prefix][key] == nil {
							byPrefix[prefix][key] = make(map[refKey]bool)
						}
						byPrefix[prefix][key][refKey{zone: zone, version: version.Version, kind: kind}] = true
					}
				}
			}
		}
		addVersion(&source.Snapshot.Current, "current")
		addVersion(source.Snapshot.Next, "next")
	}
	if len(blockers) > 0 {
		return nil, blockers, warnings
	}

	type groupBuilder struct {
		prefixes []netip.Prefix
		keys     []authKey
		refs     []CompressionAuthorization
	}
	groups := make(map[string]*groupBuilder)
	order := make([]string, 0)
	for prefix, keyMap := range byPrefix {
		keys := make([]authKey, 0, len(keyMap))
		for key := range keyMap {
			keys = append(keys, key)
		}
		sort.SliceStable(keys, func(i, j int) bool {
			if keys[i].Protocol != keys[j].Protocol {
				return keys[i].Protocol < keys[j].Protocol
			}
			return keys[i].Port < keys[j].Port
		})
		parts := make([]string, len(keys))
		for i, key := range keys {
			parts[i] = key.String()
		}
		groupKey := strings.Join(parts, "\x00")
		builder := groups[groupKey]
		if builder == nil {
			builder = &groupBuilder{keys: keys}
			groups[groupKey] = builder
			order = append(order, groupKey)
		}
		builder.prefixes = append(builder.prefixes, prefix)
		for _, key := range keys {
			refs := keyMap[key]
			refKeys := make([]refKey, 0, len(refs))
			for ref := range refs {
				refKeys = append(refKeys, ref)
			}
			sort.SliceStable(refKeys, func(i, j int) bool {
				if refKeys[i].zone != refKeys[j].zone {
					return refKeys[i].zone < refKeys[j].zone
				}
				if refKeys[i].version != refKeys[j].version {
					return refKeys[i].version < refKeys[j].version
				}
				return refKeys[i].kind < refKeys[j].kind
			})
			for _, ref := range refKeys {
				builder.refs = append(builder.refs, CompressionAuthorization{
					ZoneID: ref.zone, Version: ref.version, VersionKind: ref.kind,
					Prefix: prefix.String(), Protocol: key.Protocol, PortItem: key.Port,
				})
			}
		}
	}
	sort.Strings(order)
	out := make([]compressionAuthGroup, 0, len(order))
	for _, key := range order {
		builder := groups[key]
		sort.SliceStable(builder.prefixes, func(i, j int) bool {
			return builder.prefixes[i].String() < builder.prefixes[j].String()
		})
		out = append(out, compressionAuthGroup{
			Prefixes: builder.prefixes,
			Keys:     builder.keys,
			Refs:     builder.refs,
		})
	}
	return out, nil, warnings
}

type compressionAuthGroup struct {
	Prefixes []netip.Prefix
	Keys     []authKey
	Refs     []CompressionAuthorization
}

// buildCompressionRepresentation 构建指定覆盖前缀集合的物理行；covers 为空时使用无损合并。
func buildCompressionRepresentation(in LighthouseCompressionInput, groups []compressionAuthGroup, covers [][]netip.Prefix) (CompressionRepresentation, bool, string) {
	rep := CompressionRepresentation{Rows: []CompressionRow{}, AddRows: []config.RuleAction{}, ReusedRows: []config.RuleInfo{}, Witnesses: []CompressionWitness{}, Exact: covers == nil}
	existing := make(map[string][]config.RuleInfo)
	for _, rule := range in.Snapshot.Rules {
		if !strings.EqualFold(strings.TrimSpace(rule.Action), "ACCEPT") || !tag.IsOwned(rule.Description, in.EdgeOneTag) {
			continue
		}
		cidr, ok := CanonicalCIDR(rule.CidrBlock)
		if !ok {
			continue
		}
		key := compressionWireKey(rule.Protocol, rule.Port, cidr, rule.Action)
		existing[key] = append(existing[key], rule)
	}
	desc := RenderDescription(config.CloudTCLighthouse, in.EdgeOneTag, "")
	for gi, group := range groups {
		prefixes := mergeIPv4PrefixesExact(group.Prefixes)
		if covers != nil {
			if gi >= len(covers) || len(covers[gi]) == 0 {
				return rep, false, "有损覆盖缺失"
			}
			prefixes = append([]netip.Prefix(nil), covers[gi]...)
		}
		byProtocol := make(map[string][]string)
		for _, key := range group.Keys {
			byProtocol[key.Protocol] = append(byProtocol[key.Protocol], key.Port)
		}
		protocols := make([]string, 0, len(byProtocol))
		for proto := range byProtocol {
			protocols = append(protocols, proto)
		}
		sort.Strings(protocols)
		for _, proto := range protocols {
			exprs, ok := portExpressions(byProtocol[proto])
			if !ok {
				return rep, false, "无法生成端口表达式"
			}
			for _, prefix := range prefixes {
				for _, expr := range exprs {
					row := CompressionRow{
						Protocol: proto, Port: expr, CIDR: prefix.Masked().String(), Action: "ACCEPT",
						Description: desc,
					}
					for _, ref := range group.Refs {
						if !strings.EqualFold(ref.Protocol, proto) {
							continue
						}
						if !prefix.Contains(netip.MustParsePrefix(ref.Prefix).Addr()) {
							continue
						}
						if !portExpressionCovers(expr, ref.PortItem) {
							continue
						}
						row.Covered = append(row.Covered, ref)
						if ref.VersionKind == "current" || ref.VersionKind == "both" {
							row.ServesCurrent = true
						}
						if ref.VersionKind == "next" || ref.VersionKind == "both" {
							row.ServesNext = true
						}
					}
					sort.SliceStable(row.Covered, func(i, j int) bool {
						a, b := row.Covered[i], row.Covered[j]
						if a.Prefix != b.Prefix {
							return a.Prefix < b.Prefix
						}
						if a.Protocol != b.Protocol {
							return a.Protocol < b.Protocol
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
					key := compressionWireKey(row.Protocol, row.Port, row.CIDR, row.Action)
					if rules := existing[key]; len(rules) > 0 {
						row.Reused = true
						rep.ReusedRows = append(rep.ReusedRows, rules[0])
						existing[key] = rules[1:]
					} else {
						rep.AddRows = append(rep.AddRows, config.RuleAction{
							Protocol: row.Protocol, Port: row.Port, CidrBlock: row.CIDR,
							Action: row.Action, Description: row.Description,
						})
						if row.ServesCurrent {
							rep.CurrentAdds++
						}
						if row.ServesNext && !row.ServesCurrent {
							rep.PendingExtra++
						}
					}
					rep.Rows = append(rep.Rows, row)
				}
			}
		}
	}
	rep.Rows = sortCompressionRows(rep.Rows)
	rep.AddRows = sortCompressionActions(rep.AddRows)
	rep.ReusedRows = sortCompressionRules(rep.ReusedRows)
	// witness 索引以排序后的行为准。
	rep.Witnesses = nil
	for i, row := range rep.Rows {
		for _, ref := range row.Covered {
			rep.Witnesses = append(rep.Witnesses, CompressionWitness{Authorization: ref, RowIndex: i, Lossy: !rep.Exact})
		}
	}
	required := ipv4Set{}
	covered := ipv4Set{}
	for _, group := range groups {
		required = required.union(ipv4SetFromPrefixes(group.Prefixes))
	}
	for _, row := range rep.Rows {
		if p, err := netip.ParsePrefix(row.CIDR); err == nil {
			covered = covered.union(ipv4SetFromPrefixes([]netip.Prefix{p}))
		}
	}
	extra := covered.subtract(required)
	rep.ExtraIPv4 = extra.count()
	rep.MaxExtraRun = extra.maxRun()
	if covers != nil {
		rep.Exact = rep.ExtraIPv4 == 0
	}
	return rep, true, ""
}

func compressionWireKey(protocol, port, cidr, action string) string {
	return strings.ToUpper(strings.TrimSpace(protocol)) + "|" + strings.TrimSpace(port) + "|" +
		strings.TrimSpace(cidr) + "|" + strings.ToUpper(strings.TrimSpace(action))
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

func sortCompressionActions(rows []config.RuleAction) []config.RuleAction {
	sort.SliceStable(rows, func(i, j int) bool {
		return compressionWireKey(rows[i].Protocol, rows[i].Port, rows[i].CidrBlock, rows[i].Action) < compressionWireKey(rows[j].Protocol, rows[j].Port, rows[j].CidrBlock, rows[j].Action)
	})
	return rows
}

func sortCompressionRules(rows []config.RuleInfo) []config.RuleInfo {
	sort.SliceStable(rows, func(i, j int) bool {
		return compressionWireKey(rows[i].Protocol, rows[i].Port, rows[i].CidrBlock, rows[i].Action) < compressionWireKey(rows[j].Protocol, rows[j].Port, rows[j].CidrBlock, rows[j].Action)
	})
	return rows
}
