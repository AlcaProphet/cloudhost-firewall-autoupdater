package syncer

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
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
//   - ApplyState 原子替换状态、保留真实恢复边沿，并投递一条可合并控制通知；
//   - Run goroutine 消费通知时重新读取最新状态，据此决定是否启动新一轮、
//     是否重置 ticker；最终状态与尚未消费的恢复边沿不会因为 channel 满而丢失。
type Syncer struct {
	runtime *RuntimeManager
	bus     *notifier.EventBus

	triggerCh chan struct{} // 手动触发同步（容量 1，可合并）
	controlCh chan struct{} // 状态变化通知（容量 1，可合并；只表示「请重新读取状态」）
	stopCh    chan struct{}
	stopOnce  sync.Once
	doneCh    chan struct{}

	// startedCh 在 Run 首次进入运行态（running=true 已可见）后关闭一次（Build7 Step 7）。
	//
	// 启动方（run.go）据此把「监督器与 Push 循环」安排在同步主循环进入运行之后，
	// 避免 30 秒监督器的首检与 Push 首发读到 running=false 而误报「同步引擎未运行」。
	// Stop 先于 Run 时 Run 被拒绝，本通道永不关闭，因此等待方必须有界等待。
	startedCh chan struct{}

	// 生命周期标记（由 mu 保护）
	stopped  bool // Stop 已请求：所有新轮次的硬门控
	runGuard bool // Run 已被调用过（拒绝一切第二次调用，含首个已退出后）

	// beforeRoundHook 仅用于测试构造确定性交错（默认 nil，生产零行为变化）。
	// 在「stop 门控通过之后、syncAll 之前」调用，与 SetStateAppliedHook 同风格。
	beforeRoundHook func()

	// 目标级 attempt 的三个测试接缝（默认 nil → 生产实现，零行为变化）：
	//   - resolveHostFn 注入确定性 DNS 结果（避免单测依赖真实解析）；
	//   - backoffFn 覆盖重试退避时长（默认 1s、2s）；
	//   - sleepFn 覆盖等待实现（默认 time.Sleep），使单测不真实等待。
	resolveHostFn func(host string) ([]dns.ResolvedIP, error)
	backoffFn     func(attempt int) time.Duration
	sleepFn       func(time.Duration)

	// 状态追踪（保护以下字段）
	mu             sync.RWMutex
	running        bool
	startedAt      *time.Time // Run 进入运行态的时间；从未进入过运行态时为 nil（Build7 Step 7）
	lastSync       time.Time
	lastSuccess    *time.Time    // 最近一次「整轮成功」完成时间（内存态，重启归 null）
	lastRound      *RoundSummary // 最近一轮的整轮汇总（内存态，重启归 null）
	processStarted time.Time     // 本次进程内 Syncer 构造时间（健康判定的启动宽限基准）
	roundStarted   *time.Time    // 当前轮次开始时间；无轮次时为 nil（Build7 §7.2）

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

	// pendingResume 由 mu 保护，表示至少一次已发布的 false→true 尚未被 Run 消费。
	// 多次恢复合并为一轮；取得最终状态时同锁清除，轮中新的恢复留待下一次消费。
	pendingResume bool

	// targetNextStart 由 mu 保护，按 CloudType 保留正式目标的跨轮冷却。
	// 只保存完成后的截止时间，配置发布不清空；等待与目标执行均不持锁。
	targetNextStart map[config.CloudType]time.Time

	dryRunMu sync.Mutex // Dry Run 防重入，同时保护跨调用的平台冷却
	// dryRunNextRead 按云产品保留下一次快照读取的最早时间；只由持有 dryRunMu 的
	// DryRun 访问，不随 RuntimeState 替换清空。仅保存时间，不缓存 DNS 或云快照。
	dryRunNextRead map[config.CloudType]time.Time

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
		startedCh: make(chan struct{}),
	}
	s.processStarted = time.Now()
	if st := runtime.Snapshot(); st != nil {
		s.enabled = st.Config.SyncEnabled
		s.applied = st
	}
	return s
}

