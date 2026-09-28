// Package health 提供 FWAlizer 唯一的运行健康计算源（Build7 §7.2）。
//
// 内部监督器、`/api/health/operational` HTTP 端点与 Uptime Kuma Push **必须**
// 共用本包的 `Checker.Evaluate`，禁止任何使用方各自实现一套判定。
//
// 能力边界（Build7 §7.1）：本包只能在进程仍可调度时给出 best-effort 结论；
// 进程已退出、runtime 完全卡死、宿主机断电或网络完全中断都不在覆盖范围内，
// 这些场景依赖外部 dead-man 检查（Uptime Kuma）。
package health

import (
	"context"
	"slices"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// 稳定原因常量：对外的 JSON 与日志只使用这些固定文本，
// 绝不拼接底层 SQL、路径、凭据或 URL。
const (
	// ReasonSQLite SQLite 探活失败（含 2 秒内未返回）
	ReasonSQLite = "SQLite 检查失败"
	// ReasonSyncerStopped 非 shutdown 阶段同步主循环未运行
	ReasonSyncerStopped = "同步引擎未运行"
	// ReasonRoundTimeout 当前轮次开始后超过 health_timeout 仍未完成
	ReasonRoundTimeout = "同步轮次超时"
	// ReasonLastRoundFailed 最近一轮结论为 failed
	ReasonLastRoundFailed = "最近一轮同步失败"
	// ReasonLastRoundPartial 最近一轮结论为 partial
	ReasonLastRoundPartial = "最近一轮部分完成"
	// ReasonScheduleStalled 同步开启但距最近完成时间超过 interval + health_timeout
	ReasonScheduleStalled = "同步调度停滞"
	// ReasonNoCompletedRound 同步开启但启动后超过 health_timeout 仍无完成轮次
	ReasonNoCompletedRound = "启动后尚无完成轮次"
)

// DefaultPingTimeout SQLite 探活的硬上限（Build7 §7.2 第 1 步）。
const DefaultPingTimeout = 2 * time.Second

// DefaultStartupGrace 是「同步主循环尚未进入运行态」的固定启动宽限（Build7 Step 7）。
//
// 进程启动后的这段窗口内，running=false 可能只是主循环尚未被调度（或尚未启动），
// 不视为异常；超过宽限仍未进入运行态，才按「同步引擎未运行」上报。
// 宽限只在「从未进入过运行态」时生效：已进入运行后停止会立即上报。
//
// 该值是内部固定常量，不新增用户可配置项（Build7 §7.2 只保留一个 health_timeout）。
const DefaultStartupGrace = 10 * time.Second

// Pinger 是有界 SQLite 探活入口（生产由 *config.Store 实现）。
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Deps 是 Checker 的依赖集合。所有函数字段都必须是快速、无阻塞的内存读取。
type Deps struct {
	// Pinger 执行 SQLite 探活；不得在内部持有 Syncer 或配置协调器锁。
	Pinger Pinger
	// Status 返回一份一致的同步状态快照。
	Status func() syncer.SyncStatus
	// Policy 返回当前告警策略（提供唯一 health_timeout）。
	Policy func() config.AlertPolicyConfig
	// Interval 返回当前同步间隔（调度停滞判定使用）。
	Interval func() time.Duration
	// Now 是时间接缝（测试用）；为 nil 时使用 time.Now。
	Now func() time.Time
	// PingTimeout 是探活上限（测试接缝）；<= 0 时使用 DefaultPingTimeout。
	PingTimeout time.Duration
	// StartupGrace 是启动宽限（测试接缝）；<= 0 时使用 DefaultStartupGrace。
	StartupGrace time.Duration
}

// Result 是一次运行健康判定的结果。
type Result struct {
	// Healthy 为 true 表示全部已启用检查都通过。
	Healthy bool
	// CheckedAt 是本次判定的时间（UTC 由调用方决定展示形式）。
	CheckedAt time.Time
	// Reasons 是稳定原因数组：已去重并按固定顺序排列，健康时为空。
	Reasons []string
}

// Checker 是唯一的运行健康计算源。
type Checker struct {
	deps Deps
}

// New 创建健康计算源。
func New(deps Deps) *Checker {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.PingTimeout <= 0 {
		deps.PingTimeout = DefaultPingTimeout
	}
	if deps.StartupGrace <= 0 {
		deps.StartupGrace = DefaultStartupGrace
	}
	return &Checker{deps: deps}
}

// Evaluate 按 Build7 §7.2 的固定顺序计算一次健康状态。
//
//  1. 最多 2 秒的 SQLite 探活，失败即 unhealthy；
//  2. 非 shutdown 阶段主循环未运行即 unhealthy；但进程启动后的固定启动宽限内、
//     且主循环从未进入过运行态时不算异常（Build7 Step 7）；
//  3. sync_enabled=false 时只保留以上两项，直接返回；
//  4. 当前轮次超过 health_timeout 未完成 → 轮次超时；
//  5. 最近一轮 failed / partial → 最近一轮失败 / 部分完成；
//  6. 同步开启且当前没有在途轮次时：有 last_sync 则判调度停滞，
//     否则用 ProcessStartedAt 判「启动后尚无完成轮次」；
//  7. idle 与空目标/空规则不产生任何原因；
//  8. 任一原因成立即 unhealthy，多原因全部返回且去重、顺序固定。
//
// SQLite 检查在状态快照之外有界执行，不持有 Syncer 或配置协调器锁。
func (c *Checker) Evaluate(ctx context.Context) Result {
	now := c.deps.Now()
	res := Result{CheckedAt: now, Reasons: []string{}}

	// 1) SQLite（有界、不持锁）
	pingCtx, cancel := context.WithTimeout(ctx, c.deps.PingTimeout)
	err := c.deps.Pinger.PingContext(pingCtx)
	cancel()
	if err != nil {
		res.Reasons = append(res.Reasons, ReasonSQLite)
	}

	// 一致快照：状态、策略与间隔各只读一次
	status := c.deps.Status()
	policy := c.deps.Policy()
	interval := c.deps.Interval()
	healthTimeout := policy.HealthTimeout
	if healthTimeout <= 0 {
		healthTimeout = config.DefaultHealthTimeout
	}

	// 2) 主循环（Build7 Step 7 三分支）：
	//   - 已进入运行后停止 → 立即异常，不受启动宽限影响；
	//   - 从未进入运行且启动宽限已过 → 按引擎未运行处理；
	//   - 从未进入运行但在启动宽限内 → 主循环可能只是尚未被调度，不制造伪异常。
	if !status.Running {
		switch {
		case status.StartedAt != nil:
			res.Reasons = append(res.Reasons, ReasonSyncerStopped)
		case now.Sub(status.ProcessStartedAt) > c.deps.StartupGrace:
			res.Reasons = append(res.Reasons, ReasonSyncerStopped)
		}
	}

	// 3) 暂停：跳过全部轮次结论、轮次超时与调度停滞检查
	if !status.Enabled {
		res.Reasons = slices.Compact(res.Reasons)
		res.Healthy = len(res.Reasons) == 0
		return res
	}

	// 4) 当前轮次超时
	if status.RoundStartedAt != nil && now.Sub(*status.RoundStartedAt) > healthTimeout {
		res.Reasons = append(res.Reasons, ReasonRoundTimeout)
	}

	// 5) 最近一轮结论（success/idle 覆盖，failed/partial 保持异常）
	if status.LastRound != nil {
		switch status.LastRound.Outcome {
		case syncer.RoundFailed:
			res.Reasons = append(res.Reasons, ReasonLastRoundFailed)
		case syncer.RoundPartial:
			res.Reasons = append(res.Reasons, ReasonLastRoundPartial)
		}
	}

	// 6) 调度停滞 / 启动后尚无完成轮次（仅在当前没有在途轮次时判定）
	if status.RoundStartedAt == nil {
		if status.LastSync != nil {
			if now.Sub(*status.LastSync) > interval+healthTimeout {
				res.Reasons = append(res.Reasons, ReasonScheduleStalled)
			}
		} else if now.Sub(status.ProcessStartedAt) > healthTimeout {
			res.Reasons = append(res.Reasons, ReasonNoCompletedRound)
		}
	}

	// 8) 去重（固定顺序由上面的追加顺序保证）
	res.Reasons = slices.Compact(res.Reasons)
	res.Healthy = len(res.Reasons) == 0
	return res
}

// TriggerEnabled 报告第三个触发条件（运行健康异常）当前是否开启。
//
// 监督器始终计算与记录健康状态；本开关只决定异常事件是否发布给
// 邮件/Webhook 订阅者，不影响 operational 端点与 Push。
func (c *Checker) TriggerEnabled() bool {
	if c.deps.Policy == nil {
		return false
	}
	return c.deps.Policy().OperationalErrorEnabled
}
