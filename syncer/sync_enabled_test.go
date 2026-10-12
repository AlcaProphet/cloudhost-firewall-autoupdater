package syncer

import (
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// I8-R07：使用实例私有接缝，不在测试间共享可变的全局钩子。
func i8r07State(t *testing.T, enabled bool, fresh bool) *RuntimeState {
	t.Helper()
	rc := gatedState(t, enabled, time.Hour).Config.DeepCopy()
	rc.Targets = []config.TargetConfig{{ID: 7, CloudType: config.CloudTCCVM, Region: "ap-guangzhou", ResourceID: "sg-test-old"}}
	rc.DomainRules[0].Targets = []int{7}
	if fresh {
		rc.Tag = "fresh-tag"
		rc.Interval = 2 * time.Hour
		rc.DNS = "127.0.0.1:5353"
		rc.DNSTimeout = 3 * time.Second
		rc.DNSFailThreshold = 9
		rc.Theme = "dark"
		rc.Credentials.TencentSecretID = "fixture-new-id"
		rc.Credentials.TencentSecretKey = "fixture-new-key"
		rc.Targets[0].ResourceID = "sg-test-new"
		rc.DomainRules[0].Host = "fresh.invalid"
		rc.DomainRules[0].Comment = "fresh-comment"
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func i8r07Wait(t *testing.T, done <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("局部完成屏障未到达：%s", name)
	}
}

func i8r07Control(s *Syncer, enabled bool) {
	if enabled {
		s.Resume()
	} else {
		s.Pause()
	}
}

func i8r07WantState(t *testing.T, got, fresh *RuntimeState, enabled bool) {
	t.Helper()
	want := fresh.Config.DeepCopy()
	want.SyncEnabled = enabled
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("完整配置回滚或开关顺序错误：tag=%s interval=%s enabled=%t", got.Config.Tag, got.Config.Interval, got.Config.SyncEnabled)
	}
	if got.Pool != fresh.Pool || got.Resolver != fresh.Resolver || got.Breaker != fresh.Breaker || len(got.Providers) != len(fresh.Providers) {
		t.Fatal("运行时依赖被旧对象回滚")
	}
	for i := range fresh.Providers {
		if got.Providers[i] != fresh.Providers[i] {
			t.Fatal("Provider 被旧对象回滚")
		}
	}
}

// 读快照屏障故意放大旧实现允许的窗口；最终断言检查行为，不以锁状态判绿。
// Mutex 阻塞不属于 synctest 的 durable blocking，本测试使用有界真实 channel。
func TestI8R07ReadWindowPublish(t *testing.T) {
	for _, request := range []bool{false, true} {
		for _, fullEnabled := range []bool{false, true} {
			name := "pause"
			if request {
				name = "resume"
			}
			if fullEnabled {
				name += "_full_true"
			} else {
				name += "_full_false"
			}
			t.Run(name, func(t *testing.T) {
				old := i8r07State(t, !request, false)
				fresh := i8r07State(t, fullEnabled, true)
				s := New(NewRuntimeManager(old))
				reached, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				t.Cleanup(unblock)
				s.afterSyncEnabledReadHook = func() { close(reached); <-release }
				controlled, published := make(chan struct{}), make(chan struct{})
				go func() { i8r07Control(s, request); close(controlled) }()
				i8r07Wait(t, reached, "读快照")
				wantEnabled := fullEnabled
				if s.mu.TryLock() {
					// 旧实现：在读改写窗口内先完成完整发布，再放行开关更新。
					s.mu.Unlock()
					s.ApplyState(fresh)
					close(published)
					wantEnabled = request
				} else {
					// 同锁候选：完整发布只能在当前开关更新释放锁后完成。
					started := make(chan struct{})
					go func() { close(started); s.ApplyState(fresh); close(published) }()
					i8r07Wait(t, started, "完整发布 goroutine 启动")
				}
				unblock()
				i8r07Wait(t, controlled, "开关更新完成")
				i8r07Wait(t, published, "完整发布完成")
				i8r07WantState(t, s.Runtime().Snapshot(), fresh, wantEnabled)
				if !reflect.DeepEqual(old.Config, i8r07State(t, !request, false).Config) {
					t.Fatal("旧快照被修改")
				}
			})
		}
	}
}

func TestI8R07SerialOrder(t *testing.T) {
	for _, request := range []bool{false, true} {
		for _, controlFirst := range []bool{false, true} {
			s := New(NewRuntimeManager(i8r07State(t, !request, false)))
			fresh := i8r07State(t, !request, true)
			if controlFirst {
				i8r07Control(s, request)
				s.ApplyState(fresh)
				i8r07WantState(t, s.Runtime().Snapshot(), fresh, !request)
			} else {
				s.ApplyState(fresh)
				i8r07Control(s, request)
				i8r07WantState(t, s.Runtime().Snapshot(), fresh, request)
			}
		}
	}
}

func TestI8R07StateContract(t *testing.T) {
	s := New(NewRuntimeManager(nil))
	s.Pause()
	s.Resume()
	if s.Runtime().Snapshot() != nil || len(s.controlCh) != 0 {
		t.Fatal("空状态须无发布和通知")
	}
	old := i8r07State(t, false, true)
	s.ApplyState(old)
	<-s.controlCh
	s.Pause()
	if s.Runtime().Snapshot() != old || len(s.controlCh) != 0 {
		t.Fatal("同值操作必须不发布和通知")
	}
	s.Resume()
	got := s.Runtime().Snapshot()
	i8r07WantState(t, got, old, true)
	if !s.enabled || !s.pendingResume {
		t.Fatal("恢复镜像或恢复边沿丢失")
	}
	if len(s.controlCh) != 1 {
		t.Fatal("开关发布必须通知 Run")
	}
	if old.Config.SyncEnabled || &got.Config.Targets[0] == &old.Config.Targets[0] || &got.Config.DomainRules[0] == &old.Config.DomainRules[0] || &got.Config.DomainRules[0].Targets[0] == &old.Config.DomainRules[0].Targets[0] {
		t.Fatal("配置未深复制或旧状态被修改")
	}
	s.Resume()
	if s.Runtime().Snapshot() != got {
		t.Fatal("同值操作必须不发布和通知")
	}
	s.Pause()
	latest, pending := s.consumeControlState()
	if latest.Config.SyncEnabled || !pending || s.pendingResume || s.enabled {
		t.Fatal("最终暂停和边沿消费合同错误")
	}
}

// 真实 Run 与目标链，直接使用具体控制方法；轮内变化不能取消捕获旧快照的当前轮。
func TestI8R07ControlDuringRound(t *testing.T) {
	for _, scenario := range []string{"resume_then_save", "multiple_edges", "final_pause", "stopped"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := p324New(t, true, time.Hour, nil)
				r.s.Pause()
				r.s.Resume()
				want := 2
				switch scenario {
				case "resume_then_save":
					r.publish(true, 2*time.Hour, "dark")
				case "multiple_edges":
					r.s.Pause()
					r.s.Resume()
					r.s.Resume()
				case "final_pause":
					r.s.Pause()
					want = 1
				case "stopped":
					r.s.Stop()
					want = 1
				}
				r.finish()
				r.count(t, want)
				if want == 2 {
					r.finish()
				}
				synctest.Sleep(time.Minute)
				r.count(t, want)
			})
		})
	}
}
