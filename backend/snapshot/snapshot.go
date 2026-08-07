// Package snapshot records what a graph looked like at a moment, and works out what can honestly
// be put back later (P29).
//
// It is a sidecar for the same reason findings are: a snapshot is a record of the analyst's
// working state, not of the evidence, and evidence must read back exactly as it was ingested.
// Plain readable JSON, one file per snapshot, so the record outlives this program.
//
// 🔒 The one decision the whole package turns on: endpoints are recorded by hash_normalized as
// well as by node id. Node ids are assignment-order — a re-ingest hands the same id to a
// different event — so a restore matching on id alone would silently move a saved graph onto
// unrelated records. That failure would be invisible: the canvas would look completely normal
// and every claim on it would be wrong.
//
// This package stores and PLANS. It never writes to the graph store; applying a plan is the
// caller's job, and the plan is shown to the analyst first. Nothing restores silently.
package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"rohy/backend/consts"
)

// ErrNotFound is returned for a snapshot id that does not exist.
var ErrNotFound = errors.New("snapshot not found")

// Node is one canvas node as it was: its id, its content identity, and where it sat.
type Node struct {
	ID uint64 `json:"id"`
	// Hash is the event's hash_normalized — its content identity, and the only field a restore
	// trusts to say "this is the same event".
	Hash string  `json:"hash"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	// Descriptor is a human-readable summary of the event as it was, so a snapshot whose events
	// have left the case still describes what it held. Same reasoning as findings.Descriptor: an
	// orphaned bare hash tells a reader nothing.
	Descriptor string `json:"descriptor,omitempty"`
}

// Relation is one edge as it was, with its endpoints recorded by hash as well as by id.
type Relation struct {
	ID           uint64 `json:"id"`
	FromHash     string `json:"from_hash"`
	ToHash       string `json:"to_hash"`
	RelationType string `json:"relation_type"`
	Label        string `json:"relation_label,omitempty"`
	CreatedBy    string `json:"created_by,omitempty"`
	// Provenance, copied verbatim. It is NOT re-applied on a restore — see Plan — but it is kept
	// so the snapshot describes what the edge was, which is the point of taking one.
	RuleID    string   `json:"rule_id,omitempty"`
	Algorithm string   `json:"algorithm,omitempty"`
	MatchID   string   `json:"match_id,omitempty"`
	StepIndex int      `json:"step_index,omitempty"`
	Basis     []string `json:"basis,omitempty"`
}

// Viewport is the canvas pan/zoom, mirroring the layout sidecar's shape.
type Viewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

// Document is one snapshot file.
type Document struct {
	SnapshotVersion int    `json:"snapshot_version"`
	ID              string `json:"id"`
	// Label is the analyst's own name for this snapshot. Optional: an unnamed snapshot is still
	// identified by when it was taken.
	Label     string    `json:"label,omitempty"`
	GraphID   uint64    `json:"graph_id"`
	GraphName string    `json:"graph_name,omitempty"`
	RuleID    string    `json:"rule_id,omitempty"`
	TakenAt   time.Time `json:"taken_at"`
	// StoreVersion and AppVersion record the state this was taken against, so a restore that
	// finds nothing can say whether the case changed underneath it.
	StoreVersion uint64     `json:"store_version,omitempty"`
	AppVersion   string     `json:"app_version,omitempty"`
	Viewport     Viewport   `json:"viewport"`
	Nodes        []Node     `json:"nodes"`
	Relations    []Relation `json:"relations"`
}

// Summary is a snapshot without its contents, for the list.
type Summary struct {
	ID         string    `json:"id"`
	Label      string    `json:"label,omitempty"`
	GraphID    uint64    `json:"graph_id"`
	GraphName  string    `json:"graph_name,omitempty"`
	TakenAt    time.Time `json:"taken_at"`
	Nodes      int       `json:"nodes"`
	Relations  int       `json:"relations"`
	AppVersion string    `json:"app_version,omitempty"`
}

// Store is the on-disk collection of snapshots, one directory per graph.
type Store struct {
	mu  sync.Mutex
	dir string
}

// Open roots a snapshot store at dir, creating it if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) graphDir(graphID uint64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d", graphID))
}

// NewID derives a snapshot id from the instant it was taken. It is derived rather than random so
// the file name sorts chronologically in a directory listing, which is how someone reading the
// case folder by hand will encounter these.
func NewID(at time.Time) string {
	return "snap-" + at.UTC().Format("20060102-150405.000")
}

// Save writes a snapshot. It refuses rather than evicting once a graph is at the cap: a snapshot
// is something an analyst deliberately took, and quietly deleting one to make room destroys work
// without asking.
func (s *Store) Save(doc *Document) error {
	if doc == nil {
		return errors.New("nil snapshot")
	}
	if len(strings.TrimSpace(doc.Label)) > consts.MaxSnapshotLabelLen {
		return fmt.Errorf(consts.MsgSnapshotLabelLong, consts.MaxSnapshotLabelLen)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.listLocked(doc.GraphID)
	if err != nil {
		return err
	}
	if len(existing) >= consts.MaxSnapshotsPerGraph {
		return fmt.Errorf(consts.MsgSnapshotLimit, consts.MaxSnapshotsPerGraph)
	}

	doc.SnapshotVersion = consts.SnapshotVersion
	doc.Label = strings.TrimSpace(doc.Label)
	if doc.Nodes == nil {
		doc.Nodes = []Node{}
	}
	if doc.Relations == nil {
		doc.Relations = []Relation{}
	}

	dir := s.graphDir(doc.GraphID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// 🔒 Never overwrite. Two snapshots taken inside the same millisecond share a base id, and on a
	// fast machine that is one double-click apart — writing over the first would silently destroy
	// something the analyst deliberately took, which is the one thing this package must not do.
	// (Found by CI on Linux, where the clock and the filesystem are quick enough to hit it every
	// time; a Windows dev machine never reproduced it.)
	//
	// Disambiguating keeps the id sortable — `…-141133.000` then `…-141133.000-2` — so the
	// directory still reads chronologically by eye.
	path, err := s.freeIDLocked(dir, doc)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// freeIDLocked settles on an id no file already uses, updating doc.ID to match, and returns the
// path to write. The caller holds the lock, so the check and the later write cannot race.
//
// The bound exists so a pathological state cannot spin: at the snapshot cap there are only ever
// a few dozen files, so more than that many collisions on one millisecond means something else is
// wrong and the caller should hear about it rather than loop.
func (s *Store) freeIDLocked(dir string, doc *Document) (string, error) {
	base := doc.ID
	for n := 1; n <= consts.MaxSnapshotsPerGraph+1; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		path := filepath.Join(dir, id+".json")
		if _, err := os.Stat(path); err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}
			doc.ID = id
			return path, nil
		}
	}
	return "", fmt.Errorf("could not find a free snapshot id for %q", base)
}

// List returns a graph's snapshots, newest first.
func (s *Store) List(graphID uint64) ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(graphID)
}

func (s *Store) listLocked(graphID uint64) ([]Summary, error) {
	entries, err := os.ReadDir(s.graphDir(graphID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // a graph with no snapshots is not an error
		}
		return nil, err
	}
	out := make([]Summary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		doc, err := readDoc(filepath.Join(s.graphDir(graphID), e.Name()))
		if err != nil {
			// One unreadable file must not hide the rest. It is skipped here and surfaces when
			// the analyst tries to open it, where there is somewhere to put the message.
			continue
		}
		out = append(out, Summary{
			ID: doc.ID, Label: doc.Label, GraphID: doc.GraphID, GraphName: doc.GraphName,
			TakenAt: doc.TakenAt, Nodes: len(doc.Nodes), Relations: len(doc.Relations),
			AppVersion: doc.AppVersion,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].TakenAt.Equal(out[j].TakenAt) {
			return out[i].TakenAt.After(out[j].TakenAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// Get reads one snapshot.
func (s *Store) Get(graphID uint64, id string) (*Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := readDoc(filepath.Join(s.graphDir(graphID), safeID(id)+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: "+consts.MsgSnapshotNotFound, ErrNotFound, id, graphID)
		}
		return nil, err
	}
	if doc.SnapshotVersion > consts.SnapshotVersion {
		return nil, fmt.Errorf(consts.MsgSnapshotBadVersion, id, doc.SnapshotVersion)
	}
	return doc, nil
}

// Delete removes one snapshot. Deleting one that is already gone is success.
func (s *Store) Delete(graphID uint64, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.graphDir(graphID), safeID(id)+".json"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// DeleteGraph removes every snapshot of a graph, for a graph that has itself been deleted.
// Leaving them would be dead files nothing can list and nothing can restore — the graph id they
// name no longer exists.
func (s *Store) DeleteGraph(graphID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(s.graphDir(graphID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readDoc(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// safeID strips anything that could escape the snapshot directory. Ids are generated by NewID,
// but this is a filesystem path built from a value that crosses the API boundary, so it is
// sanitized where it is used rather than trusted because of where it usually comes from.
func safeID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, "\\", "")
	id = strings.ReplaceAll(id, "/", "")
	id = strings.ReplaceAll(id, "..", "")
	return id
}
