package notifier

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type r19Subscriber struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (s *r19Subscriber) OnEvent(Event) error {
	s.calls.Add(1)
	s.entered <- struct{}{}
	<-s.release
	return nil
}

func TestI8R19BusDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := NewEventBus()
		old := &r19Subscriber{entered: make(chan struct{}, 1), release: make(chan struct{})}
		fresh := &r19Subscriber{entered: make(chan struct{}, 1), release: make(chan struct{})}
		var oldRelease, freshRelease sync.Once
		defer oldRelease.Do(func() { close(old.release) })
		defer freshRelease.Do(func() { close(fresh.release) })
		b.Subscribe(EventSyncError, old)
		b.Publish(Event{Type: EventSyncError})
		<-old.entered
		b.Unsubscribe(EventSyncError, old)
		b.Subscribe(EventSyncError, fresh)
		b.Publish(Event{Type: EventSyncError})
		<-fresh.entered
		stops := make(chan struct{}, 4)
		for range 4 {
			go func() { b.DrainSubscribers(); stops <- struct{}{} }()
		}
		synctest.Wait()
		if len(stops) != 0 {
			t.Error("I8R19: drain 未等待旧配置与新配置回调")
		}
		freshRelease.Do(func() { close(fresh.release) })
		synctest.Wait()
		if len(stops) != 0 {
			t.Error("I8R19: drain 漏等已取消订阅的旧回调")
		}
		oldRelease.Do(func() { close(old.release) })
		for range 4 {
			<-stops
		}
		// 普通回调的冻结不得抢先关闭 SSE 或日志 ingress。
		ch, unsub := b.SubscribeChan()
		defer unsub()
		var ingress int
		cancel := b.SubscribeIngress(EventSyncError, func(Event) { ingress++ })
		defer cancel()
		b.Publish(Event{Type: EventSyncError})
		b.DrainSubscribers()
		synctest.Wait()
		if old.calls.Load() != 1 || fresh.calls.Load() != 1 || ingress != 1 {
			t.Fatal("I8R19: 冻结边界或独立 ingress 被破坏")
		}
		select {
		case <-ch:
		default:
			t.Fatal("I8R19: 普通回调 drain 阻止了 channel 投递")
		}
	})
}

func TestI8R19DiagnosticsLifecycle(t *testing.T) {
	for _, mode := range []string{"unused", "pending", "writing"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				capture := p315Logs(t)
				l := NewInFlightLimiter(4)
				if mode != "unused" {
					l.logDropped("email", EventSyncError)
					l.logDropped("email", EventSyncError)
				}
				var release sync.Once
				if mode == "writing" {
					capture.mu.Lock()
					capture.entered = make(chan struct{}, 1)
					capture.block = make(chan struct{})
					capture.mu.Unlock()
					defer release.Do(func() { close(capture.block) })
					time.Sleep(dropLogWindow)
					<-capture.entered
				}
				stops := make(chan struct{}, 4)
				for range 4 {
					go func() { l.StopDiagnostics(); stops <- struct{}{} }()
				}
				synctest.Wait()
				if mode == "writing" {
					if len(stops) != 0 {
						t.Error("I8R19: 诊断回调输出未完成就返回")
					}
					release.Do(func() { close(capture.block) })
				}
				for range 4 {
					<-stops
				}
				l.StopDiagnostics()
				before := len(capture.copy())
				time.Sleep(2 * dropLogWindow)
				l.logDropped("email", EventSyncError)
				synctest.Wait()
				want := map[string]int{"unused": 0, "pending": 1, "writing": 2}[mode]
				if before != want || len(capture.copy()) != want {
					t.Error("I8R19: 退出新增了强制汇总、晚到日志或续约")
				}
			})
		})
	}
}
