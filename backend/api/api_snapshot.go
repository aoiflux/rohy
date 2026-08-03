package api

import (
	"errors"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/layout"
	"rohy/backend/snapshot"
	"rohy/backend/version"
)

// SnapshotRequest takes a snapshot of one graph. GraphID 0 means the active graph.
type SnapshotRequest struct {
	GraphID uint64 `json:"graph_id"`
	Label   string `json:"label,omitempty"`
}

// RestoreRequest previews or applies a restore.
type RestoreRequest struct {
	GraphID uint64 `json:"graph_id"`
	ID      string `json:"id"`
	// Recreate lists the snapshot relation ids the analyst chose to re-create. Anything not named
	// here is not written — an empty list restores layout and viewport only, which is the safe
	// default and the one the preview defaults to.
	Recreate []uint64 `json:"recreate,omitempty"`
}

// RestoreResult reports what was actually written.
type RestoreResult struct {
	Plan snapshot.Plan `json:"plan"`
	// NodesMoved is how many canvas positions were re-applied; RelationsCreated is how many edges
	// the analyst chose to re-assert. They are separate because they are different kinds of act:
	// one rearranges a view, the other adds a claim to the case.
	NodesMoved       int `json:"nodes_moved"`
	RelationsCreated int `json:"relations_created"`
}

// SnapshotAPI is the Wails binding for graph snapshots.
//
// It is its own bound struct rather than more methods on GraphAPI because it owns a different
// store and a different lifecycle — the same reason MaintenanceAPI is separate. GraphAPI is
// already the largest binding in the app.
type SnapshotAPI struct {
	store    *graphene.Store
	layout   *layout.Store
	snaps    *snapshot.Store
	registry graphRegistry
}

// graphRegistry is the slice of the graph registry this binding needs, so the dependency is
// stated rather than inherited wholesale.
type graphRegistry interface {
	Active() uint64
}

// NewSnapshotAPI constructs the binding.
func NewSnapshotAPI(store *graphene.Store, layoutStore *layout.Store, snaps *snapshot.Store, registry graphRegistry) *SnapshotAPI {
	return &SnapshotAPI{store: store, layout: layoutStore, snaps: snaps, registry: registry}
}

func (a *SnapshotAPI) graphID(requested uint64) uint64 {
	if requested != 0 {
		return requested
	}
	if a.registry != nil {
		if id := a.registry.Active(); id != 0 {
			return id
		}
	}
	return consts.DefaultGraphID
}

// TakeSnapshot records the graph's canvas as it is now.
//
// The node set comes from the layout sidecar — canvas membership — so a node placed but not yet
// connected is part of what is recorded. Every node and endpoint is stamped with its
// hash_normalized, which is what a restore matches on.
func (a *SnapshotAPI) TakeSnapshot(req SnapshotRequest) (snapshot.Summary, error) {
	var out snapshot.Summary
	if a.snaps == nil {
		return out, AsError(consts.ErrCodeInternal, errors.New("snapshots unavailable"))
	}
	graphID := a.graphID(req.GraphID)

	saved, err := a.loadLayout(graphID)
	if err != nil {
		return out, err
	}
	rels, err := a.store.RelationsByGraph(graphID)
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}

	ids := make([]uint64, 0, len(saved.Nodes))
	for id := range saved.Nodes {
		ids = append(ids, id)
	}
	events, err := a.store.GetEvents(ids)
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}
	hashOf := make(map[uint64]string, len(events))
	nodes := make([]snapshot.Node, 0, len(events))
	for _, e := range events {
		if e == nil {
			continue
		}
		hashOf[e.ID] = e.HashNormalized
		pos := saved.Nodes[e.ID]
		nodes = append(nodes, snapshot.Node{
			ID: e.ID, Hash: e.HashNormalized, X: pos.X, Y: pos.Y, Descriptor: describeEvent(e),
		})
	}

	out2 := make([]snapshot.Relation, 0, len(rels))
	for _, r := range rels {
		if r == nil {
			continue
		}
		from, okFrom := hashOf[r.From]
		to, okTo := hashOf[r.To]
		if !okFrom || !okTo {
			// An edge to an event that is not on this canvas. It belongs to the graph but not to
			// the picture being recorded, and a snapshot that included it would restore a link to
			// something it never showed.
			continue
		}
		out2 = append(out2, snapshot.Relation{
			ID: r.ID, FromHash: from, ToHash: to,
			RelationType: r.RelationType, Label: r.Label, CreatedBy: r.CreatedBy,
			RuleID: r.RuleID, Algorithm: r.Algorithm, MatchID: r.MatchID,
			StepIndex: r.StepIndex, Basis: append([]string(nil), r.Basis...),
		})
	}

	now := time.Now().UTC()
	doc := &snapshot.Document{
		ID:           snapshot.NewID(now),
		Label:        strings.TrimSpace(req.Label),
		GraphID:      graphID,
		TakenAt:      now,
		StoreVersion: a.store.Version(),
		AppVersion:   version.Current("").Version,
		Viewport:     snapshot.Viewport{X: saved.Viewport.X, Y: saved.Viewport.Y, Zoom: saved.Viewport.Zoom},
		Nodes:        nodes,
		Relations:    out2,
	}
	if err := a.snaps.Save(doc); err != nil {
		return out, AsError(consts.ErrCodeInternal, err)
	}
	return snapshot.Summary{
		ID: doc.ID, Label: doc.Label, GraphID: graphID, TakenAt: doc.TakenAt,
		Nodes: len(doc.Nodes), Relations: len(doc.Relations), AppVersion: doc.AppVersion,
	}, nil
}

