package api

import (
	"errors"
	"fmt"
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
	// cascades are the per-graph sidecars that must be dropped when a graph is deleted —
	// snapshots and annotations today. They are registered rather than imported so this binding
	// does not grow a dependency on every sidecar that ever attaches to a graph, and so a new one
	// is added in one place (app.go) instead of edited into DeleteGraph.
	cascades []GraphCascade
}

// GraphCascade is a per-graph sidecar that is dropped along with its graph.
type GraphCascade interface {
	DeleteGraph(graphID uint64) error
}

// NewGraphAPI constructs the binding over an open event store, layout store, and graph
// registry.
func NewGraphAPI(store *graphene.Store, layoutStore *layout.Store, registry *graphreg.Store) *GraphAPI {
	return &GraphAPI{store: store, layout: layoutStore, registry: registry}
}

// WithCascades registers the sidecars a graph deletion must also clear. Returns the receiver so
// it can be chained at construction.
func (a *GraphAPI) WithCascades(c ...GraphCascade) *GraphAPI {
	a.cascades = append(a.cascades, c...)
	return a
}

// RelationDetail is everything the relationship inspector needs about one edge, resolved in a
// single call so the panel does not have to assemble it from three round trips.
type RelationDetail struct {
	Relation *graphene.Relation `json:"relation"`
	From     *graphene.Event    `json:"from"`
	To       *graphene.Event    `json:"to"`
	// GraphName is the named graph this edge belongs to, or empty when the registry no longer
	// has one — which happens after a rule is renamed, since a rule's id is a slug of its name.
	GraphName string `json:"graph_name"`
	// SiblingIDs are the other edges of the SAME occurrence. It is what lets the canvas light
	// up a whole match from any one of its edges, which is the question an analyst actually has
	// when looking at a chain: "what else is part of this?".
	//
	// Empty for a hand-drawn edge and for anything built before provenance existed — those have
	// no occurrence to belong to, which is a different statement from belonging to one alone.
	SiblingIDs []uint64 `json:"sibling_ids"`
	// Recorded reports whether this edge carries provenance at all. False means it was written
	// before v0.2.0, and the inspector says so rather than showing empty fields that read as
	// "the rule recorded nothing".
	Recorded bool `json:"recorded"`
}

// InspectRelation resolves one edge into everything worth showing about it: its endpoints, the
// graph it belongs to, and — for a rule-created edge — which rule and occurrence produced it.
//
// The endpoints come back as full events because the inspector shows them, and a single event
// read is the one place the cold store is meant to be paid for.
func (a *GraphAPI) InspectRelation(id uint64) (RelationDetail, error) {
	var out RelationDetail

	rel, err := a.store.GetRelation(id)
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}
	out.Relation = rel
	out.Recorded = rel.RelVersion > 0

	if out.From, err = a.endpoint(rel.ID, rel.From); err != nil {
		return out, err
	}
	if out.To, err = a.endpoint(rel.ID, rel.To); err != nil {
		return out, err
	}
	if a.registry != nil {
		// Scanned rather than looked up: a case holds a handful of named graphs, so a linear
		// pass is cheaper than adding a registry method for one caller.
		for _, g := range a.registry.List() {
			if g.ID == rel.GraphID {
				out.GraphName = g.Name
				break
			}
		}
	}

	// Siblings are found through the rule_id index and then narrowed by match id. Going via the
	// index rather than scanning every edge is what keeps this cheap on a graph with hundreds
	// of thousands of relations; the match id is deliberately not indexed because it is
	// near-unique, and narrowing one rule's edges in memory is the cheaper shape.
	if rel.MatchID != "" && rel.RuleID != "" {
		siblings, err := a.store.RelationsByRule(rel.RuleID)
		if err != nil {
			return out, AsError(consts.ErrCodePersistence, fmt.Errorf(consts.MsgInspectSiblingsUnreadable, rel.ID, rel.MatchID, err))
		}
		for _, s := range siblings {
			if s.MatchID == rel.MatchID && s.ID != rel.ID {
				out.SiblingIDs = append(out.SiblingIDs, s.ID)
			}
		}
	}
	return out, nil
}

// endpoint reads one end of a link for the inspector. An event the case no longer holds is nil,
// which the inspector leaves out; any other failure is returned, because an end that is there but
// cannot be read is not an end that is gone.
func (a *GraphAPI) endpoint(relID, eventID uint64) (*graphene.Event, error) {
	e, err := a.store.GetEvent(eventID)
	if err == nil {
		return e, nil
	}
	var nf *store.ErrNotFound
	if errors.As(err, &nf) {
		return nil, nil
	}
	return nil, AsError(consts.ErrCodePersistence, fmt.Errorf(consts.MsgInspectEndpointUnreadable, relID, eventID, err))
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
		return graphError(err)
	}
	return nil
}

// CreateGraph adds a new named graph and makes it active.
func (a *GraphAPI) CreateGraph(req GraphRequest) (*graphreg.Graph, error) {
	if err := theCase.writable(); err != nil {
		return nil, err
	}
	if a.registry == nil {
		return nil, AsError(consts.ErrCodeInternal, errors.New("graph registry unavailable"))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, AsError(consts.ErrCodeParse, errors.New(consts.MsgGraphNameRequired))
	}
	g, err := a.registry.Create(name, strings.TrimSpace(req.Description), time.Now().UTC())
	if err != nil {
		return nil, graphError(err)
	}
	return g, nil
}

