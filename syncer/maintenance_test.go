package syncer

import (
	"context"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

func TestBuild9S5MaintenanceWaitsForRoundAndBlocksNewRounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newCountingProvider(config.CloudTCCVM, true)
		s := newControlSyncer(t, p, time.Hour)
		s.resolveHostFn = step8Resolve(s.runtime.Snapshot())
		next := *s.runtime.Snapshot()
		next.Config = next.Config.DeepCopy()
		next.Config.SyncEnabled = true
		s.ApplyState(&next)
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		<-p.started
		entered, release := make(chan struct{}), make(chan struct{})
		if err := s.QueueMaintenance(func() { close(entered); <-release }); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case <-entered:
			t.Fatal("在途轮未结束就执行维护")
		default:
		}
		p.release <- struct{}{}
		<-entered
		before := p.calls.Load()
		if before < 2 {
			t.Fatalf("同步轮未收尾 calls=%d", before)
		}
		s.TriggerSync()
		synctest.Sleep(time.Second)
		synctest.Wait()
		if p.calls.Load() != before {
			t.Fatal("维护期间新轮并发访问云")
		}
		close(release)
		synctest.Sleep(200 * time.Millisecond)
		synctest.Wait()
		if p.calls.Load() <= before {
			t.Fatal("维护结束后手动轮未执行")
		}
	})
}

type s5BlockingTEO struct {
	step10TEO
	entered, release chan struct{}
}

func (f *s5BlockingTEO) ConfirmOriginACLUpdate(ctx context.Context, zone string, admit func() bool) (string, error) {
	if !admit() {
		return "", &provider.TEOError{Kind: provider.TEOErrorCanceled, NotSent: true}
	}
	close(f.entered)
	<-f.release
	// 该请求已经准入，即使维护排队也必须自然完成。
	return f.step10TEO.ConfirmOriginACLUpdate(ctx, zone, func() bool { return true })
}
func TestBuild9S5MaintenanceWaitsForTEOConfirmation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newProbeProvider(config.CloudTCCVM, 0)
		teo := &s5BlockingTEO{entered: make(chan struct{}), release: make(chan struct{})}
		state := step10State(t, p, teo)
		s := New(NewRuntimeManager(state))
		store := newStep10Store()
		s.SetEdgeOneTransitionStore(store)
		go s.Run()
		defer func() { s.Stop(); s.Wait() }()
		<-teo.entered
		maintenance := make(chan struct{})
		if err := s.QueueMaintenance(func() { close(maintenance) }); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case <-maintenance:
			t.Fatal("TEO 确认在途时维护已开始")
		default:
		}
		close(teo.release)
		<-maintenance
		if _, confirms := teo.counts(); confirms != 1 {
			t.Fatal("已准入确认未收尾")
		}
		row, ok := store.row("zone-a")
		if !ok || row.ConfirmationState != config.EdgeOneConfirmationAccepted {
			t.Fatalf("row=%+v", row)
		}
	})
}

func TestBuild9S5InvalidateCoveragePreservesTransitions(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 0)
	state := step10State(t, p, &step10TEO{})
	s := New(NewRuntimeManager(state))
	store := newStep10Store()
	s.SetEdgeOneTransitionStore(store)
	proof := step10Proofs(t, s, state)["zone-a"]
	s.storeSiteProofs(map[string]SiteCoverage{"zone-a": proof})
	other := proof
	other.TargetKey = "other"
	s.storeSiteProofs(map[string]SiteCoverage{"zone-b": other})
	st := EdgeOneSiteStatus{ZoneID: "zone-a", ConfirmationState: config.EdgeOneConfirmationAccepted, ConfirmationID: "req-confirm", Coverage: []SiteTargetCoverage{{TargetKey: proof.TargetKey, Complete: true}, {TargetKey: "other", Complete: true}}, ProtectedCIDRs: []string{"192.0.2.0/24"}}
	s.edgeStatus = map[string]EdgeOneSiteStatus{"zone-a": st}
	fp := [32]byte{7}
	s.confirmedMem = map[string][32]byte{"zone-a": fp}
	s.unsavedTransitions = map[string]config.EdgeOneTransition{"zone-a": {ZoneID: "zone-a", ConfirmationState: config.EdgeOneConfirmationUnknown}}
	beforeMem := s.unsavedTransitions["zone-a"]
	s.InvalidateTargetCoverage(proof.TargetKey)
	if proofs := s.RoundCoverage(); len(proofs) != 1 {
		t.Fatalf("proofs=%+v", proofs)
	}
	got := s.edgeStatus["zone-a"]
	if got.Coverage[0].Complete || !got.Coverage[1].Complete || !reflect.DeepEqual(got.Coverage[0].Reasons, []string{"manual_cleanup_revalidation_required"}) {
		t.Fatalf("coverage=%+v", got.Coverage)
	}
	if got.ConfirmationID != st.ConfirmationID || got.ConfirmationState != st.ConfirmationState || !reflect.DeepEqual(got.ProtectedCIDRs, st.ProtectedCIDRs) || s.confirmedMem["zone-a"] != fp || !reflect.DeepEqual(s.unsavedTransitions["zone-a"], beforeMem) {
		t.Fatal("清理改写了确认或过渡状态")
	}
}

func TestBuild9S5MaintenanceLifecycleAndCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := gatedState(t, false, time.Hour)
		s := New(NewRuntimeManager(state))
		if err := s.QueueMaintenance(func() {}); err == nil {
			t.Fatal("尚未运行就受理")
		}
		go s.Run()
		<-s.Started()
		first, release := make(chan struct{}), make(chan struct{})
		second := make(chan bool, 1)
		if err := s.QueueMaintenance(func() { close(first); <-release }); err != nil {
			t.Fatal(err)
		}
		<-first
		if err := s.QueueMaintenance(func() { second <- s.MaintenanceAllowed() }); err != nil {
			t.Fatal(err)
		}
		s.Stop()
		if err := s.QueueMaintenance(func() {}); err == nil {
			t.Fatal("Stop 后受理")
		}
		close(release)
		s.Wait()
		if <-second {
			t.Fatal("Stop 后排队任务仍取得删除准入")
		}
		if s.Status().Running {
			t.Fatal("引擎未停止")
		}
		// 冷却沿用正式时间表，等待在锁外，首个目标不等待。
		s2 := New(NewRuntimeManager(state))
		start := time.Now()
		s2.WaitMaintenanceCloud(config.CloudTCLighthouse)
		if time.Since(start) != 0 {
			t.Fatal("首次读取额外等待")
		}
		s2.FinishMaintenanceCloud(config.CloudTCLighthouse)
		s2.WaitMaintenanceCloud(config.CloudTCLighthouse)
		if time.Since(start) != 5*time.Second {
			t.Fatalf("cooldown=%s", time.Since(start))
		}
	})
}
