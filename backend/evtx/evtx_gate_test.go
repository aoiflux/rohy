package evtx

import (
	"context"
	"sync"
	"testing"
	"time"

	"rohy/backend/consts"
)

func TestGateBlocksUntilResumed(t *testing.T) {
	g := NewGate()
	if g.Paused() {
		t.Fatal("a new gate must start unpaused")
	}
	// An unpaused gate never blocks.
	if !g.Wait(context.Background()) {
		t.Fatal("unpaused Wait should proceed immediately")
	}

	g.Pause()
	if !g.Paused() {
		t.Fatal("Paused() should report the pause")
	}

	released := make(chan bool, 1)
	go func() { released <- g.Wait(context.Background()) }()

	select {
	case <-released:
		t.Fatal("Wait returned while paused")
	case <-time.After(50 * time.Millisecond):
	}

	g.Resume()
	select {
	case ok := <-released:
		if !ok {
			t.Error("Wait should report success after a resume")
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after Resume")
	}
	if g.Paused() {
		t.Error("gate still reports paused after Resume")
	}
}

func TestGateCancelledWhilePausedUnwinds(t *testing.T) {
	// A run cancelled while paused must not wait for a resume that will never come.
	g := NewGate()
	g.Pause()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan bool, 1)
	go func() { done <- g.Wait(ctx) }()
	cancel()

	select {
	case ok := <-done:
		if ok {
			t.Error("Wait should report failure when the run is cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelling did not release a paused Wait")
	}
}

func TestGatePauseResumeAreIdempotent(t *testing.T) {
	g := NewGate()
	g.Resume() // resume while not paused is a no-op, not a panic
	g.Pause()
	g.Pause() // double pause must not replace the channel out from under a waiter
	g.Resume()
	g.Resume() // double resume must not close a closed channel
	if g.Paused() {
		t.Error("gate should be unpaused")
	}
	if !g.Wait(context.Background()) {
		t.Error("gate should not block after resume")
	}
}

func TestNilGateNeverPauses(t *testing.T) {
	// Options.Gate is optional; a nil gate must behave as "always open".
	var g *Gate
	if g.Paused() {
		t.Error("nil gate should not report paused")
	}
	if !g.Wait(context.Background()) {
		t.Error("nil gate should not block")
	}
}

func TestGateSurvivesConcurrentPauseResume(t *testing.T) {
	g := NewGate()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				g.Pause()
			} else {
				g.Resume()
			}
		}(i)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Paused()
		}()
	}
	wg.Wait()
	g.Resume()
	if !g.Wait(ctx) {
		t.Error("gate should be open after a final Resume")
	}
}

func TestPauseFlushesAndBookmarksBeforeBlocking(t *testing.T) {
	// The P8 guarantee: pausing leaves the store at a consistent point — the in-flight
	// batch is written and its capture position persisted BEFORE the pipeline idles.
	j := &writeJournal{}
	positions := &journalPositions{journal: j}
	sink := &journalSink{journal: j}
	gate := NewGate()

	// A batch smaller than BatchSize would normally sit unwritten until the run ends. The
	// channel stays open (as a continuous capture's would), so nothing but the pause can
	// trigger the flush.
	ch := make(chan chunkResult, 1)
	ch <- liveChunk(consts.ChannelSecurity, 1, 77)

	opts := Options{Source: consts.SourceLive, Continuous: true, Positions: positions, Gate: gate, BatchSize: 100}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runSink(ctx, opts.normalized(), sink, NoopReporter{}, ch, 0)
	}()

	// Let the sink consume the chunk and settle waiting for the next one, then pause.
	time.Sleep(100 * time.Millisecond)
	if got := positions.Position(consts.ChannelSecurity); got != 0 {
		t.Fatalf("nothing should be written yet (batch is not full), got position %d", got)
	}
	gate.Pause()

	// The pause alone must flush and bookmark, without waiting for more events.
	deadline := time.Now().Add(2 * time.Second)
	for positions.Position(consts.ChannelSecurity) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := positions.Position(consts.ChannelSecurity); got != 77 {
		t.Errorf("position after pause = %d, want 77 (flushed to a consistent point)", got)
	}
	if n, ok := writesBefore(j.snapshot(), "position=77"); !ok || n < 1 {
		t.Errorf("bookmark was not preceded by a durable write: %v", j.snapshot())
	}

	cancel()
	<-done
}
