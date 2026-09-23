package webui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// ─── 测试辅助 ───

// newTestServer 创建仅绑定到 127.0.0.1 的测试服务器（不依赖业务 Store 的路由）。
func newTestServer(t *testing.T, port int) *Server {
	t.Helper()
	// store 为 nil：健康端点和 SPA 静态资源不读取 Store
	return NewServer(nil, "127.0.0.1", port)
}

// newTestServerWithStore 创建带真实临时 SQLite 的测试服务器，用于普通业务 API 回归。
func newTestServerWithStore(t *testing.T, port int) *Server {
	t.Helper()
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "webui-test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("关闭测试数据库失败: %v", err)
		}
	})
	return NewServer(store, "127.0.0.1", port)
}

// startTestServer 用空闲首选端口启动测试服务器，返回服务器与真实端口，
// 并注册 Shutdown 清理。使用真实空闲端口而非 0，避免把“OS 分配随机端口”
// 误判为“首选端口被占用后的降级”。
func startTestServer(t *testing.T, s *Server) (*Server, int) {
	t.Helper()
	port, err := s.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown 失败: %v", err)
		}
	})
	mustServeStarted(t, s)
	return s, port
}

// mustServeStarted 等待 Serve goroutine 启动（确定性同步，不依赖固定 sleep）
func mustServeStarted(t *testing.T, s *Server) {
	t.Helper()
	select {
	case <-s.ServeStarted():
	case <-time.After(3 * time.Second):
		t.Fatal("Serve goroutine 未在限期内启动")
	}
}

// freeTCPPort 申请一个当前空闲端口并立即释放。
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请空闲端口失败: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("释放探测端口失败: %v", err)
	}
	return port
}

// httpGet 发起普通 GET 请求，返回响应体与状态码。
func httpGet(t *testing.T, url string) (string, int) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s 失败: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取 %s 响应失败: %v", url, err)
	}
	return string(body), resp.StatusCode
}

// ─── Listener 与端口选择 ───

// TestStartUsesPreferredPort 首选端口可用时，实际端口等于首选端口且服务可访问
func TestStartUsesPreferredPort(t *testing.T) {
	preferred := freeTCPPort(t)
	s := newTestServer(t, preferred)

	port, err := s.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown 失败: %v", err)
		}
	}()

	if port != preferred {
		t.Fatalf("实际端口 = %d, want %d（首选端口可用时不得降级）", port, preferred)
	}
	if got := s.Addr(); got != net.JoinHostPort("127.0.0.1", strconv.Itoa(preferred)) {
		t.Errorf("Addr() = %q, want %q", got, net.JoinHostPort("127.0.0.1", strconv.Itoa(preferred)))
	}
	mustServeStarted(t, s)

	body, status := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", preferred))
	if status != http.StatusOK {
		t.Errorf("健康端点状态码 = %d, want 200", status)
	}
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("健康端点响应 = %q, want 包含 status ok", body)
	}
}

// TestStartFallsBackOnlyAfterEADDRINUSE 首选端口被真实 listener 占用时，
// 仅在 EADDRINUSE 情况下降级到随机端口；测试结束用 Shutdown 释放端口。
func TestStartFallsBackOnlyAfterEADDRINUSE(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占用测试端口失败: %v", err)
	}
	defer func() {
		if err := occupied.Close(); err != nil {
			t.Errorf("关闭占用端口失败: %v", err)
		}
	}()
	preferred := occupied.Addr().(*net.TCPAddr).Port

	s := newTestServer(t, preferred)
	port, err := s.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown 失败: %v", err)
		}
	}()

	if port == preferred {
		t.Fatalf("实际端口 = %d, 不应继续使用已被占用的首选端口", port)
	}
	if port == 0 {
		t.Fatal("降级后的随机端口不应为 0")
	}
	mustServeStarted(t, s)

	body, status := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if status != http.StatusOK || !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("随机端口健康端点 = (%d, %q), want (200, status ok)", status, body)
	}
}

