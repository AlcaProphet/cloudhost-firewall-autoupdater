package syncer

import (
	"errors"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// I8-17f：虚拟时间驱动真实 Run/目标链，固定 DNS 与云端响应，无外网依赖。
// 调用时刻、目标事件和整轮耗时均来自生产入口，不复制冷却算法。
type targetCooldownTrace struct {
	mu    sync.Mutex
	start time.Time
	calls []time.Duration
	dns   []time.Duration
}

func (tr *targetCooldownTrace) snapshot() []time.Duration {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]time.Duration(nil), tr.calls...)
}

func (tr *targetCooldownTrace) dnsSnapshot() []time.Duration {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]time.Duration(nil), tr.dns...)
}

type targetCooldownProvider struct {
	*targetProbeProvider
	trace *targetCooldownTrace
	delay time.Duration
}

func (p *targetCooldownProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	p.trace.mu.Lock()
	p.trace.calls = append(p.trace.calls, time.Since(p.trace.start))
	p.trace.mu.Unlock()
	time.Sleep(p.delay)
	return p.targetProbeProvider.GetSnapshot()
}
func targetCooldownRig(t *testing.T, types ...config.CloudType) (*Syncer, *RuntimeState, *targetCooldownTrace) {
	t.Helper()
	st := gatedState(t, true, time.Hour)
	st.Config.DomainRules[0].Targets = nil
	tr := &targetCooldownTrace{start: time.Now()}
	for i, ct := range types {
		st.Providers = append(st.Providers, &targetCooldownProvider{targetProbeProvider: newProbeProvider(ct, i), trace: tr})
	}
	s := New(NewRuntimeManager(st))
	s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
		tr.mu.Lock()
		tr.dns = append(tr.dns, time.Since(tr.start))
		tr.mu.Unlock()
		return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.1")}}, nil
	}
	t.Cleanup(func() { s.Stop(); s.Wait() })
	return s, st, tr
}
func targetCooldownWant(t *testing.T, got, want []time.Duration) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("调用时刻=%v want=%v", got, want)
	}
}
func TestTargetCooldownClouds(t *testing.T) {
	clouds := []struct {
		ct  config.CloudType
		gap time.Duration
	}{{config.CloudTCLighthouse, 5 * time.Second}, {config.CloudAliSWAS, 5 * time.Second}, {config.CloudTCCVM, 200 * time.Millisecond}, {config.CloudAliECS, 200 * time.Millisecond}}
	for _, c := range clouds {
		for _, mode := range []string{"adjacent", "remaining", "expired", "skip_last", "skip_middle", "failure", "retry", "timing"} {
			t.Run(string(c.ct)+"/"+mode, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					s, st, tr := targetCooldownRig(t, c.ct)
					p := st.Providers[0].(*targetCooldownProvider)
					ch, unsub := s.EventBus().SubscribeChan()
					defer unsub()
					if mode == "skip_last" || mode == "skip_middle" {
						st.Providers = append(st.Providers, &targetCooldownProvider{targetProbeProvider: newProbeProvider(c.ct, 1), trace: tr})
						st.Config.DomainRules[0].Targets = []int{0}
						if mode == "skip_middle" {
							st.Providers = append(st.Providers, &targetCooldownProvider{targetProbeProvider: newProbeProvider(c.ct, 2), trace: tr})
							st.Config.DomainRules[0].Targets = []int{0, 2}
						}
					}
					if mode == "failure" {
						p.snapshotErrs = []error{errors.New("fatal")}
					}
					if mode == "retry" {
						p.snapshotErrs = []error{errors.New("RequestLimitExceeded")}
					}
					if mode == "timing" {
						p.delay = 300 * time.Millisecond
					}
					s.syncAll()
					first := s.Status().LastRound.DurationMS
					if mode != "timing" && mode != "retry" && mode != "skip_middle" && first != 0 {
						t.Fatalf("首轮末尾不应等待，duration_ms=%d", first)
					}
					switch mode {
					case "skip_last":
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0})
						targetCooldownWant(t, tr.dnsSnapshot(), []time.Duration{0})
						if first != 0 {
							t.Fatal(first)
						}
						return
					case "skip_middle":
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, c.gap, c.gap})
						targetCooldownWant(t, tr.dnsSnapshot(), []time.Duration{0, c.gap})
						if first != c.gap.Milliseconds() {
							t.Fatal(first)
						}
						return
					case "remaining":
						time.Sleep(c.gap / 2)
					case "expired":
						time.Sleep(2 * c.gap)
					}
					s.syncAll()
					switch mode {
					case "failure":
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, c.gap, c.gap})
						targetCooldownWant(t, tr.dnsSnapshot(), []time.Duration{0, c.gap})
					case "retry":
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, time.Second, time.Second, time.Second + c.gap, time.Second + c.gap})
						targetCooldownWant(t, tr.dnsSnapshot(), []time.Duration{0, time.Second, time.Second + c.gap})
						if first != 1000 {
							t.Fatal(first)
						}
					case "timing":
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 300 * time.Millisecond, 600*time.Millisecond + c.gap, 900*time.Millisecond + c.gap})
						targetCooldownWant(t, tr.dnsSnapshot(), []time.Duration{0, 600*time.Millisecond + c.gap})
						if first != 600 || s.Status().LastRound.DurationMS != 600+c.gap.Milliseconds() {
							t.Fatal(s.Status().LastRound)
						}
						targets := 0
						for len(ch) > 0 {
							e := <-ch
							if e.Type == notifier.EventTargetSyncComplete {
								targets++
								if e.Data["duration_ms"] != int64(600) {
									t.Fatal(e.Data)
								}
							}
						}
						if targets != 2 {
							t.Fatal(targets)
						}
					case "expired":
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 2 * c.gap, 2 * c.gap})
						targetCooldownWant(t, tr.dnsSnapshot(), []time.Duration{0, 2 * c.gap})
					default:
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, c.gap, c.gap})
						targetCooldownWant(t, tr.dnsSnapshot(), []time.Duration{0, c.gap})
					}
				})
			})
		}
	}
}
func TestTargetCooldownLifecycle(t *testing.T) {
	for _, mode := range []string{"replace_provider", "zero_rules", "zero_providers", "new_syncer", "dryrun_independent", "stop_during_wait", "pause_during_wait", "ticker"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, st, tr := targetCooldownRig(t, config.CloudTCLighthouse)
				s.syncAll()
				if mode == "new_syncer" {
					fresh := New(NewRuntimeManager(st))
					fresh.resolveHostFn = s.resolveHostFn
					fresh.syncAll()
					targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 0, 0})
					return
				}
				if mode == "dryrun_independent" {
					if _, err := s.DryRun(); err != nil {
						t.Fatal(err)
					}
					targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 0})
					return
				}
				if mode == "stop_during_wait" || mode == "pause_during_wait" {
					var n atomic.Int32
					s.SetBeforeRoundHook(func() {
						if n.Add(1) == 1 {
							s.TriggerSync()
						} else {
							s.Stop()
						}
					})
					done := make(chan struct{})
					go func() { s.Run(); close(done) }()
					synctest.Wait()
					if n.Load() != 1 {
						t.Fatal(n.Load())
					}
					time.Sleep(2 * time.Second)
					synctest.Wait()
					if len(tr.snapshot()) != 2 {
						t.Fatal(tr.snapshot())
					}
					if mode == "stop_during_wait" {
						s.Stop()
					} else {
						next := *st
						next.Config = st.Config.DeepCopy()
						next.Config.SyncEnabled = false
						s.ApplyState(&next)
					}
					synctest.Wait()
					time.Sleep(3 * time.Second)
					synctest.Wait()
					if mode == "stop_during_wait" {
						s.Wait()
						<-done
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 5 * time.Second, 5 * time.Second})
						if n.Load() != 1 {
							t.Fatal(n.Load())
						}
					} else {
						time.Sleep(5 * time.Second)
						synctest.Wait()
						s.Stop()
						s.Wait()
						<-done
						targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 5 * time.Second, 5 * time.Second})
						if n.Load() != 1 {
							t.Fatal(n.Load())
						}
					}
					return
				}
				if mode == "ticker" {
					next := *st
					next.Config = st.Config.DeepCopy()
					next.Config.Interval = time.Second
					s.ApplyState(&next)
					go s.Run()
					synctest.Wait()
					time.Sleep(5 * time.Second)
					synctest.Wait()
					targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 5 * time.Second, 5 * time.Second})
					s.Stop()
					time.Sleep(5 * time.Second)
					s.Wait()
					return
				}
				next := *st
				next.Config = st.Config.DeepCopy()
				next.Providers = append([]provider.Provider(nil), st.Providers...)
				switch mode {
				case "replace_provider":
					next.Providers[0] = &targetCooldownProvider{targetProbeProvider: newProbeProvider(config.CloudTCLighthouse, 99), trace: tr}
				case "zero_rules":
					next.Config.DomainRules = nil
				case "zero_providers":
					next.Providers = nil
				}
				s.ApplyState(&next)
				if mode != "replace_provider" {
					s.syncAll()
					if len(tr.snapshot()) != 2 {
						t.Fatal(tr.snapshot())
					}
					s.ApplyState(st)
				}
				s.syncAll()
				targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 5 * time.Second, 5 * time.Second})
			})
		})
	}
}
func TestTargetCooldownIndependent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, st, slow := targetCooldownRig(t, config.CloudTCLighthouse)
		_, fastst, fast := targetCooldownRig(t, config.CloudTCCVM)
		st.Providers = append(st.Providers, fastst.Providers...)
		s.syncAll()
		s.syncAll()
		targetCooldownWant(t, slow.snapshot(), []time.Duration{0, 0, 5 * time.Second, 5 * time.Second})
		targetCooldownWant(t, fast.snapshot(), []time.Duration{0, 0, 200 * time.Millisecond, 200 * time.Millisecond})
	})
}
func TestTargetCooldownPartial(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, st, tr := targetCooldownRig(t, config.CloudAliECS)
		st.Config.DomainRules[0].Protocol = "ICMP"
		st.Config.DomainRules[0].Ports = ""
		st.Config.DomainRules[0].EnableIPv6 = true
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			return []dns.ResolvedIP{{IP: net.ParseIP("2001:db8::1"), IsIPv6: true}}, nil
		}
		s.syncAll()
		if s.Status().LastRound.Outcome != RoundPartial {
			t.Fatal(s.Status().LastRound)
		}
		s.syncAll()
		targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 200 * time.Millisecond, 200 * time.Millisecond})
	})
}

