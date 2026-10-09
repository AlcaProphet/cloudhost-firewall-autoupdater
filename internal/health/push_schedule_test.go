package health

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

type pushScheduleRecorder struct {
	mu     sync.Mutex
	stamps []time.Time
	urls   []string
}

func (r *pushScheduleRecorder) add(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stamps = append(r.stamps, time.Now())
	r.urls = append(r.urls, url)
}
func (r *pushScheduleRecorder) get() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.stamps...)
}
func (r *pushScheduleRecorder) getURLs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.urls...)
}

// newPushScheduleFixture 使用内存 Transport 与虚拟时间执行真实 Push 循环。
func newPushScheduleFixture(t *testing.T, cfg PushConfig) (*Pusher, *pushTestFixture, *pushScheduleRecorder) {
	f := newPushFixture(t)
	f.setConfig(cfg)
	stamps := &pushScheduleRecorder{}
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	p.client.Transport = pushFailingTransport(func(r *http.Request) (*http.Response, error) {
		stamps.add(r.URL.String())
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})
	go p.Run()
	synctest.Wait()
	return p, f, stamps
}
func TestI805Wake(t *testing.T) {
	for _, wake := range []bool{false, true} {
		t.Run(map[bool]string{false: "control", true: "repeated_wake"}[wake], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p, _, stamps := newPushScheduleFixture(t, PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: 60 * time.Second})
				defer p.Stop()
				for i := 0; i < 18; i++ {
					time.Sleep(10 * time.Second)
					if wake {
						p.Wake()
					}
					synctest.Wait()
				}
				want := 4
				if len(stamps.get()) != want {
					t.Fatalf("requests=%d want scheduled behavior=%d", len(stamps.get()), want)
				}
				t.Logf("180 virtual seconds: requests=%d", len(stamps.get()))
			})
		})
	}
}
func TestI805IntervalChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: 60 * time.Second}
		p, f, stamps := newPushScheduleFixture(t, cfg)
		defer p.Stop()
		start := time.Now()
		time.Sleep(50 * time.Second)
		cfg.Interval = 20 * time.Second
		f.setConfig(cfg)
		p.Wake()
		synctest.Wait()
		if len(stamps.get()) != 2 || stamps.get()[1].Sub(start) != 50*time.Second {
			t.Fatal("overdue shortened interval must send once at t=50")
		}
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if len(stamps.get()) != 3 || stamps.get()[2].Sub(start) != 70*time.Second {
			t.Fatalf("stamps=%v", stamps.get())
		}
		t.Log("60s -> 20s at t=50s: next request at t=50s, then t=70s")
	})
}
func TestI806BelowMinimum(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, _, stamps := newPushScheduleFixture(t, PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: time.Second})
		defer p.Stop()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if len(stamps.get()) != 1 {
			t.Fatalf("requests=%d", len(stamps.get()))
		}
		t.Log("interval=1s: 1 request in 3 virtual seconds")
	})
}

func TestI805LengthenAndURL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: 60 * time.Second}
		p, f, stamps := newPushScheduleFixture(t, cfg)
		defer p.Stop()
		start := time.Now()
		time.Sleep(30 * time.Second)
		cfg.Interval = 120 * time.Second
		f.setConfig(cfg)
		p.Wake()
		synctest.Wait()
		time.Sleep(89 * time.Second)
		synctest.Wait()
		if len(stamps.get()) != 1 {
			t.Fatal("early send")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(stamps.get()) != 2 || stamps.get()[1].Sub(start) != 120*time.Second {
			t.Fatal("must use last completion + new interval")
		}
		cfg.URL = "https://example.invalid/b"
		f.setConfig(cfg)
		p.Wake()
		synctest.Wait()
		if len(stamps.get()) != 3 {
			t.Fatal("URL must send immediately")
		}
		cfg.Enabled = false
		f.setConfig(cfg)
		p.Wake()
		synctest.Wait()
		time.Sleep(300 * time.Second)
		synctest.Wait()
		if len(stamps.get()) != 3 {
			t.Fatal("disabled sent")
		}
		cfg.Enabled = true
		f.setConfig(cfg)
		p.Wake()
		synctest.Wait()
		if len(stamps.get()) != 4 {
			t.Fatal("enable must send immediately")
		}
	})
}
func TestI805SlowCompletionAnchor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newPushFixture(t)
		f.setConfig(PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: 60 * time.Second})
		stamps := &pushScheduleRecorder{}
		p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
		defer p.Stop()
		p.client.Transport = pushFailingTransport(func(r *http.Request) (*http.Response, error) {
			stamps.add(r.URL.String())
			time.Sleep(5 * time.Second)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
		})
		start := time.Now()
		go p.Run()
		synctest.Wait()
		time.Sleep(5 * time.Second)
		synctest.Wait()
		time.Sleep(59 * time.Second)
		p.Wake()
		synctest.Wait()
		if len(stamps.get()) != 1 {
			t.Fatal("early")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(stamps.get()) != 2 || stamps.get()[1].Sub(start) != 65*time.Second {
			t.Fatal("completion anchor not preserved")
		}
	})
}

