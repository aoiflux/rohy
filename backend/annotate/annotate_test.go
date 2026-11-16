package annotate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	for i := range consts.MaxAnnotationItems {
		if _, err := s.Put(7, region(float64(i), 0, 10, 10, ""), annBase); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if _, err := s.Put(7, region(0, 0, 10, 10, ""), annBase); !errors.Is(err, ErrInvalid) {
		t.Fatalf("past the cap = %v, want a refusal marked ErrInvalid", err)
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
	for i := range consts.MaxAnnotationLayers {
		if _, err := s.AddLayer(7, "layer", "", annBase); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if _, err := s.AddLayer(7, "one more", "", annBase); !errors.Is(err, ErrInvalid) {
		t.Fatalf("past the layer cap = %v, want a refusal marked ErrInvalid", err)
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

// blockSidecar replaces a graph's sidecar with a directory, so the rename that finishes the next
// write fails. The same method redaction/journal_test.go uses for its own file.
func blockSidecar(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// 🔒 A write that fails changes nothing in memory either. The canvas reloads from the cache after
// every edit, so a cache that ran ahead of the file would show marks a restart then loses. Each
// mutation writes something different from what is held, so a cache changed in place shows here.
func TestAFailedWriteLeavesTheCacheAsTheFileIs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(s *Store, held *Document) error
	}{
		{"put-create", func(s *Store, _ *Document) error {
			_, err := s.Put(7, note("hash-b", "second"), annBase.Add(time.Second))
			return err
		}},
		{"put-update", func(s *Store, held *Document) error {
			edited := held.Items[0]
			edited.Text = "edited"
			_, err := s.Put(7, edited, annBase.Add(time.Second))
			return err
		}},
		{"add-layer", func(s *Store, _ *Document) error {
			_, err := s.AddLayer(7, "Lateral movement", "", annBase.Add(time.Second))
			return err
		}},
		{"update-layer", func(s *Store, held *Document) error {
			l := held.Layers[0]
			l.Name, l.Visible = "Renamed", false
			_, err := s.UpdateLayer(7, l)
			return err
		}},
		{"delete-layer", func(s *Store, held *Document) error {
			_, err := s.DeleteLayer(7, held.Layers[0].ID)
			return err
		}},
		{"delete", func(s *Store, held *Document) error {
			return s.Delete(7, held.Items[0].ID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openStore(t)
			if _, err := s.Put(7, note("hash-a", "first"), annBase); err != nil {
				t.Fatal(err)
			}
			before, err := s.Get(7)
			if err != nil {
				t.Fatal(err)
			}
			blockSidecar(t, filepath.Join(s.dir, "7.json"))
			if err := tc.mutate(s, before); err == nil {
				t.Fatal("the write did not fail")
			}
			after, err := s.Get(7)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Errorf("the cache moved without the file:\nbefore %+v\nafter  %+v", before, after)
			}
		})
	}
}

// A Put refused for the item limit must not leave behind the default layer it made on the way.
// Only a hand-edited sidecar reaches this (a full set of items and no layers), but the layer would
// then show on the canvas although no write ever saved it.
func TestARefusedPutLeavesNoLayerBehind(t *testing.T) {
	dir := t.TempDir()
	doc := Document{AnnotationsVersion: consts.AnnotationsVersion, GraphID: 7, Layers: []Layer{}}
	for i := range consts.MaxAnnotationItems {
		it := region(float64(i), 0, 10, 10, "")
		it.ID = fmt.Sprintf("a-hand-%d", i)
		it.Layer = "gone"
		doc.Items = append(doc.Items, it)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "7.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(7, note("hash-a", "one too many"), annBase); err == nil {
		t.Fatal("a Put past the item limit must be refused")
	}
	got, err := s.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Layers) != 0 {
		t.Errorf("a refused Put left a layer no write saved: %+v", got.Layers)
	}
}

// 🔒 Delete removes every annotation with the id, so a repeated id makes deleting one note delete
// another. A create in the same millisecond as a held item, after a delete brought the count back
// to that item's sequence, is exactly how the count alone repeats one.
func TestAnIDIsNeverRepeatedAfterADelete(t *testing.T) {
	s := openStore(t)
	a, err := s.Put(7, note("hash-a", "a"), annBase)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Put(7, note("hash-b", "b"), annBase)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(7, a.ID); err != nil {
		t.Fatal(err)
	}
	c, err := s.Put(7, note("hash-c", "c"), annBase)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == b.ID {
		t.Fatalf("a new annotation took a held id: %s", c.ID)
	}
	if err := s.Delete(7, c.ID); err != nil {
		t.Fatal(err)
	}
	doc, err := s.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 1 || doc.Items[0].ID != b.ID {
		t.Errorf("deleting one annotation took another with it: %+v", doc.Items)
	}
}

// 🔒 The layer twin of the id test. DeleteLayer removes every layer with the id and everything on
// them, so a repeated layer id would delete a second layer and its marks along with the first.
func TestALayerIDIsNeverRepeatedAfterADelete(t *testing.T) {
	s := openStore(t)
	one, err := s.AddLayer(7, "One", "", annBase)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.AddLayer(7, "Two", "", annBase)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteLayer(7, one.ID); err != nil {
		t.Fatal(err)
	}
	three, err := s.AddLayer(7, "Three", "", annBase)
	if err != nil {
		t.Fatal(err)
	}
	if three.ID == two.ID {
		t.Fatalf("a new layer took a held id: %s", three.ID)
	}
	if _, err := s.DeleteLayer(7, three.ID); err != nil {
		t.Fatal(err)
	}
	doc, err := s.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Layers) != 1 || doc.Layers[0].ID != two.ID {
		t.Errorf("deleting one layer took another with it: %+v", doc.Layers)
	}
}

// The text limit counts characters, as its message says. Counted in bytes, a note in Japanese was
// refused at a third of the stated length.
func TestTextLimitCountsCharactersNotBytes(t *testing.T) {
	s := openStore(t)
	if _, err := s.Put(7, note("hash-a", strings.Repeat("日", consts.MaxAnnotationText)), annBase); err != nil {
		t.Fatalf("a note of exactly %d characters was refused: %v", consts.MaxAnnotationText, err)
	}
	_, err := s.Put(7, note("hash-a", strings.Repeat("日", consts.MaxAnnotationText+1)), annBase)
	if err == nil {
		t.Fatal("a note one character past the limit was accepted")
	}
	if want := fmt.Sprintf(consts.MsgAnnotationTextLong, consts.MaxAnnotationText); err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	doc, err := s.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 1 {
		t.Errorf("items = %d, want only the one that fit", len(doc.Items))
	}
}

// An over-long layer name is cut to the cap between characters, on create and on rename. A cut
// between bytes split the last character and stored a name that was not valid UTF-8.
func TestOverlongLayerNameIsCutBetweenCharacters(t *testing.T) {
	s := openStore(t)
	long := "a" + strings.Repeat("日", 200)
	want := "a" + strings.Repeat("日", consts.MaxLayerNameLen-1)
	l, err := s.AddLayer(7, long, "", annBase)
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != want || !utf8.ValidString(l.Name) {
		t.Errorf("added name = %q (valid %v), want %q", l.Name, utf8.ValidString(l.Name), want)
	}
	renamed := *l
	renamed.Name = long
	got, err := s.UpdateLayer(7, renamed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want || !utf8.ValidString(got.Name) {
		t.Errorf("renamed name = %q (valid %v), want %q", got.Name, utf8.ValidString(got.Name), want)
	}
	doc, err := s.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Layers) != 1 || doc.Layers[0].Name != want {
		t.Errorf("stored layers = %+v, want one named %q", doc.Layers, want)
	}
}

// TestEveryRefusalIsMarkedInvalid pins that a request the store refuses can be told from a store
// that failed, so the binding codes the first parse_error and the second persistence_error. The
// message is the refusal's own, unchanged. The two limits are checked in TestItemLimit and
// TestLayerLimit.
func TestEveryRefusalIsMarkedInvalid(t *testing.T) {
	cases := []struct {
		name string
		want string
		do   func(s *Store) error
	}{
		{"layer with no name", consts.MsgAnnotationLayerName, func(s *Store) error {
			_, err := s.AddLayer(7, "  ", "", annBase)
			return err
		}},
		{"rename to no name", consts.MsgAnnotationLayerName, func(s *Store) error {
			l, err := s.AddLayer(7, "Initial access", "", annBase)
			if err != nil {
				return err
			}
			l.Name = ""
			_, err = s.UpdateLayer(7, *l)
			return err
		}},
		{"text too long", fmt.Sprintf(consts.MsgAnnotationTextLong, consts.MaxAnnotationText), func(s *Store) error {
			_, err := s.Put(7, note("hash-a", strings.Repeat("x", consts.MaxAnnotationText+1)), annBase)
			return err
		}},
		{"unknown kind", fmt.Sprintf(consts.MsgAnnotationBadKind, "doodle", strings.Join(consts.AnnotationKinds, ", ")), func(s *Store) error {
			_, err := s.Put(7, Item{Kind: "doodle", Anchor: Anchor{Kind: consts.AnchorWorld}}, annBase)
			return err
		}},
		{"unknown anchor", fmt.Sprintf(consts.MsgAnnotationBadAnchor, "vibes", strings.Join(consts.AnchorKinds, ", ")), func(s *Store) error {
			_, err := s.Put(7, Item{Kind: consts.AnnotationNote, Anchor: Anchor{Kind: "vibes"}}, annBase)
			return err
		}},
		{"event anchor with no hash", consts.MsgAnnotationNeedsHash, func(s *Store) error {
			_, err := s.Put(7, Item{Kind: consts.AnnotationNote, Anchor: Anchor{Kind: consts.AnchorEvent}}, annBase)
			return err
		}},
		{"arrow with one end", consts.MsgAnnotationNeedsTarget, func(s *Store) error {
			_, err := s.Put(7, Item{Kind: consts.AnnotationArrow, Anchor: Anchor{Kind: consts.AnchorEvent, Hash: "hash-a"}}, annBase)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.do(openStore(t))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want a refusal marked ErrInvalid", err)
			}
			if err.Error() != tc.want {
				t.Errorf("message = %q, want %q unchanged", err.Error(), tc.want)
			}
		})
	}
}

// TestAMissingLayerOrAFailedWriteIsNotARefusal pins the other side: an unknown layer is ErrNotFound
// (the binding codes it parse_error too, since the request named it), and a write that failed is
// neither, so it is coded the disk's fault.
func TestAMissingLayerOrAFailedWriteIsNotARefusal(t *testing.T) {
	s := openStore(t)
	it := note("hash-a", "x")
	it.Layer = "layer-nope"
	if _, err := s.Put(7, it, annBase); !errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) {
		t.Errorf("unknown layer = %v, want ErrNotFound and not ErrInvalid", err)
	}
	if _, err := s.Put(7, note("hash-a", "first"), annBase); err != nil {
		t.Fatal(err)
	}
	blockSidecar(t, filepath.Join(s.dir, "7.json"))
	_, err := s.Put(7, note("hash-b", "second"), annBase)
	if err == nil || errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) {
		t.Errorf("failed write = %v, want an error that is neither a refusal nor a missing layer", err)
	}
}
