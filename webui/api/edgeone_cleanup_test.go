package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"

	"golang.org/x/net/dns/dnsmessage"
)

// 本文件是 Build9 Step 4 的正式 HTTP 预览入口回归。
// 所有云读取由本地 fake Provider 提供；真实 mux、Deps、Store 与运行时接线不变。

type s4FakeProvider struct {
	cloudType   config.CloudType
	targetIndex int
	snapshot    provider.RuleSnapshot
	err         error

	snapshotCalls int
	createCalls   int
	deleteCalls   int
}

func (p *s4FakeProvider) Name() string {
	return fmt.Sprintf("%s#%d", p.cloudType, p.targetIndex)
}

func (p *s4FakeProvider) CloudType() config.CloudType { return p.cloudType }

func (p *s4FakeProvider) TargetIndex() int { return p.targetIndex }

func (p *s4FakeProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	p.snapshotCalls++
	return p.snapshot, p.err
}

func (p *s4FakeProvider) GetRules() ([]config.RuleInfo, error) {
	return p.snapshot.Rules, p.err
}

func (p *s4FakeProvider) CreateRules(provider.RuleSnapshot, []config.RuleAction) (provider.CreateResult, error) {
	p.createCalls++
	return provider.CreateResult{}, errors.New("预览不得创建云规则")
}

func (p *s4FakeProvider) DeleteRules(provider.RuleSnapshot, []config.RuleInfo) (provider.DeleteResult, error) {
	p.deleteCalls++
	return provider.DeleteResult{}, errors.New("预览不得删除云规则")
}

func (p *s4FakeProvider) ConvertPorts(port string) []string { return []string{port} }

type s4FakeTEO struct {
	describeCalls int
	confirmCalls  int
}

func (t *s4FakeTEO) DescribeOriginACL(context.Context, string) (provider.OriginACLSnapshot, error) {
	t.describeCalls++
	return provider.OriginACLSnapshot{}, errors.New("预览不得查询 TEO")
}

func (t *s4FakeTEO) ConfirmOriginACLUpdate(context.Context, string, func() bool) (string, error) {
	t.confirmCalls++
	return "", errors.New("预览不得确认 TEO")
}

// s4ApplyRuntime 用测试 Store 的当前业务快照发布一份完整运行时状态。
func s4ApplyRuntime(
	t *testing.T,
	e *testEnv,
	providers []provider.Provider,
	teo provider.OriginACLClient,
	mutate func(*config.RuntimeConfig),
) *syncer.RuntimeState {
	t.Helper()
	snapshot, err := e.store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("读取测试业务快照失败: %v", err)
	}
	rc := snapshot.ToRuntimeConfig()
	rc.Credentials = config.Credentials{
		TencentSecretID:       "tc-secret-id",
		TencentSecretKey:      "tc-secret-key",
		AliyunAccessKeyID:     "ali-access-id",
		AliyunAccessKeySecret: "ali-access-secret",
	}
	rc.DNS = "127.0.0.1:1"
	rc.DNSTimeout = time.Second
	if mutate != nil {
		mutate(&rc)
	}
	state := &syncer.RuntimeState{
		Config:    rc,
		Pool:      provider.NewClientPool(provider.Credentials{TencentSecretID: "tc", TencentSecretKey: "tc-key", AliyunAccessKeyID: "ali", AliyunAccessKeySecret: "ali-key"}),
		Providers: providers,
		Resolver:  dns.NewResolver(rc.DNS, rc.DNSTimeout),
		TEO:       teo,
	}
	e.runtime.Apply(state)
	return state
}

func s4SeedSite(t *testing.T, e *testEnv, site config.EdgeOneSite) {
	t.Helper()
	withTxForTest(t, e.store, func(ctx context.Context, tx *sql.Tx) error {
		return e.store.AddEdgeOneSiteTx(ctx, tx, site)
	})
}

