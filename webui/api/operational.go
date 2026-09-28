package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/health"
)

// ─── Build7 Step 4：GET /api/health/operational（Build7 §7.4） ───
//
// 语义边界：
//   - `/api/health` 继续只表示 HTTP 服务可达（Docker HEALTHCHECK 使用它）；
//   - 本端点表达应用工作状态：健康 200、异常 503；
//   - 每次请求现场计算，不只返回监督器最多 30 秒前的缓存结果；
//   - 响应只含稳定原因，不含 SQL、路径、凭据、Webhook/Push URL。

// PushWaker 是 Uptime Kuma Push 心跳循环的唤醒入口（生产由 *health.Pusher 实现）。
type PushWaker interface {
	Wake()
}

// OperationalHealthSource 是运行健康来源（生产由 *health.Supervisor 实现，
// 它同时提供唯一计算源与「配置保存后唤醒」入口）。
type OperationalHealthSource interface {
	Evaluate(ctx context.Context) health.Result
	Wake()
}

// operationalResponse 是端点的固定响应形状（Build7 §7.4）
type operationalResponse struct {
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	Reasons   []string  `json:"reasons"`
}

// handleOperationalHealth 处理 GET /api/health/operational
func (d *Deps) handleOperationalHealth(w http.ResponseWriter, r *http.Request) {
	// 响应固定禁用缓存（外部监控必须看到实时状态）
	w.Header().Set("Cache-Control", "no-store")

	if d.Health == nil {
		// 未接线时不得声称健康：返回稳定的 503 原因
		writeJSON(w, http.StatusServiceUnavailable, operationalResponse{
			Status:    "unhealthy",
			CheckedAt: time.Now().UTC(),
			Reasons:   []string{"运行健康检查未接线"},
		})
		return
	}

	res := d.Health.Evaluate(r.Context())
	reasons := append([]string{}, res.Reasons...)
	status := http.StatusOK
	label := "ok"
	if !res.Healthy {
		status = http.StatusServiceUnavailable
		label = "unhealthy"
		// 内部日志可以记录已归类的详细错误，但 HTTP 只返回稳定原因
		slog.Debug("operational 健康检查为异常", "reasons", reasons)
	}

	writeJSON(w, status, operationalResponse{
		Status:    label,
		CheckedAt: res.CheckedAt.UTC(),
		Reasons:   reasons,
	})
}
