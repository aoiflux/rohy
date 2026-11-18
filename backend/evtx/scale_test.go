package evtx

import (
	"context"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sync"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/synth"
)

// evtxChunkSize is fixed by the EVTX on-disk format (64 KiB per chunk). It lives here
// as a documented test constant only; production code never assumes it (it walks
// chunk offsets via the parser).
const evtxChunkSize = synth.ChunkSize

// synthEVTX writes a synthetic EVTX file at dst by replicating src's chunks `repeat` times
// (synth.EVTX). Replicated records share content (and hashes), which is fine for
// streaming/backpressure/batching tests (idempotency off); tests needing distinct records use the
// real fixture instead.
func synthEVTX(t *testing.T, src, dst string, repeat int) (chunks int) {
	t.Helper()
	chunks, err := synth.EVTX(src, dst, repeat)
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

// countingSink records counts and the largest batch it ever received, retaining NO
// events — so a memory measurement over it reflects the pipeline, not accumulated
// storage.
type countingSink struct {
	mu       sync.Mutex
	count    int
	maxBatch int
}

func (s *countingSink) InsertEvents(events []*graphene.Event) ([]uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(events) > s.maxBatch {
		s.maxBatch = len(events)
	}
	ids := make([]uint64, len(events))
	for i := range events {
		s.count++
		ids[i] = uint64(s.count)
	}
	return ids, nil
}

func (s *countingSink) FindEventIDByHash(string) (uint64, bool, error) { return 0, false, nil }

func (s *countingSink) IncrementDedupCounts(map[uint64]map[string]int) error { return nil }

// peakHeapDelta runs fn while sampling live heap, returning peak-minus-baseline bytes.
func peakHeapDelta(fn func()) uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	baseline := ms.HeapAlloc

	var peak uint64
	done := make(chan struct{})
	go func() {
		var m runtime.MemStats
		for {
			select {
			case <-done:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak {
				peak = m.HeapAlloc
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	fn()
	close(done)
	if peak < baseline {
		return 0
	}
	return peak - baseline
}

// TestBoundedMemoryStreaming is the bounded-memory gate: peak memory must NOT scale with input size.
//
// Two things about HOW it measures, both learned by this test failing a release build:
//
//  1. GOGC is pinned low for the duration. Peak HeapAlloc is a proxy for retention, and at the
//     default it is a poor one — Go lets the heap grow to roughly twice the live set before
//     collecting, so the sampled peak reflects accumulated GARBAGE as much as anything held. The
//     unpinned numbers across four input sizes came out 10, 20, 13, 15 MiB: not even monotonic.
//     Pinned, the same sizes give 4, 19, 18, 21 — a curve you can reason about.
//
//  2. BOTH runs are past the pipeline's steady state. The original test compared 15 against 60
//     repeats, and 15 is too small: the buffers, batch and channel have not filled, so it measured
//     a cold pipeline against a warm one and called the difference "scaling". Measured live heap
//     by input: 11k records -> 4 MiB, 44k -> 19 MiB, 89k -> 18 MiB, 177k -> 21 MiB. It rises to a
//     plateau and stays there, which is exactly what bounded streaming looks like — but only the
//     comparisons that start at 44k or above can see it.
//
// So the gate is 44k records against 177k: FOUR times the data for the same memory. Truly linear
// growth would show 4x here and cannot hide.
//
// Skipped in -short. The pinned GC and the larger fixture make this slower than most tests; that
// is the price of the assertion meaning something.
func TestBoundedMemoryStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test skipped in -short")
	}
	if raceEnabled {
		t.Skip("memory bounds are not meaningful under -race instrumentation")
	}
	defer debug.SetGCPercent(debug.SetGCPercent(10))

	dir := t.TempDir()
	smallPath := filepath.Join(dir, "small.evtx")
	largePath := filepath.Join(dir, "large.evtx")
	// Both past steady state; the large one carries 4x the records of the small.
	synthEVTX(t, fixtureSecurity, smallPath, 60)
	synthEVTX(t, fixtureSecurity, largePath, 240)

	run := func(path string) (*countingSink, uint64) {
		sink := &countingSink{}
		peak := peakHeapDelta(func() {
			_, err := Ingest(context.Background(), Options{Source: consts.SourceFile, Path: path}, sink, NoopReporter{})
			if err != nil {
				t.Fatalf("ingest %s: %v", path, err)
			}
		})
		return sink, peak
	}

	smallSink, smallPeak := run(smallPath)
	largeSink, largePeak := run(largePath)

	if largeSink.count != smallSink.count*4 {
		t.Fatalf("record count did not scale as expected: small=%d large=%d", smallSink.count, largeSink.count)
	}

	const ceiling = 96 << 20 // 96 MiB — far below the ~160 MB large file fully buffered
	if largePeak > ceiling {
		t.Errorf("peak heap %d MiB exceeds ceiling; memory not bounded", largePeak>>20)
	}
	// Measured ratio is ~1.1x. Two is generous headroom for a slow or contended runner while
	// still failing long before the 4x that genuine linear growth would produce.
	if smallPeak > 0 && largePeak > smallPeak*2 {
		t.Errorf("peak heap scaled with input: small=%d MiB large=%d MiB for 4x the records (want flat)",
			smallPeak>>20, largePeak>>20)
	}
	t.Logf("records small=%d large=%d; peak live heap small=%d MiB large=%d MiB",
		smallSink.count, largeSink.count, smallPeak>>20, largePeak>>20)
}

// TestBatchedWritesNoLargeTransaction is the batched-writes gate: the sink must only ever
// receive batches bounded by the configured batch size — never one giant write.
func TestBatchedWritesNoLargeTransaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "many.evtx")
	synthEVTX(t, fixtureSecurity, path, 8)

	const batch = 128
	sink := &countingSink{}
	_, err := Ingest(context.Background(), Options{Source: consts.SourceFile, Path: path, BatchSize: batch}, sink, NoopReporter{})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if sink.count == 0 {
		t.Fatal("nothing persisted")
	}
	if sink.maxBatch > batch {
		t.Errorf("max batch %d exceeds configured batch size %d (large transaction)", sink.maxBatch, batch)
	}
}

