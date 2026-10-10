package notifier

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ─── Issue6 A2：SMTP deadline 与「每渠道在途 ≤4」 ───

// captureLogs 捕获测试期间的 slog 输出（并发安全）。
func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	c := &logCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(c))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return c
}

type logCapture struct {
	mu   sync.Mutex
	msgs []string
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(a.Value.String())
		return true
	})
	c.mu.Lock()
	c.msgs = append(c.msgs, sb.String())
	c.mu.Unlock()
	return nil
}

func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *logCapture) WithGroup(string) slog.Handler      { return c }

func (c *logCapture) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.msgs...)
}

// setSMTPTimeouts 缩短 SMTP 接缝取值用于测试，返回恢复函数。
func setSMTPTimeouts(t *testing.T, dial, deadline time.Duration) {
	t.Helper()
	origDial, origDeadline := smtpDialTimeout, smtpDeadline
	smtpDialTimeout, smtpDeadline = dial, deadline
	t.Cleanup(func() { smtpDialTimeout, smtpDeadline = origDial, origDeadline })
}

// TestSMTPDefaultTimeoutValues 默认取值必须恒为 10s / 30s（接缝不得改变生产语义）。
func TestSMTPDefaultTimeoutValues(t *testing.T) {
	if smtpDefaultDialTimeout != 10*time.Second {
		t.Errorf("smtpDefaultDialTimeout = %v, want 10s", smtpDefaultDialTimeout)
	}
	if smtpDefaultDeadline != 30*time.Second {
		t.Errorf("smtpDefaultDeadline = %v, want 30s", smtpDefaultDeadline)
	}
	if smtpDialTimeout != smtpDefaultDialTimeout {
		t.Errorf("smtpDialTimeout 默认值 = %v, want %v", smtpDialTimeout, smtpDefaultDialTimeout)
	}
	if smtpDeadline != smtpDefaultDeadline {
		t.Errorf("smtpDeadline 默认值 = %v, want %v", smtpDeadline, smtpDefaultDeadline)
	}
}

// TestEmailSendBoundedByDeadlineOnSilentServer 静默 SMTP 必须在 deadline + 裕量内返回。
//
// 判别性：修复前使用 smtp.SendMail（net.Dial 无 timeout、greeting 读取无 deadline），
// 对「accept 但永不回包」的服务会永久阻塞，本用例永远等不到结果。
func TestEmailSendBoundedByDeadlineOnSilentServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动静默 SMTP 服务失败: %v", err)
	}
	release := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// 持有连接且不发送 greeting，直到测试收尾；避免 GC 提前关闭连接。
		<-release
		if err := conn.Close(); err != nil {
			t.Errorf("关闭静默 SMTP 连接失败: %v", err)
		}
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("解析监听地址失败: %v", err)
	}

	setSMTPTimeouts(t, 2*time.Second, 400*time.Millisecond)
	var sendDone <-chan error
	t.Cleanup(func() {
		close(release)
		if err := ln.Close(); err != nil {
			t.Errorf("关闭静默 SMTP 监听失败: %v", err)
		}
		select {
		case <-serverDone:
		case <-time.After(3 * time.Second):
			t.Error("静默 SMTP 服务未有界退出")
		}
		if sendDone != nil {
			select {
			case <-sendDone:
			case <-time.After(3 * time.Second):
				t.Error("SMTP 发送未有界退出")
			}
		}
	})

	n := NewEmailNotifier(EmailConfig{
		Host: host, Port: port, User: "", Pass: "",
		From: "f@example.com", To: "t@example.com",
	})

	const budget = 5 * time.Second
	done := make(chan error, 1)
	sendDone = done
	go func() {
		done <- n.OnEvent(Event{Type: EventSyncError, Timestamp: time.Now()})
	}()

	select {
	case err := <-done:
		sendDone = nil
		if err == nil {
			t.Fatal("对静默 SMTP 必须返回错误（deadline 生效）")
		}
		if !strings.Contains(err.Error(), "会话超时") {
			t.Fatalf("静默 SMTP 必须返回安全超时类别，实际: %v", err)
		}
	case <-time.After(budget):
		t.Fatalf("静默 SMTP 未在 %v 内返回：deadline 未生效（修复前为无界阻塞）", budget)
	}
}

