// Package layout persists graph-canvas state (node positions + viewport) as a JSON
// sidecar. This is UI state, not graph data, so it lives outside the graphene
// persistence package: the DB stores events and relations, never x/y coordinates.
package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"rohy/backend/consts"
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
	// mu serialises every read and write, and holds RemoveNode's read-modify-write of each file.
	// Without it, a Save landing between RemoveNode's read and its write would be undone by that
	// write, a Delete could be undone the same way, and two writers of one graph would share its
	// .tmp. Reads take it too: on Windows a file that another handle holds open cannot be renamed
	// over, and a file in the middle of that rename cannot be opened, so a Load overlapping a
	// Save would fail one of the two.
	mu sync.Mutex
	// moveAside renames a damaged canvas to its kept name. It is os.Rename; a test replaces it to
	// make the move fail.
	moveAside func(oldpath, newpath string) error
}

// Open returns a layout store rooted at dir, creating the directory if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir, moveAside: os.Rename}, nil
}

// damagedError is a canvas file that was read but does not hold a canvas. It keeps the decoder's
// message as its own, so Load reports what it always did; RemoveNode tells it apart from a read
// that failed, which says nothing about the file.
type damagedError struct{ err error }

func (e damagedError) Error() string { return e.err.Error() }
func (e damagedError) Unwrap() error { return e.err }

// Save writes the layout for one graph, replacing any previous one. The write is atomic
// (temp file + rename) so a crash mid-write cannot corrupt an existing layout.
func (s *Store) Save(graphID uint64, l *Layout) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(graphID, l)
}

// save is Save without the lock, for callers that already hold it.
func (s *Store) save(graphID uint64, l *Layout) error {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(graphID)
}

// load is Load without the lock, for callers that already hold it.
func (s *Store) load(graphID uint64) (*Layout, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, layoutFileFor(graphID)))
	if err != nil {
		if os.IsNotExist(err) {
			return &Layout{Nodes: map[uint64]Position{}}, nil
		}
		return nil, err
	}
	var l Layout
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, damagedError{err}
	}
	if l.Nodes == nil {
		l.Nodes = map[uint64]Position{}
	}
	return &l, nil
}

// Delete removes a graph's layout file (used when the graph is deleted). A missing file
// is not an error.
func (s *Store) Delete(graphID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.dir, layoutFileFor(graphID)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RemoveNode drops an event from every graph's saved canvas. It is called when the event
// leaves the case.
//
// A layout is canvas membership keyed by node id, and the store never reuses an id, so an
// entry for a deleted event can never name anything again; left in place it is an id every
// reader of that canvas must step over. Snapshots, annotations and findings keep theirs: they
// are keyed by hash, they are the analyst's record, and they reattach if the event is ingested
// again.
//
// A file that is not a canvas is moved aside as canvas-<n>.json.unreadable-<UTC>, kept byte for
// byte, and its kept name returned, so one damaged file does not fail every later delete. Moving
// it loses nothing: no reader could use it, and the graph opens without saved positions either
// way. A file that could not be read is left alone, because a failed read says nothing about the
// file.
//
// Every file is visited even when one fails, so one unreadable canvas does not leave the
// others naming the event; the failures are returned together, each naming its file. A file
// that does not hold the id is not rewritten.
func (s *Store) RemoveNode(id uint64) (setAside []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list saved canvases: %w", err)
	}
	var errs []error
	for _, e := range entries {
		graphID, ok := graphIDFromFile(e)
		if !ok {
			continue
		}
		l, err := s.load(graphID)
		var damaged damagedError
		if errors.As(err, &damaged) {
			kept, moveErr := s.keepAside(e.Name())
			if moveErr != nil {
				errs = append(errs, fmt.Errorf("set aside %s, which is not a canvas (%v): %w", e.Name(), err, moveErr))
				continue
			}
			setAside = append(setAside, kept)
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", e.Name(), err))
			continue
		}
		if _, held := l.Nodes[id]; !held {
			continue
		}
		delete(l.Nodes, id)
		if err := s.save(graphID, l); err != nil {
			errs = append(errs, fmt.Errorf("write %s: %w", e.Name(), err))
		}
	}
	return setAside, errors.Join(errs...)
}

