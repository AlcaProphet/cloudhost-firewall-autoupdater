package syncer

import (
	"context"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/dns"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件承载 Build8 Step 12 的只读预览数据准备与 DTO，不执行任何写入。
// 正式与模拟共用 FirstOverlap/DecideAdmission/PlanTarget；区别仅是模拟不持久化。

// DryRunPreflight 是一次模拟预览的全局判定摘要。
type DryRunPreflight struct {
	State           PreflightState         `json:"state"`
	Conflict        *AddressConflictStatus `json:"conflict,omitempty"`
	DNSComplete     bool                   `json:"dns_complete"`
	EdgeOneComplete bool                   `json:"edgeone_complete"`
	KnownConflict   bool                   `json:"known_conflict"`
	ProtectedCIDRs  []string               `json:"protected_cidrs"`
	Error           string                 `json:"error"`
}

// DryRunSite 是一个站点在一次模拟预览中的版本与完整性摘要。
type DryRunSite struct {
	ZoneID          string     `json:"zone_id"`
	TargetIDs       []int      `json:"target_ids"`
	CurrentVersion  string     `json:"current_version"`
	PendingVersion  string     `json:"pending_version"`
	PlannedActiveAt *time.Time `json:"planned_active_at"`
	AddressFamily   string     `json:"address_family"`
	DataComplete    bool       `json:"data_complete"`
	Reasons         []string   `json:"reasons"`
	ProtectedCIDRs  []string   `json:"protected_cidrs"`
}

// DryRunExecutionUnit 是一个物理目标的预览摘要；正式执行按同一身份归并别名。
type DryRunExecutionUnit struct {
	TargetKey         string                `json:"target_key"`
	TargetIDs         []int                 `json:"target_ids"`
	Provider          string                `json:"provider"`
	Sites             []DryRunSite          `json:"sites"`
	ProtectedCIDRs    []string              `json:"protected_cidrs"`
	RulesRead         int                   `json:"rules_read"`
	CurrentRuleCount  int                   `json:"current_rule_count"`
	PlannedAdd        int                   `json:"planned_add"`
	TransitionPeak    int                   `json:"transition_peak"`
	Limit             *int                  `json:"limit"`
	LimitSource       string                `json:"limit_source"`
	LimitExceeded     bool                  `json:"limit_exceeded"`
	CoverageReady     bool                  `json:"coverage_ready"`
	DataComplete      bool                  `json:"data_complete"`
	Conflicts         []provider.PlanIssue  `json:"conflicts"`
	CleanupCandidates []provider.RuleChange `json:"cleanup_candidates"`
	CleanupDeletable  []provider.RuleChange `json:"cleanup_deletable"`
	CleanupProtected  []provider.RuleChange `json:"cleanup_protected"`
	Error             string                `json:"error"`
}

type dryRunDNSData struct {
	addresses []DNSAddress
	complete  bool
	resolved  map[int][]dns.ResolvedIP
	errors    map[int]string
}

// previewDNS 只读解析全部适用域名；不写熔断器、不发事件、不参与正式探测名额。
func (s *Syncer) previewDNS(state *RuntimeState) dryRunDNSData {
	out := dryRunDNSData{
		complete: true,
		resolved: make(map[int][]dns.ResolvedIP),
		errors:   make(map[int]string),
	}
	if state == nil || !state.Config.DNSFeatureEnabled() {
		return out
	}
	rules := applicableRules(state)
	if len(rules) == 0 {
		return out
	}
	type hostResult struct {
		ips []dns.ResolvedIP
		err error
	}
	cache := make(map[string]hostResult)
	for _, rule := range rules {
		key := dns.DomainKey(rule.Host)
		hr, ok := cache[key]
		if !ok {
			hr.ips, hr.err = s.resolveHost(state, rule.Host)
			cache[key] = hr
		}
		if hr.err != nil {
			out.errors[rule.ID] = hr.err.Error()
			out.complete = false
			continue
		}
		if len(hr.ips) == 0 {
			out.complete = false
			continue
		}
		used := 0
		for _, ip := range hr.ips {
			addr, ok := netip.AddrFromSlice(ip.IP)
			if !ok {
				out.complete = false
				continue
			}
			out.addresses = append(out.addresses, DNSAddress{Host: rule.Host, Addr: addr.Unmap()})
			if ip.IsIPv6 && !rule.EnableIPv6 {
				continue
			}
			out.resolved[rule.ID] = append(out.resolved[rule.ID], ip)
			used++
		}
		if used == 0 {
			out.complete = false
		}
	}
	sort.SliceStable(out.addresses, func(i, j int) bool {
		if out.addresses[i].Host != out.addresses[j].Host {
			return out.addresses[i].Host < out.addresses[j].Host
		}
		return out.addresses[i].Addr.String() < out.addresses[j].Addr.String()
	})
	return out
}

// previewEdgeOneData 只读准备站点快照与过渡保护；不写正式缓存/状态/过渡表。
func (s *Syncer) previewEdgeOneData(state *RuntimeState) map[string]edgeOneSiteData {
	out := make(map[string]edgeOneSiteData)
	if state == nil || !state.Config.EdgeOneEnabled {
		return out
	}
	transitions, transitionErr := s.edgeOneTransitionsForRound()
	for _, site := range state.Config.EdgeOneSites {
		zone := strings.TrimSpace(site.ZoneID)
		if zone == "" {
			continue
		}
		data := edgeOneSiteData{site: site, transitionReadErr: transitionErr, transitionErr: transitionErr}
		s.mu.RLock()
		cached, ok := s.edgeCache[zone]
		s.mu.RUnlock()
		switch {
		case ok && s.edgeCacheFresh(cached, site, state, s.now()):
			data.snapshot = cached.snapshot
		case state.TEO == nil:
			data.unavailable = "TEO 客户端未接线"
		default:
			snapshot, err := state.TEO.DescribeOriginACL(context.Background(), zone)
			if err != nil {
				data.unavailable = safeTEOErrorText(err)
			} else {
				data.snapshot = snapshot
			}
		}
		row, hasRow := transitions[zone]
		var rowPtr *config.EdgeOneTransition
		if hasRow {
			cp := row
			rowPtr = &cp
		}
		snapshotValid := data.unavailable == "" &&
			strings.TrimSpace(data.snapshot.ZoneID) != "" &&
			strings.TrimSpace(data.snapshot.Current.Version) != "" &&
			provider.OriginACLSnapshotPrefixesValid(data.snapshot)
		var plan edgeOneTransitionPlanResult
		if snapshotValid {
			plan = planEdgeOneTransition(rowPtr, data.snapshot, s.now())
		} else {
			plan = edgeOneTransitionBackfill(rowPtr, s.now())
		}
		data.transition = plan.next
		data.protected = plan.protected
		data.transitionErr = transitionErr
		out[zone] = data
	}
	return out
}

// previewPreflight 复用正式纯判定，但只读冲突记录。
func (s *Syncer) previewPreflight(state *RuntimeState, dnsAddresses []DNSAddress, dnsComplete bool, edge []EdgeOnePrefix, edgeComplete bool) (PreflightOutcome, error) {
	in := Preflight{
		DNSEnabled:      state != nil && state.Config.DNSFeatureEnabled(),
		EdgeOneEnabled:  state != nil && state.Config.EdgeOneEnabled,
		DNSComplete:     dnsComplete,
		EdgeOneComplete: edgeComplete,
		DNS:             dnsAddresses,
		EdgeOne:         edge,
	}
	if s.conflict != nil {
		return s.conflict.Preview(context.Background(), in)
	}
	known := s.hasKnownConflict()
	in.KnownConflict = known
	if !in.DNSEnabled || !in.EdgeOneEnabled {
		return PreflightOutcome{
			State:           PreflightStateSingleSource,
			DNSComplete:     dnsComplete,
			EdgeOneComplete: edgeComplete,
			KnownConflict:   known,
			Admission:       DecideAdmission(in),
		}, nil
	}
	return PreflightOutcome{
		State:           PreflightStateDataUnavailable,
		DNSComplete:     dnsComplete,
		EdgeOneComplete: edgeComplete,
		KnownConflict:   known,
		Admission:       DecideAdmission(in),
	}, errAddressConflictUnavailable
}

var errAddressConflictUnavailable = &previewError{"地址冲突状态未接线，无法完成只读预检"}

type previewError struct{ text string }

func (e *previewError) Error() string { return e.text }

func dryRunPreflight(outcome PreflightOutcome, protected []EdgeOnePrefix, err error) DryRunPreflight {
	out := DryRunPreflight{
		State:           outcome.State,
		DNSComplete:     outcome.DNSComplete,
		EdgeOneComplete: outcome.EdgeOneComplete,
		KnownConflict:   outcome.KnownConflict,
		ProtectedCIDRs:  dryRunProtectedStrings(protected),
	}
	if outcome.Overlap != nil {
		out.Conflict = &AddressConflictStatus{
			DNSName:       outcome.Overlap.DNS.Host,
			DNSAddress:    outcome.Overlap.DNS.Addr.Unmap().String(),
			EdgeOnePrefix: outcome.Overlap.EdgeOne.Prefix.Masked().String(),
			ZoneID:        outcome.Overlap.EdgeOne.ZoneID,
		}
	}
	if err != nil {
		out.Error = "地址冲突只读检查失败"
	}
	return out
}

func dryRunProtectedStrings(prefixes []EdgeOnePrefix) []string {
	out := make([]string, 0, len(prefixes))
	seen := make(map[string]bool, len(prefixes))
	for _, item := range prefixes {
		if !item.Prefix.IsValid() {
			continue
		}
		v := item.Prefix.Masked().String()
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func dryRunSites(sites []config.EdgeOneSite, data map[string]edgeOneSiteData) []DryRunSite {
	out := make([]DryRunSite, 0, len(sites))
	sort.SliceStable(sites, func(i, j int) bool { return sites[i].ZoneID < sites[j].ZoneID })
	for _, site := range sites {
		zone := strings.TrimSpace(site.ZoneID)
		item := data[zone]
		reasons := make([]string, 0)
		if item.unavailable != "" {
			reasons = append(reasons, "snapshot_unavailable")
		}
		if item.transitionReadErr != "" {
			reasons = append(reasons, "transition_unavailable")
		}
		// 版本号独立于完整性：查询成功但缺少地址族/过渡记录不可读时，
		// 已知的当前/待更新版本仍应回显，不能显示成“未知”。
		knownVersion := strings.TrimSpace(item.snapshot.ZoneID) != "" &&
			strings.TrimSpace(item.snapshot.Current.Version) != ""
		snapshotValid := knownVersion && provider.OriginACLSnapshotPrefixesValid(item.snapshot)
		if item.unavailable == "" && !snapshotValid {
			reasons = append(reasons, "snapshot_incomplete")
		}
		complete := item.unavailable == "" && item.transitionReadErr == "" && snapshotValid
		if complete && !hasRequiredAddressFamilies(item.snapshot, site.AddressFamily) {
			complete = false
			reasons = append(reasons, "address_family_missing")
		}
		reasons = stableUniqueStrings(reasons)
		entry := DryRunSite{
			ZoneID:         zone,
			TargetIDs:      append([]int(nil), site.TargetIDs...),
			AddressFamily:  site.AddressFamily,
			DataComplete:   complete,
			Reasons:        reasons,
			ProtectedCIDRs: protectedEdgeOneCIDRStrings(item.protected),
		}
		if knownVersion {
			entry.CurrentVersion = item.snapshot.Current.Version
			if item.snapshot.Next != nil {
				entry.PendingVersion = item.snapshot.Next.Version
			}
			entry.PlannedActiveAt = copyTimePtr(item.snapshot.PlannedActiveAt)
		}
		out = append(out, entry)
	}
	return out
}

func dryRunLimit(ct config.CloudType) (*int, string) {
	if ct == config.CloudTCCVM {
		limit := 100
		return &limit, "cvm_ingress_100"
	}
	return nil, "unknown"
}

func dryRunRuleChanges(infos []config.RuleInfo) []provider.RuleChange {
	out := make([]provider.RuleChange, 0, len(infos))
	for _, info := range infos {
		out = append(out, provider.RuleChangeFromInfo(info))
	}
	return out
}

func dryRunActionChanges(actions []config.RuleAction) []provider.RuleChange {
	out := make([]provider.RuleChange, 0, len(actions))
	for _, action := range actions {
		out = append(out, provider.RuleChangeFromAction(action))
	}
	return out
}

func dryRunProtectedCleanup(plan provider.TargetPlan, edgeTag string, protected []netip.Prefix, unknown bool) []provider.RuleChange {
	if strings.TrimSpace(edgeTag) == "" {
		return []provider.RuleChange{}
	}
	out := make([]provider.RuleChange, 0)
	for _, rule := range plan.CleanupCandidates {
		if !tag.IsOwned(rule.Description, edgeTag) {
			continue
		}
		keys, ok := provider.SnapshotRuleKeys(rule)
		if !ok {
			continue
		}
		hit := unknown
		if !hit {
			for _, key := range keys {
				if protectedPrefixesOverlapCandidate(protected, key.CIDR) {
					hit = true
					break
				}
			}
		}
		if hit {
			out = append(out, provider.RuleChangeFromInfo(rule))
		}
	}
	return out
}

func edgeOneInputsForPreview(sites []config.EdgeOneSite, data map[string]edgeOneSiteData) []provider.EdgeOneSitePlanInput {
	if len(sites) == 0 {
		return nil
	}
	out := make([]provider.EdgeOneSitePlanInput, 0, len(sites))
	for _, site := range sites {
		item := data[strings.TrimSpace(site.ZoneID)]
		out = append(out, provider.EdgeOneSitePlanInput{
			Site:              site,
			Snapshot:          item.snapshot,
			UnavailableReason: item.unavailable,
		})
	}
	return out
}

func protectedPrefixesForSites(sites []config.EdgeOneSite, data map[string]edgeOneSiteData) []netip.Prefix {
	var out []netip.Prefix
	for _, site := range sites {
		out = append(out, data[strings.TrimSpace(site.ZoneID)].protected...)
	}
	return normalizeEdgeOnePrefixes(out)
}

func protectedUnknownForSites(sites []config.EdgeOneSite, data map[string]edgeOneSiteData) bool {
	for _, site := range sites {
		if data[strings.TrimSpace(site.ZoneID)].transitionReadErr != "" {
			return true
		}
	}
	return false
}

func localTargetSites(sites []config.EdgeOneSite, targetID int) []config.EdgeOneSite {
	out := make([]config.EdgeOneSite, 0)
	for _, site := range sites {
		for _, id := range site.TargetIDs {
			if id == targetID {
				out = append(out, site)
				break
			}
		}
	}
	return out
}

func subsetDNSData(data dryRunDNSData, rules []config.DomainRule) (map[int][]dns.ResolvedIP, map[int]string) {
	resolved := make(map[int][]dns.ResolvedIP, len(rules))
	errors := make(map[int]string)
	for _, rule := range rules {
		if v, ok := data.resolved[rule.ID]; ok {
			resolved[rule.ID] = v
		}
		if v, ok := data.errors[rule.ID]; ok {
			errors[rule.ID] = v
		}
	}
	return resolved, errors
}
