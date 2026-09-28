package health

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// ─── Build7 §7.6：Uptime Kuma Push 的判别性用例（全部使用本地 httptest） ───

// pushRecorder 记录收到的 Push 请求
type pushRecorder struct {
	mu       sync.Mutex
	requests []*http.Request
	queries  []string
}

func (r *pushRecorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	r.queries = append(r.queries, req.URL.RawQuery)
}

func (r *pushRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *pushRecorder) lastQuery() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queries) == 0 {
		return ""
	}
	return r.queries[len(r.queries)-1]
}

// lastURL 返回最近一次请求的完整 URL（含 path，用于断言 token 保留）
func (r *pushRecorder) lastURL() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		return ""
	}
	return r.requests[len(r.requests)-1].URL.String()
}

// startPushServer 启动本地 Push 目标服务器
func startPushServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *pushRecorder) {
	t.Helper()
	rec := &pushRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// okHandler 返回 Uptime Kuma 成功响应
func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// pushTestFixture 组装 Checker 与可变的 Push 配置
type pushTestFixture struct {
	deps   *healthTestDeps
	mu     sync.Mutex
	config PushConfig
}

func newPushFixture(t *testing.T) *pushTestFixture {
	t.Helper()
	d := newHealthTestDeps()
	d.setup(func() { d.status = baseStatus(d.now) })
	return &pushTestFixture{deps: d, config: PushConfig{}}
}

func (f *pushTestFixture) setConfig(cfg PushConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.config = cfg
}

func (f *pushTestFixture) configFunc() PushConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.config
}

// startPush 启动 Push 循环（短超时接缝，避免测试变慢）
func (f *pushTestFixture) startPush(t *testing.T, timeout time.Duration) *Pusher {
	t.Helper()
	p := NewPusher(PusherDeps{
		Checker: f.deps.checker(),
		Config:  f.configFunc,
		Timeout: timeout,
	})
	go p.Run()
	t.Cleanup(func() { p.Stop() })
	return p
}

// waitForRequests 等待至少 n 个请求
func waitForRequests(t *testing.T, rec *pushRecorder, n int, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if rec.count() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待 %d 个 Push 请求超时（当前 %d）", n, rec.count())
}

// TestPushDisabledMakesNoRequests 默认/关闭时不得发出任何请求。
func TestPushDisabledMakesNoRequests(t *testing.T) {
	f := newPushFixture(t)
	srv, rec := startPushServer(t, okHandler)
	f.setConfig(PushConfig{Enabled: false, URL: srv.URL + "/api/push/tok-disabled", Interval: 20 * time.Millisecond})

	f.startPush(t, time.Second)
	time.Sleep(200 * time.Millisecond)
	if got := rec.count(); got != 0 {
		t.Fatalf("关闭状态请求数 = %d, want 0", got)
	}

	// 空 URL 同样不得请求
	f.setConfig(PushConfig{Enabled: true, URL: "", Interval: 20 * time.Millisecond})
	time.Sleep(200 * time.Millisecond)
	if got := rec.count(); got != 0 {
		t.Fatalf("空 URL 请求数 = %d, want 0", got)
	}
}

// TestPushImmediateFirstSendAndQueryContract 启用后立即首发，并覆盖 status/msg/ping、
// 保留 token 与其他未知 query。
func TestPushImmediateFirstSendAndQueryContract(t *testing.T) {
	f := newPushFixture(t)
	srv, rec := startPushServer(t, okHandler)

	const token = "push-token-sentinel-1234"
	f.setConfig(PushConfig{
		Enabled: true, Interval: time.Hour, // 周期很长：只应出现「立即首发」那一次
		URL: srv.URL + "/api/push/" + token + "?foo=bar&status=down",
	})

	p := f.startPush(t, time.Second)
	waitForRequests(t, rec, 1, 3*time.Second)
	time.Sleep(100 * time.Millisecond) // 确认没有额外周期请求
	if got := rec.count(); got != 1 {
		t.Fatalf("立即首发后请求数 = %d, want 1", got)
	}
	_ = p

	q := rec.lastQuery()
	values := parseQuery(q)
	if values["status"] != "up" {
		t.Errorf("status = %q, want up（必须覆盖 URL 里已有的 status）", values["status"])
	}
	if values["msg"] != "OK" {
		t.Errorf("msg = %q, want OK", values["msg"])
	}
	if _, err := strconv.ParseInt(values["ping"], 10, 64); err != nil {
		t.Errorf("ping 必须是毫秒整数: %q", values["ping"])
	}
	if values["foo"] != "bar" {
		t.Errorf("未知 query 必须保留: %q", q)
	}
	if !strings.Contains(rec.lastURL(), token) {
		t.Errorf("Uptime Kuma token（路径）必须保留: %q", rec.lastURL())
	}
}

