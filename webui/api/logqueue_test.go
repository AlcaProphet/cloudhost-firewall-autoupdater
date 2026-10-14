package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

type prepRecord struct {
	message string
	attrs   map[string]any
}
type prepHandler struct {
	mu      sync.Mutex
	records []prepRecord
}

func (h *prepHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *prepHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]any{}
	r.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value.Any(); return true })
	h.mu.Lock()
	h.records = append(h.records, prepRecord{r.Message, m})
	h.mu.Unlock()
	return nil
}
func (h *prepHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *prepHandler) WithGroup(string) slog.Handler      { return h }
func (h *prepHandler) snapshot() []prepRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]prepRecord(nil), h.records...)
}
func prepCapture(t *testing.T) *prepHandler {
	t.Helper()
	h := &prepHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })
	return h
}
func prepEvent(i int) notifier.Event {
	return notifier.Event{Type: notifier.EventTargetSyncComplete, Timestamp: time.Unix(int64(i), 0), Data: map[string]any{"provider": fmt.Sprintf("ali_ecs(target-%03d)", i), "domain": "business-sentinel.example", "added": 3, "deleted": 2, "cleanup_deleted": 2, "outcome": "success"}}
}
func prepWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("local fixture did not complete")
		var z T
		return z
	}
}

func TestI8R10CapacityOrderAndIsolation(t *testing.T) {
	h := prepCapture(t)
	bus := notifier.NewEventBus()
	hold := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(hold) }) })
	entered := make(chan struct{}, 1)
	var written []config.SyncLog
	var active, peak atomic.Int32
	q := newQueuedLogWriter(bus, func(ctx context.Context, l config.SyncLog) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		if l.Target == "target-000" {
			entered <- struct{}{}
			select {
			case <-hold:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		written = append(written, l)
		return nil
	}, logQueueCapacity, logDrainTimeout)
	t.Cleanup(func() { release.Do(func() { close(hold) }); q.Stop() })
	received := make(chan notifier.Event, 300)
	bus.Subscribe(notifier.EventTargetSyncComplete, eventCapture{ch: received})
	sse, cancel := bus.SubscribeChan()
	defer cancel()
	bus.Publish(prepEvent(0))
	prepWait(t, entered)
	prepWait(t, sse)
	for i := 1; i < 300; i++ {
		e := prepEvent(i)
		bus.Publish(e)
		prepWait(t, sse)
		e.Data["provider"] = "mutated-after-publish"
	}
	q.mu.Lock()
	size, overflow := q.size, q.overflow
	q.mu.Unlock()
	if size != 256 || overflow != 43 || active.Load() != 1 {
		t.Fatalf("pending=%d overflow=%d active=%d", size, overflow, active.Load())
	}
	for i := 0; i < 300; i++ {
		prepWait(t, received)
	}
	release.Do(func() { close(hold) })
	q.Stop()
	if len(written) != 257 || written[0].Target != "target-000" || peak.Load() != 1 {
		t.Fatalf("written=%d peak=%d", len(written), peak.Load())
	}
	for i := 1; i < len(written); i++ {
		if got, want := written[i].Target, fmt.Sprintf("target-%03d", i+43); got != want {
			t.Fatalf("retention/order[%d]=%s want %s", i, got, want)
		}
	}
	var dropped uint64
	for _, r := range h.snapshot() {
		if r.message == "同步日志持久化丢弃" {
			dropped += r.attrs["overflow_oldest"].(uint64)
			if strings.Contains(fmt.Sprint(r.attrs), "business-sentinel") {
				t.Fatal("drop diagnostic contains business data")
			}
		}
	}
	if dropped != 43 {
		t.Fatalf("reported overflow=%d", dropped)
	}
	for _, slot := range q.ring {
		if slot != (pendingLog{}) {
			t.Fatal("drained ring retained data")
		}
	}
}

