package health

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type pushResponseBody struct {
	r        io.Reader
	read     int
	closed   bool
	eof      bool
	closeErr error
}

func (b *pushResponseBody) Read(p []byte) (int, error) {
	n, e := b.r.Read(p)
	b.read += n
	if e == io.EOF {
		b.eof = true
	}
	return n, e
}
func (b *pushResponseBody) Close() error { b.closed = true; return b.closeErr }
func sendPushResponseForTest(t *testing.T, status int, b *pushResponseBody) string {
	t.Helper()
	f := newPushFixture(t)
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	p.client.Transport = pushFailingTransport(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: status, Body: b}, nil })
	buf := &syncBuffer{}
	prev := slog.Default()
	writer, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer func() { slog.SetDefault(prev); log.SetOutput(writer); log.SetFlags(flags); log.SetPrefix(prefix) }()
	if !p.sendOnce(context.Background(), PushConfig{URL: "https://url-secret/api/push/token-secret"}) {
		t.Fatal("valid URL rejected")
	}
	if !b.closed || p.InFlight() {
		t.Fatalf("closed=%v inflight=%v", b.closed, p.InFlight())
	}
	return buf.String()
}
func TestPushResponseDocument(t *testing.T) {
	exact := `{"ok":true}` + strings.Repeat(" ", 16*1024-len(`{"ok":true}`))
	for _, tc := range []struct {
		name, body string
		success    bool
		category   string
	}{
		{"normal", `{"ok":true}`, true, ""},
		{"extra_field", `{"ok":true,"future":"allowed"}`, true, ""},
		{"trailing_space", `{"ok":true}` + " \r\n", true, ""},
		{"exact_limit", exact, true, ""},
		{"over_limit_spaces", exact + " ", false, "response_too_large"},
		{"large_unknown_field", `{"ok":true,"x":"` + strings.Repeat("x", 1024*1024) + `"}`, false, "response_too_large"},
		{"trailing_garbage", `{"ok":true}secret`, false, "invalid_json"},
		{"second_value", `{"ok":true}{"ok":false}`, false, "invalid_json"},
		{"empty", "", false, "invalid_json"},
		{"null", "null", false, "not_ok"},
		{"missing", "{}", false, "not_ok"},
		{"false", `{"ok":false}`, false, "not_ok"},
		{"null_ok", `{"ok":null}`, false, "not_ok"},
		{"true_then_null", `{"ok":true,"ok":null}`, false, "not_ok"},
		{"wrong_type", `{"ok":"true"}`, false, "invalid_json"},
		{"array", `[{"ok":true}]`, false, "invalid_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &pushResponseBody{r: strings.NewReader(tc.body)}
			logs := sendPushResponseForTest(t, 200, b)
			success := strings.Contains(logs, "Uptime Kuma Push 成功")
			if success != tc.success || tc.category != "" && !strings.Contains(logs, "category="+tc.category) {
				t.Errorf("success=%v read=%d logs=%s", success, b.read, logs)
			}
			if success && !b.eof {
				t.Errorf("success before EOF read=%d", b.read)
			}
			if b.read > 16385 {
				t.Errorf("read=%d exceeds 16385", b.read)
			}
			for _, secret := range []string{"url-secret", "token-secret", "secret"} {
				if strings.Contains(logs, secret) {
					t.Errorf("secret leaked: %s", logs)
				}
			}
		})
	}
}

type pushResponseFailReader struct{}