// TestStartReturnsNonPortErrorsUnchanged 非端口占用的监听错误必须原样返回，不随机降级
func TestStartReturnsNonPortErrorsUnchanged(t *testing.T) {
	// 192.0.2.0/24 为 TEST-NET-1，本机不会分配该地址：
	// 绑定返回 EADDRNOTAVAIL（非 EADDRINUSE），因此不允许降级到随机端口。
	const host = "192.0.2.1"
	preferred := freeTCPPort(t)
	s := NewServer(nil, host, preferred)

	port, err := s.Start()
	if err == nil {
		_ = s.Shutdown(context.Background())
		t.Fatalf("绑定 %s 应失败，实际 port=%d err=nil", host, port)
	}
	if port != 0 {
		t.Errorf("失败时端口 = %d, want 0", port)
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		t.Skipf("地址 %s 未产生 EADDRNOTAVAIL（环境相关），跳过断言: %v", host, err)
	}
	if errors.Is(err, syscall.EADDRNOTAVAIL) {
		t.Logf("确认原样返回 EADDRNOTAVAIL: %v", err)
	} else {
		t.Logf("绑定错误非 EADDRINUSE，原样返回: %v", err)
	}
}

// TestStartDoesNotReleaseBoundListener 成功绑定到随机端口后，该 listener 一直被 Serve 持有，
// 中途不存在“释放后重新绑定”的窗口（等价于消除端口探测 TOCTOU）。
func TestStartDoesNotReleaseBoundListener(t *testing.T) {
	s := newTestServer(t, freeTCPPort(t))
	port, err := s.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown 失败: %v", err)
		}
	}()
	mustServeStarted(t, s)

	// 先与服务器完成一次真实连接，确定 Serve 已在监听
	if _, status := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port)); status != http.StatusOK {
		t.Fatalf("健康端点状态码 = %d, want 200", status)
	}

	// 端口仍被同一个 listener 占用：其他进程无法抢占
	probe, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err == nil {
		_ = probe.Close()
		t.Fatalf("端口 %d 在 Start 成功后仍可被重新绑定，说明 listener 已被释放", port)
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Errorf("重新绑定端口 %d 的错误 = %v, want EADDRINUSE", port, err)
	}
}

// ─── HTTP 功能回归 ───

// TestAccessURLAlwaysIncludesPort 日志访问地址必须始终包含端口
func TestAccessURLAlwaysIncludesPort(t *testing.T) {
	for _, tc := range []struct {
		host string
		want string
	}{
		{"127.0.0.1", "http://127.0.0.1:60200"},
		{"0.0.0.0", "http://0.0.0.0:60200"},
		{"::", "http://[::]:60200"},
		{"::1", "http://[::1]:60200"},
		{"[::]", "http://[::]:60200"},
		{"example.internal", "http://example.internal:60200"},
	} {
		if got := accessURL(tc.host, 60200); got != tc.want {
			t.Errorf("accessURL(%q, 60200) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

// TestHTTPRoutesRegression 真实 listener 上健康端点、普通业务 API 与 SPA 根页面均可用
func TestHTTPRoutesRegression(t *testing.T) {
	_, port := startTestServer(t, newTestServerWithStore(t, freeTCPPort(t)))

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	body, status := httpGet(t, base+"/api/health")
	if status != http.StatusOK || !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("/api/health = (%d, %q), want (200, status ok)", status, body)
	}

	// 普通业务 API：空数据库下 /api/targets 返回空数组
	body, status = httpGet(t, base+"/api/targets")
	if status != http.StatusOK {
		t.Errorf("/api/targets 状态码 = %d, want 200；响应: %s", status, body)
	}
	if strings.TrimSpace(body) != "[]" {
		t.Errorf("/api/targets 响应 = %q, want []", strings.TrimSpace(body))
	}

	// SPA 根页面（embed 的 webui/frontend/dist/index.html）
	body, status = httpGet(t, base+"/")
	if status != http.StatusOK {
		t.Errorf("SPA 根页面状态码 = %d, want 200", status)
	}
	if !strings.Contains(body, "<div id=\"app\">") {
		t.Errorf("SPA 根页面未返回 index.html 内容: %.120q", body)
	}
}

// ─── Server 生命周期 ───

// TestServerTimeoutContract 固定超时参数：SSE 长连接不会被全局短读写超时切断
func TestServerTimeoutContract(t *testing.T) {
	s := newTestServer(t, freeTCPPort(t))
	if _, err := s.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown 失败: %v", err)
		}
	}()

	s.mu.Lock()
	hs := s.httpServer
	s.mu.Unlock()

	if hs == nil {
		t.Fatal("Start 成功后必须持有显式 http.Server")
	}
	if hs.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 5s", hs.ReadHeaderTimeout)
	}
	if hs.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", hs.IdleTimeout)
	}
	if hs.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want 0（不得周期性切断 SSE）", hs.ReadTimeout)
	}
	if hs.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0（不得周期性切断 SSE）", hs.WriteTimeout)
	}
}

