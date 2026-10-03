package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// ─── Build7 Step 5 / §7.6：Uptime Kuma Push 反向心跳 ───
//
// 固定行为：
//   - 默认关闭；启用或 URL 变化后立即发送第一条，不等待完整 interval；
//   - 每次发送当前 up/down（与 operational 端点共用同一健康计算源）；
//   - 解析用户填写的完整 URL，覆盖 status/msg/ping，保留 token 与未知 query；
//   - HTTP 上限 10 秒；同一时刻最多一条在途，新 tick 直接跳过（不排队、不重试）；
//   - 仅 HTTP 2xx 且响应 JSON 为 {"ok":true} 才算成功；
//   - 失败只写安全 WARN（不含 URL/token），不改变应用健康、不发布告警事件；
//   - shutdown 取消在途请求并退出循环。

// DefaultPushTimeout 单次 Push 请求的 HTTP 上限（Build7 §7.6）。
const DefaultPushTimeout = 10 * time.Second

// pushResponseLimit 限制业务响应大小，额外读一字节识别超限。
const pushResponseLimit = 16 * 1024

// maxDownMessageRunes down 状态短原因的最大字符数（Build7 §7.6）。
const maxDownMessageRunes = 250

// PushConfig 是 Push 循环的运行配置（由已发布的告警策略提供）。
type PushConfig struct {
	Enabled  bool
	URL      string
	Interval time.Duration
}

// PusherDeps 是 Pusher 依赖。
type PusherDeps struct {
	// Checker 是唯一的健康计算源（与 operational 端点、内部监督器共用）。
	Checker *Checker
	// Config 返回当前 Push 配置（每次发送前重新读取，实现热重载）。
	Config func() PushConfig
	// Bus 为保留字段：Push 失败刻意不发布任何事件（避免自激循环），
	// 该字段仅用于测试断言「没有事件被发布」。
	Bus Publisher
	// Timeout 是单次 HTTP 上限（测试接缝）；<= 0 时使用 DefaultPushTimeout。
	Timeout time.Duration
}

// Pusher 是 Uptime Kuma Push 心跳循环。
type Pusher struct {
	checker *Checker
	config  func() PushConfig
	bus     Publisher
	timeout time.Duration
	client  *http.Client

	wake chan struct{}
	stop chan struct{}
	done chan struct{}

	stopOnce sync.Once
	// baseCtx/cancel 在构造时创建：Stop 可能早于 Run 被调用，
	// 因此不能在 Run 内才赋值（否则与 Stop 构成数据竞态）。
	baseCtx context.Context
	cancel  context.CancelFunc

	// inFlight 保证同一时刻最多一条在途请求
	inFlight sync.Mutex
	sending  bool
}

// NewPusher 创建 Push 循环。
func NewPusher(deps PusherDeps) *Pusher {
	timeout := deps.Timeout
	if timeout <= 0 {
		timeout = DefaultPushTimeout
	}
	baseCtx, cancel := context.WithCancel(context.Background())
	return &Pusher{
		baseCtx: baseCtx,
		cancel:  cancel,
		checker: deps.Checker,
		config:  deps.Config,
		bus:     deps.Bus,
		timeout: timeout,
		// 独立 client：只设置整体超时，不使用重试
		client: &http.Client{Timeout: timeout},
		wake:   make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Run 启动 Push 循环（阻塞，直到 Stop）。
//
// 循环在每个周期开始前重新读取配置，因此 URL/interval 热重载立即生效；
// 关闭或 URL 为空时只等待唤醒；非空非法 URL 按有下限的间隔重新校验，不发网络请求。
func (p *Pusher) Run() {
	defer close(p.done)

	baseCtx := p.baseCtx
	active := false
	lastURL := ""
	for {
		cfg := p.config()

		if !cfg.Enabled || strings.TrimSpace(cfg.URL) == "" {
			active = false
			lastURL = ""
			select {
			case <-p.wake:
				continue
			case <-p.stop:
				return
			}
		}

		// 启用后的第一条、以及 URL 变化后的第一条都必须立即发送
		if !active || cfg.URL != lastURL {
			if p.sendOnce(baseCtx, cfg) {
				active = true
				lastURL = cfg.URL
			} else {
				// 配置非法时保持未激活；定时重读配置，避免只依赖下一次唤醒。
				active = false
				lastURL = ""
				timer := time.NewTimer(invalidURLRetryInterval(cfg.Interval))
				select {
				case <-timer.C:
					continue
				case <-p.wake:
					timer.Stop()
					continue
				case <-p.stop:
					timer.Stop()
					return
				}
			}
		}

		interval := cfg.Interval
		if interval <= 0 {
			interval = config.DefaultPushInterval
		}
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			p.sendOnce(baseCtx, cfg)
		case <-p.wake:
			timer.Stop()
		case <-p.stop:
			timer.Stop()
			return
		}
	}
}

// invalidURLRetryInterval 限制异常配置的校验频率；不改变正常发送路径的间隔。
func invalidURLRetryInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return config.DefaultPushInterval
	}
	if interval < config.MinPushInterval {
		return config.MinPushInterval
	}
	return interval
}

