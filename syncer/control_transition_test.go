package syncer

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// P3-24 回归夹具：S0 用 channel 屏障阻塞真实目标链，无真实 DNS/云请求。
type p324BlockingProvider struct {
	*targetProbeProvider
	release   chan struct{}
	snapshots int
}

func (p *p324BlockingProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	p.snapshots++
	if p.snapshots%2 == 1 {
		<-p.release
	}
	return p.targetProbeProvider.GetSnapshot()
}

type p324Rig struct {
	s      *Syncer
	start  time.Time
	rounds []time.Duration
	mu     sync.Mutex
	p      *p324BlockingProvider
}

func p324New(t *testing.T, enabled bool, interval time.Duration, pre []bool) *p324Rig {
	t.Helper()
	st := gatedState(t, enabled, interval)
	p := &p324BlockingProvider{targetProbeProvider: newProbeProvider(config.CloudTCCVM, 0), release: make(chan struct{}, 10)}
	st.Providers = []provider.Provider{p}
	r := &p324Rig{s: New(NewRuntimeManager(st)), start: time.Now(), p: p}
	r.s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) { return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil }
	r.s.sleepFn = func(time.Duration) {}
	r.s.SetBeforeRoundHook(func() { r.mu.Lock(); r.rounds = append(r.rounds, time.Since(r.start)); r.mu.Unlock() })
	for _, e := range pre {
		r.save(e, interval, "dark")
	}
	go r.s.Run()
	synctest.Wait()
	t.Cleanup(func() {
		r.s.Stop()
		for i := 0; i < 10; i++ {
			select {
			case p.release <- struct{}{}:
			default:
			}
		}
		r.s.Wait()
	})
	return r
}

// publish 只发布；save 在发布后等待 Run 或在途目标链进入确定性阻塞点。
func (r *p324Rig) publish(enabled bool, interval time.Duration, theme string) {
	next := *r.s.Runtime().Snapshot()
	next.Config = next.Config.DeepCopy()
	next.Config.SyncEnabled = enabled
	next.Config.Interval = interval
	next.Config.Theme = theme
	r.s.ApplyState(&next)
}
func (r *p324Rig) save(enabled bool, interval time.Duration, theme string) {
	r.publish(enabled, interval, theme)
	synctest.Wait()
}
func (r *p324Rig) times() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.rounds...)
}

