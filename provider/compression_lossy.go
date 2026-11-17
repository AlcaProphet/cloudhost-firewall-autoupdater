package provider

import (
	"context"
	"net/netip"
	"sort"
	"strings"
)

type compressionCoverOption struct {
	rows    []CompressionRow
	extra   ipv4Set
	adds    int
	current int
}

type compressionCoverTree struct {
	option      *compressionCoverOption
	left, right *compressionCoverTree
	cost        []uint64
}

type compressionSearchBudget struct {
	ctx       context.Context
	remaining int
	blocker   *CompressionBlocker
}

func (b *compressionSearchBudget) take() bool {
	if b.blocker != nil {
		return false
	}
	switch {
	case b.ctx != nil && b.ctx.Err() != nil:
		b.blocker = &CompressionBlocker{Code: CompressionBlockedCanceled, Message: "规划被取消"}
	case b.remaining == 0:
		b.blocker = &CompressionBlocker{Code: CompressionBlockedIncomplete, Message: "候选生成或联合搜索超过状态上限"}
	default:
		b.remaining--
		return true
	}
	return false
}

// buildCoverTree 保留风险额度内的父前缀与子树表示，包括可复用的更宽 S0 行。
func (p compressionPlanner) buildCoverTree(group compressionAuthGroup, prefix netip.Prefix, required []netip.Prefix,
	budget *compressionSearchBudget, rowBudget int, rejections map[string]bool,
) *compressionCoverTree {
	if !budget.take() {
		return nil
	}
	var left, right *compressionCoverTree
	leaf := len(required) == 1 && prefix == required[0]
	if !leaf {
		childBits := prefix.Bits() + 1
		leftPrefix := netip.PrefixFrom(prefix.Addr(), childBits)
		rightPrefix, _ := siblingPrefix(leftPrefix)
		split := sort.Search(len(required), func(i int) bool { return !leftPrefix.Contains(required[i].Addr()) })
		if split > 0 {
			left = p.buildCoverTree(group, leftPrefix, required[:split], budget, rowBudget, rejections)
		}
		if split < len(required) {
			right = p.buildCoverTree(group, rightPrefix, required[split:], budget, rowBudget, rejections)
		}
		if budget.blocker != nil {
			return nil
		}
	}
	tree := &compressionCoverTree{left: left, right: right}
	unary := !leaf && (left == nil || right == nil)
	if leaf || !group.exactOnly {
		rows := p.rows(group, prefix)
		option := &compressionCoverOption{rows: rows}
		for _, row := range rows {
			if row.Reused {
				continue
			}
			option.adds++
			if !row.ServesNext || row.ServesCurrent {
				option.current++
			}
		}
		if p.hitsDrop(rows) {
			rejections[CompressionBlockedOppositeAction] = true
		} else if option.adds <= rowBudget {
			if !leaf {
				addresses := ipv4SetFromPrefixes([]netip.Prefix{prefix})
				for _, port := range group.ports {
					option.extra = option.extra.union(p.risk.candidateExtra(port, addresses))
				}
			}
			if p.hitsDNS(option.extra) {
				rejections[CompressionBlockedDNSHit] = true
			} else if p.risk.oldExtra.union(option.extra).count() <= uint64(p.in.Config.ExtraIPv4Limit) {
				tree.option = option
			}
		}
	}
	if tree.option == nil && unary {
		if left == nil {
			return right
		}
		return left
	}
	tree.cost = coverTreeCosts(tree, p.risk.oldExtra, rowBudget)
	return tree
}

func (p compressionPlanner) hitsDNS(extra ipv4Set) bool {
	if p.in.DNSEnabled {
		for _, addr := range p.in.DNSAddresses {
			if extra.containsAddr(addr.Unmap()) {
				return true
			}
		}
	}
	return false
}

func (p compressionPlanner) planLossy(ctx context.Context, groups []compressionAuthGroup) (
	*CompressionRepresentation, CompressionCapacity, bool, []CompressionBlocker,
) {
	if p.in.DNSEnabled && !p.in.DNSComplete {
		return nil, CompressionCapacity{}, false, []CompressionBlocker{{
			Code: CompressionBlockedDNSIncomplete, Message: "DNS 数据不完整，不能使用有损扩张",
		}}
	}
	maxStates := p.in.MaxStates
	if maxStates <= 0 {
		maxStates = 200000
	}
	budget := compressionSearchBudget{ctx: ctx, remaining: maxStates}
	baseRows := len(p.in.Snapshot.Rules) + p.in.OtherNewRows
	normalBudget := max(0, compressionNormalCap-baseRows)
	rowBudget := normalBudget
	if p.pending && p.in.AssessCleanup != nil && strings.TrimSpace(p.in.PendingFingerprint) != "" {
		rowBudget = max(0, compressionHardCap-baseRows)
	}
	var roots []*compressionCoverTree
	rejections := make(map[string]bool)
	for _, group := range groups {
		root := p.buildCoverTree(group, netip.PrefixFrom(netip.IPv4Unspecified(), 0), group.prefixes, &budget, rowBudget, rejections)
		if budget.blocker != nil {
			return nil, CompressionCapacity{}, true, []CompressionBlocker{*budget.blocker}
		}
		roots = append(roots, root)
	}
	search := compressionCoverSearch{
		planner: p, budget: &budget, roots: roots, rowBudget: rowBudget,
		normalBudget: normalBudget, limit: uint64(p.in.Config.ExtraIPv4Limit),
	}
	search.run()
	if budget.blocker != nil {
		return nil, CompressionCapacity{}, true, []CompressionBlocker{*budget.blocker}
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, CompressionCapacity{}, true, []CompressionBlocker{{
			Code: CompressionBlockedCanceled, Message: "规划被取消",
		}}
	}
	if search.best.rep != nil {
		return search.best.rep, search.best.capacity, false, nil
	}
	blockers := []CompressionBlocker{{
		Code: CompressionBlockedNoCandidate, Message: "没有满足容量与风险额度的压缩候选",
	}}
	if rejections[CompressionBlockedOppositeAction] {
		blockers = append(blockers, compressionOppositeAction)
	}
	if rejections[CompressionBlockedDNSHit] {
		blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedDNSHit, Message: "有损候选的额外放行范围包含 DNS 地址"})
	}
	for _, group := range groups {
		if group.exactOnly {
			blockers = append(blockers, CompressionBlocker{Code: CompressionBlockedLossyForbidden, Message: "全端口授权组只允许无损表示"})
			break
		}
	}
	return nil, CompressionCapacity{}, false, blockers
}
