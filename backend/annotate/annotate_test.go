package annotate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
)

var annBase = time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func note(hash, text string) Item {
	return Item{
		Kind:   consts.AnnotationNote,
		Anchor: Anchor{Kind: consts.AnchorEvent, Hash: hash},
		Text:   text,
	}
}

func region(x, y, w, h float64, text string) Item {
	return Item{
		Kind:   consts.AnnotationRegion,
		Anchor: Anchor{Kind: consts.AnchorWorld, X: x, Y: y, W: w, H: h},
		Text:   text,
	}
}

func TestPutCreatesADefaultLayerRatherThanRefusing(t *testing.T) {
	// The common path is annotating something before thinking about layers at all. Stopping the
	// analyst to make one first is friction with no purpose.
	s := openStore(t)
	got, err := s.Put(7, note("hash-a", "first failure burst"), annBase)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got.Layer == "" {
		t.Fatal("the annotation landed on no layer")
	}
	doc, _ := s.Get(7)
	if len(doc.Layers) != 1 || doc.Layers[0].Name != consts.DefaultLayerName {
		t.Errorf("layers = %+v, want one named %q", doc.Layers, consts.DefaultLayerName)
	}
	if !doc.Layers[0].Visible {
		t.Error("a new layer starts hidden, so the annotation just made would be invisible")
	}
}

func TestAnnotationsAreScopedToTheirGraph(t *testing.T) {
	// 🔒 An annotation is about a PICTURE, not about an event. It must not appear on another
	// graph, which is exactly what distinguishes it from a finding.
	s := openStore(t)
	if _, err := s.Put(7, note("hash-a", "on graph 7"), annBase); err != nil {
		t.Fatal(err)
	}
	other, err := s.Get(9)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Items) != 0 {
		t.Errorf("graph 9 sees graph 7's annotations: %+v", other.Items)
	}
}

func TestEventAnchorRequiresAContentHash(t *testing.T) {
	// 🔒 Without it there is nothing to follow the event by, and the note would attach to whatever
	// node id happened to be reused after a re-ingest.
	s := openStore(t)
	_, err := s.Put(7, Item{Kind: consts.AnnotationNote, Anchor: Anchor{Kind: consts.AnchorEvent}}, annBase)
	if err == nil {
		t.Fatal("an event anchor with no hash must be refused")
	}
	if !strings.Contains(err.Error(), "hash") {
		t.Errorf("the message does not say what is missing: %v", err)
	}
}

func TestWorldAnchorNeedsNoHash(t *testing.T) {
	// A region drawn around empty space is about a place, not a record.
	s := openStore(t)
	if _, err := s.Put(7, region(0, 0, 600, 400, "lateral movement"), annBase); err != nil {
		t.Fatalf("a world-anchored region should not need a hash: %v", err)
	}
}

func TestUnknownKindsAndAnchorsAreRefusedByName(t *testing.T) {
	s := openStore(t)
	_, err := s.Put(7, Item{Kind: "doodle", Anchor: Anchor{Kind: consts.AnchorWorld}}, annBase)
	if err == nil {
		t.Fatal("an unknown kind must be refused")
	}
	for _, k := range consts.AnnotationKinds {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error does not name the %q kind: %v", k, err)
		}
	}
	_, err = s.Put(7, Item{Kind: consts.AnnotationNote, Anchor: Anchor{Kind: "vibes"}}, annBase)
	if err == nil {
		t.Fatal("an unknown anchor kind must be refused")
	}
}

func TestArrowNeedsASecondAnchor(t *testing.T) {
	s := openStore(t)
	_, err := s.Put(7, Item{
		Kind:   consts.AnnotationArrow,
		Anchor: Anchor{Kind: consts.AnchorEvent, Hash: "hash-a"},
	}, annBase)
	if err == nil {
		t.Fatal("an arrow with one end must be refused")
	}
	got, err := s.Put(7, Item{
		Kind:   consts.AnnotationArrow,
		Anchor: Anchor{Kind: consts.AnchorEvent, Hash: "hash-a"},
		To:     &Anchor{Kind: consts.AnchorEvent, Hash: "hash-b"},
	}, annBase)
	if err != nil {
		t.Fatalf("a two-ended arrow: %v", err)
	}
	if got.To == nil || got.To.Hash != "hash-b" {
		t.Errorf("the second anchor was not stored: %+v", got)
	}
}

