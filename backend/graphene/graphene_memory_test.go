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
	// Payload now lives outside the node record, so this bounds index + topology only.
	const ceiling = 2200
	if perEvent > ceiling {
		t.Errorf("store holds ~%.0f B per event, above the %d B regression ceiling", perEvent, ceiling)
	}
}

// TestStoreMemoryIsFlatInRecordSize pins the property that makes a large case survivable:
// an event's resident cost no longer depends on how big its raw record is.
//
// It used to. The raw record and parsed fields lived in the node blob, the graph holds its
// records in memory, and so a 64x larger record cost 4.6x the memory — ~7.8 kB per event at
// a 4 kB payload. Moving both fields to the payload cold store made the cost flat: the node
// carries a reference, and the bytes are read only when one event is opened.
//
// If this test starts failing, something has been put back into the node record that scales
// with the source data. That is the regression worth catching, because it is invisible until
// a case gets big.
func TestStoreMemoryIsFlatInRecordSize(t *testing.T) {
	if testing.Short() {
		t.Skip("memory profile skipped in -short")
	}

	const n = 30000

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
	t.Logf("64 B record: ~%.0f B/event; 4096 B record: ~%.0f B/event (record 64x larger, memory %.2fx)",
		smallPer, largePer, largePer/smallPer)

	// Flat, not merely sub-linear. A 64x record increase may move this a little through
	// allocator noise, but anything approaching a doubling means payload bytes are resident
	// again.
	if largePer > smallPer*1.5 {
		t.Errorf("memory scales with record size again (%.0f -> %.0f B/event for a 64x larger record); "+
			"something bulky is back in the node record", smallPer, largePer)
	}
}
