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
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 使用虚拟时间驱动真实 Run 与目标链，固定 DNS 和云端响应，不访问外部网络。
// 延迟位于真实目标链的 S0，避免对调度器复制一份模型。
type p323SlowProvider struct {
	*targetProbeProvider
	snapshots int
	delay     time.Duration
}

func (p *p323SlowProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	p.snapshots++
	if p.snapshots%2 == 1 {
		time.Sleep(p.delay)
	}
	return p.targetProbeProvider.GetSnapshot()
}

type p323Rig struct {
	s       *Syncer
	start   time.Time
	rounds  []time.Duration
	mu      sync.Mutex
	applied atomic.Int32
}

func p323New(t *testing.T, enabled bool, interval, delay time.Duration) *p323Rig {
	t.Helper()
	st := gatedState(t, enabled, interval)
	p := &p323SlowProvider{targetProbeProvider: newProbeProvider(config.CloudTCCVM, 0), delay: delay}
	st.Providers = []provider.Provider{p}
	r := &p323Rig{s: New(NewRuntimeManager(st)), start: time.Now()}
	r.s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) { return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil }
	r.s.sleepFn = func(time.Duration) {}
	r.s.SetBeforeRoundHook(func() { r.mu.Lock(); r.rounds = append(r.rounds, time.Since(r.start)); r.mu.Unlock() })
	r.s.SetStateAppliedHook(func(*RuntimeState) { r.applied.Add(1) })
	go r.s.Run()
	synctest.Wait()
	return r
}
func (r *p323Rig) close() { r.s.Stop(); r.s.Wait() }
func (r *p323Rig) save(enabled bool, interval time.Duration, theme string) {
	next := *r.s.Runtime().Snapshot()
	next.Config = next.Config.DeepCopy()
	next.Config.SyncEnabled = enabled
	next.Config.Interval = interval
	next.Config.Theme = theme
	r.s.ApplyState(&next)
	synctest.Wait()
}
func (r *p323Rig) times() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.rounds...)
}
func (r *p323Rig) want(t *testing.T, secs ...int) {
	actual := r.times()
	t.Helper()
	expected := make([]time.Duration, len(secs))
	for i, n := range secs {
		expected[i] = time.Duration(n) * time.Second
	}
	if len(actual) == 0 && len(expected) == 0 {
		return
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("轮次开始时刻 = %v，期望 %v", actual, expected)
	}
}
func TestP323SameInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 0)
		defer r.close()
		synctest.Sleep(6 * time.Second)
		r.save(true, 10*time.Second, "dark")
		synctest.Sleep(3 * time.Second)
		r.save(true, 10*time.Second, "light")
		if r.applied.Load() != 2 {
			t.Fatalf("状态观察次数 = %d，期望 2", r.applied.Load())
		}
		synctest.Sleep(time.Second - time.Nanosecond)
		r.want(t, 0)
		synctest.Sleep(time.Nanosecond)
		r.want(t, 0, 10)
	})
}
func TestP323FrequentSaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 0)
		defer r.close()
		for i := 0; i < 15; i++ {
			synctest.Sleep(2 * time.Second)
			r.save(true, 10*time.Second, "dark")
		}
		r.want(t, 0, 10, 20, 30)
		if r.applied.Load() != 15 {
			t.Fatalf("状态观察次数 = %d，期望 15", r.applied.Load())
		}
		if got := r.s.Status().LastRound; got == nil || got.Outcome != RoundSuccess {
			t.Fatalf("真实目标链未成功：%+v", got)
		}
	})
}
func TestP323IntervalChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 0)
		defer r.close()
		synctest.Sleep(6 * time.Second)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(4*time.Second - time.Nanosecond)
		r.want(t, 0)
		synctest.Sleep(time.Nanosecond)
		r.want(t, 0, 10)
		synctest.Sleep(time.Second)
		r.save(true, 7*time.Second, "light")
		synctest.Sleep(7 * time.Second)
		r.want(t, 0, 10, 18)
		synctest.Sleep(time.Second)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(4 * time.Second)
		r.want(t, 0, 10, 18, 23)
	})
}
func TestP323PausedUpdatesAndResume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 0)
		defer r.close()
		synctest.Sleep(4 * time.Second)
		r.save(false, 10*time.Second, "dark")
		r.s.TriggerSync()
		synctest.Wait()
		synctest.Sleep(time.Second)
		r.save(false, 3*time.Second, "light")
		synctest.Sleep(time.Second)
		r.save(false, 7*time.Second, "dark")
		synctest.Sleep(40 * time.Second)
		r.want(t, 0)
		r.save(true, 7*time.Second, "light")
		r.want(t, 0, 46)
		synctest.Sleep(7*time.Second - time.Nanosecond)
		r.want(t, 0, 46)
		synctest.Sleep(time.Nanosecond)
		r.want(t, 0, 46, 53)
	})
}
func TestP323InitialPause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, false, 10*time.Second, 0)
		defer r.close()
		synctest.Sleep(30 * time.Second)
		r.save(false, 3*time.Second, "dark")
		r.want(t)
		synctest.Sleep(30 * time.Second)
		r.save(true, 3*time.Second, "light")
		r.want(t, 60)
		synctest.Sleep(3 * time.Second)
		r.want(t, 60, 63)
	})
}
func TestP323TimelineSlowRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(24 * time.Second)
		r.want(t, 0, 10, 23)
	})
}
func TestP323TimelineManualRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(6 * time.Second)
		r.s.TriggerSync()
		synctest.Wait()
		synctest.Sleep(3 * time.Second)
		synctest.Sleep(12 * time.Second)
		r.want(t, 0, 6, 19)
	})
}
func TestP323TimelineOverrun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 12*time.Second)
		defer r.close()
		synctest.Sleep(25 * time.Second)
		r.want(t, 0, 12)
	})
}

