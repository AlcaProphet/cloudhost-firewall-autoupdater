package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

// ─── Step 7：Provider 请求构造（纯 mock 层，不访问任何真实云 API） ───
//
// 这些用例把四个真实 Provider 的 SDK client 指向本地 httptest 端点，捕获并断言
// 实际发出的请求（协议/端口/地址族字段/删除标识/上限检查）。
// 证据边界：mock 端点只证明本地请求构造正确，**不构成真实腾讯云或阿里云验收**。

// recordedRequest mock 端点捕获的单次请求
type recordedRequest struct {
	Method string
	URL    string
	Body   string
	Header http.Header
}

// action SDK 请求携带的操作名（腾讯云在 X-Tc-Action，阿里云在 X-Acs-Action）
func (r recordedRequest) action() string {
	if a := r.Header.Get("X-Tc-Action"); a != "" {
		return a
	}
	return r.Header.Get("X-Acs-Action")
}

// query 返回 URL 查询参数（阿里云 RPC 风格请求的参数在此）
func (r recordedRequest) query(key string) string {
	u, err := url.Parse(r.URL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

// mockCloudAPI 本地 mock 云 API 端点
type mockCloudAPI struct {
	mu       sync.Mutex
	requests []recordedRequest
	// reply 按请求序号返回 (status, body)；为 nil 时使用 defaultReply
	reply        func(index int, req recordedRequest) (int, string)
	defaultReply string
}

func newMockCloudAPI(t *testing.T) (*mockCloudAPI, string) {
	t.Helper()
	m := &mockCloudAPI{defaultReply: `{"Response":{"RequestId":"mock"},"RequestId":"mock"}`}
	srv := httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(srv.Close)
	return m, strings.TrimPrefix(srv.URL, "http://")
}

func (m *mockCloudAPI) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.requests = append(m.requests, recordedRequest{
		Method: r.Method, URL: r.URL.String(), Body: string(body), Header: r.Header.Clone(),
	})
	idx := len(m.requests) - 1
	req := m.requests[idx]
	reply := m.reply
	m.mu.Unlock()

	status, payload := http.StatusOK, m.defaultReply
	if reply != nil {
		status, payload = reply(idx, req)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, payload)
}

func (m *mockCloudAPI) recorded() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedRequest(nil), m.requests...)
}

// requestsWithAction 筛选指定 SDK 操作名的请求
func requestsWithAction(t *testing.T, reqs []recordedRequest, action string) []recordedRequest {
	t.Helper()
	var out []recordedRequest
	for _, r := range reqs {
		if r.action() == action {
			out = append(out, r)
		}
	}
	return out
}

// bodyJSON 解析腾讯云 JSON 请求体
func bodyJSON(t *testing.T, req recordedRequest) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(req.Body), &out); err != nil {
		t.Fatalf("解析请求体失败: %v; body=%s", err, req.Body)
	}
	return out
}

// objList 取 JSON 对象中的对象数组字段
func objList(t *testing.T, parent map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("字段 %s 不是数组: %+v", key, parent)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("数组 %s 的元素不是对象: %+v", key, item)
		}
		out = append(out, obj)
	}
	return out
}

// objField 取 JSON 对象中的对象字段
func objField(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	obj, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("字段 %s 不是对象: %+v", key, parent)
	}
	return obj
}

// strField 取字符串字段；缺失返回 ok=false
func strField(parent map[string]any, key string) (string, bool) {
	value, ok := parent[key]
	if !ok || value == nil {
		return "", false
	}
	s, ok := value.(string)
	return s, ok
}

// decodeJSONArrayText 解析阿里云 RPC 参数里的 JSON 数组文本
func decodeJSONArrayText(t *testing.T, text string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("解析 RPC JSON 数组参数失败: %v; text=%s", err, text)
	}
	return out
}

// rpcIndexedObjects 解析阿里云 RPC 的扁平数组参数（形如 Permissions.1.IpProtocol），
// 返回 下标 → 字段 → 值（下标从 1 开始，与 SDK 序列化一致）。
func rpcIndexedObjects(t *testing.T, req recordedRequest, prefix string) map[int]map[string]string {
	t.Helper()
	u, err := url.Parse(req.URL)
	if err != nil {
		t.Fatalf("解析请求 URL 失败: %v", err)
	}
	out := make(map[int]map[string]string)
	for key, vals := range u.Query() {
		if !strings.HasPrefix(key, prefix+".") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(key, prefix+"."), ".", 2)
		if len(parts) != 2 {
			continue
		}
		idx, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		if out[idx] == nil {
			out[idx] = make(map[string]string)
		}
		if len(vals) > 0 {
			out[idx][parts[1]] = vals[0]
		}
	}
	return out
}

// rpcCommaList 解析阿里云 RPC 的逗号分隔列表参数（形如 RuleIds=a,b）
func rpcCommaList(req recordedRequest, key string) []string {
	raw := req.query(key)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// rpcIndexedList 解析阿里云 RPC 的下标标量列表参数（形如 SecurityGroupRuleId.1=a），按下标升序返回。
func rpcIndexedList(t *testing.T, req recordedRequest, prefix string) []string {
	t.Helper()
	u, err := url.Parse(req.URL)
	if err != nil {
		t.Fatalf("解析请求 URL 失败: %v", err)
	}
	indexed := make(map[int]string)
	for key, vals := range u.Query() {
		if !strings.HasPrefix(key, prefix+".") {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimPrefix(key, prefix+"."))
		if err != nil {
			continue
		}
		if len(vals) > 0 {
			indexed[idx] = vals[0]
		}
	}
	keys := make([]int, 0, len(indexed))
	for k := range indexed {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]string, 0, len(indexed))
	for _, k := range keys {
		out = append(out, indexed[k])
	}
	return out
}

