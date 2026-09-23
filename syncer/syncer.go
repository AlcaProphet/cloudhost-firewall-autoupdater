package syncer

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// Syncer 同步引擎
type Syncer struct {
	cfg       *config.Config
	providers []provider.Provider
	resolver  *dns.Resolver
	cb        *dns.CircuitBreaker
	bus       *notifier.EventBus
	configCh  chan *config.Config
	triggerCh chan struct{} // 手动触发同步
	stopCh    chan struct{}
	doneCh    chan struct{} // Run 退出时关闭，用于等待当前轮次完成

	// 状态追踪（保护以下字段）
	mu       sync.RWMutex
	running  bool
	lastSync time.Time

	dryRunMu sync.Mutex // Dry Run 防重入

	// 同步开关（暂停门控）：运行时镜像，启动时从 cfg.SyncEnabled 初始化
	// 使用 atomic.Bool 避免 Pause()/Resume()（API goroutine）与 Run() 主循环并发写竞态
	syncEnabled atomic.Bool
	pauseCh     chan struct{} // 接收暂停信号，容量 1
	resumeCh    chan struct{} // 接收恢复信号，容量 1
}

// New 创建同步引擎
func New(cfg *config.Config, providers []provider.Provider, resolver *dns.Resolver) *Syncer {
	s := &Syncer{
		cfg:       cfg,
		providers: providers,
		resolver:  resolver,
		cb:        dns.NewCircuitBreaker(cfg.DNSFailThreshold),
		bus:       notifier.NewEventBus(),
		configCh:  make(chan *config.Config, 1),
		triggerCh: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
		pauseCh:   make(chan struct{}, 1),
		resumeCh:  make(chan struct{}, 1),
	}
	s.syncEnabled.Store(cfg.SyncEnabled) // 运行时镜像从配置初始化
	return s
}

// EventBus 返回事件总线（供外部订阅）
func (s *Syncer) EventBus() *notifier.EventBus {
	return s.bus
}

// Run 启动同步主循环（阻塞，直到收到停止信号）
func (s *Syncer) Run() {
	defer close(s.doneCh)
	s.setRunning(true)
	defer s.setRunning(false)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	// 启动门控：开关关闭时跳过首次 syncAll，进入暂停等待（Design2 §7.1）
	// 注意：waitForResume 返回后必须继续进入主循环，不可 return（否则恢复后定时同步失效）
	if !s.syncEnabled.Load() {
		slog.Info("同步已暂停（SyncEnabled=false），等待开启")
		s.waitForResume(ticker)
	} else {
		s.syncAll()
	}

	for {
		select {
		case <-ticker.C:
			s.syncAll()
		case <-s.triggerCh:
			slog.Info("手动触发同步")
			s.syncAll()
		case newCfg := <-s.configCh:
			slog.Info("配置热重载")
			s.mu.Lock()
			s.cfg = newCfg // 保持 Step 2 引入的锁结构
			s.mu.Unlock()
			ticker.Reset(newCfg.Interval)
			// 5.6 开关同步：热重载变更 sync_enabled 时同步门控（DB 状态与运行时镜像一致）
			if newCfg.SyncEnabled != s.syncEnabled.Load() {
				if newCfg.SyncEnabled {
					slog.Info("热重载开启同步")
					s.syncEnabled.Store(true)
					s.syncAll() // 立即执行首次
				} else {
					slog.Info("热重载暂停同步")
					s.syncEnabled.Store(false)
					s.pauseGate(ticker)
				}
			}
		case <-s.pauseCh:
			s.pauseGate(ticker)
		case <-s.stopCh:
			slog.Info("同步引擎停止")
			return
		}
	}
}

// pauseGate 暂停门控：停止 ticker，进入等待子循环（resume/热重载开启/stop 均返回后回到主循环）
func (s *Syncer) pauseGate(ticker *time.Ticker) {
	ticker.Stop()
	s.waitForResume(ticker)
}

// waitForResume 暂停等待子循环（Run 启动时暂停与运行中暂停共用）
// 返回条件：收到 resumeCh / 热重载开启（configCh 携带 SyncEnabled=true）→ 已恢复 ticker 并执行首次 syncAll；
// 收到 stopCh → 直接返回（外层 Run 退出）
func (s *Syncer) waitForResume(ticker *time.Ticker) {
	for {
		select {
		case <-s.resumeCh:
			slog.Info("同步恢复")
			s.syncEnabled.Store(true)
			ticker.Reset(s.cfg.Interval)
			s.syncAll() // 恢复后立即执行首次
			return
		case newCfg := <-s.configCh:
			s.mu.Lock()
			s.cfg = newCfg
			s.mu.Unlock()
			if newCfg.SyncEnabled {
				s.syncEnabled.Store(true)
				ticker.Reset(newCfg.Interval)
				s.syncAll()
				return
			}
			// SyncEnabled 仍为 false：继续等待（ticker 已停止，不触发同步）
		case <-s.stopCh:
			return
		}
	}
}

// Pause 暂停同步（非阻塞）
func (s *Syncer) Pause() {
	s.syncEnabled.Store(false)
	select {
	case s.pauseCh <- struct{}{}:
	default: // 已在暂停中
	}
}

// Resume 恢复同步（非阻塞）
func (s *Syncer) Resume() {
	s.syncEnabled.Store(true)
	select {
	case s.resumeCh <- struct{}{}:
	default: // 已在运行中
	}
}

// IsEnabled 返回当前开关状态
func (s *Syncer) IsEnabled() bool { return s.syncEnabled.Load() }