func s4Preview(t *testing.T, e *testEnv, body string) (*httptest.ResponseRecorder, edgeOneCleanupPreviewResponse) {
	t.Helper()
	w := e.do(t, http.MethodPost, "/api/edgeone/firewall-cleanup/preview", body)
	var resp edgeOneCleanupPreviewResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析预览响应失败: %v; body=%s", err, w.Body.String())
		}
	}
	return w, resp
}

func s4Rule(description, protocol, port, cidr, action string) config.RuleInfo {
	return config.RuleInfo{
		Description: description,
		Protocol:    protocol,
		Port:        port,
		CidrBlock:   cidr,
		Action:      action,
	}
}

func s4TargetByCloud(resp edgeOneCleanupPreviewResponse, cloudType config.CloudType) (CleanupPreviewTarget, bool) {
	for _, target := range resp.Targets {
		if target.CloudType == cloudType {
			return target, true
		}
	}
	return CleanupPreviewTarget{}, false
}

func TestBuild9S4PreviewMixedTagsAndCounts(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-mixed")
	s4SeedSite(t, e, config.EdgeOneSite{
		ZoneID: "zone-mixed", TargetIDs: []int{id},
		Protocol: "TCP", Ports: "80,443", AddressFamily: "ipv4",
	})
	e.seedRule(t, config.DomainRule{
		Host: "dns.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
		Targets: []int{id}, Comment: "dns",
	})

	fake := &s4FakeProvider{
		cloudType:   config.CloudTCLighthouse,
		targetIndex: id,
		snapshot: provider.RuleSnapshot{
			Revision: "7",
			Rules: []config.RuleInfo{
				s4Rule("[edgeone]", "TCP", "80", "1.1.1.1/32", "ACCEPT"),
				s4Rule("[edgeone] 主站", "TCP", "80,443", "2.2.2.0/24", "ACCEPT"),
				s4Rule("[edgeone]foo", "TCP", "80", "3.3.3.3/32", "ACCEPT"),
				s4Rule("[edgeone-old] 历史", "TCP", "80", "4.4.4.4/32", "ACCEPT"),
				s4Rule("[auto-ddns] DNS", "TCP", "80", "5.5.5.5/32", "ACCEPT"),
				s4Rule("手工规则", "TCP", "80", "6.6.6.6/32", "ACCEPT"),
			},
		},
	}
	teo := &s4FakeTEO{}
	before, err := e.store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("读取零写入前快照失败: %v", err)
	}
	beforeLogs, err := e.store.GetSyncLogs(100)
	if err != nil {
		t.Fatalf("读取零写入前日志失败: %v", err)
	}
	dnsAddr := s4StartDNS(t, "v4")
	s4ApplyRuntime(t, e, []provider.Provider{fake}, teo, func(rc *config.RuntimeConfig) {
		rc.DNS = dnsAddr
		rc.DNSTimeout = 2 * time.Second
	})

	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if resp.PreviewID == "" || resp.ExpiresAt == nil {
		t.Fatalf("必须签发预览标识与过期时间: %+v", resp)
	}
	if resp.Tag != "edgeone" || resp.Scope != cleanupScopeSelected {
		t.Fatalf("tag/scope = %q/%q", resp.Tag, resp.Scope)
	}
	if !slicesEqualInt(resp.SelectedTargetIDs, []int{id}) {
		t.Fatalf("selected_target_ids = %v, want [%d]", resp.SelectedTargetIDs, id)
	}
	if !resp.AllRead || resp.Summary.Targets != 1 || resp.Summary.TargetsRead != 1 || resp.Summary.TargetsUnread != 0 {
		t.Fatalf("目标汇总错误: %+v", resp.Summary)
	}
	if resp.Summary.RulesRead != 6 || resp.Summary.MatchedRows != 2 || resp.Summary.RetainedRows != 4 ||
		resp.Summary.DeletableRows != 2 || resp.Summary.SkippedRows != 0 {
		t.Fatalf("物理行汇总错误: %+v", resp.Summary)
	}
	if len(resp.Targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(resp.Targets))
	}
	target := resp.Targets[0]
	if target.Status != cleanupTargetRead || target.Revision != "7" {
		t.Fatalf("目标状态/revision 错误: %+v", target)
	}
	if !slicesEqualInt(target.TargetIDs, []int{id}) {
		t.Fatalf("target_ids = %v", target.TargetIDs)
	}
	if len(target.Sites) != 1 || target.Sites[0].ZoneID != "zone-mixed" {
		t.Fatalf("站点绑定错误: %+v", target.Sites)
	}
	if len(target.Rows) != 2 {
		t.Fatalf("rows = %d, want 2（80,443 只算一行）: %+v", len(target.Rows), target.Rows)
	}
	descriptions := []string{target.Rows[0].Description, target.Rows[1].Description}
	if !slicesEqualString(descriptions, []string{"[edgeone]", "[edgeone] 主站"}) {
		t.Fatalf("匹配名单错误: %v", descriptions)
	}
	for _, retained := range []string{"[edgeone]foo", "[edgeone-old] 历史", "[auto-ddns] DNS", "手工规则"} {
		for _, row := range target.Rows {
			if row.Description == retained {
				t.Fatalf("保留项不得进入规则明细: %q", retained)
			}
		}
	}
	if target.Rows[1].Port != "80,443" || len(target.Rows[1].FunctionalKeys) != 2 {
		t.Fatalf("多端口行必须保留完整表达式并展开两个 key: %+v", target.Rows[1])
	}
	for _, row := range target.Rows {
		if row.Status != provider.FirewallCleanupDeletable || row.ReasonCode != "" {
			t.Fatalf("合法前缀行应可删: %+v", row)
		}
	}
	if !target.DNSDependency.Potential || target.DNSDependency.Confirmed || target.DNSDependency.Unverified ||
		target.DNSDependency.Status != "potential" {
		t.Fatalf("DNS 依赖应为潜在而非已确认: %+v", target.DNSDependency)
	}

	if fake.snapshotCalls != 1 || fake.createCalls != 0 || fake.deleteCalls != 0 {
		t.Fatalf("Provider 调用次数错误: snapshot=%d create=%d delete=%d", fake.snapshotCalls, fake.createCalls, fake.deleteCalls)
	}
	if teo.describeCalls != 0 || teo.confirmCalls != 0 {
		t.Fatalf("TEO 调用必须为零: describe=%d confirm=%d", teo.describeCalls, teo.confirmCalls)
	}
	after, err := e.store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatalf("读取零写入后快照失败: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("预览不得改变业务配置")
	}
	afterLogs, err := e.store.GetSyncLogs(100)
	if err != nil {
		t.Fatalf("读取零写入后日志失败: %v", err)
	}
	if len(afterLogs) != len(beforeLogs) || e.applyCount() != 0 {
		t.Fatalf("预览不得写同步日志或发布配置: logs %d→%d apply=%d", len(beforeLogs), len(afterLogs), e.applyCount())
	}

	// 响应数组必须是非 null JSON 数组。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析原始响应失败: %v", err)
	}
	for _, field := range []string{"targets", "warnings"} {
		if strings.TrimSpace(string(raw[field])) == "null" || !strings.HasPrefix(strings.TrimSpace(string(raw[field])), "[") {
			t.Fatalf("%s 必须是 JSON 数组，实际 %s", field, raw[field])
		}
	}
}

