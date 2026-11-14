package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func step12RowsSnapshot(store *step10Store) map[string]config.EdgeOneTransition {
	rows, _ := store.ListEdgeOneTransitions(nil)
	out := make(map[string]config.EdgeOneTransition, len(rows))
	for _, row := range rows {
		out[row.ZoneID] = row
	}
	return out
}

func TestStep12DryRunExecutionUnitMatchesFormalProtectedPlan(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b},
		config.EdgeOneConfirmationAccepted, "req-old")
	site := step8Site("zone-a")

	// 正式计划：B 受保护，只保留清理候选，不删除。
	pFormal := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teoFormal := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "", nil),
	}}
	stFormal := step11State(t, false, pFormal, []config.EdgeOneSite{site})
	sFormal := step11Syncer(t, stFormal, teoFormal, store, nil)
	sFormal.runScheduledRound(stFormal, false, true)
	_, _, formalDeletes := pFormal.counts()
	if formalDeletes != 0 {
		t.Fatalf("正式计划受保护时不得删除：%d", formalDeletes)
	}
	formalRound := sFormal.Status().LastRound
	if formalRound == nil || formalRound.CleanupDeferred != 1 {
		t.Fatalf("正式清理延后 = %+v", formalRound)
	}

	// 模拟：同一过渡记录与快照，只读计划应与正式一致。
	pDry := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teoDry := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "", nil),
	}}
	stDry := step11State(t, false, pDry, []config.EdgeOneSite{site})
	sDry := step11Syncer(t, stDry, teoDry, store, nil)
	resp, err := sDry.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	_, creates, deletes := pDry.counts()
	_, confirms := teoDry.counts()
	if creates != 0 || deletes != 0 || confirms != 0 {
		t.Fatalf("模拟不得写入：create=%d delete=%d confirm=%d", creates, deletes, confirms)
	}
	if len(resp.ExecutionUnits) != 1 || len(resp.Results) != 1 {
		t.Fatalf("模拟单元/结果 = %d/%d", len(resp.ExecutionUnits), len(resp.Results))
	}
	unit := resp.ExecutionUnits[0]
	if unit.ProtectedCIDRs == nil || len(unit.ProtectedCIDRs) != 1 || unit.ProtectedCIDRs[0] != b.String() {
		t.Fatalf("模拟保护集合 = %+v", unit.ProtectedCIDRs)
	}
	if len(unit.CleanupCandidates) != 1 || len(unit.CleanupDeletable) != 0 || len(unit.CleanupProtected) != 1 {
		t.Fatalf("模拟清理候选 = candidates:%d deletable:%d protected:%d", len(unit.CleanupCandidates), len(unit.CleanupDeletable), len(unit.CleanupProtected))
	}
	if len(unit.Sites) != 1 || unit.Sites[0].CurrentVersion != "v1" || unit.Sites[0].PendingVersion != "" {
		t.Fatalf("模拟站点版本 = %+v", unit.Sites)
	}
	if unit.Limit != nil || unit.LimitSource != "unknown" {
		t.Fatalf("ECS 限额必须显式未知：%+v %q", unit.Limit, unit.LimitSource)
	}
	if resp.Results[0].CleanupCandidates == nil || len(resp.Results[0].CleanupCandidates) != 1 {
		t.Fatalf("每目标结果清理候选 = %+v", resp.Results[0].CleanupCandidates)
	}
}

