package provider

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
)

// 本文件是 Build9 Step 3 的压缩运行期纯核验与清理核心。
//
// 只处理本地快照/规则对象，不访问网络、数据库、时钟或日志。正式 attempt 与
// Dry Run 共用同一输入输出；Provider 写删协议继续使用既有 CreateRules/DeleteRules。

// CompressionCoverage 是实际规则集对站点原始 IPv4 授权的逐端口覆盖与风险摘要。
type CompressionCoverage struct {
	Applicable    bool
	Complete      bool
	RiskKnown     bool
	Missing       []CompressionAuthorization
	Conflicts     []CompressionBlocker
	ExtraIPv4     uint64
	MaxExtraRun   uint64
	PerPort       []CompressionPortRisk
	ManualIgnored bool
}

// CompressionReadback 是本 attempt 计划表示在 S1/S2 实际回读中的核实结果。
type CompressionReadback struct {
	Complete      bool
	Reasons       []CompressionBlocker
	PlannedRows   int
	VerifiedRows  []CompressionRow
	MissingRows   []CompressionRow
	AmbiguousRows []CompressionRow
	Witnesses     int
	Coverage      CompressionCoverage
}

// CompressionCleanupPlan 是同一快照下可安全删除的 EdgeOne IPv4 行子集。
type CompressionCleanupPlan struct {
	Candidates      []config.RuleInfo
	Deletable       []config.RuleInfo
	Deferred        []PlanIssue
	CoverageBefore  CompressionCoverage
	CoverageAfter   CompressionCoverage
	Remaining       int
	ProtectedKnown  bool
	CoverageBlocked bool
}

// CompressionCleanupInput 是实际清理规划的全部输入。
type CompressionCleanupInput struct {
	Snapshot RuleSnapshot
	Tag      string
	// OtherTag 是另一功能 TAG（DNS TAG），只用于删除定位歧义判断。
	OtherTag string
	// Sites 是本实例的全部适用站点；覆盖与保留判断按当前/待更新原始授权执行。
	Sites []EdgeOneSitePlanInput
	// Protected 是本轮仍有效的过渡保护；与候选功能 key 重叠时保留。
	Protected []CompressionAuthorization
	// ProtectedUnknown 为 true 时不做任何压缩清理。
	ProtectedUnknown bool
	// Retained 是本 attempt 已选择并回读核实的压缩行，绝不进入删除候选。
	Retained []CompressionRow
	// RetainedRules holds the S0 witnesses adopted by a cleanup-only attempt.
	RetainedRules []config.RuleInfo
	// KeepKeys 是 base 计划的完整期望 key；候选与这些 key 重叠时保留。
	KeepKeys []FunctionalKey
}

// CompressionSiteEligible 判断站点是否参与 Lighthouse IPv4 策略压缩。
//
// IPv6/ICMP/协议 ALL 沿用原规划；只有 TCP/UDP/TCP+UDP 且包含 IPv4 的站点进入压缩。
func CompressionSiteEligible(site config.EdgeOneSite) bool {
	protocol := strings.ToUpper(strings.TrimSpace(site.Protocol))
	switch protocol {
	case "TCP", "UDP", "TCP+UDP":
	default:
		return false
	}
	family := strings.ToLower(strings.TrimSpace(site.AddressFamily))
	if family != config.EdgeOneAddressFamilyIPv4 && family != config.EdgeOneAddressFamilyDual {
		return false
	}
	spans, ok := compressionPortSpans(site.Ports)
	return ok && len(spans) > 0
}

