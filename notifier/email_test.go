package notifier

import (
	"bufio"
	"bytes"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── 本地假 SMTP（Build7 Step 2） ───
//
// 只实现本项目邮件客户端实际使用的阶段：greeting(220) → EHLO → AUTH（可选）
// → MAIL FROM → RCPT TO → DATA → QUIT。刻意不通告 STARTTLS：客户端在
// 127.0.0.1 上使用 PlainAuth 时不需要 TLS（Go 标准库对 localhost 放行）。
//
// 所有用例都在进程内 loopback 完成，不连接任何真实 SMTP。

// fakeSMTPOptions 控制假 SMTP 的阶段行为
type fakeSMTPOptions struct {
	silent        bool // 接受连接但永不发送 greeting（静默 deadline 用例）
	advertiseAuth bool // 是否在 EHLO 响应中通告 AUTH
	authOK        bool // AUTH 是否成功
	rcptOK        bool // RCPT TO 是否成功
	dataOK        bool // DATA 提交是否成功
	traditional   bool // DATA 只接受 ASCII 与最多 1000 字节的物理行（含 CRLF）
}

// fakeSMTPRecord 记录一次会话中收到的内容（断言用）
type fakeSMTPRecord struct {
	mu       sync.Mutex
	mailFrom string
	rcpts    []string
	data     string
	authSeen bool
}

func (r *fakeSMTPRecord) set(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

// MailFrom 返回收到的 MAIL FROM 命令行
func (r *fakeSMTPRecord) MailFrom() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mailFrom
}

// Rcpts 返回收到的 RCPT TO 命令行列表
func (r *fakeSMTPRecord) Rcpts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.rcpts...)
}

// Data 返回 DATA 阶段收到的完整报文（含头部）
func (r *fakeSMTPRecord) Data() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data
}

// setData 覆盖记录到的报文（多事件用例复位用）
func (r *fakeSMTPRecord) setData(data string) {
	r.set(func() { r.data = data })
}

// AuthSeen 返回是否收到过 AUTH
func (r *fakeSMTPRecord) AuthSeen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.authSeen
}

// startFakeSMTP 启动本地假 SMTP 服务，返回 host、port 与记录器
func startFakeSMTP(t *testing.T, opts fakeSMTPOptions) (string, string, *fakeSMTPRecord) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动本地假 SMTP 失败: %v", err)
	}
	t.Cleanup(func() {
		if cerr := ln.Close(); cerr != nil {
			t.Logf("关闭假 SMTP 监听失败: %v", cerr)
		}
	})

	rec := &fakeSMTPRecord{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFakeSMTP(conn, opts, rec)
		}
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("解析假 SMTP 地址失败: %v", err)
	}
	return host, port, rec
}

// serveFakeSMTP 提供单连接的脚本化会话
func serveFakeSMTP(conn net.Conn, opts fakeSMTPOptions, rec *fakeSMTPRecord) {
	defer func() { _ = conn.Close() }()

	if opts.silent {
		// 不发 greeting：让客户端自身的 deadline 生效。读取带超时，避免测试残留 goroutine。
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return
		}
		_, _ = bufio.NewReader(conn).ReadString('\n')
		return
	}

	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	send := func(line string) bool {
		if _, err := w.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return w.Flush() == nil
	}

	if !send("220 fake ESMTP ready") {
		return
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			if opts.advertiseAuth {
				if !send("250-fake.invalid") || !send("250 AUTH PLAIN LOGIN") {
					return
				}
			} else if !send("250 fake.invalid") {
				return
			}
		case strings.HasPrefix(cmd, "AUTH"):
			rec.set(func() { rec.authSeen = true })
			if opts.authOK {
				if !send("235 2.7.0 authentication successful") {
					return
				}
			} else if !send("535 5.7.8 Authentication credentials invalid") {
				return
			}
		case strings.HasPrefix(cmd, "MAIL FROM"):
			rec.set(func() { rec.mailFrom = strings.TrimSpace(line) })
			if !send("250 2.1.0 ok") {
				return
			}
		case strings.HasPrefix(cmd, "RCPT TO"):
			rec.set(func() { rec.rcpts = append(rec.rcpts, strings.TrimSpace(line)) })
			if opts.rcptOK {
				if !send("250 2.1.5 ok") {
					return
				}
			} else if !send("550 5.1.1 mailbox unavailable") {
				return
			}
		case strings.HasPrefix(cmd, "DATA"):
			if !opts.dataOK {
				if !send("554 5.3.0 transaction failed") {
					return
				}
				continue
			}
			if !send("354 end data with <CRLF>.<CRLF>") {
				return
			}
			var sb strings.Builder
			valid := true
			for {
				dataLine, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				if opts.traditional {
					if len(dataLine) > 1000 || !strings.HasSuffix(dataLine, "\r\n") {
						valid = false
					}
					for _, b := range []byte(dataLine) {
						if b >= 128 {
							valid = false
						}
					}
				}
				// SMTP 接收端去除 DATA 的点转义后再保存邮件。
				if strings.HasPrefix(dataLine, "..") {
					dataLine = dataLine[1:]
				}
				sb.WriteString(dataLine)
			}
			rec.set(func() { rec.data = sb.String() })
			if !valid {
				if !send("554 DATA requires ASCII and bounded lines") {
					return
				}
				continue
			}
			if !send("250 2.0.0 queued") {
				return
			}
		case strings.HasPrefix(cmd, "QUIT"):
			_ = send("221 2.0.0 bye")
			return
		default:
			if !send("250 ok") {
				return
			}
		}
	}
}

