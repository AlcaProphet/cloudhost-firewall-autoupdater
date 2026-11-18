package provider

import (
	"net/http"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 本文件是 Build9 Step 4 的纯归类回归。夹具只构造完整 RuleSnapshot，
// 不访问网络；CVM 入站用例复用既有本地 mock helper。

func s4Rule(description, protocol, port, cidr, action string) config.RuleInfo {
	return config.RuleInfo{
		Description: description,
		Protocol:    protocol,
		Port:        port,
		CidrBlock:   cidr,
		Action:      action,
	}
}

func s4RowStatuses(rows []FirewallCleanupRow) map[string]string {
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Rule.Description] = row.Status + "|" + row.ReasonCode
	}
	return out
}

func TestBuild9S4AssessLighthouseMixedTagsAndPhysicalRows(t *testing.T) {
	snapshot := RuleSnapshot{
		Revision: "7",
		Rules: []config.RuleInfo{
			s4Rule("[edgeone]", "TCP", "80", "1.1.1.1/32", "ACCEPT"),
			s4Rule("[edgeone] 主站", "TCP", "80,443", "2.2.2.0/24", "ACCEPT"),
			s4Rule("[edgeone]foo", "TCP", "80", "3.3.3.3/32", "ACCEPT"),
			s4Rule("[edgeone-old] 历史", "TCP", "80", "4.4.4.4/32", "ACCEPT"),
			s4Rule("[auto-ddns] DNS", "TCP", "80", "5.5.5.5/32", "ACCEPT"),
			s4Rule("手工规则", "TCP", "80", "6.6.6.6/32", "ACCEPT"),
		},
	}
	assessment := AssessFirewallCleanup(config.CloudTCLighthouse, snapshot, "edgeone")
	if assessment.RulesRead != 6 || assessment.MatchedCount() != 2 || assessment.DeletableCount() != 2 || assessment.RetainedCount() != 4 {
		t.Fatalf("计数错误: read=%d matched=%d deletable=%d retained=%d",
			assessment.RulesRead, assessment.MatchedCount(), assessment.DeletableCount(), assessment.RetainedCount())
	}
	got := s4RowStatuses(assessment.Rows)
	if got["[edgeone]"] != "deletable|" || got["[edgeone] 主站"] != "deletable|" {
		t.Fatalf("合法前缀行必须可删: %+v", got)
	}
	if _, ok := got["[edgeone]foo"]; ok {
		t.Fatalf("相似 TAG 不得进入匹配行: %+v", got)
	}
	if _, ok := got["[edgeone-old] 历史"]; ok {
		t.Fatalf("历史 TAG 不得进入匹配行: %+v", got)
	}
	if assessment.Rows[1].Keys == nil || len(assessment.Rows[1].Keys) != 2 {
		t.Fatalf("80,443 必须展开为两个 key: %+v", assessment.Rows[1].Keys)
	}
	if len(assessment.Rows) != 2 {
		t.Fatalf("80,443 必须按一条云端物理行计数，实际匹配 %d 条", len(assessment.Rows))
	}
}

func TestBuild9S4AssessLighthouseMultiPortAmbiguity(t *testing.T) {
	snapshot := RuleSnapshot{
		Revision: "11",
		Rules: []config.RuleInfo{
			s4Rule("[edgeone] 多端口冲突", "TCP", "80,443", "1.1.1.0/24", "ACCEPT"),
			s4Rule("[edgeone] 443冲突", "TCP", "443", "1.1.1.0/24", "ACCEPT"),
			s4Rule("[edgeone] 独立80", "TCP", "80", "2.2.2.2/32", "ACCEPT"),
			s4Rule("[edgeone] 独立多端口", "TCP", "80,443", "3.3.3.0/24", "ACCEPT"),
		},
	}
	assessment := AssessFirewallCleanup(config.CloudTCLighthouse, snapshot, "edgeone")
	if assessment.DeletableCount() != 2 || assessment.SkippedCount() != 2 {
		t.Fatalf("deletable=%d skipped=%d, want 2/2", assessment.DeletableCount(), assessment.SkippedCount())
	}
	got := s4RowStatuses(assessment.Rows)
	if got["[edgeone] 多端口冲突"] != "skipped|"+FirewallCleanupReasonKeyAmbiguous {
		t.Fatalf("多端口行任一 key 冲突必须整行跳过: %+v", got)
	}
	if got["[edgeone] 443冲突"] != "skipped|"+FirewallCleanupReasonKeyAmbiguous {
		t.Fatalf("443 行与多端口行冲突必须跳过: %+v", got)
	}
	if got["[edgeone] 独立80"] != "deletable|" || got["[edgeone] 独立多端口"] != "deletable|" {
		t.Fatalf("无歧义行必须可删: %+v", got)
	}
}

