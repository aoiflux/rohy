package api

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/layout"
	"rohy/backend/snapshot"
)

// snapFixture builds a two-event chain on a canvas, in one named graph.
func snapFixture(t *testing.T) (*SnapshotAPI, *graphene.Store, *layout.Store, uint64, []*graphene.Event) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	base := time.Date(2026, 7, 27, 14, 0, 0, 0, time.UTC)
	events := []*graphene.Event{
		{EventID: "4625", Computer: "HOST-A", Timestamp: base, HashNormalized: "hash-a"},
		{EventID: "4624", Computer: "HOST-A", Timestamp: base.Add(time.Minute), HashNormalized: "hash-b"},
	}
	if _, err := store.InsertEvents(events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	registry, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := registry.EnsureForRule("brute-force", "Brute Force", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rel := &graphene.Relation{
		From: events[0].ID, To: events[1].ID, GraphID: g.ID,
		RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
	}
	rel.StampProvenance("brute-force", consts.AlgoSequence, "m-1", 0, []string{"logon_id=0x3e7"})
	if _, err := store.InsertRelation(rel); err != nil {
		t.Fatal(err)
	}

	layoutStore, err := layout.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layoutStore.Save(g.ID, &layout.Layout{
		Nodes: map[uint64]layout.Position{
			events[0].ID: {X: 10, Y: 20},
			events[1].ID: {X: 300, Y: 20},
		},
		Viewport: layout.Viewport{X: -50, Y: 5, Zoom: 0.8},
	}); err != nil {
		t.Fatal(err)
	}

	snaps, err := snapshot.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewSnapshotAPI(store, layoutStore, snaps, registry), store, layoutStore, g.ID, events
}

func TestTakeSnapshotRecordsTheCanvasWithHashes(t *testing.T) {
	api, _, _, graphID, _ := snapFixture(t)

	got, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID, Label: "before the reset"})
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if got.Nodes != 2 || got.Relations != 1 {
		t.Errorf("snapshot holds %d nodes / %d relations, want 2/1", got.Nodes, got.Relations)
	}
	if got.Label != "before the reset" {
		t.Errorf("label = %q", got.Label)
	}

	// 🔒 The hashes are what a restore matches on. Without them every restore silently falls back
	// to matching ids, which a re-ingest reassigns.
	plan, err := api.PreviewRestore(graphID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range plan.Nodes {
		if n.Hash == "" {
			t.Fatalf("a node was recorded without its content identity: %+v", n)
		}
	}
}

func TestSnapshotDescribesEventsSoAnOrphanIsStillMeaningful(t *testing.T) {
	api, _, _, graphID, _ := snapFixture(t)
	got, _ := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})

	plan, err := api.PreviewRestore(graphID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range plan.Nodes {
		if !strings.Contains(n.Descriptor, "HOST-A") {
			t.Errorf("descriptor = %q, want something a reader can recognise", n.Descriptor)
		}
	}
}

func TestPreviewRestoreWritesNothing(t *testing.T) {
	// 🔒 Nothing restores silently. The preview must be pure, or "look before you leap" is a lie.
	api, store, layoutStore, graphID, _ := snapFixture(t)
	got, _ := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})

	// Move a node, so a preview that wrote would visibly undo it.
	before, _ := layoutStore.Load(graphID)
	before.Nodes[1] = layout.Position{X: 999, Y: 999}
	if err := layoutStore.Save(graphID, before); err != nil {
		t.Fatal(err)
	}
	relsBefore, _ := store.RelationsByGraph(graphID)

	if _, err := api.PreviewRestore(graphID, got.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := layoutStore.Load(graphID)
	if after.Nodes[1].X != 999 {
		t.Error("PreviewRestore moved a node")
	}
	relsAfter, _ := store.RelationsByGraph(graphID)
	if len(relsAfter) != len(relsBefore) {
		t.Errorf("PreviewRestore changed the relations: %d -> %d", len(relsBefore), len(relsAfter))
	}
}

