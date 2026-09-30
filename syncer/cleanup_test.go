package syncer

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/notifier"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Issue7 Step 3「四平台条件清理」的 syncer 级判别性用例：
// 清理安全门、删除定位、S2 强制验证、残留计数与失败语义。
//
// 平台请求细节（FirewallVersion / 单请求批量 PolicyIndex+Version / RuleId / 100 分批）
// 由 provider/request_mock_test.go 覆盖；这里只证明调度层是否真的按安全门调用删除。

func staleRule(protocol, port, cidr, ruleID, policyIndex string) config.RuleInfo {
	return config.RuleInfo{
		Protocol: protocol, Port: port, CidrBlock: cidr, Action: "ACCEPT",
		Description: "[auto-dns]", RuleID: ruleID, PolicyIndex: policyIndex,
	}
}

// TestCleanup_LighthouseDeletesStaleOwnedAndVerifiesS2 清理门满足时删除陈旧 Owned 规则，
// 并且删除必须使用 S1 版本、删除后强制 S2 验证。
func TestCleanup_LighthouseDeletesStaleOwnedAndVerifiesS2(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1, staleRule("TCP", "9999", "10.9.9.9/32", "", ""))
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	snapshots, creates, deletes := p.counts()
	if creates != 1 || deletes != 1 {
		t.Fatalf("create=%d delete=%d, want 1/1（新增先于删除且清理门满足）", creates, deletes)
	}
	// S0 + S1 + S2
	if snapshots != 3 {
		t.Fatalf("快照调用 = %d, want 3（发生删除后必须强制 S2）", snapshots)
	}
	// 删除必须携带 S1 版本：create 让版本自增到 2，因此 S1/S2 版本为 2
	revisions := p.deleteRevisionLog()
	if len(revisions) != 1 || revisions[0] != "2" {
		t.Fatalf("删除携带版本 = %v, want [\"2\"]（必须是 S1 版本）", revisions)
	}
	if p.hasRule("10.9.9.9/32", "9999") {
		t.Fatal("陈旧规则必须已被删除")
	}
	if !p.hasRule("1.1.1.1/32", "443") {
		t.Fatal("期望规则必须保留")
	}

	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundSuccess {
		t.Fatalf("清理成功必须 success: %+v", sum)
	}
	if sum.CleanupCandidates != 1 || sum.CleanupDeleted != 1 || sum.CleanupDeferred != 0 {
		t.Fatalf("清理计数 = candidates:%d deleted:%d deferred:%d, want 1/1/0",
			sum.CleanupCandidates, sum.CleanupDeleted, sum.CleanupDeferred)
	}
	if sum.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1（只统计云端确认的实际删除）", sum.Deleted)
	}
}

