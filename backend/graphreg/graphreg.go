// Package graphreg is the registry of named graphs in a case (multiple-graphs, P15).
// It owns only graph metadata (id, name, description, timestamps) and which graph is
// active, persisted as a JSON sidecar. Relations (scoped by graph_id) and per-graph
// canvas layout live in the graphene store and the layout package respectively; this
// package never touches event or relation data.
package graphreg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"rohy/backend/consts"
)

const registryFile = "registry.json"

// ErrLastGraph is returned when deleting the only remaining graph is attempted; a case
// must always retain at least one graph.
var ErrLastGraph = errors.New("cannot delete the last remaining graph")

// ErrNotFound is returned when an operation targets a graph id that does not exist.
var ErrNotFound = errors.New("graph not found")

// Graph is the metadata of one named graph. RuleID binds a graph to the correlation rule
// that produces it (P6, "one rule = one graph"); it is empty for manually created graphs
// and survives renaming the graph, so a rule rebuild always finds its own graph.
type Graph struct {
	ID          uint64    `json:"id"`
	RuleID      string    `json:"rule_id,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// state is the on-disk registry document.
type state struct {
	NextID   uint64   `json:"next_id"`
	ActiveID uint64   `json:"active_id"`
	Graphs   []*Graph `json:"graphs"`
}

// Store reads and writes the graph registry under a directory.
type Store struct {
	dir string
	mu  sync.Mutex
	s   state
}

// Open loads (or initializes) the registry rooted at dir, creating the directory if
// needed. Id assignment starts at 1 so the first graph created is consts.DefaultGraphID.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	st := &Store{dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, registryFile))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else if err := json.Unmarshal(data, &st.s); err != nil {
		return nil, err
	}
	if st.s.NextID == 0 {
		st.s.NextID = consts.DefaultGraphID
	}
	return st, nil
}

// draft returns a deep copy of the registry for a mutator to change. Nothing a mutator does to
// it is seen until commit writes it.
func (s *Store) draft() state {
	d := s.s
	d.Graphs = make([]*Graph, len(s.s.Graphs))
	for i, g := range s.s.Graphs {
		d.Graphs[i] = clone(g)
	}
	return d
}

// commit writes next and, only once the write succeeded, makes it the registry in memory.
// 🔒 A failed write must change nothing: memory that ran ahead of the file would show a graph,
// a name or an active graph that the next start takes away.
func (s *Store) commit(next state) error {
	if err := s.persist(next); err != nil {
		return err
	}
	s.s = next
	return nil
}

// persist atomically writes a registry document (temp file + rename).
func (s *Store) persist(st state) error {
	data, err := json.MarshalIndent(&st, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, registryFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, registryFile))
}

// clone returns a defensive copy so callers cannot mutate internal state.
func clone(g *Graph) *Graph {
	c := *g
	return &c
}

// findIndex returns the slice index of a graph id, or -1.
func (s *Store) findIndex(id uint64) int {
	for i, g := range s.s.Graphs {
		if g.ID == id {
			return i
		}
	}
	return -1
}

// List returns all graphs (copies), in creation order.
func (s *Store) List() []*Graph {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Graph, len(s.s.Graphs))
	for i, g := range s.s.Graphs {
		out[i] = clone(g)
	}
	return out
}

// Active returns the active graph id (0 if none).
func (s *Store) Active() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.s.ActiveID
}

// SetActive marks a graph active. The id must exist.
func (s *Store) SetActive(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findIndex(id) < 0 {
		return ErrNotFound
	}
	next := s.draft()
	next.ActiveID = id
	return s.commit(next)
}

// EnsureDefault guarantees at least one graph exists: if the registry is empty it
// creates a graph named `name` (which receives consts.DefaultGraphID) and marks it
// active. It returns the active graph. Safe to call on every startup (idempotent).
func (s *Store) EnsureDefault(name string, now time.Time) (*Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.s.Graphs) == 0 {
		next := s.draft()
		g := &Graph{ID: next.NextID, Name: name, CreatedAt: now, UpdatedAt: now}
		next.NextID++
		next.Graphs = append(next.Graphs, g)
		next.ActiveID = g.ID
		if err := s.commit(next); err != nil {
			return nil, err
		}
		return clone(g), nil
	}
	if s.s.ActiveID == 0 {
		next := s.draft()
		next.ActiveID = next.Graphs[0].ID
		if err := s.commit(next); err != nil {
			return nil, err
		}
	}
	idx := s.findIndex(s.s.ActiveID)
	return clone(s.s.Graphs[idx]), nil
}

// Create adds a new graph and makes it active. Returns the created graph.
func (s *Store) Create(name, description string, now time.Time) (*Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.draft()
	g := &Graph{ID: next.NextID, Name: name, Description: description, CreatedAt: now, UpdatedAt: now}
	next.NextID++
	next.Graphs = append(next.Graphs, g)
	next.ActiveID = g.ID
	if err := s.commit(next); err != nil {
		return nil, err
	}
	return clone(g), nil
}

// EnsureForRule returns the graph bound to ruleID, creating it if it does not exist yet.
// Unlike Create it deliberately does NOT change the active graph: a rule run populates
// graphs in the background and must never hijack the one the user is looking at (P6).
// Binding is by rule id, so renaming a rule's graph never orphans it; a pre-existing
// unbound graph with the same name is adopted once, which keeps a re-run idempotent for
// graphs created before rule binding existed.
func (s *Store) EnsureForRule(ruleID, name, description string, now time.Time) (*Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, g := range s.s.Graphs {
		if g.RuleID != "" && g.RuleID == ruleID {
			return clone(g), nil
		}
	}
	next := s.draft()
	for _, g := range next.Graphs {
		if g.RuleID == "" && g.Name == name {
			g.RuleID = ruleID
			g.UpdatedAt = now
			if err := s.commit(next); err != nil {
				return nil, err
			}
			return clone(g), nil
		}
	}

	g := &Graph{ID: next.NextID, RuleID: ruleID, Name: name, Description: description, CreatedAt: now, UpdatedAt: now}
	next.NextID++
	next.Graphs = append(next.Graphs, g)
	if err := s.commit(next); err != nil {
		return nil, err
	}
	return clone(g), nil
}

// Rename updates a graph's name and description. The id must exist.
func (s *Store) Rename(id uint64, name, description string, now time.Time) (*Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return nil, ErrNotFound
	}
	next := s.draft()
	next.Graphs[idx].Name = name
	next.Graphs[idx].Description = description
	next.Graphs[idx].UpdatedAt = now
	if err := s.commit(next); err != nil {
		return nil, err
	}
	return clone(next.Graphs[idx]), nil
}

// Delete removes a graph from the registry. It refuses to remove the last remaining
// graph. If the deleted graph was active, the first remaining graph becomes active.
// Cascading its relations/layout is the caller's responsibility (this package holds no
// graph data).
func (s *Store) Delete(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return ErrNotFound
	}
	if len(s.s.Graphs) == 1 {
		return ErrLastGraph
	}
	next := s.draft()
	next.Graphs = append(next.Graphs[:idx], next.Graphs[idx+1:]...)
	if next.ActiveID == id {
		next.ActiveID = next.Graphs[0].ID
	}
	return s.commit(next)
}
