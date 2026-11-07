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

// deletedAndOffered takes a snapshot of the fixture's graph, deletes its one link, and returns
// the snapshot id and the deleted link's snapshot id, which a restore then offers.
func deletedAndOffered(t *testing.T, api *SnapshotAPI, store *graphene.Store, graphID uint64) (string, uint64) {
	t.Helper()
	got, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	rels, err := store.RelationsByGraph(graphID)
	if err != nil || len(rels) != 1 {
		t.Fatalf("fixture relations = %d (%v), want 1", len(rels), err)
	}
	if err := store.DeleteRelation(rels[0].ID); err != nil {
		t.Fatal(err)
	}
	return got.ID, rels[0].ID
}

func TestRestoringTwiceDoesNotDuplicateALink(t *testing.T) {
	// The re-created link has an id the snapshot never saw. Matched on the id alone, it stayed on
	// offer, and a second apply with the same ticks drew the same link again beside the first.
	api, store, _, graphID, _ := snapFixture(t)
	snapID, relID := deletedAndOffered(t, api, store, graphID)
	req := RestoreRequest{GraphID: graphID, ID: snapID, Recreate: []uint64{relID}}

	first, err := api.ApplyRestore(req)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if first.RelationsCreated != 1 {
		t.Fatalf("first apply created %d, want the deleted link", first.RelationsCreated)
	}
	second, err := api.ApplyRestore(req)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if second.RelationsCreated != 0 {
		t.Errorf("second apply created %d, want nothing: the link is already there", second.RelationsCreated)
	}
	after, err := store.RelationsByGraph(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Errorf("graph holds %d relations after restoring twice, want 1", len(after))
	}
}

