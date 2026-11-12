package provider

import (
	"net/netip"
	"reflect"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// 本文件是 Build8 Step 6 的纯规划器判别性用例。
//
// 只验证纯函数：不访问网络、数据库、时钟或配置写入。真实云端回读与写入由后续 Step 接入。

func edgeSite(zoneID string, targetIDs []int, protocol, ports, family string) config.EdgeOneSite {
	return config.EdgeOneSite{
		ZoneID:        zoneID,
		TargetIDs:     targetIDs,
		Protocol:      protocol,
		Ports:         ports,
		AddressFamily: family,
	}
}

func edgeVersion(version string, cidrs ...string) OriginACLVersion {
	prefixes := make([]netip.Prefix, len(cidrs))
	for i, cidr := range cidrs {
		prefixes[i] = netip.MustParsePrefix(cidr)
	}
	return OriginACLVersion{Version: version, Prefixes: prefixes}
}

func planEdgeOne(t *testing.T, ct config.CloudType, tag, otherTag string, sites []EdgeOneSitePlanInput, snapshot []config.RuleInfo) TargetPlan {
	t.Helper()
	return PlanTarget(TargetPlanInput{
		CloudType:    ct,
		Tag:          tag,
		OtherTag:     otherTag,
		EdgeOneSites: sites,
		Snapshot:     RuleSnapshot{Rules: snapshot, Revision: "1"},
	})
}

// TestEdgeOnePlanTwoSitesSharePrefixProduceOneAdd 是 Step 6 的核心收口用例：
// 同一目标的两个站点给出相同 /24 TCP 443 时，只计划一条新增，并保留两个来源。
func TestEdgeOnePlanTwoSitesSharePrefixProduceOneAdd(t *testing.T) {
	sites := []EdgeOneSitePlanInput{
		{Site: edgeSite("zone-b", []int{7}, "TCP", "443", "ipv4"), Snapshot: OriginACLSnapshot{ZoneID: "zone-b", Current: edgeVersion("1", "203.0.113.0/24")}},
		{Site: edgeSite("zone-a", []int{7}, "TCP", "443", "ipv4"), Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/24")}},
		// 只绑定到其他目标的站点不得进入本目标规划。
		{Site: edgeSite("zone-other", []int{8}, "TCP", "443", "ipv4"), Snapshot: OriginACLSnapshot{ZoneID: "zone-other", Current: edgeVersion("1", "198.51.100.0/24")}},
	}
	plan := PlanTarget(TargetPlanInput{
		CloudType:    config.CloudTCLighthouse,
		Tag:          "edgeone",
		OtherTag:     "auto-dns",
		TargetID:     7,
		EdgeOneSites: sites,
		Snapshot:     RuleSnapshot{Revision: "1"},
	})

	if len(plan.Desired) != 1 {
		t.Fatalf("Desired = %d, want 1: %+v", len(plan.Desired), plan.Desired)
	}
	desired := plan.Desired[0]
	if desired.Key.CIDR != "203.0.113.0/24" || desired.Key.Protocol != "TCP" ||
		desired.Key.Port != "443" || desired.Key.Action != "ACCEPT" {
		t.Fatalf("Desired key = %+v", desired.Key)
	}
	if !reflect.DeepEqual(desired.SiteIDs, []string{"zone-a", "zone-b"}) {
		t.Fatalf("SiteIDs = %v, want 两站点稳定排序", desired.SiteIDs)
	}
	if len(desired.RuleIDs) != 0 || desired.RuleIDs == nil {
		t.Fatalf("EdgeOne 来源 RuleIDs 必须是非 null 空数组: %#v", desired.RuleIDs)
	}
	if len(plan.ToAdd) != 1 {
		t.Fatalf("ToAdd = %d, want 1: %+v", len(plan.ToAdd), plan.ToAdd)
	}
	if plan.ToAdd[0].CidrBlock != "203.0.113.0/24" || plan.ToAdd[0].Port != "443" {
		t.Fatalf("ToAdd action = %+v", plan.ToAdd[0])
	}
	if plan.ToAdd[0].Description != "[edgeone]" {
		t.Fatalf("Description = %q", plan.ToAdd[0].Description)
	}
	if plan.CoverageReady {
		t.Fatal("尚未回读确认时 CoverageReady 必须为 false")
	}
}

// TestEdgeOnePlanCurrentAndNextSamePrefixOneAdd 同站点两版本共同 CIDR 只产生一条新增。
func TestEdgeOnePlanCurrentAndNextSamePrefixOneAdd(t *testing.T) {
	next := edgeVersion("2", "203.0.113.0/24", "198.51.100.0/24")
	sites := []EdgeOneSitePlanInput{{
		Site: edgeSite("zone-a", []int{3}, "TCP", "443", "ipv4"),
		Snapshot: OriginACLSnapshot{
			ZoneID:  "zone-a",
			Current: edgeVersion("1", "203.0.113.0/24"),
			Next:    &next,
		},
	}}
	plan := planEdgeOne(t, config.CloudTCCVM, "edgeone", "auto-dns", sites, nil)
	if len(plan.Desired) != 2 || len(plan.ToAdd) != 2 {
		t.Fatalf("Desired/ToAdd = %d/%d, want 2/2: %+v", len(plan.Desired), len(plan.ToAdd), plan.Desired)
	}
	got := map[string]bool{}
	for _, d := range plan.Desired {
		got[d.Key.CIDR] = true
		if !reflect.DeepEqual(d.SiteIDs, []string{"zone-a"}) {
			t.Errorf("SiteIDs = %v", d.SiteIDs)
		}
	}
	if !got["203.0.113.0/24"] || !got["198.51.100.0/24"] {
		t.Fatalf("并集 CIDR = %v", got)
	}
}

// TestEdgeOnePlanPreservesMaskWhileDNSHostPrefix DNS 单地址保持 /32，EdgeOne 保留 /24。
func TestEdgeOnePlanPreservesMaskWhileDNSHostPrefix(t *testing.T) {
	dnsPlan := PlanTarget(TargetPlanInput{
		CloudType: config.CloudTCLighthouse,
		Tag:       "auto-dns",
		Rules:     []config.DomainRule{rule(1, "home.example", "TCP", "443", "ACCEPT", "")},
		Resolved:  map[int][]dns.ResolvedIP{1: {v4("203.0.113.7")}},
		Snapshot:  RuleSnapshot{Revision: "1"},
	})
	if len(dnsPlan.Desired) != 1 || dnsPlan.Desired[0].Key.CIDR != "203.0.113.7/32" {
		t.Fatalf("DNS 前缀 = %+v", dnsPlan.Desired)
	}
	if len(dnsPlan.Desired[0].SiteIDs) != 0 || dnsPlan.Desired[0].SiteIDs == nil {
		t.Fatalf("DNS 来源 SiteIDs 必须是非 null 空数组: %#v", dnsPlan.Desired[0].SiteIDs)
	}

	edgePlan := planEdgeOne(t, config.CloudTCLighthouse, "edgeone", "auto-dns", []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP", "443", "ipv4"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/24")},
	}}, nil)
	if len(edgePlan.Desired) != 1 || edgePlan.Desired[0].Key.CIDR != "203.0.113.0/24" {
		t.Fatalf("EdgeOne 前缀 = %+v", edgePlan.Desired)
	}
	if edgePlan.Desired[0].Action.CidrBlock != "203.0.113.0/24" || edgePlan.Desired[0].Action.Ipv6CidrBlock != "" {
		t.Fatalf("EdgeOne Action 地址字段 = %+v", edgePlan.Desired[0].Action)
	}
}

