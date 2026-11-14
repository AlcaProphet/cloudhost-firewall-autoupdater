package syncer

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Build8 Step 11 的过渡保护与清理回归：只使用本地 TEO/Provider
// 替身与内存过渡表，不访问真实腾讯云或 SQLite 文件。

func step11Snapshot(current string, currentPrefixes []string, next string, nextPrefixes []string) provider.OriginACLSnapshot {
	parse := func(values []string) []netip.Prefix {
		out := make([]netip.Prefix, 0, len(values))
		for _, v := range values {
			out = append(out, netip.MustParsePrefix(v))
		}
		return out
	}
	snap := provider.OriginACLSnapshot{
		ZoneID:    "zone-a",
		Status:    "online",
		Current:   provider.OriginACLVersion{Version: current, Prefixes: parse(currentPrefixes)},
		RequestID: "req-describe",
	}
	if next != "" {
		snap.Next = &provider.OriginACLVersion{Version: next, Prefixes: parse(nextPrefixes)}
	}
	return snap
}

func step11Rule(cidr, port string) config.RuleInfo {
	return config.RuleInfo{
		CidrBlock: cidr, Protocol: "TCP", Port: port, Action: "ACCEPT",
		Description: "[edgeone]", RuleID: "r-" + strings.ReplaceAll(strings.ReplaceAll(cidr, "/", "-"), ".", "-"),
	}
}

func step11State(t *testing.T, dnsEnabled bool, p *targetProbeProvider, sites []config.EdgeOneSite) *RuntimeState {
	t.Helper()
	st := step8State(t, dnsEnabled, true, time.Hour, time.Hour, p, sites)
	st.Config.EdgeOneTag = "edgeone"
	return st
}

func step11Syncer(t *testing.T, st *RuntimeState, teo provider.OriginACLClient, store EdgeOneTransitionStore, resolve map[string]string) *Syncer {
	t.Helper()
	st.TEO = teo
	s := New(NewRuntimeManager(st))
	s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
	if store != nil {
		s.SetEdgeOneTransitionStore(store)
	}
	s.sleepFn = func(time.Duration) {}
	s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
		cidr, ok := resolve[strings.ToLower(host)]
		if !ok {
			return nil, errors.New("fixture: no DNS answer")
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, err
		}
		addr := prefix.Addr()
		return []dns.ResolvedIP{{IP: net.IP(addr.AsSlice()), IsIPv6: addr.Is6() && !addr.Is4()}}, nil
	}
	return s
}

func step11Row(zone, base, pending string, pendingCIDRs []netip.Prefix, protected []netip.Prefix, state, requestID string) config.EdgeOneTransition {
	return config.EdgeOneTransition{
		ZoneID: zone, BaseVersion: base, PendingVersion: pending,
		PendingCIDRs: pendingCIDRs, ProtectedCIDRs: protected,
		ConfirmationState: state, ConfirmationRequestID: requestID,
		UpdatedAt: time.Now().UTC(),
	}
}

func TestStep11LegacyStep10EmptyProtectionIsBackfilledAndBlocks(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, nil,
		config.EdgeOneConfirmationAccepted, "req-old")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "", nil),
	}}
	st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, map[string]string{"home.example": b.String()})

	out := s.runScheduledRound(st, true, true)
	_, adds, deletes := p.counts()
	_, confirms := teo.counts()
	if out.Preflight.State != PreflightStateOverlap || adds != 0 || deletes != 0 || confirms != 0 {
		t.Fatalf("Step 10 旧行保护必须回填并阻断重叠：preflight=%+v adds=%d deletes=%d confirms=%d", out.Preflight, adds, deletes, confirms)
	}
	row, ok := store.row("zone-a")
	if !ok || len(row.ProtectedCIDRs) != 1 || row.ProtectedCIDRs[0] != b ||
		row.ConfirmationState != config.EdgeOneConfirmationAccepted || row.ConfirmationRequestID != "req-old" {
		t.Fatalf("旧行回填不得丢失确认状态：%+v ok=%v", row, ok)
	}
	if !p.hasRule(a.String(), "443") || !p.hasRule(b.String(), "443") {
		t.Fatalf("阻断时两侧规则必须保留：%+v", p.rules)
	}
}

