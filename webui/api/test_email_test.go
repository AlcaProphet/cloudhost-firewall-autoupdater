package api

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"testing"
)

// ─── Build7 Step 2：测试邮件端点的判别性用例 ───
//
// 全部使用进程内 loopback 假 SMTP；不连接任何真实 SMTP，也不代表真实投递。

// apiFakeSMTPRecord 记录假 SMTP 收到的会话内容
type apiFakeSMTPRecord struct {
	mu    sync.Mutex
	rcpts []string
	data  string
}

func (r *apiFakeSMTPRecord) addRcpt(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rcpts = append(r.rcpts, line)
}

func (r *apiFakeSMTPRecord) setData(data string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = data
}

// Rcpts 返回收到的 RCPT TO 列表
func (r *apiFakeSMTPRecord) Rcpts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.rcpts...)
}

// Data 返回 DATA 阶段收到的报文
func (r *apiFakeSMTPRecord) Data() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data
}

// decodeAPIMail 按接收端 MIME 语义检查真实 SMTP 报文。
func decodeAPIMail(t *testing.T, wire string) (string, string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(wire))
	if err != nil {
		t.Fatalf("解析 SMTP 报文: %v", err)
	}
	if msg.Header.Get("MIME-Version") != "1.0" || msg.Header.Get("Content-Transfer-Encoding") != "base64" {
		t.Fatalf("SMTP 报文 MIME 字段错误: %v", msg.Header)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/plain" || !strings.EqualFold(params["charset"], "UTF-8") {
		t.Fatalf("SMTP 报文必须是纯文本 UTF-8: %v / %v", msg.Header, err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("解码 SMTP Subject: %v", err)
	}
	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
	if err != nil {
		t.Fatalf("解码 SMTP 正文: %v", err)
	}
	return subject, string(body)
}

// startAPIFakeSMTP 启动端点用例使用的最小假 SMTP（addr 返回 "host:port" 形式）
func startAPIFakeSMTP(t *testing.T, authOK bool) (string, *apiFakeSMTPRecord) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动假 SMTP 失败: %v", err)
	}
	t.Cleanup(func() {
		if cerr := ln.Close(); cerr != nil {
			t.Logf("关闭假 SMTP 失败: %v", cerr)
		}
	})

	rec := &apiFakeSMTPRecord{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				w := bufio.NewWriter(c)
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
						if !send("250-fake.invalid") || !send("250 AUTH PLAIN LOGIN") {
							return
						}
					case strings.HasPrefix(cmd, "AUTH"):
						if authOK {
							if !send("235 2.7.0 ok") {
								return
							}
						} else if !send("535 5.7.8 Authentication credentials invalid") {
							return
						}
					case strings.HasPrefix(cmd, "MAIL FROM"):
						if !send("250 2.1.0 ok") {
							return
						}
					case strings.HasPrefix(cmd, "RCPT TO"):
						rec.addRcpt(strings.TrimSpace(line))
						if !send("250 2.1.5 ok") {
							return
						}
					case strings.HasPrefix(cmd, "DATA"):
						if !send("354 end data with <CRLF>.<CRLF>") {
							return
						}
						var sb strings.Builder
						for {
							dl, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if strings.TrimRight(dl, "\r\n") == "." {
								break
							}
							if strings.HasPrefix(dl, "..") {
								dl = dl[1:]
							}
							sb.WriteString(dl)
						}
						rec.setData(sb.String())
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
			}(conn)
		}
	}()
	return ln.Addr().String(), rec
}

// testEmailBody 构造测试邮件请求体
func testEmailBody(t *testing.T, addr, password, subject, body string) string {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("解析假 SMTP 地址失败: %v", err)
	}
	return `{"host":"` + host + `","port":"` + port + `","security":"auto_starttls","username":"form-user",` +
		`"password":"` + password + `","from_addr":"form-from@example.com",` +
		`"to_addr":"form-to@example.com","subject":"` + subject + `","body":"` + body + `"}`
}