// TestEdgeOnePlanCrossTagEquivalentIsConflictOnly 另一功能 TAG 的等价规则：
// 不计覆盖、不新增、不删除、不改标，只报告目标级冲突。
func TestEdgeOnePlanCrossTagEquivalentIsConflictOnly(t *testing.T) {
	other := config.RuleInfo{
		Protocol: "TCP", Port: "443", CidrBlock: "203.0.113.0/24",
		Action: "ACCEPT", Description: "[auto-dns] DNS 侧旧规则", RuleID: "dns-rule",
	}
	plan := planEdgeOne(t, config.CloudTCLighthouse, "edgeone", "auto-dns", []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP", "443", "ipv4"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/24")},
	}}, []config.RuleInfo{other})

	if len(plan.Desired) != 1 {
		t.Fatalf("Desired = %d, want 1", len(plan.Desired))
	}
	if len(plan.SatisfiedByOwned) != 0 || len(plan.SatisfiedByExternal) != 0 {
		t.Fatalf("另一 TAG 等价规则不得提供覆盖: owned=%v external=%v", plan.SatisfiedByOwned, plan.SatisfiedByExternal)
	}
	if !hasIssue(plan.Conflicts, IssueCrossTagEquivalent) {
		t.Fatalf("缺少目标级冲突: %+v", plan.Conflicts)
	}
	if len(plan.ToAdd) != 0 {
		t.Fatalf("冲突 key 不得新增重复规则: %+v", plan.ToAdd)
	}
	if plan.CoverageReady {
		t.Fatal("另一 TAG 等价规则不得声明覆盖")
	}
	if len(plan.CleanupCandidates) != 0 || len(plan.CleanupDeletable) != 0 {
		t.Fatalf("另一 TAG 规则不得进入清理: candidates=%v deletable=%v", plan.CleanupCandidates, plan.CleanupDeletable)
	}
	if !hasIssue(plan.CleanupDeferred, IssueCrossTagEquivalent) || !hasIssue(plan.CleanupDeferred, IssueCoverageNotReady) {
		t.Fatalf("清理门必须冻结: %+v", plan.CleanupDeferred)
	}
}

