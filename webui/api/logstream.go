package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

// 环形缓冲容量：回放最近 N 条日志（与前端显示上限 1000 一致）
const logRingSize = 1000

// LogBroadcaster 将 slog 日志广播到 SSE 订阅者
// level 与 stdout 日志级别一致（debug/info/warn/error，默认 info），保证 WebUI 日志流与终端输出级别一致
// 行格式与 stdout（slog.TextHandler）逐字符一致，保证 WebUI 与 docker compose logs 输出对齐（Build4 Step 2）
// 支持历史回放：订阅时先回放环形缓冲中的最近 logRingSize 条，再进入增量推送（弥补"页面打开前的日志不显示"）
type LogBroadcaster struct {
	mu    sync.Mutex
	subs  map[int]chan string
	next  int
	level slog.Level // 日志流级别（与 cfg.LogLevel 一致）

	// 环形缓冲（最近 logRingSize 条）
	ring    [logRingSize]string
	ringPos int // 写指针（下一个写入位置）
	ringCnt int // 已写入条数（≤ logRingSize）
}

// NewLogBroadcaster 创建日志广播器（level: debug/info/warn/error 字符串）
func NewLogBroadcaster(level string) *LogBroadcaster {
	return &LogBroadcaster{subs: make(map[int]chan string), level: parseLevel(level)}
}

// parseLevel 解析日志级别字符串（与 app.InitLogger 语义一致，默认 info）
func parseLevel(level string) slog.Level {
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

// Subscribe 订阅日志流：先回放最近 logRingSize 条历史，再返回 channel 和取消函数
func (b *LogBroadcaster) Subscribe() (<-chan string, func()) {
	b.mu.Lock()
	id := b.nextID()
	// 通道容量 ≥ 回放条数 + 增量余量：锁外回放写入不阻塞
	ch := make(chan string, logRingSize+256)
	b.subs[id] = ch

	// 拷贝环形缓冲快照（时间正序：最旧 → 最新）
	history := make([]string, 0, b.ringCnt)
	if b.ringCnt < logRingSize {
		history = append(history, b.ring[:b.ringCnt]...)
	} else {
		for i := 0; i < logRingSize; i++ {
			history = append(history, b.ring[(b.ringPos+i)%logRingSize])
		}
	}
	b.mu.Unlock()

	// 锁外回放（通道容量充足，阻塞写入不会死锁；SSE handler 在 Subscribe 返回后立即消费）
	for _, line := range history {
		ch <- line
	}

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			close(c)
			delete(b.subs, id)
		}
	}
}

func (b *LogBroadcaster) nextID() int {
	id := b.next
	b.next++
	return id
}

// ─── slog.Handler 实现 ───

func (b *LogBroadcaster) Enabled(_ context.Context, level slog.Level) bool {
	return level >= b.level // 按日志流级别过滤，避免 debug 噪音
}

// renderLine 用 slog.TextHandler 渲染单行（与 stdout 格式完全一致）
// TextHandler 输出形如：time=2026-08-02T10:00:00.000+08:00 level=INFO msg=同步完成 provider=...
func renderLine(level slog.Level, r slog.Record) string {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})
	_ = h.Handle(context.Background(), r)
	return buf.String()
}

func (b *LogBroadcaster) Handle(_ context.Context, r slog.Record) error {
	line := renderLine(b.level, r)

	b.mu.Lock()
	defer b.mu.Unlock()

	// 1. 写入环形缓冲
	b.ring[b.ringPos] = line
	b.ringPos = (b.ringPos + 1) % logRingSize
	if b.ringCnt < logRingSize {
		b.ringCnt++
	}

	// 2. 推送订阅者（满则跳过；通道容量充足 + 回放兜底，丢失概率极低）
	for _, ch := range b.subs {
		select {
		case ch <- line:
		default:
		}
	}
	return nil
}

func (b *LogBroadcaster) WithAttrs(attrs []slog.Attr) slog.Handler { return b }
func (b *LogBroadcaster) WithGroup(name string) slog.Handler       { return b }

// ─── LogBroadcaster 已使用 app.MultiHandler（定义于 app/logutil.go） ───

// ─── SSE 端点 ───

func (d *Deps) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if d.LogBroadcaster == nil {
		writeError(w, http.StatusBadRequest, "日志流不可用")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE 不可用")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := d.LogBroadcaster.Subscribe()
	defer unsubscribe()

	// 立即写出响应头：客户端可在 Subscribe（含历史回放）完成后确认连接已建立。
	flusher.Flush()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-d.ShutdownCh:
			// 服务器级 shutdown：主动返回，由 defer unsubscribe() 取消订阅。
			// 不通过关闭 LogBroadcaster 订阅 channel 驱动退出（保持既有订阅语义）。
			slog.Info("服务器关闭，日志流 SSE 退出")
			return
		}
	}
}
