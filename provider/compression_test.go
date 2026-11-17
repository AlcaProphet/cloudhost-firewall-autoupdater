package provider

import (
	"context"
	"fmt"
	"math/bits"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func compressionSiteInput(zone string, prefixes []string, protocol, ports string) EdgeOneSitePlanInput {
	return EdgeOneSitePlanInput{
		Site: edgeSite(zone, []int{1}, protocol, ports, "ipv4"),
		Snapshot: OriginACLSnapshot{
			ZoneID:  zone,
			Current: edgeVersion("v1", prefixes...),
		},
	}
}

func runCompressionPlan(t *testing.T, in LighthouseCompressionInput) LighthouseCompressionPlan {
	t.Helper()
	if in.CloudType == "" {
		in.CloudType = config.CloudTCLighthouse
	}
	if in.EdgeOneTag == "" {
		in.EdgeOneTag = "edgeone"
	}
	plan := PlanLighthouseCompression(context.Background(), in)
	if selected := plan.Selected; selected != nil {
		if plan.Capacity.AddRows != len(selected.AddRows) ||
			selected.CurrentAdds+selected.PendingExtra != len(selected.AddRows) ||
			plan.Capacity.Peak != len(in.Snapshot.Rules)+in.OtherNewRows+len(selected.AddRows) {
			t.Fatalf("计划物理行数与容量不一致: %+v capacity=%+v", selected, plan.Capacity)
		}
	}
	return plan
}

func TestBuild9S2LosslessMergeAndPorts(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-a",
			[]string{"10.0.0.0/25", "10.0.0.128/25"}, "TCP", "80,443")},
		Snapshot: RuleSnapshot{Revision: "1"},
	}
	plan := runCompressionPlan(t, in)
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers=%+v", plan.Blockers)
	}
	if plan.Selected == nil || len(plan.Selected.Rows) != 1 {
		t.Fatalf("rows=%+v", plan.Selected)
	}
	row := plan.Selected.Rows[0]
	if row.CIDR != "10.0.0.0/24" || row.Port != "80,443" || row.Protocol != "TCP" {
		t.Fatalf("row=%+v", row)
	}
	if plan.Selected.ExtraIPv4 != 0 || !plan.Selected.Exact {
		t.Fatalf("无损结果错误: extra=%d exact=%v", plan.Selected.ExtraIPv4, plan.Selected.Exact)
	}
	if len(plan.Selected.Witnesses) != 4 {
		t.Fatalf("witnesses=%d, want 4", len(plan.Selected.Witnesses))
	}
	if plan.Capacity.Mode != CompressionModeNormal || plan.Capacity.Peak != 1 {
		t.Fatalf("capacity=%+v", plan.Capacity)
	}
}

func TestBuild9S2DifferentAuthSetsNoMerge(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{
			compressionSiteInput("zone-a", []string{"10.0.0.0/24"}, "TCP", "80"),
			compressionSiteInput("zone-b", []string{"10.0.1.0/24"}, "TCP", "80,443"),
		},
		Snapshot: RuleSnapshot{Revision: "1"},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || len(plan.Selected.Rows) != 2 {
		t.Fatalf("rows=%+v blockers=%+v", plan.Selected, plan.Blockers)
	}
	for _, row := range plan.Selected.Rows {
		if row.Port == "80,443" && row.CIDR == "10.0.0.0/24" {
			t.Fatalf("不同授权集合被错误合并: %+v", plan.Selected.Rows)
		}
	}
}

func TestBuild9S2PortPacking64(t *testing.T) {
	ports := make([]string, 0, 14)
	for p := 1000; p <= 1013; p++ {
		ports = append(ports, fmt.Sprintf("%d", p))
	}
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites:      []EdgeOneSitePlanInput{compressionSiteInput("zone-a", []string{"10.0.0.0/24"}, "TCP", strings.Join(ports[:13], ","))},
		Snapshot:   RuleSnapshot{Revision: "1"},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || len(plan.Selected.Rows) != 1 || len(plan.Selected.Rows[0].Port) != 64 {
		t.Fatalf("13 端口应恰好 64 字符: %+v", plan.Selected)
	}
	in.Sites = []EdgeOneSitePlanInput{compressionSiteInput("zone-a", []string{"10.0.0.0/24"}, "TCP", strings.Join(ports, ","))}
	plan = runCompressionPlan(t, in)
	if plan.Selected == nil || len(plan.Selected.Rows) != 2 {
		t.Fatalf("14 端口应拆两条: %+v", plan.Selected)
	}
}

