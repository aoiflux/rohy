package graphene

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"rohy/backend/consts"
)

// The two reads the query console is built on: counts per field over the same match the
// list uses, and a relation scan that uses the index where it can and visits in id order.

// backends runs fn on the in-memory and the disk store, which differ where the console cares: the
// order edges come back in, and what an unknown node's adjacency returns.
func backends(t *testing.T, fn func(t *testing.T, s *Store)) {
	t.Run("memory", func(t *testing.T) {
		s := OpenInMemory()
		t.Cleanup(func() { s.Close() })
		fn(t, s)
	})
	t.Run("disk", func(t *testing.T) {
		s, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		fn(t, s)
	})
}

var consoleBase = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

// consoleEvents inserts six events: two providers, two users, one undated.
func consoleEvents(t *testing.T, s *Store) []uint64 {
	t.Helper()
	evs := []*Event{
		mkEvent("4624", "P", "Security", "alice", consoleBase, "c1"),
		mkEvent("4624", "P", "Security", "bob", consoleBase.Add(time.Minute), "c2"),
		mkEvent("4688", "P", "Security", "alice", consoleBase.Add(2*time.Minute), "c3"),
		mkEvent("4688", "Q", "System", "", consoleBase.Add(3*time.Minute), "c4"),
		mkEvent("4625", "Q", "System", "bob", consoleBase.Add(4*time.Minute), "c5"),
		mkEvent("4625", "Q", "System", "bob", time.Time{}, "c6"), // undated
	}
	evs[3].SourceType = consts.SourceTypeSingleEVTX
	ids, err := s.InsertEvents(evs)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// consoleRelations inserts five relations across two rules, two graphs and both provenances.
func consoleRelations(t *testing.T, s *Store, ids []uint64) []*Relation {
	t.Helper()
	rel := func(from, to int, rule, algo, match string, graph uint64, by string) *Relation {
		return &Relation{
			From: ids[from], To: ids[to], GraphID: graph, RelationType: consts.RelationTemporal,
			CreatedBy: by, CreatedAt: consoleBase, RuleID: rule, Algorithm: algo, MatchID: match,
		}
	}
	rels := []*Relation{
		rel(0, 1, "rule-a", consts.AlgoSequence, "m-1", 1, consts.CreatedBySystem),
		rel(1, 2, "rule-a", consts.AlgoSequence, "m-1", 1, consts.CreatedBySystem),
		rel(2, 3, "rule-b", consts.AlgoField, "m-2", 2, consts.CreatedBySystem),
		rel(0, 3, "", "", "", 1, consts.CreatedByUser),
		rel(4, 5, "rule-b", consts.AlgoField, "m-3", 1, consts.CreatedBySystem),
	}
	for _, r := range rels {
		if _, err := s.InsertRelation(r); err != nil {
			t.Fatal(err)
		}
	}
	return rels
}

// scanned collects what ScanRelations visits, in visit order.
func scanned(t *testing.T, s *Store, f RelationFilter) ([]uint64, ScanStats) {
	t.Helper()
	var got []uint64
	st, err := s.ScanRelations(f, func(r *Relation) error {
		got = append(got, r.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("ScanRelations(%+v): %v", f, err)
	}
	return got, st
}

// expected is the same filter applied by hand to every relation, in id order: what the scan must
// give whichever path it takes.
func expected(t *testing.T, s *Store, keep func(*Relation) bool) []uint64 {
	t.Helper()
	all, err := s.GetRelations()
	if err != nil {
		t.Fatal(err)
	}
	var out []uint64
	for _, r := range all {
		if keep(r) {
			out = append(out, r.ID)
		}
	}
	slices.Sort(out)
	return out
}

func TestScanRelationsMatchesAFilterOverEveryRelation(t *testing.T) {
	backends(t, func(t *testing.T, s *Store) {
		ids := consoleEvents(t, s)
		consoleRelations(t, s, ids)
		for _, tc := range []struct {
			name string
			f    RelationFilter
			keep func(*Relation) bool
		}{
			{"everything", RelationFilter{}, func(*Relation) bool { return true }},
			{"by rule", RelationFilter{RuleID: "rule-a"}, func(r *Relation) bool { return r.RuleID == "rule-a" }},
			{"by graph", RelationFilter{GraphID: new(uint64(1))}, func(r *Relation) bool { return r.GraphID == 1 }},
			{"by creator", RelationFilter{CreatedBy: consts.CreatedByUser}, func(r *Relation) bool { return r.CreatedBy == consts.CreatedByUser }},
			{"by type", RelationFilter{RelationType: consts.RelationTemporal}, func(*Relation) bool { return true }},
			{"two indexed keys", RelationFilter{RuleID: "rule-b", GraphID: new(uint64(1))},
				func(r *Relation) bool { return r.RuleID == "rule-b" && r.GraphID == 1 }},
			{"by algorithm", RelationFilter{Algorithm: consts.AlgoField}, func(r *Relation) bool { return r.Algorithm == consts.AlgoField }},
			{"by match", RelationFilter{MatchID: "m-1"}, func(r *Relation) bool { return r.MatchID == "m-1" }},
			{"by event, either end", RelationFilter{EventID: new(ids[3])},
				func(r *Relation) bool { return r.From == ids[3] || r.To == ids[3] }},
			{"event and rule", RelationFilter{EventID: new(ids[3]), RuleID: "rule-b"},
				func(r *Relation) bool { return (r.From == ids[3] || r.To == ids[3]) && r.RuleID == "rule-b" }},
			{"an unknown event", RelationFilter{EventID: new(uint64(999_999))}, func(*Relation) bool { return false }},
			{"an unknown rule", RelationFilter{RuleID: "nope"}, func(*Relation) bool { return false }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got, st := scanned(t, s, tc.f)
				if want := expected(t, s, tc.keep); !slices.Equal(got, want) {
					t.Errorf("visited %v, want %v (in id order)", got, want)
				}
				if st.Vanished != 0 {
					t.Errorf("vanished %d", st.Vanished)
				}
			})
		}
	})
}

func TestScanRelationsStopsOnTheVisitorsError(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	consoleRelations(t, s, consoleEvents(t, s))
	stop := errors.New("enough")
	n := 0
	_, err := s.ScanRelations(RelationFilter{}, func(*Relation) error {
		n++
		if n == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || n != 2 {
		t.Fatalf("err %v after %d visits, want %v after 2", err, n, stop)
	}
}

// Reads take no case lock, so a rebuild can delete an edge between the id list and its read. That
// is the case changing under the query, not a failure of it.
func TestAnEdgeDeletedMidScanIsCountedNotFatal(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	rels := consoleRelations(t, s, consoleEvents(t, s))
	var got []uint64
	st, err := s.ScanRelations(RelationFilter{}, func(r *Relation) error {
		got = append(got, r.ID)
		if r.ID == rels[0].ID {
			return s.DeleteRelation(rels[1].ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Vanished != 1 || slices.Contains(got, rels[1].ID) || len(got) != len(rels)-1 {
		t.Fatalf("visited %v, vanished %d", got, st.Vanished)
	}
}

// The documented limit: an indexed query finds what the index lists. A crash can leave a relation
// listed under some keys and not others until the next repair (graphene_inventory.go); the
// integrity check reports it, and a query with no indexed condition still finds it.
func TestAHalfIndexedRelationIsMissedOnlyByTheKeyItLacks(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	ids := consoleEvents(t, s)
	r := &Relation{From: ids[0], To: ids[2], GraphID: 1, RelationType: consts.RelationTemporal,
		CreatedBy: consts.CreatedBySystem, CreatedAt: consoleBase, RuleID: "rule-x", Algorithm: consts.AlgoSequence}
	if err := s.InsertRelationPartlyIndexedForTesting(r, consts.PropGraphID); err != nil {
		t.Fatal(err)
	}
	if got, _ := scanned(t, s, RelationFilter{RuleID: "rule-x"}); len(got) != 0 {
		t.Errorf("by rule found %v through an index entry that was never written", got)
	}
	if got, _ := scanned(t, s, RelationFilter{GraphID: new(uint64(1))}); !slices.Equal(got, []uint64{r.ID}) {
		t.Errorf("by graph: %v", got)
	}
	if got, _ := scanned(t, s, RelationFilter{Algorithm: consts.AlgoSequence}); !slices.Equal(got, []uint64{r.ID}) {
		t.Errorf("by algorithm, which reads every edge: %v", got)
	}
}

func TestCountEventsBySumsToCountEvents(t *testing.T) {
	backends(t, func(t *testing.T, s *Store) {
		consoleRelations(t, s, consoleEvents(t, s))
		for _, f := range []EventFilter{
			{Undated: consts.UndatedInclude},
			{Undated: consts.UndatedExclude},
			{Undated: consts.UndatedOnly},
			{Undated: consts.UndatedInclude, Provider: "Q"},
			{Undated: consts.UndatedInclude, MinDuplicateCount: 2},
			{Undated: consts.UndatedInclude, RelationState: consts.RelationFilterUser},
			{Undated: consts.UndatedInclude, TimeFrom: new(consoleBase.Add(time.Minute))},
		} {
			for _, field := range EventGroupFields {
				g, err := s.CountEventsBy(f, field)
				if err != nil {
					t.Fatalf("%s: %v", field, err)
				}
				total, err := s.CountEvents(f)
				if err != nil {
					t.Fatal(err)
				}
				sum := g.Redacted
				for _, n := range g.Counts {
					sum += n
				}
				if sum != total {
					t.Errorf("%+v by %s: groups sum to %d, CountEvents says %d (%v)", f, field, sum, total, g)
				}
			}
		}
	})
}

func TestCountEventsByCountsEachValue(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	consoleEvents(t, s)
	all := EventFilter{Undated: consts.UndatedInclude}
	for field, want := range map[string]map[string]int{
		consts.PropEventID:          {"4624": 2, "4688": 2, "4625": 2},
		consts.PropProvider:         {"P": 3, "Q": 3},
		consts.PropChannel:          {"Security": 3, "System": 3},
		consts.PropUser:             {"alice": 2, "bob": 3, "": 1},
		consts.PropComputer:         {"HOST-1": 6},
		consts.PropSourceType:       {"": 5, consts.SourceTypeSingleEVTX: 1},
		consts.PropSourceIdentifier: {"": 6},
	} {
		g, err := s.CountEventsBy(all, field)
		if err != nil {
			t.Fatal(err)
		}
		if !maps.Equal(g.Counts, want) || g.Redacted != 0 {
			t.Errorf("by %s: %v (redacted %d), want %v", field, g.Counts, g.Redacted, want)
		}
	}
	if _, err := s.CountEventsBy(all, "raw_xml"); err == nil {
		t.Error("an unknown field was counted")
	}
}

// A redacted event has no fields left. Counting it under the blank value would say a user was
// empty when the truth is the record is gone.
func TestARedactedEventIsCountedApartFromTheBlankValue(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	ids := consoleEvents(t, s)
	bare(t, s, ids[1]) // bob, 4624
	g, err := s.CountEventsBy(EventFilter{Undated: consts.UndatedInclude}, consts.PropUser)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{"alice": 2, "bob": 2, "": 1}; !maps.Equal(g.Counts, want) || g.Redacted != 1 {
		t.Errorf("%v redacted %d, want %v redacted 1", g.Counts, g.Redacted, want)
	}
}

// computer is indexed (consts.IndexedNodeKeys) and is what an analyst narrows to first. The order
// cache must key it too, or the second host's list would be served the first host's order.
func TestTheComputerFilterSelectsOneHost(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	evs := []*Event{
		mkEvent("4624", "P", "Security", "alice", consoleBase, "h1"),
		mkEvent("4624", "P", "Security", "alice", consoleBase.Add(time.Minute), "h2"),
		mkEvent("4624", "P", "Security", "alice", consoleBase.Add(2*time.Minute), "h3"),
	}
	evs[1].Computer = "HOST-2"
	ids, err := s.InsertEvents(evs)
	if err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string][]uint64{"HOST-1": {ids[0], ids[2]}, "HOST-2": {ids[1]}, "HOST-9": nil} {
		f := EventFilter{Computer: host, Undated: consts.UndatedInclude}
		got, err := s.QueryEvents(f)
		if err != nil {
			t.Fatal(err)
		}
		var gotIDs []uint64
		for _, e := range got {
			gotIDs = append(gotIDs, e.ID)
		}
		n, err := s.CountEvents(f)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(gotIDs, want) || n != len(want) {
			t.Errorf("%s: listed %v, counted %d, want %v", host, gotIDs, n, want)
		}
	}
}
