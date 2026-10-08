package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// 本文件是 Issue7 Step 1「四个 Provider 的完整 snapshot 只读能力」判别性用例。
//
// 证据边界：全部通过本地 httptest mock 端点，只证明本地分页/版本/定位处理正确，
// 不构成任何真实云行为验收（真实云见 ProdTestList.md PT-I7）。

// ─── Lighthouse：FirewallVersion 必须进入快照 ───

func lighthouseRuleJSON(cidr string) string {
	return `{"Protocol":"TCP","Port":"443","CidrBlock":"` + cidr +
		`","Action":"ACCEPT","FirewallRuleDescription":"[auto-dns]"}`
}

func lighthousePage(rules []string, version int) string {
	return fmt.Sprintf(`{"Response":{"FirewallRuleSet":[%s],"FirewallVersion":%d,"TotalCount":%d,"RequestId":"mock"}}`,
		strings.Join(rules, ","), version, len(rules))
}

// TestSnapshot_LighthouseCarriesFirewallVersion 版本号必须随规则一起进入快照（不得丢弃）。
func TestSnapshot_LighthouseCarriesFirewallVersion(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, _ recordedRequest) (int, string) {
		return http.StatusOK, lighthousePage([]string{lighthouseRuleJSON("1.1.1.1/32")}, 7)
	}
	p := mockLighthouse(t, host)

	snap, err := p.GetSnapshot()
	if err != nil {
		t.Fatalf("GetSnapshot 失败: %v", err)
	}
	if snap.Revision != "7" {
		t.Fatalf("Revision = %q, want \"7\"（FirewallVersion 必须进入快照）", snap.Revision)
	}
	if len(snap.Rules) != 1 || snap.Rules[0].CidrBlock != "1.1.1.1/32" {
		t.Fatalf("Rules = %+v", snap.Rules)
	}
}

// TestSnapshot_LighthouseVersionChangeForcesReread 分页期间版本变化必须重读整个快照；
// 重读后一致则成功，持续变化则按快照不完整失败。
func TestSnapshot_LighthouseVersionChangeForcesReread(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	calls := 0
	firstPage := make([]string, 100) // 满页强制进入第二页
	for i := range firstPage {
		firstPage[i] = lighthouseRuleJSON(fmt.Sprintf("10.0.0.%d/32", i+1))
	}
	mock.reply = func(_ int, req recordedRequest) (int, string) {
		calls++
		body := bodyJSON(t, req)
		offset, _ := body["Offset"].(float64)
		if offset == 0 {
			return http.StatusOK, lighthousePage(firstPage, 7)
		}
		// 第 1 次读取的第二页版本已变化；第 2 次读取（重读）一致。
		if calls <= 2 {
			return http.StatusOK, lighthousePage([]string{lighthouseRuleJSON("2.2.2.2/32")}, 8)
		}
		return http.StatusOK, lighthousePage([]string{lighthouseRuleJSON("2.2.2.2/32")}, 7)
	}
	p := mockLighthouse(t, host)

	snap, err := p.GetSnapshot()
	if err != nil {
		t.Fatalf("重读后版本一致时必须成功: %v", err)
	}
	if snap.Revision != "7" || len(snap.Rules) != 101 {
		t.Fatalf("Revision=%q len(Rules)=%d, want \"7\" / 101", snap.Revision, len(snap.Rules))
	}

	// 持续变化：必须失败且不带回半截规则。
	mock2, host2 := newMockCloudAPI(t)
	mock2.reply = func(_ int, req recordedRequest) (int, string) {
		body := bodyJSON(t, req)
		offset, _ := body["Offset"].(float64)
		if offset == 0 {
			return http.StatusOK, lighthousePage(firstPage, 7)
		}
		return http.StatusOK, lighthousePage([]string{lighthouseRuleJSON("2.2.2.2/32")}, 8)
	}
	p2 := mockLighthouse(t, host2)
	snap2, err := p2.GetSnapshot()
	if err == nil {
		t.Fatalf("版本持续变化必须失败，实际返回 %d 条规则", len(snap2.Rules))
	}
	if !errors.Is(err, ErrSnapshotIncomplete) {
		t.Fatalf("错误必须可识别为 ErrSnapshotIncomplete，实际: %v", err)
	}
	if len(snap2.Rules) != 0 {
		t.Fatalf("快照失败时不得返回半截规则: %d 条", len(snap2.Rules))
	}
}