// TestEdgeOnePlanManualExactMatchStillCovers 手工规则继续沿用精确等价语义。
func TestEdgeOnePlanManualExactMatchStillCovers(t *testing.T) {
	manual := config.RuleInfo{
		Protocol: "TCP", Port: "443", CidrBlock: "203.0.113.0/24",
		Action: "ACCEPT", Description: "手工维护", RuleID: "manual",
	}
	plan := planEdgeOne(t, config.CloudTCLighthouse, "edgeone", "auto-dns", []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP", "443", "ipv4"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/24")},
	}}, []config.RuleInfo{manual})
	if len(plan.SatisfiedByExternal) != 1 || len(plan.ToAdd) != 0 || !plan.CoverageReady {
		t.Fatalf("手工精确等价必须覆盖: %+v", plan)
	}
	if hasIssue(plan.Conflicts, IssueCrossTagEquivalent) {
		t.Fatalf("手工规则不是另一功能 TAG 冲突: %+v", plan.Conflicts)
	}

	// 非精确的手工规则不推断覆盖：不同端口必须新增。
	wrongPort := manual
	wrongPort.Port = "8443"
	planWrong := planEdgeOne(t, config.CloudTCLighthouse, "edgeone", "auto-dns", []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP", "443", "ipv4"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/24")},
	}}, []config.RuleInfo{wrongPort})
	if len(planWrong.SatisfiedByExternal) != 0 || len(planWrong.ToAdd) != 1 || planWrong.CoverageReady {
		t.Fatalf("非精确手工规则不得覆盖: %+v", planWrong)
	}
}