func TestBuild9S2CapacityBoundaries(t *testing.T) {
	mk := func(s0, other, cur, pending int, hasPending bool, cleanup int) CompressionCapacity {
		return EvaluateCompressionCapacity(CompressionCapacityInput{
			S0Rows: s0, OtherNewRows: other, CurrentAdds: cur, PendingExtra: pending,
			HasPendingVersion: hasPending, PendingFingerprint: "fp", CleanupFingerprint: "fp",
			ExpectedCleanupRows: cleanup, CleanupTrusted: hasPending,
		})
	}
	for _, tc := range []struct {
		name string
		cap  CompressionCapacity
		mode string
		peak int
	}{
		{"S0=8 add88 normal", mk(8, 0, 0, 88, false, 0), CompressionModeNormal, 96},
		{"S0=8 add89 pending cleanup1", mk(8, 0, 0, 89, true, 1), CompressionModeBorrow, 97},
		{"S0=8 add89 no pending", mk(8, 0, 0, 89, false, 0), CompressionModeBlocked, 97},
		{"S0=96 add1 cleanup1", mk(96, 0, 0, 1, true, 1), CompressionModeBorrow, 97},
		{"S0=96 add4 cleanup4", mk(96, 0, 0, 4, true, 4), CompressionModeBorrow, 100},
		{"S0=96 add4 cleanup3", mk(96, 0, 0, 4, true, 3), CompressionModeBlocked, 100},
		{"S0=96 add5 hard", mk(96, 0, 0, 5, true, 5), CompressionModeBlocked, 101},
		{"S0=92 add4 normal", mk(92, 0, 0, 4, false, 0), CompressionModeNormal, 96},
		{"S0=92 add5 borrow", mk(92, 0, 0, 5, true, 1), CompressionModeBorrow, 97},
		{"S0=90 other2 add5 block", mk(90, 2, 0, 5, false, 0), CompressionModeBlocked, 97},
		{"current add cannot borrow", mk(96, 0, 1, 0, false, 0), CompressionModeBlocked, 97},
		{"cleanup only", mk(97, 0, 0, 0, false, 0), CompressionModeCleanupOnly, 97},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cap.Mode != tc.mode || tc.cap.Peak != tc.peak {
				t.Fatalf("mode=%s peak=%d want mode=%s peak=%d reasons=%+v", tc.cap.Mode, tc.cap.Peak, tc.mode, tc.peak, tc.cap.Reasons)
			}
		})
	}
	// 峰值 100、清理后 96/97 的分界。
	ok := mk(92, 0, 0, 8, true, 4)
	if ok.Mode != CompressionModeBorrow || ok.ExpectedAfter != 96 {
		t.Fatalf("清理后 96 应可借用: %+v", ok)
	}
	bad := mk(92, 0, 0, 8, true, 3)
	if bad.Mode != CompressionModeBlocked || bad.ExpectedAfter != 97 {
		t.Fatalf("清理后 97 应阻断: %+v", bad)
	}
}

func TestBuild9S2RiskCrossPortAndResidue(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 3},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{
			compressionSiteInput("zone-a", []string{"10.0.0.0/30"}, "TCP", "80,443"),
		},
		Snapshot: RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{{
			Protocol: "TCP", Port: "80,443", CidrBlock: "10.0.0.0/29", Action: "ACCEPT", Description: "[edgeone]",
		}}},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected != nil {
		t.Fatalf("旧残留额外 4 个地址应超过额度 3: %+v", plan.Risk)
	}
	if plan.Risk.ExtraIPv4 != 4 || plan.Risk.OldResidue != 4 {
		t.Fatalf("风险核算错误: %+v", plan.Risk)
	}
	in.Config.ExtraIPv4Limit = 4
	plan = runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Risk.ExtraIPv4 != 4 {
		t.Fatalf("额度 4 应允许: %+v", plan.Risk)
	}

	// 同地址在 80 合法、443 因扩张被放行，仍计入风险。
	in = LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 10},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{
			compressionSiteInput("zone-a", []string{"10.0.0.0/29"}, "TCP", "80"),
			compressionSiteInput("zone-b", []string{"10.0.0.0/30"}, "TCP", "443"),
		},
		Snapshot: RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{{
			Protocol: "TCP", Port: "80,443", CidrBlock: "10.0.0.0/29", Action: "ACCEPT", Description: "[edgeone]",
		}}},
	}
	plan = runCompressionPlan(t, in)
	if plan.Risk.ExtraIPv4 != 4 {
		t.Fatalf("同地址在 80 合法、443 额外仍应计 4: %+v", plan.Risk)
	}
}

func TestBuild9S2ManualRuleDoesNotOffset(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 3},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{
			compressionSiteInput("zone-a", []string{"10.0.0.0/30"}, "TCP", "80"),
		},
		Snapshot: RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{
			{Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.0/29", Action: "ACCEPT", Description: "[edgeone]"},
			{Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.0/29", Action: "ACCEPT", Description: "manual"},
		}},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected != nil || plan.Risk.ExtraIPv4 != 4 {
		t.Fatalf("手工规则不得抵扣: selected=%v risk=%+v", plan.Selected, plan.Risk)
	}
	if !plan.Risk.ManualIgnored {
		t.Fatal("应标记存在手工放行")
	}
}

func TestBuild9S2JointSelectionExhaustive(t *testing.T) {
	const first, second uint16 = 1<<8 | 1<<9 | 1<<10 | 1<<13 | 1<<15, 1<<5 | 1<<6 | 1<<9 | 1<<13 | 1<<15
	a, b := bruteCompressionCovers(first, 0), bruteCompressionCovers(second, 0)
	localRisk := 32
	for rows := 1; rows < 5; rows++ {
		local := func(options []smallCompressionCover, count int) (smallCompressionCover, bool) {
			var best smallCompressionCover
			found := false
			for _, option := range options {
				if len(option.cidrs) == count && (!found || betterSmallCompressionCover(option, best)) {
					best, found = option, true
				}
			}
			return best, found
		}
		left, leftOK := local(a, rows)
		right, rightOK := local(b, 5-rows)
		if leftOK && rightOK {
			localRisk = min(localRisk, bits.OnesCount16(left.extra|right.extra))
		}
	}
	if localRisk != 5 {
		t.Fatalf("合成对照的独立局部最优风险=%d，预期 5", localRisk)
	}

	in := LighthouseCompressionInput{
		Config:   CompressionConfig{Enabled: true, ExtraIPv4Limit: 3},
		Snapshot: compressionTestSnapshot(91),
		Sites: []EdgeOneSitePlanInput{
			compressionSiteInput("zone-a", smallCompressionCIDRs(first), "TCP", "80"),
			compressionSiteInput("zone-b", smallCompressionCIDRs(second), "TCP", "443"),
		},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Risk.ExtraIPv4 != 3 || plan.Capacity.AddRows != 5 {
		t.Fatalf("额度 3 应取联合最优: %+v", plan)
	}
	in.Config.ExtraIPv4Limit = 2
	if plan := runCompressionPlan(t, in); plan.Selected != nil || plan.Incomplete {
		t.Fatalf("额度 2 应完整搜索并阻断: %+v", plan)
	}
}

func TestBuild9S2LossyCIDRAndQuota(t *testing.T) {
	s0 := make([]config.RuleInfo, 0, 95)
	for i := 0; i < 95; i++ {
		s0 = append(s0, config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", i+1), Action: "ACCEPT", Description: "manual"})
	}
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 64},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-a",
			[]string{"10.0.0.0/25", "10.0.0.192/26"}, "TCP", "443")},
		Snapshot: RuleSnapshot{Revision: "1", Rules: s0},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Lossy == nil {
		t.Fatalf("应选择有损方案: blockers=%+v", plan.Blockers)
	}
	if len(plan.Selected.Rows) != 1 || plan.Selected.Rows[0].CIDR != "10.0.0.0/24" || plan.Selected.ExtraIPv4 != 64 {
		t.Fatalf("有损覆盖错误: %+v", plan.Selected)
	}
	if plan.Capacity.Mode != CompressionModeNormal || plan.Capacity.Peak != 96 {
		t.Fatalf("容量错误: %+v", plan.Capacity)
	}
	in.Config.ExtraIPv4Limit = 63
	plan = runCompressionPlan(t, in)
	if plan.Selected != nil || len(plan.Blockers) == 0 {
		t.Fatalf("额度 63 应阻断: selected=%v blockers=%+v", plan.Selected, plan.Blockers)
	}
}

