package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

type step10Store struct {
	mu           sync.Mutex
	rows         map[string]config.EdgeOneTransition
	failUnknown  bool
	failAccepted bool
}

func newStep10Store() *step10Store {
	return &step10Store{rows: make(map[string]config.EdgeOneTransition)}
}

func edgePendingJSON(prefixes []netip.Prefix) string {
	values := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		values = append(values, p.Masked().String())
	}
	sort.Strings(values)
	raw, _ := json.Marshal(values)
	return string(raw)
}

func (s *step10Store) GetEdgeOneTransition(_ context.Context, zone string) (*config.EdgeOneTransition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[zone]
	if !ok {
		return nil, nil
	}
	cp := row
	cp.PendingCIDRs = append([]netip.Prefix(nil), row.PendingCIDRs...)
	return &cp, nil
}

func (s *step10Store) SaveEdgeOneObservation(_ context.Context, t config.EdgeOneTransition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.rows[t.ZoneID]
	state := config.EdgeOneConfirmationNotAttempted
	requestID := ""
	if ok && old.PendingVersion == t.PendingVersion && edgePendingJSON(old.PendingCIDRs) == edgePendingJSON(t.PendingCIDRs) {
		if old.ConfirmationState == config.EdgeOneConfirmationAccepted || old.ConfirmationState == config.EdgeOneConfirmationUnknown {
			state, requestID = old.ConfirmationState, old.ConfirmationRequestID
		}
	}
	t.ConfirmationState = state
	t.ConfirmationRequestID = requestID
	s.rows[t.ZoneID] = t
	return nil
}

func (s *step10Store) SetEdgeOneConfirmation(_ context.Context, zone, pendingVersion string, pendingCIDRs []netip.Prefix, state, requestID string, updated time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[zone]
	if !ok || row.PendingVersion != pendingVersion || edgePendingJSON(row.PendingCIDRs) != edgePendingJSON(pendingCIDRs) {
		return false, nil
	}
	if state == config.EdgeOneConfirmationUnknown && s.failUnknown {
		return false, errors.New("fixture unknown save failure")
	}
	if state == config.EdgeOneConfirmationAccepted && s.failAccepted {
		return false, errors.New("fixture accepted save failure")
	}
	row.ConfirmationState = state
	row.ConfirmationRequestID = requestID
	row.UpdatedAt = updated
	s.rows[zone] = row
	return true, nil
}

func (s *step10Store) row(zone string) (config.EdgeOneTransition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[zone]
	return row, ok
}

func (s *step10Store) RestoreUnsentEdgeOneConfirmation(_ context.Context, zone, pendingVersion string, pendingCIDRs []netip.Prefix, preparedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[zone]
	if !ok || row.PendingVersion != pendingVersion || edgePendingJSON(row.PendingCIDRs) != edgePendingJSON(pendingCIDRs) ||
		row.ConfirmationState != config.EdgeOneConfirmationUnknown || !row.UpdatedAt.Equal(preparedAt) {
		return false, nil
	}
	row.ConfirmationState = config.EdgeOneConfirmationNotAttempted
	row.ConfirmationRequestID = ""
	s.rows[zone] = row
	return true, nil
}

type step10TEO struct {
	mu         sync.Mutex
	describes  int
	confirms   int
	snapshots  []provider.OriginACLSnapshot
	describeFn func() (provider.OriginACLSnapshot, error)
	confirmErr error
}

func (f *step10TEO) DescribeOriginACL(context.Context, string) (provider.OriginACLSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.describes++
	if f.describeFn != nil {
		return f.describeFn()
	}
	if len(f.snapshots) > 0 {
		snap := f.snapshots[0]
		f.snapshots = f.snapshots[1:]
		return snap, nil
	}
	return step10Snapshot(), nil
}
func (f *step10TEO) ConfirmOriginACLUpdate(_ context.Context, _ string, admit func() bool) (string, error) {
	if !admit() {
		return "", &provider.TEOError{Kind: provider.TEOErrorCanceled, NotSent: true}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirms++
	if f.confirmErr != nil {
		return "", f.confirmErr
	}
	return "req-confirm", nil
}
func (f *step10TEO) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.describes, f.confirms
}

func step10Snapshot() provider.OriginACLSnapshot {
	prefix := netip.MustParsePrefix("203.0.113.0/24")
	return provider.OriginACLSnapshot{
		ZoneID:    "zone-a",
		Status:    "online",
		Current:   provider.OriginACLVersion{Version: "v1", Prefixes: []netip.Prefix{prefix}},
		Next:      &provider.OriginACLVersion{Version: "v2", Prefixes: []netip.Prefix{prefix}},
		RequestID: "req-describe",
	}
}

