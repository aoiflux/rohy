package api

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/annotate"
	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/layout"
	"rohy/backend/snapshot"
)

func annFixture(t *testing.T) (*AnnotateAPI, *graphene.Store, uint64, []*graphene.Event) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	base := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	events := []*graphene.Event{
		{EventID: "4625", Computer: "HOST-A", Timestamp: base, HashNormalized: "hash-a"},
		{EventID: "4624", Computer: "HOST-A", Timestamp: base.Add(time.Minute), HashNormalized: "hash-b"},
	}
	if _, err := store.InsertEvents(events); err != nil {
		t.Fatal(err)
	}
	registry, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := registry.EnsureForRule("brute-force", "Brute Force", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	notes, err := annotate.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewAnnotateAPI(store, notes, registry), store, g.ID, events
}

func TestAnnotationsResolveAnchorsToLiveNodeIds(t *testing.T) {
	api, _, graphID, events := annFixture(t)

	if _, err := api.SaveAnnotation(AnnotationRequest{GraphID: graphID, Item: annotate.Item{
		Kind:   consts.AnnotationNote,
		Anchor: annotate.Anchor{Kind: consts.AnchorEvent, Hash: "hash-a"},
		Text:   "first failure burst",
	}}); err != nil {
		t.Fatal(err)
	}

	got, err := api.Annotations(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeOf["hash-a"] != events[0].ID {
		t.Errorf("node_of = %v, want hash-a on node %d", got.NodeOf, events[0].ID)
	}
	if len(got.Orphaned) != 0 {
		t.Errorf("orphaned = %v", got.Orphaned)
	}
	if len(got.Document.Items) != 1 {
		t.Errorf("document holds %d items", len(got.Document.Items))
	}
}

func TestAnnotationsFollowTheEventAfterAReingest(t *testing.T) {
	// 🔒 The point of hash anchoring. Node ids are assignment-order, so a note anchored by id
	// would end up describing whichever unrelated event inherited the number.
	api, store, graphID, events := annFixture(t)
	if _, err := api.SaveAnnotation(AnnotationRequest{GraphID: graphID, Item: annotate.Item{
		Kind:   consts.AnnotationNote,
		Anchor: annotate.Anchor{Kind: consts.AnchorEvent, Hash: "hash-a"},
		Text:   "the burst",
	}}); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if err := store.DeleteEvent(e.ID); err != nil {
			t.Fatal(err)
		}
	}
	fresh := []*graphene.Event{
		{EventID: "1102", Computer: "HOST-Z", HashNormalized: "hash-unrelated"},
		{EventID: "4625", Computer: "HOST-A", HashNormalized: "hash-a"},
	}
	if _, err := store.InsertEvents(fresh); err != nil {
		t.Fatal(err)
	}

	got, err := api.Annotations(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeOf["hash-a"] != fresh[1].ID {
		t.Errorf("the note did not follow its event: %v, want node %d", got.NodeOf, fresh[1].ID)
	}
	if got.NodeOf["hash-a"] == fresh[0].ID {
		t.Error("the note landed on the unrelated event that inherited the old id")
	}
}

func TestAnnotationsReportAnOrphanRatherThanDroppingIt(t *testing.T) {
	// A note that quietly stops being drawn looks like a note that was never made.
	api, store, graphID, events := annFixture(t)
	if _, err := api.SaveAnnotation(AnnotationRequest{GraphID: graphID, Item: annotate.Item{
		Kind:   consts.AnnotationNote,
		Anchor: annotate.Anchor{Kind: consts.AnchorEvent, Hash: "hash-a"},
		Text:   "x",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteEvent(events[0].ID); err != nil {
		t.Fatal(err)
	}

	got, err := api.Annotations(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Orphaned) != 1 || got.Orphaned[0] != "hash-a" {
		t.Errorf("orphaned = %v, want the anchor whose event is gone", got.Orphaned)
	}
	// The annotation itself survives — it is the analyst's, and it is not rohy's to delete.
	if len(got.Document.Items) != 1 {
		t.Errorf("the orphaned annotation was removed: %+v", got.Document.Items)
	}
}

func TestLayerCrudThroughTheBinding(t *testing.T) {
	api, _, graphID, _ := annFixture(t)

	made, err := api.SaveLayer(LayerRequest{GraphID: graphID, Name: "Initial access", Colour: "#c2410c"})
	if err != nil {
		t.Fatal(err)
	}
	if made.ID == "" || !made.Visible {
		t.Errorf("new layer = %+v", made)
	}
	updated, err := api.SaveLayer(LayerRequest{
		GraphID: graphID, ID: made.ID, Name: "Renamed", Visible: false, Z: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Renamed" || updated.Visible || updated.Z != 3 {
		t.Errorf("updated = %+v", updated)
	}
}

func TestDeletingALayerSaysWhatItTookAndCanBeAskedFirst(t *testing.T) {
	api, _, graphID, _ := annFixture(t)
	l, err := api.SaveLayer(LayerRequest{GraphID: graphID, Name: "Initial access"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"hash-a", "hash-b"} {
		if _, err := api.SaveAnnotation(AnnotationRequest{GraphID: graphID, Item: annotate.Item{
			Layer:  l.ID,
			Kind:   consts.AnnotationNote,
			Anchor: annotate.Anchor{Kind: consts.AnchorEvent, Hash: h},
		}}); err != nil {
			t.Fatal(err)
		}
	}

	// Asked first, so the UI can warn rather than explain afterwards.
	count, err := api.CountOnLayer(graphID, l.ID)
	if err != nil || count.Annotations != 2 {
		t.Fatalf("count = %+v, %v", count, err)
	}
	got, err := api.DeleteLayer(graphID, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Annotations != 2 {
		t.Errorf("delete reported %d", got.Annotations)
	}
	after, _ := api.Annotations(graphID)
	if len(after.Document.Items) != 0 || len(after.Document.Layers) != 0 {
		t.Errorf("after delete: %+v", after.Document)
	}
}

func TestSaveAnnotationRefusesAnEventAnchorWithNoHash(t *testing.T) {
	api, _, graphID, _ := annFixture(t)
	_, err := api.SaveAnnotation(AnnotationRequest{GraphID: graphID, Item: annotate.Item{
		Kind:   consts.AnnotationNote,
		Anchor: annotate.Anchor{Kind: consts.AnchorEvent},
	}})
	if err == nil {
		t.Fatal("an event anchor with no hash must be refused")
	}
	if !strings.Contains(err.Error(), "hash") {
		t.Errorf("message = %v", err)
	}
}

func TestDeleteAnnotationIsIdempotentThroughTheBinding(t *testing.T) {
	api, _, graphID, _ := annFixture(t)
	made, err := api.SaveAnnotation(AnnotationRequest{GraphID: graphID, Item: annotate.Item{
		Kind:   consts.AnnotationRegion,
		Anchor: annotate.Anchor{Kind: consts.AnchorWorld, W: 100, H: 100},
		Text:   "lateral movement",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := api.DeleteAnnotation(graphID, made.ID); err != nil {
		t.Fatal(err)
	}
	if err := api.DeleteAnnotation(graphID, made.ID); err != nil {
		t.Errorf("deleting twice must be success: %v", err)
	}
}

func TestAnnotationsOfAGraphWithNone(t *testing.T) {
	api, _, graphID, _ := annFixture(t)
	got, err := api.Annotations(graphID)
	if err != nil {
		t.Fatalf("a graph with no annotations is not an error: %v", err)
	}
	if got.Document == nil {
		t.Fatal("Document is nil — the canvas would have nothing to read")
	}
	if got.NodeOf == nil || got.Orphaned == nil {
		t.Error("nil maps/slices cross the wire as null and break the reader")
	}
}

func TestDeletingAGraphTakesItsSidecarsWithIt(t *testing.T) {
	// 🔒 Otherwise a deleted graph leaves snapshot and annotation files nothing can list and
	// nothing can restore — the graph id they name no longer exists.
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	registry, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.EnsureDefault(consts.DefaultGraphName, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	g, err := registry.Create("Doomed", "", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	layoutStore, err := layout.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	notes, err := annotate.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snaps, err := snapshot.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := snaps.Save(&snapshot.Document{ID: "snap-x", GraphID: g.ID}); err != nil {
		t.Fatal(err)
	}
	notesAPI := NewAnnotateAPI(store, notes, registry)
	if _, err := notesAPI.SaveAnnotation(AnnotationRequest{GraphID: g.ID, Item: annotate.Item{
		Kind:   consts.AnnotationRegion,
		Anchor: annotate.Anchor{Kind: consts.AnchorWorld, W: 10, H: 10},
	}}); err != nil {
		t.Fatal(err)
	}

	graphAPI := NewGraphAPI(store, layoutStore, registry).WithCascades(snaps, notes)
	if err := graphAPI.DeleteGraph(g.ID); err != nil {
		t.Fatalf("delete graph: %v", err)
	}

	left, err := snaps.List(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("%d snapshots outlived their graph", len(left))
	}
	doc, err := notes.Get(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 0 {
		t.Errorf("%d annotations outlived their graph", len(doc.Items))
	}
}