func (r *p324Rig) finish() { r.p.release <- struct{}{}; synctest.Wait() }
func (r *p324Rig) count(t *testing.T, want int) {
	t.Helper()
	if got := len(r.times()); got != want {
		t.Fatalf("轮数=%d，期望=%d，时间=%v", got, want, r.times())
	}
}
func TestP324CoalescedEdges(t *testing.T) {
	cases := []struct {
		name    string
		edges   []bool
		extra   int
		trigger bool
	}{
		{"single", []bool{false, true}, 1, false},
		{"multiple", []bool{false, true, false, true, false, true}, 1, false},
		{"final_pause", []bool{false, true, false}, 0, false},
		{"ordinary_saves", []bool{true, true, true}, 0, false},
		{"stale_trigger", []bool{false, true}, 1, true},
		{"final_pause_trigger", []bool{false, true, false}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := p324New(t, true, time.Hour, nil)
				r.count(t, 1)
				for _, e := range tc.edges {
					r.save(e, time.Hour, "dark")
				}
				if tc.trigger {
					r.s.TriggerSync()
				}
				r.finish()
				r.count(t, 1+tc.extra)
				if tc.extra > 0 {
					r.finish()
				}
				synctest.Sleep(time.Minute)
				r.count(t, 1+tc.extra)
				if tc.name == "final_pause" {
					r.save(true, time.Hour, "light")
					r.count(t, 2)
					r.finish()
					synctest.Sleep(time.Minute)
					r.count(t, 2)
				}
			})
		})
	}
}
func TestP324EdgeDuringResume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p324New(t, true, time.Hour, nil)
		r.save(false, time.Hour, "dark")
		r.save(true, time.Hour, "light")
		r.finish()
		r.count(t, 2)
		r.save(false, time.Hour, "dark")
		r.save(true, time.Hour, "light")
		r.finish()
		r.count(t, 3)
		r.finish()
		synctest.Sleep(time.Minute)
		r.count(t, 3)
	})
}
func TestP324NormalResumeIdempotent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p324New(t, false, time.Hour, nil)
		r.count(t, 0)
		r.s.Resume()
		synctest.Wait()
		r.count(t, 1)
		r.s.Resume()
		r.save(true, time.Hour, "dark")
		r.finish()
		r.count(t, 1)
		synctest.Sleep(time.Minute)
		r.count(t, 1)
	})
}
func TestP324StopSuppressesResume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p324New(t, true, time.Hour, nil)
		r.save(false, time.Hour, "dark")
		r.save(true, time.Hour, "light")
		r.s.Stop()
		r.finish()
		r.s.Wait()
		r.count(t, 1)
	})
}
func TestP324BeforeRun(t *testing.T) {
	for _, edges := range [][]bool{{false, true}, {false, true, false}, {true, true}} {
		t.Run(fmt.Sprint(edges), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := p324New(t, true, time.Hour, edges)
				want := 0
				if edges[len(edges)-1] {
					want = 1
					r.finish()
				}
				r.count(t, want)
				synctest.Sleep(time.Minute)
				r.count(t, want)
			})
		})
	}
}
func TestP324CompetingWakeup(t *testing.T) {
	for _, kind := range []string{"trigger", "tick"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := p324New(t, true, 10*time.Second, nil)
				r.save(false, 10*time.Second, "dark")
				r.save(true, 10*time.Second, "light")
				// 强制普通唤醒赢得 select：移除唤醒 token 不改变已发布状态与恢复标记。
				<-r.s.controlCh
				if kind == "trigger" {
					r.s.TriggerSync()
				} else {
					synctest.Sleep(20 * time.Second)
				}
				r.finish()
				r.count(t, 2)
				r.finish()
				r.count(t, 2)
			})
		})
	}
}
func TestP324ResumeLatestInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p324New(t, true, 10*time.Second, nil)
		r.save(false, 10*time.Second, "dark")
		r.save(true, 7*time.Second, "light")
		r.finish()
		r.count(t, 2)
		r.finish()
		synctest.Sleep(7*time.Second - time.Nanosecond)
		r.count(t, 2)
		synctest.Sleep(time.Nanosecond)
		r.count(t, 3)
		r.finish()
	})
}

func TestP324EdgesDuringOrdinaryRound(t *testing.T) {
	for _, kind := range []string{"trigger", "tick"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := p324New(t, true, 10*time.Second, nil)
				r.finish()
				if kind == "trigger" {
					r.s.TriggerSync()
					synctest.Wait()
				} else {
					synctest.Sleep(10 * time.Second)
				}
				r.count(t, 2)
				r.save(false, 10*time.Second, "dark")
				r.save(true, 10*time.Second, "light")
				r.finish()
				r.count(t, 3)
				r.finish()
				r.count(t, 3)
			})
		})
	}
}
func TestP324EdgeAfterResumeAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p324New(t, true, time.Hour, nil)
		r.s.SetBeforeRoundHook(func() {
			r.mu.Lock()
			r.rounds = append(r.rounds, time.Since(r.start))
			n := len(r.rounds)
			r.mu.Unlock()
			if n == 2 {
				// 注入发生在恢复已准入、syncAll 尚未取得快照时；新边沿仍须保留。
				next := *r.s.Runtime().Snapshot()
				next.Config = next.Config.DeepCopy()
				next.Config.SyncEnabled = false
				r.s.ApplyState(&next)
				again := next
				again.Config = again.Config.DeepCopy()
				again.Config.SyncEnabled = true
				r.s.ApplyState(&again)
			}
		})
		r.save(false, time.Hour, "dark")
		r.save(true, time.Hour, "light")
		r.finish()
		r.count(t, 2)
		r.finish()
		r.count(t, 3)
		r.finish()
		r.count(t, 3)
	})
}

// 已消费状态的回调在锁外运行，可以安全发布下一批恢复；旧通知不得重复起轮。
func TestP324RecoveryPublishedByStateHook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p324New(t, true, time.Hour, nil)
		var callbacks atomic.Int32
		r.s.SetStateAppliedHook(func(*RuntimeState) {
			if callbacks.Add(1) == 1 {
				r.publish(false, time.Hour, "dark")
				r.publish(true, time.Hour, "light")
			}
		})
		r.save(false, time.Hour, "dark")
		r.save(true, time.Hour, "light")
		r.finish()
		r.count(t, 2)
		r.finish()
		r.count(t, 3)
		r.finish()
		r.count(t, 3)
		if got := callbacks.Load(); got != 2 {
			t.Fatalf("状态回调=%d，期望2", got)
		}
		synctest.Sleep(time.Minute)
		r.count(t, 3)
	})
}

