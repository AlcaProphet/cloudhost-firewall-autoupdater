package provider

import (
	"net"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// 本文件是 Issue7 Step 1 的目标级纯规划器判别性用例。
//
// 证据边界：这些用例只证明纯 planner 的所有权、canonical key、目标聚合、能力矩阵
// 与清理候选/安全门语义；不构成任何真实云行为证据，也不替代 Provider 请求 mock。

func v4(ip string) dns.ResolvedIP { return dns.ResolvedIP{IP: net.ParseIP(ip), IsIPv6: false} }
func v6(ip string) dns.ResolvedIP { return dns.ResolvedIP{IP: net.ParseIP(ip), IsIPv6: true} }

func rule(id int, host, protocol, ports, action, comment string) config.DomainRule {
	return config.DomainRule{ID: id, Host: host, Protocol: protocol, Ports: ports, Action: action, Comment: comment}
}

func planFor(t *testing.T, ct config.CloudType, rules []config.DomainRule, resolved map[int][]dns.ResolvedIP, snapshot []config.RuleInfo) TargetPlan {
	t.Helper()
	return PlanTarget(TargetPlanInput{
		CloudType: ct,
		Tag:       "auto-dns",
		Rules:     rules,
		Resolved:  resolved,
		Snapshot:  RuleSnapshot{Rules: snapshot, Revision: "1"},
	})
}

func hasIssue(issues []PlanIssue, code string) bool {
	for _, it := range issues {
		if it.Code == code {
			return true
		}
	}
	return false
}

// TestPlan_TwoEmptyCommentsDifferentDomainsDoNotDeleteEachOther 是 P1-01 的主判别性用例：
// 两条 comment 为空的本地规则（不同域名、不同 IP）必须形成两个独立 Desired key，
// 任何一方都不得成为另一方的清理候选，且第二轮必须收敛（零删除、零新增）。
func TestPlan_TwoEmptyCommentsDifferentDomainsDoNotDeleteEachOther(t *testing.T) {
	rules := []config.DomainRule{
		rule(1, "a.example.com", "TCP", "443", "ACCEPT", ""),
		rule(2, "b.example.com", "TCP", "443", "ACCEPT", ""),
	}
	resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}, 2: {v4("2.2.2.2")}}

	// 第一轮：云端为空 → 两条都要新增，且没有任何清理候选。
	first := planFor(t, config.CloudTCLighthouse, rules, resolved, nil)
	if len(first.Desired) != 2 {
		t.Fatalf("Desired 数量 = %d, want 2（两条空 comment 规则必须形成两个独立功能 key）", len(first.Desired))
	}
	if len(first.ToAdd) != 2 {
		t.Fatalf("ToAdd 数量 = %d, want 2", len(first.ToAdd))
	}
	if len(first.CleanupCandidates) != 0 {
		t.Fatalf("CleanupCandidates 数量 = %d, want 0", len(first.CleanupCandidates))
	}
	if first.Desired[0].Description == "" || first.Desired[1].Description == "" {
		t.Fatalf("Desired 必须带可读 description: %+v", first.Desired)
	}

	// 第二轮：云端只有规则 1 的规则（comment 空 → description 与规则 2 完全相同）。
	// 修复前这一形态会让规则 2 把规则 1 的规则列入删除。
	existing := []config.RuleInfo{{
		Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]",
	}}
	second := planFor(t, config.CloudTCLighthouse, rules, resolved, existing)
	if len(second.CleanupCandidates) != 0 {
		t.Fatalf("CleanupCandidates 数量 = %d, want 0（不同域名的规则绝不互为候选）: %+v",
			len(second.CleanupCandidates), second.CleanupCandidates)
	}
	if len(second.SatisfiedByOwned) != 1 {
		t.Fatalf("SatisfiedByOwned 数量 = %d, want 1", len(second.SatisfiedByOwned))
	}
	if len(second.ToAdd) != 1 {
		t.Fatalf("ToAdd 数量 = %d, want 1（只缺规则 2 的 IP）", len(second.ToAdd))
	}
}

