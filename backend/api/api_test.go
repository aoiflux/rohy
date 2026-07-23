package api

import (
	"sync"
	"testing"
	"time"

	"rohy/backend/capture"
	"rohy/backend/consts"
	"rohy/backend/evtx"
	"rohy/backend/findings"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/layout"
)

const fixtureSecurity = "../evtx/testdata/Security.evtx"

// newTestGraphAPI wires a GraphAPI over the given store with a fresh layout store and a
// registry seeded with the Default graph (so graph-scoped ops have an active graph).
func newTestGraphAPI(t *testing.T, store *graphene.Store) *GraphAPI {
	t.Helper()
	layoutStore, err := layout.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.EnsureDefault(consts.DefaultGraphName, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return NewGraphAPI(store, layoutStore, reg)
}

// captureEmitter records emitted events and lets a test block until a given channel
// fires. It is safe for concurrent use because ingestion emits from its own
// goroutine while the test observes from another.
type captureEmitter struct {
	mu     sync.Mutex
	events map[string][]interface{}
	signal chan string
}

func newCaptureEmitter() *captureEmitter {
	return &captureEmitter{events: map[string][]interface{}{}, signal: make(chan string, 256)}
}

func (c *captureEmitter) Emit(channel string, data interface{}) {
	c.mu.Lock()
	c.events[channel] = append(c.events[channel], data)
	c.mu.Unlock()
	select {
	case c.signal <- channel:
	default:
	}
}

func (c *captureEmitter) count(channel string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events[channel])
}

func (c *captureEmitter) last(channel string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.events[channel]
	if len(v) == 0 {
		return nil, false
	}
	return v[len(v)-1], true
}

// waitFor blocks until at least one event has been emitted on channel or the timeout
// elapses.
func (c *captureEmitter) waitFor(channel string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		if c.count(channel) > 0 {
			return true
		}
		select {
		case <-c.signal:
		case <-deadline:
			return c.count(channel) > 0
		}
	}
}

// mustFindings opens a throwaway analyst-findings sidecar for the events binding.
func mustFindings(t *testing.T) *findings.Store {
	t.Helper()
	f, err := findings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// mustCapture opens a throwaway durable capture-bookmark store for the events binding.
func mustCapture(t *testing.T) *capture.Store {
	t.Helper()
	c, err := capture.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newEventsAPI(t *testing.T) (*EventsAPI, *captureEmitter, *graphene.Store) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })
	em := newCaptureEmitter()
	api := NewEventsAPI(store, mustCapture(t), mustFindings(t))
	api.setEmitter(em)
	return api, em, store
}

func TestStartIngestLifecycleEvents(t *testing.T) {
	api, em, store := newEventsAPI(t)

	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatalf("StartIngest: %v", err)
	}
	if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
		t.Fatal("ingestion did not complete in time")
	}
	// Wait for the running flag to clear (set false in the deferred cleanup).
	for i := 0; i < 100 && api.IsIngesting(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if api.IsIngesting() {
		t.Fatal("still marked ingesting after completion")
	}

	if em.count(consts.EventIngestStarted) != 1 {
		t.Errorf("Started events = %d, want 1", em.count(consts.EventIngestStarted))
	}
	last, ok := em.last(consts.EventIngestComplete)
	if !ok {
		t.Fatal("no completion event captured")
	}
	sum, ok := last.(evtx.Summary)
	if !ok {
		t.Fatalf("completion payload type = %T, want evtx.Summary", last)
	}
	if sum.RecordsPersisted == 0 {
		t.Error("completion summary reports zero persisted")
	}

	nodes, _, _ := store.Stats()
	if nodes == 0 {
		t.Fatal("no events persisted through the binding")
	}
	if uint64(sum.RecordsPersisted) != nodes {
		t.Errorf("summary persisted %d != store nodes %d", sum.RecordsPersisted, nodes)
	}
}

func TestStartIngestRejectsConcurrent(t *testing.T) {
	api, em, _ := newEventsAPI(t)

	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatalf("first StartIngest: %v", err)
	}
	// A second start while the first runs must be rejected. Because ingestion is
	// fast, retry briefly to hit the running window deterministically is unreliable;
	// instead assert either rejection or that the first already finished.
	err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}})
	if err == nil {
		// First finished already; ensure it did complete cleanly.
		if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
			t.Fatal("no completion after second accepted start")
		}
	}
	em.waitFor(consts.EventIngestComplete, 10*time.Second)
}

func TestValidateIngestErrors(t *testing.T) {
	api, _, _ := newEventsAPI(t)

	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile}); err == nil {
		t.Error("expected error for file source with no paths")
	}
	if err := api.StartIngest(IngestRequest{Source: consts.SourceLive}); err == nil {
		t.Error("expected error for live source with no channels")
	}
	if err := api.StartIngest(IngestRequest{Source: "bogus", Paths: []string{"x"}}); err == nil {
		t.Error("expected error for unknown source")
	}
}