// TestCleanup_IdempotentNotFoundUsesS2FinalCandidates 验证幂等 NotFound 不虚增实际删除，
// 且成功取得的 S2 planner 是最终残留的唯一来源，而不是继续用 S1-Resolved 间接推导。
func TestCleanup_IdempotentNotFoundUsesS2FinalCandidates(t *testing.T) {
	stale1 := staleRule("TCP", "9998", "10.9.9.8/32", "", "")
	stale2 := staleRule("TCP", "9999", "10.9.9.9/32", "", "")
	newStale1 := staleRule("TCP", "10001", "10.9.10.1/32", "", "")
	newStale2 := staleRule("TCP", "10002", "10.9.10.2/32", "", "")

	tests := []struct {
		name         string
		initialStale []config.RuleInfo
		afterError   []config.RuleInfo
		wantDeferred int
	}{
		{
			name:         "单候选已被并发删除",
			initialStale: []config.RuleInfo{stale1},
			afterError:   []config.RuleInfo{},
			wantDeferred: 0,
		},
		{
			name:         "两候选仅剩一条",
			initialStale: []config.RuleInfo{stale1, stale2},
			afterError:   []config.RuleInfo{stale2},
			wantDeferred: 1,
		},
		{
			name:         "两候选均仍存在",
			initialStale: []config.RuleInfo{stale1, stale2},
			afterError:   []config.RuleInfo{stale1, stale2},
			wantDeferred: 2,
		},
		{
			name:         "S2 新发现两条并发残留",
			initialStale: []config.RuleInfo{stale1},
			afterError:   []config.RuleInfo{newStale1, newStale2},
			wantDeferred: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newProbeProvider(config.CloudTCLighthouse, 1, tc.initialStale...)
			p.deleteErr = errors.New("ResourceNotFound.FirewallRulesNotFound")
			p.deleteErrorHook = func() {
				// 保留 Create 后已由 S1 证明的期望规则，再模拟并发者改变陈旧规则集合。
				kept := make([]config.RuleInfo, 0, 1+len(tc.afterError))
				for _, r := range p.rules {
					if r.Port == "443" {
						kept = append(kept, r)
					}
				}
				p.rules = append(kept, tc.afterError...)
			}
			rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
			s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

			s.syncAll()

			snapshots, _, deletes := p.counts()
			if snapshots != 3 || deletes != 1 {
				t.Fatalf("snapshot/delete = %d/%d, want 3/1（NotFound 后仍必须取得 S2）", snapshots, deletes)
			}
			sum := s.Status().LastRound
			if sum == nil || sum.Outcome != RoundSuccess {
				t.Fatalf("幂等 NotFound 且 S2 覆盖成立必须 success: %+v", sum)
			}
			if sum.CleanupCandidates != len(tc.initialStale) || sum.CleanupDeleted != 0 ||
				sum.Deleted != 0 || sum.CleanupDeferred != tc.wantDeferred {
				t.Fatalf("清理计数 = candidates:%d cleanup_deleted:%d deleted:%d deferred:%d, want %d/0/0/%d",
					sum.CleanupCandidates, sum.CleanupDeleted, sum.Deleted, sum.CleanupDeferred,
					len(tc.initialStale), tc.wantDeferred)
			}
		})
	}
}

// TestCleanup_IdempotentNotFoundStillFailsWhenS2Untrusted 验证幂等错误不能吞掉 S2
// Describe/覆盖失败；失败时不得把不可信 S2 当成最终残留，继续保留 S1 fallback。
func TestCleanup_IdempotentNotFoundStillFailsWhenS2Untrusted(t *testing.T) {
	tests := []struct {
		name          string
		snapshotError error
		removeDesired bool
	}{
		{name: "S2 Describe 失败", snapshotError: errors.New("模拟 S2 Describe 失败")},
		{name: "S2 覆盖失败", removeDesired: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newProbeProvider(config.CloudTCLighthouse, 1,
				staleRule("TCP", "9999", "10.9.9.9/32", "", ""))
			p.deleteErr = errors.New("ResourceNotFound.FirewallRulesNotFound")
			p.deleteErrorHook = func() {
				if tc.removeDesired {
					p.rules = nil
					return
				}
				kept := p.rules[:0]
				for _, r := range p.rules {
					if r.Port == "443" {
						kept = append(kept, r)
					}
				}
				p.rules = kept
			}
			if tc.snapshotError != nil {
				p.snapshotErrs = []error{nil, nil, tc.snapshotError}
			}
			rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
			s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
			errorEvents := make(chan notifier.Event, 1)
			s.bus.Subscribe(notifier.EventSyncError, roundEventSink{ch: errorEvents})

			s.syncAll()

			sum := s.Status().LastRound
			if sum == nil || sum.Outcome != RoundFailed || sum.Failed != 1 {
				t.Fatalf("S2 不可信必须 failed: %+v", sum)
			}
			if sum.CleanupCandidates != 1 || sum.CleanupDeleted != 0 || sum.CleanupDeferred != 1 {
				t.Fatalf("失败 fallback = candidates:%d deleted:%d deferred:%d, want 1/0/1",
					sum.CleanupCandidates, sum.CleanupDeleted, sum.CleanupDeferred)
			}
			select {
			case <-errorEvents:
			case <-time.After(time.Second):
				t.Fatal("S2 失败后未发布目标同步错误事件")
			}
		})
	}
}

