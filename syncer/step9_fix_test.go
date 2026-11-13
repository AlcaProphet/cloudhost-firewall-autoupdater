package syncer

import (
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
)

func TestStep9SharedKeyWrittenWithEdgeOneTagWhenEdgeOneOnlyDue(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	teo := &step8FakeTEO{snapshot: fixSnapshot("zone-a", "203.0.113.0/32", true)}
	st := fixState(t, config.CloudTCCVM, true, true, p,
		[]config.DomainRule{fixRule(1, "shared.example")},
		[]config.EdgeOneSite{fixSite("ipv4")}, teo)
	s := New(NewRuntimeManager(st))
	s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
		return []dns.ResolvedIP{{IP: net.ParseIP("203.0.113.0")}}, nil
	}
	// 直接调用目标执行单元，绕过全局预检。双开时同一地址必然被判为跨源重叠，
	// 因此这里验证的是执行层在“仅 EdgeOne 活动”时的 TAG 选择合同。
	dnsData := &roundDNSData{
		resolved:  map[int][]dns.ResolvedIP{1: {{IP: net.ParseIP("203.0.113.0")}}},
		errors:    map[int]string{},
		rawErrors: map[int]error{},
		complete:  true,
	}
	res := s.syncTargetExec(st, targetExecution{
		identity:   config.TargetIdentity(st.Config.Targets[0]),
		epoch:      s.Generation(),
		provider:   p,
		rules:      st.Config.DomainRules,
		targetIDs:  []int{0},
		dnsActive:  false,
		edgeActive: true,
		dnsData:    dnsData,
		edgeData:   map[string]edgeOneSiteData{"zone-a": {site: st.Config.EdgeOneSites[0], snapshot: teo.snapshot}},
		edgeSites:  st.Config.EdgeOneSites,
	}, nil)
	if res.outcome != TargetSuccess || res.added != 1 {
		t.Fatalf("目标执行结果 = %+v", res)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.createCalls != 1 || len(p.rules) != 1 || p.rules[0].Description != "[edgeone]" {
		t.Fatalf("仅 EdgeOne 活动时共享 key 应写 EdgeOne TAG，create=%d rules=%+v", p.createCalls, p.rules)
	}
}

func TestStep9CapabilitySkipBlocksConfirmation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudAliSWAS, 0)
		teo := &step8FakeTEO{snapshot: fixSnapshot("zone-a", "2001:db8::/64", true)}
		st := fixState(t, config.CloudAliSWAS, false, true, p, nil,
			[]config.EdgeOneSite{fixSite("ipv6")}, teo)
		s := New(NewRuntimeManager(st))
		s.SetEdgeOneTransitionStore(newStep10Store())
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()

		if _, confirms := teo.counts(); confirms != 0 {
			t.Fatalf("能力跳过时确认次数 = %d，期望 0", confirms)
		}
		proofs := s.RoundCoverage()
		if len(proofs) != 1 {
			t.Fatalf("站点证明数量 = %d，期望 1: %+v", len(proofs), proofs)
		}
		for _, proof := range proofs {
			if proof.Complete {
				t.Fatalf("能力跳过的站点不得生成完整证明: %+v", proof)
			}
			found := false
			for _, reason := range proof.Reasons {
				if reason == "unsupported" {
					found = true
				}
			}
			if !found {
				t.Fatalf("证明缺少 unsupported 原因: %+v", proof.Reasons)
			}
		}
	})
}

func TestStep9FailedAttemptProofNotRetained(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stale := config.RuleInfo{
			Protocol: "TCP", Port: "80", CidrBlock: "198.51.100.0/24", Action: "ACCEPT",
			Description: "[auto-dns] stale", RuleID: "stale-1", PolicyIndex: "1",
		}
		p := newProbeProvider(config.CloudTCCVM, 0, stale)
		p.deleteErr = errors.New("requestlimitexceeded")
		p.snapshotErrs = []error{nil, nil, errors.New("fixture S0 failure")}
		teo := &step8FakeTEO{snapshot: fixSnapshot("zone-a", "203.0.113.0/32", true)}
		st := fixState(t, config.CloudTCCVM, true, true, p,
			[]config.DomainRule{fixRule(1, "home.example")},
			[]config.EdgeOneSite{fixSite("ipv4")}, teo)
		s := New(NewRuntimeManager(st))
		s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
		s.SetEdgeOneTransitionStore(newStep10Store())
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil
		}
		s.backoffFn = func(int) time.Duration { return 0 }
		s.sleepFn = func(time.Duration) {}
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		synctest.Wait()

		status := s.Status()
		if status.LastRound == nil || status.LastRound.Failed != 1 {
			t.Fatalf("目标最终应 failed: %+v", status.LastRound)
		}
		if _, confirms := teo.counts(); confirms != 0 {
			t.Fatalf("失败 attempt 之后确认次数 = %d，期望 0", confirms)
		}
		if len(s.RoundCoverage()) != 0 {
			t.Fatalf("失败 attempt 的证明被保留: %+v", s.RoundCoverage())
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.deleteCalls != 1 {
			t.Fatalf("应只发生一次清理调用，实际 %d", p.deleteCalls)
		}
	})
}
