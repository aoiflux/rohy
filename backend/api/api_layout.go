package api

import (
	"sort"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphlayout"
)

// LayoutRequest asks for an auto-layout of one graph. Slot names a correlation field for the
// resource profile and is ignored by the others; empty means the profile's own default.
type LayoutRequest struct {
	GraphID uint64 `json:"graph_id"`
	Profile string `json:"profile"`
	Slot    string `json:"slot,omitempty"`
}

// LayoutProfileInfo describes one profile for the UI's picker, so the menu is generated from the
// same list the backend dispatches on and the two can never drift.
type LayoutProfileInfo struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Summary string `json:"summary"`
	// NeedsSlot marks the profile that groups by a correlation field, so the picker knows to
	// offer the field list alongside it rather than showing a dead control for the others.
	NeedsSlot bool `json:"needs_slot"`
}

// LayoutProfiles returns the pickable profiles, in the order they should be offered.
func (a *GraphAPI) LayoutProfiles() []LayoutProfileInfo {
	return []LayoutProfileInfo{
		{
			Name:  consts.LayoutSequence,
			Label: "Sequence",
			Summary: "Columns by position in the chain, chronological within each. " +
				"Read this one for what led to what.",
		},
		{
			Name:  consts.LayoutLineage,
			Label: "Lineage",
			Summary: "A process tree: parents above, each centred over its children. " +
				"Read this one for ancestry.",
		},
		{
			Name:  consts.LayoutResource,
			Label: "Resource",
			Summary: "One column per logon session, account or process — whichever field you pick. " +
				"Read this one for what a single entity did.",
			NeedsSlot: true,
		},
		{
			Name:  consts.LayoutTemporal,
			Label: "Time",
			Summary: "Scattered along real time, packed into as few rows as fit. " +
				"Read this one for pace and gaps.",
		},
	}
}

// LayoutFields returns the correlation fields the resource profile can group by.
func (a *GraphAPI) LayoutFields() []string {
	return append([]string(nil), consts.CorrelationSlots...)
}

// ComputeLayout arranges a graph's canvas nodes and returns their positions.
//
// It computes and returns; it does NOT save. Persisting is the frontend's existing SaveLayout
// call, made only once the analyst keeps the arrangement — an auto-layout that overwrote hand
// placement the moment it was previewed would destroy work that cannot be recovered, and the
// preview is the whole reason to offer several profiles.
//
// The node set comes from the layout sidecar rather than from the edge set, because the sidecar
// IS canvas membership: a node dragged onto the canvas and not yet connected belongs to the
// picture and must be arranged with everything else.
func (a *GraphAPI) ComputeLayout(req LayoutRequest) (graphlayout.Result, error) {
	var out graphlayout.Result
	graphID := a.activeGraphID(req.GraphID)

	slot, err := graphlayout.SlotByName(req.Slot)
	if err != nil {
		return out, AsError(consts.ErrCodeInternal, err)
	}

	rels, err := a.store.RelationsByGraph(graphID)
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}
	ids, err := a.canvasNodeIDs(graphID, rels)
	if err != nil {
		return out, err
	}
	events, err := a.store.GetEvents(ids)
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}

	nodes := make([]graphlayout.NodeInfo, 0, len(events))
	for _, e := range events {
		if e == nil {
			continue
		}
		nodes = append(nodes, graphlayout.NodeInfo{
			ID:        e.ID,
			Timestamp: e.Timestamp,
			// An event with no timestamp is undated, not an event from the zero year. Every
			// profile branches on this rather than on the timestamp value.
			Dated:    !e.Timestamp.IsZero(),
			CorrKeys: e.CorrKeys,
			Label:    nodeLabel(e),
		})
	}

	res, err := graphlayout.Compute(req.Profile, nodes, rels, graphlayout.Options{Slot: slot})
	if err != nil {
		return out, AsError(consts.ErrCodeInternal, err)
	}
	return res, nil
}

// ClusterRequest asks how a graph's nodes group. Slot names a correlation field for the slot
// mode and is ignored by the others.
type ClusterRequest struct {
	GraphID uint64 `json:"graph_id"`
	Mode    string `json:"mode"`
	Slot    string `json:"slot,omitempty"`
}

// ClusterModes returns the accepted grouping modes, so the picker is generated from the same
// list the engine dispatches on.
func (a *GraphAPI) ClusterModes() []string {
	return append([]string(nil), consts.ClusterModes...)
}

