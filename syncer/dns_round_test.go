package syncer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func TestDNSRound_TargetFlow(t *testing.T) {
	t.Run("five_failed_rounds_then_one_probe", func(t *testing.T) {
		ps := []provider.Provider{}
		for i := 1; i <= 5; i++ {
			ps = append(ps, newProbeProvider(config.CloudTCLighthouse, i))
		}
		s := newTargetSyncer(t, ps, []config.DomainRule{staticRule(1, "a.test", "TCP", "443")}, nil)
		var n atomic.Int32
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) { n.Add(1); return nil, context.DeadlineExceeded }
		cb := s.runtime.Snapshot().Breaker
		for i := 1; i <= 5; i++ {
			s.syncAll()
			if cb.IsOpen("a.test") != (i == 5) {
				t.Fatalf("round %d: unexpected open state", i)
			}
		}
		if n.Load() != 75 {
			t.Fatalf("normal calls=%d want75", n.Load())
		}
		ch, cancel := s.bus.SubscribeChan()
		defer cancel()
		s.syncAll()
		if n.Load() != 76 {
			t.Fatalf("open round adds %d calls want1", n.Load()-75)
		}
		events := 0
	drain:
		for {
			select {
			case e := <-ch:
				if e.Type == notifier.EventDNSFailed {
					events++
				}
			default:
				break drain
			}
		}
		if events != 5 || s.Status().LastRound.Failed != 5 {
			t.Fatalf("events=%d summary=%+v", events, s.Status().LastRound)
		}
	})
	t.Run("mixed_order_is_success_dominant_without_hiding_target_failure", func(t *testing.T) {
		for _, failFirst := range []bool{true, false} {
			ps := []provider.Provider{newProbeProvider(config.CloudTCLighthouse, 1), newProbeProvider(config.CloudTCLighthouse, 2)}
			s := newTargetSyncer(t, ps, []config.DomainRule{staticRule(1, "a.test", "TCP", "443")}, nil)
			cb := s.runtime.Snapshot().Breaker
			cb.SetThreshold(2)
			cb.RecordFailure("a.test")
			n := 0
			s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
				n++
				if (n == 1) == failFirst {
					return nil, errors.New("NXDOMAIN")
				}
				return []dns.ResolvedIP{mockResolved("1.2.3.4/32")}, nil
			}
			s.syncAll()
			cb.SetThreshold(1)
			if cb.IsOpen("a.test") || s.Status().LastRound.Failed != 1 {
				t.Fatalf("order=%v breaker/health wrong", failFirst)
			}
		}
	})
	t.Run("open_cross_cloud_probe_dry_run_and_mixed_rule_safety", func(t *testing.T) {
		stale := config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"}
		p1 := newProbeProvider(config.CloudTCLighthouse, 1, stale)
		p2 := newProbeProvider(config.CloudAliSWAS, 2, stale)
		rules := []config.DomainRule{staticRule(1, "bad.test", "TCP", "443"), staticRule(2, "good.test", "TCP", "80")}
		s := newTargetSyncer(t, []provider.Provider{p1, p2}, rules, nil)
		cb := s.runtime.Snapshot().Breaker
		cb.SetThreshold(1)
		cb.RecordFailure("bad.test")
		var bad atomic.Int32
		entered := make(chan struct{})
		release := make(chan struct{})
		releaseProbe := sync.OnceFunc(func() { close(release) })
		defer releaseProbe()
		s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
			if host == "bad.test" {
				if bad.Add(1) == 1 {
					close(entered)
					<-release
				}
				return nil, context.DeadlineExceeded
			}
			return []dns.ResolvedIP{mockResolved("1.2.3.4/32")}, nil
		}
		finished := make(chan struct{})
		go func() { s.syncAll(); close(finished) }()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("probe did not enter")
		}
		if _, err := s.DryRun(); err != nil {
			t.Fatal(err)
		}
		if bad.Load() != 3 {
			t.Fatalf("Dry Run should independently resolve 2 targets, calls=%d", bad.Load())
		}
		releaseProbe()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("waiter stuck")
		}
		if bad.Load() != 3 {
			t.Fatalf("formal sync exceeded one probe: calls=%d", bad.Load())
		}
		for _, p := range []*targetProbeProvider{p1, p2} {
			_, creates, deletes := p.counts()
			if creates != 1 || deletes != 0 || !p.hasRule("10.9.9.9/32", "9999") {
				t.Fatalf("mixed safety creates=%d deletes=%d", creates, deletes)
			}
		}
		if s.Status().LastRound.Failed != 2 {
			t.Fatal("DNS failure hidden")
		}
	})
	t.Run("probe_success_version_retry_gets_changed_ip", func(t *testing.T) {
		p := newProbeProvider(config.CloudTCLighthouse, 1)
		p.createErrs = []error{errors.New("UnsupportedOperation.FirewallVersionMismatch")}
		s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.test", "TCP", "443")}, nil)
		cb := s.runtime.Snapshot().Breaker
		cb.SetThreshold(1)
		cb.RecordFailure("a.test")
		n := 0
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			n++
			cidr := "1.2.3.4/32"
			if n > 1 {
				cidr = "5.6.7.8/32"
			}
			return []dns.ResolvedIP{mockResolved(cidr)}, nil
		}
		s.syncAll()
		if n != 2 || !p.hasRule("5.6.7.8/32", "443") || cb.IsOpen("a.test") || s.Status().LastRound.Failed != 0 {
			t.Fatal("success result was reused or recovery failed")
		}
	})
	t.Run("canonical_clone_isolation_and_import_reset", func(t *testing.T) {
		s := newTargetSyncer(t, nil, []config.DomainRule{staticRule(1, "A.test", "TCP", "443")}, nil)
		old := s.runtime.Snapshot()
		old.Breaker.SetThreshold(1)
		old.Breaker.RecordFailure("a.test")
		rc := old.Config.DeepCopy()
		rc.DNSFailThreshold = 1
		next, err := BuildRuntimeState(old, rc, BreakerPreserve)
		if err != nil {
			t.Fatal(err)
		}
		if !next.Breaker.IsOpen("A.test") {
			t.Fatal("canonical clone lost state")
		}
		old.Breaker.RecordSuccess("a.test")
		if !next.Breaker.IsOpen("a.test") {
			t.Fatal("old snapshot mutated new state")
		}
		reset, err := BuildRuntimeState(next, rc, BreakerReset)
		if err != nil || reset.Breaker.IsOpen("a.test") {
			t.Fatal("import reset failed")
		}
	})
	t.Run("no_rules_does_not_probe_or_change_state", func(t *testing.T) {
		s := newTargetSyncer(t, []provider.Provider{newProbeProvider(config.CloudTCLighthouse, 1)}, nil, nil)
		cb := s.runtime.Snapshot().Breaker
		cb.SetThreshold(1)
		cb.RecordFailure("a.test")
		s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
			t.Error("unexpected resolve")
			return nil, errors.New("unexpected")
		}
		s.syncAll()
		if !cb.IsOpen("a.test") || s.Status().LastRound.Total != 0 {
			t.Fatal("idle changed state")
		}
	})
}