// mockLighthouse 构造指向 mock 端点的 Lighthouse Provider
func mockLighthouse(t *testing.T, host string) *TCLighthouse {
	t.Helper()
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = host
	cpf.HttpProfile.Scheme = "http"
	cli, err := lighthouse.NewClient(common.NewCredential("mock-id", "mock-key"), "ap-guangzhou", cpf)
	if err != nil {
		t.Fatalf("构造 Lighthouse client 失败: %v", err)
	}
	return &TCLighthouse{client: cli, instanceID: "lhins-mock", targetIndex: 1}
}

// mockCVM 构造指向 mock 端点的 CVM Provider
func mockCVM(t *testing.T, host string) *TCCVM {
	t.Helper()
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = host
	cpf.HttpProfile.Scheme = "http"
	cli, err := vpc.NewClient(common.NewCredential("mock-id", "mock-key"), "ap-guangzhou", cpf)
	if err != nil {
		t.Fatalf("构造 CVM client 失败: %v", err)
	}
	return &TCCVM{client: cli, securityGroupID: "sg-mock", targetIndex: 1}
}

// mockSWAS 构造指向 mock 端点的 SWAS Provider
func mockSWAS(t *testing.T, host string) *AliSWAS {
	t.Helper()
	cli, err := swas.NewClient(&openapi.Config{
		AccessKeyId:     tea.String("mock-id"),
		AccessKeySecret: tea.String("mock-key"),
		Endpoint:        tea.String(host),
		Protocol:        tea.String("http"),
	})
	if err != nil {
		t.Fatalf("构造 SWAS client 失败: %v", err)
	}
	return &AliSWAS{client: cli, instanceID: "swas-mock", regionID: "cn-hangzhou", targetIndex: 1}
}

// mockECS 构造指向 mock 端点的 ECS Provider
func mockECS(t *testing.T, host string) *AliECS {
	t.Helper()
	cli, err := ecs.NewClient(&openapi.Config{
		AccessKeyId:     tea.String("mock-id"),
		AccessKeySecret: tea.String("mock-key"),
		Endpoint:        tea.String(host),
		Protocol:        tea.String("http"),
	})
	if err != nil {
		t.Fatalf("构造 ECS client 失败: %v", err)
	}
	return &AliECS{client: cli, securityGroupID: "sg-mock", regionID: "cn-hangzhou", targetIndex: 1}
}

// mockSnapshot 返回测试用云端快照（腾讯云写入需要非空版本号）。
func mockSnapshot() RuleSnapshot { return RuleSnapshot{Revision: "7"} }

// ─── Lighthouse ───

// TestRequest_LighthouseCreateProtocolPortAndIPv6 ICMP 端口固定 ALL、IPv6+ICMP 切 ICMPv6 并写 Ipv6CidrBlock。
func TestRequest_LighthouseCreateProtocolPortAndIPv6(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	p := mockLighthouse(t, host)

	cases := []struct {
		name  string
		rule  config.RuleAction
		check func(t *testing.T, fw map[string]any)
	}{
		{
			name: "ICMP IPv4 端口固定 ALL",
			rule: config.RuleAction{Protocol: "ICMP", Port: "ALL", Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] icmp"},
			check: func(t *testing.T, fw map[string]any) {
				if got, _ := strField(fw, "Protocol"); got != "ICMP" {
					t.Errorf("Protocol = %q, want ICMP", got)
				}
				if got, _ := strField(fw, "Port"); got != "ALL" {
					t.Errorf("ICMP 端口必须固定为 ALL，实际 %q", got)
				}
				if got, _ := strField(fw, "CidrBlock"); got != "1.2.3.4/32" {
					t.Errorf("CidrBlock = %q, want 1.2.3.4/32", got)
				}
				if _, ok := strField(fw, "Ipv6CidrBlock"); ok {
					t.Errorf("IPv4 规则不得写 Ipv6CidrBlock: %+v", fw)
				}
			},
		},
		{
			name: "ICMP IPv6 切 ICMPv6 并写 Ipv6CidrBlock",
			rule: config.RuleAction{Protocol: "ICMP", Port: "ALL", Action: "ACCEPT", Ipv6CidrBlock: "2001:db8::1/128", Description: "[t] icmp6"},
			check: func(t *testing.T, fw map[string]any) {
				if got, _ := strField(fw, "Protocol"); got != "ICMPv6" {
					t.Errorf("IPv6 ICMP 协议 = %q, want ICMPv6", got)
				}
				if got, _ := strField(fw, "Ipv6CidrBlock"); got != "2001:db8::1/128" {
					t.Errorf("Ipv6CidrBlock = %q", got)
				}
				if _, ok := strField(fw, "CidrBlock"); ok {
					t.Errorf("IPv6 规则不得写 CidrBlock: %+v", fw)
				}
			},
		},
		{
			name: "TCP 端口透传",
			rule: config.RuleAction{Protocol: "TCP", Port: "8000-8010", Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] tcp"},
			check: func(t *testing.T, fw map[string]any) {
				if got, _ := strField(fw, "Protocol"); got != "TCP" {
					t.Errorf("Protocol = %q, want TCP", got)
				}
				if got, _ := strField(fw, "Port"); got != "8000-8010" {
					t.Errorf("TCP 端口 = %q, want 8000-8010", got)
				}
			},
		},
	}

	for _, tc := range cases {
		if _, err := p.CreateRules(mockSnapshot(), []config.RuleAction{tc.rule}); err != nil {
			t.Fatalf("%s: CreateRules 失败: %v", tc.name, err)
		}
	}

	creates := requestsWithAction(t, mock.recorded(), "CreateFirewallRules")
	if len(creates) != len(cases) {
		t.Fatalf("CreateFirewallRules 请求数 = %d, want %d", len(creates), len(cases))
	}
	for i, tc := range cases {
		body := bodyJSON(t, creates[i])
		if got, _ := strField(body, "InstanceId"); got != "lhins-mock" {
			t.Errorf("%s: InstanceId = %q", tc.name, got)
		}
		// Issue7 §6.1：Create 必须携带 S0 的 FirewallVersion（mockSnapshot = 7）
		if got, ok := body["FirewallVersion"]; !ok || got != float64(7) {
			t.Errorf("%s: FirewallVersion = %v, want 7（版本保护不得丢失）", tc.name, got)
		}
		rules := objList(t, body, "FirewallRules")
		if len(rules) != 1 {
			t.Fatalf("%s: FirewallRules 数量 = %d, want 1", tc.name, len(rules))
		}
		tc.check(t, rules[0])
	}
}