// TestPlan_SameDomainMultiProtocolMultiPort 同域名多协议/多端口必须各自成 key 且互不删除。
func TestPlan_SameDomainMultiProtocolMultiPort(t *testing.T) {
	rules := []config.DomainRule{
		rule(1, "api.example.com", "TCP", "443", "ACCEPT", ""),
		rule(2, "api.example.com", "UDP", "443", "ACCEPT", ""),
		rule(3, "api.example.com", "TCP", "8000-8010", "ACCEPT", ""),
	}
	resolved := map[int][]dns.ResolvedIP{
		1: {v4("1.1.1.1")}, 2: {v4("1.1.1.1")}, 3: {v4("1.1.1.1")},
	}
	plan := planFor(t, config.CloudAliSWAS, rules, resolved, nil)
	if len(plan.Desired) != 3 {
		t.Fatalf("Desired 数量 = %d, want 3", len(plan.Desired))
	}

	// 云端回读形态：SWAS 端口为斜杠格式（GetRules 已归一为内部格式）
	existing := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "r1"},
		{Protocol: "UDP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "r2"},
		{Protocol: "TCP", Port: "8000-8010", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "r3"},
	}
	second := planFor(t, config.CloudAliSWAS, rules, resolved, existing)
	if len(second.ToAdd) != 0 || len(second.CleanupCandidates) != 0 {
		t.Fatalf("同域名多协议/多端口应收敛: ToAdd=%d CleanupCandidates=%d", len(second.ToAdd), len(second.CleanupCandidates))
	}
	if len(second.SatisfiedByOwned) != 3 {
		t.Fatalf("SatisfiedByOwned 数量 = %d, want 3", len(second.SatisfiedByOwned))
	}
}

// TestPlan_CommentChangeZeroAddZeroCandidates comment 不参与身份：改备注必须零增删。
func TestPlan_CommentChangeZeroAddZeroCandidates(t *testing.T) {
	rules := []config.DomainRule{rule(1, "api.example.com", "TCP", "443", "ACCEPT", "新备注")}
	resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}}
	existing := []config.RuleInfo{{
		Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns] 旧备注",
	}}
	plan := planFor(t, config.CloudTCLighthouse, rules, resolved, existing)
	if len(plan.ToAdd) != 0 {
		t.Fatalf("ToAdd 数量 = %d, want 0（comment 改变不触发云规则重建）", len(plan.ToAdd))
	}
	if len(plan.CleanupCandidates) != 0 {
		t.Fatalf("CleanupCandidates 数量 = %d, want 0", len(plan.CleanupCandidates))
	}
	if len(plan.SatisfiedByOwned) != 1 {
		t.Fatalf("SatisfiedByOwned 数量 = %d, want 1", len(plan.SatisfiedByOwned))
	}
}

// TestPlan_ExternalExactMatchOnly External 规则只做精确 key 满足，不推断包含关系、永不操作。
func TestPlan_ExternalExactMatchOnly(t *testing.T) {
	rules := []config.DomainRule{rule(1, "api.example.com", "TCP", "8000-8010", "ACCEPT", "")}
	resolved := map[int][]dns.ResolvedIP{1: {v4("1.2.3.4")}}

	exact := []config.RuleInfo{{
		Protocol: "TCP", Port: "8000-8010", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: "手动规则",
	}}
	plan := planFor(t, config.CloudTCLighthouse, rules, resolved, exact)
	if len(plan.SatisfiedByExternal) != 1 {
		t.Fatalf("SatisfiedByExternal 数量 = %d, want 1", len(plan.SatisfiedByExternal))
	}
	if len(plan.ToAdd) != 0 || len(plan.CleanupCandidates) != 0 {
		t.Fatalf("External 精确满足时不得新增或删除: ToAdd=%d CleanupCandidates=%d", len(plan.ToAdd), len(plan.CleanupCandidates))
	}
	if !plan.CoverageReady {
		t.Fatalf("External 精确满足必须计入覆盖（CoverageReady=true）")
	}

	// 宽 CIDR 不推断覆盖窄 CIDR；宽端口范围不推断覆盖窄范围。
	wide := []config.RuleInfo{
		{Protocol: "TCP", Port: "8000-8010", CidrBlock: "0.0.0.0/0", Action: "ACCEPT", Description: "手动规则"},
		{Protocol: "TCP", Port: "8000-9000", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: "手动规则"},
	}
	planWide := planFor(t, config.CloudTCLighthouse, rules, resolved, wide)
	if len(planWide.ToAdd) != 1 {
		t.Fatalf("ToAdd 数量 = %d, want 1（宽 CIDR/宽端口不得推断为满足）", len(planWide.ToAdd))
	}
	if len(planWide.SatisfiedByExternal) != 0 {
		t.Fatalf("SatisfiedByExternal 数量 = %d, want 0", len(planWide.SatisfiedByExternal))
	}
}