func TestBuild9S2FullPortNoLossy(t *testing.T) {
	s0 := make([]config.RuleInfo, 95)
	for i := range s0 {
		s0[i] = config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", i+1), Action: "ACCEPT", Description: "manual"}
	}
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 1000},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-a",
			[]string{"10.0.0.0/24", "10.0.2.0/24"}, "TCP", "ALL")},
		Snapshot: RuleSnapshot{Revision: "1", Rules: s0},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected != nil {
		t.Fatalf("全端口不得有损: %+v", plan.Selected)
	}
	found := false
	for _, blocker := range plan.Blockers {
		if blocker.Code == CompressionBlockedLossyForbidden {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少全端口阻断: %+v", plan.Blockers)
	}
}

func TestBuild9S2DNSAndCancel(t *testing.T) {
	s0 := make([]config.RuleInfo, 95)
	for i := range s0 {
		s0[i] = config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", i+1), Action: "ACCEPT", Description: "manual"}
	}
	base := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 1000},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-a",
			[]string{"10.0.0.0/25", "10.0.0.192/26"}, "TCP", "443")},
		Snapshot:   RuleSnapshot{Revision: "1", Rules: s0},
		DNSEnabled: true,
	}
	incomplete := base
	incomplete.DNSComplete = false
	plan := runCompressionPlan(t, incomplete)
	if plan.Selected != nil {
		t.Fatal("DNS 不完整不得有损")
	}
	hit := base
	hit.DNSComplete = true
	hit.DNSAddresses = []netip.Addr{netip.MustParseAddr("10.0.0.128")}
	plan = runCompressionPlan(t, hit)
	if plan.Selected != nil {
		t.Fatal("DNS 命中额外地址不得有损")
	}
	conflict := base
	conflict.DNSConflict = true
	plan = runCompressionPlan(t, conflict)
	if plan.Selected != nil || len(plan.Blockers) == 0 || plan.Blockers[0].Code != CompressionBlockedDNSConflict {
		t.Fatalf("DNS 冲突必须阻断: %+v", plan)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan = PlanLighthouseCompression(ctx, base)
	if !plan.Incomplete || plan.Selected != nil || len(plan.Blockers) == 0 {
		t.Fatalf("取消必须零计划: %+v", plan)
	}
}

func TestBuild9S2Deterministic(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{
			compressionSiteInput("zone-b", []string{"10.0.1.0/24"}, "TCP", "443"),
			compressionSiteInput("zone-a", []string{"10.0.0.0/25", "10.0.0.128/25"}, "TCP", "80,443"),
		},
		Snapshot: RuleSnapshot{Revision: "1"},
	}
	first := runCompressionPlan(t, in)
	in.Sites[0], in.Sites[1] = in.Sites[1], in.Sites[0]
	second := runCompressionPlan(t, in)
	if !reflect.DeepEqual(first.Selected, second.Selected) {
		t.Fatalf("输入顺序改变了结果:\n%+v\n%+v", first.Selected, second.Selected)
	}
}

func TestBuild9S2MixedS0Count(t *testing.T) {
	rules := []config.RuleInfo{
		{Protocol: "TCP", Port: "22", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: "policy"},
		{Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.1/32", Action: "ACCEPT", Description: "[auto-ddns]"},
		{Protocol: "TCP", Port: "443", CidrBlock: "2001:db8::1/128", Ipv6CidrBlock: "2001:db8::1/128", Action: "ACCEPT", Description: "[auto-ddns]"},
		{Protocol: "TCP", Port: "80,443", CidrBlock: "10.0.1.0/24", Action: "ACCEPT", Description: "[edgeone]"},
		{Protocol: "TCP", Port: "80,443", CidrBlock: "10.0.1.7/32", Action: "ACCEPT", Description: "[edgeone]"},
		{Protocol: "UDP", Port: "53", CidrBlock: "0.0.0.0/0", Action: "DROP", Description: "policy"},
		{Protocol: "TCP", Port: "8000-8010", CidrBlock: "10.0.2.0/24", Action: "ACCEPT", Description: "[edgeone]"},
		{Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "manual"},
	}
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Snapshot:   RuleSnapshot{Revision: "1", Rules: rules},
		Sites:      []EdgeOneSitePlanInput{compressionSiteInput("zone-a", []string{"10.0.1.0/24"}, "TCP", "80,443")},
	}
	plan := runCompressionPlan(t, in)
	if plan.Capacity.S0Rows != 8 {
		t.Fatalf("S0=%d want 8", plan.Capacity.S0Rows)
	}
}

