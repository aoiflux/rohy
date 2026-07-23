package evtx

import (
	"context"
	"os"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

const (
	fixtureOneRecord = "testdata/Security_1_record.evtx"
	fixtureSecurity  = "testdata/Security.evtx"
)

// captureReporter records lifecycle callbacks for assertions. Ingest guarantees a
// single reporting goroutine, so no locking is required here.
type captureReporter struct {
	started    int
	progress   int
	recordErrs []string
	completed  *Summary
	cancelled  *Summary
}

func (r *captureReporter) Started(string, int) { r.started++ }
func (r *captureReporter) Progress(Progress)   { r.progress++ }
func (r *captureReporter) RecordError(_, msg string) {
	r.recordErrs = append(r.recordErrs, msg)
}
func (r *captureReporter) Completed(s Summary) { r.completed = &s }
func (r *captureReporter) Cancelled(s Summary) { r.cancelled = &s }

func TestNormalizeRecordFromFixture(t *testing.T) {
	fd, err := os.Open(fixtureOneRecord)
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()

	offsets, err := chunkOffsets(fd)
	if err != nil {
		t.Fatalf("chunkOffsets: %v", err)
	}
	if len(offsets) == 0 {
		t.Fatal("no chunks found in fixture")
	}

	recs, err := parseChunkAt(fd, offsets[0], 0)
	if err != nil {
		t.Fatalf("parseChunkAt: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("no records parsed")
	}

	ev, err := normalizeRecord(recs[0])
	if err != nil {
		t.Fatalf("normalizeRecord: %v", err)
	}

	if ev.EventID != "1102" {
		t.Errorf("EventID = %q, want 1102", ev.EventID)
	}
	if ev.Channel != consts.ChannelSecurity {
		t.Errorf("Channel = %q, want %q", ev.Channel, consts.ChannelSecurity)
	}
	if ev.Computer != "TestComputer" {
		t.Errorf("Computer = %q, want TestComputer", ev.Computer)
	}
	if !strings.Contains(ev.Provider, "Eventlog") {
		t.Errorf("Provider = %q, want to contain Eventlog", ev.Provider)
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if ev.Timestamp.Year() != 2019 {
		t.Errorf("Timestamp year = %d, want 2019", ev.Timestamp.Year())
	}
	if len(ev.HashRaw) != 64 || len(ev.HashNormalized) != 64 {
		t.Errorf("hashes not 64 hex chars: raw=%d norm=%d", len(ev.HashRaw), len(ev.HashNormalized))
	}
	if ev.HashRaw == ev.HashNormalized {
		t.Error("hash_raw and hash_normalized should differ")
	}
	// The UserData payload fields must be flattened into ParsedFields. This event's
	// UserData wraps its fields in a LogFileCleared element, so the nested key is
	// prefixed with that element name (lossless flattening).
	if got := ev.ParsedFields["LogFileCleared.SubjectUserName"]; got != "test" {
		t.Errorf("ParsedFields[LogFileCleared.SubjectUserName] = %q, want test; got fields %v", got, ev.ParsedFields)
	}
}

func TestIngestFileEndToEnd(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	rep := &captureReporter{}
	opts := Options{Source: consts.SourceFile, Path: fixtureSecurity, BatchSize: 8, Workers: 3}

	sum, err := Ingest(context.Background(), opts, store, rep)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if rep.started != 1 {
		t.Errorf("Started called %d times, want 1", rep.started)
	}
	if rep.completed == nil {
		t.Fatal("Completed never called")
	}
	if sum.RecordsPersisted == 0 {
		t.Fatal("no records persisted")
	}
	if sum.RecordsPersisted != sum.RecordsRead {
		t.Errorf("persisted %d != read %d (unexpected skips/dupes)", sum.RecordsPersisted, sum.RecordsRead)
	}

	nodes, _, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if int(nodes) != sum.RecordsPersisted {
		t.Errorf("store has %d nodes, summary says %d persisted", nodes, sum.RecordsPersisted)
	}

	// Every persisted event must be chronologically queryable and hashed.
	events, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != sum.RecordsPersisted {
		t.Errorf("QueryEvents returned %d, want %d", len(events), sum.RecordsPersisted)
	}
	for _, e := range events {
		if e.HashNormalized == "" || e.Channel == "" {
			t.Fatalf("event %d missing hash/channel: %+v", e.ID, e)
		}
	}
}

func TestIngestStampsSourceMetadata(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	opts := Options{
		Source:           consts.SourceFile,
		Path:             fixtureSecurity,
		SourceType:       consts.SourceTypeMultiEVTX,
		SourceIdentifier: fixtureSecurity,
		BatchSize:        8,
		Workers:          3,
	}
	sum, err := Ingest(context.Background(), opts, store, NoopReporter{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if sum.RecordsPersisted == 0 {
		t.Fatal("no records persisted")
	}

	events, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.SourceType != consts.SourceTypeMultiEVTX {
			t.Fatalf("event %d SourceType = %q, want %q", e.ID, e.SourceType, consts.SourceTypeMultiEVTX)
		}
		if e.SourceIdentifier != fixtureSecurity {
			t.Fatalf("event %d SourceIdentifier = %q, want %q", e.ID, e.SourceIdentifier, fixtureSecurity)
		}
		if e.DeduplicationCount != consts.DefaultDeduplicationCount {
			t.Fatalf("event %d DeduplicationCount = %d, want %d", e.ID, e.DeduplicationCount, consts.DefaultDeduplicationCount)
		}
	}

	// The source_type index must serve an equality query for the stamped type.
	filtered, err := store.QueryEvents(graphene.EventFilter{SourceType: consts.SourceTypeMultiEVTX})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != len(events) {
		t.Errorf("source_type filter returned %d, want %d", len(filtered), len(events))
	}
}

func TestIngestIdempotentResume(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	opts := Options{Source: consts.SourceFile, Path: fixtureSecurity, BatchSize: 16, Workers: 2, Idempotent: true}

	first, err := Ingest(context.Background(), opts, store, NoopReporter{})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.RecordsPersisted == 0 {
		t.Fatal("first ingest persisted nothing")
	}

	// Re-ingesting the same file with idempotency on must persist nothing new: every
	// record is recognized as a duplicate by hash_normalized.
	second, err := Ingest(context.Background(), opts, store, NoopReporter{})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.RecordsPersisted != 0 {
		t.Errorf("second ingest persisted %d, want 0 (all duplicates)", second.RecordsPersisted)
	}
	if second.RecordsDuplicate != first.RecordsPersisted {
		t.Errorf("duplicates = %d, want %d", second.RecordsDuplicate, first.RecordsPersisted)
	}

	nodes, _, _ := store.Stats()
	if int(nodes) != first.RecordsPersisted {
		t.Errorf("store grew on resume: %d nodes, want %d", nodes, first.RecordsPersisted)
	}
}

func TestIngestCancellation(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	rep := &captureReporter{}
	opts := Options{Source: consts.SourceFile, Path: fixtureSecurity, Workers: 2}
	_, err := Ingest(ctx, opts, store, rep)
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if rep.cancelled == nil {
		t.Error("Cancelled never called")
	}
	if rep.completed != nil {
		t.Error("Completed should not be called on cancellation")
	}
}

func TestIngestUnknownSource(t *testing.T) {
	_, err := Ingest(context.Background(), Options{Source: "bogus"}, graphene.OpenInMemory(), NoopReporter{})
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}