// 普通轮已准入后新增的恢复必须作为下一批，不能因本轮取得新快照而消失。
func TestP324RecoveryAfterOrdinaryAdmission(t *testing.T) {
	for _, kind := range []string{"trigger", "tick"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := p324New(t, true, 10*time.Second, nil)
				r.finish()
				r.s.SetBeforeRoundHook(func() {
					r.mu.Lock()
					r.rounds = append(r.rounds, time.Since(r.start))
					n := len(r.rounds)
					r.mu.Unlock()
					if n == 2 {
						r.publish(false, 10*time.Second, "dark")
						r.publish(true, 10*time.Second, "light")
					}
				})
				if kind == "trigger" {
					r.s.TriggerSync()
					synctest.Wait()
				} else {
					synctest.Sleep(10 * time.Second)
				}
				r.count(t, 2)
				r.finish()
				r.count(t, 3)
				r.finish()
				r.count(t, 3)
			})
		})
	}
}

// 恢复意图独立于上一轮 success/failed/idle，不以旧结果取消已发布的恢复。
func TestP324RecoveryIndependentOfOutcome(t *testing.T) {
	for _, kind := range []string{"idle", "failed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				st := gatedState(t, true, time.Hour)
				if kind == "idle" {
					st.Config.DomainRules = nil
				}
				p := newProbeProvider(config.CloudTCCVM, 0)
				st.Providers = []provider.Provider{p}
				s := New(NewRuntimeManager(st))
				var rounds atomic.Int32
				s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) { return nil, errors.New("test dns failure") }
				s.backoffFn = func(int) time.Duration { return 0 }
				s.sleepFn = func(time.Duration) {}
				s.SetBeforeRoundHook(func() {
					if rounds.Add(1) == 1 {
						next := *s.Runtime().Snapshot()
						next.Config = next.Config.DeepCopy()
						next.Config.SyncEnabled = false
						s.ApplyState(&next)
						again := next
						again.Config = again.Config.DeepCopy()
						again.Config.SyncEnabled = true
						s.ApplyState(&again)
					}
				})
				go s.Run()
				synctest.Wait()
				defer func() { s.Stop(); s.Wait() }()
				if got := rounds.Load(); got != 2 {
					t.Fatalf("轮数=%d，期望2", got)
				}
				want := RoundIdle
				if kind == "failed" {
					want = RoundFailed
				}
				if got := s.Status().LastRound; got == nil || got.Outcome != want {
					t.Fatalf("最后结果=%+v，期望%s", got, want)
				}
				synctest.Sleep(time.Minute)
				if got := rounds.Load(); got != 2 {
					t.Fatalf("重复轮数=%d，期望2", got)
				}
			})
		})
	}
}

// 当前轮继续用旧 Provider；恢复轮必须取得最新状态中的 Provider/TAG。
func TestP324RecoveryUsesLatestState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p324New(t, true, time.Hour, nil)
		r.save(false, time.Hour, "dark")
		next := *r.s.Runtime().Snapshot()
		next.Config = next.Config.DeepCopy()
		next.Config.SyncEnabled = true
		next.Config.Tag = "new-tag"
		newer := newProbeProvider(config.CloudTCCVM, 0)
		next.Providers = []provider.Provider{newer}
		r.s.ApplyState(&next)
		r.finish()
		r.count(t, 2)
		r.p.mu.Lock()
		oldCalls := r.p.snapshotCalls
		r.p.mu.Unlock()
		newer.mu.Lock()
		newCalls := newer.snapshotCalls
		newRules := append([]config.RuleInfo(nil), newer.rules...)
		newer.mu.Unlock()
		if oldCalls != 2 || newCalls != 2 {
			t.Fatalf("旧/新快照调用=%d/%d，期望2/2", oldCalls, newCalls)
		}
		if len(newRules) != 1 || !strings.HasPrefix(newRules[0].Description, "[new-tag]") {
			t.Fatalf("恢复轮未使用新TAG: %+v", newRules)
		}
		synctest.Sleep(time.Minute)
		r.count(t, 2)
	})
}
