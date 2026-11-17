package provider

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// 本文件是 Build9 Step 2 纯压缩规划的地址/端口基础运算。
// 所有函数不访问网络、数据库、时钟或日志，可被正式同步与 Dry Run 共用。

// ipv4Range 是闭区间 [Start,End]，使用 uint64 以容纳 /0 的 2^32 个地址。
type ipv4Range struct {
	Start uint64
	End   uint64
}

// ipv4Set 是排序、合并、互不重叠的 IPv4 区间集合。
type ipv4Set []ipv4Range

func prefixIPv4Range(p netip.Prefix) (ipv4Range, bool) {
	p = p.Masked()
	addr := p.Addr()
	if !addr.Is4() {
		return ipv4Range{}, false
	}
	a := addr.As4()
	start := uint64(a[0])<<24 | uint64(a[1])<<16 | uint64(a[2])<<8 | uint64(a[3])
	bits := p.Bits()
	size := uint64(1) << uint(32-bits)
	return ipv4Range{Start: start, End: start + size - 1}, true
}

func (s ipv4Set) normalize() ipv4Set {
	if len(s) == 0 {
		return nil
	}
	out := append(ipv4Set(nil), s...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].End < out[j].End
	})
	merged := out[:0]
	for _, r := range out {
		if r.End < r.Start {
			continue
		}
		if len(merged) == 0 || r.Start > merged[len(merged)-1].End+1 {
			merged = append(merged, r)
			continue
		}
		if r.End > merged[len(merged)-1].End {
			merged[len(merged)-1].End = r.End
		}
	}
	return merged
}

func ipv4SetFromPrefixes(prefixes []netip.Prefix) ipv4Set {
	out := make(ipv4Set, 0, len(prefixes))
	for _, p := range prefixes {
		if r, ok := prefixIPv4Range(p); ok {
			out = append(out, r)
		}
	}
	return out.normalize()
}

func (s ipv4Set) union(other ipv4Set) ipv4Set {
	all := make(ipv4Set, 0, len(s)+len(other))
	all = append(all, s...)
	all = append(all, other...)
	return all.normalize()
}

// subtract 返回 s \ other。
func (s ipv4Set) subtract(other ipv4Set) ipv4Set {
	if len(s) == 0 {
		return nil
	}
	other = other.normalize()
	out := make(ipv4Set, 0, len(s))
	for _, r := range s {
		cur := r
		for _, o := range other {
			if o.End < cur.Start {
				continue
			}
			if o.Start > cur.End {
				break
			}
			if o.Start > cur.Start {
				out = append(out, ipv4Range{Start: cur.Start, End: o.Start - 1})
			}
			if o.End >= cur.End {
				cur.Start = cur.End + 1
				break
			}
			cur.Start = o.End + 1
		}
		if cur.Start <= cur.End {
			out = append(out, cur)
		}
	}
	return out.normalize()
}

func (s ipv4Set) intersect(other ipv4Set) ipv4Set {
	other = other.normalize()
	out := make(ipv4Set, 0)
	for _, a := range s {
		for _, b := range other {
			if b.End < a.Start {
				continue
			}
			if b.Start > a.End {
				break
			}
			start := a.Start
			if b.Start > start {
				start = b.Start
			}
			end := a.End
			if b.End < end {
				end = b.End
			}
			if start <= end {
				out = append(out, ipv4Range{Start: start, End: end})
			}
		}
	}
	return out.normalize()
}

func (s ipv4Set) count() uint64 {
	var n uint64
	for _, r := range s {
		n += r.End - r.Start + 1
	}
	return n
}

func (s ipv4Set) maxRun() uint64 {
	var max uint64
	for _, r := range s {
		if n := r.End - r.Start + 1; n > max {
			max = n
		}
	}
	return max
}

func (s ipv4Set) containsPrefix(p netip.Prefix) bool {
	r, ok := prefixIPv4Range(p)
	if !ok {
		return false
	}
	for _, item := range s {
		if item.Start <= r.Start && item.End >= r.End {
			return true
		}
	}
	return false
}

