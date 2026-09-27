package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// ─── Issue6 A15：SSE 必须检查写错误，首个失败即退出 ───

// erroringResponseWriter 模拟半开连接 / 客户端停止读取：
//   - Flush() 成功（连接在 TCP 层面仍可写）；
//   - Write() 返回 io.ErrClosedPipe；
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

// TestHandleLogStream_WriteErrorExitsAndUnsubscribes 日志流 SSE 同样必须检查写错误。
func TestHandleLogStream_WriteErrorExitsAndUnsubscribes(t *testing.T) {
	b := NewLogBroadcaster("info")
	d := &Deps{LogBroadcaster: b}
	mux := http.NewServeMux()
	d.Register(mux)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil).WithContext(ctx)

	w := newErroringResponseWriter()

	before := b.subCount()
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		mux.ServeHTTP(w, req)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for b.subCount() != before+1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := b.subCount(); got != before+1 {
		t.Fatalf("建立订阅后订阅数 = %d, want %d", got, before+1)
	}

	if err := b.Handle(ctx, slog.NewRecord(time.Now(), slog.LevelInfo, "触发写错误", 0)); err != nil {
		t.Fatalf("LogBroadcaster.Handle 失败: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("首个写错误后日志流 SSE handler 未退出（修复前会永久循环）")
	}

	deadline = time.Now().Add(3 * time.Second)
	for b.subCount() != before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := b.subCount(); got != before {
		t.Errorf("订阅数 = %d, want 恢复到 %d（defer unsubscribe 必须生效）", got, before)
	}
	for _, s := range w.statuses {
		if s >= 400 {
			t.Errorf("写错误后不得写错误状态码，实际记录了 %d", s)
		}
	}
}

// TestProbeSSE_DetectsCapabilityAndUnwraps 能力检测覆盖三种形态，并由 Unwrap 递归判定。
func TestProbeSSE_DetectsCapabilityAndUnwraps(t *testing.T) {
	if !probeSSE(httptest.NewRecorder()) {
		t.Error("httptest.ResponseRecorder 支持 Flush，probeSSE 必须返回 true")
	}
	if !probeSSE(newErroringResponseWriter()) {
		t.Error("Flush 成功的 writer 必须通过 probeSSE")
	}
	if probeSSE(noFlushWriter{}) {
		t.Error("不支持 Flush 的 writer 必须被 probeSSE 判定为不可用")
	}

	// http.ResponseController 会沿 Unwrap 采用底层 writer 的能力
	if !probeSSE(struct{ *erroringResponseWriter }{newErroringResponseWriter()}) {
		t.Error("嵌套 writer 的 Unwrap 必须被采用（底层支持 Flush）")
	}
	if probeSSE(struct{ *noFlushWriter }{&noFlushWriter{}}) {
		t.Error("嵌套的无 Flush writer 必须被 probeSSE 判定为不可用")
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
