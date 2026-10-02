package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// healthErrorWriter 模拟静态探针响应写出失败，记录重试与重复状态码。
type healthErrorWriter struct {
	header          http.Header
	writes, headers int
}

func (w *healthErrorWriter) Header() http.Header { return w.header }
func (w *healthErrorWriter) WriteHeader(int)     { w.headers++ }
func (w *healthErrorWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("health write failure")
}

// TestStaticHealthWriteFailureDebug 写失败只记录 Debug，不补写 HTTP 500。
func TestStaticHealthWriteFailureDebug(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	s := NewServer(nil, "127.0.0.1", 0)
	w := &healthErrorWriter{header: make(http.Header)}
	s.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.writes != 1 || w.headers != 0 {
		t.Errorf("Write 次数=%d WriteHeader 次数=%d，期望 1/0", w.writes, w.headers)
	}
	if got := w.header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type=%q", got)
	}
	var entry map[string]any
	decoder := json.NewDecoder(&logs)
	if err := decoder.Decode(&entry); err != nil {
		t.Fatalf("写失败缺少诊断: %v", err)
	}
	if entry["level"] != "DEBUG" || entry["msg"] != "静态健康响应写出失败" || entry["error"] != "health write failure" {
		t.Errorf("诊断错误: %v", entry)
	}
	// 正常静态探针仍返回原有状态、正文与头，不因未配置同步引擎而异常。
	good := httptest.NewRecorder()
	s.mux.ServeHTTP(good, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if good.Code != http.StatusOK || good.Body.String() != `{"status":"ok"}` {
		t.Fatalf("正常响应改变: status=%d body=%q", good.Code, good.Body.String())
	}
	if good.Header().Get("Content-Type") != "application/json; charset=utf-8" || good.Header().Get("Cache-Control") != "" {
		t.Error("静态探针头部契约改变")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Errorf("不应有重复诊断或正常响应诊断: extra=%v err=%v", extra, err)
	}
}
