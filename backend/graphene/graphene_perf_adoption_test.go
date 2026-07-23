package graphene

import (
	"testing"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// These tests pin the performance-adoption decisions themselves, not just the behaviour
// they preserve. Each one would still pass on the slow path if it only checked results,
// so each asserts the mechanism: that the ordered key is declared and drives the range
// query, that a batch write lands as a batch, and that an update leaves no stale index.

// TestNoOrderedPropertiesDeclared pins a decision that is easy to undo by accident,
// because undoing it looks like an optimisation.
//
// Declaring the timestamp key ordered speeds up range lookups in principle, and costs
// roughly 7 seconds of EVERY store open at 100k events — a declaration does not survive a
// reopen, so it re-absorbs every registered entry each time, and for a near-unique key
// that dominates startup. Measured on this workload it bought nothing back: range queries
// are dominated by decoding matched records, not by finding them. See the note in
// graphene_store.go for the numbers.
//
// If this test fails, someone declared a key. That may well be right — but it needs a
// fresh measurement of OPEN, not just of the query it accelerates.
func TestNoOrderedPropertiesDeclared(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	g, err := s.graph()
	if err != nil {
		t.Fatal(err)
	}
	nodeKeys, edgeKeys := g.OrderedProperties()
	if len(nodeKeys) != 0 || len(edgeKeys) != 0 {
		t.Errorf("ordered declarations present (nodes=%v edges=%v); each one is re-absorbed on every open — re-measure open before keeping this",
			nodeKeys, edgeKeys)
	}
}

// TestTimeRangeQueryPlanShape records what a time-range query actually costs, so the
// trade-off behind the no-declaration decision stays visible instead of being folded into
// a comment nobody re-checks.
//
// The plan is: driver=labels, with the range applied as a residual set over those
// candidates. The upstream diagnostic table reads that shape as "a range on an undeclared
// key — declare it". rohy deliberately does not, because declaring costs ~7 s of every
// open at 100k events and measured no query improvement here (see graphene_store.go).
//
// What this test guards is the floor: the query must not degenerate into a full scan, and
// it must return exactly the window. If the plan shape changes, that is worth knowing.
func TestTimeRangeQueryPlanShape(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	events := make([]*Event, 0, 200)
	for i := range 200 {
		events = append(events, mkEvent("4624", "p", "c", "u", base.Add(time.Duration(i)*time.Minute), "h"+string(rune('a'+i%26))+string(rune('a'+i/26))))
	}
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatal(err)
	}

	g, err := s.graph()
	if err != nil {
		t.Fatal(err)
	}
	from, to := base.Add(10*time.Minute), base.Add(20*time.Minute)
	plan, err := g.ExplainNodeQuery(store.NodeQuery{
		Types: []store.NodeType{consts.NodeEvent},
		Filters: []store.PropertyFilter{{
			Key:        consts.PropTimestamp,
			Op:         store.PropertyOpBetweenInclusive,
			Value:      []byte(timestampIndex(from)),
			ValueUpper: []byte(timestampIndex(to)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A scan driver means nothing bounded the query at all — worse than the label-driven
	// plan this accepts, and the one shape that is never acceptable.
	if plan.Driver == store.DriverScan {
		t.Errorf("time-range query fell back to a full scan; plan: %s", plan)
	}
	// The range must still be evaluated against the timestamp key's index entries. If it
	// stops appearing as a residual on that key, the key is no longer indexed and the
	// filter is being answered some other way.
	var sawTimestampResidual bool
	for _, r := range plan.Residuals {
		if r.Key == consts.PropTimestamp {
			sawTimestampResidual = true
		}
	}
	if plan.DriverKey != consts.PropTimestamp && !sawTimestampResidual {
		t.Errorf("timestamp filter is neither driver nor residual; plan: %s", plan)
	}
	if plan.Results != 11 {
		t.Errorf("plan reports %d results for an 11-minute inclusive window, want 11; plan: %s",
			plan.Results, plan)
	}
	t.Logf("time-range plan: %s", plan)
}

// TestTimeRangeResultsUnchangedUnderByteWiseComparison guards the semantics half of the
// declaration. Declaring switches the key to byte-wise comparison in the index and in the
// residual filter alike, so the encoding has to make byte order and chronological order
// the same thing. Boundaries are where a broken encoding shows up first.
func TestTimeRangeResultsUnchangedUnderByteWiseComparison(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	// Straddle a year, a month, and a single-to-double digit rollover: each is a place a
	// non-fixed-width or non-UTC layout would sort wrongly.
	stamps := []time.Time{
		time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
		base,
		base.Add(9 * time.Minute),
		base.Add(10 * time.Minute),
		base.Add(100 * time.Minute),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	events := make([]*Event, 0, len(stamps))
	for i, ts := range stamps {
		events = append(events, mkEvent("4624", "p", "c", "u", ts, "hash-"+string(rune('a'+i))))
	}
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatal(err)
	}

	from, to := base, base.Add(100*time.Minute)
	got, err := s.QueryEvents(EventFilter{TimeFrom: &from, TimeTo: &to})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d events in range, want 4 (inclusive both ends)", len(got))
	}
	for _, e := range got {
		if e.Timestamp.Before(from) || e.Timestamp.After(to) {
			t.Errorf("event at %s is outside the requested range %s..%s", e.Timestamp, from, to)
		}
	}
}

// TestUndatedEventsExcludedFromTimeRange pins the position a zero timestamp takes. It
// encodes to year one, which must sort before every real record so an undated event is
// excluded by a lower bound rather than landing inside the range.
func TestUndatedEventsExcludedFromTimeRange(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	dated := mkEvent("4624", "p", "c", "u", base, "dated")
	undated := mkEvent("4625", "p", "c", "u", time.Time{}, "undated")
	if _, err := s.InsertEvents([]*Event{dated, undated}); err != nil {
		t.Fatal(err)
	}

	// Both are reachable when the undated policy asks for them — an undated event is
	// still an event, it just is not timeline evidence (P22).
	all, err := s.QueryEvents(EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d events with undated included, want 2", len(all))
	}

	// A lower time bound must exclude the undated event even when the policy would
	// otherwise include it: its encoded position is before every real record, which is
	// what keeps it out of the range rather than landing inside it.
	from := base.Add(-time.Hour)
	ranged, err := s.QueryEvents(EventFilter{TimeFrom: &from, Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranged) != 1 {
		t.Fatalf("got %d events from a lower bound, want 1 — the undated event has no position in time", len(ranged))
	}
	if ranged[0].HashNormalized != "dated" {
		t.Errorf("range returned %q, want the dated event", ranged[0].HashNormalized)
	}
}

// TestInsertRelationsBatchRoundTrip pins the batched relation write: every relation is
// persisted, stamped, and findable by its graph, exactly as the per-relation path was.
func TestInsertRelationsBatchRoundTrip(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	events := []*Event{
		mkEvent("4625", "p", "c", "u", base, "h1"),
		mkEvent("4625", "p", "c", "u", base.Add(time.Minute), "h2"),
		mkEvent("4624", "p", "c", "u", base.Add(2*time.Minute), "h3"),
	}
	ids, err := s.InsertEvents(events)
	if err != nil {
		t.Fatal(err)
	}

	const graphID = 7
	rels := []*Relation{
		{From: ids[0], To: ids[1], GraphID: graphID, RelationType: "correlation", CreatedBy: consts.CreatedBySystem},
		{From: ids[1], To: ids[2], GraphID: graphID, RelationType: "correlation", Label: "then succeeds", CreatedBy: consts.CreatedBySystem},
	}
	got, err := s.InsertRelations(rels)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(rels) {
		t.Fatalf("got %d ids, want %d", len(got), len(rels))
	}
	for i, r := range rels {
		if r.ID == 0 || r.ID != got[i] {
			t.Errorf("relation %d was not stamped with its assigned id (id=%d, returned=%d)", i, r.ID, got[i])
		}
	}

	// Index entries must be registered too, or the graph loads empty despite the edges
	// existing — the failure a separate indexing pass is most likely to produce.
	byGraph, err := s.RelationsByGraph(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(byGraph) != len(rels) {
		t.Fatalf("graph %d holds %d relations, want %d — index entries were not registered", graphID, len(byGraph), len(rels))
	}
}

// TestInsertRelationsEmptyIsNoOp guards the batch path's degenerate case: a rule that
// matches nothing must not fail the build.
func TestInsertRelationsEmptyIsNoOp(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	ids, err := s.InsertRelations(nil)
	if err != nil {
		t.Fatalf("empty batch returned an error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("empty batch returned %d ids", len(ids))
	}
}

// TestUpdateRelationLeavesNoStaleIndexEntry is the regression guard for the atomic
// indexed update. Under a plain update the index keeps the previous graph_id, so the
// relation stays findable under the graph it was moved out of — results that are wrong
// rather than merely slow.
func TestUpdateRelationLeavesNoStaleIndexEntry(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("4625", "p", "c", "u", base, "h1"),
		mkEvent("4624", "p", "c", "u", base.Add(time.Minute), "h2"),
	})
	if err != nil {
		t.Fatal(err)
	}

	const oldGraph, newGraph = 1, 2
	rel := &Relation{From: ids[0], To: ids[1], GraphID: oldGraph, RelationType: "correlation", CreatedBy: consts.CreatedBySystem}
	if _, err := s.InsertRelation(rel); err != nil {
		t.Fatal(err)
	}

	rel.GraphID = newGraph
	if err := s.UpdateRelation(rel); err != nil {
		t.Fatal(err)
	}

	stale, err := s.RelationsByGraph(oldGraph)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("graph %d still returns %d relation(s) after the move — the index entry went stale", oldGraph, len(stale))
	}
	moved, err := s.RelationsByGraph(newGraph)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 {
		t.Errorf("graph %d returns %d relation(s), want 1", newGraph, len(moved))
	}
}

// TestDeleteGraphRelationsIsAllOrNothing pins the transactional clear: the graph ends up
// empty and the events it referenced are untouched.
func TestDeleteGraphRelationsIsAllOrNothing(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("4625", "p", "c", "u", base, "h1"),
		mkEvent("4625", "p", "c", "u", base.Add(time.Minute), "h2"),
		mkEvent("4624", "p", "c", "u", base.Add(2*time.Minute), "h3"),
	})
	if err != nil {
		t.Fatal(err)
	}

	const graphID = 3
	if _, err := s.InsertRelations([]*Relation{
		{From: ids[0], To: ids[1], GraphID: graphID, CreatedBy: consts.CreatedBySystem},
		{From: ids[1], To: ids[2], GraphID: graphID, CreatedBy: consts.CreatedBySystem},
	}); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteGraphRelations(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("reported %d deletions, want 2", n)
	}
	left, err := s.RelationsByGraph(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("graph %d still holds %d relation(s) after the clear", graphID, len(left))
	}
	// Clearing a graph removes edges only; the events stay.
	events, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Errorf("got %d events after clearing a graph, want 3 — the clear touched nodes", len(events))
	}
}

// TestDeleteGraphRelationsEmptyIsNoOp covers clearing a graph that was never built, which
// the idempotent rebuild does on every first run.
func TestDeleteGraphRelationsEmptyIsNoOp(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	n, err := s.DeleteGraphRelations(42)
	if err != nil {
		t.Fatalf("clearing an empty graph returned an error: %v", err)
	}
	if n != 0 {
		t.Errorf("reported %d deletions on an empty graph", n)
	}
}

// TestIncrementDedupCountsBatched pins the batched read-modify-write: every delta lands,
// and an id that no longer exists is skipped rather than failing the whole pass.
func TestIncrementDedupCountsBatched(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("4625", "p", "c", "u", base, "h1"),
		mkEvent("4624", "p", "c", "u", base.Add(time.Minute), "h2"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// A deleted id in the same map must not abort the increments that are still valid.
	const goneID = 99999
	if err := s.IncrementDedupCounts(map[uint64]int{ids[0]: 2, ids[1]: 5, goneID: 3}); err != nil {
		t.Fatal(err)
	}

	first, err := s.GetEvent(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.DeduplicationCount != consts.DefaultDeduplicationCount+2 {
		t.Errorf("event 1 count = %d, want %d", first.DeduplicationCount, consts.DefaultDeduplicationCount+2)
	}
	second, err := s.GetEvent(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if second.DeduplicationCount != consts.DefaultDeduplicationCount+5 {
		t.Errorf("event 2 count = %d, want %d", second.DeduplicationCount, consts.DefaultDeduplicationCount+5)
	}
}
