package graphene

import (
	"context"
	"fmt"
	"testing"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// legacyEvent builds an event the way a v0.1.0 build would have persisted it: parsed fields
// in the cold store, and no correlation projection at all.
func legacyEvent(t *testing.T, s *Store, hash string, fields map[string]string) *Event {
	t.Helper()
	e := &Event{
		EventID:        "4688",
		Computer:       "HOST-1",
		HashNormalized: hash,
		RawXML:         "<Event/>",
		ParsedFields:   fields,
	}
	// Deliberately NOT calling ComputeCorrelationKeys: that is the point of the fixture.
	if _, err := s.InsertEvents([]*Event{e}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return e
}

func TestBackfillProjectsLegacyEvents(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	legacyEvent(t, s, "b1", map[string]string{"SubjectLogonId": "0x3E7", "NewProcessId": "0x1A4"})
	legacyEvent(t, s, "b2", map[string]string{"TargetUserName": "Alice"})

	st, err := s.CorrelationKeyStatus()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Total != 2 || st.Stale != 2 || st.Current != 0 {
		t.Fatalf("before backfill: %+v", st)
	}
	if !st.NeedsBackfill() {
		t.Fatal("a store of unprojected events must report that it needs a backfill")
	}

	res, err := s.BackfillCorrelationKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.Examined != 2 || res.Projected != 2 || res.Failed != 0 {
		t.Fatalf("backfill result: %+v", res)
	}

	st, err = s.CorrelationKeyStatus()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Stale != 0 || st.Current != 2 {
		t.Fatalf("after backfill: %+v", st)
	}
	if st.NeedsBackfill() {
		t.Fatal("a fully projected store must not ask to be backfilled again")
	}

	// The values must match what ingest would have produced, or the backfill is a second
	// implementation that disagrees with the first.
	events, err := s.QueryEvents(EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	slot, _ := consts.CorrelationSlotIndex("subject_logon_id")
	found := false
	for _, e := range events {
		if e.HashNormalized == "b1" {
			found = true
			if got := e.CorrelationKey(slot); got != "0x3e7" {
				t.Errorf("backfilled projection = %q, want 0x3e7 (keys=%v)", got, e.CorrKeys)
			}
		}
	}
	if !found {
		t.Fatal("backfilled event not found")
	}
}

func TestBackfillIsIdempotent(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	legacyEvent(t, s, "i1", map[string]string{"TargetLogonId": "0x3E7"})

	first, err := s.BackfillCorrelationKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Projected != 1 {
		t.Fatalf("first run should project: %+v", first)
	}

	second, err := s.BackfillCorrelationKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Projected != 0 || second.AlreadyCurrent != 1 {
		t.Fatalf("a second run must read everything and write nothing: %+v", second)
	}
}

func TestBackfillSkipsEventsIngestAlreadyProjected(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	// An event ingested by THIS build already carries the projection, so a backfill must not
	// pay a cold-store read for it.
	e := &Event{EventID: "4624", Computer: "H", HashNormalized: "n1",
		ParsedFields: map[string]string{"TargetLogonId": "0x3E7"}}
	e.ComputeCorrelationKeys()
	if _, err := s.InsertEvents([]*Event{e}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	res, err := s.BackfillCorrelationKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.AlreadyCurrent != 1 || res.Projected != 0 {
		t.Fatalf("freshly ingested events must be skipped: %+v", res)
	}
}

// TestBackfillIsResumable is the property that lets the run be cancelled without bookkeeping:
// resumability comes from each event carrying its own recipe version, not from a checkpoint
// file that could disagree with the store.
func TestBackfillIsResumable(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	const n = consts.EventBatchSize + 10
	for i := range n {
		legacyEvent(t, s, fmt.Sprintf("r%d", i), map[string]string{"TargetLogonId": "0x3E7"})
	}

	// Cancel after the first batch commits.
	ctx, cancel := context.WithCancel(context.Background())
	first, err := s.BackfillCorrelationKeys(ctx, func(done, total int) { cancel() })
	if err == nil {
		t.Fatal("a cancelled run must report ctx.Err()")
	}
	if !first.Cancelled {
		t.Fatalf("run did not report itself cancelled: %+v", first)
	}
	if first.Projected == 0 || first.Projected == n {
		t.Fatalf("expected a partial run, got %+v", first)
	}

	// What it completed is durable.
	st, err := s.CorrelationKeyStatus()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Current != first.Projected {
		t.Fatalf("completed work not durable: status %+v vs run %+v", st, first)
	}

	// Re-running finishes the job and re-reads nothing it already did.
	second, err := s.BackfillCorrelationKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if second.AlreadyCurrent != first.Projected {
		t.Fatalf("resume did not skip completed work: %+v", second)
	}
	st, _ = s.CorrelationKeyStatus()
	if st.Stale != 0 {
		t.Fatalf("resume did not finish: %+v", st)
	}
}

// TestBackfillDoesNotStampWhatItCouldNotRead pins the honesty rule. Stamping an event whose
// payload could not be read would record "projected, and there was nothing there", which is a
// different and false claim — and it would stop any later run from retrying.
func TestBackfillLeavesUnreadablePayloadsUnstamped(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	legacyEvent(t, s, "u1", map[string]string{"TargetLogonId": "0x3E7"})

	// Corrupt the reference so the cold-store read fails for this event.
	events, err := s.QueryEvents(EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	broken := events[0]
	broken.Payload.Offset = 1 << 40 // beyond the log end
	node, err := broken.toNode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	node.ID = store.NodeID(broken.ID)
	tx := s.g.Begin()
	tx.UpdateNode(node)
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	res, err := s.BackfillCorrelationKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.Failed != 1 || res.Projected != 0 {
		t.Fatalf("expected one failure and no projection: %+v", res)
	}
	st, _ := s.CorrelationKeyStatus()
	if st.Stale != 1 {
		t.Fatalf("a failed event must stay stale so a later run retries it: %+v", st)
	}
}

func TestBackfillEmptyStore(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	res, err := s.BackfillCorrelationKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.Examined != 0 || res.Projected != 0 {
		t.Fatalf("empty store: %+v", res)
	}
	st, err := s.CorrelationKeyStatus()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.NeedsBackfill() {
		t.Fatal("an empty store does not need a backfill")
	}
}