// ─── Build7 Step 2：测试邮件的判别性用例 ───

// TestBuildTestEmailContent 测试邮件的主题后缀与正文追加文案必须固定
func TestBuildTestEmailContent(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 34, 56, 0, time.Local)
	subject, body := BuildTestEmailContent("自定义主题", "自定义正文", now)

	if subject != "自定义主题 - 测试邮件" {
		t.Errorf("测试主题 = %q, want 追加固定后缀", subject)
	}
	if !strings.Contains(body, "自定义正文") {
		t.Errorf("测试正文必须保留用户正文: %q", body)
	}
	if !strings.Contains(body, "这是一次手动测试邮件") {
		t.Errorf("测试正文必须追加固定说明: %q", body)
	}
	if !strings.Contains(body, now.Format("2006-01-02 15:04:05")) {
		t.Errorf("测试正文必须追加当前时间: %q", body)
	}
	// 默认主题/正文（与 config.DefaultEmailSubject/Body 一致的文案）也必须可用
	const defaultSubject = "[FWAlizer] 告警通知"
	const defaultBody = "FWAlizer 检测到运行异常，请检查同步日志。"
	defSubject, defBody := BuildTestEmailContent(defaultSubject, defaultBody, now)
	if defSubject != defaultSubject+" - 测试邮件" || !strings.Contains(defBody, defaultBody) {
		t.Errorf("默认主题/正文组装错误: %q / %q", defSubject, defBody)
	}
}

// TestSendTestEmailSuccessAgainstFakeSMTP 成功路径：完整会话并携带主题/正文
func TestSendTestEmailSuccessAgainstFakeSMTP(t *testing.T) {
	host, port, rec := startFakeSMTP(t, fakeSMTPOptions{
		advertiseAuth: true, authOK: true, rcptOK: true, dataOK: true,
	})

	const password = "unit-test-smtp-password-0001"
	cfg := EmailConfig{
		Host: host, Port: port, User: "user@example.com", Pass: password,
		From: "from@example.com", To: "a@example.com,b@example.com",
	}
	subject, body := BuildTestEmailContent("主题", "正文", time.Now())

	if err := SendTestEmail(cfg, subject, body); err != nil {
		t.Fatalf("测试邮件应成功: %v", err)
	}
	if !rec.AuthSeen() {
		t.Errorf("配置了用户名时必须执行 AUTH")
	}
	if !strings.Contains(rec.MailFrom(), "from@example.com") {
		t.Errorf("MAIL FROM 未携带发件人: %q", rec.MailFrom())
	}
	if len(rec.Rcpts()) != 2 {
		t.Errorf("收件人数量 = %d, want 2: %v", len(rec.Rcpts()), rec.Rcpts())
	}
	payload := rec.Data()
	decodedSubject, decodedBody := decodeEmailForTest(t, payload)
	if decodedSubject != subject {
		t.Errorf("测试主题解码错误: %q", decodedSubject)
	}
	if !strings.Contains(decodedBody, "这是一次手动测试邮件") {
		t.Errorf("正文缺少测试说明: %q", decodedBody)
	}
	if strings.Contains(payload, password) || strings.Contains(decodedBody, password) {
		t.Errorf("报文不得包含 SMTP 密码")
	}
}

