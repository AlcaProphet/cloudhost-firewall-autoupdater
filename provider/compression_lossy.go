package provider

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// coverOptionsForGroup 生成一个授权组的无损 + 贪心有损覆盖选项。
//
// 每个选项保存覆盖前缀、相对无损集合的额外地址和行数；有损只减少物理行数，
// 不改变原始授权。贪心按“合并后新增额外地址最少”选择兄弟对，保证确定性。
func coverOptionsForGroup(group compressionAuthGroup, maxOptions int) ([]compressionCoverOption, bool) {
	if maxOptions <= 0 {
		maxOptions = 128
	}
	required := mergeIPv4PrefixesExact(group.Prefixes)
	requiredSet := ipv4SetFromPrefixes(required)
	units, ok := groupPortUnits(group)
	if !ok {
		return nil, false
	}
	current := append([]netip.Prefix(nil), required...)
	options := make([]compressionCoverOption, 0, maxOptions)
	options = append(options, compressionCoverOption{
		Rows: units * len(current), Cover: append([]netip.Prefix(nil), current...), Extra: nil, Key: compressionCoverKey(current),
	})
	for len(current) > 1 && len(options) < maxOptions {
		type pair struct {
			a, b   netip.Prefix
			parent netip.Prefix
			extra  uint64
			key    string
		}
		var best *pair
		for i := 0; i < len(current); i++ {
			for j := i + 1; j < len(current); j++ {
				parent, ok := coveringPrefix(current[i], current[j])
				if !ok {
					continue
				}
				extra := ipv4SetFromPrefixes([]netip.Prefix{parent}).subtract(requiredSet).count()
				candidate := pair{a: current[i], b: current[j], parent: parent, extra: extra, key: parent.String()}
				if best == nil || candidate.extra < best.extra || (candidate.extra == best.extra && candidate.key < best.key) {
					copyCandidate := candidate
					best = &copyCandidate
				}
			}
		}
		if best == nil {
			break
		}
		next := make([]netip.Prefix, 0, len(current))
		for _, q := range current {
			if best.parent.Contains(q.Addr()) && q.Bits() >= best.parent.Bits() {
				continue
			}
			next = append(next, q)
		}
		next = append(next, best.parent)
		sort.SliceStable(next, func(i, j int) bool { return next[i].String() < next[j].String() })
		current = next
		extra := ipv4SetFromPrefixes(current).subtract(requiredSet)
		options = append(options, compressionCoverOption{
			Rows: units * len(current), Cover: append([]netip.Prefix(nil), current...),
			Extra: extra, Key: compressionCoverKey(current),
		})
	}
	return options, true
}

func groupPortUnits(group compressionAuthGroup) (int, bool) {
	byProtocol := make(map[string][]string)
	for _, key := range group.Keys {
		byProtocol[key.Protocol] = append(byProtocol[key.Protocol], key.Port)
	}
	total := 0
	for _, items := range byProtocol {
		exprs, ok := portExpressions(items)
		if !ok {
			return 0, false
		}
		total += len(exprs)
	}
	if total == 0 {
		return 0, false
	}
	return total, true
}

