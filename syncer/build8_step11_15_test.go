package syncer

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func TestBuild8RetainsProtectionWithoutConfirmation(t *testing.T) {
	const a = "203.0.113.0/24"
	const b = "198.51.100.0/24"
	store := newStep10Store()
	store.saveProtectedErr = errors.New("temporary protection store failure")
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a, "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a}, "v2", []string{b}),
		// Recheck sees Next disappear, so no confirmation upsert takes place.
		step11Snapshot("v1", []string{a}, "", nil),
		step11Snapshot("v1", []string{a}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)
	s.runScheduledRound(st, false, true)
	_, adds, firstDeletes := p.counts()
	_, confirms := teo.counts()
	if adds != 1 || firstDeletes != 0 || confirms != 0 || !p.hasRule(b, "443/443") {
		t.Fatalf("invalid fixture: adds=%d deletes=%d confirms=%d", adds, firstDeletes, confirms)
	}
	preview, err := s.DryRun()
	if err != nil || len(preview.ExecutionUnits) != 1 ||
		len(preview.ExecutionUnits[0].CleanupProtected) != 1 ||
		len(preview.Preflight.ProtectedCIDRs) != 1 || preview.Preflight.ProtectedCIDRs[0] != b {
		t.Errorf("preview lost uncommitted protection: %+v err=%v", preview, err)
	}
	if _, exists := store.row("zone-a"); exists {
		t.Fatal("preview wrote the transition row")
	}
	store.saveProtectedErr = nil
	s.runScheduledRound(st, false, true)
	row, exists := store.row("zone-a")
	if !exists || !sameEdgeOnePrefixSet(row.ProtectedCIDRs, []netip.Prefix{netip.MustParsePrefix(b)}) {
		t.Errorf("uncommitted protection was not retried: %+v", row)
	}
	_, _, deletes := p.counts()
	if deletes != 0 || !p.hasRule(b, "443/443") {
		t.Fatalf("same process lost pending protection without evidence of v2 becoming current: deletes=%d B-present=%v API=%+v", deletes, p.hasRule(b, "443/443"), s.Status().Preflight)
	}
}

func TestBuild8PendingDisappearanceIsNotActivation(t *testing.T) {
	const a = "203.0.113.0/24"
	const b = "198.51.100.0/24"
	store := newStep10Store()
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a}, "v2", []string{b}),
		step11Snapshot("v1", []string{a}, "v2", []string{b}),
		step11Snapshot("v1", []string{a}, "", nil),
	}}
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a, "443"))
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)
	s.runScheduledRound(st, false, true)
	row, ok := store.row("zone-a")
	if !ok || row.ConfirmationState != config.EdgeOneConfirmationAccepted {
		t.Fatalf("invalid fixture: row=%+v", row)
	}
	s.runScheduledRound(st, false, true)
	status := s.EdgeOneRuntimeStatus()[0]
	if status.ActualEffective {
		t.Fatalf("activation reported while tracked v2 is still not current: %+v", status)
	}
	if status.ConfirmationState != config.EdgeOneConfirmationAccepted || status.TransitionPendingVersion != "v2" {
		t.Fatalf("lost confirmation while waiting for activation: %+v", status)
	}
	teo.snapshots = []provider.OriginACLSnapshot{
		step11Snapshot("v2", []string{a}, "", nil),
		step11Snapshot("v2", []string{b}, "", nil),
	}
	s.runScheduledRound(st, false, true)
	if st := s.EdgeOneRuntimeStatus()[0]; st.ActualEffective {
		t.Fatalf("version match with a different prefix set reported active: %+v", st)
	}
	s.runScheduledRound(st, false, true)
	if st := s.EdgeOneRuntimeStatus()[0]; !st.ActualEffective {
		t.Fatalf("matching version and prefix set did not report active: %+v", st)
	}
}