// Clusters groups a graph's canvas nodes so the canvas can outline and collapse them.
//
// Like ComputeLayout it reads and returns; grouping is a view of the graph, not a change to it,
// and nothing about which clusters are collapsed belongs in the case data.
func (a *GraphAPI) Clusters(req ClusterRequest) ([]graphlayout.Cluster, error) {
	graphID := a.activeGraphID(req.GraphID)

	slot, err := graphlayout.SlotByName(req.Slot)
	if err != nil {
		return nil, AsError(consts.ErrCodeInternal, err)
	}
	rels, err := a.store.RelationsByGraph(graphID)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	ids, err := a.canvasNodeIDs(graphID, rels)
	if err != nil {
		return nil, err
	}
	events, err := a.store.GetEvents(ids)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}

	nodes := make([]graphlayout.NodeInfo, 0, len(events))
	for _, e := range events {
		if e == nil {
			continue
		}
		nodes = append(nodes, graphlayout.NodeInfo{ID: e.ID, CorrKeys: e.CorrKeys})
	}

	out, err := graphlayout.Clusters(req.Mode, nodes, rels, graphlayout.Options{Slot: slot})
	if err != nil {
		return nil, AsError(consts.ErrCodeInternal, err)
	}
	return out, nil
}

// HeatmapRequest asks for a graph's relations bucketed over time. GraphID 0 covers the active
// graph; pass a negative Buckets or zero to take the default resolution.
type HeatmapRequest struct {
	GraphID uint64 `json:"graph_id"`
	Buckets int    `json:"buckets"`
	GroupBy string `json:"group_by"`
	// AllGraphs widens the answer to every relation in the case. It is an explicit flag rather
	// than GraphID 0, because 0 already means "the active graph" everywhere else in this binding
	// and one value meaning two things is how a whole-case answer gets shown as a per-graph one.
	AllGraphs bool `json:"all_graphs,omitempty"`
	// From/To pin the window. The timeline passes its own view bounds here so the strip drawn
	// over the histogram shares its axis exactly; leave them unset for the relations' own extent.
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`
}

// HeatmapGroups returns the accepted groupings, generated from the same list the store
// dispatches on.
func (a *GraphAPI) HeatmapGroups() []string {
	return append([]string(nil), consts.HeatmapGroups...)
}

// RelationHeatmap buckets relations over time so the timeline can show WHEN the things rohy
// inferred actually happened, rather than only when the events did.
func (a *GraphAPI) RelationHeatmap(req HeatmapRequest) (graphene.HeatmapSummary, error) {
	graphID := uint64(0)
	if !req.AllGraphs {
		graphID = a.activeGraphID(req.GraphID)
	}
	out, err := a.store.RelationHeatmap(graphene.HeatmapQuery{
		GraphID: graphID,
		Buckets: req.Buckets,
		GroupBy: req.GroupBy,
		From:    req.From,
		To:      req.To,
	})
	if err != nil {
		return graphene.HeatmapSummary{}, AsError(consts.ErrCodeInternal, err)
	}
	return out, nil
}

// canvasNodeIDs reads a graph's canvas membership from the layout sidecar, falling back to the
// endpoints of its relations when there is no sidecar yet — which is exactly the state a graph
// is in immediately after a rule build, and the state in which an auto-layout is most useful.
func (a *GraphAPI) canvasNodeIDs(graphID uint64, rels []*graphene.Relation) ([]uint64, error) {
	seen := map[uint64]bool{}
	ids := make([]uint64, 0, 64)

	if a.layout != nil {
		l, err := a.layout.Load(graphID)
		if err != nil {
			return nil, AsError(consts.ErrCodePersistence, err)
		}
		for id := range l.Nodes {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		for _, r := range rels {
			for _, id := range []uint64{r.From, r.To} {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	// GetEvents returns a compacted found-set, so the order it is asked in does not survive;
	// sorting here is for the layout's determinism, not for the query's. The ids come out of a
	// map, so without this the request order would differ on every call.
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

// nodeLabel is the short name a lineage tree is titled with. Process name first, because a
// process tree titled "svchost.exe" says more than one titled "4688"; the event id is the
// fallback, and an empty label means the tree is numbered instead.
func nodeLabel(e *graphene.Event) string {
	if i, ok := consts.CorrelationSlotIndex("process_name"); ok && i < len(e.CorrKeys) {
		if v := strings.TrimSpace(e.CorrKeys[i]); v != "" {
			return v
		}
	}
	return strings.TrimSpace(e.EventID)
}
