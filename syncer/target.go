package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件实现 Issue7 Step 2 的目标级同步主流程：
//
//	Resolve（按 host 去重）→ S0 → Plan → Add(S0 版本) → S1 → 同一 planner 验证覆盖
//	→ 清理安全门（本 Step 恒不删除，候选全部 cleanup_deferred）
//
// 不可破坏不变量：任何 Add 都必须在 Delete 之前；覆盖验证失败、快照失败或
// 提交状态未知时，旧规则必须全部保留。

// TargetOutcome 单个目标的结论（Issue7 §5.3：failed > partial > success）。
type TargetOutcome string

const (
	// TargetSuccess 所需功能全部确认；允许存在 cleanup_deferred
	TargetSuccess TargetOutcome = "success"
	// TargetPartial 平台能力限制导致部分期望功能无法实施
	TargetPartial TargetOutcome = "partial"
	// TargetFailed DNS/快照/新增/覆盖验证失败
	TargetFailed TargetOutcome = "failed"
)

// targetResult 单个目标的同步结果（RoundSummary 的统计单元）。
type targetResult struct {
	targetID          int
	provider          string
	domains           []string
	outcome           TargetOutcome
	added             int
	deleted           int
	unsupported       []provider.PlanIssue
	cleanupCandidates int
	cleanupDeleted    int
	cleanupResolved   int
	cleanupDeferred   int
	durationMS        int64
	err               error
}

// retryableCleanupError 标记「本 attempt 已由 S1 证明所需功能存在，只有 Delete
// 请求发生可重试错误」。前两次仍由 syncTarget 做完整目标重试；最后一次耗尽时，
// 该类型允许把残留收敛为 success + cleanup_deferred，而不会吞掉 DNS、Describe、
// Add、S1 覆盖验证或 S2 验证失败。
type retryableCleanupError struct {
	err error
}

func (e *retryableCleanupError) Error() string { return e.err.Error() }
func (e *retryableCleanupError) Unwrap() error { return e.err }

// syncTarget 执行一个目标的完整同步：最多 maxRetries 次整目标 attempt。
//
// 每个 attempt 都重新 Resolve、取 S0、规划；已由云端确认的写入跨 attempt 保留，
// 但再次规划后不会重复计数（已生效规则不会再次出现在 ToAdd 中）。
func (s *Syncer) syncTarget(state *RuntimeState, p provider.Provider, rules []config.DomainRule) targetResult {
	started := time.Now()
	res := targetResult{
		targetID: p.TargetIndex(),
		provider: p.Name(),
		domains:  ruleHosts(rules),
		outcome:  TargetSuccess,
	}
	finish := func() targetResult {
		// 目标耗时覆盖完整生命周期，包括整目标重试与退避；所有最终事件共用同一口径。
		res.durationMS = time.Since(started).Milliseconds()
		s.publishTargetResult(res)
		return res
	}
	dnsFailedHosts := make(map[string]bool)
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := s.backoff(attempt)
			slog.Warn("重试目标同步", "attempt", attempt+1, "backoff", backoff, "provider", p.Name())
			s.sleep(backoff)
		}

		attemptRes, err := s.runTargetAttempt(state, p, rules, dnsFailedHosts)
		res.added += attemptRes.added
		res.deleted += attemptRes.deleted
		res.unsupported = attemptRes.unsupported
		res.cleanupCandidates = attemptRes.cleanupCandidates
		res.cleanupDeleted += attemptRes.cleanupDeleted
		res.cleanupDeferred = attemptRes.cleanupDeferred
		res.cleanupResolved = attemptRes.cleanupResolved

		if err == nil {
			res.outcome = attemptRes.outcome
			return finish()
		}
		lastErr = err
		var cleanupErr *retryableCleanupError
		if attempt == maxRetries-1 && errors.As(err, &cleanupErr) {
			// 本次已由 S1 证明所需功能存在；三次清理重试耗尽只延后残留，
			// 不得把访问能力正常的目标误报为 failed/unhealthy。
			res.outcome = attemptRes.outcome
			res.err = nil
			slog.Warn("清理重试耗尽，保留残留并记为 cleanup_deferred",
				"provider", p.Name(), "cleanup_deferred", res.cleanupDeferred, "error", cleanupErr)
			return finish()
		}
		if !isRetryable(err) {
			break
		}
	}

	res.outcome = TargetFailed
	res.err = lastErr
	// 达到最大重试次数仍失败时发布一次目标级失败事件（中间 attempt 不发布）
	return finish()
}

