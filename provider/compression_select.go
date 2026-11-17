package provider

import (
	"net/netip"
	"sort"
	"strings"
)

// compressionCoverOption 是一个授权组的物理行覆盖选项。
type compressionCoverOption struct {
	Rows  int
	Cover []netip.Prefix
	Extra ipv4Set
	Key   string
}

// pruneCompressionOptions 只删除全局安全的被支配项：
// A.Rows <= B.Rows 且 A.Extra ⊆ B.Extra 时，B 不可能在任何组合中胜出。
//
// 不能仅按 Rows 或单组 Extra 数量裁剪；不同 Extra 集合可能在联合选择中重叠。
func pruneCompressionOptions(options []compressionCoverOption) []compressionCoverOption {
	out := make([]compressionCoverOption, 0, len(options))
	for i, candidate := range options {
		dominated := false
		for j, other := range options {
			if i == j {
				continue
			}
			if other.Rows <= candidate.Rows && other.Extra.isSubset(candidate.Extra) {
				// 完全相同的 rows/extra 时保留字典序更小的一项，避免重复。
				if other.Rows == candidate.Rows && candidate.Extra.equal(other.Extra) {
					if other.Key < candidate.Key {
						dominated = true
						break
					}
					continue
				}
				dominated = true
				break
			}
		}
		if !dominated {
			out = append(out, candidate)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rows != out[j].Rows {
			return out[i].Rows < out[j].Rows
		}
		return out[i].Key < out[j].Key
	})
	return out
}

type compressionSelection struct {
	chosen []compressionCoverOption
	risk   ipv4Set
	rows   int
	key    string
}

// selectCompressionOptions 为每个授权组选择一个覆盖选项，满足总行数与风险上限。
//
// 小规模穷举；超过 maxStates 个递归状态时返回 incomplete=true，由调用方按
// “证据不足”阻断，绝不返回部分计划。
func selectCompressionOptions(groups [][]compressionCoverOption, rowBudget int, riskLimit uint64, baseRisk ipv4Set, maxStates int) (compressionSelection, bool, bool) {
	if maxStates <= 0 {
		maxStates = 200000
	}
	pruned := make([][]compressionCoverOption, len(groups))
	for i, group := range groups {
		pruned[i] = pruneCompressionOptions(group)
		if len(pruned[i]) == 0 {
			return compressionSelection{}, false, false
		}
	}
	var best *compressionSelection
	states := 0
	incomplete := false
	var walk func(index, rows int, risk ipv4Set, chosen []compressionCoverOption)
	walk = func(index, rows int, risk ipv4Set, chosen []compressionCoverOption) {
		if incomplete {
			return
		}
		states++
		if states > maxStates {
			incomplete = true
			return
		}
		if rows > rowBudget || risk.count() > riskLimit {
			return
		}
		if index == len(pruned) {
			keyParts := make([]string, len(chosen))
			for i := range chosen {
				keyParts[i] = chosen[i].Key
			}
			candidate := compressionSelection{
				chosen: append([]compressionCoverOption(nil), chosen...),
				risk:   risk,
				rows:   rows,
				key:    strings.Join(keyParts, "\x00"),
			}
			if best == nil || betterSelection(candidate, *best) {
				best = &candidate
			}
			return
		}
		for _, option := range pruned[index] {
			nextRisk := risk.union(option.Extra)
			walk(index+1, rows+option.Rows, nextRisk, append(chosen, option))
		}
	}
	walk(0, 0, baseRisk, nil)
	if incomplete {
		return compressionSelection{}, false, true
	}
	if best == nil {
		return compressionSelection{}, false, false
	}
	return *best, true, false
}

func betterSelection(a, b compressionSelection) bool {
	if a.risk.count() != b.risk.count() {
		return a.risk.count() < b.risk.count()
	}
	if a.risk.maxRun() != b.risk.maxRun() {
		return a.risk.maxRun() < b.risk.maxRun()
	}
	if a.rows != b.rows {
		return a.rows < b.rows
	}
	return a.key < b.key
}
