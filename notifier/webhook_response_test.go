package notifier

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestWebhookResponseMatrix 验证各渠道明确成功、业务拒绝与未知响应，并检查资源回收。
func TestWebhookResponseMatrix(t *testing.T) {
	cases := []struct {
		name, channel, body string
		status              int
		ok                  bool
	}{
		{"ding_success", "dingtalk", `{"errcode":0,"errmsg":"ok"}`, 200, true},
		{"ding_business", "dingtalk", `{"errcode":310000,"errmsg":"response-secret"}`, 200, false},
		{"ding_missing", "dingtalk", `{}`, 200, false},
		{"ding_null", "dingtalk", `{"errcode":null}`, 200, false},
		{"ding_string", "dingtalk", `{"errcode":"0"}`, 200, false},
		{"ding_bool", "dingtalk", `{"errcode":false}`, 200, false},
		{"ding_fraction", "dingtalk", `{"errcode":0.5}`, 200, false},
		{"ding_case", "dingtalk", `{"ERRCODE":0}`, 200, false},
		{"ding_extra", "dingtalk", `{"errcode":0,"extra":{"a":1}}`, 200, true},
		{"feishu_current", "feishu", `{"StatusCode":0,"StatusMessage":"success","code":0,"data":{},"msg":"success"}`, 200, true},
		{"feishu_code", "feishu", `{"code":0}`, 200, true},
		{"feishu_legacy", "feishu", `{"Extra":null,"StatusCode":0,"StatusMessage":"success"}`, 200, true},
		{"feishu_business", "feishu", `{"code":19024,"msg":"response-secret"}`, 200, false},
		{"feishu_legacy_business", "feishu", `{"StatusCode":19024}`, 200, false},
		{"feishu_conflict", "feishu", `{"code":19024,"StatusCode":0}`, 200, false},
		{"feishu_reverse_conflict", "feishu", `{"code":0,"StatusCode":19024}`, 200, false},
		{"feishu_null", "feishu", `{"code":null,"StatusCode":0}`, 200, false},
		{"feishu_legacy_null", "feishu", `{"code":0,"StatusCode":null}`, 200, false},
		{"feishu_missing", "feishu", `{"msg":"success"}`, 200, false},
		{"slack_ok", "slack", "ok", 200, true},
		{"slack_space", "slack", " \nok\t", 200, true},
		{"slack_failure", "slack", "invalid_token", 200, false},
		{"slack_json", "slack", `{"ok":true}`, 200, false},
		{"slack_empty", "slack", "", 200, false},
		{"slack_201", "slack", "ok", 201, false},
		{"ding_204", "dingtalk", "", 204, false},
		{"ding_empty", "dingtalk", "", 200, false},
		{"ding_invalid", "dingtalk", "<html>response-secret</html>", 200, false},
		{"ding_array", "dingtalk", `[{"errcode":0}]`, 200, false},
		{"ding_top_null", "dingtalk", `null`, 200, false},
		{"ding_trailing", "dingtalk", `{"errcode":0} {}`, 200, false},
		{"ding_oversize", "dingtalk", `{"errcode":0}` + strings.Repeat(" ", 16*1024), 200, false},
		{"ding_exact_limit", "dingtalk", `{"errcode":0}` + strings.Repeat(" ", 16*1024-len(`{"errcode":0}`)), 200, true},
		{"http_failure", "dingtalk", `{"errcode":0}`, 500, false},
		{"unknown_channel", "unknown-secret", `{"errcode":0}`, 200, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := NewWebhookNotifier("https://host-secret.invalid/path-secret?token=token-secret", tc.channel)
			n.SetInFlightLimiter(NewInFlightLimiter(4))
			var closed atomic.Bool
			n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: &webhookTrackedBody{Reader: strings.NewReader(tc.body), closed: &closed}}, nil
			})}
			err := n.OnEvent(Event{Type: EventDNSFailed, Timestamp: time.Now()})
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%v want=%v err=%v", err == nil, tc.ok, err)
			}
			if !closed.Load() {
				t.Fatal("响应体未关闭")
			}
			if n.limiter.InFlight() != 0 {
				t.Fatal("名额未释放")
			}
			if err != nil {
				for _, s := range []string{"host-secret", "path-secret", "token-secret", "response-secret", "unknown-secret"} {
					if strings.Contains(err.Error(), s) {
						t.Fatalf("错误泄漏 %s", s)
					}
				}
			}
		})
	}
}

