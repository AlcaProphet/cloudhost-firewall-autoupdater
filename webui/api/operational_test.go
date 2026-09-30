package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/health"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// ─── Build7 Step 4：GET /api/health/operational ───

// fakeHealthSource 可编程的运行健康来源（记录调用次数与唤醒次数）
type fakeHealthSource struct {
	mu        sync.Mutex
	result    health.Result
	calls     int
	wakeCalls int
}

func (f *fakeHealthSource) Evaluate(context.Context) health.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result
}

func (f *fakeHealthSource) Wake() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wakeCalls++
}

func (f *fakeHealthSource) setResult(r health.Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result = r
}

func (f *fakeHealthSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// operationalResponseForTest 解析端点的固定响应形状
type operationalResponseForTest struct {
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	Reasons   []string  `json:"reasons"`
}

func decodeOperational(t *testing.T, body []byte) operationalResponseForTest {
	t.Helper()
	var got operationalResponseForTest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("解析 operational 响应失败: %v; body=%s", err, body)
	}
	return got
}

// TestOperationalEndpointHealthy200 健康：200 + ok + 空原因 + 固定头。
func TestOperationalEndpointHealthy200(t *testing.T) {
	e := newTestEnv(t)
	checkedAt := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	src := &fakeHealthSource{result: health.Result{Healthy: true, CheckedAt: checkedAt, Reasons: []string{}}}
	e.deps.Health = src

	w := e.do(t, http.MethodGet, "/api/health/operational", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	got := decodeOperational(t, w.Body.Bytes())
	if got.Status != "ok" || len(got.Reasons) != 0 {
		t.Errorf("健康响应错误: %+v", got)
	}
	if !got.CheckedAt.Equal(checkedAt) {
		t.Errorf("checked_at = %v, want %v", got.CheckedAt, checkedAt)
	}
}

// TestOperationalEndpointUnhealthy503 异常：503 + unhealthy + 稳定原因。
func TestOperationalEndpointUnhealthy503(t *testing.T) {
	e := newTestEnv(t)
	src := &fakeHealthSource{result: health.Result{
		Healthy: false, CheckedAt: time.Now(),
		Reasons: []string{health.ReasonLastRoundFailed, health.ReasonSQLite},
	}}
	e.deps.Health = src

	w := e.do(t, http.MethodGet, "/api/health/operational", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d, want 503; body=%s", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	got := decodeOperational(t, w.Body.Bytes())
	if got.Status != "unhealthy" || len(got.Reasons) != 2 {
		t.Errorf("异常响应错误: %+v", got)
	}
	if got.Reasons[0] != health.ReasonLastRoundFailed {
		t.Errorf("原因顺序必须稳定: %+v", got.Reasons)
	}
}

// TestOperationalEndpointComputesLive 每次请求必须现场计算，不得只返回监督器缓存。
func TestOperationalEndpointComputesLive(t *testing.T) {
	e := newTestEnv(t)
	src := &fakeHealthSource{result: health.Result{Healthy: true, CheckedAt: time.Now(), Reasons: []string{}}}
	e.deps.Health = src

	if w := e.do(t, http.MethodGet, "/api/health/operational", ""); w.Code != http.StatusOK {
		t.Fatalf("第一次请求状态码 = %d, want 200", w.Code)
	}
	src.setResult(health.Result{Healthy: false, CheckedAt: time.Now(), Reasons: []string{health.ReasonSyncerStopped}})
	if w := e.do(t, http.MethodGet, "/api/health/operational", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("第二次请求状态码 = %d, want 503（必须现场计算）", w.Code)
	}
	if got := src.callCount(); got != 2 {
		t.Errorf("现场计算次数 = %d, want 2", got)
	}
}

// TestOperationalEndpointDoesNotLeakDetails 端点不得泄露 SQL、路径、凭据或 URL。
func TestOperationalEndpointDoesNotLeakDetails(t *testing.T) {
	e := newTestEnv(t)

	// 使用真实 Checker：探活错误里放入路径与 SQL，验证只输出稳定原因
	checker := health.New(health.Deps{
		Pinger: failingPinger{err: errors.New(`disk I/O error near "SELECT 1" at /var/lib/fwalizer/config.db?token=secret`)},
		Status: func() syncer.SyncStatus {
			return syncer.SyncStatus{Running: true, Enabled: true, ProcessStartedAt: time.Now()}
		},
		Policy:      func() config.AlertPolicyConfig { return config.DefaultAlertPolicy() },
		Interval:    func() time.Duration { return 5 * time.Minute },
		PingTimeout: 100 * time.Millisecond,
	})
	e.deps.Health = checkerSource{checker}

	w := e.do(t, http.MethodGet, "/api/health/operational", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d, want 503; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, secret := range []string{"SELECT", "config.db", "/var/lib", "token=secret", "disk I/O"} {
		if strings.Contains(body, secret) {
			t.Errorf("operational 响应泄露内部细节 %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, health.ReasonSQLite) {
		t.Errorf("响应必须包含稳定原因 %q: %s", health.ReasonSQLite, body)
	}
}

// TestOperationalEndpointWithoutWiringIs503 未接线时不得声称健康。
func TestOperationalEndpointWithoutWiringIs503(t *testing.T) {
	e := newTestEnv(t)
	e.deps.Health = nil

	w := e.do(t, http.MethodGet, "/api/health/operational", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d, want 503", w.Code)
	}
	got := decodeOperational(t, w.Body.Bytes())
	if got.Status != "unhealthy" || len(got.Reasons) == 0 {
		t.Errorf("未接线必须返回稳定异常原因: %+v", got)
	}
}

// TestPutAlertsWakesHealthSource 保存配置必须唤醒健康监督器（Build7 §4.6）。
func TestPutAlertsWakesHealthSource(t *testing.T) {
	e := newTestEnv(t)
	src := &fakeHealthSource{result: health.Result{Healthy: true, CheckedAt: time.Now(), Reasons: []string{}}}
	e.deps.Health = src

	if w := e.do(t, http.MethodPut, "/api/alerts", alertsBody("h", "p", "", "dingtalk")); w.Code != http.StatusOK {
		t.Fatalf("PUT 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := src.wakeCalls; got != 1 {
		t.Errorf("保存配置后必须唤醒监督器一次，实际 %d", got)
	}
}

// failingPinger 永远返回固定错误
type failingPinger struct{ err error }

func (p failingPinger) PingContext(context.Context) error { return p.err }

// checkerSource 把 *health.Checker 适配为 OperationalHealthSource（只实现 Evaluate）
type checkerSource struct{ checker *health.Checker }

func (s checkerSource) Evaluate(ctx context.Context) health.Result { return s.checker.Evaluate(ctx) }
func (s checkerSource) Wake()                                      {}

// fakePushWaker 记录 Push 循环被唤醒的次数
type fakePushWaker struct {
	mu    sync.Mutex
	calls int
}

func (f *fakePushWaker) Wake() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
}

func (f *fakePushWaker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestPutAlertsAndImportWakePushLoop 保存配置与导入都必须唤醒 Push 循环
// （commit 后发布顺序：告警集合 → RuntimeState → 监督器 → Push）。
func TestPutAlertsAndImportWakePushLoop(t *testing.T) {
	e := newTestEnv(t)
	src := &fakeHealthSource{result: health.Result{Healthy: true, CheckedAt: time.Now(), Reasons: []string{}}}
	push := &fakePushWaker{}
	e.deps.Health = src
	e.deps.Push = push

	if w := e.do(t, http.MethodPut, "/api/alerts", alertsBody("h", "p", "", "dingtalk")); w.Code != http.StatusOK {
		t.Fatalf("PUT 状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := push.count(); got != 1 {
		t.Errorf("PUT 后 Push 唤醒次数 = %d, want 1", got)
	}

	if w := e.do(t, http.MethodPost, "/api/config/import", validBundle()); w.Code != http.StatusOK {
		t.Fatalf("导入状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := push.count(); got != 2 {
		t.Errorf("导入后 Push 唤醒次数 = %d, want 2", got)
	}

	// 非法输入不得唤醒
	before := push.count()
	if w := e.do(t, http.MethodPut, "/api/alerts", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("非法 PUT 状态码 = %d, want 400", w.Code)
	}
	if got := push.count(); got != before {
		t.Errorf("非法输入不得唤醒 Push: %d → %d", before, got)
	}
}

// awaitPushConfigRead 用有界 channel 等待配置读取，不依赖 sleep 猜测调度。
func awaitPushConfigRead(t *testing.T, ch <-chan *syncer.RuntimeState) *syncer.RuntimeState {
	t.Helper()
	select {
	case st := <-ch:
		return st
	case <-time.After(2 * time.Second):
		t.Fatal("config reread timeout")
		return nil
	}
}

// TestPutAlertsEnablesPushAfterPublication 固定唤醒后重读时序，验证关闭到启用的首发。
// 健康状态采用受控夹具；真实 Pusher、SQLite 与 API 协调器不访问外部服务。
func TestPutAlertsEnablesPushAfterPublication(t *testing.T) {
	for _, real := range []bool{false, true} {
		name := "Runtime"
		if real {
			name = "Syncer"
		}
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			old := &syncer.RuntimeState{Config: config.RuntimeConfig{
				LogLevel:       "info",
				Policy:         config.DefaultAlertPolicy(),
				UptimeKumaPush: config.DefaultUptimeKumaPush(),
			}}
			e.runtime.Apply(old)
			if real {
				s := syncer.New(e.runtime)
				e.deps.Syncer = s
				t.Cleanup(s.Stop)
			}
			hits := make(chan string, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits <- r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
					t.Errorf("response: %v", err)
				}
			}))
			t.Cleanup(srv.Close)
			reads := make(chan *syncer.RuntimeState, 16)
			checker := health.New(health.Deps{
				Pinger:   e.store,
				Status:   func() syncer.SyncStatus { return syncer.SyncStatus{Running: true, Enabled: false} },
				Policy:   func() config.AlertPolicyConfig { return e.runtime.Snapshot().Config.Policy },
				Interval: func() time.Duration { return time.Hour },
			})
			p := health.NewPusher(health.PusherDeps{Checker: checker, Config: func() health.PushConfig {
				st := e.runtime.Snapshot()
				reads <- st
				c := st.Config.UptimeKumaPush
				return health.PushConfig{Enabled: c.Enabled, URL: c.URL, Interval: c.Interval}
			}, Timeout: time.Second})
			go p.Run()
			t.Cleanup(p.Stop)
			if awaitPushConfigRead(t, reads) != old {
				t.Fatal("initial state mismatch")
			}
			var wakeState *syncer.RuntimeState
			// 唤醒调用返回前，确保 Pusher 已读完快照，稳定暴露先 Wake 后发布的错误顺序。
			e.deps.Push = runtimeWakeProbe{fn: func() {
				p.Wake()
				wakeState = awaitPushConfigRead(t, reads)
			}}
			var body map[string]any
			if err := json.Unmarshal([]byte(alertsBody("h", "p", "", "dingtalk")), &body); err != nil {
				t.Fatal(err)
			}
			body["uptime_kuma_push"] = map[string]any{"enabled": true, "url": srv.URL + "/api/push/new", "interval": "1h"}
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			w := e.do(t, http.MethodPut, "/api/alerts", string(b))
			if w.Code != 200 {
				t.Fatalf("PUT=%d %s", w.Code, w.Body.String())
			}
			published := e.runtime.Snapshot()
			if !published.Config.UptimeKumaPush.Enabled {
				t.Fatal("API did not publish enabled config")
			}
			if wakeState != published {
				t.Fatal("Wake did not see published candidate")
			}
			select {
			case path := <-hits:
				if path != "/api/push/new" {
					t.Fatal(path)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no immediate heartbeat")
			}
		})
	}
}

// TestAlertsAndImportPublishBeforeWake 保存与导入均先发布再唤醒，非法输入零发布零唤醒。
func TestAlertsAndImportPublishBeforeWake(t *testing.T) {
	for _, real := range []bool{false, true} {
		name := "Runtime"
		if real {
			name = "Syncer"
		}
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			e.runtime.Apply(&syncer.RuntimeState{Config: config.RuntimeConfig{LogLevel: "info"}})
			if real {
				s := syncer.New(e.runtime)
				e.deps.Syncer = s
				t.Cleanup(s.Stop)
			}
			var before *syncer.RuntimeState
			seen := []*syncer.RuntimeState{}
			order := []string{}
			probe := func(label string) runtimeWakeProbe {
				return runtimeWakeProbe{fn: func() {
					st := e.runtime.Snapshot()
					seen = append(seen, st)
					order = append(order, label)
					if st == before {
						t.Errorf("%s sees preceding config", label)
					}
				}}
			}
			e.deps.Health = probe("Health")
			e.deps.Push = probe("Push")
			for _, op := range []struct{ method, path, body string }{{http.MethodPut, "/api/alerts", alertsBody("h", "p", "", "dingtalk")}, {http.MethodPost, "/api/config/import", validBundle()}} {
				before = e.runtime.Snapshot()
				seen = nil
				order = nil
				w := e.do(t, op.method, op.path, op.body)
				if w.Code != 200 {
					t.Fatalf("%s=%d %s", op.path, w.Code, w.Body.String())
				}
				after := e.runtime.Snapshot()
				if len(seen) != 2 || seen[0] != after || seen[1] != after {
					t.Errorf("%s Wake snapshots differ from published result", op.path)
				}
				if len(order) != 2 || order[0] != "Health" || order[1] != "Push" {
					t.Error("wake order", order)
				}
			}
			before = e.runtime.Snapshot()
			seen = nil
			w := e.do(t, http.MethodPut, "/api/alerts", `{}`)
			if w.Code != 400 || len(seen) != 0 || e.runtime.Snapshot() != before {
				t.Error("invalid PUT applied/woke")
			}
			w = e.do(t, http.MethodPost, "/api/config/import", `{}`)
			if w.Code != 400 || len(seen) != 0 || e.runtime.Snapshot() != before {
				t.Error("invalid import applied/woke")
			}
		})
	}
}
