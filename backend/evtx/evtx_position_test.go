package evtx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// writeJournal is the shared, mutex-guarded record of durable writes and bookmark commits, in
// order. The pause tests observe it from the test goroutine while the sink runs in its
// own, so it must be safe for concurrent use.
type writeJournal struct {
	mu      sync.Mutex
	entries []string
}

func (j *writeJournal) add(entry string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, entry)
}

func (j *writeJournal) snapshot() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.entries...)
}

// journalSink records the interleaving of durable writes so a test can assert that a
// capture position is never persisted before the events it covers.
type journalSink struct {
	journal  *writeJournal
	failFrom int // fail InsertEvents once this many events have been written (0 = never)
	written  int
}

func (s *journalSink) InsertEvents(events []*graphene.Event) ([]uint64, error) {
	if s.failFrom > 0 && s.written+len(events) >= s.failFrom {
		return nil, errors.New("disk on fire")
	}
	s.written += len(events)
	s.journal.add("write")
	return make([]uint64, len(events)), nil
}

func (s *journalSink) FindEventIDByHash(string) (uint64, bool, error) { return 0, false, nil }

func (s *journalSink) IncrementDedupCounts(map[uint64]map[string]int) error {
	s.journal.add("inc")
	return nil
}

// journalPositions records every committed bookmark, in order.
type journalPositions struct {
	journal *writeJournal
	mu      sync.Mutex
	pos     map[string]uint64
	failing bool
}

func (p *journalPositions) Position(channel string) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pos[channel]
}

func (p *journalPositions) SetPosition(channel string, recordID uint64) error {
	if p.failing {
		return errors.New("read-only filesystem")
	}
	p.mu.Lock()
	if p.pos == nil {
		p.pos = map[string]uint64{}
	}
	p.pos[channel] = recordID
	p.mu.Unlock()
	p.journal.add(fmt.Sprintf("position=%d", recordID))
	return nil
}

// writesBefore counts the durable writes that happened before the given journal entry,
// which is how the ordering invariant is expressed: a bookmark for the events of the Nth
// batch must not appear until that batch has been written.
func writesBefore(journal []string, entry string) (writes int, found bool) {
	for _, e := range journal {
		if e == entry {
			return writes, true
		}
		if e == "write" {
			writes++
		}
	}
	return writes, false
}

// liveChunk builds a live-tagged batch of n distinct events ending at record id maxRec.
func liveChunk(channel string, n int, maxRec uint64) chunkResult {
	events := make([]*graphene.Event, 0, n)
	for i := 0; i < n; i++ {
		events = append(events, &graphene.Event{
			EventID:        "4624",
			Channel:        channel,
			HashNormalized: channel + string(rune('a'+i)) + string(rune('0'+maxRec%10)),
		})
	}
	return chunkResult{channel: channel, events: events, maxRecID: maxRec}
}

