package webui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestI8R19RequestLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestServer(t, 0)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		s.mux.HandleFunc("GET /r19", func(http.ResponseWriter, *http.Request) { close(entered); <-release })
		finished := make(chan struct{})
		go func() {
			s.serveRequest(httptest.NewRecorder(), httptest.NewRequest("GET", "/r19", nil))
			close(finished)
		}()
		<-entered
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		waited := make(chan struct{}, 4)
		for range 4 {
			go func() { s.WaitRequests(); waited <- struct{}{} }()
		}
		synctest.Wait()
		if len(waited) != 0 {
			t.Error("I8R19: request join 在 handler 完成前返回")
		}
		late := httptest.NewRecorder()
		s.serveRequest(late, httptest.NewRequest("GET", "/api/health", nil))
		if late.Code != http.StatusServiceUnavailable || late.Header().Get("Cache-Control") != "no-store" {
			t.Error("I8R19: 冻结后仍准入业务请求")
		}
		once.Do(func() { close(release) })
		<-finished
		for range 4 {
			<-waited
		}
		s.WaitRequests()
	})
}

func TestI8R19ForcedCloseJoinsHandler(t *testing.T) {
	s, port := startTestServer(t, newTestServer(t, freeTCPPort(t)))
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	s.mux.HandleFunc("GET /r19", func(http.ResponseWriter, *http.Request) {
		close(entered)
		defer close(finished)
		<-release
	})
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/r19", port))
		if resp != nil {
			_, readErr := io.Copy(io.Discard, resp.Body)
			err = errors.Join(err, readErr, resp.Body.Close())
		}
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler 未进入")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v", err)
	}
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
		t.Fatal("handler 在释放前意外完成")
	default:
	}
	once.Do(func() { close(release) })
	s.WaitRequests()
	<-finished
	if err := <-requestDone; err == nil {
		t.Error("强制关闭后的客户端请求应失败")
	}
}

// 包装层透传原 ResponseWriter，不隐藏 SSE 所需的 Flush/ResponseController。
func TestI8R19RequestWriterCapabilities(t *testing.T) {
	s := newTestServer(t, 0)
	s.mux.HandleFunc("GET /r19", func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
	})
	w := httptest.NewRecorder()
	s.serveRequest(w, httptest.NewRequest("GET", "/r19", nil))
	s.WaitRequests()
	if !w.Flushed {
		t.Fatal("I8R19: 包装层破坏 Flush")
	}
}

func TestI8R19ConcurrentRequestFreeze(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestServer(t, 0)
		release := make(chan struct{})
		var admitted atomic.Int32
		s.mux.HandleFunc("GET /r19", func(http.ResponseWriter, *http.Request) {
			admitted.Add(1)
			<-release
		})
		results := make(chan int, 32)
		for range 32 {
			go func() {
				w := httptest.NewRecorder()
				s.serveRequest(w, httptest.NewRequest("GET", "/r19", nil))
				results <- w.Code
			}()
		}
		waited := make(chan struct{})
		go func() { s.WaitRequests(); close(waited) }()
		synctest.Wait()
		if admitted.Load() > 0 {
			select {
			case <-waited:
				t.Error("I8R19: 冻结与准入并发时漏等 handler")
			default:
			}
		}
		close(release)
		<-waited
		var accepted int32
		for range 32 {
			switch code := <-results; code {
			case http.StatusOK:
				accepted++
			case http.StatusServiceUnavailable:
			default:
				t.Errorf("意外响应码: %d", code)
			}
		}
		if accepted != admitted.Load() {
			t.Fatal("I8R19: 准入数与实际 handler 数不符")
		}
	})
}
