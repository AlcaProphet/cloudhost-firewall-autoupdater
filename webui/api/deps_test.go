package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// shortWriteResponseWriter 在响应写出非零短前缀后模拟客户端断开。
type shortWriteResponseWriter struct {
	header      http.Header
	statuses    []int
	events      []string
	writeCalls  int
	writtenBody []byte
}

func newShortWriteResponseWriter() *shortWriteResponseWriter {
	return &shortWriteResponseWriter{header: make(http.Header)}
}

func (w *shortWriteResponseWriter) Header() http.Header { return w.header }

func (w *shortWriteResponseWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	w.events = append(w.events, "header")
}

func (w *shortWriteResponseWriter) Write(p []byte) (int, error) {
	w.writeCalls++
	w.events = append(w.events, "write")

	n := len(p) / 2
	if n == 0 && len(p) > 0 {
		n = 1
	}
	w.writtenBody = append(w.writtenBody, p[:n]...)
	return n, io.ErrClosedPipe
}

// TestWriteJSONEncodeFailureLogsWithoutSecondHeader 验证响应头发出后的 Encode
// 失败只记录安全日志，不再尝试写第二个错误响应。
func TestWriteJSONEncodeFailureLogsWithoutSecondHeader(t *testing.T) {
	const payloadSentinel = "fixture-json-payload-business-sentinel"
	payload := map[string]string{"value": payloadSentinel}
	fullJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("编码测试 payload 失败: %v", err)
	}
	fullJSON = append(fullJSON, '\n')

	var logBuf bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(previousLogger)

	w := newShortWriteResponseWriter()
	writeJSON(w, http.StatusCreated, payload)

	if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
	if len(w.statuses) != 1 || w.statuses[0] != http.StatusCreated {
		t.Errorf("WriteHeader 调用 = %v, want 仅一次 201", w.statuses)
	}
	if len(w.events) != 2 || w.events[0] != "header" || w.events[1] != "write" {
		t.Errorf("响应事件顺序 = %v, want [header write]", w.events)
	}
	if w.writeCalls != 1 {
		t.Errorf("Write 调用次数 = %d, want 1", w.writeCalls)
	}
	if len(w.writtenBody) == 0 {
		t.Error("用例前提：必须写出非零 JSON 前缀")
	}
	if len(w.writtenBody) >= len(fullJSON) {
		t.Errorf("写出字节数 = %d, want 小于完整 JSON 的 %d", len(w.writtenBody), len(fullJSON))
	}

	logs := logBuf.String()
	for _, required := range []string{"写出 JSON 响应失败", "status=201", io.ErrClosedPipe.Error()} {
		if !strings.Contains(logs, required) {
			t.Errorf("错误日志缺少 %q: %s", required, logs)
		}
	}
	if strings.Contains(logs, payloadSentinel) {
		t.Errorf("错误日志不得包含 payload 业务值 %q: %s", payloadSentinel, logs)
	}
}
