package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// assertJSONNoStore 检查客户端或 Recorder.Result 已提交的响应头。
func assertJSONNoStore(t *testing.T, resp *http.Response, status int) {
	t.Helper()
	if resp.StatusCode != status {
		t.Errorf("状态码 = %d，期望 %d", resp.StatusCode, status)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	values := resp.Header.Values("Cache-Control")
	if len(values) != 1 || values[0] != "no-store" {
		t.Errorf("Cache-Control = %q，期望仅一个 no-store", values)
	}
}

// TestJSONCachePolicyCommittedHeaders 同时验证成功与错误输出在提交前覆盖已有缓存策略。
func TestJSONCachePolicyCommittedHeaders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		failure bool
	}{
		{"created", http.StatusCreated, false},
		{"body_too_large", http.StatusRequestEntityTooLarge, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			w.Header().Set("Cache-Control", "public, max-age=60")
			if tc.failure {
				writeError(w, tc.status, "测试错误")
			} else {
				writeJSON(w, tc.status, map[string]string{"message": "测试成功"})
			}
			resp := w.Result()
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Error(err)
				}
			}()
			assertJSONNoStore(t, resp, tc.status)
			var got map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if tc.failure && got["error"] != "测试错误" {
				t.Error("错误载荷改变")
			}
			if !tc.failure && got["message"] != "测试成功" {
				t.Error("成功载荷改变")
			}
		})
	}
}

// TestJSONCachePolicySettingsHTTP 通过真实 HTTP 保留凭据回显并覆盖数据库读取失败与 HEAD。
func TestJSONCachePolicySettingsHTTP(t *testing.T) {
	for _, broken := range []bool{false, true} {
		name := "success"
		status := http.StatusOK
		if broken {
			name = "db_failure"
			status = http.StatusInternalServerError
		}
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			sentinels := map[string]string{"tc_access_id": "fixture-tc-id", "tc_access_key": "fixture-tc-key", "ali_access_id": "fixture-ali-id", "ali_access_key": "fixture-ali-key"}
			for key, value := range sentinels {
				if err := e.store.SetSetting(key, value); err != nil {
					t.Fatal(err)
				}
			}
			if broken {
				e.execRaw(t, "DROP TABLE settings")
			}
			mux := http.NewServeMux()
			e.deps.Register(mux)
			server := httptest.NewServer(mux)
			defer server.Close()
			client := &http.Client{Timeout: 3 * time.Second}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				t.Run(method, func(t *testing.T) {
					req, err := http.NewRequest(method, server.URL+"/api/settings", nil)
					if err != nil {
						t.Fatal(err)
					}
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := resp.Body.Close(); err != nil {
							t.Error(err)
						}
					}()
					assertJSONNoStore(t, resp, status)
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					if method == http.MethodHead {
						if len(body) != 0 {
							t.Error("HEAD 不应含正文")
						}
						return
					}
					var data map[string]string
					if err := json.Unmarshal(body, &data); err != nil {
						t.Fatal(err)
					}
					if broken {
						if data["error"] == "" {
							t.Error("缺少错误消息")
						}
						for key, value := range sentinels {
							if strings.Contains(string(body), value) {
								t.Errorf("失败响应泄露 %s", key)
							}
						}
						return
					}
					if len(data) != len(settingsEditableKeys) {
						t.Errorf("设置字段数 = %d", len(data))
					}
					for key, value := range sentinels {
						if data[key] != value {
							t.Errorf("凭据回显改变：%s", key)
						}
					}
				})
			}
		})
	}
}