func (s ipv4Set) equal(other ipv4Set) bool {
	a, b := s.normalize(), other.normalize()
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeIPv4PrefixesExact 返回与输入地址集严格相等的极小 CIDR 集合。
//
// 只接受 IPv4；先去掉被更大前缀覆盖的项，再不断合并兄弟节点。结果排序稳定。
func mergeIPv4PrefixesExact(prefixes []netip.Prefix) []netip.Prefix {
	seen := make(map[netip.Prefix]bool, len(prefixes))
	unique := make([]netip.Prefix, 0, len(prefixes))
	for _, p := range prefixes {
		if !p.IsValid() || !p.Addr().Is4() {
			continue
		}
		p = p.Masked()
		if seen[p] {
			continue
		}
		seen[p] = true
		unique = append(unique, p)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		if unique[i].Addr() != unique[j].Addr() {
			return unique[i].Addr().Less(unique[j].Addr())
		}
		return unique[i].Bits() < unique[j].Bits()
	})
	// 去掉被前面更大前缀覆盖的项。
	kept := make([]netip.Prefix, 0, len(unique))
	for _, p := range unique {
		covered := false
		for _, k := range kept {
			if k.Bits() <= p.Bits() && k.Contains(p.Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			kept = append(kept, p)
		}
	}

	set := make(map[netip.Prefix]bool, len(kept))
	for _, p := range kept {
		set[p] = true
	}
	for {
		merged := false
		keys := make([]netip.Prefix, 0, len(set))
		for p := range set {
			keys = append(keys, p)
		}
		sort.SliceStable(keys, func(i, j int) bool {
			if keys[i].Addr() != keys[j].Addr() {
				return keys[i].Addr().Less(keys[j].Addr())
			}
			return keys[i].Bits() < keys[j].Bits()
		})
		for _, p := range keys {
			if !set[p] || p.Bits() == 0 {
				continue
			}
			sibling, ok := siblingPrefix(p)
			if !ok || !set[sibling] {
				continue
			}
			delete(set, p)
			delete(set, sibling)
			parent := netip.PrefixFrom(p.Addr(), p.Bits()-1).Masked()
			set[parent] = true
			merged = true
			break
		}
		if !merged {
			break
		}
	}
	out := make([]netip.Prefix, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Addr() != out[j].Addr() {
			return out[i].Addr().Less(out[j].Addr())
		}
		return out[i].Bits() < out[j].Bits()
	})
	return out
}

func siblingPrefix(p netip.Prefix) (netip.Prefix, bool) {
	if !p.IsValid() || !p.Addr().Is4() || p.Bits() == 0 {
		return netip.Prefix{}, false
	}
	a := p.Addr().As4()
	bit := uint(p.Bits() - 1)
	byteIndex := int(bit / 8)
	mask := byte(1 << (7 - (bit % 8)))
	a[byteIndex] ^= mask
	return netip.PrefixFrom(netip.AddrFrom4(a), p.Bits()).Masked(), true
}

// portItemIsFull 判断单个端口项是否代表全端口。
func portItemIsFull(item string) bool {
	s := strings.ToUpper(strings.TrimSpace(item))
	return s == "ALL" || s == "1-65535" || s == "-1/-1"
}

// parsePortItem 把一个端口项拆成单端口或闭区间。
func parsePortItem(item string) (start, end int, isRange bool, ok bool) {
	s := strings.TrimSpace(item)
	if s == "" || strings.EqualFold(s, "ALL") {
		return 0, 0, false, false
	}
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		a, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		b, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || a < 1 || b > 65535 || a > b {
			return 0, 0, false, false
		}
		if a == 1 && b == 65535 {
			return 0, 0, false, false
		}
		return a, b, true, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, 0, false, false
	}
	return n, n, false, true
}

// portExpressions 把同一协议、同一授权集合的端口项装箱为 Lighthouse Port 表达式。
//
// 离散单端口按数值升序装入逗号串（每个表达式 ≤64 字符）；范围保持为独立表达式，
// 重叠/相邻范围可无损合并。不把 range 与 discrete 混在同一字段，避免依赖官方
// “离散端口或范围”措辞的未验证混合语义。
func portExpressions(items []string) ([]string, bool) {
	full := false
	singles := make([]int, 0, len(items))
	ranges := make([][2]int, 0, len(items))
	seenSingle := make(map[int]bool)
	for _, item := range items {
		if portItemIsFull(item) {
			full = true
			continue
		}
		start, end, isRange, ok := parsePortItem(item)
		if !ok {
			return nil, false
		}
		if isRange {
			ranges = append(ranges, [2]int{start, end})
			continue
		}
		if !seenSingle[start] {
			seenSingle[start] = true
			singles = append(singles, start)
		}
	}
	if full {
		return []string{"ALL"}, true
	}
	sort.Ints(singles)
	sort.SliceStable(ranges, func(i, j int) bool {
		if ranges[i][0] != ranges[j][0] {
			return ranges[i][0] < ranges[j][0]
		}
		return ranges[i][1] < ranges[j][1]
	})
	mergedRanges := make([][2]int, 0, len(ranges))
	for _, r := range ranges {
		if len(mergedRanges) == 0 || r[0] > mergedRanges[len(mergedRanges)-1][1]+1 {
			mergedRanges = append(mergedRanges, r)
			continue
		}
		if r[1] > mergedRanges[len(mergedRanges)-1][1] {
			mergedRanges[len(mergedRanges)-1][1] = r[1]
		}
	}
	// 单端口若已被范围覆盖，删除冗余项，避免同一授权生成两条重叠物理行。
	filteredSingles := singles[:0]
	for _, port := range singles {
		covered := false
		for _, r := range mergedRanges {
			if port >= r[0] && port <= r[1] {
				covered = true
				break
			}
		}
		if !covered {
			filteredSingles = append(filteredSingles, port)
		}
	}
	singles = filteredSingles
	expressions := make([]string, 0, len(mergedRanges)+1)
	current := ""
	for _, port := range singles {
		part := strconv.Itoa(port)
		switch {
		case current == "":
			current = part
		case len(current)+1+len(part) <= 64:
			current += "," + part
		default:
			expressions = append(expressions, current)
			current = part
		}
	}
	if current != "" {
		expressions = append(expressions, current)
	}
	for _, r := range mergedRanges {
		expressions = append(expressions, fmt.Sprintf("%d-%d", r[0], r[1]))
	}
	sort.SliceStable(expressions, func(i, j int) bool {
		si, sj := firstPortNumber(expressions[i]), firstPortNumber(expressions[j])
		if si != sj {
			return si < sj
		}
		return expressions[i] < expressions[j]
	})
	return expressions, true
}

