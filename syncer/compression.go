package syncer

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// LighthouseCompressionStore 是实例压缩运行观察的最小读写接口；*config.Store 实现。
type LighthouseCompressionStore interface {
	LoadLighthouseCompressionState(c config.LighthouseCompression) (config.LighthouseCompressionState, bool, error)
	SaveLighthouseCompressionState(state config.LighthouseCompressionState) error
	DeleteLighthouseCompressionState(c config.LighthouseCompression) error
}

// SetLighthouseCompressionStore 注入实例压缩运行观察表；必须在 Run 前调用。
func (s *Syncer) SetLighthouseCompressionStore(store LighthouseCompressionStore) {
	s.mu.Lock()
	s.compressionStore = store
	s.mu.Unlock()
}

func (s *Syncer) compressionStateStore() LighthouseCompressionStore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.compressionStore
}

// compressionContext 是一个物理 Lighthouse 实例本轮的压缩设置、站点输入与规划结果。
//
// 正式 attempt 与 Dry Run 共用本结构与 buildPlan，保证同一输入得到同一计划。
type compressionContext struct {
	Key              config.LighthouseCompression
	Settings         config.LighthouseCompression
	Pending          config.LighthouseCompressionState
	PendingKnown     bool
	StateErr         string
	Enabled          bool
	ProtectedActive  bool
	ProtectedUnknown bool
	ProtectedCIDRs   []string
	EdgeOneTag       string

	BaseSites []config.EdgeOneSite
	Eligible  []provider.EdgeOneSitePlanInput
	All       []provider.EdgeOneSitePlanInput

	Plan          provider.LighthouseCompressionPlan
	Selected      *provider.CompressionRepresentation
	Blockers      []provider.CompressionBlocker
	Input         provider.LighthouseCompressionInput
	Readback      *provider.CompressionReadback
	Observed      *provider.RuleSnapshot
	Stage         string
	Failure       string
	Coverage      provider.CompressionCoverage
	CleanupOnly   bool
	RecoveryRules []config.RuleInfo
}

// compressionContextFor 读取实例设置与待复查状态；不访问云 API。
//
// 只有 Lighthouse 且存在已保存设置或待复查观察时才返回非 nil。读取失败保留
// 上下文并记录 StateErr：enabled 仍可规划，disabled 的收口会保守延后。
func (s *Syncer) compressionContextFor(state *RuntimeState, exec targetExecution) *compressionContext {
	if state == nil || exec.provider == nil || exec.provider.CloudType() != config.CloudTCLighthouse {
		return nil
	}
	target, ok := targetConfigForExecution(state, exec)
	if !ok {
		return nil
	}
	key := config.LighthouseCompression{CloudType: target.CloudType, Region: target.Region, ResourceID: target.ResourceID}
	var settings config.LighthouseCompression
	settingsFound := false
	for _, item := range state.Config.LighthouseCompression {
		if item.Identity() == key.Identity() {
			settings, settingsFound = item, true
			break
		}
	}
	pending := config.LighthouseCompressionState{CloudType: key.CloudType, Region: key.Region, ResourceID: key.ResourceID}
	pendingFound := false
	pendingKnown := false
	stateErr := ""
	if store := s.compressionStateStore(); store == nil {
		stateErr = "Lighthouse 压缩状态未接线"
	} else {
		if loaded, found, err := store.LoadLighthouseCompressionState(key); err != nil {
			stateErr = "读取 Lighthouse 压缩待复查失败"
			slog.Warn(stateErr, "instance", key.Identity(), "error_type", "state_read")
		} else {
			pending, pendingFound, pendingKnown = loaded, found, true
		}
	}
	if !settingsFound && !pendingFound && stateErr == "" {
		return nil
	}
	enabled := settingsFound && settings.Enabled
	all := edgeOneInputsForSites(exec.edgeSites, exec.edgeData)
	eligible := make([]provider.EdgeOneSitePlanInput, 0, len(all))
	for _, input := range all {
		if provider.CompressionSiteEligible(input.Site) {
			eligible = append(eligible, input)
		}
	}
	protectedActive := false
	for _, site := range exec.edgeSites {
		if len(exec.edgeData[strings.TrimSpace(site.ZoneID)].protected) > 0 {
			protectedActive = true
			break
		}
	}
	return &compressionContext{
		Key: key, Settings: settings, Pending: pending, PendingKnown: pendingKnown,
		StateErr: stateErr,
		Enabled:  enabled, ProtectedActive: protectedActive, ProtectedUnknown: protectedUnknownForExec(exec),
		EdgeOneTag: state.Config.EdgeOneTag, ProtectedCIDRs: protectedEdgeOneCIDRStrings(protectedPrefixesForExec(exec)),
		BaseSites: compressionBaseSites(exec.edgeSites, enabled && exec.edgeActive),
		Eligible:  eligible, All: all,
	}
}

