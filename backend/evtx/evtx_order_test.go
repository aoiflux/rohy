package evtx

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// The file pipeline hands chunks to the sink in FILE order, whichever worker finishes first.
//
// Node ids are assigned in the order events reach the store, and the rule engine breaks timestamp
// ties on node id. Before this, the same file got different ids on every ingest, and two events
// at one instant could be joined on one ingest and not on the next. These tests make the worker
// pool finish out of order on purpose, which real parsing does only by chance.

const orderChunks = 32

// fakeHandle stands in for a worker's private handle to the evidence file.
type fakeHandle struct{ closed *atomic.Int32 }

func (fakeHandle) Read([]byte) (int, error)       { return 0, io.EOF }
func (fakeHandle) Seek(int64, int) (int64, error) { return 0, nil }
func (h fakeHandle) Close() error                 { h.closed.Add(1); return nil }

// recordingSink keeps the order events reached it, by their tag in HashRaw.
type recordingSink struct {
	mu  sync.Mutex
	got []string
}

func (s *recordingSink) InsertEvents(evs []*graphene.Event) ([]uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]uint64, len(evs))
	for i, e := range evs {
		s.got = append(s.got, e.HashRaw)
		ids[i] = uint64(len(s.got))
	}
	return ids, nil
}

func (*recordingSink) FindEventIDByHash(string) (uint64, bool, error)       { return 0, false, nil }
func (*recordingSink) IncrementDedupCounts(map[uint64]map[string]int) error { return nil }

func (s *recordingSink) order() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

// recordingReporter keeps the errors in the order they were reported, and how the run ended.
type recordingReporter struct {
	mu        sync.Mutex
	errs      []string
	completed *Summary
	cancelled *Summary
}

func (*recordingReporter) Started(string, int) {}
func (*recordingReporter) Progress(Progress)   {}
func (r *recordingReporter) RecordError(_, msg string) {
	r.mu.Lock()
	r.errs = append(r.errs, msg)
	r.mu.Unlock()
}
func (r *recordingReporter) Completed(s Summary) { r.mu.Lock(); r.completed = &s; r.mu.Unlock() }
func (r *recordingReporter) Cancelled(s Summary) { r.mu.Lock(); r.cancelled = &s; r.mu.Unlock() }

// chunkEvents is what the fake parser returns for chunk c: two undated events tagged c-0, c-1.
// Undated, so the sink leaves their identity alone.
func chunkEvents(c int64) []*graphene.Event {
	return []*graphene.Event{
		{EventID: "4624", HashRaw: fmt.Sprintf("%02d-0", c), HashNormalized: fmt.Sprintf("%02d-0", c)},
		{EventID: "4624", HashRaw: fmt.Sprintf("%02d-1", c), HashNormalized: fmt.Sprintf("%02d-1", c)},
	}
}

func fileOrder() []string {
	var out []string
	for c := range int64(orderChunks) {
		for _, e := range chunkEvents(c) {
			out = append(out, e.HashRaw)
		}
	}
	return out
}

// orderRun is one run of runFileChunks over fake offsets 0..orderChunks-1 on four fake handles.
type orderRun struct {
	handles []io.ReadSeekCloser
	closed  atomic.Int32
	offsets []int64
	sink    *recordingSink
	rep     *recordingReporter
	opts    Options
}

func newOrderRun() *orderRun {
	r := &orderRun{sink: &recordingSink{}, rep: &recordingReporter{}, opts: Options{Workers: 4, BatchSize: 1}.normalized()}
	r.opts.BatchSize = 1
	for range 4 {
		r.handles = append(r.handles, fakeHandle{closed: &r.closed})
	}
	for c := range int64(orderChunks) {
		r.offsets = append(r.offsets, c)
	}
	return r
}

func (r *orderRun) run(ctx context.Context, parse chunkParser) (Summary, error) {
	return runFileChunks(ctx, r.opts, r.handles, r.offsets, parse, r.sink, r.rep)
}

// within fails the test if done does not close in time, rather than hanging the suite.
func within(t *testing.T, d time.Duration, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not happen within %s", what, d)
	}
}

// settled waits until n stops changing for a while, and returns its value.
func settled(n *atomic.Int32) int32 {
	last, still := n.Load(), 0
	for deadline := time.Now().Add(5 * time.Second); still < 5 && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		if cur := n.Load(); cur == last {
			still++
		} else {
			last, still = cur, 0
		}
	}
	return last
}