// runTargetAttempt 执行一次完整的「S0 → Plan → Add → S1 → 覆盖验证 → 条件清理」。
func (s *Syncer) runTargetAttempt(
	state *RuntimeState,
	p provider.Provider,
	rules []config.DomainRule,
	dnsFailedHosts map[string]bool,
) (targetResult, error) {
	res := targetResult{
		targetID: p.TargetIndex(),
		provider: p.Name(),
		domains:  ruleHosts(rules),
		outcome:  TargetSuccess,
	}

	// 1) 按 host 去重解析（每次 attempt 重新解析；attempt 内同一 host 只解析一次）
	resolved, dnsErrors, dnsErrValues := s.resolveTargetRules(state, rules, dnsFailedHosts, true)

	// 2) S0：本次 attempt 的完整快照（含腾讯版本号）
	s0, err := p.GetSnapshot()
	if err != nil {
		return res, fmt.Errorf("获取 S0 快照失败: %w", err)
	}

	// 3) Plan(S0)
	plan0 := provider.PlanTarget(provider.TargetPlanInput{
		CloudType: p.CloudType(),
		Tag:       state.Config.Tag,
		Rules:     rules,
		Resolved:  resolved,
		DNSErrors: dnsErrors,
		Snapshot:  s0,
	})
	// S0 已经确定的平台能力限制必须立即进入 attempt 结果；若后续 Add 失败，
	// 最终 failed 事件仍需保留这些 unsupported 明细（Issue7 §5.3、§7.2）。
	res.unsupported = plan0.Unsupported

	// 4) Add：永远先于任何删除；携带 S0 快照做版本保护
	if len(plan0.ToAdd) > 0 {
		createRes, err := p.CreateRules(s0, plan0.ToAdd)
		// 即使返回错误，Written 仍表示此前已由云端确认的独立子批次
		res.added += createRes.Written
		if err != nil {
			if !isIdempotentCreate(err) {
				return res, fmt.Errorf("新增失败: %w", err)
			}
			slog.Warn("规则已存在，跳过", "provider", p.Name())
		}
	}

	// 5) S1：Add 后必须重新取得完整快照
	s1, err := p.GetSnapshot()
	if err != nil {
		return res, fmt.Errorf("获取 S1 快照失败: %w", err)
	}

	// 6) 用同一 canonical key / 同一纯规划器复算覆盖
	plan1 := provider.PlanTarget(provider.TargetPlanInput{
		CloudType: p.CloudType(),
		Tag:       state.Config.Tag,
		Rules:     rules,
		Resolved:  resolved,
		DNSErrors: dnsErrors,
		Snapshot:  s1,
	})
	res.unsupported = plan1.Unsupported
	res.cleanupCandidates = len(plan1.CleanupCandidates)
	// 失败 attempt 若已确认删除，其计数必须保留；残留由 resolved 推算。
	res.cleanupDeferred = len(plan1.CleanupCandidates) - res.cleanupResolved
	res.cleanupDeleted = 0

	if len(plan1.ToAdd) > 0 {
		return res, fmt.Errorf("新增后覆盖验证失败：%d 个期望功能未在 S1 快照中得到确认", len(plan1.ToAdd))
	}
	if len(plan1.DNSErrors) > 0 {
		return res, dnsFailureError(plan1.DNSErrors, dnsErrValues)
	}
	// S1 覆盖确认后即确定功能结论；后续清理失败只影响 cleanup_deferred。
	if len(plan1.Unsupported) > 0 {
		res.outcome = TargetPartial
	} else {
		res.outcome = TargetSuccess
	}

	// 7) 条件清理：只有安全门全部满足（CleanupDeletable 非空）且 S1 覆盖已确认时才删除；
	//    Add 或覆盖验证失败时上面的 return 已保证本 attempt 零删除。
	cleanupDeleted, cleanupResolved, err := s.runTargetCleanup(state, p, rules, s1, plan1, resolved, dnsErrors)
	res.cleanupDeleted = cleanupDeleted
	res.deleted += cleanupDeleted
	res.cleanupResolved = cleanupResolved
	res.cleanupDeferred = len(plan1.CleanupCandidates) - cleanupResolved
	if err != nil {
		return res, err
	}
	return res, nil
}