func TestI8R10DrainTenSecondBoundaryAndJoin(t *testing.T) {
	h := prepCapture(t)
	synctest.Test(t, func(t *testing.T) {
		bus := notifier.NewEventBus()
		entered := make(chan struct{})
		canceled := make(chan error, 1)
		release := make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		var calls atomic.Int32
		q := newQueuedLogWriter(bus, func(ctx context.Context, l config.SyncLog) error {
			if calls.Add(1) != 1 {
				return errors.New("unexpected additional write after expiry")
			}
			close(entered)
			<-ctx.Done()
			canceled <- ctx.Err()
			<-release
			return ctx.Err()
		}, logQueueCapacity, logDrainTimeout)
		defer prepCleanupWorker(q, func() { once.Do(func() { close(release) }) })
		defer q.cancel()
		bus.Publish(prepEvent(0))
		<-entered
		bus.Publish(prepEvent(1))
		bus.Publish(prepEvent(2))
		stopped := make(chan struct{})
		go func() { q.Stop(); close(stopped) }()
		synctest.Wait()
		time.Sleep(10*time.Second - time.Nanosecond)
		synctest.Wait()
		select {
		case <-canceled:
			t.Fatal("canceled before ten-second boundary")
		default:
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		select {
		case err := <-canceled:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("active write not canceled: %v", err)
			}
		default:
			t.Fatal("active write not canceled at ten-second boundary")
		}
		q.mu.Lock()
		size, abandoned, expired := q.size, q.abandoned, q.expired
		q.mu.Unlock()
		if size != 0 || abandoned != 2 || !expired {
			t.Fatalf("expiry pending=%d abandoned=%d expired=%v", size, abandoned, expired)
		}
		select {
		case <-stopped:
			t.Fatal("Stop returned before active write ended")
		default:
		}
		bus.Publish(prepEvent(3))
		q.Enqueue(prepEvent(4))
		once.Do(func() { close(release) })
		<-stopped
		if calls.Load() != 1 {
			t.Fatalf("write calls=%d", calls.Load())
		}
		q.Stop()
	})
	var abandoned uint64
	var failures int
	for _, r := range h.snapshot() {
		if r.message == "同步日志持久化丢弃" {
			abandoned += r.attrs["shutdown_pending"].(uint64)
		}
		if r.message == "事件处理失败" {
			failures++
		}
	}
	if abandoned != 2 || failures != 1 {
		t.Fatalf("diagnostics abandoned=%d failures=%d", abandoned, failures)
	}
}

func TestI8R10NormalDrainCancelsTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bus := notifier.NewEventBus()
		entered := make(chan struct{})
		release := make(chan struct{})
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
		}()
		var calls atomic.Int32
		var canceled atomic.Bool
		q := newQueuedLogWriter(bus, func(ctx context.Context, l config.SyncLog) error {
			n := calls.Add(1)
			if n == 1 {
				close(entered)
				<-release
			}
			if ctx.Err() != nil {
				canceled.Store(true)
				return ctx.Err()
			}
			return nil
		}, 256, 10*time.Second)
		defer prepCleanupWorker(q, func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		defer q.cancel()
		bus.Publish(prepEvent(0))
		<-entered
		bus.Publish(prepEvent(1))
		stopped := make(chan struct{})
		go func() { q.Stop(); close(stopped) }()
		synctest.Wait()
		select {
		case <-stopped:
			t.Fatal("Stop returned before drain completed")
		default:
		}
		time.Sleep(9 * time.Second)
		close(release)
		<-stopped
		if calls.Load() != 2 || canceled.Load() {
			t.Fatalf("normal drain calls=%d", calls.Load())
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if q.expired {
			t.Fatal("timer fired after successful Stop")
		}
	})
}

func TestI8R10ConcurrentStopJoinsSameWorker(t *testing.T) {
	bus := notifier.NewEventBus()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	q := newQueuedLogWriter(bus, func(ctx context.Context, l config.SyncLog) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, 256, 10*time.Second)
	defer q.cancel()
	bus.Publish(prepEvent(0))
	prepWait(t, entered)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); q.Stop() }()
	}
	deadline := time.Now().Add(time.Second)
	for {
		q.mu.Lock()
		frozen := !q.accepting
		q.mu.Unlock()
		if frozen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Stop did not freeze admission")
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		t.Fatal("concurrent Stop returned before writer finished")
	default:
	}
	once.Do(func() { close(release) })
	prepWait(t, done)
	q.Stop()
}

func TestI8R10SQLiteDrainSoftBudget(t *testing.T) {
	h := prepCapture(t)
	path := filepath.Join(t.TempDir(), "drain.db")
	s, err := config.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	other, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	}()
	holder, err := other.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := holder.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := holder.ExecContext(context.Background(), "ROLLBACK"); err != nil {
			t.Error(err)
		}
	}()
	bus := notifier.NewEventBus()
	entered := make(chan struct{}, 1)
	var calls atomic.Int32
	q := newQueuedLogWriter(bus, func(ctx context.Context, l config.SyncLog) error {
		if calls.Add(1) == 1 {
			entered <- struct{}{}
		}
		return s.AddSyncLogContext(ctx, l)
	}, 256, 10*time.Second)
	bus.Publish(prepEvent(0))
	prepWait(t, entered)
	for i := 1; i < 4; i++ {
		bus.Publish(prepEvent(i))
	}
	start := time.Now()
	q.Stop()
	elapsed := time.Since(start)
	if elapsed < 10*time.Second {
		t.Fatalf("soft drain canceled early: %v", elapsed)
	}
	var abandoned uint64
	for _, r := range h.snapshot() {
		if r.message == "同步日志持久化丢弃" {
			abandoned += r.attrs["shutdown_pending"].(uint64)
		}
	}
	if abandoned < 1 || int64(calls.Load())+int64(abandoned) != 4 {
		t.Fatalf("calls=%d abandoned=%d", calls.Load(), abandoned)
	}
	rows, err := s.GetSyncLogs(10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("locked writes persisted=%v err=%v", rows, err)
	}
	select {
	case <-q.done:
	default:
		t.Fatal("active worker was not joined")
	}
	t.Logf("actual SQLite: Stop=%v calls=%d abandoned=%d; no committed rows, worker joined before Store.Close", elapsed, calls.Load(), abandoned)
}

