package provider

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 本文件只验证 Build9 Step 3 的纯运行期核验/清理核心，不访问网络或数据库。

func compressionRule(cidr, port, desc string) config.RuleInfo {
	return config.RuleInfo{
		Protocol: "TCP", Port: port, CidrBlock: cidr, Action: "ACCEPT", Description: desc,
	}
}

func compressionActionFromRow(row CompressionRow) config.RuleInfo {
	return config.RuleInfo{
		Protocol: row.Protocol, Port: row.Port, CidrBlock: row.CIDR, Action: row.Action, Description: row.Description,
	}
}

func TestBuild9S3ReadbackRequiresWitnessPorts(t *testing.T) {
	site := compressionSiteInput("zone-a", []string{"10.0.0.0/32"}, "TCP", "80,443")
	in := LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse, EdgeOneTag: "edgeone",
		Config:   CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		Snapshot: RuleSnapshot{Revision: "1"},
		Sites:    []EdgeOneSitePlanInput{site},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || len(plan.Selected.Rows) != 1 {
		t.Fatalf("计划应生成一条 80,443 行: %+v", plan)
	}
	row := plan.Selected.Rows[0]
	partial := compressionActionFromRow(row)
	partial.Port = "80"
	readback := VerifyCompressionReadback(LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse, EdgeOneTag: "edgeone",
		Config:   in.Config,
		Snapshot: RuleSnapshot{Revision: "2", Rules: []config.RuleInfo{partial}},
		Sites:    in.Sites,
	}, *plan.Selected)
	if readback.Complete || len(readback.MissingRows) == 0 {
		t.Fatalf("缺 443 端口不得证明完整覆盖: %+v", readback)
	}

	otherTag := compressionActionFromRow(row)
	otherTag.Description = "[auto-dns]"
	readback = VerifyCompressionReadback(LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse, EdgeOneTag: "edgeone",
		Config:   in.Config,
		Snapshot: RuleSnapshot{Revision: "2", Rules: []config.RuleInfo{otherTag}},
		Sites:    in.Sites,
	}, *plan.Selected)
	if readback.Complete {
		t.Fatalf("另一 TAG 等价行不得计压缩 witness: %+v", readback)
	}

	full := compressionActionFromRow(row)
	readback = VerifyCompressionReadback(LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse, EdgeOneTag: "edgeone",
		Config:   in.Config,
		Snapshot: RuleSnapshot{Revision: "2", Rules: []config.RuleInfo{full}},
		Sites:    in.Sites,
	}, *plan.Selected)
	if !readback.Complete {
		t.Fatalf("完整 80,443 行应证明覆盖: %+v", readback)
	}
}

func TestBuild9S3CleanupBatchKeepsCoverage(t *testing.T) {
	site := compressionSiteInput("zone-a", []string{"10.0.0.0/25"}, "TCP", "80")
	in := LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse, EdgeOneTag: "edgeone",
		Config: CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		Snapshot: RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{
			compressionRule("10.0.0.0/24", "80", "[edgeone]"),
			compressionRule("10.0.0.0/25", "80", "[edgeone]"),
		}},
		Sites: []EdgeOneSitePlanInput{site},
	}
	plan := PlanCompressionCleanup(CompressionCleanupInput{
		Snapshot: in.Snapshot, Tag: in.EdgeOneTag, OtherTag: "auto-dns",
		Sites: in.Sites, EligibleSites: in.Sites,
	})
	if len(plan.Deletable) != 1 || plan.Remaining != 1 {
		t.Fatalf("整批删除会失去覆盖时只能提交一个子集: %+v", plan)
	}
	if !plan.CoverageAfter.Complete {
		t.Fatalf("删除后仍须完整覆盖: %+v", plan.CoverageAfter)
	}
}

