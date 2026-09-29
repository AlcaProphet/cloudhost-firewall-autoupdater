package syncer

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Issue7 Step 2「目标级先增后验主流程」判别性用例。
//
// 证据边界：全部为本地 mock Provider 的行为断言，只证明调度单元、调用顺序、
// 覆盖验证与重试语义；不构成任何真实云证据（真实云见 ProdTestList.md PT-I7）。

// targetProbeProvider 模拟一个会随写入改变的云目标：Create 成功后规则在后续快照可见。
type targetProbeProvider struct {
	cloudType   config.CloudType
	targetIndex int

	mu            sync.Mutex
	rules         []config.RuleInfo
	revision      int
	seq           []string
	snapshotCalls int
	createCalls   int
	deleteCalls   int

	deleteErr       error    // 非 nil 时删除调用直接失败
	deleteErrs      []error  // 按 delete 调用序号（0 基）返回；用于验证清理重试收敛
	deleteRevisions []string // 每次删除调用携带的快照版本（版本保护判别用）
	deleteSizeLog   []int    // 每次删除调用的候选条数（分批判别用）
	// partialDelete 非 nil 时模拟「部分批次成功」：返回该结果与 *PartialDeleteError
	partialDelete      *provider.DeleteResult
	afterDelete        func()  // 删除调用后的云端突变（模拟清理误伤/并发变化）
	createErrs         []error // 按 create 调用序号（0 基）返回
	snapshotErrs       []error // 按 snapshot 调用序号（0 基）返回
	createVisible      bool    // Create 调用后规则是否在后续快照可见（默认 true）
	materializeOnError bool    // 幂等「已存在」场景：错误返回时规则仍可见
}

func newProbeProvider(ct config.CloudType, targetIndex int, rules ...config.RuleInfo) *targetProbeProvider {
	return &targetProbeProvider{cloudType: ct, targetIndex: targetIndex, rules: rules, revision: 1, createVisible: true}
}

func (p *targetProbeProvider) Name() string                { return fmt.Sprintf("probe(%d)", p.targetIndex) }
func (p *targetProbeProvider) CloudType() config.CloudType { return p.cloudType }
func (p *targetProbeProvider) TargetIndex() int            { return p.targetIndex }

func (p *targetProbeProvider) GetSnapshot() (provider.RuleSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.snapshotCalls
	p.snapshotCalls++
	p.seq = append(p.seq, "snapshot")
	if n < len(p.snapshotErrs) && p.snapshotErrs[n] != nil {
		return provider.RuleSnapshot{}, p.snapshotErrs[n]
	}
	return provider.RuleSnapshot{
		Rules:    append([]config.RuleInfo(nil), p.rules...),
		Revision: strconv.Itoa(p.revision),
	}, nil
}

func (p *targetProbeProvider) GetRules() ([]config.RuleInfo, error) {
	snap, err := p.GetSnapshot()
	if err != nil {
		return nil, err
	}
	return snap.Rules, nil
}

func (p *targetProbeProvider) CreateRules(_ provider.RuleSnapshot, rules []config.RuleAction) (provider.CreateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.createCalls
	p.createCalls++
	p.seq = append(p.seq, fmt.Sprintf("create:%d", len(rules)))
	if n < len(p.createErrs) && p.createErrs[n] != nil {
		// 幂等「已存在」场景：云端本就有等价规则，后续快照可见
		if p.createVisible && p.materializeOnError {
			p.materialize(rules)
		}
		return provider.CreateResult{}, p.createErrs[n]
	}
	if p.createVisible {
		p.materialize(rules)
	}
	p.revision++
	return provider.CreateResult{Written: len(rules)}, nil
}