func (pushResponseFailReader) Read([]byte) (int, error) { return 0, errors.New("read-secret") }
func TestPushResponseReadAndClose(t *testing.T) {
	b := &pushResponseBody{r: pushResponseFailReader{}}
	logs := sendPushResponseForTest(t, 200, b)
	if strings.Contains(logs, "read-secret") {
		t.Fatal("raw read error leaked")
	}
	if !strings.Contains(logs, "category=response_read") {
		t.Errorf("%s", logs)
	}
	b = &pushResponseBody{r: strings.NewReader(`{"ok":true}`), closeErr: errors.New("close-secret")}
	logs = sendPushResponseForTest(t, 200, b)
	if strings.Contains(logs, "close-secret") {
		t.Fatal("raw close error leaked")
	}
	if !strings.Contains(logs, "category=response_close") || !strings.Contains(logs, "Push 成功") {
		t.Errorf("%s", logs)
	}
	b = &pushResponseBody{r: pushResponseFailReader{}}
	logs = sendPushResponseForTest(t, 503, b)
	if b.read != 0 || !strings.Contains(logs, "category=http_status status=503") {
		t.Errorf("read=%d %s", b.read, logs)
	}
}
func TestPushResponseHTTPFraming(t *testing.T) {
	for _, kind := range []string{"length", "chunked", "gzip"} {
		t.Run(kind, func(t *testing.T) {
			data := `{"ok":true,"x":"` + strings.Repeat("x", 500000) + `"}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "length":
					w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				case "chunked":
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				case "gzip":
					w.Header().Set("Content-Encoding", "gzip")
					gz := gzip.NewWriter(w)
					_, _ = gz.Write([]byte(data))
					_ = gz.Close()
					return
				}
				_, _ = io.WriteString(w, data)
			}))
			defer srv.Close()
			f := newPushFixture(t)
			p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc, Timeout: time.Second})
			tr := http.DefaultTransport.(*http.Transport).Clone()
			p.client.Transport = tr
			defer tr.CloseIdleConnections()
			buf := &syncBuffer{}
			prev := slog.Default()
			writer, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
			slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer func() { slog.SetDefault(prev); log.SetOutput(writer); log.SetFlags(flags); log.SetPrefix(prefix) }()
			p.sendOnce(context.Background(), PushConfig{URL: srv.URL + "/api/push/token-secret"})
			if !strings.Contains(buf.String(), "category=response_too_large") {
				t.Errorf("%s", buf.String())
			}
		})
	}
}
func TestPushResponsePrefixStall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	f := newPushFixture(t)
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc, Timeout: 120 * time.Millisecond})
	buf := &syncBuffer{}
	prev := slog.Default()
	writer, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer func() { slog.SetDefault(prev); log.SetOutput(writer); log.SetFlags(flags); log.SetPrefix(prefix) }()
	start := time.Now()
	p.sendOnce(context.Background(), PushConfig{URL: srv.URL})
	t.Logf("elapsed=%v logs=%s", time.Since(start), buf.String())
	if strings.Contains(buf.String(), "Push 成功") || !strings.Contains(buf.String(), "category=response_read") {
		t.Errorf("incomplete body counted success: %s", buf.String())
	}
	if time.Since(start) > time.Second {
		t.Fatal("timeout exceeded")
	}
}

// 真实正文读取阶段的 shutdown 取消，而不是仅阻塞响应头。
func TestPushResponseStopDuringBody(t *testing.T) {
	ready := make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(ready)
		<-r.Context().Done()
	}))
	defer srv.Close()
	f := newPushFixture(t)
	f.setConfig(PushConfig{Enabled: true, URL: srv.URL, Interval: time.Hour})
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	go p.Run()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("no request")
	}
	start := time.Now()
	p.Stop()
	if time.Since(start) > time.Second || p.InFlight() || calls.Load() != 1 {
		t.Fatal("stop/retry contract")
	}
}
func TestPushResponseFieldCompatibility(t *testing.T) {
	for _, tc := range []struct {
		body    string
		success bool
	}{
		{`{"OK":true}`, true}, {`{"ok":false,"ok":true}`, true}, {`{"ok":true,"ok":false}`, false}, {`{"ok":true,"ok":null}`, false},
	} {
		logs := sendPushResponseForTest(t, 200, &pushResponseBody{r: bytes.NewBufferString(tc.body)})
		if got := strings.Contains(logs, "Push 成功"); got != tc.success {
			t.Errorf("body=%s success=%v logs=%s", tc.body, got, logs)
		}
	}
}

type pushResponseCloseGate struct {
	r            io.Reader
	atClose      chan struct{}
	releaseClose chan struct{}
}

func (b *pushResponseCloseGate) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *pushResponseCloseGate) Close() error               { close(b.atClose); <-b.releaseClose; return nil }
func TestPushResponseSlotDuringClose(t *testing.T) {
	f := newPushFixture(t)
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	body := &pushResponseCloseGate{r: strings.NewReader(`{"ok":true}`), atClose: make(chan struct{}), releaseClose: make(chan struct{})}
	p.client.Transport = pushFailingTransport(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: 200, Body: body}, nil })
	done := make(chan struct{})
	go func() { p.sendOnce(context.Background(), PushConfig{URL: "https://example.invalid"}); close(done) }()
	select {
	case <-body.atClose:
	case <-time.After(time.Second):
		t.Fatal("Close not entered")
	}
	held := p.InFlight()
	second := p.tryAcquire()
	if second {
		p.release()
	}
	close(body.releaseClose)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("send did not end")
	}
	if !held || second || p.InFlight() {
		t.Fatalf("held=%v second_acquire=%v final=%v", held, second, p.InFlight())
	}
}

type pushResponseReadGate struct {
	r                io.Reader
	entered, release chan struct{}
	once             sync.Once
	closed           atomic.Bool
}

func (b *pushResponseReadGate) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.r.Read(p)
}
func (b *pushResponseReadGate) Close() error { b.closed.Store(true); return nil }
func TestPushResponseSlotDuringRead(t *testing.T) {
	f := newPushFixture(t)
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	body := &pushResponseReadGate{r: strings.NewReader(`{"ok":true}`), entered: make(chan struct{}), release: make(chan struct{})}
	p.client.Transport = pushFailingTransport(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: 200, Body: body}, nil })
	done := make(chan struct{})
	go func() { p.sendOnce(context.Background(), PushConfig{URL: "https://example.invalid"}); close(done) }()
	select {
	case <-body.entered:
	case <-time.After(time.Second):
		close(body.release)
		t.Fatal("Read not entered")
	}
	held, closed := p.InFlight(), body.closed.Load()
	second := p.tryAcquire()
	if second {
		p.release()
	}
	close(body.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("send did not end")
	}
	if !held || closed || second || !body.closed.Load() || p.InFlight() {
		t.Fatalf("held=%v earlyClosed=%v second=%v finalClosed=%v finalInFlight=%v", held, closed, second, body.closed.Load(), p.InFlight())
	}
}
func TestPushResponseIncompleteHTTPBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "40")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	f := newPushFixture(t)
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc, Timeout: time.Second})
	buf := &syncBuffer{}
	prev := slog.Default()
	writer, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer func() { slog.SetDefault(prev); log.SetOutput(writer); log.SetFlags(flags); log.SetPrefix(prefix) }()
	p.sendOnce(context.Background(), PushConfig{URL: srv.URL})
	if strings.Contains(buf.String(), "Push 成功") || !strings.Contains(buf.String(), "category=response_read") {
		t.Fatalf("incomplete framed body: %s", buf.String())
	}
}
func TestPushResponseSuccessReusesHTTP1(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); _, _ = io.WriteString(w, `{"ok":true}`) }))
	defer srv.Close()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	got := make(chan bool, 2)
	f := newPushFixture(t)
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	p.client.Transport = pushFailingTransport(func(r *http.Request) (*http.Response, error) {
		ctx := httptrace.WithClientTrace(r.Context(), &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { got <- i.Reused }})
		return tr.RoundTrip(r.WithContext(ctx))
	})
	buf := &syncBuffer{}
	prev := slog.Default()
	writer, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer func() { slog.SetDefault(prev); log.SetOutput(writer); log.SetFlags(flags); log.SetPrefix(prefix) }()
	for i := 0; i < 2; i++ {
		p.sendOnce(context.Background(), PushConfig{URL: srv.URL})
		select {
		case reused := <-got:
			if reused != (i == 1) {
				t.Errorf("attempt=%d reused=%v", i, reused)
			}
		case <-time.After(time.Second):
			t.Fatal("missing trace")
		}
	}
	if requests.Load() != 2 || strings.Count(buf.String(), "Push 成功") != 2 {
		t.Fatalf("requests=%d logs=%s", requests.Load(), buf.String())
	}
}
func TestPushResponseFailuresKeepPeriod(t *testing.T) {
	for _, mode := range []string{"response_read", "response_too_large"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newPushFixture(t)
				f.setConfig(PushConfig{Enabled: true, URL: "https://example.invalid", Interval: 20 * time.Second})
				p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
				hits := make(chan time.Time, 16)
				p.client.Transport = pushFailingTransport(func(*http.Request) (*http.Response, error) {
					hits <- time.Now()
					var reader io.Reader = pushResponseFailReader{}
					if mode == "response_too_large" {
						reader = strings.NewReader(strings.Repeat(" ", 20000))
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(reader)}, nil
				})
				go p.Run()
				defer p.Stop()
				var first time.Time
				for i := 0; i < 2; i++ {
					select {
					case at := <-hits:
						if i == 0 {
							first = at
						} else if at.Sub(first) < 20*time.Second {
							t.Fatal("immediate retry introduced")
						}
					case <-time.After(21 * time.Second):
						t.Fatal("response failure stopped regular period")
					}
				}
			})
		})
	}
}
func TestPushResponseDefaultTimeoutContract(t *testing.T) {
	f := newPushFixture(t)
	p := NewPusher(PusherDeps{Checker: f.deps.checker(), Config: f.configFunc})
	if p.timeout != 10*time.Second || p.client.Timeout != 10*time.Second {
		t.Fatalf("request/client timeout=%v/%v", p.timeout, p.client.Timeout)
	}
}
