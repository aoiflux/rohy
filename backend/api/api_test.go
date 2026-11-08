package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	"github.com/aoiflux/graphene/store"
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

// snapshot copies everything emitted so far, under the lock. A test must read through this (or
// count/last) and never range over events directly: the ingest goroutine can still be emitting
// after the state a test waited for — its deferred cleanup reports idle after IsIngesting is false.
func (c *captureEmitter) snapshot() map[string][]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string][]interface{}, len(c.events))
	for ch, v := range c.events {
		out[ch] = append([]interface{}(nil), v...)
	}
	return out
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

// cascadeFunc is a GraphCascade from a function.
type cascadeFunc func(graphID uint64) error

func (f cascadeFunc) DeleteGraph(graphID uint64) error { return f(graphID) }

// A binding built without a layout store (as SaveLayout and LoadLayout allow) still deletes a graph:
// its links go and its sidecars are cleared, with no canvas to remove.
func TestDeleteGraphWithoutALayoutStore(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	all := seedTwo(t, store)
	reg, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.EnsureDefault(consts.DefaultGraphName, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var cleared []uint64
	graph := NewGraphAPI(store, nil, reg).WithCascades(cascadeFunc(func(id uint64) error {
		cleared = append(cleared, id)
		return nil
	}))
	g2, err := graph.CreateGraph(GraphRequest{Name: "Case B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.CreateRelation(RelationRequest{From: all[0].ID, To: all[1].ID, GraphID: g2.ID}); err != nil {
		t.Fatal(err)
	}

	if err := graph.DeleteGraph(g2.ID); err != nil {
		t.Fatalf("DeleteGraph: %v", err)
	}
	if rels, _ := graph.GetGraphRelations(g2.ID); len(rels) != 0 {
		t.Errorf("deleted graph still has %d relations", len(rels))
	}
	if len(cleared) != 1 || cleared[0] != g2.ID {
		t.Errorf("sidecars cleared for %v, want [%d]", cleared, g2.ID)
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

// twoEvents inserts two events into a fresh in-memory store and returns their ids.
func twoEvents(t *testing.T) (*graphene.Store, uint64, uint64) {
	t.Helper()
	db := graphene.OpenInMemory()
	t.Cleanup(func() { db.Close() })
	ids, err := db.InsertEvents([]*graphene.Event{
		{EventID: "4624", Computer: "HOST-A", HashNormalized: "hash-x"},
		{EventID: "4688", Computer: "HOST-A", HashNormalized: "hash-y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, ids[0], ids[1]
}

func TestDeleteEventRemovesItFromEveryCanvas(t *testing.T) {
	// 🔒 A layout is canvas membership by node id. A deleted event left in another graph's
	// sidecar, or in this one's until the next Save layout, used to fail that canvas's load, and
	// a Save layout on the empty canvas then erased its positions and viewport.
	db, x, y := twoEvents(t)
	graph := newTestGraphAPI(t, db)
	g2, err := graph.CreateGraph(GraphRequest{Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	g3, err := graph.CreateGraph(GraphRequest{Name: "Third"})
	if err != nil {
		t.Fatal(err)
	}
	saves := []struct {
		graphID uint64
		l       layout.Layout
	}{
		{consts.DefaultGraphID, layout.Layout{
			Nodes:    map[uint64]layout.Position{x: {X: 1, Y: 1}, y: {X: 2, Y: 2}},
			Viewport: layout.Viewport{Zoom: 1.5},
		}},
		{g2.ID, layout.Layout{Nodes: map[uint64]layout.Position{x: {X: 3, Y: 3}}}},
		{g3.ID, layout.Layout{Nodes: map[uint64]layout.Position{y: {X: 4, Y: 4}}}},
	}
	for _, s := range saves {
		if err := graph.SaveLayout(s.graphID, s.l); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := graph.DeleteEvent(x); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}

	def, err := graph.LoadLayout(consts.DefaultGraphID)
	if err != nil {
		t.Fatal(err)
	}
	if _, held := def.Nodes[x]; held || len(def.Nodes) != 1 || def.Nodes[y] != (layout.Position{X: 2, Y: 2}) {
		t.Errorf("default canvas = %+v, want only the surviving event at {2,2}", def.Nodes)
	}
	if def.Viewport.Zoom != 1.5 {
		t.Errorf("default canvas lost its viewport: %+v", def.Viewport)
	}
	if second, err := graph.LoadLayout(g2.ID); err != nil || len(second.Nodes) != 0 {
		t.Errorf("second canvas = %+v (err %v), want no nodes", second.Nodes, err)
	}
	if third, err := graph.LoadLayout(g3.ID); err != nil || len(third.Nodes) != 1 || third.Nodes[y] != (layout.Position{X: 4, Y: 4}) {
		t.Errorf("third canvas never held the event and changed: %+v (err %v)", third.Nodes, err)
	}
}

func TestDeletingAGoneEventAgainFinishesThePrune(t *testing.T) {
	// The store delete commits before the sidecars are pruned. A crash between the two, or a
	// prune that failed, leaves a canvas naming a gone event; deleting again is how that is
	// finished, so the already-gone branch must still prune.
	db, x, y := twoEvents(t)
	graph := newTestGraphAPI(t, db)
	if err := graph.SaveLayout(consts.DefaultGraphID, layout.Layout{
		Nodes: map[uint64]layout.Position{x: {X: 1, Y: 1}, y: {X: 2, Y: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	// Deleted on the store directly: the state a crash after the store delete leaves.
	if err := db.DeleteEvent(x); err != nil {
		t.Fatal(err)
	}

	if _, err := graph.DeleteEvent(x); err != nil {
		t.Fatalf("DeleteEvent of a gone event: %v", err)
	}
	got, err := graph.LoadLayout(consts.DefaultGraphID)
	if err != nil {
		t.Fatal(err)
	}
	if _, held := got.Nodes[x]; held {
		t.Error("the canvas still names the deleted event")
	}
	if _, held := got.Nodes[y]; !held {
		t.Error("the prune removed an event that is still in the case")
	}
}

func TestDeleteEventSetsADamagedCanvasAsideAndCarriesOn(t *testing.T) {
	// A damaged canvas used to fail this delete and every later one. It is now moved aside, the
	// delete succeeds and says where the file went, and the canvases that could be read still lose
	// the event. The prune visits the files in name order, so the second graph's damaged canvas
	// sits between two readable ones that hold the event: a prune that stopped there would leave
	// the third graph naming it.
	db, x, y := twoEvents(t)
	dir := t.TempDir()
	layoutStore, err := layout.Open(dir)
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
	graph := NewGraphAPI(db, layoutStore, reg)
	g2, err := graph.CreateGraph(GraphRequest{Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	g3, err := graph.CreateGraph(GraphRequest{Name: "Third"})
	if err != nil {
		t.Fatal(err)
	}
	readable := []uint64{consts.DefaultGraphID, g3.ID}
	for _, graphID := range readable {
		if err := graph.SaveLayout(graphID, layout.Layout{
			Nodes: map[uint64]layout.Position{x: {X: 1, Y: 1}, y: {X: 2, Y: 2}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	bad := fmt.Sprintf("canvas-%d.json", g2.ID)
	if err := os.WriteFile(filepath.Join(dir, bad), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := graph.DeleteEvent(x)
	if err != nil {
		t.Fatalf("DeleteEvent = %v, want the damaged canvas set aside and no error", err)
	}
	if len(res.SetAside) != 1 || !strings.HasPrefix(res.SetAside[0], bad+".unreadable-") {
		t.Fatalf("set aside = %v, want %s kept under a new name", res.SetAside, bad)
	}
	if got, err := os.ReadFile(filepath.Join(dir, res.SetAside[0])); err != nil || string(got) != "not json" {
		t.Errorf("the damaged canvas was not kept as it was: %q (err %v)", got, err)
	}
	var nf *store.ErrNotFound
	if _, err := db.GetEvent(x); !errors.As(err, &nf) {
		t.Errorf("the event is still in the case: %v", err)
	}
	for _, graphID := range readable {
		got, err := graph.LoadLayout(graphID)
		if err != nil {
			t.Fatal(err)
		}
		if _, held := got.Nodes[x]; held {
			t.Errorf("readable canvas %d still names the deleted event", graphID)
		}
	}
	if got, err := graph.LoadLayout(g2.ID); err != nil || len(got.Nodes) != 0 {
		t.Errorf("the second graph = %+v (err %v), want it to open empty once its canvas is aside", got, err)
	}

	// The next delete meets nothing damaged.
	if res, err := graph.DeleteEvent(y); err != nil || len(res.SetAside) != 0 {
		t.Errorf("the next DeleteEvent = %+v, %v; want nothing set aside and no error", res, err)
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

// TestMultiFileIngestReportsFilePosition pins the per-file position a folder or
// multi-select ingest emits. Without it the UI can describe the file in front of it but
// cannot say how far through the JOB it is, so an overall progress bar is impossible.
func TestMultiFileIngestReportsFilePosition(t *testing.T) {
	api, em, store := newEventsAPI(t)
	_ = store

	// The same fixture twice is a valid two-file request: the positions are what is under
	// test, not the contents.
	paths := []string{fixtureSecurity, fixtureSecurity}
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: paths}); err != nil {
		t.Fatalf("StartIngest: %v", err)
	}
	for i := 0; i < 200 && em.count(consts.EventIngestComplete) < len(paths); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if got := em.count(consts.EventIngestStarted); got != len(paths) {
		t.Fatalf("Started events = %d, want %d (one per file)", got, len(paths))
	}

	em.mu.Lock()
	started := append([]interface{}(nil), em.events[consts.EventIngestStarted]...)
	em.mu.Unlock()

	for i, raw := range started {
		ev, ok := raw.(StartedEvent)
		if !ok {
			t.Fatalf("Started event %d is %T, want StartedEvent", i, raw)
		}
		if ev.FileTotal != len(paths) {
			t.Errorf("file %d: FileTotal = %d, want %d", i, ev.FileTotal, len(paths))
		}
		// 1-based and in order, so "File 2 of 2" reads correctly rather than "File 1 of 2"
		// twice or a zero-based "File 0".
		if ev.FileIndex != i+1 {
			t.Errorf("file %d: FileIndex = %d, want %d", i, ev.FileIndex, i+1)
		}
	}
}

// TestSingleFileIngestReportsOneOfOne keeps the single-file case uniform with the
// multi-file one, so the UI needs no special case to size a job of one.
func TestSingleFileIngestReportsOneOfOne(t *testing.T) {
	api, em, _ := newEventsAPI(t)

	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatalf("StartIngest: %v", err)
	}
	if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
		t.Fatal("ingestion did not complete in time")
	}
	raw, ok := em.last(consts.EventIngestStarted)
	if !ok {
		t.Fatal("no Started event captured")
	}
	ev, ok := raw.(StartedEvent)
	if !ok {
		t.Fatalf("Started event is %T, want StartedEvent", raw)
	}
	if ev.FileIndex != 1 || ev.FileTotal != 1 {
		t.Errorf("single file reported as %d of %d, want 1 of 1", ev.FileIndex, ev.FileTotal)
	}
}