// TestPushUpDownRecovery 每次发送当前状态：健康 up、异常 down（短原因）、恢复 up。
func TestPushUpDownRecovery(t *testing.T) {
	f := newPushFixture(t)
	srv, rec := startPushServer(t, okHandler)
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL + "/api/push/t", Interval: 30 * time.Millisecond})

	p := f.startPush(t, time.Second)
	waitForRequests(t, rec, 1, 3*time.Second)
	if got := parseQuery(rec.lastQuery())["status"]; got != "up" {
		t.Fatalf("健康时 status = %q, want up", got)
	}

	// 异常：down + 原因
	f.deps.setRunning(false)
	waitForStatus(t, rec, "down", 3*time.Second)
	msg := parseQuery(rec.lastQuery())["msg"]
	if !strings.Contains(msg, ReasonSyncerStopped) {
		t.Errorf("down 的 msg 必须包含稳定原因: %q", msg)
	}

	// 恢复：up
	f.deps.setRunning(true)
	p.Wake()
	waitForStatus(t, rec, "up", 3*time.Second)
}

// waitForStatus 等待出现指定 status 的请求
func waitForStatus(t *testing.T, rec *pushRecorder, status string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if parseQuery(rec.lastQuery())["status"] == status {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待 status=%s 超时，最后一个 query = %q", status, rec.lastQuery())
}

// TestPushDownMessageTruncatedTo250Chars 长原因必须截断到 250 字符以内。
func TestPushDownMessageTruncatedTo250Chars(t *testing.T) {
	f := newPushFixture(t)
	srv, rec := startPushServer(t, okHandler)
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL + "/api/push/t", Interval: 20 * time.Millisecond})

	p := f.startPush(t, time.Second)
	waitForRequests(t, rec, 1, 3*time.Second)
	p.Stop()

	// 直接验证构建函数：超长原因必须截断
	long := strings.Repeat("原", 400)
	msg := buildDownMessage([]string{long})
	if len([]rune(msg)) > 250 {
		t.Errorf("down msg rune 长度 = %d, want <= 250", len([]rune(msg)))
	}

	// 多原因用 "; " 连接
	joined := buildDownMessage([]string{"A", "B"})
	if joined != "A; B" {
		t.Errorf("多原因连接错误: %q", joined)
	}
}

// TestPushFailureModes HTTP 非 2xx、ok=false、坏 JSON 都必须判为失败。
func TestPushFailureModes(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"非 2xx": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"ok=false": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":false}`))
		},
		"坏 JSON": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`not json`)) },
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPushFixture(t)
			srv, rec := startPushServer(t, handler)
			f.setConfig(PushConfig{Enabled: true, URL: srv.URL + "/api/push/tok-secret", Interval: time.Hour})

			buf := &syncBuffer{}
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
			defer slog.SetDefault(prev)

			p := f.startPush(t, time.Second)
			waitForRequests(t, rec, 1, 3*time.Second)

			logs := waitForLogContains(t, buf, "Uptime Kuma Push 失败", 3*time.Second)
			p.Stop()
			if strings.Contains(logs, "tok-secret") || strings.Contains(logs, srv.URL) {
				t.Errorf("失败日志不得包含完整 URL 或 token: %s", logs)
			}
		})
	}
}

// syncBuffer 是并发安全的日志缓冲：slog handler 会在后台 goroutine 中写入。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write 实现 io.Writer
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String 返回当前日志内容
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForLogContains 等待日志出现指定文本（返回当时的完整日志）
func waitForLogContains(t *testing.T, buf *syncBuffer, want string, budget time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return buf.String()
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待日志 %q 超时，当前日志: %s", want, buf.String())
	return buf.String()
}

// TestPushSingleInFlightSkipsNewTicks 在途时新 tick 必须跳过，不排队、不重试。
func TestPushSingleInFlightSkipsNewTicks(t *testing.T) {
	f := newPushFixture(t)
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	srv, rec := startPushServer(t, func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		okHandler(w, nil)
	})
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL + "/api/push/t", Interval: 20 * time.Millisecond})

	p := f.startPush(t, 5*time.Second)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("首个请求未发出")
	}

	// 在途期间触发多个 tick 与唤醒：必须被跳过（请求数保持 1）
	time.Sleep(150 * time.Millisecond)
	p.Wake()
	time.Sleep(50 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Fatalf("在途期间请求数 = %d, want 1（不得排队/重试）", got)
	}

	close(release)
	waitForRequests(t, rec, 2, 3*time.Second)
}