// CompressionPendingFingerprint 对同一物理实例全部合格站点的待更新版本做稳定摘要。
//
// 无 Next 时返回空串；多个站点时把所有待更新版本一并绑定，避免借用证据只覆盖
// 其中一部分站点。该摘要只用于借用容量判定，不进入 v5 配置或 API。
func CompressionPendingFingerprint(sites []EdgeOneSitePlanInput) string {
	type pending struct {
		zone     string
		version  string
		prefixes []string
	}
	items := make([]pending, 0, len(sites))
	for _, input := range sites {
		if input.Snapshot.Next == nil {
			continue
		}
		zone := strings.TrimSpace(input.Site.ZoneID)
		if zone == "" {
			continue
		}
		prefixes := make([]string, 0, len(input.Snapshot.Next.Prefixes))
		for _, prefix := range input.Snapshot.Next.Prefixes {
			prefixes = append(prefixes, prefix.Masked().String())
		}
		sort.Strings(prefixes)
		items = append(items, pending{zone: zone, version: input.Snapshot.Next.Version, prefixes: prefixes})
	}
	if len(items) == 0 {
		return ""
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].zone < items[j].zone })
	h := sha256.New()
	for _, item := range items {
		_, _ = h.Write([]byte(item.zone))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(item.version))
		_, _ = h.Write([]byte{0})
		for _, prefix := range item.prefixes {
			_, _ = h.Write([]byte(prefix))
			_, _ = h.Write([]byte{0})
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

type compressionRefMode int

const (
	compressionRefsCurrentAndNext compressionRefMode = iota
	compressionRefsNextOnly
	compressionRefsPostActivation
)

// compressionRefsFromSites 展开站点当前/待更新版的 IPv4 原始授权。
//
// 与 buildCompressionAuthorizations 不同，本函数不因 ICMP/协议 ALL 等不可压缩
// 站点拒绝整份输入：这些站点只跳过压缩，仍参与清理保留判断。
func compressionRefsFromSites(sites []EdgeOneSitePlanInput, mode compressionRefMode) ([]CompressionAuthorization, []CompressionBlocker) {
	seen := make(map[CompressionAuthorization]bool)
	refs := make([]CompressionAuthorization, 0)
	var blockers []CompressionBlocker
	add := func(ref CompressionAuthorization) {
		if ref.Prefix == "" || ref.Protocol == "" || ref.PortItem == "" || seen[ref] {
			return
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	for _, input := range sites {
		site := input.Site
		zone := strings.TrimSpace(site.ZoneID)
		if zone == "" || strings.TrimSpace(input.UnavailableReason) != "" ||
			input.Snapshot.ZoneID != zone || strings.TrimSpace(input.Snapshot.Current.Version) == "" ||
			!OriginACLSnapshotPrefixesValid(input.Snapshot) {
			blockers = append(blockers, CompressionBlocker{
				Code: CompressionBlockedSnapshotIncomplete, Message: "站点快照不可用或缺少版本/网段", ZoneID: zone,
			})
			continue
		}
		family := strings.ToLower(strings.TrimSpace(site.AddressFamily))
		if family == config.EdgeOneAddressFamilyIPv6 {
			continue
		}
		if family != config.EdgeOneAddressFamilyIPv4 && family != config.EdgeOneAddressFamilyDual {
			blockers = append(blockers, CompressionBlocker{
				Code: CompressionBlockedSnapshotIncomplete, Message: "站点地址族无效", ZoneID: zone,
			})
			continue
		}
		protocol := strings.ToUpper(strings.TrimSpace(site.Protocol))
		var protocols []string
		var items []string
		if protocol == "ICMP" {
			protocols = []string{"ICMP"}
			items = []string{"ALL"}
		} else {
			protocols = compressionProtocols(protocol)
			if len(protocols) == 0 {
				blockers = append(blockers, CompressionBlocker{
					Code: CompressionBlockedInvalidPort, Message: "站点协议无效", ZoneID: zone,
				})
				continue
			}
			for _, item := range strings.Split(site.Ports, ",") {
				item = strings.ToUpper(strings.TrimSpace(item))
				if item == "" {
					continue
				}
				items = append(items, item)
			}
			if len(items) == 0 {
				blockers = append(blockers, CompressionBlocker{
					Code: CompressionBlockedInvalidPort, Message: "站点端口为空", ZoneID: zone,
				})
				continue
			}
		}
		addVersion := func(version OriginACLVersion, kind string) {
			for _, prefix := range version.Prefixes {
				if _, ok := prefixIPv4Range(prefix); !ok {
					continue
				}
				cidr := prefix.Masked().String()
				for _, protocol := range protocols {
					for _, item := range items {
						add(CompressionAuthorization{
							ZoneID: zone, Version: version.Version, VersionKind: kind,
							Prefix: cidr, Protocol: protocol, PortItem: item,
						})
					}
				}
			}
		}
		switch mode {
		case compressionRefsCurrentAndNext:
			current := input.Snapshot.Current
			addVersion(current, "current")
			if input.Snapshot.Next != nil {
				addVersion(*input.Snapshot.Next, "next")
			}
		case compressionRefsNextOnly:
			if input.Snapshot.Next == nil {
				continue
			}
			addVersion(*input.Snapshot.Next, "next")
		case compressionRefsPostActivation:
			if input.Snapshot.Next != nil {
				addVersion(*input.Snapshot.Next, "next")
			} else {
				addVersion(input.Snapshot.Current, "current")
			}
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.ZoneID != b.ZoneID {
			return a.ZoneID < b.ZoneID
		}
		if a.Prefix != b.Prefix {
			return a.Prefix < b.Prefix
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		if a.PortItem != b.PortItem {
			return a.PortItem < b.PortItem
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.VersionKind < b.VersionKind
	})
	return refs, blockers
}

// CompressionCoverageFromRules 核验实际规则集对当前/待更新原始 IPv4 授权的覆盖。
//
// 手工规则仅按精确功能键取证。只有当前 EdgeOne TAG 行可按地址和端口并集取证。
func CompressionCoverageFromRules(snapshot RuleSnapshot, edgeTag, otherTag string, sites []EdgeOneSitePlanInput, protected []CompressionAuthorization, limit int64) CompressionCoverage {
	refs, blockers := compressionRefsFromSites(sites, compressionRefsCurrentAndNext)
	coverage := compressionCoverageForRefs(snapshot.Rules, refs, edgeTag, otherTag)
	coverage.Conflicts = append(coverage.Conflicts, blockers...)
	coverage.Applicable = len(refs) > 0 || len(blockers) > 0
	coverage.Complete = coverage.Complete && len(coverage.Conflicts) == 0
	risk := compressionRiskFromRules(snapshot, edgeTag, sites, protected, limit)
	coverage.RiskKnown = risk.Complete
	coverage.ExtraIPv4 = risk.ExtraIPv4
	coverage.MaxExtraRun = risk.MaxExtraRun
	coverage.PerPort = risk.PerPort
	coverage.ManualIgnored = risk.ManualIgnored
	return coverage
}

// compressionCoverageForRefs 对每一原始授权按协议/端口求实际覆盖并集。
func compressionCoverageForRefs(rules []config.RuleInfo, refs []CompressionAuthorization, edgeTag, otherTag string) CompressionCoverage {
	out := CompressionCoverage{Applicable: len(refs) > 0}
	external := compressionExternalKeys(RuleSnapshot{Rules: rules}, edgeTag, otherTag)
	grants := make(map[string][]compressionGrant)
	for _, rule := range rules {
		if !strings.EqualFold(strings.TrimSpace(rule.Action), "ACCEPT") || !tag.IsOwned(rule.Description, edgeTag) {
			continue
		}
		if _, ok := SnapshotRuleKeys(rule); !ok {
			continue
		}
		addresses, ports := compressionRuleGrant(rule)
		for _, port := range ports {
			protocol := canonicalProtocol(rule.Protocol)
			grants[protocol] = append(grants[protocol], compressionGrant{port, addresses})
		}
	}
	for _, ref := range refs {
		if compressionExternalCovers(external, ref) {
			continue
		}
		spans, ok := compressionPortSpans(ref.PortItem)
		if !ok || len(spans) != 1 {
			out.Conflicts = append(out.Conflicts, CompressionBlocker{
				Code: CompressionBlockedInvalidPort, Message: "原始授权端口无法解析", ZoneID: ref.ZoneID, Prefix: ref.Prefix,
			})
			continue
		}
		prefix := netip.MustParsePrefix(ref.Prefix)
		span := spans[0]
		protocolGrants := grants[ref.Protocol]
		boundaries := []compressionPortSpan{span}
		for _, grant := range protocolGrants {
			if grant.ports.overlaps(span) {
				boundaries = append(boundaries, grant.ports)
			}
		}
		for _, part := range splitCompressionPorts(boundaries) {
			if part.overlaps(span) && !compressionAddressesAtPort(protocolGrants, part).containsPrefix(prefix) {
				out.Missing = append(out.Missing, ref)
				break
			}
		}
	}
	out.Conflicts = append(out.Conflicts, compressionDropConflictsForRefs(rules, refs)...)
	out.Complete = len(out.Missing) == 0 && len(out.Conflicts) == 0
	return out
}

func compressionExternalKeys(snapshot RuleSnapshot, edgeTag, otherTag string) map[FunctionalKey][]config.RuleInfo {
	_, external, _ := compressionRuleIndexes(snapshot.Rules, edgeTag, otherTag)
	return external
}

func compressionAuthorizationKey(ref CompressionAuthorization) FunctionalKey {
	return FunctionalKey{
		Family: AddressIPv4, CIDR: ref.Prefix, Protocol: ref.Protocol,
		Port: canonicalPortExpression(ref.Protocol, ref.PortItem), Action: "ACCEPT",
	}
}

func compressionExternalCovers(external map[FunctionalKey][]config.RuleInfo, ref CompressionAuthorization) bool {
	return len(external[compressionAuthorizationKey(ref)]) > 0
}

func compressionCrossTagConflicts(in LighthouseCompressionInput) []CompressionBlocker {
	owned, external, other := compressionRuleIndexes(in.Snapshot.Rules, in.EdgeOneTag, in.OtherTag)
	if len(other) == 0 {
		return nil
	}
	refs, _ := compressionRefsFromSites(in.Sites, compressionRefsCurrentAndNext)
	var conflicts []CompressionBlocker
	for _, ref := range refs {
		key := compressionAuthorizationKey(ref)
		if len(other[key]) > 0 && len(owned[key]) == 0 && len(external[key]) == 0 {
			conflicts = append(conflicts, CompressionBlocker{
				Code: IssueCrossTagEquivalent, Message: "另一 TAG 下存在精确等价规则",
				ZoneID: ref.ZoneID, Protocol: ref.Protocol, Port: ref.PortItem, Prefix: ref.Prefix,
			})
		}
	}
	return sortCompressionBlockers(conflicts)
}

func compressionDropConflicts(rules []config.RuleInfo, sites []EdgeOneSitePlanInput) []CompressionBlocker {
	refs, _ := compressionRefsFromSites(sites, compressionRefsCurrentAndNext)
	return compressionDropConflictsForRefs(rules, refs)
}

func compressionDropConflictsForRefs(rules []config.RuleInfo, refs []CompressionAuthorization) []CompressionBlocker {
	drops := compressionDrops(rules)
	if len(drops) == 0 {
		return nil
	}
	var conflicts []CompressionBlocker
	for _, ref := range refs {
		if compressionHitsDrop(drops, ref.Protocol, ref.Prefix, ref.PortItem) {
			conflicts = append(conflicts, CompressionBlocker{
				Code: CompressionBlockedOppositeAction, Message: "实际快照存在与原始授权重叠的 DROP 规则",
				ZoneID: ref.ZoneID, Protocol: ref.Protocol, Port: ref.PortItem, Prefix: ref.Prefix,
			})
		}
	}
	return sortCompressionBlockers(conflicts)
}

// ipv4SetPorts 是端口闭区间的并集运算，保持稳定排序。
type ipv4SetPorts []compressionPortSpan

func (s ipv4SetPorts) covers(target compressionPortSpan) bool {
	cur := target.first
	ordered := append(ipv4SetPorts(nil), s...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].first != ordered[j].first {
			return ordered[i].first < ordered[j].first
		}
		return ordered[i].last < ordered[j].last
	})
	for _, span := range ordered {
		if span.last < cur {
			continue
		}
		if span.first > cur {
			return false
		}
		if span.last >= target.last {
			return true
		}
		cur = span.last + 1
	}
	return false
}

// compressionRiskFromRules 用实际当前 TAG 的 IPv4 允许行计算风险摘要。
func compressionRiskFromRules(snapshot RuleSnapshot, edgeTag string, sites []EdgeOneSitePlanInput, protected []CompressionAuthorization, limit int64) CompressionRisk {
	input := LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse, EdgeOneTag: edgeTag,
		Snapshot: snapshot, Sites: sites, Protected: protected,
	}
	groups, blockers, _ := buildCompressionAuthorizations(input, nil)
	if len(blockers) > 0 {
		return CompressionRisk{Limit: limit, Complete: false, PerPort: []CompressionPortRisk{}}
	}
	model := buildCompressionRiskModel(input, groups)
	return model.summarize(limit, CompressionRepresentation{})
}

// compressionRowFromRule 把一条云端允许规则转换为压缩行摘要。
func compressionRowFromRule(rule config.RuleInfo) (CompressionRow, bool) {
	if !strings.EqualFold(strings.TrimSpace(rule.Action), "ACCEPT") {
		return CompressionRow{}, false
	}
	protocol := canonicalProtocol(rule.Protocol)
	if protocol != "TCP" && protocol != "UDP" {
		return CompressionRow{}, false
	}
	cidr, ok := CanonicalCIDR(rule.CidrBlock)
	if !ok {
		return CompressionRow{}, false
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return CompressionRow{}, false
	}
	portExpr, ok := compressionCanonicalPortExpression(rule.Port)
	if !ok {
		return CompressionRow{}, false
	}
	return CompressionRow{
		Protocol: protocol, Port: portExpr, CIDR: cidr, Action: "ACCEPT", Description: rule.Description,
	}, true
}

// compressionCanonicalPortExpression 把端口表达式归一为可直接比较的区间串。
func compressionCanonicalPortExpression(expr string) (string, bool) {
	spans, ok := compressionPortSpans(expr)
	if !ok || len(spans) == 0 {
		return "", false
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].first != spans[j].first {
			return spans[i].first < spans[j].first
		}
		return spans[i].last < spans[j].last
	})
	merged := make([]compressionPortSpan, 0, len(spans))
	for _, span := range spans {
		if len(merged) == 0 || span.first > merged[len(merged)-1].last+1 {
			merged = append(merged, span)
			continue
		}
		if span.last > merged[len(merged)-1].last {
			merged[len(merged)-1].last = span.last
		}
	}
	if len(merged) == 1 && merged[0] == allCompressionPorts {
		return "ALL", true
	}
	parts := make([]string, len(merged))
	for i, span := range merged {
		parts[i] = span.String()
	}
	return strings.Join(parts, ","), true
}