// keepAside moves a damaged canvas to a kept name no earlier kept file holds, and returns that
// name. Called under mu.
func (s *Store) keepAside(name string) (string, error) {
	base := name + consts.CanvasKeptInfix + time.Now().UTC().Format(consts.CanvasKeptStamp)
	for i := range keptNameTries {
		kept := base
		if i > 0 {
			kept = fmt.Sprintf("%s-%d", base, i)
		}
		// 🔒 Never onto an existing name: os.Rename replaces its target on every platform, which
		// would destroy a canvas kept earlier to keep this one.
		if _, err := os.Lstat(filepath.Join(s.dir, kept)); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := s.moveAside(filepath.Join(s.dir, name), filepath.Join(s.dir, kept)); err != nil {
			return "", err
		}
		return kept, nil
	}
	return "", fmt.Errorf("every kept name for %s from %s to -%d is taken", name, base, keptNameTries-1)
}

// keptNameTries bounds the numbered kept names tried for one canvas in one second.
const keptNameTries = 1000

// Damaged is a per-graph canvas file that is there but does not hold a canvas.
type Damaged struct {
	GraphID uint64
	File    string
	Reason  string
}

// Findings is what Survey found in a layout folder.
type Findings struct {
	// Damaged lists the canvases that cannot be read as one, by file name.
	Damaged []Damaged
	// Kept lists the damaged canvases RemoveNode already set aside, by file name.
	Kept []string
}

// Survey lists the canvases in dir that the integrity check reports: each per-graph canvas that is
// not a canvas, and each one already set aside. A missing folder holds neither. A file that cannot
// be read is returned as an error naming it, not as damage, because a failed read says nothing
// about the file; the walk carries on past it.
//
// It reads without the store's lock, because the check is given a folder, not a Store. A read that
// meets a Save's rename fails as a read, never as damage.
func Survey(dir string) (Findings, error) {
	var out Findings
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("list saved canvases: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if isKeptCanvas(e) {
			out.Kept = append(out.Kept, e.Name())
			continue
		}
		graphID, ok := graphIDFromFile(e)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", e.Name(), err))
			continue
		}
		var l Layout
		if err := json.Unmarshal(data, &l); err != nil {
			out.Damaged = append(out.Damaged, Damaged{GraphID: graphID, File: e.Name(), Reason: err.Error()})
		}
	}
	return out, errors.Join(errs...)
}

// isKeptCanvas reports a file keepAside wrote: canvas-<n>.json.unreadable-<anything>.
func isKeptCanvas(e os.DirEntry) bool {
	if e.IsDir() {
		return false
	}
	canvas, _, ok := strings.Cut(e.Name(), consts.CanvasKeptInfix)
	if !ok {
		return false
	}
	rest, ok := strings.CutPrefix(canvas, "canvas-")
	if !ok {
		return false
	}
	rest, ok = strings.CutSuffix(rest, ".json")
	if !ok {
		return false
	}
	id, err := strconv.ParseUint(rest, 10, 64)
	return err == nil && layoutFileFor(id) == canvas
}

// graphIDFromFile reads the graph id out of a per-graph layout file's name, and reports false
// for anything else in the directory: the legacy canvas.json, a .tmp left by an interrupted
// Save, a directory, or a name layoutFileFor would not have written.
func graphIDFromFile(e os.DirEntry) (uint64, bool) {
	if e.IsDir() {
		return 0, false
	}
	rest, ok := strings.CutPrefix(e.Name(), "canvas-")
	if !ok {
		return 0, false
	}
	rest, ok = strings.CutSuffix(rest, ".json")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(rest, 10, 64)
	return id, err == nil && layoutFileFor(id) == e.Name()
}

// MigrateLegacy folds a pre-P15 single-graph layout (canvas.json) into the given graph's
// per-graph file, once. It is a no-op if there is no legacy file or the target already
// exists. Returns true if a migration was performed.
func (s *Store) MigrateLegacy(graphID uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