// targetConfigForExecution 从运行时目标列表定位物理身份对应的配置。
func targetConfigForExecution(state *RuntimeState, exec targetExecution) (config.TargetConfig, bool) {
	for _, target := range state.Config.Targets {
		if config.TargetIdentity(target) == exec.identity {
			return target, true
		}
	}
	parts := strings.Split(exec.identity, "\x00")
	if len(parts) == 3 {
		return config.TargetConfig{CloudType: config.CloudType(parts[0]), Region: parts[1], ResourceID: parts[2]}, true
	}
	return config.TargetConfig{}, false
}

// compressionBaseSites 返回 base 规划使用的站点：
//
// enabled 时只移除可压缩站点对 IPv4 的精确期望，IPv6/ICMP/协议 ALL 继续走原规划；
// disabled 时保留全部站点，由原精确表示接管。
func compressionBaseSites(sites []config.EdgeOneSite, enabled bool) []config.EdgeOneSite {
	out := make([]config.EdgeOneSite, 0, len(sites))
	for _, site := range sites {
		if !enabled || !provider.CompressionSiteEligible(site) {
			out = append(out, site)
			continue
		}
		switch strings.ToLower(strings.TrimSpace(site.AddressFamily)) {
		case config.EdgeOneAddressFamilyIPv4:
			// 完全由压缩表示接管，base 不再产生 IPv4 精确期望。
			continue
		case config.EdgeOneAddressFamilyDual:
			cp := site
			cp.AddressFamily = config.EdgeOneAddressFamilyIPv6
			out = append(out, cp)
		default:
			out = append(out, site)
		}
	}
	return out
}

// compressionBaseAddLimit 返回配置了压缩的实例本轮 base 新增上限。
//
// enabled 使用常态 96 条（固定预留 4 条）；disabled 按 Design6 的原表示与
// 100 条硬配额规划，不套用 96 条预留。
func (c *compressionContext) baseAddLimit(s0Rows int) int {
	if c == nil {
		return -1
	}
	limit := 100
	if c.Enabled {
		limit = 96
	}
	if s0Rows >= limit {
		return 0
	}
	return limit - s0Rows
}

// buildCompressionPlan 使用已冻结的 S0/base 新增行数构造压缩计划。
func (c *compressionContext) buildCompressionPlan(exec targetExecution, snapshot provider.RuleSnapshot, basePlan provider.TargetPlan, otherNewRows int) {
	in := c.Input
	in.OtherNewRows = otherNewRows
	if !c.Enabled || !exec.edgeActive || len(c.Eligible) == 0 {
		c.Plan.Capacity = provider.EvaluateCompressionCapacity(provider.CompressionCapacityInput{
			S0Rows: len(snapshot.Rules), OtherNewRows: otherNewRows,
		})
		if !c.Enabled {
			peak := len(snapshot.Rules) + otherNewRows
			c.Plan.Capacity.Mode = provider.CompressionModeDisabled
			c.Plan.Capacity.Peak = peak
			c.Plan.Capacity.ExpectedAfter = peak
			c.Plan.Capacity.NormalBudget = max(0, 100-peak)
		}
		return
	}
	in.Sites = c.Eligible
	in.PendingFingerprint = provider.CompressionPendingFingerprint(c.Eligible)
	in.CleanupKeepKeys = planDesiredKeys(basePlan)
	if !c.ProtectedUnknown {
		in.PostActivationProtected = postActivationCompressionProtection(exec)
		in.AssessCleanup = func(candidate provider.CompressionRepresentation) provider.CompressionCleanupEvidence {
			return provider.AssessCompressionCleanupEvidence(in, c.All, candidate)
		}
	}
	c.Plan = provider.PlanLighthouseCompression(context.Background(), in)
	c.Selected = c.Plan.Selected
	c.Blockers = append([]provider.CompressionBlocker{}, c.Plan.Blockers...)
	if c.Selected == nil && otherNewRows == 0 && c.Coverage.Complete && c.Coverage.RiskKnown &&
		(c.Coverage.ExtraIPv4 > uint64(c.riskLimit()) || len(snapshot.Rules) >= 96) {
		c.prepareCompressionCleanupOnly(snapshot, basePlan, in)
	}
}