// compressionRowMatchesRule 判断一条计划行是否与云端实际行物理等价（忽略 comment）。
func compressionRowMatchesRule(row CompressionRow, rule config.RuleInfo, edgeTag string) bool {
	if !tag.IsOwned(rule.Description, edgeTag) {
		return false
	}
	actual, ok := compressionRowFromRule(rule)
	if !ok {
		return false
	}
	if row.Protocol != actual.Protocol || row.CIDR != actual.CIDR || row.Action != actual.Action {
		return false
	}
	rowPort, ok := compressionCanonicalPortExpression(row.Port)
	if !ok {
		return false
	}
	return rowPort == actual.Port
}

// VerifyCompressionReadback 核验本 attempt 计划行与 witness 是否被实际快照完整证明。
func VerifyCompressionReadback(in LighthouseCompressionInput, rep CompressionRepresentation) CompressionReadback {
	out := CompressionReadback{PlannedRows: len(rep.Rows), Coverage: CompressionCoverageFromRules(in.Snapshot, in.EdgeOneTag, in.OtherTag, in.Sites, in.Protected, in.Config.ExtraIPv4Limit)}
	drops := compressionDrops(in.Snapshot.Rules)
	matched := make([]int, len(in.Snapshot.Rules))
	verifiedRow := make([]bool, len(rep.Rows))
	for i, row := range rep.Rows {
		count := 0
		for j, rule := range in.Snapshot.Rules {
			if compressionRowMatchesRule(row, rule, in.EdgeOneTag) {
				count++
				matched[j]++
			}
		}
		switch {
		case count == 0:
			out.MissingRows = append(out.MissingRows, row)
		case count > 1:
			out.AmbiguousRows = append(out.AmbiguousRows, row)
		default:
			verifiedRow[i] = true
			out.VerifiedRows = append(out.VerifiedRows, row)
		}
	}
	// 计划行之外的同 key 重复行也属于定位歧义；Lighthouse 无稳定 RuleID。
	for j, count := range matched {
		if count > 0 && matched[j] != 1 {
			out.Reasons = append(out.Reasons, CompressionBlocker{
				Code: IssueOwnedKeyAmbiguous, Message: "回读存在重复的计划行，无法唯一定位",
				Protocol: in.Snapshot.Rules[j].Protocol, Port: in.Snapshot.Rules[j].Port, Prefix: in.Snapshot.Rules[j].CidrBlock,
			})
		}
	}
	if len(out.MissingRows) > 0 {
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "实际回读缺少计划行"})
	}
	if len(out.AmbiguousRows) > 0 {
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: IssueOwnedKeyAmbiguous, Message: "实际回读存在重复计划行"})
	}
	// witness 端口并集必须完整覆盖原始授权。
	expected, blockers := compressionRefsFromSites(in.Sites, compressionRefsCurrentAndNext)
	out.Reasons = append(out.Reasons, blockers...)
	covered := make(map[CompressionAuthorization]ipv4SetPorts)
	for _, witness := range rep.Witnesses {
		if witness.RowIndex < 0 || witness.RowIndex >= len(rep.Rows) || !verifiedRow[witness.RowIndex] {
			continue
		}
		spans, ok := compressionPortSpans(witness.Port)
		if !ok {
			continue
		}
		covered[witness.Authorization] = append(covered[witness.Authorization], spans...)
		out.Witnesses++
	}
	external := compressionExternalKeys(in.Snapshot, in.EdgeOneTag, in.OtherTag)
	for _, ref := range expected {
		if compressionExternalCovers(external, ref) {
			continue
		}
		original, ok := compressionPortSpans(ref.PortItem)
		if !ok || len(original) == 0 {
			continue
		}
		if !covered[ref].covers(original[0]) {
			out.MissingRows = append(out.MissingRows, CompressionRow{
				Protocol: ref.Protocol, Port: ref.PortItem, CIDR: ref.Prefix, Action: "ACCEPT",
			})
		}
	}
	if len(out.MissingRows) > 0 {
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "压缩 witness 未完整覆盖原始授权"})
	}
	if !out.Coverage.Complete {
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "实际规则集未完整覆盖原始授权"})
	}
	out.Reasons = append(out.Reasons, out.Coverage.Conflicts...)
	for _, row := range out.VerifiedRows {
		if compressionHitsDrop(drops, row.Protocol, row.CIDR, row.Port) {
			out.Reasons = append(out.Reasons, compressionOppositeAction)
			break
		}
	}
	if len(in.Snapshot.Rules) > compressionHardCap {
		out.Reasons = append(out.Reasons, CompressionBlocker{
			Code:    CompressionBlockedHardQuota,
			Message: fmt.Sprintf("实际规则数 %d 超过硬上限 %d", len(in.Snapshot.Rules), compressionHardCap),
		})
	}
	out.Reasons = sortCompressionBlockers(out.Reasons)
	out.Complete = len(out.Reasons) == 0 && len(out.MissingRows) == 0 && len(out.AmbiguousRows) == 0
	return out
}