func TestASecondAnchorOnANonArrowIsDropped(t *testing.T) {
	// Storing it would be data nothing reads, and a later build giving it a meaning would silently
	// start honouring values nobody chose.
	s := openStore(t)
	it := note("hash-a", "x")
	it.To = &Anchor{Kind: consts.AnchorWorld, X: 5}
	got, err := s.Put(7, it, annBase)
	if err != nil {
		t.Fatal(err)
	}
	if got.To != nil {
		t.Errorf("a note kept a second anchor: %+v", got.To)
	}
}

func TestOverlongTextIsRefusedRatherThanTruncated(t *testing.T) {
	// Same rule findings follow: silently discarding the tail of an analyst's reasoning is worse
	// than telling them it did not fit.
	s := openStore(t)
	_, err := s.Put(7, note("hash-a", strings.Repeat("x", consts.MaxAnnotationText+1)), annBase)
	if err == nil {
		t.Fatal("overlong text must be refused")
	}
	doc, _ := s.Get(7)
	if len(doc.Items) != 0 {
		t.Error("the refused annotation was stored anyway")
	}
}

func TestPutUpdatesInPlaceAndKeepsTheCreationTime(t *testing.T) {
	s := openStore(t)
	made, err := s.Put(7, note("hash-a", "first"), annBase)
	if err != nil {
		t.Fatal(err)
	}
	later := annBase.Add(time.Hour)
	edited := *made
	edited.Text = "second"
	got, err := s.Put(7, edited, later)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "second" {
		t.Errorf("text = %q", got.Text)
	}
	if !got.CreatedAt.Equal(annBase) {
		t.Errorf("created_at was overwritten on edit: %v", got.CreatedAt)
	}
	if !got.UpdatedAt.Equal(later) {
		t.Errorf("updated_at = %v", got.UpdatedAt)
	}
	doc, _ := s.Get(7)
	if len(doc.Items) != 1 {
		t.Errorf("an edit added a second annotation: %d", len(doc.Items))
	}
}

func TestPutRefusesAnUnknownLayer(t *testing.T) {
	s := openStore(t)
	it := note("hash-a", "x")
	it.Layer = "layer-nope"
	if _, err := s.Put(7, it, annBase); err == nil {
		t.Fatal("an annotation on a layer that does not exist must be refused")
	}
}

func TestItemLimit(t *testing.T) {
	s := openStore(t)
	for i := 0; i < consts.MaxAnnotationItems; i++ {
		if _, err := s.Put(7, region(float64(i), 0, 10, 10, ""), annBase); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if _, err := s.Put(7, region(0, 0, 10, 10, ""), annBase); err == nil {
		t.Fatal("past the cap must be refused")
	}
	// An UPDATE past the cap must still work: the limit is on how many exist, not on editing.
	doc, _ := s.Get(7)
	edit := doc.Items[0]
	edit.Text = "edited"
	if _, err := s.Put(7, edit, annBase); err != nil {
		t.Errorf("editing at the cap was refused: %v", err)
	}
}

func TestLayerLifecycle(t *testing.T) {
	s := openStore(t)
	l, err := s.AddLayer(7, "Initial access", "#c2410c", annBase)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Visible || l.Z != 0 {
		t.Errorf("new layer = %+v", l)
	}
	second, err := s.AddLayer(7, "Lateral movement", "", annBase)
	if err != nil {
		t.Fatal(err)
	}
	if second.Z <= l.Z {
		t.Errorf("a new layer must stack above the last: %d vs %d", second.Z, l.Z)
	}

	l.Name = "Renamed"
	l.Visible = false
	got, err := s.UpdateLayer(7, *l)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Renamed" || got.Visible {
		t.Errorf("update = %+v", got)
	}
	if _, err := s.UpdateLayer(7, Layer{ID: "nope", Name: "x"}); err == nil {
		t.Error("updating a layer that does not exist must be refused")
	}
	if _, err := s.AddLayer(7, "   ", "", annBase); err == nil {
		t.Error("a layer with no name must be refused")
	}
}