// TestRequest_LighthouseDeleteCarriesS1Version Issue7 §6.1：Delete 必须携带 S1 的
// FirewallVersion；版本缺失即快照不完整，绝不降级为无版本删除。
func TestRequest_LighthouseDeleteCarriesS1Version(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	p := mockLighthouse(t, host)

	target := config.RuleInfo{Protocol: "TCP", Port: "443", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: "[auto-dns]"}
	res, err := p.DeleteRules(RuleSnapshot{Revision: "12"}, []config.RuleInfo{target})
	if err != nil {
		t.Fatalf("DeleteRules 失败: %v", err)
	}
	if res.Deleted != 1 || res.Resolved != 1 {
		t.Errorf("DeleteResult = %+v, want {Deleted:1 Resolved:1}", res)
	}

	deletes := requestsWithAction(t, mock.recorded(), "DeleteFirewallRules")
	if len(deletes) != 1 {
		t.Fatalf("DeleteFirewallRules 请求数 = %d, want 1", len(deletes))
	}
	if got, ok := bodyJSON(t, deletes[0])["FirewallVersion"]; !ok || got != float64(12) {
		t.Errorf("DeleteFirewallRules 必须携带 S1 FirewallVersion=12, got %v（版本保护不得丢失）", got)
	}

	// 版本缺失：必须按快照不完整失败，且不得发出删除请求
	before := len(requestsWithAction(t, mock.recorded(), "DeleteFirewallRules"))
	if _, err := p.DeleteRules(RuleSnapshot{}, []config.RuleInfo{target}); !errors.Is(err, ErrSnapshotIncomplete) {
		t.Fatalf("缺少 FirewallVersion 必须返回 ErrSnapshotIncomplete，实际: %v", err)
	}
	if after := len(requestsWithAction(t, mock.recorded(), "DeleteFirewallRules")); after != before {
		t.Errorf("版本缺失时不得发出删除请求: %d → %d", before, after)
	}
}

// TestRequest_LighthouseExactDeleteByRuleSpec Lighthouse 按完整规则定义精确删除（API 无规则 ID）。
func TestRequest_LighthouseExactDeleteByRuleSpec(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	p := mockLighthouse(t, host)

	_, err := p.DeleteRules(mockSnapshot(), []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "1.2.3.4/32", Action: "ACCEPT", Description: "[t] c"},
		{Protocol: "ICMP", CidrBlock: "5.6.7.8/32", Action: "ACCEPT", Description: "[t] icmp"},
	})
	if err != nil {
		t.Fatalf("DeleteRules 失败: %v", err)
	}

	deletes := requestsWithAction(t, mock.recorded(), "DeleteFirewallRules")
	if len(deletes) != 1 {
		t.Fatalf("DeleteFirewallRules 请求数 = %d, want 1", len(deletes))
	}
	rules := objList(t, bodyJSON(t, deletes[0]), "FirewallRules")
	if len(rules) != 2 {
		t.Fatalf("删除规则数 = %d, want 2", len(rules))
	}
	if got, _ := strField(rules[0], "Protocol"); got != "TCP" {
		t.Errorf("第 1 条 Protocol = %q, want TCP", got)
	}
	if got, _ := strField(rules[0], "Port"); got != "443" {
		t.Errorf("第 1 条 Port = %q, want 443", got)
	}
	if got, _ := strField(rules[0], "CidrBlock"); got != "1.2.3.4/32" {
		t.Errorf("第 1 条 CidrBlock = %q", got)
	}
	// 空端口必须补 ALL，保证删除条件与云端规则一致（精确删除）
	if got, _ := strField(rules[1], "Port"); got != "ALL" {
		t.Errorf("第 2 条空端口必须补 ALL，实际 %q", got)
	}
}

// ─── CVM ───

// emptyPolicyReply CVM DescribeSecurityGroupPolicies 的成功响应（0 条规则）
const emptyPolicyReply = `{"Response":{"SecurityGroupPolicySet":{"Ingress":[],"Egress":[]},"RequestId":"mock"}}`

