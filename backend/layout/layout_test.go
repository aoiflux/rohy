package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testGraphID = 1

func TestSaveLoadRoundTrip(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Loading before any save returns an empty, non-nil layout.
	empty, err := store.Load(testGraphID)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if empty == nil || len(empty.Nodes) != 0 {
		t.Fatalf("expected empty layout, got %+v", empty)
	}

	want := &Layout{
		Nodes: map[uint64]Position{
			5:  {X: 10, Y: 20},
			42: {X: -3.5, Y: 128},
		},
		Viewport: Viewport{X: 100, Y: -50, Zoom: 1.5},
	}
	if err := store.Save(testGraphID, want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.Load(testGraphID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("node count = %d, want 2", len(got.Nodes))
	}
	if got.Nodes[42].Y != 128 || got.Nodes[5].X != 10 {
		t.Errorf("positions not round-tripped: %+v", got.Nodes)
	}
	if got.Viewport.Zoom != 1.5 {
		t.Errorf("viewport zoom = %v, want 1.5", got.Viewport.Zoom)
	}
}

func TestSaveNilSafe(t *testing.T) {
	store, _ := Open(t.TempDir())
	if err := store.Save(testGraphID, nil); err != nil {
		t.Fatalf("save nil: %v", err)
	}
	got, err := store.Load(testGraphID)
	if err != nil || got == nil {
		t.Fatalf("load after nil save: %v", err)
	}
}

// TestPerGraphIsolation confirms two graphs keep independent layouts.
func TestPerGraphIsolation(t *testing.T) {
	store, _ := Open(t.TempDir())
	if err := store.Save(1, &Layout{Nodes: map[uint64]Position{5: {X: 1, Y: 1}}}); err != nil {
		t.Fatalf("save g1: %v", err)
	}
	if err := store.Save(2, &Layout{Nodes: map[uint64]Position{9: {X: 2, Y: 2}}}); err != nil {
		t.Fatalf("save g2: %v", err)
	}
	g1, _ := store.Load(1)
	g2, _ := store.Load(2)
	if _, ok := g1.Nodes[5]; !ok || len(g1.Nodes) != 1 {
		t.Errorf("graph 1 layout leaked: %+v", g1.Nodes)
	}
	if _, ok := g2.Nodes[9]; !ok || len(g2.Nodes) != 1 {
		t.Errorf("graph 2 layout leaked: %+v", g2.Nodes)
	}

	// Delete graph 2 → its layout is gone, graph 1 untouched.
	if err := store.Delete(2); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if g2, _ := store.Load(2); len(g2.Nodes) != 0 {
		t.Errorf("graph 2 layout survived delete")
	}
	if g1, _ := store.Load(1); len(g1.Nodes) != 1 {
		t.Errorf("graph 1 layout affected by graph 2 delete")
	}
}

// TestMigrateLegacy folds a pre-P15 canvas.json into the default graph's file, once.
func TestMigrateLegacy(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)

	// Simulate a legacy single-graph layout on disk.
	legacy := filepath.Join(dir, legacyLayoutFile)
	if err := os.WriteFile(legacy, []byte(`{"nodes":{"7":{"x":3,"y":4}},"viewport":{"x":0,"y":0,"zoom":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	migrated, err := store.MigrateLegacy(1)
	if err != nil || !migrated {
		t.Fatalf("migrate = %v, err %v; want true", migrated, err)
	}
	got, _ := store.Load(1)
	if got.Nodes[7].X != 3 {
		t.Fatalf("legacy layout not migrated: %+v", got.Nodes)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy file should be gone after migration")
	}

	// Idempotent: nothing left to migrate.
	if again, _ := store.MigrateLegacy(1); again {
		t.Errorf("second migration should be a no-op")
	}
}

// TestRemoveNodeDropsTheIDFromEveryGraphAndNothingElse pins that a deleted event leaves every
// graph's canvas, that whatever else a canvas holds (other nodes, the viewport) survives, and
// that the other files a layout directory can hold are never read as canvases.
func TestRemoveNodeDropsTheIDFromEveryGraphAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(1, &Layout{
		Nodes:    map[uint64]Position{5: {X: 1, Y: 1}, 6: {X: 2, Y: 2}},
		Viewport: Viewport{X: 7, Y: 8, Zoom: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(2, &Layout{Nodes: map[uint64]Position{5: {X: 3, Y: 3}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(3, &Layout{Nodes: map[uint64]Position{6: {X: 4, Y: 4}}}); err != nil {
		t.Fatal(err)
	}

	// A .tmp left by an interrupted Save, a legacy canvas.json that the migration left behind,
	// and a directory that merely has a canvas's name. None of them is a canvas, and reading any
	// of them as one would fail the prune or rewrite a file nobody reads. The .tmp belongs to
	// graph 3, which does not hold the event, so no Save of the prune's own reuses it.
	stray := filepath.Join(dir, "canvas-3.json.tmp")
	if err := os.WriteFile(stray, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, legacyLayoutFile)
	legacyBody := []byte(`{"nodes":{"5":{"x":9,"y":9}},"viewport":{"x":0,"y":0,"zoom":1}}`)
	if err := os.WriteFile(legacy, legacyBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "canvas-7.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := store.RemoveNode(5); err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}

	g1, err := store.Load(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, held := g1.Nodes[5]; held || len(g1.Nodes) != 1 || g1.Nodes[6] != (Position{X: 2, Y: 2}) {
		t.Errorf("graph 1 after the prune = %+v, want only node 6 at {2,2}", g1.Nodes)
	}
	if g1.Viewport != (Viewport{X: 7, Y: 8, Zoom: 2}) {
		t.Errorf("graph 1 lost its viewport: %+v", g1.Viewport)
	}
	if g2, err := store.Load(2); err != nil || len(g2.Nodes) != 0 {
		t.Errorf("graph 2 after the prune = %+v (err %v), want no nodes", g2, err)
	}
	if g3, err := store.Load(3); err != nil || len(g3.Nodes) != 1 || g3.Nodes[6] != (Position{X: 4, Y: 4}) {
		t.Errorf("graph 3 does not hold the event and must be left alone, got %+v (err %v)", g3, err)
	}
	if got, err := os.ReadFile(stray); err != nil || string(got) != "not json" {
		t.Errorf("the stray .tmp was touched: %q (err %v)", got, err)
	}
	if got, err := os.ReadFile(legacy); err != nil || string(got) != string(legacyBody) {
		t.Errorf("the legacy canvas.json was rewritten: %q (err %v)", got, err)
	}
}

// TestRemoveNodeReportsAnUnreadableCanvasAndStillPrunesTheRest pins that one bad file neither
// hides its failure nor stops the walk: the readable canvases still lose the event. os.ReadDir
// returns names sorted, so the bad canvas-2.json is reached before the readable canvas-3.json,
// and a walk that stopped at the first failure would leave graph 3 naming the event. A second
// bad file pins that every failure is reported, not only the first.
func TestRemoveNodeReportsAnUnreadableCanvasAndStillPrunesTheRest(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, graphID := range []uint64{1, 3} {
		if err := store.Save(graphID, &Layout{Nodes: map[uint64]Position{5: {X: 1, Y: 1}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"canvas-2.json", "canvas-4.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not json"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err = store.RemoveNode(5)
	if err == nil || !strings.Contains(err.Error(), "canvas-2.json") || !strings.Contains(err.Error(), "canvas-4.json") {
		t.Errorf("RemoveNode = %v, want an error that names both canvas-2.json and canvas-4.json", err)
	}
	for _, graphID := range []uint64{1, 3} {
		l, err := store.Load(graphID)
		if err != nil {
			t.Fatal(err)
		}
		if _, held := l.Nodes[5]; held {
			t.Errorf("readable canvas %d still holds the event: %+v", graphID, l.Nodes)
		}
	}
}

// TestConcurrentSaveAndRemoveNodeLeaveAReadableCanvas is a smoke test for the store's mutex,
// meant for -race. Writers of one graph share its .tmp, and on Windows a rename over a file
// that a reader holds open fails, so readers run alongside the writers too. A lost update
// between two writes is timing-dependent, so this cannot prove the lock; it can only catch a
// write or read that fails outright.
func TestConcurrentSaveAndRemoveNodeLeaveAReadableCanvas(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const rounds = 50
	errs := make(chan error, 3*rounds)
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			l := &Layout{Nodes: map[uint64]Position{uint64(i): {X: 1, Y: 1}, uint64(i + rounds): {X: 2, Y: 2}}}
			if err := store.Save(1, l); err != nil {
				errs <- fmt.Errorf("Save: %w", err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if err := store.RemoveNode(uint64(i)); err != nil {
				errs <- fmt.Errorf("RemoveNode: %w", err)
			}
		}(i)
		go func() {
			defer wg.Done()
			if _, err := store.Load(1); err != nil {
				errs <- fmt.Errorf("Load: %w", err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if _, err := store.Load(1); err != nil {
		t.Fatalf("the final canvas cannot be read: %v", err)
	}
}