// planCompressionAttempt uses the same S0, base budget and source gates in
// formal execution and Dry Run.
func (c *compressionContext) planAttempt(state *RuntimeState, exec targetExecution, snapshot provider.RuleSnapshot, basePlan provider.TargetPlan) ([]config.RuleAction, bool) {
	actions := activeAddActions(state, basePlan, exec)
	dnsEnabled, dnsComplete, dnsAddresses := compressionDNSInput(state, exec)
	c.Input = provider.LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse,
		Config: provider.CompressionConfig{
			Enabled: c.Enabled, ExtraIPv4Limit: c.riskLimit(),
		},
		EdgeOneTag:   c.EdgeOneTag,
		OtherTag:     state.Config.Tag,
		Snapshot:     snapshot,
		Sites:        c.All,
		Protected:    compressionProtectedAuthorizations(exec),
		DNSEnabled:   dnsEnabled,
		DNSComplete:  dnsComplete,
		DNSAddresses: dnsAddresses,
	}
	c.observe("s0", snapshot)
	blocked := false
	if exec.edgeActive && !c.Enabled {
		actions, blocked = c.filterUnsafeReplacement(actions)
	}
	limit := c.baseAddLimit(len(snapshot.Rules))
	truncated := len(actions) > limit
	if truncated {
		actions = actions[:limit]
	}
	c.buildCompressionPlan(exec, snapshot, basePlan, len(actions))
	if truncated {
		c.Blockers = append(c.Blockers, provider.CompressionBlocker{
			Code: provider.CompressionBlockedCurrentAdd, Message: "本轮常态新增受实例容量限制",
		})
	}
	if blocked {
		c.Blockers = append(c.Blockers, provider.CompressionBlocker{
			Code: provider.CompressionBlockedRiskLimit, Message: "现有快照无法证明关闭态替换安全",
		})
	}
	return append(actions, c.compressionAddActions()...), truncated
}

func (c *compressionContext) filterUnsafeReplacement(actions []config.RuleAction) ([]config.RuleAction, bool) {
	unsafe := c.hasOppositeAction()
	if !unsafe && c.Coverage.ExtraIPv4 > uint64(c.riskLimit()) {
		projected := c.Input
		projected.Snapshot.Rules = append([]config.RuleInfo(nil), c.Input.Snapshot.Rules...)
		for _, action := range actions {
			projected.Snapshot.Rules = append(projected.Snapshot.Rules, config.RuleInfo{
				CidrBlock: action.CidrBlock, Ipv6CidrBlock: action.Ipv6CidrBlock,
				Protocol: action.Protocol, Port: action.Port, Action: action.Action, Description: action.Description,
			})
		}
		unsafe = !provider.CompressionRiskDoesNotExpand(c.Input, projected)
	}
	if !unsafe {
		return actions, false
	}
	return slices.DeleteFunc(actions, func(action config.RuleAction) bool {
		return tag.IsOwned(action.Description, c.EdgeOneTag)
	}), true
}

func (c *compressionContext) riskLimit() int64 {
	if c.Enabled {
		return c.Settings.ExtraIPv4Allow
	}
	return 0
}

func (c *compressionContext) observe(stage string, snapshot provider.RuleSnapshot) {
	c.Stage, c.Observed = stage, &snapshot
	c.Coverage = provider.CompressionCoverageFromRules(snapshot, c.EdgeOneTag, c.Input.OtherTag, c.All, c.Input.Protected, c.riskLimit())
	if c.ProtectedUnknown || strings.TrimSpace(snapshot.Revision) == "" {
		c.Coverage.RiskKnown = false
	}
}

