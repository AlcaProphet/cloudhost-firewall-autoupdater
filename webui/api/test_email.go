package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// ─── Build7 Step 2：POST /api/alerts/test-email（Build7 §5） ───
//
// 固定行为：
//   - 使用请求中的未保存表单值，绝不从 Store 或 RuntimeState 替换字段；
//   - 不写 SQLite、不进入 ConfigCoordinator、不 Apply 告警集合、不改变订阅；
//   - 不要求 email.enabled=true；
//   - 复用生产邮件的 SMTP 会话实现（10 秒连接上限 / 30 秒整会话 deadline）；
//   - 不经过自动告警的事件开关与每渠道在途订阅路径。

// testEmailRequest 测试邮件请求体：告警页当前邮件表单的全部发送字段。
//
// 每个字段都用指针做 presence 检查：缺失与显式 null 都必须拒绝（Build7 §5.1）。
type testEmailRequest struct {
	Host     *string `json:"host"`
	Port     *string `json:"port"`
	Security *string `json:"security"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	FromAddr *string `json:"from_addr"`
	ToAddr   *string `json:"to_addr"`
	Subject  *string `json:"subject"`
	Body     *string `json:"body"`
}

// testEmailResponse 测试邮件结果（Build7 §5.2）：
// success=true 只表示 SMTP 服务器已接受，不表示已投递到收件箱。
type testEmailResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (d *Deps) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	var req testEmailRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	if req.Host == nil || req.Port == nil || req.Security == nil || req.Username == nil || req.Password == nil ||
		req.FromAddr == nil || req.ToAddr == nil || req.Subject == nil || req.Body == nil {
		writeRequestError(w, badRequest("测试邮件的每个字段都必须出现"))
		return
	}

	// 按「实际要发送」校验 SMTP 字段：即使自动通知关闭也要求 host/from/to/subject 完整。
	// 复用与普通 API、配置导入完全相同的领域校验，不建立第二套更宽松的规则。
	cfg, err := config.NormalizeAlertEmail(config.AlertEmailConfig{
		Enabled:  true,
		Host:     *req.Host,
		Port:     *req.Port,
		Security: *req.Security,
		Username: *req.Username,
		Password: *req.Password,
		FromAddr: *req.FromAddr,
		ToAddr:   *req.ToAddr,
		Subject:  *req.Subject,
		Body:     *req.Body,
	})
	if err != nil {
		writeRequestError(w, err)
		return
	}

	subject, body := notifier.BuildTestEmailContent(cfg.Subject, cfg.Body, time.Now())
	err = notifier.SendTestEmail(notifier.EmailConfig{
		Host: cfg.Host, Port: cfg.Port, Security: cfg.Security, User: cfg.Username, Pass: cfg.Password,
		From: cfg.FromAddr, To: cfg.ToAddr,
	}, subject, body)
	if err != nil {
		// SendTestEmail 只返回固定阶段、SMTP 数字响应码或固定类别；不含服务器原文或底层错误链。
		slog.Warn("测试邮件发送失败", "error", err)
		writeJSON(w, http.StatusOK, testEmailResponse{Success: false, Error: err.Error()})
		return
	}

	slog.Info("测试邮件已被 SMTP 服务器接受", "to", cfg.ToAddr)
	writeJSON(w, http.StatusOK, testEmailResponse{
		Success: true,
		Message: "SMTP 服务器已接受测试邮件",
	})
}
