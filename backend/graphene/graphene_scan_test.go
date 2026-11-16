package graphene

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// The whole-case scans read through graphene's batch readers and, for relations, decode no more
// than they need. Each is held here to a plain read of every record, one GetEdge at a time, on a
// case that holds a relation the index lost.
func TestTheWholeCaseScansAgreeWithEveryRecord(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	events := make([]*Event, 12)
	for i := range events {
		events[i] = mkEvent("4624", "p", "Security", "alice", base.Add(time.Duration(i)*time.Minute), "h"+string(rune('a'+i)))
	}
	ids, err := s.InsertEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	// System relations link 0-1 and 2-3 in graph 1, user relations 4-5 in graph 2; 6-7 is in
	// graph 9 and indexed under nothing, as a crash between record and index used to leave it.
	for _, r := range []*Relation{
		{From: ids[0], To: ids[1], GraphID: 1, CreatedBy: consts.CreatedBySystem, RelationType: consts.RelationCorrelation},
		{From: ids[2], To: ids[3], GraphID: 1, CreatedBy: consts.CreatedBySystem, RelationType: consts.RelationCorrelation},
		{From: ids[4], To: ids[5], GraphID: 2, CreatedBy: consts.CreatedByUser, RelationType: consts.RelationTemporal},
	} {
		if _, err := s.InsertRelation(r); err != nil {
			t.Fatal(err)
		}
	}
	gap := &Relation{From: ids[6], To: ids[7], GraphID: 9, CreatedBy: consts.CreatedBySystem, RelationType: consts.RelationCorrelation}
	if err := s.InsertRelationPartlyIndexedForTesting(gap); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s, err = Open(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	records := relationRecords(t, s)
	if len(records) != 4 {
		t.Fatalf("read %d relation records, want 4", len(records))
	}

	got, err := s.GetRelations()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, records) {
		t.Errorf("GetRelations:\n got  %+v\n want %+v", got, records)
	}

	counts, err := s.GraphRelationCounts()
	if err != nil {
		t.Fatal(err)
	}
	if want := map[uint64]int{1: 2, 2: 1, 9: 1}; !maps.Equal(counts, want) {
		t.Errorf("GraphRelationCounts = %v, want %v: the relation the index lost must still count", counts, want)
	}

	for state, want := range map[string][]uint64{
		consts.RelationFilterAny:    {ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7]},
		consts.RelationFilterSystem: {ids[0], ids[1], ids[2], ids[3]}, // created_by is indexed; the lost one is not found by it
		consts.RelationFilterUser:   {ids[4], ids[5]},
	} {
		related, err := s.relatedEventIDs(state)
		if err != nil {
			t.Fatal(err)
		}
		if got := slices.Sorted(maps.Keys(related)); !slices.Equal(got, want) {
			t.Errorf("relatedEventIDs(%q) = %v, want %v", state, got, want)
		}
	}
	if related, _ := s.relatedEventIDs(""); related != nil {
		t.Errorf("no relation filter must filter nothing, got %v", related)
	}
}

// relationRecords is the oracle: every relation edge, read one at a time and decoded whole.
func relationRecords(t *testing.T, s *Store) []*Relation {
	t.Helper()
	g, err := s.graph()
	if err != nil {
		t.Fatal(err)
	}
	ids, err := g.EdgesByType(consts.EdgeRelation)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]*Relation, 0, len(ids))
	for _, id := range ids {
		ed, err := g.GetEdge(id)
		if err != nil {
			t.Fatal(err)
		}
		r, err := relationFromEdge(ed)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// A budget smaller than any record puts one record in each batch, so a visit that spans many
// batches is seen whole and in order, and a visit's error stops the walk where it was returned.
func TestTheScanHelpersVisitEveryRecordAcrossBatches(t *testing.T) {
	s, ids := seedRelated(t)
	g, err := s.graph()
	if err != nil {
		t.Fatal(err)
	}
	nodes := make([]store.NodeID, len(ids))
	for i, id := range ids {
		nodes[i] = store.NodeID(id)
	}

	var seen []store.NodeID
	if err := eachNode(g, nodes, 1, func(n *store.Node) error { seen = append(seen, n.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, nodes) {
		t.Errorf("visited %v, want %v", seen, nodes)
	}

	stop := errors.New("stop")
	seen = seen[:0]
	err = eachNode(g, nodes, 1, func(n *store.Node) error {
		seen = append(seen, n.ID)
		if len(seen) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || len(seen) != 2 {
		t.Errorf("after an error at the second record: err %v, visited %d; want stop and 2", err, len(seen))
	}

	edges, err := g.EdgesByType(consts.EdgeRelation)
	if err != nil {
		t.Fatal(err)
	}
	var seenEdges []store.EdgeID
	if err := eachEdge(g, edges, 1, func(e *store.Edge) error { seenEdges = append(seenEdges, e.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seenEdges, edges) || len(edges) != 2 {
		t.Errorf("visited edges %v, want %v (2)", seenEdges, edges)
	}
}
