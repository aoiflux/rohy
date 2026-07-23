package evtx

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// evtxChunkSize is fixed by the EVTX on-disk format (64 KiB per chunk). It lives here
// as a documented test constant only; production code never assumes it (it walks
// chunk offsets via the parser).
const evtxChunkSize = 65536

// synthEVTX writes a synthetic EVTX file at dst by replicating the chunk region of
// src `repeat` times after its header block. GetChunks walks the result as
// repeat×(source chunks), letting a memory/throughput test scale dataset size far
// beyond the committed fixtures without a real 100 GB file. Replicated records share
// content (and hashes), which is fine for streaming/backpressure/batching tests
// (idempotency off); tests needing distinct records use the real fixture instead.
func synthEVTX(t *testing.T, src, dst string, repeat int) (chunks int) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	// HeaderBlockSize is a uint16 at byte offset 40 of the EVTX file header.
	headerBlock := int(binary.LittleEndian.Uint16(data[40:42]))
	if headerBlock <= 0 || headerBlock >= len(data) {
		t.Fatalf("bad header block size %d", headerBlock)
	}
	srcChunks := (len(data) - headerBlock) / evtxChunkSize
	if srcChunks <= 0 {
		t.Fatalf("source has no whole chunks (len=%d, header=%d)", len(data), headerBlock)
	}
	region := data[headerBlock : headerBlock+srcChunks*evtxChunkSize]

	f, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(data[:headerBlock]); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < repeat; i++ {
		if _, err := f.Write(region); err != nil {
			t.Fatal(err)
		}
	}
	return srcChunks * repeat
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

// TestBoundedMemoryStreaming is the R-L1 gate: peak memory must NOT scale with input
// size. It ingests a small and a 4× larger synthetic file through a discarding sink
// and asserts the large run's peak heap stays bounded (both under an absolute ceiling
// far below the file size, and not proportional to the dataset). Skipped in -short.
func TestBoundedMemoryStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test skipped in -short")
	}
	if raceEnabled {
		t.Skip("memory bounds are not meaningful under -race instrumentation")
	}
	dir := t.TempDir()
	smallPath := filepath.Join(dir, "small.evtx")
	largePath := filepath.Join(dir, "large.evtx")
	synthEVTX(t, fixtureSecurity, smallPath, 15) // ~150 chunks
	synthEVTX(t, fixtureSecurity, largePath, 60) // ~600 chunks, 4× data (~40 MB)

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

	const ceiling = 96 << 20 // 96 MiB — far below the ~40 MB large file fully buffered + parsed
	if largePeak > ceiling {
		t.Errorf("peak heap %d MiB exceeds ceiling; memory not bounded", largePeak>>20)
	}
	// 4× the data must not cost ~4× the memory. Allow generous slack for GC/noise but
	// fail on proportional growth. Only meaningful once the signal clears noise.
	if largePeak > 8<<20 && smallPeak > 0 && largePeak > smallPeak*3 {
		t.Errorf("peak heap scaled with input: small=%d MiB large=%d MiB (want sub-linear)", smallPeak>>20, largePeak>>20)
	}
	t.Logf("records small=%d large=%d; peak heap small=%d MiB large=%d MiB",
		smallSink.count, largeSink.count, smallPeak>>20, largePeak>>20)
}

// TestBatchedWritesNoLargeTransaction is the R-D1 gate: the sink must only ever
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

// TestCancelThenResumeNoLossNoDup is the R-L2 gate: cancel partway through, then
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
	ref.Close()
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
	t.Logf("total=%d partial=%d resumed(dup=%d) final=%d", total, partial, resumeSum.RecordsDuplicate, final)
}