// postActivationCompressionProtection projects only the protections the existing
// transition state machine would still hold after the observed Next becomes current.
func postActivationCompressionProtection(exec targetExecution) []provider.CompressionAuthorization {
	projected := exec
	projected.edgeData = make(map[string]edgeOneSiteData, len(exec.edgeSites))
	for _, site := range exec.edgeSites {
		zone := strings.TrimSpace(site.ZoneID)
		data := exec.edgeData[zone]
		if data.snapshot.Next != nil && data.transitionErr == "" {
			current := data.snapshot
			current.Current, current.Next = *current.Next, nil
			row := data.transition
			if row == nil {
				row = data.transitionInput.row
			}
			if row == nil {
				row = planEdgeOneTransition(nil, data.snapshot, time.Time{}).next
			}
			if row != nil {
				data.protected = planEdgeOneTransition(row, current, time.Time{}).protected
			}
		}
		projected.edgeData[zone] = data
	}
	return compressionProtectedAuthorizations(projected)
}

func (c *compressionContext) prepareCompressionCleanupOnly(snapshot provider.RuleSnapshot, basePlan provider.TargetPlan, in provider.LighthouseCompressionInput) {
	cleanup := provider.PlanCompressionCleanup(provider.CompressionCleanupInput{
		Snapshot: snapshot, Tag: in.EdgeOneTag, OtherTag: in.OtherTag, Sites: c.All,
		Protected: in.Protected, ProtectedUnknown: c.ProtectedUnknown, KeepKeys: planDesiredKeys(basePlan),
	})
	if len(cleanup.Deletable) == 0 {
		return
	}
	// Freeze the remaining S0 rows as witnesses. S1 and S2 must still prove
	// coverage using this set, even if unrelated rows appeared concurrently.
	removed := make(map[config.RuleInfo]bool, len(cleanup.Deletable))
	for _, rule := range cleanup.Deletable {
		removed[rule] = true
	}
	for _, rule := range snapshot.Rules {
		if !removed[rule] && compressionScopeRule(rule, in.EdgeOneTag) {
			c.RecoveryRules = append(c.RecoveryRules, rule)
		}
	}
	c.CleanupOnly = true
	c.Plan.Capacity = provider.EvaluateCompressionCapacity(provider.CompressionCapacityInput{S0Rows: len(snapshot.Rules)})
	c.Plan.Capacity.Mode = provider.CompressionModeCleanupOnly
	c.Plan.Capacity.ExpectedCleanupRows = len(cleanup.Deletable)
	c.Plan.Capacity.ExpectedAfter = len(snapshot.Rules) - len(cleanup.Deletable)
}

func (c *compressionContext) verify(snapshot provider.RuleSnapshot) *provider.CompressionReadback {
	in := c.Input
	in.Sites = c.Eligible
	in.Snapshot = snapshot
	switch {
	case c.Selected != nil:
		verified := provider.VerifyCompressionReadback(in, *c.Selected)
		return &verified
	case c.CleanupOnly:
		verified := provider.VerifyCompressionCleanupReadback(in, c.RecoveryRules)
		return &verified
	default:
		return nil
	}
}

func isCompressionOppositeAction(conflict provider.CompressionBlocker) bool {
	return conflict.Code == provider.CompressionBlockedOppositeAction
}

func (c *compressionContext) hasOppositeAction() bool {
	return slices.ContainsFunc(c.Coverage.Conflicts, isCompressionOppositeAction)
}

func (c *compressionContext) pendingReviewCanClear(snapshot provider.RuleSnapshot) bool {
	return !c.Enabled && c.PendingKnown && c.Pending.PendingReview && !c.ProtectedActive &&
		c.Coverage.Complete && c.Coverage.RiskKnown && c.Coverage.ExtraIPv4 == 0 && len(snapshot.Rules) <= 96
}

func (c *compressionContext) riskAllowsCleanup(snapshot provider.RuleSnapshot) bool {
	if !c.Coverage.RiskKnown {
		return false
	}
	if c.Coverage.ExtraIPv4 <= uint64(c.riskLimit()) {
		return true
	}
	before := c.Input
	initial := provider.CompressionCoverageFromRules(before.Snapshot, c.EdgeOneTag, before.OtherTag, c.All, before.Protected, c.riskLimit())
	after := before
	after.Snapshot = snapshot
	return initial.RiskKnown && initial.ExtraIPv4 > uint64(c.riskLimit()) &&
		provider.CompressionRiskDoesNotExpand(before, after)
}