// 本地真实 UDP 上游保持接收但不回应，使用生产 Resolver 的整体超时。
func TestDNSRound_RealUDP(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var packets atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		for {
			_, _, err := conn.ReadFrom(buf)
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					t.Errorf("UDP read: %v", err)
				}
				return
			}
			packets.Add(1)
		}
	}()
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("UDP close: %v", err)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("UDP listener not reclaimed")
		}
	})
	ps := []provider.Provider{newProbeProvider(config.CloudTCLighthouse, 1), newProbeProvider(config.CloudTCLighthouse, 2)}
	s := newTargetSyncer(t, ps, []config.DomainRule{staticRule(1, "blocked.example.test.", "TCP", "443")}, nil)
	st := s.runtime.Snapshot()
	st.Resolver = dns.NewResolver(conn.LocalAddr().String(), 150*time.Millisecond)
	st.Breaker.SetThreshold(1)
	st.Breaker.RecordFailure("blocked.example.test.")
	s.resolveHostFn = nil
	started := time.Now()
	s.syncAll()
	elapsed := time.Since(started)
	t.Logf("actual UDP packets=%d elapsed=%s failed=%d", packets.Load(), elapsed, s.Status().LastRound.Failed)
	if packets.Load() != 2 || s.Status().LastRound.Failed != 2 {
		t.Fatalf("one A + one AAAA probe expected; got %d", packets.Load())
	}
}