// TestPlan_AliyunPortRoundTripConverges 保留 P0-01 的正向控制：阿里云创建侧斜杠端口经
// 回读归一化后，下一轮必须收敛（零新增、零候选）；单端口/范围/ALL 三种形态都覆盖。
func TestPlan_AliyunPortRoundTripConverges(t *testing.T) {
	tests := []struct {
		name     string
		ct       config.CloudType
		ports    string
		existing string
	}{
		{"SWAS 单端口", config.CloudAliSWAS, "443", "443"},
		{"SWAS 范围端口", config.CloudAliSWAS, "8000-8010", "8000-8010"},
		{"SWAS ALL", config.CloudAliSWAS, "ALL", "ALL"},
		{"ECS 单端口", config.CloudAliECS, "443", "443"},
		{"ECS 范围端口", config.CloudAliECS, "8000-8010", "8000-8010"},
		{"ECS ALL", config.CloudAliECS, "ALL", "ALL"},
		{"SWAS 斜杠原样回读", config.CloudAliSWAS, "443", "443/443"},
		{"SWAS 斜杠范围原样回读", config.CloudAliSWAS, "8000-8010", "8000/8010"},
		{"ECS 斜杠原样回读", config.CloudAliECS, "8000-8010", "8000/8010"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := []config.DomainRule{rule(1, "api.example.com", "TCP", tt.ports, "ACCEPT", "")}
			resolved := map[int][]dns.ResolvedIP{1: {v4("1.2.3.4")}}
			existing := []config.RuleInfo{{
				Protocol: "TCP", Port: tt.existing, CidrBlock: "1.2.3.4/32", Action: "ACCEPT",
				Description: "[auto-dns]", RuleID: "r1",
			}}
			plan := planFor(t, tt.ct, rules, resolved, existing)
			if len(plan.ToAdd) != 0 || len(plan.CleanupCandidates) != 0 {
				t.Fatalf("端口未收敛: ToAdd=%d CleanupCandidates=%d (desired=%+v)",
					len(plan.ToAdd), len(plan.CleanupCandidates), plan.Desired)
			}
		})
	}
}

// TestPlan_IPv6ICMPNormalization P2-01：IPv6+ICMP 在期望侧与云端回读侧必须对称。
func TestPlan_IPv6ICMPNormalization(t *testing.T) {
	mk := func() ([]config.DomainRule, map[int][]dns.ResolvedIP) {
		r := rule(1, "v6.example.com", "ICMP", "ALL", "ACCEPT", "")
		r.EnableIPv6 = true
		return []config.DomainRule{r}, map[int][]dns.ResolvedIP{1: {v6("2001:db8::1")}}
	}
	rules, resolved := mk()

	tests := []struct {
		name     string
		ct       config.CloudType
		protocol string
		port     string
	}{
		{"Lighthouse ICMPv6", config.CloudTCLighthouse, "ICMPv6", "ALL"},
		{"CVM ICMPV6", config.CloudTCCVM, "ICMPV6", ""},
		{"Lighthouse ICMP 小写", config.CloudTCLighthouse, "icmpv6", "ALL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := []config.RuleInfo{{
				Protocol: tt.protocol, Port: tt.port, Ipv6CidrBlock: "2001:db8::1/128",
				Action: "ACCEPT", Description: "[auto-dns]",
			}}
			plan := planFor(t, tt.ct, rules, resolved, existing)
			if len(plan.ToAdd) != 0 || len(plan.CleanupCandidates) != 0 {
				t.Fatalf("IPv6+ICMP 未收敛: ToAdd=%d CleanupCandidates=%d", len(plan.ToAdd), len(plan.CleanupCandidates))
			}
			if len(plan.SatisfiedByOwned) != 1 {
				t.Fatalf("SatisfiedByOwned 数量 = %d, want 1", len(plan.SatisfiedByOwned))
			}
		})
	}
}

