package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// ─── Issue6 A15：SSE 必须检查写错误，首个失败即退出 ───

// erroringResponseWriter 模拟已经可观察到 broken pipe 的连接（不是仍阻塞的慢客户端）：
//   - Flush() 成功；
//   - Write() 立即返回 io.ErrClosedPipe；
//   - 记录 WriteHeader 调用，用于断言「不得写第二个响应头」。
type erroringResponseWriter struct {
	header     http.Header
	writeCalls int
	writeErrs  int
	statuses   []int
}

func newErroringResponseWriter() *erroringResponseWriter {
	return &erroringResponseWriter{header: make(http.Header)}
}

func (w *erroringResponseWriter) Header() http.Header { return w.header }

func (w *erroringResponseWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
}

func (w *erroringResponseWriter) Write(p []byte) (int, error) {
	w.writeCalls++
	w.writeErrs++
	return 0, io.ErrClosedPipe
}

func (w *erroringResponseWriter) Flush() {}

func (w *erroringResponseWriter) SetWriteDeadline(time.Time) error { return nil }

// deadlineRecordingWriter 记录单次 SSE 写出的 deadline、Write 与 Flush 顺序。
type deadlineRecordingWriter struct {
	header    http.Header
	mu        sync.Mutex
	events    []string
	deadlines []time.Time
}

func newDeadlineRecordingWriter() *deadlineRecordingWriter {
	return &deadlineRecordingWriter{header: make(http.Header)}
}

func (w *deadlineRecordingWriter) Header() http.Header { return w.header }
func (w *deadlineRecordingWriter) WriteHeader(int)     {}
func (w *deadlineRecordingWriter) Write(p []byte) (int, error) {
	w.record("write")
	return len(p), nil
}
func (w *deadlineRecordingWriter) Flush() { w.record("flush") }
func (w *deadlineRecordingWriter) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	w.deadlines = append(w.deadlines, deadline)
	w.mu.Unlock()
	if deadline.IsZero() {
		w.record("deadline:clear")
	} else {
		w.record("deadline:set")
	}
	return nil
}

type flushErrorWriter struct {
	*deadlineRecordingWriter
	flushErr error
}

func newFlushErrorWriter(err error) *flushErrorWriter {
	return &flushErrorWriter{deadlineRecordingWriter: newDeadlineRecordingWriter(), flushErr: err}
}

func (w *flushErrorWriter) FlushError() error {
	w.record("flush")
	return w.flushErr
}
func (w *deadlineRecordingWriter) record(event string) {
	w.mu.Lock()
	w.events = append(w.events, event)
	w.mu.Unlock()
}

// flushOnlyWriter 支持 Flush，但故意不支持 SetWriteDeadline。
type flushOnlyWriter struct {
	header   http.Header
	statuses []int
}

func newFlushOnlyWriter() *flushOnlyWriter {
	return &flushOnlyWriter{header: make(http.Header)}
}

func (w *flushOnlyWriter) Header() http.Header { return w.header }
func (w *flushOnlyWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
}
func (w *flushOnlyWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *flushOnlyWriter) Flush()                      {}

type deadlineResponseRecorder struct{ *httptest.ResponseRecorder }

func newDeadlineResponseRecorder() *deadlineResponseRecorder {
	return &deadlineResponseRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (w *deadlineResponseRecorder) SetWriteDeadline(time.Time) error { return nil }

// noFlushWriter 是最小 ResponseWriter：不实现 Flusher，无法承载 SSE。
type noFlushWriter struct{}

func (noFlushWriter) Header() http.Header         { return make(http.Header) }
func (noFlushWriter) Write(p []byte) (int, error) { return len(p), nil }
func (noFlushWriter) WriteHeader(int)             {}

// TestHandleSyncEvents_WriteErrorExitsAndUnsubscribes 首个写错误必须立即退出并取消订阅。
//
// 判别性：修复前 fmt.Fprintf/Flush 的返回值被忽略，handler 会继续在 select 上循环、
// 订阅永不取消——本用例会因为 handlerDone 一直不关闭而超时失败。
func TestHandleSyncEvents_WriteErrorExitsAndUnsubscribes(t *testing.T) {
	sub := newCountingEventSubscriber()
	d := &Deps{EventBus: sub}
	mux := http.NewServeMux()
	d.Register(mux)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/sync/events", nil).WithContext(ctx)

	w := newErroringResponseWriter()

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		mux.ServeHTTP(w, req)
	}()

	waitForSSESubscribe(t, sub)

	sub.bus.Publish(notifier.Event{Type: notifier.EventSyncStart, Timestamp: time.Now()})

	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("首个写错误后 SSE handler 未退出（修复前会永久循环）")
	}

	if got := sub.cancels.Load(); got != 1 {
		t.Errorf("取消订阅调用次数 = %d, want 1（defer unsubscribe 必须生效）", got)
	}
	if got := sub.active.Load(); got != 0 {
		t.Errorf("活跃订阅数 = %d, want 0", got)
	}
	if w.writeErrs == 0 {
		t.Error("用例前提：必须至少发生一次写错误")
	}
	for _, s := range w.statuses {
		if s >= 400 {
			t.Errorf("写错误后不得写错误状态码，实际记录了 %d", s)
		}
	}
	if len(w.statuses) > 1 {
		t.Errorf("不得重复写响应头：WriteHeader 调用 %d 次", len(w.statuses))
	}
}