func TestI8R10FailureContinuesWithoutRetry(t *testing.T) {
	h := prepCapture(t)
	bus := notifier.NewEventBus()
	var calls []config.SyncLog
	sentinel := errors.New("fixture write failure")
	q := newQueuedLogWriter(bus, func(ctx context.Context, l config.SyncLog) error {
		calls = append(calls, l)
		if l.Target == "target-000" {
			return sentinel
		}
		return nil
	}, 256, 10*time.Second)
	bus.Publish(prepEvent(0))
	second := prepEvent(1)
	second.Type = notifier.EventSyncError
	second.Data["error"] = "failure-business-sentinel"
	bus.Publish(second)
	q.Stop()
	if len(calls) != 2 || calls[0].Target != "target-000" || calls[1].Target != "target-001" || calls[1].Result != "failed" || calls[1].Added != 3 || calls[1].Deleted != 2 || !strings.Contains(calls[1].Error, "failure-business-sentinel") {
		t.Fatalf("calls=%+v", calls)
	}
	var failures []prepRecord
	for _, r := range h.snapshot() {
		if r.message == "事件处理失败" {
			failures = append(failures, r)
		}
	}
	if len(failures) != 1 || failures[0].attrs["type"] != notifier.EventTargetSyncComplete || failures[0].attrs["error"] != sentinel {
		t.Fatalf("unified failures=%+v", failures)
	}
}

func TestI8R10ConcurrentEnqueueAndStop(t *testing.T) {
	bus := notifier.NewEventBus()
	var writeCalls atomic.Int64
	q := newQueuedLogWriter(bus, func(context.Context, config.SyncLog) error { writeCalls.Add(1); return nil }, 256, 10*time.Second)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				bus.Publish(prepEvent(i))
			}
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); q.Stop() }()
	wg.Wait()
	q.Stop()
	before := writeCalls.Load()
	q.Enqueue(prepEvent(999))
	bus.Publish(prepEvent(998))
	if got := writeCalls.Load(); got != before {
		t.Fatalf("late writes before=%d after=%d", before, got)
	}
	q.mu.Lock()
	pending := q.size
	q.mu.Unlock()
	if pending != 0 {
		t.Fatalf("late events retained after Stop: pending=%d", pending)
	}
	select {
	case <-q.done:
	default:
		t.Fatal("worker not joined")
	}
}

func TestI8R10LogMaterializationMatchesExistingWriter(t *testing.T) {
	for _, typ := range []notifier.EventType{notifier.EventTargetSyncComplete, notifier.EventSyncError} {
		e := prepEvent(1)
		e.Type = typ
		e.Data["outcome"] = "partial"
		e.Data["error"] = "fixture"
		e.Data["cleanup_deferred"] = 4
		first, ok := syncLogForEvent(e)
		if !ok {
			t.Fatal("valid event ignored")
		}
		e.Data["provider"] = "later"
		e.Data["added"] = 99
		second, _ := syncLogForEvent(e)
		if reflect.DeepEqual(first, second) || first.Target != "target-001" || first.Added != 3 {
			t.Fatalf("log not materialized: %+v", first)
		}
	}
	if _, ok := syncLogForEvent(notifier.Event{Type: notifier.EventSyncComplete}); ok {
		t.Fatal("global completion must not add target log")
	}
}

