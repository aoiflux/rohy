package graphene

import (
	"fmt"
	"testing"
	"time"

	"rohy/backend/consts"
)

// Benchmarks for the paths the v0.3.0 adoption actually changed. The pre-existing
// benchmarks cover unfiltered paging, which none of these changes touch — so on their own
// they cannot show whether the adoption was worth anything.

// benchBase is the timestamp seedBench starts from; range bounds are derived from it so a
// window is a known fraction of the seeded set rather than a guess.
var benchBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// BenchmarkQueryEventsTimeRangeNarrow measures a small time window over a large set — the
// case an ordered key is supposed to turn from a scan of every timestamp entry into a
// binary search plus a short walk.
func BenchmarkQueryEventsTimeRangeNarrow(b *testing.B) {
	s := seedBench(b, 20000)
	// seedBench spaces events one second apart, so this is ~100 of 20 000.
	from := benchBase.Add(5000 * time.Second)
	to := from.Add(100 * time.Second)
	f := EventFilter{TimeFrom: &from, TimeTo: &to, Limit: consts.EventBatchSize}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.QueryEvents(f); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkQueryEventsTimeRangeWide measures a window covering about half the set, where
// the driver cannot help as much and residual work dominates.
func BenchmarkQueryEventsTimeRangeWide(b *testing.B) {
	s := seedBench(b, 20000)
	from := benchBase
	to := from.Add(10000 * time.Second)
	f := EventFilter{TimeFrom: &from, TimeTo: &to, Limit: consts.EventBatchSize}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.QueryEvents(f); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCountEventsTimeRange measures the count that accompanies a filtered load, which
// has no limit to cut it short and so pays the driver's full cost.
func BenchmarkCountEventsTimeRange(b *testing.B) {
	s := seedBench(b, 20000)
	from := benchBase.Add(5000 * time.Second)
	to := from.Add(100 * time.Second)
	f := EventFilter{TimeFrom: &from, TimeTo: &to}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.CountEvents(f); err != nil {
			b.Fatal(err)
		}
	}
}

// seedRelationTargets inserts n events and returns their ids, for benchmarks that write
// edges between existing nodes.
func seedRelationTargets(tb testing.TB, n int) (*Store, []uint64) {
	tb.Helper()
	s := OpenInMemory()
	tb.Cleanup(func() { s.Close() })

	events := make([]*Event, 0, n)
	for i := range n {
		events = append(events, &Event{
			EventID:            fmt.Sprintf("4%03d", i%50),
			Timestamp:          benchBase.Add(time.Duration(i) * time.Second),
			Provider:           "Microsoft-Windows-Security-Auditing",
			Channel:            "Security",
			Computer:           "HOST-1",
			HashNormalized:     fmt.Sprintf("h%d", i),
			DeduplicationCount: 1,
		})
	}
	ids, err := s.InsertEvents(events)
	if err != nil {
		tb.Fatal(err)
	}
	return s, ids
}

// mkRelations builds n chained relations over the given event ids.
func mkRelations(ids []uint64, n int, graphID uint64) []*Relation {
	rels := make([]*Relation, 0, n)
	for i := range n {
		rels = append(rels, &Relation{
			From:         ids[i%(len(ids)-1)],
			To:           ids[(i%(len(ids)-1))+1],
			GraphID:      graphID,
			RelationType: consts.RelationCorrelation,
			CreatedBy:    consts.CreatedBySystem,
		})
	}
	return rels
}

// BenchmarkInsertRelationsBatched measures the rule-generated graph write path as it now
// is: one commit per chunk of relations.
func BenchmarkInsertRelationsBatched(b *testing.B) {
	const n = 1000
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, ids := seedRelationTargets(b, 200)
		rels := mkRelations(ids, n, 1)
		b.StartTimer()
		if _, err := s.InsertRelations(rels); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInsertRelationsIndividually measures the path graphbuild used to take — one
// durable write per edge — so the batch's benefit is a measured difference rather than a
// claim inherited from the upstream benchmark table.
func BenchmarkInsertRelationsIndividually(b *testing.B) {
	const n = 1000
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, ids := seedRelationTargets(b, 200)
		rels := mkRelations(ids, n, 1)
		b.StartTimer()
		for _, r := range rels {
			if _, err := s.InsertRelation(r); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// seedRelationTargetsDisk is seedRelationTargets on the DISK backend.
//
// The in-memory variants above understate the write-path changes badly, and it would be
// easy to read them as showing the batching was not worth doing. On disk every commit must
// reach the write-ahead log before it returns, so the number of commits — not the work per
// record — is what dominates. That is the configuration a real case runs in, so it is the
// one the write-path claims have to be measured against.
func seedRelationTargetsDisk(tb testing.TB, n int) (*Store, []uint64) {
	tb.Helper()
	s, err := Open(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { s.Close() })

	events := make([]*Event, 0, n)
	for i := range n {
		events = append(events, &Event{
			EventID:            fmt.Sprintf("4%03d", i%50),
			Timestamp:          benchBase.Add(time.Duration(i) * time.Second),
			Provider:           "Microsoft-Windows-Security-Auditing",
			Channel:            "Security",
			Computer:           "HOST-1",
			HashNormalized:     fmt.Sprintf("h%d", i),
			DeduplicationCount: 1,
		})
	}
	ids, err := s.InsertEvents(events)
	if err != nil {
		tb.Fatal(err)
	}
	return s, ids
}

// BenchmarkInsertRelationsBatchedDisk measures the graph write path as it now is, on the
// backend where the commit count actually costs something.
func BenchmarkInsertRelationsBatchedDisk(b *testing.B) {
	const n = 1000
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, ids := seedRelationTargetsDisk(b, 200)
		rels := mkRelations(ids, n, 1)
		b.StartTimer()
		if _, err := s.InsertRelations(rels); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInsertRelationsIndividuallyDisk measures the path graphbuild used to take, on
// disk: one durable write per edge.
func BenchmarkInsertRelationsIndividuallyDisk(b *testing.B) {
	const n = 1000
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, ids := seedRelationTargetsDisk(b, 200)
		rels := mkRelations(ids, n, 1)
		b.StartTimer()
		for _, r := range rels {
			if _, err := s.InsertRelation(r); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkDeleteGraphRelations measures the idempotent rebuild's clear step, which now
// commits once for the whole graph.
func BenchmarkDeleteGraphRelations(b *testing.B) {
	const n = 1000
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, ids := seedRelationTargets(b, 200)
		if _, err := s.InsertRelations(mkRelations(ids, n, 1)); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := s.DeleteGraphRelations(1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRelationsAdjacency measures the per-visible-row relation decoration on the
// events list — the path that reads incident edges without an edge-type filter.
func BenchmarkRelationsAdjacency(b *testing.B) {
	s, ids := seedRelationTargets(b, 200)
	if _, err := s.InsertRelations(mkRelations(ids, 1000, 1)); err != nil {
		b.Fatal(err)
	}
	window := ids[:100]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.RelationsAdjacency(window); err != nil {
			b.Fatal(err)
		}
	}
}