// Wake 立即唤醒一次配置重读（配置保存后调用；可合并、不阻塞）。
func (p *Pusher) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Stop 取消在途请求、停止循环并等待退出（幂等、有界）。
//
// 先取消 context 让在途 HTTP 立即返回，再等待 Run 退出，因此不必等满 10 秒。
func (p *Pusher) Stop() {
	p.stopOnce.Do(func() {
		p.cancel()
		close(p.stop)
	})
	<-p.done
}

// InFlight 报告当前是否有在途请求（观测/测试用）。
func (p *Pusher) InFlight() bool {
	p.inFlight.Lock()
	defer p.inFlight.Unlock()
	return p.sending
}

// sendOnce 计算一次健康状态并发送一条心跳。
//
// 返回 false 表示配置非法、未发送（调用方保持未激活状态）。
func (p *Pusher) sendOnce(baseCtx context.Context, cfg PushConfig) bool {
	target, err := buildPushURL(cfg.URL, "up", "OK", 0)
	if err != nil {
		// 只记录安全类别，不记录 URL 本身
		slog.Warn("Uptime Kuma Push 配置无效，跳过发送", "category", "invalid_url")
		return false
	}
	_ = target

	// 单在途：满载直接跳过本次 tick，不排队、不重试
	if !p.tryAcquire() {
		slog.Warn("Uptime Kuma Push 上一次请求仍在途，跳过本次心跳", "category", "in_flight")
		return true
	}
	defer p.release()

	ctx, cancel := context.WithTimeout(baseCtx, p.timeout)
	defer cancel()

	// 现场计算健康（与 operational 端点完全相同的判定），并测量耗时作为 ping
	start := time.Now()
	res := p.checker.Evaluate(ctx)
	pingMS := time.Since(start).Milliseconds()

	status := "up"
	msg := "OK"
	if !res.Healthy {
		status = "down"
		msg = buildDownMessage(res.Reasons)
	}

	target, err = buildPushURL(cfg.URL, status, msg, pingMS)
	if err != nil {
		slog.Warn("Uptime Kuma Push 配置无效，跳过发送", "category", "invalid_url")
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		slog.Warn("Uptime Kuma Push 请求构造失败", "category", "request")
		return true
	}
	resp, err := p.client.Do(req)
	if err != nil {
		slog.Warn("Uptime Kuma Push 失败", "category", pushErrorCategory(err))
		return true
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Uptime Kuma Push 响应体关闭失败", "category", "response_close")
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("Uptime Kuma Push 失败", "category", "http_status", "status", resp.StatusCode)
		return true
	}

	response, err := io.ReadAll(io.LimitReader(resp.Body, pushResponseLimit+1))
	if err != nil {
		slog.Warn("Uptime Kuma Push 失败", "category", "response_read")
		return true
	}
	if len(response) > pushResponseLimit {
		slog.Warn("Uptime Kuma Push 失败", "category", "response_too_large")
		return true
	}
	// 完整正文只允许一个 JSON 文档；未知字段保留兼容，缺失/null 不视为成功。
	var payload struct {
		OK *bool `json:"ok"`
	}
	if err := json.Unmarshal(response, &payload); err != nil {
		slog.Warn("Uptime Kuma Push 失败", "category", "invalid_json")
		return true
	}
	if payload.OK == nil || !*payload.OK {
		slog.Warn("Uptime Kuma Push 失败", "category", "not_ok")
		return true
	}

	// 成功只写 DEBUG，且不含 URL/token
	slog.Debug("Uptime Kuma Push 成功", "status", status)
	return true
}

// tryAcquire 尝试占用在途名额
func (p *Pusher) tryAcquire() bool {
	p.inFlight.Lock()
	defer p.inFlight.Unlock()
	if p.sending {
		return false
	}
	p.sending = true
	return true
}

// release 释放在途名额
func (p *Pusher) release() {
	p.inFlight.Lock()
	p.sending = false
	p.inFlight.Unlock()
}

// buildPushURL 在用户填写的完整 URL 上覆盖 status/msg/ping，保留 token 与未知 query。
func buildPushURL(raw, status, msg string, pingMS int64) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("push url scheme must be http/https")
	}
	if u.Host == "" {
		return "", errors.New("push url host must not be empty")
	}
	q := u.Query()
	q.Set("status", status)
	q.Set("msg", msg)
	q.Set("ping", strconv.FormatInt(pingMS, 10))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// buildDownMessage 用 "; " 连接稳定原因，并截断到 250 字符以内。
func buildDownMessage(reasons []string) string {
	msg := strings.Join(reasons, "; ")
	runes := []rune(msg)
	if len(runes) > maxDownMessageRunes {
		return string(runes[:maxDownMessageRunes])
	}
	return msg
}

// pushErrorCategory 把底层错误收敛为固定安全类别；绝不回显可能包含 URL 的文本。
func pushErrorCategory(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout"
		}
		return "network"
	}
	return "transport"
}