func TestLayerLimit(t *testing.T) {
	s := openStore(t)
	for i := 0; i < consts.MaxAnnotationLayers; i++ {
		if _, err := s.AddLayer(7, "layer", "", annBase); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if _, err := s.AddLayer(7, "one more", "", annBase); err == nil {
		t.Fatal("past the layer cap must be refused")
	}
}

func TestDeleteLayerTakesItsAnnotationsAndSaysHowMany(t *testing.T) {
	// 🔒 All-or-nothing. Orphaning the items onto another layer would leave marks on the canvas
	// the analyst thought they had just removed; leaving them on a layer that no longer exists
	// would leave them invisible and unreachable.
	s := openStore(t)
	l, _ := s.AddLayer(7, "Initial access", "", annBase)
	other, _ := s.AddLayer(7, "Keep me", "", annBase)

	for _, hash := range []string{"hash-a", "hash-b"} {
		it := note(hash, "x")
		it.Layer = l.ID
		if _, err := s.Put(7, it, annBase); err != nil {
			t.Fatal(err)
		}
	}
	keep := note("hash-c", "kept")
	keep.Layer = other.ID
	if _, err := s.Put(7, keep, annBase); err != nil {
		t.Fatal(err)
	}

	count, err := s.CountOnLayer(7, l.ID)
	if err != nil || count != 2 {
		t.Fatalf("count = %d, %v", count, err)
	}
	removed, err := s.DeleteLayer(7, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("removed %d, want 2 — the caller needs the number to warn with", removed)
	}
	doc, _ := s.Get(7)
	if len(doc.Layers) != 1 || len(doc.Items) != 1 || doc.Items[0].Layer != other.ID {
		t.Errorf("after delete: %d layers, %d items", len(doc.Layers), len(doc.Items))
	}
	if _, err := s.DeleteLayer(7, "nope"); err == nil {
		t.Error("deleting a layer that does not exist must be refused")
	}
}

func TestDeleteAnnotationIsIdempotent(t *testing.T) {
	s := openStore(t)
	made, _ := s.Put(7, note("hash-a", "x"), annBase)
	if err := s.Delete(7, made.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(7, made.ID); err != nil {
		t.Errorf("deleting twice must be success: %v", err)
	}
	doc, _ := s.Get(7)
	if len(doc.Items) != 0 {
		t.Errorf("still present: %+v", doc.Items)
	}
}

func TestGetReturnsACopy(t *testing.T) {
	// A caller mutating the result must not rewrite stored state through it.
	s := openStore(t)
	if _, err := s.Put(7, note("hash-a", "original"), annBase); err != nil {
		t.Fatal(err)
	}
	doc, _ := s.Get(7)
	doc.Items[0].Text = "tampered"
	doc.Layers[0].Name = "tampered"

	again, _ := s.Get(7)
	if again.Items[0].Text != "original" || again.Layers[0].Name == "tampered" {
		t.Error("Get handed out the stored document")
	}
}

func TestSurvivesAReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(7, note("hash-a", "persisted"), annBase); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := reopened.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 1 || doc.Items[0].Text != "persisted" {
		t.Errorf("after reopen: %+v", doc.Items)
	}
}

func TestSidecarIsReadableJSONAndHashKeyed(t *testing.T) {
	// An analyst's notes may outlive this program. The file has to be legible without it, and the
	// anchor has to be the content hash rather than a node id.
	s := openStore(t)
	if _, err := s.Put(7, note("hash-a", "first failure burst"), annBase); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, "7.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n  \"graph_id\"") {
		t.Errorf("not indented for reading:\n%s", data)
	}
	if !strings.Contains(string(data), `"hash": "hash-a"`) {
		t.Errorf("the anchor is not the content hash:\n%s", data)
	}
	var any map[string]any
	if err := json.Unmarshal(data, &any); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
}

func TestRefusesAFutureFormatVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "7.json"),
		[]byte(`{"annotations_version": 99, "graph_id": 7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(7); err == nil {
		t.Fatal("a future format version must be refused rather than partly read")
	}
}

func TestHashesListsEveryAnchoredEvent(t *testing.T) {
	// The caller uses this to report annotations whose event has left the case, rather than
	// leaving them silently unanchored on the canvas.
	s := openStore(t)
	if _, err := s.Put(7, note("hash-b", ""), annBase); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(7, Item{
		Kind:   consts.AnnotationArrow,
		Anchor: Anchor{Kind: consts.AnchorEvent, Hash: "hash-a"},
		To:     &Anchor{Kind: consts.AnchorWorld, X: 1},
	}, annBase); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(7, region(0, 0, 10, 10, ""), annBase); err != nil {
		t.Fatal(err)
	}
	got, err := s.Hashes(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "hash-a" || got[1] != "hash-b" {
		t.Errorf("hashes = %v, want the two anchored events in a stable order", got)
	}
}

func TestDeleteGraphRemovesEverything(t *testing.T) {
	s := openStore(t)
	if _, err := s.Put(7, note("hash-a", "x"), annBase); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGraph(7); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "7.json")); !os.IsNotExist(err) {
		t.Error("the sidecar survived the graph")
	}
	doc, err := s.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 0 {
		t.Errorf("the cache survived the delete: %+v", doc.Items)
	}
	if err := s.DeleteGraph(404); err != nil {
		t.Errorf("deleting a graph with no annotations must be success: %v", err)
	}
}
