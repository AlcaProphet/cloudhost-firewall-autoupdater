package health

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func r19HealthReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("I8R19: 等待完成信号超时")
		var zero T
		return zero
	}
}

type r19HealthGate struct {
	entered chan context.Context
	release chan struct{}
}

func (g *r19HealthGate) PingContext(ctx context.Context) error {
	g.entered <- ctx
	<-g.release
	return ctx.Err()
}

// 观察实际 Run 返回；持久阻塞只用于证明 Stop 尚不能提前返回。
func TestI8R19SupervisorLifecycle(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "checking"}[blocked], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d := newHealthTestDeps()
				checker := d.checker()
				gate := &r19HealthGate{make(chan context.Context, 1), make(chan struct{})}
				var release sync.Once
				defer release.Do(func() { close(gate.release) })
				if blocked {
					checker.deps.Pinger = gate
				}
				s := NewSupervisor(SupervisorDeps{Checker: checker, Interval: time.Hour})
				runDone := make(chan struct{})
				go func() { s.Run(); close(runDone) }()
				defer func() { release.Do(func() { close(gate.release) }); s.Stop(); r19HealthReceive(t, runDone) }()
				if blocked {
					r19HealthReceive(t, gate.entered)
				} else {
					synctest.Wait()
				}
				stops := make(chan struct{}, 4)
				for range 4 {
					go func() { s.Stop(); stops <- struct{}{} }()
				}
				synctest.Wait()
				if blocked {
					if len(stops) != 0 {
						t.Error("I8R19: Supervisor Stop 未等待正在执行的检查")
					}
				}
				release.Do(func() { close(gate.release) })
				// 先观察实际 Run 返回，再验证全部停止等待者均释放。
				r19HealthReceive(t, runDone)
				for range 4 {
					r19HealthReceive(t, stops)
				}
				s.Stop()
				s.Wake()
				time.Sleep(2 * time.Hour)
				synctest.Wait()
				if !blocked && d.pinger.Calls() != 1 {
					t.Fatal("I8R19: Supervisor 退出后仍执行检查")
				}
			})
		})
	}
}

func TestI8R19PushLifecycle(t *testing.T) {
	for _, mode := range []string{"disabled", "timer", "checking"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newPushFixture(t)
				f.setConfig(PushConfig{Enabled: mode != "disabled", URL: "https://example.invalid", Interval: time.Hour})
				checker := f.deps.checker()
				gate := &r19HealthGate{make(chan context.Context, 1), make(chan struct{})}
				var release sync.Once
				defer release.Do(func() { close(gate.release) })
				if mode == "checking" {
					checker.deps.Pinger = gate
				}
				p := NewPusher(PusherDeps{Checker: checker, Config: f.configFunc})
				calls := 0
				p.client.Transport = pushFailingTransport(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
				})
				runDone := make(chan struct{})
				go func() { p.Run(); close(runDone) }()
				defer func() { release.Do(func() { close(gate.release) }); p.Stop(); r19HealthReceive(t, runDone) }()
				var checkCtx context.Context
				if mode == "checking" {
					checkCtx = r19HealthReceive(t, gate.entered)
				} else {
					synctest.Wait()
				}
				stops := make(chan struct{}, 4)
				for range 4 {
					go func() { p.Stop(); stops <- struct{}{} }()
				}
				synctest.Wait()
				if mode == "checking" {
					if checkCtx.Err() != context.Canceled {
						t.Error("I8R19: Push Stop 没有传播原有取消")
					}
					if len(stops) != 0 {
						t.Error("I8R19: Push Stop 未等待健康检查返回")
					}
				}
				release.Do(func() { close(gate.release) })
				r19HealthReceive(t, runDone)
				for range 4 {
					r19HealthReceive(t, stops)
				}
				if p.InFlight() {
					t.Error("I8R19: Run 返回仍占用在途名额")
				}
				before := calls
				p.Wake()
				p.Stop()
				time.Sleep(2 * time.Hour)
				synctest.Wait()
				if calls != before || (mode == "disabled" && calls != 0) {
					t.Error("I8R19: Push 停止或关闭时仍发送")
				}
			})
		})
	}
}

type r19TraceTransport struct {
	base http.RoundTripper
	idle chan struct{}
}

func (x r19TraceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	trace := &httptrace.ClientTrace{PutIdleConn: func(err error) {
		if err == nil {
			select {
			case x.idle <- struct{}{}:
			default:
			}
		}
	}}
	return x.base.RoundTrip(r.WithContext(httptrace.WithClientTrace(r.Context(), trace)))
}

// 使用实际 socket 的身份与关闭信号，避免用进程 FD 数量推断回收。
func TestI8R19PushOwnedPool(t *testing.T) {
	remotes, closed := make(chan string, 8), make(chan string, 8)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remotes <- r.RemoteAddr
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Error(err)
		}
	}))
	srv.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed <- c.RemoteAddr().String()
		}
	}
	srv.Start()
	defer srv.Close()
	shared := &http.Client{Timeout: 3 * time.Second}
	defer shared.CloseIdleConnections()
	getShared := func() string {
		resp, err := shared.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("响应读取/关闭失败: %v / %v", readErr, closeErr)
		}
		return r19HealthReceive(t, remotes)
	}
	sharedRemote := getShared()
	f := newPushFixture(t)
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL, Interval: time.Hour})
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	idle := make(chan struct{}, 1)
	p.client.Transport = r19TraceTransport{p.client.Transport, idle}
	runDone := make(chan struct{})
	go func() { p.Run(); close(runDone) }()
	defer p.Stop()
	r19HealthReceive(t, idle)
	pushRemote := r19HealthReceive(t, remotes)
	p.Stop()
	r19HealthReceive(t, runDone)
	if pushRemote == sharedRemote {
		t.Error("I8R19: Push 复用了进程共享池")
	}
	select {
	case remote := <-closed:
		if remote != pushRemote {
			t.Error("I8R19: Push 关闭了其他客户端的连接")
		}
	case <-time.After(3 * time.Second):
		t.Error("I8R19_BEHAVIOR: stopped Push retained idle socket")
	}
	if getShared() != sharedRemote {
		t.Error("I8R19: 共享客户端的空闲连接被关闭")
	}
}

func TestI8R19PushHTTP2Pool(t *testing.T) {
	closed := make(chan struct{}, 1)
	proto := make(chan int, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto <- r.ProtoMajor
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Error(err)
		}
	}))
	srv.EnableHTTP2 = true
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed <- struct{}{}
		}
	}
	srv.StartTLS()
	defer srv.Close()
	f := newPushFixture(t)
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	defer p.cancel()
	defer p.ownedTransport.CloseIdleConnections()
	p.ownedTransport.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	if !p.sendOnce(context.Background(), PushConfig{URL: srv.URL}) || r19HealthReceive(t, proto) != 2 {
		t.Fatal("I8R19: 未实际执行 HTTP/2 Push")
	}
	runDone := make(chan struct{})
	go func() { p.Run(); close(runDone) }()
	p.Stop()
	r19HealthReceive(t, runDone)
	r19HealthReceive(t, closed)
}
