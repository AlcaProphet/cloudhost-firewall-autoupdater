package syncer

import (
	"errors"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func fixState(t *testing.T, ct config.CloudType, dnsEnabled, edgeEnabled bool, p provider.Provider, rules []config.DomainRule, sites []config.EdgeOneSite, teo provider.OriginACLClient) *RuntimeState {
	t.Helper()
	targets := []config.TargetConfig{{ID: 0, CloudType: ct, Region: "ap-guangzhou", ResourceID: "sg-fix"}}
	rc := config.RuntimeConfig{
		Targets: targets, Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8",
		DNSTimeout: time.Second, DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		DomainRules: rules, EdgeOneEnabled: edgeEnabled, EdgeOneTag: "edgeone", EdgeOneInterval: time.Hour,
		EdgeOneSites: sites,
	}
	rc.SetDNSFeatureEnabled(dnsEnabled)
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	st.Providers = nil
	if p != nil {
		st.Providers = []provider.Provider{p}
	}
	st.TEO = teo
	return st
}

func fixRule(id int, host string) config.DomainRule {
	return config.DomainRule{ID: id, Host: host, Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0}}
}

func fixSite(family string) config.EdgeOneSite {
	return config.EdgeOneSite{ZoneID: "zone-a", TargetIDs: []int{0}, Protocol: "TCP", Ports: "443", AddressFamily: family}
}

func fixSnapshot(zone, prefix string, withNext bool) provider.OriginACLSnapshot {
	pr, err := netip.ParsePrefix(prefix)
	if err != nil {
		panic(err)
	}
	current := provider.OriginACLVersion{Version: "v1", Prefixes: []netip.Prefix{pr.Masked()}}
	snap := provider.OriginACLSnapshot{ZoneID: zone, Status: "online", Current: current, RequestID: "req-fix"}
	if withNext {
		next := current
		next.Version = "v2"
		snap.Next = &next
	}
	return snap
}

func TestStep8BlockedRoundClearsRoundStartedAndSchedulesRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		teo := &step8FakeTEO{err: errors.New("fixture edgeone failure")}
		st := fixState(t, config.CloudTCCVM, false, true, nil, nil, []config.EdgeOneSite{fixSite("ipv4")}, teo)
		s := New(NewRuntimeManager(st))
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()

		status := s.Status()
		if status.RoundStartedAt != nil {
			t.Fatalf("被阻断轮次必须清空 round_started_at: %+v", status.RoundStartedAt)
		}
		if status.EdgeOne.NextDueAt == nil {
			t.Fatal("EdgeOne 应保留退避后的 next_due_at")
		}
		retryDelay := status.EdgeOne.NextDueAt.Sub(status.ProcessStartedAt)
		if retryDelay < 50*time.Second || retryDelay > 70*time.Second {
			t.Fatalf("启动失败后的 EdgeOne 重试应约为 1m，实际 %v", retryDelay)
		}
		if describes, confirms := teo.counts(); describes != 1 || confirms != 0 {
			t.Fatalf("启动失败调用 = %d/%d，期望 1/0", describes, confirms)
		}

		// 第二次失败应退避到 2m，而不是继续保持 1m 或直接等到 72h。
		synctest.Sleep(time.Minute)
		synctest.Wait()
		next := s.Status().EdgeOne.NextDueAt
		if next == nil {
			t.Fatal("第二次失败后 next_due_at 不得为空")
		}
		if d := next.Sub(time.Now()); d < 90*time.Second || d > 3*time.Minute {
			t.Fatalf("第二次失败的 EdgeOne 退避应约为 2m，实际 %v", d)
		}
		if s.Status().RoundStartedAt != nil {
			t.Fatalf("第二次阻断轮次仍遗留 round_started_at")
		}
	})
}