// TestCleanup_GateClosedKeepsCandidates 任一安全门未满足时删除调用必须为 0。
func TestCleanup_GateClosedKeepsCandidates(t *testing.T) {
	stale := staleRule("TCP", "9999", "10.9.9.9/32", "r-stale", "")

	cases := []struct {
		name       string
		ct         config.CloudType
		rules      []config.DomainRule
		resolve    map[string]string
		snapshot   []config.RuleInfo
		wantReason string
	}{
		{
			// 适用规则存在但解析结果为空 → Desired 为空 → 不授权清空 TAG
			name: "空期望集", ct: config.CloudAliSWAS,
			rules:    []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")},
			resolve:  map[string]string{"a.example.com": ""},
			snapshot: []config.RuleInfo{stale},
		},
		{
			name: "DNS 解析失败", ct: config.CloudAliSWAS,
			rules:   []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")},
			resolve: map[string]string{}, snapshot: []config.RuleInfo{stale},
			wantReason: provider.IssueDNSFailed,
		},
		{
			name: "平台能力不可实施", ct: config.CloudAliSWAS,
			rules:   []config.DomainRule{{ID: 1, Host: "a.example.com", Protocol: "TCP", Ports: "443", Action: "DROP"}},
			resolve: map[string]string{"a.example.com": "1.1.1.1/32"}, snapshot: []config.RuleInfo{stale},
			wantReason: provider.IssueUnsupportedAction,
		},
		{
			name: "快照字段冲突", ct: config.CloudAliSWAS,
			rules:   []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")},
			resolve: map[string]string{"a.example.com": "1.1.1.1/32"},
			snapshot: []config.RuleInfo{
				stale,
				{Protocol: "TCP", Port: "1", CidrBlock: "9.9.9.9/32", Ipv6CidrBlock: "2001:db8::/128", Action: "ACCEPT", Description: "[auto-dns]"},
			},
			wantReason: provider.IssueSnapshotRuleInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newProbeProvider(tc.ct, 1, tc.snapshot...)
			s := newTargetSyncer(t, []provider.Provider{p}, tc.rules, tc.resolve)
			s.syncAll()

			if _, _, deletes := p.counts(); deletes != 0 {
				t.Fatalf("安全门未满足时删除调用必须为 0，实际 %d（序列 %v）", deletes, p.callSeq())
			}
			if !p.hasRule("10.9.9.9/32", "9999") {
				t.Fatal("安全门未满足时陈旧规则必须保留")
			}
			sum := s.Status().LastRound
			if sum == nil || sum.CleanupDeferred != 1 {
				t.Fatalf("残留候选必须计入 cleanup_deferred: %+v", sum)
			}
		})
	}
}

// TestCleanup_NonTAGRulesNeverDeleted 非当前 TAG 与 [TAG]foo 规则永不进入删除。
func TestCleanup_NonTAGRulesNeverDeleted(t *testing.T) {
	fakeTwin := staleRule("TCP", "9998", "10.9.9.8/32", "r-fake", "")
	fakeTwin.Description = "[auto-dns]foo" // 紧贴后缀：不属于当前命名空间
	other := staleRule("TCP", "9997", "10.9.9.7/32", "r-other", "")
	other.Description = "[other] 手工规则"
	p := newProbeProvider(config.CloudTCLighthouse, 1, fakeTwin, other)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	if _, _, deletes := p.counts(); deletes != 0 {
		t.Fatalf("非当前 TAG 规则绝不能被删除，实际删除调用 %d", deletes)
	}
	if !p.hasRule("10.9.9.8/32", "9998") || !p.hasRule("10.9.9.7/32", "9997") {
		t.Fatal("非当前 TAG 规则必须原样保留")
	}
	if sum := s.Status().LastRound; sum == nil || sum.CleanupCandidates != 0 {
		t.Fatalf("非当前 TAG 规则不得成为清理候选: %+v", sum)
	}
}

