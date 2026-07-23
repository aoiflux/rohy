package graphene

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

func mkRel(from, to, graphID uint64) *Relation {
	return &Relation{
		From:         from,
		To:           to,
		GraphID:      graphID,
		RelationType: consts.RelationDefault,
		CreatedBy:    consts.CreatedByUser,
		CreatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestRelationsByGraphScoping verifies relations are partitioned by graph_id and that
// deleting a graph's relations leaves the other graph and all events intact.
func TestRelationsByGraphScoping(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("1", "P", "Security", "alice", base, "a"),
		mkEvent("2", "P", "Security", "bob", base.Add(time.Minute), "b"),
		mkEvent("3", "P", "Security", "carol", base.Add(2*time.Minute), "c"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Graph 1: 1—2. Graph 2: 2—3 and 1—3.
	for _, r := range []*Relation{
		mkRel(ids[0], ids[1], 1),
		mkRel(ids[1], ids[2], 2),
		mkRel(ids[0], ids[2], 2),
	} {
		if _, err := s.InsertRelation(r); err != nil {
			t.Fatalf("insert relation: %v", err)
		}
	}

	g1, err := s.RelationsByGraph(1)
	if err != nil {
		t.Fatalf("RelationsByGraph(1): %v", err)
	}
	if len(g1) != 1 {
		t.Fatalf("graph 1 relations = %d, want 1", len(g1))
	}
	g2, err := s.RelationsByGraph(2)
	if err != nil {
		t.Fatalf("RelationsByGraph(2): %v", err)
	}
	if len(g2) != 2 {
		t.Fatalf("graph 2 relations = %d, want 2", len(g2))
	}

	// Deleting graph 2 removes only its edges; events and graph 1 survive.
	n, err := s.DeleteGraphRelations(2)
	if err != nil {
		t.Fatalf("DeleteGraphRelations(2): %v", err)
	}
	if n != 2 {
		t.Errorf("deleted %d, want 2", n)
	}
	if left, _ := s.RelationsByGraph(2); len(left) != 0 {
		t.Errorf("graph 2 still has %d relations", len(left))
	}
	if still, _ := s.RelationsByGraph(1); len(still) != 1 {
		t.Errorf("graph 1 relations = %d, want 1 (untouched)", len(still))
	}
	nodes, _, _ := s.Stats()
	if nodes != 3 {
		t.Errorf("events = %d, want 3 (never deleted)", nodes)
	}
}

// TestMigrateRelationsToGraph verifies legacy relations (graph_id == 0) are assigned to
// the default graph and become queryable, and that migration is idempotent.
func TestMigrateRelationsToGraph(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("1", "P", "Security", "alice", base, "a"),
		mkEvent("2", "P", "Security", "bob", base.Add(time.Minute), "b"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	// A pre-P15 relation carries no graph id.
	if _, err := s.InsertRelation(mkRel(ids[0], ids[1], 0)); err != nil {
		t.Fatalf("insert relation: %v", err)
	}

	migrated, err := s.MigrateRelationsToGraph(consts.DefaultGraphID)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if migrated != 1 {
		t.Fatalf("migrated %d, want 1", migrated)
	}
	if got, _ := s.RelationsByGraph(consts.DefaultGraphID); len(got) != 1 {
		t.Fatalf("default graph relations = %d, want 1", len(got))
	}

	// Idempotent: a second migration moves nothing.
	again, err := s.MigrateRelationsToGraph(consts.DefaultGraphID)
	if err != nil {
		t.Fatalf("migrate 2: %v", err)
	}
	if again != 0 {
		t.Errorf("second migration moved %d, want 0", again)
	}
}
