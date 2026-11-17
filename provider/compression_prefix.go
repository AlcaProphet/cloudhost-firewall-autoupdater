package provider

import (
	"fmt"
	"math/bits"
	"net/netip"
	"slices"
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

// compressionPortSpan 表示 TCP/UDP 闭区间；端点切分后可独立计算授权与风险。
type compressionPortSpan struct {
	first, last int
}

var allCompressionPorts = compressionPortSpan{1, 65535}

func (s compressionPortSpan) String() string {
	if s.first == s.last {
		return strconv.Itoa(s.first)
	}
	return fmt.Sprintf("%d-%d", s.first, s.last)
}

func (s compressionPortSpan) overlaps(other compressionPortSpan) bool {
	return s.first <= other.last && other.first <= s.last
}

func compressionPortSpans(expr string) ([]compressionPortSpan, bool) {
	var spans []compressionPortSpan
	for _, item := range strings.Split(strings.TrimSpace(expr), ",") {
		item = strings.TrimSpace(item)
		if strings.EqualFold(item, "ALL") || item == "-1/-1" {
			spans = append(spans, allCompressionPorts)
			continue
		}
		start, end, isRange := strings.Cut(item, "-")
		first, err := strconv.Atoi(strings.TrimSpace(start))
		if err != nil || first < 1 || first > 65535 {
			return nil, false
		}
		last := first
		if isRange {
			last, err = strconv.Atoi(strings.TrimSpace(end))
			if err != nil || last < first || last > 65535 {
				return nil, false
			}
		}
		spans = append(spans, compressionPortSpan{first, last})
	}
	return spans, true
}

// splitCompressionPorts 只在输入端口的首尾切分，区间内部的授权和放行保持相同。
func splitCompressionPorts(spans []compressionPortSpan) []compressionPortSpan {
	edges := make([]int, 0, len(spans)*2)
	for _, span := range spans {
		edges = append(edges, span.first, span.last+1)
	}
	slices.Sort(edges)
	edges = slices.Compact(edges)
	var parts []compressionPortSpan
	for i := 1; i < len(edges); i++ {
		parts = append(parts, compressionPortSpan{edges[i-1], edges[i] - 1})
	}
	return parts
}

func compressionPortsFull(spans []compressionPortSpan) bool {
	end := 0
	ordered := slices.Clone(spans)
	slices.SortFunc(ordered, func(a, b compressionPortSpan) int { return a.first - b.first })
	for _, span := range ordered {
		if span.first > end+1 {
			return false
		}
		end = max(end, span.last)
	}
	return end == allCompressionPorts.last
}

// portExpressions 把同一协议、同一授权集合的端口项装箱为 Lighthouse Port 表达式。
//
// 离散单端口按数值升序装入逗号串（每个表达式 ≤64 字符）；范围保持为独立表达式，
// 重叠/相邻范围可无损合并。不把 range 与 discrete 混在同一字段，避免依赖官方
// “离散端口或范围”措辞的未验证混合语义。
func portExpressions(spans []compressionPortSpan, rangedParts map[compressionPortSpan]bool) []string {
	if compressionPortsFull(spans) {
		return []string{"ALL"}
	}
	var singles []int
	var ranges [][2]int
	for _, span := range spans {
		if !rangedParts[span] && span.first == span.last {
			singles = append(singles, span.first)
		} else {
			ranges = append(ranges, [2]int{span.first, span.last})
		}
	}
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
	expressions := make([]string, 0, len(mergedRanges)+1)
	for _, r := range mergedRanges {
		if r[0] == r[1] {
			singles = append(singles, r[0])
		}
	}
	sort.Ints(singles)
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
		if r[0] != r[1] {
			expressions = append(expressions, fmt.Sprintf("%d-%d", r[0], r[1]))
		}
	}
	sort.SliceStable(expressions, func(i, j int) bool {
		si, sj := firstPortNumber(expressions[i]), firstPortNumber(expressions[j])
		if si != sj {
			return si < sj
		}
		return expressions[i] < expressions[j]
	})
	return expressions
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

// ipv4SetPrefixes 将已归并的地址区间转换为最小等价 CIDR 集合。
func ipv4SetPrefixes(set ipv4Set) []netip.Prefix {
	var out []netip.Prefix
	for _, span := range set {
		for first := span.Start; first <= span.End; {
			sizeBits := min(bits.TrailingZeros64(first), bits.Len64(span.End-first+1)-1)
			addr := netip.AddrFrom4([4]byte{byte(first >> 24), byte(first >> 16), byte(first >> 8), byte(first)})
			out = append(out, netip.PrefixFrom(addr, 32-sizeBits))
			first += uint64(1) << sizeBits
		}
	}
	return out
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
