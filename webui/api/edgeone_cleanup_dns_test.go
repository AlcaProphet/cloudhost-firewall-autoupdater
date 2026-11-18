package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Build9 Step 4 的 DNS 实际覆盖证据回归。
// 本地回环 DNS 替身只服务本组用例，不转发真实上游。

func s4SetupCVMWithDNS(
	t *testing.T,
	dnsMode string,
	rules []config.RuleInfo,
	withDNSRule bool,
) (*testEnv, int, *s4FakeProvider) {
	t.Helper()
	e := newTestEnv(t)
	id := e.seedTarget(t, config.CloudTCCVM, "ap-guangzhou", "sg-dns")
	if withDNSRule {
		withTxForTest(t, e.store, func(ctx context.Context, tx *sql.Tx) error {
			return e.store.AddRuleTx(ctx, tx, config.DomainRule{
				Host: "a.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT",
				Targets: []int{id}, Comment: "dns",
			})
		})
	}
	dnsAddr := s4StartDNS(t, dnsMode)
	fake := &s4FakeProvider{
		cloudType:   config.CloudTCCVM,
		targetIndex: id,
		snapshot:    provider.RuleSnapshot{Revision: "9", Rules: rules},
	}
	s4ApplyRuntime(t, e, []provider.Provider{fake}, &s4FakeTEO{}, func(rc *config.RuntimeConfig) {
		rc.DNS = dnsAddr
		rc.DNSTimeout = 2 * time.Second
	})
	return e, id, fake
}

func TestBuild9S4PreviewDNSConfirmedAcrossWholeBatch(t *testing.T) {
	// 同一功能 key 的两条 CVM 行各有唯一 PolicyIndex，均可删。DNS 规则只由
	// 这两行覆盖时，删除整批才会中断 DNS；逐行判断“还有另一行”会漏报。
	rules := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "192.0.2.23/32", Action: "ACCEPT", Description: "[edgeone] A", PolicyIndex: "1"},
		{Protocol: "TCP", Port: "443", CidrBlock: "192.0.2.23/32", Action: "ACCEPT", Description: "[edgeone] B", PolicyIndex: "2"},
	}
	e, id, fake := s4SetupCVMWithDNS(t, "v4", rules, true)
	w, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", w.Code, w.Body.String())
	}
	target := resp.Targets[0]
	if target.DeletableRows != 2 || len(target.Rows) != 2 {
		t.Fatalf("两条唯一索引行应可删: %+v", target.Rows)
	}
	dep := target.DNSDependency
	if !dep.Potential || !dep.Confirmed || dep.Unverified {
		t.Fatalf("整批覆盖应确认 DNS 依赖: %+v", dep)
	}
	if dep.ConfirmedRows != 2 || dep.ApplicableRules != 1 || dep.Status != "confirmed" {
		t.Fatalf("DNS 确认计数/状态错误: %+v", dep)
	}
	if fake.snapshotCalls != 1 {
		t.Fatalf("快照次数 = %d", fake.snapshotCalls)
	}
}

func TestBuild9S4PreviewDNSRemainingDNSTagPreventsConfirmation(t *testing.T) {
	// DNS TAG 下已有精确等价规则时，删除 EdgeOne 行不会移除最后覆盖。
	rules := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "192.0.2.23/32", Action: "ACCEPT", Description: "[edgeone] A", PolicyIndex: "1"},
		{Protocol: "TCP", Port: "443", CidrBlock: "192.0.2.23/32", Action: "ACCEPT", Description: "[auto-ddns] DNS", PolicyIndex: "2"},
	}
	e, id, _ := s4SetupCVMWithDNS(t, "v4", rules, true)
	_, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	target := resp.Targets[0]
	if target.DeletableRows != 1 {
		t.Fatalf("EdgeOne 行应可删、DNS TAG 行不进入候选: %+v", target.Rows)
	}
	dep := target.DNSDependency
	if !dep.Potential || dep.Confirmed || dep.Unverified || dep.ConfirmedRows != 0 || dep.Status != "potential" {
		t.Fatalf("DNS TAG 仍有覆盖时不得确认依赖: %+v", dep)
	}
}

func TestBuild9S4PreviewDNSManualCoveragePreventsConfirmation(t *testing.T) {
	// 无 TAG 手工规则同样是保留覆盖，不得把删除 EdgeOne 行标成最后覆盖。
	rules := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "192.0.2.23/32", Action: "ACCEPT", Description: "[edgeone] A", PolicyIndex: "1"},
		{Protocol: "TCP", Port: "443", CidrBlock: "192.0.2.23/32", Action: "ACCEPT", Description: "手工放行", PolicyIndex: "2"},
	}
	e, id, _ := s4SetupCVMWithDNS(t, "v4", rules, true)
	_, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	target := resp.Targets[0]
	if target.RetainedRows != 1 || target.DeletableRows != 1 {
		t.Fatalf("手工规则必须保留且不进入候选: %+v", target)
	}
	dep := target.DNSDependency
	if dep.Confirmed || dep.ConfirmedRows != 0 || dep.Status != "potential" {
		t.Fatalf("手工精确覆盖仍在时不得确认依赖: %+v", dep)
	}
}

func TestBuild9S4PreviewDNSUnverifiedOnResolutionFailure(t *testing.T) {
	rules := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "192.0.2.23/32", Action: "ACCEPT", Description: "[edgeone] A", PolicyIndex: "1"},
	}
	e, id, _ := s4SetupCVMWithDNS(t, "nxdomain", rules, true)
	_, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	dep := resp.Targets[0].DNSDependency
	if !dep.Potential || dep.Confirmed || !dep.Unverified || dep.Status != "unverified" {
		t.Fatalf("DNS 解析失败必须只标记未验证: %+v", dep)
	}
}

func TestBuild9S4PreviewDNSPotentialWithoutMatchingCoverage(t *testing.T) {
	rules := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "[edgeone] A", PolicyIndex: "1"},
	}
	e, id, _ := s4SetupCVMWithDNS(t, "v4", rules, true)
	_, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	dep := resp.Targets[0].DNSDependency
	if !dep.Potential || dep.Confirmed || dep.Unverified || dep.Status != "potential" {
		t.Fatalf("存在 DNS 规则但候选不覆盖时必须只标潜在: %+v", dep)
	}
}

func TestBuild9S4PreviewDNSNoneWithoutApplicableRules(t *testing.T) {
	rules := []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "192.0.2.23/32", Action: "ACCEPT", Description: "[edgeone] A", PolicyIndex: "1"},
	}
	e, id, _ := s4SetupCVMWithDNS(t, "v4", rules, false)
	_, resp := s4Preview(t, e, fmt.Sprintf(`{"target_ids":[%d]}`, id))
	dep := resp.Targets[0].DNSDependency
	if dep.Potential || dep.Confirmed || dep.Unverified || dep.Status != "none" {
		t.Fatalf("无适用 DNS 规则时必须为 none: %+v", dep)
	}
}