// 该控制故意保持既有合并语义，固定 P3-23 的范围边界，不表示 P3-24 已修复或其现状被认可。
func TestP323CoalescedResumeRemainsSeparate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, time.Hour, 3*time.Second)
		defer r.close()
		r.save(false, time.Hour, "dark")
		r.save(true, time.Hour, "light")
		synctest.Sleep(5 * time.Second)
		r.want(t, 0)
	})
}

func TestP323TracksActualInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 0)
		defer r.close()
		synctest.Sleep(6 * time.Second)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(5 * time.Second)
		r.save(true, 4*time.Second, "light")
		synctest.Sleep(3 * time.Second)
		r.want(t, 0, 10, 14)
		synctest.Sleep(time.Second)
		r.save(true, 10*time.Second, "dark")
		synctest.Sleep(10 * time.Second)
		r.want(t, 0, 10, 14, 25)
	})
}
func TestP323SaveDuringStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(time.Second)
		r.save(true, 10*time.Second, "dark")
		synctest.Sleep(9 * time.Second)
		r.want(t, 0, 10)
	})
}
func TestP323SaveDuringResume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(4 * time.Second)
		r.save(false, 10*time.Second, "dark")
		synctest.Sleep(2 * time.Second)
		r.save(true, 10*time.Second, "light")
		synctest.Sleep(2 * time.Second)
		r.save(true, 10*time.Second, "dark")
		synctest.Sleep(8 * time.Second)
		r.want(t, 0, 6, 16)
	})
}

func p323WantDurations(t *testing.T, r *p323Rig, expected ...time.Duration) {
	t.Helper()
	if got := r.times(); !reflect.DeepEqual(got, expected) {
		t.Fatalf("轮次开始时刻 = %v，期望 %v", got, expected)
	}
}

func TestP323ScheduledRoundSameIntervalSave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(11 * time.Second)
		r.save(true, 10*time.Second, "dark")
		synctest.Sleep(12 * time.Second)
		r.want(t, 0, 10, 23)
		if r.applied.Load() != 1 {
			t.Fatalf("状态观察重复，次数 = %d", r.applied.Load())
		}
	})
}

func TestP323LatestIntervalAfterScheduledRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(11 * time.Second)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(time.Second)
		r.save(true, 7*time.Second, "light")
		synctest.Sleep(2 * time.Second)
		if r.applied.Load() != 1 {
			t.Fatalf("合并后的状态观察次数 = %d，期望 1", r.applied.Load())
		}
		synctest.Sleep(time.Second)
		r.save(true, 7*time.Second, "dark")
		synctest.Sleep(5 * time.Second)
		r.want(t, 0, 10, 20)
	})
}

func TestP323ChangeDuringManualRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(6 * time.Second)
		r.s.TriggerSync()
		synctest.Wait()
		synctest.Sleep(time.Second)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(6 * time.Second)
		r.want(t, 0, 6, 13)
	})
}

func TestP323StartupChangeBeforeTickExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(time.Second)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(13 * time.Second)
		r.want(t, 0, 7, 14)
	})
}

func TestP323StartupCoalescedIntervalsReturnToActual(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 12*time.Second)
		defer r.close()
		synctest.Sleep(time.Second)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(time.Second)
		r.save(true, 10*time.Second, "light")
		synctest.Sleep(33 * time.Second)
		r.want(t, 0, 12, 34)
		if r.applied.Load() != 1 {
			t.Fatalf("合并后的状态观察次数 = %d，期望 1", r.applied.Load())
		}
	})
}

