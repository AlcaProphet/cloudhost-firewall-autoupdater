package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// 心跳夹具只在完成屏障后观察；锁保护计时推进后的并发写入。
type i8r11Writer struct {
	mu       sync.Mutex
	header   http.Header
	frames   []string
	calls    []string
	budgets  []time.Duration
	mode     string
	statuses []int
}

func (w *i8r11Writer) Flush()              { panic("夹具应使用 FlushError") }
func (w *i8r11Writer) Header() http.Header { return w.header }
func (w *i8r11Writer) WriteHeader(n int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.statuses = append(w.statuses, n)
}
func (w *i8r11Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, "write")
	if w.mode == "write" {
		return 0, errors.New("fixture write")
	}
	w.frames = append(w.frames, string(p))
	return len(p), nil
}
func (w *i8r11Writer) FlushError() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, "flush")
	if w.mode == "flush" {
		return errors.New("fixture flush")
	}
	return nil
}
func (w *i8r11Writer) SetWriteDeadline(v time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	phase := "set"
	if v.IsZero() {
		phase = "clear"
	} else {
		w.budgets = append(w.budgets, v.Sub(time.Now()))
	}
	w.calls = append(w.calls, phase)
	if w.mode == phase {
		return errors.New("fixture " + phase)
	}
	return nil
}
func (w *i8r11Writer) observe() ([]string, []string, []time.Duration, []int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.frames...), append([]string(nil), w.calls...), append([]time.Duration(nil), w.budgets...), append([]int(nil), w.statuses...)
}
func (w *i8r11Writer) arm(s string) { w.mu.Lock(); defer w.mu.Unlock(); w.mode = s }

type i8r11Session struct {
	w        *i8r11Writer
	sub      *countingEventSubscriber
	b        *LogBroadcaster
	cancel   context.CancelFunc
	shutdown chan struct{}
	done     chan struct{}
	log      bool
}

