package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestJSONCachePolicyServerBoundaries 静态健康与 SPA 不经过业务 JSON 出口。
func TestJSONCachePolicyServerBoundaries(t *testing.T) {
	s := newTestServer(t, 0)
	server := httptest.NewServer(s.mux)
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	for _, path := range []string{"/api/health", "/"} {
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s 状态码 = %d", path, resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != "" {
			t.Errorf("%s 独立缓存策略被改写：%q", path, got)
		}
		if path == "/api/health" && string(body) != `{"status":"ok"}` {
			t.Error("静态存活响应改变")
		}
		if path == "/" && !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			t.Error("SPA 未返回 HTML")
		}
	}
}