func TestStep8ResumeSuccessClearsEdgeOneBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		teo := &step8FakeTEO{err: errors.New("fixture edgeone failure")}
		st := fixState(t, config.CloudTCCVM, false, true, nil, nil, []config.EdgeOneSite{fixSite("ipv4")}, teo)
		s := New(NewRuntimeManager(st))
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		if s.Status().EdgeOne.NextDueAt == nil {
			t.Fatal("启动失败后应有退避到期时间")
		}

		s.Pause()
		synctest.Wait()
		teo.mu.Lock()
		teo.err = nil
		teo.snapshot = fixSnapshot("zone-a", "203.0.113.0/32", false)
		teo.mu.Unlock()
		s.Resume()
		synctest.Wait()

		next := s.Status().EdgeOne.NextDueAt
		if next == nil {
			t.Fatal("恢复成功后 next_due_at 不得为空")
		}
		if d := time.Until(*next); d < 30*time.Minute {
			t.Fatalf("恢复成功应重置退避并回到正常间隔，实际剩余 %v", d)
		}
		if s.Status().RoundStartedAt != nil {
			t.Fatal("恢复轮结束后不得遗留 round_started_at")
		}
	})
}

func TestStep8ManualTriggerForcesEdgeOneQuery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		teo := &step8FakeTEO{snapshot: fixSnapshot("zone-a", "203.0.113.0/32", false)}
		st := fixState(t, config.CloudTCCVM, false, true, nil, nil, []config.EdgeOneSite{fixSite("ipv4")}, teo)
		s := New(NewRuntimeManager(st))
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		if describes, _ := teo.counts(); describes != 1 {
			t.Fatalf("启动查询次数 = %d，期望 1", describes)
		}
		s.TriggerSync()
		synctest.Wait()
		if describes, _ := teo.counts(); describes != 2 {
			t.Fatalf("手动触发必须实际查询 EdgeOne，describe=%d，期望 2", describes)
		}
	})
}

func TestEdgeOneCacheHitPreservesQueryMetadata(t *testing.T) {
	planned := time.Now().Add(time.Hour)
	observed := time.Now().Add(-time.Minute).UTC()
	teo := &step8FakeTEO{}
	st := fixState(t, config.CloudTCCVM, true, true, nil, nil, []config.EdgeOneSite{fixSite("ipv4")}, teo)
	s := New(NewRuntimeManager(st))
	snap := fixSnapshot("zone-a", "203.0.113.0/32", true)
	snap.PlannedActiveAt = &planned
	snap.ObservedAt = observed
	s.mu.Lock()
	s.edgeCache = map[string]edgeOneCacheEntry{
		"zone-a": {
			snapshot:      snap,
			siteKey:       edgeOneSiteKey(st.Config.EdgeOneSites[0]),
			credential:    s.credentialFingerprint(st),
			observedAt:    observed,
			plannedActive: &planned,
		},
	}
	s.mu.Unlock()

	out := s.prepareEdgeOneData(st, s.Generation(), false)
	if len(out) != 1 || out["zone-a"].unavailable != "" {
		t.Fatalf("缓存命中准备结果 = %+v", out)
	}
	statuses := s.EdgeOneRuntimeStatus()
	if len(statuses) != 1 {
		t.Fatalf("站点状态 = %+v", statuses)
	}
	got := statuses[0]
	if got.PlannedActiveAt == nil || !got.PlannedActiveAt.Equal(planned) {
		t.Fatalf("缓存命中丢失 planned_active_at: %+v", got.PlannedActiveAt)
	}
	if got.LastQueryAt == nil || !got.LastQueryAt.Equal(observed) {
		t.Fatalf("缓存命中应保留实际查询时间，got=%+v want=%v", got.LastQueryAt, observed)
	}
}

