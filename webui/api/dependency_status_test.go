package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// 验证未接线错误在普通响应阶段返回，不进入 SSE 能力探测。
type dependencyGuardWriter struct{ *httptest.ResponseRecorder }

func (w dependencyGuardWriter) Flush() { panic("未接线时不得 Flush") }
func (w dependencyGuardWriter) SetWriteDeadline(time.Time) error {
	panic("未接线时不得设置 SSE deadline")
}

type dependencyStatusSyncer struct {
	stubSyncer
	status syncer.SyncStatus
	dryErr error
}

func (s *dependencyStatusSyncer) Status() syncer.SyncStatus { return s.status }
func (s *dependencyStatusSyncer) DryRun() (syncer.DryRunResponse, error) {
	return syncer.DryRunResponse{Results: []syncer.DryRunResult{}}, s.dryErr
}

func assertDependencyJSON(t *testing.T, code int, h http.Header, body []byte, want int, msg string) {
	t.Helper()
	if code != want {
		t.Errorf("HTTP = %d, want %d; body=%s", code, want, body)
	}
	if got := h.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type=%q", got)
	}
	if got := h.Values("Cache-Control"); len(got) != 1 || got[0] != "no-store" {
		t.Errorf("Cache-Control=%v", got)
	}
	if h.Get("Retry-After") != "" {
		t.Errorf("没有恢复时间依据时不发送 Retry-After")
	}
	if msg != "" {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(body, &value); err != nil {
			t.Fatal(err)
		}
		if len(value) != 1 {
			t.Errorf("错误 JSON 键集合=%v", value)
		}
		var got string
		if err := json.Unmarshal(value["error"], &got); err != nil {
			t.Fatal(err)
		}
		if got != msg {
			t.Errorf("error=%q, want %q", got, msg)
		}
	}
}

func TestP314MissingDependencies(t *testing.T) {
	for _, tc := range []struct{ method, path, msg string }{
		{"POST", "/api/sync/trigger", "同步引擎未启动"},
		{"POST", "/api/sync/dryrun", "同步引擎未启动"},
		{"POST", "/api/sync/pause", "同步引擎未启动"},
		{"POST", "/api/sync/resume", "同步引擎未启动"},
		{"GET", "/api/sync/events", "事件总线不可用"},
		{"GET", "/api/logs/stream", "日志流不可用"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			d := &Deps{}
			mux := http.NewServeMux()
			d.Register(mux)
			w := dependencyGuardWriter{httptest.NewRecorder()}
			mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			resp := w.Result()
			assertDependencyJSON(t, resp.StatusCode, resp.Header, w.Body.Bytes(), 503, tc.msg)
			if d.Coord != nil {
				t.Error("未接线路径创建了协调器")
			}
			// Store 保持 nil：若遗漏早期返回，pause/resume 将在写事务入口失败。
		})
	}
}

func TestP314HTTPRoundTrip(t *testing.T) {
	d := &Deps{}
	mux := http.NewServeMux()
	d.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/sync/trigger"}, {"POST", "/api/sync/dryrun"},
		{"POST", "/api/sync/pause"}, {"POST", "/api/sync/resume"},
		{"GET", "/api/sync/events"}, {"GET", "/api/logs/stream"},
	} {
		req, err := http.NewRequest(tc.method, server.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(errors.Join(readErr, closeErr))
		}
		assertDependencyJSON(t, resp.StatusCode, resp.Header, body, 503, "")
	}
}

func TestP314BoundaryControls(t *testing.T) {
	t.Run("nil_status", func(t *testing.T) {
		d := &Deps{}
		mux := http.NewServeMux()
		d.Register(mux)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sync/status", nil))
		assertDependencyJSON(t, w.Code, w.Header(), w.Body.Bytes(), 200, "")
		var status syncer.SyncStatus
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if status.Running {
			t.Error("未接线状态却为 running")
		}
	})
	for _, tc := range []struct {
		name, path string
		enabled    bool
		err        error
		want       int
	}{
		{"未进入Run但已接线", "/api/sync/trigger", true, nil, 202},
		{"暂停trigger", "/api/sync/trigger", false, nil, 409},
		{"dryrun成功", "/api/sync/dryrun", true, nil, 200},
		{"dryrun暂停仍可预览", "/api/sync/dryrun", false, nil, 200},
		{"dryrun冲突", "/api/sync/dryrun", true, syncer.ErrDryRunInProgress, 409},
		{"dryrun内部失败", "/api/sync/dryrun", true, errors.New("test"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &dependencyStatusSyncer{status: syncer.SyncStatus{Enabled: tc.enabled}, dryErr: tc.err}
			d := &Deps{Syncer: s}
			mux := http.NewServeMux()
			d.Register(mux)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("POST", tc.path, nil))
			assertDependencyJSON(t, w.Code, w.Header(), w.Body.Bytes(), tc.want, "")
			if tc.path == "/api/sync/trigger" && s.triggered.Load() != (tc.want == 202) {
				t.Error("TriggerSync调用边界错误")
			}
		})
	}
	// 接线存在但 writer 不支持 SSE 时保持 500；不得误报 503。
	for _, path := range []string{"/api/sync/events", "/api/logs/stream"} {
		t.Run("能力缺失"+path, func(t *testing.T) {
			sub := &countingEventSubscriber{}
			d := &Deps{EventBus: sub, LogBroadcaster: NewLogBroadcaster("info")}
			mux := http.NewServeMux()
			d.Register(mux)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", path, strings.NewReader("")).WithContext(ctx))
			assertDependencyJSON(t, w.Code, w.Header(), w.Body.Bytes(), 500, "SSE 不可用")
			if sub.subscribes.Load() != 0 {
				t.Error("SSE 能力失败却建立事件订阅")
			}
			if d.LogBroadcaster.subCount() != 0 {
				t.Error("SSE能力失败却建立日志订阅")
			}
		})
	}
}