type webhookTrackedBody struct {
	io.Reader
	closed   *atomic.Bool
	closeErr error
}

func (b *webhookTrackedBody) Close() error { b.closed.Store(true); return b.closeErr }

type webhookFailingReader struct{}

func (webhookFailingReader) Read([]byte) (int, error) { return 0, errors.New("read-secret") }

func TestWebhookResponseReadFailure(t *testing.T) {
	var closed atomic.Bool
	n := NewWebhookNotifier("https://host-secret.invalid/hook", "dingtalk")
	n.SetInFlightLimiter(NewInFlightLimiter(4))
	n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: &webhookTrackedBody{Reader: webhookFailingReader{}, closed: &closed}}, nil
	})}
	err := n.OnEvent(Event{Type: EventDNSFailed})
	if err == nil {
		t.Fatal("读取失败误判成功")
	}
	if strings.Contains(err.Error(), "read-secret") || errors.Unwrap(err) != nil {
		t.Fatal("泄漏原始错误")
	}
	if !closed.Load() || n.limiter.InFlight() != 0 {
		t.Fatal("资源未释放")
	}
}

// TestWebhookEventBusBusinessWarning 验证三渠道失败经生产事件总线写出安全 WARN。
func TestWebhookEventBusBusinessWarning(t *testing.T) {
	cases := []struct{ channel, body, want string }{
		{"dingtalk", `{"errcode":310000,"errmsg":"response-secret"}`, "code=310000"},
		{"feishu", `{"code":19024,"msg":"response-secret"}`, "code=19024"},
		{"slack", "response-secret", "category=invalid_response"},
	}
	for _, tc := range cases {
		t.Run(tc.channel, func(t *testing.T) {
			logs := captureLogs(t)
			n := NewWebhookNotifier("https://host-secret.invalid/path-secret?token=token-secret", tc.channel)
			n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			bus := NewEventBus()
			bus.Subscribe(EventDNSFailed, n)
			bus.Publish(Event{Type: EventDNSFailed})
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				for _, message := range logs.all() {
					if strings.Contains(message, "事件处理失败") {
						if !strings.Contains(message, "channel="+tc.channel) || !strings.Contains(message, tc.want) {
							t.Fatalf("错误信息不完整: %s", message)
						}
						for _, secret := range []string{"host-secret", "path-secret", "token-secret", "response-secret"} {
							if strings.Contains(message, secret) {
								t.Fatalf("日志泄漏: %s", secret)
							}
						}
						return
					}
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatal("没有业务失败 WARN")
		})
	}
}

func TestWebhookResponseBodyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	n := NewWebhookNotifier(srv.URL, "dingtalk")
	n.client.Timeout = 100 * time.Millisecond
	n.SetInFlightLimiter(NewInFlightLimiter(4))
	start := time.Now()
	err := n.OnEvent(Event{Type: EventDNSFailed})
	if err == nil {
		t.Fatal("响应体未结束却成功")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("读取未受 deadline 限制")
	}
	if n.limiter.InFlight() != 0 {
		t.Fatal("超时后名额未释放")
	}
}

