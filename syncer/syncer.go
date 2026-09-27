package syncer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// Syncer 同步引擎。
//
// 运行时配置（Config + Providers + Resolver + Breaker）全部来自 RuntimeManager
// 的一份不可变快照：每轮同步、每次 Dry Run 只在开始时取一次快照，全程不再
// 读取其他来源，因此不会出现「同一轮混用新 TAG、新 Provider、旧 Resolver」。
//
// 状态替换与调度控制是两个不同问题：
//   - ApplyState 原子替换状态并投递一条可合并的控制通知；
//   - Run goroutine 消费通知时重新读取最新状态，据此决定是否启动新一轮、
//     是否重置 ticker，因此最终状态不会因为 channel 满而永久丢失。
type Syncer struct {
	runtime *RuntimeManager
	bus     *notifier.EventBus

	triggerCh chan struct{} // 手动触发同步（容量 1，可合并）
	controlCh chan struct{} // 状态变化通知（容量 1，可合并；只表示「请重新读取状态」）
	stopCh    chan struct{}
	stopOnce  sync.Once
	doneCh    chan struct{}

	// 生命周期标记（由 mu 保护）
	stopped  bool // Stop 已请求：所有新轮次的硬门控
	runGuard bool // Run 已被调用过（拒绝一切第二次调用，含首个已退出后）

	// beforeRoundHook 仅用于测试构造确定性交错（默认 nil，生产零行为变化）。
	// 在「stop 门控通过之后、syncAll 之前」调用，与 SetStateAppliedHook 同风格。
	beforeRoundHook func()

	// 状态追踪（保护以下字段）
	mu          sync.RWMutex
	running     bool
	lastSync    time.Time
	lastSuccess *time.Time    // 最近一次「整轮成功」完成时间（内存态，重启归 null）
	lastRound   *RoundSummary // 最近一轮的整轮汇总（内存态，重启归 null）

	// applied 是「Run 已消费并已通知观察者」的状态指针（Issue6 A13）。
	//
	// RuntimeState 发布后不可修改，因此指针比较即可判定是否换了新状态；
	// 修复前每次 select 返回都无条件触发 hook，使 ticker/trigger 的普通轮次
	// 也反复输出「告警已更新」。
	applied *RuntimeState
	// enabled 是「已提交开关」的调度镜像：唯一真值在 SQLite 与已发布的运行时状态。
	//
	// 由 ApplyState 在发布新状态时同步推进，仅用于运行时状态尚未发布（启动极早期）
	// 的 IsEnabled 回退。Run 的过渡判定使用自身已经处理过的本地相位（见 Run），
	// 不再使用本镜像：否则镜像先于循环相位推进时会把 false→true 误判为 true→true，
	// 漏掉恢复后的立即一轮（Issue6 A7）。
	enabled bool

	dryRunMu sync.Mutex // Dry Run 防重入

	// onStateApplied 在状态发布完成后调用（run.go 注入告警订阅的无失败替换与安全日志）。
	// 必须是零 error、不访问网络的内存操作。
	onStateApplied func(*RuntimeState)
}