// TestRequest_CVMICMPOmitsPortAndIPv6Field CVM 仅 TCP/UDP 传 Port；IPv6+ICMP 切 ICMPV6。
func TestRequest_CVMICMPOmitsPortAndIPv6Field(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.defaultReply = emptyPolicyReply
	p := mockCVM(t, host)

	if _, err := p.CreateRules(mockSnapshot(), []config.RuleAction{
		{Protocol: "ICMP", Port: "ALL", Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] icmp"},
	}); err != nil {
		t.Fatalf("CreateRules(ICMP) 失败: %v", err)
	}
	if _, err := p.CreateRules(mockSnapshot(), []config.RuleAction{
		{Protocol: "ICMP", Port: "ALL", Action: "ACCEPT", Ipv6CidrBlock: "2001:db8::1/128", Description: "[t] icmp6"},
	}); err != nil {
		t.Fatalf("CreateRules(ICMPv6) 失败: %v", err)
	}
	if _, err := p.CreateRules(mockSnapshot(), []config.RuleAction{
		{Protocol: "TCP", Port: "443", Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] tcp"},
	}); err != nil {
		t.Fatalf("CreateRules(TCP) 失败: %v", err)
	}

	creates := requestsWithAction(t, mock.recorded(), "CreateSecurityGroupPolicies")
	if len(creates) != 3 {
		t.Fatalf("CreateSecurityGroupPolicies 请求数 = %d, want 3", len(creates))
	}

	// Issue7 §6.2：Create 必须携带 S0 的 Version（mockSnapshot = "7"）
	if v, _ := strField(objField(t, bodyJSON(t, creates[0]), "SecurityGroupPolicySet"), "Version"); v != "7" {
		t.Errorf("SecurityGroupPolicySet.Version = %q, want \"7\"（版本保护不得丢失）", v)
	}

	icmp := objList(t, objField(t, bodyJSON(t, creates[0]), "SecurityGroupPolicySet"), "Ingress")[0]
	if got, _ := strField(icmp, "Protocol"); got != "ICMP" {
		t.Errorf("Protocol = %q, want ICMP", got)
	}
	if _, ok := strField(icmp, "Port"); ok {
		t.Errorf("CVM ICMP 必须省略 Port: %+v", icmp)
	}
	if got, _ := strField(icmp, "CidrBlock"); got != "1.2.3.4/32" {
		t.Errorf("CidrBlock = %q", got)
	}

	icmp6 := objList(t, objField(t, bodyJSON(t, creates[1]), "SecurityGroupPolicySet"), "Ingress")[0]
	if got, _ := strField(icmp6, "Protocol"); got != "ICMPV6" {
		t.Errorf("IPv6 ICMP 协议 = %q, want ICMPV6", got)
	}
	if got, _ := strField(icmp6, "Ipv6CidrBlock"); got != "2001:db8::1/128" {
		t.Errorf("Ipv6CidrBlock = %q", got)
	}
	if _, ok := strField(icmp6, "CidrBlock"); ok {
		t.Errorf("IPv6 规则不得写 CidrBlock: %+v", icmp6)
	}

	tcp := objList(t, objField(t, bodyJSON(t, creates[2]), "SecurityGroupPolicySet"), "Ingress")[0]
	if got, _ := strField(tcp, "Port"); got != "443" {
		t.Errorf("TCP 端口 = %q, want 443", got)
	}
}

// TestRequest_CVMDeleteBatchedIndicesIssue7 §6.2：候选必须放在**同一个**
// DeleteSecurityGroupPolicies 请求的 Ingress 数组中，并携带同一 S1 的 Version，
// 避免逐条删除造成索引漂移。
func TestRequest_CVMDeleteBatchedIndices(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.defaultReply = emptyPolicyReply
	p := mockCVM(t, host)

	res, err := p.DeleteRules(RuleSnapshot{Revision: "39"}, []config.RuleInfo{
		{PolicyIndex: "3", Description: "[t] a"},
		{PolicyIndex: "10", Description: "[t] b"},
		{PolicyIndex: "4", Description: "[t] c"},
	})
	if err != nil {
		t.Fatalf("DeleteRules 失败: %v", err)
	}
	if res.Deleted != 3 || res.Resolved != 3 {
		t.Errorf("DeleteResult = %+v, want {Deleted:3 Resolved:3}", res)
	}

	deletes := requestsWithAction(t, mock.recorded(), "DeleteSecurityGroupPolicies")
	if len(deletes) != 1 {
		t.Fatalf("DeleteSecurityGroupPolicies 请求数 = %d, want 1（必须单请求批量删除）", len(deletes))
	}
	ps := objField(t, bodyJSON(t, deletes[0]), "SecurityGroupPolicySet")
	if v, _ := strField(ps, "Version"); v != "39" {
		t.Errorf("SecurityGroupPolicySet.Version = %q, want \"39\"（删除必须使用同一 S1 版本）", v)
	}
	ingress := objList(t, ps, "Ingress")
	if len(ingress) != 3 {
		t.Fatalf("Ingress 数量 = %d, want 3（同一请求内批量提交）", len(ingress))
	}
	got := map[float64]bool{}
	for _, item := range ingress {
		idx, ok := item["PolicyIndex"].(float64)
		if !ok {
			t.Fatalf("Ingress 项缺少 PolicyIndex: %+v", item)
		}
		got[idx] = true
	}
	for _, want := range []float64{3, 4, 10} {
		if !got[want] {
			t.Errorf("缺少 PolicyIndex=%v: %+v", want, ingress)
		}
	}
}