// TestCleanup_LighthouseAmbiguousCandidateDeferred 同 key 不唯一时绝不删除。
func TestCleanup_LighthouseAmbiguousCandidateDeferred(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1,
		staleRule("TCP", "9999", "10.9.9.9/32", "", ""),
		staleRule("TCP", "9999", "10.9.9.9/32", "", ""),
	)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	if _, _, deletes := p.counts(); deletes != 0 {
		t.Fatalf("Lighthouse 同 key 歧义时不得删除，实际 %d", deletes)
	}
	if p.ruleCount() != 3 { // 2 条歧义 + 1 条新增
		t.Fatalf("歧义规则必须保留，云端规则数 = %d, want 3", p.ruleCount())
	}
	sum := s.Status().LastRound
	if sum == nil || sum.CleanupCandidates != 2 || sum.CleanupDeferred != 2 {
		t.Fatalf("歧义候选必须全部 deferred: %+v", sum)
	}
}

// TestCleanup_FailureKeepsSuccessAndDefers 清理请求失败不得把已确认权限改成 partial/failed。
func TestCleanup_FailureKeepsSuccessAndDefers(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1, staleRule("TCP", "9999", "10.9.9.9/32", "", ""))
	p.deleteErr = errors.New("permission denied")
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundSuccess {
		t.Fatalf("清理失败后目标仍必须 success: %+v", sum)
	}
	if sum.CleanupDeleted != 0 || sum.CleanupDeferred != 1 {
		t.Fatalf("清理失败计数 = deleted:%d deferred:%d, want 0/1", sum.CleanupDeleted, sum.CleanupDeferred)
	}
	if !p.hasRule("10.9.9.9/32", "9999") {
		t.Fatal("清理失败时残留必须保留")
	}
	if !p.hasRule("1.1.1.1/32", "443") {
		t.Fatal("新增的期望规则不得受影响")
	}
}