// TestSnapshot_LighthouseMissingVersionFails 缺少 FirewallVersion 必须失败，绝不退化为无版本写入。
func TestSnapshot_LighthouseMissingVersionFails(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, _ recordedRequest) (int, string) {
		return http.StatusOK, `{"Response":{"FirewallRuleSet":[` + lighthouseRuleJSON("1.1.1.1/32") + `],"TotalCount":1,"RequestId":"mock"}}`
	}
	p := mockLighthouse(t, host)
	if _, err := p.GetSnapshot(); !errors.Is(err, ErrSnapshotIncomplete) {
		t.Fatalf("缺版本必须返回 ErrSnapshotIncomplete，实际: %v", err)
	}
}

// ─── CVM：Version 与 PolicyIndex 必须进入快照 ───

func cvmPolicyJSON(index string, withIndex bool) string {
	idx := ""
	if withIndex {
		idx = `"PolicyIndex":` + index + `,`
	}
	return `{` + idx + `"Protocol":"TCP","Port":"443","CidrBlock":"1.1.1.1/32","Action":"ACCEPT","PolicyDescription":"[auto-dns]"}`
}

// TestSnapshot_CVMCarriesVersionAndPolicyIndex 同一快照的 Version 与真实 PolicyIndex 都必须保留。
func TestSnapshot_CVMCarriesVersionAndPolicyIndex(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, _ recordedRequest) (int, string) {
		return http.StatusOK, `{"Response":{"SecurityGroupPolicySet":{"Version":"39","Ingress":[` +
			cvmPolicyJSON("3", true) + `,` + cvmPolicyJSON("", false) + `]},"RequestId":"mock"}}`
	}
	p := mockCVM(t, host)

	snap, err := p.GetSnapshot()
	if err != nil {
		t.Fatalf("GetSnapshot 失败: %v", err)
	}
	if snap.Revision != "39" {
		t.Fatalf("Revision = %q, want \"39\"（Version 必须进入快照）", snap.Revision)
	}
	if len(snap.Rules) != 2 {
		t.Fatalf("Rules 数量 = %d, want 2（缺 PolicyIndex 的规则仍须保留以参与覆盖判断）", len(snap.Rules))
	}
	if snap.Rules[0].PolicyIndex != "3" {
		t.Fatalf("PolicyIndex = %q, want \"3\"", snap.Rules[0].PolicyIndex)
	}
	if snap.Rules[1].PolicyIndex != "" {
		t.Fatalf("缺 PolicyIndex 时必须留空而不是伪造: %q", snap.Rules[1].PolicyIndex)
	}
}

// TestSnapshot_CVMMissingVersionFails 缺少 Version 必须失败。
func TestSnapshot_CVMMissingVersionFails(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, _ recordedRequest) (int, string) {
		return http.StatusOK, `{"Response":{"SecurityGroupPolicySet":{"Ingress":[` + cvmPolicyJSON("3", true) + `]},"RequestId":"mock"}}`
	}
	p := mockCVM(t, host)
	if _, err := p.GetSnapshot(); !errors.Is(err, ErrSnapshotIncomplete) {
		t.Fatalf("缺 Version 必须返回 ErrSnapshotIncomplete，实际: %v", err)
	}
}

// ─── SWAS：必须完整遍历 PageNumber ───

func swasPage(rules []string, total, page int) string {
	return fmt.Sprintf(`{"TotalCount":%d,"PageSize":2,"PageNumber":%d,"FirewallRules":[%s],"RequestId":"mock"}`,
		total, page, strings.Join(rules, ","))
}

func swasRuleJSON(id, cidr string) string {
	return `{"RuleId":"` + id + `","RuleProtocol":"TCP","Port":"443/443","SourceCidrIp":"` + cidr +
		`","Policy":"accept","Remark":"[auto-dns]"}`
}

// TestSnapshot_SWASPaginatesAllPages 必须遍历全部页并带出稳定 RuleId。
func TestSnapshot_SWASPaginatesAllPages(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, req recordedRequest) (int, string) {
		switch req.query("PageNumber") {
		case "1":
			return http.StatusOK, swasPage([]string{swasRuleJSON("r1", "1.1.1.1/32"), swasRuleJSON("r2", "2.2.2.2/32")}, 3, 1)
		default:
			return http.StatusOK, swasPage([]string{swasRuleJSON("r3", "3.3.3.3/32")}, 3, 2)
		}
	}
	p := mockSWAS(t, host)

	snap, err := p.GetSnapshot()
	if err != nil {
		t.Fatalf("GetSnapshot 失败: %v", err)
	}
	if len(snap.Rules) != 3 {
		t.Fatalf("Rules 数量 = %d, want 3（必须遍历全部页）", len(snap.Rules))
	}
	if snap.Rules[2].RuleID != "r3" {
		t.Fatalf("RuleId = %q, want r3（删除定位必须来自同一快照）", snap.Rules[2].RuleID)
	}
	if snap.Revision != "" {
		t.Fatalf("阿里云 Revision 合法为空，实际 %q", snap.Revision)
	}
}