func TestBuild8SharedPhysicalAliasPreview(t *testing.T) {
	targets := []config.TargetConfig{
		{ID: 0, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-alias"},
		{ID: 1, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-alias"},
	}
	p0 := newProbeProvider(config.CloudTCCVM, 0)
	p1 := newProbeProvider(config.CloudTCCVM, 1)
	teo := &step8FakeTEO{snapshot: step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil)}
	st := step9State(t, targets, []provider.Provider{p0, p1}, teo)
	st.Config.SetDNSFeatureEnabled(false)
	s := step11Syncer(t, st, teo, newStep10Store(), nil)
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	s.runScheduledRound(st, false, true)
	snapshots, adds, _ := p0.counts()
	if len(resp.ExecutionUnits) != 1 || resp.ExecutionUnits[0].PlannedAdd != 1 || snapshots != 3 || adds != 1 {
		t.Fatalf("invalid fixture: units=%+v snapshots=%d adds=%d", resp.ExecutionUnits, snapshots, adds)
	}
	if len(resp.Results) != 2 || len(resp.Results[0].EdgeOneSites) != 1 ||
		len(resp.Results[1].EdgeOneSites) != 0 ||
		resp.Results[1].ExecutionTargetKey != resp.ExecutionUnits[0].TargetKey {

		t.Fatalf("local binding or common execution key is incorrect: results=%+v", resp.Results)
	}
}

func TestBuild8SourceBlockStaysAfterClearOnDNSOnlyDue(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	snap := step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil)
	teo := &step8FakeTEO{snapshot: snap}
	st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
	dnsInput := map[string]string{"home.example": "203.0.113.7/32"}
	s := step11Syncer(t, st, teo, newStep10Store(), dnsInput)
	s.runScheduledRound(st, true, true)
	first := s.Status()
	if first.Preflight.State != PreflightStateOverlap || first.EdgeOne.LastOutcome != SourceOutcomeBlocked {
		t.Fatalf("invalid fixture before recovery: %+v", first)
	}
	dnsInput["home.example"] = "192.0.2.7/32"
	s.runScheduledRound(st, true, false)
	status := s.Status()
	if status.Preflight.State != PreflightStateClear || status.DNS.LastOutcome != SourceOutcomeSuccess ||
		status.EdgeOne.LastOutcome != SourceOutcomeBlocked ||
		!status.EdgeOne.LastCompletedAt.Equal(*first.EdgeOne.LastCompletedAt) {
		t.Fatalf("invalid fixture after recovery: %+v", status)
	}
}

