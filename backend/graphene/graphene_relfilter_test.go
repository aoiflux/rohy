package graphene

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

// seedRelated builds three events and links the first two: one rule-created relation and
// one manual one, both between the same pair. The third event stays unrelated.
func seedRelated(t *testing.T) (*Store, []uint64) {
	t.Helper()
	s := OpenInMemory()
	t.Cleanup(func() { s.Close() })

	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("4625", "p", "Security", "alice", base, "r1"),
		mkEvent("4624", "p", "Security", "alice", base.Add(time.Minute), "r2"),
		mkEvent("1102", "p", "Security", "bob", base.Add(2*time.Minute), "r3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertRelation(&Relation{
		From: ids[0], To: ids[1], RelationType: consts.RelationCorrelation,
		CreatedBy: consts.CreatedBySystem, CreatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertRelation(&Relation{
		From: ids[0], To: ids[1], RelationType: consts.RelationTemporal,
		CreatedBy: consts.CreatedByUser, CreatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	return s, ids
}

func idsOf(events []*Event) map[uint64]bool {
	out := map[uint64]bool{}
	for _, e := range events {
		out[e.ID] = true
	}
	return out
}

func TestRelationStateFilters(t *testing.T) {
	s, ids := seedRelated(t)

	cases := []struct {
		state string
		want  []uint64
	}{
		{"", ids}, // unfiltered: everything
		{consts.RelationFilterAny, []uint64{ids[0], ids[1]}},
		{consts.RelationFilterSystem, []uint64{ids[0], ids[1]}},
		{consts.RelationFilterUser, []uint64{ids[0], ids[1]}},
	}
	for _, c := range cases {
		events, err := s.QueryEvents(EventFilter{RelationState: c.state})
		if err != nil {
			t.Fatalf("state %q: %v", c.state, err)
		}
		got := idsOf(events)
		if len(got) != len(c.want) {
			t.Errorf("state %q: got %d events, want %d", c.state, len(got), len(c.want))
		}
		for _, id := range c.want {
			if !got[id] {
				t.Errorf("state %q: event %d missing", c.state, id)
			}
		}
		// The unrelated event must be excluded by every relation filter.
		if c.state != "" && got[ids[2]] {
			t.Errorf("state %q: unrelated event was included", c.state)
		}
	}
}

func TestRelationStateSeparatesProvenance(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("1", "p", "c", "u", base, "h1"),
		mkEvent("2", "p", "c", "u", base.Add(time.Minute), "h2"),
		mkEvent("3", "p", "c", "u", base.Add(2*time.Minute), "h3"),
		mkEvent("4", "p", "c", "u", base.Add(3*time.Minute), "h4"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Pair 0-1 linked by a rule; pair 2-3 linked by hand.
	if _, err := s.InsertRelation(&Relation{From: ids[0], To: ids[1], CreatedBy: consts.CreatedBySystem, CreatedAt: base}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertRelation(&Relation{From: ids[2], To: ids[3], CreatedBy: consts.CreatedByUser, CreatedAt: base}); err != nil {
		t.Fatal(err)
	}

	sys, err := s.QueryEvents(EventFilter{RelationState: consts.RelationFilterSystem})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOf(sys)
	if !got[ids[0]] || !got[ids[1]] {
		t.Errorf("rule-correlated filter missed the rule-linked pair: %v", got)
	}
	if got[ids[2]] || got[ids[3]] {
		t.Errorf("rule-correlated filter included manually mapped events: %v", got)
	}

	usr, err := s.QueryEvents(EventFilter{RelationState: consts.RelationFilterUser})
	if err != nil {
		t.Fatal(err)
	}
	got = idsOf(usr)
	if !got[ids[2]] || !got[ids[3]] {
		t.Errorf("manual filter missed the hand-linked pair: %v", got)
	}
	if got[ids[0]] || got[ids[1]] {
		t.Errorf("manual filter included rule-correlated events: %v", got)
	}
}

func TestCountAgreesWithQueryUnderRelationFilter(t *testing.T) {
	// The list and its "X of N" total must never disagree — that was the whole point of
	// routing both through one matcher.
	s, _ := seedRelated(t)
	for _, state := range []string{"", consts.RelationFilterAny, consts.RelationFilterSystem, consts.RelationFilterUser} {
		f := EventFilter{RelationState: state}
		events, err := s.QueryEvents(f)
		if err != nil {
			t.Fatal(err)
		}
		n, err := s.CountEvents(f)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(events) {
			t.Errorf("state %q: count = %d but query returned %d", state, n, len(events))
		}
	}
}

func TestRelationFilterComposesWithOtherFilters(t *testing.T) {
	s, ids := seedRelated(t)
	// Related AND matching an event id: only the first event qualifies.
	events, err := s.QueryEvents(EventFilter{RelationState: consts.RelationFilterAny, EventID: "4625"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != ids[0] {
		t.Errorf("composed filter = %+v, want just the 4625 event", idsOf(events))
	}
}

func TestUnknownRelationStateFiltersNothing(t *testing.T) {
	// A stale or misspelled value must not silently hide every event.
	s, ids := seedRelated(t)
	events, err := s.QueryEvents(EventFilter{RelationState: "not-a-state"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(ids) {
		t.Errorf("unknown state returned %d events, want all %d", len(events), len(ids))
	}
}

func TestAdjacencyReportsProvenance(t *testing.T) {
	s, ids := seedRelated(t)
	adj, err := s.RelationsAdjacency(ids)
	if err != nil {
		t.Fatal(err)
	}
	a := adj[ids[0]]
	if a == nil {
		t.Fatal("no adjacency for the linked event")
	}
	if a.Count != 2 {
		t.Errorf("count = %d, want 2", a.Count)
	}
	if a.SystemCount != 1 || a.UserCount != 1 {
		t.Errorf("provenance split = %d system / %d user, want 1/1", a.SystemCount, a.UserCount)
	}
	if _, ok := adj[ids[2]]; ok {
		t.Errorf("unrelated event should have no adjacency entry")
	}
}