func TestBuild9S3CleanupEvidence96And97(t *testing.T) {
	makeInput := func(oldRows int) LighthouseCompressionInput {
		site := compressionSiteInput("zone-a", []string{"10.0.0.0/32", "10.0.0.2/32"}, "TCP", "1-65535")
		next := edgeVersion("v2", "10.0.0.4/32", "10.0.0.6/32", "10.0.0.8/32", "10.0.0.10/32")
		site.Snapshot.Next = &next
		rules := make([]config.RuleInfo, 0, 96)
		// 当前版两行复用，不进入释放证据。
		rules = append(rules,
			compressionRule("10.0.0.0/32", "ALL", "[edgeone]"),
			compressionRule("10.0.0.2/32", "ALL", "[edgeone]"),
		)
		for i := 0; i < oldRows; i++ {
			rules = append(rules, compressionRule(fmt.Sprintf("10.0.0.%d/32", 12+i*2), "ALL", "[edgeone]"))
		}
		for len(rules) < 96 {
			rules = append(rules, compressionRule(fmt.Sprintf("192.0.2.%d/32", len(rules)), "9999", "manual"))
		}
		in := LighthouseCompressionInput{
			CloudType: config.CloudTCLighthouse, EdgeOneTag: "edgeone",
			Config:   CompressionConfig{Enabled: true, ExtraIPv4Limit: 8},
			Snapshot: RuleSnapshot{Revision: "1", Rules: rules},
			Sites:    []EdgeOneSitePlanInput{site},
		}
		in.PendingFingerprint = CompressionPendingFingerprint(in.Sites)
		in.AssessCleanup = func(rep CompressionRepresentation) CompressionCleanupEvidence {
			return AssessCompressionCleanupEvidence(in, in.Sites, rep)
		}
		return in
	}

	in := makeInput(4)
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Capacity.Mode != CompressionModeBorrow ||
		plan.Capacity.Peak != 100 || plan.Capacity.ExpectedAfter != 96 || plan.Capacity.ExpectedCleanupRows != 4 {
		t.Fatalf("峰值 100 清理 4 条应允许借用: capacity=%+v blockers=%+v selected=%+v", plan.Capacity, plan.Blockers, plan.Selected)
	}

	in = makeInput(3)
	plan = runCompressionPlan(t, in)
	if plan.Selected != nil || plan.Capacity.Mode != CompressionModeBlocked {
		t.Fatalf("峰值 100 清理 3 条应阻断借用: capacity=%+v blockers=%+v", plan.Capacity, plan.Blockers)
	}
	found := false
	for _, blocker := range plan.Blockers {
		if blocker.Code == CompressionBlockedCleanupInsufficient {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报告 cleanup_insufficient: %+v", plan.Blockers)
	}
}

func TestBuild9S3PendingFingerprintComposite(t *testing.T) {
	a := compressionSiteInput("zone-a", []string{"10.0.0.0/32"}, "TCP", "80")
	b := compressionSiteInput("zone-b", []string{"10.0.0.1/32"}, "TCP", "443")
	nextA := edgeVersion("v2", "10.0.0.4/32")
	nextB := edgeVersion("v2", "10.0.0.5/32")
	a.Snapshot.Next, b.Snapshot.Next = &nextA, &nextB
	first := CompressionPendingFingerprint([]EdgeOneSitePlanInput{b, a})
	second := CompressionPendingFingerprint([]EdgeOneSitePlanInput{a, b})
	if first == "" || first != second {
		t.Fatalf("复合指纹必须稳定: %q %q", first, second)
	}
	nextB.Prefixes[0] = netip.MustParsePrefix("10.0.0.6/32")
	if third := CompressionPendingFingerprint([]EdgeOneSitePlanInput{a, b}); third == first {
		t.Fatalf("待更新网段变化必须改变指纹: %q", third)
	}
}

func TestBuild9S3CoverageOppositeAction(t *testing.T) {
	site := compressionSiteInput("zone-a", []string{"10.0.0.0/32"}, "TCP", "80")
	snapshot := RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{
		compressionRule("10.0.0.0/32", "ALL", "[edgeone]"),
		{Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.0/32", Action: "DROP", Description: "manual"},
	}}
	coverage := CompressionCoverageFromRules(snapshot, "edgeone", []EdgeOneSitePlanInput{site}, nil, 0)
	if coverage.Complete || len(coverage.Conflicts) == 0 {
		t.Fatalf("相反动作必须冻结覆盖: %+v", coverage)
	}
}

func TestBuild9S3CleanupEvidenceRejectsMissingPendingCoverage(t *testing.T) {
	site := compressionSiteInput("zone-a", []string{"10.0.0.0/32"}, "TCP", "80")
	next := edgeVersion("v2", "10.0.0.4/32")
	site.Snapshot.Next = &next
	rules := []config.RuleInfo{compressionRule("10.0.0.0/32", "ALL", "[edgeone]")}
	in := LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse, EdgeOneTag: "edgeone",
		Config:   CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		Snapshot: RuleSnapshot{Revision: "1", Rules: rules},
		Sites:    []EdgeOneSitePlanInput{site},
	}
	in.PendingFingerprint = CompressionPendingFingerprint(in.Sites)
	candidate := CompressionRepresentation{
		Rows: []CompressionRow{{Protocol: "TCP", Port: "80", CIDR: "10.0.0.0/32", Action: "ACCEPT", Description: "[edgeone]"}},
	}
	evidence := AssessCompressionCleanupEvidence(in, in.Sites, candidate)
	if len(evidence.ReleasableRows) != 0 {
		t.Fatalf("待更新版缺覆盖时不得产生借用证据: %+v", evidence)
	}
}