// TestRequest_CVMDeleteRejectsUnsafeCandidates 缺少/不可解析/重复的删除定位必须拒绝，
// 绝不允许无版本或按值降级删除（Issue7 §6.2）。
func TestRequest_CVMDeleteRejectsUnsafeCandidates(t *testing.T) {
	target := config.RuleInfo{PolicyIndex: "3", Description: "[t] a"}

	cases := []struct {
		name       string
		snapshot   RuleSnapshot
		candidates []config.RuleInfo
	}{
		{"缺少 S1 版本", RuleSnapshot{}, []config.RuleInfo{target}},
		{"缺少 PolicyIndex", RuleSnapshot{Revision: "39"}, []config.RuleInfo{{Description: "[t] a"}}},
		{"PolicyIndex 不可解析", RuleSnapshot{Revision: "39"}, []config.RuleInfo{{PolicyIndex: "x", Description: "[t] a"}}},
		{"重复 PolicyIndex", RuleSnapshot{Revision: "39"}, []config.RuleInfo{
			{PolicyIndex: "3", Description: "[t] a"}, {PolicyIndex: "3", Description: "[t] b"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock, host := newMockCloudAPI(t)
			mock.defaultReply = emptyPolicyReply
			p := mockCVM(t, host)
			if _, err := p.DeleteRules(tc.snapshot, tc.candidates); err == nil {
				t.Fatal("不安全的删除定位必须返回错误")
			}
			if got := len(requestsWithAction(t, mock.recorded(), "DeleteSecurityGroupPolicies")); got != 0 {
				t.Errorf("定位不安全时不得发出删除请求，实际 %d", got)
			}
		})
	}
}

// TestRequest_CVMDeleteSwallowsResourceNotFound 已不存在（ResourceNotFound）视为成功（幂等），不报错。
func TestRequest_CVMDeleteSwallowsResourceNotFound(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.defaultReply = `{"Response":{"Error":{"Code":"ResourceNotFound","Message":"规则不存在"},"RequestId":"mock"}}`
	p := mockCVM(t, host)

	if _, err := p.DeleteRules(mockSnapshot(), []config.RuleInfo{{PolicyIndex: "7", Description: "[t] a"}}); err != nil {
		t.Fatalf("ResourceNotFound 必须视为成功（幂等），实际返回: %v", err)
	}
	if got := len(requestsWithAction(t, mock.recorded(), "DeleteSecurityGroupPolicies")); got != 1 {
		t.Errorf("删除请求数 = %d, want 1", got)
	}
}

// TestRequest_CVMDeleteIsSingleRequest CVM 删除不再有逐条部分进度：
// 全部候选必须在一个请求内提交，失败即整体未确认（部分进度语义由 ECS 分批承担）。
func TestRequest_CVMDeleteIsSingleRequest(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(_ int, _ recordedRequest) (int, string) {
		return http.StatusInternalServerError, `{"Response":{"Error":{"Code":"InternalError","Message":"模拟失败"},"RequestId":"mock"}}`
	}
	p := mockCVM(t, host)

	res, err := p.DeleteRules(RuleSnapshot{Revision: "39"}, []config.RuleInfo{
		{PolicyIndex: "1", Description: "[t] a"},
		{PolicyIndex: "2", Description: "[t] b"},
	})
	if err == nil {
		t.Fatal("删除失败必须返回错误")
	}
	if res.Deleted != 0 {
		t.Errorf("未确认任何删除时 Deleted = %d, want 0", res.Deleted)
	}
	if got := len(requestsWithAction(t, mock.recorded(), "DeleteSecurityGroupPolicies")); got != 1 {
		t.Errorf("请求数 = %d, want 1（单请求批量）", got)
	}
}

// TestRequest_CVMRuleLimitStopsAt100 安全组规则总数（含新增）超过 100 条时必须停止新增。
func TestRequest_CVMRuleLimitStopsAt100(t *testing.T) {
	statisticsReply := func(total int) string {
		return `{"Response":{"SecurityGroupPolicySet":{"PolicyStatistics":{` +
			`"IngressIPv4TotalCount":` + strconv.Itoa(total) + `,"IngressIPv6TotalCount":0,` +
			`"EgressIPv4TotalCount":0,"EgressIPv6TotalCount":0}},"RequestId":"mock"}}`
	}

	t.Run("超过上限必须报错且不发送创建请求", func(t *testing.T) {
		mock, host := newMockCloudAPI(t)
		mock.defaultReply = statisticsReply(95)
		p := mockCVM(t, host)

		_, err := p.CreateRules(mockSnapshot(), makeRules(6))
		if err == nil {
			t.Fatal("95 + 6 > 100 必须返回错误")
		}
		if !strings.Contains(err.Error(), "上限 100") {
			t.Errorf("错误文案应说明上限: %v", err)
		}
		if got := len(requestsWithAction(t, mock.recorded(), "CreateSecurityGroupPolicies")); got != 0 {
			t.Errorf("超过上限时不得发送创建请求，实际 %d", got)
		}
	})

	t.Run("接近上限仅告警仍允许新增", func(t *testing.T) {
		mock, host := newMockCloudAPI(t)
		mock.defaultReply = statisticsReply(85)
		p := mockCVM(t, host)

		if _, err := p.CreateRules(mockSnapshot(), makeRules(6)); err != nil {
			t.Fatalf("85 + 6 = 91 未超上限，应允许（仅 WARN）: %v", err)
		}
		if got := len(requestsWithAction(t, mock.recorded(), "CreateSecurityGroupPolicies")); got != 1 {
			t.Errorf("创建请求数 = %d, want 1", got)
		}
	})

	t.Run("无 PolicyStatistics 时回退为手动计数", func(t *testing.T) {
		mock, host := newMockCloudAPI(t)
		ingress := strings.TrimSuffix(strings.Repeat(`{"PolicyIndex":1},`, 100), ",")
		mock.defaultReply = `{"Response":{"SecurityGroupPolicySet":{"Ingress":[` + ingress + `]},"RequestId":"mock"}}`
		p := mockCVM(t, host)

		if _, err := p.CreateRules(mockSnapshot(), makeRules(1)); err == nil {
			t.Fatal("手动计数 100 + 1 > 100 必须返回错误")
		}
		if got := len(requestsWithAction(t, mock.recorded(), "CreateSecurityGroupPolicies")); got != 0 {
			t.Errorf("超过上限时不得发送创建请求，实际 %d", got)
		}
	})
}

// makeRules 构造 n 条合法 TCP 规则
func makeRules(n int) []config.RuleAction {
	rules := make([]config.RuleAction, 0, n)
	for i := 0; i < n; i++ {
		rules = append(rules, config.RuleAction{
			Protocol: "TCP", Port: "443", Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] c",
		})
	}
	return rules
}