// TestEdgeOnePlanSharedKeyMergesDNSAndSite 同一功能 key 只计划一条新增，
// 同时保留 DNS 规则 ID 与 EdgeOne 站点来源。
func TestEdgeOnePlanSharedKeyMergesDNSAndSite(t *testing.T) {
	plan := PlanTarget(TargetPlanInput{
		CloudType: config.CloudTCLighthouse,
		Tag:       "auto-dns",
		OtherTag:  "edgeone",
		Rules:     []config.DomainRule{rule(11, "shared.example", "TCP", "443", "ACCEPT", "共享")},
		Resolved:  map[int][]dns.ResolvedIP{11: {v4("203.0.113.0")}},
		Snapshot:  RuleSnapshot{Revision: "1"},
		EdgeOneSites: []EdgeOneSitePlanInput{{
			Site:     edgeSite("zone-a", []int{2}, "TCP", "443", "ipv4"),
			Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/32")},
		}},
	})
	if len(plan.Desired) != 1 || len(plan.ToAdd) != 1 {
		t.Fatalf("Desired/ToAdd = %d/%d, want 1/1: %+v", len(plan.Desired), len(plan.ToAdd), plan.Desired)
	}
	desired := plan.Desired[0]
	if !reflect.DeepEqual(desired.RuleIDs, []int{11}) || !reflect.DeepEqual(desired.SiteIDs, []string{"zone-a"}) {
		t.Fatalf("来源 = RuleIDs %v SiteIDs %v", desired.RuleIDs, desired.SiteIDs)
	}
	if desired.Action.Description != "[auto-dns] 共享" {
		t.Fatalf("合并后的描述 = %q", desired.Action.Description)
	}
}

// TestEdgeOnePlanIPv6OnSWASUnsupported 保留平台能力矩阵与保守清理。
func TestEdgeOnePlanIPv6OnSWASUnsupported(t *testing.T) {
	plan := planEdgeOne(t, config.CloudAliSWAS, "edgeone", "auto-dns", []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP", "443", "ipv6"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "2001:db8::/64")},
	}}, nil)
	if len(plan.Desired) != 1 || plan.Desired[0].Implementable {
		t.Fatalf("SWAS IPv6 应标记为不可实施: %+v", plan.Desired)
	}
	if !hasIssue(plan.Unsupported, IssueUnsupportedIPv6) || !hasIssue(plan.CleanupDeferred, IssueUnsupportedIPv6) {
		t.Fatalf("能力跳过与清理冻结必须保留: %+v", plan)
	}
	if len(plan.ToAdd) != 0 || plan.CoverageReady {
		t.Fatalf("不可实施项不得新增或声明覆盖: %+v", plan)
	}
}

// TestEdgeOnePlanOppositeActionAndTemplateFreezeCleanup 相反动作与模板规则均冻结清理。
func TestEdgeOnePlanOppositeActionAndTemplateFreezeCleanup(t *testing.T) {
	snapshot := []config.RuleInfo{
		// 已存在的当前 TAG 规则（覆盖 desired）
		{Protocol: "TCP", Port: "443", CidrBlock: "203.0.113.0/24", Action: "ACCEPT", Description: "[edgeone] current", RuleID: "current"},
		// 同 tuple 的相反动作
		{Protocol: "TCP", Port: "443", CidrBlock: "203.0.113.0/24", Action: "DROP", Description: "[edgeone] drop", RuleID: "drop"},
		// 陈旧 Owned 规则：可成为候选，但不得删除
		{Protocol: "TCP", Port: "80", CidrBlock: "198.51.100.0/24", Action: "ACCEPT", Description: "[edgeone] stale", RuleID: "stale"},
		// 模板/畸形规则：字段不足
		{Description: "[edgeone] template", RuleID: "template"},
	}
	plan := planEdgeOne(t, config.CloudAliECS, "edgeone", "auto-dns", []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP", "443", "ipv4"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/24")},
	}}, snapshot)
	if !hasIssue(plan.Conflicts, IssueOppositeAction) {
		t.Fatalf("缺少相反动作冲突: %+v", plan.Conflicts)
	}
	if !hasIssue(plan.Conflicts, IssueSnapshotRuleInvalid) {
		t.Fatalf("缺少模板/畸形规则冲突: %+v", plan.Conflicts)
	}
	if len(plan.CleanupCandidates) != 2 {
		t.Fatalf("CleanupCandidates = %d, want 2（DROP 与陈旧规则；当前规则覆盖 desired、模板规则无定位）: %+v", len(plan.CleanupCandidates), plan.CleanupCandidates)
	}
	if len(plan.CleanupDeletable) != 0 {
		t.Fatalf("冲突存在时不得删除: %+v", plan.CleanupDeletable)
	}
	for _, code := range []string{IssueOppositeAction, IssueSnapshotRuleInvalid} {
		if !hasIssue(plan.CleanupDeferred, code) {
			t.Errorf("CleanupDeferred 缺少 %s: %+v", code, plan.CleanupDeferred)
		}
	}
}

