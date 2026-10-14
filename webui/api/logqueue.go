package api

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

const logQueueCapacity = 256
const logDrainTimeout = 10 * time.Second
const logDropWindow = 30 * time.Second

type pendingLog struct {
	eventType notifier.EventType
	log       config.SyncLog
}

// QueuedLogWriter owns one writer and a ring of pending records, independent of alerts.
type QueuedLogWriter struct {
	mu                  sync.Mutex
	ring                []pendingLog
	head, size          int
	accepting, expired  bool
	deadline            time.Time
	overflow, abandoned uint64
	lastReport          time.Time
	write               func(context.Context, config.SyncLog) error
	ctx                 context.Context
	cancel              context.CancelFunc
	wake                chan struct{}
	done                chan struct{}
	unsub               []func()
	stopOnce            sync.Once
	drainTimeout        time.Duration
}

func NewQueuedLogWriter(store *config.Store, bus *notifier.EventBus) *QueuedLogWriter {
	return newQueuedLogWriter(bus, store.AddSyncLogContext, logQueueCapacity, logDrainTimeout)
}

func newQueuedLogWriter(bus *notifier.EventBus, write func(context.Context, config.SyncLog) error, capacity int, drainTimeout time.Duration) *QueuedLogWriter {
	ctx, cancel := context.WithCancel(context.Background())
	q := &QueuedLogWriter{ring: make([]pendingLog, capacity), accepting: true, write: write, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), drainTimeout: drainTimeout}
	q.unsub = []func(){bus.SubscribeIngress(notifier.EventTargetSyncComplete, q.Enqueue), bus.SubscribeIngress(notifier.EventSyncError, q.Enqueue)}
	go q.run()
	return q
}

// Enqueue executes no SQLite or diagnostic I/O; only this fast method is called inline.
func (q *QueuedLogWriter) Enqueue(event notifier.Event) {
	log, ok := syncLogForEvent(event)
	if !ok {
		return
	}
	q.mu.Lock()
	if !q.accepting {
		q.mu.Unlock()
		return
	}
	if q.size == len(q.ring) {
		q.ring[q.head] = pendingLog{}
		q.head = (q.head + 1) % len(q.ring)
		q.size--
		q.overflow++
	}
	q.ring[(q.head+q.size)%len(q.ring)] = pendingLog{eventType: event.Type, log: log}
	q.size++
	q.mu.Unlock()
	q.notify()
}

func (q *QueuedLogWriter) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// expire linearizes pending-record abandonment before canceling the active SQL.
func (q *QueuedLogWriter) expire() {
	q.mu.Lock()
	if q.expired {
		q.mu.Unlock()
		return
	}
	q.expired = true
	q.accepting = false
	q.abandoned += uint64(q.size)
	clear(q.ring)
	q.size = 0
	q.mu.Unlock()
	q.cancel()
	q.notify()
}

func (q *QueuedLogWriter) run() {
	defer close(q.done)
	defer q.cancel()
	defer q.reportDrops(true)
	for {
		q.mu.Lock()
		if !q.deadline.IsZero() && !time.Now().Before(q.deadline) && !q.expired {
			q.mu.Unlock()
			q.expire()
			continue
		}
		if q.size > 0 {
			item := q.ring[q.head]
			q.ring[q.head] = pendingLog{}
			q.head = (q.head + 1) % len(q.ring)
			q.size--
			q.mu.Unlock()
			if err := q.write(q.ctx, item.log); err != nil {
				notifier.ReportEventError(item.eventType, err)
			}
			q.reportDrops(false)
			continue
		}
		accepting := q.accepting
		q.mu.Unlock()
		if !accepting {
			return
		}
		<-q.wake
	}
}

// reportDrops runs only on the worker. Counters include no business values;
// idle tails are flushed at Stop, without independent periodic goroutines.
func (q *QueuedLogWriter) reportDrops(force bool) {
	q.mu.Lock()
	if q.overflow+q.abandoned == 0 || (!force && !q.lastReport.IsZero() && time.Since(q.lastReport) < logDropWindow) {
		q.mu.Unlock()
		return
	}
	overflow, abandoned := q.overflow, q.abandoned
	q.overflow, q.abandoned = 0, 0
	q.lastReport = time.Now()
	capacity := len(q.ring)
	q.mu.Unlock()
	slog.Warn("同步日志持久化丢弃", "queue_capacity", capacity, "overflow_oldest", overflow, "shutdown_pending", abandoned)
}

// Stop freezes admission, attempts bounded draining, then joins the worker.
// It is idempotent. The drain budget is soft: active driver calls may delay return.
func (q *QueuedLogWriter) Stop() {
	q.stopOnce.Do(func() {
		q.mu.Lock()
		q.accepting = false
		q.deadline = time.Now().Add(q.drainTimeout)
		q.mu.Unlock()
		expiredDone := make(chan struct{})
		timer := time.AfterFunc(q.drainTimeout, func() { defer close(expiredDone); q.expire() })
		for _, unsub := range q.unsub {
			unsub()
		}
		q.notify()
		<-q.done
		if !timer.Stop() {
			<-expiredDone
		}
	})
}