// TestPlan_UnsupportedMatrixFreezesCleanup P2-03：平台能力限制必须显式进入 unsupported，
// 既不进入 ToAdd，也冻结清理门（候选全部 deferred）。
func TestPlan_UnsupportedMatrixFreezesCleanup(t *testing.T) {
	// 云端存在一条与期望无关的陈旧 Owned 规则，作为清理候选。
	stale := config.RuleInfo{
		Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT",
		Description: "[auto-dns]", RuleID: "stale",
	}

	t.Run("SWAS IPv6", func(t *testing.T) {
		r := rule(1, "v6.example.com", "TCP", "443", "ACCEPT", "")
		r.EnableIPv6 = true
		rules := []config.DomainRule{r}
		resolved := map[int][]dns.ResolvedIP{1: {v6("2001:db8::1")}}
		plan := planFor(t, config.CloudAliSWAS, rules, resolved, []config.RuleInfo{stale})
		if !hasIssue(plan.Unsupported, IssueUnsupportedIPv6) {
			t.Fatalf("缺少 %s: %+v", IssueUnsupportedIPv6, plan.Unsupported)
		}
		if len(plan.ToAdd) != 0 {
			t.Fatalf("unsupported 项不得进入 ToAdd: %+v", plan.ToAdd)
		}
		if len(plan.Desired) != 1 || plan.Desired[0].Implementable {
			t.Fatalf("Desired 必须保留不可实施项并标记 Implementable=false: %+v", plan.Desired)
		}
		if !hasIssue(plan.CleanupDeferred, IssueUnsupportedIPv6) {
			t.Fatalf("unsupported 必须冻结清理: %+v", plan.CleanupDeferred)
		}
	})

	t.Run("SWAS DROP", func(t *testing.T) {
		rules := []config.DomainRule{rule(1, "api.example.com", "TCP", "443", "DROP", "")}
		resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}}
		plan := planFor(t, config.CloudAliSWAS, rules, resolved, []config.RuleInfo{stale})
		if !hasIssue(plan.Unsupported, IssueUnsupportedAction) {
			t.Fatalf("缺少 %s: %+v", IssueUnsupportedAction, plan.Unsupported)
		}
		if len(plan.ToAdd) != 0 || !hasIssue(plan.CleanupDeferred, IssueUnsupportedAction) {
			t.Fatalf("DROP 必须不可实施且冻结清理: ToAdd=%d deferred=%+v", len(plan.ToAdd), plan.CleanupDeferred)
		}
	})

	t.Run("ECS ICMPv6", func(t *testing.T) {
		r := rule(1, "v6.example.com", "ICMP", "ALL", "ACCEPT", "")
		r.EnableIPv6 = true
		rules := []config.DomainRule{r}
		resolved := map[int][]dns.ResolvedIP{1: {v6("2001:db8::1")}}
		plan := planFor(t, config.CloudAliECS, rules, resolved, []config.RuleInfo{stale})
		if !hasIssue(plan.Unsupported, IssueUnsupportedICMPv6) {
			t.Fatalf("缺少 %s: %+v", IssueUnsupportedICMPv6, plan.Unsupported)
		}
		if len(plan.ToAdd) != 0 || !hasIssue(plan.CleanupDeferred, IssueUnsupportedICMPv6) {
			t.Fatalf("ECS ICMPv6 必须不可实施且冻结清理: ToAdd=%d deferred=%+v", len(plan.ToAdd), plan.CleanupDeferred)
		}
	})

	t.Run("ECS IPv4 不受影响", func(t *testing.T) {
		rules := []config.DomainRule{rule(1, "api.example.com", "ICMP", "ALL", "ACCEPT", "")}
		resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}}
		plan := planFor(t, config.CloudAliECS, rules, resolved, []config.RuleInfo{stale})
		if len(plan.Unsupported) != 0 {
			t.Fatalf("ECS IPv4 ICMP 必须可实施: %+v", plan.Unsupported)
		}
		if len(plan.ToAdd) != 1 {
			t.Fatalf("ToAdd 数量 = %d, want 1", len(plan.ToAdd))
		}
	})
}

