package graphene

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"rohy/backend/consts"
	"rohy/backend/payload"

	"github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/signing"
	"github.com/aoiflux/graphene/store"
)

// Store is rohy's persistence facade over a graphene graph. It is the only
// type the rest of the backend uses to read and write events and relations.
type Store struct {
	dir string // empty for the in-memory store
	// inMemory selects the in-memory backend when the store is first opened.
	inMemory bool

	// cur is the open: the engine, the payload log and the posture they were opened under. A reopen
	// replaces it whole (Reopen), so a reader that loaded it holds one consistent open.
	cur atomic.Pointer[caseOpen]

	// The open. It is deferred until first use, so the application can show its window before
	// paying for a WAL replay, and — for the application's store — until a session has fixed the
	// posture (OpenAwaitingSession), because graphene takes the signer and the actor at open.
	//
	// ready is set once cur has been stored, under openMu, and loaded first by every reader.
	//
	// A failed open is kept only for a lazy store, which has no other way to report it. A session's
	// open can be tried again with another posture: nothing is kept from a failed attempt.
	openMu    sync.Mutex
	ready     atomic.Bool
	attempted bool
	openErr   error
	awaiting  bool
	session   chan struct{} // closed once a session has opened the store
	stopping  chan struct{} // closed when shutdown begins: waiters stop waiting, nothing reopens
	stopOnce  sync.Once
	// halted is why this open may no longer write, or nil: a seal replaced the image on disk but
	// this open's copy no longer matches it (Poison), or the engine's log stopped taking records
	// (noteStopped). Either way the open itself is the problem, so the next open clears it.
	halted atomic.Pointer[string]
	// order caches the id ordering of the most recent event query so that paging through
	// a filter does not rescan and re-sort the whole matching set for every page. Any
	// write invalidates it (see bumpVersion).
	order orderCache
	// forensic is the integrity posture this case is to be opened under (graphene_forensics.go);
	// once it is open, the posture in force is cur's. The zero value is the engine's permissive
	// default, which is what every existing case already gets — see WithVerifyOnOpen for why that
	// stays the default.
	forensic forensicOptions
}

// caseOpen is one open of the case. A reopen replaces it whole: a reader holds either the old open
// or the new one, with the posture that open was made under, and never half of each.
type caseOpen struct {
	g *graphene.Graph
	// payloads holds the bulky per-event fields outside the graph, because the graph keeps its
	// records resident. Opened alongside the graph so the two share a lifetime.
	payloads *payload.Store
	posture  Posture
	// readOnly is why this open cannot write, or nil. It is decided at the open and does not
	// change with it: a key unlocked later does not make a read-only open writable (Unlock).
	readOnly error
	// kept is every torn tail this open kept aside on the way in (graphene_torntail.go).
	kept []KeptTail
}

// close closes the payload log first: it is written before the events that reference it, so it is
// flushed before them too.
func (o *caseOpen) close() error {
	if err := o.payloads.Close(); err != nil {
		o.g.Close()
		return err
	}
	return o.g.Close()
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
	if err := s.openLocked(p); err != nil {
		return err
	}
	close(s.session)
	return nil
}

// ErrNotOpen is a reopen of a store that is not open, or whose shutdown has begun.
var ErrNotOpen = errors.New("the case is not open")

// ErrCaseHeld is an open refused because another program holds the case: the engine's lock is
// exclusive to a writer, and a reader shares it only with readers.
var ErrCaseHeld = errors.New(consts.MsgCaseHeld)

// ErrLogRefused is an open refused because the case's log is damaged before its end. The engine
// cuts a crash's torn tail itself, but records after damage were acknowledged, so it cuts nothing
// and the analyst decides: a backup, or a cut that gives those records up.
var ErrLogRefused = errors.New(consts.MsgLogRefused)

// NoPostureOpens reports whether err is an open refused for a reason no other posture gets past,
// so the session step does not offer recovery as a way in: a recovery open meets the same lock and
// replays the same log.
func NoPostureOpens(err error) bool {
	return errors.Is(err, ErrCaseHeld) || errors.Is(err, ErrLogRefused) || errors.Is(err, ErrLabelsDisagree)
}

// ErrLabelsDisagree is an open refused because the engine's labels file names rohy's event or
// relation type otherwise (nameTypes). The file is not evidence and not part of the image; rohy
// writes it again once it is gone.
var ErrLabelsDisagree = errors.New(consts.MsgLabelsDisagree)

