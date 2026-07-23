// Package layout persists graph-canvas state (node positions + viewport) as a JSON
// sidecar. This is UI state, not graph data, so it lives outside the graphene
// persistence package: the DB stores events and relations, never x/y coordinates.
package layout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// legacyLayoutFile is the pre-P15 single-graph layout filename. It is migrated into the
// default graph's per-graph file on first open (see MigrateLegacy).
const legacyLayoutFile = "canvas.json"

// layoutFileFor returns the per-graph layout filename (multiple-graphs, P15). Each graph
// owns its own node positions + viewport; the node id set doubles as canvas membership.
func layoutFileFor(graphID uint64) string {
	return fmt.Sprintf("canvas-%d.json", graphID)
}

// Position is a node's world-space coordinate on the canvas.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Viewport is the canvas pan/zoom state.
type Viewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

// Layout is the persisted canvas: node positions keyed by event (graphene node) id,
// plus the viewport. Event content itself is reloaded from the DB by id.
type Layout struct {
	Nodes    map[uint64]Position `json:"nodes"`
	Viewport Viewport            `json:"viewport"`
}

// Store reads and writes the canvas layout under a directory.
type Store struct {
	dir string
}

// Open returns a layout store rooted at dir, creating the directory if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Save writes the layout for one graph, replacing any previous one. The write is atomic
// (temp file + rename) so a crash mid-write cannot corrupt an existing layout.
func (s *Store) Save(graphID uint64, l *Layout) error {
	if l == nil {
		l = &Layout{}
	}
	if l.Nodes == nil {
		l.Nodes = map[uint64]Position{}
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	name := layoutFileFor(graphID)
	tmp := filepath.Join(s.dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, name))
}

// Load returns the saved layout for a graph, or an empty (non-nil) layout when none
// exists yet.
func (s *Store) Load(graphID uint64) (*Layout, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, layoutFileFor(graphID)))
	if err != nil {
		if os.IsNotExist(err) {
			return &Layout{Nodes: map[uint64]Position{}}, nil
		}
		return nil, err
	}
	var l Layout
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	if l.Nodes == nil {
		l.Nodes = map[uint64]Position{}
	}
	return &l, nil
}

// Delete removes a graph's layout file (used when the graph is deleted). A missing file
// is not an error.
func (s *Store) Delete(graphID uint64) error {
	err := os.Remove(filepath.Join(s.dir, layoutFileFor(graphID)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MigrateLegacy folds a pre-P15 single-graph layout (canvas.json) into the given graph's
// per-graph file, once. It is a no-op if there is no legacy file or the target already
// exists. Returns true if a migration was performed.
func (s *Store) MigrateLegacy(graphID uint64) (bool, error) {
	legacy := filepath.Join(s.dir, legacyLayoutFile)
	if _, err := os.Stat(legacy); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	target := filepath.Join(s.dir, layoutFileFor(graphID))
	if _, err := os.Stat(target); err == nil {
		return false, nil // per-graph layout already exists; leave legacy untouched
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.Rename(legacy, target); err != nil {
		return false, err
	}
	return true, nil
}
