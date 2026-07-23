package graphene

import (
	"testing"
	"time"
)

// TestCountEventsAndStablePagination verifies CountEvents matches the true total and that
// offset/limit paging over a chronologically ordered set (with duplicate timestamps) is
// stable — every event appears exactly once across pages, in the same order as one query.
func TestCountEventsAndStablePagination(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 25 events; several share a timestamp to exercise the id tie-break in the sort.
	events := make([]*Event, 0, 25)
	for i := 0; i < 25; i++ {
		ts := base.Add(time.Duration(i/3) * time.Minute) // groups of 3 share a timestamp
		events = append(events, mkEvent("4624", "P", "Security", "u", ts, "h"+time.Duration(i).String()))
	}
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Count with no filter equals the total.
	total, err := s.CountEvents(EventFilter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 25 {
		t.Fatalf("CountEvents = %d, want 25", total)
	}

	// The single full query is the reference order.
	full, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatalf("query all: %v", err)
	}
	if len(full) != 25 {
		t.Fatalf("full query = %d, want 25", len(full))
	}

	// Page through in chunks of 7; the concatenation must equal `full` exactly.
	var paged []*Event
	const page = 7
	for off := 0; off < total; off += page {
		chunk, err := s.QueryEvents(EventFilter{Offset: off, Limit: page})
		if err != nil {
			t.Fatalf("page @%d: %v", off, err)
		}
		paged = append(paged, chunk...)
	}
	if len(paged) != len(full) {
		t.Fatalf("paged %d != full %d (skips/dupes)", len(paged), len(full))
	}
	seen := map[uint64]bool{}
	for i := range full {
		if paged[i].ID != full[i].ID {
			t.Fatalf("order diverged at %d: paged=%d full=%d", i, paged[i].ID, full[i].ID)
		}
		if seen[paged[i].ID] {
			t.Fatalf("duplicate id %d across pages", paged[i].ID)
		}
		seen[paged[i].ID] = true
	}
}

// TestCountEventsWithFilter confirms the count honors the active filter.
func TestCountEventsWithFilter(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.InsertEvents([]*Event{
		mkEvent("4624", "P", "Security", "alice", base, "a"),
		mkEvent("4625", "P", "Security", "bob", base.Add(time.Minute), "b"),
		mkEvent("4624", "P", "System", "carol", base.Add(2*time.Minute), "c"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	if n, _ := s.CountEvents(EventFilter{}); n != 3 {
		t.Errorf("total count = %d, want 3", n)
	}
	if n, _ := s.CountEvents(EventFilter{Channel: "Security"}); n != 2 {
		t.Errorf("Security count = %d, want 2", n)
	}
	if n, _ := s.CountEvents(EventFilter{EventID: "4624"}); n != 2 {
		t.Errorf("4624 count = %d, want 2", n)
	}
}