// ─── SWAS ───

// TestRequest_SWASPortSlashDropSkipAndDelete SWAS 端口用斜杠格式；DROP 规则跳过；删除用 RuleIds。
func TestRequest_SWASPortSlashDropSkipAndDelete(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	p := mockSWAS(t, host)

	// ConvertPorts 是 buildDesired 使用的入口：ICMP 的 ALL → -1/-1
	ports := p.ConvertPorts("ALL")
	if len(ports) != 1 || ports[0] != "-1/-1" {
		t.Fatalf("SWAS ConvertPorts(ALL) = %v, want [-1/-1]", ports)
	}
	// 全 ACCEPT：Written 必须等于实际提交条数，Skipped 为 0（Issue6 A11）
	res, err := p.CreateRules(mockSnapshot(), []config.RuleAction{
		{Protocol: "ICMP", Port: ports[0], Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] icmp"},
		{Protocol: "TCP", Port: "80/80", Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] tcp"},
	})
	if err != nil {
		t.Fatalf("CreateRules 失败: %v", err)
	}
	if res.Written != 2 || res.Skipped != 0 {
		t.Errorf("全 ACCEPT 结果 = %+v, want {Written:2 Skipped:0}", res)
	}

	creates := requestsWithAction(t, mock.recorded(), "CreateFirewallRules")
	if len(creates) != 1 {
		t.Fatalf("CreateFirewallRules 请求数 = %d, want 1", len(creates))
	}
	rules := decodeJSONArrayText(t, creates[0].query("FirewallRules"))
	if len(rules) != 2 {
		t.Fatalf("FirewallRules 数量 = %d, want 2", len(rules))
	}
	if got, _ := strField(rules[0], "Port"); got != "-1/-1" {
		t.Errorf("SWAS ICMP 端口 = %q, want -1/-1", got)
	}
	if got, _ := strField(rules[0], "RuleProtocol"); got != "ICMP" {
		t.Errorf("SWAS 协议字段 = %q, want ICMP", got)
	}
	if got, _ := strField(rules[0], "SourceCidrIp"); got != "1.2.3.4/32" {
		t.Errorf("SWAS SourceCidrIp = %q", got)
	}

	// 全 DROP：SWAS 不支持 DROP，必须全部跳过、不发送请求，并如实报告
	// {Written:0, Skipped:N}——修复前返回 nil，调用方无法区分「写入成功」与「全部跳过」，
	// 于是 added 虚增且每轮重复出现、永不收敛（Issue6 A11）。
	before := len(mock.recorded())
	dropRes, err := p.CreateRules(mockSnapshot(), []config.RuleAction{
		{Protocol: "TCP", Port: "80/80", Action: "DROP", CidrBlock: "1.2.3.4/32", Description: "[t] drop"},
	})
	if err != nil {
		t.Fatalf("全 DROP 跳过不应报错: %v", err)
	}
	if dropRes.Written != 0 || dropRes.Skipped != 1 {
		t.Errorf("全 DROP 结果 = %+v, want {Written:0 Skipped:1}", dropRes)
	}
	if got := len(mock.recorded()); got != before {
		t.Errorf("全 DROP 必须跳过且不发送请求：请求数 %d → %d", before, got)
	}

	// 混合 DROP + ACCEPT：只提交 ACCEPT，DROP 被跳过，两者分别计数
	mixedRes, err := p.CreateRules(mockSnapshot(), []config.RuleAction{
		{Protocol: "TCP", Port: "443/443", Action: "DROP", CidrBlock: "1.2.3.4/32", Description: "[t] drop"},
		{Protocol: "TCP", Port: "443/443", Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] accept"},
	})
	if err != nil {
		t.Fatalf("混合 DROP 不应报错: %v", err)
	}
	if mixedRes.Written != 1 || mixedRes.Skipped != 1 {
		t.Errorf("混合批次结果 = %+v, want {Written:1 Skipped:1}", mixedRes)
	}
	mixed := requestsWithAction(t, mock.recorded(), "CreateFirewallRules")
	rules = decodeJSONArrayText(t, mixed[len(mixed)-1].query("FirewallRules"))
	if len(rules) != 1 {
		t.Fatalf("混合场景只应提交 1 条 ACCEPT，实际 %d", len(rules))
	}

	// 删除：只使用 S1 回读的 RuleId。含空 RuleId 的批次必须整体拒绝（Issue7 §6.3），
	// 由规划器在候选阶段就把缺定位的规则排除（cleanup_deferred）。
	if _, err := p.DeleteRules(mockSnapshot(), []config.RuleInfo{{RuleID: "rule-a", Description: "[t] a"}}); err != nil {
		t.Fatalf("DeleteRules 失败: %v", err)
	}
	deletes := requestsWithAction(t, mock.recorded(), "DeleteFirewallRules")
	if len(deletes) != 1 {
		t.Fatalf("DeleteFirewallRules 请求数 = %d, want 1", len(deletes))
	}
	if ids := rpcCommaList(deletes[0], "RuleIds"); len(ids) != 1 || ids[0] != "rule-a" {
		t.Errorf("RuleIds = %v, want [rule-a]", ids)
	}
}

