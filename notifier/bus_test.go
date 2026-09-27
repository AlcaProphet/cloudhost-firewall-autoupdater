package notifier

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockSubscriber struct {
	mu     sync.Mutex
	events []Event
}

func (m *mockSubscriber) OnEvent(event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

// count 返回已记录的事件数（并发安全）。
func (m *mockSubscriber) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

// failingSubscriber 回调固定返回错误的订阅者：用于验证 Subscriber 错误隔离
// （Build6 Step 7：错误只记录 WARN，不影响同类型其他订阅者，也不破坏总线可用性）。
type failingSubscriber struct {
	calls  atomic.Int32
	notify chan struct{}
}

func (s *failingSubscriber) OnEvent(Event) error {
	s.calls.Add(1)
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return errors.New("订阅者故意失败")
}

func TestEventBus_Publish(t *testing.T) {
	bus := NewEventBus()
	sub := &mockSubscriber{}

	bus.Subscribe(EventSyncComplete, sub)

	bus.Publish(Event{
		Type:      EventSyncComplete,
		Timestamp: time.Now(),
		Data:      map[string]any{"domain": "example.com"},
	})

	// 等待异步投递完成
	time.Sleep(100 * time.Millisecond)

	sub.mu.Lock()
	defer sub.mu.Unlock()
	if len(sub.events) != 1 {
		t.Fatalf("收到事件数 = %d, want 1", len(sub.events))
	}
	if sub.events[0].Type != EventSyncComplete {
		t.Errorf("事件类型 = %s, want sync:complete", sub.events[0].Type)
	}
}

func TestEventBus_NoCrossTalk(t *testing.T) {
	bus := NewEventBus()
	sub := &mockSubscriber{}

	// 只订阅 sync:error
	bus.Subscribe(EventSyncError, sub)

	// 发布 sync:complete（不应收到）
	bus.Publish(Event{Type: EventSyncComplete, Timestamp: time.Now()})

	time.Sleep(100 * time.Millisecond)

	sub.mu.Lock()
	defer sub.mu.Unlock()
	if len(sub.events) != 0 {
		t.Errorf("不应收到非订阅类型的事件, got %d", len(sub.events))
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()
	sub := &mockSubscriber{}

	bus.Subscribe(EventSyncError, sub)
	bus.Unsubscribe(EventSyncError, sub)

	// 发布不应收到的事件
	bus.Publish(Event{Type: EventSyncError, Timestamp: time.Now()})
	time.Sleep(100 * time.Millisecond)

	sub.mu.Lock()
	defer sub.mu.Unlock()
	if len(sub.events) != 0 {
		t.Errorf("取消订阅后不应收到事件, got %d", len(sub.events))
	}
}

func TestEventBus_UnsubscribeIdempotent(t *testing.T) {
	bus := NewEventBus()
	sub := &mockSubscriber{}

	// 对未订阅的类型取消订阅 — 不应 panic
	bus.Unsubscribe(EventSyncComplete, sub)

	// 重复取消 — 不应 panic
	bus.Subscribe(EventSyncError, sub)
	bus.Unsubscribe(EventSyncError, sub)
	bus.Unsubscribe(EventSyncError, sub) // 幂等
}

func TestEventBus_UnsubscribeOnlyTargetType(t *testing.T) {
	bus := NewEventBus()
	sub := &mockSubscriber{}

	bus.Subscribe(EventSyncError, sub)
	bus.Subscribe(EventDNSFailed, sub)

	// 仅取消 sync:error 的订阅
	bus.Unsubscribe(EventSyncError, sub)

	// 发布 dns:failed — 应收到
	bus.Publish(Event{Type: EventDNSFailed, Timestamp: time.Now()})
	time.Sleep(100 * time.Millisecond)

	sub.mu.Lock()
	defer sub.mu.Unlock()
	if len(sub.events) != 1 {
		t.Errorf("dns:failed 应收到事件, got %d", len(sub.events))
	}
}

// ─── Step 1（R5-02）：channel 订阅取消竞态回归测试 ───
//
// 固定口径（Build6 §12.14）：取消订阅只从订阅表删除记录，不关闭 channel；
// 已取得快照的一个或多个并发 Publish 仍可能非阻塞投递，不承诺"最多一个在途事件"。

// blockingSubscriber 回调阻塞直到 release 关闭的订阅者（用于验证回调在锁外异步执行）
type blockingSubscriber struct {
	entered chan struct{} // 回调进入后非阻塞通知（容量 1）
	release chan struct{} // 关闭后回调返回
}

func (s *blockingSubscriber) OnEvent(Event) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

// drainOpen 非阻塞抽干 channel；发现 channel 已关闭时返回 false
func drainOpen(ch <-chan Event) bool {
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return false
			}
		default:
			return true
		}
	}
}