func TestStep12DryRunNoFormalSideEffects(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, nil,
		config.EdgeOneConfirmationUnknown, "")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "", nil),
	}}
	st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, map[string]string{"home.example": b.String()})
	beforeStatus := s.Status()
	beforeCoverage := s.RoundCoverage()
	beforeRows := step12RowsSnapshot(store)
	events, unsubscribe := s.bus.SubscribeChan()
	defer unsubscribe()

	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Preflight.State != PreflightStateOverlap {
		t.Fatalf("DNS 命中旧保护 B 必须预览为 overlap：%+v", resp.Preflight)
	}
	_, creates, deletes := p.counts()
	_, confirms := teo.counts()
	if creates != 0 || deletes != 0 || confirms != 0 {
		t.Fatalf("模拟写入计数 = create:%d delete:%d confirm:%d", creates, deletes, confirms)
	}
	afterRows := step12RowsSnapshot(store)
	if len(afterRows) != 1 || len(afterRows["zone-a"].ProtectedCIDRs) != 0 ||
		afterRows["zone-a"].ConfirmationState != config.EdgeOneConfirmationUnknown {
		t.Fatalf("模拟不得回填/改写过渡记录：%+v", afterRows)
	}
	if len(beforeRows) != len(afterRows) {
		t.Fatalf("过渡记录数量变化：%d → %d", len(beforeRows), len(afterRows))
	}
	afterStatus := s.Status()
	if afterStatus.Preflight.State != beforeStatus.Preflight.State ||
		!equalTimePtr(afterStatus.DNS.NextDueAt, beforeStatus.DNS.NextDueAt) ||
		!equalTimePtr(afterStatus.EdgeOne.NextDueAt, beforeStatus.EdgeOne.NextDueAt) {
		t.Fatalf("模拟改变了正式状态/下次到期：before=%+v after=%+v", beforeStatus, afterStatus)
	}
	if len(beforeCoverage) != len(s.RoundCoverage()) {
		t.Fatalf("模拟改变了覆盖证明：%v → %v", beforeCoverage, s.RoundCoverage())
	}
	select {
	case ev := <-events:
		t.Fatalf("模拟不得发布事件：%s", ev.Type)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStep12DryRunOnlyEdgeOneAndCapacity(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "v2", []string{b.String()}),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ExecutionUnits) != 1 {
		t.Fatalf("单元 = %+v", resp.ExecutionUnits)
	}
	unit := resp.ExecutionUnits[0]
	if unit.PlannedAdd != 1 || unit.RulesRead != 1 || unit.CurrentRuleCount != 1 || unit.TransitionPeak != 2 {
		t.Fatalf("容量字段 = %+v", unit)
	}
	if unit.Limit != nil || unit.LimitSource != "unknown" {
		t.Fatalf("ECS 限额未知 = %+v/%s", unit.Limit, unit.LimitSource)
	}
	if len(unit.Sites) != 1 || unit.Sites[0].CurrentVersion != "v1" || unit.Sites[0].PendingVersion != "v2" {
		t.Fatalf("站点版本 = %+v", unit.Sites)
	}
}

func TestStep12DryRunCVMKnownLimit(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step10State(t, p, teo)
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ExecutionUnits) != 1 || resp.ExecutionUnits[0].Limit == nil ||
		*resp.ExecutionUnits[0].Limit != 100 || resp.ExecutionUnits[0].LimitSource != "cvm_ingress_100" {
		t.Fatalf("CVM 已知限额 = %+v", resp.ExecutionUnits)
	}
}

func TestStep12DryRunPausedStillRuns(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	st.Config.SyncEnabled = false
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || len(resp.ExecutionUnits) != 1 {
		t.Fatalf("总暂停仍应可模拟：%+v", resp)
	}
}

func TestStep12DryRunArraysAndExecutionUnitsNotNull(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil),
	}}
	st := step11State(t, false, p, nil)
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"results", "warnings", "execution_units"} {
		var items []json.RawMessage
		if err := json.Unmarshal(top[key], &items); err != nil || items == nil {
			t.Fatalf("顶层 %s 必须是非 null 数组：%s err=%v", key, top[key], err)
		}
	}
	var pre map[string]json.RawMessage
	if err := json.Unmarshal(top["preflight"], &pre); err != nil {
		t.Fatal(err)
	}
	var protected []json.RawMessage
	if err := json.Unmarshal(pre["protected_cidrs"], &protected); err != nil || protected == nil {
		t.Fatalf("preflight.protected_cidrs 必须是非 null 数组：%s err=%v", pre["protected_cidrs"], err)
	}
	var units []map[string]json.RawMessage
	if err := json.Unmarshal(top["execution_units"], &units); err != nil || len(units) == 0 {
		t.Fatalf("execution_units = %s err=%v", top["execution_units"], err)
	}
	for _, field := range []string{"target_ids", "sites", "protected_cidrs", "conflicts", "cleanup_candidates", "cleanup_deletable", "cleanup_protected"} {
		var items []json.RawMessage
		if err := json.Unmarshal(units[0][field], &items); err != nil || items == nil {
			t.Fatalf("unit.%s 必须是非 null 数组：%s err=%v", field, units[0][field], err)
		}
	}
}

func equalTimePtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func TestStep12ConflictPreviewDoesNotClearPersistedConflict(t *testing.T) {
	stored := &config.AddressConflict{
		DNSName: "old.example", DNSAddress: "203.0.113.9", EdgeOnePrefix: "203.0.113.0/24",
		ZoneID: "zone-old", FirstSeenAt: time.Now().Add(-time.Hour).UTC(), LastSeenAt: time.Now().Add(-time.Hour).UTC(),
	}
	conflictStore := &step8MemConflictStore{conflict: stored}
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{"198.51.100.0/24"}, "", nil),
	}}
	st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
	st.TEO = teo
	s := New(NewRuntimeManager(st))
	s.SetConflictState(NewAddressConflictState(conflictStore))
	s.SetEdgeOneTransitionStore(newStep10Store())
	s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
		return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
	}
	s.sleepFn = func(time.Duration) {}
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Preflight.State != PreflightStateClear || resp.Preflight.KnownConflict {
		t.Fatalf("完整无重叠预览应为 clear：%+v", resp.Preflight)
	}
	if conflictStore.conflict == nil || conflictStore.conflict.ZoneID != "zone-old" {
		t.Fatalf("模拟不得清除已持久化冲突：%+v", conflictStore.conflict)
	}
}

func TestStep12PreviewMatchesFormalAdmission(t *testing.T) {
	inputs := []Preflight{
		{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true, EdgeOneComplete: true},
		{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: false, EdgeOneComplete: true},
		{DNSEnabled: true, EdgeOneEnabled: true, DNSComplete: true, EdgeOneComplete: true,
			DNS:     []DNSAddress{{Host: "a.example", Addr: netip.MustParseAddr("203.0.113.7")}},
			EdgeOne: []EdgeOnePrefix{{ZoneID: "zone-a", Prefix: netip.MustParsePrefix("203.0.113.0/24")}}},
	}
	for _, in := range inputs {
		formalStore := &step8MemConflictStore{}
		previewStore := &step8MemConflictStore{}
		formal := NewAddressConflictState(formalStore)
		preview := NewAddressConflictState(previewStore)
		gotFormal, errFormal := formal.Evaluate(context.Background(), in)
		gotPreview, errPreview := preview.Preview(context.Background(), in)
		if (errFormal == nil) != (errPreview == nil) || gotFormal.State != gotPreview.State ||
			gotFormal.Admission != gotPreview.Admission {
			t.Fatalf("正式/预览不一致：formal=%+v/%v preview=%+v/%v", gotFormal, errFormal, gotPreview, errPreview)
		}
	}
}

// TestStep12DryRunEdgeOneOnlyResults 验证仅 EdgeOne 启用时 results 不再
// 把未启用的 DNS 规则带入规划（旧实现会产生伪 dns_empty）。
func TestStep12DryRunEdgeOneOnlyResults(t *testing.T) {
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "v2", []string{b.String()}),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	st.Config.DomainRules = []config.DomainRule{{
		ID: 1, Host: "home.example", Protocol: "TCP", Ports: "443",
		Action: "ACCEPT", Targets: []int{0},
	}}
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || len(resp.ExecutionUnits) != 1 {
		t.Fatalf("results/units = %d/%d", len(resp.Results), len(resp.ExecutionUnits))
	}
	r := resp.Results[0]
	if len(r.Domains) != 0 || len(r.DNSErrors) != 0 {
		t.Fatalf("仅 EdgeOne 不得出现 DNS 规则/伪错误：domains=%v dns_errors=%+v", r.Domains, r.DNSErrors)
	}
	if len(r.EdgeOneSites) != 1 || r.EdgeOneSites[0] != "zone-a" || len(r.ToAdd) != 1 {
		t.Fatalf("EdgeOne 目标结果 = sites=%v to_add=%d", r.EdgeOneSites, len(r.ToAdd))
	}
	if resp.ExecutionUnits[0].PlannedAdd != 1 {
		t.Fatalf("执行单元预计新增 = %d", resp.ExecutionUnits[0].PlannedAdd)
	}
}

