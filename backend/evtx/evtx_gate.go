package evtx

import (
	"context"
	"sync"
)

// Gate is the pause control for a running ingestion (P8). The API layer owns one per run
// and flips it; the pipeline consults it at batch boundaries.
//
// Pausing does not abandon work: the sink finishes the batch it is on, flushes it, and
// persists the capture position BEFORE it blocks — so a pause always leaves the store at a
// consistent point, and a resume (or even an app restart while paused) continues without a
// gap or a duplicate. Resuming is therefore just "unblock"; the durable state is already
// correct.
//
// The zero value is an unpaused, usable Gate.
type Gate struct {
	mu     sync.Mutex
	paused bool
	// resume is closed to release everyone waiting. It is replaced on each pause, so a
	// waiter can never latch onto a stale channel.
	resume chan struct{}
	// pause is closed when a pause begins, so a pipeline blocked waiting for the next
	// batch wakes up and reaches its pause boundary immediately instead of idling with
	// unwritten work. It is replaced on each resume.
	pause chan struct{}
}

// NewGate returns an unpaused gate.
func NewGate() *Gate { return &Gate{} }

// pauseChanLocked lazily initializes the pause signal so the zero-value Gate works.
func (g *Gate) pauseChanLocked() chan struct{} {
	if g.pause == nil {
		g.pause = make(chan struct{})
	}
	return g.pause
}

// Pausing returns a channel that is closed when a pause begins. A pipeline blocked waiting
// for its next batch should select on it so a pause takes effect promptly rather than
// whenever the next event happens to arrive. A nil gate never signals.
func (g *Gate) Pausing() <-chan struct{} {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pauseChanLocked()
}

// Pause halts the pipeline at its next batch boundary. It is idempotent.
func (g *Gate) Pause() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused {
		return
	}
	g.paused = true
	g.resume = make(chan struct{})
	close(g.pauseChanLocked())
}

// Resume releases a paused pipeline. It is idempotent and safe to call when not paused.
func (g *Gate) Resume() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.paused {
		return
	}
	g.paused = false
	close(g.resume)
	g.resume = nil
	g.pause = make(chan struct{}) // fresh signal for the next pause
}

// Paused reports whether the gate is currently closed.
func (g *Gate) Paused() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// Wait blocks while the gate is paused and reports whether the caller may proceed. It
// returns false only when ctx is cancelled — so a run that is cancelled while paused
// unwinds immediately instead of waiting for a resume that will never come.
func (g *Gate) Wait(ctx context.Context) bool {
	for {
		if g == nil {
			return ctx.Err() == nil
		}
		g.mu.Lock()
		paused, resume := g.paused, g.resume
		g.mu.Unlock()
		if !paused {
			return ctx.Err() == nil
		}
		select {
		case <-ctx.Done():
			return false
		case <-resume:
			// Re-check rather than trusting this wake-up: a Pause may have landed again.
		}
	}
}
