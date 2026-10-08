package syncer

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// P3-07：回归通过现生产轮次/目标链执行，不保留第二套同步算法。
// 仅使用本地 Provider 与确定性 DNS 夹具，不构成真实云证据。

// TestTargetRetry_SnapshotFailures 验证真实超时/腾讯 SDK 错误的整目标重试与有界退避。
func TestTargetRetry_SnapshotFailures(t *testing.T) {
	realTimeout := realHTTPTimeoutError(t)
	capacity := errors.New("安全组入站规则数将达 101（上限 100），停止新增")
	cases := []struct {
		name                         string
		errs                         []error
		snapshots, creates, resolves int
		outcome                      RoundOutcome
		backoffs                     []time.Duration
	}{
		{"real_http_timeout", []error{realTimeout}, 3, 1, 2, RoundSuccess, []time.Duration{time.Second}},
		{"tencent_wrapped_network", []error{tencentNetworkError("dial tcp: connect: connection refused")}, 3, 1, 2, RoundSuccess, []time.Duration{time.Second}},
		{"non_retryable", []error{capacity}, 1, 0, 1, RoundFailed, nil},
		{"exhausted", []error{realTimeout, realTimeout, realTimeout}, 3, 0, 3, RoundFailed, []time.Duration{time.Second, 2 * time.Second}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := newProbeProvider(config.CloudTCCVM, 1)
			p.snapshotErrs = tt.errs
			rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
			s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
			var resolves int
			base := s.resolveHostFn
			s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) { resolves++; return base(host) }
			var backoffs []time.Duration
			s.sleepFn = func(d time.Duration) {
				if d == time.Second || d == 2*time.Second {
					backoffs = append(backoffs, d)
				}
			}
			s.syncAll()
			snap, create, del := p.counts()
			sum := s.Status().LastRound
			if snap != tt.snapshots || create != tt.creates || del != 0 || resolves != tt.resolves || sum.Outcome != tt.outcome || sum.Added != tt.creates || sum.Deleted != 0 || !reflect.DeepEqual(backoffs, tt.backoffs) {
				t.Fatalf("快照/新增/删除/解析/退避/汇总 = %d/%d/%d/%d/%v/%+v，期望 %d/%d/0/%d/%v/%s", snap, create, del, resolves, backoffs, sum, tt.snapshots, tt.creates, tt.resolves, tt.backoffs, tt.outcome)
			}
		})
	}
}

type targetWriteProgressProbe struct {
	*targetProbeProvider
	mode string
	err  error
}

func (p *targetWriteProgressProbe) CreateRules(snap provider.RuleSnapshot, actions []config.RuleAction) (provider.CreateResult, error) {
	_, creates, _ := p.counts()
	if p.mode == "partial" && creates > 0 {
		return p.targetProbeProvider.CreateRules(snap, actions)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.createCalls++
	p.seq = append(p.seq, fmt.Sprintf("create:%d", len(actions)))
	switch p.mode {
	case "partial":
		p.materialize(actions[:1])
		p.revision++
		return provider.CreateResult{Written: 1}, p.err
	case "external":
		// 模拟并发方满足权限；当前调用没有确认写入，不能计入 added。
		for i := range actions {
			actions[i].Description = "external"
		}
		p.materialize(actions)
		p.revision++
		return provider.CreateResult{}, nil
	case "missing":
		return provider.CreateResult{Skipped: len(actions)}, nil
	}
	panic("未知写入测试模式")
}

// TestTargetRetry_WriteAccounting 只统计确认 Written，跨 attempt 保留进度，S1 仍须证明覆盖。
func TestTargetRetry_WriteAccounting(t *testing.T) {
	for _, tt := range []struct {
		name, mode                string
		err                       error
		added, creates, remaining int
		outcome                   RoundOutcome
	}{
		{"partial_then_retry", "partial", errors.New("RequestLimitExceeded"), 2, 2, 2, RoundSuccess},
		{"partial_then_stop", "partial", errors.New("permission denied"), 1, 1, 1, RoundFailed},
		{"uncovered_provider_skip", "missing", nil, 0, 1, 0, RoundFailed},
		{"externally_covered_zero_written", "external", nil, 0, 1, 2, RoundSuccess},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &targetWriteProgressProbe{targetProbeProvider: newProbeProvider(config.CloudTCCVM, 1), mode: tt.mode, err: tt.err}
			rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "80"), staticRule(2, "a.example.com", "TCP", "443")}
			s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
			s.syncAll()
			sum := s.Status().LastRound
			_, create, del := p.counts()
			if sum.Outcome != tt.outcome || sum.Added != tt.added || create != tt.creates || del != 0 || p.ruleCount() != tt.remaining {
				t.Fatalf("汇总/新增调用/删除调用/剩余规则 = %+v/%d/%d/%d，期望 outcome=%s added=%d calls=%d/0 rules=%d", sum, create, del, p.ruleCount(), tt.outcome, tt.added, tt.creates, tt.remaining)
			}
		})
	}
}

