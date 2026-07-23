package graphene

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

// An event with no timestamp cannot be placed on a timeline, so it is excluded from the
// chronological views by default. These tests pin that policy — and pin that the data is
// still reachable, because hiding evidence outright would be worse than showing it badly.

func seedUndated(t *testing.T) (*Store, uint64, uint64) {
	t.Helper()
	s := OpenInMemory()
	t.Cleanup(func() { s.Close() })

	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	dated := mkEvent("4624", "p", "Security", "u", base, "dated")
	undated := mkEvent("4625", "p", "Security", "u", time.Time{}, "undated")
	ids, err := s.InsertEvents([]*Event{dated, undated})
	if err != nil {
		t.Fatal(err)
	}
	return s, ids[0], ids[1]
}

func TestUndatedExcludedByDefault(t *testing.T) {
	s, datedID, undatedID := seedUndated(t)

	got, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != datedID {
		t.Fatalf("default query returned %d events, want only the dated one", len(got))
	}
	if got[0].ID == undatedID {
		t.Errorf("undated event appeared in the timeline by default")
	}

	// The count must agree, or the list would say "1 of 2" and look like it lost a row.
	n, err := s.CountEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1 — the total must match what the list shows", n)
	}
}

func TestUndatedIncludeAndOnly(t *testing.T) {
	s, datedID, undatedID := seedUndated(t)

	all, err := s.QueryEvents(EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("include returned %d events, want both", len(all))
	}

	only, err := s.QueryEvents(EventFilter{Undated: consts.UndatedOnly})
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].ID != undatedID {
		t.Errorf("only-undated returned %+v, want just the undated event", only)
	}
	if len(only) == 1 && only[0].ID == datedID {
		t.Errorf("only-undated returned the dated event")
	}

	// The "only" count is what the UI uses to say how many are hidden.
	n, err := s.CountEvents(EventFilter{Undated: consts.UndatedOnly})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("undated count = %d, want 1", n)
	}
}

func TestUndatedPolicyIsPerFilterInTheCache(t *testing.T) {
	// The orderings differ, so they must not share a cache entry.
	s, _, _ := seedUndated(t)

	excluded, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	included, err := s.QueryEvents(EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	againExcluded, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(excluded) != 1 || len(included) != 2 || len(againExcluded) != 1 {
		t.Errorf("undated policy shared a cached ordering: %d / %d / %d",
			len(excluded), len(included), len(againExcluded))
	}
}

func TestUndatedComposesWithOtherFilters(t *testing.T) {
	s, _, _ := seedUndated(t)
	got, err := s.QueryEvents(EventFilter{Undated: consts.UndatedOnly, EventID: "4625"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EventID != "4625" {
		t.Errorf("composed filter = %+v, want the single undated 4625", got)
	}
	// And a filter that cannot match anything undated yields nothing rather than leaking.
	none, err := s.QueryEvents(EventFilter{Undated: consts.UndatedOnly, EventID: "4624"})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("expected no matches, got %+v", none)
	}
}

func TestUndatedSortToTheEndInBothDirections(t *testing.T) {
	// An undated event has no chronological position, so it must never be ordered as if it
	// happened at the zero time — which, ascending, would put it ahead of every real record
	// and read as a date rather than the absence of one (P23).
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("A", "p", "c", "u", base, "h1"),
		mkEvent("B", "p", "c", "u", time.Time{}, "h2"), // undated
		mkEvent("C", "p", "c", "u", base.Add(time.Hour), "h3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	undatedID := ids[1]

	for _, desc := range []bool{false, true} {
		got, err := s.QueryEvents(EventFilter{Undated: consts.UndatedInclude, Descending: desc})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("descending=%v: got %d events, want 3", desc, len(got))
		}
		if got[len(got)-1].ID != undatedID {
			t.Errorf("descending=%v: undated event is at position %d, want last", desc, indexOfID(got, undatedID))
		}
	}
}

func indexOfID(events []*Event, id uint64) int {
	for i, e := range events {
		if e.ID == id {
			return i
		}
	}
	return -1
}

func TestUnknownUndatedValueHidesNothing(t *testing.T) {
	// A stale or misspelled value must not silently drop evidence.
	s, _, _ := seedUndated(t)
	got, err := s.QueryEvents(EventFilter{Undated: "not-a-policy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("unknown policy returned %d events; it should fall back to the default", len(got))
	}
}