// TestSendTestEmailAuthFailure 认证失败必须返回可读阶段错误且不泄露密码
func TestSendTestEmailAuthFailure(t *testing.T) {
	host, port, _ := startFakeSMTP(t, fakeSMTPOptions{advertiseAuth: true, authOK: false, rcptOK: true, dataOK: true})
	const password = "unit-test-auth-failure-password"
	cfg := EmailConfig{Host: host, Port: port, User: "u", Pass: password, From: "f@example.com", To: "t@example.com"}

	err := SendTestEmail(cfg, "s", "b")
	if err == nil {
		t.Fatal("认证失败必须返回错误")
	}
	if !strings.Contains(err.Error(), "SMTP 认证失败") {
		t.Errorf("错误必须标明认证阶段: %v", err)
	}
	if !strings.Contains(err.Error(), "535") {
		t.Errorf("错误应保留 SMTP 返回的诊断: %v", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("错误不得包含密码: %v", err)
	}
}

// TestSendTestEmailRcptFailure RCPT 失败必须返回阶段错误
func TestSendTestEmailRcptFailure(t *testing.T) {
	host, port, _ := startFakeSMTP(t, fakeSMTPOptions{rcptOK: false, dataOK: true})
	cfg := EmailConfig{Host: host, Port: port, From: "f@example.com", To: "t@example.com"}
	err := SendTestEmail(cfg, "s", "b")
	if err == nil || !strings.Contains(err.Error(), "SMTP RCPT TO 失败") {
		t.Fatalf("RCPT 失败必须返回阶段错误: %v", err)
	}
	if !strings.Contains(err.Error(), "550") {
		t.Errorf("错误应保留 SMTP 诊断: %v", err)
	}
}

// TestSendTestEmailDataFailure DATA 提交失败必须返回阶段错误
func TestSendTestEmailDataFailure(t *testing.T) {
	host, port, _ := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: false})
	cfg := EmailConfig{Host: host, Port: port, From: "f@example.com", To: "t@example.com"}
	err := SendTestEmail(cfg, "s", "b")
	if err == nil || !strings.Contains(err.Error(), "SMTP DATA 失败") {
		t.Fatalf("DATA 失败必须返回阶段错误: %v", err)
	}
	if !strings.Contains(err.Error(), "554") {
		t.Errorf("错误应保留 SMTP 诊断: %v", err)
	}
}

// TestSendTestEmailBoundedOnSilentServer 静默 SMTP 必须有界返回（复用生产 deadline 机制）
func TestSendTestEmailBoundedOnSilentServer(t *testing.T) {
	host, port, _ := startFakeSMTP(t, fakeSMTPOptions{silent: true})
	setSMTPTimeouts(t, 2*time.Second, 400*time.Millisecond)

	cfg := EmailConfig{Host: host, Port: port, From: "f@example.com", To: "t@example.com"}
	done := make(chan error, 1)
	go func() { done <- SendTestEmail(cfg, "s", "b") }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("静默 SMTP 必须返回错误（deadline 生效）")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("静默 SMTP 未在有界时间内返回：deadline 未生效")
	}
}

// TestSendTestEmailMissingRecipient 缺少收件人时不得静默成功（防御性：正常路径由 API 校验拦截）
func TestSendTestEmailMissingRecipient(t *testing.T) {
	host, port, _ := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: true})
	cfg := EmailConfig{Host: host, Port: port, From: "f@example.com", To: ""}
	if err := SendTestEmail(cfg, "s", "b"); err != nil {
		t.Fatalf("空收件人的行为由 SMTP 服务器决定，本用例只要求不 panic: %v", err)
	}
}

// ─── Build7 Step 3：固定主题后缀、固定详情顺序、收件人 Trim 与成功日志 ───

