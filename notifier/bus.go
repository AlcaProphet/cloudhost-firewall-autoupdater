package notifier

import (
	"log/slog"
	"sync"
	"time"
)

// EventType 事件类型
type EventType string

const (
	EventSyncStart          EventType = "sync:start"
	EventSyncComplete       EventType = "sync:complete"        // 全局：一轮同步完成
	EventDomainSyncComplete EventType = "domain:sync_complete" // 逐域名：单个域名同步成功
	EventSyncError          EventType = "sync:error"
	EventRuleChanged        EventType = "rule:changed"
	EventDNSFailed          EventType = "dns:failed"
)

// Event 事件
type Event struct {
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data"`
}

// Subscriber 事件订阅者接口
type Subscriber interface {
	OnEvent(event Event) error
}

// EventBus 事件总线
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]Subscriber
	// channel 订阅者（用于 SSE）：取消订阅只删除记录，不关闭 channel
	chanSubs map[int]chan Event
	nextID   int
}

// NewEventBus 创建事件总线
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[EventType][]Subscriber),
		chanSubs:    make(map[int]chan Event),
	}
}

// Subscribe 订阅事件（接口方式）
func (b *EventBus) Subscribe(eventType EventType, sub Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[eventType] = append(b.subscribers[eventType], sub)
}

// Unsubscribe 取消订阅。sub 必须与 Subscribe 时传入的为同一实例，否则无法匹配。
// 若 sub 未找到（已取消或从未订阅），无操作（幂等）。
func (b *EventBus) Unsubscribe(eventType EventType, sub Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[eventType]
	for i, s := range subs {
		if s == sub {
			b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
	// 未找到 — 幂等，不报错
}

// SubscribeChan 订阅所有事件（channel 方式，用于 SSE 推送）
// 返回事件 channel 和取消订阅函数。
// 取消订阅只在写锁内删除订阅表记录，不关闭 channel：Publish 可能已持有该 channel 的快照，
// 关闭 channel 会让锁外投递触发 send on closed channel panic。channel 由 SSE handler 退出后交由 GC 回收。
// 取消函数使用 sync.Once，重复调用与并发调用均幂等。
func (b *EventBus) SubscribeChan() (<-chan Event, func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan Event, 32)
	b.chanSubs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.chanSubs, id)
			b.mu.Unlock()
		})
	}
}

// Publish 异步投递，不阻塞调用方
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	// 必须复制接口订阅者 slice：直接使用 map 中的 slice 会与 Subscribe/Unsubscribe 的原地修改竞态
	subs := append([]Subscriber(nil), b.subscribers[event.Type]...)
	// 复制 channel 订阅表快照：快照取得后的取消订阅不影响本次投递
	chanSubs := make([]chan Event, 0, len(b.chanSubs))
	for _, ch := range b.chanSubs {
		chanSubs = append(chanSubs, ch)
	}
	b.mu.RUnlock()

	// 接口订阅者：锁外异步回调，慢订阅者不阻塞事件总线全局锁
	for _, sub := range subs {
		go func(s Subscriber) {
			if err := s.OnEvent(event); err != nil {
				slog.Warn("事件处理失败", "type", event.Type, "error", err)
			}
		}(sub)
	}
	// channel 订阅者：非阻塞发送，满则跳过
	for _, ch := range chanSubs {
		select {
		case ch <- event:
		default:
		}
	}
}