func TestBuild8DryRunReadFailureDoesNotAuthorizeEitherSource(t *testing.T) {
	p := newProbeProvider(config.CloudAliECS, 0)
	teo := &step8FakeTEO{snapshot: step11Snapshot("v1", []string{"203.0.113.0/24"}, "", nil)}
	st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, newStep10Store(), map[string]string{"home.example": "192.0.2.10/32"})
	s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{getErr: errors.New("fixture: conflict record unavailable")}))
	resp, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	formal := s.runScheduledRound(st, true, true)
	if formal.PreflightErr == nil || formal.DNSRan || formal.EdgeRan {
		t.Fatalf("invalid formal fixture: %+v", formal)
	}
	if resp.Preflight.State != PreflightStateDataUnavailable || resp.Preflight.Error == "" ||
		!resp.Preflight.DNSComplete || !resp.Preflight.EdgeOneComplete || s.Status().Preflight.Error == "" {
		t.Fatalf("invalid preview fixture: %+v", resp.Preflight)
	}
}
func TestBuild8SQLiteReplacementPreservesLegacyPending(t *testing.T) {
	ctx := context.Background()
	const a = "203.0.113.0/24"
	const b = "198.51.100.0/24"
	const c = "192.0.2.0/24"
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "transition.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	prefixB := netip.MustParsePrefix(b)
	row := step11Row("zone-a", "v1", "v2", []netip.Prefix{prefixB}, nil, config.EdgeOneConfirmationAccepted, "old-accept")
	if err := store.SaveEdgeOneObservation(ctx, row); err != nil {
		t.Fatal(err)
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TRIGGER fixture_protected_save_failure BEFORE UPDATE ON edgeone_origin_transitions
      WHEN NEW.protected_cidrs_json != '[]' BEGIN SELECT RAISE(ABORT, 'fixture protection write failure'); END;`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	p := newProbeProvider(config.CloudAliECS, 0, step11Rule(a, "443"), step11Rule(b, "443"))
	teo := &step10TEO{snapshots: []provider.OriginACLSnapshot{
		step11Snapshot("v1", []string{a}, "v3", []string{c}),
		step11Snapshot("v1", []string{a}, "v3", []string{c}),
		step11Snapshot("v1", []string{a}, "", nil),
	}}
	st := step11State(t, false, p, []config.EdgeOneSite{step8Site("zone-a")})
	s := step11Syncer(t, st, teo, store, nil)
	first := s.runScheduledRound(st, false, true)
	_, confirmed := teo.counts()
	if confirmed != 0 {
		t.Fatalf("confirmation overwrote protection while writes failed: %d", confirmed)
	}
	if _, adds, deletes := p.counts(); adds != 1 || deletes != 0 || !first.EdgeRetry {
		t.Fatalf("protection write failure must allow safe adds and request a retry: adds=%d deletes=%d round=%+v", adds, deletes, first)
	}
	persisted, err := store.GetEdgeOneTransition(ctx, "zone-a")
	if err != nil || persisted == nil || persisted.PendingVersion != "v2" || persisted.ConfirmationRequestID != "old-accept" {
		t.Fatalf("old evidence was modified: row=%+v err=%v", persisted, err)
	}
	s.runScheduledRound(st, false, true)
	_, _, deletes := p.counts()
	if deletes != 0 || !p.hasRule(b, "443") {
		t.Fatalf("real SQLite: B deleted without activation proof: row=%+v deletes=%d", persisted, deletes)
	}
	tx, err = store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "DROP TRIGGER fixture_protected_save_failure"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	replacement := step11Snapshot("v1", []string{a}, "v3", []string{c})
	teo.snapshots = []provider.OriginACLSnapshot{replacement, replacement}
	s.runScheduledRound(st, false, true)
	persisted, err = store.GetEdgeOneTransition(ctx, "zone-a")
	_, confirmed = teo.counts()
	if err != nil || persisted == nil || persisted.PendingVersion != "v3" ||
		!sameEdgeOnePrefixSet(persisted.ProtectedCIDRs, []netip.Prefix{prefixB, netip.MustParsePrefix(c)}) ||
		persisted.ConfirmationState != config.EdgeOneConfirmationAccepted || confirmed != 1 {
		t.Fatalf("recovery failed: row=%+v confirms=%d err=%v", persisted, confirmed, err)
	}
}

func TestBuild8ExpiredCacheCannotReleasePendingProtection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const b = "198.51.100.0/24"
		prefixB := netip.MustParsePrefix(b)
		store := newStep10Store()
		store.rows["zone-a"] = step11Row("zone-a", "v1", "v2", []netip.Prefix{prefixB}, []netip.Prefix{prefixB}, config.EdgeOneConfirmationAccepted, "accepted")
		p := newProbeProvider(config.CloudAliECS, 0, step11Rule(b, "443"))
		teo := &step8FakeTEO{snapshot: step11Snapshot("v2", []string{b}, "", nil)}
		st := step11State(t, true, p, []config.EdgeOneSite{step8Site("zone-a")})
		site := st.Config.EdgeOneSites[0]
		s := step11Syncer(t, st, teo, store, map[string]string{"home.example": "198.51.100.7/32"})
		s.storeEdgeOneCacheLocked(site, teo.snapshot, time.Now().Add(-st.Config.EdgeOneInterval+time.Second), st)
		originalResolve := s.resolveHostFn
		s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
			time.Sleep(2 * time.Second)
			return originalResolve(host)
		}
		round := s.runScheduledRound(st, true, false)
		_, exists := store.row("zone-a")
		if round.Preflight.EdgeOneComplete {
			t.Fatalf("fixture unexpectedly sees complete EdgeOne input: %+v", round.Preflight)
		}
		if !exists || round.DNSRan || round.Preflight.State != PreflightStateOverlap {
			t.Fatalf("expired snapshot released prior B protection: row-present=%t dns-ran=%t preflight=%+v", exists, round.DNSRan, round.Preflight)
		}
	})
}