// TestEdgeOnePlanUnavailableSiteNeverBecomesEmpty 查询失败的站点不是空集合：
// 记录 EdgeOneErrors、禁止覆盖、冻结清理，但仍可保留候选用于展示。
func TestEdgeOnePlanUnavailableSiteNeverBecomesEmpty(t *testing.T) {
	stale := config.RuleInfo{
		Protocol: "TCP", Port: "80", CidrBlock: "198.51.100.0/24",
		Action: "ACCEPT", Description: "[edgeone] stale", RuleID: "stale",
	}
	plan := planEdgeOne(t, config.CloudAliECS, "edgeone", "auto-dns", []EdgeOneSitePlanInput{{
		Site:              edgeSite("zone-a", []int{1}, "TCP", "443", "ipv4"),
		UnavailableReason: "查询预算耗尽",
	}}, []config.RuleInfo{stale})
	if len(plan.Desired) != 0 || len(plan.ToAdd) != 0 {
		t.Fatalf("不可用站点不得产生新增: %+v", plan)
	}
	if !hasIssue(plan.EdgeOneErrors, IssueEdgeOneUnavailable) {
		t.Fatalf("缺少 EdgeOneErrors: %+v", plan.EdgeOneErrors)
	}
	if plan.CoverageReady {
		t.Fatal("站点查询失败时不得声明覆盖")
	}
	if len(plan.CleanupCandidates) != 1 || len(plan.CleanupDeletable) != 0 {
		t.Fatalf("候选可展示但不得删除: %+v", plan)
	}
	if !hasIssue(plan.CleanupDeferred, IssueEdgeOneUnavailable) {
		t.Fatalf("清理门未冻结: %+v", plan.CleanupDeferred)
	}

	// 零值/半截快照同样是不可用，不能被当作合法空地址族。
	zero := planEdgeOne(t, config.CloudAliECS, "edgeone", "auto-dns", []EdgeOneSitePlanInput{{
		Site: edgeSite("zone-a", []int{1}, "TCP", "443", "ipv4"),
	}}, nil)
	if !hasIssue(zero.EdgeOneErrors, IssueEdgeOneUnavailable) || zero.CoverageReady {
		t.Fatalf("零值快照必须按不可用处理: %+v", zero)
	}
}

// TestEdgeOnePlanTCPUDPSplitAndPortExpansion 复用既有端口展开与平台能力矩阵。
func TestEdgeOnePlanTCPUDPSplitAndPortExpansion(t *testing.T) {
	sites := []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP+UDP", "443", "ipv4"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/24")},
	}}
	lighthouse := planEdgeOne(t, config.CloudTCLighthouse, "edgeone", "auto-dns", sites, nil)
	if len(lighthouse.Desired) != 2 || len(lighthouse.ToAdd) != 2 {
		t.Fatalf("Lighthouse TCP+UDP 应拆两条: %+v", lighthouse.Desired)
	}
	protocols := map[string]bool{}
	for _, d := range lighthouse.Desired {
		protocols[d.Key.Protocol] = true
	}
	if !protocols["TCP"] || !protocols["UDP"] {
		t.Fatalf("拆分协议 = %v", protocols)
	}
	swas := planEdgeOne(t, config.CloudAliSWAS, "edgeone", "auto-dns", sites, nil)
	if len(swas.Desired) != 1 || swas.Desired[0].Key.Protocol != "TCP+UDP" {
		t.Fatalf("SWAS 应保留原生 TCP+UDP 单条: %+v", swas.Desired)
	}

	multiPort := []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP", "80,443", "ipv4"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/24")},
	}}
	cvm := planEdgeOne(t, config.CloudTCCVM, "edgeone", "auto-dns", multiPort, nil)
	if len(cvm.Desired) != 2 || len(cvm.ToAdd) != 2 {
		t.Fatalf("CVM 多端口应拆两条: %+v", cvm.Desired)
	}
}

