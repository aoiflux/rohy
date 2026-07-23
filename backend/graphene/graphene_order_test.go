package graphene

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

// The ordering cache is a correctness hazard as much as a speedup: if it ever survives a
// write, the list silently shows stale data. These tests pin that it does not.

func TestOrderCacheInvalidatesOnInsert(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

	if _, err := s.InsertEvents([]*Event{mkEvent("1", "p", "c", "u", base, "h1")}); err != nil {
		t.Fatal(err)
	}
	first, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("got %d events, want 1", len(first))
	}

	// A write after the ordering was cached must be visible to the very next read.
	if _, err := s.InsertEvents([]*Event{mkEvent("2", "p", "c", "u", base.Add(time.Minute), "h2")}); err != nil {
		t.Fatal(err)
	}
	second, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Errorf("got %d events after insert, want 2 — the cached order went stale", len(second))
	}
	n, err := s.CountEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("count = %d after insert, want 2", n)
	}
}

func TestOrderCacheInvalidatesOnDelete(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("1", "p", "c", "u", base, "h1"),
		mkEvent("2", "p", "c", "u", base.Add(time.Minute), "h2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueryEvents(EventFilter{}); err != nil { // warm the cache
		t.Fatal(err)
	}
	if err := s.DeleteEvent(ids[0]); err != nil {
		t.Fatal(err)
	}
	got, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != ids[1] {
		t.Errorf("after delete got %d events, want just the survivor", len(got))
	}
}

func TestOrderCacheKeyedByFilter(t *testing.T) {
	// Two different filters must not share one cached ordering.
	s := OpenInMemory()
	defer s.Close()
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	if _, err := s.InsertEvents([]*Event{
		mkEvent("4624", "p", "Security", "alice", base, "h1"),
		mkEvent("4625", "p", "Security", "bob", base.Add(time.Minute), "h2"),
	}); err != nil {
		t.Fatal(err)
	}

	all, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	one, err := s.QueryEvents(EventFilter{EventID: "4625"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || len(one) != 1 || len(again) != 2 {
		t.Errorf("filters shared a cache entry: all=%d filtered=%d all-again=%d", len(all), len(one), len(again))
	}
	// Sort direction is part of the identity too.
	desc, err := s.QueryEvents(EventFilter{Descending: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(desc) != 2 || desc[0].ID == all[0].ID {
		t.Errorf("descending returned the ascending order — sort direction is not in the cache key")
	}
}

func TestPagingIsStableAndComplete(t *testing.T) {
	// Paging through a cached ordering must cover every row exactly once — the property
	// progressive loading depends on.
	s := OpenInMemory()
	defer s.Close()
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

	const total = 25
	events := make([]*Event, 0, total)
	for i := 0; i < total; i++ {
		// Deliberately duplicate timestamps so the id tie-break is exercised.
		events = append(events, mkEvent("1", "p", "c", "u", base.Add(time.Duration(i/5)*time.Minute), string(rune('a'+i))))
	}
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatal(err)
	}

	seen := map[uint64]int{}
	for offset := 0; offset < total; offset += 7 {
		page, err := s.QueryEvents(EventFilter{Offset: offset, Limit: 7})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			seen[e.ID]++
		}
	}
	if len(seen) != total {
		t.Errorf("paging covered %d distinct events, want %d", len(seen), total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("event %d returned %d times across pages, want once", id, n)
		}
	}
}

func TestOrderedPagingMatchesFullQuery(t *testing.T) {
	// The paged path and the unpaged path must agree, including under a post-hydration
	// filter that the minimal sort-view decode has to evaluate.
	s := OpenInMemory()
	defer s.Close()
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

	a := mkEvent("1", "p", "c", "u", base, "h1")
	a.DeduplicationCount = 5
	b := mkEvent("2", "p", "c", "u", base.Add(time.Minute), "h2")
	b.DeduplicationCount = 1
	if _, err := s.InsertEvents([]*Event{a, b}); err != nil {
		t.Fatal(err)
	}

	f := EventFilter{MinDuplicateCount: 3}
	full, err := s.QueryEvents(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 1 || full[0].DeduplicationCount != 5 {
		t.Fatalf("min-occurrence filter = %+v, want just the 5× event", full)
	}
	n, err := s.CountEvents(f)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(full) {
		t.Errorf("count %d disagrees with query %d", n, len(full))
	}
}

func TestSortViewMatchesHydratedFilter(t *testing.T) {
	// The minimal decode used for ordering and the full-event filter must agree, or a row
	// could be ordered in but filtered out (or vice versa).
	e := &Event{SourceIdentifier: "case.evtx", DeduplicationCount: 4}
	v := &eventSortView{SourceIdentifier: "case.evtx", DeduplicationCount: 4}

	cases := []EventFilter{
		{},
		{SourceIdentifier: "case.evtx"},
		{SourceIdentifier: "other.evtx"},
		{MinDuplicateCount: 4},
		{MinDuplicateCount: 5},
		{SourceIdentifier: "case.evtx", MinDuplicateCount: 2},
	}
	for _, f := range cases {
		if got, want := f.matchesSortView(v), f.matchesPostHydration(e); got != want {
			t.Errorf("filter %+v: sort view says %v, hydrated says %v", f, got, want)
		}
	}
}

func TestLegacyDedupCountTreatedAsOne(t *testing.T) {
	// Nodes written before deduplication_count existed decode as 0; both filter paths must
	// treat that as a single occurrence rather than excluding the event.
	f := EventFilter{MinDuplicateCount: 1}
	if !f.matchesSortView(&eventSortView{DeduplicationCount: 0}) {
		t.Errorf("legacy node excluded by the sort-view filter")
	}
	if !f.matchesPostHydration(&Event{DeduplicationCount: consts.DefaultDeduplicationCount}) {
		t.Errorf("legacy node excluded by the hydrated filter")
	}
}
