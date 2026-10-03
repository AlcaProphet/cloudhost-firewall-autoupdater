package notifier

import (
	"encoding/base64"
	"io"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// decodeEmailForTest 按 MIME 字段解码，既有业务断言检查接收端文本而非传输字节。
func decodeEmailForTest(t *testing.T, wire string) (string, string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(wire))
	if err != nil {
		t.Fatalf("解析 MIME 邮件: %v", err)
	}
	if msg.Header.Get("MIME-Version") != "1.0" || msg.Header.Get("Content-Transfer-Encoding") != "base64" {
		t.Fatalf("MIME-Version/CTE 错误: %v", msg.Header)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/plain" || !strings.EqualFold(params["charset"], "UTF-8") {
		t.Fatalf("必须是纯文本 UTF-8: %v / %v", msg.Header, err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("解码 Subject: %v", err)
	}
	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
	if err != nil {
		t.Fatalf("解码正文: %v", err)
	}
	return subject, string(body)
}

// assertEmailEncoding 检查接收端报文的行长、字符边界与逐字文本等价性。
func assertEmailEncoding(t *testing.T, wire, wantSubject, wantBody string) {
	t.Helper()
	header, body, ok := strings.Cut(wire, "\r\n\r\n")
	if !ok {
		t.Fatal("缺失头部与正文边界")
	}
	for _, line := range strings.Split(header, "\r\n") {
		if len(line) > 76 {
			t.Fatalf("头部行长超限: %d", len(line))
		}
	}
	for _, word := range strings.Fields(header) {
		if strings.HasPrefix(word, "=?") {
			if len(word) > 75 || !strings.HasPrefix(strings.ToUpper(word), "=?UTF-8?B?") {
				t.Fatalf("encoded-word 长度或编码错误: %q", word)
			}
			decoded, err := new(mime.WordDecoder).Decode(word)
			if err != nil || !utf8.ValidString(decoded) {
				t.Fatalf("encoded-word 字符被拆断: %q / %v", word, err)
			}
		}
	}
	for _, line := range strings.Split(body, "\r\n") {
		if len(line) > 76 {
			t.Fatalf("正文编码行超限: %d", len(line))
		}
	}
	subject, decodedBody := decodeEmailForTest(t, wire)
	if subject != wantSubject {
		t.Errorf("主题解码不等价: got %q want %q", subject, wantSubject)
	}
	// 以逐字符扫描独立表达文本规范，避免测试复制生产的 ReplaceAll 实现。
	var canonical strings.Builder
	for i := 0; i < len(wantBody); i++ {
		switch wantBody[i] {
		case '\r':
			canonical.WriteString("\r\n")
			if i+1 < len(wantBody) && wantBody[i+1] == '\n' {
				i++
			}
		case '\n':
			canonical.WriteString("\r\n")
		default:
			canonical.WriteByte(wantBody[i])
		}
	}
	if decodedBody != canonical.String() {
		t.Errorf("正文解码不等价: got len=%d want len=%d", len(decodedBody), canonical.Len())
	}
}

// TestEmailMIMETestSend 测试邮件必须通过未宣告 SMTPUTF8/8BITMIME 的 SMTP。
func TestEmailMIMETestSend(t *testing.T) {
	cases := []struct{ name, subject, body string }{
		{"default", "[FWAlizer] 告警通知", "FWAlizer 检测到运行异常，请检查同步日志。"},
		{"maxChinese", strings.Repeat("中", 200), strings.Repeat("中", 3400)},
		{"maxEmoji", strings.Repeat("😀", 200), "正文😀"},
		{"maxASCII", strings.Repeat("A", 200), strings.Repeat("A", 10240)},
		{"trailingWS", "空白", "first  \t\n\t \nend \t"},
		{"dot", "点转义", ".\n..\n.中文\nnext"},
		{"crlf", "换行", "a\r\nb\nc\rd\r\n"},
		{"literal", "=?UTF-8?B?5Lit?= 字面量", "=?UTF-8?B?5Lit?= =\n中文"},
		{"crUnicode", "裸CR", "a\r中\nb\r😀\nc"},
		{"empty", "空正文", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, port, rec := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: true, traditional: true})
			cfg := EmailConfig{Host: host, Port: port, From: "from@example.com", To: "a@example.com, b@example.com"}
			subject, body := BuildTestEmailContent(tc.subject, tc.body, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
			if err := SendTestEmail(cfg, subject, body); err != nil {
				t.Fatalf("传统 SMTP 应接受: %v", err)
			}
			assertEmailEncoding(t, rec.Data(), subject, body)
		})
	}
}

// TestEmailMIMEExactBody 不追加业务详情，单独锁定末尾空白、空正文与换行有无。
func TestEmailMIMEExactBody(t *testing.T) {
	for _, body := range []string{"", "正文 \t", "正文\n", "正文\r\n\r\n", "正文\r"} {
		t.Run(body, func(t *testing.T) {
			host, port, rec := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: true, traditional: true})
			cfg := EmailConfig{Host: host, Port: port, From: "from@example.com", To: "a@example.com"}
			if err := SendTestEmail(cfg, "ASCII subject", body); err != nil {
				t.Fatal(err)
			}
			assertEmailEncoding(t, rec.Data(), "ASCII subject", body)
		})
	}
}

// TestEmailMIMEAutomatic 三种自动事件共享编码出口，后缀与固定详情不能改变。
func TestEmailMIMEAutomatic(t *testing.T) {
	cases := []struct {
		typ     EventType
		display string
	}{
		{EventDNSFailed, "DNS 解析失败"}, {EventSyncError, "同步失败"}, {EventOperationalUnhealthy, "运行健康异常"},
	}
	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			host, port, rec := startFakeSMTP(t, fakeSMTPOptions{rcptOK: true, dataOK: true, traditional: true})
			cfg := EmailConfig{Host: host, Port: port, From: "from@example.com", To: "a@example.com", Subject: strings.Repeat("😀", 200), Body: "自定义正文  \t\n"}
			errorText := strings.Repeat("失败", 1024)
			event := Event{Type: tc.typ, Timestamp: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), Data: map[string]any{"provider": "tc_cvm", "domain": "example.com", "error": errorText}}
			wantBody := cfg.Body + "\n\n事件类型：" + tc.display + "\n时间：2026-10-03 12:00:00\nProvider：tc_cvm\n域名：example.com\n错误：" + errorText
			if tc.typ == EventOperationalUnhealthy {
				event.Data["reasons"] = []string{"同步失败", "SQLite 检查失败"}
				wantBody += "\n原因：同步失败; SQLite 检查失败"
			}
			if err := NewEmailNotifier(cfg).OnEvent(event); err != nil {
				t.Fatalf("自动邮件失败: %v", err)
			}
			assertEmailEncoding(t, rec.Data(), cfg.Subject+" - "+tc.display, wantBody)
		})
	}
}