// TestSnapshot_SWASIncompletePaginationFails 云端声明总数大于实际读取条数时必须失败。
func TestSnapshot_SWASIncompletePaginationFails(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, req recordedRequest) (int, string) {
		if req.query("PageNumber") == "1" {
			return http.StatusOK, swasPage([]string{swasRuleJSON("r1", "1.1.1.1/32")}, 5, 1)
		}
		// 第 2 页提前返回空页：必须按快照不完整失败，而不是把 1 条当成完整快照。
		return http.StatusOK, swasPage(nil, 5, 2)
	}
	p := mockSWAS(t, host)
	snap, err := p.GetSnapshot()
	if err == nil {
		t.Fatalf("分页不完整必须失败，实际返回 %d 条", len(snap.Rules))
	}
	if !errors.Is(err, ErrSnapshotIncomplete) {
		t.Fatalf("错误必须可识别为 ErrSnapshotIncomplete，实际: %v", err)
	}
}

// TestSnapshot_SWASPageCapFailsIncomplete 分页硬上限用尽且无法用 TotalCount 证明完整时，
// 必须按快照不完整失败，绝不把「读满 100 页」当成完整快照（Issue7 §4.1/§6.3）。
func TestSnapshot_SWASPageCapFailsIncomplete(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	// 每页都返回满页（100 条）且不带 TotalCount → 只能靠页数上限终止
	full := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		full = append(full, swasRuleJSON(fmt.Sprintf("r%d", i), fmt.Sprintf("10.0.%d.%d/32", i/256, i%256)))
	}
	body := `{"PageSize":100,"FirewallRules":[` + strings.Join(full, ",") + `],"RequestId":"mock"}`
	mock.reply = func(_ int, _ recordedRequest) (int, string) {
		return http.StatusOK, body
	}
	p := mockSWAS(t, host)

	snap, err := p.GetSnapshot()
	if err == nil {
		t.Fatalf("页数上限用尽仍未证明完整时必须失败，实际返回 %d 条规则", len(snap.Rules))
	}
	if !errors.Is(err, ErrSnapshotIncomplete) {
		t.Fatalf("错误必须可识别为 ErrSnapshotIncomplete，实际: %v", err)
	}
	if len(snap.Rules) != 0 {
		t.Fatalf("快照失败时不得返回半截规则: %d 条", len(snap.Rules))
	}
	if got := len(requestsWithAction(t, mock.recorded(), "ListFirewallRules")); got != 100 {
		t.Errorf("ListFirewallRules 请求数 = %d, want 100（页数硬上限）", got)
	}
}

// ecsRulePage 构造一页 DescribeSecurityGroupAttribute 响应（阿里云 RPC 扁平 JSON）。
func ecsRulePage(ruleID, cidr, nextToken string) string {
	token := ""
	if nextToken != "" {
		token = `"NextToken":"` + nextToken + `",`
	}
	return `{` + token + `"Permissions":{"Permission":[{"SecurityGroupRuleId":"` + ruleID +
		`","IpProtocol":"TCP","PortRange":"443/443","SourceCidrIp":"` + cidr +
		`","Policy":"accept","Description":"[auto-dns]"}]},"RequestId":"mock"}`
}

// TestSnapshot_ECSPaginationFollowsToken 正向控制：token 正常推进时必须完整遍历全部页。
func TestSnapshot_ECSPaginationFollowsToken(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, req recordedRequest) (int, string) {
		if req.query("NextToken") == "" {
			return http.StatusOK, ecsRulePage("sgr-1", "1.1.1.1/32", "T2")
		}
		if req.query("NextToken") == "T2" {
			return http.StatusOK, ecsRulePage("sgr-2", "2.2.2.2/32", "")
		}
		return http.StatusOK, ecsRulePage("sgr-unexpected", "3.3.3.3/32", "")
	}
	p := mockECS(t, host)

	// GetRules 在 Step 1 之后是 GetSnapshot 的兼容包装，两者必须走同一分页实现。
	rules, err := p.GetRules()
	if err != nil {
		t.Fatalf("GetRules 失败: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("规则数量 = %d, want 2（必须遍历全部 NextToken 页）: %+v", len(rules), rules)
	}
	if rules[0].RuleID != "sgr-1" || rules[1].RuleID != "sgr-2" {
		t.Fatalf("RuleID = %q/%q, want sgr-1/sgr-2", rules[0].RuleID, rules[1].RuleID)
	}
	if got := len(requestsWithAction(t, mock.recorded(), "DescribeSecurityGroupAttribute")); got != 2 {
		t.Fatalf("Describe 请求数 = %d, want 2", got)
	}
}