// New 创建同步引擎。
//
// 初始运行时状态必须已经通过 RuntimeManager 发布（启动路径由 run.go 构造），
// 因此 New 不再接收分散的 cfg/providers/resolver 参数。
func New(runtime *RuntimeManager) *Syncer {
	s := &Syncer{
		runtime:   runtime,
		bus:       notifier.NewEventBus(),
		triggerCh: make(chan struct{}, 1),
		controlCh: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
	if st := runtime.Snapshot(); st != nil {
		s.enabled = st.Config.SyncEnabled
		s.applied = st
	}
	return s
}

// EventBus 返回事件总线（供外部订阅）
func (s *Syncer) EventBus() *notifier.EventBus {
	return s.bus
}

// Runtime 返回运行时状态管理器（协调器与只读操作据此取得完整快照）
func (s *Syncer) Runtime() *RuntimeManager {
	return s.runtime
}

// ApplyState 原子替换完整运行时状态，并通知 Run goroutine 重新读取最新状态。
//
// 这是无失败操作：调用前候选状态必须已构造成功（Build6 §12.4）。通知可合并，
// 但不会丢失最终状态——Run 消费时总是重新读取 RuntimeManager 的快照。
func (s *Syncer) ApplyState(next *RuntimeState) {
	s.runtime.Apply(next)

	// 调度镜像在此同步推进：IsEnabled 读的是已发布真值，镜像只作为
	// 「运行时状态尚未发布」时的回退（见 enabled 字段注释与 Issue6 A7）。
	s.setEnabledMirror(next.Config.SyncEnabled)

	select {
	case s.controlCh <- struct{}{}:
	default: // 已有待处理通知，最终状态在快照里，合并即可
	}
}

// SetStateAppliedHook 注入「新状态已被 Run 消费」后的回调，用于重新读取生效中的
// 告警集合并输出安全日志。回调必须是无失败的内存操作，且不得访问网络。
//
// 注意：回调由 Run 在消费控制消息并完成调度决策后触发，因此它同时可作为
// 「状态已真正生效」的同步屏障（而不仅是「已被发布」）。
func (s *Syncer) SetStateAppliedHook(fn func(*RuntimeState)) {
	s.mu.Lock()
	s.onStateApplied = fn
	s.mu.Unlock()
}

// Run 启动同步主循环（阻塞，直到收到停止信号）。
//
// 重复调用契约（Issue6 A16，2026-09-27 用户裁决）：用锁保护的 runGuard 拒绝**一切**
// 第二次调用（含首个 Run 已退出之后），WARN 后立即返回，绝不 panic。
// 刻意不使用 sync.Once.Do——那会让第二个调用等待首个 Run 结束，语义不同。
func (s *Syncer) Run() {
	s.mu.Lock()
	if s.runGuard {
		s.mu.Unlock()
		slog.Warn("同步引擎已在运行，忽略重复的 Run 调用")
		// 不关闭 doneCh：doneCh 归首个 Run 所有，关闭两次会 panic
		return
	}
	s.runGuard = true
	s.mu.Unlock()

	defer close(s.doneCh)
	s.setRunning(true)
	defer s.setRunning(false)

	initial := s.runtime.Snapshot()
	if initial == nil {
		// 尚无状态：等待第一条控制通知，避免 nil 解引用
		select {
		case <-s.controlCh:
		case <-s.stopCh:
			slog.Info("同步引擎停止")
			return
		}
	}

	state := s.runtime.Snapshot()
	ticker := time.NewTicker(state.Config.Interval)
	defer ticker.Stop()

	s.setEnabledMirror(state.Config.SyncEnabled)
	enabled := state.Config.SyncEnabled
	if enabled {
		// Run 入口的启动轮也是「新轮次」，必须走同一 stop 门控（Issue6 A20）：
		// s.Stop() 先于 go s.Run()（生产对应“启动即收到 SIGTERM”）在修复前会 100%
		// 确定性跑完整一轮云写入。
		if !s.beginRound() {
			slog.Info("同步引擎已停止，跳过启动轮")
			return
		}
		s.syncAll()
	} else {
		slog.Info("同步已暂停（SyncEnabled=false），等待开启")
	}

	for {
		// 过渡前值取「本循环已经处理过的相位」，而不是已发布镜像（Issue6 A7）：
		// ApplyState 在发布时就会同步推进镜像，若镜像先于本循环推进，
		// 用镜像判定会把 false→true 误判为 true→true，从而漏掉恢复后的立即一轮。
		wasEnabled := enabled

		if !enabled {
			// 暂停子循环：不接收 ticker/trigger，只在状态通知或停止时退出。
			// 暂停期间排队的 trigger 不会在这里被消费，由恢复时的清空统一处理。
			select {
			case <-s.controlCh:
			case <-s.stopCh:
				slog.Info("同步引擎停止")
				return
			}
		} else {
			select {
			case <-ticker.C:
				// 暂停可能已在当前轮次进行中提交、但控制通知尚未被消费；
				// 定时触发必须与手动触发使用同一门控（AGENTS §五「暂停时 ticker
				// 与手动 trigger 均不触发同步」）。此处不直接 syncAll，而是跳出
				// select 进入下方统一的状态重读与过渡判定，避免启动一轮已暂停的同步。
				if !s.IsEnabled() {
					slog.Debug("丢弃已暂停状态下的定时同步")
					break
				}
				if !s.beginRound() {
					slog.Debug("丢弃已停止状态下的定时同步")
					break
				}
				s.syncAll()
			case <-s.triggerCh:
				// 排队中的 trigger 在消费前重新检查开关（Build6 §12.5）：
				// 以**已提交状态**为准（而不是本循环的相位镜像），因为暂停可能在
				// 当前同步轮次进行中就已生效；此时排队中的触发属过期触发，必须丢弃，
				// 否则当前轮结束后会再启动一轮已暂停的同步。
				if !s.IsEnabled() {
					slog.Debug("丢弃已暂停状态下的同步触发")
					continue
				}
				if !s.beginRound() {
					slog.Debug("丢弃已停止状态下的同步触发")
					continue
				}
				slog.Info("手动触发同步")
				s.syncAll()
			case <-s.controlCh:
			case <-s.stopCh:
				slog.Info("同步引擎停止")
				return
			}
		}

		// 每轮循环结束都重新读取最新状态（控制通知消费后必须重新读取）
		latest := s.runtime.Snapshot()
		if latest == nil {
			continue
		}
		// 控制消息消费之后，循环标志推进到已发布真值
		// （调度镜像已由 ApplyState 在发布时同步推进，此处不重复写）
		enabled = latest.Config.SyncEnabled

		switch {
		case !wasEnabled && enabled:
			// false → true：更新 interval 并立即触发一轮（与 Resume 一致），
			// 且先清空暂停期间排队的过期 trigger —— 本轮的立即同步已代表最新配置，
			// 陈旧 trigger 再补一轮会造成「恢复多跑一轮」（Build6 §12.5）。
			slog.Info("同步已开启")
			ticker.Reset(latest.Config.Interval)
			s.drainTrigger()
			// false→true 的恢复轮同样是「新轮次」，必须走同一 stop 门控（Issue6 A20）：
			// pause/resume 与 SIGTERM 并发时，暂停子循环的 select 可能选中 control
			// 通知，修复前会在这里再启动一整轮云写入。
			if !s.beginRound() {
				slog.Info("同步引擎已停止，跳过恢复轮")
				return
			}
			s.syncAll()
		case wasEnabled && enabled:
			// true → true：只按新 interval 重置 ticker，不额外立即同步
			ticker.Reset(latest.Config.Interval)
		case wasEnabled && !enabled:
			// true → false：当前轮已完成，停止 ticker 并进入暂停等待
			slog.Info("同步已暂停")
			ticker.Stop()
		default:
			// false → false：暂停期间仍按最新 interval 准备，恢复后立即生效
			ticker.Reset(latest.Config.Interval)
		}

		// 调度决策完成后通知观察者：此时新状态的开关/interval 已真正生效。
		// 只在 Run **真正消费了一份新状态**（指针变化）时触发（Issue6 A13）。
		s.notifyStateApplied(latest)
	}
}

// Pause 暂停同步（非阻塞，幂等）。
//
// sync_enabled 的持久化真值在 SQLite，由协调器写入并发布新状态；本方法只把
// 运行时状态里的同步开关置为 false，使 dispatch 立即停止启动新一轮。
func (s *Syncer) Pause() {
	s.setSyncEnabled(false)
}

// Resume 恢复同步（非阻塞，幂等）：与 Resume 语义一致，false → true 立即触发一轮。
func (s *Syncer) Resume() {
	s.setSyncEnabled(true)
}

// setSyncEnabled 在运行时状态的一次指针替换内翻转同步开关。
//
// 运行时快照始终是调度与业务逻辑的唯一真相来源，因此控制通知消费后读到的是
// 最新状态；通知可合并，但最终状态不会因 channel 满而丢失。
func (s *Syncer) setSyncEnabled(enabled bool) {
	current := s.runtime.Snapshot()
	if current == nil {
		return
	}
	if current.Config.SyncEnabled == enabled {
		return
	}
	next := *current
	next.Config = current.Config.DeepCopy()
	next.Config.SyncEnabled = enabled
	s.ApplyState(&next)
}

// notifyStateApplied 只在**首次见到该状态指针**时触发一次「状态已应用」回调。
//
// RuntimeState 发布后不可修改，因此指针比较足以判定「是否换了新状态」，
// 无需额外 generation（Issue6 A13 的最终方案）。
func (s *Syncer) notifyStateApplied(state *RuntimeState) {
	s.mu.Lock()
	if s.applied == state {
		s.mu.Unlock()
		return
	}
	s.applied = state
	s.mu.Unlock()
	s.logAppliedState(state)
}

// logAppliedState 在状态发布后触发回调（无回调时静默）。
func (s *Syncer) logAppliedState(state *RuntimeState) {
	s.mu.RLock()
	fn := s.onStateApplied
	s.mu.RUnlock()
	if fn != nil {
		fn(state)
	}
}

// IsEnabled 返回当前开关状态。
//
// 真值取自已发布的运行时状态（SQLite 提交后即生效），因此 pause/resume 之后
// 的 409 判定不会因为 Run goroutine 尚未消费控制消息而短暂出错。
func (s *Syncer) IsEnabled() bool {
	if state := s.runtime.Snapshot(); state != nil {
		return state.Config.SyncEnabled
	}
	return s.isEnabledMirror()
}

// isEnabledMirror 读取开关镜像，仅在运行时状态尚未发布时作为 IsEnabled 的回退。
//
// 过渡判定与 trigger 门控都不再使用本镜像：前者用 Run 的本地相位（Issue6 A7），
// 后者用已发布状态。因此镜像不在 Run 消费控制消息后才推进，而是在 ApplyState
// 发布时推进（它只表示「已提交」）。
func (s *Syncer) isEnabledMirror() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// drainTrigger 清空暂停期间排队的过期同步触发（非阻塞）。
//
// 唯一调用点是 false → true 过渡：此时本轮立即同步已代表最新配置，陈旧
// trigger 必须丢弃，否则恢复会额外多跑一轮（Build6 §12.5）。
func (s *Syncer) drainTrigger() {
	for {
		select {
		case <-s.triggerCh:
		default:
			return
		}
	}
}

// setEnabledMirror 推进已生效的开关镜像。
func (s *Syncer) setEnabledMirror(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

// Stop 优雅停止（幂等：重复调用安全，不 panic）。
//
// 语义（Issue6 A20）：Stop 只阻止**新轮次**，绝不取消或中断已经开始的 syncAll；
// 当前轮完成后 Run 才退出。因此这里只置位 stopped 标记并关闭 stopCh，
// 不改变 IsEnabled()（开关真值仍由 SQLite 与已发布运行时状态决定）。
func (s *Syncer) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// isStopped 报告是否已请求停止。抽取为可单测的门控谓词（Issue6 A20 测试机制）。
func (s *Syncer) isStopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopped
}

// beginRound 是「启动一个新同步轮次」的统一硬门控（Issue6 A20）。
//
// 四处 syncAll 调用点（Run 入口的启动轮、ticker、trigger、false→true 恢复轮）
// 必须先通过本门控；返回 false 表示已请求停止，调用方不得启动新轮次。
// stop 门控与 A6 的 enabled 门控**并列存在、不合并**：前者回答「是否还要跑」，
// 后者回答「本轮是否被允许跑」。
//
// 刻意不检查 IsEnabled()，因此本门控不会外溢到 Dry Run / 连接测试
// （AGENTS §五 要求二者独立于 Run() 主循环）。
func (s *Syncer) beginRound() bool {
	if s.isStopped() {
		return false
	}
	// 测试专用 hook：位于门控通过之后、syncAll 之前，可构造
	// 「门控已判定 → 此刻 Stop() → 下一处门控必须拦截」的确定性交错。
	s.mu.RLock()
	hook := s.beforeRoundHook
	s.mu.RUnlock()
	if hook != nil {
		hook()
	}
	return true
}

// SetBeforeRoundHook 注入「stop 门控通过、轮次开始之前」的测试钩子。
//
// 仅用于测试构造确定性交错；默认 nil，生产零行为变化（与 SetStateAppliedHook 同风格）。
func (s *Syncer) SetBeforeRoundHook(fn func()) {
	s.mu.Lock()
	s.beforeRoundHook = fn
	s.mu.Unlock()
}

// Wait 等待 Syncer 完全退出（Stop 后调用；可多次安全等待）
func (s *Syncer) Wait() { <-s.doneCh }

// TriggerSync 手动触发一次同步（非阻塞；暂停期间的排队触发在消费前会被丢弃）
func (s *Syncer) TriggerSync() {
	select {
	case s.triggerCh <- struct{}{}:
	default: // 已有待处理的触发，跳过
	}
}

// SyncStatus 同步状态
// Enabled: 开关状态（true=开启，false=暂停）；Running 保持"引擎存活"语义
// （Run() 存活于暂停子循环时 running 仍为 true，前端三态判断以 Enabled 为准）
//
// LastSuccess / LastRound 是对既有三字段的**向后兼容追加**（Issue6 A3/A18）：
// 既有字段的类型与语义完全不变，仅新增同步健康表达。
// 两者都是内存态，进程重启后归 null——前端文案不得表述为「从未成功」。
type SyncStatus struct {
	Running     bool          `json:"running"`
	Enabled     bool          `json:"enabled"`
	LastSync    *time.Time    `json:"last_sync"`
	LastSuccess *time.Time    `json:"last_success"`
	LastRound   *RoundSummary `json:"last_round"`
}

// Status 返回当前同步状态
func (s *Syncer) Status() SyncStatus {
	enabled := s.IsEnabled()
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := SyncStatus{Running: s.running, Enabled: enabled}
	if !s.lastSync.IsZero() {
		t := s.lastSync
		status.LastSync = &t
	}
	if s.lastSuccess != nil {
		t := *s.lastSuccess
		status.LastSuccess = &t
	}
	if s.lastRound != nil {
		r := *s.lastRound
		status.LastRound = &r
	}
	return status
}

func (s *Syncer) setRunning(v bool) {
	s.mu.Lock()
	s.running = v
	s.mu.Unlock()
}

// ErrDryRunInProgress 防重入冲突错误（多个 Dry Run 并发执行时返回）
var ErrDryRunInProgress = errors.New("Dry Run 正在执行中")

// DryRunResponse 试运行响应（包装对象：空状态语义化）
type DryRunResponse struct {
	Results  []DryRunResult `json:"results"`
	Warnings []string       `json:"warnings"`
}

// DryRunResult 试运行结果（明细化：to_add/to_delete 为规则数组）
type DryRunResult struct {
	Provider string                `json:"provider"`
	Domain   string                `json:"domain"`
	ToAdd    []provider.RuleChange `json:"to_add"`
	ToDelete []provider.RuleChange `json:"to_delete"`
	Error    string                `json:"error,omitempty"`
	// Skipped 无法实施的期望规则（如 SWAS DROP）：只追加字段，
	// to_add/to_delete 的既有名称与结构不变（Issue6 A11 / AGENTS §十一）
	Skipped []provider.RuleChange `json:"skipped,omitempty"`
}

// DryRun 试运行：DNS 解析 + Diff，不写入不触发事件。
//
// 与同步一样只在开始时取**一次**完整运行时快照，全程使用该快照（不受并发状态替换影响）；
// 不受同步暂停开关限制。
func (s *Syncer) DryRun() (DryRunResponse, error) {
	if !s.dryRunMu.TryLock() {
		return DryRunResponse{}, ErrDryRunInProgress
	}
	defer s.dryRunMu.Unlock()

	state := s.runtime.Snapshot()
	resp := DryRunResponse{Results: []DryRunResult{}}
	if state == nil {
		resp.Warnings = append(resp.Warnings, "运行时状态尚未就绪")
		return resp, nil
	}

	if len(state.Providers) == 0 {
		resp.Warnings = append(resp.Warnings, "暂无云资源目标，请先在云资源管理页配置")
	}
	if len(state.Config.DomainRules) == 0 {
		resp.Warnings = append(resp.Warnings, "暂无域名规则，请先在域名规则页配置")
	}
	for _, p := range state.Providers {
		rules := filterRulesForTarget(state.Config.DomainRules, p.TargetIndex())
		for _, rule := range rules {
			result := DryRunResult{Provider: p.Name(), Domain: rule.Host}
			resolved, err := state.Resolver.Resolve(context.Background(), rule.Host)
			if err != nil {
				result.Error = err.Error()
				resp.Results = append(resp.Results, result)
				continue
			}
			if !rule.EnableIPv6 {
				resolved = filterIPv4(resolved)
			}
			allRules, err := p.GetRules()
			if err != nil {
				result.Error = err.Error()
				resp.Results = append(resp.Results, result)
				continue
			}
			owned := provider.OwnedRules(allRules, state.Config.Tag)
			desc := truncateDesc(tag.Format(state.Config.Tag, rule.Comment), p.CloudType())
			diff := provider.Diff(resolved, rule, desc, owned, p)
			for _, a := range diff.ToAdd {
				result.ToAdd = append(result.ToAdd, provider.RuleChangeFromAction(a))
			}
			for _, r := range diff.ToDelete {
				result.ToDelete = append(result.ToDelete, provider.RuleChangeFromInfo(r))
			}
			// 云端能力限制导致无法实施的期望规则：如实列为 skipped，不再伪装成 to_add
			for _, sk := range diff.Skipped {
				change := provider.RuleChangeFromAction(sk.Action)
				change.SkipReason = sk.Reason
				result.Skipped = append(result.Skipped, change)
			}
			resp.Results = append(resp.Results, result)
			time.Sleep(rateLimitInterval(p.CloudType())) // 限速：与 syncAll 一致（AGENTS.md §七）
		}
	}
	return resp, nil
}

// syncAll 执行一轮完整同步。
//
// 本轮开始时只取一次运行时快照：TAG、规则、Provider、Resolver 与熔断器全部
// 来自该快照，下游函数一律显式接收参数，不再回读运行时状态。
// RoundOutcome 一轮同步的整体结论（Issue6 A18，口径按 §六.4 F4）。
type RoundOutcome string

const (
	// RoundSuccess 仅当 total>0 且 failed==0 且 skipped==0
	RoundSuccess RoundOutcome = "success"
	// RoundFailed 存在失败单元
	RoundFailed RoundOutcome = "failed"
	// RoundPartial 无失败但有跳过单元
	RoundPartial RoundOutcome = "partial"
	// RoundIdle 没有 Provider 或没有适用规则（空调度，不制造统计噪声）
	RoundIdle RoundOutcome = "idle"
)

// RoundSummary 一轮同步的整轮汇总（Issue6 A18）。
//
// 统计单元口径：**一个 Provider × 一条适用规则**，
// total = Σ_p len(filterRulesForTarget(rules, p.TargetIndex()))。
//
// 不变量：total == ok + changed + failed + skipped
//
//   - ok      成功且**无变更**的单元；
//   - changed 成功且**发生了增删**的单元（added/deleted 任一非零）；
//   - failed  DNS 解析失败或云调用最终失败的单元；
//   - skipped Provider 明确未实施操作的单元（Issue6 A11 的 SWAS DROP 等）。
//
// 只有 total>0 且 failed==0 且 skipped==0 才是 success 并刷新 last_success。
type RoundSummary struct {
	FinishedAt time.Time    `json:"finished_at"`
	Total      int          `json:"total"`
	OK         int          `json:"ok"`
	Changed    int          `json:"changed"`
	Failed     int          `json:"failed"`
	Skipped    int          `json:"skipped"`
	Added      int          `json:"added"`
	Deleted    int          `json:"deleted"`
	DurationMS int64        `json:"duration_ms"`
	Outcome    RoundOutcome `json:"outcome"`
}

// Data 返回事件负载形态的汇总（EventSyncComplete.Data 增加同一汇总）。
func (r RoundSummary) Data() map[string]any {
	return map[string]any{
		"finished_at": r.FinishedAt,
		"total":       r.Total,
		"ok":          r.OK,
		"changed":     r.Changed,
		"failed":      r.Failed,
		"skipped":     r.Skipped,
		"added":       r.Added,
		"deleted":     r.Deleted,
		"duration_ms": r.DurationMS,
		"outcome":     string(r.Outcome),
	}
}

// unitOutcome 单个统计单元的结论。
type unitOutcome int

const (
	unitOK unitOutcome = iota
	unitChanged
	unitFailed
	unitSkipped
)

func (s *Syncer) syncAll() {
	state := s.runtime.Snapshot()
	if state == nil {
		return
	}

	// 先算清本轮统计单元总数（一个 Provider × 一条适用规则）
	total := 0
	for _, p := range state.Providers {
		total += len(filterRulesForTarget(state.Config.DomainRules, p.TargetIndex()))
	}

	slog.Info("开始同步", "targets", len(state.Providers), "rules", len(state.Config.DomainRules), "units", total)
	start := time.Now()

	// 发布 sync:start 事件
	s.bus.Publish(notifier.Event{
		Type:      notifier.EventSyncStart,
		Timestamp: time.Now(),
		Data:      map[string]any{"targets": len(state.Providers), "rules": len(state.Config.DomainRules)},
	})

	summary := s.runRound(state, total)
	summary.FinishedAt = time.Now()
	summary.DurationMS = time.Since(start).Milliseconds()
	summary.Outcome = outcomeOf(summary)

	s.mu.Lock()
	s.lastSync = summary.FinishedAt
	if summary.Outcome == RoundSuccess {
		success := summary.FinishedAt
		s.lastSuccess = &success
	}
	s.lastRound = &summary
	s.mu.Unlock()

	// 发布 sync:complete 事件：保留既有 duration 字符串并增加同一份整轮汇总
	data := summary.Data()
	data["duration"] = time.Since(start).String()
	s.bus.Publish(notifier.Event{
		Type:      notifier.EventSyncComplete,
		Timestamp: summary.FinishedAt,
		Data:      data,
	})

	slog.Info("同步完成",
		"耗时", time.Since(start).Round(time.Millisecond),
		"outcome", string(summary.Outcome),
		"total", summary.Total, "ok", summary.OK, "changed", summary.Changed,
		"failed", summary.Failed, "skipped", summary.Skipped,
		"added", summary.Added, "deleted", summary.Deleted)
}

// outcomeOf 依据 F4 裁决计算整轮结论。
func outcomeOf(s RoundSummary) RoundOutcome {
	switch {
	case s.Total == 0:
		return RoundIdle
	case s.Failed > 0:
		return RoundFailed
	case s.Skipped > 0:
		return RoundPartial
	default:
		return RoundSuccess
	}
}

// runRound 执行一轮完整同步并按单元归类结果。
//
// 本轮开始时只取一次运行时快照：TAG、规则、Provider、Resolver 与熔断器全部
// 来自该快照，下游函数一律显式接收参数，不再回读运行时状态。
func (s *Syncer) runRound(state *RuntimeState, total int) RoundSummary {
	counts := make([]atomic.Int32, 4) // 下标 = unitOutcome
	var added, deleted, skipped atomic.Int32

	// 按云厂商分组，跨云并行
	groups := s.groupByCloud(state.Providers)
	var wg sync.WaitGroup
	for ct, ps := range groups {
		wg.Add(1)
		go func(ct config.CloudType, ps []provider.Provider) {
			defer wg.Done()
			for _, p := range ps {
				rules := filterRulesForTarget(state.Config.DomainRules, p.TargetIndex())
				for _, rule := range rules {
					w := &unitResult{}
					s.syncDomain(state, p, rule, w)
					counts[w.outcome()].Add(1)
					added.Add(int32(w.added))
					deleted.Add(int32(w.deleted))
					skipped.Add(int32(w.skipped))
				}
				// 同一云厂商内串行处理（共享配额）；按 Provider 限速一次
				time.Sleep(rateLimitInterval(ct))
			}
		}(ct, ps)
	}
	wg.Wait()

	return RoundSummary{
		Total:   total,
		OK:      int(counts[unitOK].Load()),
		Changed: int(counts[unitChanged].Load()),
		Failed:  int(counts[unitFailed].Load()),
		Skipped: int(counts[unitSkipped].Load()),
		Added:   int(added.Load()),
		Deleted: int(deleted.Load()),
	}
}

// unitResult 单个统计单元的结果（一个 goroutine 独占，无需加锁）。
type unitResult struct {
	failed  bool
	added   int
	deleted int
	skipped int
}

// outcome 按 F4 口径归类：失败优先，其次「明确跳过」，最后按是否发生增删区分。
func (w *unitResult) outcome() unitOutcome {
	switch {
	case w.failed:
		return unitFailed
	case w.skipped > 0:
		return unitSkipped
	case w.added > 0 || w.deleted > 0:
		return unitChanged
	default:
		return unitOK
	}
}

// syncDomain 同步单个域名到单个 Provider。
//
// state 是本轮开始的完整快照：TAG、Resolver 与熔断器都只从它读取，
// 因此热重载产生的下一份状态不会影响正在执行的本轮；
// w 收集本单元的结果（失败/增删/跳过），不再由 syncDomainInternal 直接返回。
func (s *Syncer) syncDomain(state *RuntimeState, p provider.Provider, rule config.DomainRule, w *unitResult) {
	// 0. DNS 解析（无论是否熔断都执行，熔断时作为半开探测）
	resolved, err := state.Resolver.Resolve(context.Background(), rule.Host)
	if err != nil {
		w.failed = true
		if state.Breaker.IsOpen(rule.Host) {
			// 半开探测失败：维持熔断（不调用 RecordFailure，熔断中已停止计数）
			slog.Debug("域名半开探测失败，维持熔断", "domain", rule.Host, "error", err)
		} else {
			state.Breaker.RecordFailure(rule.Host)
			slog.Warn("DNS 解析失败，保留现有规则", "domain", rule.Host, "error", err)
		}
		s.bus.Publish(notifier.Event{
			Type:      notifier.EventDNSFailed,
			Timestamp: time.Now(),
			Data:      map[string]any{"domain": rule.Host, "error": err.Error()},
		})
		return
	}

	// 解析成功：解除熔断（RecordSuccess 内部处理计数并输出解除日志）
	state.Breaker.RecordSuccess(rule.Host)

	// 1. 按规则配置过滤 IPv6 地址
	if !rule.EnableIPv6 {
		resolved = filterIPv4(resolved)
	}

	// 2. 委托给内部方法执行同步
	s.syncDomainInternal(p, rule, resolved, state.Config.Tag, w)
}

// syncDomainInternal 执行 DNS 已解析后的同步流程（Describe → Diff → Create/Delete）
// tagStr 为本轮快照 TAG，继续显式向下传递；结果写入 w。
func (s *Syncer) syncDomainInternal(p provider.Provider, rule config.DomainRule, resolved []dns.ResolvedIP, tagStr string, w *unitResult) {
	// ECS ICMPv6 警告（仅当实际有 IPv6 地址时输出一次）
	if rule.Protocol == "ICMP" && p.CloudType() == config.CloudAliECS {
		for _, ip := range resolved {
			if ip.IsIPv6 {
				slog.Warn("ECS 不支持 ICMPv6 入站规则，IPv6 地址将被跳过", "domain", rule.Host)
				break
			}
		}
	}

	added, deleted, skipped, skippedDetails, err := s.retrySyncDetailed(p, rule, resolved, tagStr)
	// failed 与已确认增删计数正交：错误发生前已经由云端确认的独立请求仍须进入
	// 单元/整轮汇总；当前失败且提交状态未知的请求由 Provider 保持为 0。
	w.added, w.deleted, w.skipped = added, deleted, skipped
	if err != nil {
		w.failed = true
		slog.Error("同步失败", "provider", p.Name(), "domain", rule.Host, "added", added, "deleted", deleted, "error", err)
		s.bus.Publish(notifier.Event{
			Type:      notifier.EventSyncError,
			Timestamp: time.Now(),
			Data: map[string]any{
				"provider": p.Name(), "domain": rule.Host, "error": err.Error(),
				"added": added, "deleted": deleted,
			},
		})
		return
	}

	slog.Info("同步完成", "provider", p.Name(), "domain", rule.Host, "added", added, "deleted", deleted, "skipped", skipped, "skipped_details", skippedDetails)
	s.bus.Publish(notifier.Event{
		Type:      notifier.EventDomainSyncComplete,
		Timestamp: time.Now(),
		Data: map[string]any{
			"provider": p.Name(), "domain": rule.Host,
			"added": added, "deleted": deleted, "skipped": skipped,
			"skipped_details": skippedDetails,
		},
	})
}

func (s *Syncer) groupByCloud(providers []provider.Provider) map[config.CloudType][]provider.Provider {
	groups := make(map[config.CloudType][]provider.Provider)
	for _, p := range providers {
		ct := p.CloudType()
		groups[ct] = append(groups[ct], p)
	}
	return groups
}

// filterRulesForTarget 筛选适用于指定目标的规则
func filterRulesForTarget(rules []config.DomainRule, targetDBID int) []config.DomainRule {
	var filtered []config.DomainRule
	for _, r := range rules {
		if len(r.Targets) == 0 {
			filtered = append(filtered, r) // 空 = 所有目标
			continue
		}
		for _, t := range r.Targets {
			if t == targetDBID { // 直接比较 DB ID
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered
}

// filterIPv4 仅保留 IPv4 地址（禁用 IPv6 解析时使用）
func filterIPv4(ips []dns.ResolvedIP) []dns.ResolvedIP {
	var v4 []dns.ResolvedIP
	for _, ip := range ips {
		if !ip.IsIPv6 {
			v4 = append(v4, ip)
		}
	}
	return v4
}