func TestI8R10DropAggregationWindowAndFinalFlush(t *testing.T) {
	h := prepCapture(t)
	synctest.Test(t, func(t *testing.T) {
		bus := notifier.NewEventBus()
		entered := make(chan config.SyncLog, 1)
		gates := map[string]chan struct{}{}
		for _, i := range []int{0, 2, 4, 6, 8} {
			gates[fmt.Sprintf("target-%03d", i)] = make(chan struct{})
		}
		q := newQueuedLogWriter(bus, func(ctx context.Context, l config.SyncLog) error {
			entered <- l
			select {
			case <-gates[l.Target]:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, 1, 10*time.Second)
		defer prepCleanupWorker(q, nil)
		defer q.cancel()
		next := func(i int) {
			t.Helper()
			l := <-entered
			if l.Target != fmt.Sprintf("target-%03d", i) {
				t.Fatalf("worker item=%s want=%d", l.Target, i)
			}
		}
		reports := func() []prepRecord {
			var out []prepRecord
			for _, r := range h.snapshot() {
				if r.message == "同步日志持久化丢弃" {
					out = append(out, r)
				}
			}
			return out
		}
		bus.Publish(prepEvent(0))
		next(0)
		bus.Publish(prepEvent(1))
		bus.Publish(prepEvent(2))
		close(gates["target-000"])
		next(2)
		if rs := reports(); len(rs) != 1 || rs[0].attrs["overflow_oldest"].(uint64) != 1 {
			t.Fatalf("first report=%v", rs)
		}
		bus.Publish(prepEvent(3))
		bus.Publish(prepEvent(4))
		close(gates["target-002"])
		next(4)
		if len(reports()) != 1 {
			t.Fatal("logged each overflow instead of aggregating")
		}
		time.Sleep(30*time.Second - time.Nanosecond)
		bus.Publish(prepEvent(5))
		bus.Publish(prepEvent(6))
		close(gates["target-004"])
		next(6)
		if len(reports()) != 1 {
			t.Fatal("summary emitted before thirty-second window")
		}
		time.Sleep(time.Nanosecond)
		bus.Publish(prepEvent(7))
		bus.Publish(prepEvent(8))
		close(gates["target-006"])
		next(8)
		if rs := reports(); len(rs) != 2 || rs[1].attrs["overflow_oldest"].(uint64) != 3 {
			t.Fatalf("summary missing or double-counted: %v", rs)
		}
		stopped := make(chan struct{})
		go func() { q.Stop(); close(stopped) }()
		synctest.Wait()
		close(gates["target-008"])
		<-stopped
		if len(reports()) != 2 {
			t.Fatal("empty shutdown should not emit extra loss report")
		}
	})
}

// Fixture-only forced teardown, including when a negative control breaks Stop.
// It does not repair a behavior assertion; it prevents panic/leak from replacing that assertion.
func prepCleanupWorker(q *QueuedLogWriter, release func()) {
	select {
	case <-q.done:
		return
	default:
	}
	q.mu.Lock()
	q.accepting = false
	q.expired = true
	clear(q.ring)
	q.size = 0
	q.mu.Unlock()
	q.cancel()
	if release != nil {
		release()
	}
	q.notify()
	<-q.done
}

func TestI8R10SyncerQueueSQLiteProductionChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chain.db")
	store, err := config.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p := &logWriterChainProvider{rules: []config.RuleInfo{
		{Protocol: "TCP", Port: "443", CidrBlock: "127.0.0.1/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "desired"},
		{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.8/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "old-a"},
		{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]", RuleID: "old-b"},
	}}
	st, err := syncer.BuildRuntimeState(nil, config.RuntimeConfig{Tag: "auto-dns", Interval: time.Hour, DNS: "127.0.0.1:1", DNSTimeout: time.Second, DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light", DomainRules: []config.DomainRule{{ID: 1, Host: "127.0.0.1", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{1}}}}, syncer.BreakerReset)
	if err != nil {
		t.Fatal(err)
	}
	st.Providers = []provider.Provider{p}
	engine := syncer.New(syncer.NewRuntimeManager(st))
	q := NewQueuedLogWriter(store, engine.EventBus())
	other, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	holder, err := other.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	locked := true
	release := func() {
		if locked {
			if _, err := holder.ExecContext(context.Background(), "ROLLBACK"); err != nil {
				t.Error(err)
			}
			locked = false
		}
	}
	t.Cleanup(func() {
		release()
		engine.Stop()
		engine.Wait()
		q.Stop()
		prepCleanupWorker(q, nil)
		if err := holder.Close(); err != nil {
			t.Error(err)
		}
		if err := other.Close(); err != nil {
			t.Error(err)
		}
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	completed := make(chan notifier.Event, 1)
	engine.EventBus().Subscribe(notifier.EventSyncComplete, eventCapture{ch: completed})
	go engine.Run()
	prepWait(t, completed)
	engine.Stop()
	engine.Wait()
	joined := make(chan struct{})
	go func() { q.Stop(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("log Stop completed while SQLite write locked")
	case <-time.After(10 * time.Millisecond):
	}
	release()
	prepWait(t, joined)
	rows, err := store.GetSyncLogs(10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	got := rows[0]
	if got.Target != "sg-r702" || got.Result != "success" || got.Deleted != 1 || got.Added != 0 || got.Domain != "127.0.0.1" {
		t.Fatalf("target log fields=%+v", got)
	}
	for _, want := range []string{"清理候选 2 条", "已确认清理 1 条", "延后 1 条"} {
		if !strings.Contains(got.Error, want) {
			t.Fatalf("missing observation %s: %+v", want, got)
		}
	}
}
