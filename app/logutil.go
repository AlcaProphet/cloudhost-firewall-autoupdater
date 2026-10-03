package app

import (
	"log/slog"
	"os"
)

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
	slog.SetDefault(slog.New(slog.NewMultiHandler(stdout, extra)))
}
