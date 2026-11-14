package provider

import (
	"net/netip"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func TestEdgeOnePlanProtectedPrefixDefersOnlyEdgeOneCleanup(t *testing.T) {
	edgeSite := config.EdgeOneSite{
		ZoneID: "zone-a", TargetIDs: []int{1}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
	}
	snapshot := OriginACLSnapshot{
		ZoneID:  "zone-a",
		Current: OriginACLVersion{Version: "v1", Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}},
	}
	rules := []config.RuleInfo{
		{CidrBlock: "203.0.113.0/24", Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: "[edgeone]", RuleID: "keep-a"},
		{CidrBlock: "198.51.100.0/24", Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: "[edgeone]", RuleID: "protect-b"},
		{CidrBlock: "192.0.2.55/32", Protocol: "TCP", Port: "9999", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "stale-dns"},
	}
	base := TargetPlanInput{
		CloudType:  config.CloudAliECS,
		Tag:        "auto-dns",
		EdgeOneTag: "edgeone",
		EdgeOneSites: []EdgeOneSitePlanInput{{
			Site: edgeSite, Snapshot: snapshot,
		}},
		Snapshot: RuleSnapshot{Rules: rules, Revision: "1"},
	}
	plan := PlanTarget(base)
	if !plan.CoverageReady {
		t.Fatalf("夹具应覆盖完整：%+v", plan)
	}
	if len(plan.CleanupDeletable) != 2 {
		t.Fatalf("无保护时两个陈旧候选都可删除：%+v", plan.CleanupDeletable)
	}

	base.ProtectedPrefixes = []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}
	plan = PlanTarget(base)
	if len(plan.CleanupCandidates) != 2 {
		t.Fatalf("保护候选仍应作为预览：%+v", plan.CleanupCandidates)
	}
	if len(plan.CleanupDeletable) != 1 || plan.CleanupDeletable[0].RuleID != "stale-dns" {
		t.Fatalf("只允许删除未受保护的 DNS 候选：%+v", plan.CleanupDeletable)
	}
	if len(plan.CleanupDeferred) != 1 || plan.CleanupDeferred[0].Code != IssueTransitionProtected {
		t.Fatalf("受保护候选必须记录 transition_protected：%+v", plan.CleanupDeferred)
	}

	base.ProtectedUnknown = true
	plan = PlanTarget(base)
	if len(plan.CleanupDeletable) != 1 || plan.CleanupDeletable[0].RuleID != "stale-dns" {
		t.Fatalf("保护未知时仍允许 DNS TAG 清理：%+v", plan.CleanupDeletable)
	}
}

func TestEdgeOnePlanProtectedPrefixUsesContainment(t *testing.T) {
	edgeSite := config.EdgeOneSite{
		ZoneID: "zone-a", TargetIDs: []int{1}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
	}
	snapshot := OriginACLSnapshot{
		ZoneID:  "zone-a",
		Current: OriginACLVersion{Version: "v1", Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}},
	}
	plan := PlanTarget(TargetPlanInput{
		CloudType: config.CloudAliECS, Tag: "auto-dns", EdgeOneTag: "edgeone",
		EdgeOneSites: []EdgeOneSitePlanInput{{Site: edgeSite, Snapshot: snapshot}},
		Snapshot: RuleSnapshot{Rules: []config.RuleInfo{
			{CidrBlock: "203.0.113.0/24", Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: "[edgeone]", RuleID: "keep"},
			{CidrBlock: "198.51.100.0/25", Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: "[edgeone]", RuleID: "protect"},
		}, Revision: "1"},
		ProtectedPrefixes: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")},
	})
	if len(plan.CleanupDeletable) != 0 {
		t.Fatalf("保护 /24 内的 /25 候选不得删除：%+v", plan.CleanupDeletable)
	}
}

func TestEdgeOnePlanOtherSiteStillNeedsOldPrefix(t *testing.T) {
	siteB := config.EdgeOneSite{ZoneID: "zone-b", TargetIDs: []int{1}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"}
	siteA := config.EdgeOneSite{ZoneID: "zone-a", TargetIDs: []int{1}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"}
	plan := PlanTarget(TargetPlanInput{
		CloudType: config.CloudAliECS, Tag: "auto-dns", EdgeOneTag: "edgeone",
		EdgeOneSites: []EdgeOneSitePlanInput{
			{Site: siteB, Snapshot: OriginACLSnapshot{ZoneID: "zone-b", Current: OriginACLVersion{Version: "vB", Prefixes: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}}}},
			{Site: siteA, Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: OriginACLVersion{Version: "vA", Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}}}},
		},
		Snapshot: RuleSnapshot{Rules: []config.RuleInfo{
			{CidrBlock: "198.51.100.0/24", Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: "[edgeone]", RuleID: "b"},
			{CidrBlock: "203.0.113.0/24", Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: "[edgeone]", RuleID: "a"},
		}, Revision: "1"},
		ProtectedPrefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")},
	})
	if len(plan.CleanupCandidates) != 0 || len(plan.CleanupDeletable) != 0 {
		t.Fatalf("另一站点仍需 A 时不得进入清理候选：%+v", plan.CleanupCandidates)
	}
	if !plan.CoverageReady {
		t.Fatalf("A+B 完整覆盖应就绪：%+v", plan.Conflicts)
	}
}