func compressionCoverKey(prefixes []netip.Prefix) string {
	parts := make([]string, len(prefixes))
	for i, p := range prefixes {
		parts[i] = p.Masked().String()
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// compressionExistingRisk 返回当前 S0 中 EdgeOne TAG 已存在的额外放行集合。
func compressionExistingRisk(in LighthouseCompressionInput, groups []compressionAuthGroup) ipv4Set {
	authorized := make(map[string]ipv4Set)
	var keys []string
	for _, group := range groups {
		set := ipv4SetFromPrefixes(group.Prefixes)
		for _, key := range group.Keys {
			authorized[key.String()] = authorized[key.String()].union(set)
			keys = append(keys, key.String())
		}
	}
	set, _ := compressionRiskForRules(in.Snapshot.Rules, authorized, keys, in.EdgeOneTag, true)
	return set
}

func optionHitsDrop(in LighthouseCompressionInput, group compressionAuthGroup, option compressionCoverOption) bool {
	coverSet := ipv4SetFromPrefixes(option.Cover)
	for _, rule := range in.Snapshot.Rules {
		if !strings.EqualFold(strings.TrimSpace(rule.Action), "DROP") {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(rule.CidrBlock))
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		proto := strings.ToUpper(strings.TrimSpace(rule.Protocol))
		for _, key := range group.Keys {
			if !protocolMatches(proto, key.Protocol) {
				continue
			}
			if !portExpressionOverlaps(rule.Port, key.Port) {
				continue
			}
			if coverSet.intersectsPrefix(prefix) || option.Extra.intersectsPrefix(prefix) {
				return true
			}
		}
	}
	return false
}

func optionHitsDNS(in LighthouseCompressionInput, option compressionCoverOption) bool {
	if !in.DNSComplete {
		return false
	}
	for _, addr := range in.DNSAddresses {
		if option.Extra.containsAddr(addr) {
			return true
		}
	}
	return false
}

// planLossyCompression 生成并联合选择有损覆盖；返回 incomplete 表示搜索上限阻断。
func planLossyCompression(ctx context.Context, in LighthouseCompressionInput, groups []compressionAuthGroup) (*CompressionRepresentation, bool, []CompressionBlocker) {
	var blockers []CompressionBlocker
	optionsByGroup := make([][]compressionCoverOption, 0, len(groups))
	for _, group := range groups {
		for _, key := range group.Keys {
			if portItemIsFull(key.Port) {
				blockers = append(blockers, CompressionBlocker{
					Code: CompressionBlockedLossyForbidden, Message: "全端口配置不进入有损扩张",
					Protocol: key.Protocol, Port: key.Port,
				})
				return nil, false, blockers
			}
		}
		options, ok := coverOptionsForGroup(group, 512)
		if !ok {
			blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedInvalidPort, Message: "无法生成端口表达式"})
			return nil, false, blockers
		}
		// 选项的 Rows 表示“实际新增行”：S0 中精确存在的压缩行已经占用预算，
		// 不能在选择时再次计入新增名额。
		for i := range options {
			rep, built, _ := buildCompressionRepresentation(in, []compressionAuthGroup{group}, [][]netip.Prefix{options[i].Cover})
			if built {
				options[i].Rows = len(rep.AddRows)
			}
		}
		filtered := make([]compressionCoverOption, 0, len(options))
		for _, option := range options {
			if option.Extra.count() > uint64(in.Config.ExtraIPv4Limit) {
				continue
			}
			if optionHitsDNS(in, option) {
				continue
			}
			if optionHitsDrop(in, group, option) {
				continue
			}
			filtered = append(filtered, option)
		}
		if len(filtered) == 0 {
			blockers = append(blockers, CompressionBlocker{
				Code: CompressionBlockedNoCandidate, Message: "该授权组没有满足 DNS/相反动作/额度的候选",
			})
			return nil, false, blockers
		}
		optionsByGroup = append(optionsByGroup, filtered)
	}
	if in.DNSEnabled && !in.DNSComplete {
		blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedDNSIncomplete, Message: "DNS 数据不完整，不能使用有损扩张"})
		return nil, false, blockers
	}
	baseRisk := compressionExistingRisk(in, groups)
	rowBudget := maxInt(0, compressionNormalCap-len(in.Snapshot.Rules)-in.OtherNewRows)
	if in.HasPendingVersion {
		rowBudget = maxInt(0, compressionHardCap-len(in.Snapshot.Rules)-in.OtherNewRows)
	}
	selection, ok, incomplete := selectCompressionOptions(optionsByGroup, rowBudget, uint64(in.Config.ExtraIPv4Limit), baseRisk, in.MaxStates)
	if incomplete {
		return nil, true, []CompressionBlocker{{Code: CompressionBlockedIncomplete, Message: "有损候选搜索超过上限，按证据不足阻断"}}
	}
	if !ok {
		return nil, false, []CompressionBlocker{{Code: CompressionBlockedNoCandidate, Message: "没有满足容量与风险额度的有损候选"}}
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, true, []CompressionBlocker{{Code: CompressionBlockedCanceled, Message: "规划被取消"}}
	}
	covers := make([][]netip.Prefix, len(groups))
	for i, option := range selection.chosen {
		if i >= len(covers) {
			break
		}
		covers[i] = option.Cover
	}
	rep, built, perr := buildCompressionRepresentation(in, groups, covers)
	if !built {
		return nil, false, []CompressionBlocker{{Code: CompressionBlockedNoCandidate, Message: fmt.Sprintf("有损表示构造失败: %s", perr)}}
	}
	return &rep, false, nil
}