// TestPlan_CommentSelectionByRuleIDAscending 同 key 多来源时按本地 rule ID 升序取第一个非空 comment；
// 全空时只写 [TAG]。
func TestPlan_CommentSelectionByRuleIDAscending(t *testing.T) {
	rules := []config.DomainRule{
		rule(3, "c.example.com", "TCP", "443", "ACCEPT", "丙"),
		rule(1, "a.example.com", "TCP", "443", "ACCEPT", ""),
		rule(2, "b.example.com", "TCP", "443", "ACCEPT", "乙"),
	}
	resolved := map[int][]dns.ResolvedIP{
		1: {v4("1.1.1.1")}, 2: {v4("1.1.1.1")}, 3: {v4("1.1.1.1")},
	}
	plan := planFor(t, config.CloudTCLighthouse, rules, resolved, nil)
	if len(plan.Desired) != 1 {
		t.Fatalf("Desired 数量 = %d, want 1（同 key 必须合并）", len(plan.Desired))
	}
	got := plan.Desired[0]
	if got.Comment != "乙" {
		t.Fatalf("选中 comment = %q, want %q（rule ID 升序第一个非空值）", got.Comment, "乙")
	}
	if got.Description != "[auto-dns] 乙" {
		t.Fatalf("description = %q, want %q", got.Description, "[auto-dns] 乙")
	}
	if len(got.RuleIDs) != 3 || got.RuleIDs[0] != 1 || got.RuleIDs[2] != 3 {
		t.Fatalf("RuleIDs = %v, want [1 2 3]", got.RuleIDs)
	}
	if len(got.Domains) != 3 {
		t.Fatalf("Domains = %v, want 3 个来源域名", got.Domains)
	}

	allEmpty := []config.DomainRule{
		rule(1, "a.example.com", "TCP", "443", "ACCEPT", ""),
		rule(2, "b.example.com", "TCP", "443", "ACCEPT", ""),
	}
	planEmpty := planFor(t, config.CloudTCLighthouse, allEmpty, resolved, nil)
	if len(planEmpty.Desired) != 1 || planEmpty.Desired[0].Description != "[auto-dns]" {
		t.Fatalf("全空 comment 必须只写 [TAG]: %+v", planEmpty.Desired)
	}
}

// TestPlan_EmptyDesiredDoesNotAuthorizeWipe 空期望集不得授权清空 TAG 命名空间。
func TestPlan_EmptyDesiredDoesNotAuthorizeWipe(t *testing.T) {
	existing := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "r1"},
		{Protocol: "TCP", Port: "80", CidrBlock: "2.2.2.2/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "r2"},
	}
	plan := planFor(t, config.CloudAliSWAS, nil, nil, existing)
	if len(plan.Desired) != 0 {
		t.Fatalf("Desired 数量 = %d, want 0", len(plan.Desired))
	}
	if plan.CoverageReady {
		t.Fatalf("空 Desired 必须 CoverageReady=false")
	}
	if len(plan.ToAdd) != 0 {
		t.Fatalf("ToAdd 数量 = %d, want 0", len(plan.ToAdd))
	}
	if len(plan.CleanupCandidates) != 2 {
		t.Fatalf("CleanupCandidates 数量 = %d, want 2（只作预览）", len(plan.CleanupCandidates))
	}
	if !hasIssue(plan.CleanupDeferred, IssueDesiredEmpty) {
		t.Fatalf("空 Desired 必须冻结清理: %+v", plan.CleanupDeferred)
	}
}

