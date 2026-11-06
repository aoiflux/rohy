package graphene

import (
	"testing"

	"rohy/backend/consts"
)

// 🔒 A crash part way through registering a relation's index entries leaves it findable one way and
// not another, because the engine appends one write-ahead record per key. The integrity count and
// the repair must look at every key the relation is indexed under, not only graph_id: a relation
// graph_id finds but rule_id does not is missing from "this rule's edges", and nothing repaired it.
func TestAHalfIndexedRelationIsCountedAndRepaired(t *testing.T) {
	for name, keys := range map[string][]string{
		"none":               nil,
		"only graph_id":      {consts.PropGraphID},
		"only rule_id":       {consts.PropRuleID},
		"all but created_by": {consts.PropGraphID, consts.PropRuleID, consts.PropRelationType},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			a, b := seedTwoEvents(t, s)
			r := &Relation{
				From: a, To: b, GraphID: 4, RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
				RuleID: "rule-x", Algorithm: consts.AlgoSequence, MatchID: "m-1", Basis: []string{"why"},
			}
			if err := s.InsertRelationPartlyIndexedForTesting(r, keys...); err != nil {
				t.Fatal(err)
			}
			// The crash: closed without compacting, then opened again.
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if s, err = Open(dir); err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			if n, err := s.CountUnindexedRelations(); err != nil || n != 1 {
				t.Fatalf("counted %d unindexed (err %v), want 1", n, err)
			}
			if n, err := s.RepairRelationIndex(); err != nil || n != 1 {
				t.Fatalf("repaired %d (err %v), want 1", n, err)
			}
			byRule, err := s.RelationsByRule("rule-x")
			if err != nil {
				t.Fatal(err)
			}
			byGraph, err := s.RelationsByGraph(4)
			if err != nil {
				t.Fatal(err)
			}
			if len(byRule) != 1 || len(byGraph) != 1 {
				t.Fatalf("after repair: %d by rule, %d by graph; want 1 and 1", len(byRule), len(byGraph))
			}
			if got := byRule[0]; got.MatchID != "m-1" || got.Algorithm != consts.AlgoSequence || len(got.Basis) != 1 {
				t.Errorf("provenance lost: %+v", got)
			}
			if n, _ := s.CountUnindexedRelations(); n != 0 {
				t.Errorf("still %d unindexed after repair", n)
			}
			if n, _ := s.RepairRelationIndex(); n != 0 {
				t.Errorf("a second repair registered %d", n)
			}
			if err := s.VerifyIndexes(); err != nil {
				t.Errorf("indexes inconsistent after repair: %v", err)
			}
		})
	}
}

// Closing a store twice is safe: the second close finds nothing to close.
func TestClosingAStoreTwiceIsSafe(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedTwoEvents(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