// TestInFlightLimiterMechanism 限流器单元语义：容量 4、满载失败、release 幂等。
func TestInFlightLimiterMechanism(t *testing.T) {
	l := NewInFlightLimiter(InFlightLimit)

	releases := make([]func(), 0, InFlightLimit)
	for i := 0; i < InFlightLimit; i++ {
		release, ok := l.Acquire()
		if !ok {
			t.Fatalf("第 %d 次 Acquire 必须成功", i+1)
		}
		releases = append(releases, release)
	}
	if got := l.InFlight(); got != InFlightLimit {
		t.Errorf("在途 = %d, want %d", got, InFlightLimit)
	}
	if _, ok := l.Acquire(); ok {
		t.Fatal("满载时 Acquire 必须失败（丢弃最新）")
	}

	// release 幂等：重复调用不得让容量膨胀
	releases[0]()
	releases[0]()
	if got := l.InFlight(); got != InFlightLimit-1 {
		t.Errorf("release 之后在途 = %d, want %d", got, InFlightLimit-1)
	}
	if _, ok := l.Acquire(); !ok {
		t.Error("释放一个名额后必须可以再次 Acquire")
	}
}

// blockingWebhookServer 是「已接受但挂起」的 Webhook 服务：记录并发连接数。
func blockingWebhookServer(t *testing.T) (srv *httptest.Server, concurrent func() int32, entered chan struct{}, release chan struct{}) {
	t.Helper()
	var cur, max atomic.Int32
	entered = make(chan struct{}, 16)
	release = make(chan struct{})

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		for {
			old := max.Load()
			if n <= old || max.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		cur.Add(-1)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"errcode":0}`)); err != nil {
			t.Errorf("本地响应写入: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, max.Load, entered, release
}

// TestWebhookInFlightCapAndDropNewest 阻塞 Webhook 下在途 ≤4，第 5 条被丢弃并输出安全 WARN。
//
// 「在途」的确定性观测 = 假服务端**已接受的并发请求数**（不使用 runtime.NumGoroutine）。
func TestWebhookInFlightCapAndDropNewest(t *testing.T) {
	logs := captureLogs(t)
	srv, maxConcurrent, entered, release := blockingWebhookServer(t)

	n := NewWebhookNotifier(srv.URL, "dingtalk")
	n.SetInFlightLimiter(NewInFlightLimiter(InFlightLimit))

	var wg sync.WaitGroup
	for i := 0; i < InFlightLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = n.OnEvent(Event{Type: EventSyncError, Timestamp: time.Now()})
		}()
	}

	// 等待 4 个请求全部进入服务端
	for i := 0; i < InFlightLimit; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			wg.Wait()
			t.Fatalf("第 %d 个在途请求未到达服务端", i+1)
		}
	}
	if got := maxConcurrent(); got > InFlightLimit {
		close(release)
		wg.Wait()
		t.Fatalf("服务端并发 = %d, 超过在途上限 %d", got, InFlightLimit)
	}

	// 满载时再发一条：必须被丢弃、不产生第 5 个服务端请求，且有安全 WARN
	extra := make(chan error, 1)
	go func() { extra <- n.OnEvent(Event{Type: EventSyncError, Timestamp: time.Now()}) }()
	select {
	case err := <-extra:
		if err != nil {
			t.Errorf("满载丢弃不得返回错误（否则会被当作订阅者故障）: %v", err)
		}
	case <-time.After(3 * time.Second):
		close(release)
		wg.Wait()
		t.Fatal("满载时 OnEvent 必须立即返回（丢弃最新），不得阻塞")
	}

	if got := maxConcurrent(); got > InFlightLimit {
		t.Errorf("丢弃后服务端并发 = %d, 不得超过 %d", got, InFlightLimit)
	}

	rejected := func() bool {
		for _, m := range logs.all() {
			if strings.Contains(m, "在途已满") && strings.Contains(m, "channel=dingtalk") {
				return true
			}
		}
		return false
	}()
	if !rejected {
		t.Errorf("必须输出「在途已满」WARN 且只带渠道名；实际日志: %v", logs.all())
	}
	for _, m := range logs.all() {
		if strings.Contains(m, srv.URL) {
			t.Errorf("告警日志不得包含 Webhook URL: %s", m)
		}
	}

	close(release)
	wg.Wait()
}

// TestWebhookLimiterSurvivesHotReload 在途上限在配置热重载后仍然成立。
//
// 模拟 AlertManager 语义：限流器由管理器长期持有，热重载只替换 notifier 实例。
// 旧实例尚未完成的发送继续占用名额，新实例不得因此获得额外容量。
func TestWebhookLimiterSurvivesHotReload(t *testing.T) {
	logs := captureLogs(t)
	srv, _, entered, release := blockingWebhookServer(t)
	limiter := NewInFlightLimiter(InFlightLimit)

	// 旧实例占满全部名额
	oldNotifier := NewWebhookNotifier(srv.URL, "dingtalk")
	oldNotifier.SetInFlightLimiter(limiter)

	var wg sync.WaitGroup
	for i := 0; i < InFlightLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = oldNotifier.OnEvent(Event{Type: EventSyncError, Timestamp: time.Now()})
		}()
	}
	for i := 0; i < InFlightLimit; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			wg.Wait()
			t.Fatalf("旧实例第 %d 个在途请求未到达服务端", i+1)
		}
	}

	// 热重载：替换实例，但沿用同一限流器（AlertManager 的真实语义）
	freshNotifier := NewWebhookNotifier(srv.URL, "dingtalk")
	freshNotifier.SetInFlightLimiter(limiter)

	// 旧实例的在途发送不取消、不等待：此时新实例必须仍然受到 ≤4 约束
	if err := freshNotifier.OnEvent(Event{Type: EventSyncError, Timestamp: time.Now()}); err != nil {
		t.Errorf("满载丢弃不得返回错误: %v", err)
	}
	found := false
	for _, m := range logs.all() {
		if strings.Contains(m, "在途已满") {
			found = true
		}
	}
	if !found {
		t.Error("热重载后满载仍必须丢弃最新并输出 WARN（限流器必须跨热重载连续）")
	}

	close(release)
	wg.Wait()
}

// TestEmailInFlightCapAndDropNewest 邮件渠道同样受 ≤4 约束，且与 Webhook 各自独立。
func TestEmailInFlightCapAndDropNewest(t *testing.T) {
	logs := captureLogs(t)
	setSMTPTimeouts(t, 10*time.Second, 30*time.Second)

	limiter := NewInFlightLimiter(InFlightLimit)
	n := NewEmailNotifier(EmailConfig{
		Host: "127.0.0.1", Port: "1", // 立即失败，验证「在途占用 → 释放」链路
		From: "f@example.com", To: "t@example.com",
	})
	n.SetInFlightLimiter(limiter)

	// 手工占满名额，模拟 4 条邮件正在发送中
	releases := make([]func(), 0, InFlightLimit)
	for i := 0; i < InFlightLimit; i++ {
		release, ok := limiter.Acquire()
		if !ok {
			t.Fatalf("第 %d 次 Acquire 必须成功", i+1)
		}
		releases = append(releases, release)
	}

	if err := n.OnEvent(Event{Type: EventDNSFailed, Timestamp: time.Now()}); err != nil {
		t.Errorf("满载丢弃不得返回错误: %v", err)
	}
	found := false
	for _, m := range logs.all() {
		if strings.Contains(m, "在途已满") && strings.Contains(m, "channel=email") {
			found = true
		}
	}
	if !found {
		t.Errorf("邮件渠道满载必须输出 WARN；实际日志: %v", logs.all())
	}

	// 释放一个名额后必须能真正尝试发送（此处指向无效端口，错误来自连接失败而非限流）
	for _, r := range releases {
		r()
	}
	if err := n.OnEvent(Event{Type: EventDNSFailed, Timestamp: time.Now()}); err == nil {
		t.Error("名额释放后必须真正尝试发送（指向无效端口应返回连接错误）")
	}
}
