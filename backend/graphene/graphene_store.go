package graphene

import (
	"errors"
	"sync"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/store"
)

// Store is rohy's persistence facade over a graphene graph. It is the only
// type the rest of the backend uses to read and write events and relations.
type Store struct {
	g   *graphene.Graph
	dir string // empty for the in-memory store
	// inMemory selects the in-memory backend when the store is first opened.
	inMemory bool
	// openOnce defers the actual open until first use, so the application can show its
	// window before paying for a WAL replay. It also provides the happens-before edge that
	// makes reading g lock-free once ensure() has returned.
	openOnce sync.Once
	openErr  error
	// order caches the id ordering of the most recent event query so that paging through
	// a filter does not rescan and re-sort the whole matching set for every page. Any
	// write invalidates it (see bumpVersion).
	order orderCache
}

// Open opens (creating if necessary) a disk-backed store rooted at dir, eagerly. The WAL
// is replayed automatically on restart. Call Compact after a bulk ingest.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := s.ensure(); err != nil {
		return nil, err
	}
	return s, nil
}

// OpenLazy returns a disk-backed store that opens on FIRST USE rather than immediately.
//
// Opening replays the WAL and loads the graph, which on a large case is the slowest thing
// the app does at startup — and doing it before the window exists means the user stares at
// nothing. Deferring it lets the UI appear first and report progress while the store warms
// in the background (see Warm). Any call arriving before the store is open simply blocks
// until it is, so laziness can never surface as a nil dereference or a half-open read.
func OpenLazy(dir string) *Store {
	return &Store{dir: dir}
}

// OpenInMemory returns an in-memory store, suitable for development and tests.
func OpenInMemory() *Store {
	s := &Store{inMemory: true}
	_ = s.ensure() // cannot fail for the in-memory backend
	return s
}

// ensure performs the one-time open. Every accessor funnels through it, so the store
// behaves identically whether it was opened eagerly or lazily.
func (s *Store) ensure() error {
	s.openOnce.Do(func() {
		if s.inMemory {
			s.g = graphene.NewInMemory()
			return
		}
		g, err := graphene.Open(s.dir)
		if err != nil {
			s.openErr = err
			return
		}
		s.g = g
	})
	return s.openErr
}

// Warm opens the store now, so the app can pay that cost deliberately (on a background
// goroutine, with progress reported) rather than on whichever user action happens to touch
// the store first. Safe to call repeatedly and concurrently.
func (s *Store) Warm() error {
	return s.ensure()
}

// graph returns the opened graph, opening it if this is the first access.
func (s *Store) graph() (*graphene.Graph, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	return s.g, nil
}

// nodeEventType is the node label events are stored under; a tiny helper so the ordering
// code does not need to import consts for a single value.
func nodeEventType() store.NodeType {
	return consts.NodeEvent
}

// Close releases the underlying store resources. Closing a store that was never opened is
// a no-op — nothing was acquired to release.
func (s *Store) Close() error {
	if s.g == nil {
		return nil
	}
	return s.g.Close()
}

// Compact merges the delta layer and truncates the WAL (disk store only; no-op
// for in-memory). Call after a large ingest completes.
func (s *Store) Compact() error {
	g, err := s.graph()
	if err != nil {
		return err
	}
	return g.Compact()
}

// InsertEvents persists a batch of events and registers their secondary indexes.
// It mutates each event's ID with the graphene-assigned node id and returns the
// assigned ids in input order. Batches are kept small by the caller so that no
// single write becomes a large transaction.
func (s *Store) InsertEvents(events []*Event) ([]uint64, error) {
	defer s.bumpVersion() // a new event changes what the events list matches
	if len(events) == 0 {
		return nil, nil
	}
	g, err := s.graph()
	if err != nil {
		return nil, err
	}

	nodes := make([]*store.Node, len(events))
	for i, e := range events {
		n, err := e.toNode()
		if err != nil {
			return nil, err
		}
		nodes[i] = n
	}

	ids, err := g.AddNodes(nodes)
	if err != nil {
		return nil, err
	}

	out := make([]uint64, len(ids))
	for i, id := range ids {
		events[i].ID = uint64(id)
		out[i] = uint64(id)
		if err := g.IndexNodeProperties(id, events[i].indexValues()); err != nil {
			return out, err
		}
	}
	return out, nil
}

// InsertRelation persists a single mapped relation and registers its indexes,
// stamping the relation's ID. Src and Dst nodes must already exist.
func (s *Store) InsertRelation(r *Relation) (uint64, error) {
	defer s.bumpVersion() // relations feed the relation-state filter
	g, err := s.graph()
	if err != nil {
		return 0, err
	}
	edge, err := r.toEdge()
	if err != nil {
		return 0, err
	}
	id, err := g.AddEdge(edge)
	if err != nil {
		return 0, err
	}
	r.ID = uint64(id)
	if err := g.IndexEdgeProperties(id, r.indexValues()); err != nil {
		return uint64(id), err
	}
	return uint64(id), nil
}