// 🔒 The head chunk finishes last; the sink still sees the file's order.
func TestFileChunksReachTheSinkInFileOrder(t *testing.T) {
	r := newOrderRun()
	var others atomic.Int32
	_, err := r.run(context.Background(), func(_ io.ReadSeeker, off int64, _ uint64) chunkResult {
		if off == 0 {
			for deadline := time.Now().Add(5 * time.Second); others.Load() < 3 && time.Now().Before(deadline); {
				time.Sleep(time.Millisecond)
			}
		} else {
			defer others.Add(1)
		}
		return chunkResult{events: chunkEvents(off)}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.sink.order(); !slices.Equal(got, fileOrder()) {
		t.Fatalf("the sink saw chunks out of file order: %v", got[:8])
	}
}

// While the head chunk is stuck, only a bounded window of chunks is parsed ahead of it. Without
// the bound, one slow chunk would make every later one pile up in memory.
func TestTheReorderWindowBoundsWhatIsParsedAhead(t *testing.T) {
	r := newOrderRun()
	release := make(chan struct{})
	var started atomic.Int32
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = r.run(context.Background(), func(_ io.ReadSeeker, off int64, _ uint64) chunkResult {
			started.Add(1)
			if off == 0 {
				<-release
			}
			return chunkResult{events: chunkEvents(off)}
		})
	}()
	ahead := settled(&started)
	close(release)
	within(t, 5*time.Second, done, "the run")
	if err != nil {
		t.Fatal(err)
	}
	if limit := int32(reorderWindow(len(r.handles)) + 1); ahead > limit {
		t.Errorf("%d chunks were parsed while the head was stuck; the window allows %d", ahead, limit)
	}
	if !slices.Equal(r.sink.order(), fileOrder()) {
		t.Error("out of file order once the head was released")
	}
}

// Cancelling while the head is stuck ends the run, closes every handle and leaves no goroutine.
func TestCancelWhileTheHeadIsStuckReleasesEverything(t *testing.T) {
	r := newOrderRun()
	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	var started atomic.Int32
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = r.run(ctx, func(_ io.ReadSeeker, off int64, _ uint64) chunkResult {
			started.Add(1)
			if off == 0 {
				<-ctx.Done()
			}
			return chunkResult{events: chunkEvents(off)}
		})
	}()
	for deadline := time.Now().Add(5 * time.Second); started.Load() < 2 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	cancel()
	within(t, 5*time.Second, done, "the cancelled run's return")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if r.rep.cancelled == nil {
		t.Error("the run was not reported cancelled")
	}
	if got := r.closed.Load(); got != int32(len(r.handles)) {
		t.Errorf("%d of %d handles closed", got, len(r.handles))
	}
	for deadline := time.Now().Add(2 * time.Second); runtime.NumGoroutine() > baseline && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseline {
		t.Errorf("%d goroutines left behind", n-baseline)
	}
}

// A pause while the head is stuck holds the run; a resume finishes it, still in file order.
func TestPauseWhileTheHeadIsStuckThenResume(t *testing.T) {
	r := newOrderRun()
	gate := NewGate()
	r.opts.Gate = gate
	release := make(chan struct{})
	var started atomic.Int32
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = r.run(context.Background(), func(_ io.ReadSeeker, off int64, _ uint64) chunkResult {
			started.Add(1)
			if off == 0 {
				<-release
			}
			return chunkResult{events: chunkEvents(off)}
		})
	}()
	settled(&started)
	gate.Pause()
	close(release)
	select {
	case <-done:
		t.Fatal("a paused run finished")
	case <-time.After(200 * time.Millisecond):
	}
	gate.Resume()
	within(t, 5*time.Second, done, "the resumed run")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.sink.order(), fileOrder()) {
		t.Error("out of file order after the pause")
	}
}

// Errors are reported in file order too, and every chunk is counted once.
func TestChunkErrorsAreReportedInFileOrder(t *testing.T) {
	r := newOrderRun()
	var sevenDone atomic.Bool
	_, err := r.run(context.Background(), func(_ io.ReadSeeker, off int64, _ uint64) chunkResult {
		switch off {
		case 5:
			for deadline := time.Now().Add(5 * time.Second); !sevenDone.Load() && time.Now().Before(deadline); {
				time.Sleep(time.Millisecond)
			}
			return chunkResult{parseErr: "chunk 5 failed"}
		case 7:
			defer sevenDone.Store(true)
			return chunkResult{events: chunkEvents(off)[:1], shortfall: 1, parseErr: "chunk 7 short"}
		}
		return chunkResult{events: chunkEvents(off)}
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"chunk 5 failed", "chunk 7 short"}; !slices.Equal(r.rep.errs, want) {
		t.Errorf("errors = %q, want %q", r.rep.errs, want)
	}
	s := r.rep.completed
	if s == nil {
		t.Fatal("not completed")
	}
	if s.ChunksParsed != orderChunks || s.RecordsSkipped != 1 {
		t.Errorf("chunks parsed %d (want %d), skipped %d (want 1)", s.ChunksParsed, orderChunks, s.RecordsSkipped)
	}
}

// 🔒 The real fixture on four workers reaches the store in the same order as on one.
func TestTheSameFileIsWrittenInOneOrderWhateverTheWorkers(t *testing.T) {
	ingestOrder := func(workers int) []string {
		sink := &recordingSink{}
		if _, err := Ingest(context.Background(), Options{Source: consts.SourceFile, Path: fixtureSecurity, Workers: workers}, sink, nil); err != nil {
			t.Fatal(err)
		}
		return sink.order()
	}
	want := ingestOrder(1)
	if len(want) < 100 {
		t.Fatalf("fixture gave %d events", len(want))
	}
	for run := range 5 {
		if got := ingestOrder(4); !slices.Equal(got, want) {
			t.Fatalf("run %d: four workers wrote the file in another order", run)
		}
	}
}

// eventsByID lists a store's events by node id, as their normalized hashes.
func eventsByID(t *testing.T, s *graphene.Store) []string {
	t.Helper()
	evs, err := s.QueryEvents(graphene.EventFilter{Limit: 1 << 20, Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(evs, func(a, b *graphene.Event) int { return cmp.Compare(a.ID, b.ID) })
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.HashNormalized
	}
	return out
}