// ListSnapshots returns a graph's snapshots, newest first.
func (a *SnapshotAPI) ListSnapshots(graphID uint64) ([]snapshot.Summary, error) {
	if a.snaps == nil {
		return nil, nil
	}
	out, err := a.snaps.List(a.graphID(graphID))
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return out, nil
}

// DeleteSnapshot removes one. Deleting an already-gone snapshot is success.
func (a *SnapshotAPI) DeleteSnapshot(graphID uint64, id string) error {
	if a.snaps == nil {
		return nil
	}
	if err := a.snaps.Delete(a.graphID(graphID), id); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// PreviewRestore works out what a snapshot could put back, and writes nothing.
//
// 🔒 This is the whole point of the feature's shape: the analyst sees exactly what will be
// re-applied, what is offered for re-creation, and what cannot be resolved, before anything
// happens. Nothing restores silently.
func (a *SnapshotAPI) PreviewRestore(graphID uint64, id string) (snapshot.Plan, error) {
	var out snapshot.Plan
	doc, live, rels, err := a.restoreInputs(graphID, id)
	if err != nil {
		return out, err
	}
	return snapshot.BuildPlan(doc, live, rels), nil
}

// ApplyRestore re-applies a snapshot's layout and, only for the relations the analyst explicitly
// named, re-creates the edges.
//
// 🔒 A re-created edge is written as USER-ASSERTED with a "restored from snapshot" basis, never
// with the rule provenance the snapshot recorded. rohy asserting a link today is a different
// claim from a rule having inferred it then, and an edge that wore the old provenance would be
// indistinguishable from one the current rules produced.
func (a *SnapshotAPI) ApplyRestore(req RestoreRequest) (RestoreResult, error) {
	var out RestoreResult
	doc, live, rels, err := a.restoreInputs(req.GraphID, req.ID)
	if err != nil {
		return out, err
	}
	plan := snapshot.BuildPlan(doc, live, rels)
	out.Plan = plan
	graphID := a.graphID(req.GraphID)

	// Layout first: it is the part that is always safe, so a failure re-creating an edge does not
	// also cost the arrangement.
	if a.layout != nil {
		saved, err := a.loadLayout(graphID)
		if err != nil {
			return out, err
		}
		if saved.Nodes == nil {
			saved.Nodes = map[uint64]layout.Position{}
		}
		for id, p := range plan.Positions() {
			saved.Nodes[id] = layout.Position{X: p.X, Y: p.Y}
			out.NodesMoved++
		}
		saved.Viewport = layout.Viewport{X: plan.Viewport.X, Y: plan.Viewport.Y, Zoom: plan.Viewport.Zoom}
		if err := a.layout.Save(graphID, saved); err != nil {
			return out, AsError(consts.ErrCodePersistence, err)
		}
	}

	if len(req.Recreate) == 0 {
		return out, nil
	}
	wanted := make(map[uint64]bool, len(req.Recreate))
	for _, id := range req.Recreate {
		wanted[id] = true
	}
	for _, r := range plan.Recreatable() {
		if !wanted[r.SnapshotID] {
			continue
		}
		rel := &graphene.Relation{
			From:      r.FromID,
			To:        r.ToID,
			GraphID:   graphID,
			Label:     r.Label,
			CreatedBy: consts.CreatedByUser,
			CreatedAt: time.Now().UTC(),
		}
		rel.RelationType = relationTypeOrDefault(r.RelationType)
		// The basis says where this edge came from, in the same field a rule would have used to
		// say why it matched. An analyst reading the inspector sees "restored from snapshot …"
		// rather than a blank, and never a rule's reasoning.
		rel.Basis = []string{formatRestoredBasis(plan.SnapshotID)}
		if _, err := a.store.InsertRelation(rel); err != nil {
			return out, AsError(consts.ErrCodePersistence, err)
		}
		out.RelationsCreated++
	}
	return out, nil
}

// restoreInputs gathers everything the planner needs: the document, every event in the case, and
// the target graph's current relations.
func (a *SnapshotAPI) restoreInputs(graphID uint64, id string) (*snapshot.Document, []snapshot.LiveEvent, []snapshot.LiveRelation, error) {
	if a.snaps == nil {
		return nil, nil, nil, AsError(consts.ErrCodeInternal, errors.New("snapshots unavailable"))
	}
	gid := a.graphID(graphID)
	doc, err := a.snaps.Get(gid, id)
	if err != nil {
		return nil, nil, nil, AsError(consts.ErrCodeInternal, err)
	}

	// Every event in the case, not only those on the canvas: a snapshot's node may have been
	// removed from the canvas while remaining in the case, and that is a restorable node.
	hashes, err := a.store.EventHashes()
	if err != nil {
		return nil, nil, nil, AsError(consts.ErrCodePersistence, err)
	}
	live := make([]snapshot.LiveEvent, 0, len(hashes))
	for id, h := range hashes {
		live = append(live, snapshot.LiveEvent{ID: id, Hash: h})
	}

	rels, err := a.store.RelationsByGraph(gid)
	if err != nil {
		return nil, nil, nil, AsError(consts.ErrCodePersistence, err)
	}
	out := make([]snapshot.LiveRelation, 0, len(rels))
	for _, r := range rels {
		if r != nil {
			out = append(out, snapshot.LiveRelation{ID: r.ID, From: r.From, To: r.To})
		}
	}
	return doc, live, out, nil
}

func (a *SnapshotAPI) loadLayout(graphID uint64) (*layout.Layout, error) {
	if a.layout == nil {
		return &layout.Layout{Nodes: map[uint64]layout.Position{}}, nil
	}
	l, err := a.layout.Load(graphID)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	if l.Nodes == nil {
		l.Nodes = map[uint64]layout.Position{}
	}
	return l, nil
}

// describeEvent is the human-readable summary stored alongside a node's hash, so a snapshot whose
// events have left the case still describes what it held. Same reasoning as findings.Descriptor.
func describeEvent(e *graphene.Event) string {
	parts := make([]string, 0, 3)
	if e.EventID != "" {
		parts = append(parts, e.EventID)
	}
	if e.Computer != "" {
		parts = append(parts, e.Computer)
	}
	if !e.Timestamp.IsZero() {
		parts = append(parts, e.Timestamp.UTC().Format("2006-01-02 15:04:05"))
	}
	return strings.Join(parts, " ")
}

func formatRestoredBasis(snapshotID string) string {
	return strings.Replace(consts.MsgSnapshotRestoredBasis, "%s", snapshotID, 1)
}
