package api

import (
	"fmt"
	"net/http"
)

// writeSSE 向长连接写出一条 SSE 消息并立即刷新（两类 SSE 共用）。
//
// Issue6 A15：修复前 `fmt.Fprintf` 与 `Flush` 的返回值都被忽略，半开连接或客户端
// 停止读取时 handler 会无界循环，订阅也永不取消。这里改用
// `http.ResponseController` 的可检查路径：任一写出或刷新失败都返回 error，
// 调用方立即退出，由既有 `defer unsubscribe()` 取消订阅。
//
// 约定：**任一失败都不得再写第二个响应头或 500**——响应头通常已经发出。
func writeSSE(w http.ResponseWriter, format string, args ...any) error {
	rc := http.NewResponseController(w)
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return fmt.Errorf("写出 SSE 消息失败: %w", err)
	}
	if err := rc.Flush(); err != nil {
		// 不支持 Flush 的 writer 由调用方在建立连接前已判定并拒绝，
		// 这里出现的错误只可能是真实 I/O 失败。
		return fmt.Errorf("刷新 SSE 消息失败: %w", err)
	}
	return nil
}

// probeSSE 在写响应头**之前**判定 writer 是否可用于 SSE。
//
// 返回 false 表示必须按普通 HTTP 错误响应结束（此时尚未写头，可以安全改写状态码）；
// 返回 true 表示后续写出错误只能直接返回、不得再写第二个响应头。
//
// 注意：不能只用 `http.ResponseController.Flush()` 做能力判定——对不实现
// `http.Flusher` 的 writer 它返回 `http.ErrNotSupported`，与「支持 deadline
// 但不支持 flush」无法区分，必须显式断言 `http.Flusher`。
func probeSSE(w http.ResponseWriter) bool {
	_, ok := w.(http.Flusher)
	return ok
}