// explainOpen says in the analyst's terms why the engine refused an open, keeping the engine's
// error, which names the holder's process or the damaged byte, inside it.
func explainOpen(err error) error {
	switch {
	case errors.Is(err, disk.ErrStoreLocked):
		return fmt.Errorf("%w: %w", ErrCaseHeld, err)
	case errors.Is(err, disk.ErrLogMalformed):
		return fmt.Errorf("%w: %w", ErrLogRefused, err)
	case errors.Is(err, store.ErrTypeNameConflict):
		return fmt.Errorf("%w: %w", ErrLabelsDisagree, err)
	}
	return err
}

// Reopen closes the case and opens it again under p. It is how a session that began read-only
// becomes writable: a read-only open holds a shared lock and cannot write, and only a new open
// lifts that. If the open under p fails, the case is opened again under the posture it had and the
// error is returned, so a refused reopen leaves the session as it was.
//
// 🔒 The caller holds every writer off for the duration (the API takes the case's exclusive lock).
// A read in flight finishes on the old open, which the engine's Close waits for, and a read that
// reaches the old open after it closed is refused with store.ErrClosed: never a crash, never a
// stale answer.
func (s *Store) Reopen(p Posture) error {
	if err := p.validate(); err != nil {
		return err
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	old := s.cur.Load()
	if old == nil || s.inMemory || s.shuttingDown() {
		return ErrNotOpen
	}
	defer s.bumpVersion() // nothing cached from the old open outlives it
	if err := old.close(); err != nil {
		return err
	}
	err := s.openLocked(p)
	if err == nil {
		s.forensic.posture = p
		return nil
	}
	if rerr := s.openLocked(old.posture); rerr != nil {
		// cur still holds the closed open, so every call is refused as closed until a reopen
		// succeeds or the app shuts down.
		return errors.Join(err, rerr)
	}
	return err
}

// Unlock makes a signed session that began with its key locked writable under key, by reopening the
// case: its open is read-only, and only a new open lifts that. The new posture carries a fresh slot
// holding key, and the session's own slot is left locked, so a refused reopen falls back to exactly
// the read-only session it was.
func (s *Store) Unlock(key *signing.Key) error {
	p := s.posture()
	if p.Mode != consts.CaseModeSigned || p.Slot == nil {
		return fmt.Errorf("%w: a %s session has no key to unlock", ErrReadOnly, p.Mode)
	}
	if s.Writable() == nil {
		return nil
	}
	slot := NewKeySlot(p.Slot.KeyID())
	if err := slot.Unlock(key); err != nil {
		return err
	}
	p.Slot = slot
	return s.Reopen(p)
}

// KeptAside is every torn tail the current open kept beside its file, for the session to record.
func (s *Store) KeptAside() []KeptTail {
	if o := s.cur.Load(); o != nil {
		return slices.Clone(o.kept)
	}
	return nil
}

// Opened reports whether the store is open. Shutdown asks it before sealing, so closing the app
// at the session step never opens a case only to seal it.
func (s *Store) Opened() bool { return s.ready.Load() }

// Shutdown stops anything waiting for a session, and any reopen. Idempotent.
func (s *Store) Shutdown() { s.stopOnce.Do(func() { close(s.stopping) }) }

func (s *Store) shuttingDown() bool {
	select {
	case <-s.stopping:
		return true
	default:
		return false
	}
}

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
		s.openErr = s.openLocked(s.forensic.posture)
	}
	return s.openErr
}

// openLocked opens the graph under p, then the payload log. On failure nothing is left open.
// Caller holds openMu.
//
// A posture that cannot write opens both read-only, so the session changes nothing on disk. One
// that can keeps every torn tail the engine is about to cut beside its file first.
func (s *Store) openLocked(p Posture) error {
	if s.inMemory {
		g := graphene.NewInMemory()
		if err := declare(g, "", false); err != nil {
			return err
		}
		s.cur.Store(&caseOpen{g: g, payloads: payload.OpenInMemory(), posture: p})
		s.ready.Store(true)
		return nil
	}
	readOnlyErr := p.writable()
	readOnly := readOnlyErr != nil
	var guard *tailGuard
	if !readOnly {
		var err error
		if guard, err = guardTornTails(s.dir, time.Now()); err != nil {
			return err
		}
	}
	g, err := graphene.OpenWithOptions(s.dir, p.diskOptions(s.forensic.verifyOnOpen))
	if err != nil {
		if guard != nil {
			guard.discard()
		}
		return explainOpen(err)
	}
	var kept []KeptTail
	if guard != nil {
		if kept, err = guard.settle(); err != nil {
			g.Close()
			return err
		}
	}
	if err := declare(g, s.dir, readOnly); err != nil {
		g.Close()
		return explainOpen(err)
	}
	openPayloads := payload.Open
	if readOnly {
		openPayloads = payload.OpenReadOnly
	}
	pl, err := openPayloads(filepath.Join(s.dir, consts.PayloadDirName))
	if err != nil {
		g.Close()
		return err
	}
	s.cur.Store(&caseOpen{g: g, payloads: pl, posture: p, readOnly: readOnlyErr, kept: kept})
	s.halted.Store(nil) // a fresh open loaded the image on disk and holds a log that takes records
	s.ready.Store(true)
	return nil
}