// compressionProtectedAuthorizations 把过渡保护前缀展开为逐站点协议端口授权。
func compressionProtectedAuthorizations(exec targetExecution) []provider.CompressionAuthorization {
	out := make([]provider.CompressionAuthorization, 0)
	for _, site := range exec.edgeSites {
		zone := strings.TrimSpace(site.ZoneID)
		data := exec.edgeData[zone]
		if len(data.protected) == 0 {
			continue
		}
		protocols := compressionSiteProtocols(site.Protocol)
		items := compressionSitePortItems(site)
		for _, prefix := range data.protected {
			for _, protocol := range protocols {
				for _, item := range items {
					out = append(out, provider.CompressionAuthorization{
						ZoneID: zone, Prefix: prefix.Masked().String(), Protocol: protocol, PortItem: item,
					})
				}
			}
		}
	}
	return out
}

func compressionSiteProtocols(protocol string) []string {
	switch strings.ToUpper(strings.TrimSpace(protocol)) {
	case "TCP":
		return []string{"TCP"}
	case "UDP":
		return []string{"UDP"}
	case "TCP+UDP":
		return []string{"TCP", "UDP"}
	case "ICMP":
		return []string{"ICMP"}
	default:
		return nil
	}
}

func compressionSitePortItems(site config.EdgeOneSite) []string {
	if strings.EqualFold(strings.TrimSpace(site.Protocol), "ICMP") {
		return []string{"ALL"}
	}
	out := make([]string, 0)
	for _, item := range strings.Split(site.Ports, ",") {
		item = strings.ToUpper(strings.TrimSpace(item))
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// compressionDNSInput 提供有损候选的完整 DNS IPv4 检查输入。
func compressionDNSInput(state *RuntimeState, exec targetExecution) (bool, bool, []netip.Addr) {
	if state == nil || !state.Config.DNSFeatureEnabled() {
		return false, false, nil
	}
	if exec.dnsData == nil {
		return true, false, nil
	}
	addrs := make([]netip.Addr, 0, len(exec.dnsData.addresses))
	for _, item := range exec.dnsData.addresses {
		addrs = append(addrs, item.Addr.Unmap())
	}
	return true, exec.dnsData.complete, addrs
}

// compressionAddActions 返回压缩计划实际要提交的物理行。
func (c *compressionContext) compressionAddActions() []config.RuleAction {
	if c == nil || c.Selected == nil {
		return nil
	}
	return append([]config.RuleAction(nil), c.Selected.AddRows...)
}

// compressionSelectedRows 返回已选压缩行，供清理保留判断。
func (c *compressionContext) compressionSelectedRows() []provider.CompressionRow {
	if c == nil || c.Selected == nil {
		return nil
	}
	return append([]provider.CompressionRow(nil), c.Selected.Rows...)
}

// compressionAllBlocks 返回本轮压缩阻断原因；disabled 无压缩计划时不阻断 base。
func (c *compressionContext) compressionBlocked() bool {
	return c != nil && c.Enabled && len(c.Eligible) > 0 && c.Selected == nil && !c.CleanupOnly
}

func (c *compressionContext) blockerText() string {
	if c == nil || len(c.Blockers) == 0 {
		return ""
	}
	parts := make([]string, 0, len(c.Blockers))
	seen := make(map[string]bool)
	for _, blocker := range c.Blockers {
		if blocker.Code == "" || seen[blocker.Code] {
			continue
		}
		seen[blocker.Code] = true
		parts = append(parts, blocker.Code)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// recordCompressionPendingReview 在可信回读后清除关闭态待复查，或保留残留。
//
// 只有 disabled、待复查已知、S2 覆盖完整、额外 IPv4 为 0、总行数恢复 ≤96 且
// 无过渡保护时才清除；写入失败只记录安全诊断，不改变本轮删除/确认结果。
func (s *Syncer) recordCompressionPendingReview(state *RuntimeState, epoch uint64, c *compressionContext, snapshot provider.RuleSnapshot) {
	if c == nil || !c.pendingReviewCanClear(snapshot) {
		return
	}
	if c.StateErr != "" || s.compressionStateStore() == nil {
		return
	}
	_ = s.withStableConfig(func() bool {
		if state == nil || s.runtime.Snapshot() != state || s.Generation() != epoch {
			return false
		}
		if err := s.compressionStateStore().DeleteLighthouseCompressionState(c.Key); err != nil {
			slog.Warn("清除 Lighthouse 压缩待复查失败", "instance", c.Key.Identity(), "error_type", "state_delete")
			return false
		}
		c.Pending.PendingReview = false
		slog.Info("Lighthouse 压缩待复查已收口", "instance", c.Key.Identity(), "rules", len(snapshot.Rules))
		return true
	})
}

// compressionObservation records the latest actual read and the separate S0 projection.
func (c *compressionContext) compressionObservation() map[string]any {
	if c == nil {
		return nil
	}
	out := map[string]any{
		"enabled": c.Enabled, "pending_review": c.Pending.PendingReview, "pending_known": c.PendingKnown,
		"stage": c.Stage, "failure": c.Failure, "state_error": c.StateErr,
		"protected_active": c.ProtectedActive, "protected_known": !c.ProtectedUnknown, "protected_cidrs": c.ProtectedCIDRs,
		"mode": c.Plan.Capacity.Mode, "s0_rows": nil, "actual_rows": nil, "peak": nil,
		"risk_limit": c.riskLimit(), "reserved_restored": nil,
		"risk_extra_ipv4": nil, "risk_complete": c.Coverage.RiskKnown,
		"blockers":    c.blockerText(),
		"observed_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if c.Observed != nil {
		out["s0_rows"] = len(c.Input.Snapshot.Rules)
		out["actual_rows"] = len(c.Observed.Rules)
		out["reserved_restored"] = len(c.Observed.Rules) <= 96
	}
	if c.Coverage.RiskKnown {
		out["risk_extra_ipv4"] = c.Coverage.ExtraIPv4
		out["max_extra_run"] = c.Coverage.MaxExtraRun
	}
	if c.Plan.Capacity.HardCap != 0 {
		out["peak"] = c.Plan.Capacity.Peak
		out["other_new_rows"] = c.Plan.Capacity.OtherNewRows
		out["add_rows"] = c.Plan.Capacity.AddRows
		out["normal_budget"] = c.Plan.Capacity.NormalBudget
		out["borrowed"] = c.Plan.Capacity.Borrowed
		out["expected_cleanup"] = c.Plan.Capacity.ExpectedCleanupRows
		out["expected_after"] = c.Plan.Capacity.ExpectedAfter
	}
	return out
}

func (c *compressionContext) cleanupPlan(snapshot provider.RuleSnapshot, basePlan provider.TargetPlan) provider.CompressionCleanupPlan {
	planned := provider.PlanCompressionCleanup(provider.CompressionCleanupInput{
		Snapshot: snapshot, Tag: c.EdgeOneTag, OtherTag: c.Input.OtherTag, Sites: c.All,
		Protected: c.Input.Protected, ProtectedUnknown: c.ProtectedUnknown,
		Retained: c.compressionSelectedRows(), RetainedRules: c.RecoveryRules, KeepKeys: planDesiredKeys(basePlan),
	})
	baseSafe := len(basePlan.Unsupported) == 0 && len(basePlan.Conflicts) == 0 &&
		len(basePlan.EdgeOneErrors) == 0 && len(basePlan.DNSErrors) == 0 &&
		(basePlan.CoverageReady || len(basePlan.Desired) == 0)
	coverageProven := !c.Enabled && c.StateErr == "" || c.Enabled && c.Readback != nil && c.Readback.Complete
	if !baseSafe || !coverageProven || !c.riskAllowsCleanup(snapshot) {
		planned.Deletable = nil
	}
	return planned
}

// withoutCompressionScope removes the IPv4 rules handled by the compression cleanup planner.
func withoutCompressionScope(rules []config.RuleInfo, cctx *compressionContext) []config.RuleInfo {
	if cctx == nil {
		return rules
	}
	out := make([]config.RuleInfo, 0, len(rules))
	for _, rule := range rules {
		if compressionScopeRule(rule, cctx.EdgeOneTag) {
			continue
		}
		out = append(out, rule)
	}
	return out
}
