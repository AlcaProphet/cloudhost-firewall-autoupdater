package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/app"
)

// 环形缓冲容量：回放最近 N 条日志（与前端显示上限 1000 一致）
const logRingSize = 1000

// logEntry 的序号是日志身份，文本相同的记录仍分别保留。
type logEntry struct {
	Seq  uint64
	Line string
}

// LogBroadcaster 将 slog 日志广播到 SSE 订阅者
// level 使用可动态更新的 slog.LevelVar：与 stdout 日志级别保持一致（debug/info/warn/error，默认 info），
// 设置保存后经 SetLevel 即时生效且并发安全
// 行格式与 stdout（slog.TextHandler）逐字符一致，保证 WebUI 与 docker compose logs 输出对齐（Build4 Step 2）
// 支持历史回放：订阅时先回放环形缓冲中的最近 logRingSize 条，再进入增量推送（弥补"页面打开前的日志不显示"）
type LogBroadcaster struct {
	epoch string
	seq   uint64
	mu    sync.Mutex
	subs  map[int]chan logEntry
	next  int
	level *slog.LevelVar // 日志流级别（与 stdout 日志级别一致，可在运行时更新）

	// 环形缓冲（最近 logRingSize 条）
	ring    [logRingSize]logEntry
	ringPos int // 写指针（下一个写入位置）
	ringCnt int // 已写入条数（≤ logRingSize）
}

// NewLogBroadcaster 创建日志广播器（level: debug/info/warn/error 字符串）
func NewLogBroadcaster(level string) *LogBroadcaster {
	lv := new(slog.LevelVar)
	// 复用 app.ParseLogLevel：stdout 与日志流必须使用同一套级别解析
	lv.Set(app.ParseLogLevel(level))
	return &LogBroadcaster{epoch: rand.Text(), subs: make(map[int]chan logEntry), level: lv}
}

// SetLevel 线程安全地更新日志流级别（与 stdout 日志级别保持一致）
func (b *LogBroadcaster) SetLevel(level slog.Level) {
	b.level.Set(level)
}

// Subscribe 按 Last-Event-ID 选择回放。历史入队和注册订阅在同一把锁下，
// 保证历史先于实时日志且切换无漏收；网络写入始终在锁外。
// reason 非空时先发送 reset，baseline 是可回放窗口的前一序号（空流为 0）。
func (b *LogBroadcaster) Subscribe(cursor string) (<-chan logEntry, string, uint64, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// 缓存 [oldest, seq] 能完整衔接 oldest-1；更早的游标需要重建窗口。
	oldest := b.seq - uint64(b.ringCnt) + 1
	baseline := oldest - 1
	reason := ""
	after := baseline
	if cursor == "" {
		reason = "initial"
	} else {
		epoch, raw, ok := strings.Cut(cursor, ":")
		n, err := strconv.ParseUint(raw, 10, 64)
		switch {
		case !ok || err != nil || strconv.FormatUint(n, 10) != raw:
			reason = "invalid_cursor"
		case epoch != b.epoch:
			reason = "instance_changed"
		case n > b.seq:
			reason = "invalid_cursor"
		case n < baseline:
			reason = "history_expired"
		default:
			after = n
		}
	}
	// 历史最多 1000 条，通道容量更大，锁内入队不会等待消费者。
	ch := make(chan logEntry, logRingSize+256)
	start := (b.ringPos - b.ringCnt + logRingSize) % logRingSize
	for i := 0; i < b.ringCnt; i++ {
		entry := b.ring[(start+i)%logRingSize]
		if entry.Seq > after {
			ch <- entry
		}
	}
	id := b.nextID()
	b.subs[id] = ch
	return ch, reason, baseline, func() {
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
	return level >= b.level.Level() // 按日志流级别过滤，避免 debug 噪音
}

// renderLine 用 slog.TextHandler 渲染单行（与 stdout 格式完全一致）
// TextHandler 输出形如：time=2026-08-02T10:00:00.000+08:00 level=INFO msg=同步完成 provider=...
func renderLine(level slog.Level, r slog.Record) (string, error) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})
	if err := h.Handle(context.Background(), r); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (b *LogBroadcaster) Handle(_ context.Context, r slog.Record) error {
	line, err := renderLine(b.level.Level(), r)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// 1. 写入环形缓冲
	b.seq++
	entry := logEntry{Seq: b.seq, Line: line}
	b.ring[b.ringPos] = entry
	b.ringPos = (b.ringPos + 1) % logRingSize
	if b.ringCnt < logRingSize {
		b.ringCnt++
	}

	// 2. 推送订阅者（满则跳过；前端通过序号跳跃提示可能不连续）
	for _, ch := range b.subs {
		select {
		case ch <- entry:
		default:
		}
	}
	return nil
}

func (b *LogBroadcaster) WithAttrs(attrs []slog.Attr) slog.Handler { return b }
func (b *LogBroadcaster) WithGroup(name string) slog.Handler       { return b }

// ─── SSE 端点 ───

func (d *Deps) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if d.LogBroadcaster == nil {
		writeError(w, http.StatusBadRequest, "日志流不可用")
		return
	}

	// 能力检测必须在写响应头之前：两类 SSE 都要求 Flush 与单次写 deadline
	// 可用，不能静默降级为可能无限阻塞的连接（Issue6 A15）。
	if err := probeSSE(w); err != nil {
		slog.Warn("日志流 SSE 能力检测失败", "error", err)
		writeError(w, http.StatusInternalServerError, "SSE 不可用")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, reason, baseline, unsubscribe := d.LogBroadcaster.Subscribe(r.Header.Get("Last-Event-ID"))
	defer unsubscribe()

	// 立即写出响应头：客户端可在 Subscribe（含历史回放）完成后确认连接已建立。
	// 失败时直接返回：响应头已发出，不得再写第二个响应头或 500。
	if err := flushSSE(w); err != nil {
		slog.Warn("日志流 SSE 初始刷新失败，结束连接", "error", err)
		return
	}

	// reset 携带基准游标；完整 reset 后再次断线仍能从窗口起点续传。
	if reason != "" {
		if err := writeSSE(w, "event: reset\nid: %s:%d\ndata: %s\n\n", d.LogBroadcaster.epoch, baseline, reason); err != nil {
			slog.Debug("日志流 SSE 重置写出失败，结束连接", "error", err)
			return
		}
	}
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSE(w, "id: %s:%d\ndata: %s\n\n", d.LogBroadcaster.epoch, line.Seq, strings.TrimSuffix(line.Line, "\n")); err != nil {
				// 半开连接 / 客户端停止读取：首个写错误即退出（修复前会永久循环）
				slog.Debug("日志流 SSE 写出失败，结束连接", "error", err)
				return
			}
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
