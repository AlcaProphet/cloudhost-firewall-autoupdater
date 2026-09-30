package notifier

import (
	"crypto/tls"
	"fmt"
	"log/slog"
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

// EmailConfig SMTP 邮件配置。
//
// Subject / Body 是用户在告警页配置的纯文本主题与正文（Build7 §4.4）：
// 自动邮件会在主题后追加固定事件后缀、在正文后追加固定详情块。
type EmailConfig struct {
	Host    string
	Port    string
	User    string
	Pass    string
	From    string
	To      string
	Subject string
	Body    string
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

// eventSubjectSuffix 返回事件类型对应的固定主题后缀（Build7 §4.4）。
//
// 返回 false 表示该事件类型不产生邮件（订阅过滤之外的二次防御）。
func eventSubjectSuffix(t EventType) (string, bool) {
	switch t {
	case EventDNSFailed:
		return " - DNS 解析失败", true
	case EventSyncError:
		return " - 同步失败", true
	case EventOperationalUnhealthy:
		return " - 运行健康异常", true
	default:
		return "", false
	}
}

// eventDisplayName 返回事件类型的中文展示名（用于正文详情块）
func eventDisplayName(t EventType) string {
	switch t {
	case EventDNSFailed:
		return "DNS 解析失败"
	case EventSyncError:
		return "同步失败"
	case EventOperationalUnhealthy:
		return "运行健康异常"
	default:
		return string(t)
	}
}

// detailValue 读取事件数据中的字符串字段；缺失或非字符串时返回占位符 "-"。
func detailValue(event Event, key string) string {
	v, ok := event.Data[key]
	if !ok {
		return "-"
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "-"
	}
	return s
}

// reasonsLine 生成运行健康异常事件的可选固定「原因」行（Build7 Step 7）。
//
// 事件数据只保证 operational 事件带 reasons（稳定原因数组），因此这里只在存在非空
// 原因时输出一行；DNS/同步事件不输出该行，邮件正文保持既有逐字节形态。
// 兼容 []string 与 JSON 解码得到的 []any 两种形态，逐项跳过空值并按 "; " 连接。
func reasonsLine(event Event) string {
	var items []string
	switch v := event.Data["reasons"].(type) {
	case []string:
		items = v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				items = append(items, s)
			}
		}
	}
	nonEmpty := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			nonEmpty = append(nonEmpty, item)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	return "原因：" + strings.Join(nonEmpty, "; ")
}

// formatEventDetails 生成固定顺序的事件详情块（Build7 §4.4）：
// 事件类型 / 时间 / Provider / 域名 / 错误，缺失字段用 "-" 表示；
// 运行健康异常事件在末尾追加固定「原因」行（Build7 Step 7）。
//
// 邮件与 Webhook 共用本渲染器（Build7 Step 7），因此两个渠道的详情块完全同构，
// 且顺序稳定。刻意不遍历 map：map 迭代顺序随机，会让同一事件产生不同文本。
func formatEventDetails(event Event) string {
	// 逐段写入而不在 WriteString 内拼接："标签 + 值 + 换行"的拼接会为每一行产生
	// 一个临时字符串（writestring 分析器），分段写入的最终字节序列与拼接版本完全一致。
	var sb strings.Builder
	sb.WriteString("事件类型：")
	sb.WriteString(eventDisplayName(event.Type))
	sb.WriteString("\n")
	sb.WriteString("时间：")
	sb.WriteString(event.Timestamp.Format("2006-01-02 15:04:05"))
	sb.WriteString("\n")
	sb.WriteString("Provider：")
	sb.WriteString(detailValue(event, "provider"))
	sb.WriteString("\n")
	sb.WriteString("域名：")
	sb.WriteString(detailValue(event, "domain"))
	sb.WriteString("\n")
	sb.WriteString("错误：")
	sb.WriteString(detailValue(event, "error"))
	if line := reasonsLine(event); line != "" {
		sb.WriteString("\n")
		sb.WriteString(line)
	}
	return sb.String()
}

// OnEvent 实现 Subscriber 接口
func (n *EmailNotifier) OnEvent(event Event) error {
	// 仅处理已订阅的错误事件
	suffix, ok := eventSubjectSuffix(event.Type)
	if !ok {
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

	subject := n.cfg.Subject + suffix
	body := n.cfg.Body + "\n\n" + formatEventDetails(event)

	err := n.send(subject, body)
	if err == nil {
		// 成功日志只记录事件类型与收件人：不含密码与正文
		slog.Info("邮件告警已被 SMTP 服务器接受", "event", string(event.Type), "to", n.cfg.To)
	}
	return err
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
	// 多收件人逐项 Trim（Build7 §4.5）：页面示例 "a@x.com, b@y.com" 的第二个地址
	// 不得把前导空格传给 SMTP；空项直接跳过。
	for _, rcpt := range strings.Split(n.cfg.To, ",") {
		rcpt = strings.TrimSpace(rcpt)
		if rcpt == "" {
			continue
		}
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

// ─── Build7 Step 2：测试邮件（复用同一条有界 SMTP 会话实现） ───

const (
	// EmailTestSubjectSuffix 测试邮件主题的固定后缀（Build7 §5.1）
	EmailTestSubjectSuffix = " - 测试邮件"
	// EmailTestNotice 测试邮件正文的固定说明（Build7 §5.1）
	EmailTestNotice = "这是一次手动测试邮件"
	// emailTestTimeLayout 测试邮件正文的时间格式
	emailTestTimeLayout = "2006-01-02 15:04:05"
)

// BuildTestEmailContent 组装测试邮件的主题与正文（Build7 §5.1）：
// 主题追加固定后缀；正文在用户文本之后追加固定说明与当前时间。
//
// 成功口径只表示「SMTP 服务器已接受」，不表示已投递到收件箱。
func BuildTestEmailContent(subject, body string, now time.Time) (string, string) {
	var sb strings.Builder
	sb.WriteString(body)
	sb.WriteString("\n\n")
	sb.WriteString(EmailTestNotice)
	sb.WriteString("\n时间：")
	sb.WriteString(now.Format(emailTestTimeLayout))
	return subject + EmailTestSubjectSuffix, sb.String()
}

// SendTestEmail 使用与自动告警**完全相同**的 SMTP 会话实现发送一次测试邮件。
//
// 因此它复用 10 秒连接上限与 30 秒整会话 deadline（Build7 §5.1），并且：
//   - 不经过 EventBus、触发开关、每渠道在途限流与 ConfigCoordinator；
//   - 不读写 SQLite，也不发布任何运行时状态；
//   - 配置完全来自调用方传入的请求表单值。
func SendTestEmail(cfg EmailConfig, subject, body string) error {
	n := &EmailNotifier{cfg: cfg}
	return n.send(subject, body)
}