// feed runs runSink over a fixed set of chunks.
func feed(t *testing.T, opts Options, sink EventSink, chunks ...chunkResult) (Summary, error) {
	t.Helper()
	ch := make(chan chunkResult, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return runSink(context.Background(), opts.normalized(), sink, NoopReporter{}, ch, 0)
}

func TestPositionIsCommittedOnlyAfterADurableWrite(t *testing.T) {
	j := &writeJournal{}
	positions := &journalPositions{journal: j}
	sink := &journalSink{journal: j}

	opts := Options{Source: consts.SourceLive, Continuous: true, Positions: positions, BatchSize: 2}
	if _, err := feed(t, opts, sink,
		liveChunk(consts.ChannelSecurity, 2, 10),
		liveChunk(consts.ChannelSecurity, 2, 20),
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The ordering that makes a crash re-read instead of skip: the bookmark covering the
	// first batch may only appear after that batch is written, and likewise for the second.
	if n, ok := writesBefore(j.snapshot(), "position=10"); !ok || n < 1 {
		t.Errorf("position=10 committed after %d writes (found=%v): %v", n, ok, j.snapshot())
	}
	if n, ok := writesBefore(j.snapshot(), "position=20"); !ok || n < 2 {
		t.Errorf("position=20 committed after %d writes (found=%v): %v", n, ok, j.snapshot())
	}
	if got := positions.Position(consts.ChannelSecurity); got != 20 {
		t.Errorf("final position = %d, want 20", got)
	}
}

func TestPositionIsNotCommittedWhenTheWriteFails(t *testing.T) {
	j := &writeJournal{}
	positions := &journalPositions{journal: j}
	sink := &journalSink{journal: j, failFrom: 2}

	opts := Options{Source: consts.SourceLive, Continuous: true, Positions: positions, BatchSize: 2}
	if _, err := feed(t, opts, sink, liveChunk(consts.ChannelSecurity, 2, 10)); err == nil {
		t.Fatal("expected the persistence failure to surface")
	}
	if got := positions.Position(consts.ChannelSecurity); got != 0 {
		t.Errorf("position advanced to %d despite a failed write — a restart would skip events", got)
	}
}

func TestPositionsAreTrackedPerChannel(t *testing.T) {
	j := &writeJournal{}
	positions := &journalPositions{journal: j}
	sink := &journalSink{journal: j}

	opts := Options{Source: consts.SourceLive, Continuous: true, Positions: positions, BatchSize: 1}
	if _, err := feed(t, opts, sink,
		liveChunk(consts.ChannelSecurity, 1, 100),
		liveChunk(consts.ChannelSystem, 1, 5),
		liveChunk(consts.ChannelSecurity, 1, 101),
	); err != nil {
		t.Fatal(err)
	}
	if got := positions.Position(consts.ChannelSecurity); got != 101 {
		t.Errorf("Security = %d, want 101", got)
	}
	if got := positions.Position(consts.ChannelSystem); got != 5 {
		t.Errorf("System = %d, want 5", got)
	}
}

func TestCaptureResumesFromTheStoredPosition(t *testing.T) {
	// A store that already knows where the last session stopped must report it, so the
	// reader can query only newer records.
	positions := &journalPositions{journal: &writeJournal{}, pos: map[string]uint64{consts.ChannelSecurity: 4242}}
	if got := positions.Position(consts.ChannelSecurity); got != 4242 {
		t.Fatalf("position = %d, want 4242", got)
	}
	if q := channelQuery(4242); q == consts.LiveQueryAll {
		t.Errorf("a resumed channel must not query everything again (got %q)", q)
	}
	if q := channelQuery(0); q != consts.LiveQueryAll {
		t.Errorf("a fresh channel should query everything, got %q", q)
	}
}

func TestBookmarkFailureDoesNotAbortCapture(t *testing.T) {
	// Losing a bookmark costs a re-read next session; it must never stop the capture or
	// lose the events already written.
	j := &writeJournal{}
	positions := &journalPositions{journal: j, failing: true}
	sink := &journalSink{journal: j}

	opts := Options{Source: consts.SourceLive, Continuous: true, Positions: positions, BatchSize: 2}
	summary, err := feed(t, opts, sink, liveChunk(consts.ChannelSecurity, 2, 10))
	if err != nil {
		t.Fatalf("a bookmark failure must not fail the run: %v", err)
	}
	if summary.RecordsPersisted != 2 {
		t.Errorf("persisted = %d, want 2", summary.RecordsPersisted)
	}
}

func TestNilPositionStoreIsSafe(t *testing.T) {
	j := &writeJournal{}
	sink := &journalSink{journal: j}
	opts := Options{Source: consts.SourceLive, Continuous: true, BatchSize: 2}
	if _, err := feed(t, opts, sink, liveChunk(consts.ChannelSecurity, 2, 10)); err != nil {
		t.Fatalf("bookmarking is optional: %v", err)
	}
}
