package evtx

import (
	"context"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// dedupEvent builds a minimal event with a controlled normalized hash, mirroring what
// the normalizers produce (DeduplicationCount seeded to the canonical default).
// dedupEvent builds an event whose identity is driven by a REAL field (the event id),
// not by a hand-set hash. The sink derives identity itself for a dated event — it has to,
// because the source is only known there — so a fixture that distinguished two events by
// assigning them different hashes would have those hashes overwritten and collapse into one.
func dedupEvent(kind string) *graphene.Event {
	e := &graphene.Event{
		EventID:            kind,
		Timestamp:          time.Unix(0, 0).UTC(),
		Provider:           "P",
		Channel:            consts.ChannelApplication,
		Computer:           "H",
		User:               "u",
		RawXML:             "<E/>",
		HashRaw:            "raw-" + kind,
		DeduplicationCount: consts.DefaultDeduplicationCount,
	}
	e.ComputeNormalizedHash()
	return e
}

// countByHash returns each canonical event's deduplication_count keyed by hash.
func countByHash(t *testing.T, s *graphene.Store) map[string]int {
	t.Helper()
	events, err := s.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	out := make(map[string]int)
	for _, e := range events {
		// Keyed by event id: identity is derived now, so the hash is not a label the test
		// can choose.
		out[e.EventID] = e.DeduplicationCount
	}
	return out
}

// TestDedupCountsWithinAndAcrossBatches drives runSink with a duplicate pattern that
// spans a flush boundary, exercising the in-batch increment path (pendingByHash), the
// deferred DB-increment path (dbInc), and the final increment flush. Pattern A,A,B,A,B
// with BatchSize 2 collapses to two canonical nodes with counts A=3, B=2.
func TestDedupCountsWithinAndAcrossBatches(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	resultCh := make(chan chunkResult, 1)
	resultCh <- chunkResult{events: []*graphene.Event{
		dedupEvent("A"), dedupEvent("A"), dedupEvent("B"), dedupEvent("A"), dedupEvent("B"),
	}}
	close(resultCh)

	opts := Options{Source: consts.SourceFile, Idempotent: true, BatchSize: 2}
	sum, err := runSink(context.Background(), opts, store, NoopReporter{}, resultCh, 1)
	if err != nil {
		t.Fatalf("runSink: %v", err)
	}

	if sum.RecordsRead != 5 {
		t.Errorf("RecordsRead = %d, want 5", sum.RecordsRead)
	}
	if sum.RecordsPersisted != 2 {
		t.Errorf("RecordsPersisted = %d, want 2 (two canonical nodes)", sum.RecordsPersisted)
	}
	if sum.RecordsDuplicate != 3 {
		t.Errorf("RecordsDuplicate = %d, want 3", sum.RecordsDuplicate)
	}

	nodes, _, _ := store.Stats()
	if nodes != 2 {
		t.Fatalf("store has %d nodes, want 2", nodes)
	}
	counts := countByHash(t, store)
	if counts["A"] != 3 {
		t.Errorf("count[A] = %d, want 3", counts["A"])
	}
	if counts["B"] != 2 {
		t.Errorf("count[B] = %d, want 2", counts["B"])
	}
}

// TestDedupNoDoubleCountWithoutIdempotent confirms dedup is opt-in: with Idempotent
// off, identical events are inserted as separate nodes and never merged/counted.
func TestDedupNoDoubleCountWithoutIdempotent(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	resultCh := make(chan chunkResult, 1)
	resultCh <- chunkResult{events: []*graphene.Event{dedupEvent("A"), dedupEvent("A")}}
	close(resultCh)

	opts := Options{Source: consts.SourceFile, BatchSize: 8} // Idempotent: false
	sum, err := runSink(context.Background(), opts, store, NoopReporter{}, resultCh, 1)
	if err != nil {
		t.Fatalf("runSink: %v", err)
	}
	if sum.RecordsPersisted != 2 || sum.RecordsDuplicate != 0 {
		t.Fatalf("persisted=%d duplicate=%d, want 2/0", sum.RecordsPersisted, sum.RecordsDuplicate)
	}
}

// TestDedupCountsAcrossRuns re-ingests the same fixture with idempotency on and
// asserts no new nodes appear while every canonical's occurrence count rises to 2.
func TestDedupCountsAcrossRuns(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	opts := Options{Source: consts.SourceFile, Path: fixtureSecurity, BatchSize: 16, Workers: 2, Idempotent: true}

	first, err := Ingest(context.Background(), opts, store, NoopReporter{})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.RecordsPersisted == 0 {
		t.Fatal("first ingest persisted nothing")
	}

	second, err := Ingest(context.Background(), opts, store, NoopReporter{})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.RecordsPersisted != 0 {
		t.Errorf("second ingest persisted %d new nodes, want 0", second.RecordsPersisted)
	}
	if second.RecordsDuplicate != first.RecordsPersisted {
		t.Errorf("duplicates = %d, want %d", second.RecordsDuplicate, first.RecordsPersisted)
	}

	nodes, _, _ := store.Stats()
	if int(nodes) != first.RecordsPersisted {
		t.Errorf("node count changed on re-ingest: %d, want %d", nodes, first.RecordsPersisted)
	}
	events, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.DeduplicationCount != 2 {
			t.Fatalf("event %d count = %d, want 2 after two ingests", e.ID, e.DeduplicationCount)
		}
	}
}
