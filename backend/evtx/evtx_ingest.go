package evtx

import (
	"context"
	"fmt"
	"os"
	"sync"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// EventSink is the persistence surface the ingestion pipeline writes to. It is
// satisfied by *graphene.Store. Depending on an interface (rather than the concrete
// store) keeps ingestion unit-testable and honors the dependency direction
// evtx -> graphene.
type EventSink interface {
	InsertEvents(events []*graphene.Event) ([]uint64, error)
	FindEventIDByHash(hashNormalized string) (uint64, bool, error)
	IncrementDedupCounts(deltas map[uint64]int) error
}

// PositionStore is the durable per-channel bookmark surface used by continuous live
// capture (P7). It is satisfied by *capture.Store. The pipeline only ever advances a
// position AFTER the events up to it are durably persisted, so a crash re-reads a little
// (harmless under hash idempotency) and can never skip.
type PositionStore interface {
	Position(channel string) uint64
	SetPosition(channel string, recordID uint64) error
}

// Progress is a snapshot of pipeline counters, emitted periodically. All counts are
// cumulative for the run.
type Progress struct {
	ChunksParsed     int    `json:"chunks_parsed"`
	ChunksTotal      int    `json:"chunks_total"`
	RecordsRead      int    `json:"records_read"`
	RecordsPersisted int    `json:"records_persisted"`
	RecordsDuplicate int    `json:"records_duplicate"`
	RecordsSkipped   int    `json:"records_skipped"`
	LastRecordID     uint64 `json:"last_record_id"`
	// RecordsUndated counts stored records that carry no timestamp. They cannot be placed
	// on a timeline, so they are excluded from timeline analysis — and this count is what
	// lets the UI say so instead of hiding them silently (P22).
	RecordsUndated int `json:"records_undated"`
}

// Summary is the terminal state of an ingest run, carried on Completed/Cancelled.
type Summary struct {
	Progress
	Checkpoint Checkpoint `json:"checkpoint"`
}

// Checkpoint records how far a run progressed so a later run can resume. Safe
// skip-ahead resume (dropping already-persisted RecordIDs) is a P2-L guarantee; in
// this iteration correctness on resume comes from hash idempotency, and the
// checkpoint's LastRecordID is the highest persisted id (informational).
type Checkpoint struct {
	LastRecordID uint64 `json:"last_record_id"`
}

// Reporter receives lifecycle, progress, and per-record error callbacks. The API
// layer (P4) adapts these to Wails events; tests use a capturing implementation.
// All methods are invoked from a single goroutine, so implementations need not be
// safe for concurrent use.
type Reporter interface {
	Started(source string, chunksTotal int)
	Progress(p Progress)
	RecordError(code, message string)
	Completed(s Summary)
	Cancelled(s Summary)
}

// NoopReporter discards every callback. Useful as a default and in tests that do
// not assert on reporting.
type NoopReporter struct{}

func (NoopReporter) Started(string, int)        {}
func (NoopReporter) Progress(Progress)          {}
func (NoopReporter) RecordError(string, string) {}
func (NoopReporter) Completed(Summary)          {}
func (NoopReporter) Cancelled(Summary)          {}

// Options configures a single ingest run. Zero-valued tuning fields fall back to
// the consts defaults via normalize().
type Options struct {
	Source           string     // consts.SourceFile or consts.SourceLive
	Path             string     // file path for SourceFile
	Channels         []string   // channel names for SourceLive
	SourceType       string     // recorded per event (consts.SourceType*); classifies origin
	SourceIdentifier string     // recorded per event: concrete file path or channel(s)
	BatchSize        int        // events per persistence write (defaults to consts.EventBatchSize)
	Workers          int        // parse/normalize concurrency (defaults to consts.ParseWorkerCount)
	Resume           Checkpoint // resume position; records with RecordID < LastRecordID are skipped at parse
	// Idempotent enables deduplication by hash_normalized: identical events collapse
	// into a single canonical node whose deduplication_count reflects the total number
	// of occurrences seen (within the run and against already-persisted events). This
	// also makes resume safe (re-ingesting a file adds no new nodes, only counts).
	Idempotent bool
	// Continuous turns a live run from drain-once into an open-ended capture: drained
	// channels keep polling for new records until the run is cancelled (P7). Ignored by
	// the file source, which always terminates.
	Continuous bool
	// Positions supplies durable per-channel bookmarks for live capture. When set, each
	// channel resumes after its recorded position and the position advances only after a
	// durable write. Nil disables bookmarking (the run reads from the beginning).
	Positions PositionStore
	// Gate pauses/resumes the run (P8). Nil means the run can never pause.
	Gate *Gate
}

func (o Options) normalized() Options {
	if o.BatchSize <= 0 {
		o.BatchSize = consts.EventBatchSize
	}
	if o.Workers <= 0 {
		o.Workers = consts.ParseWorkerCount
	}
	return o
}

// chunkResult is the unit workers hand to the sink: the normalized events from one
// chunk plus any errors encountered parsing/normalizing it. Funneling everything
// through this single channel means only the sink touches the Reporter and the
// counters, so no locking is needed for those.
type chunkResult struct {
	events   []*graphene.Event
	parseErr string
	normErrs []string
	maxRecID uint64
	// channel is set by the live reader so the sink can advance that channel's durable
	// bookmark once the batch is persisted. Empty for the file source.
	channel string
}

// Ingest runs one ingestion to completion (or cancellation). It streams the source,
// normalizes and hashes every record, persists in bounded batches, and reports
// progress/errors/lifecycle through reporter. Peak memory is bounded independent of
// input size: at most Options.Workers chunks (64 KB each) and ChunkQueueDepth
// batches are resident at once.
func Ingest(ctx context.Context, opts Options, sink EventSink, reporter Reporter) (Summary, error) {
	opts = opts.normalized()
	if reporter == nil {
		reporter = NoopReporter{}
	}

	switch opts.Source {
	case consts.SourceFile:
		// Dispatch on the file's kind, not on a separate source enum: a .db carrying EVTX
		// data is still "a file ingest" to every caller, and both readers feed the same
		// normalizer and sink (P17).
		if IsDBPath(opts.Path) {
			return ingestDB(ctx, opts, sink, reporter)
		}
		return ingestFile(ctx, opts, sink, reporter)
	case consts.SourceLive:
		return ingestLive(ctx, opts, sink, reporter)
	default:
		return Summary{}, fmt.Errorf("unknown ingestion source %q", opts.Source)
	}
}

// ingestFile drives the file pipeline: producer (offsets) -> worker pool
// (parse+normalize) -> sink (dedup, batch, persist, report).
func ingestFile(ctx context.Context, opts Options, sink EventSink, reporter Reporter) (Summary, error) {
	fd, offsets, err := openFileSource(opts.Path)
	if err != nil {
		return Summary{}, err
	}
	fd.Close() // workers open their own handles; this one only validated the file

	reporter.Started(consts.SourceFile, len(offsets))

	offsetCh := make(chan int64, consts.ChunkQueueDepth)
	resultCh := make(chan chunkResult, consts.ChunkQueueDepth)

	// Producer: feed chunk offsets, honoring cancellation and applying backpressure
	// (blocks when the bounded offsetCh is full).
	go func() {
		defer close(offsetCh)
		for _, off := range offsets {
			select {
			case <-ctx.Done():
				return
			case offsetCh <- off:
			}
		}
	}()

	// Worker pool: each worker owns a private file handle (parsing seeks the handle,
	// so a shared one would race).
	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wfd, err := os.Open(opts.Path)
			if err != nil {
				select {
				case resultCh <- chunkResult{parseErr: fmt.Sprintf(consts.MsgOpenFailed, opts.Path, err)}:
				case <-ctx.Done():
				}
				return
			}
			defer wfd.Close()
			for off := range offsetCh {
				if ctx.Err() != nil {
					return
				}
				res := parseAndNormalize(wfd, off, opts.Resume.LastRecordID)
				select {
				case resultCh <- res:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	return runSink(ctx, opts, sink, reporter, resultCh, len(offsets))
}

// parseAndNormalize parses one chunk and normalizes its records into events,
// collecting non-fatal errors rather than aborting the run.
func parseAndNormalize(wfd *os.File, offset int64, minRecordID uint64) chunkResult {
	var res chunkResult
	records, err := parseChunkAt(wfd, offset, minRecordID)
	if err != nil {
		res.parseErr = fmt.Sprintf(consts.MsgChunkParseFail, offset, err)
		return res
	}
	res.events = make([]*graphene.Event, 0, len(records))
	for _, rec := range records {
		ev, err := normalizeRecord(rec)
		if err != nil {
			res.normErrs = append(res.normErrs, fmt.Sprintf(consts.MsgRecordNormFail, rec.Header.RecordID, err))
			continue
		}
		if rec.Header.RecordID > res.maxRecID {
			res.maxRecID = rec.Header.RecordID
		}
		res.events = append(res.events, ev)
	}
	return res
}

// runSink consumes chunk results on the calling goroutine: it reports errors,
// deduplicates, accumulates events into batches, persists them, tracks the
// checkpoint, and emits progress. Being single-goroutine means the counters and
// Reporter need no synchronization.
func runSink(ctx context.Context, opts Options, sink EventSink, reporter Reporter, resultCh <-chan chunkResult, chunksTotal int) (Summary, error) {
	var p Progress
	p.ChunksTotal = chunksTotal
	pending := make([]*graphene.Event, 0, opts.BatchSize)
	lastReported := 0

	// Deduplication state (only used when opts.Idempotent). pendingByHash indexes the
	// canonical events in the current unflushed batch so duplicates within a batch
	// increment the in-memory count instead of creating a second node; it is cleared
	// on every flush. dbInc accumulates increments for canonicals already persisted
	// (this run or a prior one), applied in bounded batches so no single write is large.
	pendingByHash := make(map[string]*graphene.Event)
	dbInc := make(map[uint64]int)

	// Live capture bookmarks (P7). staged holds the highest record id seen per channel for
	// chunks whose events have all been handed to the batch; a position is committed only
	// once nothing is left unwritten (len(pending) == 0), i.e. strictly AFTER the durable
	// write that covers it. That ordering is what makes a crash re-read rather than skip.
	staged := map[string]uint64{}
	commitPositions := func() {
		if opts.Positions == nil || len(staged) == 0 || len(pending) > 0 {
			return
		}
		for channel, recID := range staged {
			if err := opts.Positions.SetPosition(channel, recID); err != nil {
				reporter.RecordError(consts.ErrCodeIO, fmt.Sprintf(consts.MsgPositionSaveFail, channel, err))
			}
		}
		staged = map[string]uint64{}
	}

	flushInc := func() error {
		if len(dbInc) == 0 {
			return nil
		}
		if err := sink.IncrementDedupCounts(dbInc); err != nil {
			return fmt.Errorf(consts.MsgPersistFailed, err)
		}
		dbInc = make(map[uint64]int)
		commitPositions()
		return nil
	}

	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		if _, err := sink.InsertEvents(pending); err != nil {
			return fmt.Errorf(consts.MsgPersistFailed, err)
		}
		p.RecordsPersisted += len(pending)
		pending = pending[:0]
		// Canonicals from this batch are now persisted; later duplicates must go
		// through the DB path, so drop the in-memory index.
		if len(pendingByHash) > 0 {
			pendingByHash = make(map[string]*graphene.Event)
		}
		commitPositions()
		return nil
	}

	cancelled := false
	var fatal error

loop:
	for {
		// Pause boundary. Everything buffered is written and bookmarked BEFORE blocking,
		// so a pause (or an app restart during one) always leaves the store at a consistent
		// point and a resume never re-reads or skips. Cancelling while paused unwinds here.
		if opts.Gate.Paused() {
			if err := flush(); err != nil {
				fatal = err
				break loop
			}
			if err := flushInc(); err != nil {
				fatal = err
				break loop
			}
			commitPositions()
			if !opts.Gate.Wait(ctx) {
				cancelled = true
				break loop
			}
		}

		select {
		case <-ctx.Done():
			cancelled = true
			break loop
		case <-opts.Gate.Pausing():
			// A pause arrived while waiting for the next batch: loop back so the pause
			// boundary above flushes and bookmarks now, rather than whenever the next
			// event happens to show up (which on a quiet channel could be minutes).
			continue
		case res, ok := <-resultCh:
			if !ok {
				break loop
			}
			p.ChunksParsed++
			if res.parseErr != "" {
				reporter.RecordError(consts.ErrCodeParse, res.parseErr)
			}
			for _, e := range res.normErrs {
				reporter.RecordError(consts.ErrCodeParse, e)
				p.RecordsSkipped++
			}
			if res.maxRecID > p.LastRecordID {
				p.LastRecordID = res.maxRecID
			}
			for _, ev := range res.events {
				p.RecordsRead++
				if ev.Timestamp.IsZero() {
					p.RecordsUndated++
				}
				// Stamp run-level provenance. Source metadata is uniform for a run and
				// deliberately excluded from HashNormalized, so it is applied here at the
				// single-goroutine sink rather than in the per-record normalizers.
				ev.SourceType = opts.SourceType
				ev.SourceIdentifier = opts.SourceIdentifier
				if opts.Idempotent {
					// Duplicate already in the current unflushed batch: bump its count.
					if canon, ok := pendingByHash[ev.HashNormalized]; ok {
						canon.DeduplicationCount++
						p.RecordsDuplicate++
						continue
					}
					// Duplicate of a canonical persisted earlier: defer a batched increment.
					if id, exists, err := sink.FindEventIDByHash(ev.HashNormalized); err != nil {
						fatal = fmt.Errorf(consts.MsgPersistFailed, err)
						break loop
					} else if exists {
						dbInc[id]++
						p.RecordsDuplicate++
						if len(dbInc) >= opts.BatchSize {
							if err := flushInc(); err != nil {
								fatal = err
								break loop
							}
						}
						continue
					}
					// First time this event is seen: it becomes the canonical node.
					pendingByHash[ev.HashNormalized] = ev
				}
				pending = append(pending, ev)
				if len(pending) >= opts.BatchSize {
					if err := flush(); err != nil {
						fatal = err
						break loop
					}
				}
			}
			// Stage this chunk's channel position now that all of its events are in the
			// batch; commitPositions writes it out after the next durable write.
			if res.channel != "" && res.maxRecID > staged[res.channel] {
				staged[res.channel] = res.maxRecID
			}
			// Continuous capture trickles rather than floods, so report every batch that
			// carried events — the record-count interval alone would leave live counters
			// looking frozen for minutes at a time.
			if (opts.Continuous && len(res.events) > 0) || p.RecordsRead-lastReported >= consts.ProgressInterval {
				reporter.Progress(p)
				lastReported = p.RecordsRead
			}
		}
	}

	// Persist whatever remains, unless we are aborting on a fatal error: the final
	// batch of new canonicals, then any deferred deduplication-count increments.
	if fatal == nil {
		if err := flush(); err != nil {
			fatal = err
		}
	}
	if fatal == nil {
		if err := flushInc(); err != nil {
			fatal = err
		}
	}
	// A cancelled capture still owns whatever it durably wrote, so its bookmarks must
	// land — otherwise resuming would re-read the tail of every stopped session.
	if fatal == nil {
		commitPositions()
	}

	summary := Summary{Progress: p, Checkpoint: Checkpoint{LastRecordID: p.LastRecordID}}
	switch {
	case fatal != nil:
		return summary, fatal
	case cancelled:
		reporter.Cancelled(summary)
		return summary, ctx.Err()
	default:
		reporter.Completed(summary)
		return summary, nil
	}
}
