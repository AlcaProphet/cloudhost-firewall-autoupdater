package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// p310LogCapture 线程安全地捕获诊断日志，避免用 buffer 与 SSE goroutine 并发读写。
type p310LogCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *p310LogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *p310LogCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *p310LogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *p310LogCapture) WithGroup(string) slog.Handler      { return h }
func (h *p310LogCapture) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]slog.Record(nil), h.records...)
}

type p310SSEWriter struct {
	header http.Header
	ready  chan struct{}
	frames chan string
	once   sync.Once
}

func (w *p310SSEWriter) Header() http.Header              { return w.header }
func (w *p310SSEWriter) WriteHeader(int)                  {}
func (w *p310SSEWriter) Write(b []byte) (int, error)      { w.frames <- string(b); return len(b), nil }
func (w *p310SSEWriter) Flush()                           { w.once.Do(func() { close(w.ready) }) }
func (w *p310SSEWriter) SetWriteDeadline(time.Time) error { return nil }

type p310SecretMarshaler struct{}

func (p310SecretMarshaler) MarshalJSON() ([]byte, error) { return nil, errors.New("secret-p310-token") }

type p310SecretKey struct{}

func (p310SecretKey) MarshalText() ([]byte, error) { return nil, errors.New("secret-p310-token") }

// TestSyncEventsMarshalFailureContinues 通过真实 EventBus 发布坏事件和正常事件，
// 检查诊断不含敏感原文、正常帧完整、连接保留，以及取消后恰好退订一次。
func TestSyncEventsMarshalFailureContinues(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	cases := []struct {
		name    string
		value   any
		badTime bool
	}{
		{"function", func() {}, false}, {"channel", make(chan int), false}, {"nan", math.NaN(), false},
		{"cycle", cycle, false}, {"custom_marshaler", p310SecretMarshaler{}, false},
		{"map_key_marshaler", map[p310SecretKey]int{p310SecretKey{}: 1}, false}, {"invalid_time", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &p310LogCapture{}
			old := slog.Default()
			slog.SetDefault(slog.New(capture))
			defer slog.SetDefault(old)
			sub := newCountingEventSubscriber()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := &p310SSEWriter{header: make(http.Header), ready: make(chan struct{}), frames: make(chan string, 4)}
			d := &Deps{EventBus: sub}
			done := make(chan struct{})
			go func() {
				defer close(done)
				d.handleSyncEvents(w, httptest.NewRequest("GET", "/api/sync/events", nil).WithContext(ctx))
			}()
			defer func() {
				cancel()
				select {
				case <-done:
					if sub.cancels.Load() != 1 || sub.active.Load() != 0 {
						t.Error("取消后未恰好退订一次")
					}
				case <-time.After(time.Second):
					t.Error("handler 未结束")
				}
			}()
			select {
			case <-w.ready:
			case <-time.After(time.Second):
				t.Fatal("未订阅")
			}
			bad := notifier.Event{Type: notifier.EventSyncError, Timestamp: time.Now(), Data: map[string]any{"bad": tc.value, "token": "secret-p310-token"}}
			if tc.badTime {
				bad.Timestamp = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			_, marshalErr := json.Marshal(bad)
			if marshalErr == nil {
				t.Fatal("夹具未触发序列化失败")
			}
			sub.bus.Publish(bad)
			sub.bus.Publish(notifier.Event{Type: notifier.EventSyncComplete, Timestamp: time.Now(), Data: map[string]any{"marker": "good"}})
			select {
			case frame := <-w.frames:
				if !strings.HasPrefix(frame, "data: ") || !strings.HasSuffix(frame, "\n\n") {
					t.Fatalf("SSE 帧格式错误: %q", frame)
				}
				var got notifier.Event
				if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(frame, "data: "), "\n\n")), &got); err != nil {
					t.Fatalf("正常帧无法解码: %v", err)
				}
				if got.Type != notifier.EventSyncComplete || got.Data["marker"] != "good" || strings.Contains(frame, "secret-p310-token") {
					t.Fatal("坏事件被发送或后续正常事件丢失")
				}
			case <-done:
				t.Fatal("坏事件中断后续正常事件")
			case <-time.After(time.Second):
				t.Fatal("未收到正常事件")
			}
			records := capture.snapshot()
			warnings := 0
			for _, r := range records {
				if r.Level != slog.LevelWarn {
					continue
				}
				warnings++
				if r.Message != "同步事件 SSE 序列化失败，跳过事件" || r.NumAttrs() != 2 {
					t.Error("诊断只应包含固定消息及两项安全元数据")
				}
				attrs := map[string]string{}
				r.Attrs(func(a slog.Attr) bool {
					attrs[a.Key] = a.Value.String()
					if strings.Contains(a.Value.String(), "secret-p310-token") {
						t.Error("错误日志泄露数据")
					}
					return true
				})
				if attrs["type"] != string(bad.Type) || attrs["error_type"] != fmt.Sprintf("%T", marshalErr) {
					t.Errorf("诊断元数据错误: %v", attrs)
				}
			}
			if warnings != 1 {
				t.Errorf("warn=%d want 1", warnings)
			}
			select {
			case frame := <-w.frames:
				t.Errorf("多余帧（坏事件不应有输出）: %q", frame)
			default:
			}
			select {
			case <-done:
				t.Error("连接被结束")
			default:
			}
		})
	}
}