func TestStep11RestartNextDisappearsStillBlocksProtectedPrefix(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b},
		config.EdgeOneConfirmationAccepted, "req-old")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "", nil),
	}}
	st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, map[string]string{"home.example": b.String()})

	out := s.runScheduledRound(st, true, true)
	_, adds, deletes := p.counts()
	_, confirms := teo.counts()
	if out.Preflight.State != PreflightStateOverlap || adds != 0 || deletes != 0 || confirms != 0 {
		t.Fatalf("Next 消失后 B 仍受保护：preflight=%+v adds=%d deletes=%d confirms=%d", out.Preflight, adds, deletes, confirms)
	}
	if row, ok := store.row("zone-a"); !ok || !sameEdgeOnePrefixSet(row.ProtectedCIDRs, []netip.Prefix{b}) {
		t.Fatalf("重启后保护记录 = %+v ok=%v", row, ok)
	}
	if !p.hasRule(b.String(), "443") {
		t.Fatal("受保护前缀规则不得被删除")
	}
}

func TestStep11ReleaseAllowsOnlyProvenStalePrefixCleanup(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b},
		config.EdgeOneConfirmationAccepted, "req-old")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v2", []string{b.String()}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	s.runScheduledRound(st, false, true)
	_, _, deletes := p.counts()
	if deletes != 1 || !p.hasRule(b.String(), "443") || p.hasRule(a.String(), "443") {
		t.Fatalf("B 成为当前后应只删除 A：deletes=%d rules=%+v", deletes, p.rules)
	}
	if row, ok := store.row("zone-a"); ok {
		t.Fatalf("释放完成后应删除过渡记录：%+v", row)
	}
}

func TestStep11ProtectedPrefixDefersCleanup(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b},
		config.EdgeOneConfirmationAccepted, "req-old")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	s.runScheduledRound(st, false, true)
	if _, _, deletes := p.counts(); deletes != 0 {
		t.Fatalf("受保护 B 不得删除，deletes=%d", deletes)
	}
	if !p.hasRule(b.String(), "443") {
		t.Fatal("受保护 B 必须保留")
	}
	summary := s.Status().LastRound
	if summary == nil || summary.CleanupDeferred == 0 {
		t.Fatalf("清理延后计数 = %+v", summary)
	}
	if row, ok := store.row("zone-a"); !ok || !sameEdgeOnePrefixSet(row.ProtectedCIDRs, []netip.Prefix{b}) {
		t.Fatalf("无释放证据时保护记录 = %+v ok=%v", row, ok)
	}
}

func TestStep11PendingReplacementKeepsOldProtection(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	c := netip.MustParsePrefix("192.0.2.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b},
		config.EdgeOneConfirmationAccepted, "req-old")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	snap := step11Snapshot("v1", []string{a.String()}, "v3", []string{c.String()})
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{snap, snap}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	s.runScheduledRound(st, false, true)
	row, ok := store.row("zone-a")
	if !ok || row.PendingVersion != "v3" || !sameEdgeOnePrefixSet(row.ProtectedCIDRs, []netip.Prefix{b, c}) {
		t.Fatalf("待更新替换必须保留旧保护并加入新保护：%+v ok=%v", row, ok)
	}
	if !p.hasRule(b.String(), "443") {
		t.Fatal("旧待更新 B 仍可能用于回源，不得删除")
	}
}

