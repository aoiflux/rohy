package graphene

import (
	"fmt"
	"testing"

	"rohy/backend/consts"
)

// seedRelations adds n rule-made relations over the store's first events, spread over 20 rules
// and 20 graphs, the shape a build leaves.
func seedRelations(tb testing.TB, s *Store, n int) {
	tb.Helper()
	evs, err := s.QueryEvents(EventFilter{Limit: 2000})
	if err != nil || len(evs) < 2 {
		tb.Fatalf("seed: %d events, %v", len(evs), err)
	}
	rels := make([]*Relation, 0, n)
	for i := range n {
		rels = append(rels, &Relation{
			From: evs[i%len(evs)].ID, To: evs[(i+1)%len(evs)].ID, GraphID: uint64(1 + i%20),
			RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
			RuleID: fmt.Sprintf("rule-%d", i%20), Algorithm: consts.AlgoSequence,
			MatchID: fmt.Sprintf("m-%d", i), Basis: []string{"logon_id=0x3e7"},
		})
	}
	if _, err := s.InsertRelations(rels); err != nil {
		tb.Fatal(err)
	}
}

// BenchmarkRepairRelationIndexHealthy is what every build pays before it runs: the repair over a
// healthy case, which registers nothing. It checks all four index keys of every relation.
func BenchmarkRepairRelationIndexHealthy(b *testing.B) {
	for _, n := range []int{10_000, 50_000} {
		b.Run(fmt.Sprintf("%drelations", n), func(b *testing.B) {
			s := seedBench(b, 2000)
			seedRelations(b, s, n)
			b.ReportAllocs()
			for b.Loop() {
				if got, err := s.RepairRelationIndex(); err != nil || got != 0 {
					b.Fatalf("repaired %d, %v", got, err)
				}
			}
		})
	}
}
