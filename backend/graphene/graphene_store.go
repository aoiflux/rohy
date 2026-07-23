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

// NOTE — why no ordered property is declared here.
//
// Declaring the timestamp key ordered is the textbook move: rohy runs range filters on it,
// and a declared key answers those by binary search instead of by scanning every entry
// registered under it. It was implemented, measured, and removed again, because on this
// workload it is a large net loss:
//
//	timestamp declared     100k events: open ~8.0 s
//	timestamp undeclared   100k events: open ~0.8 s
//
// A declaration is runtime state that does not survive a reopen, so it has to absorb every
// already-registered entry on EVERY open — and for a near-unique key like a timestamp that
// absorption dominates startup and grows faster than linearly. The open cost is paid by
// every user on every launch; the query it accelerates is not.
//
// And it accelerates almost nothing here. Measured on a 20k-event store, declared versus
// not: narrow range 1.63–1.85 ms vs 1.71–1.91 ms, wide range 10.6–12.4 ms vs 10.1–12.4 ms,
// count over a range ~0.55 µs either way. Range queries in rohy are dominated by decoding
// the matched event records — RawXML is kilobytes each — not by locating candidate ids, so
// making the lookup asymptotically better moves a cost that was never the bottleneck.
//
// The upstream guidance is explicit that this is a measurement, not a rule ("measure before
// indexing... intuition about selectivity usually is not [exact]"). This is that
// measurement. If event records are ever hydrated more cheaply, or ranges are run over a
// far larger store, re-measure before re-declaring — and measure OPEN, not just the query.

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

// VerifyIndexes cross-checks the store's indexes against the records they describe and
// reports the first inconsistency found.
//
// It is a RECOVERY and TEST tool, not a startup step. Verification is proportional to the
// whole store, and a structurally damaged index section is already rejected while the file
// is parsed, so running it on every open would tax every launch to re-prove something that
// is almost always true. The trigger points are: the test suite (where it is the assertion
// that a write path left the index truthful), and an explicit user-initiated check on a
// store suspected of damage.
//
// What it CANNOT check is whether an indexed value still matches the record's properties —
// those values are written by this package in its own encoding and are opaque to the
// storage layer. Guarding that is what the update path's atomic indexed writes are for.
func (s *Store) VerifyIndexes() error {
	g, err := s.graph()
	if err != nil {
		return err
	}
	return g.VerifyIndexes()
}

// RebuildIndexes recomputes everything the store can derive from its records — label
// postings and adjacency — and drops property entries whose entity is gone.
//
// It repairs STRUCTURE, not CONTENT: it cannot restore a property entry whose value was
// never registered, because the value is this package's encoding of a field the storage
// layer cannot read. A store that lost property entries (for example, a crash between a
// batch commit and its index registration) is repaired by re-running the work that
// registers them, not by this call.
func (s *Store) RebuildIndexes() error {
	g, err := s.graph()
	if err != nil {
		return err
	}
	return g.RebuildIndexes()
}

