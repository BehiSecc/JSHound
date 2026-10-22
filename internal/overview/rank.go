package overview

import (
	"container/heap"
	"math"
	"sort"
	"strings"
)

const (
	rootScore          = 1 << 20
	maxDiagramEdges    = 14
	maxDiagramChildren = 4
)

// unitScores estimates how central each declaration is to the file's flow.
// Keys use link labels such as "start()". Roots outrank every declaration.
func unitScores(units []unitRange, links []Link, dispatches []Dispatch) map[string]int {
	scores := map[string]int{"module load": rootScore}
	out := make(map[string]map[string]struct{})
	in := make(map[string]int)
	for _, link := range links {
		if out[link.From] == nil {
			out[link.From] = make(map[string]struct{})
		}
		if _, seen := out[link.From][link.To]; !seen {
			out[link.From][link.To] = struct{}{}
			in[link.To]++
		}
	}
	// Events registered at module level are entry points; events registered
	// inside a function are ordinary flow nodes.
	for _, link := range links {
		if strings.HasSuffix(link.From, " event") {
			if in[link.From] == 0 {
				scores[link.From] = rootScore - 1
			} else {
				scores[link.From] = 15
			}
		}
	}
	reachable := make(map[string]bool)
	queue := []string{"module load"}
	for label, score := range scores {
		if score >= rootScore-1 {
			queue = append(queue, label)
		}
	}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if reachable[node] {
			continue
		}
		reachable[node] = true
		for target := range out[node] {
			queue = append(queue, target)
		}
	}
	dispatchCases := make(map[string]int)
	for _, dispatch := range dispatches {
		if dispatch.In != "" {
			dispatchCases[dispatch.In+"()"] += dispatch.Count
		}
	}
	for _, unit := range units {
		label := unit.Name + "()"
		score := 3*min(len(out[label]), 10) + 2*min(in[label], 10) + 2*len(unit.Interactions)
		if reachable[label] {
			score += 25
		}
		if cases := dispatchCases[label]; cases > 0 {
			score += 20 + min(cases, 40)/2
		}
		if size := unit.end - unit.start; size > 1 {
			score += int(math.Log2(float64(size)))
		}
		if strings.Contains(unit.Origin, "node_modules/") {
			score -= 40
		}
		scores[label] = score
	}
	return scores
}

// orderLinks expands the flow best-first from its roots: each step follows
// the most central target reachable so far, so a large dispatcher's branches
// come before deep chains of small helpers. Unreached links follow by score.
func orderLinks(links []Link, scores map[string]int) []Link {
	bySource := make(map[string][]int)
	for i, link := range links {
		bySource[link.From] = append(bySource[link.From], i)
	}
	ordered := make([]Link, 0, len(links))
	used := make([]bool, len(links))
	expanded := make(map[string]bool)
	frontier := &linkQueue{}
	expand := func(node string, depth int) {
		if expanded[node] {
			return
		}
		expanded[node] = true
		best := -1
		for _, i := range bySource[node] {
			if best < 0 || scores[links[i].To] > scores[links[best].To] {
				best = i
			}
		}
		for _, i := range bySource[node] {
			priority := scores[links[i].To] - 8*depth
			if depth == 0 && i == best {
				priority = rootScore // every entry point shows its main target
			}
			heap.Push(frontier, queuedLink{index: i, depth: depth, priority: priority, order: i})
		}
	}
	drain := func() {
		for frontier.Len() > 0 {
			next := heap.Pop(frontier).(queuedLink)
			ordered = append(ordered, links[next.index])
			used[next.index] = true
			expand(links[next.index].To, next.depth+1)
		}
	}
	var roots []string
	for label, score := range scores {
		if score >= rootScore-1 && len(bySource[label]) > 0 {
			roots = append(roots, label)
		}
	}
	sort.Slice(roots, func(a, b int) bool {
		if scores[roots[a]] != scores[roots[b]] {
			return scores[roots[a]] > scores[roots[b]]
		}
		return bySource[roots[a]][0] < bySource[roots[b]][0]
	})
	for _, root := range roots {
		expand(root, 0)
	}
	drain()
	var rest []string
	for from := range bySource {
		if !expanded[from] {
			rest = append(rest, from)
		}
	}
	sort.Slice(rest, func(a, b int) bool {
		if scores[rest[a]] != scores[rest[b]] {
			return scores[rest[a]] > scores[rest[b]]
		}
		return bySource[rest[a]][0] < bySource[rest[b]][0]
	})
	for _, from := range rest {
		expand(from, 0)
		drain()
	}
	for i, link := range links {
		if !used[i] {
			ordered = append(ordered, link)
		}
	}
	return ordered
}

type queuedLink struct {
	index, depth, priority, order int
}

type linkQueue []queuedLink

func (q linkQueue) Len() int { return len(q) }
func (q linkQueue) Less(i, j int) bool {
	if q[i].priority != q[j].priority {
		return q[i].priority > q[j].priority
	}
	return q[i].order < q[j].order
}
func (q linkQueue) Swap(i, j int)   { q[i], q[j] = q[j], q[i] }
func (q *linkQueue) Push(value any) { *q = append(*q, value.(queuedLink)) }
func (q *linkQueue) Pop() any {
	old := *q
	item := old[len(old)-1]
	*q = old[:len(old)-1]
	return item
}

// selectUnits keeps the most central declarations. A selected method brings
// its class along so the method has context.
func selectUnits(units []unitRange, scores map[string]int) ([]Unit, int) {
	order := make([]int, len(units))
	byName := make(map[string]int, len(units))
	for i := range units {
		order[i] = i
		byName[units[i].Name] = i
	}
	// A class ranks by its most central method.
	classScores := make(map[string]int)
	for _, unit := range units {
		if dot := strings.LastIndexByte(unit.Name, '.'); dot >= 0 && unit.Kind == "method" {
			owner := unit.Name[:dot]
			classScores[owner] = max(classScores[owner], scores[unit.Name+"()"])
		}
	}
	unitScore := make([]int, len(units))
	for i, unit := range units {
		if unit.Kind == "class" {
			unitScore[i] = classScores[unit.Name]
		} else {
			unitScore[i] = scores[unit.Name+"()"]
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return unitScore[order[a]] > unitScore[order[b]] })
	var selected []Unit
	chosen := make(map[int]bool)
	add := func(i int) {
		if !chosen[i] && len(selected) < maxUnits {
			chosen[i] = true
			selected = append(selected, units[i].Unit)
		}
	}
	for _, i := range order {
		if dot := strings.LastIndexByte(units[i].Name, '.'); dot >= 0 && units[i].Kind == "method" {
			if owner, ok := byName[units[i].Name[:dot]]; ok {
				add(owner)
			}
		}
		add(i)
	}
	return selected, max(0, len(units)-len(selected))
}
