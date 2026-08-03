package api

import (
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
)

// inspectFixture builds a store holding one rule-created occurrence of three events (so two
// edges share a match id) plus an unrelated hand-drawn edge.
func inspectFixture(t *testing.T) (*GraphAPI, []uint64) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	base := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	events := []*graphene.Event{
		{EventID: "4625", Computer: "HOST-A", Timestamp: base, HashNormalized: "e1"},
		{EventID: "4625", Computer: "HOST-A", Timestamp: base.Add(time.Second), HashNormalized: "e2"},
		{EventID: "4624", Computer: "HOST-A", Timestamp: base.Add(2 * time.Second), HashNormalized: "e3"},
	}
	if _, err := store.InsertEvents(events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	registry, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatalf("graphreg: %v", err)
	}
	g, err := registry.EnsureForRule("brute-force", "Brute Force Then Success", "", time.Now())
	if err != nil {
		t.Fatalf("ensure graph: %v", err)
	}

	// Two edges of ONE occurrence.
	var ids []uint64
	for i, pair := range [][2]uint64{{events[0].ID, events[1].ID}, {events[1].ID, events[2].ID}} {
		rel := &graphene.Relation{
			From: pair[0], To: pair[1], GraphID: g.ID,
			RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
		}
		rel.StampProvenance("brute-force", consts.AlgoSequence, "m-1-3-3", i, []string{"logon_id=0x3e7"})
		if _, err := store.InsertRelation(rel); err != nil {
			t.Fatalf("insert relation: %v", err)
		}
		ids = append(ids, rel.ID)
	}

	// And one a human drew, which belongs to no occurrence.
	manual := &graphene.Relation{
		From: events[0].ID, To: events[2].ID, GraphID: g.ID,
		RelationType: consts.RelationDefault, CreatedBy: consts.CreatedByUser,
	}
	if _, err := store.InsertRelation(manual); err != nil {
		t.Fatalf("insert manual: %v", err)
	}
	ids = append(ids, manual.ID)

	return NewGraphAPI(store, nil, registry), ids
}

func TestInspectRelationResolvesEverythingInOneCall(t *testing.T) {
	api, ids := inspectFixture(t)

	got, err := api.InspectRelation(ids[0])
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if got.From == nil || got.To == nil {
		t.Fatal("endpoints not resolved — the inspector shows them, so it must not need a second call")
	}
	if got.From.EventID != "4625" || got.To.EventID != "4625" {
		t.Errorf("endpoints = %s -> %s", got.From.EventID, got.To.EventID)
	}
	if got.GraphName != "Brute Force Then Success" {
		t.Errorf("graph name = %q", got.GraphName)
	}
	if !got.Recorded {
		t.Error("a rule-created edge carries provenance and must report it")
	}
	if got.Relation.RuleID != "brute-force" || got.Relation.StepIndex != 0 {
		t.Errorf("provenance = %+v", got.Relation)
	}
	if len(got.Relation.Basis) != 1 {
		t.Errorf("basis = %v, want the reason the edge exists", got.Relation.Basis)
	}
}

// TestInspectRelationFindsTheRestOfTheMatch is the shared-selection payoff: from any one edge,
// the canvas can light up the whole occurrence. "What else is part of this chain?" is the
// question an analyst actually has when looking at one link of it.
func TestInspectRelationFindsTheRestOfTheMatch(t *testing.T) {
	api, ids := inspectFixture(t)

	got, err := api.InspectRelation(ids[0])
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(got.SiblingIDs) != 1 || got.SiblingIDs[0] != ids[1] {
		t.Fatalf("siblings = %v, want [%d] — the other edge of the same occurrence", got.SiblingIDs, ids[1])
	}
	// And never itself: an edge is not its own sibling, and highlighting it twice would make a
	// two-edge match look like a three-edge one.
	for _, s := range got.SiblingIDs {
		if s == ids[0] {
			t.Error("an edge was returned as its own sibling")
		}
	}
}

// TestInspectHandDrawnRelationHasNoOccurrence pins the distinction the inspector must draw: a
// human-asserted edge is not a rule-inferred one with missing fields.
func TestInspectHandDrawnRelationHasNoOccurrence(t *testing.T) {
	api, ids := inspectFixture(t)

	got, err := api.InspectRelation(ids[2])
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if got.Relation.CreatedBy != consts.CreatedByUser {
		t.Fatalf("created_by = %q, want user", got.Relation.CreatedBy)
	}
	if len(got.SiblingIDs) != 0 {
		t.Errorf("a hand-drawn edge belongs to no occurrence, got siblings %v", got.SiblingIDs)
	}
	if got.Relation.RuleID != "" {
		t.Errorf("a hand-drawn edge has no rule, got %q", got.Relation.RuleID)
	}
}

// TestInspectLegacyRelationReportsUnrecorded is the compatibility statement. An edge written
// before provenance existed must be distinguishable from one whose rule recorded no basis —
// "we did not track this yet" and "there was nothing to say" are different claims.
func TestInspectLegacyRelationReportsUnrecorded(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	a := &graphene.Event{EventID: "1", Computer: "H", HashNormalized: "la"}
	b := &graphene.Event{EventID: "2", Computer: "H", HashNormalized: "lb"}
	if _, err := store.InsertEvents([]*graphene.Event{a, b}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	legacy := &graphene.Relation{From: a.ID, To: b.ID, GraphID: 1, CreatedBy: consts.CreatedBySystem}
	if _, err := store.InsertRelation(legacy); err != nil {
		t.Fatalf("insert relation: %v", err)
	}

	got, err := NewGraphAPI(store, nil, nil).InspectRelation(legacy.ID)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if got.Recorded {
		t.Error("an edge written before provenance existed must report itself as unrecorded")
	}
}

func TestInspectMissingRelationIsAnError(t *testing.T) {
	api, _ := inspectFixture(t)
	if _, err := api.InspectRelation(999999); err == nil {
		t.Fatal("inspecting an edge that does not exist must be an error, not an empty detail")
	}
}
