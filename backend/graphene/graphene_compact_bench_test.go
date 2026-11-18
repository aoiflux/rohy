package graphene

import (
	"fmt"
	"testing"
	"time"
)

// Compaction at the ingest boundary — the 🟡 gate.
//
// WHAT THIS DECIDES. Compaction is what computes the snapshot roots, so until it runs nothing in a
// case can be proven. rohy currently compacts in exactly one place — clean shutdown — which means a
// case that was force-quit, crashed, or is simply still open has no roots for anything ingested
// this session. Moving it to the ingest boundary fixes that.
//
// The question is not what BenchmarkCompact already measures. Compacting a fresh 10k store says
// nothing about the case that matters, which is this one:
//
//	an analyst has a large case, and ingests one more log file into it
//
// Compaction rewrites the whole image, so its cost is proportional to the TOTAL store rather than
// to what was just added. If that holds, then compacting per FILE in a multi-file ingest pays the
// full cost once per file — ten files into a 100k case would be ten full compactions, and the
// marginal nine buy nothing, because only the last one's roots survive.
//
// So the two numbers wanted here are:
//
//  1. the full cost at each size, and
//  2. the cost of compacting after a SMALL increment on an already-compacted store.
//
// If (2) ≈ (1), compaction belongs at the end of an ingest REQUEST and never per file.

// addEvents appends n events to an open store, offset so they do not collide with the seed.
func addEvents(tb testing.TB, s *Store, n, offset int) {
	tb.Helper()
	base := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	batch := make([]*Event, 0, n)
	for i := range n {
		batch = append(batch, &Event{
			EventID:            fmt.Sprintf("4%03d", i%50),
			Timestamp:          base.Add(time.Duration(i) * time.Second),
			Provider:           "Microsoft-Windows-Security-Auditing",
			Channel:            "Security",
			Computer:           fmt.Sprintf("HOST-%d", i%8),
			User:               fmt.Sprintf("S-1-5-%d", i%20),
			RawXML:             "<Event><Data Name='TargetUserName'>alice</Data></Event>",
			HashNormalized:     fmt.Sprintf("inc-%d-%d", offset, i),
			DeduplicationCount: 1,
		})
	}
	if _, err := s.InsertEvents(batch); err != nil {
		tb.Fatal(err)
	}
}

// benchmarkCompactFull measures folding an entirely uncompacted store of n events.
func benchmarkCompactFull(b *testing.B, n int) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		dir := seedDiskStore(b, n, false)
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.Compact(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// benchmarkCompactIncrement is the ingest-boundary shape: an already-compacted store of n events,
// plus a small addition, compacted again.
//
// 🔒 The store is re-seeded per iteration rather than reused. A second compaction of an unchanged
// store is a different and much cheaper operation, and measuring that instead would report the
// ingest boundary as nearly free — the exact wrong answer.
func benchmarkCompactIncrement(b *testing.B, n, increment int) {
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		b.StopTimer()
		dir := seedDiskStore(b, n, true) // already compacted: the case the analyst arrives with
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		addEvents(b, s, increment, i)
		b.StartTimer()
		if err := s.Compact(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func BenchmarkCompactFull10k(b *testing.B)  { benchmarkCompactFull(b, 10000) }
func BenchmarkCompactFull100k(b *testing.B) { benchmarkCompactFull(b, 100000) }

// A 500-event increment is a realistic single small log file.
func BenchmarkCompactIncrement10k(b *testing.B)  { benchmarkCompactIncrement(b, 10000, 500) }
func BenchmarkCompactIncrement100k(b *testing.B) { benchmarkCompactIncrement(b, 100000, 500) }