// TestShutdownNormalizesServeResult 正常 Shutdown 后 Wait 返回 nil
func TestShutdownNormalizesServeResult(t *testing.T) {
	s, port := startTestServer(t, newTestServer(t, freeTCPPort(t)))
	if _, status := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port)); status != http.StatusOK {
		t.Fatalf("健康端点状态码 = %d, want 200", status)
	}

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- s.Shutdown(context.Background()) }()

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("正常 Shutdown 返回错误: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown 未在限期内返回")
	}

	serveDone := make(chan error, 1)
	go func() { serveDone <- s.Wait() }()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Errorf("正常关闭后 Wait() = %v, want nil（ErrServerClosed 应被归一化）", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown 后 Wait 未返回（Serve goroutine 可能泄漏）")
	}
}

// TestShutdownIdempotent 重复 Shutdown 安全且不 double close，shutdown channel 只关闭一次
func TestShutdownIdempotent(t *testing.T) {
	s := newTestServer(t, freeTCPPort(t))
	if _, err := s.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	shutdownCh := s.ShutdownCh()
	if shutdownCh == nil {
		t.Fatal("服务器必须拥有服务器级 shutdown channel")
	}

	for i := 1; i <= 3; i++ {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatalf("第 %d 次 Shutdown 失败: %v", i, err)
		}
		select {
		case <-shutdownCh:
		default:
			t.Fatalf("第 %d 次 Shutdown 后 shutdown channel 应已关闭", i)
		}
	}

	// channel 已关闭：再读一次必须立即返回，说明只关闭了一次且未 panic
	select {
	case <-shutdownCh:
	case <-time.After(time.Second):
		t.Fatal("shutdown channel 状态异常")
	}
}

// TestShutdownBeforeStart 未启动时 Shutdown 安全，且不阻塞
func TestShutdownBeforeStart(t *testing.T) {
	s := newTestServer(t, freeTCPPort(t))
	done := make(chan error, 1)
	go func() { done <- s.Shutdown(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("未启动时 Shutdown 返回错误: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未启动时 Shutdown 不应阻塞")
	}

	// 未启动时不得创建 listener / http.Server
	if got := s.Addr(); got != "" {
		t.Errorf("未启动时 Addr() = %q, want 空字符串", got)
	}
	s.mu.Lock()
	hs, ln := s.httpServer, s.listener
	s.mu.Unlock()
	if hs != nil || ln != nil {
		t.Error("未启动时不应存在 http.Server 或 listener")
	}
}

// TestShutdownTwiceBeforeStart 未启动时重复 Shutdown 也安全
func TestShutdownTwiceBeforeStart(t *testing.T) {
	s := newTestServer(t, freeTCPPort(t))
	for i := 1; i <= 2; i++ {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatalf("未启动时第 %d 次 Shutdown 失败: %v", i, err)
		}
	}
	select {
	case <-s.ShutdownCh():
	default:
		t.Fatal("Shutdown 后 shutdown channel 应已关闭")
	}
}