// UpdateRelation replaces a relation's type/label/confidence in place (its endpoints
// are immutable — graphene ignores Src/Dst on update). The secondary index is
// refreshed for the type; graphene does not auto-update the index on edit, so a stale
// prior value can linger until the edge is deleted, which is harmless here because
// relations are read by edge type, not by the relation_type property.
func (s *Store) UpdateRelation(r *Relation) error {
	defer s.bumpVersion()
	g, err := s.graph()
	if err != nil {
		return err
	}
	edge, err := r.toEdge()
	if err != nil {
		return err
	}
	edge.ID = store.EdgeID(r.ID)
	if err := g.UpdateEdge(edge); err != nil {
		return err
	}
	return g.IndexEdgeProperties(edge.ID, r.indexValues())
}

// DeleteRelation removes a single mapped relation (and purges its index entries).
// A missing id returns *store.ErrNotFound.
func (s *Store) DeleteRelation(id uint64) error {
	defer s.bumpVersion()
	g, err := s.graph()
	if err != nil {
		return err
	}
	return g.DeleteEdge(store.EdgeID(id))
}

// DeleteEvent removes an event node and cascades to every relation incident to it, so
// the graph never retains an edge pointing at a missing event. A missing id returns
// *store.ErrNotFound.
func (s *Store) DeleteEvent(id uint64) error {
	defer s.bumpVersion()
	g, err := s.graph()
	if err != nil {
		return err
	}
	return g.DeleteNode(store.NodeID(id))
}

// IncrementDedupCounts adds each delta to the deduplication_count of an existing
// event node. It is a read-modify-write per node, applied over a caller-sized map so
// no single write becomes a large transaction (the ingestion sink flushes these in
// bounded batches). Non-positive deltas and missing ids are skipped; the secondary
// index is untouched because deduplication_count is not indexed.
func (s *Store) IncrementDedupCounts(deltas map[uint64]int) error {
	defer s.bumpVersion() // occurrence counts drive the min-occurrences filter
	g, err := s.graph()
	if err != nil {
		return err
	}
	for id, delta := range deltas {
		if delta <= 0 {
			continue
		}
		e, err := s.GetEvent(id)
		if err != nil {
			return err
		}
		e.DeduplicationCount += delta
		n, err := e.toNode()
		if err != nil {
			return err
		}
		n.ID = store.NodeID(id)
		if err := g.UpdateNode(n); err != nil {
			return err
		}
	}
	return nil
}

// MigrateRelationsToGraph assigns graphID to every relation that has no graph yet
// (GraphID == 0), i.e. relations created before multiple-graphs existed. It is
// idempotent: a second call finds nothing to migrate. Returns the number migrated.
func (s *Store) MigrateRelationsToGraph(graphID uint64) (int, error) {
	rels, err := s.GetRelations()
	if err != nil {
		return 0, err
	}
	migrated := 0
	for _, r := range rels {
		if r.GraphID != 0 {
			continue
		}
		r.GraphID = graphID
		if err := s.UpdateRelation(r); err != nil {
			return migrated, err
		}
		migrated++
	}
	return migrated, nil
}

// DeleteGraphRelations removes every relation belonging to a graph (used when a graph
// is deleted). Event nodes are never touched — only the graph's edges. Returns the
// number of relations deleted.
func (s *Store) DeleteGraphRelations(graphID uint64) (int, error) {
	rels, err := s.RelationsByGraph(graphID)
	if err != nil {
		return 0, err
	}
	for _, r := range rels {
		if err := s.DeleteRelation(r.ID); err != nil {
			var nf *store.ErrNotFound
			if errors.As(err, &nf) {
				continue
			}
			return 0, err
		}
	}
	return len(rels), nil
}

// FindEventIDByHash returns the node id of an event whose normalized hash matches,
// or (0, false) if none exists. Used for idempotent resume so re-ingested events
// are not duplicated.
func (s *Store) FindEventIDByHash(hashNormalized string) (uint64, bool, error) {
	g, err := s.graph()
	if err != nil {
		return 0, false, err
	}
	ids, err := g.NodesByProperty(consts.PropHashNormalized, []byte(hashNormalized))
	if err != nil {
		return 0, false, err
	}
	if len(ids) == 0 {
		return 0, false, nil
	}
	return uint64(ids[0]), true, nil
}

// Stats returns high-level node/edge counts.
func (s *Store) Stats() (nodes uint64, edges uint64, err error) {
	g, err := s.graph()
	if err != nil {
		return 0, 0, err
	}
	st, err := g.Stats()
	if err != nil {
		return 0, 0, err
	}
	return st.NodeCount, st.EdgeCount, nil
}
