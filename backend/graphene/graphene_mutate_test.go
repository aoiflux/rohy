package graphene

import (
	"errors"
	"testing"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

func seedTwoEvents(t *testing.T, s *Store) (uint64, uint64) {
	t.Helper()
	ids, err := s.InsertEvents([]*Event{
		{EventID: "4624", Timestamp: time.Now(), Channel: consts.ChannelSecurity, HashNormalized: "h1"},
		{EventID: "4634", Timestamp: time.Now(), Channel: consts.ChannelSecurity, HashNormalized: "h2"},
	})
	if err != nil || len(ids) != 2 {
		t.Fatalf("seed events: %v (n=%d)", err, len(ids))
	}
	return ids[0], ids[1]
}

func TestUpdateRelation(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	a, b := seedTwoEvents(t, s)

	rel := &Relation{From: a, To: b, RelationType: consts.RelationDefault, Label: "first"}
	id, err := s.InsertRelation(rel)
	if err != nil {
		t.Fatal(err)
	}

	// Edit type + label in place.
	rel.RelationType = consts.RelationTemporal
	rel.Label = "same session"
	if err := s.UpdateRelation(rel); err != nil {
		t.Fatalf("UpdateRelation: %v", err)
	}

	rels, err := s.GetRelations()
	if err != nil || len(rels) != 1 {
		t.Fatalf("GetRelations: %v (n=%d)", err, len(rels))
	}
	got := rels[0]
	if got.ID != id || got.RelationType != consts.RelationTemporal || got.Label != "same session" {
		t.Errorf("update not applied: %+v", got)
	}
	// Endpoints must be preserved.
	if got.From != a || got.To != b {
		t.Errorf("endpoints changed: from=%d to=%d, want %d/%d", got.From, got.To, a, b)
	}
}

func TestDeleteRelation(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	a, b := seedTwoEvents(t, s)

	id, err := s.InsertRelation(&Relation{From: a, To: b, RelationType: consts.RelationDefault})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRelation(id); err != nil {
		t.Fatalf("DeleteRelation: %v", err)
	}
	rels, _ := s.GetRelations()
	if len(rels) != 0 {
		t.Errorf("relation still present after delete: %d", len(rels))
	}

	// Deleting again is a not-found (callers may treat as idempotent).
	err = s.DeleteRelation(id)
	var nf *store.ErrNotFound
	if err == nil || !errors.As(err, &nf) {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestDeleteEventCascadesRelations(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	a, b := seedTwoEvents(t, s)

	if _, err := s.InsertRelation(&Relation{From: a, To: b, RelationType: consts.RelationCorrelation}); err != nil {
		t.Fatal(err)
	}
	// Deleting an endpoint node must cascade-remove the incident relation.
	if err := s.DeleteEvent(a); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}

	nodes, edges, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if nodes != 1 {
		t.Errorf("node count = %d, want 1 after deleting one event", nodes)
	}
	if edges != 0 {
		t.Errorf("edge count = %d, want 0 (relation should have cascaded)", edges)
	}
}