// 已到期 ticker 与 control 同时可选时，允许两个既有 select 顺序。
func TestP323ExpiredTickAndIntervalChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 12*time.Second)
		defer r.close()
		synctest.Sleep(time.Second)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(15 * time.Second)
		actual := r.times()
		if !reflect.DeepEqual(actual, []time.Duration{0, 12 * time.Second}) && !reflect.DeepEqual(actual, []time.Duration{0, 16 * time.Second}) {
			t.Fatalf("tick/通知同时就绪时，轮次开始 = %v，期望 [0 12s] 或 [0 16s]", actual)
		}
		second := actual[1]
		t.Logf("tick/通知同时就绪时，第二轮开始 = %v", second)
		synctest.Sleep(second + 16*time.Second - time.Since(r.start))
		p323WantDurations(t, r, 0, second, second+16*time.Second)
	})
}

func TestP323IntervalChangeJustBeforeTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 0)
		defer r.close()
		synctest.Sleep(10*time.Second - time.Nanosecond)
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(4*time.Second - time.Nanosecond)
		r.want(t, 0)
		synctest.Sleep(time.Nanosecond)
		p323WantDurations(t, r, 0, 14*time.Second-time.Nanosecond)
	})
}

func TestP323QueuedManualRoundRemainsImmediate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(11 * time.Second)
		r.s.TriggerSync()
		r.save(true, 4*time.Second, "dark")
		synctest.Sleep(9 * time.Second)
		r.want(t, 0, 10, 13, 20)
	})
}

func TestP323FinalPauseSuppressesPendingRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		defer r.close()
		synctest.Sleep(time.Second)
		r.save(false, 4*time.Second, "dark")
		r.save(true, 4*time.Second, "light")
		r.save(false, 7*time.Second, "dark")
		r.s.TriggerSync()
		synctest.Sleep(30 * time.Second)
		r.want(t, 0)
		if r.s.IsEnabled() {
			t.Fatal("最终发布的暂停状态丢失")
		}
	})
}

func TestP323StopWithPendingUpdateAndTrigger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := p323New(t, true, 10*time.Second, 3*time.Second)
		synctest.Sleep(time.Second)
		r.save(true, 4*time.Second, "dark")
		r.s.TriggerSync()
		r.s.Stop()
		r.s.Wait()
		r.want(t, 0)
		if r.s.Status().Running {
			t.Fatal("Wait 返回后 Run 仍在运行")
		}
		if elapsed := time.Since(r.start); elapsed != 3*time.Second {
			t.Fatalf("Stop 中断了当前轮或等待过久：%v", elapsed)
		}
	})
}

// 失败轮也必须保留轮后等待，不能把 roundCompleted 误写成 success。
type p323FailProvider struct{ *targetProbeProvider }

func (p *p323FailProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	time.Sleep(3 * time.Second)
	return provider.RuleSnapshot{}, errors.New("fixture snapshot rejected")
}

func TestP323FailedRoundWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := gatedState(t, true, 10*time.Second)
		st.Providers = []provider.Provider{&p323FailProvider{newProbeProvider(config.CloudTCCVM, 0)}}
		r := &p323Rig{s: New(NewRuntimeManager(st)), start: time.Now()}
		r.s.sleepFn = func(time.Duration) {}
		r.s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) { return []dns.ResolvedIP{{IP: net.ParseIP("192.0.2.10")}}, nil }
		r.s.SetBeforeRoundHook(func() { r.mu.Lock(); r.rounds = append(r.rounds, time.Since(r.start)); r.mu.Unlock() })
		go r.s.Run()
		synctest.Wait()
		defer r.close()
		synctest.Sleep(11 * time.Second)
		r.save(true, 10*time.Second, "dark")
		synctest.Sleep(15 * time.Second)
		r.want(t, 0, 10, 23)
		if got := r.s.Status().LastRound; got == nil || got.Outcome != RoundFailed {
			t.Fatalf("轮次结果 = %+v，期望 failed", got)
		}
	})
}

func TestP323IdleRoundStillScheduled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := gatedState(t, true, 10*time.Second)
		r := &p323Rig{s: New(NewRuntimeManager(st)), start: time.Now()}
		r.s.SetBeforeRoundHook(func() { r.mu.Lock(); r.rounds = append(r.rounds, time.Since(r.start)); r.mu.Unlock() })
		go r.s.Run()
		synctest.Wait()
		defer r.close()
		synctest.Sleep(6 * time.Second)
		r.save(true, 10*time.Second, "dark")
		synctest.Sleep(14 * time.Second)
		r.want(t, 0, 10, 20)
		if got := r.s.Status().LastRound; got == nil || got.Outcome != RoundIdle {
			t.Fatalf("轮次结果 = %+v，期望 idle", got)
		}
	})
}