// failAfterResponseWriter 允许 reset 成功，再独立验证日志帧写失败。
type failAfterResponseWriter struct {
	*erroringResponseWriter
	allowed int
	calls   int
}

func (w *failAfterResponseWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls <= w.allowed {
		return len(p), nil
	}
	return w.erroringResponseWriter.Write(p)
}

// TestHandleLogStream_WriteErrorExitsAndUnsubscribes 分别覆盖 reset 与日志帧失败。
func TestHandleLogStream_WriteErrorExitsAndUnsubscribes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed int
	}{{"reset", 0}, {"log_after_reset", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewLogBroadcaster("info")
			if err := b.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "触发写错误", 0)); err != nil {
				t.Fatal(err)
			}
			d := &Deps{LogBroadcaster: b}
			mux := http.NewServeMux()
			d.Register(mux)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil).WithContext(ctx)
			w := &failAfterResponseWriter{erroringResponseWriter: newErroringResponseWriter(), allowed: tc.allowed}
			done := make(chan struct{})
			go func() { defer close(done); mux.ServeHTTP(w, req) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("首个写错误后日志流 SSE handler 未退出")
			}
			if got := b.subCount(); got != 0 {
				t.Errorf("写失败后残留订阅 %d", got)
			}
			if w.calls != tc.allowed+1 || w.writeErrs != 1 {
				t.Errorf("写调用=%d 错误=%d", w.calls, w.writeErrs)
			}
			for _, status := range w.statuses {
				if status >= 400 {
					t.Errorf("写错误后不得写错误状态码 %d", status)
				}
			}
			if len(w.statuses) > 1 {
				t.Errorf("重复写响应头 %d 次", len(w.statuses))
			}
		})
	}
}

// TestWriteSSE_SetsAndClearsDeadline 固定每条消息的 deadline 边界与调用顺序。
func TestWriteSSE_SetsAndClearsDeadline(t *testing.T) {
	w := newDeadlineRecordingWriter()
	started := time.Now()
	if err := writeSSE(w, "data: test\n\n"); err != nil {
		t.Fatalf("writeSSE 失败: %v", err)
	}

	w.mu.Lock()
	events := append([]string(nil), w.events...)
	deadlines := append([]time.Time(nil), w.deadlines...)
	w.mu.Unlock()
	want := []string{"deadline:set", "write", "flush", "deadline:clear"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("调用顺序 = %v, want %v", events, want)
	}
	if len(deadlines) != 2 {
		t.Fatalf("deadline 调用次数 = %d, want 2", len(deadlines))
	}
	if min, max := started.Add(sseWriteTimeout-time.Second), started.Add(sseWriteTimeout+time.Second); deadlines[0].Before(min) || deadlines[0].After(max) {
		t.Errorf("写 deadline = %v, want %v 到 %v", deadlines[0], min, max)
	}
	if !deadlines[1].IsZero() {
		t.Errorf("写完成后 deadline = %v, want 零值", deadlines[1])
	}
}

// TestHandleSyncEvents_InitialFlushErrorExitsAndUnsubscribes 初始 Flush 失败时
// 必须清除 deadline、直接退出且不得尝试写第二个错误响应。
func TestHandleSyncEvents_InitialFlushErrorExitsAndUnsubscribes(t *testing.T) {
	sub := newCountingEventSubscriber()
	d := &Deps{EventBus: sub}
	mux := http.NewServeMux()
	d.Register(mux)
	w := newFlushErrorWriter(context.DeadlineExceeded)
	req := httptest.NewRequest(http.MethodGet, "/api/sync/events", nil)

	mux.ServeHTTP(w, req)

	if got := sub.subscribes.Load(); got != 1 {
		t.Errorf("订阅次数 = %d, want 1", got)
	}
	if got := sub.cancels.Load(); got != 1 {
		t.Errorf("取消订阅次数 = %d, want 1", got)
	}
	w.mu.Lock()
	events := append([]string(nil), w.events...)
	w.mu.Unlock()
	want := []string{
		"deadline:set", "deadline:clear", // 能力探测
		"deadline:set", "flush", "deadline:clear", // 初始刷新
	}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Errorf("调用顺序 = %v, want %v", events, want)
	}
}

// TestHandleSyncEvents_NoDeadlineCapabilityReturns500 要求在写头和订阅之前拒绝
// 无法设置写 deadline 的 ResponseWriter。
func TestHandleSyncEvents_NoDeadlineCapabilityReturns500(t *testing.T) {
	sub := newCountingEventSubscriber()
	d := &Deps{EventBus: sub}
	mux := http.NewServeMux()
	d.Register(mux)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/sync/events", nil).WithContext(ctx)
	w := newFlushOnlyWriter()
	mux.ServeHTTP(w, req)

	if got := sub.subscribes.Load(); got != 0 {
		t.Errorf("deadline 能力检测未通过时不得建立订阅，实际 %d 次", got)
	}
	if len(w.statuses) != 1 || w.statuses[0] != http.StatusInternalServerError {
		t.Errorf("HTTP 状态 = %v, want [500]", w.statuses)
	}
	if got := w.header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
}