// TestCleanup_RetryableFailureExhaustedKeepsSuccessAndDefers 验证 R7-01：
// 每次 attempt 均已由 S1 证明所需功能存在，只有 Delete 可重试错误时仍完成三次
// 整目标重试；重试耗尽后不得把清理残留升级成 failed/unhealthy。
func TestCleanup_RetryableFailureExhaustedKeepsSuccessAndDefers(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "限流", err: errors.New("RequestLimitExceeded")},
		{name: "版本竞争", err: errors.New("UnsupportedOperation.FirewallVersionMismatch")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newProbeProvider(config.CloudTCLighthouse, 1, staleRule("TCP", "9999", "10.9.9.9/32", "", ""))
			p.deleteErr = tc.err
			rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
			s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
			completedEvents := make(chan notifier.Event, 1)
			errorEvents := make(chan notifier.Event, 1)
			s.bus.Subscribe(notifier.EventTargetSyncComplete, roundEventSink{ch: completedEvents})
			s.bus.Subscribe(notifier.EventSyncError, roundEventSink{ch: errorEvents})
			var sleeps []time.Duration
			s.sleepFn = func(d time.Duration) { sleeps = append(sleeps, d) }

			s.syncAll()

			snapshots, creates, deletes := p.counts()
			if snapshots != 6 || creates != 1 || deletes != maxRetries {
				t.Fatalf("snapshot/create/delete = %d/%d/%d, want 6/1/%d（每次必须从新快照重新规划）",
					snapshots, creates, deletes, maxRetries)
			}
			if len(sleeps) < 2 || sleeps[0] != time.Second || sleeps[1] != 2*time.Second {
				t.Fatalf("退避序列 = %v, want 前两项 [1s 2s]", sleeps)
			}
			if got := p.deleteRevisionLog(); !reflect.DeepEqual(got, []string{"2", "2", "2"}) {
				t.Fatalf("删除版本 = %v, want [2 2 2]（每次都必须使用当次 S1 revision）", got)
			}

			sum := s.Status().LastRound
			if sum == nil || sum.Outcome != RoundSuccess || sum.Failed != 0 || sum.Changed != 1 {
				t.Fatalf("清理重试耗尽仍必须 success 且保留已确认新增: %+v", sum)
			}
			if sum.CleanupCandidates != 1 || sum.CleanupDeleted != 0 || sum.CleanupDeferred != 1 {
				t.Fatalf("清理计数 = candidates:%d deleted:%d deferred:%d, want 1/0/1",
					sum.CleanupCandidates, sum.CleanupDeleted, sum.CleanupDeferred)
			}
			if s.Status().LastSuccess == nil {
				t.Fatal("success + cleanup_deferred 必须刷新 last_success，以保持 operational health healthy")
			}
			if !p.hasRule("10.9.9.9/32", "9999") || !p.hasRule("1.1.1.1/32", "443") {
				t.Fatal("清理重试耗尽后必须同时保留残留与已确认的期望规则")
			}
			select {
			case ev := <-completedEvents:
				if ev.Data["outcome"] != string(TargetSuccess) {
					t.Fatalf("目标完成事件 outcome = %v, want success", ev.Data["outcome"])
				}
			case <-time.After(time.Second):
				t.Fatal("清理重试耗尽后未发布目标完成事件")
			}
			select {
			case ev := <-errorEvents:
				t.Fatalf("清理重试耗尽后不得发布同步失败事件: %+v", ev)
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

// TestCleanup_RetryableFailureThenSuccessKeepsWholeTargetRetry 验证修复不得取消重试：
// 前两次 Delete 失败、第三次成功时，仍应删除候选并经 S2 收敛。
func TestCleanup_RetryableFailureThenSuccessKeepsWholeTargetRetry(t *testing.T) {
	retryErr := errors.New("RequestLimitExceeded")
	p := newProbeProvider(config.CloudTCLighthouse, 1, staleRule("TCP", "9999", "10.9.9.9/32", "", ""))
	p.deleteErrs = []error{retryErr, retryErr, nil}
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	snapshots, creates, deletes := p.counts()
	if snapshots != 7 || creates != 1 || deletes != maxRetries {
		t.Fatalf("snapshot/create/delete = %d/%d/%d, want 7/1/%d（第三次成功后必须补 S2）",
			snapshots, creates, deletes, maxRetries)
	}
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundSuccess || sum.CleanupDeleted != 1 || sum.CleanupDeferred != 0 {
		t.Fatalf("第三次清理成功必须正常收敛: %+v", sum)
	}
	if p.hasRule("10.9.9.9/32", "9999") || !p.hasRule("1.1.1.1/32", "443") {
		t.Fatal("第三次成功后必须删除陈旧规则并保留期望规则")
	}
}

// TestCleanup_S2MissingCoverageFails S2 不再覆盖所需功能时必须 failed 并记录高优先级错误。
func TestCleanup_S2MissingCoverageFails(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1, staleRule("TCP", "9999", "10.9.9.9/32", "", ""))
	// 模拟清理把期望规则一并抹掉：删除调用后云端只剩陈旧规则被移除、新增规则也被移除
	p.afterDelete = func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		kept := make([]config.RuleInfo, 0, len(p.rules))
		for _, r := range p.rules {
			if r.Port == "9999" {
				continue
			}
			kept = append(kept, r) // 期望规则也被移除
		}
		p.rules = nil
		_ = kept
	}
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundFailed {
		t.Fatalf("S2 覆盖验证失败必须 failed: %+v", sum)
	}
}

// TestCleanup_CSMUsesS1PolicyIndexAndRevision CVM 删除必须使用同一 S1 的 PolicyIndex 与 Version。
func TestCleanup_CSMUsesS1PolicyIndexAndRevision(t *testing.T) {
	p := newProbeProvider(config.CloudTCCVM, 1, staleRule("TCP", "9999", "10.9.9.9/32", "", "7"))
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	_, _, deletes := p.counts()
	if deletes != 1 {
		t.Fatalf("CVM 删除调用 = %d, want 1", deletes)
	}
	revisions := p.deleteRevisionLog()
	if len(revisions) != 1 || revisions[0] != "2" {
		t.Fatalf("CVM 删除版本 = %v, want [\"2\"]（同一 S1 版本）", revisions)
	}
	if p.hasRule("10.9.9.9/32", "9999") {
		t.Fatal("陈旧规则必须已删除")
	}
}

