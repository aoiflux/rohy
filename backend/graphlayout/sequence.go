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
	// ready holds ids whose predecessors are all placed, or that are being placed by force, kept
	// sorted so the traversal order — and therefore the ranks assigned when a placement has to be
	// forced — is identical every run.
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
	forcedPlacements := 0

	for len(placed) < len(nodes) {
		if len(ready) == 0 {
			// No remaining node has every predecessor placed, so the remaining nodes contain a
			// cycle. The node forced here is the LOWEST REMAINING ID, which need not be on that
			// cycle: it may only sit downstream of one, so one cycle can cost more than one
			// forced placement. The note therefore counts forced placements, not cycles. A cycle
			// in an event graph is real — two rules can legitimately relate a pair in both
			// directions — so this is a rendering decision, not an error, but it is one the
			// analyst is told about rather than one that silently picks a direction and presents
			// it as the evidence's.
			var forced uint64
			for _, id := range remaining {
				if !placed[id] {
					forced = id
					break
				}
			}
			forcedPlacements++
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
				// An edge into a node that was placed by force; it cannot re-rank a placed node.
				continue
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

	if forcedPlacements > 0 {
		out.Note = fmt.Sprintf(
			"%d forced placement(s): a cycle left no event free to go next, so the remaining "+
				"event with the lowest internal id was placed anyway — some edges point leftward "+
				"or stay within one column as a result", forcedPlacements)
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
