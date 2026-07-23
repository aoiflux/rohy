package graphene

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

func mkEvent(eventID, provider, channel, user string, ts time.Time, hash string) *Event {
	return &Event{
		EventID:        eventID,
		Timestamp:      ts,
		Provider:       provider,
		Channel:        channel,
		Computer:       "HOST-1",
		User:           user,
		RawXML:         "<Event/>",
		ParsedFields:   map[string]string{"k": "v"},
		HashRaw:        "raw-" + hash,
		HashNormalized: hash,
	}
}

func TestInsertQueryRoundTrip(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	events := []*Event{
		mkEvent("4624", "Microsoft-Windows-Security-Auditing", "Security", "alice", base.Add(2*time.Hour), "h2"),
		mkEvent("4625", "Microsoft-Windows-Security-Auditing", "Security", "bob", base.Add(1*time.Hour), "h1"),
		mkEvent("1000", "Application Error", "Application", "alice", base.Add(3*time.Hour), "h3"),
	}
	ids, err := s.InsertEvents(events)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("want 3 ids, got %d", len(ids))
	}

	// Chronological ordering (timestamp lexicographic index correctness).
	all, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatalf("query all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 events, got %d", len(all))
	}
	if all[0].EventID != "4625" || all[1].EventID != "4624" || all[2].EventID != "1000" {
		t.Fatalf("wrong chronological order: %s,%s,%s", all[0].EventID, all[1].EventID, all[2].EventID)
	}

	// Equality filter by channel.
	sec, err := s.QueryEvents(EventFilter{Channel: "Security"})
	if err != nil {
		t.Fatalf("query channel: %v", err)
	}
	if len(sec) != 2 {
		t.Fatalf("want 2 Security events, got %d", len(sec))
	}

	// Time-range filter (between inclusive).
	from := base.Add(90 * time.Minute)
	to := base.Add(150 * time.Minute)
	rng, err := s.QueryEvents(EventFilter{TimeFrom: &from, TimeTo: &to})
	if err != nil {
		t.Fatalf("query range: %v", err)
	}
	if len(rng) != 1 || rng[0].EventID != "4624" {
		t.Fatalf("time range wrong: %+v", rng)
	}

	// Substring search over the search blob.
	found, err := s.QueryEvents(EventFilter{Search: "application"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found) != 1 || found[0].EventID != "1000" {
		t.Fatalf("search wrong: %+v", found)
	}

	// Dedup lookup by normalized hash.
	gotID, ok, err := s.FindEventIDByHash("h1")
	if err != nil || !ok {
		t.Fatalf("hash lookup: id=%d ok=%v err=%v", gotID, ok, err)
	}
}

func TestRelationRoundTrip(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("4624", "P", "Security", "alice", base, "a"),
		mkEvent("4672", "P", "Security", "alice", base.Add(time.Minute), "b"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rel := &Relation{
		From:            ids[0],
		To:              ids[1],
		RelationType:    consts.RelationTemporal,
		ConfidenceScore: 0.9,
		CreatedBy:       consts.CreatedByUser,
		CreatedAt:       base,
	}
	if _, err := s.InsertRelation(rel); err != nil {
		t.Fatalf("insert relation: %v", err)
	}

	rels, err := s.GetRelations()
	if err != nil || len(rels) != 1 {
		t.Fatalf("get relations: n=%d err=%v", len(rels), err)
	}
	if rels[0].RelationType != consts.RelationTemporal || rels[0].From != ids[0] || rels[0].To != ids[1] {
		t.Fatalf("relation mismatch: %+v", rels[0])
	}

	inc, err := s.RelationsOf(ids[0])
	if err != nil || len(inc) != 1 {
		t.Fatalf("relations of: n=%d err=%v", len(inc), err)
	}
}

func TestRelationsAdjacency(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("1", "P", "Security", "alice", base, "a"),
		mkEvent("2", "P", "Security", "alice", base.Add(time.Minute), "b"),
		mkEvent("3", "P", "Security", "bob", base.Add(2*time.Minute), "c"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// 1—2 (temporal) and 1—3 (correlation); node 2 also links to 3 (temporal).
	for _, r := range []*Relation{
		{From: ids[0], To: ids[1], RelationType: consts.RelationTemporal, CreatedBy: consts.CreatedByUser, CreatedAt: base},
		{From: ids[0], To: ids[2], RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedByUser, CreatedAt: base},
		{From: ids[1], To: ids[2], RelationType: consts.RelationTemporal, CreatedBy: consts.CreatedByUser, CreatedAt: base},
	} {
		if _, err := s.InsertRelation(r); err != nil {
			t.Fatalf("insert relation: %v", err)
		}
	}

	adj, err := s.RelationsAdjacency(ids)
	if err != nil {
		t.Fatalf("adjacency: %v", err)
	}

	// Node 1 touches 2 and 3, with two distinct types.
	a1 := adj[ids[0]]
	if a1 == nil || a1.Count != 2 {
		t.Fatalf("node1 adjacency = %+v, want count 2", a1)
	}
	if len(a1.Types) != 2 {
		t.Errorf("node1 types = %v, want 2 distinct", a1.Types)
	}
	if len(a1.RelatedIDs) != 2 {
		t.Errorf("node1 neighbors = %v, want 2", a1.RelatedIDs)
	}

	// Node 3 is touched by both 1 and 2.
	if a3 := adj[ids[2]]; a3 == nil || a3.Count != 2 || len(a3.RelatedIDs) != 2 {
		t.Errorf("node3 adjacency = %+v, want count 2 / 2 neighbors", a3)
	}
}

func TestRelationsAdjacencyOmitsUnrelated(t *testing.T) {
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

	// No relations at all → adjacency map is empty (unrelated ids omitted).
	adj, err := s.RelationsAdjacency(ids)
	if err != nil {
		t.Fatalf("adjacency: %v", err)
	}
	if len(adj) != 0 {
		t.Fatalf("want empty adjacency, got %d entries", len(adj))
	}
}