// cancelSink wraps a real store and cancels the run once `after` events have been
// persisted, simulating an interruption mid-ingestion.
type cancelSink struct {
	store  *graphene.Store
	cancel context.CancelFunc
	after  int
	n      int
	mu     sync.Mutex
}

func (s *cancelSink) InsertEvents(events []*graphene.Event) ([]uint64, error) {
	ids, err := s.store.InsertEvents(events)
	s.mu.Lock()
	s.n += len(events)
	if s.n >= s.after && s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()
	return ids, err
}

func (s *cancelSink) FindEventIDByHash(h string) (uint64, bool, error) {
	return s.store.FindEventIDByHash(h)
}

func (s *cancelSink) IncrementDedupCounts(deltas map[uint64]map[string]int) error {
	return s.store.IncrementDedupCounts(deltas)
}

// TestCancelThenResumeNoLossNoDup is the resume gate: cancel partway through, then
// resume with idempotency on, and confirm the final store holds every distinct event
// exactly once — no loss, no duplication. Uses the real fixture (distinct records).
func TestCancelThenResumeNoLossNoDup(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	// Baseline: how many distinct events the fixture yields when fully ingested.
	ref := graphene.OpenInMemory()
	fullSum, err := Ingest(context.Background(), Options{Source: consts.SourceFile, Path: fixtureSecurity}, ref, NoopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()
	total := fullSum.RecordsPersisted
	if total < 100 {
		t.Fatalf("fixture too small for a meaningful cancel test: %d", total)
	}

	// Run 1: cancel after ~a third of the events are persisted.
	ctx, cancel := context.WithCancel(context.Background())
	cs := &cancelSink{store: store, cancel: cancel, after: total / 3}
	_, _ = Ingest(ctx, Options{Source: consts.SourceFile, Path: fixtureSecurity, Idempotent: true}, cs, NoopReporter{})

	partial, _, _ := store.Stats()
	if partial == 0 {
		t.Fatal("run 1 persisted nothing")
	}
	if int(partial) >= total {
		t.Skip("run 1 completed before cancel took effect; timing-dependent, skipping")
	}

	// Run 2: resume to completion with idempotency — must top up to exactly `total`.
	resumeSum, err := Ingest(context.Background(), Options{Source: consts.SourceFile, Path: fixtureSecurity, Idempotent: true}, store, NoopReporter{})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	final, _, _ := store.Stats()
	if int(final) != total {
		t.Errorf("final count %d != expected %d (loss or duplication after resume)", final, total)
	}
	if resumeSum.RecordsDuplicate == 0 {
		t.Error("resume reported no duplicates; idempotency did not engage")
	}
	// 🔒 The interrupted run wrote a prefix of the file, and the resume wrote the rest after it, so
	// the case holds the events in the order a single full ingest gives them.
	if !slices.Equal(eventsByID(t, store), eventsByID(t, ref)) {
		t.Error("cancel and resume gave the events another order than a full ingest")
	}
	t.Logf("total=%d partial=%d resumed(dup=%d) final=%d", total, partial, resumeSum.RecordsDuplicate, final)
}