// AssessCompressionCleanupEvidence 模拟待更新版成为当前后的整批安全清理。
//
// 只用于借用容量判定；实际删除仍必须由 PlanCompressionCleanup 在 S1 上重算。
func AssessCompressionCleanupEvidence(in LighthouseCompressionInput, sites []EdgeOneSitePlanInput, candidate CompressionRepresentation) CompressionCleanupEvidence {
	if strings.TrimSpace(in.PendingFingerprint) == "" || in.Snapshot.Revision == "" {
		return CompressionCleanupEvidence{}
	}
	pendingRefs, blockers := compressionRefsFromSites(sites, compressionRefsPostActivation)
	if len(blockers) > 0 || len(pendingRefs) == 0 {
		return CompressionCleanupEvidence{}
	}
	// 构造候选表示加入后的实际规则集；候选行均视为已生效。
	addRules := make([]config.RuleInfo, 0, len(candidate.AddRows))
	for _, row := range candidate.AddRows {
		addRules = append(addRules, config.RuleInfo{
			CidrBlock: row.CidrBlock, Protocol: row.Protocol, Port: row.Port, Action: row.Action, Description: row.Description,
		})
	}
	working := append(append([]config.RuleInfo(nil), in.Snapshot.Rules...), addRules...)
	owners, external, other := compressionRuleIndexes(working, in.EdgeOneTag, in.OtherTag)
	keep := make(map[FunctionalKey]bool, len(in.CleanupKeepKeys))
	for _, key := range in.CleanupKeepKeys {
		keep[key] = true
	}
	evidence := CompressionCleanupEvidence{
		SnapshotRevision: in.Snapshot.Revision, PendingFingerprint: in.PendingFingerprint,
	}
	for index, rule := range in.Snapshot.Rules {
		row, scoped := compressionRowFromRule(rule)
		if !scoped || !tag.IsOwned(rule.Description, in.EdgeOneTag) {
			continue
		}
		retainsNext := false
		for _, planned := range candidate.Rows {
			if planned.ServesNext && compressionRetained(row, []CompressionRow{planned}) {
				retainsNext = true
				break
			}
		}
		keys, known := SnapshotRuleKeys(rule)
		if retainsNext || !known || compressionRuleProtected(in.PostActivationProtected, keys) {
			continue
		}
		if _, bad := cleanupLocatorIssue(config.CloudTCLighthouse, rule, keys, owners, external, other); bad ||
			compressionKeepKey(keys, keep) {
			continue
		}
		trial := removeRuleAt(working, rule)
		if !compressionCoverageForRefs(trial, pendingRefs, in.EdgeOneTag, in.OtherTag).Complete {
			continue
		}
		working = trial
		evidence.ReleasableRows = append(evidence.ReleasableRows, index)
	}
	if len(evidence.ReleasableRows) == 0 {
		return CompressionCleanupEvidence{}
	}
	return evidence
}