func step10State(t *testing.T, p provider.Provider, teo provider.OriginACLClient) *RuntimeState {
	t.Helper()
	target := config.TargetConfig{ID: 0, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-step10"}
	rc := config.RuntimeConfig{
		Targets:          []config.TargetConfig{target},
		Tag:              "auto-dns",
		Interval:         time.Hour,
		DNS:              "8.8.8.8",
		DNSTimeout:       time.Second,
		DNSFailThreshold: 5,
		LogLevel:         "info",
		SyncEnabled:      true,
		Theme:            "light",
		EdgeOneEnabled:   true,
		EdgeOneTag:       "edgeone",
		EdgeOneInterval:  time.Hour,
		EdgeOneSites: []config.EdgeOneSite{{
			ZoneID: "zone-a", TargetIDs: []int{0}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		}},
	}
	rc.SetDNSFeatureEnabled(false)
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	st.Providers = []provider.Provider{p}
	st.TEO = teo
	return st
}

func step10Proofs(t *testing.T, s *Syncer, state *RuntimeState) map[string]SiteCoverage {
	t.Helper()
	snap := step10Snapshot()
	site := state.Config.EdgeOneSites[0]
	proof := SiteCoverage{
		ZoneID:         site.ZoneID,
		TargetKey:      config.TargetIdentity(state.Config.Targets[0]),
		Current:        snap.Current.Version,
		Pending:        snap.Next.Version,
		SnapshotDigest: snapshotDigest(snap),
		ConfigEpoch:    s.Generation(),
		Complete:       true,
	}
	return map[string]SiteCoverage{site.ZoneID: proof}
}

func TestStep10HappyPathAndCrossRoundDedupe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &step10TEO{}
		st := step10State(t, p, teo)
		store := newStep10Store()
		s := New(NewRuntimeManager(st))
		s.SetEdgeOneTransitionStore(store)
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		if _, confirms := teo.counts(); confirms != 1 {
			t.Fatalf("首次确认次数 = %d，期望 1", confirms)
		}
		row, ok := store.row("zone-a")
		if !ok || row.ConfirmationState != config.EdgeOneConfirmationAccepted || row.ConfirmationRequestID != "req-confirm" {
			t.Fatalf("确认状态 = %+v ok=%v", row, ok)
		}
		// 同一指纹下一轮仍出现：不得重发。
		synctest.Sleep(time.Hour)
		synctest.Wait()
		if _, confirms := teo.counts(); confirms != 1 {
			t.Fatalf("同指纹跨轮确认次数 = %d，期望 1", confirms)
		}
	})
}

func TestStep10UnknownSaveFailureSendsZero(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	teo := &step10TEO{}
	st := step10State(t, p, teo)
	store := newStep10Store()
	store.failUnknown = true
	s := New(NewRuntimeManager(st))
	s.SetEdgeOneTransitionStore(store)
	s.storeSiteProofs(step10Proofs(t, s, st))
	retry := s.ConfirmEdgeOneSites(st, map[string]edgeOneSiteData{"zone-a": {site: st.Config.EdgeOneSites[0], snapshot: step10Snapshot()}}, s.Generation())
	if !retry {
		t.Fatal("unknown 提交失败应进入退避复查")
	}
	if _, confirms := teo.counts(); confirms != 0 {
		t.Fatalf("unknown 提交失败后确认次数 = %d，期望 0", confirms)
	}
}

func TestStep10IncompleteCoverageSendsZero(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	teo := &step10TEO{}
	st := step10State(t, p, teo)
	store := newStep10Store()
	s := New(NewRuntimeManager(st))
	s.SetEdgeOneTransitionStore(store)
	// 不注入 RoundCoverage：任一目标缺少回读证明时确认必须为零。
	retry := s.ConfirmEdgeOneSites(st, map[string]edgeOneSiteData{"zone-a": {site: st.Config.EdgeOneSites[0], snapshot: step10Snapshot()}}, s.Generation())
	if retry || func() int { _, c := teo.counts(); return c }() != 0 {
		t.Fatalf("缺覆盖时确认次数/重试 = %d/%v，期望 0/false", func() int { _, c := teo.counts(); return c }(), retry)
	}
}

func TestStep10AcceptedSaveFailureDoesNotResend(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	teo := &step10TEO{}
	st := step10State(t, p, teo)
	store := newStep10Store()
	store.failAccepted = true
	s := New(NewRuntimeManager(st))
	s.SetEdgeOneTransitionStore(store)
	s.storeSiteProofs(step10Proofs(t, s, st))
	edgeData := map[string]edgeOneSiteData{"zone-a": {site: st.Config.EdgeOneSites[0], snapshot: step10Snapshot()}}
	retry := s.ConfirmEdgeOneSites(st, edgeData, s.Generation())
	if !retry {
		t.Fatal("accepted 保存失败应进入退避")
	}
	if _, confirms := teo.counts(); confirms != 1 {
		t.Fatalf("确认次数 = %d，期望 1", confirms)
	}
	// 表中是 unknown；同指纹再次调用只能读，不得重发。
	s.ConfirmEdgeOneSites(st, edgeData, s.Generation())
	if _, confirms := teo.counts(); confirms != 1 {
		t.Fatalf("accepted 保存失败后重发，确认次数 = %d", confirms)
	}
}