// TestRequest_SWASDeleteUsesRuleIdsOnly Issue7 §6.3：SWAS 清理只使用同一 S1 回读的
// 非空 RuleId；任一候选缺 RuleId 时必须整体拒绝，绝不按值降级删除。
func TestRequest_SWASDeleteUsesRuleIdsOnly(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	p := mockSWAS(t, host)

	res, err := p.DeleteRules(RuleSnapshot{}, []config.RuleInfo{
		{RuleID: "r1", Description: "[auto-dns] a"},
		{RuleID: "r2", Description: "[auto-dns] b"},
	})
	if err != nil {
		t.Fatalf("DeleteRules 失败: %v", err)
	}
	if res.Deleted != 2 || res.Resolved != 2 {
		t.Errorf("DeleteResult = %+v, want {Deleted:2 Resolved:2}", res)
	}
	deletes := requestsWithAction(t, mock.recorded(), "DeleteFirewallRules")
	if len(deletes) != 1 {
		t.Fatalf("DeleteFirewallRules 请求数 = %d, want 1", len(deletes))
	}
	if ids := rpcCommaList(deletes[0], "RuleIds"); len(ids) != 2 || ids[0] != "r1" || ids[1] != "r2" {
		t.Errorf("RuleIds = %v, want [r1 r2]", ids)
	}

	// 任一候选缺 RuleId：整体拒绝且不得发出任何删除请求
	before := len(requestsWithAction(t, mock.recorded(), "DeleteFirewallRules"))
	if _, err := p.DeleteRules(RuleSnapshot{}, []config.RuleInfo{
		{RuleID: "r1", Description: "[auto-dns] a"},
		{Description: "[auto-dns] 缺定位"},
	}); err == nil {
		t.Fatal("缺少 RuleId 的候选必须整体拒绝删除")
	}
	if after := len(requestsWithAction(t, mock.recorded(), "DeleteFirewallRules")); after != before {
		t.Errorf("定位不完整时不得发出删除请求: %d → %d", before, after)
	}
}

// ─── ECS ───

// TestRequest_ECSPortRangeIPv6DeleteAndBatching ECS 端口范围、IPv6 字段、删除标识与 100 条分批。
func TestRequest_ECSPortRangeIPv6DeleteAndBatching(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	p := mockECS(t, host)

	if got := p.ConvertPorts("ALL"); len(got) != 1 || got[0] != "-1/-1" {
		t.Fatalf("ECS ConvertPorts(ALL) = %v, want [-1/-1]", got)
	}

	if _, err := p.CreateRules(mockSnapshot(), []config.RuleAction{
		{Protocol: "ICMP", Port: "-1/-1", Action: "ACCEPT", CidrBlock: "1.2.3.4/32", Description: "[t] icmp"},
		{Protocol: "TCP", Port: "443/443", Action: "ACCEPT", Ipv6CidrBlock: "2001:db8::1/128", Description: "[t] v6"},
	}); err != nil {
		t.Fatalf("CreateRules 失败: %v", err)
	}

	auths := requestsWithAction(t, mock.recorded(), "AuthorizeSecurityGroup")
	if len(auths) != 1 {
		t.Fatalf("AuthorizeSecurityGroup 请求数 = %d, want 1", len(auths))
	}
	perms := rpcIndexedObjects(t, auths[0], "Permissions")
	if len(perms) != 2 {
		t.Fatalf("Permissions 数量 = %d, want 2", len(perms))
	}
	if got := perms[1]["PortRange"]; got != "-1/-1" {
		t.Errorf("ECS ICMP PortRange = %q, want -1/-1", got)
	}
	if got := perms[1]["IpProtocol"]; got != "ICMP" {
		t.Errorf("ECS IpProtocol = %q, want ICMP", got)
	}
	if got := perms[1]["SourceCidrIp"]; got != "1.2.3.4/32" {
		t.Errorf("ECS SourceCidrIp = %q", got)
	}
	if got := perms[2]["Ipv6SourceCidrIp"]; got != "2001:db8::1/128" {
		t.Errorf("ECS Ipv6SourceCidrIp = %q", got)
	}
	if _, ok := perms[2]["SourceCidrIp"]; ok {
		t.Errorf("IPv6 规则不得写 SourceCidrIp: %+v", perms[2])
	}

	// 删除：SecurityGroupRuleId 数组（定位不完整的候选由规划器 deferred，不进入删除请求）
	if _, err := p.DeleteRules(mockSnapshot(), []config.RuleInfo{{RuleID: "sgr-a", Description: "[t] a"}}); err != nil {
		t.Fatalf("DeleteRules 失败: %v", err)
	}
	revokes := requestsWithAction(t, mock.recorded(), "RevokeSecurityGroup")
	if len(revokes) != 1 {
		t.Fatalf("RevokeSecurityGroup 请求数 = %d, want 1", len(revokes))
	}
	ids := rpcIndexedList(t, revokes[0], "SecurityGroupRuleId")
	if len(ids) != 1 || ids[0] != "sgr-a" {
		t.Errorf("SecurityGroupRuleId = %v, want [sgr-a]", ids)
	}

	// 分批：150 条 → 100 + 50
	mockBefore := len(requestsWithAction(t, mock.recorded(), "AuthorizeSecurityGroup"))
	if _, err := p.CreateRules(mockSnapshot(), makeRules(150)); err != nil {
		t.Fatalf("分批 CreateRules 失败: %v", err)
	}
	auths = requestsWithAction(t, mock.recorded(), "AuthorizeSecurityGroup")
	if got := len(auths) - mockBefore; got != 2 {
		t.Fatalf("150 条必须分 2 批，实际新增请求 %d", got)
	}
	if got := len(rpcIndexedObjects(t, auths[mockBefore], "Permissions")); got != 100 {
		t.Errorf("第 1 批数量 = %d, want 100", got)
	}
	if got := len(rpcIndexedObjects(t, auths[mockBefore+1], "Permissions")); got != 50 {
		t.Errorf("第 2 批数量 = %d, want 50", got)
	}
}