// materialize 把已写入的期望规则变成后续快照可见的云端规则。
func (p *targetProbeProvider) materialize(rules []config.RuleAction) {
	for _, r := range rules {
		exists := false
		for _, cur := range p.rules {
			if cur.CidrBlock == r.CidrBlock && cur.Ipv6CidrBlock == r.Ipv6CidrBlock &&
				strings.EqualFold(cur.Protocol, r.Protocol) && cur.Port == r.Port {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		p.rules = append(p.rules, config.RuleInfo{
			Protocol: r.Protocol, Port: r.Port, CidrBlock: r.CidrBlock, Ipv6CidrBlock: r.Ipv6CidrBlock,
			Action: r.Action, Description: r.Description, RuleID: fmt.Sprintf("r%d", len(p.rules)+1),
		})
	}
}

func (p *targetProbeProvider) DeleteRules(snapshot provider.RuleSnapshot, rules []config.RuleInfo) (provider.DeleteResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.deleteCalls
	p.deleteCalls++
	p.deleteRevisions = append(p.deleteRevisions, snapshot.Revision)
	p.deleteSizeLog = append(p.deleteSizeLog, len(rules))
	p.seq = append(p.seq, fmt.Sprintf("delete:%d", len(rules)))
	if n < len(p.deleteErrs) && p.deleteErrs[n] != nil {
		return provider.DeleteResult{}, p.deleteErrs[n]
	}
	if p.deleteErr != nil {
		return provider.DeleteResult{}, p.deleteErr
	}
	if p.partialDelete != nil {
		res := *p.partialDelete
		// 已确认批次对应的规则从云端移除（模拟真实部分删除）
		for i := 0; i < res.Resolved && i < len(rules); i++ {
			for j, cur := range p.rules {
				if cur.RuleID == rules[i].RuleID {
					p.rules = append(p.rules[:j], p.rules[j+1:]...)
					break
				}
			}
		}
		return res, &provider.PartialDeleteError{Deleted: res.Deleted, Err: errors.New("模拟第二批失败")}
	}
	// 模拟云端真实删除：命中的规则从后续快照中消失
	deleted := 0
	for _, target := range rules {
		for i, cur := range p.rules {
			if cur.Port == target.Port && cur.CidrBlock == target.CidrBlock &&
				cur.Ipv6CidrBlock == target.Ipv6CidrBlock && strings.EqualFold(cur.Protocol, target.Protocol) {
				p.rules = append(p.rules[:i], p.rules[i+1:]...)
				deleted++
				break
			}
		}
	}
	if p.afterDelete != nil {
		p.mu.Unlock()
		p.afterDelete()
		p.mu.Lock()
	}
	return provider.DeleteResult{Deleted: deleted, Resolved: len(rules)}, nil
}

func (p *targetProbeProvider) ConvertPorts(port string) []string {
	return provider.ExpandPorts(p.cloudType, port)
}

func (p *targetProbeProvider) counts() (snapshot, create, del int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotCalls, p.createCalls, p.deleteCalls
}

func (p *targetProbeProvider) callSeq() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seq...)
}

func (p *targetProbeProvider) deleteSizes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.deleteSizeLog...)
}

func (p *targetProbeProvider) deleteRevisionLog() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.deleteRevisions...)
}

func (p *targetProbeProvider) ruleCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.rules)
}

func (p *targetProbeProvider) hasRule(cidr, port string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.rules {
		if r.CidrBlock == cidr && r.Port == port {
			return true
		}
	}
	return false
}

// newTargetSyncer 构造只含测试接缝的 Syncer（DNS 结果确定、退避不真实等待）。
func newTargetSyncer(t *testing.T, providers []provider.Provider, rules []config.DomainRule, resolve map[string]string) *Syncer {
	t.Helper()
	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light",
		DomainRules: rules,
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	st.Providers = providers
	s := New(NewRuntimeManager(st))
	s.sleepFn = func(time.Duration) {} // 单测不真实等待（退避与云厂商限速共用）
	s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
		cidr, ok := resolve[strings.ToLower(host)]
		if !ok {
			return nil, fmt.Errorf("mock 未配置域名 %q", host)
		}
		if cidr == "" {
			return nil, nil // 解析成功但结果为空（dns_empty）
		}
		return []dns.ResolvedIP{mockResolved(cidr)}, nil
	}
	return s
}

// mockResolved 把 "1.1.1.1/32" / "2001:db8::1/128" 转成确定性解析结果。
func mockResolved(cidr string) dns.ResolvedIP {
	ipStr := strings.TrimSuffix(strings.TrimSuffix(cidr, "/32"), "/128")
	return dns.ResolvedIP{IP: net.ParseIP(ipStr), IsIPv6: strings.Contains(ipStr, ":")}
}

func staticRule(id int, host, protocol, ports string) config.DomainRule {
	return config.DomainRule{ID: id, Host: host, Protocol: protocol, Ports: ports, Action: "ACCEPT"}
}

