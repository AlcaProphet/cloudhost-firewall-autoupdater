package syncer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
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

	// 状态追踪（保护以下字段）
	mu       sync.RWMutex
	running  bool
	lastSync time.Time
	// enabled 是「已提交开关」的调度镜像：唯一真值在 SQLite 与已发布的运行时状态。
	//
	// 由 ApplyState 在发布新状态时同步推进，用于两处判定：
	//   - trigger 门控：暂停期间排队的陈旧 trigger 在恢复后不会额外多跑一轮
	//     （Build6 §12.5「排队中的 trigger 在消费前重新检查 enabled」）；
	//   - Run 的过渡判定：以「进入 select 阻塞之前记录的镜像」与「已发布真值」
	//     比较，因此 false→true 的立即轮次只由 Run 执行一次，不会与调用方重复。
	//
	// 对外的 IsEnabled/Status 读的是已发布状态本身，保证 pause/resume 之后
	// TriggerSync 的 409 判定立即正确。
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

	// 调度镜像在此同步推进，使 pause/resume 之后 trigger 门控立即反映已提交状态；
	// Run 的过渡判定使用「进入 select 前记录的镜像」与「已发布真值」，因此
	// false→true 的立即轮次只由 Run 执行一次，不会与调用方重复。
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

// Run 启动同步主循环（阻塞，直到收到停止信号）
func (s *Syncer) Run() {
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
		s.syncAll()
	} else {
		slog.Info("同步已暂停（SyncEnabled=false），等待开启")
	}

	for {
		// 进入阻塞等待之前先记录「本次等待开始前已生效的开关」：
		// 唤醒后据此判定 false→true / true→false 过渡（Build6 §12.5）。
		wasEnabled := s.isEnabledMirror()

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

		// 调度决策完成后通知观察者：此时新状态的开关/interval 已真正生效
		s.logAppliedState(latest)
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

// isEnabledMirror 读取**调度已生效**的开关镜像（供 Run 的过渡判定与 trigger 门控使用）。
//
// 该镜像只在控制消息被 Run 消费后推进，因此暂停期间排队的陈旧 trigger 不会
// 在恢复通知消费前通过门控（Build6 §12.5）。
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
func (s *Syncer) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
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
type SyncStatus struct {
	Running  bool       `json:"running"`
	Enabled  bool       `json:"enabled"`
	LastSync *time.Time `json:"last_sync"`
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
func (s *Syncer) syncAll() {
	state := s.runtime.Snapshot()
	if state == nil {
		return
	}

	slog.Info("开始同步", "targets", len(state.Providers), "rules", len(state.Config.DomainRules))
	start := time.Now()

	// 发布 sync:start 事件
	s.bus.Publish(notifier.Event{
		Type:      notifier.EventSyncStart,
		Timestamp: time.Now(),
		Data:      map[string]any{"targets": len(state.Providers), "rules": len(state.Config.DomainRules)},
	})

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
					s.syncDomain(state, p, rule)
					time.Sleep(rateLimitInterval(ct))
				}
			}
		}(ct, ps)
	}
	wg.Wait()

	s.mu.Lock()
	s.lastSync = time.Now()
	s.mu.Unlock()

	// 发布 sync:complete 事件
	s.bus.Publish(notifier.Event{
		Type:      notifier.EventSyncComplete,
		Timestamp: time.Now(),
		Data:      map[string]any{"duration": time.Since(start).String()},
	})

	slog.Info("同步完成", "耗时", time.Since(start).Round(time.Millisecond))
}

// syncDomain 同步单个域名到单个 Provider。
//
// state 是本轮开始的完整快照：TAG、Resolver 与熔断器都只从它读取，
// 因此热重载产生的下一份状态不会影响正在执行的本轮。
func (s *Syncer) syncDomain(state *RuntimeState, p provider.Provider, rule config.DomainRule) {
	// 0. DNS 解析（无论是否熔断都执行，熔断时作为半开探测）
	resolved, err := state.Resolver.Resolve(context.Background(), rule.Host)
	if err != nil {
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
	s.syncDomainInternal(p, rule, resolved, state.Config.Tag)
}

// syncDomainInternal 执行 DNS 已解析后的同步流程（Describe → Diff → Create/Delete）
// tagStr 为本轮快照 TAG，继续显式向下传递
func (s *Syncer) syncDomainInternal(p provider.Provider, rule config.DomainRule, resolved []dns.ResolvedIP, tagStr string) {
	// ECS ICMPv6 警告（仅当实际有 IPv6 地址时输出一次）
	if rule.Protocol == "ICMP" && p.CloudType() == config.CloudAliECS {
		for _, ip := range resolved {
			if ip.IsIPv6 {
				slog.Warn("ECS 不支持 ICMPv6 入站规则，IPv6 地址将被跳过", "domain", rule.Host)
				break
			}
		}
	}

	added, deleted, err := s.retrySync(p, rule, resolved, tagStr)
	if err != nil {
		slog.Error("同步失败", "provider", p.Name(), "domain", rule.Host, "error", err)
		s.bus.Publish(notifier.Event{
			Type:      notifier.EventSyncError,
			Timestamp: time.Now(),
			Data:      map[string]any{"provider": p.Name(), "domain": rule.Host, "error": err.Error()},
		})
		return
	}

	slog.Info("同步完成", "provider", p.Name(), "domain", rule.Host)
	s.bus.Publish(notifier.Event{
		Type:      notifier.EventDomainSyncComplete,
		Timestamp: time.Now(),
		Data:      map[string]any{"provider": p.Name(), "domain": rule.Host, "added": added, "deleted": deleted},
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
