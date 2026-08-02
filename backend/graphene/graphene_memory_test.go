package graphene

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
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

// --- Correlation projection budget (v0.2.0) ---
//
// The projection puts a dozen EventData scalars back INTO the node record, which is the one
// place this codebase has spent real effort emptying: moving the raw record out took an event
// from 3137 to 867 resident bytes. Adding fields back is therefore a decision that has to be
// paid for in a measurement, not argued for in a comment.
//
// The budget is enforced against the ENCODED NODE RECORD rather than against the heap, and
// that choice is deliberate. The heap tests above measure a ~1000 B/event base with GC timing
// noise comfortably larger than the 200 B this feature is allowed to cost — they could not
// resolve a breach, and a test that cannot fail for the reason it exists is worse than no
// test. The properties blob IS what the graph holds resident per node, so measuring its size
// measures the actual quantity, exactly and without noise.

// encodedNodeBytes returns the size of the properties blob the store would persist for an
// event — the per-node cost the graph keeps in memory.
func encodedNodeBytes(t *testing.T, e *Event) int {
	t.Helper()
	n, err := e.toNode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return len(n.Properties)
}

// TestCorrelationProjectionBudget fails the build when the projection costs more per event
// than it was budgeted.
//
// If this fails, the fix is to SHRINK THE VOCABULARY — drop a slot, or tighten
// CorrelationValueMaxLen — not to raise the ceiling. The whole reason this project has a
// PERFORMANCE.md is that a plausible-sounding optimisation was measured and reverted; the
// same discipline applies to a plausible-sounding feature.
func TestCorrelationProjectionBudget(t *testing.T) {
	base := &Event{
		EventID:            "4688",
		Timestamp:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Provider:           "Microsoft-Windows-Security-Auditing",
		Channel:            "Security",
		Computer:           "WORKSTATION-01",
		User:               "S-1-5-18",
		HashRaw:            strings.Repeat("a", 64),
		HashNormalized:     strings.Repeat("b", 64),
		SourceType:         "single_evtx_file",
		SourceIdentifier:   `C:\Windows\System32\winevt\Logs\Security.evtx`,
		DeduplicationCount: 1,
	}
	without := encodedNodeBytes(t, base)

	// The realistic case: a 4688 populates about a third of the vocabulary.
	typical := *base
	typical.ParsedFields = map[string]string{
		"SubjectUserName":   "alice",
		"SubjectLogonId":    "0x3E7",
		"NewProcessId":      "0x1A4",
		"NewProcessName":    `C:\Windows\System32\cmd.exe`,
		"ProcessId":         "0x2C8",
		"ParentProcessName": `C:\Windows\explorer.exe`,
	}
	typical.ComputeCorrelationKeys()
	typicalCost := encodedNodeBytes(t, &typical) - without

	// The worst case the format ALLOWS: every slot populated at the maximum value length.
	// This is the number the budget has to hold against, because it is what an adversarial
	// (or merely verbose) log can produce.
	worst := *base
	worst.CorrKeys = make([]string, consts.CorrelationSlotCount)
	for i := range worst.CorrKeys {
		worst.CorrKeys[i] = strings.Repeat("x", consts.CorrelationSlotMaxLen[i])
	}
	worst.CorrKeyVersion = consts.CorrelationKeyVersion
	worstCost := encodedNodeBytes(t, &worst) - without

	t.Logf("correlation projection: typical 4688 adds %d B/event, format worst case adds %d B/event "+
		"(baseline node record %d B)", typicalCost, worstCost, without)
	t.Logf("extrapolation at worst case: 1M events ~%.1f MB, 10M events ~%.1f MB",
		float64(worstCost)*1e6/(1<<20), float64(worstCost)*1e7/(1<<20))

	// The budget. Worst case, because a ceiling that only holds for the average is not a
	// ceiling.
	//
	// The figure is MEASURED, not chosen. The design opened with an estimate of 200 B, made
	// before any of this existed; the first run of this test returned 819 B for a twelve-slot
	// vocabulary sharing one 64-byte cap, which is most of the way back to the resident cost
	// that moving the raw record out of the node was supposed to remove. Two things came out
	// of that measurement, and both were the right change independently of the number:
	//
	//   - Slots are bounded to their own domain (consts.CorrelationSlotMaxLen). A 64-byte cap
	//     on a process id was never anything but slack.
	//   - parent_process_name and session_id left the vocabulary — one redundant with data
	//     already on the other endpoint of a lineage edge, one strictly less precise than
	//     logon_id.
	//
	// What is left costs ~370 B at a worst case no real Windows event reaches, and ~70 B on a
	// representative 4688. Against an 867 B/event baseline that is around +8% typical to make
	// field, temporal and lineage correlation possible at all, which is a trade worth making —
	// and unlike the estimate it replaced, it is a number this test will hold to.
	const budget = 400
	if worstCost > budget {
		t.Errorf("correlation projection costs %d B/event at the format's worst case, above the "+
			"%d B budget — shrink consts.CorrelationSlots or tighten consts.CorrelationSlotMaxLen "+
			"rather than raising this number", worstCost, budget)
	}

	// A projection stored by NAME instead of by position is the obvious-looking alternative
	// that this design rejected. Pinning the comparison keeps the reason present in the
	// codebase rather than only in a plan that will be deleted.
	named := make(map[string]string, consts.CorrelationSlotCount)
	for i, name := range consts.CorrelationSlots {
		named[name] = worst.CorrKeys[i]
	}
	namedBlob, err := json.Marshal(named)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("by-position costs %d B; the same values keyed by name would cost %d B (+%d B/event of pure vocabulary)",
		worstCost, len(namedBlob), len(namedBlob)-worstCost)
}

// TestCorrelationProjectionIsFlatInFieldCount pins the property that makes the budget hold:
// the cost tracks the VOCABULARY, not how many fields the source event happened to carry.
//
// An event with two hundred EventData entries must cost the same as one with six, because the
// projection selects a fixed set. If this fails, something is copying ParsedFields wholesale
// into the node record again — which is the exact regression the cold store exists to prevent,
// arriving through a new door.
func TestCorrelationProjectionIsFlatInFieldCount(t *testing.T) {
	mk := func(extra int) int {
		e := &Event{EventID: "4688", HashNormalized: "h"}
		fields := map[string]string{"SubjectLogonId": "0x3E7", "NewProcessId": "0x1A4"}
		for i := range extra {
			fields[fmt.Sprintf("Filler%d", i)] = strings.Repeat("v", 128)
		}
		e.ParsedFields = fields
		e.ComputeCorrelationKeys()
		return encodedNodeBytes(t, e)
	}
	few, many := mk(4), mk(200)
	t.Logf("6 EventData fields: %d B; 202 EventData fields: %d B", few, many)
	if many != few {
		t.Errorf("node record grew with EventData size (%d -> %d B); the projection must select "+
			"a fixed vocabulary, not carry the payload", few, many)
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