// TestJSONCachePolicyAPIMatrix 用实际路由验证普通 JSON 成功与多类错误统一禁缓存。
func TestJSONCachePolicyAPIMatrix(t *testing.T) {
	e := newTestEnv(t)
	for _, path := range []string{"/api/settings", "/api/alerts", "/api/zones", "/api/targets", "/api/rules", "/api/scanned-resources?cloud_type=tc_lighthouse", "/api/sync/status", "/api/sync/logs"} {
		t.Run("get"+path, func(t *testing.T) {
			w := e.do(t, http.MethodGet, path, "")
			assertJSONNoStore(t, w.Result(), http.StatusOK)
			if !json.Valid(w.Body.Bytes()) {
				t.Error("成功响应不是有效 JSON")
			}
		})
	}
	t.Run("created", func(t *testing.T) {
		w := e.do(t, http.MethodPost, "/api/targets", `{"cloud_type":"tc_lighthouse","region":"ap-guangzhou","resource_id":"lhins-cache"}`)
		assertJSONNoStore(t, w.Result(), http.StatusCreated)
	})
	t.Run("bad_request", func(t *testing.T) {
		w := e.do(t, http.MethodPut, "/api/settings", `{"unknown":true}`)
		assertJSONNoStore(t, w.Result(), http.StatusBadRequest)
	})
	t.Run("not_found", func(t *testing.T) {
		w := e.do(t, http.MethodDelete, "/api/targets/999999", "")
		assertJSONNoStore(t, w.Result(), http.StatusNotFound)
	})
	t.Run("conflict", func(t *testing.T) {
		id := e.seedTarget(t, config.CloudTCLighthouse, "ap-guangzhou", "lhins-referenced")
		e.seedRule(t, config.DomainRule{Host: "cache.test", Protocol: "TCP", Ports: "80", Action: "ACCEPT", Targets: []int{id}})
		w := e.do(t, http.MethodDelete, pathWithID("/api/targets/", id), "")
		assertJSONNoStore(t, w.Result(), http.StatusConflict)
	})
	t.Run("body_too_large", func(t *testing.T) {
		big := `{"cloud_type":"tc_cvm","region":"gz","resource_id":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
		w := e.do(t, http.MethodPost, "/api/targets", big)
		assertJSONNoStore(t, w.Result(), http.StatusRequestEntityTooLarge)
	})
	t.Run("operational_unwired", func(t *testing.T) {
		w := e.do(t, http.MethodGet, "/api/health/operational", "")
		assertJSONNoStore(t, w.Result(), http.StatusServiceUnavailable)
	})
	t.Run("export", func(t *testing.T) {
		w := e.do(t, http.MethodPost, "/api/config/export", "")
		assertJSONNoStore(t, w.Result(), http.StatusOK)
		if w.Result().Header.Get("Content-Disposition") == "" {
			t.Error("导出附件头丢失")
		}
	})
	t.Run("db_failures", func(t *testing.T) {
		broken := newTestEnv(t)
		broken.execRaw(t, "DROP TABLE alert_policy")
		for _, tc := range []struct{ method, path string }{{http.MethodGet, "/api/alerts"}, {http.MethodPost, "/api/config/export"}} {
			w := broken.do(t, tc.method, tc.path, "")
			assertJSONNoStore(t, w.Result(), http.StatusInternalServerError)
			var data map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if data["error"] == "" {
				t.Error("数据库失败缺少错误响应")
			}
		}
	})
}

// TestJSONCachePolicyBoundaries 成功 SSE 与路由器自动错误使用各自响应路径。
func TestJSONCachePolicyBoundaries(t *testing.T) {
	t.Run("sse_success", func(t *testing.T) {
		d := &Deps{EventBus: notifier.NewEventBus(), LogBroadcaster: NewLogBroadcaster("info")}
		mux := http.NewServeMux()
		d.Register(mux)
		server := httptest.NewServer(mux)
		defer server.Close()
		client := &http.Client{Timeout: 3 * time.Second}
		for _, path := range []string{"/api/sync/events", "/api/logs/stream"} {
			resp, err := client.Get(server.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s 状态码 = %d", path, resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
				t.Errorf("%s Content-Type = %q", path, got)
			}
			if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
				t.Errorf("%s Cache-Control = %q", path, got)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("sse_json_error", func(t *testing.T) {
		d := &Deps{}
		for _, path := range []string{"/api/sync/events", "/api/logs/stream"} {
			w := doJSON(t, d, http.MethodGet, path, "")
			assertJSONNoStore(t, w.Result(), http.StatusBadRequest)
		}
	})
	t.Run("mux_errors", func(t *testing.T) {
		d := &Deps{}
		for _, tc := range []struct {
			method, path string
			status       int
		}{{http.MethodGet, "/api/not-registered", http.StatusNotFound}, {http.MethodPatch, "/api/settings", http.StatusMethodNotAllowed}} {
			w := doJSON(t, d, tc.method, tc.path, "")
			resp := w.Result()
			if resp.StatusCode != tc.status {
				t.Errorf("路由错误状态码 = %d", resp.StatusCode)
			}
			if got := resp.Header.Get("Cache-Control"); got != "" {
				t.Errorf("路由器缓存策略被改写：%q", got)
			}
		}
	})
}
