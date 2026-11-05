package graphene

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"

	"rohy/backend/consts"
	"rohy/backend/payload"

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

	// The open. It is deferred until first use, so the application can show its window before
	// paying for a WAL replay, and — for the application's store — until a session has fixed the
	// posture (OpenAwaitingSession), because graphene takes the signer and the actor at open.
	//
	// ready is the happens-before edge that makes reading g lock-free once ensure() has returned:
	// it is stored after g, under openMu, and loaded first by every reader.
	//
	// A failed open is kept only for a lazy store, which has no other way to report it. A session's
	// open can be tried again with another posture: nothing is kept from a failed attempt.
	openMu    sync.Mutex
	ready     atomic.Bool
	attempted bool
	openErr   error
	awaiting  bool
	session   chan struct{} // closed once a session has opened the store
	stopping  chan struct{} // closed when shutdown begins: waiters stop waiting
	stopOnce  sync.Once
	// poisoned is set when a seal replaced the image on disk but this session's copy no longer
	// matches it: nothing more is written until a restart reopens it.
	poisoned atomic.Bool
	// order caches the id ordering of the most recent event query so that paging through
	// a filter does not rescan and re-sort the whole matching set for every page. Any
	// write invalidates it (see bumpVersion).
	order orderCache
	// payloads holds the bulky per-event fields outside the graph, because the graph keeps
	// its records resident. Opened alongside the graph so the two share a lifetime.
	payloads *payload.Store
	// forensic is the integrity posture this case was opened under (graphene_forensics.go).
	// The zero value is the engine's permissive default, which is what every existing case
	// already gets — see WithVerifyOnOpen for why that stays the default.
	forensic forensicOptions
}

// Open opens (creating if necessary) a disk-backed store rooted at dir, eagerly. The WAL
// is replayed automatically on restart. Call Compact after a bulk ingest.
func Open(dir string) (*Store, error) {
	s := newStore(dir)
	if err := s.ensure(); err != nil {
		return nil, err
	}
	return s, nil
}

// newStore is a store with the default posture: unsigned, audited, with the redaction ledger.
func newStore(dir string) *Store {
	return &Store{
		dir:      dir,
		session:  make(chan struct{}),
		stopping: make(chan struct{}),
		forensic: forensicOptions{posture: Posture{Mode: consts.CaseModeUnsigned}},
	}
}

// OpenAwaitingSession returns the application's store: it opens only when BeginSession fixes the
// session's posture. A call that needs the store before then waits for it, exactly as a call to a
// lazy store waits for the open; once shutdown begins, it returns ErrNoSession instead.
func OpenAwaitingSession(dir string) *Store {
	s := newStore(dir)
	s.awaiting = true
	return s
}