// PlanCompressionCleanup 在 S1 实际规则集上选择可安全删除的整行子集。
func PlanCompressionCleanup(in CompressionCleanupInput) CompressionCleanupPlan {
	out := CompressionCleanupPlan{CoverageBefore: CompressionCoverageFromRules(in.Snapshot, in.Tag, in.OtherTag, in.Sites, in.Protected, 0)}
	out.Remaining = len(in.Snapshot.Rules)
	out.ProtectedKnown = !in.ProtectedUnknown
	refs, blockers := compressionRefsFromSites(in.Sites, compressionRefsCurrentAndNext)
	gateOpen := !in.ProtectedUnknown && len(refs) > 0 && len(blockers) == 0 && out.CoverageBefore.Complete &&
		strings.TrimSpace(in.Snapshot.Revision) != ""
	if in.ProtectedUnknown {
		out.Deferred = append(out.Deferred, PlanIssue{Code: IssueTransitionProtected, Message: "过渡保护状态未知，保留全部压缩清理候选"})
	}
	if !gateOpen && !in.ProtectedUnknown {
		out.CoverageBlocked = true
		out.Deferred = append(out.Deferred, PlanIssue{Code: IssueCoverageNotReady, Message: "实际规则集未完整覆盖原始授权，保留全部压缩清理候选"})
	}
	keep := make(map[FunctionalKey]bool, len(in.KeepKeys))
	for _, key := range in.KeepKeys {
		keep[key] = true
	}
	ownedIndex, externalIndex, otherTagIndex := compressionRuleIndexes(in.Snapshot.Rules, in.Tag, in.OtherTag)
	var eligible []config.RuleInfo
	for _, rule := range in.Snapshot.Rules {
		row, ok := compressionRowFromRule(rule)
		if !ok || !tag.IsOwned(rule.Description, in.Tag) {
			continue
		}
		if compressionRetained(row, in.Retained) || slices.Contains(in.RetainedRules, rule) {
			continue
		}
		keys, known := SnapshotRuleKeys(rule)
		if !known || len(keys) == 0 {
			continue
		}
		if compressionKeepKey(keys, keep) {
			continue
		}
		out.Candidates = append(out.Candidates, rule)
		if issue, bad := cleanupLocatorIssue(config.CloudTCLighthouse, rule, keys, ownedIndex, externalIndex, otherTagIndex); bad {
			out.Deferred = append(out.Deferred, issue)
			continue
		}
		if compressionRuleProtected(in.Protected, keys) {
			out.Deferred = append(out.Deferred, PlanIssue{
				Code: IssueTransitionProtected, Message: "候选仍处于过渡保护范围，保留为残留",
			})
			continue
		}
		if gateOpen {
			eligible = append(eligible, rule)
		}
	}
	working := append([]config.RuleInfo(nil), in.Snapshot.Rules...)
	for _, candidate := range eligible {
		trial := removeRuleAt(working, candidate)
		coverage := compressionCoverageForRefs(trial, refs, in.Tag, in.OtherTag)
		if !coverage.Complete {
			out.Deferred = append(out.Deferred, PlanIssue{
				Code: IssueCoverageNotReady, Message: "移除该候选后无法完整覆盖原始授权，保留为残留",
			})
			continue
		}
		working = trial
		out.Deletable = append(out.Deletable, candidate)
	}
	out.CoverageAfter = out.CoverageBefore
	if len(out.Deletable) > 0 {
		out.CoverageAfter = compressionCoverageForRefs(working, refs, in.Tag, in.OtherTag)
	}
	out.Remaining = len(working)
	return out
}

