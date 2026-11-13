package provider

import (
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

// 停用来源的 TAG 只作为只读精确覆盖：不计 owned、不报跨 TAG 冲突、
// 不新增、不删除、不改标。
func TestEdgeOnePlanCoverageOnlyTagDNSActive(t *testing.T) {
	existing := config.RuleInfo{
		Protocol: "TCP", Port: "443", CidrBlock: "203.0.113.0/32",
		Action: "ACCEPT", Description: "[edgeone] inactive", RuleID: "edge-rule",
	}
	plan := PlanTarget(TargetPlanInput{
		CloudType:       config.CloudTCLighthouse,
		Tag:             "auto-dns",
		OtherTag:        "edgeone",
		CoverageOnlyTag: "edgeone",
		Rules:           []config.DomainRule{rule(1, "home.example", "TCP", "443", "ACCEPT", "")},
		Resolved:        map[int][]dns.ResolvedIP{1: {v4("203.0.113.0")}},
		Snapshot:        RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{existing}},
	})
	if len(plan.Desired) != 1 || len(plan.SatisfiedByExternal) != 1 || len(plan.SatisfiedByOwned) != 0 {
		t.Fatalf("停用 TAG 应提供只读覆盖: %+v", plan)
	}
	if len(plan.Conflicts) != 0 || len(plan.ToAdd) != 0 || len(plan.CleanupCandidates) != 0 ||
		len(plan.CleanupDeletable) != 0 {
		t.Fatalf("停用 TAG 不得冲突/新增/清理: %+v", plan)
	}
	if !plan.CoverageReady {
		t.Fatalf("精确外部覆盖应使 CoverageReady=true: %+v", plan)
	}
}

func TestEdgeOnePlanCoverageOnlyTagEdgeOneActive(t *testing.T) {
	existing := config.RuleInfo{
		Protocol: "TCP", Port: "443", CidrBlock: "203.0.113.0/32",
		Action: "ACCEPT", Description: "[auto-dns] inactive", RuleID: "dns-rule",
	}
	plan := PlanTarget(TargetPlanInput{
		CloudType:       config.CloudTCLighthouse,
		Tag:             "auto-dns",
		EdgeOneTag:      "edgeone",
		CoverageOnlyTag: "auto-dns",
		EdgeOneSites: []EdgeOneSitePlanInput{{
			Site:     edgeSite("zone-a", []int{1}, "TCP", "443", "ipv4"),
			Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("1", "203.0.113.0/32")},
		}},
		Snapshot: RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{existing}},
	})
	if len(plan.Desired) != 1 || len(plan.SatisfiedByExternal) != 1 || len(plan.SatisfiedByOwned) != 0 {
		t.Fatalf("停用 DNS TAG 应提供只读覆盖: %+v", plan)
	}
	if len(plan.Conflicts) != 0 || len(plan.ToAdd) != 0 || len(plan.CleanupCandidates) != 0 ||
		len(plan.CleanupDeletable) != 0 {
		t.Fatalf("停用 TAG 不得冲突/新增/清理: %+v", plan)
	}
	if !plan.CoverageReady {
		t.Fatalf("精确外部覆盖应使 CoverageReady=true: %+v", plan)
	}
}