// RepairRelationIndex re-registers index entries for relations whose entries are missing,
// and returns how many it repaired.
//
// This is the recovery for the one gap every batched write path has: a record commit and
// its index registration are separate steps, because registration is not part of a commit.
// A crash in between leaves an edge that exists but is invisible to every graph-scoped
// query, since those resolve through the graph_id index — while adjacency still shows it,
// because adjacency reads incident edges directly. The result is an event displaying a
// relation that belongs to no graph and that clearing the graph cannot remove.
//
// Structural verification does not catch this. A property entry that was never written is
// not damage to the index's structure — the edge and its adjacency agree — so
// VerifyIndexes passes and RebuildIndexes cannot help, because neither can re-derive a
// caller-encoded value it never saw. Repair therefore has to come from this package, which
// is the only thing that knows how a relation encodes its own index entries.
//
// It works by walking edges, which does not depend on the index being correct, and
// comparing what it finds against what the index reports for each graph. It is idempotent:
// on a healthy store it registers nothing and returns zero, which is what makes it safe to
// offer as a maintenance action rather than a last resort.
func (s *Store) RepairRelationIndex() (int, error) {
	g, err := s.graph()
	if err != nil {
		return 0, err
	}

	// Walk every relation edge directly. This is the authoritative set: it is derived from
	// adjacency, not from the property index whose truthfulness is in question.
	edgeIDs, err := g.EdgesByType(consts.EdgeRelation)
	if err != nil {
		return 0, err
	}
	if len(edgeIDs) == 0 {
		return 0, nil
	}

	byGraph := make(map[uint64][]*Relation)
	for _, id := range edgeIDs {
		ed, err := g.GetEdge(id)
		if err != nil {
			return 0, err
		}
		r, err := relationFromEdge(ed)
		if err != nil {
			return 0, err
		}
		byGraph[r.GraphID] = append(byGraph[r.GraphID], r)
	}

	repaired := 0
	for graphID, rels := range byGraph {
		// What the index currently believes belongs to this graph.
		indexed, err := g.EdgesWithProperties(map[string][]byte{consts.PropGraphID: graphIDValue(graphID)})
		if err != nil {
			return repaired, err
		}
		known := make(map[uint64]bool, len(indexed))
		for _, ed := range indexed {
			known[uint64(ed.ID)] = true
		}
		for _, r := range rels {
			if known[r.ID] {
				continue
			}
			if err := g.IndexEdgeProperties(store.EdgeID(r.ID), r.indexValues()); err != nil {
				return repaired, err
			}
			repaired++
		}
	}
	if repaired > 0 {
		s.bumpVersion() // relations became visible; any cached ordering is now stale
	}
	return repaired, nil
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

// InsertRelations persists a batch of relations in ONE commit and registers their
// indexes, stamping each relation's ID. Src and Dst nodes must already exist.
//
// This is the write path a rule-generated graph takes, where relations arrive by the
// thousand. Inserting them one at a time costs one durable write per edge — the WAL is
// what a crash-safe write buys, and it is not tunable — so the batch is the whole
// difference between a graph build that is write-bound per edge and one that pays a
// single framed commit for the lot. The batch is atomic: on error nothing is applied and
// no ids are returned, so a failed build cannot leave a half-built graph behind.
//
// Indexing is deliberately a separate pass after the commit: index registration is not
// part of a batch commit, so the edges must exist before their entries are registered.
// A crash in between leaves edges that are present but not yet findable by graph_id — the
// build is idempotent and re-running it replaces the graph wholesale, which is the
// recovery path.
func (s *Store) InsertRelations(rels []*Relation) ([]uint64, error) {
	defer s.bumpVersion()
	if len(rels) == 0 {
		return nil, nil
	}
	g, err := s.graph()
	if err != nil {
		return nil, err
	}

	edges := make([]*store.Edge, len(rels))
	for i, r := range rels {
		ed, err := r.toEdge()
		if err != nil {
			return nil, err
		}
		edges[i] = ed
	}

	ids, err := g.AddEdges(edges)
	if err != nil {
		return nil, err
	}

	out := make([]uint64, len(ids))
	for i, id := range ids {
		rels[i].ID = uint64(id)
		out[i] = uint64(id)
		// One call per relation rather than one per key: registration takes a lock per
		// entry, so the per-entity form is the cheaper shape.
		if err := g.IndexEdgeProperties(id, rels[i].indexValues()); err != nil {
			return out, err
		}
	}
	return out, nil
}

// UpdateRelation replaces a relation's type/label/confidence in place (its endpoints
// are immutable — graphene ignores Src/Dst on update).
//
// The record and its index entries are replaced in ONE call. The plain update leaves the
// index alone under the default reindex policy, so an edited relation would keep matching
// its previous indexed values — and graph_id is one of those values, so a relation moved
// between graphs would still be found under the old graph. There is no window here in
// which the record and its index disagree.
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
	return g.UpdateEdgeIndexed(edge, r.indexValues())
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
	// Read the whole set in one batched fetch rather than one point lookup per id, then
	// commit every increment together. Applied one at a time this was a durable write per
	// event, which on a duplicate-heavy ingest is the dominant cost of the dedup pass.
	//
	// Iteration order over the map is deliberately not fixed: these are independent
	// per-node increments, so the order they are buffered in cannot change the result.
	ids := make([]store.NodeID, 0, len(deltas))
	for id, delta := range deltas {
		if delta > 0 {
			ids = append(ids, store.NodeID(id))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	nodes, missing, err := g.GetNodes(ids)
	if err != nil {
		return err
	}
	// A missing id is skipped rather than fatal: the event it counted duplicates for is
	// already gone, so there is nothing to increment.
	_ = missing

	tx := g.Begin()
	for _, n := range nodes {
		e, err := eventFromNode(n)
		if err != nil {
			return err
		}
		e.DeduplicationCount += deltas[uint64(n.ID)]
		updated, err := e.toNode()
		if err != nil {
			return err
		}
		updated.ID = n.ID
		// deduplication_count is not an indexed key, so no index entry needs replacing
		// alongside the record — which is what lets these updates live in a transaction,
		// since index registration is not part of a commit.
		tx.UpdateNode(updated)
	}
	return tx.Commit()
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
	defer s.bumpVersion()
	rels, err := s.RelationsByGraph(graphID)
	if err != nil {
		return 0, err
	}
	if len(rels) == 0 {
		return 0, nil
	}
	g, err := s.graph()
	if err != nil {
		return 0, err
	}

	// Clearing a graph is one logical act, so it commits once. Deleting edge by edge
	// paid a durable write each time and, worse, could stop half way and leave the graph
	// partly cleared — which the idempotent rebuild would then treat as the previous
	// result. A transaction either removes the whole graph's edges or removes none.
	// Index entries are cleaned up as part of the delete, so no separate purge is needed.
	tx := g.Begin()
	for _, r := range rels {
		tx.DeleteEdge(store.EdgeID(r.ID))
	}
	if err := tx.Commit(); err != nil {
		// A relation that is already gone is not a failure: the caller's intent is that
		// the graph ends up empty. Fall back to a per-edge pass that tolerates it.
		var nf *store.ErrNotFound
		if !errors.As(err, &nf) {
			return 0, err
		}
		return s.deleteRelationsIndividually(rels)
	}
	return len(rels), nil
}

// deleteRelationsIndividually is the tolerant fallback for clearing a graph whose edge set
// has drifted from what the index reported — an already-deleted edge is skipped rather
// than failing the clear.
func (s *Store) deleteRelationsIndividually(rels []*Relation) (int, error) {
	g, err := s.graph()
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, r := range rels {
		if err := g.DeleteEdge(store.EdgeID(r.ID)); err != nil {
			var nf *store.ErrNotFound
			if errors.As(err, &nf) {
				continue
			}
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
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