// compressionRuleIndexes 为 Lighthouse 删除定位构造 owned/external/other TAG 索引。
func compressionRuleIndexes(rules []config.RuleInfo, edgeTag, otherTag string) (map[FunctionalKey][]config.RuleInfo, map[FunctionalKey][]config.RuleInfo, map[FunctionalKey][]config.RuleInfo) {
	owned := make(map[FunctionalKey][]config.RuleInfo)
	external := make(map[FunctionalKey][]config.RuleInfo)
	other := make(map[FunctionalKey][]config.RuleInfo)
	for _, rule := range rules {
		keys, ok := SnapshotRuleKeys(rule)
		if !ok {
			continue
		}
		var target map[FunctionalKey][]config.RuleInfo
		switch {
		case tag.IsOwned(rule.Description, edgeTag):
			target = owned
		case otherTag != "" && tag.IsOwned(rule.Description, otherTag):
			target = other
		default:
			target = external
		}
		for _, key := range keys {
			target[key] = append(target[key], rule)
		}
	}
	return owned, external, other
}

func compressionRetained(row CompressionRow, retained []CompressionRow) bool {
	for _, item := range retained {
		a, _ := compressionCanonicalPortExpression(row.Port)
		b, _ := compressionCanonicalPortExpression(item.Port)
		if row.Protocol == item.Protocol && row.CIDR == item.CIDR && a == b {
			return true
		}
	}
	return false
}