// TestCleanup_SWASUsesRuleIDPath SWAS 删除必须携带 S1 回读的 RuleId 并完成 S2。
func TestCleanup_SWASUsesRuleIDPath(t *testing.T) {
	p := newProbeProvider(config.CloudAliSWAS, 1, staleRule("TCP", "9999", "10.9.9.9/32", "r-stale", ""))
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	snapshots, _, deletes := p.counts()
	if deletes != 1 || snapshots != 3 {
		t.Fatalf("SWAS 删除调用 = %d, 快照 = %d, want 1/3（删除后强制 S2）", deletes, snapshots)
	}
	if p.hasRule("10.9.9.9/32", "9999") {
		t.Fatal("陈旧规则必须已删除")
	}
}

// TestCleanup_ECSCandidatesHandedOverForBatching ECS 的 150 个候选必须整体交给 Provider
// （分批 100 + 50 由 Provider 内部完成，见 provider/request_mock_test.go
// TestRequest_ECSDeleteBatches100），并完成 S2 与残留计数。
func TestCleanup_ECSDeletesInBatchesOf100(t *testing.T) {
	stale := make([]config.RuleInfo, 0, 150)
	for i := 0; i < 150; i++ {
		stale = append(stale, staleRule("TCP", fmt.Sprintf("%d", 20000+i), fmt.Sprintf("10.%d.%d.%d/32", i/65536, (i/256)%256, i%256), fmt.Sprintf("r-%03d", i), ""))
	}
	p := newProbeProvider(config.CloudAliECS, 1, stale...)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	deletes := p.deleteSizes()
	if len(deletes) != 1 || deletes[0] != 150 {
		t.Fatalf("调度层应把全部候选一次交给 Provider，实际 %v", deletes)
	}
	if snapshots, _, _ := p.counts(); snapshots != 3 {
		t.Fatalf("快照调用 = %d, want 3（删除后强制 S2）", snapshots)
	}
	if got := p.ruleCount(); got != 1 {
		t.Fatalf("清理后云端规则数 = %d, want 1（只留期望规则）", got)
	}
	sum := s.Status().LastRound
	if sum == nil || sum.CleanupDeleted != 150 || sum.CleanupDeferred != 0 {
		t.Fatalf("ECS 清理计数 = %+v, want deleted=150 deferred=0", sum)
	}
}

// TestCleanup_PartialDeleteKeepsConfirmedAndDefersRest 部分删除（后续批次失败）时：
// 已确认批次必须保留计数，剩余候选进入 cleanup_deferred，且仍强制 S2；
// 所需权限已由 S1 证明，目标结论保持 success。
func TestCleanup_PartialDeleteKeepsConfirmedAndDefersRest(t *testing.T) {
	stale := make([]config.RuleInfo, 0, 150)
	for i := 0; i < 150; i++ {
		stale = append(stale, staleRule("TCP", fmt.Sprintf("%d", 20000+i),
			fmt.Sprintf("10.%d.%d.%d/32", i/65536, (i/256)%256, i%256), fmt.Sprintf("r-%03d", i), ""))
	}
	p := newProbeProvider(config.CloudAliECS, 1, stale...)
	p.partialDelete = &provider.DeleteResult{Deleted: 100, Resolved: 100}
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	snapshots, _, deletes := p.counts()
	if deletes != 1 {
		t.Fatalf("删除调用 = %d, want 1", deletes)
	}
	if snapshots != 3 {
		t.Fatalf("快照调用 = %d, want 3（部分删除后仍必须 S2）", snapshots)
	}
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundSuccess {
		t.Fatalf("部分删除后所需权限仍已确认，目标必须 success: %+v", sum)
	}
	if sum.CleanupDeleted != 100 || sum.CleanupDeferred != 50 {
		t.Fatalf("清理计数 = deleted:%d deferred:%d, want 100/50（保留已确认批次，剩余残留）",
			sum.CleanupDeleted, sum.CleanupDeferred)
	}
	if sum.Deleted != 100 {
		t.Fatalf("deleted = %d, want 100（只统计云端确认的实际删除）", sum.Deleted)
	}
}