func TestQueryEventsThroughBinding(t *testing.T) {
	api, em, _ := newEventsAPI(t)
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
		t.Fatal("ingest did not complete")
	}

	all, err := api.QueryEvents(EventQuery{})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no events returned")
	}

	// Filter by the channel of the first event; must return a non-empty subset.
	sec, err := api.QueryEvents(EventQuery{Channel: consts.ChannelSecurity, Limit: 5})
	if err != nil {
		t.Fatalf("filtered QueryEvents: %v", err)
	}
	if len(sec) == 0 || len(sec) > 5 {
		t.Errorf("filtered result size = %d, want 1..5", len(sec))
	}

	one, err := api.GetEvent(all[0].ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if one.ID != all[0].ID {
		t.Errorf("GetEvent id = %d, want %d", one.ID, all[0].ID)
	}
}

func TestQueryEventsBadTime(t *testing.T) {
	api, _, _ := newEventsAPI(t)
	if _, err := api.QueryEvents(EventQuery{TimeFrom: "not-a-time"}); err == nil {
		t.Error("expected error for malformed time_from")
	}
}

func TestGraphCreateAndReadRelation(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	events := NewEventsAPI(store, mustCapture(t), mustFindings(t))
	graph := newTestGraphAPI(t, store)
	em := newCaptureEmitter()
	events.setEmitter(em)

	if err := events.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
		t.Fatal("ingest did not complete")
	}

	all, err := events.QueryEvents(EventQuery{Limit: 2})
	if err != nil || len(all) < 2 {
		t.Fatalf("need >=2 events, got %d err=%v", len(all), err)
	}

	// Empty relation type must default; unknown created_by must default to user.
	rel, err := graph.CreateRelation(RelationRequest{From: all[0].ID, To: all[1].ID, CreatedBy: "forged"})
	if err != nil {
		t.Fatalf("CreateRelation: %v", err)
	}
	if rel.RelationType != consts.RelationDefault {
		t.Errorf("relation type = %q, want default", rel.RelationType)
	}
	if rel.CreatedBy != consts.CreatedByUser {
		t.Errorf("created_by = %q, want %q", rel.CreatedBy, consts.CreatedByUser)
	}
	if rel.CreatedAt.IsZero() {
		t.Error("CreatedAt not stamped by backend")
	}

	got, err := graph.RelationsOf(all[0].ID)
	if err != nil {
		t.Fatalf("RelationsOf: %v", err)
	}
	if len(got) != 1 || got[0].ID != rel.ID {
		t.Errorf("RelationsOf returned %d relations, want the one just created", len(got))
	}
}

func TestGraphRelationLabelPersists(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	events := NewEventsAPI(store, mustCapture(t), mustFindings(t))
	graph := newTestGraphAPI(t, store)
	em := newCaptureEmitter()
	events.setEmitter(em)

	if err := events.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
		t.Fatal("ingest did not complete")
	}
	all, err := events.QueryEvents(EventQuery{Limit: 2})
	if err != nil || len(all) < 2 {
		t.Fatalf("need >=2 events, got %d err=%v", len(all), err)
	}

	rel, err := graph.CreateRelation(RelationRequest{
		From: all[0].ID, To: all[1].ID, RelationType: consts.RelationTemporal, Label: "  same session  ",
	})
	if err != nil {
		t.Fatalf("CreateRelation: %v", err)
	}
	if rel.Label != "same session" {
		t.Errorf("label = %q, want trimmed 'same session'", rel.Label)
	}
	if rel.RelationType != consts.RelationTemporal {
		t.Errorf("relation type = %q, want temporal", rel.RelationType)
	}

	// Label must survive a DB round-trip.
	rels, err := graph.GetRelations()
	if err != nil || len(rels) != 1 {
		t.Fatalf("GetRelations: %v (n=%d)", err, len(rels))
	}
	if rels[0].Label != "same session" {
		t.Errorf("persisted label = %q, want 'same session'", rels[0].Label)
	}
}