func compressionKeepKey(keys []FunctionalKey, keep map[FunctionalKey]bool) bool {
	for _, key := range keys {
		if keep[key] {
			return true
		}
	}
	return false
}

func compressionRuleProtected(protected []CompressionAuthorization, keys []FunctionalKey) bool {
	for _, item := range protected {
		prefix, err := netip.ParsePrefix(item.Prefix)
		if err != nil {
			continue
		}
		for _, key := range keys {
			candidate, err := netip.ParsePrefix(key.CIDR)
			if err != nil || candidate.Addr().BitLen() != prefix.Addr().BitLen() {
				continue
			}
			if prefix.Contains(candidate.Addr()) || candidate.Contains(prefix.Addr()) {
				return true
			}
		}
	}
	return false
}

func removeRuleAt(rules []config.RuleInfo, target config.RuleInfo) []config.RuleInfo {
	out := make([]config.RuleInfo, 0, len(rules))
	removed := false
	for _, rule := range rules {
		if !removed && rule == target {
			removed = true
			continue
		}
		out = append(out, rule)
	}
	return out
}

func sortCompressionBlockers(in []CompressionBlocker) []CompressionBlocker {
	seen := make(map[CompressionBlocker]bool, len(in))
	out := make([]CompressionBlocker, 0, len(in))
	for _, item := range in {
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.ZoneID != b.ZoneID {
			return a.ZoneID < b.ZoneID
		}
		if a.Prefix != b.Prefix {
			return a.Prefix < b.Prefix
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		return a.Port < b.Port
	})
	return out
}

