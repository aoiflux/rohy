package graphlayout

import (
	"fmt"
	"sort"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// lineageLayout arranges the graph as a forest: roots at the top, each parent centred over its
// children, depth increasing downward. It is what makes a process tree read as ancestry rather
// than as a mesh with arrows in it.
//
// It is a two-pass tidy layout — assign every leaf the next free column, then centre each parent
// over its children — rather than full Reingold–Tilford contour threading. The difference only
// shows on subtrees of very unequal width, where R-T packs them closer; the two-pass version
// never overlaps and never crosses, which are the properties that make the picture readable, and
// it is short enough to be obviously correct.
//
// A node with several parents is attached to exactly ONE of them (the lowest id), because a tree
// is what is being drawn. That is a rendering decision and the other edges are still drawn by
// the canvas — they simply do not shape the tree.
func lineageLayout(nodes []NodeInfo, rels []*graphene.Relation, opts Options) Result {
	out := emptyResult(consts.LayoutLineage)
	if len(nodes) == 0 {
		return out
	}
	es := buildEdges(nodes, rels)
	meta := indexByID(nodes)

	// Roots first, in chronological order so the leftmost tree is the earliest one. A component
	// that is entirely a cycle has no root at all; those are picked up by the forced pass below,
	// so every node is placed exactly once whatever the edge set looks like.
	roots := make([]NodeInfo, 0, 8)
	for _, n := range nodes {
		if len(es.in[n.ID]) == 0 {
			roots = append(roots, n)
		}
	}
	sort.SliceStable(roots, byTimeThenID(roots))

	var (
		depth   = make(map[uint64]int, len(nodes))
		visited = make(map[uint64]bool, len(nodes))
		kids    = make(map[uint64][]uint64, len(nodes))
		cursor  float64
		forced  int
	)

	// place walks one tree, assigning depth on the way down and x on the way back up. Recursion
	// depth is bounded by the tree's height, which for process ancestry is small; the visited set
	// makes a cycle terminate rather than recurse forever.
	var place func(id uint64, d int)
	place = func(id uint64, d int) {
		visited[id] = true
		depth[id] = d

		children := make([]NodeInfo, 0, len(es.out[id]))
		for _, c := range es.out[id] {
			if !visited[c] {
				visited[c] = true // claim it here, so a node with two parents joins the first only
				children = append(children, meta[c])
			}
		}
		sort.SliceStable(children, byTimeThenID(children))

		if len(children) == 0 {
			out.Positions[id] = Point{X: cursor, Y: float64(d) * opts.gapY()}
			cursor += opts.gapX()
			kids[id] = nil
			return
		}
		first, last := 0.0, 0.0
		for i, c := range children {
			place(c.ID, d+1)
			kids[id] = append(kids[id], c.ID)
			if i == 0 {
				first = out.Positions[c.ID].X
			}
			last = out.Positions[c.ID].X
		}
		out.Positions[id] = Point{X: (first + last) / 2, Y: float64(d) * opts.gapY()}
	}

	rootIDs := make([]uint64, 0, len(roots))
	for _, r := range roots {
		if visited[r.ID] {
			continue
		}
		place(r.ID, 0)
		rootIDs = append(rootIDs, r.ID)
	}
	// Anything still unvisited belongs to a component with no source — a pure cycle. Rooting it
	// at its lowest id is arbitrary but deterministic, and it is reported rather than presented
	// as if the evidence named that node the ancestor.
	for _, n := range nodes {
		if !visited[n.ID] {
			forced++
			place(n.ID, 0)
			rootIDs = append(rootIDs, n.ID)
		}
	}

	for i, root := range rootIDs {
		members := subtree(root, kids)
		label := meta[root].Label
		if label == "" {
			label = fmt.Sprintf("Tree %d", i+1)
		}
		out.Groups = append(out.Groups, Group{Label: label, NodeIDs: members})
	}

	if forced > 0 {
		out.Note = fmt.Sprintf(
			"%d group(s) here have no starting node — every event in them has a parent inside the "+
				"group — so the lowest event id was used as the root", forced)
	}
	return out
}

// subtree lists a tree's members in a stable pre-order, so a group's node list is the same on
// every run and reads top-down when displayed.
func subtree(root uint64, kids map[uint64][]uint64) []uint64 {
	out := []uint64{root}
	for _, c := range kids[root] {
		out = append(out, subtree(c, kids)...)
	}
	return out
}