// TestDNSPlanArraysRemainNonNull 新字段是纯追加，DNS 规划保持既有数组语义。
func TestDNSPlanArraysRemainNonNull(t *testing.T) {
	plan := planFor(t, config.CloudTCLighthouse,
		[]config.DomainRule{rule(1, "a.test", "TCP", "443", "ACCEPT", "")},
		map[int][]dns.ResolvedIP{1: {v4("1.2.3.4")}}, nil)
	if len(plan.Desired) != 1 {
		t.Fatalf("Desired = %d", len(plan.Desired))
	}
	if plan.Desired[0].SiteIDs == nil || len(plan.Desired[0].SiteIDs) != 0 {
		t.Fatalf("DNS SiteIDs = %#v", plan.Desired[0].SiteIDs)
	}
	if plan.EdgeOneErrors == nil {
		t.Fatal("EdgeOneErrors 必须是非 null 数组")
	}
}

// TestSelectEdgeOneSitesForIdentityMergesAliases 按物理目标归并别名本地 ID 上的站点。
func TestSelectEdgeOneSitesForIdentityMergesAliases(t *testing.T) {
	targets := []config.TargetConfig{
		{ID: 7, CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lh-1"},
		// 历史/外部空白值仍归并为同一物理目标（TargetIdentity 会 Trim）。
		{ID: 8, CloudType: config.CloudTCLighthouse, Region: " ap-guangzhou ", ResourceID: " lh-1 "},
		{ID: 9, CloudType: config.CloudTCLighthouse, Region: "ap-guangzhou", ResourceID: "lh-2"},
	}
	sites := []config.EdgeOneSite{
		edgeSite("zone-b", []int{8}, "TCP", "443", "ipv4"),
		edgeSite("zone-a", []int{7}, "TCP", "443", "ipv4"),
		edgeSite("zone-other", []int{9}, "TCP", "443", "ipv4"),
	}

	selected := SelectEdgeOneSitesForIdentity(sites, targets, config.TargetIdentity(targets[0]))
	if len(selected) != 2 || selected[0].ZoneID != "zone-a" || selected[1].ZoneID != "zone-b" {
		t.Fatalf("按物理目标归并 = %+v", selected)
	}
	if selected[0].TargetIDs[0] != 7 || selected[1].TargetIDs[0] != 8 {
		t.Fatalf("别名站点的本地绑定应保留: %+v", selected)
	}

	inputs := make([]EdgeOneSitePlanInput, 0, len(selected))
	for _, site := range selected {
		inputs = append(inputs, EdgeOneSitePlanInput{
			Site:     site,
			Snapshot: OriginACLSnapshot{ZoneID: site.ZoneID, Current: edgeVersion("1", "203.0.113.0/24")},
		})
	}
	plan := planEdgeOne(t, config.CloudTCLighthouse, "edgeone", "auto-dns", inputs, nil)
	if len(plan.Desired) != 1 || len(plan.ToAdd) != 1 {
		t.Fatalf("别名目标应合并为一条新增: %+v", plan)
	}
	if !reflect.DeepEqual(plan.Desired[0].SiteIDs, []string{"zone-a", "zone-b"}) {
		t.Fatalf("SiteIDs = %v", plan.Desired[0].SiteIDs)
	}
	if got := SelectEdgeOneSitesForIdentity(sites, targets, ""); got != nil {
		t.Fatalf("空 identity 应返回 nil: %+v", got)
	}
}
