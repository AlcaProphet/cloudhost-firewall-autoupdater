package notifier

import (
	"log/slog"
	"sync"
	"time"
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
// 在热重载后依旧成立的关键。满载丢弃日志的计数与窗口也由同一实例连续持有。
type InFlightLimiter struct {
	capacity  int
	slots     chan struct{}
	dropMu    sync.Mutex
	dropTimer *time.Timer
	drops     dropCounts
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

// dropLogWindow 只限制丢弃日志，不影响发送容量或投递结果。
const dropLogWindow = 30 * time.Second

// dropCounts 只保存固定类别计数，不持有事件、URL、正文或配置引用。
type dropCounts struct {
	channels [5]uint64
	events   [3]uint64
}

// logDropped 首条立即输出，后续定时汇总；锁内取走快照，锁外写日志。
// 首条不计入后续汇总，固定类别避免保留敏感事件或高基数配置。
func (l *InFlightLimiter) logDropped(channel string, event EventType) {
	ci := 4
	switch channel {
	case "email":
		ci = 0
	case "dingtalk":
		ci = 1
	case "feishu":
		ci = 2
	case "slack":
		ci = 3
	}
	var ei int
	switch event {
	case EventDNSFailed:
		ei = 0
	case EventSyncError:
		ei = 1
	case EventOperationalUnhealthy:
		ei = 2
	default:
		return
	}
	l.dropMu.Lock()
	l.drops.channels[ci]++
	l.drops.events[ei]++
	if l.dropTimer != nil {
		l.dropMu.Unlock()
		return
	}
	snapshot := l.drops
	l.drops = dropCounts{}
	l.dropTimer = time.AfterFunc(dropLogWindow, l.flushDrops)
	l.dropMu.Unlock()
	l.writeDropLog(snapshot, "first")
}

// flushDrops 锁外写日志；回调输出后才重新计时，避免同渠道汇总回调重叠。
// 空窗口结束活动周期；汇总只表示发生过丢弃，不表示渠道已恢复。
func (l *InFlightLimiter) flushDrops() {
	l.dropMu.Lock()
	snapshot := l.drops
	if snapshot.events == [3]uint64{} {
		l.dropTimer = nil
		l.dropMu.Unlock()
		return
	}
	l.drops = dropCounts{}
	l.dropMu.Unlock()
	l.writeDropLog(snapshot, "summary")
	l.dropMu.Lock()
	l.dropTimer = time.AfterFunc(dropLogWindow, l.flushDrops)
	l.dropMu.Unlock()
}

func (l *InFlightLimiter) writeDropLog(c dropCounts, phase string) {
	names := [5]string{"email", "dingtalk", "feishu", "slack", "unknown"}
	channel := "mixed"
	count := 0
	for i, n := range c.channels {
		if n > 0 {
			count++
			channel = names[i]
		}
	}
	if count > 1 {
		channel = "mixed"
	}
	total := c.events[0] + c.events[1] + c.events[2]
	slog.Warn("告警渠道在途已满，丢弃最新通知",
		"channel", channel, "phase", phase, "in_flight_limit", l.capacity,
		"window_seconds", int(dropLogWindow/time.Second), "dropped", total,
		"dns_failed", c.events[0], "sync_error", c.events[1], "operational_unhealthy", c.events[2],
		"email", c.channels[0], "dingtalk", c.channels[1], "feishu", c.channels[2], "slack", c.channels[3], "unknown", c.channels[4])
}