// TestTargetRound_AddBeforeDeleteOrder 先增后验不变式：
// 任何 Delete 都必须发生在 Create 与紧随其后的 S1 覆盖验证之后；
// 陈旧 Owned 规则只有在清理门满足时才被删除（Step 3 起），并计入 cleanup_deleted。
func TestTargetRound_AddBeforeDeleteOrder(t *testing.T) {
	stale := config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"}
	p := newProbeProvider(config.CloudTCLighthouse, 1, stale)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	_, creates, deletes := p.counts()
	if creates != 1 {
		t.Fatalf("Create 调用 = %d, want 1", creates)
	}
	if deletes != 1 {
		t.Fatalf("清理门满足时必须删除陈旧 Owned 规则，实际删除调用 %d（序列 %v）", deletes, p.callSeq())
	}
	if !p.hasRule("1.1.1.1/32", "443") {
		t.Fatalf("新 IP 规则必须已创建: %+v", p.callSeq())
	}

	// 顺序不变式：S0 → create → S1 → delete → S2
	wantSeq := []string{"snapshot", "create:1", "snapshot", "delete:1", "snapshot"}
	got := p.callSeq()
	if len(got) != len(wantSeq) {
		t.Fatalf("调用序列 = %v, want %v", got, wantSeq)
	}
	for i := range wantSeq {
		if got[i] != wantSeq[i] {
			t.Fatalf("调用序列 = %v, want %v（Add 与 S1 覆盖验证必须早于任何 Delete）", got, wantSeq)
		}
	}

	sum := s.Status().LastRound
	if sum == nil {
		t.Fatal("缺少整轮汇总")
	}
	if sum.CleanupCandidates != 1 || sum.CleanupDeleted != 1 || sum.CleanupDeferred != 0 {
		t.Fatalf("清理计数 = candidates:%d deleted:%d deferred:%d, want 1/1/0",
			sum.CleanupCandidates, sum.CleanupDeleted, sum.CleanupDeferred)
	}
	if sum.Outcome != RoundSuccess {
		t.Fatalf("outcome = %q, want success", sum.Outcome)
	}
}

// TestTargetRound_AddFailureKeepsOldRules Add 失败时旧规则必须全保留。
func TestTargetRound_AddFailureKeepsOldRules(t *testing.T) {
	stale := config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"}
	p := newProbeProvider(config.CloudTCLighthouse, 1, stale)
	p.createErrs = []error{errors.New("permission denied")}
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	_, _, deletes := p.counts()
	if deletes != 0 {
		t.Fatalf("Add 失败后删除调用必须为 0，实际 %d（序列 %v）", deletes, p.callSeq())
	}
	if !p.hasRule("10.9.9.9/32", "9999") {
		t.Fatal("Add 失败后旧规则必须保留")
	}
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundFailed {
		t.Fatalf("Add 失败必须使目标 failed: %+v", sum)
	}
	if sum.Failed != 1 {
		t.Fatalf("failed 目标数 = %d, want 1", sum.Failed)
	}
}

// TestTargetRound_S1MissingCoverageFails 新增后覆盖验证失败必须 failed 且零删除。
func TestTargetRound_S1MissingCoverageFails(t *testing.T) {
	stale := config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"}
	p := newProbeProvider(config.CloudTCLighthouse, 1, stale)
	p.createVisible = false // 云端未真正生效：S1 看不到新增规则
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	snapshots, _, deletes := p.counts()
	if snapshots < 2 {
		t.Fatalf("Add 后必须重新取得 S1（快照调用 = %d, want >= 2）", snapshots)
	}
	if deletes != 0 {
		t.Fatalf("覆盖验证失败时删除调用必须为 0，实际 %d", deletes)
	}
	if !p.hasRule("10.9.9.9/32", "9999") {
		t.Fatal("覆盖验证失败时旧规则必须保留")
	}
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundFailed || sum.Failed != 1 {
		t.Fatalf("S1 缺覆盖必须 failed: %+v", sum)
	}
}