// TestPlan_LighthouseAmbiguityAndLocators 删除定位必须完整且无歧义。
func TestPlan_LighthouseAmbiguityAndLocators(t *testing.T) {
	// 期望为空 → 所有 Owned 规则成为候选，便于只观察定位判定。
	rules := []config.DomainRule{rule(1, "keep.example.com", "TCP", "443", "ACCEPT", "")}
	resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}}

	t.Run("Lighthouse 同 key 重复", func(t *testing.T) {
		existing := []config.RuleInfo{
			{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"},
			{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"},
		}
		plan := planFor(t, config.CloudTCLighthouse, rules, resolved, existing)
		if len(plan.CleanupCandidates) != 2 {
			t.Fatalf("CleanupCandidates 数量 = %d, want 2", len(plan.CleanupCandidates))
		}
		if !hasIssue(plan.CleanupDeferred, IssueOwnedKeyAmbiguous) {
			t.Fatalf("重复 key 必须 deferred: %+v", plan.CleanupDeferred)
		}
	})

	t.Run("Lighthouse Owned 与 External 同 key", func(t *testing.T) {
		existing := []config.RuleInfo{
			{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"},
			{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "手动规则"},
		}
		plan := planFor(t, config.CloudTCLighthouse, rules, resolved, existing)
		if !hasIssue(plan.CleanupDeferred, IssueOwnedKeyAmbiguous) {
			t.Fatalf("Owned/External 同 key 必须 deferred: %+v", plan.CleanupDeferred)
		}
	})

	t.Run("CVM 缺 PolicyIndex", func(t *testing.T) {
		existing := []config.RuleInfo{
			{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"},
		}
		plan := planFor(t, config.CloudTCCVM, rules, resolved, existing)
		if !hasIssue(plan.CleanupDeferred, IssueOwnedLocatorMissing) {
			t.Fatalf("CVM 缺 PolicyIndex 必须 deferred: %+v", plan.CleanupDeferred)
		}
	})

	t.Run("SWAS 缺 RuleId", func(t *testing.T) {
		existing := []config.RuleInfo{
			{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"},
		}
		plan := planFor(t, config.CloudAliSWAS, rules, resolved, existing)
		if !hasIssue(plan.CleanupDeferred, IssueOwnedLocatorMissing) {
			t.Fatalf("SWAS 缺 RuleId 必须 deferred: %+v", plan.CleanupDeferred)
		}
	})

	t.Run("ECS 缺 SecurityGroupRuleId", func(t *testing.T) {
		existing := []config.RuleInfo{
			{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"},
		}
		plan := planFor(t, config.CloudAliECS, rules, resolved, existing)
		if !hasIssue(plan.CleanupDeferred, IssueOwnedLocatorMissing) {
			t.Fatalf("ECS 缺 SecurityGroupRuleId 必须 deferred: %+v", plan.CleanupDeferred)
		}
	})

	t.Run("SWAS 有 RuleId 时无定位问题", func(t *testing.T) {
		existing := []config.RuleInfo{
			// 期望功能已在快照中被 Owned 规则精确覆盖（模拟 S1 形态），使清理门只剩定位检查。
			{Protocol: "TCP", Port: "443/443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "r-keep"},
			{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "r-stale"},
		}
		plan := planFor(t, config.CloudAliSWAS, rules, resolved, existing)
		if hasIssue(plan.CleanupDeferred, IssueOwnedLocatorMissing) {
			t.Fatalf("有 RuleId 时不得报定位缺失: %+v", plan.CleanupDeferred)
		}
		if !plan.CoverageReady {
			t.Fatalf("期望已被 Owned 精确覆盖时必须 CoverageReady=true: %+v", plan.SatisfiedByOwned)
		}
		if len(plan.CleanupDeferred) != 0 {
			t.Fatalf("无其它门禁问题时 deferred 应为空: %+v", plan.CleanupDeferred)
		}
		if len(plan.CleanupCandidates) != 1 || plan.CleanupCandidates[0].RuleID != "r-stale" {
			t.Fatalf("候选应只有陈旧规则: %+v", plan.CleanupCandidates)
		}
	})
}

// TestPlan_OppositeActionConflict 同 family/CIDR/protocol/port 同时存在 ACCEPT/DROP 时
// 只提示并冻结清理，不做包含或优先级推断。
func TestPlan_OppositeActionConflict(t *testing.T) {
	rules := []config.DomainRule{rule(1, "keep.example.com", "TCP", "443", "ACCEPT", "")}
	resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}}
	existing := []config.RuleInfo{
		{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "r1"},
		{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "DROP", Description: "手动规则"},
	}
	plan := planFor(t, config.CloudAliSWAS, rules, resolved, existing)
	if !hasIssue(plan.Conflicts, IssueOppositeAction) {
		t.Fatalf("缺少 %s: %+v", IssueOppositeAction, plan.Conflicts)
	}
	if !hasIssue(plan.CleanupDeferred, IssueOppositeAction) {
		t.Fatalf("conflict 必须冻结清理: %+v", plan.CleanupDeferred)
	}
}

// TestPlan_SnapshotRuleInvalid 云端规则字段不足（地址族同时有值或都为空等）不得进入删除集合。
func TestPlan_SnapshotRuleInvalid(t *testing.T) {
	rules := []config.DomainRule{rule(1, "keep.example.com", "TCP", "443", "ACCEPT", "")}
	resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}}
	existing := []config.RuleInfo{
		{Protocol: "TCP", Port: "9999", CidrBlock: "9.9.9.9/32", Ipv6CidrBlock: "2001:db8::/128", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "bad1"},
		{Protocol: "TCP", Port: "9999", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "bad2"},
		{Protocol: "", Port: "9999", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "bad3"},
	}
	plan := planFor(t, config.CloudAliSWAS, rules, resolved, existing)
	if len(plan.Conflicts) != 3 {
		t.Fatalf("Conflicts 数量 = %d, want 3: %+v", len(plan.Conflicts), plan.Conflicts)
	}
	for _, it := range plan.Conflicts {
		if it.Code != IssueSnapshotRuleInvalid {
			t.Fatalf("冲突码 = %q, want %q", it.Code, IssueSnapshotRuleInvalid)
		}
	}
	if len(plan.CleanupCandidates) != 0 {
		t.Fatalf("字段不足的规则绝不进入候选: %+v", plan.CleanupCandidates)
	}
	if !hasIssue(plan.CleanupDeferred, IssueSnapshotRuleInvalid) {
		t.Fatalf("snapshot_rule_invalid 必须冻结清理: %+v", plan.CleanupDeferred)
	}
}

// TestPlan_CoreFilesNeverUseCommentForIdentity 锁定「comment/description 不进入功能 key」。
func TestPlan_CoreFilesNeverUseCommentForIdentity(t *testing.T) {
	a := config.RuleAction{Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns] A"}
	b := config.RuleAction{Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns] 完全不同的备注"}
	ka, kb := PlannedActionKeys(a), PlannedActionKeys(b)
	if len(ka) != 1 || len(kb) != 1 || ka[0] != kb[0] {
		t.Fatalf("comment 不得影响功能 key: %v vs %v", ka, kb)
	}

	// 期望侧与回读侧必须走同一 canonical 层
	info := config.RuleInfo{Protocol: "tcp", Port: "443/443", CidrBlock: "1.1.1.1/32", Action: "accept"}
	keys, ok := SnapshotRuleKeys(info)
	if !ok || len(keys) != 1 {
		t.Fatalf("SnapshotRuleKeys = %v, %v", keys, ok)
	}
	if keys[0] != ka[0] {
		t.Fatalf("期望 key %v 与回读 key %v 必须同一 canonical 形态", ka[0], keys[0])
	}
}

// TestPlan_LighthouseDesiredMultiPortExpandsPerPort 核验补强（2026-09-29）：
// Issue7 §2.5 的用户裁决要求「展开项同时是 Desired 与 ToAdd 的最小单元」，
// 因此 Lighthouse 期望侧的多端口必须落成**多个单端口功能**，新建时下发多条单端口规则。
//
// 修复前的缺陷形态：Desired 只产出合并 key（TCP 80,443），而云端回读会把同一条
// 合并规则展开为 80 与 443 两个 key → 每轮重复 Add、已存在规则反成清理候选、
// coverage_ready 恒 false（Lighthouse 多端口目标不收敛）。
func TestPlan_LighthouseDesiredMultiPortExpandsPerPort(t *testing.T) {
	rules := []config.DomainRule{rule(1, "api.example.com", "TCP", "80,443", "ACCEPT", "")}
	resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}}

	// 云端为空：必须产出两个独立功能，且下发的是单端口线格式（不得再出现逗号规则）。
	empty := planFor(t, config.CloudTCLighthouse, rules, resolved, nil)
	if len(empty.Desired) != 2 {
		t.Fatalf("Desired 数量 = %d, want 2（多端口必须逐项展开）: %+v", len(empty.Desired), empty.Desired)
	}
	if len(empty.ToAdd) != 2 {
		t.Fatalf("ToAdd 数量 = %d, want 2（展开项即 ToAdd 最小单元）: %+v", len(empty.ToAdd), empty.ToAdd)
	}
	seen := make(map[string]bool, 2)
	for _, a := range empty.ToAdd {
		if strings.Contains(a.Port, ",") {
			t.Fatalf("新建不得下发合并的逗号端口规则: %+v", a)
		}
		seen[a.Port] = true
	}
	if !seen["80"] || !seen["443"] {
		t.Fatalf("ToAdd 端口 = %v, want 80 与 443 各一条单端口规则", seen)
	}

	// 已存在的合并规则覆盖展开后的全部 key：零新增、零候选（§12.3 偏差 1 的前提）。
	merged := []config.RuleInfo{{
		Protocol: "TCP", Port: "80,443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]",
	}}
	converged := planFor(t, config.CloudTCLighthouse, rules, resolved, merged)
	if len(converged.ToAdd) != 0 || len(converged.CleanupCandidates) != 0 {
		t.Fatalf("已存在合并规则时不得重建或清理: ToAdd=%d CleanupCandidates=%d",
			len(converged.ToAdd), len(converged.CleanupCandidates))
	}
	if len(converged.SatisfiedByOwned) != 2 || !converged.CoverageReady {
		t.Fatalf("合并规则必须精确覆盖两个展开 key: owned=%d coverage=%v",
			len(converged.SatisfiedByOwned), converged.CoverageReady)
	}

	// 两个单端口规则同样必须收敛（新建后的稳定形态）。
	twoRules := []config.RuleInfo{
		{Protocol: "TCP", Port: "80", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]"},
		{Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]"},
	}
	steady := planFor(t, config.CloudTCLighthouse, rules, resolved, twoRules)
	if len(steady.ToAdd) != 0 || len(steady.CleanupCandidates) != 0 {
		t.Fatalf("单端口稳定形态必须收敛: ToAdd=%d CleanupCandidates=%d",
			len(steady.ToAdd), len(steady.CleanupCandidates))
	}
}