// Started 返回「同步主循环已进入运行态」信号（Build7 Step 7）。
//
// 通道在 Run 首次把 running=true 置为可见之后关闭一次；等待方读到关闭即可确认
// SyncStatus.Running 已为 true（channel close 与锁释放共同提供 happens-before）。
//
// 若 Stop 先于 Run 完成，Run 按吸收态被拒绝，本通道永不关闭——调用方必须有界等待，
// 不得无限阻塞。
func (s *Syncer) Started() <-chan struct{} {
	return s.startedCh
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
// 最终状态与恢复边沿由同一把 mu 线性化，Run 消费时同锁取得快照与恢复标记。
func (s *Syncer) ApplyState(next *RuntimeState) {
	s.mu.Lock()
	previous := s.runtime.Snapshot()
	if previous != nil && !previous.Config.SyncEnabled && next.Config.SyncEnabled {
		s.pendingResume = true
	}
	s.runtime.Apply(next)
	// 镜像只作为尚未发布状态时的 IsEnabled 回退，与本次发布一起推进。
	s.enabled = next.Config.SyncEnabled
	s.mu.Unlock()

	select {
	case s.controlCh <- struct{}{}:
	default: // 已有待处理通知，最终状态在快照里，合并即可
	}
}

// consumeControlState 同锁取得最终状态并消费已合并的恢复边沿。
// 此处在起轮前消费；最终暂停则抑制恢复，起轮后发布的新恢复不会被轮后清除。
// 锁只保护内存操作，不跨越同步轮、日志、ticker 操作或状态回调。
func (s *Syncer) consumeControlState() (*RuntimeState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.runtime.Snapshot()
	pending := s.pendingResume
	s.pendingResume = false
	return state, pending
}

// hasPendingResume 在普通轮准入前检查已存在的恢复，交给下方统一恢复分支处理。
// 检查后新发布的恢复属于下一批，仍可在当前轮结束后补轮；不提供 select 优先级。
func (s *Syncer) hasPendingResume() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingResume
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
// 第二次调用（含首个 Run 已退出之后），WARN 后立即返回，绝不 panic。Stop 是吸收态：
// 若 Stop 在首次 Run 前完成，Run 同样立即拒绝，doneCh 已由 Stop 关闭。
// 刻意不使用 sync.Once.Do——那会让第二个调用等待首个 Run 结束，语义不同。
func (s *Syncer) Run() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		slog.Warn("同步引擎已停止，忽略 Run 调用")
		return
	}
	if s.runGuard {
		s.mu.Unlock()
		slog.Warn("同步引擎已运行过，忽略重复的 Run 调用")
		// 不关闭 doneCh：doneCh 归首个 Run 所有，关闭两次会 panic
		return
	}
	s.runGuard = true
	s.mu.Unlock()

	defer close(s.doneCh)
	s.markRunning()
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

	// 启动轮代表此时的最终配置，同时消费启动前标记，避免随后重复补轮。
	state, _ := s.consumeControlState()
	// 记录 ticker 实际使用的间隔，不以初始配置或最新发布指针代替。
	tickerInterval := state.Config.Interval
	ticker := time.NewTicker(tickerInterval)
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
		// 定时/手动轮返回即算完成（包括失败或 idle），保留原有轮后间隔。
		// 启动/恢复轮保持轮前计时，不能由普通配置通知补做轮后 Reset。
		roundCompleted := false
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
				if s.hasPendingResume() {
					break
				}
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
				roundCompleted = true
			case <-s.triggerCh:
				if s.hasPendingResume() {
					break
				}
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
				roundCompleted = true
			case <-s.controlCh:
			case <-s.stopCh:
				slog.Info("同步引擎停止")
				return
			}
		}

		// 每轮循环结束都重新读取最新状态（控制通知消费后必须重新读取）
		latest, pendingResume := s.consumeControlState()
		if latest == nil {
			continue
		}
		// 控制消息消费之后，循环标志推进到已发布真值
		// （调度镜像已由 ApplyState 在发布时同步推进，此处不重复写）
		enabled = latest.Config.SyncEnabled

		switch {
		case enabled && (!wasEnabled || pendingResume):
			// 已观察的恢复或被合并的恢复边沿：更新 interval 并立即触发一轮，
			// 且先清空暂停期间排队的过期 trigger —— 本轮的立即同步已代表最新配置，
			// 陈旧 trigger 再补一轮会造成「恢复多跑一轮」（Build6 §12.5）。
			slog.Info("同步已开启")
			ticker.Reset(latest.Config.Interval)
			tickerInterval = latest.Config.Interval
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
			// 定时/手动轮完成后保留原有轮后等待；纯配置通知只在间隔变化时重新计时。
			if roundCompleted || latest.Config.Interval != tickerInterval {
				ticker.Reset(latest.Config.Interval)
				tickerInterval = latest.Config.Interval
			}
		case wasEnabled && !enabled:
			// true → false：当前轮已完成，停止 ticker 并进入暂停等待
			slog.Info("同步已暂停")
			ticker.Stop()
		default:
			// false → false：保持暂停，最新间隔在恢复时使用。
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
// 最新状态；通知可合并，真实恢复边沿由 ApplyState 单独保留。
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
// 唯一调用点是恢复轮准入：此时本轮立即同步已代表最新配置，陈旧
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
// 当前轮完成后 Run 才退出。若 Run 已取得生命周期所有权，doneCh 仍只由 Run 在退出时
// 关闭；若 Stop 发生在首次 Run 之前，则 Stop 直接关闭 doneCh，后续 Run 因 stopped
// 吸收态而被拒绝。两种所有权由同一把 mu 线性化，不会重复关闭。
// Stop 不改变 IsEnabled()（开关真值仍由 SQLite 与已发布运行时状态决定）。
func (s *Syncer) Stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		neverStarted := !s.runGuard
		s.mu.Unlock()

		close(s.stopCh)
		if neverStarted {
			close(s.doneCh)
		}
	})
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
// 必须先通过本门控；恢复入口也处理被合并的边沿。返回 false 时不得启动新轮次。
// stop 门控与 A6 的 enabled 门控**并列存在、不合并**：前者回答「是否还要跑」，
// 后者回答「本轮是否被允许跑」。
//
// 刻意不检查 IsEnabled()，因此本门控不会外溢到 Dry Run / 连接测试
// （AGENTS §五 要求二者独立于 Run() 主循环）。
func (s *Syncer) beginRound() bool {
	if s.isStopped() {
		return false
	}
	// 轮次开始时间：与 running/lastRound 共用同一把锁，Status() 取得一致快照
	started := time.Now()
	s.mu.Lock()
	s.roundStarted = &started
	s.mu.Unlock()
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
	// RoundStartedAt 是当前轮次开始时间；没有在途轮次时为 null（Build7 §7.2）。
	RoundStartedAt *time.Time `json:"round_started_at"`
	// ProcessStartedAt 是 Syncer 构造时间，作为「启动后尚无完成轮次」的基准。
	ProcessStartedAt time.Time `json:"process_started_at"`
	// StartedAt 是 Run 首次进入运行态的时间；从未进入过运行态时为 null（Build7 Step 7）。
	//
	// 运行健康判定据此区分「进程启动宽限内尚未进入运行」（不算异常）与
	// 「进入运行后又停止」（立即异常，不受宽限影响）。
	StartedAt *time.Time `json:"started_at"`
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
	if s.roundStarted != nil {
		t := *s.roundStarted
		status.RoundStartedAt = &t
	}
	if s.startedAt != nil {
		t := *s.startedAt
		status.StartedAt = &t
	}
	status.ProcessStartedAt = s.processStarted
	return status
}

