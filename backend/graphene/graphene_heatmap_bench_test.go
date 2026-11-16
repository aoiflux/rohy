package graphene

import (
	"fmt"
	"testing"

	"rohy/backend/consts"
)

// Relationship heatmap cost (plan §11.2, PERFORMANCE.md §21).
//
// WHAT THIS DECIDES. RelationHeatmap places each relation at its later endpoint's time, and to
// find those times it scans every event's sort view once (matchingRows) rather than looking each
// endpoint up. The plan's claim is that it therefore "reuses the timeline path's cost profile".
// The claim needs the two measured side by side on the same store: the timeline is the baseline,
// and the heatmap should cost about one timeline plus reading its relations — whatever the graph.
//
//	go test ./backend/graphene/ -run XXX -bench 'RelationHeatmap' -benchtime 5x

const (
	heatmapBenchGraphs = 20
	// heatmapBenchEventsPerRelation is the number of events per relation: one relation for every
	// two events, a heavily correlated case.
	heatmapBenchEventsPerRelation = 2
)

// seedHeatmapDiskStore writes n events and n/2 rule-made relations spread across
// heatmapBenchGraphs graphs, compacts, and returns the open store.
func seedHeatmapDiskStore(b *testing.B, n int) *Store {
	b.Helper()
	dir := seedDiskStore(b, n, false)
	s, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })

	// In time order, so each relation joins two neighbouring events, as a sequence rule's would.
	ids, err := s.orderedIDs(EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		b.Fatal(err)
	}
	if len(ids) != n {
		b.Fatalf("seeded %d events, found %d", n, len(ids))
	}
	batch := make([]*Relation, 0, consts.RelationBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if _, err := s.InsertRelations(batch); err != nil {
			b.Fatal(err)
		}
		batch = batch[:0]
	}
	for i := 0; i+1 < len(ids); i += heatmapBenchEventsPerRelation {
		graph := uint64(i/heatmapBenchEventsPerRelation%heatmapBenchGraphs) + 1
		rel := &Relation{
			From: ids[i], To: ids[i+1], GraphID: graph,
			RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
		}
		rel.StampProvenance(fmt.Sprintf("rule-%d", graph), consts.AlgoSequence,
			fmt.Sprintf("m-%d", i), 0, nil)
		batch = append(batch, rel)
		if len(batch) == consts.RelationBatchSize {
			flush()
		}
	}
	flush()
	if err := s.Compact(); err != nil {
		b.Fatal(err)
	}
	return s
}

func BenchmarkRelationHeatmap100k(b *testing.B) {
	const n = 100000
	s := seedHeatmapDiskStore(b, n)
	all := EventFilter{Undated: consts.UndatedInclude}

	// The baseline: the timeline histogram over the same case.
	b.Run("timeline", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sum, err := s.Timeline(all, 0)
			if err != nil {
				b.Fatal(err)
			}
			if sum.Dated != n {
				b.Fatalf("timeline dated %d, want %d", sum.Dated, n)
			}
		}
	})

	cases := []struct {
		name    string
		graph   uint64
		want    int
		groupBy string
	}{
		{"case-by-rule", 0, n / heatmapBenchEventsPerRelation, consts.HeatmapGroupRule},
		{"case-by-computer", 0, n / heatmapBenchEventsPerRelation, consts.HeatmapGroupComputer},
		{"one-graph", 1, n / heatmapBenchEventsPerRelation / heatmapBenchGraphs, consts.HeatmapGroupRule},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sum, err := s.RelationHeatmap(HeatmapQuery{GraphID: c.graph, GroupBy: c.groupBy})
				if err != nil {
					b.Fatal(err)
				}
				// A heatmap that placed nothing would be measuring an early return.
				if sum.Total != c.want || sum.Placed != c.want {
					b.Fatalf("heatmap total %d placed %d, want %d", sum.Total, sum.Placed, c.want)
				}
			}
		})
	}
}
