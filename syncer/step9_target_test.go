package syncer

import (
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func step9State(t *testing.T, targets []config.TargetConfig, providers []provider.Provider, teo provider.OriginACLClient) *RuntimeState {
	t.Helper()
	rc := config.RuntimeConfig{
		Targets:          targets,
		Tag:              "auto-dns",
		Interval:         time.Hour,
		DNS:              "8.8.8.8",
		DNSTimeout:       time.Second,
		DNSFailThreshold: 5,
		LogLevel:         "info",
		SyncEnabled:      true,
		Theme:            "light",
		DomainRules: []config.DomainRule{{
			ID: 1, Host: "home.example", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{0},
		}},
		EdgeOneEnabled:  true,
		EdgeOneTag:      "edgeone",
		EdgeOneInterval: time.Hour,
		EdgeOneSites: []config.EdgeOneSite{{
			ZoneID: "zone-a", TargetIDs: []int{0}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4",
		}},
	}
	rc.SetDNSFeatureEnabled(true)
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	st.Providers = providers
	st.TEO = teo
	return st
}

func TestStep9AliasesShareOneExecutionUnit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		targets := []config.TargetConfig{
			{ID: 0, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-alias"},
			{ID: 1, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-alias"},
		}
		p0 := newProbeProvider(config.CloudTCCVM, 0)
		p1 := newProbeProvider(config.CloudTCCVM, 1)
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step9State(t, targets, []provider.Provider{p0, p1}, teo)
		st.Config.DomainRules[0].Targets = []int{0, 1}
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
		s.resolveHostFn = step8Resolve(st)
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()

		if p1.snapshotCalls != 0 {
			t.Fatalf("别名目标不应有第二个执行单元，p1 snapshot=%d", p1.snapshotCalls)
		}
		if p0.createCalls != 1 {
			t.Fatalf("同一物理目标应只有一个 CreateRules 调用，实际 %d", p0.createCalls)
		}
		if len(p0.rules) != 2 {
			t.Fatalf("同一物理目标应合并 DNS+EdgeOne 两条规则，实际 %+v", p0.rules)
		}
		if p0.snapshotCalls != 2 {
			t.Fatalf("同一物理目标应只执行 S0/S1 各一次，实际 snapshot=%d", p0.snapshotCalls)
		}
		proofs := s.RoundCoverage()
		if len(proofs) != 1 {
			t.Fatalf("站点覆盖证明 = %d，期望 1: %+v", len(proofs), proofs)
		}
	})
}

func TestStep9DoubleSourceRetryKeepsCheckedDNS(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		targets := []config.TargetConfig{{ID: 0, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-retry"}}
		p := newProbeProvider(config.CloudTCCVM, 0)
		p.snapshotErrs = []error{errors.New("versionMismatch")}
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step9State(t, targets, []provider.Provider{p}, teo)
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
		resolves := 0
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			resolves++
			if resolves == 1 {
				return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
			}
			return []dns.ResolvedIP{{IP: net.ParseIP("203.0.113.7")}}, nil
		}
		s.backoffFn = func(int) time.Duration { return 0 }
		s.sleepFn = func(time.Duration) {}
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()

		if resolves != 1 {
			t.Fatalf("双开重试使用了新 DNS 解析：resolves=%d，期望 1", resolves)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		foundChecked, foundNew := false, false
		for _, rule := range p.rules {
			if rule.Description == "" || rule.Description[:1] != "[" {
				continue
			}
			if rule.CidrBlock == "192.0.2.10/32" {
				foundChecked = true
			}
			if rule.CidrBlock == "203.0.113.7/32" {
				foundNew = true
			}
		}
		if !foundChecked || foundNew {
			t.Fatalf("重试写入地址 checked=%v new=%v rules=%+v", foundChecked, foundNew, p.rules)
		}
	})
}

func TestStep9S1MissingBlocksConfirmation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		targets := []config.TargetConfig{{ID: 0, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-missing"}}
		p := newProbeProvider(config.CloudTCCVM, 0)
		p.createVisible = false
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		st := step9State(t, targets, []provider.Provider{p}, teo)
		store := newStep10Store()
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
		s.SetEdgeOneTransitionStore(store)
		s.resolveHostFn = step8Resolve(st)
		s.backoffFn = func(int) time.Duration { return 0 }
		s.sleepFn = func(time.Duration) {}
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		if _, confirms := teo.counts(); confirms != 0 {
			t.Fatalf("S1 缺项时确认次数 = %d，期望 0", confirms)
		}
		if len(s.RoundCoverage()) == 0 {
			t.Fatal("S1 缺项应保留不完整证明")
		}
	})
}

func TestStep9EdgeOneCleanupDeferred(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		targets := []config.TargetConfig{{ID: 0, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-defer"}}
		stale := config.RuleInfo{
			Protocol: "TCP", Port: "80", CidrBlock: "198.51.100.0/24", Action: "ACCEPT",
			Description: "[edgeone] stale", RuleID: "stale-1",
		}
		p := newProbeProvider(config.CloudTCCVM, 0, stale)
		teo := &step8FakeTEO{snapshot: step8Snapshot("zone-a", "203.0.113.0/24")}
		rc := config.RuntimeConfig{
			Targets: targets, Tag: "auto-dns", Interval: time.Hour,
			DNS: "8.8.8.8", DNSTimeout: time.Second, DNSFailThreshold: 5, LogLevel: "info",
			SyncEnabled: true, Theme: "light", EdgeOneEnabled: true, EdgeOneTag: "edgeone",
			EdgeOneInterval: time.Hour,
			EdgeOneSites:    []config.EdgeOneSite{{ZoneID: "zone-a", TargetIDs: []int{0}, Protocol: "TCP", Ports: "443", AddressFamily: "ipv4"}},
		}
		rc.SetDNSFeatureEnabled(false)
		st, err := BuildRuntimeState(nil, rc, BreakerReset)
		if err != nil {
			t.Fatalf("构造运行时状态失败: %v", err)
		}
		st.Providers = []provider.Provider{p}
		st.TEO = teo
		s := New(NewRuntimeManager(st))
		s.SetEdgeOneTransitionStore(newStep10Store())
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.deleteCalls != 0 {
			t.Fatalf("EdgeOne TAG 旧网段在 Step 9 不得删除，实际 delete=%d", p.deleteCalls)
		}
		stillThere := false
		for _, rule := range p.rules {
			if rule.RuleID == "stale-1" {
				stillThere = true
			}
		}
		if !stillThere {
			t.Fatalf("旧 EdgeOne 规则被删除: %+v", p.rules)
		}
	})
}
