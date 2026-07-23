package api

import (
	"errors"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/layout"

	"github.com/aoiflux/graphene/store"
)

// GraphAPI is the Wails binding for graph reads, manual edge creation, per-graph canvas
// layout persistence, and graph management (multiple-graphs, P15). Like EventsAPI it is
// a thin delegate over the persistence layers.
type GraphAPI struct {
	store    *graphene.Store
	layout   *layout.Store
	registry *graphreg.Store
}

// NewGraphAPI constructs the binding over an open event store, layout store, and graph
// registry.
func NewGraphAPI(store *graphene.Store, layoutStore *layout.Store, registry *graphreg.Store) *GraphAPI {
	return &GraphAPI{store: store, layout: layoutStore, registry: registry}
}

// activeGraphID returns the caller-supplied graph id, or the active graph when the
// caller passes 0, or the default graph as a last resort. Every relation and layout is
// scoped through this so nothing is ever written to an ambiguous graph.
func (a *GraphAPI) activeGraphID(requested uint64) uint64 {
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

// --- Graph management (P15) ---

// ListGraphs returns all named graphs in the case.
func (a *GraphAPI) ListGraphs() ([]*graphreg.Graph, error) {
	if a.registry == nil {
		return nil, nil
	}
	return a.registry.List(), nil
}

// ActiveGraph returns the id of the active graph.
func (a *GraphAPI) ActiveGraph() uint64 {
	if a.registry == nil {
		return consts.DefaultGraphID
	}
	return a.registry.Active()
}

// SetActiveGraph marks a graph active so subsequent relation/layout ops target it.
func (a *GraphAPI) SetActiveGraph(id uint64) error {
	if a.registry == nil {
		return nil
	}
	if err := a.registry.SetActive(id); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// CreateGraph adds a new named graph and makes it active.
func (a *GraphAPI) CreateGraph(req GraphRequest) (*graphreg.Graph, error) {
	if a.registry == nil {
		return nil, AsError(consts.ErrCodeInternal, errors.New("graph registry unavailable"))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, AsError(consts.ErrCodeInternal, errors.New("graph name is required"))
	}
	g, err := a.registry.Create(name, strings.TrimSpace(req.Description), time.Now().UTC())
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return g, nil
}

// RenameGraph updates a graph's name and description.
func (a *GraphAPI) RenameGraph(req GraphRequest) (*graphreg.Graph, error) {
	if a.registry == nil {
		return nil, AsError(consts.ErrCodeInternal, errors.New("graph registry unavailable"))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, AsError(consts.ErrCodeInternal, errors.New("graph name is required"))
	}
	g, err := a.registry.Rename(req.ID, name, strings.TrimSpace(req.Description), time.Now().UTC())
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return g, nil
}

// DeleteGraph removes a graph and cascades to its relations and layout — but never to
// the underlying shared event nodes. Refuses to delete the last remaining graph.
func (a *GraphAPI) DeleteGraph(id uint64) error {
	if a.registry == nil {
		return nil
	}
	if err := a.registry.Delete(id); err != nil {
		if errors.Is(err, graphreg.ErrLastGraph) || errors.Is(err, graphreg.ErrNotFound) {
			return AsError(consts.ErrCodeInternal, err)
		}
		return AsError(consts.ErrCodePersistence, err)
	}
	// Registry entry is gone; now drop the graph's edges + layout (events are shared and
	// left intact). Best-effort: a failure here leaves orphaned edges scoped to a
	// deleted graph id, which no view queries.
	if _, err := a.store.DeleteGraphRelations(id); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	if err := a.layout.Delete(id); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// GetEvents hydrates the events backing a set of canvas nodes, in the order given.
func (a *GraphAPI) GetEvents(ids []uint64) ([]*graphene.Event, error) {
	events, err := a.store.GetEvents(ids)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return events, nil
}

// GetRelations returns every mapped relation (for loading a saved mapping).
func (a *GraphAPI) GetRelations() ([]*graphene.Relation, error) {
	rels, err := a.store.GetRelations()
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return rels, nil
}

// RelationsOf returns the relations incident to one event node.
func (a *GraphAPI) RelationsOf(eventID uint64) ([]*graphene.Relation, error) {
	rels, err := a.store.RelationsOf(eventID)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return rels, nil
}

// RelationsAdjacency returns relation summaries for a set of events — how many
// relations each has, their types, and neighbor ids — for relation-aware highlighting
// in the table view (P14). One call covers the whole loaded window.
func (a *GraphAPI) RelationsAdjacency(ids []uint64) (map[uint64]*graphene.EventAdjacency, error) {
	adj, err := a.store.RelationsAdjacency(ids)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return adj, nil
}

// GetGraphRelations returns the relations belonging to one graph (P15). Loading a
// mapping uses this so each graph restores only its own edges.
func (a *GraphAPI) GetGraphRelations(graphID uint64) ([]*graphene.Relation, error) {
	rels, err := a.store.RelationsByGraph(a.activeGraphID(graphID))
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return rels, nil
}

// CreateRelation persists a manually mapped edge scoped to a graph (req.GraphID, or the
// active graph when 0). The backend fills the relation type and provenance defaults and
// stamps CreatedAt, so the frontend cannot forge provenance timestamps.
func (a *GraphAPI) CreateRelation(req RelationRequest) (*graphene.Relation, error) {
	rel := &graphene.Relation{
		From:            req.From,
		To:              req.To,
		GraphID:         a.activeGraphID(req.GraphID),
		RelationType:    relationTypeOrDefault(req.RelationType),
		Label:           strings.TrimSpace(req.Label),
		ConfidenceScore: req.ConfidenceScore,
		CreatedBy:       createdByOrDefault(req.CreatedBy),
		CreatedAt:       time.Now().UTC(),
	}
	id, err := a.store.InsertRelation(rel)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	rel.ID = id
	return rel, nil
}

// UpdateRelation edits an existing relation's type and label (read-modify-write so
// endpoints and provenance are preserved). Returns the updated relation.
func (a *GraphAPI) UpdateRelation(req RelationUpdate) (*graphene.Relation, error) {
	rel, err := a.store.GetRelation(req.ID)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	rel.RelationType = relationTypeOrDefault(req.RelationType)
	rel.Label = strings.TrimSpace(req.Label)
	if req.ConfidenceScore != 0 {
		rel.ConfidenceScore = req.ConfidenceScore
	}
	if err := a.store.UpdateRelation(rel); err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return rel, nil
}

// DeleteRelation removes a mapped relation from the graph DB. Deleting an already-gone
// relation is treated as success (idempotent).
func (a *GraphAPI) DeleteRelation(id uint64) error {
	if err := a.store.DeleteRelation(id); err != nil {
		var nf *store.ErrNotFound
		if errors.As(err, &nf) {
			return nil
		}
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// DeleteEvent removes an event node and cascades to its relations. This is
// destructive (forensic evidence is removed from the case DB); callers must confirm
// intent in the UI. Idempotent on a missing id.
func (a *GraphAPI) DeleteEvent(id uint64) error {
	if err := a.store.DeleteEvent(id); err != nil {
		var nf *store.ErrNotFound
		if errors.As(err, &nf) {
			return nil
		}
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// SaveLayout persists a graph's canvas node positions and viewport so a mapping session
// can be restored exactly. Layout is UI state (not graph data) stored as a per-graph
// JSON sidecar; the node id set doubles as the graph's canvas membership. A nil layout
// store makes this a no-op.
func (a *GraphAPI) SaveLayout(graphID uint64, l layout.Layout) error {
	if a.layout == nil {
		return nil
	}
	if err := a.layout.Save(a.activeGraphID(graphID), &l); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// LoadLayout returns a graph's saved canvas layout, or an empty layout if none exists.
func (a *GraphAPI) LoadLayout(graphID uint64) (layout.Layout, error) {
	if a.layout == nil {
		return layout.Layout{Nodes: map[uint64]layout.Position{}}, nil
	}
	l, err := a.layout.Load(a.activeGraphID(graphID))
	if err != nil {
		return layout.Layout{}, AsError(consts.ErrCodePersistence, err)
	}
	return *l, nil
}

// relationTypeOrDefault falls back to the default relation type for an empty or
// unrecognized value, so no edge is ever persisted without a valid const-driven type.
func relationTypeOrDefault(t string) string {
	switch t {
	case consts.RelationTemporal, consts.RelationCorrelation, consts.RelationDefault:
		return t
	default:
		return consts.RelationDefault
	}
}

// createdByOrDefault falls back to user provenance (manual mapping) when unset.
func createdByOrDefault(by string) string {
	if by == consts.CreatedBySystem {
		return consts.CreatedBySystem
	}
	return consts.CreatedByUser
}
