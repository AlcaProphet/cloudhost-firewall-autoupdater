package health

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
)

// DefaultSupervisorInterval 内部监督器的固定检查周期（Build7 §7.3）。
//
// 监督器无法覆盖进程已退出的场景，那由外部 dead-man（Uptime Kuma）补足。
const DefaultSupervisorInterval = 30 * time.Second

// Publisher 是事件发布入口（生产为 *notifier.EventBus）。
type Publisher interface {
	Publish(event notifier.Event)
}

// SupervisorDeps 是监督器依赖。
type SupervisorDeps struct {
	// Checker 是唯一的健康计算源。
	Checker *Checker
	// Bus 是事件总线；为 nil 时只记录日志（最小接线/测试场景）。
	Bus Publisher
	// Interval 是检查周期（测试接缝）；<= 0 时使用 DefaultSupervisorInterval。
	Interval time.Duration
}

// Supervisor 是 30 秒内部运行健康监督器（Build7 §7.3）。
//
// 边沿语义：
//   - healthy → unhealthy：写一次 WARN；若第三触发开关开启则发布一次
//     `EventOperationalUnhealthy`；
//   - 持续 unhealthy：不重复发布，原因变化只更新日志；
//   - unhealthy → healthy：只写 INFO，不要求发送恢复邮件；
//   - 从「第三开关关闭」变为开启且当前已异常：下一次检查补发一次当前异常；
//   - shutdown：Run 退出后不再发布任何事件，不制造伪异常。
//
// 监督器始终计算与记录健康状态；第三开关只决定事件是否发布，
// 不影响 operational 端点与 Uptime Kuma Push。
type Supervisor struct {
	checker  *Checker
	bus      Publisher
	interval time.Duration

	wake chan struct{}
	stop chan struct{}
	done chan struct{}

	stopOnce sync.Once

	// 边沿状态（由 mu 保护）
	mu           sync.Mutex
	wasUnhealthy bool
	published    bool
	lastReasons  []string
}

// NewSupervisor 创建监督器。
func NewSupervisor(deps SupervisorDeps) *Supervisor {
	interval := deps.Interval
	if interval <= 0 {
		interval = DefaultSupervisorInterval
	}
	return &Supervisor{
		checker:  deps.Checker,
		bus:      deps.Bus,
		interval: interval,
		wake:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Run 启动监督循环（阻塞，直到 Stop）。
//
// 循环只等待 ticker、唤醒信号与停止信号，因此 Stop 的等待上限就是一次检查的
// 时长（SQLite 探活最多 2 秒），不会无限阻塞。
func (s *Supervisor) Run() {
	defer close(s.done)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.check()
	for {
		select {
		case <-ticker.C:
			s.check()
		case <-s.wake:
			s.check()
		case <-s.stop:
			return
		}
	}
}

// Wake 立即唤醒一次检查（配置保存后调用；可合并，不阻塞）。
func (s *Supervisor) Wake() {
	select {
	case s.wake <- struct{}{}:
	default: // 已有待处理唤醒，合并即可
	}
}

// Stop 停止监督并等待 Run 退出（幂等、有界）。
func (s *Supervisor) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
}

// Evaluate 暴露唯一健康计算源（HTTP 端点与 Push 循环复用同一实现）。
func (s *Supervisor) Evaluate(ctx context.Context) Result {
	return s.checker.Evaluate(ctx)
}

// TriggerEnabled 报告第三触发开关当前是否开启。
func (s *Supervisor) TriggerEnabled() bool {
	return s.checker.TriggerEnabled()
}

// check 执行一次健康检查并处理边沿语义。
func (s *Supervisor) check() {
	res := s.checker.Evaluate(context.Background())
	triggerEnabled := s.checker.TriggerEnabled()

	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case res.Healthy:
		if s.wasUnhealthy {
			slog.Info("运行健康已恢复", "checked_at", res.CheckedAt)
		}
		s.wasUnhealthy = false
		s.published = false
		s.lastReasons = nil
	default:
		if !s.wasUnhealthy {
			// 健康→异常边沿：只写一次 WARN
			slog.Warn("运行健康异常", "reasons", res.Reasons, "checked_at", res.CheckedAt)
			s.wasUnhealthy = true
			s.published = false
		} else if !slices.Equal(s.lastReasons, res.Reasons) {
			// 持续异常但原因变化：只更新日志，不重复发布
			slog.Info("运行健康异常原因变化", "reasons", res.Reasons, "checked_at", res.CheckedAt)
		}
		s.lastReasons = append([]string(nil), res.Reasons...)

		// 第三开关关闭时不发布事件；开启且本段异常尚未发布时发布一次
		// （因此「关闭→开启且当前已异常」会在下一次检查补发一次）。
		if triggerEnabled && !s.published {
			s.publish(res)
			s.published = true
		}
	}
}

// publish 发布一次运行健康异常事件（数据只含检查时间与稳定原因数组）。
func (s *Supervisor) publish(res Result) {
	if s.bus == nil {
		return
	}
	reasons := append([]string(nil), res.Reasons...)
	s.bus.Publish(notifier.Event{
		Type:      notifier.EventOperationalUnhealthy,
		Timestamp: res.CheckedAt,
		Data: map[string]any{
			"checked_at": res.CheckedAt,
			"reasons":    reasons,
		},
	})
}