// TestHandleLogStream_NoDeadlineCapabilityReturns500 对日志 SSE 固定相同能力合同。
func TestHandleLogStream_NoDeadlineCapabilityReturns500(t *testing.T) {
	b := NewLogBroadcaster("info")
	d := &Deps{LogBroadcaster: b}
	mux := http.NewServeMux()
	d.Register(mux)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil).WithContext(ctx)
	w := newFlushOnlyWriter()
	mux.ServeHTTP(w, req)

	if got := b.subCount(); got != 0 {
		t.Errorf("deadline 能力检测未通过时不得建立订阅，实际 %d 个", got)
	}
	if len(w.statuses) != 1 || w.statuses[0] != http.StatusInternalServerError {
		t.Errorf("HTTP 状态 = %v, want [500]", w.statuses)
	}
}

type smallWriteBufferListener struct{ net.Listener }

func (l smallWriteBufferListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		if err := tcp.SetWriteBuffer(1024); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

// TestHandleSyncEvents_StoppedTCPReaderExitsAtWriteDeadline 使用真实 loopback TCP：
// 客户端读取初始响应头后保持 socket 打开但停止读取，服务端必须在单次写 deadline 后退出。
func TestHandleSyncEvents_StoppedTCPReaderExitsAtWriteDeadline(t *testing.T) {
	sub := newCountingEventSubscriber()
	d := &Deps{EventBus: sub}
	mux := http.NewServeMux()
	d.Register(mux)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	listener := smallWriteBufferListener{Listener: ln}
	srv := &http.Server{Handler: mux}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("连接测试服务器失败: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("关闭客户端连接失败: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("关闭测试服务器失败: %v", err)
		}
		select {
		case err := <-serveDone:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("测试服务器退出错误: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("测试服务器未退出")
		}
	})

	if _, err := io.WriteString(conn, "GET /api/sync/events HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("读取初始响应头失败: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	waitForSSESubscribe(t, sub)

	// 大消息配合服务端小写缓冲，确保客户端不读时进入真实写背压。
	sub.bus.Publish(notifier.Event{
		Type:      notifier.EventSyncStart,
		Timestamp: time.Now(),
		Data:      map[string]any{"payload": strings.Repeat("x", 16<<20)},
	})

	select {
	case <-sub.firstCancel:
	case <-time.After(7 * time.Second):
		t.Fatal("客户端停止读取后 SSE handler 未在写 deadline 内退出")
	}
	if got := sub.cancels.Load(); got != 1 {
		t.Errorf("取消订阅调用次数 = %d, want 1", got)
	}
}

// TestProbeSSE_DetectsCapabilityAndUnwraps 能力检测覆盖三种形态，并由 Unwrap 递归判定。
func TestProbeSSE_DetectsCapabilityAndUnwraps(t *testing.T) {
	if err := probeSSE(httptest.NewRecorder()); err == nil {
		t.Error("仅支持 Flush、不支持写 deadline 的 recorder 必须被拒绝")
	}
	if err := probeSSE(newErroringResponseWriter()); err != nil {
		t.Errorf("支持 Flush 与写 deadline 的 writer 必须通过 probeSSE: %v", err)
	}
	if err := probeSSE(noFlushWriter{}); err == nil {
		t.Error("不支持 Flush 的 writer 必须被 probeSSE 拒绝")
	}

	// http.ResponseController 会沿 Unwrap 采用底层 writer 的能力
	if err := probeSSE(struct{ *erroringResponseWriter }{newErroringResponseWriter()}); err != nil {
		t.Errorf("嵌套 writer 的底层能力必须被采用: %v", err)
	}
	if err := probeSSE(struct{ *noFlushWriter }{&noFlushWriter{}}); err == nil {
		t.Error("嵌套的无 Flush writer 必须被 probeSSE 拒绝")
	}
}

// TestHandleSyncEvents_NoFlushCapabilityReturns500 不支持 Flush 时按能力缺失返回 500。
//
// 此时尚未写出响应头，因此可以安全地改写状态码——与「写错误后不得再写头」不同。
func TestHandleSyncEvents_NoFlushCapabilityReturns500(t *testing.T) {
	sub := newCountingEventSubscriber()
	d := &Deps{EventBus: sub}
	mux := http.NewServeMux()
	d.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/sync/events", nil)
	w := noFlushWriter{}

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		mux.ServeHTTP(w, req)
	}()

	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("不支持 Flush 时 handler 必须立即返回")
	}
	if got := sub.subscribes.Load(); got != 0 {
		t.Errorf("能力检测未通过时不得建立订阅，实际 %d 次", got)
	}
}