func TestI806Intervals(t *testing.T) {
	for _, tc := range []struct {
		name        string
		input, want time.Duration
	}{
		{"negative", -time.Second, 60 * time.Second}, {"zero", 0, 60 * time.Second}, {"tiny", time.Nanosecond, 20 * time.Second},
		{"below", 19 * time.Second, 20 * time.Second}, {"minimum", 20 * time.Second, 20 * time.Second}, {"default", 60 * time.Second, 60 * time.Second}, {"long", time.Hour, time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p, _, rec := newPushScheduleFixture(t, PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: tc.input})
				defer p.Stop()
				start := time.Now()
				time.Sleep(tc.want - time.Nanosecond)
				synctest.Wait()
				if len(rec.get()) != 1 {
					t.Fatal("before boundary")
				}
				time.Sleep(time.Nanosecond)
				synctest.Wait()
				if len(rec.get()) != 2 || rec.get()[1].Sub(start) != tc.want {
					t.Fatal("wrong effective interval")
				}
			})
		})
	}
}
func TestI806EquivalentInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: time.Nanosecond}
		p, f, rec := newPushScheduleFixture(t, cfg)
		defer p.Stop()
		time.Sleep(15 * time.Second)
		cfg.Interval = 19 * time.Second
		cfg.URL = "  https://example.invalid/a  "
		f.setConfig(cfg)
		p.Wake()
		synctest.Wait()
		if len(rec.get()) != 1 {
			t.Fatal("equivalent config sent extra request")
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if len(rec.get()) != 2 {
			t.Fatal("equivalent config reset deadline")
		}
	})
}
func TestI805InvalidURL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, attempts := captureInvalidURLAttempts(t)
		cfg := PushConfig{Enabled: true, URL: "ftp://invalid/token", Interval: time.Nanosecond}
		p, f, rec := newPushScheduleFixture(t, cfg)
		defer p.Stop()
		if len(attempts) != 1 {
			t.Fatal("initial validation missing")
		}
		for i := 0; i < 19; i++ {
			time.Sleep(time.Second)
			p.Wake()
			synctest.Wait()
		}
		if len(attempts) != 1 {
			t.Fatal("Wake accelerated invalid validation")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(attempts) != 2 {
			t.Fatal("validation deadline was postponed")
		}
		if len(rec.get()) != 0 || f.deps.pinger.Calls() != 0 {
			t.Fatal("invalid URL entered network/health")
		}
		cfg.URL = "https://example.invalid/a"
		f.setConfig(cfg)
		p.Wake()
		synctest.Wait()
		if len(rec.get()) != 1 {
			t.Fatal("recovery missing")
		}
	})
}
func TestI805TimerReadsLatest(t *testing.T) {
	for _, mode := range []string{"disable", "url", "lengthen"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: 60 * time.Second}
				p, f, rec := newPushScheduleFixture(t, cfg)
				defer p.Stop()
				time.Sleep(59 * time.Second)
				switch mode {
				case "disable":
					cfg.Enabled = false
				case "url":
					cfg.URL = "https://example.invalid/b"
				case "lengthen":
					cfg.Interval = 120 * time.Second
				}
				f.setConfig(cfg) // 刻意不 Wake，独立验证到期分支重读配置。
				time.Sleep(time.Second)
				synctest.Wait()
				want := 1
				if mode == "url" {
					want = 2
				}
				if len(rec.get()) != want {
					t.Fatal("timer used stale config")
				}
				if mode == "url" && !strings.HasPrefix(rec.getURLs()[1], cfg.URL+"?") {
					t.Fatal("timer sent to old URL")
				}
			})
		})
	}
}
func TestI805FailuresAndStop(t *testing.T) {
	for _, mode := range []string{"network", "http", "json", "read", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newPushFixture(t)
				f.setConfig(PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: time.Nanosecond})
				rec := &pushScheduleRecorder{}
				p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
				defer p.Stop()
				p.client.Transport = pushFailingTransport(func(r *http.Request) (*http.Response, error) {
					rec.add(r.URL.String())
					if mode == "network" {
						return nil, errors.New("offline")
					}
					code := 200
					var body io.Reader = strings.NewReader(`{"ok":false}`)
					switch mode {
					case "http":
						code = 500
					case "json":
						body = strings.NewReader("bad")
					case "read":
						body = pushResponseFailReader{}
					case "oversize":
						body = strings.NewReader(strings.Repeat(" ", 20000))
					}
					return &http.Response{StatusCode: code, Body: io.NopCloser(body)}, nil
				})
				go p.Run()
				synctest.Wait()
				for i := 0; i < 19; i++ {
					time.Sleep(time.Second)
					p.Wake()
					synctest.Wait()
				}
				if len(rec.get()) != 1 {
					t.Fatal("failure introduced early retry")
				}
				time.Sleep(time.Second)
				synctest.Wait()
				if len(rec.get()) != 2 {
					t.Fatal("failure stopped period")
				}
				p.Stop()
				time.Sleep(time.Hour)
				synctest.Wait()
				if len(rec.get()) != 2 {
					t.Fatal("sent after stop")
				}
			})
		})
	}
}
func TestI805InFlightChanges(t *testing.T) {
	for _, mode := range []string{"disable", "url", "interval", "stop"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newPushFixture(t)
				cfg := PushConfig{Enabled: true, URL: "https://example.invalid/a", Interval: 60 * time.Second}
				f.setConfig(cfg)
				rec := &pushScheduleRecorder{}
				gate := make(chan struct{})
				p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
				defer p.Stop()
				p.client.Transport = pushFailingTransport(func(r *http.Request) (*http.Response, error) {
					rec.add(r.URL.String())
					if len(rec.get()) == 1 {
						select {
						case <-gate:
						case <-r.Context().Done():
							return nil, r.Context().Err()
						}
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
				})
				start := time.Now()
				go p.Run()
				synctest.Wait()
				time.Sleep(5 * time.Second)
				if mode == "stop" {
					p.Stop()
					if p.InFlight() {
						t.Fatal("stop retained in-flight")
					}
					return
				}
				switch mode {
				case "disable":
					cfg.Enabled = false
				case "url":
					cfg.URL = "https://example.invalid/b"
				case "interval":
					cfg.Interval = 20 * time.Second
				}
				f.setConfig(cfg)
				p.Wake()
				synctest.Wait()
				if len(rec.get()) != 1 || !p.InFlight() {
					t.Fatal("Wake started overlap or cancelled old send")
				}
				close(gate)
				synctest.Wait()
				if mode == "url" {
					if len(rec.get()) != 2 || rec.get()[1].Sub(start) != 5*time.Second {
						t.Fatal("URL not consumed after completion")
					}
					if !strings.HasPrefix(rec.getURLs()[1], cfg.URL+"?") {
						t.Fatal("in-flight change sent to old URL")
					}
				} else if mode == "disable" {
					time.Sleep(120 * time.Second)
					synctest.Wait()
					if len(rec.get()) != 1 {
						t.Fatal("disabled sent")
					}
				} else {
					time.Sleep(20 * time.Second)
					synctest.Wait()
					if len(rec.get()) != 2 || rec.get()[1].Sub(start) != 25*time.Second {
						t.Fatal("new interval didn't use in-flight completion")
					}
				}
			})
		})
	}
}
func TestI806DirtySQLite(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	}()
	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), "UPDATE uptime_kuma_push SET enabled=1,url=?,interval=? WHERE id=1", "https://example.invalid/a", "1ns"); err != nil {
		if e := tx.Rollback(); e != nil {
			t.Error(e)
		}
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadBusinessSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	cfg := snapshot.ToRuntimeConfig().UptimeKumaPush
	if cfg.Interval != time.Nanosecond {
		t.Fatal("loader silently changed dirty config")
	}
	synctest.Test(t, func(t *testing.T) {
		p, _, rec := newPushScheduleFixture(t, PushConfig{Enabled: cfg.Enabled, URL: cfg.URL, Interval: cfg.Interval})
		defer p.Stop()
		start := time.Now()
		time.Sleep(20*time.Second - time.Nanosecond)
		synctest.Wait()
		if len(rec.get()) != 1 {
			t.Fatal("dirty SQLite caused rapid sends")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if len(rec.get()) != 2 || rec.get()[1].Sub(start) != 20*time.Second {
			t.Fatal("dirty config not clamped at consumer")
		}
	})
}
