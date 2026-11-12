package syncer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// targetGroup 是一个物理资源的唯一写入执行单元：同一 cloud_type + region +
// resource_id 的本地别名合并为一个组，组内规则是绑定到任一边名的并集。
type targetGroup struct {
	identity  string
	targetIDs []int
	primary   provider.Provider
	rules     []config.DomainRule
	sites     []config.EdgeOneSite
}

// targetGroups 按实际资源身份归并 Provider，并保留稳定顺序。
func (s *Syncer) targetGroups(state *RuntimeState) []targetGroup {
	if state == nil {
		return nil
	}
	byID := make(map[int]config.TargetConfig, len(state.Config.Targets))
	for _, t := range state.Config.Targets {
		byID[t.ID] = t
	}
	type builder struct {
		identity string
		ids      []int
		provider provider.Provider
	}
	builders := make(map[string]*builder)
	order := make([]string, 0)
	for _, p := range state.Providers {
		id := p.TargetIndex()
		target, ok := byID[id]
		identity := fmt.Sprintf("#%d|%p", id, p)
		if ok {
			identity = config.TargetIdentity(target)
		}
		b, exists := builders[identity]
		if !exists {
			b = &builder{identity: identity, provider: p}
			builders[identity] = b
			order = append(order, identity)
		}
		b.ids = append(b.ids, id)
		if p.TargetIndex() < b.provider.TargetIndex() {
			b.provider = p
		}
	}
	sort.Strings(order)
	groups := make([]targetGroup, 0, len(order))
	for _, identity := range order {
		b := builders[identity]
		sort.Ints(b.ids)
		idSet := make(map[int]bool, len(b.ids))
		for _, id := range b.ids {
			idSet[id] = true
		}
		var rules []config.DomainRule
		if state.Config.DNSFeatureEnabled() {
			rules = filterRulesForTargetIDs(state.Config.DomainRules, idSet)
		}
		var sites []config.EdgeOneSite
		if state.Config.EdgeOneEnabled {
			sites = provider.SelectEdgeOneSitesForIdentity(state.Config.EdgeOneSites, state.Config.Targets, identity)
		}
		groups = append(groups, targetGroup{
			identity:  identity,
			targetIDs: b.ids,
			primary:   b.provider,
			rules:     rules,
			sites:     sites,
		})
	}
	return groups
}

func filterRulesForTargetIDs(rules []config.DomainRule, ids map[int]bool) []config.DomainRule {
	out := make([]config.DomainRule, 0, len(rules))
	for _, rule := range rules {
		if len(rule.Targets) == 0 {
			out = append(out, rule)
			continue
		}
		for _, id := range rule.Targets {
			if ids[id] {
				out = append(out, rule)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// targetUnitCount 统计本轮有 DNS 规则或 EdgeOne 站点的物理目标数。
func targetUnitCount(groups []targetGroup) int {
	n := 0
	for _, g := range groups {
		if len(g.rules) > 0 || len(g.sites) > 0 {
			n++
		}
	}
	return n
}

func groupTargetsByCloud(groups []targetGroup) map[config.CloudType][]targetGroup {
	out := make(map[config.CloudType][]targetGroup)
	for _, g := range groups {
		ct := g.primary.CloudType()
		out[ct] = append(out[ct], g)
	}
	return out
}

func edgeSiteForProof(sites []config.EdgeOneSite, zone string) (config.EdgeOneSite, bool) {
	zone = strings.TrimSpace(zone)
	for _, site := range sites {
		if strings.TrimSpace(site.ZoneID) == zone {
			return site, true
		}
	}
	return config.EdgeOneSite{}, false
}