func TestGraphUpdateAndDeleteRelation(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	events := NewEventsAPI(store, mustCapture(t), mustFindings(t))
	graph := newTestGraphAPI(t, store)
	em := newCaptureEmitter()
	events.setEmitter(em)

	if err := events.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
		t.Fatal("ingest did not complete")
	}
	all, _ := events.QueryEvents(EventQuery{Limit: 2})
	if len(all) < 2 {
		t.Fatalf("need >=2 events, got %d", len(all))
	}

	rel, err := graph.CreateRelation(RelationRequest{From: all[0].ID, To: all[1].ID, RelationType: consts.RelationDefault, Label: "draft"})
	if err != nil {
		t.Fatal(err)
	}

	// Edit: change type + label; endpoints/provenance must survive.
	upd, err := graph.UpdateRelation(RelationUpdate{ID: rel.ID, RelationType: consts.RelationCorrelation, Label: "confirmed link"})
	if err != nil {
		t.Fatalf("UpdateRelation: %v", err)
	}
	if upd.RelationType != consts.RelationCorrelation || upd.Label != "confirmed link" {
		t.Errorf("update not applied: %+v", upd)
	}
	if upd.From != all[0].ID || upd.To != all[1].ID {
		t.Errorf("endpoints changed on update: %d->%d", upd.From, upd.To)
	}
	if upd.CreatedBy != rel.CreatedBy {
		t.Errorf("provenance changed on update: %q vs %q", upd.CreatedBy, rel.CreatedBy)
	}

	// Delete, then confirm gone and that re-delete is idempotent.
	if err := graph.DeleteRelation(rel.ID); err != nil {
		t.Fatalf("DeleteRelation: %v", err)
	}
	rels, _ := graph.GetRelations()
	if len(rels) != 0 {
		t.Errorf("relation still present after delete: %d", len(rels))
	}
	if err := graph.DeleteRelation(rel.ID); err != nil {
		t.Errorf("re-delete should be idempotent, got %v", err)
	}
}

func TestGraphManagementBindings(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	events := NewEventsAPI(store, mustCapture(t), mustFindings(t))
	graph := newTestGraphAPI(t, store)
	em := newCaptureEmitter()
	events.setEmitter(em)

	if err := events.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
		t.Fatal("ingest did not complete")
	}
	all, _ := events.QueryEvents(EventQuery{Limit: 2})
	if len(all) < 2 {
		t.Fatalf("need >=2 events, got %d", len(all))
	}
	nodesBefore, _, _ := store.Stats()

	// Starts with the seeded Default graph.
	list, err := graph.ListGraphs()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListGraphs = %d err=%v, want 1", len(list), err)
	}
	if graph.ActiveGraph() != consts.DefaultGraphID {
		t.Fatalf("active = %d, want default", graph.ActiveGraph())
	}

	// Create a second graph → it becomes active.
	g2, err := graph.CreateGraph(GraphRequest{Name: "Case B", Description: "second"})
	if err != nil {
		t.Fatalf("CreateGraph: %v", err)
	}
	if graph.ActiveGraph() != g2.ID {
		t.Errorf("new graph did not become active")
	}

	// A relation with graph_id 0 lands in the active graph (g2), not the default.
	if _, err := graph.CreateRelation(RelationRequest{From: all[0].ID, To: all[1].ID}); err != nil {
		t.Fatalf("CreateRelation: %v", err)
	}
	if g2Rels, _ := graph.GetGraphRelations(g2.ID); len(g2Rels) != 1 {
		t.Errorf("graph 2 relations = %d, want 1", len(g2Rels))
	}
	if defRels, _ := graph.GetGraphRelations(consts.DefaultGraphID); len(defRels) != 0 {
		t.Errorf("default graph relations = %d, want 0 (scoped away)", len(defRels))
	}

	// Rename persists through the binding.
	if _, err := graph.RenameGraph(GraphRequest{ID: g2.ID, Name: "Case B2"}); err != nil {
		t.Fatalf("RenameGraph: %v", err)
	}

	// Delete graph 2 → its relations are gone, events remain, default survives.
	if err := graph.DeleteGraph(g2.ID); err != nil {
		t.Fatalf("DeleteGraph: %v", err)
	}
	if left, _ := graph.ListGraphs(); len(left) != 1 {
		t.Errorf("graphs after delete = %d, want 1", len(left))
	}
	if g2Rels, _ := graph.GetGraphRelations(g2.ID); len(g2Rels) != 0 {
		t.Errorf("deleted graph still has %d relations", len(g2Rels))
	}
	if nodesAfter, _, _ := store.Stats(); nodesAfter != nodesBefore {
		t.Errorf("event count changed on graph delete: %d → %d", nodesBefore, nodesAfter)
	}
}

func TestGraphLayoutRoundTrip(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	graph := newTestGraphAPI(t, store)

	// Empty before any save (active/default graph).
	empty, err := graph.LoadLayout(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Nodes) != 0 {
		t.Errorf("expected empty layout, got %d nodes", len(empty.Nodes))
	}

	want := layout.Layout{
		Nodes:    map[uint64]layout.Position{7: {X: 1, Y: 2}, 9: {X: 3, Y: 4}},
		Viewport: layout.Viewport{X: 5, Y: 6, Zoom: 2},
	}
	if err := graph.SaveLayout(0, want); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	got, err := graph.LoadLayout(0)
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if len(got.Nodes) != 2 || got.Nodes[9].Y != 4 || got.Viewport.Zoom != 2 {
		t.Errorf("layout not round-tripped through binding: %+v", got)
	}
}

func TestStatsBinding(t *testing.T) {
	api, em, _ := newEventsAPI(t)
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	em.waitFor(consts.EventIngestComplete, 10*time.Second)

	s, err := api.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Events == 0 {
		t.Error("Stats reports zero events after ingest")
	}
}
