package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

const sseWriteTimeout = 5 * time.Second

// writeSSE 向长连接写出一条 SSE 消息并立即刷新（两类 SSE 共用）。
//
// Issue6 A15：每次写出都单独设置 5 秒 deadline，成功或失败后都尝试清除。
// 客户端保持连接但停止读取时，底层写入因此不会无限等待发送缓冲区。
//
// 约定：**任一失败都不得再写第二个响应头或 500**——响应头通常已经发出。
func writeSSE(w http.ResponseWriter, format string, args ...any) error {
	return withSSEWriteDeadline(w, func(rc *http.ResponseController) error {
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return fmt.Errorf("写出 SSE 消息失败: %w", err)
		}
		if err := rc.Flush(); err != nil {
			return fmt.Errorf("刷新 SSE 消息失败: %w", err)
		}
		return nil
	})
}

// flushSSE 在单次写 deadline 内刷新初始 SSE 响应头。
func flushSSE(w http.ResponseWriter) error {
	return withSSEWriteDeadline(w, func(rc *http.ResponseController) error {
		if err := rc.Flush(); err != nil {
			return fmt.Errorf("刷新 SSE 响应头失败: %w", err)
		}
		return nil
	})
}

// withSSEWriteDeadline 为一次 Write/Flush 设置并清除独立 deadline。
// 清除失败也必须结束连接，因为此后 deadline 状态已不可信。
func withSSEWriteDeadline(w http.ResponseWriter, write func(*http.ResponseController) error) (err error) {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
		return fmt.Errorf("设置 SSE 写 deadline 失败: %w", err)
	}
	defer func() {
		if clearErr := rc.SetWriteDeadline(time.Time{}); clearErr != nil {
			err = errors.Join(err, fmt.Errorf("清除 SSE 写 deadline 失败: %w", clearErr))
		}
	}()
	return write(rc)
}

// probeSSE 在写响应头**之前**判定 writer 是否同时支持 Flush 与写 deadline。
//
// 探测 deadline 后立即清除；任一步失败时，调用方仍可安全返回普通 HTTP 500。
func probeSSE(w http.ResponseWriter) error {
	if !supportsSSEFlush(w) {
		return fmt.Errorf("ResponseWriter 不支持 Flush: %w", http.ErrNotSupported)
	}
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
		return fmt.Errorf("ResponseWriter 不支持写 deadline: %w", err)
	}
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		return fmt.Errorf("清除 SSE 能力探测 deadline 失败: %w", err)
	}
	return nil
}

type responseWriterUnwrapper interface {
	Unwrap() http.ResponseWriter
}

func supportsSSEFlush(w http.ResponseWriter) bool {
	for {
		if _, ok := w.(http.Flusher); ok {
			return true
		}
		unwrapper, ok := w.(responseWriterUnwrapper)
		if !ok {
			return false
		}
		w = unwrapper.Unwrap()
	}
}
