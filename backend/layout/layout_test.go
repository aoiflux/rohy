package layout

import (
	"os"
	"path/filepath"
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