func TestDNSRound_ConcurrentProbe(t *testing.T) {
	for _, success := range []bool{false, true} {
		s := newTargetSyncer(t, nil, []config.DomainRule{staticRule(1, "a.test", "TCP", "443")}, nil)
		st := s.runtime.Snapshot()
		st.Breaker.SetThreshold(1)
		st.Breaker.RecordFailure("a.test")
		r := newDNSRound(st)
		var n atomic.Int32
		entered := make(chan struct{})
		release := make(chan struct{})
		releaseProbe := sync.OnceFunc(func() { close(release) })
		defer releaseProbe()
		var wg sync.WaitGroup
		results := make(chan string, 32)
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ips, err := r.resolve("A.test", func() ([]dns.ResolvedIP, error) {
					k := n.Add(1)
					if k == 1 {
						close(entered)
						<-release
					}
					if !success {
						return nil, context.DeadlineExceeded
					}
					return []dns.ResolvedIP{mockResolved(fmt.Sprintf("10.0.0.%d/32", k))}, nil
				})
				if success {
					if err != nil || len(ips) != 1 {
						t.Error("success lost")
						return
					}
					results <- ips[0].CIDR()
				} else if !errors.Is(err, context.DeadlineExceeded) {
					t.Error("timeout cause lost")
				}
			}()
		}
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("probe absent")
		}
		releaseProbe()
		finished := make(chan struct{})
		go func() { wg.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("waiters stuck")
		}
		r.finish()
		want := int32(1)
		if success {
			want = 32
		}
		if n.Load() != want {
			t.Fatalf("success=%v calls=%d want=%d", success, n.Load(), want)
		}
		if st.Breaker.IsOpen("a.test") == success {
			t.Fatal("wrong recovered state")
		}
		if success {
			if t.Failed() {
				return
			}
			seen := map[string]bool{}
			for i := 0; i < 32; i++ {
				seen[<-results] = true
			}
			if len(seen) != 32 {
				t.Fatal("waiters reused successful IP")
			}
		}
	}
}

func TestDNSRound_FailureExpiresAndLatePublishIsIsolated(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.test", "TCP", "443")}, nil)
	st := s.runtime.Snapshot()
	st.Breaker.SetThreshold(1)
	st.Breaker.RecordFailure("a.test")
	n := 0
	s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
		n++
		if n == 1 {
			return nil, context.DeadlineExceeded
		}
		return []dns.ResolvedIP{mockResolved("1.2.3.4/32")}, nil
	}
	s.syncAll()
	if n != 1 || s.Status().LastRound.Failed != 1 {
		t.Fatal("first round unexpected")
	}
	s.syncAll()
	if n != 2 || s.Status().LastRound.Failed != 0 || st.Breaker.IsOpen("a.test") {
		t.Fatal("failed probe carried over to next round")
	}
	// 候选发布发生在旧轮次失败提交之前，晚到计数只写旧 breaker。
	r := newDNSRound(st)
	_, err := r.resolve("a.test", func() ([]dns.ResolvedIP, error) { return nil, context.DeadlineExceeded })
	if err == nil {
		t.Fatal("expected timeout")
	}
	rc := st.Config.DeepCopy()
	rc.DNSFailThreshold = 1
	next, err := BuildRuntimeState(st, rc, BreakerPreserve)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime.Apply(next)
	r.finish()
	if !st.Breaker.IsOpen("a.test") || next.Breaker.IsOpen("a.test") {
		t.Fatal("old round late failure contaminated publication")
	}
}

// 解析返回空结果不能被视为恢复；IPv6 过滤则仍交由目标规划器判定。
func TestDNSRound_EmptyResultDoesNotRecover(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	s := newTargetSyncer(t, []provider.Provider{p}, []config.DomainRule{staticRule(1, "a.test", "TCP", "443")}, nil)
	cb := s.runtime.Snapshot().Breaker
	cb.SetThreshold(2)
	cb.RecordFailure("a.test")
	s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) { return nil, nil }
	s.syncAll()
	if !cb.IsOpen("a.test") || s.Status().LastRound.Failed != 1 {
		t.Fatal("空解析结果错误地解除了熔断")
	}
	_, creates, deletes := p.counts()
	if creates != 0 || deletes != 0 {
		t.Fatal("空解析结果触发云端写入")
	}
}

// TestDNSRound_FilteredIPv6DoesNotCountFailure 区分解析上游成功与单条规则不可用。
func TestDNSRound_FilteredIPv6DoesNotCountFailure(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	rules := []config.DomainRule{staticRule(1, "a.test", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.test": "2001:db8::1/128"})
	cb := s.runtime.Snapshot().Breaker
	cb.SetThreshold(2)
	cb.RecordFailure("a.test")
	ch, cancel := s.bus.SubscribeChan()
	defer cancel()
	s.syncAll()
	cb.SetThreshold(1)
	if cb.IsOpen("a.test") || s.Status().LastRound.Failed != 1 {
		t.Fatal("IPv6 规则过滤混淆了 DNS 上游恢复与目标失败")
	}
	_, creates, deletes := p.counts()
	if creates != 0 || deletes != 0 {
		t.Fatal("无可用地址的目标触发写入")
	}
	for {
		select {
		case event := <-ch:
			if event.Type == notifier.EventDNSFailed {
				t.Fatal("IPv6 规则过滤错误地产生 DNS 上游失败事件")
			}
		default:
			return
		}
	}
}
