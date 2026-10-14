package notifier

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestI8R10IngressExplicitInlineOutsideBusLock(t *testing.T) {
	bus := NewEventBus()
	var calls atomic.Int32
	var locked atomic.Bool
	cancel := bus.SubscribeIngress(EventTargetSyncComplete, func(e Event) {
		calls.Add(1)
		if !bus.mu.TryLock() {
			locked.Store(true)
		} else {
			bus.mu.Unlock()
		}
	})
	bus.Publish(Event{Type: EventTargetSyncComplete})
	if calls.Load() != 1 || locked.Load() {
		t.Fatalf("ingress not inline/outside lock: calls=%d locked=%v", calls.Load(), locked.Load())
	}
	bus.Publish(Event{Type: EventSyncError})
	if calls.Load() != 1 {
		t.Fatal("wrong event delivered")
	}
	cancel()
	cancel()
	bus.Publish(Event{Type: EventTargetSyncComplete})
	if calls.Load() != 1 {
		t.Fatal("canceled registration still delivered")
	}
}

func TestI8R10IngressCancelSnapshotAndConcurrentRegistrations(t *testing.T) {
	bus := NewEventBus()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	cancel := bus.SubscribeIngress(EventSyncError, func(Event) { close(entered); <-release })
	published := make(chan struct{})
	go func() { bus.Publish(Event{Type: EventSyncError}); close(published) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("ingress not entered")
	}
	select {
	case <-published:
		t.Fatal("Publish returned before inline ingress completed")
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	once.Do(func() { close(release) })
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("snapshot callback not released")
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				c := bus.SubscribeIngress(EventSyncError, func(Event) {})
				bus.Publish(Event{Type: EventSyncError})
				c()
			}
		}()
	}
	wg.Wait()
}