// TestRequest_ECSDeleteBatches100 Issue7 §6.4：删除也必须每批最多 100，稳定切批；
// 150 个候选固定拆为 100 + 50。
func TestRequest_ECSDeleteBatches100(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	p := mockECS(t, host)

	candidates := make([]config.RuleInfo, 0, 150)
	for i := 0; i < 150; i++ {
		candidates = append(candidates, config.RuleInfo{RuleID: fmt.Sprintf("sgr-%03d", i), Description: "[auto-dns]"})
	}
	res, err := p.DeleteRules(RuleSnapshot{}, candidates)
	if err != nil {
		t.Fatalf("DeleteRules 失败: %v", err)
	}
	if res.Deleted != 150 || res.Resolved != 150 {
		t.Errorf("DeleteResult = %+v, want {Deleted:150 Resolved:150}", res)
	}

	revokes := requestsWithAction(t, mock.recorded(), "RevokeSecurityGroup")
	if len(revokes) != 2 {
		t.Fatalf("RevokeSecurityGroup 请求数 = %d, want 2（150 = 100 + 50）", len(revokes))
	}
	if got := len(rpcIndexedList(t, revokes[0], "SecurityGroupRuleId")); got != 100 {
		t.Errorf("第 1 批数量 = %d, want 100", got)
	}
	if got := len(rpcIndexedList(t, revokes[1], "SecurityGroupRuleId")); got != 50 {
		t.Errorf("第 2 批数量 = %d, want 50", got)
	}
}

// TestRequest_ECSDeleteSecondBatchFailureKeepsConfirmedProgress 第二批失败时，
// 第一批已确认删除必须保留，剩余候选由调用方计入 deferred（Issue7 §6.4）。
func TestRequest_ECSDeleteSecondBatchFailureKeepsConfirmedProgress(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(index int, _ recordedRequest) (int, string) {
		if index == 1 {
			return http.StatusInternalServerError,
				`{"Code":"InternalError","Message":"模拟第二批失败","RequestId":"mock"}`
		}
		return http.StatusOK, mock.defaultReply
	}
	p := mockECS(t, host)

	candidates := make([]config.RuleInfo, 0, 150)
	for i := 0; i < 150; i++ {
		candidates = append(candidates, config.RuleInfo{RuleID: fmt.Sprintf("sgr-%03d", i), Description: "[auto-dns]"})
	}
	res, err := p.DeleteRules(RuleSnapshot{}, candidates)
	if err == nil {
		t.Fatal("第二批失败必须返回错误")
	}
	var partial *PartialDeleteError
	if !errors.As(err, &partial) {
		t.Fatalf("部分成功必须返回 *PartialDeleteError，实际: %v", err)
	}
	if res.Deleted != 100 || res.Resolved != 100 {
		t.Errorf("DeleteResult = %+v, want {Deleted:100 Resolved:100}（保留前一批确认计数）", res)
	}
	if partial.Deleted != 100 {
		t.Errorf("PartialDeleteError.Deleted = %d, want 100", partial.Deleted)
	}
	if got := len(requestsWithAction(t, mock.recorded(), "RevokeSecurityGroup")); got != 2 {
		t.Errorf("请求数 = %d, want 2（失败后不再继续后续批次）", got)
	}
}

// TestRequest_ECSDeleteRejectsMissingLocator 定位不完整必须整体拒绝，不得按值降级删除。
func TestRequest_ECSDeleteRejectsMissingLocator(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	p := mockECS(t, host)

	if _, err := p.DeleteRules(RuleSnapshot{}, []config.RuleInfo{
		{RuleID: "sgr-a", Description: "[auto-dns] a"},
		{Description: "[auto-dns] 缺定位"},
	}); err == nil {
		t.Fatal("缺少 SecurityGroupRuleId 的候选必须整体拒绝删除")
	}
	if got := len(requestsWithAction(t, mock.recorded(), "RevokeSecurityGroup")); got != 0 {
		t.Errorf("定位不完整时不得发出删除请求，实际 %d", got)
	}
}

// TestRequest_ECSCreateReturnsConfirmedProgress 前一批成功、后一批失败时，Written
// 必须保留已由云端确认成功的 100 条，当前失败批次不计数。
func TestRequest_ECSCreateReturnsConfirmedProgress(t *testing.T) {
	mock, host := newMockCloudAPI(t)
	mock.reply = func(index int, _ recordedRequest) (int, string) {
		if index == 1 {
			return http.StatusInternalServerError, `{"Code":"InternalError","Message":"模拟失败","RequestId":"mock"}`
		}
		return http.StatusOK, mock.defaultReply
	}
	p := mockECS(t, host)

	res, err := p.CreateRules(mockSnapshot(), makeRules(101))
	if err == nil {
		t.Fatal("第 2 批失败必须返回错误")
	}
	if res.Written != 100 || res.Skipped != 0 {
		t.Errorf("CreateResult = %+v, want {Written:100 Skipped:0}", res)
	}
}