// VerifyCompressionCleanupReadback proves a cleanup-only attempt using the S0
// rules it adopted, plus currently verified exact external coverage.
func VerifyCompressionCleanupReadback(in LighthouseCompressionInput, retained []config.RuleInfo) CompressionReadback {
	witnesses := make([]config.RuleInfo, 0, len(in.Snapshot.Rules))
	for _, rule := range in.Snapshot.Rules {
		if !tag.IsOwned(rule.Description, in.EdgeOneTag) {
			witnesses = append(witnesses, rule)
			continue
		}
		for _, original := range retained {
			if rule == original {
				witnesses = append(witnesses, rule)
				break
			}
		}
	}
	refs, blockers := compressionRefsFromSites(in.Sites, compressionRefsCurrentAndNext)
	coverage := compressionCoverageForRefs(witnesses, refs, in.EdgeOneTag, in.OtherTag)
	out := CompressionReadback{
		Coverage: CompressionCoverageFromRules(in.Snapshot, in.EdgeOneTag, in.OtherTag, in.Sites, in.Protected, in.Config.ExtraIPv4Limit),
		Reasons:  append(blockers, coverage.Conflicts...),
	}
	if !coverage.Complete {
		out.Reasons = append(out.Reasons, CompressionBlocker{Code: CompressionBlockedSnapshotIncomplete, Message: "回读无法验证纯清理所采用的规则"})
	}
	out.Complete = coverage.Complete && len(out.Reasons) == 0
	return out
}
