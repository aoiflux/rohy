package evtx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

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

// dupSink is a journalSink that reports one hash as already persisted, so a matching event
// takes the deferred-increment (dbInc) path instead of becoming a new canonical.
type dupSink struct {
	journalSink
	knownHash string
	knownID   uint64
}

func (s *dupSink) FindEventIDByHash(h string) (uint64, bool, error) {
	if h == s.knownHash {
		return s.knownID, true, nil
	}
	return 0, false, nil
}

// TestPositionNotCommittedWhileIncrementsBuffered pins that a bookmark is not advanced past
// records whose deduplication-count increments are still buffered.
//
// The gap this guards: a full batch of new events flushes and, being empty afterwards, lets
// the position commit — but a duplicate of a previously-persisted canonical from the SAME
// chunk sits in the increment buffer until its own threshold. If the bookmark advanced past
// that record and the process then died, the increment would be lost and the canonical's
// occurrence count would be one short. Not event loss — an undercount — but still a durable
// write escaping its bookmark, which the whole position discipline exists to prevent.
func TestPositionNotCommittedWhileIncrementsBuffered(t *testing.T) {
	j := &writeJournal{}
	positions := &journalPositions{journal: j}
	// The duplicate's hash is reported as already persisted, so it defers an increment.
	sink := &dupSink{journalSink: journalSink{journal: j}, knownHash: "known-dup", knownID: 7}

	// Chunk 1 (ends at record 10) carries only a duplicate of the pre-persisted canonical,
	// so it stages position 10 and buffers an increment — nothing is written or flushed yet.
	// Chunk 2 (ends at 20) carries two new events that fill the batch and trigger a flush;
	// that flush finds pending empty afterwards and would commit the STAGED position from
	// chunk 1 (10) — while chunk 1's increment, covering record 10, is still buffered.
	//
	// Staging happens after each chunk's event loop, which is why the two must be separate
	// chunks: a single chunk cannot both stage its position and flush before staging it.
	chunk1 := chunkResult{
		channel:  consts.ChannelSecurity,
		maxRecID: 10,
		events: []*graphene.Event{
			{EventID: "4624", Channel: consts.ChannelSecurity, HashNormalized: "known-dup"},
		},
	}
	chunk2 := chunkResult{
		channel:  consts.ChannelSecurity,
		maxRecID: 20,
		events: []*graphene.Event{
			{EventID: "4624", Channel: consts.ChannelSecurity, HashNormalized: "new-a"},
			{EventID: "4624", Channel: consts.ChannelSecurity, HashNormalized: "new-b"},
		},
	}
	opts := Options{Source: consts.SourceLive, Continuous: true, Idempotent: true, Positions: positions, BatchSize: 2}
	if _, err := feed(t, opts, sink, chunk1, chunk2); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The invariant: no bookmark may be committed while an increment covering a record at or
	// below it is still buffered. Chunk 1's increment covers record 10, so NO "position="
	// entry may appear before the "inc". Once the increment is durable the bookmark may jump
	// straight to 20 — positions are monotonic high-water marks, and 20 covers 10 too.
	journal := j.snapshot()
	firstPos, incAt := -1, -1
	for i, e := range journal {
		if incAt == -1 && e == "inc" {
			incAt = i
		}
		if firstPos == -1 && len(e) > 9 && e[:9] == "position=" {
			firstPos = i
		}
	}
	if firstPos == -1 {
		t.Fatalf("bookmark never committed; journal=%v", journal)
	}
	if incAt == -1 {
		t.Fatalf("increment never flushed; journal=%v", journal)
	}
	if firstPos < incAt {
		t.Errorf("a bookmark was committed before the increment covering record 10; journal=%v", journal)
	}
}

// TestPauseDrainsIncrementsBeforeBookmarking pins that the pause boundary flushes the
// deduplication-increment buffer too, not only new events — the same boundary must drain
// BOTH, or a pause could bookmark past an increment it left buffered (R-10.1).
func TestPauseDrainsIncrementsBeforeBookmarking(t *testing.T) {
	j := &writeJournal{}
	positions := &journalPositions{journal: j}
	sink := &dupSink{journalSink: journalSink{journal: j}, knownHash: "known-dup", knownID: 7}
	gate := NewGate()

	// One chunk, one event: a duplicate of the pre-persisted canonical, ending at record 55.
	// It buffers an increment and stages position 55, but writes no new event — so without a
	// pause draining the increment, nothing would flush and the bookmark could not advance.
	ch := make(chan chunkResult, 1)
	ch <- chunkResult{
		channel:  consts.ChannelSecurity,
		maxRecID: 55,
		events:   []*graphene.Event{{EventID: "4624", Channel: consts.ChannelSecurity, HashNormalized: "known-dup"}},
	}

	opts := Options{Source: consts.SourceLive, Continuous: true, Idempotent: true, Positions: positions, Gate: gate, BatchSize: 100}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runSink(ctx, opts.normalized(), sink, NoopReporter{}, ch, 0)
	}()

	time.Sleep(100 * time.Millisecond)
	if got := positions.Position(consts.ChannelSecurity); got != 0 {
		t.Fatalf("nothing should be bookmarked yet, got position %d", got)
	}
	gate.Pause()

	deadline := time.Now().Add(2 * time.Second)
	for positions.Position(consts.ChannelSecurity) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := positions.Position(consts.ChannelSecurity); got != 55 {
		t.Errorf("position after pause = %d, want 55 (the increment must be drained and bookmarked)", got)
	}
	// The bookmark must follow the increment, never precede it.
	journal := j.snapshot()
	incAt, posAt := -1, -1
	for i, e := range journal {
		if incAt == -1 && e == "inc" {
			incAt = i
		}
		if posAt == -1 && e == "position=55" {
			posAt = i
		}
	}
	if incAt == -1 || posAt == -1 || incAt > posAt {
		t.Errorf("increment was not flushed before the bookmark at pause; journal=%v", journal)
	}

	cancel()
	<-done
}
