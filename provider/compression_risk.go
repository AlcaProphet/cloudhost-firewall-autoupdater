package provider

import (
	"net/netip"
	"sort"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/internal/tag"
)

// computeCompressionRisk 按协议端口分别求当前 EdgeOne TAG 允许集合与计划新增的并集，
// 减去该端口由站点精确授权的地址，最后跨端口并集去重。
//
// 手工/其他 TAG 规则不进入 grant，因此不会抵扣额度；旧压缩残留仍属于 EdgeOne TAG，
// 会自然计入额外地址。全端口规则按“所有授权端口集合的交集”扣除，避免漏算只在一个
// 端口合法的地址。
func computeCompressionRisk(in LighthouseCompressionInput, groups []compressionAuthGroup, rep CompressionRepresentation) CompressionRisk {
	out := CompressionRisk{Limit: in.Config.ExtraIPv4Limit, Complete: true}
	authorized := make(map[string]ipv4Set)
	protocols := make(map[string]bool)
	for _, group := range groups {
		set := ipv4SetFromPrefixes(group.Prefixes)
		for _, key := range group.Keys {
			authorized[key.String()] = authorized[key.String()].union(set)
			protocols[key.Protocol] = true
		}
	}
	keys := make([]string, 0, len(authorized))
	for key := range authorized {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	existingRisk, existingPerPort := compressionRiskForRules(in.Snapshot.Rules, authorized, keys, in.EdgeOneTag, true)
	planned := make([]configRuleLike, 0, len(rep.Rows))
	for _, row := range rep.Rows {
		planned = append(planned, configRuleLike{
			Protocol: row.Protocol, Port: row.Port, CidrBlock: row.CIDR, Action: row.Action,
		})
	}
	total, perPort := compressionRiskForRuleLikes(planned, authorized, keys)
	total = total.union(existingRisk)
	for key, set := range existingPerPort {
		perPort[key] = perPort[key].union(set)
	}

	out.ExtraIPv4 = total.count()
	out.MaxExtraRun = total.maxRun()
	out.OldResidue = existingRisk.count()
	out.PerPort = make([]CompressionPortRisk, 0, len(keys))
	for _, key := range keys {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		set := perPort[key]
		out.PerPort = append(out.PerPort, CompressionPortRisk{
			Protocol: parts[0], Port: parts[1],
			ExtraIPv4: set.count(), MaxExtraRun: set.maxRun(), Ranges: ipv4RangeStrings(set),
		})
	}
	for _, rule := range in.Snapshot.Rules {
		if strings.EqualFold(strings.TrimSpace(rule.Action), "ACCEPT") && !tag.IsOwned(rule.Description, in.EdgeOneTag) {
			out.ManualIgnored = true
			break
		}
	}
	return out
}

type configRuleLike struct {
	Protocol  string
	Port      string
	CidrBlock string
	Action    string
}

func compressionRiskForRules(rules []config.RuleInfo, authorized map[string]ipv4Set, keys []string, edgeTag string, requireOwned bool) (ipv4Set, map[string]ipv4Set) {
	out := make([]configRuleLike, 0, len(rules))
	for _, rule := range rules {
		if requireOwned && !tag.IsOwned(rule.Description, edgeTag) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rule.Action), "ACCEPT") {
			continue
		}
		out = append(out, configRuleLike{Protocol: rule.Protocol, Port: rule.Port, CidrBlock: rule.CidrBlock, Action: rule.Action})
	}
	return compressionRiskForRuleLikes(out, authorized, keys)
}

func compressionRiskForRuleLikes(rules []configRuleLike, authorized map[string]ipv4Set, keys []string) (ipv4Set, map[string]ipv4Set) {
	perPort := make(map[string]ipv4Set)
	var total ipv4Set
	for _, rule := range rules {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(rule.CidrBlock))
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		prefix = prefix.Masked()
		ruleSet := ipv4SetFromPrefixes([]netip.Prefix{prefix})
		proto := strings.ToUpper(strings.TrimSpace(rule.Protocol))
		if proto == "ICMP" || proto == "ICMPV6" || proto == "" {
			continue
		}
		hasUnconfiguredPort := false
		for _, item := range strings.Split(rule.Port, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if portItemIsFull(item) {
				continue
			}
			matched := false
			for _, key := range keys {
				parts := strings.SplitN(key, "|", 2)
				if len(parts) != 2 || !protocolMatches(proto, parts[0]) {
					continue
				}
				if portExpressionOverlaps(item, parts[1]) {
					matched = true
					break
				}
			}
			if !matched {
				hasUnconfiguredPort = true
				break
			}
		}
		for _, key := range keys {
			parts := strings.SplitN(key, "|", 2)
			if len(parts) != 2 || !protocolMatches(proto, parts[0]) {
				continue
			}
			if !portExpressionOverlaps(rule.Port, parts[1]) {
				continue
			}
			perPort[key] = perPort[key].union(ruleSet.subtract(authorized[key]))
		}
		if hasUnconfiguredPort {
			total = total.union(ruleSet)
		}
	}
	for _, set := range perPort {
		total = total.union(set)
	}
	return total, perPort
}

func protocolMatches(ruleProto, authProto string) bool {
	ruleProto = strings.ToUpper(strings.TrimSpace(ruleProto))
	authProto = strings.ToUpper(strings.TrimSpace(authProto))
	switch ruleProto {
	case "ALL", "TCP+UDP":
		return authProto == "TCP" || authProto == "UDP"
	case "TCP", "UDP":
		return ruleProto == authProto
	default:
		return false
	}
}