func TestBuild9S2Build8SampleConsistency(t *testing.T) {
	normal := EvaluateCompressionCapacity(CompressionCapacityInput{S0Rows: 8, PendingExtra: 88})
	if normal.Mode != CompressionModeNormal || normal.NormalBudget != 88 || normal.Peak != 96 {
		t.Fatalf("88 条常态错误: %+v", normal)
	}
	noPending := EvaluateCompressionCapacity(CompressionCapacityInput{S0Rows: 8, PendingExtra: 89})
	if noPending.Mode != CompressionModeBlocked || noPending.Reasons[0].Code != CompressionBlockedReserveNeedsPending {
		t.Fatalf("89 条无 pending 应阻断: %+v", noPending)
	}
	borrow := EvaluateCompressionCapacity(CompressionCapacityInput{
		S0Rows: 8, PendingExtra: 92, HasPendingVersion: true,
		PendingFingerprint: "fp", CleanupFingerprint: "fp", CleanupTrusted: true, ExpectedCleanupRows: 4,
	})
	if borrow.Mode != CompressionModeBorrow || borrow.Peak != 100 || borrow.ExpectedAfter != 96 {
		t.Fatalf("92 条借用错误: %+v", borrow)
	}
	bad := EvaluateCompressionCapacity(CompressionCapacityInput{
		S0Rows: 8, PendingExtra: 92, HasPendingVersion: true,
		PendingFingerprint: "fp", CleanupFingerprint: "fp", CleanupTrusted: true, ExpectedCleanupRows: 3,
	})
	if bad.Mode != CompressionModeBlocked || bad.ExpectedAfter != 97 {
		t.Fatalf("92 条清理 3 应阻断: %+v", bad)
	}
	hard := EvaluateCompressionCapacity(CompressionCapacityInput{S0Rows: 8, PendingExtra: 93, HasPendingVersion: true})
	if hard.Mode != CompressionModeBlocked || hard.Reasons[0].Code != CompressionBlockedHardQuota {
		t.Fatalf("93 条应硬阻断: %+v", hard)
	}
}

func TestBuild9S2ExactMergeAndIPv6(t *testing.T) {
	input := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/25"),
		netip.MustParsePrefix("10.0.0.128/25"),
		netip.MustParsePrefix("10.0.0.1/24"),
		netip.MustParsePrefix("10.0.1.0/24"),
		netip.MustParsePrefix("2001:db8::/64"),
	}
	merged := ipv4SetPrefixes(ipv4SetFromPrefixes(input))
	if len(merged) != 1 || merged[0].String() != "10.0.0.0/23" {
		t.Fatalf("无损合并=%v", merged)
	}
	if !ipv4SetFromPrefixes(input).equal(ipv4SetFromPrefixes(merged)) {
		t.Fatal("合并前后地址集合不等")
	}
	for _, p := range merged {
		if !p.Addr().Is4() {
			t.Fatal("IPv6 不得进入 IPv4 合并")
		}
		if sibling, ok := siblingPrefix(p); ok {
			for _, other := range merged {
				if sibling == other {
					t.Fatalf("输出不是极小覆盖: %v", merged)
				}
			}
		}
	}
}

func TestBuild9S2PortRangeAndTCPUDP(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{{
			Site:     edgeSite("zone-a", []int{1}, "TCP", "80,443,8000-8010", "ipv4"),
			Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("v1", "10.0.0.0/24")},
		}},
		Snapshot: RuleSnapshot{Revision: "1"},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || len(plan.Selected.Rows) != 2 {
		t.Fatalf("range/discrete 应分开: %+v", plan.Selected)
	}
	ports := map[string]bool{}
	for _, row := range plan.Selected.Rows {
		ports[row.Port] = true
	}
	if !ports["80,443"] || !ports["8000-8010"] {
		t.Fatalf("端口表达式=%v", ports)
	}

	in.Sites = []EdgeOneSitePlanInput{{
		Site:     edgeSite("zone-a", []int{1}, "TCP+UDP", "80,443", "ipv4"),
		Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("v1", "10.0.0.0/24")},
	}}
	plan = runCompressionPlan(t, in)
	if plan.Selected == nil || len(plan.Selected.Rows) != 2 {
		t.Fatalf("TCP+UDP 应拆两组: %+v", plan.Selected)
	}
	for _, row := range plan.Selected.Rows {
		if row.Protocol != "TCP" && row.Protocol != "UDP" {
			t.Fatalf("不得输出 TCP+UDP 单条: %+v", plan.Selected.Rows)
		}
	}
}

func TestBuild9S2ZeroQuotaLosslessBlocked(t *testing.T) {
	s0 := make([]config.RuleInfo, 95)
	for i := range s0 {
		s0[i] = config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", i+1), Action: "ACCEPT", Description: "manual"}
	}
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-a",
			[]string{"10.0.0.0/24", "10.0.2.0/24"}, "TCP", "443")},
		Snapshot: RuleSnapshot{Revision: "1", Rules: s0},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected != nil || len(plan.Blockers) == 0 {
		t.Fatalf("额度 0 且无损超额应阻断: selected=%v blockers=%+v", plan.Selected, plan.Blockers)
	}
}