// runTargetCleanup 在清理安全门满足时执行条件删除，并在发生删除后强制 S2 覆盖验证。
//
// 返回「已处理完成的候选数」（= 删除 + 幂等已不存在），调用方据此计算残留候选。
//
// 固定语义（Issue7 §4.6、§5.5）：
//   - 删除前再次证明候选严格属于当前 TAG 且 key 不在完整 Desired；
//   - 云端确认删除或返回 PartialDeleteError 后必须取得 S2 并复算覆盖；
//   - 清理请求失败（无确认删除）不改写 success，只记为 cleanup_deferred；
//   - 腾讯版本竞争属于可重试条件，整目标重新 attempt，绝不无版本重发。
func (s *Syncer) runTargetCleanup(
	state *RuntimeState,
	p provider.Provider,
	rules []config.DomainRule,
	s1 provider.RuleSnapshot,
	plan1 provider.TargetPlan,
	resolvedIPs map[int][]dns.ResolvedIP,
	dnsErrors map[int]string,
) (int, int, error) {
	if len(plan1.CleanupDeletable) == 0 {
		return 0, 0, nil
	}

	desiredKeys := make(map[provider.FunctionalKey]bool, len(plan1.Desired))
	for _, d := range plan1.Desired {
		desiredKeys[d.Key] = true
	}
	deletable := make([]config.RuleInfo, 0, len(plan1.CleanupDeletable))
	for _, c := range plan1.CleanupDeletable {
		if !tag.IsOwned(c.Description, state.Config.Tag) {
			// 非当前 TAG：绝不操作，也不计入已处理
			slog.Warn("清理候选不属于当前 TAG，跳过", "provider", p.Name(), "description", c.Description)
			continue
		}
		keys, ok := provider.SnapshotRuleKeys(c)
		if !ok {
			slog.Warn("清理候选字段不足，跳过", "provider", p.Name(), "description", c.Description)
			continue
		}
		conflict := false
		for _, k := range keys {
			if desiredKeys[k] {
				conflict = true
				break
			}
		}
		if conflict {
			slog.Warn("清理候选仍属于完整期望集，跳过", "provider", p.Name(), "description", c.Description)
			continue
		}
		deletable = append(deletable, c)
	}
	if len(deletable) == 0 {
		return 0, 0, nil
	}

	delRes, delErr := p.DeleteRules(s1, deletable)
	resolved := delRes.Resolved

	switch {
	case delErr == nil, isPartialDelete(delErr):
		// 已确认删除或部分成功：必须取得 S2 并复算覆盖
		if err := s.verifyCleanupResult(state, p, rules, s1.Revision, resolvedIPs, dnsErrors); err != nil {
			return delRes.Deleted, resolved, err
		}
	case isIdempotentDelete(delErr):
		// 「已不存在」视为清理成功但不计 Deleted；仍需 S2 确认覆盖
		slog.Warn("清理候选已不存在，按幂等处理", "provider", p.Name())
		if err := s.verifyCleanupResult(state, p, rules, s1.Revision, resolvedIPs, dnsErrors); err != nil {
			return delRes.Deleted, resolved, err
		}
	case isVersionMismatch(delErr):
		// 版本竞争：整目标重新 attempt（绝不无版本重发）
		return delRes.Deleted, resolved, &retryableCleanupError{
			err: fmt.Errorf("清理版本竞争，整目标重试: %w", delErr),
		}
	case isRetryable(delErr):
		return delRes.Deleted, resolved, &retryableCleanupError{
			err: fmt.Errorf("清理失败（可重试）：%w", delErr),
		}
	default:
		// 其它清理失败：所需权限已由 S1 证明，结果仍为 success + deferred
		slog.Warn("清理失败，保留残留并记为 cleanup_deferred", "provider", p.Name(), "error", delErr)
		resolved = 0
	}
	return delRes.Deleted, resolved, nil
}

