package webui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/webui/api"
)

// Listener 与 Serve 的固定参数（Build6 Step 3 契约）
const (
	// readHeaderTimeout 限制请求头读取时间，防止慢速请求头长期占用连接
	readHeaderTimeout = 5 * time.Second
	// idleTimeout 限制 keep-alive 空闲连接的存活时间；两类 SSE 连接处于活跃状态，不受该超时影响
	idleTimeout = 120 * time.Second
	// shutdownTimeout HTTP 收尾上限：超过后强制关闭剩余连接（由 main 以同值 context 传入）
	shutdownTimeout = 10 * time.Second
)

// Server WebUI HTTP 服务器。
//
// 生命周期契约（Build6 Step 3）：
//   - Start 同步完成 net.Listen，把同一个 listener 交给 http.Server.Serve，
//     不存在“先探测端口、关闭后再重新绑定”的窗口；
//   - Wait 把 Serve 的非正常错误交给 main；
//   - Shutdown 幂等：先关闭服务器级 SSE shutdown channel，再调用 http.Server.Shutdown；
//     超时后强制 http.Server.Close 结束剩余连接，不手工提前关闭 listener。
type Server struct {
	// 构造后不可变
	host         string
	port         int
	mux          *http.ServeMux
	deps         *api.Deps
	shutdownCh   chan struct{} // 服务器级 SSE shutdown 信号（只关闭一次，永不写入）
	shutdownOnce sync.Once
	serveOnce    sync.Once
	serveStarted chan struct{} // Serve goroutine 已启动（测试用确定性同步）
	serveDone    chan error    // Serve 结果；容量 1，保证 Serve goroutine 不因无人接收而阻塞

	// 生命周期状态（由 mu 保护）
	mu         sync.Mutex
	httpServer *http.Server
	listener   net.Listener
	shutdown   bool // 是否已进入关闭流程（用于归一化 ErrServerClosed/net.ErrClosed）
}

// NewServer 创建 WebUI 服务器
func NewServer(store *config.Store, host string, port int) *Server {
	s := &Server{
		host:         host,
		port:         port,
		mux:          http.NewServeMux(),
		shutdownCh:   make(chan struct{}),
		serveStarted: make(chan struct{}),
		serveDone:    make(chan error, 1),
	}
	s.deps = &api.Deps{Store: store, ShutdownCh: s.shutdownCh}
	s.registerRoutes()
	return s
}

// SetSyncer 设置同步引擎和事件总线（WebUI 模式下由 main.go 调用）
func (s *Server) SetSyncer(sync api.Syncer, bus api.EventSubscriber) {
	s.deps.Syncer = sync
	s.deps.EventBus = bus
}

// SetReloadFunc 设置配置重载回调（WebUI 修改配置后通知 Syncer）
func (s *Server) SetReloadFunc(fn func()) {
	s.deps.ReloadFunc = fn
}

// SetLogBroadcaster 设置日志广播器（实时日志流 SSE）
func (s *Server) SetLogBroadcaster(b *api.LogBroadcaster) {
	s.deps.LogBroadcaster = b
}

// ShutdownCh 返回服务器级 shutdown channel（两类 SSE handler 共用）。
func (s *Server) ShutdownCh() <-chan struct{} {
	return s.shutdownCh
}

// ServeStarted 返回 Serve goroutine 启动信号（关闭表示已启动）。
// 仅用于测试确定性同步：得到该信号后端口必然仍被同一个 listener 占用。
func (s *Server) ServeStarted() <-chan struct{} {
	return s.serveStarted
}

// Addr 返回实际监听地址；Start 成功前返回空字符串。
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// accessURL 构造供日志显示的访问地址。
// 监听全部接口时 net.JoinHostPort("0.0.0.0", port) 会得到不带端口的 "0.0.0.0:60200"，
// 这里显式拼接端口，保证日志中的访问地址始终包含端口。
func accessURL(host string, port int) string {
	suffix := ":" + strconv.Itoa(port)
	if strings.Contains(host, ":") && !strings.Contains(host, "]") {
		// 裸 IPv6 字面量需要方括号
		return "http://[" + host + "]" + suffix
	}
	return "http://" + host + suffix
}

