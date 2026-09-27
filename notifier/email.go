package notifier

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTP 单次发送的有界时限（Issue6 A2）默认值。
//
// 标准库 smtp.SendMail 内部用 net.Dial（无 timeout），且 greeting、STARTTLS、
// AUTH、MAIL、RCPT、DATA、QUIT 全链路没有任何 deadline：对静默或不响应的 SMTP
// 服务器会永久挂住一个 goroutine。这里改为显式建连 + 整连接 deadline。
const (
	smtpDefaultDialTimeout = 10 * time.Second // 建连上限
	smtpDefaultDeadline    = 30 * time.Second // 连接建立后整条会话的上限（覆盖 greeting 与 QUIT）
)

// 实际生效值。非导出变量仅为测试接缝（Issue6 §六.4 F8）：默认值就是上面的常量，
// 生产零行为变化；用例可缩短取值以断言 deadline 机制。
var (
	smtpDialTimeout = smtpDefaultDialTimeout
	smtpDeadline    = smtpDefaultDeadline
)

// EmailConfig SMTP 邮件配置
type EmailConfig struct {
	Host string
	Port string
	User string
	Pass string
	From string
	To   string
}

// EmailNotifier 邮件告警
type EmailNotifier struct {
	cfg EmailConfig
	// limiter 为渠道在途限流器（由 AlertManager 注入；nil 表示不限流，供既有测试构造）
	limiter *InFlightLimiter
}

// NewEmailNotifier 创建邮件通知器
func NewEmailNotifier(cfg EmailConfig) *EmailNotifier {
	return &EmailNotifier{cfg: cfg}
}

// SetInFlightLimiter 注入渠道在途限流器（实现 limitedNotifier）
func (n *EmailNotifier) SetInFlightLimiter(l *InFlightLimiter) { n.limiter = l }

// ChannelName 返回渠道名（用于安全日志）
func (n *EmailNotifier) ChannelName() string { return "email" }

// OnEvent 实现 Subscriber 接口
func (n *EmailNotifier) OnEvent(event Event) error {
	// 仅处理错误事件
	if event.Type != EventSyncError && event.Type != EventDNSFailed {
		return nil
	}

	// 在途上限：满载丢弃最新通知（不排队、不阻塞、不返回错误，避免被当作订阅者故障）
	if n.limiter != nil {
		release, ok := n.limiter.Acquire()
		if !ok {
			logDropped(n.ChannelName(), event, InFlightLimit)
			return nil
		}
		defer release()
	}

	subject := fmt.Sprintf("[FWAlizer] %s", event.Type)
	body := formatEventBody(event)

	return n.send(subject, body)
}

// send 用显式建连 + deadline 发送邮件。
//
// 刻意不调用 smtp.SendMail：它无法设置连接与整体 deadline。手写版本完整保留
// greeting(220)、EHLO、STARTTLS（服务端通告时）、AUTH（配置了用户名时）、
// MAIL、RCPT、DATA、QUIT 的既有顺序与语义，并由 SetDeadline 覆盖首尾。
func (n *EmailNotifier) send(subject, body string) error {
	addr := net.JoinHostPort(n.cfg.Host, n.cfg.Port)

	conn, err := net.DialTimeout("tcp", addr, smtpDialTimeout)
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	// 整条会话的硬上限：覆盖初始 greeting 与最后的 QUIT
	if err := conn.SetDeadline(time.Now().Add(smtpDeadline)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("设置 SMTP deadline 失败: %w", err)
	}

	c, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer func() {
		// Quit 已正常结束时再次 Close 是幂等的；这里保证异常路径也释放连接
		_ = c.Close()
	}()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: n.cfg.Host}); err != nil {
			return fmt.Errorf("STARTTLS 失败: %w", err)
		}
	}

	if n.cfg.User != "" {
		auth := smtp.PlainAuth("", n.cfg.User, n.cfg.Pass, n.cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}

	if err := c.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("SMTP MAIL FROM 失败: %w", err)
	}
	for _, rcpt := range strings.Split(n.cfg.To, ",") {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("SMTP RCPT TO 失败: %w", err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA 失败: %w", err)
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		n.cfg.From, n.cfg.To, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		_ = w.Close()
		return fmt.Errorf("写入邮件正文失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("结束 DATA 失败: %w", err)
	}

	if err := c.Quit(); err != nil {
		return fmt.Errorf("SMTP QUIT 失败: %w", err)
	}
	return nil
}

func formatEventBody(event Event) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("事件类型: %s\n", event.Type))
	sb.WriteString(fmt.Sprintf("时间: %s\n", event.Timestamp.Format("2006-01-02 15:04:05")))
	for k, v := range event.Data {
		sb.WriteString(fmt.Sprintf("%s: %v\n", k, v))
	}
	return sb.String()
}
