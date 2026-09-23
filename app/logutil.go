package app

import (
	"context"
	"log/slog"
	"os"
)

// MultiHandler 将日志同时写入多个 Handler
type MultiHandler struct {
	Handlers []slog.Handler
}

// NewMultiHandler 创建多路 Handler
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{Handlers: handlers}
}

func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.Handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.Handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.Handlers))
	for i, h := range m.Handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{Handlers: handlers}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.Handlers))
	for i, h := range m.Handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &MultiHandler{Handlers: handlers}
}

// InitLoggerWithBroadcaster 初始化日志系统：stdout 文本日志 + WebUI 日志流。
//
// 这是唯一运行形态（WebUI + SQLite）使用的日志入口；Headless 模式已随
// Build6 Step 2 移除，不再提供只写 stdout 的 InitLogger。
func InitLoggerWithBroadcaster(level string, extra slog.Handler) {
	opts := &slog.HandlerOptions{Level: parseLogLevel(level)}
	stdout := slog.NewTextHandler(os.Stdout, opts)
	slog.SetDefault(slog.New(NewMultiHandler(stdout, extra)))
}

// parseLogLevel 将业务设置中的日志级别字符串转为 slog 级别，未知值按 info 处理
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