func TestBuild9S4PreviewAliasDedupAndSiteLookup(t *testing.T) {
	e := newTestEnv(t)
	id1 := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-alias")
	id2 := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-alias")
	s4SeedSite(t, e, config.EdgeOneSite{
		ZoneID: "zone-alias", TargetIDs: []int{id2},
		Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
	})
	fake := &s4FakeProvider{
		cloudType:   config.CloudTCLighthouse,
		targetIndex: id1,
		snapshot: provider.RuleSnapshot{
			Revision: "8",
			Rules:    []config.RuleInfo{s4Rule("[edgeone] alias", "TCP", "443", "1.1.1.1/32", "ACCEPT")},
		},
	}
	s4ApplyRuntime(t, e, []provider.Provider{fake}, &s4FakeTEO{}, nil)

	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id2))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if fake.snapshotCalls != 1 {
		t.Fatalf("别名必须合并为一次快照，实际 %d", fake.snapshotCalls)
	}
	if len(resp.Targets) != 1 {
		t.Fatalf("物理目标数 = %d, want 1", len(resp.Targets))
	}
	target := resp.Targets[0]
	if !slicesEqualInt(target.TargetIDs, []int{id1, id2}) {
		t.Fatalf("target_ids 必须包含全部别名: %v", target.TargetIDs)
	}
	if !slicesEqualInt(resp.SelectedTargetIDs, []int{id2}) {
		t.Fatalf("selected_target_ids 必须保留用户选择: %v", resp.SelectedTargetIDs)
	}
	if len(target.Sites) != 1 || target.Sites[0].ZoneID != "zone-alias" {
		t.Fatalf("必须跨别名找到站点绑定: %+v", target.Sites)
	}
}