// verifyCleanupResult 取得 S2 并用同一 planner 复算覆盖：S2 失败或不再覆盖所需功能即 failed。
func (s *Syncer) verifyCleanupResult(
	state *RuntimeState,
	p provider.Provider,
	rules []config.DomainRule,
	s1Revision string,
	resolvedIPs map[int][]dns.ResolvedIP,
	dnsErrors map[int]string,
) error {
	s2, err := p.GetSnapshot()
	if err != nil {
		return fmt.Errorf("获取 S2 快照失败: %w", err)
	}
	if strings.TrimSpace(s2.Revision) == "" && strings.TrimSpace(s1Revision) != "" {
		return fmt.Errorf("%w: S2 缺少版本号，无法证明清理后状态", provider.ErrSnapshotIncomplete)
	}
	plan2 := provider.PlanTarget(provider.TargetPlanInput{
		CloudType: p.CloudType(),
		Tag:       state.Config.Tag,
		Rules:     rules,
		Resolved:  resolvedIPs, // 与本次 attempt 完全相同的解析结果
		DNSErrors: dnsErrors,
		Snapshot:  s2,
	})
	if len(plan2.ToAdd) > 0 {
		return fmt.Errorf("S2 覆盖验证失败：清理后 %d 个期望功能不再被快照覆盖", len(plan2.ToAdd))
	}
	return nil
}

// resolveTargetRules 按 host 去重解析，并维护熔断器与 DNS 失败事件。
//
// 返回：本地 rule ID → 解析结果；本地 rule ID → 稳定错误文案；本地 rule ID → 原始错误。
// 解析成功但结果为空时两者都不写入，由规划器归类为 dns_empty。
func (s *Syncer) resolveTargetRules(
	state *RuntimeState,
	rules []config.DomainRule,
	dnsFailedHosts map[string]bool,
	report bool,
) (map[int][]dns.ResolvedIP, map[int]string, map[int]error) {
	type hostResolution struct {
		ips []dns.ResolvedIP
		err error
	}
	cache := make(map[string]hostResolution, len(rules))
	resolved := make(map[int][]dns.ResolvedIP, len(rules))
	dnsErrors := make(map[int]string)
	rawErrors := make(map[int]error)

	for _, rule := range rules {
		hostKey := strings.ToLower(strings.TrimSpace(rule.Host))
		hr, cached := cache[hostKey]
		if !cached {
			ips, err := s.resolveHost(state, rule.Host)
			hr = hostResolution{ips: ips, err: err}
			cache[hostKey] = hr
			if !report {
				// Dry Run：只读解析，不改熔断器、不发事件
			} else if err != nil {
				if state.Breaker.IsOpen(rule.Host) {
					// 半开探测失败：维持熔断（熔断中已停止计数）
					slog.Debug("域名半开探测失败，维持熔断", "domain", rule.Host, "error", err)
				} else {
					state.Breaker.RecordFailure(rule.Host)
					slog.Warn("DNS 解析失败，保留现有规则", "domain", rule.Host, "error", err)
				}
			} else {
				state.Breaker.RecordSuccess(rule.Host)
			}
		}

		if hr.err != nil {
			dnsErrors[rule.ID] = hr.err.Error()
			rawErrors[rule.ID] = hr.err
			// 同一目标同一 host 一轮最多发布一次 DNS 失败事件（Dry Run 不发布）
			if report && dnsFailedHosts != nil && !dnsFailedHosts[hostKey] {
				dnsFailedHosts[hostKey] = true
				s.bus.Publish(notifier.Event{
					Type:      notifier.EventDNSFailed,
					Timestamp: time.Now(),
					Data:      map[string]any{"domain": rule.Host, "error": hr.err.Error()},
				})
			}
			continue
		}

		ips := hr.ips
		if !rule.EnableIPv6 {
			ips = filterIPv4(ips)
		}
		if len(ips) > 0 {
			resolved[rule.ID] = ips
		}
	}

	return resolved, dnsErrors, rawErrors
}