// 紧邻 trigger 与恢复均经过真实 Run 门控，冷却在目标前执行，Stop 不取消已准入轮。
func TestTargetCooldownRunAdjacent(t *testing.T) {
	for _, mode := range []string{"trigger", "resume"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, st, tr := targetCooldownRig(t, config.CloudTCLighthouse)
				go s.Run()
				synctest.Wait()
				if sum := s.Status().LastRound; sum == nil || sum.DurationMS != 0 {
					t.Fatalf("首次单目标轮应已完成且无末尾等待，汇总=%+v", sum)
				}
				time.Sleep(time.Second)
				if mode == "trigger" {
					s.TriggerSync()
				} else {
					paused := *st
					paused.Config = st.Config.DeepCopy()
					paused.Config.SyncEnabled = false
					s.ApplyState(&paused)
					synctest.Wait()
					resumed := paused
					resumed.Config = paused.Config.DeepCopy()
					resumed.Config.SyncEnabled = true
					s.ApplyState(&resumed)
				}
				synctest.Wait()
				targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0})
				s.Stop()
				s.Wait()
				targetCooldownWant(t, tr.snapshot(), []time.Duration{0, 0, 5 * time.Second, 5 * time.Second})
				if s.Status().LastRound.DurationMS != 4000 {
					t.Fatal(s.Status().LastRound)
				}
			})
		})
	}
}