func TestStep10TimeoutOnlyRechecks(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	teo := &step10TEO{confirmErr: &provider.TEOError{Kind: provider.TEOErrorTimeout, ZoneID: "zone-a", Message: "超时"}}
	st := step10State(t, p, teo)
	store := newStep10Store()
	s := New(NewRuntimeManager(st))
	s.SetEdgeOneTransitionStore(store)
	s.storeSiteProofs(step10Proofs(t, s, st))
	edgeData := map[string]edgeOneSiteData{"zone-a": {site: st.Config.EdgeOneSites[0], snapshot: step10Snapshot()}}
	s.ConfirmEdgeOneSites(st, edgeData, s.Generation())
	if describes, confirms := teo.counts(); describes != 1 || confirms != 1 {
		t.Fatalf("超时后调用 = %d describe / %d confirm，期望 1/1", describes, confirms)
	}
	row, _ := store.row("zone-a")
	if row.ConfirmationState != config.EdgeOneConfirmationUnknown {
		t.Fatalf("超时后状态 = %s，期望 unknown", row.ConfirmationState)
	}
	statuses := s.EdgeOneRuntimeStatus()
	if len(statuses) != 1 || statuses[0].LastConfirmationErr == "" {
		t.Fatalf("超时后必须留下确认诊断原因: %+v", statuses)
	}
	s.ConfirmEdgeOneSites(st, edgeData, s.Generation())
	if describes, confirms := teo.counts(); describes != 2 || confirms != 1 {
		t.Fatalf("unknown 同指纹应只增加 Describe，实际 describe=%d confirm=%d", describes, confirms)
	}
	if got := s.EdgeOneRuntimeStatus()[0].LastConfirmationErr; got != statuses[0].LastConfirmationErr {
		t.Fatalf("同指纹复查应保留诊断，之前=%q 现在=%q", statuses[0].LastConfirmationErr, got)
	}

	next := step10Snapshot()
	next.Next.Prefixes = []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}
	if err := store.SaveEdgeOneObservation(context.Background(), config.EdgeOneTransition{
		ZoneID: next.ZoneID, PendingVersion: next.Next.Version, PendingCIDRs: next.Next.Prefixes,
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.SetEdgeOneConfirmation(context.Background(), next.ZoneID, next.Next.Version, next.Next.Prefixes,
		config.EdgeOneConfirmationUnknown, "", time.Now()); err != nil || !ok {
		t.Fatalf("预置同版本新网段的 unknown 失败：%v %v", ok, err)
	}
	teo.snapshots = []provider.OriginACLSnapshot{next, next}
	edgeData[next.ZoneID] = edgeOneSiteData{site: st.Config.EdgeOneSites[0], snapshot: next}
	restarted := New(NewRuntimeManager(st))
	restarted.SetEdgeOneTransitionStore(store)
	for _, engine := range []*Syncer{s, restarted} {
		if !engine.ConfirmEdgeOneSites(st, edgeData, engine.Generation()) {
			t.Fatal("结果仍未知时应安排只读复查")
		}
		if got := engine.EdgeOneRuntimeStatus()[0].LastConfirmationErr; got != EdgeOneUnknownConfirmationDiagnostic {
			t.Fatalf("新指纹或重启后应使用固定诊断，got=%q", got)
		}
	}
	if describes, confirms := teo.counts(); describes != 4 || confirms != 1 {
		t.Fatalf("新指纹已有 unknown 时只读复查，describe=%d confirm=%d", describes, confirms)
	}
}

func TestStep10DNSFailureStillConfirmsCompleteEdgeOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &step10TEO{}
		target := config.TargetConfig{ID: 0, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-step10"}
		rc := config.RuntimeConfig{
			Targets: []config.TargetConfig{target}, Tag: "auto-dns", Interval: time.Hour,
			DNS: "8.8.8.8", DNSTimeout: time.Second, DNSFailThreshold: 5, LogLevel: "info",
			SyncEnabled: true, Theme: "light",
			DomainRules:    []config.DomainRule{{ID: 1, Host: "home.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}}},
			EdgeOneEnabled: true, EdgeOneTag: "edgeone", EdgeOneInterval: time.Hour,
			EdgeOneSites: []config.EdgeOneSite{{ZoneID: "zone-a", TargetIDs: []int{0}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"}},
		}
		rc.SetDNSFeatureEnabled(true)
		st, err := BuildRuntimeState(nil, rc, BreakerReset)
		if err != nil {
			t.Fatalf("构造运行时状态失败: %v", err)
		}
		st.Providers = []provider.Provider{p}
		st.TEO = teo
		store := newStep10Store()
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
		s.SetEdgeOneTransitionStore(store)
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) { return nil, errors.New("fixture dns failure") }
		s.backoffFn = func(int) time.Duration { return 0 }
		s.sleepFn = func(time.Duration) {}
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		if _, confirms := teo.counts(); confirms != 1 {
			t.Fatalf("DNS 失败但 EdgeOne 完整时确认次数 = %d，期望 1", confirms)
		}
		for _, desc := range p.createdSince(0) {
			if strings.HasPrefix(desc, "[auto-dns]") {
				t.Fatalf("DNS 失败时仍创建了 DNS TAG 动作: %q", desc)
			}
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, rule := range p.rules {
			if rule.CidrBlock == "192.0.2.10/32" {
				t.Fatalf("DNS 失败时不应写入 DNS 规则: %+v", rule)
			}
		}
	})
}

func TestStep10LatestVersionNowDoesNotMarkAccepted(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	next := step10Snapshot()
	noNext := next
	noNext.Next = nil
	teo := &step10TEO{
		snapshots:  []provider.OriginACLSnapshot{next, noNext},
		confirmErr: &provider.TEOError{Kind: provider.TEOErrorAlreadyLatest, ZoneID: "zone-a", Code: "OperationDenied.LatestVersionNow"},
	}
	st := step10State(t, p, teo)
	store := newStep10Store()
	s := New(NewRuntimeManager(st))
	s.SetEdgeOneTransitionStore(store)
	s.storeSiteProofs(step10Proofs(t, s, st))
	edgeData := map[string]edgeOneSiteData{"zone-a": {site: st.Config.EdgeOneSites[0], snapshot: next}}
	retry := s.ConfirmEdgeOneSites(st, edgeData, s.Generation())
	if retry {
		t.Fatal("明确已是最新后不应进入退避重试")
	}
	if _, confirms := teo.counts(); confirms != 1 {
		t.Fatalf("确认次数 = %d，期望 1", confirms)
	}
	row, _ := store.row("zone-a")
	if row.ConfirmationState == config.EdgeOneConfirmationAccepted {
		t.Fatalf("LatestVersionNow 不得伪造 accepted: %+v", row)
	}
	statuses := s.EdgeOneRuntimeStatus()
	if len(statuses) != 1 || !statuses[0].ActualEffective {
		t.Fatalf("LatestVersionNow 后应更新实际生效状态: %+v", statuses)
	}
	if statuses[0].LastConfirmationErr == "" {
		t.Fatalf("LatestVersionNow 后应有诊断原因: %+v", statuses[0])
	}
}

func TestStep10RecheckPrefixChangeSendsZero(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	original := step10Snapshot()
	changed := original
	changed.Current = provider.OriginACLVersion{
		Version:  "v1",
		Prefixes: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")},
	}
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{changed}}
	st := step10State(t, p, teo)
	store := newStep10Store()
	s := New(NewRuntimeManager(st))
	s.SetEdgeOneTransitionStore(store)
	s.storeSiteProofs(step10Proofs(t, s, st))
	edgeData := map[string]edgeOneSiteData{"zone-a": {site: st.Config.EdgeOneSites[0], snapshot: original}}
	retry := s.ConfirmEdgeOneSites(st, edgeData, s.Generation())
	if !retry {
		t.Fatal("确认前重查不一致应进入退避")
	}
	if _, confirms := teo.counts(); confirms != 0 {
		t.Fatalf("同版本网段变化后的确认次数 = %d，期望 0", confirms)
	}
}

func TestStep10AcceptedFingerprintSurvivesRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newStep10Store()
		teo := &step10TEO{}
		p := newProbeProvider(config.CloudTCCVM, 0)
		st := step10State(t, p, teo)
		s := New(NewRuntimeManager(st))
		s.SetEdgeOneTransitionStore(store)
		go s.Run()
		synctest.Wait()
		s.Stop()
		s.Wait()
		if _, confirms := teo.counts(); confirms != 1 {
			t.Fatalf("首次确认次数 = %d", confirms)
		}

		// 重启：新的 Syncer/Provider/TEO，确认表保留同一指纹。
		p2 := newProbeProvider(config.CloudTCCVM, 0)
		teo2 := &step10TEO{}
		st2 := step10State(t, p2, teo2)
		s2 := New(NewRuntimeManager(st2))
		s2.SetEdgeOneTransitionStore(store)
		go s2.Run()
		synctest.Wait()
		s2.Stop()
		s2.Wait()
		if _, confirms := teo2.counts(); confirms != 0 {
			t.Fatalf("重启后同指纹确认次数 = %d，期望 0", confirms)
		}
	})
}