// resolveHost 解析单个域名；测试接缝为 nil 时使用当轮运行时快照的 Resolver。
func (s *Syncer) resolveHost(state *RuntimeState, host string) ([]dns.ResolvedIP, error) {
	if s.resolveHostFn != nil {
		return s.resolveHostFn(host)
	}
	return state.Resolver.Resolve(context.Background(), host)
}

// backoff 返回第 attempt 次重试（1 基）的退避时长；默认 1s、2s。
func (s *Syncer) backoff(attempt int) time.Duration {
	if s.backoffFn != nil {
		return s.backoffFn(attempt)
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

// sleep 是唯一的等待入口（退避与云厂商限速共用），测试接缝为 nil 时真实等待。
func (s *Syncer) sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	if s.sleepFn != nil {
		s.sleepFn(d)
		return
	}
	time.Sleep(d)
}

// dnsFailureError 把目标级 DNS 失败转成可重试判定友好的错误。
func dnsFailureError(issues []provider.PlanIssue, raw map[int]error) error {
	ids := make([]int, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if err := raw[id]; err != nil {
			return fmt.Errorf("域名解析失败（%d 个适用规则）：%w", len(issues), err)
		}
	}
	return fmt.Errorf("域名解析失败或为空（%d 个适用规则）", len(issues))
}

// ruleHosts 返回去重并按字典序排列的来源域名。
func ruleHosts(rules []config.DomainRule) []string {
	seen := make(map[string]bool, len(rules))
	hosts := make([]string, 0, len(rules))
	for _, r := range rules {
		h := strings.TrimSpace(r.Host)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

// publishTargetResult 发布目标级结果事件（Issue7 §7.2）。
//
//   - 成功/部分实施：EventTargetSyncComplete，Data 携带 provider/target_id/domains/
//     outcome/added/deleted/unsupported/cleanup_*/duration_ms；
//   - 失败：EventSyncError，Data 明确 target_id/domains，不再伪装成单域名结果。
//
// 生产链不再发布 EventDomainSyncComplete（避免两套完成事件造成重复日志）。
func (s *Syncer) publishTargetResult(res targetResult) {
	unsupported := append([]provider.PlanIssue{}, res.unsupported...)
	data := map[string]any{
		"provider":           res.provider,
		"target_id":          res.targetID,
		"domains":            res.domains,
		"domain":             strings.Join(res.domains, ", "),
		"added":              res.added,
		"deleted":            res.deleted,
		"outcome":            string(res.outcome),
		"unsupported":        unsupported,
		"cleanup_candidates": res.cleanupCandidates,
		"cleanup_deleted":    res.cleanupDeleted,
		"cleanup_deferred":   res.cleanupDeferred,
		"duration_ms":        res.durationMS,
		// skipped 仅保留目标事件的既有计数表达；结构化明细唯一以 unsupported 为准。
		"skipped": len(unsupported),
	}
	if res.outcome == TargetFailed {
		data["error"] = errText(res.err)
		s.bus.Publish(notifier.Event{Type: notifier.EventSyncError, Timestamp: time.Now(), Data: data})
		return
	}
	s.bus.Publish(notifier.Event{Type: notifier.EventTargetSyncComplete, Timestamp: time.Now(), Data: data})
}

// errText 返回可安全展示的错误文案。
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