// BeginSession opens the store under p. A failed open leaves the store closed and keeps nothing,
// so the session can be begun again with another posture — a case whose signed open failed is
// then opened for recovery. Once a session has opened the store, it cannot be begun again.
func (s *Store) BeginSession(p Posture) error {
	if !s.awaiting {
		return errors.New("this store is not awaiting a session")
	}
	select {
	case <-s.stopping:
		return ErrNoSession
	default:
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if s.ready.Load() {
		return errors.New(consts.MsgSessionStarted)
	}
	if err := p.validate(); err != nil {
		return err
	}
	s.forensic.posture = p
	if err := s.openLocked(); err != nil {
		return err
	}
	close(s.session)
	return nil
}

// Opened reports whether the store is open. Shutdown asks it before sealing, so closing the app
// at the session step never opens a case only to seal it.
func (s *Store) Opened() bool { return s.ready.Load() }

// Shutdown stops anything waiting for a session. Idempotent.
func (s *Store) Shutdown() { s.stopOnce.Do(func() { close(s.stopping) }) }

// OpenLazy returns a disk-backed store that opens on FIRST USE rather than immediately.
//
// Opening replays the WAL and loads the graph, which on a large case is the slowest thing
// the app does at startup — and doing it before the window exists means the user stares at
// nothing. Deferring it lets the UI appear first and report progress while the store warms
// in the background (see Warm). Any call arriving before the store is open simply blocks
// until it is, so laziness can never surface as a nil dereference or a half-open read.
func OpenLazy(dir string) *Store {
	return newStore(dir)
}

// OpenInMemory returns an in-memory store, suitable for development and tests.
func OpenInMemory() *Store {
	s := newStore("")
	s.inMemory = true
	_ = s.ensure() // cannot fail for the in-memory backend
	return s
}

// ensure opens the store on first use. Every accessor funnels through it, so the store behaves
// identically whether it was opened eagerly, lazily, or by a session.
func (s *Store) ensure() error {
	if s.ready.Load() {
		return nil
	}
	if s.awaiting {
		select {
		case <-s.session:
			return nil
		case <-s.stopping:
			return ErrNoSession
		}
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if s.ready.Load() {
		return nil
	}
	if !s.attempted {
		s.attempted = true
		s.openErr = s.openLocked()
	}
	return s.openErr
}

// openLocked opens the graph under the store's posture, then the payload log. On failure nothing is
// left open. Caller holds openMu.
func (s *Store) openLocked() error {
	if s.inMemory {
		s.g = graphene.NewInMemory()
		s.payloads = payload.OpenInMemory()
		s.ready.Store(true)
		return nil
	}
	g, err := s.openGraph()
	if err != nil {
		return err
	}
	p, err := payload.Open(filepath.Join(s.dir, consts.PayloadDirName))
	if err != nil {
		g.Close()
		return err
	}
	s.g, s.payloads = g, p
	s.ready.Store(true)
	return nil
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
	s.Shutdown()
	// Holding openMu means a session's open cannot be half way through while this closes.
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if !s.ready.Load() {
		return nil
	}
	// Close the payload log first: it is written before the events that reference it, so it
	// is flushed before them too.
	if s.payloads != nil {
		if err := s.payloads.Close(); err != nil {
			s.g.Close()
			return err
		}
	}
	return s.g.Close()
}

// hydrate loads an event's cold fields from the payload store. Callers that return a SINGLE
// event use it; list queries deliberately do not, because hydrating a page of rows is
// exactly the cost this store exists to avoid.
func (s *Store) hydrate(e *Event) error {
	if e == nil || s.payloads == nil || e.Payload.IsZero() {
		return nil
	}
	blob, err := s.payloads.Read(e.Payload)
	var red *payload.RedactedError
	switch {
	case errors.As(err, &red) && red.Owner != e.ID && red.Version == consts.PayloadTombstoneVersion:
		// A tombstone naming another event is not this event's removal. Damage.
		return &payload.TombstoneOwnerError{Offset: red.Offset, Owner: red.Owner, Requested: e.ID}
	case red != nil:
		// A redaction removed the cold record. The event reads without it and says so.
		e.payloadRedacted = true
		return nil
	case err != nil:
		return err
	}
	return e.applyPayload(blob)
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

	// Write the bulky fields to the cold store FIRST, then commit the events that reference
	// them. A crash between the two leaves payload bytes nothing points at — wasted space,
	// reclaimed by rebuilding. The opposite order would leave events pointing at payloads
	// that were never written, which cannot be recovered from.
	if s.payloads != nil {
		blobs := make([][]byte, len(events))
		for i, e := range events {
			b, err := e.encodePayload()
			if err != nil {
				return nil, err
			}
			blobs[i] = b
		}
		refs, err := s.payloads.AppendBatch(blobs)
		if err != nil {
			return nil, err
		}
		if err := s.payloads.Sync(); err != nil {
			return nil, err
		}
		for i := range events {
			events[i].Payload = refs[i]
		}
	}

	nodes := make([]*store.Node, len(events))
	for i, e := range events {
		// Guarantee the per-source breakdown exists on everything persisted, whoever built
		// the event. Doing it here rather than at each producer is what keeps the invariant
		// "DeduplicationCount == sum(SourceCounts)" true for every stored event.
		e.ensureSourceCounts()
		n, err := e.toNode()
		if err != nil {
			return nil, err
		}
		nodes[i] = n
	}

	// One attributed commit for the batch (item 41). The engine's own batch insert commits as
	// actor 0; a transaction carries the session's analyst, and is just as atomic.
	tx := s.tx(g)
	ids := make([]store.NodeID, len(nodes))
	for i, n := range nodes {
		ids[i] = tx.AddNode(n)
	}
	if err := tx.Commit(); err != nil {
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
	tx := s.tx(g)
	id := tx.AddEdge(edge)
	if err := tx.Commit(); err != nil {
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

	tx := s.tx(g)
	ids := make([]store.EdgeID, len(rels))
	for i, r := range rels {
		ed, err := r.toEdge()
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		ids[i] = tx.AddEdge(ed)
	}
	if err := tx.Commit(); err != nil {
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

// UpdateRelation replaces a relation's type/label/confidence in place. Its endpoints are the
// relation's own: callers read the relation, change it and write it back, and a transaction's update
// writes the endpoints it is given (TestATransactionUpdateKeepsARelationsEndpoints pins it).
//
// The record is replaced in one attributed commit, then its index entries are purged and registered
// again. The plain update leaves the index alone under the default reindex policy, so an edited
// relation would keep matching its previous indexed values — and graph_id is one of those values, so
// a relation moved between graphs would still be found under the old graph. Index entries cannot be
// part of a commit, so between the commit and the re-registration the relation briefly matches
// nothing by its indexed values; it never matches stale ones once this returns.
func (s *Store) UpdateRelation(r *Relation) error {
	_, err := s.updateRelations([]*Relation{r})
	return err
}

// updateRelations replaces every relation in one commit, then re-registers their index entries.
// Returns how many were re-indexed, which on error is how far the index pass got.
func (s *Store) updateRelations(rels []*Relation) (int, error) {
	defer s.bumpVersion()
	if len(rels) == 0 {
		return 0, nil
	}
	g, err := s.graph()
	if err != nil {
		return 0, err
	}
	tx := s.tx(g)
	for _, r := range rels {
		edge, err := r.toEdge()
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		edge.ID = store.EdgeID(r.ID)
		tx.UpdateEdge(edge)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	reindexer, canPurge := g.GraphStore.(store.Reindexer)
	for i, r := range rels {
		if canPurge {
			if err := reindexer.PurgeEdgeIndex(store.EdgeID(r.ID)); err != nil {
				return i, err
			}
		}
		if err := g.IndexEdgeProperties(store.EdgeID(r.ID), r.indexValues()); err != nil {
			return i, err
		}
	}
	return len(rels), nil
}

// DeleteRelation removes a single mapped relation (and purges its index entries).
// A missing id returns *store.ErrNotFound.
func (s *Store) DeleteRelation(id uint64) error {
	defer s.bumpVersion()
	g, err := s.graph()
	if err != nil {
		return err
	}
	if _, err := g.GetEdge(store.EdgeID(id)); err != nil {
		return err
	}
	tx := s.tx(g)
	tx.DeleteEdge(store.EdgeID(id))
	return tx.Commit()
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
	if _, err := g.GetNode(store.NodeID(id)); err != nil {
		return err
	}
	tx := s.tx(g)
	tx.DeleteNode(store.NodeID(id))
	return tx.Commit()
}

// IncrementDedupCounts adds each delta to the deduplication_count of an existing
// event node. It is a read-modify-write per node, applied over a caller-sized map so
// no single write becomes a large transaction (the ingestion sink flushes these in
// bounded batches). Non-positive deltas and missing ids are skipped; the secondary
// index is untouched because deduplication_count is not indexed.
func (s *Store) IncrementDedupCounts(deltas map[uint64]map[string]int) error {
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
	for id, bySource := range deltas {
		if len(bySource) > 0 {
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

	tx := s.tx(g)
	for _, n := range nodes {
		// 🔒 A redacted event keeps no record, and writing one back would restore what the
		// redaction removed. Its occurrences are not counted: there is nothing left to count them on.
		if isStrippedEvent(n) {
			continue
		}
		e, err := eventFromNode(n)
		if err != nil {
			return err
		}
		// Attribute each occurrence to the source it came from. AddSourceOccurrence keeps
		// DeduplicationCount as the sum, so the total and the breakdown cannot drift apart
		// and no occurrence is counted twice.
		for source, delta := range deltas[uint64(n.ID)] {
			e.AddSourceOccurrence(source, delta)
		}
		updated, err := e.toNode()
		if err != nil {
			return err
		}
		updated.ID = n.ID
		// deduplication_count and source_counts are not indexed keys, so no index entry
		// needs replacing alongside the record — which is what lets these updates live in a
		// transaction, since index registration is not part of a commit.
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
	var legacy []*Relation
	for _, r := range rels {
		if r.GraphID == 0 {
			r.GraphID = graphID
			legacy = append(legacy, r)
		}
	}
	// One commit for the lot: a commit per relation would be a durable write each.
	return s.updateRelations(legacy)
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
	tx := s.tx(g)
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
	// The edges still there, deleted in one attributed commit: a commit per edge would be a durable
	// write each. Nothing else writes between the look and the commit — the API holds the case
	// lock — so an edge found here is still there to delete.
	tx := s.tx(g)
	deleted := 0
	for _, r := range rels {
		if _, err := g.GetEdge(store.EdgeID(r.ID)); err != nil {
			var nf *store.ErrNotFound
			if errors.As(err, &nf) {
				continue
			}
			_ = tx.Rollback()
			return 0, err
		}
		tx.DeleteEdge(store.EdgeID(r.ID))
		deleted++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
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
