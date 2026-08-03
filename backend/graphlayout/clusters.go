package graphlayout

import (
	"fmt"
	"sort"
	"strings"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// Cluster is a named set of nodes that belong together under some grouping. The canvas draws a
// hull around one and can collapse it to a single card; Size is carried separately from NodeIDs
// so a collapsed cluster can always say how much it is hiding.
type Cluster struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	NodeIDs []uint64 `json:"node_ids"`
	Size    int      `json:"size"`
	// Overlapping reports that a node in this cluster may also be in another one. It is true for
	// rule grouping, where an event genuinely participates in two rules, and false for the modes
	// that partition. The canvas needs to know: a hull drawn as if membership were exclusive
	// would claim something the grouping does not.
	Overlapping bool `json:"overlapping,omitempty"`
}

// Clusters groups a graph's nodes.
//
//	component — connected components, ignoring edge direction. "What is joined to what."
//	rule      — the nodes each rule's edges touch. A node may appear in more than one.
//	slot      — the distinct values of one correlation field.
//
// Every mode returns a total, deterministic ordering: largest first, ties broken by label, then
// by lowest member id. An analyst scanning a hull list reads the biggest first, and it must be
// the same list every time.
func Clusters(mode string, nodes []NodeInfo, rels []*graphene.Relation, opts Options) ([]Cluster, error) {
	switch mode {
	case "", consts.ClusterComponent:
		return componentClusters(nodes, rels), nil
	case consts.ClusterRule:
		return ruleClusters(nodes, rels), nil
	case consts.ClusterSlot:
		return slotClusters(nodes, opts), nil
	default:
		return nil, fmt.Errorf(consts.MsgClusterUnknownMode, mode, strings.Join(consts.ClusterModes, ", "))
	}
}

// componentClusters finds connected components with union-find — O(E α(N)), which is effectively
// linear. Direction is ignored deliberately: "reachable from" is a different question, and a
// component is being drawn as a region on a canvas, where an arrow's direction does not change
// whether two cards belong inside the same outline.
func componentClusters(nodes []NodeInfo, rels []*graphene.Relation) []Cluster {
	parent := make(map[uint64]uint64, len(nodes))
	size := make(map[uint64]int, len(nodes))
	for _, n := range nodes {
		parent[n.ID] = n.ID
		size[n.ID] = 1
	}

	var find func(uint64) uint64
	find = func(x uint64) uint64 {
		if parent[x] != x {
			parent[x] = find(parent[x]) // path compression
		}
		return parent[x]
	}
	union := func(a, b uint64) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if size[ra] < size[rb] { // union by size, so find stays shallow
			ra, rb = rb, ra
		}
		parent[rb] = ra
		size[ra] += size[rb]
	}

	present := make(map[uint64]bool, len(nodes))
	for _, n := range nodes {
		present[n.ID] = true
	}
	for _, r := range rels {
		if r == nil || !present[r.From] || !present[r.To] {
			continue
		}
		union(r.From, r.To)
	}

	members := map[uint64][]uint64{}
	for _, n := range nodes {
		root := find(n.ID)
		members[root] = append(members[root], n.ID)
	}

	out := make([]Cluster, 0, len(members))
	for _, ids := range members {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		// Named after its LOWEST MEMBER, not after the union-find root. The root depends on the
		// order the unions happened in, so a root-derived id would rename a component whose
		// membership had not changed at all — and the canvas remembers which clusters are
		// collapsed by id.
		out = append(out, Cluster{
			ID:      fmt.Sprintf("component-%d", ids[0]),
			Label:   fmt.Sprintf("Group of %d", len(ids)),
			NodeIDs: ids,
			Size:    len(ids),
		})
	}
	return ordered(out)
}

// ruleClusters groups by the rule whose edges touch a node.
//
// Membership OVERLAPS here, and that is the honest answer rather than a limitation: an event
// really can be the 4624 in two different chains. Forcing an exclusive assignment would have to
// pick a winner, and there is no non-arbitrary way to pick one.
//
// Nodes touched by no rule edge — hand-drawn links, or nodes placed and never connected — are
// collected under one named cluster rather than dropped, because a clustering that silently
// omitted a third of the canvas would read as "these nodes are not here".
func ruleClusters(nodes []NodeInfo, rels []*graphene.Relation) []Cluster {
	present := make(map[uint64]bool, len(nodes))
	for _, n := range nodes {
		present[n.ID] = true
	}

	members := map[string]map[uint64]bool{}
	claimed := map[uint64]bool{}
	for _, r := range rels {
		if r == nil || r.RuleID == "" {
			continue
		}
		for _, id := range [2]uint64{r.From, r.To} {
			if !present[id] {
				continue
			}
			if members[r.RuleID] == nil {
				members[r.RuleID] = map[uint64]bool{}
			}
			members[r.RuleID][id] = true
			claimed[id] = true
		}
	}

	out := make([]Cluster, 0, len(members)+1)
	for rule, set := range members {
		out = append(out, Cluster{
			ID:          "rule-" + rule,
			Label:       rule,
			NodeIDs:     sortedKeys(set),
			Size:        len(set),
			Overlapping: true,
		})
	}
	var loose []uint64
	for _, n := range nodes {
		if !claimed[n.ID] {
			loose = append(loose, n.ID)
		}
	}
	if len(loose) > 0 {
		sort.Slice(loose, func(i, j int) bool { return loose[i] < loose[j] })
		out = append(out, Cluster{
			ID:          "rule-none",
			Label:       consts.ClusterNoRuleLabel,
			NodeIDs:     loose,
			Size:        len(loose),
			Overlapping: true,
		})
	}
	return ordered(out)
}

// slotClusters groups by the distinct values of one correlation field. Absent is collected under
// its own named cluster and never treated as a shared value — the same rule the resource layout
// and the correlation vocabulary both follow.
func slotClusters(nodes []NodeInfo, opts Options) []Cluster {
	slot := opts.Slot
	if slot < 0 || slot >= len(consts.CorrelationSlots) {
		slot = 0
	}
	members := map[string]map[uint64]bool{}
	var absent []uint64
	for _, n := range nodes {
		v := ""
		if slot < len(n.CorrKeys) {
			v = n.CorrKeys[slot]
		}
		if v == "" {
			absent = append(absent, n.ID)
			continue
		}
		if members[v] == nil {
			members[v] = map[uint64]bool{}
		}
		members[v][n.ID] = true
	}

	out := make([]Cluster, 0, len(members)+1)
	for v, set := range members {
		out = append(out, Cluster{
			ID:      "slot-" + v,
			Label:   v,
			NodeIDs: sortedKeys(set),
			Size:    len(set),
		})
	}
	if len(absent) > 0 {
		sort.Slice(absent, func(i, j int) bool { return absent[i] < absent[j] })
		out = append(out, Cluster{
			ID:      "slot-absent",
			Label:   consts.LayoutAbsentLabel,
			NodeIDs: absent,
			Size:    len(absent),
		})
	}
	return ordered(out)
}

// ordered imposes the one ordering every mode shares: largest first, then label, then lowest
// member id. Nothing here may depend on map iteration order.
func ordered(cs []Cluster) []Cluster {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Size != cs[j].Size {
			return cs[i].Size > cs[j].Size
		}
		if cs[i].Label != cs[j].Label {
			return cs[i].Label < cs[j].Label
		}
		return cs[i].ID < cs[j].ID
	})
	return cs
}

func sortedKeys(set map[uint64]bool) []uint64 {
	out := make([]uint64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