func (s *Syncer) setRunning(v bool) {
	s.mu.Lock()
	s.running = v
	s.mu.Unlock()
}

// markRunning 进入运行态：置 running=true、记录首次进入时间，并在状态可见后
// 关闭 Started() 信号（Build7 Step 7）。
//
// 顺序保证：锁内写入 → 解锁 → close(startedCh)。读到通道关闭的等待方必然也能
// 看到 running=true（channel close 提供 happens-before），因此启动方无需轮询。
func (s *Syncer) markRunning() {
	now := time.Now()
	s.mu.Lock()
	s.running = true
	if s.startedAt == nil {
		started := now
		s.startedAt = &started
	}
	s.mu.Unlock()
	close(s.startedCh)
}

// ErrDryRunInProgress 防重入冲突错误（多个 Dry Run 并发执行时返回）
var ErrDryRunInProgress = errors.New("Dry Run 正在执行中")

// DryRunResponse 试运行响应（包装对象：空状态语义化）。
//
// Issue7 §7.1：results 每个已配置目标一项，且必须是非 null 数组（空态序列化为 []）。
type DryRunResponse struct {
	Results  []DryRunResult `json:"results"`
	Warnings []string       `json:"warnings"`
}

// DryRunResult 目标级试运行结果（Issue7 §7.1）。
//
// 每个已配置目标一项，`target_id` 是前端列表与 v-for 的稳定 key；所有数组固定输出 []，
// 绝不输出 null。无适用规则时结果表示未调度，CoverageReady 保持 false；有适用规则时
// Dry Run 只基于 S0，因此 CoverageReady 表示「当前快照是否已覆盖全部可实施期望」，
// 而不是对未来 Add 的预测。
type DryRunResult struct {
	TargetID            int                    `json:"target_id"`
	Provider            string                 `json:"provider"`
	Domains             []string               `json:"domains"`
	Desired             []provider.PlannedRule `json:"desired"`
	SatisfiedByOwned    []provider.PlanMatch   `json:"satisfied_by_owned"`
	SatisfiedByExternal []provider.PlanMatch   `json:"satisfied_by_external"`
	ToAdd               []provider.RuleChange  `json:"to_add"`
	// CleanupCandidates 只是预览：若此刻进入正式流程且之后 S1 仍满足全部安全门，
	// 才**可能**删除；不得表述为「将删除」。
	CleanupCandidates []provider.RuleChange `json:"cleanup_candidates"`
	CleanupDeferred   []provider.PlanIssue  `json:"cleanup_deferred"`
	DNSErrors         []provider.PlanIssue  `json:"dns_errors"`
	Unsupported       []provider.PlanIssue  `json:"unsupported"`
	Conflicts         []provider.PlanIssue  `json:"conflicts"`
	CoverageReady     bool                  `json:"coverage_ready"`
	Error             string                `json:"error"`
}

