package notifier

// P3-15：标准库虚拟时间驱动真实计时器；经生产 OnEvent 满载分支验证计数与日志。

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type p315Record struct {
	level   slog.Level
	message string
	attrs   map[string]any
}
type p315Capture struct {
	mu      sync.Mutex
	rows    []p315Record
	block   chan struct{}
	entered chan struct{}
}

func (*p315Capture) Enabled(context.Context, slog.Level) bool { return true }
func (c *p315Capture) Handle(_ context.Context, r slog.Record) error {
	if r.Level != slog.LevelWarn || r.Message != "告警渠道在途已满，丢弃最新通知" {
		return nil
	}
	a := map[string]any{}
	r.Attrs(func(v slog.Attr) bool { a[v.Key] = v.Value.Any(); return true })
	c.mu.Lock()
	block, entered := c.block, c.entered
	c.mu.Unlock()
	if block != nil {
		entered <- struct{}{}
		<-block
	}
	c.mu.Lock()
	c.rows = append(c.rows, p315Record{r.Level, r.Message, a})
	c.mu.Unlock()
	return nil
}
func (c *p315Capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *p315Capture) WithGroup(string) slog.Handler      { return c }
func (c *p315Capture) copy() []p315Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]p315Record(nil), c.rows...)
}
func p315Logs(t *testing.T) *p315Capture {
	t.Helper()
	c := &p315Capture{}
	old := slog.Default()
	oldOutput, oldFlags := log.Writer(), log.Flags()
	slog.SetDefault(slog.New(c))
	t.Cleanup(func() {
		slog.SetDefault(old)
		// SetDefault 恢复 defaultHandler 时不会恢复标准 log 的 writer。
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	})
	return c
}
func p315Fill(t *testing.T, l *InFlightLimiter) {
	t.Helper()
	for i := 0; i < l.capacity; i++ {
		r, ok := l.Acquire()
		if !ok {
			t.Fatal("fill failed")
		}
		t.Cleanup(r)
	}
}
func p315Email(l *InFlightLimiter) *EmailNotifier {
	n := NewEmailNotifier(EmailConfig{Security: "auto_starttls", Host: "host-secret", Pass: "password-secret", Body: "body-secret"})
	n.SetInFlightLimiter(l)
	return n
}
func p315Hook(l *InFlightLimiter, channel string) *WebhookNotifier {
	n := NewWebhookNotifier("https://url-secret/?token=token-secret", channel)
	n.SetInFlightLimiter(l)
	return n
}
func p315Send(t *testing.T, n Subscriber, e EventType) {
	t.Helper()
	if err := n.OnEvent(Event{Type: e, Data: map[string]any{"error": "error-secret", "domain": "domain-secret"}}); err != nil {
		t.Fatal(err)
	}
}
func p315Count(t *testing.T, rows []p315Record) uint64 {
	t.Helper()
	var total uint64
	for _, r := range rows {
		if r.level != slog.LevelWarn {
			t.Fatal("not WARN")
		}
		if r.message != "告警渠道在途已满，丢弃最新通知" || r.attrs["window_seconds"] != int64(30) {
			t.Fatalf("wrong message/window: %v", r)
		}
		if phase := r.attrs["phase"]; phase != "first" && phase != "summary" {
			t.Fatalf("wrong phase: %v", r)
		}
		v, ok := r.attrs["dropped"].(uint64)
		if !ok {
			t.Fatalf("missing integer dropped: %v", r.attrs)
		}
		a := r.attrs
		count := func(key string) uint64 {
			t.Helper()
			value, ok := a[key].(uint64)
			if !ok {
				t.Fatalf("missing integer %s: %v", key, a)
			}
			return value
		}
		events := count("dns_failed") + count("sync_error") + count("operational_unhealthy")
		channels := count("email") + count("dingtalk") + count("feishu") + count("slack") + count("unknown")
		if v != events || v != channels {
			t.Fatalf("bad accounting %v", a)
		}
		total += v
		for _, secret := range []string{"host-secret", "password-secret", "body-secret", "url-secret", "token-secret", "error-secret", "domain-secret", "channel-secret"} {
			if strings.Contains(fmt.Sprint(r), secret) {
				t.Fatalf("leaked %s", secret)
			}
		}
	}
	return total
}
func TestP315Burst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := p315Logs(t)
		l := NewInFlightLimiter(4)
		p315Fill(t, l)
		n := p315Email(l)
		for i := 0; i < 496; i++ {
			p315Send(t, n, EventDNSFailed)
		}
		if len(c.copy()) != 1 {
			t.Fatalf("burst WARN=%d want 1", len(c.copy()))
		}
		time.Sleep(30*time.Second - time.Nanosecond)
		synctest.Wait()
		if len(c.copy()) != 1 {
			t.Fatal("early summary")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		rows := c.copy()
		if len(rows) != 2 || p315Count(t, rows) != 496 {
			t.Fatalf("tail summary=%v", rows)
		}
		if rows[0].attrs["dropped"] != uint64(1) || rows[1].attrs["dropped"] != uint64(495) {
			t.Fatalf("wrong partition %v", rows)
		}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if len(c.copy()) != 2 {
			t.Fatal("empty log loop")
		}
		t.Log("496 drops -> 2 WARN; dropped=1+495")
	})
}
func TestP315TwoChannels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := p315Logs(t)
		a, b := NewInFlightLimiter(4), NewInFlightLimiter(4)
		p315Fill(t, a)
		p315Fill(t, b)
		email, hook := p315Email(a), p315Hook(b, "dingtalk")
		for i := 0; i < 496; i++ {
			p315Send(t, email, EventDNSFailed)
			p315Send(t, hook, EventDNSFailed)
		}
		if len(c.copy()) != 2 {
			t.Fatalf("two channels first WARN=%d", len(c.copy()))
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if rows := c.copy(); len(rows) != 4 || p315Count(t, rows) != 992 {
			t.Fatalf("two channel accounting: %v", rows)
		}
		t.Log("992 drops -> 4 WARN")
	})
}
func TestP315HotReloadMixed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := p315Logs(t)
		l := NewInFlightLimiter(2)
		p315Fill(t, l)
		old, fresh := p315Hook(l, "dingtalk"), p315Hook(l, "feishu")
		p315Send(t, old, EventDNSFailed)
		p315Send(t, old, EventSyncError)
		p315Send(t, fresh, EventOperationalUnhealthy)
		p315Send(t, fresh, EventDNSFailed)
		if len(c.copy()) != 1 {
			t.Fatalf("reload reset window: %d", len(c.copy()))
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		rows := c.copy()
		if len(rows) != 2 || p315Count(t, rows) != 4 {
			t.Fatalf("bad reload count %v", rows)
		}
		summary := rows[1].attrs
		if summary["channel"] != "mixed" || summary["dingtalk"] != uint64(1) || summary["feishu"] != uint64(2) || summary["in_flight_limit"] != int64(2) {
			t.Fatalf("incorrect platform/capacity %v", summary)
		}
	})
}
func TestP315ConcurrentBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := p315Logs(t)
		l := NewInFlightLimiter(4)
		p315Fill(t, l)
		n := p315Email(l)
		p315Send(t, n, EventDNSFailed)
		var wg sync.WaitGroup
		for g := 0; g < 32; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					p315Send(t, n, EventSyncError)
				}
			}()
		}
		wg.Wait()
		if len(c.copy()) != 1 {
			t.Fatal("concurrent flood")
		}
		time.Sleep(30*time.Second - time.Nanosecond)
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Nanosecond)
			for i := 0; i < 100; i++ {
				p315Send(t, n, EventOperationalUnhealthy)
			}
		}()
		time.Sleep(time.Nanosecond)
		wg.Wait()
		synctest.Wait()
		time.Sleep(90 * time.Second)
		synctest.Wait()
		rows := c.copy()
		if p315Count(t, rows) != 3301 {
			t.Fatalf("lost/duplicated: %v", rows)
		}
		var dns, syncErrors, operational uint64
		for _, row := range rows {
			dns += row.attrs["dns_failed"].(uint64)
			syncErrors += row.attrs["sync_error"].(uint64)
			operational += row.attrs["operational_unhealthy"].(uint64)
		}
		if dns != 1 || syncErrors != 3200 || operational != 100 {
			t.Fatalf("wrong event categories: dns=%d sync=%d operational=%d", dns, syncErrors, operational)
		}
		if len(rows) > 3 {
			t.Fatalf("boundary duplicated reports=%d", len(rows))
		}
	})
}
func TestP315SustainedAndRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := p315Logs(t)
		l := NewInFlightLimiter(4)
		p315Fill(t, l)
		n := p315Email(l)
		for window := 0; window < 10; window++ {
			for i := 0; i < 20; i++ {
				p315Send(t, n, EventDNSFailed)
			}
			time.Sleep(30 * time.Second)
			synctest.Wait()
		}
		rows := c.copy()
		if len(rows) != 11 || p315Count(t, rows) != 200 {
			t.Fatalf("sustained=%v", rows)
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		p315Send(t, n, EventDNSFailed)
		if len(c.copy()) != 12 || p315Count(t, c.copy()) != 201 {
			t.Fatal("no immediate warning after idle")
		}
		time.Sleep(90 * time.Second)
		synctest.Wait()
		if len(c.copy()) != 12 {
			t.Fatal("isolated drop duplicate")
		}
	})
}
func TestP315FilterAndUnknown(t *testing.T) {
	for _, channel := range []string{"dingtalk", "feishu", "slack", "channel-secret"} {
		t.Run(channel, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := p315Logs(t)
				l := NewInFlightLimiter(4)
				p315Fill(t, l)
				n := p315Hook(l, channel)
				p315Send(t, n, EventSyncStart)
				p315Send(t, p315Email(l), EventSyncStart)
				if len(c.copy()) != 0 {
					t.Fatal("unsupported counted")
				}
				if channel == "channel-secret" {
					l.logDropped(channel, EventDNSFailed)
				} else {
					p315Send(t, n, EventDNSFailed)
				}
				want := channel
				if channel == "channel-secret" {
					want = "unknown"
				}
				if rows := c.copy(); len(rows) != 1 || rows[0].attrs["channel"] != want || rows[0].attrs[want] != uint64(1) || p315Count(t, rows) != 1 {
					t.Fatalf("wrong or unsafe channel: %v", rows)
				}
			})
		})
	}
}
func TestP315BlockedLogger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := p315Logs(t)
		c.mu.Lock()
		c.block = make(chan struct{})
		c.entered = make(chan struct{}, 2)
		c.mu.Unlock()
		l := NewInFlightLimiter(4)
		p315Fill(t, l)
		n := p315Email(l)
		done := make(chan struct{})
		go func() { p315Send(t, n, EventDNSFailed); close(done) }()
		<-c.entered
		other := make(chan struct{})
		go func() {
			for i := 0; i < 100; i++ {
				p315Send(t, n, EventDNSFailed)
			}
			close(other)
		}()
		select {
		case <-other:
		case <-time.After(time.Second):
			close(c.block)
			t.Fatal("logger held drop lock")
		}
		close(c.block)
		<-done
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if p315Count(t, c.copy()) != 101 {
			t.Fatal("lost during logger block")
		}
	})
}
func TestP315BusIsolation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := p315Logs(t)
		l := NewInFlightLimiter(4)
		p315Fill(t, l)
		bus := NewEventBus()
		bus.Subscribe(EventDNSFailed, p315Email(l))
		ch, stop := bus.SubscribeChan()
		defer stop()
		for i := 0; i < 16; i++ {
			bus.Publish(Event{Type: EventDNSFailed})
			select {
			case <-ch:
			default:
				t.Fatal("SSE starved")
			}
		}
		synctest.Wait()
		if len(c.copy()) != 1 {
			t.Fatal("bus log flood")
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if p315Count(t, c.copy()) != 16 {
			t.Fatal("bus count")
		}
	})
}
func TestP315TimerBecomesIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p315Logs(t)
		l := NewInFlightLimiter(4)
		p315Fill(t, l)
		n := p315Email(l)
		p315Send(t, n, EventDNSFailed)
		l.dropMu.Lock()
		firstTimer := l.dropTimer
		l.dropMu.Unlock()
		for i := 0; i < 100; i++ {
			p315Send(t, n, EventDNSFailed)
		}
		l.dropMu.Lock()
		same := l.dropTimer == firstTimer
		l.dropMu.Unlock()
		if !same {
			t.Fatal("每条丢弃创建了新计时器")
		}
		time.Sleep(60 * time.Second)
		synctest.Wait()
		l.dropMu.Lock()
		idle := l.dropTimer == nil && l.drops == dropCounts{}
		l.dropMu.Unlock()
		if !idle {
			t.Fatal("空闲后仍保有计时器或计数")
		}
	})
}
func TestP315BlockedSummary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := p315Logs(t)
		l := NewInFlightLimiter(4)
		p315Fill(t, l)
		n := p315Email(l)
		p315Send(t, n, EventDNSFailed)
		p315Send(t, n, EventSyncError)
		c.mu.Lock()
		c.block = make(chan struct{})
		c.entered = make(chan struct{}, 4)
		c.mu.Unlock()
		time.Sleep(30 * time.Second)
		<-c.entered
		for i := 0; i < 100; i++ {
			p315Send(t, n, EventOperationalUnhealthy)
		}
		time.Sleep(120 * time.Second)
		synctest.Wait()
		select {
		case <-c.entered:
			t.Fatal("前一汇总未结束又启动了汇总回调")
		default:
		}
		close(c.block)
		synctest.Wait()
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if rows := c.copy(); len(rows) != 3 || p315Count(t, rows) != 102 {
			t.Fatalf("汇总阻塞丢计数：%v", rows)
		}
	})
}
