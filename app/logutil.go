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

// LogLevelVar 全局动态日志级别（Build6 Step 4）。
//
// 通过 SetLogLevel 更新后立即对 stdout 生效，不需要重启进程；
// 初值为 info，启动时按业务设置覆盖。
var LogLevelVar = new(slog.LevelVar)

// SetLogLevel 按业务设置更新全局日志级别（未知值按 info 处理）
func SetLogLevel(level string) {
	LogLevelVar.Set(ParseLogLevel(level))
}

// ParseLogLevel 将业务设置中的日志级别字符串转为 slog 级别，未知值按 info 处理
func ParseLogLevel(level string) slog.Level {
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

// InitLoggerWithBroadcaster 初始化日志系统：stdout 文本日志 + WebUI 日志流。
//
// 这是唯一运行形态（WebUI + SQLite）使用的日志入口；Headless 模式已随
// Build6 Step 2 移除，不再提供只写 stdout 的 InitLogger。
// stdout 使用全局 LogLevelVar，使设置保存后的日志级别即时生效。
func InitLoggerWithBroadcaster(level string, extra slog.Handler) {
	SetLogLevel(level)
	stdout := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: LogLevelVar})
	slog.SetDefault(slog.New(NewMultiHandler(stdout, extra)))
}