// TestStep12DryRunEdgeOneOnlyNoDNSRules 验证没有 DNS 规则但存在 EdgeOne 站点
// 时 results 仍表达真实 EdgeOne 计划，不能被 UI 当成“无适用规则”。
func TestStep12DryRunEdgeOneOnlyNoDNSRules(t *testing.T) {
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "v2", []string{b.String()}),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	st.Config.DomainRules = nil
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	r := resp.Results[0]
	if len(r.Domains) != 0 || len(r.EdgeOneSites) != 1 || len(r.ToAdd) != 1 {
		t.Fatalf("无 DNS 规则的 EdgeOne 目标 = domains=%v sites=%v to_add=%d", r.Domains, r.EdgeOneSites, len(r.ToAdd))
	}
}

// TestStep12DryRunSiteVersionAndCompleteness 验证已知版本在数据不完整时仍回显，
// 且执行单元 data_complete 与站点完整性一致（不能一边不完整一边“完整”）。
func TestStep12DryRunSiteVersionAndCompleteness(t *testing.T) {
	a := netip.MustParsePrefix("203.0.113.0/24")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "", nil),
	}}
	site := step8Site("zone-a")
	site.AddressFamily = "ipv6" // 快照只有 IPv4
	st := step11State(t, false, p, []config.EdgeOneSite{site})
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	unit := resp.ExecutionUnits[0]
	if unit.DataComplete || unit.Sites[0].DataComplete {
		t.Fatalf("完整性矛盾：unit=%v site=%v，两者都应为不完整", unit.DataComplete, unit.Sites[0].DataComplete)
	}
	if unit.Sites[0].CurrentVersion != "v1" {
		t.Fatalf("已知当前版本必须回显：%+v", unit.Sites[0])
	}
	if !strings.Contains(strings.Join(unit.Sites[0].Reasons, ","), "address_family_missing") {
		t.Fatalf("缺少地址族原因：%+v", unit.Sites[0].Reasons)
	}
}

// TestStep12DryRunIncompleteQuery 覆盖查询不完整：单元与站点均为不完整，
// 且模拟不产生任何云写入或事件。
func TestStep12DryRunIncompleteQuery(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step10TEO{describeFn: func() (provider.OriginACLSnapshot, error) {
		return provider.OriginACLSnapshot{}, errors.New("fixture: describe failed")
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	events, unsubscribe := s.bus.SubscribeChan()
	defer unsubscribe()
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Preflight.EdgeOneComplete {
		t.Fatalf("查询失败时 EdgeOne 预检必须不完整：%+v", resp.Preflight)
	}
	unit := resp.ExecutionUnits[0]
	if unit.DataComplete || unit.Sites[0].DataComplete {
		t.Fatalf("查询失败时单元/站点必须不完整：unit=%v site=%v", unit.DataComplete, unit.Sites[0].DataComplete)
	}
	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("模拟不得写入：creates=%d deletes=%d", creates, deletes)
	}
	if _, confirms := teo.counts(); confirms != 0 {
		t.Fatalf("模拟不得确认：%d", confirms)
	}
	select {
	case ev := <-events:
		t.Fatalf("模拟不得发布事件：%s", ev.Type)
	case <-time.After(20 * time.Millisecond):
	}
}
