package syncer

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func TestStep8PreflightUsesAAAAWithIPv6WritesDisabled(t *testing.T) {
	for _, overlap := range []bool{true, false} {
		name := "overlap"
		if !overlap {
			name = "non_overlapping_AAAA_stays_out_of_DNS_rules"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newProbeProvider(config.CloudTCCVM, 0)
				snap := step8Snapshot("zone-a", "203.0.113.0/24", "2001:db8:8::/64")
				teo := &step8FakeTEO{snapshot: snap}
				st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{step8Site("zone-a")})
				st.TEO = teo
				s := New(NewRuntimeManager(st))
				s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
				s.SetEdgeOneTransitionStore(newStep10Store())
				address := "2001:db8:8::42"
				if !overlap {
					address = "2001:db8:9::42"
				}
				resolves := 0
				s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
					resolves++
					return []dns.ResolvedIP{
						{IP: net.ParseIP("192.0.2.10")},
						{IP: net.ParseIP(address), IsIPv6: true},
					}, nil
				}
				go s.Run()
				defer func() { s.Stop(); s.Wait() }()
				synctest.Wait()
				reads, adds, deletes := p.counts()
				_, confirms := teo.counts()
				status := s.Status().Preflight
				if resolves != 1 {
					t.Fatalf("DNS queries=%d, preflight=%+v", resolves, status)
				}
				if overlap {
					if reads != 0 || adds != 0 || deletes != 0 || confirms != 0 || status.State != PreflightStateOverlap {
						t.Fatalf("AAAA 重叠应阻断两侧，snapshot=%d create=%d delete=%d confirm=%d preflight=%+v", reads, adds, deletes, confirms, status)
					}
					return
				}
				if reads != 2 || adds != 1 || deletes != 0 || confirms != 1 || status.State != PreflightStateClear ||
					probeTaggedRules(p, st.Config.Tag) != 1 || probeTaggedRules(p, st.Config.EdgeOneTag) != 1 {
					t.Fatalf("非重叠时只写入 DNS A 和 EdgeOne IPv4，snapshot=%d create=%d delete=%d confirm=%d preflight=%+v", reads, adds, deletes, confirms, status)
				}
			})
		})
	}
}

func TestStep8SingleSourceDueSkipsOtherSourceTargets(t *testing.T) {
	for _, dnsDue := range []bool{false, true} {
		name := "EdgeOne_due"
		if dnsDue {
			name = "DNS_due"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p0 := newProbeProvider(config.CloudTCCVM, 0)
				p1 := newProbeProvider(config.CloudTCCVM, 1)
				active, inactive := p0, p1
				dnsInterval, edgeInterval := time.Hour, 10*time.Second
				if dnsDue {
					active, inactive = p1, p0
					dnsInterval, edgeInterval = edgeInterval, dnsInterval
				}
				inactive.snapshotErrs = []error{nil, nil, errors.New("fixture: inactive target unavailable")}
				snap := step10Snapshot()
				snap.Next = nil
				teo := &step8FakeTEO{snapshot: snap}
				st := step8State(t, true, true, dnsInterval, edgeInterval, p1, []config.EdgeOneSite{step8Site("zone-a")})
				st.Config.Targets = append(st.Config.Targets, config.TargetConfig{ID: 0, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-edge"})
				st.Config.DomainRules[0].Targets = []int{1}
				st.Providers = []provider.Provider{p0, p1}
				st.TEO = teo
				s := New(NewRuntimeManager(st))
				s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
				s.resolveHostFn = step8Resolve(st)
				s.sleepFn = func(time.Duration) {}
				go s.Run()
				defer func() { s.Stop(); s.Wait() }()
				synctest.Wait()
				beforeReads, beforeAdds, beforeDeletes := inactive.counts()
				activeReads, _, _ := active.counts()
				if beforeReads != 2 || activeReads != 2 {
					t.Fatalf("启动轮应回读两个目标，active=%d inactive=%d", activeReads, beforeReads)
				}
				synctest.Sleep(11 * time.Second)
				synctest.Wait()
				reads, adds, deletes := inactive.counts()
				activeReads, _, _ = active.counts()
				summary := s.Status().LastRound
				if reads != beforeReads || adds != beforeAdds || deletes != beforeDeletes || activeReads != 4 ||
					summary.Total != 1 || summary.OK != 1 || summary.Outcome != RoundSuccess {
					t.Fatalf("单源到期应只执行一个目标，inactive=%v active=%v summary=%+v", inactive.callSeq(), active.callSeq(), summary)
				}
			})
		})
	}
}