func TestBuild9S4PreviewAllFourPlatformsAndUnbound(t *testing.T) {
	e := newTestEnv(t)
	lh := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-all")
	cvm := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-all")
	swas := e.seedTarget(t, config.CloudAliSWAS, "cn-hangzhou", "swas-all")
	ecs := e.seedTarget(t, config.CloudAliECS, "cn-beijing", "sg-ecs")
	s4SeedSite(t, e, config.EdgeOneSite{
		ZoneID: "zone-all", TargetIDs: []int{lh},
		Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
	})

	lhFake := &s4FakeProvider{cloudType: config.CloudTCLighthouse, targetIndex: lh, snapshot: provider.RuleSnapshot{
		Revision: "1", Rules: []config.RuleInfo{s4Rule("[edgeone] lh", "TCP", "443", "1.1.1.1/32", "ACCEPT")},
	}}
	cvmFake := &s4FakeProvider{cloudType: config.CloudTCCVM, targetIndex: cvm, snapshot: provider.RuleSnapshot{
		Revision: "2", Rules: []config.RuleInfo{{Protocol: "TCP", Port: "443", CidrBlock: "2.2.2.2/32", Action: "ACCEPT", Description: "[edgeone] cvm", PolicyIndex: "1"}},
	}}
	swasFake := &s4FakeProvider{cloudType: config.CloudAliSWAS, targetIndex: swas, snapshot: provider.RuleSnapshot{
		Rules: []config.RuleInfo{{Protocol: "TCP", Port: "443", CidrBlock: "3.3.3.3/32", Action: "ACCEPT", Description: "[edgeone] swas", RuleID: "r1"}},
	}}
	ecsFake := &s4FakeProvider{cloudType: config.CloudAliECS, targetIndex: ecs, snapshot: provider.RuleSnapshot{
		Rules: []config.RuleInfo{{Protocol: "TCP", Port: "443", CidrBlock: "4.4.4.4/32", Action: "ACCEPT", Description: "[edgeone] ecs", RuleID: "r2"}},
	}}
	s4ApplyRuntime(t, e, []provider.Provider{lhFake, cvmFake, swasFake, ecsFake}, &s4FakeTEO{}, nil)

	w, resp := s4Preview(t, e, `{"all":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if resp.Scope != cleanupScopeAll || len(resp.SelectedTargetIDs) != 4 {
		t.Fatalf("all 范围错误: scope=%q ids=%v", resp.Scope, resp.SelectedTargetIDs)
	}
	if resp.Summary.Targets != 4 || resp.Summary.TargetsRead != 4 || resp.Summary.DeletableRows != 4 {
		t.Fatalf("四平台汇总错误: %+v", resp.Summary)
	}
	for _, fake := range []*s4FakeProvider{lhFake, cvmFake, swasFake, ecsFake} {
		if fake.snapshotCalls != 1 {
			t.Fatalf("%s 快照次数 = %d, want 1", fake.Name(), fake.snapshotCalls)
		}
	}
	cvmTarget, ok := s4TargetByCloud(resp, config.CloudTCCVM)
	if !ok {
		t.Fatal("缺少 CVM 目标")
	}
	if len(cvmTarget.Sites) != 0 {
		t.Fatalf("未绑定站点的目标应可读且 sites 为空数组: %+v", cvmTarget.Sites)
	}
	if cvmTarget.Sites == nil {
		t.Fatal("sites 必须是非 null 数组")
	}
	swasTarget, ok := s4TargetByCloud(resp, config.CloudAliSWAS)
	if !ok || swasTarget.Revision != "" {
		t.Fatalf("SWAS 目标/revision 错误: %+v", swasTarget)
	}
}

func TestBuild9S4PreviewEmptyAndUnknownScope(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-empty")
	fake := &s4FakeProvider{cloudType: config.CloudTCLighthouse, targetIndex: id, snapshot: provider.RuleSnapshot{Revision: "1"}}
	s4ApplyRuntime(t, e, []provider.Provider{fake}, &s4FakeTEO{}, nil)

	bodies := []string{
		`{}`,
		`{"target_ids":[]}`,
		`{"all":false}`,
		fmt.Sprintf(`{"all":true,"target_ids":[%d]}`, id),
		`{"target_ids":[999]}`,
		`{"target_ids":[0]}`,
		fmt.Sprintf(`{"target_ids":[%d],"tag":"edgeone"}`, id),
		fmt.Sprintf(`{"target_ids":[%d],"all":"yes"}`, id),
		fmt.Sprintf(`{"target_ids":[%d]} {"x":1}`, id),
	}
	for _, body := range bodies {
		w := e.do(t, http.MethodPost, "/api/edgeone/firewall-cleanup/preview", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s 状态码 = %d, want 400; response=%s", body, w.Code, w.Body.String())
		}
	}
	if fake.snapshotCalls != 0 {
		t.Fatalf("非法范围不得读取快照，实际 %d", fake.snapshotCalls)
	}

	// 重复目标 ID 去重，且只按一个物理目标读取一次。
	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d,%d]}`, id, id))
	if w.Code != http.StatusOK {
		t.Fatalf("重复 ID 状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if !slicesEqualInt(resp.SelectedTargetIDs, []int{id}) || len(resp.Targets) != 1 {
		t.Fatalf("重复 ID 必须去重: selected=%v targets=%d", resp.SelectedTargetIDs, len(resp.Targets))
	}
	if fake.snapshotCalls != 1 {
		t.Fatalf("重复 ID 只应读取一次快照，实际 %d", fake.snapshotCalls)
	}

	// all:false 与 target_ids 同时出现按显式选择处理，不扩大为全部。
	w, resp = s4Preview(t, e, fmt.Sprintf(`{"all":false,"target_ids":[%d]}`, id))
	if w.Code != http.StatusOK || resp.Scope != cleanupScopeSelected {
		t.Fatalf("all:false + target_ids 状态/范围错误: code=%d scope=%q body=%s", w.Code, resp.Scope, w.Body.String())
	}

	// 明确 all 但当前没有任何配置目标：400，不得伪装成空预览。
	emptyEnv := newTestEnv(t)
	s4ApplyRuntime(t, emptyEnv, nil, &s4FakeTEO{}, nil)
	w = emptyEnv.do(t, http.MethodPost, "/api/edgeone/firewall-cleanup/preview", `{"all":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("无目标 all 状态码 = %d, want 400; response=%s", w.Code, w.Body.String())
	}
}

func TestBuild9S4PreviewRuntimeUnavailable(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodPost, "/api/edgeone/firewall-cleanup/preview", `{"all":true}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("运行时未就绪状态码 = %d, want 503; body=%s", w.Code, w.Body.String())
	}
}