// TestEventBus_CancelDoesNotCloseChannel 取消订阅后 channel 必须仍然打开且可写
// 回归点：关闭 channel 会让"已取得快照、尚未投递"的在途 Publish 触发 send on closed channel panic
func TestEventBus_CancelDoesNotCloseChannel(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.SubscribeChan()

	// 取到底层双向 channel（Publish 快照中保存的正是它），用于验证取消后仍可非阻塞投递
	bus.mu.RLock()
	var raw chan Event
	for _, c := range bus.chanSubs {
		raw = c
	}
	bus.mu.RUnlock()
	if raw == nil {
		t.Fatal("订阅表未记录 channel 订阅者")
	}

	cancel()

	// channel 被关闭时，这里的接收会立刻返回 ok=false
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("取消订阅后 channel 被关闭（应只删除订阅表记录）")
		}
	default:
	}

	// 仍可写入：等价证明在途快照的非阻塞投递不会 panic（channel 容量 32）
	select {
	case raw <- Event{Type: EventSyncComplete, Timestamp: time.Now()}:
	default:
		t.Fatal("取消订阅后 channel 不可写，缓冲容量异常")
	}
	if _, ok := <-ch; !ok {
		t.Fatal("在途投递后 channel 被关闭")
	}

	// 订阅表已删除该记录
	bus.mu.RLock()
	n := len(bus.chanSubs)
	bus.mu.RUnlock()
	if n != 0 {
		t.Fatalf("取消订阅后订阅表记录数 = %d, want 0", n)
	}
}

// TestEventBus_InFlightSnapshotStillDelivers 取消订阅后，已取得快照的在途投递仍能完成
// 按 Publish 的同一锁协议构造"快照已取得、投递尚未发生"的状态；不限制在途事件数量
func TestEventBus_InFlightSnapshotStillDelivers(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.SubscribeChan()

	bus.mu.RLock()
	snapshot := make([]chan Event, 0, len(bus.chanSubs))
	for _, c := range bus.chanSubs {
		snapshot = append(snapshot, c)
	}
	bus.mu.RUnlock()

	cancel()

	delivered := 0
	for _, c := range snapshot {
		select {
		case c <- Event{Type: EventSyncComplete, Timestamp: time.Now()}:
			delivered++
		default:
		}
	}
	if delivered != 1 {
		t.Fatalf("在途快照投递成功数 = %d, want 1", delivered)
	}
	select {
	case <-ch:
	default:
		t.Fatal("在途快照投递的事件未被 channel 接收")
	}
}

// TestEventBus_PublishAfterCancelNotDelivered 取消订阅后，新的 Publish 快照不再包含该订阅
func TestEventBus_PublishAfterCancelNotDelivered(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.SubscribeChan()

	cancel()
	bus.Publish(Event{Type: EventSyncComplete, Timestamp: time.Now()})

	// Publish 内部为同步非阻塞投递，返回后即可判定
	select {
	case ev := <-ch:
		t.Fatalf("取消订阅后不应再投递事件, got %s", ev.Type)
	default:
	}
}

// TestEventBus_CancelIdempotentConcurrent 同一取消函数的重复与并发调用保持幂等
func TestEventBus_CancelIdempotentConcurrent(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.SubscribeChan()

	const goroutines = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cancel()
		}()
	}
	close(start)
	wg.Wait()
	cancel() // 串行重复调用

	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("重复/并发取消后 channel 被关闭")
		}
	default:
	}

	bus.mu.RLock()
	n := len(bus.chanSubs)
	bus.mu.RUnlock()
	if n != 0 {
		t.Fatalf("取消订阅后订阅表记录数 = %d, want 0", n)
	}
}

// TestEventBus_PublishCancelConcurrent 并发 Publish 与取消订阅：不 panic、不阻塞、不关闭 channel
// 注意：不断言"最多一个在途事件"——并发 Publish 可各自独立取得快照，事件数量允许 0..publishers
func TestEventBus_PublishCancelConcurrent(t *testing.T) {
	bus := NewEventBus()
	const (
		rounds     = 100
		publishers = 4
	)
	for i := 0; i < rounds; i++ {
		ch, cancel := bus.SubscribeChan()

		start := make(chan struct{})
		var wg sync.WaitGroup
		for j := 0; j < publishers; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				bus.Publish(Event{Type: EventSyncComplete, Timestamp: time.Now()})
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cancel()
		}()
		close(start)
		wg.Wait()

		if !drainOpen(ch) {
			t.Fatalf("第 %d 轮：取消订阅后 channel 被关闭", i)
		}
		cancel() // 轮次结束后再次调用取消函数仍须幂等
	}
}

