package graphlayout

import (
	"fmt"
	"sort"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// sequenceLayout ranks nodes by their depth in the edge DAG and lays each rank out as a column.
// It is the profile for reading a chain: x is "how far along", y is "when, among the things
// equally far along".
//
// Rank is the LONGEST path from a source, not the shortest. Shortest-path ranking would let a
// node sit in the same column as an event it demonstrably follows whenever a second, shorter
// route to it exists — which reads, wrongly, as "these two are contemporaneous". The longest
// path is the one that keeps every edge pointing strictly rightward.
func sequenceLayout(nodes []NodeInfo, rels []*graphene.Relation, opts Options) Result {
	out := emptyResult(consts.LayoutSequence)
	if len(nodes) == 0 {
		return out
	}
	es := buildEdges(nodes, rels)

	indeg := make(map[uint64]int, len(nodes))
	for _, n := range nodes {
		indeg[n.ID] = len(es.in[n.ID])
	}

	rank := make(map[uint64]int, len(nodes))
	// ready holds ids whose predecessors are all placed, kept sorted so the traversal order —
	// and therefore the ranks assigned when a cycle has to be broken — is identical every run.
	ready := make([]uint64, 0, len(nodes))
	for _, n := range nodes {
		if indeg[n.ID] == 0 {
			ready = append(ready, n.ID)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })

	placed := make(map[uint64]bool, len(nodes))
	remaining := make([]uint64, len(nodes))
	for i, n := range nodes {
		remaining[i] = n.ID // nodes is already id-sorted by Compute
	}
	brokeCycle := 0

	for len(placed) < len(nodes) {
		if len(ready) == 0 {
			// Every remaining node is inside a cycle. Break it at the LOWEST REMAINING ID and
			// say so afterwards. A cycle in an event graph is real — two rules can legitimately
			// relate a pair in both directions — so this is a rendering decision, not an error,
			// but it is one the analyst is told about rather than one that silently picks a
			// direction and presents it as the evidence's.
			var forced uint64
			for _, id := range remaining {
				if !placed[id] {
					forced = id
					break
				}
			}
			brokeCycle++
			indeg[forced] = 0
			ready = append(ready, forced)
		}
		id := ready[0]
		ready = ready[1:]
		if placed[id] {
			continue
		}
		placed[id] = true
		for _, next := range es.out[id] {
			if placed[next] {
				continue // an edge back into the broken cycle; it cannot re-rank a placed node
			}
			if r := rank[id] + 1; r > rank[next] {
				rank[next] = r
			}
			indeg[next]--
			if indeg[next] == 0 {
				ready = insertSorted(ready, next)
			}
		}
	}

	// Collect each rank's members, then order within the rank chronologically. The two passes
	// are separate because y depends on the whole rank, not on the traversal order that
	// produced it.
	byRank := map[int][]NodeInfo{}
	maxRank := 0
	for _, n := range nodes {
		r := rank[n.ID]
		byRank[r] = append(byRank[r], n)
		if r > maxRank {
			maxRank = r
		}
	}
	for r := 0; r <= maxRank; r++ {
		members := byRank[r]
		if len(members) == 0 {
			continue
		}
		sort.SliceStable(members, byTimeThenID(members))
		ids := make([]uint64, 0, len(members))
		for i, n := range members {
			out.Positions[n.ID] = Point{X: float64(r) * opts.gapX(), Y: float64(i) * opts.gapY()}
			ids = append(ids, n.ID)
		}
		out.Groups = append(out.Groups, Group{Label: fmt.Sprintf("Step %d", r+1), NodeIDs: ids})
	}

	if brokeCycle > 0 {
		out.Note = fmt.Sprintf(
			"%d cycle(s) in this graph were broken at the lowest event id so it could be ranked — "+
				"some edges point leftward as a result", brokeCycle)
	}
	return out
}

// insertSorted keeps the ready queue ordered without pulling in a heap for a queue that is
// usually a handful of ids deep.
func insertSorted(xs []uint64, v uint64) []uint64 {
	i := sort.Search(len(xs), func(i int) bool { return xs[i] >= v })
	xs = append(xs, 0)
	copy(xs[i+1:], xs[i:])
	xs[i] = v
	return xs
}