func TestBuild9S2IPv6NotCompressed(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{{
			Site:     edgeSite("zone-a", []int{1}, "TCP", "443", "dual"),
			Snapshot: OriginACLSnapshot{ZoneID: "zone-a", Current: edgeVersion("v1", "10.0.0.0/24", "2001:db8::/64")},
		}},
		Snapshot: RuleSnapshot{Revision: "1"},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || len(plan.Selected.Rows) != 1 {
		t.Fatalf("IPv4/IPv6 应只压 IPv4: %+v", plan.Selected)
	}
	for _, row := range plan.Selected.Rows {
		if strings.Contains(row.CIDR, ":") {
			t.Fatalf("IPv6 进入压缩: %+v", row)
		}
	}
}

func TestBuild9S2OppositeActionAndIncomplete(t *testing.T) {
	s0 := make([]config.RuleInfo, 95)
	for i := range s0 {
		s0[i] = config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: fmt.Sprintf("192.0.2.%d/32", i+1), Action: "ACCEPT", Description: "manual"}
	}
	s0[0] = config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: "10.0.0.128/25", Action: "DROP", Description: "policy"}
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 1000},
		EdgeOneTag: "edgeone",
		Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-a",
			[]string{"10.0.0.0/25", "10.0.0.192/26"}, "TCP", "443")},
		Snapshot: RuleSnapshot{Revision: "1", Rules: s0},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected != nil {
		t.Fatalf("与 DROP 重叠的有损候选应被排除: %+v", plan.Selected)
	}

	// 必须从正式纯规划入口覆盖生成与搜索的共享状态预算。
	in.Snapshot.Rules[0] = compressionTestSnapshot(1).Rules[0]
	in.MaxStates = 1
	if plan := runCompressionPlan(t, in); !plan.Incomplete || plan.Selected != nil || plan.Lossy != nil ||
		len(plan.Blockers) != 1 || plan.Blockers[0].Code != CompressionBlockedIncomplete {
		t.Fatalf("状态预算耗尽应零可执行计划: %+v", plan)
	}
}

func TestBuild9S2OtherTagNotReused(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites:      []EdgeOneSitePlanInput{compressionSiteInput("zone-a", []string{"10.0.0.0/24"}, "TCP", "443")},
		Snapshot: RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{{
			Protocol: "TCP", Port: "443", CidrBlock: "10.0.0.0/24", Action: "ACCEPT", Description: "[auto-ddns]",
		}}},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || len(plan.Selected.ReusedRows) != 0 || len(plan.Selected.AddRows) != 1 {
		t.Fatalf("其他 TAG 不得复用: %+v", plan.Selected)
	}
	if plan.Risk.ExtraIPv4 != 0 {
		t.Fatalf("其他 TAG 不得作为 grant: %+v", plan.Risk)
	}
}

func TestBuild9S2ICMPWarningNoCompression(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:     CompressionConfig{Enabled: true, ExtraIPv4Limit: 0},
		EdgeOneTag: "edgeone",
		Sites:      []EdgeOneSitePlanInput{compressionSiteInput("zone-a", []string{"10.0.0.0/24"}, "ICMP", "ALL")},
		Snapshot:   RuleSnapshot{Revision: "1"},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected != nil {
		t.Fatalf("ICMP 不应产生压缩计划: %+v", plan.Selected)
	}
	found := false
	for _, warning := range plan.Warnings {
		if warning.Code == "icmp_not_compressed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少 ICMP 警告: %+v", plan.Warnings)
	}
}

func compressionTestSnapshot(rows int) RuleSnapshot {
	snapshot := RuleSnapshot{Revision: "1", Rules: make([]config.RuleInfo, rows)}
	for i := range snapshot.Rules {
		snapshot.Rules[i] = config.RuleInfo{
			Protocol: "TCP", Port: "9999", Action: "ACCEPT",
			CidrBlock: fmt.Sprintf("192.0.2.%d/32", i+1), Description: "manual",
		}
	}
	return snapshot
}

func TestBuild9S2EquivalentFullPortCountsPhysicalRows(t *testing.T) {
	for _, ports := range []string{"ALL", "1-65535", "1-32767,32768-65535"} {
		t.Run(ports, func(t *testing.T) {
			site := compressionSiteInput("zone-a", []string{"10.0.0.0/32", "10.0.0.2/32"}, "TCP", ports)
			in := LighthouseCompressionInput{
				Config:   CompressionConfig{Enabled: true, ExtraIPv4Limit: 2},
				Snapshot: compressionTestSnapshot(95), Sites: []EdgeOneSitePlanInput{site},
			}
			plan := runCompressionPlan(t, in)
			if plan.Selected != nil || plan.Capacity.Peak != 97 || plan.Capacity.CurrentAdds != 2 ||
				len(plan.Lossless.AddRows) != 2 || len(plan.Lossless.Witnesses) < 2 {
				t.Fatalf("全端口无损计划应按物理新增 2 条阻断: %+v", plan)
			}
			for _, row := range plan.Lossless.Rows {
				if row.Port != "ALL" || !row.ServesCurrent {
					t.Fatalf("全端口物理行错误: %+v", row)
				}
			}
		})
	}
}