// TestEventBus_InterfaceSubscribeConcurrentWithPublish 接口订阅 slice 与 Publish 并发修改
// 回归点：Publish 必须在读锁内复制接口订阅者 slice，否则与 Unsubscribe 的原地搬移、Subscribe 的原地追加竞态
func TestEventBus_InterfaceSubscribeConcurrentWithPublish(t *testing.T) {
	bus := NewEventBus()
	const (
		subscribers = 4
		iterations  = 200
	)
	subs := make([]*mockSubscriber, subscribers)
	for i := range subs {
		subs[i] = &mockSubscriber{}
		bus.Subscribe(EventSyncComplete, subs[i])
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				bus.Publish(Event{Type: EventSyncComplete, Timestamp: time.Now()})
			}
		}()
	}
	for _, sub := range subs {
		wg.Add(1)
		go func(s *mockSubscriber) {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				bus.Unsubscribe(EventSyncComplete, s)
				bus.Subscribe(EventSyncComplete, s)
			}
		}(sub)
	}
	close(start)
	wg.Wait()

	// 压力阶段不做数量断言：订阅窗口重叠，且 Publish 取得的在途快照会在压力结束后
	// 继续异步投递（§12.14 明确不承诺"最多一个在途事件"），因此不能断言
	// "取消后旧订阅者不再收到任何事件"。
	// 这里只断言读取快照路径确实投递过事件，避免该回归用例在非 -race 下"零断言"通过。
	var delivered int
	for _, s := range subs {
		delivered += s.count()
	}
	if delivered == 0 {
		t.Fatal("并发订阅/取消期间没有任何投递，读取快照路径可能已失效")
	}

	// 压力结束后总线必须仍然可用：新订阅者能收到后续事件
	fresh := &mockSubscriber{}
	bus.Subscribe(EventSyncComplete, fresh)
	bus.Publish(Event{Type: EventSyncComplete, Timestamp: time.Now()})

	deadline := time.Now().Add(5 * time.Second)
	for fresh.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := fresh.count(); got != 1 {
		t.Fatalf("压力结束后新订阅者投递数 = %d, want 1", got)
	}
}

// TestEventBus_FullBufferDoesNotBlock 缓冲已满的慢 channel：非阻塞投递，满则跳过本次事件
func TestEventBus_FullBufferDoesNotBlock(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.SubscribeChan()
	defer cancel()

	published := cap(ch) * 2
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < published; i++ {
			bus.Publish(Event{Type: EventSyncComplete, Timestamp: time.Now()})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("缓冲已满时 Publish 被阻塞")
	}

	received := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				t.Fatal("慢 channel 的缓冲不应被关闭")
			}
			received++
			continue
		default:
		}
		break
	}
	if received != cap(ch) {
		t.Errorf("满缓冲保留事件数 = %d, want %d（满则跳过后续）", received, cap(ch))
	}
}

// TestEventBus_SlowCallbackDoesNotHoldLock 接口回调必须异步且在全局锁外执行
func TestEventBus_SlowCallbackDoesNotHoldLock(t *testing.T) {
	bus := NewEventBus()
	sub := &blockingSubscriber{entered: make(chan struct{}, 1), release: make(chan struct{})}
	bus.Subscribe(EventSyncComplete, sub)

	bus.Publish(Event{Type: EventSyncComplete, Timestamp: time.Now()})
	select {
	case <-sub.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("接口回调未被异步调用")
	}

	// 回调仍阻塞时，订阅/取消/发布必须仍能取得全局锁
	done := make(chan struct{})
	go func() {
		defer close(done)
		ch, cancel := bus.SubscribeChan()
		_ = ch
		cancel()
		another := &mockSubscriber{}
		bus.Subscribe(EventDNSFailed, another)
		bus.Unsubscribe(EventDNSFailed, another)
		bus.Publish(Event{Type: EventDNSFailed, Timestamp: time.Now()})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("慢回调阻塞了 EventBus 全局锁")
	}
	close(sub.release)
}

// ─── Step 7：Subscriber 错误隔离 ───

// TestEventBus_SubscriberErrorIsolation 订阅者返回错误时必须被隔离：
// 错误只由 EventBus 记录 WARN（bus.go 中 Publish 的异步回调），
// 既不影响同事件类型的其他订阅者，也不破坏总线后续可用性。
func TestEventBus_SubscriberErrorIsolation(t *testing.T) {
	bus := NewEventBus()
	bad := &failingSubscriber{notify: make(chan struct{}, 4)}
	good := &mockSubscriber{}

	bus.Subscribe(EventSyncError, bad)
	bus.Subscribe(EventSyncError, good)

	bus.Publish(Event{
		Type:      EventSyncError,
		Timestamp: time.Now(),
		Data:      map[string]any{"domain": "example.com"},
	})

	// 确定性等待两个订阅者都被调用：错误订阅者不得阻断同类型其他订阅者
	select {
	case <-bad.notify:
	case <-time.After(5 * time.Second):
		t.Fatal("错误订阅者未被调用")
	}
	deadline := time.Now().Add(5 * time.Second)
	for good.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := bad.calls.Load(); got != 1 {
		t.Errorf("错误订阅者调用次数 = %d, want 1", got)
	}
	if got := good.count(); got != 1 {
		t.Fatalf("错误订阅者的返回值影响了其他订阅者：收到 %d 个事件, want 1", got)
	}

	// 错误回调之后总线仍然可用：再次发布必须继续投递
	bus.Publish(Event{Type: EventSyncError, Timestamp: time.Now()})
	deadline = time.Now().Add(5 * time.Second)
	for good.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := good.count(); got != 2 {
		t.Errorf("错误回调后事件总线不可用：收到 %d 个事件, want 2", got)
	}
}