func TestStep11ForcedSwitchKeepsTrackedPendingProtection(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	c := netip.MustParsePrefix("192.0.2.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b},
		config.EdgeOneConfirmationUnknown, "")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v4", []string{c.String()}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	s.runScheduledRound(st, false, true)
	row, ok := store.row("zone-a")
	if !ok || row.BaseVersion != "v4" || row.PendingVersion != "v2" ||
		!sameEdgeOnePrefixSet(row.ProtectedCIDRs, []netip.Prefix{b}) {
		t.Fatalf("强制切换不得释放未证明的 B：%+v ok=%v", row, ok)
	}
	if !p.hasRule(b.String(), "443") {
		t.Fatal("强制切换到 C 后 B 仍受保护")
	}
}

func TestStep11StorageFailureUsesMemoryProtectionAndContinues(t *testing.T) {
	store := newStep10Store()
	store.saveProtectedErr = errors.New("fixture: transition write failed")
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"))
	p.createVisible = false // 让本轮不进入确认，单独观察保护持久化失败下的写入
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "v2", []string{b.String()}),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	s.runScheduledRound(st, false, true)
	_, creates, _ := p.counts()
	if creates != 1 {
		t.Fatalf("裁决 C：保护写入失败仍应允许 EdgeOne 新增，creates=%d", creates)
	}
	if _, ok := store.row("zone-a"); ok {
		t.Fatal("保存失败时不应声称过渡记录已落库")
	}
	status := s.EdgeOneRuntimeStatus()
	if len(status) != 1 || !strings.Contains(status[0].TransitionError, "写入过渡保护失败") {
		t.Fatalf("状态必须保留保护写入失败：%+v", status)
	}
}

func TestStep11TransitionReadFailureBlocksEdgeOneOnly(t *testing.T) {
	store := newStep10Store()
	store.listErr = errors.New("fixture: transition read failed")
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "v2", []string{b.String()}),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	out := s.runScheduledRound(st, false, true)
	_, creates, deletes := p.counts()
	_, confirms := teo.counts()
	if creates != 0 || deletes != 0 || confirms != 0 || out.Preflight.EdgeOneComplete {
		t.Fatalf("过渡读取失败必须阻断 EdgeOne：preflight=%+v creates=%d deletes=%d confirms=%d", out.Preflight, creates, deletes, confirms)
	}
	status := s.EdgeOneRuntimeStatus()
	if len(status) != 1 || status[0].TransitionError != "读取过渡保护失败" {
		t.Fatalf("状态必须显示读取失败：%+v", status)
	}
}

func TestStep11ReleaseWithLeftoverProtectionKeepsResidual(t *testing.T) {
	store := newStep10Store()
	b := netip.MustParsePrefix("198.51.100.0/24")
	c := netip.MustParsePrefix("192.0.2.0/24")
	// B 成为当前并释放；C 是更早替换留下、尚无释放证据的残留。
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b, c},
		config.EdgeOneConfirmationAccepted, "req-old")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(b.String(), "443"), step11Rule(c.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v2", []string{b.String()}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	s.runScheduledRound(st, false, true)
	row, ok := store.row("zone-a")
	if !ok || row.BaseVersion != "v2" || row.PendingVersion != "v2" ||
		!sameEdgeOnePrefixSet(row.ProtectedCIDRs, []netip.Prefix{c}) {
		t.Fatalf("释放 B 后必须保留 C 残保护：%+v ok=%v", row, ok)
	}
	if !p.hasRule(c.String(), "443") {
		t.Fatal("无释放证据的 C 规则不得删除")
	}
	if s.Status().Preflight.ProtectedCIDRs == nil {
		t.Fatal("预检保护数组不得为 nil")
	}
}

func TestStep11TransitionSQLiteRealStore(t *testing.T) {
	// 同一路径由 config 包的真实 SQLite 覆盖；本文件其余用例使用内存替身。
	// 这里只做纯逻辑对照，确保 base==pending 的残留保护不会被回退为 pending。
	row := &config.EdgeOneTransition{
		ZoneID: "zone-a", BaseVersion: "v2", PendingVersion: "v2",
		PendingCIDRs:   []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")},
		ProtectedCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
	}
	got := edgeOneTransitionProtected(row)
	if len(got) != 1 || got[0].String() != "192.0.2.0/24" {
		t.Fatalf("base==pending 时不得回退 pending 保护：%v", got)
	}
}