// RenameGraph updates a graph's name and description.
func (a *GraphAPI) RenameGraph(req GraphRequest) (*graphreg.Graph, error) {
	if err := theCase.writable(); err != nil {
		return nil, err
	}
	if a.registry == nil {
		return nil, AsError(consts.ErrCodeInternal, errors.New("graph registry unavailable"))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, AsError(consts.ErrCodeParse, errors.New(consts.MsgGraphNameRequired))
	}
	g, err := a.registry.Rename(req.ID, name, strings.TrimSpace(req.Description), time.Now().UTC())
	if err != nil {
		return nil, graphError(err)
	}
	return g, nil
}

// graphError codes a registry failure. A refusal (the last graph, a graph that is not there) is the
// request's fault, parse_error, so it does not read as a fault in rohy; anything else is a failed
// write.
func graphError(err error) error {
	if errors.Is(err, graphreg.ErrLastGraph) || errors.Is(err, graphreg.ErrNotFound) {
		return AsError(consts.ErrCodeParse, err)
	}
	return AsError(consts.ErrCodePersistence, err)
}

// DeleteGraph removes a graph and cascades to its relations and layout — but never to
// the underlying shared event nodes. Refuses to delete the last remaining graph.
func (a *GraphAPI) DeleteGraph(id uint64) error {
	release, busy := theCase.share(consts.CaseActivityGraphEdit)
	if busy != nil {
		return busy
	}
	defer release()
	if a.registry == nil {
		return nil
	}
	if err := a.registry.Delete(id); err != nil {
		return graphError(err)
	}
	// Registry entry is gone; now drop the graph's edges + layout (events are shared and
	// left intact). Best-effort: a failure here leaves orphaned edges scoped to a
	// deleted graph id, which no view queries.
	if _, err := a.store.DeleteGraphRelations(id); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	if a.layout != nil {
		if err := a.layout.Delete(id); err != nil {
			return AsError(consts.ErrCodePersistence, err)
		}
	}
	// The graph's own sidecars go with it. Best-effort and in order: a failure here leaves files
	// nothing can list rather than a graph that half exists, which is the better of the two.
	for _, c := range a.cascades {
		if c == nil {
			continue
		}
		if err := c.DeleteGraph(id); err != nil {
			return AsError(consts.ErrCodePersistence, err)
		}
	}
	return nil
}

// GetEvents returns the stored records behind a set of canvas cards, in the order given. An
// id no longer in the case is left out, so one deleted event costs its own card and not the
// canvas. The cold fields are not read: a card shows neither, and EventsAPI.GetEvent is
// where they are paid for.
func (a *GraphAPI) GetEvents(ids []uint64) ([]*graphene.Event, error) {
	events, err := a.store.GetEventRecords(ids)
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
	release, busy := theCase.share(consts.CaseActivityGraphEdit)
	if busy != nil {
		return nil, busy
	}
	defer release()
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
	release, busy := theCase.share(consts.CaseActivityGraphEdit)
	if busy != nil {
		return nil, busy
	}
	defer release()
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
	release, busy := theCase.share(consts.CaseActivityGraphEdit)
	if busy != nil {
		return busy
	}
	defer release()
	if err := a.store.DeleteRelation(id); err != nil {
		var nf *store.ErrNotFound
		if errors.As(err, &nf) {
			return nil
		}
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// DeleteEventResult says what a delete did beyond removing the event.
type DeleteEventResult struct {
	// SetAside names each saved canvas that did not hold a canvas, by the name it was moved to
	// and kept under. Its graph now opens without saved positions.
	SetAside []string `json:"set_aside"`
}

// DeleteEvent removes an event node, cascades to its relations, and removes it from every
// graph's saved canvas. This is destructive (forensic evidence is removed from the case DB);
// callers must confirm intent in the UI. Idempotent on a missing id.
//
// A saved canvas that does not hold a canvas is moved aside and named in the result, so one
// damaged file does not fail every delete (layout.Store.RemoveNode).
func (a *GraphAPI) DeleteEvent(id uint64) (DeleteEventResult, error) {
	var out DeleteEventResult
	release, busy := theCase.share(consts.CaseActivityGraphEdit)
	if busy != nil {
		return out, busy
	}
	defer release()
	if err := a.store.DeleteEvent(id); err != nil {
		var nf *store.ErrNotFound
		if !errors.As(err, &nf) {
			return out, AsError(consts.ErrCodePersistence, err)
		}
		// Already gone. The prune still runs: deleting again is how a delete whose prune
		// failed is finished.
	}
	// Store first, sidecars second. A crash between the two leaves a canvas naming a gone
	// event, which GetEventRecords skips; the other order could leave an event in the case
	// that its canvases no longer show.
	if a.layout != nil {
		setAside, err := a.layout.RemoveNode(id)
		out.SetAside = setAside
		if err != nil {
			return out, AsError(consts.ErrCodePersistence, fmt.Errorf(consts.MsgEventDeletedCanvasStale, id, err))
		}
	}
	return out, nil
}

// SaveLayout persists a graph's canvas node positions and viewport so a mapping session
// can be restored exactly. Layout is UI state (not graph data) stored as a per-graph
// JSON sidecar; the node id set doubles as the graph's canvas membership. A nil layout
// store makes this a no-op.
//
// A read-only session refuses it, as it refuses creating or renaming a graph: a canvas holds no
// event content, so a redaction need not exclude it, but it is the analyst's work on the case.
func (a *GraphAPI) SaveLayout(graphID uint64, l layout.Layout) error {
	if err := theCase.writable(); err != nil {
		return err
	}
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