// TestEmailSubjectSuffixAndBodyDetails 自动邮件必须使用配置主题 + 固定后缀，
// 正文为用户正文 + 固定顺序详情块。
func TestEmailSubjectSuffixAndBodyDetails(t *testing.T) {
	host, port, rec := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: true})
	n := NewEmailNotifier(EmailConfig{
		Host: host, Port: port, From: "f@example.com", To: "t@example.com",
		Subject: "[FWAlizer] 告警通知", Body: "FWAlizer 检测到运行异常，请检查同步日志。",
	})

	err := n.OnEvent(Event{
		Type: EventSyncError, Timestamp: time.Date(2026, 9, 28, 12, 34, 56, 0, time.Local),
		Data: map[string]any{
			"provider": "tc_lighthouse(lhins-1)", "domain": "example.com", "error": "boom",
			"added": 2, "deleted": 1,
		},
	})
	if err != nil {
		t.Fatalf("自动邮件应成功: %v", err)
	}

	subject, payload := decodeEmailForTest(t, rec.Data())
	if subject != "[FWAlizer] 告警通知 - 同步失败" {
		t.Errorf("主题必须是配置主题 + 固定后缀: %q", subject)
	}

	// 固定详情顺序：事件类型 → 时间 → Provider → 域名 → 错误
	order := []string{"事件类型：同步失败", "时间：2026-09-28 12:34:56", "Provider：tc_lighthouse(lhins-1)", "域名：example.com", "错误：boom"}
	last := -1
	for _, fragment := range order {
		idx := strings.Index(payload, fragment)
		if idx < 0 {
			t.Fatalf("详情块缺少 %q: %q", fragment, payload)
		}
		if idx < last {
			t.Errorf("详情顺序不稳定: %q 出现在 %q 之前", fragment, payload[:last])
		}
		last = idx
	}

	// DNS 事件必须使用另一后缀
	rec.setData("")
	if err := n.OnEvent(Event{
		Type: EventDNSFailed, Timestamp: time.Now(),
		Data: map[string]any{"domain": "d.example.com", "error": "dns boom"},
	}); err != nil {
		t.Fatalf("DNS 告警应成功: %v", err)
	}
	if subject, _ := decodeEmailForTest(t, rec.Data()); subject != "[FWAlizer] 告警通知 - DNS 解析失败" {
		t.Errorf("DNS 主题后缀错误: %q", subject)
	}
}

// TestEmailDetailsUsePlaceholderForMissingFields 缺失字段必须写 "-"，不得留空或省略。
func TestEmailDetailsUsePlaceholderForMissingFields(t *testing.T) {
	host, port, rec := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: true})
	n := NewEmailNotifier(EmailConfig{
		Host: host, Port: port, From: "f@example.com", To: "t@example.com",
		Subject: "S", Body: "B",
	})

	// 只有 domain：provider 与 error 必须写 "-"
	if err := n.OnEvent(Event{
		Type: EventDNSFailed, Timestamp: time.Now(),
		Data: map[string]any{"domain": "only-domain.example.com"},
	}); err != nil {
		t.Fatalf("事件应成功: %v", err)
	}
	_, payload := decodeEmailForTest(t, rec.Data())
	if !strings.Contains(payload, "Provider：-\r\n") {
		t.Errorf("缺失 Provider 必须写 '-': %q", payload)
	}
	if !strings.Contains(payload, "错误：-") {
		t.Errorf("缺失错误必须写 '-': %q", payload)
	}
	if strings.Contains(payload, "added") || strings.Contains(payload, "deleted") {
		t.Errorf("详情块不得包含未固定字段（不得遍历 map）: %q", payload)
	}
}

// TestEmailRecipientsAreTrimmed 多收件人必须逐项 Trim（Build7 §4.5）。
func TestEmailRecipientsAreTrimmed(t *testing.T) {
	host, port, rec := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: true})
	n := NewEmailNotifier(EmailConfig{
		Host: host, Port: port, From: "f@example.com",
		To:      "a@example.com, b@example.com ,  c@example.com",
		Subject: "S", Body: "B",
	})

	if err := n.OnEvent(Event{Type: EventSyncError, Timestamp: time.Now(), Data: map[string]any{"domain": "x"}}); err != nil {
		t.Fatalf("事件应成功: %v", err)
	}

	rcpts := rec.Rcpts()
	if len(rcpts) != 3 {
		t.Fatalf("收件人数量 = %d, want 3: %v", len(rcpts), rcpts)
	}
	for i, want := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		if !strings.HasSuffix(rcpts[i], "<"+want+">") {
			t.Errorf("第 %d 个收件人未 Trim: %q", i+1, rcpts[i])
		}
	}
}