// TestTestEmailUsesRequestValuesAndDoesNotWrite 端点必须使用请求表单值，
// 不写库、不进协调器、不 Apply、不改变订阅。
func TestTestEmailUsesRequestValuesAndDoesNotWrite(t *testing.T) {
	e := newTestEnv(t)
	// 预置与请求完全不同的 Store 值（host 指向必然不可达的地址）
	if w := e.do(t, http.MethodPut, "/api/alerts", alertsBody("store.invalid", "store-pass-0002", "", "dingtalk")); w.Code != http.StatusOK {
		t.Fatalf("预置 Store 告警失败: %d %s", w.Code, w.Body.String())
	}

	addr, rec := startAPIFakeSMTP(t, true)

	emailBefore, err := e.store.GetAlertEmail()
	if err != nil {
		t.Fatalf("读取预置邮件配置失败: %v", err)
	}
	applyBefore := e.applyCount()
	alertsBefore := e.alerts.Current()

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	const formPassword = "form-password-0003"
	w := e.do(t, http.MethodPost, "/api/alerts/test-email",
		testEmailBody(t, addr, formPassword, "表单主题", "表单正文"))

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"success":true`) ||
		!strings.Contains(w.Body.String(), "SMTP 服务器已接受测试邮件") {
		t.Errorf("成功响应不符合固定口径: %s", w.Body.String())
	}

	// 假 SMTP 必须收到表单值（而不是 Store 值）
	subject, payload := decodeAPIMail(t, rec.Data())
	if subject != "表单主题 - 测试邮件" {
		t.Errorf("主题必须来自请求并追加测试后缀: %q", subject)
	}
	if !strings.Contains(payload, "表单正文") || !strings.Contains(payload, "这是一次手动测试邮件") {
		t.Errorf("正文必须来自请求并追加测试说明: %q", payload)
	}
	if len(rec.Rcpts()) != 1 || !strings.Contains(rec.Rcpts()[0], "form-to@example.com") {
		t.Errorf("收件人必须来自请求: %v", rec.Rcpts())
	}

	// 零写入、零发布、订阅不变
	emailAfter, err := e.store.GetAlertEmail()
	if err != nil {
		t.Fatalf("读取邮件配置失败: %v", err)
	}
	if *emailAfter != *emailBefore {
		t.Errorf("测试邮件不得写库: before=%+v after=%+v", emailBefore, emailAfter)
	}
	if got := e.applyCount(); got != applyBefore {
		t.Errorf("测试邮件不得触发运行时发布: applyCount=%d, want %d", got, applyBefore)
	}
	alertsAfter := e.alerts.Current()
	if alertsAfter.email != alertsBefore.email || alertsAfter.webhook != alertsBefore.webhook ||
		len(alertsAfter.events) != len(alertsBefore.events) {
		t.Errorf("测试邮件不得改变告警订阅: before=%+v after=%+v", alertsBefore, alertsAfter)
	}

	// 日志：成功 INFO，且不含密码或正文
	logs := logBuf.String()
	if !strings.Contains(logs, "测试邮件已被 SMTP 服务器接受") {
		t.Errorf("必须写成功 INFO 日志: %s", logs)
	}
	if strings.Contains(logs, formPassword) {
		t.Errorf("日志不得包含 SMTP 密码: %s", logs)
	}
	if strings.Contains(logs, "表单正文") {
		t.Errorf("日志不得包含完整正文: %s", logs)
	}
}

// TestTestEmailValidationErrors JSON/字段错误必须 400 且零写入零发布。
func TestTestEmailValidationErrors(t *testing.T) {
	addr, _ := startAPIFakeSMTP(t, true)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("解析假 SMTP 地址失败: %v", err)
	}
	base := func(overrides map[string]string) string {
		fields := map[string]string{
			"host": host, "port": port, "security": "auto_starttls", "username": "u", "password": "p",
			"from_addr": "f@example.com", "to_addr": "t@example.com",
			"subject": "s", "body": "b",
		}
		for k, v := range overrides {
			if v == "" {
				delete(fields, k)
				continue
			}
			fields[k] = v
		}
		parts := make([]string, 0, len(fields))
		for _, k := range []string{"host", "port", "security", "username", "password", "from_addr", "to_addr", "subject", "body"} {
			if v, ok := fields[k]; ok {
				parts = append(parts, `"`+k+`":"`+v+`"`)
			}
		}
		return "{" + strings.Join(parts, ",") + "}"
	}

	cases := map[string]string{
		"缺 host":    base(map[string]string{"host": ""}),
		"缺 from":    base(map[string]string{"from_addr": ""}),
		"缺 to":      base(map[string]string{"to_addr": ""}),
		"缺 subject": base(map[string]string{"subject": ""}),
		"缺 body":    base(map[string]string{"body": ""}),
		"端口非法":      base(map[string]string{"port": "abc"}),
		"host 为 null": `{"host":null,"port":"` + port + `","security":"auto_starttls","username":"u","password":"p",` +
			`"from_addr":"f@example.com","to_addr":"t@example.com","subject":"s","body":"b"}`,
		"未知字段":      strings.TrimSuffix(base(nil), "}") + `,"extra":1}`,
		"尾随 JSON":   base(nil) + `{"x":1}`,
		"空 body 请求": ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			applyBefore := e.applyCount()
			w := e.do(t, http.MethodPost, "/api/alerts/test-email", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if got := e.applyCount(); got != applyBefore {
				t.Errorf("非法请求不得触发运行时发布: %d, want %d", got, applyBefore)
			}
			if email, _ := e.store.GetAlertEmail(); email.Host != "" || email.Password != "" {
				t.Errorf("非法请求不得写库: %+v", email)
			}
		})
	}
}

// TestTestEmailSmtpFailureReturnsStageErrorWithoutSecrets SMTP 阶段失败必须是
// 结构化结果（HTTP 200 + success=false），错误可展示但不含密码或正文。
func TestTestEmailSmtpFailureReturnsStageErrorWithoutSecrets(t *testing.T) {
	e := newTestEnv(t)
	addr, _ := startAPIFakeSMTP(t, false)

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	const formPassword = "auth-failure-password-0004"
	w := e.do(t, http.MethodPost, "/api/alerts/test-email",
		testEmailBody(t, addr, formPassword, "主题X", "正文Y"))

	if w.Code != http.StatusOK {
		t.Fatalf("SMTP 阶段错误应返回 HTTP 200 结构化结果，实际 %d; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"success":false`) || !strings.Contains(body, "SMTP 认证失败") {
		t.Errorf("失败响应必须包含阶段错误: %s", body)
	}
	if strings.Contains(body, formPassword) {
		t.Errorf("失败响应不得包含 SMTP 密码: %s", body)
	}
	if strings.Contains(body, "正文Y") {
		t.Errorf("失败响应不得包含完整正文: %s", body)
	}
	logs := logBuf.String()
	if !strings.Contains(logs, "测试邮件发送失败") {
		t.Errorf("必须写失败 WARN 日志: %s", logs)
	}
	if strings.Contains(logs, formPassword) {
		t.Errorf("日志不得包含 SMTP 密码: %s", logs)
	}
	if strings.Contains(logs, "正文Y") {
		t.Errorf("日志不得包含完整正文: %s", logs)
	}
}

// TestTestEmailRejectsOversizedBody 普通请求 1 MiB 上限仍然生效（413）。
func TestTestEmailRejectsOversizedBody(t *testing.T) {
	e := newTestEnv(t)
	addr, _ := startAPIFakeSMTP(t, true)
	big := testEmailBody(t, addr, "p", "s", strings.Repeat("x", maxJSONBodyBytes))
	w := e.do(t, http.MethodPost, "/api/alerts/test-email", big)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, want 413", w.Code)
	}
}