// posture is the posture the store is open under, or, before it opens, the one it will open under.
func (s *Store) posture() Posture {
	if o := s.cur.Load(); o != nil {
		return o.posture
	}
	return s.forensic.posture
}

// payloadLog is the open's payload log, or nil before the store opens.
func (s *Store) payloadLog() *payload.Store {
	if o := s.cur.Load(); o != nil {
		return o.payloads
	}
	return nil
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
	return s.cur.Load().g, nil
}

// nodeEventType is the node label events are stored under; a tiny helper so the ordering
// code does not need to import consts for a single value.
func nodeEventType() store.NodeType {
	return consts.NodeEvent
}

// Close releases the underlying store resources. Closing a store that was never opened is
// a no-op — nothing was acquired to release. Idempotent: the engine and the payload log each close
// once and answer a second Close with nil, and a call after Close is refused as closed by them.
func (s *Store) Close() error {
	s.Shutdown()
	// Holding openMu means a session's open cannot be half way through while this closes.
	s.openMu.Lock()
	defer s.openMu.Unlock()
	// ready is not cleared: a closed store must never look unopened, or the next accessor would
	// open it again behind the caller's back.
	if !s.ready.Load() {
		return nil
	}
	return s.cur.Load().close()
}

// hydrate loads an event's cold fields from the payload store. Callers that return a SINGLE
// event use it; list queries deliberately do not, because hydrating a page of rows is
// exactly the cost this store exists to avoid.
func (s *Store) hydrate(e *Event) error {
	pl := s.payloadLog()
	if e == nil || pl == nil || e.Payload.IsZero() {
		return nil
	}
	blob, err := pl.Read(e.Payload)
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
	return s.noteStopped(g.Compact())
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
// This is the recovery for a gap rohy's writes had until graphene v0.10: a relation was
// committed first and its index entries registered after, so a crash in between left an edge
// that exists but is invisible to every graph-scoped query, since those resolve through the
// graph_id index — while adjacency still shows it, because adjacency reads incident edges
// directly. The result is an event displaying a relation that belongs to no graph and that
// clearing the graph cannot remove. Writes now commit their entries with their records
// (TestInsertsCommitTheirIndexEntriesWithTheirRecords), so only a case written before that
// can still hold such an edge — which is why the graph build and a restore still repair first.
//
// Structural verification does not catch this. A property entry that was never written is
// not damage to the index's structure — the edge and its adjacency agree — so
// VerifyIndexes passes and RebuildIndexes cannot help, because neither can re-derive a
// caller-encoded value it never saw. Repair therefore has to come from this package, which
// is the only thing that knows how a relation encodes its own index entries.
//
// It works by walking edges, which does not depend on the index being correct, and comparing
// what it finds against what the index lists under EVERY key a relation is indexed by — see
// relationIndexGaps for why graph_id alone is not enough. It is idempotent:
// on a healthy store it registers nothing and returns zero, which is what makes it safe to
// offer as a maintenance action rather than a last resort.
func (s *Store) RepairRelationIndex() (int, error) {
	g, err := s.graph()
	if err != nil {
		return 0, err
	}
	gaps, err := relationIndexGaps(g)
	if err != nil || len(gaps) == 0 {
		return 0, err
	}
	// One attributed commit for the lot: the repair is all or nothing, and names who made it.
	tx := s.tx(g)
	for _, gap := range gaps {
		// Only the entries that are missing: the rest are already listed.
		tx.IndexEdgeProperties(gap.id, gap.missing)
	}
	if err := s.commit(tx); err != nil {
		return 0, err
	}
	s.bumpVersion() // relations became visible; any cached ordering is now stale
	return len(gaps), nil
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
	if pl := s.payloadLog(); pl != nil {
		blobs := make([][]byte, len(events))
		for i, e := range events {
			b, err := e.encodePayload()
			if err != nil {
				return nil, err
			}
			blobs[i] = b
		}
		refs, err := pl.AppendBatch(blobs)
		if err != nil {
			return nil, err
		}
		if err := pl.Sync(); err != nil {
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

	// One attributed commit for the batch, records and index entries together: an event
	// is never committed without the keys it is found by.
	tx := s.tx(g)
	ids := make([]store.NodeID, len(nodes))
	for i, n := range nodes {
		ids[i] = tx.AddNode(n)
		tx.IndexNodeProperties(ids[i], events[i].indexValues())
	}
	if err := s.commit(tx); err != nil {
		return nil, err
	}

	out := make([]uint64, len(ids))
	for i, id := range ids {
		events[i].ID = uint64(id)
		out[i] = uint64(id)
	}
	return out, nil
}

// InsertRelation persists a single mapped relation and its index entries in one commit,
// stamping the relation's ID. Src and Dst nodes must already exist.
func (s *Store) InsertRelation(r *Relation) (uint64, error) {
	ids, err := s.InsertRelations([]*Relation{r})
	if err != nil {
		return 0, err
	}
	return ids[0], nil
}

// InsertRelations persists a batch of relations and their index entries in ONE commit,
// stamping each relation's ID. Src and Dst nodes must already exist.
//
// This is the write path a rule-generated graph takes, where relations arrive by the
// thousand. Inserting them one at a time costs one durable write per edge — the WAL is
// what a crash-safe write buys, and it is not tunable — so the batch is the whole
// difference between a graph build that is write-bound per edge and one that pays a
// single framed commit for the lot. The batch is atomic, index entries included: on error
// nothing is applied and no ids are returned, and a crash leaves either the whole batch,
// findable by graph_id, or none of it (TestInsertsCommitTheirIndexEntriesWithTheirRecords).
func (s *Store) InsertRelations(rels []*Relation) ([]uint64, error) {
	defer s.bumpVersion() // relations feed the relation-state filter
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
		tx.IndexEdgeProperties(ids[i], r.indexValues())
	}
	if err := s.commit(tx); err != nil {
		return nil, err
	}

	out := make([]uint64, len(ids))
	for i, id := range ids {
		rels[i].ID = uint64(id)
		out[i] = uint64(id)
	}
	return out, nil
}

// UpdateRelation replaces a relation's type/label/confidence in place. Its endpoints are the
// relation's own: callers read the relation, change it and write it back, and a transaction's update
// writes the endpoints it is given (TestATransactionUpdateKeepsARelationsEndpoints pins it).
//
// The record and its whole set of index entries are replaced in one attributed commit. graph_id is
// one of those entries, so a relation moved between graphs is found under the new graph and never
// under the old one, and there is no moment at which it matches nothing.
func (s *Store) UpdateRelation(r *Relation) error {
	_, err := s.updateRelations([]*Relation{r})
	return err
}

// updateRelations replaces every relation, record and index entries together, in one commit.
// Returns how many were replaced: all of them, or none on error.
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
		tx.UpdateEdgeIndexed(edge, r.indexValues())
	}
	if err := s.commit(tx); err != nil {
		return 0, err
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
	return s.commit(tx)
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
	return s.commit(tx)
}

// IncrementDedupCounts adds each delta to the deduplication_count of an existing
// event node. It is a read-modify-write per node, applied over a caller-sized map so
// no single write becomes a large transaction (the ingestion sink flushes these in
// bounded batches). Non-positive deltas and missing ids are skipped.
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
		indexed := e.indexValues()
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
		tx.UpdateNodePartialIndex(updated, indexChanges(indexed, e))
	}
	return s.commit(tx)
}

// indexChanges returns the index entries of e that differ from before: exactly what an update of
// the record must re-register. An ordinary update is refused on an indexed entity, and re-registering
// every key would rewrite entries that did not move; naming only the changed ones keeps an update
// correct when a field it touches becomes indexed later.
func indexChanges(before map[string][]byte, e *Event) map[string][]byte {
	var changed map[string][]byte
	for k, v := range e.indexValues() {
		if old, ok := before[k]; ok && bytes.Equal(old, v) {
			continue
		}
		if changed == nil {
			changed = map[string][]byte{}
		}
		changed[k] = v
	}
	return changed
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
	if err := s.commit(tx); err != nil {
		// A relation that is already gone is not a failure: the caller's intent is that
		// the graph ends up empty. Fall back to a per-edge pass that tolerates it.
		if _, ok := errors.AsType[*store.ErrNotFound](err); !ok {
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
			if _, ok := errors.AsType[*store.ErrNotFound](err); ok {
				continue
			}
			_ = tx.Rollback()
			return 0, err
		}
		tx.DeleteEdge(store.EdgeID(r.ID))
		deleted++
	}
	if err := s.commit(tx); err != nil {
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