func TestBuild9S2FullPortGroupCanStayExact(t *testing.T) {
	in := LighthouseCompressionInput{
		Config:   CompressionConfig{Enabled: true, ExtraIPv4Limit: 2},
		Snapshot: compressionTestSnapshot(94),
		Sites: []EdgeOneSitePlanInput{
			compressionSiteInput("zone-a", []string{"10.0.0.0/32", "10.0.0.2/32"}, "TCP", "80"),
			compressionSiteInput("zone-b", []string{"10.0.0.0/32", "10.0.0.2/32"}, "UDP", "ALL"),
		},
	}
	in.Snapshot.Rules[0] = config.RuleInfo{
		Protocol: "UDP", Port: "ALL", CidrBlock: "10.0.0.0/32", Action: "ACCEPT", Description: "[edgeone]",
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Capacity.AddRows != 2 || plan.Risk.ExtraIPv4 != 2 {
		t.Fatalf("精确 UDP 与有损 TCP 应可组合: %+v", plan)
	}
	for _, row := range plan.Selected.Rows {
		if row.Protocol == "UDP" && row.Lossy {
			t.Fatalf("全端口 UDP 被扩大: %+v", row)
		}
	}
}

func TestBuild9S2OldPortRangeRiskAndProtection(t *testing.T) {
	site := compressionSiteInput("zone-a", []string{"10.0.0.0/30"}, "TCP", "80")
	in := LighthouseCompressionInput{
		Config: CompressionConfig{Enabled: true}, Sites: []EdgeOneSitePlanInput{site},
		Snapshot: RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{{
			Protocol: "TCP", Port: "80-81", CidrBlock: "10.0.0.0/30", Action: "ACCEPT", Description: "[edgeone]",
		}}},
	}
	for _, ports := range []string{"80-81", "ALL", "1-32767,32768-65535"} {
		in.Snapshot.Rules[0].Port = ports
		plan := runCompressionPlan(t, in)
		if plan.Selected != nil || plan.Risk.ExtraIPv4 != 4 || plan.Risk.OldResidue != 4 {
			t.Fatalf("旧端口 %s 在未授权端口仍放行 4 个地址: %+v", ports, plan)
		}
		if !slices.ContainsFunc(plan.Risk.PerPort, func(r CompressionPortRisk) bool {
			return r.Protocol == "TCP" && r.Port != "80" && r.ExtraIPv4 == 4
		}) {
			t.Fatalf("未授权端口无风险明细: %+v", plan.Risk)
		}
	}
	in.Snapshot.Rules[0].Port = "80-81"
	in.Protected = []CompressionAuthorization{{Prefix: "10.0.0.0/30", Protocol: "TCP", PortItem: "81"}}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Risk.ExtraIPv4 != 0 || len(plan.Selected.AddRows) != 1 ||
		plan.Selected.AddRows[0].Port != "80" {
		t.Fatalf("受保护的 81 应抵扣风险，仅 80 作为新增要求: %+v", plan)
	}
	in.Protected[0].Protocol = "UDP"
	if plan := runCompressionPlan(t, in); plan.Selected != nil || plan.Risk.ExtraIPv4 != 4 {
		t.Fatalf("UDP 保护不得抵扣 TCP: %+v", plan)
	}
}

func TestBuild9S2EquivalentAddressUnionsAndPortWitnesses(t *testing.T) {
	in := LighthouseCompressionInput{
		Config: CompressionConfig{Enabled: true}, Snapshot: compressionTestSnapshot(95),
		Sites: []EdgeOneSitePlanInput{
			compressionSiteInput("zone-a", []string{"10.0.0.0/24"}, "TCP", "80"),
			compressionSiteInput("zone-b", []string{"10.0.0.0/25", "10.0.0.128/25"}, "TCP", "443"),
			compressionSiteInput("zone-c", []string{"10.0.0.0/25"}, "TCP", "80"),
		},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Capacity.AddRows != 1 || plan.Selected.AddRows[0].Port != "80,443" ||
		len(plan.Selected.Witnesses) != 4 {
		t.Fatalf("等价精确集合应合并端口并保留所有来源: %+v", plan)
	}

	in.Sites = []EdgeOneSitePlanInput{
		compressionSiteInput("zone-a", []string{"10.0.0.0/24"}, "TCP", "80-82"),
		compressionSiteInput("zone-b", []string{"10.0.0.0/25"}, "TCP", "82"),
	}
	plan = runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Capacity.AddRows != 1 || plan.Selected.AddRows[0].Port != "80-82" {
		t.Fatalf("端口区间切分后应恢复等价线格式: %+v", plan)
	}
	in.Snapshot = compressionTestSnapshot(94)
	in.Sites[1] = compressionSiteInput("zone-b", []string{"10.0.1.0/24"}, "TCP", "82")
	plan = runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Capacity.AddRows != 2 {
		t.Fatalf("端口区间应按不同授权拆分: %+v", plan)
	}
	var parts []string
	for _, witness := range plan.Selected.Witnesses {
		if witness.Authorization.ZoneID == "zone-a" && witness.Authorization.PortItem == "80-82" {
			parts = append(parts, witness.Port)
		}
	}
	slices.Sort(parts)
	if !slices.Equal(parts, []string{"80-81", "82"}) {
		t.Fatalf("原始端口范围应由两条物理行共同取证: %v", parts)
	}
}

func TestBuild9S2AlternativeAvoidsDNSAndOppositeAction(t *testing.T) {
	in := LighthouseCompressionInput{
		Config: CompressionConfig{Enabled: true, ExtraIPv4Limit: 2}, Snapshot: compressionTestSnapshot(93),
		DNSEnabled: true, DNSComplete: true, DNSAddresses: []netip.Addr{netip.MustParseAddr("10.0.0.1")},
		Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-a",
			[]string{"10.0.0.0/32", "10.0.0.2/32", "10.0.0.8/32", "10.0.0.10/32"}, "TCP", "80")},
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Capacity.AddRows != 3 || plan.Risk.ExtraIPv4 != 2 {
		t.Fatalf("应找到同容量的 DNS 安全候选: %+v", plan)
	}
	for _, row := range plan.Selected.Rows {
		if netip.MustParsePrefix(row.CIDR).Contains(in.DNSAddresses[0]) {
			t.Fatalf("选择的候选仍包含 DNS 地址: %+v", row)
		}
	}

	in.DNSEnabled = false
	in.Snapshot = compressionTestSnapshot(1)
	in.Snapshot.Rules[0] = config.RuleInfo{
		Protocol: "TCP", Port: "80-81", CidrBlock: "10.0.0.0/32", Action: "DROP", Description: "manual",
	}
	plan = runCompressionPlan(t, in)
	if plan.Selected != nil || len(plan.Blockers) != 1 || plan.Blockers[0].Code != CompressionBlockedOppositeAction {
		t.Fatalf("无损计划同样应拒绝相反动作: %+v", plan)
	}
}