// TestSnapshot_ECSNextTokenDoesNotAdvance P3-25：token 不推进（或已见 token 重现）时
// 必须硬失败并返回 snapshot_incomplete，绝不允许无限循环或返回半截快照。
//
// 修复前 GetRules 只判断 NextToken 是否为空，相同 token 会被无限重发；
// 本用例的 mock 在第 4 次请求开始返回服务端错误，用于让缺陷路径必然终止并暴露请求次数。
func TestSnapshot_ECSNextTokenDoesNotAdvance(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(index int, _ recordedRequest) (int, string) {
		if index >= 3 {
			return http.StatusInternalServerError,
				`{"Code":"InternalError","Message":"模拟云端异常终止无限分页","RequestId":"mock"}`
		}
		return http.StatusOK, ecsRulePage("sgr-1", "1.1.1.1/32", "SAME-TOKEN")
	}
	p := mockECS(t, host)

	rules, err := p.GetRules()
	calls := len(requestsWithAction(t, mock.recorded(), "DescribeSecurityGroupAttribute"))
	if calls > 2 {
		t.Fatalf("Describe 请求数 = %d, want <= 2（第二次看到相同 token 必须立即失败）", calls)
	}
	if err == nil {
		t.Fatalf("token 不推进必须返回错误，实际返回 %d 条规则", len(rules))
	}
	if !errors.Is(err, ErrSnapshotIncomplete) {
		t.Fatalf("错误必须可识别为 ErrSnapshotIncomplete，实际: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("快照失败时不得返回半截规则: %+v", rules)
	}
}

// TestSnapshot_ECSTokenProgress 保护历史环路、token 原值与合法空页的分页语义。
func TestSnapshot_ECSTokenProgress(t *testing.T) {
	cases := []struct {
		name      string
		tokens    []string
		emptyPage int
		failed    bool
	}{
		{"history_cycle", []string{"T1", "T2", "T1"}, -1, true},
		{"opaque_whitespace", []string{" T+/= ", "T+/=", ""}, -1, false},
		{"opaque_case", []string{"Token", "token", ""}, -1, false},
		{"empty_middle", []string{"T1", "T2", ""}, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock, host := newMockCloudAPI(t)
			mock.reply = func(i int, req recordedRequest) (int, string) {
				// 没有保护的候选也会结束；判别力来自错误类型与请求次数，而非超时。
				if i >= len(tc.tokens) {
					return http.StatusBadRequest, `{"Code":"InvalidParameter","Message":"fixture stop"}`
				}
				want := ""
				if i > 0 {
					want = tc.tokens[i-1]
				}
				if got := req.query("NextToken"); got != want {
					t.Errorf("请求 token=%q，预期原值 %q", got, want)
				}
				if i == tc.emptyPage {
					return http.StatusOK, fmt.Sprintf(`{"Permissions":{"Permission":[]},"NextToken":%q}`, tc.tokens[i])
				}
				return http.StatusOK, ecsRulePage(fmt.Sprintf("sgr-%d", i), "1.1.1.1/32", tc.tokens[i])
			}
			snap, err := mockECS(t, host).GetSnapshot()
			if tc.failed {
				if !errors.Is(err, ErrSnapshotIncomplete) || len(snap.Rules) != 0 || snap.Revision != "" {
					t.Fatalf("必须不带回半截快照: snapshot=%+v err=%v", snap, err)
				}
			} else {
				wantRules := len(tc.tokens)
				if tc.emptyPage >= 0 {
					wantRules--
				}
				if err != nil || len(snap.Rules) != wantRules {
					t.Fatalf("snapshot=%+v err=%v，预期 %d 条规则", snap, err, wantRules)
				}
				// 空中间页仍须取得末页，不能只以数量掩盖重复读取。
				if snap.Rules[len(snap.Rules)-1].RuleID != "sgr-2" {
					t.Fatalf("未取得末页规则: %+v", snap.Rules)
				}
			}
			if got := len(mock.recorded()); got != len(tc.tokens) {
				t.Fatalf("请求次数=%d，预期 %d", got, len(tc.tokens))
			}
		})
	}
}