func TestBuild9S4AssessTencentRevisionGuard(t *testing.T) {
	t.Run("lighthouse-empty", func(t *testing.T) {
		snapshot := RuleSnapshot{Revision: "", Rules: []config.RuleInfo{
			s4Rule("[edgeone] 无版本", "TCP", "80", "1.1.1.1/32", "ACCEPT"),
		}}
		assessment := AssessFirewallCleanup(config.CloudTCLighthouse, snapshot, "edgeone")
		if assessment.DeletableCount() != 0 {
			t.Fatalf("缺版本不得产生可删行: %+v", assessment.Rows)
		}
		if assessment.Rows[0].ReasonCode != FirewallCleanupReasonSnapshotIncomplete {
			t.Fatalf("原因 = %q", assessment.Rows[0].ReasonCode)
		}
	})
	t.Run("lighthouse-invalid", func(t *testing.T) {
		snapshot := RuleSnapshot{Revision: "v7", Rules: []config.RuleInfo{
			s4Rule("[edgeone] 非法版本", "TCP", "80", "1.1.1.1/32", "ACCEPT"),
		}}
		assessment := AssessFirewallCleanup(config.CloudTCLighthouse, snapshot, "edgeone")
		if assessment.DeletableCount() != 0 || assessment.Rows[0].ReasonCode != FirewallCleanupReasonSnapshotIncomplete {
			t.Fatalf("非法版本必须整目标不可验证: %+v", assessment.Rows)
		}
	})
	t.Run("cvm-empty", func(t *testing.T) {
		snapshot := RuleSnapshot{Revision: "", Rules: []config.RuleInfo{
			{Protocol: "TCP", Port: "80", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[edgeone]", PolicyIndex: "1"},
		}}
		assessment := AssessFirewallCleanup(config.CloudTCCVM, snapshot, "edgeone")
		if assessment.DeletableCount() != 0 || assessment.Rows[0].ReasonCode != FirewallCleanupReasonSnapshotIncomplete {
			t.Fatalf("CVM 缺版本不得产生候选: %+v", assessment.Rows)
		}
	})
}

func TestBuild9S4AssessCVMLocatorUniqueness(t *testing.T) {
	snapshot := RuleSnapshot{
		Revision: "7",
		Rules: []config.RuleInfo{
			{Protocol: "TCP", Port: "80", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "[auto-ddns]", PolicyIndex: "9"},
			{Protocol: "TCP", Port: "80", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[edgeone] 唯一", PolicyIndex: "1"},
			{Protocol: "TCP", Port: "81", CidrBlock: "2.2.2.2/32", Action: "ACCEPT", Description: "[edgeone] 重复A", PolicyIndex: "2"},
			{Protocol: "TCP", Port: "82", CidrBlock: "3.3.3.3/32", Action: "ACCEPT", Description: "[edgeone] 重复B", PolicyIndex: "2"},
			{Protocol: "TCP", Port: "83", CidrBlock: "4.4.4.4/32", Action: "ACCEPT", Description: "[edgeone] 非匹配占位", PolicyIndex: "3"},
			{Protocol: "TCP", Port: "84", CidrBlock: "5.5.5.5/32", Action: "ACCEPT", Description: "[edgeone] 索引缺失", PolicyIndex: ""},
			{Protocol: "TCP", Port: "85", CidrBlock: "6.6.6.6/32", Action: "ACCEPT", Description: "[edgeone] 负数索引", PolicyIndex: "-1"},
			{Protocol: "TCP", Port: "86", CidrBlock: "7.7.7.7/32", Action: "ACCEPT", Description: "[edgeone] 非法索引", PolicyIndex: "abc"},
			{Protocol: "TCP", Port: "87", CidrBlock: "8.8.8.8/32", Action: "ACCEPT", Description: "[edgeone] 共享外部索引", PolicyIndex: "9"},
		},
	}
	assessment := AssessFirewallCleanup(config.CloudTCCVM, snapshot, "edgeone")
	got := s4RowStatuses(assessment.Rows)
	if got["[edgeone] 唯一"] != "deletable|" {
		t.Fatalf("唯一索引应可删: %+v", got)
	}
	for _, desc := range []string{"[edgeone] 重复A", "[edgeone] 重复B"} {
		if got[desc] != "skipped|"+FirewallCleanupReasonLocatorDuplicate {
			t.Fatalf("%s 必须因重复索引跳过: %+v", desc, got)
		}
	}
	if got["[edgeone] 共享外部索引"] != "skipped|"+FirewallCleanupReasonLocatorDuplicate {
		t.Fatalf("非当前 TAG 规则占用同索引也必须跳过: %+v", got)
	}
	for _, desc := range []string{"[edgeone] 索引缺失", "[edgeone] 负数索引", "[edgeone] 非法索引"} {
		if got[desc] != "skipped|"+FirewallCleanupReasonLocatorMissing {
			t.Fatalf("%s 必须因缺定位跳过: %+v", desc, got)
		}
	}
}

func TestBuild9S4AssessAliLocatorUniqueness(t *testing.T) {
	for _, cloudType := range []config.CloudType{config.CloudAliSWAS, config.CloudAliECS} {
		t.Run(string(cloudType), func(t *testing.T) {
			snapshot := RuleSnapshot{Rules: []config.RuleInfo{
				{Protocol: "TCP", Port: "80", CidrBlock: "9.9.9.9/32", Action: "ACCEPT", Description: "手工占位", RuleID: "dup"},
				{Protocol: "TCP", Port: "80", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[edgeone] 唯一", RuleID: "unique"},
				{Protocol: "TCP", Port: "81", CidrBlock: "2.2.2.2/32", Action: "ACCEPT", Description: "[edgeone] 重复", RuleID: "dup"},
				{Protocol: "TCP", Port: "82", CidrBlock: "3.3.3.3/32", Action: "ACCEPT", Description: "[edgeone] 空ID", RuleID: ""},
				{Protocol: "TCP", Port: "83", CidrBlock: "4.4.4.4/32", Action: "ACCEPT", Description: "[edgeone] 空格ID", RuleID: "   "},
				{Protocol: "TCP", Port: "84", CidrBlock: "5.5.5.5/32", Action: "ACCEPT", Description: "[edgeone] A", RuleID: "same"},
				{Protocol: "TCP", Port: "85", CidrBlock: "6.6.6.6/32", Action: "ACCEPT", Description: "[edgeone] B", RuleID: "same"},
			}}
			assessment := AssessFirewallCleanup(cloudType, snapshot, "edgeone")
			got := s4RowStatuses(assessment.Rows)
			if got["[edgeone] 唯一"] != "deletable|" {
				t.Fatalf("唯一 RuleID 应可删: %+v", got)
			}
			if got["[edgeone] 重复"] != "skipped|"+FirewallCleanupReasonLocatorDuplicate {
				t.Fatalf("与非匹配行共享 RuleID 必须跳过: %+v", got)
			}
			if got["[edgeone] 空ID"] != "skipped|"+FirewallCleanupReasonLocatorMissing {
				t.Fatalf("空 RuleID 必须跳过: %+v", got)
			}
			if got["[edgeone] 空格ID"] != "skipped|"+FirewallCleanupReasonLocatorMissing {
				t.Fatalf("空白 RuleID 必须跳过: %+v", got)
			}
			if got["[edgeone] A"] != "skipped|"+FirewallCleanupReasonLocatorDuplicate ||
				got["[edgeone] B"] != "skipped|"+FirewallCleanupReasonLocatorDuplicate {
				t.Fatalf("重复 RuleID 两行都必须跳过: %+v", got)
			}
		})
	}
}

func TestBuild9S4AssessDuplicateKeysWithUniqueLocators(t *testing.T) {
	// CVM/阿里云按稳定 ID 删除，同一功能 key 的两条物理行只要 ID 唯一即可分别删除；
	// Lighthouse 无稳定 ID，同一 key 出现两次时两行都必须跳过。
	ruleA := s4Rule("[edgeone] 同 key A", "TCP", "80", "1.1.1.1/32", "ACCEPT")
	ruleB := s4Rule("[edgeone] 同 key B", "TCP", "80", "1.1.1.1/32", "ACCEPT")

	cvm := RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{
		{Protocol: ruleA.Protocol, Port: ruleA.Port, CidrBlock: ruleA.CidrBlock, Action: ruleA.Action, Description: ruleA.Description, PolicyIndex: "1"},
		{Protocol: ruleB.Protocol, Port: ruleB.Port, CidrBlock: ruleB.CidrBlock, Action: ruleB.Action, Description: ruleB.Description, PolicyIndex: "2"},
	}}
	cvmAssessment := AssessFirewallCleanup(config.CloudTCCVM, cvm, "edgeone")
	if cvmAssessment.DeletableCount() != 2 {
		t.Fatalf("CVM 同 key 不同 PolicyIndex 应都可删: %+v", cvmAssessment.Rows)
	}

	swas := RuleSnapshot{Rules: []config.RuleInfo{
		{Protocol: ruleA.Protocol, Port: ruleA.Port, CidrBlock: ruleA.CidrBlock, Action: ruleA.Action, Description: ruleA.Description, RuleID: "r1"},
		{Protocol: ruleB.Protocol, Port: ruleB.Port, CidrBlock: ruleB.CidrBlock, Action: ruleB.Action, Description: ruleB.Description, RuleID: "r2"},
	}}
	swasAssessment := AssessFirewallCleanup(config.CloudAliSWAS, swas, "edgeone")
	if swasAssessment.DeletableCount() != 2 {
		t.Fatalf("SWAS 同 key 不同 RuleID 应都可删: %+v", swasAssessment.Rows)
	}

	lighthouse := RuleSnapshot{Revision: "1", Rules: []config.RuleInfo{ruleA, ruleB}}
	lhAssessment := AssessFirewallCleanup(config.CloudTCLighthouse, lighthouse, "edgeone")
	if lhAssessment.DeletableCount() != 0 || lhAssessment.SkippedCount() != 2 {
		t.Fatalf("Lighthouse 同 key 重复必须两行都跳过: %+v", lhAssessment.Rows)
	}
}

func TestBuild9S4AssessInvalidRuleFields(t *testing.T) {
	snapshot := RuleSnapshot{
		Revision: "7",
		Rules: []config.RuleInfo{
			{Protocol: "TCP", Port: "80", CidrBlock: "1.1.1.1/32", Ipv6CidrBlock: "2001:db8::1/128", Action: "ACCEPT", Description: "[edgeone] 双地址族"},
			{Protocol: "", Port: "80", CidrBlock: "2.2.2.2/32", Action: "ACCEPT", Description: "[edgeone] 缺协议"},
			{Protocol: "TCP", Port: "", CidrBlock: "3.3.3.3/32", Action: "ACCEPT", Description: "[edgeone] 缺端口"},
			{Protocol: "TCP", Port: "80", CidrBlock: "not-a-cidr", Action: "ACCEPT", Description: "[edgeone] 非法地址"},
		},
	}
	assessment := AssessFirewallCleanup(config.CloudTCLighthouse, snapshot, "edgeone")
	if assessment.MatchedCount() != 4 || assessment.DeletableCount() != 0 || assessment.SkippedCount() != 4 {
		t.Fatalf("非法字段必须全部跳过: %+v", assessment.Rows)
	}
	for _, row := range assessment.Rows {
		if row.ReasonCode != FirewallCleanupReasonRuleInvalid {
			t.Fatalf("%s 原因 = %q, want %q", row.Rule.Description, row.ReasonCode, FirewallCleanupReasonRuleInvalid)
		}
	}
}

// TestBuild9S4CVMGetSnapshotIngressOnly 证明预览消费的 CVM 快照只含受管入站方向。
func TestBuild9S4CVMGetSnapshotIngressOnly(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, _ recordedRequest) (int, string) {
		return http.StatusOK, `{"Response":{"SecurityGroupPolicySet":{"Version":"39","Ingress":[` +
			cvmPolicyJSON("3", true) + `],"Egress":[` + cvmPolicyJSON("4", true) + `]},"RequestId":"mock"}}`
	}
	snapshot, err := mockCVM(t, host).GetSnapshot()
	if err != nil {
		t.Fatalf("GetSnapshot 失败: %v", err)
	}
	if len(snapshot.Rules) != 1 {
		t.Fatalf("CVM 快照必须只含入站规则，实际 %d 条", len(snapshot.Rules))
	}
	if snapshot.Rules[0].PolicyIndex != "3" {
		t.Fatalf("入站 PolicyIndex = %q, want 3", snapshot.Rules[0].PolicyIndex)
	}
}

// TestBuild9S4ECSGetSnapshotIngressOnly 证明 ECS 预览读取固定 Direction=ingress。
func TestBuild9S4ECSGetSnapshotIngressOnly(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, req recordedRequest) (int, string) {
		if req.query("Direction") != "ingress" {
			t.Errorf("ECS Direction = %q, want ingress", req.query("Direction"))
		}
		return http.StatusOK, ecsRulePage("sgr-1", "1.1.1.1/32", "")
	}
	snapshot, err := mockECS(t, host).GetSnapshot()
	if err != nil {
		t.Fatalf("GetSnapshot 失败: %v", err)
	}
	if len(snapshot.Rules) != 1 || snapshot.Rules[0].RuleID != "sgr-1" {
		t.Fatalf("ECS 入站快照错误: %+v", snapshot.Rules)
	}
}