// Start 同步建立 HTTP 监听并启动 Serve goroutine，返回实际监听端口。
//
// 端口语义：优先尝试 host:preferredPort；只有 errors.Is(err, syscall.EADDRINUSE)
// 时才降级到 host:0（由 OS 随机分配）。权限、非法地址等其他监听错误原样返回，
// 不做随机降级。成功创建的 listener 直接交给 http.Server.Serve，全程不释放端口。
func (s *Server) Start() (int, error) {
	preferred := s.port
	ln, err := net.Listen("tcp", net.JoinHostPort(s.host, strconv.Itoa(preferred)))
	if err != nil && errors.Is(err, syscall.EADDRINUSE) {
		ln, err = net.Listen("tcp", net.JoinHostPort(s.host, "0"))
	}
	if err != nil {
		return 0, err
	}

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		// 非 TCP 地址不应出现；无法确定端口时立即释放监听，避免端口泄漏
		closeErr := ln.Close()
		return 0, fmt.Errorf("监听地址 %q 不是 TCP 地址 (关闭错误: %v)", ln.Addr(), closeErr)
	}
	actualPort := tcpAddr.Port

	hs := &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		// ReadTimeout/WriteTimeout 保持零值：不设置会周期性切断
		// /api/sync/events 与 /api/logs/stream 的全局短超时。
	}

	s.mu.Lock()
	s.httpServer = hs
	s.listener = ln
	s.mu.Unlock()

	s.serveOnce.Do(func() {
		close(s.serveStarted)
		// listener/httpServer 已在启动 Serve goroutine 前发布到 Server 状态
		go func() { s.serveDone <- s.normalizeServeError(hs.Serve(ln)) }()
	})

	access := accessURL(s.host, actualPort)
	if actualPort != preferred {
		slog.Warn("端口已被占用，使用随机端口", "请求端口", preferred, "实际端口", actualPort, "访问地址", access)
	}
	slog.Info("WebUI 启动", "访问地址", access)
	return actualPort, nil
}

// Wait 等待 Serve 结束并返回其结果。
//
// http.ErrServerClosed 与 net.ErrClosed 只在已经进入关闭流程（Shutdown 已调用）时
// 归一化为 nil；关闭流程之前出现的 Serve 错误原样返回给调用方。
func (s *Server) Wait() error {
	return <-s.serveDone
}

// normalizeServeError 归一化 Serve 返回值。
func (s *Server) normalizeServeError(err error) error {
	if err == nil {
		return nil
	}
	s.mu.Lock()
	shuttingDown := s.shutdown
	s.mu.Unlock()

	if shuttingDown && (errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed)) {
		return nil
	}
	return err
}

// Shutdown 幂等优雅关闭。
//
//   - 先关闭服务器级 SSE shutdown channel：两类 SSE handler 据此主动退出并执行既有
//     defer unsubscribe()，不依赖 http.Server.Shutdown 自动取消长连接；
//   - 再调用 http.Server.Shutdown(ctx) 停止接收新连接并等待普通请求完成；
//   - ctx 超时后强制 http.Server.Close() 结束剩余 HTTP 连接并返回超时错误；
//   - 不手工提前关闭同一个 listener（交由 http.Server 负责），避免伪错误与 double close。
//
// 未启动、重复调用、Serve 已退出后调用均安全。
func (s *Server) Shutdown(ctx context.Context) error {
	// 关闭流程标记必须在最早时机置位，使 Serve 收尾按正常退出归一化
	s.mu.Lock()
	s.shutdown = true
	hs := s.httpServer
	s.mu.Unlock()

	s.shutdownOnce.Do(func() { close(s.shutdownCh) })

	if hs == nil {
		// 尚未启动：没有 listener 与 Serve goroutine，无需等待
		return nil
	}

	if err := hs.Shutdown(ctx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("HTTP 收尾超时，强制关闭剩余连接", "timeout", shutdownTimeout)
			if closeErr := hs.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				slog.Warn("强制关闭 HTTP 服务器失败", "error", closeErr)
			}
			return err
		}
		return err
	}
	return nil
}

// registerRoutes 注册路由：health + API 委托 + 静态文件
func (s *Server) registerRoutes() {
	// 健康检查
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{"status":"ok"}`))
	})
	// 所有业务 API 端点
	s.deps.Register(s.mux)
	// 静态文件（Vue SPA）
	distFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err == nil {
		s.mux.Handle("/", http.FileServer(http.FS(distFS)))
	}
}