// TestTargetRetry_UnsupportedFinalAttempt 新的有效规划替换旧明细，不重复累加。
func TestTargetRetry_UnsupportedFinalAttempt(t *testing.T) {
	p := newProbeProvider(config.CloudAliSWAS, 1)
	p.createErrs = []error{errors.New("RequestLimitExceeded")}
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443"), staticRule(2, "a.example.com", "TCP", "80")}
	rules[1].Action = "DROP"
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
	state := s.runtime.Snapshot()
	round := newDNSRound(state)
	res := s.syncTarget(state, p, rules, round)
	round.finish()
	if res.outcome != TargetPartial || res.added != 1 || len(res.unsupported) != 1 || res.unsupported[0].Code != provider.IssueUnsupportedAction {
		t.Fatalf("目标结果 = %+v", res)
	}
	if snap, create, del := p.counts(); snap != 3 || create != 2 || del != 0 {
		t.Fatalf("快照/新增/删除调用 = %d/%d/%d，期望 3/2/0", snap, create, del)
	}
}

// TestTargetRetry_IdempotentCreateRequiresCoverage 已存在错误不能替代 S1 覆盖证明。
func TestTargetRetry_IdempotentCreateRequiresCoverage(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	p.createErrs = []error{errors.New("FirewallRulesExist")}
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
	s.syncAll()
	sum := s.Status().LastRound
	if sum.Outcome != RoundFailed || sum.Added != 0 || sum.Deleted != 0 {
		t.Fatalf("幂等新增后仍缺覆盖，结果 = %+v", sum)
	}
}

type targetDeleteProgressProbe struct{ *targetProbeProvider }

func (p *targetDeleteProgressProbe) DeleteRules(snap provider.RuleSnapshot, rules []config.RuleInfo) (provider.DeleteResult, error) {
	_, _, deletes := p.counts()
	if deletes == 0 {
		p.partialDelete = &provider.DeleteResult{Deleted: 1, Resolved: 1}
		res, err := p.targetProbeProvider.DeleteRules(snap, rules)
		p.partialDelete = nil
		return res, err
	}
	return p.targetProbeProvider.DeleteRules(snap, rules)
}

// TestTargetRetry_DeleteProgressAcrossAttempts 部分删除后 S2 失败仍保留确认增删数。
// I8-02 的来源/历史回归在下方独立覆盖；本用例继续守护确认计数。
func TestTargetRetry_DeleteProgressAcrossAttempts(t *testing.T) {
	retryErr := errors.New("RequestLimitExceeded")
	for _, tt := range []struct {
		name                            string
		errs                            []error
		deleted, snapshots, deleteCalls int
		outcome                         RoundOutcome
	}{
		{"s2_fail_then_recover", []error{nil, nil, retryErr}, 2, 6, 2, RoundSuccess},
		{"s2_fail_then_s0_exhausted", []error{nil, nil, retryErr, retryErr, retryErr}, 1, 5, 1, RoundFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &targetDeleteProgressProbe{newProbeProvider(config.CloudAliECS, 1, staleRule("TCP", "80", "10.0.0.1/32", "old-1", ""), staleRule("TCP", "81", "10.0.0.2/32", "old-2", ""))}
			p.snapshotErrs = tt.errs
			rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
			s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
			s.syncAll()
			sum := s.Status().LastRound
			snap, create, del := p.counts()
			if sum.Outcome != tt.outcome || sum.Added != 1 || sum.Deleted != tt.deleted || sum.CleanupDeleted != tt.deleted || (tt.outcome == RoundSuccess && sum.CleanupDeferred != 0) || snap != tt.snapshots || create != 1 || del != tt.deleteCalls {
				t.Fatalf("汇总/快照/新增/删除调用 = %+v/%d/%d/%d", sum, snap, create, del)
			}
		})
	}
}

