package webui

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/webui/api"
)

// Server WebUI HTTP 服务器
type Server struct {
	store *config.Store
	port  int
	mux   *http.ServeMux
	deps  *api.Deps
}

// NewServer 创建 WebUI 服务器
func NewServer(store *config.Store, port int) *Server {
	s := &Server{
		store: store,
		port:  port,
		mux:   http.NewServeMux(),
		deps:  &api.Deps{Store: store},
	}
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

// Start 启动 HTTP 服务器（阻塞）。若配置端口被占用，自动随机选择可用端口。
// 返回实际监听的端口号。
func (s *Server) Start() (int, error) {
	actualPort := findAvailablePort(s.port)
	if actualPort != s.port {
		slog.Warn("端口已被占用，使用随机端口", "请求端口", s.port, "实际端口", actualPort)
		s.port = actualPort
	}
	addr := fmt.Sprintf("127.0.0.1:%d", actualPort)
	slog.Info("WebUI 启动", "访问地址", "http://"+addr)
	return actualPort, http.ListenAndServe(addr, s.mux)
}

// findAvailablePort 探测端口：优先使用 preferred，被占用时由 OS 随机分配
func findAvailablePort(preferred int) int {
	addr := fmt.Sprintf("127.0.0.1:%d", preferred)
	l, err := net.Listen("tcp", addr)
	if err == nil {
		l.Close()
		return preferred
	}
	l, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return preferred
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
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
