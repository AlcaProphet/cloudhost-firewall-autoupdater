package provider

import (
	"math"
	"net/netip"
	"slices"
	"strings"
)

// 同组前缀不相交，所以扣除 S0 残留后的风险在子树间可加。
// 此 DP 只用作风险下界，同条数但额外地址集、复用行或清理条件不同的候选仍分别搜索。
const noCoverCost uint64 = math.MaxUint64

func coverTreeCosts(tree *compressionCoverTree, oldExtra ipv4Set, limit int) []uint64 {
	var cost []uint64
	switch {
	case tree.left != nil && tree.right != nil:
		cost = combineCoverCosts(tree.left.cost, tree.right.cost, limit)
	case tree.left != nil:
		cost = slices.Clone(tree.left.cost)
	case tree.right != nil:
		cost = slices.Clone(tree.right.cost)
	}
	if option := tree.option; option != nil {
		for len(cost) <= option.adds {
			cost = append(cost, noCoverCost)
		}
		cost[option.adds] = min(cost[option.adds], option.extra.subtract(oldExtra).count())
	}
	return cost
}

func combineCoverCosts(a, b []uint64, limit int) []uint64 {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	out := make([]uint64, min(limit+1, len(a)+len(b)-1))
	for i := range out {
		out[i] = noCoverCost
	}
	for i, left := range a {
		if left == noCoverCost {
			continue
		}
		for j, right := range b[:min(len(b), max(0, len(out)-i))] {
			if right != noCoverCost {
				out[i+j] = min(out[i+j], left+right)
			}
		}
	}
	return out
}

func minCoverCost(cost []uint64, rowBudget int) uint64 {
	if rowBudget < 0 || len(cost) == 0 {
		return noCoverCost
	}
	return slices.Min(cost[:min(len(cost), rowBudget+1)])
}

type compressionCoverTask struct {
	tree *compressionCoverTree
	next *compressionCoverTask
	cost []uint64
}

func pushCoverTask(tree *compressionCoverTree, next *compressionCoverTask, limit int) *compressionCoverTask {
	if tree == nil {
		return next
	}
	task := &compressionCoverTask{tree: tree, next: next, cost: tree.cost}
	if next != nil {
		task.cost = combineCoverCosts(tree.cost, next.cost, limit)
	}
	return task
}

type compressionCoverResult struct {
	rep      *CompressionRepresentation
	capacity CompressionCapacity
	risk     uint64
	maxRun   uint64
	key      string
}

type compressionCoverSearch struct {
	planner      compressionPlanner
	budget       *compressionSearchBudget
	roots        []*compressionCoverTree
	rowBudget    int
	normalBudget int
	limit        uint64
	narrowing    bool
	remainingMin []int
	best         compressionCoverResult
}

// run 先扣除后续各组的最少新增行。跨组风险按实际并集计算；只有当前组
// 的下界可与该组已选前缀相加，避免把跨组重叠的额外地址计两次而误剪。
func (s *compressionCoverSearch) run() {
	s.remainingMin = make([]int, len(s.roots)+1)
	for i := len(s.roots) - 1; i >= 0; i-- {
		minimum := slices.IndexFunc(s.roots[i].cost, func(cost uint64) bool { return cost != noCoverCost })
		if minimum < 0 {
			return
		}
		s.remainingMin[i] = minimum + s.remainingMin[i+1]
	}
	start := pushCoverTask(s.roots[0], nil, s.rowBudget)
	s.walk(0, start, 0, 0, nil, nil, nil)
}

func (s *compressionCoverSearch) walk(group int, task *compressionCoverTask, adds, current int, risk, groupExtra ipv4Set, rows []CompressionRow) {
	if !s.budget.take() {
		return
	}
	baseRisk := s.planner.risk.oldExtra
	if s.narrowing {
		// Options already proved containment within S0 per port. Rank
		// replacements by their retained extra addresses after old rows leave.
		baseRisk = nil
	}
	total := baseRisk.union(risk)
	limit := s.limit
	if s.best.rep != nil {
		limit = min(limit, s.best.risk)
	}
	if total.count() > limit || adds+s.remainingMin[group+1] > s.rowBudget || current > s.normalBudget {
		return
	}
	if task == nil {
		if group+1 < len(s.roots) {
			next := pushCoverTask(s.roots[group+1], nil, s.rowBudget-adds)
			s.walk(group+1, next, adds, current, risk, nil, rows)
			return
		}
		s.consider(rows, total, adds)
		return
	}

	available := s.rowBudget - adds - s.remainingMin[group+1]
	extraSoFar := baseRisk.union(groupExtra).count()
	lowerBound := minCoverCost(task.cost, available)
	if lowerBound == noCoverCost || extraSoFar+lowerBound > limit {
		return
	}
	tree := task.tree
	var next *compressionCoverTask
	if tree.left != nil || tree.right != nil {
		next = pushCoverTask(tree.right, task.next, available)
		next = pushCoverTask(tree.left, next, available)
	}
	take := func() {
		if option := tree.option; option != nil && option.adds <= available {
			s.walk(group, task.next, adds+option.adds, current+option.current,
				risk.union(option.extra), groupExtra.union(option.extra), append(rows, option.rows...))
		}
	}
	// 优先访问风险下界更低的分支，尽早取得可用于剪枝的可行解。
	if tree.option != nil && next != nil {
		childCost := minCoverCost(next.cost, available)
		parentCost := tree.option.extra.subtract(baseRisk).count()
		if task.next != nil {
			tail := minCoverCost(task.next.cost, available-tree.option.adds)
			if tail == noCoverCost {
				parentCost = noCoverCost
			} else {
				parentCost += tail
			}
		}
		if childCost < parentCost {
			s.walk(group, next, adds, current, risk, groupExtra, rows)
			take()
			return
		}
	}
	take()
	if next != nil {
		s.walk(group, next, adds, current, risk, groupExtra, rows)
	}
}

func (s *compressionCoverSearch) consider(rows []CompressionRow, risk ipv4Set, adds int) {
	count, maxRun := risk.count(), risk.maxRun()
	best := s.best
	if best.rep != nil && (count > best.risk ||
		count == best.risk && (maxRun > best.maxRun || maxRun == best.maxRun && adds > len(best.rep.AddRows))) {
		return
	}
	rep := s.planner.representation(rows)
	capacity := s.planner.capacity(rep)
	if capacity.Mode == CompressionModeBlocked {
		return
	}
	parts := make([]string, 0, len(rep.Rows))
	for _, row := range rep.Rows {
		parts = append(parts, row.CIDR+"|"+row.Protocol+"|"+row.Port)
	}
	slices.Sort(parts)
	key := strings.Join(parts, "\x00")
	if best.rep != nil && count == best.risk && maxRun == best.maxRun && adds == len(best.rep.AddRows) && key >= best.key {
		return
	}
	s.best = compressionCoverResult{rep: &rep, capacity: capacity, risk: count, maxRun: maxRun, key: key}
}

func compressionCoverKey(prefixes []netip.Prefix) string {
	parts := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		parts[i] = prefix.String()
	}
	return strings.Join(parts, ",")
}