// TestPlan_LighthouseCommaPortsExpandPerKey Lighthouse 逗号端口按业务语义展开为独立 key。
func TestPlan_LighthouseCommaPortsExpandPerKey(t *testing.T) {
	keys, ok := SnapshotRuleKeys(config.RuleInfo{
		Protocol: "TCP", Port: "80,443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]",
	})
	if !ok || len(keys) != 2 {
		t.Fatalf("逗号端口必须展开为两个 key: %v ok=%v", keys, ok)
	}
	// 顺序不同的逗号串是同一功能集合
	keys2, _ := SnapshotRuleKeys(config.RuleInfo{
		Protocol: "TCP", Port: "443,80", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]",
	})
	if keys[0] != keys2[0] || keys[1] != keys2[1] {
		t.Fatalf("顺序不同的逗号串必须等价: %v vs %v", keys, keys2)
	}

	// 一条逗号规则已覆盖期望中的单个端口时，不得重建该端口，也不得把该规则列为候选。
	rules := []config.DomainRule{rule(1, "api.example.com", "TCP", "80", "ACCEPT", "")}
	resolved := map[int][]dns.ResolvedIP{1: {v4("1.1.1.1")}}
	existing := []config.RuleInfo{{
		Protocol: "TCP", Port: "80,443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[auto-dns]",
	}}
	plan := planFor(t, config.CloudTCLighthouse, rules, resolved, existing)
	if len(plan.ToAdd) != 0 {
		t.Fatalf("逗号规则已覆盖期望端口，不得新增: %+v", plan.ToAdd)
	}
	if len(plan.CleanupCandidates) != 0 {
		t.Fatalf("任一 key 属于 Desired 的规则不得成为候选: %+v", plan.CleanupCandidates)
	}
}