func TestApplyRestorePutsBackTheLayoutButNoEdgesByDefault(t *testing.T) {
	// The safe default: an empty Recreate list restores the arrangement and asserts nothing.
	api, store, layoutStore, graphID, events := snapFixture(t)
	got, _ := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})

	// Scatter the canvas and delete the edge.
	if err := layoutStore.Save(graphID, &layout.Layout{
		Nodes: map[uint64]layout.Position{events[0].ID: {X: 999, Y: 999}},
	}); err != nil {
		t.Fatal(err)
	}
	rels, _ := store.RelationsByGraph(graphID)
	if err := store.DeleteRelation(rels[0].ID); err != nil {
		t.Fatal(err)
	}

	res, err := api.ApplyRestore(RestoreRequest{GraphID: graphID, ID: got.ID})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.NodesMoved != 2 {
		t.Errorf("moved %d nodes, want both", res.NodesMoved)
	}
	if res.RelationsCreated != 0 {
		t.Errorf("created %d relations without being asked", res.RelationsCreated)
	}
	after, _ := layoutStore.Load(graphID)
	if after.Nodes[events[0].ID].X != 10 || after.Viewport.Zoom != 0.8 {
		t.Errorf("layout not restored: %+v", after)
	}
}

func TestARecreatedEdgeIsAssertedByTheAnalystNotByARule(t *testing.T) {
	// 🔒 rohy asserting a link today is a different claim from a rule having inferred it then. An
	// edge that wore the snapshot's rule provenance would be indistinguishable from one the
	// current rules produced — and would survive into an export saying so.
	api, store, _, graphID, _ := snapFixture(t)
	got, _ := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})

	rels, _ := store.RelationsByGraph(graphID)
	original := rels[0]
	if err := store.DeleteRelation(original.ID); err != nil {
		t.Fatal(err)
	}

	plan, err := api.PreviewRestore(graphID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	offered := plan.Recreatable()
	if len(offered) != 1 {
		t.Fatalf("offered %d edges, want the deleted one: %+v", len(offered), plan.Relations)
	}

	res, err := api.ApplyRestore(RestoreRequest{
		GraphID: graphID, ID: got.ID, Recreate: []uint64{offered[0].SnapshotID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.RelationsCreated != 1 {
		t.Fatalf("created %d", res.RelationsCreated)
	}

	after, _ := store.RelationsByGraph(graphID)
	if len(after) != 1 {
		t.Fatalf("graph holds %d relations", len(after))
	}
	made := after[0]
	if made.CreatedBy != consts.CreatedByUser {
		t.Errorf("created_by = %q, want the analyst", made.CreatedBy)
	}
	if made.RuleID != "" || made.MatchID != "" {
		t.Errorf("the re-created edge wears rule provenance: %+v", made)
	}
	if len(made.Basis) != 1 || !strings.Contains(made.Basis[0], got.ID) {
		t.Errorf("basis = %v, want it to name the snapshot it came from", made.Basis)
	}
	if made.RelationType != original.RelationType {
		t.Errorf("relation type not preserved: %q vs %q", made.RelationType, original.RelationType)
	}
}

func TestApplyRestoreOnlyCreatesWhatWasNamed(t *testing.T) {
	api, store, _, graphID, _ := snapFixture(t)
	got, _ := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	rels, _ := store.RelationsByGraph(graphID)
	if err := store.DeleteRelation(rels[0].ID); err != nil {
		t.Fatal(err)
	}

	// Name an id that is not on offer. Nothing should be written.
	res, err := api.ApplyRestore(RestoreRequest{GraphID: graphID, ID: got.ID, Recreate: []uint64{99999}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RelationsCreated != 0 {
		t.Errorf("created %d relations for an id that was not offered", res.RelationsCreated)
	}
}

func TestRestoreResolvesThroughHashesAfterAReingest(t *testing.T) {
	// 🔒 The end-to-end version of the planner's key case: the snapshot's node ids no longer name
	// its events, and the restore must follow the hashes rather than the numbers.
	api, store, layoutStore, graphID, events := snapFixture(t)
	got, _ := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})

	// Simulate a re-ingest: the original events are gone and the same content comes back under
	// new ids, with an unrelated event now holding the old ones.
	for _, e := range events {
		if err := store.DeleteEvent(e.ID); err != nil {
			t.Fatal(err)
		}
	}
	fresh := []*graphene.Event{
		{EventID: "1102", Computer: "HOST-Z", HashNormalized: "hash-unrelated"},
		{EventID: "4625", Computer: "HOST-A", HashNormalized: "hash-a"},
		{EventID: "4624", Computer: "HOST-A", HashNormalized: "hash-b"},
	}
	if _, err := store.InsertEvents(fresh); err != nil {
		t.Fatal(err)
	}

	plan, err := api.PreviewRestore(graphID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Reingested {
		t.Error("a re-ingest must be reported")
	}
	if plan.NodesMoved != 2 || plan.NodesUnresolved != 0 {
		t.Errorf("plan = %d moved / %d unresolved, want 2/0", plan.NodesMoved, plan.NodesUnresolved)
	}

	if _, err := api.ApplyRestore(RestoreRequest{GraphID: graphID, ID: got.ID}); err != nil {
		t.Fatal(err)
	}
	after, _ := layoutStore.Load(graphID)
	byHash := map[string]uint64{}
	for _, e := range fresh {
		byHash[e.HashNormalized] = e.ID
	}
	if after.Nodes[byHash["hash-a"]].X != 10 {
		t.Errorf("the saved position did not follow the hash: %+v", after.Nodes)
	}
	if _, wrong := after.Nodes[byHash["hash-unrelated"]]; wrong {
		t.Error("an unrelated event was given a position from the snapshot")
	}
}

func TestRestoreReportsWhatIsGoneRatherThanDroppingIt(t *testing.T) {
	api, store, _, graphID, events := snapFixture(t)
	got, _ := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	if err := store.DeleteEvent(events[1].ID); err != nil {
		t.Fatal(err)
	}

	plan, err := api.PreviewRestore(graphID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.NodesUnresolved != 1 {
		t.Errorf("unresolved = %d, want the deleted event", plan.NodesUnresolved)
	}
	if plan.RelationsMissing != 1 {
		t.Errorf("relations missing = %d", plan.RelationsMissing)
	}
	if len(plan.Nodes) != 2 {
		t.Errorf("the plan is shorter than the snapshot: %d nodes", len(plan.Nodes))
	}
}

func TestListAndDeleteSnapshots(t *testing.T) {
	api, _, _, graphID, _ := snapFixture(t)
	a, _ := api.TakeSnapshot(SnapshotRequest{GraphID: graphID, Label: "first"})
	if _, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID, Label: "second"}); err != nil {
		t.Fatal(err)
	}

	list, err := api.ListSnapshots(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d", len(list))
	}
	if err := api.DeleteSnapshot(graphID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := api.DeleteSnapshot(graphID, a.ID); err != nil {
		t.Errorf("deleting twice must be success: %v", err)
	}
	list, _ = api.ListSnapshots(graphID)
	if len(list) != 1 || list[0].Label != "second" {
		t.Errorf("after delete: %+v", list)
	}
}

func TestSnapshotExcludesEdgesToEventsNotOnTheCanvas(t *testing.T) {
	// The graph can hold an edge to an event the canvas does not show. Recording it would let a
	// restore re-create a link to something the snapshot never displayed.
	api, store, _, graphID, events := snapFixture(t)
	offCanvas := &graphene.Event{EventID: "1102", Computer: "HOST-A", HashNormalized: "hash-off"}
	if _, err := store.InsertEvents([]*graphene.Event{offCanvas}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertRelation(&graphene.Relation{
		From: events[0].ID, To: offCanvas.ID, GraphID: graphID,
		RelationType: consts.RelationDefault, CreatedBy: consts.CreatedByUser,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Relations != 1 {
		t.Errorf("recorded %d relations, want only the one whose endpoints are both on canvas", got.Relations)
	}
}

func TestSnapshotOfAnEmptyCanvas(t *testing.T) {
	api, _, layoutStore, graphID, _ := snapFixture(t)
	if err := layoutStore.Save(graphID, &layout.Layout{Nodes: map[uint64]layout.Position{}}); err != nil {
		t.Fatal(err)
	}
	got, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	if err != nil {
		t.Fatalf("an empty canvas is not an error: %v", err)
	}
	if got.Nodes != 0 || got.Relations != 0 {
		t.Errorf("empty snapshot holds %d/%d", got.Nodes, got.Relations)
	}
}