// Stop 优雅停止
func (s *Syncer) Stop() {
	close(s.stopCh)
}

// Wait 等待 Syncer 完全退出（Stop 后调用）
func (s *Syncer) Wait() { <-s.doneCh }

// Reload 热重载配置
func (s *Syncer) Reload(cfg *config.Config) {
	s.configCh <- cfg
}

// ReloadProviders 热重载 Provider 列表
func (s *Syncer) ReloadProviders(providers []provider.Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = providers
}

// ReloadResolver 热重载 DNS 解析器（DNS 地址或超时变更时调用）
func (s *Syncer) ReloadResolver(resolver *dns.Resolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolver = resolver
}

// TriggerSync 手动触发一次同步（非阻塞）
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := SyncStatus{Running: s.running, Enabled: s.syncEnabled.Load()}
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

// DryRun 试运行：DNS 解析 + Diff，不写入不触发事件
// 快照锁：RLock 保护 providers/cfg/resolver（与 Step 2 的 cfg 写锁配对，消除热重载并发竞态）
// 防重入：dryRunMu.TryLock，冲突返回 ErrDryRunInProgress（handler 转 409）
// 限速：与 syncAll 一致，同云厂商请求间加入间隔
func (s *Syncer) DryRun() (DryRunResponse, error) {
	if !s.dryRunMu.TryLock() {
		return DryRunResponse{}, ErrDryRunInProgress
	}
	defer s.dryRunMu.Unlock()

	// 快照：RLock 保护 providers/cfg/resolver（整个遍历使用快照，期间热重载不影响本次结果）
	s.mu.RLock()
	providers := s.providers
	cfg := s.cfg
	resolver := s.resolver
	s.mu.RUnlock()

	resp := DryRunResponse{Results: []DryRunResult{}}
	if len(providers) == 0 {
		resp.Warnings = append(resp.Warnings, "暂无云资源目标，请先在云资源管理页配置")
	}
	if len(cfg.DomainRules) == 0 {
		resp.Warnings = append(resp.Warnings, "暂无域名规则，请先在域名规则页配置")
	}
	for _, p := range providers {
		rules := filterRulesForTarget(cfg.DomainRules, p.TargetIndex())
		for _, rule := range rules {
			result := DryRunResult{Provider: p.Name(), Domain: rule.Host}
			resolved, err := resolver.Resolve(context.Background(), rule.Host)
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
			owned := provider.OwnedRules(allRules, cfg.Tag)
			desc := truncateDesc(tag.Format(cfg.Tag, rule.Comment), p.CloudType())
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

// syncAll 执行一轮完整同步
func (s *Syncer) syncAll() {
	// 快照：RLock 保护 providers/cfg/resolver（与 DryRun 一致，消除热重载并发竞态）。
	// 热重载（ReloadProviders/ReloadResolver/Reload）持写锁替换这些字段，本轮同步使用快照不受影响
	s.mu.RLock()
	providers := s.providers
	cfg := s.cfg
	resolver := s.resolver
	s.mu.RUnlock()

	// 本轮 TAG 快照：筛选、描述生成和全部重试只使用该值，热重载的新 TAG 从下一轮同步开始生效
	roundTag := cfg.Tag

	slog.Info("开始同步", "targets", len(providers), "rules", len(cfg.DomainRules))
	start := time.Now()

	// 发布 sync:start 事件
	s.bus.Publish(notifier.Event{
		Type:      notifier.EventSyncStart,
		Timestamp: time.Now(),
		Data:      map[string]any{"targets": len(providers), "rules": len(cfg.DomainRules)},
	})

	// 按云厂商分组，跨云并行
	groups := s.groupByCloud(providers)
	var wg sync.WaitGroup
	for ct, ps := range groups {
		wg.Add(1)
		go func(ct config.CloudType, ps []provider.Provider) {
			defer wg.Done()
			for _, p := range ps {
				rules := filterRulesForTarget(cfg.DomainRules, p.TargetIndex())
				for _, rule := range rules {
					s.syncDomain(p, rule, resolver, roundTag)
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

// syncDomain 同步单个域名到单个 Provider
// tagStr 为本轮快照 TAG，由 syncAll 捕获后显式传递，避免下游越过快照读取可被替换的 s.cfg
func (s *Syncer) syncDomain(p provider.Provider, rule config.DomainRule, resolver *dns.Resolver, tagStr string) {
	// 0. DNS 解析（无论是否熔断都执行，熔断时作为半开探测）
	resolved, err := resolver.Resolve(context.Background(), rule.Host)
	if err != nil {
		if s.cb.IsOpen(rule.Host) {
			// 半开探测失败：维持熔断（不调用 RecordFailure，熔断中已停止计数）
			slog.Debug("域名半开探测失败，维持熔断", "domain", rule.Host, "error", err)
		} else {
			s.cb.RecordFailure(rule.Host)
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
	s.cb.RecordSuccess(rule.Host)

	// 1. 按规则配置过滤 IPv6 地址
	if !rule.EnableIPv6 {
		resolved = filterIPv4(resolved)
	}

	// 2. 委托给内部方法执行同步
	s.syncDomainInternal(p, rule, resolved, tagStr)
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

// WaitForSignal 等待停止信号，并等待 Run 完全退出（确保当前轮次完成）
func WaitForSignal(s *Syncer) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
	slog.Info("收到停止信号，等待当前轮次完成...")
	s.Stop()
	<-s.doneCh // 等待 Run 完全退出
}