func TestStep11TransitionPlanLegacyBackfill(t *testing.T) {
	row := &config.EdgeOneTransition{
		ZoneID: "zone-a", BaseVersion: "v1", PendingVersion: "v2",
		PendingCIDRs: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")},
	}
	plan := edgeOneTransitionBackfill(row, time.Now())
	if !plan.changed || plan.next == nil || len(plan.protected) != 1 {
		t.Fatalf("旧行必须回填保护：%+v", plan)
	}
}

func TestStep11CleanupFailureConvergesOnNextSnapshot(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b},
		config.EdgeOneConfirmationAccepted, "req-old")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	p.deleteErrs = []error{errors.New("fixture: delete failed")}
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v2", []string{b.String()}, "", nil),
		step11Snapshot("v2", []string{b.String()}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	s.runScheduledRound(st, false, true)
	if !p.hasRule(a.String(), "443") {
		t.Fatal("删除失败时 A 必须保留")
	}
	if sum := s.Status().LastRound; sum == nil || sum.Outcome != RoundSuccess || sum.CleanupDeleted != 0 {
		t.Fatalf("删除失败应保留为延后 success：%+v", sum)
	}
	// 下一次从新快照收敛；过渡记录已在释放时删除，不依赖再次释放。
	s.runScheduledRound(st, false, true)
	if p.hasRule(a.String(), "443") {
		t.Fatal("第二次执行应删除 A")
	}
	if !p.hasRule(b.String(), "443") {
		t.Fatal("B 必须保留")
	}
}

func TestStep11S2LosesCoverageDropsProof(t *testing.T) {
	store := newStep10Store()
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{b}, []netip.Prefix{b},
		config.EdgeOneConfirmationAccepted, "req-old")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	p.afterDelete = func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, rule := range p.rules {
			if rule.CidrBlock == b.String() {
				p.rules = append(p.rules[:i], p.rules[i+1:]...)
				p.revision++
				return
			}
		}
	}
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v2", []string{b.String()}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)

	s.runScheduledRound(st, false, true)
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundFailed || sum.Deleted != 1 {
		t.Fatalf("S2 失去覆盖必须 failed 且保留已确认删除：%+v", sum)
	}
	if len(s.RoundCoverage()) != 0 {
		t.Fatalf("S2 失去覆盖后不得保留站点证明：%+v", s.RoundCoverage())
	}
}

func TestStep11RestartRestoresProtectionFromSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transition-step11.db")
	store, err := config.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	a := netip.MustParsePrefix("203.0.113.0/24")
	b := netip.MustParsePrefix("198.51.100.0/24")
	if err := store.SaveEdgeOneProtectedObservation(context.Background(), config.EdgeOneTransition{
		ZoneID: "zone-a", BaseVersion: "v1", PendingVersion: "v2",
		PendingCIDRs: []netip.Prefix{b}, ProtectedCIDRs: []netip.Prefix{b},
		ConfirmationState: config.EdgeOneConfirmationAccepted, ConfirmationRequestID: "req-sqlite",
		UpdatedAt: time.Now().UTC(),
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := config.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a.String(), "443"), step11Rule(b.String(), "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a.String()}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, reopened, nil)
	s.runScheduledRound(st, false, true)
	if _, _, deletes := p.counts(); deletes != 0 {
		t.Fatalf("重启后受保护 B 不得删除：%d", deletes)
	}
	row, err := reopened.GetEdgeOneTransition(context.Background(), "zone-a")
	if err != nil || row == nil || !sameEdgeOnePrefixSet(row.ProtectedCIDRs, []netip.Prefix{b}) ||
		row.ConfirmationState != config.EdgeOneConfirmationAccepted {
		t.Fatalf("SQLite 恢复的记录 = %+v err=%v", row, err)
	}
}
