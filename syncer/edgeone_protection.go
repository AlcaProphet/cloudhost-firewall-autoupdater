package syncer

import (
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
)

// 本文件是 Build8 Step 11 的过渡保护纯逻辑：
//   - Step 10 旧行的 protected_cidrs 可能为空，读取时按 pending_cidrs 兼容；
//   - 观察到待更新版时保护其规范前缀；
//   - 只有有效查询证明 pending 成为 current 且规范前缀集合完全一致才释放；
//   - 释放后若仍有旧替换留下的未释放前缀，用 base_version == pending_version
//     表示“无活动待更新版，仅保护残留”，避免清空尚未证明可释放的证据。
//
// 本文件不访问数据库或云；持久化与目标执行由调用方负责。

// edgeOneTransitionPlanResult 是一次过渡记录演进的内存结果。
type edgeOneTransitionPlanResult struct {
	next              *config.EdgeOneTransition
	delete            bool
	changed           bool
	resetConfirmation bool
	protected         []netip.Prefix
}

func normalizeEdgeOnePrefixes(prefixes []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(prefixes))
	seen := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		if !p.IsValid() {
			continue
		}
		s := p.Masked().String()
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, p.Masked())
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func sameEdgeOnePrefixSet(a, b []netip.Prefix) bool {
	left, right := normalizeEdgeOnePrefixes(a), normalizeEdgeOnePrefixes(b)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func unionEdgeOnePrefixes(a, b []netip.Prefix) []netip.Prefix {
	return normalizeEdgeOnePrefixes(append(append([]netip.Prefix(nil), a...), b...))
}

func subtractEdgeOnePrefixes(all, remove []netip.Prefix) []netip.Prefix {
	removeSet := make(map[string]bool, len(remove))
	for _, p := range normalizeEdgeOnePrefixes(remove) {
		removeSet[p.String()] = true
	}
	out := make([]netip.Prefix, 0, len(all))
	for _, p := range normalizeEdgeOnePrefixes(all) {
		if !removeSet[p.String()] {
			out = append(out, p)
		}
	}
	return out
}

func cloneEdgeOnePrefixes(prefixes []netip.Prefix) []netip.Prefix {
	return append([]netip.Prefix(nil), prefixes...)
}

// edgeOneTransitionProtected 返回过渡行当前仍可能用于回源的保护前缀。
//
// Step 10 旧行只有 pending_cidrs、protected_cidrs 为空；活动过渡
// （base_version != pending_version）按 pending 兼容。释放完成后
// base_version == pending_version，此时 protected_cidrs 是唯一依据，
// 即使为空也不能再回退到 pending。
func edgeOneTransitionProtected(t *config.EdgeOneTransition) []netip.Prefix {
	if t == nil {
		return nil
	}
	protected := normalizeEdgeOnePrefixes(t.ProtectedCIDRs)
	if strings.TrimSpace(t.BaseVersion) != "" && t.BaseVersion == t.PendingVersion {
		return protected
	}
	if len(protected) == 0 {
		return normalizeEdgeOnePrefixes(t.PendingCIDRs)
	}
	return protected
}

func copyTransition(t config.EdgeOneTransition) config.EdgeOneTransition {
	t.PendingCIDRs = cloneEdgeOnePrefixes(t.PendingCIDRs)
	t.ProtectedCIDRs = cloneEdgeOnePrefixes(t.ProtectedCIDRs)
	return t
}

func sameTransitionPending(t config.EdgeOneTransition, version string, prefixes []netip.Prefix) bool {
	return t.PendingVersion == strings.TrimSpace(version) && sameEdgeOnePrefixSet(t.PendingCIDRs, prefixes)
}

// planEdgeOneTransition 计算一次有效快照下的过渡记录演进。
//
// snapshot 必须是已校验有效的当前快照；调用方在查询失败/不可用时只做旧行
// 保护回填，不调用本函数执行释放。
func planEdgeOneTransition(row *config.EdgeOneTransition, snapshot provider.OriginACLSnapshot, now time.Time) edgeOneTransitionPlanResult {
	if snapshot.ZoneID == "" || strings.TrimSpace(snapshot.Current.Version) == "" ||
		!provider.OriginACLSnapshotPrefixesValid(snapshot) {
		return edgeOneTransitionPlanResult{}
	}
	currentPrefixes := normalizeEdgeOnePrefixes(snapshot.Current.Prefixes)
	var nextPrefixes []netip.Prefix
	nextVersion := ""
	if snapshot.Next != nil {
		nextVersion = strings.TrimSpace(snapshot.Next.Version)
		nextPrefixes = normalizeEdgeOnePrefixes(snapshot.Next.Prefixes)
	}

	if row == nil {
		if nextVersion == "" {
			return edgeOneTransitionPlanResult{}
		}
		next := &config.EdgeOneTransition{
			ZoneID:            snapshot.ZoneID,
			BaseVersion:       snapshot.Current.Version,
			PendingVersion:    nextVersion,
			PendingCIDRs:      nextPrefixes,
			ProtectedCIDRs:    nextPrefixes,
			ConfirmationState: config.EdgeOneConfirmationNotAttempted,
			UpdatedAt:         now.UTC(),
		}
		return edgeOneTransitionPlanResult{next: next, changed: true, resetConfirmation: true, protected: nextPrefixes}
	}

	cp := copyTransition(*row)
	original := copyTransition(*row)
	protected := edgeOneTransitionProtected(row)
	releasedMode := strings.TrimSpace(cp.BaseVersion) != "" && cp.BaseVersion == cp.PendingVersion
	if !releasedMode {
		// 活动过渡必须始终保护待更新前缀；兼容历史记录中 protected 非空
		// 但未包含当前 pending 的形态。
		protected = unionEdgeOnePrefixes(protected, cp.PendingCIDRs)
	}
	changed := !sameEdgeOnePrefixSet(protected, edgeOneTransitionProtected(row))
	reset := false

	setPending := func(version string, prefixes []netip.Prefix, base string) {
		cp.BaseVersion = base
		cp.PendingVersion = version
		cp.PendingCIDRs = prefixes
		cp.ProtectedCIDRs = protected
		cp.ConfirmationState = config.EdgeOneConfirmationNotAttempted
		cp.ConfirmationRequestID = ""
		reset = true
		changed = true
	}

	if releasedMode {
		// 已释放的版本不再参与释放判定；保留旧替换留下的保护残留。
		if nextVersion != "" {
			protected = unionEdgeOnePrefixes(protected, nextPrefixes)
			setPending(nextVersion, nextPrefixes, snapshot.Current.Version)
		} else {
			if !sameEdgeOnePrefixSet(cp.PendingCIDRs, currentPrefixes) ||
				cp.BaseVersion != snapshot.Current.Version || cp.PendingVersion != snapshot.Current.Version {
				cp.BaseVersion = snapshot.Current.Version
				cp.PendingVersion = snapshot.Current.Version
				cp.PendingCIDRs = currentPrefixes
				cp.ProtectedCIDRs = protected
				changed = true
			}
			if len(protected) == 0 {
				return edgeOneTransitionPlanResult{delete: true, changed: true, protected: nil}
			}
		}
	} else if snapshot.Current.Version == cp.PendingVersion {
		if sameEdgeOnePrefixSet(snapshot.Current.Prefixes, cp.PendingCIDRs) {
			remaining := subtractEdgeOnePrefixes(protected, cp.PendingCIDRs)
			if nextVersion != "" {
				protected = unionEdgeOnePrefixes(remaining, nextPrefixes)
				setPending(nextVersion, nextPrefixes, snapshot.Current.Version)
			} else if len(remaining) == 0 {
				return edgeOneTransitionPlanResult{delete: true, changed: true, protected: nil}
			} else {
				protected = remaining
				// base == pending 表示已释放、仅保留残留；不能再次按待更新版释放。
				setPending(snapshot.Current.Version, currentPrefixes, snapshot.Current.Version)
			}
		} else {
			// 版本已成为当前但规范网段不完全相符：保留活动过渡与保护证据。
			if nextVersion != "" {
				if !sameTransitionPending(cp, nextVersion, nextPrefixes) {
					protected = unionEdgeOnePrefixes(protected, nextPrefixes)
					setPending(nextVersion, nextPrefixes, cp.BaseVersion)
				} else {
					cp.ProtectedCIDRs = protected
				}
			} else {
				cp.ProtectedCIDRs = protected
			}
			if !sameEdgeOnePrefixSet(cp.ProtectedCIDRs, protected) {
				cp.ProtectedCIDRs = cloneEdgeOnePrefixes(protected)
				changed = true
			}
		}
	} else {
		// 当前版不是追踪的待更新版。更新基准版，但保留待更新保护。
		if nextVersion != "" {
			if !sameTransitionPending(cp, nextVersion, nextPrefixes) {
				protected = unionEdgeOnePrefixes(protected, nextPrefixes)
				setPending(nextVersion, nextPrefixes, snapshot.Current.Version)
			} else {
				if cp.BaseVersion != snapshot.Current.Version {
					cp.BaseVersion = snapshot.Current.Version
					changed = true
				}
				cp.ProtectedCIDRs = protected
			}
		} else {
			if cp.BaseVersion != snapshot.Current.Version {
				cp.BaseVersion = snapshot.Current.Version
				changed = true
			}
			if !sameEdgeOnePrefixSet(cp.ProtectedCIDRs, protected) {
				cp.ProtectedCIDRs = cloneEdgeOnePrefixes(protected)
				changed = true
			}
		}
	}

	if !changed {
		// 兼容 Step 10 空保护行：把 pending 回填为显式保护，确认状态不变。
		if len(normalizeEdgeOnePrefixes(original.ProtectedCIDRs)) == 0 && len(protected) > 0 {
			cp.ProtectedCIDRs = cloneEdgeOnePrefixes(protected)
			changed = true
		}
	}
	if !changed {
		return edgeOneTransitionPlanResult{protected: protected}
	}
	cp.UpdatedAt = now.UTC()
	return edgeOneTransitionPlanResult{
		next:              &cp,
		changed:           true,
		resetConfirmation: reset,
		protected:         edgeOneTransitionProtected(&cp),
	}
}

// edgeOneTransitionBackfill 在快照不可用/查询失败时只回填 Step 10 空保护。
func edgeOneTransitionBackfill(row *config.EdgeOneTransition, now time.Time) edgeOneTransitionPlanResult {
	if row == nil {
		return edgeOneTransitionPlanResult{}
	}
	cp := copyTransition(*row)
	protected := edgeOneTransitionProtected(row)
	if len(normalizeEdgeOnePrefixes(cp.ProtectedCIDRs)) == 0 && len(protected) > 0 {
		cp.ProtectedCIDRs = cloneEdgeOnePrefixes(protected)
		cp.UpdatedAt = now.UTC()
		return edgeOneTransitionPlanResult{next: &cp, changed: true, protected: protected}
	}
	return edgeOneTransitionPlanResult{protected: protected}
}

// protectedPrefixesOverlapCandidate 判断一个云端规则的 CIDR 是否落入保护范围。
//
// 采用双向包含的保守判定：候选比保护网段更宽时删除同样会移除受保护源站。
// CIDR 解析失败时按受保护处理，避免把无法判定的候选交给自动删除。
func protectedPrefixesOverlapCandidate(protected []netip.Prefix, cidr string) bool {
	candidate, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return true
	}
	candidate = candidate.Masked()
	for _, p := range normalizeEdgeOnePrefixes(protected) {
		if p.Addr().BitLen() != candidate.Addr().BitLen() {
			continue
		}
		if p.Contains(candidate.Addr()) || candidate.Contains(p.Addr()) {
			return true
		}
	}
	return false
}

func protectedEdgeOneCIDRStrings(prefixes []netip.Prefix) []string {
	normalized := normalizeEdgeOnePrefixes(prefixes)
	out := make([]string, 0, len(normalized))
	for _, p := range normalized {
		out = append(out, p.String())
	}
	return out
}
