package syncer

import (
	"context"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
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
	Key             config.LighthouseCompression
	Settings        config.LighthouseCompression
	Pending         config.LighthouseCompressionState
	PendingKnown    bool
	PendingFound    bool
	StateErr        string
	Configured      bool
	Enabled         bool
	ProtectedActive bool
	EdgeOneTag      string

	BaseSites []config.EdgeOneSite
	Eligible  []provider.EdgeOneSitePlanInput
	All       []provider.EdgeOneSitePlanInput

	Plan     provider.LighthouseCompressionPlan
	Selected *provider.CompressionRepresentation
	Blockers []provider.CompressionBlocker
	Input    provider.LighthouseCompressionInput
	Readback *provider.CompressionReadback
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
		PendingFound: pendingFound, StateErr: stateErr,
		Configured: true, Enabled: enabled, ProtectedActive: protectedActive,
		EdgeOneTag: state.Config.EdgeOneTag,
		BaseSites:  compressionBaseSites(exec.edgeSites, enabled),
		Eligible:   eligible, All: all,
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
	if c.Enabled && len(c.Eligible) > 0 {
		limit = 96
	}
	if s0Rows >= limit {
		return 0
	}
	return limit - s0Rows
}

// buildCompressionPlan 使用已冻结的 S0/base 新增行数构造压缩计划。
func (s *Syncer) buildCompressionPlan(state *RuntimeState, exec targetExecution, c *compressionContext, snapshot provider.RuleSnapshot, otherNewRows int) {
	if c == nil {
		return
	}
	protected := compressionProtectedAuthorizations(exec)
	if !c.Enabled || len(c.Eligible) == 0 {
		c.Input.Protected = protected
		c.Plan = provider.LighthouseCompressionPlan{
			Enabled: c.Enabled,
			Capacity: provider.CompressionCapacity{
				Mode: provider.CompressionModeDisabled, NormalCap: 96, Reserved: 4, HardCap: 100,
				S0Rows: len(snapshot.Rules), OtherNewRows: otherNewRows,
			},
		}
		return
	}
	dnsEnabled, dnsComplete, dnsAddresses := compressionDNSInput(state, exec)
	var in provider.LighthouseCompressionInput
	in = provider.LighthouseCompressionInput{
		CloudType: config.CloudTCLighthouse,
		Config: provider.CompressionConfig{
			Enabled: true, ExtraIPv4Limit: c.Settings.ExtraIPv4Allow,
		},
		EdgeOneTag:         state.Config.EdgeOneTag,
		Snapshot:           snapshot,
		Sites:              c.Eligible,
		OtherNewRows:       otherNewRows,
		PendingFingerprint: provider.CompressionPendingFingerprint(c.Eligible),
		Protected:          protected,
		DNSEnabled:         dnsEnabled,
		DNSComplete:        dnsComplete,
		DNSAddresses:       dnsAddresses,
	}
	in.AssessCleanup = func(candidate provider.CompressionRepresentation) provider.CompressionCleanupEvidence {
		return provider.AssessCompressionCleanupEvidence(in, c.All, candidate)
	}
	c.Input = in
	c.Plan = provider.PlanLighthouseCompression(context.Background(), in)
	c.Selected = c.Plan.Selected
	c.Blockers = append([]provider.CompressionBlocker{}, c.Plan.Blockers...)
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
	return c != nil && c.Enabled && len(c.Eligible) > 0 && c.Selected == nil
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
func (s *Syncer) recordCompressionPendingReview(state *RuntimeState, epoch uint64, c *compressionContext, snapshot provider.RuleSnapshot, coverage provider.CompressionCoverage) {
	if c == nil || c.Enabled || !c.PendingKnown || !c.Pending.PendingReview {
		return
	}
	if c.StateErr != "" || s.compressionStateStore() == nil {
		return
	}
	if !coverage.Complete || !coverage.RiskKnown || coverage.ExtraIPv4 != 0 || len(snapshot.Rules) > 96 {
		return
	}
	if c.ProtectedActive {
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
		slog.Info("Lighthouse 压缩待复查已收口", "instance", c.Key.Identity(), "rules", len(snapshot.Rules))
		return true
	})
}

// updateCompressionStatus 记录本轮压缩观察，供事件/日志与后续展示使用。
func (s *Syncer) compressionObservation(c *compressionContext, coverage provider.CompressionCoverage, snapshot provider.RuleSnapshot) map[string]any {
	if c == nil {
		return nil
	}
	out := map[string]any{
		"enabled": c.Enabled, "pending_review": c.Pending.PendingReview,
		"mode": c.Plan.Capacity.Mode, "s0_rows": len(snapshot.Rules),
		"other_new_rows": c.Plan.Capacity.OtherNewRows, "add_rows": c.Plan.Capacity.AddRows,
		"peak": c.Plan.Capacity.Peak, "normal_budget": c.Plan.Capacity.NormalBudget,
		"borrowed": c.Plan.Capacity.Borrowed, "expected_cleanup": c.Plan.Capacity.ExpectedCleanupRows,
		"expected_after":  c.Plan.Capacity.ExpectedAfter,
		"risk_extra_ipv4": coverage.ExtraIPv4, "risk_complete": coverage.RiskKnown,
		"blockers":    c.blockerText(),
		"observed_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if coverage.MaxExtraRun > 0 {
		out["max_extra_run"] = coverage.MaxExtraRun
	}
	return out
}