// TestTargetRound_ExternalSatisfiedNoCreate External 精确满足时期望零 Create。
func TestTargetRound_ExternalSatisfiedNoCreate(t *testing.T) {
	external := config.RuleInfo{
		Protocol: "TCP", Port: "443", CidrBlock: "1.1.1.1/32", Action: "ACCEPT", Description: "手工放行",
	}
	p := newProbeProvider(config.CloudTCLighthouse, 1, external)
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	if _, creates, deletes := p.counts(); creates != 0 || deletes != 0 {
		t.Fatalf("External 精确满足时不得写入: create=%d delete=%d", creates, deletes)
	}
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundSuccess || sum.OK != 1 {
		t.Fatalf("External 满足必须记为 ok 目标: %+v", sum)
	}
	if sum.Changed != 0 {
		t.Fatalf("changed = %d, want 0", sum.Changed)
	}
}

// TestTargetRound_VersionMismatchRetriesWholeTarget 版本竞争必须整目标重试（重新解析/S0/Plan）。
func TestTargetRound_VersionMismatchRetriesWholeTarget(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	p.createErrs = []error{errors.New("UnsupportedOperation.FirewallVersionMismatch")}
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
	resolves := 0
	base := s.resolveHostFn
	s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
		resolves++
		return base(host)
	}

	s.syncAll()

	snapshots, creates, deletes := p.counts()
	if creates != 2 {
		t.Fatalf("Create 调用 = %d, want 2（版本竞争后整目标重试）", creates)
	}
	// attempt 1 在 Create 失败后即中止（未发生写入，不取 S1）；attempt 2 取 S0 + S1。
	if snapshots != 3 {
		t.Fatalf("快照调用 = %d, want 3（失败 attempt 只取 S0，成功 attempt 取 S0+S1）", snapshots)
	}
	if resolves != 2 {
		t.Fatalf("Resolve 调用 = %d, want 2（每个 attempt 重新解析）", resolves)
	}
	if deletes != 0 {
		t.Fatalf("删除调用 = %d, want 0", deletes)
	}
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundSuccess {
		t.Fatalf("重试成功后必须 success: %+v", sum)
	}
	if sum.Added != 1 {
		t.Fatalf("added = %d, want 1（已确认写入不得重复计数）", sum.Added)
	}
}

// TestTargetRound_ResolveOncePerHostPerAttempt 同一 attempt 内同一 host 只解析一次。
func TestTargetRound_ResolveOncePerHostPerAttempt(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	rules := []config.DomainRule{
		staticRule(1, "a.example.com", "TCP", "443"),
		staticRule(2, "a.example.com", "UDP", "443"),
		staticRule(3, "a.example.com", "TCP", "8000-8010"),
	}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})
	resolves := 0
	base := s.resolveHostFn
	s.resolveHostFn = func(host string) ([]dns.ResolvedIP, error) {
		resolves++
		return base(host)
	}

	s.syncAll()

	if resolves != 1 {
		t.Fatalf("同一 attempt 内同 host 解析次数 = %d, want 1", resolves)
	}
	if _, creates, _ := p.counts(); creates != 1 {
		t.Fatalf("三条期望功能应合并为一次目标级 Create，实际 %d 次", creates)
	}
}

// TestTargetRound_ThreeAttemptsWithBackoff 最多三次、退避 1s/2s（通过接缝验证，不真实等待）。
func TestTargetRound_ThreeAttemptsWithBackoff(t *testing.T) {
	p := newProbeProvider(config.CloudTCLighthouse, 1)
	p.createErrs = []error{
		errors.New("RequestLimitExceeded"),
		errors.New("RequestLimitExceeded"),
		errors.New("RequestLimitExceeded"),
	}
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	rc := config.RuntimeConfig{
		Tag: "auto-dns", Interval: time.Hour, DNS: "8.8.8.8", DNSTimeout: time.Second,
		DNSFailThreshold: 5, LogLevel: "info", SyncEnabled: true, Theme: "light", DomainRules: rules,
	}
	st, err := BuildRuntimeState(nil, rc, BreakerReset)
	if err != nil {
		t.Fatalf("构造运行时状态失败: %v", err)
	}
	st.Providers = []provider.Provider{p}
	s := New(NewRuntimeManager(st))
	s.resolveHostFn = func(string) ([]dns.ResolvedIP, error) {
		return []dns.ResolvedIP{mockResolved("1.1.1.1/32")}, nil
	}
	var backoffs []time.Duration
	s.sleepFn = func(d time.Duration) { backoffs = append(backoffs, d) }

	s.syncAll()

	if _, creates, _ := p.counts(); creates != maxRetries {
		t.Fatalf("Create 调用 = %d, want %d（最多三次 attempt）", creates, maxRetries)
	}
	// 前两项必须是重试退避；其后可能跟随目标间云厂商限速（本用例经 sleepFn 拦截，不真实等待）。
	if len(backoffs) < 2 || backoffs[0] != time.Second || backoffs[1] != 2*time.Second {
		t.Fatalf("退避序列 = %v, want 前两项 [1s 2s]", backoffs)
	}
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundFailed {
		t.Fatalf("三次仍失败必须 failed: %+v", sum)
	}
}