func TestStep10DualCoverageRequiresBothAddressFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, current, pending string
		complete               bool
	}{
		{"缺 IPv6", "203.0.113.0/24", "203.0.114.0/24", false},
		{"缺 IPv4", "2001:db8:1::/64", "2001:db8:2::/64", false},
		{"当前和待更新版的并集有两族", "203.0.113.0/24", "2001:db8:2::/64", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newProbeProvider(config.CloudTCCVM, 0)
				snap := step8Snapshot("zone-a", tc.current)
				snap.Next.Prefixes = []netip.Prefix{netip.MustParsePrefix(tc.pending)}
				teo := &step8FakeTEO{snapshot: snap}
				st := step10State(t, p, teo)
				st.Config.EdgeOneSites[0].AddressFamily = config.EdgeOneAddressFamilyDual
				s := New(NewRuntimeManager(st))
				s.SetEdgeOneTransitionStore(newStep10Store())
				go s.Run()
				defer func() { s.Stop(); s.Wait() }()
				synctest.Wait()
				_, confirms := teo.counts()
				coverage := s.RoundCoverage()
				proof := coverage["zone-a\x00"+config.TargetIdentity(st.Config.Targets[0])]
				summary := s.Status().LastRound
				if tc.complete {
					if confirms != 1 || !proof.Complete || summary.Outcome != RoundSuccess {
						t.Fatalf("双族齐全应确认一次，confirm=%d proof=%+v summary=%+v", confirms, proof, summary)
					}
				} else if confirms != 0 || proof.Complete || !containsString(proof.Reasons, "address_family_missing") ||
					summary.Outcome != RoundPartial || summary.Skipped != 1 {
					t.Fatalf("缺地址族应为部分实施且零确认，confirm=%d proof=%+v summary=%+v", confirms, proof, summary)
				}
			})
		})
	}
}

func TestStep9S2ControlsFinalEdgeOneProof(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		outcome      RoundOutcome
		confirms     int
	}{
		{"跨 TAG 等价不计覆盖", "cross-tag", RoundFailed, 0},
		{"相反动作使证明不完整", "opposite", RoundPartial, 0},
		{"覆盖保持时确认一次", "", RoundSuccess, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				stale := config.RuleInfo{CidrBlock: "198.51.100.99/32", Protocol: "TCP", Port: "443", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "stale", PolicyIndex: "0"}
				p := newProbeProvider(config.CloudTCCVM, 0, stale)
				p.afterDelete = func() {
					p.mu.Lock()
					defer p.mu.Unlock()
					switch tc.change {
					case "cross-tag":
						for i := range p.rules {
							if p.rules[i].CidrBlock == "203.0.113.0/24" {
								p.rules[i].Description = "[auto-dns]"
							}
						}
					case "opposite":
						p.rules = append(p.rules, config.RuleInfo{CidrBlock: "203.0.113.0/24", Protocol: "TCP", Port: "443", Action: "DROP", Description: "manual"})
					}
					p.revision++
				}
				teo := &step10TEO{}
				st := step8State(t, true, true, time.Hour, time.Hour, p, []config.EdgeOneSite{step8Site("zone-a")})
				st.TEO = teo
				s := New(NewRuntimeManager(st))
				s.SetConflictState(NewAddressConflictState(&step8MemConflictStore{}))
				s.SetEdgeOneTransitionStore(newStep10Store())
				s.resolveHostFn = step8Resolve(st)
				go s.Run()
				defer func() { s.Stop(); s.Wait() }()
				synctest.Wait()
				reads, adds, deletes := p.counts()
				if reads != 3 || adds != 1 || deletes != 1 {
					t.Fatalf("invalid fixture: no S0/Add/S1/Delete/S2: calls=%v", p.callSeq())
				}
				_, confirms := teo.counts()
				summary := s.Status().LastRound
				coverage := s.RoundCoverage()
				proof := coverage["zone-a\x00"+config.TargetIdentity(st.Config.Targets[0])]
				if confirms != tc.confirms || summary.Outcome != tc.outcome || summary.Added != 2 ||
					summary.Deleted != 1 || summary.CleanupDeleted != 1 {
					t.Fatalf("S2 结论与实际增删计数不符，confirms=%d summary=%+v", confirms, summary)
				}
				switch tc.outcome {
				case RoundFailed:
					if len(coverage) != 0 {
						t.Fatalf("失败后仍保留站点证明：%+v", coverage)
					}
				case RoundPartial:
					if proof.Complete || !proof.HasConflicts || !containsString(proof.Reasons, "conflict") {
						t.Fatalf("应使用 S2 的冲突证据：%+v", proof)
					}
				case RoundSuccess:
					if !proof.Complete || proof.HasConflicts {
						t.Fatalf("S2 覆盖证明不完整：%+v", proof)
					}
				}
			})
		})
	}
}
