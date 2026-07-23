package graphene

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Store memory profile (2.5.1).
//
// The ingestion pipeline already proves its own peak memory does not scale with input size
// (see evtx.TestBoundedMemoryStreaming, which ingests through a discarding sink). That test
// deliberately does not measure the store, so nothing so far says what the STORE costs to
// hold — and that, not the parser, is what decides whether a very large case fits in memory.
//
// This measures bytes resident per event after ingest, which is the number to plan capacity
// against. It reports rather than merely asserting, because the useful output is a figure
// for extrapolation; the assertion is only a loose ceiling to catch a regression.

// residentBytes returns the live heap after forcing collection, so what is measured is what
// is still reachable rather than what happens to be uncollected.
func residentBytes() uint64 {
	// Twice: the first collection can leave finalizable objects reachable.
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// ingestForMemory fills a disk-backed store with n events and returns it, still open, so
// the caller can measure what holding it costs.
func ingestForMemory(t *testing.T, n int, rawXMLSize int) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Realistic printable XML. A filler of NUL bytes would be pathological rather than
	// representative: JSON escapes each one to a six-character sequence, so the encoded
	// blob would be six times the payload and the measurement would describe the fixture
	// instead of the store.
	const fragment = "<Data Name='TargetUserName'>alice</Data>"
	filler := strings.Repeat(fragment, max(1, rawXMLSize/len(fragment)))
	xml := "<Event>" + filler + "</Event>"

	const chunk = 1000
	batch := make([]*Event, 0, chunk)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if _, err := s.InsertEvents(batch); err != nil {
			t.Fatal(err)
		}
		batch = batch[:0]
	}
	for i := range n {
		batch = append(batch, &Event{
			EventID:            fmt.Sprintf("4%03d", i%50),
			Timestamp:          base.Add(time.Duration(i) * time.Second),
			Provider:           "Microsoft-Windows-Security-Auditing",
			Channel:            "Security",
			Computer:           fmt.Sprintf("HOST-%d", i%8),
			User:               fmt.Sprintf("S-1-5-%d", i%20),
			RawXML:             xml,
			HashNormalized:     fmt.Sprintf("h%d", i),
			DeduplicationCount: 1,
		})
		if len(batch) == chunk {
			flush()
		}
	}
	flush()
	return s
}

// TestStoreMemoryPerEvent reports what an open store costs per ingested event, and fails
// only if that cost has grown beyond a loose ceiling.
//
// The ceiling is deliberately generous. The point of this test is the number in its log
// output, which is what capacity planning needs; a tight bound would just make it flaky on
// a machine with different GC timing.
func TestStoreMemoryPerEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("memory profile skipped in -short")
	}

	const n = 50000
	before := residentBytes()
	s := ingestForMemory(t, n, 512)
	defer s.Close()
	after := residentBytes()

	if after < before {
		t.Skip("heap shrank during the run; measurement is not meaningful here")
	}
	perEvent := float64(after-before) / float64(n)
	t.Logf("store holds ~%.0f B per event (%d events, %.1f MB resident)",
		perEvent, n, float64(after-before)/(1<<20))
	t.Logf("extrapolation: 1M events ~%.1f GB, 10M events ~%.1f GB",
		perEvent*1e6/(1<<30), perEvent*1e7/(1<<30))

	// A regression ceiling, not a target. Upstream reports ~597 B per node for topology
	// plus a three-key property index on disk; rohy indexes nine keys, so a figure several
	// times that is expected. This catches an order-of-magnitude change, nothing finer.
	const ceiling = 4000
	if perEvent > ceiling {
		t.Errorf("store holds ~%.0f B per event, above the %d B regression ceiling", perEvent, ceiling)
	}
}

// TestStoreMemoryGrowsWithRecordSize separates the two things that grow with a case: the
// property index, and the record blob itself.
//
// The measured answer is that BOTH contribute — the store keeps node property blobs
// resident, RawXML included. That is the capacity constraint worth knowing about, because
// RawXML is by far the largest field an event carries, and it is retained even though
// almost nothing reads it: it is shown only when an analyst opens a single event.
//
// This test therefore does not assert that the record is free. It pins the shape that
// makes large cases survivable at all — that memory grows far more slowly than the record
// does — and logs the split so the fixed per-event floor stays visible.
func TestStoreMemoryGrowsWithRecordSize(t *testing.T) {
	if testing.Short() {
		t.Skip("memory profile skipped in -short")
	}

	const n = 30000

	// Same event count, small versus large RawXML. If memory tracked the record blob, the
	// second would be far larger; if it tracks the index, the two land close together.
	beforeSmall := residentBytes()
	small := ingestForMemory(t, n, 64)
	afterSmall := residentBytes()
	small.Close()

	beforeLarge := residentBytes()
	large := ingestForMemory(t, n, 4096)
	afterLarge := residentBytes()
	large.Close()

	if afterSmall < beforeSmall || afterLarge < beforeLarge {
		t.Skip("heap shrank during a run; measurement is not meaningful here")
	}
	smallPer := float64(afterSmall-beforeSmall) / float64(n)
	largePer := float64(afterLarge-beforeLarge) / float64(n)
	t.Logf("64 B RawXML: ~%.0f B/event; 4096 B RawXML: ~%.0f B/event (record is %d× larger, memory %.1f× larger)",
		smallPer, largePer, 4096/64, largePer/smallPer)
	t.Logf("implied fixed cost per event (index + topology): ~%.0f B", smallPer)

	// The record is retained, so memory does grow with it — but far more slowly than the
	// record itself. That gap is what makes a large case survivable at all, and losing it
	// would mean the store had started holding payloads proportionally.
	if largePer > smallPer*16 {
		t.Errorf("memory now tracks the record blob almost proportionally (%.0f → %.0f B/event "+
			"for a 64× larger record); large cases will not fit", smallPer, largePer)
	}
	// The floor must also stay a floor: if the fixed per-event cost climbs, every case gets
	// heavier regardless of payload size.
	const floorCeiling = 3000
	if smallPer > floorCeiling {
		t.Errorf("fixed cost is ~%.0f B/event with a tiny record, above the %d B ceiling", smallPer, floorCeiling)
	}
}