// I8-02：部分删除的确认数累计，后续 S0/Add/S1 早退不能抹去前次观察。
func TestI802_RetryObservations(t *testing.T) {
	retryErr := errors.New("RequestLimitExceeded")
	for _, mode := range []string{"s0_exhausted", "add_early", "s1_early", "recover", "zero_recover"} {
		t.Run(mode, func(t *testing.T) {
			p := &targetDeleteProgressProbe{newProbeProvider(config.CloudAliECS, 1,
				staleRule("TCP", "80", "10.0.0.1/32", "old1", ""), staleRule("TCP", "81", "10.0.0.2/32", "old2", ""))}
			p.snapshotErrs = []error{nil, nil, retryErr}
			if mode == "s0_exhausted" {
				p.snapshotErrs = append(p.snapshotErrs, retryErr, retryErr)
			}
			if mode == "s1_early" {
				p.snapshotErrs = append(p.snapshotErrs, nil, errors.New("permission denied"))
			}
			if mode == "add_early" {
				p.createErrs = []error{nil, errors.New("permission denied")}
			}
			rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
			s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
			if mode == "zero_recover" {
				s.sleepFn = func(d time.Duration) {
					if d == time.Second {
						p.mu.Lock()
						p.rules = p.rules[1:]
						p.mu.Unlock()
					}
				}
			}
			resolves := 0
			s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
				resolves++
				if mode == "add_early" && resolves > 1 {
					return []dns.ResolvedIP{mockResolved("2.2.2.2/32")}, nil
				}
				return []dns.ResolvedIP{mockResolved("1.1.1.1/32")}, nil
			}
			state := s.runtime.Snapshot()
			round := newDNSRound(state)
			res := s.syncTarget(state, p, rules, round)
			round.finish()
			o := res.cleanupObservation
			if o == nil || o.CandidatesAt.IsZero() || o.DeferredAt.Before(o.CandidatesAt) {
				t.Fatalf("缺少来源时间: %+v", o)
			}
			if mode == "zero_recover" {
				if res.outcome != TargetSuccess || o.Candidates != 0 || o.Deferred != 0 || o.Attempt != 2 || o.Historical || o.Basis != "s1" || res.deleted != 1 {
					t.Fatalf("新零未替换: %+v / %+v", res, o)
				}
			} else if mode == "recover" {
				if res.outcome != TargetSuccess || res.deleted != 2 || res.cleanupDeleted != 2 || o.Attempt != 2 || o.Historical || o.Basis != "s2" || o.Candidates != 1 || o.Deferred != 0 {
					t.Fatalf("恢复结果: %+v observation=%+v", res, o)
				}
			} else {
				attempts := 2
				if mode == "s0_exhausted" {
					attempts = 3
				}
				if res.outcome != TargetFailed || res.attempts != attempts || res.deleted != 1 || res.cleanupDeleted != 1 || o.Attempt != 1 || !o.Historical || o.Basis != "delete_progress" || o.Candidates != 2 || o.Deferred != 1 || res.cleanupDeferred != 1 || p.ruleCount() < 2 {
					t.Fatalf("历史保留失败: %+v observation=%+v", res, o)
				}
				if res.unsupportedObservation.Latest.Historical != (mode == "s0_exhausted") {
					t.Fatalf("独立规划来源错误: %+v", res.unsupportedObservation)
				}
			}
		})
	}
}

func TestI802_UnsupportedObservations(t *testing.T) {
	for _, mode := range []string{"s0_exhausted", "incomplete", "complete_empty"} {
		t.Run(mode, func(t *testing.T) {
			p := newProbeProvider(config.CloudAliECS, 1)
			retryErr := errors.New("RequestLimitExceeded")
			p.createErrs = []error{retryErr}
			if mode == "s0_exhausted" {
				p.snapshotErrs = []error{nil, retryErr, retryErr}
			}
			rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443"), staticRule(2, "b.example.com", "ICMP", "ALL")}
			rules[1].EnableIPv6 = true
			s := newTargetSyncer(t, []provider.Provider{p}, rules, nil)
			calls := 0
			s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
				calls++
				if host == "a.example.com" {
					return []dns.ResolvedIP{mockResolved("1.1.1.1/32")}, nil
				}
				if calls > 2 && mode == "incomplete" {
					return nil, errors.New("DNS unavailable")
				}
				if calls > 2 && mode == "complete_empty" {
					return []dns.ResolvedIP{mockResolved("2.2.2.2/32")}, nil
				}
				return []dns.ResolvedIP{mockResolved("2001:db8::1/128")}, nil
			}
			state := s.runtime.Snapshot()
			round := newDNSRound(state)
			res := s.syncTarget(state, p, rules, round)
			round.finish()
			o := res.unsupportedObservation
			if o == nil || o.Latest == nil || o.LastComplete == nil || o.Latest.ObservedAt.IsZero() {
				t.Fatalf("规划来源缺失: %+v", o)
			}
			switch mode {
			case "s0_exhausted":
				if o.Latest.Attempt != 1 || o.Latest.Stage != "s0" || !o.Latest.Historical || len(o.Latest.Issues) != 1 || len(res.unsupported) != 1 || res.cleanupObservation != nil {
					t.Fatalf("S0失败丢失明细: %+v", o)
				}
			case "incomplete":
				if o.Latest.Attempt != 2 || o.Latest.Stage != "s1" || o.Latest.Complete || o.Latest.Historical || o.Latest.Issues == nil || len(o.Latest.Issues) != 0 || o.LastComplete.Attempt != 1 || !o.LastComplete.Historical || len(o.LastComplete.Issues) != 1 || res.outcome != TargetFailed || res.cleanupObservation == nil || res.cleanupObservation.DesiredComplete {
					t.Fatalf("最新已知与完整历史混淆: %+v / %+v", o, res)
				}
			case "complete_empty":
				if !o.Latest.Complete || o.LastComplete.Attempt != 2 || o.LastComplete.Historical || o.LastComplete.Issues == nil || len(o.LastComplete.Issues) != 0 || res.outcome != TargetSuccess {
					t.Fatalf("完整空规划未替换: %+v", o)
				}
			}
		})
	}
}
