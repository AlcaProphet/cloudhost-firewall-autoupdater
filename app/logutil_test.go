package app

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestLogLevelVarImmediateEffect 日志级别更新后立即对使用同一 LevelVar 的 handler 生效（Build6 Step 4）
func TestLogLevelVarImmediateEffect(t *testing.T) {
	defer SetLogLevel("info")

	SetLogLevel("info")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LogLevelVar}))

	logger.Debug("debug-should-be-hidden")
	if buf.Len() != 0 {
		t.Errorf("info 级别不应输出 debug: %q", buf.String())
	}

	SetLogLevel("debug")
	logger.Debug("debug-should-be-visible")
	if !strings.Contains(buf.String(), "debug-should-be-visible") {
		t.Errorf("更新级别后 debug 应立即可见: %q", buf.String())
	}

	SetLogLevel("error")
	buf.Reset()
	logger.Info("info-should-be-hidden")
	if buf.Len() != 0 {
		t.Errorf("error 级别不应输出 info: %q", buf.String())
	}
	logger.Error("error-should-be-visible")
	if !strings.Contains(buf.String(), "error-should-be-visible") {
		t.Errorf("error 级别应输出 error: %q", buf.String())
	}
}

// TestParseLogLevel 业务设置字符串到 slog 级别的映射，未知值按 info 处理
func TestParseLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"trace": slog.LevelInfo,
		"":      slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLogLevel(in); got != want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestLogLevelVarSharedByInitLogger InitLoggerWithBroadcaster 后全局默认 logger 使用可动态更新的级别
func TestLogLevelVarSharedByInitLogger(t *testing.T) {
	prev := slog.Default()
	defer func() {
		slog.SetDefault(prev)
		SetLogLevel("info")
	}()

	// extra 固定为不可用级别：让 MultiHandler.Enabled 只由 stdout 的 LogLevelVar 决定
	extra := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1})
	InitLoggerWithBroadcaster("warn", extra)
	if LogLevelVar.Level() != slog.LevelWarn {
		t.Fatalf("初始化后级别 = %v, want warn", LogLevelVar.Level())
	}
	if slog.Default().Enabled(t.Context(), slog.LevelInfo) {
		t.Error("warn 级别下 info 不应启用")
	}
	SetLogLevel("debug")
	if !slog.Default().Enabled(t.Context(), slog.LevelDebug) {
		t.Error("更新为 debug 后应立即对默认 logger 生效")
	}
}