// 真实本地 HTTP 连接验证三渠道响应，以及读完后名额释放。
func TestWebhookResponseHTTPIntegration(t *testing.T) {
	for _, channel := range []string{"dingtalk", "feishu", "slack"} {
		t.Run(channel, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				success := map[string]string{"dingtalk": `{"errcode":0,"errmsg":"ok"}`, "feishu": `{"code":0,"StatusCode":0,"msg":"success","data":{}}`, "slack": "ok"}
				failure := map[string]string{"dingtalk": `{"errcode":310000,"errmsg":"response-secret"}`, "feishu": `{"code":19024,"msg":"response-secret"}`, "slack": "invalid_token"}
				body := success[channel]
				if call%2 == 0 {
					body = failure[channel]
				}
				if _, err := io.WriteString(w, body); err != nil {
					t.Errorf("本地响应写入: %v", err)
				}
			}))
			defer srv.Close()
			n := NewWebhookNotifier(srv.URL, channel)
			n.SetInFlightLimiter(NewInFlightLimiter(4))
			for i := 0; i < 20; i++ {
				err := n.OnEvent(Event{Type: EventDNSFailed})
				if (err == nil) != (i%2 == 0) {
					t.Fatalf("第 %d 次结果 %v", i, err)
				}
				if n.limiter.InFlight() != 0 {
					t.Fatal("名额未释放")
				}
			}
			if calls.Load() != 20 {
				t.Fatal("引入重试或请求丢失")
			}
		})
	}
}

type webhookCountingReader struct {
	n    int
	read int
}

func (r *webhookCountingReader) Read(p []byte) (int, error) {
	if r.n == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.n {
		n = r.n
	}
	for i := 0; i < n; i++ {
		p[i] = ' '
	}
	r.n -= n
	r.read += n
	return n, nil
}
func TestWebhookResponseReadBound(t *testing.T) {
	reader := &webhookCountingReader{n: 1024 * 1024}
	var closed atomic.Bool
	n := NewWebhookNotifier("https://example.invalid", "dingtalk")
	n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: &webhookTrackedBody{Reader: reader, closed: &closed}}, nil
	})}
	err := n.OnEvent(Event{Type: EventDNSFailed})
	if err == nil || reader.read != 16*1024+1 || !closed.Load() {
		t.Fatalf("err=%v read=%d closed=%v", err, reader.read, closed.Load())
	}
}

// 请求头已返回而正文挂起时，名额持续占用，并由生产默认 10 秒上限回收。
func TestWebhookResponseProductionDeadline(t *testing.T) {
	entered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	n := NewWebhookNotifier(srv.URL, "dingtalk")
	n.SetInFlightLimiter(NewInFlightLimiter(4))
	if n.client.Timeout != 10*time.Second {
		t.Fatal("生产超时发生变化")
	}
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- n.OnEvent(Event{Type: EventDNSFailed}) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("未进入服务端")
	}
	if n.limiter.InFlight() != 1 {
		t.Fatal("正文读取期间提前释放名额")
	}
	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err == nil || elapsed < 9*time.Second || elapsed > 12*time.Second {
			t.Fatalf("err=%v elapsed=%v", err, elapsed)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("超时无界")
	}
	if n.limiter.InFlight() != 0 {
		t.Fatal("超时后名额未释放")
	}
}

// TestWebhookResponseCloseWarning 不将响应体关闭错误中的敏感信息写入日志。
func TestWebhookResponseCloseWarning(t *testing.T) {
	logs := captureLogs(t)
	var closed atomic.Bool
	n := NewWebhookNotifier("https://host-secret.invalid/hook", "dingtalk")
	n.SetInFlightLimiter(NewInFlightLimiter(4))
	n.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: &webhookTrackedBody{Reader: strings.NewReader(`{"errcode":0}`), closed: &closed, closeErr: errors.New("close-secret")}}, nil
	})}
	if err := n.OnEvent(Event{Type: EventDNSFailed}); err != nil {
		t.Fatalf("平台已确认成功: %v", err)
	}
	if !closed.Load() || n.limiter.InFlight() != 0 {
		t.Fatal("资源未回收")
	}
	messages := strings.Join(logs.all(), "\n")
	if !strings.Contains(messages, "category=response_close") || !strings.Contains(messages, "channel=dingtalk") {
		t.Fatal("未记录安全关闭错误")
	}
	if strings.Contains(messages, "close-secret") || strings.Contains(messages, "host-secret") {
		t.Fatal("关闭错误泄漏")
	}
}