func TestPreviewAfterARestoreOffersNothing(t *testing.T) {
	api, store, _, graphID, _ := snapFixture(t)
	snapID, relID := deletedAndOffered(t, api, store, graphID)
	if _, err := api.ApplyRestore(RestoreRequest{GraphID: graphID, ID: snapID, Recreate: []uint64{relID}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	after, err := store.RelationsByGraph(graphID)
	if err != nil || len(after) != 1 {
		t.Fatalf("graph holds %d relations (%v), want the re-created one", len(after), err)
	}

	plan, err := api.PreviewRestore(graphID, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RelationsOffered != 0 || plan.RelationsApplied != 1 {
		t.Errorf("offered %d / applied %d, want the re-created link counted as still there: %+v",
			plan.RelationsOffered, plan.RelationsApplied, plan.Relations)
	}
	if offered := plan.Recreatable(); len(offered) != 0 {
		t.Errorf("the re-created link is offered again: %+v", offered)
	}
	// The plan names the link that is there now, not the one the snapshot recorded.
	if plan.Relations[0].LiveID != after[0].ID {
		t.Errorf("live id = %d, want the re-created link %d (the deleted one was %d)",
			plan.Relations[0].LiveID, after[0].ID, relID)
	}
}

func TestPreviewDoesNotOfferALinkARuleBuildMadeAgain(t *testing.T) {
	// Re-running the rules clears the graph's links and inserts them again under new ids. Every
	// unchanged rule link used to be offered for re-creation, and ticking one drew a user-asserted
	// copy beside the link the rule had just inferred again.
	api, store, _, graphID, events := snapFixture(t)
	got, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	// The same two steps a rule build takes: delete the graph's links, then insert the new ones.
	if _, err := store.DeleteGraphRelations(graphID); err != nil {
		t.Fatal(err)
	}
	rebuilt := &graphene.Relation{
		From: events[0].ID, To: events[1].ID, GraphID: graphID,
		RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
	}
	rebuilt.StampProvenance("brute-force", consts.AlgoSequence, "m-1", 0, []string{"logon_id=0x3e7"})
	if _, err := store.InsertRelation(rebuilt); err != nil {
		t.Fatal(err)
	}

	plan, err := api.PreviewRestore(graphID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RelationsApplied != 1 || plan.RelationsOffered != 0 {
		t.Errorf("applied %d / offered %d, want the rebuilt link counted as still there: %+v",
			plan.RelationsApplied, plan.RelationsOffered, plan.Relations)
	}
	if plan.Relations[0].LiveID != rebuilt.ID {
		t.Errorf("live id = %d, want the rebuilt link %d", plan.Relations[0].LiveID, rebuilt.ID)
	}
}

func TestARestoredLinkOfAnUnknownTypeIsNotOfferedAgain(t *testing.T) {
	// A recorded type this build does not know is re-created as the default type. The planner must
	// compare the type a re-creation writes, or that link would never match once re-created and
	// would be offered on every restore.
	api, store, _, graphID, events := snapFixture(t)
	doc := &snapshot.Document{
		ID:      snapshot.NewID(time.Now()),
		GraphID: graphID,
		TakenAt: time.Now().UTC(),
		Nodes: []snapshot.Node{
			{ID: events[0].ID, Hash: "hash-a"},
			{ID: events[1].ID, Hash: "hash-b"},
		},
		Relations: []snapshot.Relation{
			{ID: 9001, FromHash: "hash-a", ToHash: "hash-b", RelationType: "lateral"},
		},
	}
	if err := api.snaps.Save(doc); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The fixture's correlation link is a different kind, so it does not stand in for this one.
	plan, err := api.PreviewRestore(graphID, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RelationsOffered != 1 {
		t.Fatalf("offered %d, want the unknown-type link: %+v", plan.RelationsOffered, plan.Relations)
	}
	res, err := api.ApplyRestore(RestoreRequest{GraphID: graphID, ID: doc.ID, Recreate: []uint64{9001}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.RelationsCreated != 1 {
		t.Fatalf("created %d, want the unknown-type link", res.RelationsCreated)
	}
	rels, err := store.RelationsByGraph(graphID)
	if err != nil {
		t.Fatal(err)
	}
	defaults := 0
	for _, r := range rels {
		if r.RelationType == consts.RelationDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("graph holds %d default-type links, want the one re-created: %+v", defaults, rels)
	}

	again, err := api.PreviewRestore(graphID, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.RelationsOffered != 0 {
		t.Errorf("offered %d after the restore, want nothing: %+v", again.RelationsOffered, again.Relations)
	}
}

func TestOneRestoreWritesALinkTheSnapshotHeldTwiceOnce(t *testing.T) {
	// Nothing stops an analyst drawing the same link twice, so a snapshot can record it twice. A
	// link is one claim however many rows recorded it: ticking both copies writes it once.
	api, store, _, graphID, events := snapFixture(t)
	dup := &graphene.Relation{
		From: events[0].ID, To: events[1].ID, GraphID: graphID,
		RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedByUser,
	}
	if _, err := store.InsertRelation(dup); err != nil {
		t.Fatal(err)
	}
	got, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if got.Relations != 2 {
		t.Fatalf("snapshot holds %d relations, want both copies", got.Relations)
	}
	if _, err := store.DeleteGraphRelations(graphID); err != nil {
		t.Fatal(err)
	}

	plan, err := api.PreviewRestore(graphID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	offered := plan.Recreatable()
	if len(offered) != 2 {
		t.Fatalf("offered %d, want both copies while neither is live: %+v", len(offered), plan.Relations)
	}
	res, err := api.ApplyRestore(RestoreRequest{
		GraphID: graphID, ID: got.ID, Recreate: []uint64{offered[0].SnapshotID, offered[1].SnapshotID},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.RelationsCreated != 1 {
		t.Errorf("created %d, want the link written once", res.RelationsCreated)
	}
	after, err := store.RelationsByGraph(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Errorf("graph holds %d relations, want 1", len(after))
	}
}

func TestPreviewFindsAStoredLinkWithNoTypeUnderANewId(t *testing.T) {
	// A stored link can carry no type. The snapshot's side compares it as the default type, the
	// type a re-creation would write, so the live side must go through the same fallback. Compared
	// raw, an untyped link would not match its own snapshot row once a rebuild gave it a new id,
	// and it would be offered for re-creation beside itself.
	api, store, _, graphID, events := snapFixture(t)
	untyped := &graphene.Relation{
		From: events[0].ID, To: events[1].ID, GraphID: graphID, CreatedBy: consts.CreatedBySystem,
	}
	if _, err := store.InsertRelation(untyped); err != nil {
		t.Fatal(err)
	}
	got, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if got.Relations != 2 {
		t.Fatalf("snapshot holds %d relations, want the fixture's link and the untyped one", got.Relations)
	}
	// A rebuild writes the same untyped link again, under a new id.
	if err := store.DeleteRelation(untyped.ID); err != nil {
		t.Fatal(err)
	}
	rebuilt := &graphene.Relation{
		From: events[0].ID, To: events[1].ID, GraphID: graphID, CreatedBy: consts.CreatedBySystem,
	}
	if _, err := store.InsertRelation(rebuilt); err != nil {
		t.Fatal(err)
	}

	plan, err := api.PreviewRestore(graphID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RelationsApplied != 2 || plan.RelationsOffered != 0 {
		t.Errorf("applied %d / offered %d, want the rebuilt untyped link counted as still there: %+v",
			plan.RelationsApplied, plan.RelationsOffered, plan.Relations)
	}
	for _, r := range plan.Relations {
		if r.SnapshotID == untyped.ID && r.LiveID != rebuilt.ID {
			t.Errorf("live id = %d, want the rebuilt link %d", r.LiveID, rebuilt.ID)
		}
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

func TestTakeSnapshotLeavesOutAnEventTheCaseNoLongerHolds(t *testing.T) {
	// A sidecar that still names a deleted event used to fail the snapshot outright. The snapshot
	// records what the canvas can show, and a gone event is not on it.
	api, store, _, graphID, events := snapFixture(t)
	// Deleted on the store directly, so the sidecar still names it; its one relation cascades.
	if err := store.DeleteEvent(events[1].ID); err != nil {
		t.Fatal(err)
	}
	got, err := api.TakeSnapshot(SnapshotRequest{GraphID: graphID})
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	if got.Nodes != 1 || got.Relations != 0 {
		t.Errorf("snapshot holds %d nodes / %d relations, want 1/0", got.Nodes, got.Relations)
	}
}
