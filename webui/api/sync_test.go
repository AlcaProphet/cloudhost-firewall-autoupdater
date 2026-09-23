package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// stubSyncer 测试用模拟 Syncer（实现 api.Syncer 接口）
type stubSyncer struct {
	enabled   bool
	triggered atomic.Bool
	paused    atomic.Bool
	resumed   atomic.Bool
}

func (s *stubSyncer) Status() syncer.SyncStatus {
	return syncer.SyncStatus{Running: true, Enabled: s.enabled}
}
func (s *stubSyncer) TriggerSync() { s.triggered.Store(true) }
func (s *stubSyncer) DryRun() (syncer.DryRunResponse, error) {
	return syncer.DryRunResponse{Results: []syncer.DryRunResult{}}, nil
}
func (s *stubSyncer) Pause()  { s.enabled = false; s.paused.Store(true) }
func (s *stubSyncer) Resume() { s.enabled = true; s.resumed.Store(true) }

func doPost(t *testing.T, d *Deps, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	d.Register(mux)
	mux.ServeHTTP(w, req)
	return w
}

// TestHandleSyncTrigger_Paused 暂停状态下 trigger 返回 409
func TestHandleSyncTrigger_Paused(t *testing.T) {
	d := &Deps{Syncer: &stubSyncer{enabled: false}}
	w := doPost(t, d, "/api/sync/trigger")

	if w.Code != http.StatusConflict {
		t.Errorf("状态码 = %d, want 409", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "同步已暂停，请先开启") {
		t.Errorf("响应体 = %s, want 包含「同步已暂停，请先开启」", body)
	}
}

// TestHandleSyncTrigger_Enabled 开启状态下 trigger 正常触发
func TestHandleSyncTrigger_Enabled(t *testing.T) {
	s := &stubSyncer{enabled: true}
	d := &Deps{Syncer: s}
	w := doPost(t, d, "/api/sync/trigger")

	if w.Code != http.StatusAccepted {
		t.Errorf("状态码 = %d, want 202", w.Code)
	}
	if !s.triggered.Load() {
		t.Error("TriggerSync 应被调用")
	}
}

// TestHandleSyncPauseResume pause/resume 端点：200 + settings 表持久化
func TestHandleSyncPauseResume(t *testing.T) {
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer store.Close()
	s := &stubSyncer{enabled: true}
	d := &Deps{Store: store, Syncer: s}

	// 暂停
	w := doPost(t, d, "/api/sync/pause")
	if w.Code != http.StatusOK {
		t.Errorf("pause 状态码 = %d, want 200", w.Code)
	}
	if !s.paused.Load() {
		t.Error("Syncer.Pause 应被调用")
	}
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["sync_enabled"] != "false" {
		t.Errorf("sync_enabled = %s, want false", settings["sync_enabled"])
	}

	// 恢复
	w = doPost(t, d, "/api/sync/resume")
	if w.Code != http.StatusOK {
		t.Errorf("resume 状态码 = %d, want 200", w.Code)
	}
	if !s.resumed.Load() {
		t.Error("Syncer.Resume 应被调用")
	}
	settings, err = store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if settings["sync_enabled"] != "true" {
		t.Errorf("sync_enabled = %s, want true", settings["sync_enabled"])
	}
}

// ─── Step 1（R5-02）：SSE 请求生命周期与订阅清理测试 ───
//
// 固定口径（Build6 Step 1）：SSE 连接生命周期只由 request context 决定；
// 本 Step 不引入服务器级 shutdown channel（Step 3）。

// countingEventSubscriber 包装真实 EventBus，记录 SubscribeChan 与取消订阅调用次数
// 用于在 webui/api 包内观察 SSE handler 是否执行了 defer unsubscribe()（不新增生产 API）
type countingEventSubscriber struct {
	bus         *notifier.EventBus
	subscribes  atomic.Int32
	cancels     atomic.Int32
	firstCancel chan struct{} // 首次取消时关闭
}

func newCountingEventSubscriber() *countingEventSubscriber {
	return &countingEventSubscriber{
		bus:         notifier.NewEventBus(),
		firstCancel: make(chan struct{}),
	}
}

func (c *countingEventSubscriber) SubscribeChan() (<-chan notifier.Event, func()) {
	c.subscribes.Add(1)
	ch, cancel := c.bus.SubscribeChan()
	var once sync.Once
	return ch, func() {
		c.cancels.Add(1)
		cancel()
		once.Do(func() { close(c.firstCancel) })
	}
}

// waitForSSESubscribe 等待 SSE handler 建立订阅（超时失败，不依赖固定 sleep）
func waitForSSESubscribe(t *testing.T, sub *countingEventSubscriber) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sub.subscribes.Load() == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("SSE handler 未建立事件订阅")
}

// TestHandleSyncEvents_ContextCancelUnsubscribes 取消 request context 后 handler 退出并执行 defer unsubscribe()
func TestHandleSyncEvents_ContextCancelUnsubscribes(t *testing.T) {
	sub := newCountingEventSubscriber()
	d := &Deps{EventBus: sub}
	mux := http.NewServeMux()
	d.Register(mux)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/sync/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		mux.ServeHTTP(w, req)
	}()

	waitForSSESubscribe(t, sub)
	cancel()

	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("取消 request context 后 SSE handler 未退出")
	}
	if got := sub.cancels.Load(); got != 1 {
		t.Errorf("取消订阅调用次数 = %d, want 1（defer unsubscribe 必须生效）", got)
	}
}

// TestHandleSyncEvents_ClientDisconnectUnsubscribes 真实 HTTP 连接断开后 handler 退出并取消订阅
func TestHandleSyncEvents_ClientDisconnectUnsubscribes(t *testing.T) {
	sub := newCountingEventSubscriber()
	d := &Deps{EventBus: sub}
	mux := http.NewServeMux()
	d.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("连接测试服务器失败: %v", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
			t.Errorf("关闭连接失败: %v", cerr)
		}
	}()

	if _, err := fmt.Fprintf(conn, "GET /api/sync/events HTTP/1.1\r\nHost: %s\r\n\r\n", srv.Listener.Addr().String()); err != nil {
		t.Fatalf("发送 SSE 请求失败: %v", err)
	}
	waitForSSESubscribe(t, sub)

	// 通过真实 EventBus 发布事件，客户端应收到 SSE data 帧（证明 handler 在真实连接上推送）
	sub.bus.Publish(notifier.Event{Type: notifier.EventSyncComplete, Timestamp: time.Now()})
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("设置读取超时失败: %v", err)
	}
	reader := bufio.NewReader(conn)
	gotData := false
	for i := 0; i < 20; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("读取 SSE 响应失败: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			gotData = true
			break
		}
	}
	if !gotData {
		t.Fatal("未收到 SSE data 帧")
	}

	// 客户端断开连接 → 服务端 request context 取消 → handler 退出并取消订阅
	if err := conn.Close(); err != nil {
		t.Fatalf("关闭连接失败: %v", err)
	}
	select {
	case <-sub.firstCancel:
	case <-time.After(3 * time.Second):
		t.Fatal("客户端断开后 SSE handler 未退出或未取消订阅")
	}
	if got := sub.cancels.Load(); got != 1 {
		t.Errorf("取消订阅调用次数 = %d, want 1", got)
	}
}