func firstPortNumber(expr string) int {
	s := strings.TrimSpace(expr)
	if s == "ALL" {
		return 0
	}
	if idx := strings.IndexAny(s, ",-"); idx >= 0 {
		s = s[:idx]
	}
	n, _ := strconv.Atoi(s)
	return n
}

// portExpressionCovers 判断表达式是否完整覆盖一个授权端口项，且不引入该项之外的端口。
// 该函数用于 witness 校验；规划器生成的表达式按同一授权集合装箱，正常应恒为 true。
func portExpressionCovers(expr, item string) bool {
	expr = strings.TrimSpace(expr)
	item = strings.TrimSpace(item)
	if portItemIsFull(item) {
		return portItemIsFull(expr)
	}
	start, end, isRange, ok := parsePortItem(item)
	if !ok {
		return false
	}
	if strings.EqualFold(expr, "ALL") {
		return true
	}
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		es, ee, eRange, eok := parsePortItem(part)
		if !eok {
			continue
		}
		if !isRange && !eRange && es == start && ee == end {
			return true
		}
		if isRange && eRange && es <= start && ee >= end {
			return true
		}
	}
	return false
}

func portExpressionOverlaps(expr, item string) bool {
	expr = strings.TrimSpace(expr)
	item = strings.TrimSpace(item)
	if portItemIsFull(expr) || portItemIsFull(item) {
		return true
	}
	s, e, _, ok := parsePortItem(item)
	if !ok {
		return false
	}
	for _, part := range strings.Split(expr, ",") {
		ps, pe, _, pok := parsePortItem(strings.TrimSpace(part))
		if pok && ps <= e && pe >= s {
			return true
		}
	}
	return false
}

func (s ipv4Set) intersectsPrefix(p netip.Prefix) bool {
	r, ok := prefixIPv4Range(p)
	if !ok {
		return false
	}
	for _, item := range s {
		if item.Start <= r.End && item.End >= r.Start {
			return true
		}
	}
	return false
}

func ipv4AddrString(v uint64) string {
	a := [4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	return netip.AddrFrom4(a).String()
}

func ipv4RangeStrings(s ipv4Set) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, ipv4AddrString(r.Start)+"-"+ipv4AddrString(r.End))
	}
	return out
}

func (s ipv4Set) containsAddr(addr netip.Addr) bool {
	if !addr.Is4() {
		return false
	}
	a := addr.As4()
	v := uint64(a[0])<<24 | uint64(a[1])<<16 | uint64(a[2])<<8 | uint64(a[3])
	for _, r := range s {
		if r.Start <= v && r.End >= v {
			return true
		}
	}
	return false
}

func (s ipv4Set) isSubset(other ipv4Set) bool {
	return len(s.subtract(other)) == 0
}

// coveringPrefix 返回同时覆盖 a、b 的最小 CIDR 前缀（最长公共前缀）。
func coveringPrefix(a, b netip.Prefix) (netip.Prefix, bool) {
	a, b = a.Masked(), b.Masked()
	if !a.IsValid() || !b.IsValid() || !a.Addr().Is4() || !b.Addr().Is4() {
		return netip.Prefix{}, false
	}
	ab, bb := a.Addr().As4(), b.Addr().As4()
	bits := 0
	for i := 0; i < 4; i++ {
		x := ab[i] ^ bb[i]
		if x == 0 {
			bits += 8
			continue
		}
		for mask := byte(0x80); mask != 0; mask >>= 1 {
			if x&mask != 0 {
				break
			}
			bits++
		}
		break
	}
	if bits > a.Bits() {
		bits = a.Bits()
	}
	if bits > b.Bits() {
		bits = b.Bits()
	}
	return netip.PrefixFrom(a.Addr(), bits).Masked(), true
}