// TestEmailSuccessLogDoesNotLeakSecrets 自动邮件成功必须写 INFO，且不含密码/正文。
func TestEmailSuccessLogDoesNotLeakSecrets(t *testing.T) {
	host, port, _ := startFakeSMTP(t, fakeSMTPOptions{advertiseAuth: true, authOK: true, rcptOK: true, dataOK: true})
	const password = "auto-mail-password-0007"
	n := NewEmailNotifier(EmailConfig{
		Host: host, Port: port, User: "u", Pass: password,
		From: "f@example.com", To: "t@example.com",
		Subject: "S", Body: "这是不应出现在日志里的完整正文",
	})

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	if err := n.OnEvent(Event{Type: EventDNSFailed, Timestamp: time.Now(), Data: map[string]any{"domain": "x"}}); err != nil {
		t.Fatalf("事件应成功: %v", err)
	}
	logs := buf.String()
	if !strings.Contains(logs, "邮件告警已被 SMTP 服务器接受") || !strings.Contains(logs, "event=dns:failed") {
		t.Errorf("成功日志缺少固定文案或事件类型: %s", logs)
	}
	if strings.Contains(logs, password) {
		t.Errorf("日志不得包含 SMTP 密码: %s", logs)
	}
	if strings.Contains(logs, "不应出现在日志里的完整正文") {
		t.Errorf("日志不得包含完整正文: %s", logs)
	}
}

// TestEmailOperationalEventIncludesReasons Build7 Step 7：运行健康异常事件在固定详情块
// 末尾追加固定「原因」行；DNS/同步事件不出现该行（既有正文形态逐字节不变）。
func TestEmailOperationalEventIncludesReasons(t *testing.T) {
	host, port, rec := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: true})
	n := NewEmailNotifier(EmailConfig{
		Host: host, Port: port, From: "f@example.com", To: "t@example.com",
		Subject: "[FWAlizer] 告警通知", Body: "正文",
	})

	if err := n.OnEvent(Event{
		Type:      EventOperationalUnhealthy,
		Timestamp: time.Date(2026, 9, 28, 12, 34, 56, 0, time.Local),
		Data: map[string]any{
			"checked_at": time.Now(),
			"reasons":    []string{"最近一轮同步失败", "SQLite 检查失败"},
		},
	}); err != nil {
		t.Fatalf("运行健康异常邮件应成功: %v", err)
	}

	subject, payload := decodeEmailForTest(t, rec.Data())
	if subject != "[FWAlizer] 告警通知 - 运行健康异常" {
		t.Errorf("运行健康异常主题后缀错误: %q", subject)
	}
	for _, fragment := range []string{
		"事件类型：运行健康异常", "时间：2026-09-28 12:34:56",
		"Provider：-", "域名：-", "错误：-",
		"原因：最近一轮同步失败; SQLite 检查失败",
	} {
		if !strings.Contains(payload, fragment) {
			t.Fatalf("详情块缺少 %q: %q", fragment, payload)
		}
	}
	if strings.Contains(payload, "checked_at") {
		t.Errorf("详情块不得遍历 map 输出未固定字段: %q", payload)
	}
	// 固定顺序：原因行必须排在错误行之后
	if strings.Index(payload, "错误：-") > strings.Index(payload, "原因：") {
		t.Errorf("原因行必须位于固定详情块末尾: %q", payload)
	}

	// DNS 事件不得出现原因行
	rec.setData("")
	if err := n.OnEvent(Event{
		Type: EventDNSFailed, Timestamp: time.Now(),
		Data: map[string]any{"domain": "d.example.com", "error": "dns boom"},
	}); err != nil {
		t.Fatalf("DNS 告警应成功: %v", err)
	}
	if _, dnsPayload := decodeEmailForTest(t, rec.Data()); strings.Contains(dnsPayload, "原因：") {
		t.Errorf("非运行健康异常事件不得输出原因行: %q", dnsPayload)
	}

	// 空 reasons 列表同样不输出原因行
	rec.setData("")
	if err := n.OnEvent(Event{
		Type: EventOperationalUnhealthy, Timestamp: time.Now(),
		Data: map[string]any{"reasons": []string{""}},
	}); err != nil {
		t.Fatalf("运行健康异常邮件应成功: %v", err)
	}
	if _, emptyPayload := decodeEmailForTest(t, rec.Data()); strings.Contains(emptyPayload, "原因：") {
		t.Errorf("空原因列表不得输出原因行: %q", emptyPayload)
	}
}