func TestBuild9S2BorrowChecksEachCandidate(t *testing.T) {
	site := compressionSiteInput("zone-a", []string{"10.0.0.0/32", "10.0.0.2/32"}, "TCP", "80")
	next := edgeVersion("v2", "10.0.0.8/32")
	site.Snapshot.Next = &next
	in := LighthouseCompressionInput{
		Config:   CompressionConfig{Enabled: true, ExtraIPv4Limit: 3},
		Snapshot: compressionTestSnapshot(95), Sites: []EdgeOneSitePlanInput{site}, PendingFingerprint: "fp",
	}
	in.Snapshot.Rules[0] = config.RuleInfo{
		Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.32/32", Action: "ACCEPT", Description: "[edgeone]",
	}
	proof := CompressionCleanupEvidence{SnapshotRevision: "1", PendingFingerprint: "fp", ReleasableRows: []int{0}}
	in.AssessCleanup = func(rep CompressionRepresentation) CompressionCleanupEvidence {
		if rep.CurrentAdds == 1 && rep.PendingExtra == 1 && len(rep.AddRows) == 2 &&
			slices.ContainsFunc(rep.AddRows, func(row config.RuleAction) bool { return row.CidrBlock == "10.0.0.0/30" }) {
			return proof
		}
		return CompressionCleanupEvidence{}
	}
	plan := runCompressionPlan(t, in)
	if plan.Selected == nil || plan.Capacity.Mode != CompressionModeBorrow || plan.Capacity.Peak != 97 ||
		plan.Capacity.ExpectedAfter != 96 || plan.Risk.ExtraIPv4 != 3 {
		t.Fatalf("应选择满足当前版预算和候选清理证据的组合: %+v", plan)
	}
	for _, tc := range []struct {
		name   string
		proof  CompressionCleanupEvidence
		wantFP string
	}{
		{"stale revision", CompressionCleanupEvidence{"2", "fp", []int{0}}, "fp"},
		{"different pending", CompressionCleanupEvidence{"1", "other", []int{0}}, "fp"},
		{"empty pending", CompressionCleanupEvidence{"1", "", []int{0}}, ""},
		{"foreign row", CompressionCleanupEvidence{"1", "fp", []int{1}}, "fp"},
		{"duplicate row", CompressionCleanupEvidence{"1", "fp", []int{0, 0}}, "fp"},
		{"missing rows", CompressionCleanupEvidence{"1", "fp", nil}, "fp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proof, in.PendingFingerprint = tc.proof, tc.wantFP
			if plan := runCompressionPlan(t, in); plan.Selected != nil {
				t.Fatalf("该证据不足以借用: %+v", plan)
			}
		})
	}
	in.Snapshot = compressionTestSnapshot(96)
	in.Sites = nil
	in.OtherNewRows = 1
	if plan := runCompressionPlan(t, in); plan.Capacity.Mode != CompressionModeBlocked || plan.Capacity.Peak != 97 {
		t.Fatalf("DDNS 新增不可借用或走 cleanup_only: %+v", plan)
	}
}

type smallCompressionCover struct {
	cidrs []string
	extra uint16
}

func smallCompressionCIDRs(required uint16) []string {
	var cidrs []string
	for i := 0; i < 16; i++ {
		if required&(1<<i) != 0 {
			cidrs = append(cidrs, fmt.Sprintf("10.0.0.%d/32", i))
		}
	}
	return cidrs
}

// bruteCompressionCovers 枚举 /28 内全部网段子集的互斥 CIDR 覆盖。
func bruteCompressionCovers(required, dns uint16) []smallCompressionCover {
	type block struct {
		mask uint16
		cidr string
	}
	var blocks []block
	for width := 1; width <= 16; width *= 2 {
		for start := 0; start < 16; start += width {
			mask := uint16(((1 << width) - 1) << start)
			if mask&required != 0 && mask&^required&dns == 0 {
				blocks = append(blocks, block{mask, fmt.Sprintf("10.0.0.%d/%d", start, 32-bits.TrailingZeros(uint(width)))})
			}
		}
	}
	var options []smallCompressionCover
	var visit func(uint16, []string)
	visit = func(covered uint16, cidrs []string) {
		missing := required &^ covered
		if missing == 0 {
			options = append(options, smallCompressionCover{append([]string(nil), cidrs...), covered &^ required})
			return
		}
		first := uint16(1) << bits.TrailingZeros16(missing)
		for _, block := range blocks {
			if block.mask&first != 0 && block.mask&covered == 0 {
				visit(covered|block.mask, append(cidrs, block.cidr))
			}
		}
	}
	visit(0, nil)
	return options
}

func smallMaxRun(mask uint16) int {
	n := 0
	for mask != 0 {
		mask &= mask << 1
		n++
	}
	return n
}

func betterSmallCompressionCover(a, b smallCompressionCover) bool {
	if bits.OnesCount16(a.extra) != bits.OnesCount16(b.extra) {
		return bits.OnesCount16(a.extra) < bits.OnesCount16(b.extra)
	}
	if smallMaxRun(a.extra) != smallMaxRun(b.extra) {
		return smallMaxRun(a.extra) < smallMaxRun(b.extra)
	}
	if len(a.cidrs) != len(b.cidrs) {
		return len(a.cidrs) < len(b.cidrs)
	}
	return slices.Compare(a.cidrs, b.cidrs) < 0
}