// TestWaitBeforeStartBlocksUntilServeExits Start 前 Wait 必须阻塞，Serve 结束后才返回
func TestWaitBeforeStartBlocksUntilServeExits(t *testing.T) {
	s := newTestServer(t, freeTCPPort(t))

	waitDone := make(chan error, 1)
	go func() { waitDone <- s.Wait() }()

	select {
	case err := <-waitDone:
		t.Fatalf("Start 前 Wait 不应返回: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	if _, err := s.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	mustServeStarted(t, s)

	// Serve 仍在运行：Wait 必须继续阻塞
	select {
	case err := <-waitDone:
		t.Fatalf("Serve 运行中 Wait 不应返回: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}
	select {
	case err := <-waitDone:
		if err != nil {
			t.Errorf("Shutdown 后 Wait() = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown 后 Wait 未返回")
	}
}

// TestServeRuntimeErrorSurfacedToCaller 非关闭流程下的 Serve 错误必须原样交给调用方
func TestServeRuntimeErrorSurfacedToCaller(t *testing.T) {
	s, port := startTestServer(t, newTestServer(t, freeTCPPort(t)))
	_ = port

	// 在 Server 背后强制关闭同一个 listener，制造非正常 Serve 退出
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	if ln == nil {
		t.Fatal("Start 成功后必须持有 listener")
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("强制关闭 listener 失败: %v", err)
	}

	serveDone := make(chan error, 1)
	go func() { serveDone <- s.Wait() }()
	select {
	case err := <-serveDone:
		if err == nil {
			t.Fatal("关闭流程之外的 Serve 错误必须返回给调用方，实际为 nil")
		}
		t.Logf("Serve 非正常错误已交给调用方: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("listener 被关闭后 Wait 未返回")
	}

	// 清理：Serve 已退出，Shutdown 仍必须安全
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Serve 已退出后 Shutdown 失败: %v", err)
	}
}

// ─── shutdown 后的普通请求收尾 ───

// TestShutdownDrainsInflightRequest 进行中的普通请求可在 shutdown 窗口内自行完成
func TestShutdownDrainsInflightRequest(t *testing.T) {
	s, port := startTestServer(t, newTestServer(t, freeTCPPort(t)))
	_ = port

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	s.mux.HandleFunc("GET /slow-ok", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	type result struct {
		status int
		body   string
		err    error
	}
	reqDone := make(chan result, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(base + "/slow-ok")
		if err != nil {
			reqDone <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, readErr := io.ReadAll(resp.Body)
		reqDone <- result{status: resp.StatusCode, body: string(body), err: readErr}
	}()

	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("普通请求未进入 handler")
	}

	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- s.Shutdown(context.Background()) }()

	// 请求进行中：Shutdown 仍未返回
	select {
	case err := <-shutdownErr:
		t.Fatalf("进行中的请求未完成前 Shutdown 不应返回: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)

	select {
	case got := <-reqDone:
		if got.err != nil {
			t.Fatalf("进行中的请求应正常完成: %v", got.err)
		}
		if got.status != http.StatusOK || got.body != "done" {
			t.Errorf("进行中的请求响应 = (%d, %q), want (200, done)", got.status, got.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("释放后请求未完成")
	}

	select {
	case err := <-shutdownErr:
		if err != nil {
			t.Errorf("请求在窗口内完成，Shutdown 应返回 nil，实际: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("请求完成后 Shutdown 未返回")
	}
}

// TestShutdownTimeoutForcesCloseOfInflightRequest 超过 shutdown context 的请求导致
// Shutdown 有界返回（DeadlineExceeded），并强制关闭残余连接
func TestShutdownTimeoutForcesCloseOfInflightRequest(t *testing.T) {
	s, port := startTestServer(t, newTestServer(t, freeTCPPort(t)))
	_ = port

	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var once sync.Once
	s.mux.HandleFunc("GET /slow-hang", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-release
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	reqErr := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(base + "/slow-hang")
		if err != nil {
			reqErr <- err
			return
		}
		_ = resp.Body.Close()
		reqErr <- nil
	}()

	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("慢请求未进入 handler")
	}

	// 注入更短的 shutdown context，验证“有界返回 + 强制关闭”机制（生产上限仍为 10s）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := s.Shutdown(shutdownCtx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("超时 Shutdown 错误 = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Shutdown 耗时 %v，未按 context 有界返回", elapsed)
	}

	select {
	case gotErr := <-reqErr:
		if gotErr == nil {
			t.Error("强制 Close 后残余请求应失败")
		} else {
			t.Logf("强制关闭后残余请求错误: %v", gotErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("强制 Close 后残余请求未结束")
	}
}

// TestServerAcceptsNoNewRequestsAfterShutdown shutdown 完成后新连接必须失败（监听入口已关闭）
func TestServerAcceptsNoNewRequestsAfterShutdown(t *testing.T) {
	s := newTestServer(t, freeTCPPort(t))
	port, err := s.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	mustServeStarted(t, s)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if _, status := httpGet(t, base+"/api/health"); status != http.StatusOK {
		t.Fatalf("健康端点状态码 = %d, want 200", status)
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}

	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(base + "/api/health")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Shutdown 后新请求不应成功")
	}
	t.Logf("Shutdown 后新请求失败（符合预期）: %v", err)
}