func i8r11Start(t *testing.T, logStream bool) *i8r11Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &i8r11Session{w: &i8r11Writer{header: http.Header{"X-Accel-Buffering": []string{"yes"}}}, sub: newCountingEventSubscriber(), b: NewLogBroadcaster("info"), cancel: cancel, shutdown: make(chan struct{}), done: make(chan struct{}), log: logStream}
	d := &Deps{EventBus: s.sub, LogBroadcaster: s.b, ShutdownCh: s.shutdown}
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	go func() {
		defer close(s.done)
		if logStream {
			d.handleLogStream(s.w, req)
		} else {
			d.handleSyncEvents(s.w, req)
		}
	}()
	t.Cleanup(func() {
		cancel()
		synctest.Wait()
		select {
		case <-s.done:
		default:
			t.Error("handler未退出")
		}
		i8r11Retired(t, s)
	})
	synctest.Wait()
	if s.w.header.Get("Content-Type") != "text/event-stream" || s.w.header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("响应头合同错误: %v", s.w.header)
	}
	return s
}
func i8r11Retired(t *testing.T, s *i8r11Session) {
	t.Helper()
	if s.log {
		if s.b.subCount() != 0 {
			t.Error("日志订阅未清理")
		}
	} else if s.sub.cancels.Load() != 1 || s.sub.active.Load() != 0 {
		t.Error("同步订阅未恰好取消一次")
	}
}
func i8r11Cases(t *testing.T, fn func(*testing.T, bool)) {
	t.Helper()
	for _, c := range []struct {
		name string
		log  bool
	}{{"sync", false}, {"log", true}} {
		t.Run(c.name, func(t *testing.T) { synctest.Test(t, func(t *testing.T) { fn(t, c.log) }) })
	}
}
func TestI8R11HeartbeatPeriod(t *testing.T) {
	i8r11Cases(t, func(t *testing.T, logStream bool) {
		s := i8r11Start(t, logStream)
		f, c, _, _ := s.w.observe()
		n := len(f)
		k := len(c)
		time.Sleep(14 * time.Second)
		synctest.Wait()
		f, _, _, _ = s.w.observe()
		if len(f) != n {
			t.Fatal("首个心跳过早")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		f, c, b, _ := s.w.observe()
		if len(f) != n+1 || f[n] != ": heartbeat\n\n" {
			t.Fatalf("15秒未收到注释心跳: %q", f)
		}
		if strings.Join(c[k:], ",") != "set,write,flush,clear" {
			t.Fatal(c[k:])
		}
		for _, v := range b {
			if v != 5*time.Second {
				t.Fatal("写预算漂移", v)
			}
		}
		time.Sleep(15 * time.Second)
		synctest.Wait()
		f, _, _, _ = s.w.observe()
		if len(f) != n+2 || f[n+1] != ": heartbeat\n\n" {
			t.Fatal("第二周期错误", f)
		}
		if s.b.seq != 0 {
			t.Fatal("心跳推进日志游标")
		}
	})
}
func TestI8R11HeartbeatTrafficDoesNotReset(t *testing.T) {
	i8r11Cases(t, func(t *testing.T, logStream bool) {
		s := i8r11Start(t, logStream)
		for i := 0; i < 2; i++ {
			time.Sleep(5 * time.Second)
			if logStream {
				if err := s.b.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "business", 0)); err != nil {
					t.Fatal(err)
				}
			} else {
				s.sub.bus.Publish(notifier.Event{Type: notifier.EventSyncComplete, Timestamp: time.Now(), Data: map[string]any{"marker": "business"}})
			}
			synctest.Wait()
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		f, _, _, _ := s.w.observe()
		if f[len(f)-1] != ": heartbeat\n\n" {
			t.Fatal("业务数据推迟心跳", f)
		}
		n := 0
		for _, v := range f {
			if strings.HasPrefix(v, "data: ") || strings.HasPrefix(v, "id: ") {
				n++
				if !strings.Contains(v, "business") {
					t.Fatal("业务载荷改变", v)
				}
			}
		}
		if n != 2 {
			t.Fatal("业务帧数", n)
		}
		if logStream && s.b.seq != 2 {
			t.Fatal("日志序号改变")
		}
	})
}
func TestI8R11HeartbeatFailureExit(t *testing.T) {
	for _, mode := range []string{"set", "write", "flush", "clear"} {
		t.Run(mode, func(t *testing.T) {
			i8r11Cases(t, func(t *testing.T, logStream bool) {
				s := i8r11Start(t, logStream)
				_, c, _, _ := s.w.observe()
				n := len(c)
				s.w.arm(mode)
				time.Sleep(15 * time.Second)
				synctest.Wait()
				select {
				case <-s.done:
				default:
					t.Fatal("心跳错误后未退出")
				}
				i8r11Retired(t, s)
				_, c, _, status := s.w.observe()
				want := map[string]string{"set": "set", "write": "set,write,clear", "flush": "set,write,flush,clear", "clear": "set,write,flush,clear"}[mode]
				if strings.Join(c[n:], ",") != want {
					t.Fatal("错误收尾顺序", c[n:], want)
				}
				if len(status) != 0 {
					t.Fatal("响应开始后补写状态码", status)
				}
				count := len(c)
				time.Sleep(30 * time.Second)
				synctest.Wait()
				_, c, _, _ = s.w.observe()
				if len(c) != count {
					t.Fatal("退出后仍写心跳")
				}
			})
		})
	}
}
func TestI8R11HeartbeatIdleExit(t *testing.T) {
	for _, mode := range []string{"context", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			i8r11Cases(t, func(t *testing.T, logStream bool) {
				s := i8r11Start(t, logStream)
				time.Sleep(14 * time.Second)
				synctest.Wait()
				if mode == "context" {
					s.cancel()
				} else {
					close(s.shutdown)
				}
				synctest.Wait()
				select {
				case <-s.done:
				default:
					t.Fatal("空闲退出等待心跳")
				}
				i8r11Retired(t, s)
				f, c, _, _ := s.w.observe()
				nf, nc := len(f), len(c)
				time.Sleep(31 * time.Second)
				synctest.Wait()
				f, c, _, _ = s.w.observe()
				if len(f) != nf || len(c) != nc {
					t.Fatal("退出后输出")
				}
			})
		})
	}
}

func TestI8R11SSEHeaders(t *testing.T) {
	i8r11Cases(t, func(t *testing.T, logStream bool) {
		s := i8r11Start(t, logStream)
		if s.w.header.Get("X-Accel-Buffering") != "no" {
			t.Fatal("代理缓冲头缺失")
		}
	})
}