// TestTargetRound_IdempotentCreateConfirmedByS1 幂等「已存在」仍必须经 S1 确认且不虚增 Added。
func TestTargetRound_IdempotentCreateConfirmedByS1(t *testing.T) {
	stale := config.RuleInfo{Protocol: "TCP", Port: "9999", CidrBlock: "10.9.9.9/32", Action: "ACCEPT", Description: "[auto-dns]"}
	p := newProbeProvider(config.CloudTCLighthouse, 1, stale)
	p.createErrs = []error{errors.New("FirewallRulesExist")} // 幂等：另一写入者已创建
	p.materializeOnError = true
	rules := []config.DomainRule{staticRule(1, "a.example.com", "TCP", "443")}
	s := newTargetSyncer(t, []provider.Provider{p}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	_, creates, deletes := p.counts()
	if creates != 1 {
		t.Fatalf("幂等已存在仍需一次 Create，实际 %d", creates)
	}
	// 陈旧规则只能在幂等 Create 之后的 S1 覆盖确认之后才被清理
	seq := p.callSeq()
	if len(seq) < 4 || seq[1] != "create:1" || seq[3] != "delete:1" {
		t.Fatalf("调用序列 = %v, want create 之后先取 S1 再删除", seq)
	}
	if deletes != 1 {
		t.Fatalf("清理门满足时陈旧规则应被删除，实际 %d", deletes)
	}
	sum := s.Status().LastRound
	if sum == nil || sum.Outcome != RoundSuccess {
		t.Fatalf("幂等已存在经 S1 确认后必须 success: %+v", sum)
	}
	if sum.Added != 0 {
		t.Fatalf("added = %d, want 0（幂等已存在不得虚增）", sum.Added)
	}
	if sum.CleanupCandidates != 1 || sum.CleanupDeleted != 1 || sum.CleanupDeferred != 0 {
		t.Fatalf("陈旧候选必须被条件清理: candidates=%d deleted=%d deferred=%d",
			sum.CleanupCandidates, sum.CleanupDeleted, sum.CleanupDeferred)
	}
}

// TestRoundSummary_TargetLevelUnits 统计单元改为「有至少一条适用规则的目标」。
func TestRoundSummary_TargetLevelUnits(t *testing.T) {
	p1 := newProbeProvider(config.CloudTCLighthouse, 1)
	p2 := newProbeProvider(config.CloudTCCVM, 2) // 没有任何规则引用它
	rules := []config.DomainRule{
		{Host: "a.example.com", Protocol: "TCP", Ports: "443", Action: "ACCEPT", Targets: []int{1}},
		{Host: "a.example.com", Protocol: "UDP", Ports: "443", Action: "ACCEPT", Targets: []int{1}},
	}
	s := newTargetSyncer(t, []provider.Provider{p1, p2}, rules, map[string]string{"a.example.com": "1.1.1.1/32"})

	s.syncAll()

	sum := s.Status().LastRound
	if sum == nil {
		t.Fatal("缺少整轮汇总")
	}
	if sum.Total != 1 {
		t.Fatalf("total = %d, want 1（统计单元为目标，且无适用规则的目标不计入）", sum.Total)
	}
	if sum.OK+sum.Changed+sum.Failed+sum.Skipped != sum.Total {
		t.Fatalf("不变量被破坏: %+v", sum)
	}
	if sum.Changed != 1 {
		t.Fatalf("changed = %d, want 1（两条规则合并为一个目标单元）", sum.Changed)
	}
	if _, creates, _ := p2.counts(); creates != 0 {
		t.Fatalf("无适用规则的目标不得访问云 API")
	}
	if _, snapshots, _ := p2.counts(); snapshots != 0 {
		t.Fatalf("无适用规则的目标不得获取快照，实际 %d", snapshots)
	}
}