// emptyDryRunResult 构造数组字段全部非 nil 的结果骨架。
func emptyDryRunResult(targetID int, name string, domains []string) DryRunResult {
	return DryRunResult{
		TargetID:            targetID,
		Provider:            name,
		Domains:             domains,
		Desired:             []provider.PlannedRule{},
		SatisfiedByOwned:    []provider.PlanMatch{},
		SatisfiedByExternal: []provider.PlanMatch{},
		ToAdd:               []provider.RuleChange{},
		CleanupCandidates:   []provider.RuleChange{},
		CleanupDeferred:     []provider.PlanIssue{},
		DNSErrors:           []provider.PlanIssue{},
		Unsupported:         []provider.PlanIssue{},
		Conflicts:           []provider.PlanIssue{},
	}
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
	resp := DryRunResponse{Results: []DryRunResult{}, Warnings: []string{}}
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

	// 与正式同步共用同一纯规划器（provider.PlanTarget）；这里的差异只有：
	// 不写入、不发布事件、不修改熔断器、只取 S0。
	for _, p := range state.Providers {
		rules := filterRulesForTarget(state.Config.DomainRules, p.TargetIndex())
		result := emptyDryRunResult(p.TargetIndex(), p.Name(), ruleHosts(rules))
		if len(rules) == 0 {
			// Dry Run 展示所有已配置目标，便于发现规则适用范围遗漏；但正式同步不会
			// 调度无适用规则的目标，因此这里也只返回未调度骨架，不解析 DNS、
			// 不读取云快照、不进入 planner，也不产生限速等待。
			resp.Results = append(resp.Results, result)
			continue
		}

		// 每目标按 host 去重解析一次（report=false：不写熔断器、不发 DNS 事件）
		resolved, dnsErrors, _ := s.resolveTargetRules(state, rules, nil, false, nil)

		// 只在下一次同平台读取前补足冷却，首次读取与末尾返回不额外等待。
		// DNS/规划及其他平台处理已消耗的时间可抵扣；冷却跨连续 Dry Run 保留。
		if s.dryRunNextRead == nil {
			s.dryRunNextRead = make(map[config.CloudType]time.Time)
		}
		s.sleep(time.Until(s.dryRunNextRead[p.CloudType()]))
		snapshot, err := p.GetSnapshot()
		// 失败读取同样消耗请求预算；从完整快照操作结束后开始冷却，分页逻辑不变。
		s.dryRunNextRead[p.CloudType()] = time.Now().Add(rateLimitInterval(p.CloudType()))
		if err != nil {
			result.Error = err.Error()
			resp.Results = append(resp.Results, result)
			continue
		}

		plan := provider.PlanTarget(provider.TargetPlanInput{
			CloudType: p.CloudType(),
			Tag:       state.Config.Tag,
			Rules:     rules,
			Resolved:  resolved,
			DNSErrors: dnsErrors,
			Snapshot:  snapshot,
		})
		result.Desired = plan.Desired
		result.SatisfiedByOwned = plan.SatisfiedByOwned
		result.SatisfiedByExternal = plan.SatisfiedByExternal
		result.CleanupDeferred = plan.CleanupDeferred
		result.DNSErrors = plan.DNSErrors
		result.Unsupported = plan.Unsupported
		result.Conflicts = plan.Conflicts
		result.CoverageReady = plan.CoverageReady
		for _, a := range plan.ToAdd {
			result.ToAdd = append(result.ToAdd, provider.RuleChangeFromAction(a))
		}
		// 清理候选只是预览：正式流程还要在 S1 上重新过一遍全部安全门
		for _, c := range plan.CleanupCandidates {
			result.CleanupCandidates = append(result.CleanupCandidates, provider.RuleChangeFromInfo(c))
		}
		resp.Results = append(resp.Results, result)
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

// RoundSummary 一轮同步的整轮汇总（Issue7 §5.4 目标级口径）。
//
// 统计单元口径：**有至少一条适用规则的目标**，
// total = Σ_p [len(filterRulesForTarget(rules, p.TargetIndex())) > 0]。
//
// 不变量：total == ok + changed + failed + skipped
//
//   - ok      目标 success 且 added==0 && deleted==0（允许存在 cleanup_deferred）；
//   - changed 目标 success 且至少确认新增或删除一条（允许存在 cleanup_deferred）；
//   - failed  目标最终 failed；
//   - skipped 目标未失败但存在 unsupported（即 partial，字段名保留以兼容既有 API，
//     语义为「部分实施目标数」）。
//
// 只有 total>0 且 failed==0 且 skipped==0 才是 success 并刷新 last_success；
// 带 cleanup_deferred 的 success 仍刷新。
type RoundSummary struct {
	FinishedAt time.Time `json:"finished_at"`
	Total      int       `json:"total"`
	OK         int       `json:"ok"`
	Changed    int       `json:"changed"`
	Failed     int       `json:"failed"`
	Skipped    int       `json:"skipped"`
	Added      int       `json:"added"`
	Deleted    int       `json:"deleted"`
	// 候选/残留整数是各目标最近观察的兼容投影（可含历史/估计/未知零）。
	// 当前完整性和已观察数量由 CleanupObservationSummary 表达；确认清理量跨尝试累计。
	// 本 Issue 内 deleted 与 cleanup_deleted 数值相同，但保留两者以区分「总写入」与「清理」语义。
	CleanupObservationSummary CleanupObservationSummary `json:"cleanup_observation_summary"`
	CleanupCandidates         int                       `json:"cleanup_candidates"`
	CleanupDeleted            int                       `json:"cleanup_deleted"`
	CleanupDeferred           int                       `json:"cleanup_deferred"`
	DurationMS                int64                     `json:"duration_ms"`
	Outcome                   RoundOutcome              `json:"outcome"`
}

// Data 返回事件负载形态的汇总（EventSyncComplete.Data 增加同一汇总）。
func (r RoundSummary) Data() map[string]any {
	return map[string]any{
		"cleanup_observation_summary": r.CleanupObservationSummary,
		"finished_at":                 r.FinishedAt,
		"total":                       r.Total,
		"ok":                          r.OK,
		"changed":                     r.Changed,
		"failed":                      r.Failed,
		"skipped":                     r.Skipped,
		"added":                       r.Added,
		"deleted":                     r.Deleted,
		"cleanup_candidates":          r.CleanupCandidates,
		"cleanup_deleted":             r.CleanupDeleted,
		"cleanup_deferred":            r.CleanupDeferred,
		"duration_ms":                 r.DurationMS,
		"outcome":                     string(r.Outcome),
	}
}

// syncAll 执行一轮完整同步。
//
// 本轮开始时只取一次运行时快照：TAG、规则、Provider、Resolver 与熔断器全部
// 来自该快照，下游函数一律显式接收参数，不再回读运行时状态。
func (s *Syncer) syncAll() {
	// 无论轮次如何结束（正常、空状态提前返回、panic 展开）都必须清空轮次开始时间，
	// 否则会留下“陈旧在途轮次”并让健康判定长期误报同步轮次超时。
	defer func() {
		s.mu.Lock()
		s.roundStarted = nil
		s.mu.Unlock()
	}()

	state := s.runtime.Snapshot()
	if state == nil {
		return
	}

	// 先算清本轮统计单元总数（有至少一条适用规则的目标）
	total := 0
	for _, p := range state.Providers {
		if len(filterRulesForTarget(state.Config.DomainRules, p.TargetIndex())) > 0 {
			total++
		}
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
		"added", summary.Added, "deleted", summary.Deleted,
		"cleanup_candidates", summary.CleanupCandidates,
		"cleanup_deleted", summary.CleanupDeleted,
		"cleanup_deferred", summary.CleanupDeferred,
		"cleanup_observation_summary", summary.CleanupObservationSummary)
}

// outcomeOf 依据目标级口径计算整轮结论。
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

// runRound 执行一轮完整同步并按**目标**归类结果（Issue7 §5.4）。
//
// 本轮开始时只取一次运行时快照：TAG、规则、Provider、Resolver 与熔断器全部
// 来自该快照，下游函数一律显式接收参数，不再回读运行时状态。
func (s *Syncer) runRound(state *RuntimeState, total int) RoundSummary {
	round := newDNSRound(state)
	defer round.finish()
	var (
		ok, changed, failed, skipped                                       atomic.Int32
		added, deleted, cleanupCandidates, cleanupDeleted, cleanupDeferred atomic.Int32
	)

	// 按云厂商分组，跨云并行；同一云厂商内目标串行（共享配额）
	groups := s.groupByCloud(state.Providers)
	var wg sync.WaitGroup
	var observationMu sync.Mutex
	observations := CleanupObservationSummary{Complete: true}
	for ct, ps := range groups {
		wg.Add(1)
		go func(ct config.CloudType, ps []provider.Provider) {
			defer wg.Done()
			for _, p := range ps {
				rules := filterRulesForTarget(state.Config.DomainRules, p.TargetIndex())
				if len(rules) == 0 {
					// 无适用规则的目标不构成统计单元，也不访问云 API
					continue
				}
				// 同平台串行、跨轮保留冷却；先等待再解析 DNS，末尾不为空闲目标等待。
				s.mu.RLock()
				next := s.targetNextStart[ct]
				s.mu.RUnlock()
				s.sleep(time.Until(next))
				res := s.syncTarget(state, p, rules, round)
				// 成功、部分实施与失败均从完整目标结束后计时，包含重试及退避。
				s.mu.Lock()
				if s.targetNextStart == nil {
					s.targetNextStart = make(map[config.CloudType]time.Time)
				}
				s.targetNextStart[ct] = time.Now().Add(rateLimitInterval(ct))
				s.mu.Unlock()
				switch res.outcome {
				case TargetFailed:
					failed.Add(1)
				case TargetPartial:
					// 字段名保留 skipped 以兼容既有 API/前端，语义为「部分实施目标数」
					skipped.Add(1)
				default:
					if res.added > 0 || res.deleted > 0 {
						changed.Add(1)
					} else {
						ok.Add(1)
					}
				}
				added.Add(int32(res.added))
				deleted.Add(int32(res.deleted))
				cleanupCandidates.Add(int32(res.cleanupCandidates))
				cleanupDeleted.Add(int32(res.cleanupDeleted))
				cleanupDeferred.Add(int32(res.cleanupDeferred))
				observationMu.Lock()
				observations.add(res)
				observationMu.Unlock()
			}
		}(ct, ps)
	}
	wg.Wait()

	return RoundSummary{
		CleanupObservationSummary: observations,
		Total:                     total,
		OK:                        int(ok.Load()),
		Changed:                   int(changed.Load()),
		Failed:                    int(failed.Load()),
		Skipped:                   int(skipped.Load()),
		Added:                     int(added.Load()),
		Deleted:                   int(deleted.Load()),
		CleanupCandidates:         int(cleanupCandidates.Load()),
		CleanupDeleted:            int(cleanupDeleted.Load()),
		CleanupDeferred:           int(cleanupDeferred.Load()),
	}
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