func TestBuild9S4PreviewPartialUnreadAndAllUnread(t *testing.T) {
	e := newTestEnv(t)
	lh := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-ok")
	cvm := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-bad")
	okFake := &s4FakeProvider{cloudType: config.CloudTCLighthouse, targetIndex: lh, snapshot: provider.RuleSnapshot{
		Revision: "5", Rules: []config.RuleInfo{s4Rule("[edgeone] ok", "TCP", "443", "1.1.1.1/32", "ACCEPT")},
	}}
	badFake := &s4FakeProvider{cloudType: config.CloudTCCVM, targetIndex: cvm, err: fmt.Errorf("%w: 分页失败", provider.ErrSnapshotIncomplete)}
	s4ApplyRuntime(t, e, []provider.Provider{okFake, badFake}, &s4FakeTEO{}, nil)

	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d,%d]}`, lh, cvm))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if resp.PreviewID == "" || resp.ExpiresAt == nil {
		t.Fatalf("部分读取成功仍应签发预览标识: %+v", resp)
	}
	if resp.AllRead || resp.Summary.TargetsRead != 1 || resp.Summary.TargetsUnread != 1 {
		t.Fatalf("部分未读汇总错误: %+v", resp.Summary)
	}
	bad, ok := s4TargetByCloud(resp, config.CloudTCCVM)
	if !ok || bad.Status != cleanupTargetUnread || bad.ReasonCode != "snapshot_incomplete" {
		t.Fatalf("未读目标状态错误: %+v", bad)
	}
	if bad.Rows == nil || len(bad.Rows) != 0 {
		t.Fatalf("未读目标 rows 必须是非 null 空数组: %+v", bad.Rows)
	}
	if okFake.snapshotCalls != 1 || badFake.snapshotCalls != 1 {
		t.Fatalf("部分未读仍须各自尝试一次: ok=%d bad=%d", okFake.snapshotCalls, badFake.snapshotCalls)
	}

	// 全部未读：仍返回 unread 明细，但不签发可执行预览标识。
	allBadEnv := newTestEnv(t)
	allBadID := allBadEnv.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-all-bad")
	allBad := &s4FakeProvider{cloudType: config.CloudTCCVM, targetIndex: allBadID, err: errors.New("network down")}
	s4ApplyRuntime(t, allBadEnv, []provider.Provider{allBad}, &s4FakeTEO{}, nil)
	w, resp = s4Preview(t, allBadEnv, fmt.Sprintf(`{"target_ids":[%d]}`, allBadID))
	if w.Code != http.StatusOK {
		t.Fatalf("全部未读状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if resp.PreviewID != "" || resp.ExpiresAt != nil {
		t.Fatalf("全部未读不得签发预览标识: %+v", resp)
	}
	if resp.AllRead || resp.Summary.TargetsUnread != 1 || len(resp.Warnings) == 0 {
		t.Fatalf("全部未读必须明确标记: %+v", resp)
	}

	// 凭据未配置：目标可读但不得访问云，不得签发候选。
	credEnv := newTestEnv(t)
	swas := credEnv.seedTarget(t, config.CloudAliSWAS, "cn-hangzhou", "swas-no-cred")
	credFake := &s4FakeProvider{cloudType: config.CloudAliSWAS, targetIndex: swas}
	s4ApplyRuntime(t, credEnv, []provider.Provider{credFake}, &s4FakeTEO{}, func(rc *config.RuntimeConfig) {
		rc.Credentials.AliyunAccessKeyID = ""
		rc.Credentials.AliyunAccessKeySecret = ""
	})
	w, resp = s4Preview(t, credEnv, fmt.Sprintf(`{"target_ids":[%d]}`, swas))
	if w.Code != http.StatusOK {
		t.Fatalf("凭据缺失状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	if resp.PreviewID != "" {
		t.Fatalf("凭据缺失不得签发预览标识: %+v", resp)
	}
	if len(resp.Targets) != 1 || resp.Targets[0].ReasonCode != "credentials_missing" {
		t.Fatalf("凭据缺失原因错误: %+v", resp.Targets)
	}
	if credFake.snapshotCalls != 0 {
		t.Fatalf("凭据缺失不得调用快照，实际 %d", credFake.snapshotCalls)
	}
}

func TestBuild9S4PreviewExplicitUnboundTarget(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-unbound")
	fake := &s4FakeProvider{cloudType: config.CloudTCLighthouse, targetIndex: id, snapshot: provider.RuleSnapshot{
		Revision: "3",
		Rules:    []config.RuleInfo{s4Rule("[edgeone] unbound", "TCP", "443", "1.1.1.1/32", "ACCEPT")},
	}}
	s4ApplyRuntime(t, e, []provider.Provider{fake}, &s4FakeTEO{}, nil)
	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	target := resp.Targets[0]
	if target.Status != cleanupTargetRead || target.DeletableRows != 1 {
		t.Fatalf("已解绑但仍配置的目标必须可预览: %+v", target)
	}
	if target.Sites == nil || len(target.Sites) != 0 {
		t.Fatalf("未绑定站点应为非 null 空数组: %+v", target.Sites)
	}
}

func TestBuild9S4PreviewLighthouseMultiPortAmbiguity(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-multi")
	fake := &s4FakeProvider{cloudType: config.CloudTCLighthouse, targetIndex: id, snapshot: provider.RuleSnapshot{
		Revision: "11",
		Rules: []config.RuleInfo{
			s4Rule("[edgeone] 多端口冲突", "TCP", "80,443", "1.1.1.0/24", "ACCEPT"),
			s4Rule("[edgeone] 443冲突", "TCP", "443", "1.1.1.0/24", "ACCEPT"),
			s4Rule("[edgeone] 独立多端口", "TCP", "80,443", "2.2.2.0/24", "ACCEPT"),
		},
	}}
	s4ApplyRuntime(t, e, []provider.Provider{fake}, &s4FakeTEO{}, nil)
	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	target := resp.Targets[0]
	if target.MatchedRows != 3 || target.DeletableRows != 1 || target.SkippedRows != 2 {
		t.Fatalf("多端口歧义计数错误: %+v", target)
	}
	byDescription := make(map[string]CleanupPreviewRow)
	for _, row := range target.Rows {
		byDescription[row.Description] = row
	}
	if got := byDescription["[edgeone] 多端口冲突"].ReasonCode; got != provider.FirewallCleanupReasonKeyAmbiguous {
		t.Fatalf("多端口行任一 key 冲突必须整行跳过，原因 = %q", got)
	}
	if got := byDescription["[edgeone] 443冲突"].ReasonCode; got != provider.FirewallCleanupReasonKeyAmbiguous {
		t.Fatalf("443 单端口行也必须跳过，原因 = %q", got)
	}
	independent := byDescription["[edgeone] 独立多端口"]
	if independent.Status != provider.FirewallCleanupDeletable || len(independent.FunctionalKeys) != 2 {
		t.Fatalf("无歧义多端口行应可删且保留两个 key: %+v", independent)
	}
}

func TestBuild9S4PreviewLighthouseMultiPortSecondKeyAmbiguity(t *testing.T) {
	// 443 排序在前且唯一、80 排序在后且冲突：必须仍检查整行每个 key。
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-multi-second")
	fake := &s4FakeProvider{cloudType: config.CloudTCLighthouse, targetIndex: id, snapshot: provider.RuleSnapshot{
		Revision: "12",
		Rules: []config.RuleInfo{
			s4Rule("[edgeone] 多端口80冲突", "TCP", "80,443", "1.1.1.0/24", "ACCEPT"),
			s4Rule("[edgeone] 80冲突", "TCP", "80", "1.1.1.0/24", "ACCEPT"),
			s4Rule("[edgeone] 独立", "TCP", "80,443", "2.2.2.0/24", "ACCEPT"),
		},
	}}
	s4ApplyRuntime(t, e, []provider.Provider{fake}, &s4FakeTEO{}, nil)
	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	target := resp.Targets[0]
	if target.MatchedRows != 3 || target.DeletableRows != 1 || target.SkippedRows != 2 {
		t.Fatalf("第二个端口歧义计数错误: %+v", target)
	}
	for _, row := range target.Rows {
		if row.Description == "[edgeone] 多端口80冲突" {
			if row.ReasonCode != provider.FirewallCleanupReasonKeyAmbiguous {
				t.Fatalf("后排序端口的冲突也必须跳过整行: %+v", row)
			}
		}
	}
}

func TestBuild9S4PreviewCVMDuplicateLocators(t *testing.T) {
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-locator")
	fake := &s4FakeProvider{cloudType: config.CloudTCCVM, targetIndex: id, snapshot: provider.RuleSnapshot{
		Revision: "9",
		Rules: []config.RuleInfo{
			{Protocol: "TCP", Port: "80", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[edgeone] 唯一", PolicyIndex: "1"},
			{Protocol: "TCP", Port: "81", CidrBlock: "2.2.2.2/32", Action: "ACCEPT", Description: "[edgeone] 重复A", PolicyIndex: "2"},
			{Protocol: "TCP", Port: "82", CidrBlock: "3.3.3.3/32", Action: "ACCEPT", Description: "[edgeone] 重复B", PolicyIndex: "2"},
			{Protocol: "TCP", Port: "83", CidrBlock: "4.4.4.4/32", Action: "ACCEPT", Description: "[edgeone] 缺失", PolicyIndex: ""},
		},
	}}
	s4ApplyRuntime(t, e, []provider.Provider{fake}, &s4FakeTEO{}, nil)
	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	target := resp.Targets[0]
	if target.Revision != "9" || target.MatchedRows != 4 || target.DeletableRows != 1 || target.SkippedRows != 3 {
		t.Fatalf("CVM 定位计数错误: %+v", target)
	}
	reasons := make(map[string]string)
	for _, row := range target.Rows {
		reasons[row.Description] = row.ReasonCode
	}
	if reasons["[edgeone] 唯一"] != "" ||
		reasons["[edgeone] 重复A"] != provider.FirewallCleanupReasonLocatorDuplicate ||
		reasons["[edgeone] 重复B"] != provider.FirewallCleanupReasonLocatorDuplicate ||
		reasons["[edgeone] 缺失"] != provider.FirewallCleanupReasonLocatorMissing {
		t.Fatalf("CVM 行原因错误: %+v", reasons)
	}
}

// s4LocalDNS 是只服务本组用例的回环 DNS 替身，不转发查询。
type s4LocalDNS struct {
	conn net.PacketConn
	done chan struct{}
}

func s4StartDNS(t *testing.T, mode string) string {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动本地 DNS 失败: %v", err)
	}
	s := &s4LocalDNS{conn: conn, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		buf := make([]byte, 4096)
		for {
			n, peer, rerr := conn.ReadFrom(buf)
			if rerr != nil {
				return
			}
			var req dnsmessage.Message
			if uerr := req.Unpack(buf[:n]); uerr != nil || len(req.Questions) != 1 {
				continue
			}
			q := req.Questions[0]
			reply := dnsmessage.Message{
				Header: dnsmessage.Header{
					ID: req.ID, Response: true, Authoritative: true,
					RecursionDesired: req.RecursionDesired, RecursionAvailable: true,
				},
				Questions: req.Questions,
			}
			if mode == "nxdomain" {
				reply.RCode = dnsmessage.RCodeNameError
			}
			if mode == "v4" && q.Type == dnsmessage.TypeA {
				reply.Answers = append(reply.Answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.AResource{A: [4]byte{192, 0, 2, 23}},
				})
			}
			packed, perr := reply.Pack()
			if perr != nil {
				continue
			}
			_, _ = conn.WriteTo(packed, peer)
		}
	}()
	t.Cleanup(func() {
		_ = conn.Close()
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			t.Error("本地 DNS 替身未退出")
		}
	})
	return conn.LocalAddr().String()
}

func slicesEqualInt(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func slicesEqualString(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
