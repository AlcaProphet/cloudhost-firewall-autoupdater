package provider

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"
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
	return PlanLighthouseCompression(context.Background(), in)
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
	toSet := func(values ...uint64) ipv4Set {
		if len(values) == 0 {
			return nil
		}
		return ipv4Set{{Start: values[0], End: values[len(values)-1]}}.normalize()
	}
	group := func(prefix string) []compressionCoverOption {
		return []compressionCoverOption{
			{Rows: 5, Extra: nil, Key: prefix + "-keep"},
			{Rows: 4, Extra: toSet(1, 2, 3), Key: prefix + "-moderate"},
			{Rows: 3, Extra: toSet(1, 2, 3, 4, 5), Key: prefix + "-max"},
		}
	}
	groups := [][]compressionCoverOption{group("a"), group("b")}
	selection, ok, incomplete := selectCompressionOptions(groups, 8, 3, nil, 1000)
	if incomplete || !ok {
		t.Fatalf("quota 3 应可行: ok=%v incomplete=%v", ok, incomplete)
	}
	if selection.rows != 8 || selection.risk.count() != 3 ||
		!strings.Contains(selection.key, "a-moderate") || !strings.Contains(selection.key, "b-moderate") {
		t.Fatalf("联合选择错误: %+v", selection)
	}
	if _, ok, incomplete := selectCompressionOptions(groups, 8, 2, nil, 1000); ok || incomplete {
		t.Fatalf("quota 2 应阻断: ok=%v incomplete=%v", ok, incomplete)
	}
	// 局部最大压缩 max+max 是 6 行、5 个额外地址，不能因行数少而优先。
	if _, ok, _ := selectCompressionOptions(groups, 6, 3, nil, 1000); ok {
		t.Fatal("max+max 额外 5 不应在 quota 3 下可行")
	}
	pruned := pruneCompressionOptions([]compressionCoverOption{
		{Rows: 4, Extra: toSet(1, 2, 3), Key: "a"},
		{Rows: 4, Extra: toSet(1, 2, 3, 4), Key: "b"},
		{Rows: 3, Extra: toSet(1, 2, 3, 4, 5), Key: "c"},
	})
	if len(pruned) != 2 {
		t.Fatalf("支配裁剪错误: %+v", pruned)
	}
	foundC := false
	for _, option := range pruned {
		if option.Key == "b" {
			t.Fatal("被支配的 b 未删除")
		}
		if option.Key == "c" {
			foundC = true
		}
	}
	if !foundC {
		t.Fatal("行数更少的 c 被错误删除")
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
	merged := mergeIPv4PrefixesExact(input)
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

	// 联合选择搜索上限必须返回 incomplete，而不是部分计划。
	toSet := func(start, end uint64) ipv4Set { return ipv4Set{{Start: start, End: end}} }
	groups := [][]compressionCoverOption{
		{{Rows: 1, Extra: toSet(1, 2), Key: "a1"}, {Rows: 2, Extra: nil, Key: "a2"}},
		{{Rows: 1, Extra: toSet(3, 4), Key: "b1"}, {Rows: 2, Extra: nil, Key: "b2"}},
	}
	if _, ok, incomplete := selectCompressionOptions(groups, 4, 10, nil, 1); ok || !incomplete {
		t.Fatalf("搜索上限应 incomplete: ok=%v incomplete=%v", ok, incomplete)
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