func TestBuild9S2PrefixSearchMatchesBruteForce(t *testing.T) {
	for required := uint16(1); required <= 255; required++ {
		dns := uint16(0x0a) &^ required
		rowBudget := 1 + int(required)%4
		in := LighthouseCompressionInput{
			Config: CompressionConfig{Enabled: true, ExtraIPv4Limit: 7}, Snapshot: compressionTestSnapshot(96 - rowBudget),
			DNSEnabled: true, DNSComplete: true, DNSAddresses: []netip.Addr{netip.MustParseAddr("10.0.0.8")},
			Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-a", smallCompressionCIDRs(required), "TCP", "80")},
		}
		for _, cidr := range smallCompressionCIDRs(dns) {
			in.DNSAddresses = append(in.DNSAddresses, netip.MustParsePrefix(cidr).Addr())
		}
		var best smallCompressionCover
		found := false
		for _, option := range bruteCompressionCovers(required, dns|1<<8) {
			slices.Sort(option.cidrs)
			if len(option.cidrs) <= rowBudget && bits.OnesCount16(option.extra) <= 7 &&
				(!found || betterSmallCompressionCover(option, best)) {
				best, found = option, true
			}
		}
		plan := runCompressionPlan(t, in)
		if plan.Incomplete || (plan.Selected != nil) != found {
			t.Fatalf("required=%08b rows=%d oracle=%+v found=%t plan=%+v", required, rowBudget, best, found, plan)
		}
		if found {
			var got []string
			for _, row := range plan.Selected.Rows {
				got = append(got, row.CIDR)
			}
			slices.Sort(got)
			if !slices.Equal(got, best.cidrs) || plan.Risk.ExtraIPv4 != uint64(bits.OnesCount16(best.extra)) ||
				plan.Risk.MaxExtraRun != uint64(smallMaxRun(best.extra)) {
				t.Fatalf("required=%08b oracle=%+v got=%v risk=%+v", required, best, got, plan.Risk)
			}
		}
	}
}

func TestBuild9S2BorrowCleanupCanReleaseCurrentOnlyRow(t *testing.T) {
	site := compressionSiteInput("zone-a", []string{"10.0.0.0/32"}, "TCP", "80")
	next := edgeVersion("v2", "10.0.0.8/32")
	site.Snapshot.Next = &next
	in := LighthouseCompressionInput{
		Config: CompressionConfig{Enabled: true}, PendingFingerprint: "fp",
		Snapshot: compressionTestSnapshot(96), Sites: []EdgeOneSitePlanInput{site},
	}
	in.Snapshot.Rules[0] = config.RuleInfo{
		Protocol: "TCP", Port: "80", CidrBlock: "10.0.0.0/32", Action: "ACCEPT", Description: "[edgeone]",
	}
	in.AssessCleanup = func(rep CompressionRepresentation) CompressionCleanupEvidence {
		if len(rep.ReusedRows) == 1 && rep.ReusedRows[0] == in.Snapshot.Rules[0] && rep.PendingExtra == 1 {
			return CompressionCleanupEvidence{"1", "fp", []int{0}}
		}
		return CompressionCleanupEvidence{}
	}
	if plan := runCompressionPlan(t, in); plan.Selected == nil || plan.Capacity.Mode != CompressionModeBorrow {
		t.Fatalf("可信换版模拟可释放仅用于旧当前版的复用行: %+v", plan)
	}

	in.Snapshot.Rules[1] = in.Snapshot.Rules[0]
	in.Snapshot.Rules[1].Description = "manual"
	if plan := runCompressionPlan(t, in); plan.Selected != nil {
		t.Fatalf("不能扣除相同功能键的定位歧义行: %+v", plan)
	}
	in.Snapshot.Rules[1] = compressionTestSnapshot(1).Rules[0]
	next.Prefixes = append(next.Prefixes, netip.MustParsePrefix("10.0.0.0/32"))
	if plan := runCompressionPlan(t, in); plan.Selected != nil {
		t.Fatalf("不能扣除仍覆盖待更新版本的复用行: %+v", plan)
	}
}

func TestBuild9S2Synthetic218Prefixes(t *testing.T) {
	var prefixes []string
	for i := 0; i < 109; i++ {
		for _, offset := range []int{i * 4, i*4 + 2} {
			prefixes = append(prefixes, fmt.Sprintf("10.0.%d.%d/32", offset/256, offset%256))
		}
	}
	in := LighthouseCompressionInput{
		Config: CompressionConfig{Enabled: true, ExtraIPv4Limit: 132}, Snapshot: compressionTestSnapshot(8),
		Sites: []EdgeOneSitePlanInput{compressionSiteInput("zone-synthetic", prefixes, "TCP", "80,443")},
	}
	plan := runCompressionPlan(t, in)
	if plan.Incomplete || plan.Selected == nil || len(plan.Lossless.AddRows) != 218 ||
		len(plan.Selected.AddRows) != 88 || plan.Risk.ExtraIPv4 != 132 || plan.Capacity.Peak != 96 {
		t.Fatalf("218 个合成前缀应在默认搜索预算内找到 88 行候选: selected=%t incomplete=%t capacity=%+v risk=%d",
			plan.Selected != nil, plan.Incomplete, plan.Capacity, plan.Risk.ExtraIPv4)
	}
	in.Config.ExtraIPv4Limit--
	if plan := runCompressionPlan(t, in); plan.Selected != nil || plan.Incomplete {
		t.Fatalf("额度 131 必须完整规划后拒绝: selected=%t incomplete=%t", plan.Selected != nil, plan.Incomplete)
	}
}