func TestStep8SiteRemovalClearsEdgeOneRuntime(t *testing.T) {
	teo := &step8FakeTEO{}
	st := fixState(t, config.CloudTCCVM, true, true, nil, nil, []config.EdgeOneSite{fixSite("ipv4")}, teo)
	s := New(NewRuntimeManager(st))
	s.mu.Lock()
	s.edgeCache = map[string]edgeOneCacheEntry{"zone-a": {snapshot: fixSnapshot("zone-a", "203.0.113.0/32", true)}}
	s.edgeStatus = map[string]EdgeOneSiteStatus{"zone-a": {ZoneID: "zone-a"}}
	s.confirmedMem = map[string][32]byte{"zone-a": {1}}
	s.roundCoverage = map[string]SiteCoverage{"zone-a\x00x": {ZoneID: "zone-a"}}
	s.mu.Unlock()

	next := fixState(t, config.CloudTCCVM, true, true, nil, nil, nil, teo)
	s.ApplyState(next)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.edgeCache) != 0 || len(s.edgeStatus) != 0 || len(s.confirmedMem) != 0 || len(s.roundCoverage) != 0 {
		t.Fatalf("删除站点后仍保留 EdgeOne 运行态: cache=%d status=%d confirmed=%d coverage=%d",
			len(s.edgeCache), len(s.edgeStatus), len(s.confirmedMem), len(s.roundCoverage))
	}
}

func TestStep8StaleProofNotStored(t *testing.T) {
	teo := &step8FakeTEO{}
	old := fixState(t, config.CloudTCCVM, true, true, nil, nil, []config.EdgeOneSite{fixSite("ipv4")}, teo)
	s := New(NewRuntimeManager(old))
	epoch := s.Generation()
	oldProof := SiteCoverage{ZoneID: "zone-a", TargetKey: "target", ConfigEpoch: epoch, Complete: true}
	next := fixState(t, config.CloudTCCVM, true, true, nil, nil, nil, teo)
	s.ApplyState(next)
	s.storeSiteProofsForState(old, epoch, map[string]SiteCoverage{"zone-a": oldProof})
	if len(s.RoundCoverage()) != 0 {
		t.Fatalf("旧状态/旧代次证明被写入: %+v", s.RoundCoverage())
	}
}

func TestStep8StaleQueryResultNotCached(t *testing.T) {
	teo := &step8FakeTEO{}
	old := fixState(t, config.CloudTCCVM, true, true, nil, nil, []config.EdgeOneSite{fixSite("ipv4")}, teo)
	s := New(NewRuntimeManager(old))
	epoch := s.Generation()
	next := fixState(t, config.CloudTCCVM, true, true, nil, nil, nil, teo)
	s.ApplyState(next)
	snap := fixSnapshot("zone-a", "203.0.113.0/32", true)
	s.applyEdgeOneQueryResult(old, epoch, old.Config.EdgeOneSites[0], snap, "", time.Now(), nil, true, nil)
	if len(s.EdgeOneRuntimeStatus()) != 0 {
		t.Fatalf("旧状态查询结果被写入状态: %+v", s.EdgeOneRuntimeStatus())
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.edgeCache) != 0 {
		t.Fatalf("旧状态查询结果被写入缓存: %+v", s.edgeCache)
	}
}

func TestStep8SingleSourceStatusDropsOldDualPreflight(t *testing.T) {
	teo := &step8FakeTEO{}
	old := fixState(t, config.CloudTCCVM, true, true, nil, nil, []config.EdgeOneSite{fixSite("ipv4")}, teo)
	s := New(NewRuntimeManager(old))
	s.mu.Lock()
	s.preflightSet = true
	s.preflight = PreflightOutcome{State: PreflightStateClear, DNSComplete: true, EdgeOneComplete: true}
	s.mu.Unlock()

	next := fixState(t, config.CloudTCCVM, true, false, nil, nil, nil, teo)
	s.ApplyState(next)
	status := s.Status()
	if status.Preflight.State != PreflightStateSingleSource {
		t.Fatalf("单源状态必须为 single_source，实际 %+v", status.Preflight)
	}
	if status.Preflight.DNSComplete || status.Preflight.EdgeOneComplete {
		t.Fatalf("单源状态不得保留旧双开完整性: %+v", status.Preflight)
	}
}