// TestPushRequestTimeoutIsBounded HTTP 必须有界返回（超时接缝 200ms）。
func TestPushRequestTimeoutIsBounded(t *testing.T) {
	f := newPushFixture(t)
	block := make(chan struct{})
	srv, _ := startPushServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	defer close(block)
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL + "/api/push/t", Interval: time.Hour})

	p := f.startPush(t, 200*time.Millisecond)
	start := time.Now()
	// 在途请求必须在超时上限附近返回：等待其在途标志释放
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !p.InFlight() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Push 请求必须有界（超时接缝 200ms），实际 %v", elapsed)
	}
}

// TestPushHotReloadURLAndInterval 热重载：URL 变化立即首发；关闭后停止。
func TestPushHotReloadURLAndInterval(t *testing.T) {
	f := newPushFixture(t)
	srvA, recA := startPushServer(t, okHandler)
	srvB, recB := startPushServer(t, okHandler)

	f.setConfig(PushConfig{Enabled: true, URL: srvA.URL + "/api/push/a", Interval: time.Hour})
	p := f.startPush(t, time.Second)
	waitForRequests(t, recA, 1, 3*time.Second)

	// URL 变化 → 立即向新地址首发
	f.setConfig(PushConfig{Enabled: true, URL: srvB.URL + "/api/push/b", Interval: time.Hour})
	p.Wake()
	waitForRequests(t, recB, 1, 3*time.Second)

	// 关闭 → 不再发送（即使周期很短）
	f.setConfig(PushConfig{Enabled: false, URL: srvB.URL + "/api/push/b", Interval: 20 * time.Millisecond})
	p.Wake()
	time.Sleep(150 * time.Millisecond)
	afterDisable := recB.count()
	time.Sleep(150 * time.Millisecond)
	if got := recB.count(); got != afterDisable {
		t.Errorf("关闭后仍在发送: %d → %d", afterDisable, got)
	}
}

// TestPushShutdownCancelsInFlight shutdown 必须取消在途请求并有界返回。
func TestPushShutdownCancelsInFlight(t *testing.T) {
	f := newPushFixture(t)
	canceled := make(chan struct{}, 1)
	srv, _ := startPushServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			canceled <- struct{}{}
		case <-time.After(10 * time.Second):
		}
	})
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL + "/api/push/t", Interval: time.Hour})

	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc, Timeout: 10 * time.Second})
	go p.Run()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !p.InFlight() {
		time.Sleep(5 * time.Millisecond)
	}
	if !p.InFlight() {
		t.Fatal("请求未进入在途状态")
	}

	start := time.Now()
	p.Stop()
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Stop 必须在有界时间内返回，实际 %v", elapsed)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Errorf("shutdown 必须取消在途请求")
	}
}

// TestPushFailureDoesNotAffectHealthOrPublishEvents Push 失败不得改变应用健康，
// 也不得发布任何告警事件（避免自激循环）。
func TestPushFailureDoesNotAffectHealthOrPublishEvents(t *testing.T) {
	f := newPushFixture(t)
	srv, rec := startPushServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	pub := &recordingPublisher{}
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL + "/api/push/t", Interval: 20 * time.Millisecond})

	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc, Timeout: time.Second, Bus: pub})
	go p.Run()
	t.Cleanup(func() { p.Stop() })

	waitForRequests(t, rec, 2, 3*time.Second)
	if got := pub.count(); got != 0 {
		t.Errorf("Push 失败不得发布告警事件，实际 %d", got)
	}
	if res := f.deps.checker().Evaluate(context.Background()); !res.Healthy {
		t.Errorf("Push 失败不得改变应用健康: %+v", res)
	}
}

// TestPushSuccessLogHasNoURL 成功日志必须是 DEBUG 且不含 URL/token。
func TestPushSuccessLogHasNoURL(t *testing.T) {
	f := newPushFixture(t)
	srv, rec := startPushServer(t, okHandler)
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL + "/api/push/token-abc", Interval: time.Hour})

	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	p := f.startPush(t, time.Second)
	waitForRequests(t, rec, 1, 3*time.Second)
	logs := waitForLogContains(t, buf, "Uptime Kuma Push 成功", 3*time.Second)
	p.Stop()
	if strings.Contains(logs, "token-abc") || strings.Contains(logs, srv.URL) {
		t.Errorf("成功日志不得包含 URL/token: %s", logs)
	}
}

// parseQuery 把 raw query 解析为已解码的 map（只用于断言）
func parseQuery(raw string) map[string]string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for key := range values {
		out[key] = values.Get(key)
	}
	return out
}

var _ = config.DefaultPushInterval
var _ = notifier.EventOperationalUnhealthy
var _ = syncer.RoundIdle
