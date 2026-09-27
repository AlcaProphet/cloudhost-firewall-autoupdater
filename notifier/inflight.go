package notifier

import (
	"log/slog"
	"sync"
)

// InFlightLimit 每个告警渠道允许的最大在途发送数（Issue6 A2，2026-09-27 用户裁决 F5）。
//
// 语义：邮件与 Webhook **各自**最多 4 个在途任务；满载时丢弃**最新**通知并记录
// 不含密码、URL 或正文敏感值的 WARN。该上限位于告警订阅者/调度边界，
// **不限流整个 EventBus**（Publish 继续非阻塞，StoreLogWriter 与 SSE 不受影响）。
const InFlightLimit = 4

// InFlightLimiter 按渠道统计「正在发送中」的任务数。
//
// 由 AlertManager 持有并注入 notifier 实例，因此配置热重载只会替换具体实例
// （旧实例的在途发送不取消、不等待），而**在途计数仍然连续**——这是「每渠道 ≤4」
// 在热重载后依旧成立的关键。
type InFlightLimiter struct {
	capacity int
	slots    chan struct{}
}

// NewInFlightLimiter 创建容量为 capacity 的限流器；capacity <= 0 时按 1 处理。
func NewInFlightLimiter(capacity int) *InFlightLimiter {
	if capacity <= 0 {
		capacity = 1
	}
	return &InFlightLimiter{capacity: capacity, slots: make(chan struct{}, capacity)}
}

// Acquire 尝试占用一个在途名额。
//
// 成功返回 release 函数（幂等，可安全多次调用）与 true；满载返回 nil 与 false，
// 调用方必须放弃本次通知（丢弃最新）。
func (l *InFlightLimiter) Acquire() (release func(), ok bool) {
	select {
	case l.slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-l.slots }) }, true
	default:
		return nil, false
	}
}

// InFlight 返回当前在途数量（观测用）。
func (l *InFlightLimiter) InFlight() int { return len(l.slots) }

// LimitedNotifier 是可被注入渠道限流器的告警实例。
//
// 用接口 + setter 注入，避免改动 NewEmailNotifier / NewWebhookNotifier 的既有签名
// （既有测试与调用点零改动），同时让限流器由 AlertManager 跨热重载持有。
type LimitedNotifier interface {
	SetInFlightLimiter(*InFlightLimiter)
	ChannelName() string
}

// logDropped 输出「满载丢弃最新」的安全 WARN。
//
// 只记录渠道名、事件类型与在途上限：绝不记录 SMTP 密码、Webhook URL 或事件正文。
func logDropped(channel string, event Event, limit int) {
	slog.Warn("告警渠道在途已满，丢弃最新通知",
		"channel", channel,
		"event", string(event.Type),
		"in_flight_limit", limit,
	)
}
