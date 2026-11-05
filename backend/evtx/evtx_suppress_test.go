package evtx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// Suppression (plan §14.5, item 40): a redacted record does not come back with a re-ingest.

type fakeSuppressor struct {
	err        error
	normalized map[string]bool
	raw        map[string]bool
}

func (f fakeSuppressor) Err() error { return f.err }
func (f fakeSuppressor) Suppressed(hn, hr string) bool {
	return (hn != "" && f.normalized[hn]) || (hr != "" && f.raw[hr])
}

func sinkRun(t *testing.T, store *graphene.Store, opts Options, events ...*graphene.Event) Summary {
	t.Helper()
	resultCh := make(chan chunkResult, 1)
	resultCh <- chunkResult{events: events}
	close(resultCh)
	sum, err := runSink(context.Background(), opts, store, NoopReporter{}, resultCh, 1)
	if err != nil {
		t.Fatalf("runSink: %v", err)
	}
	return sum
}

func TestASuppressedRecordIsSkippedAndCounted(t *testing.T) {
	for _, idempotent := range []bool{true, false} {
		store := graphene.OpenInMemory()
		opts := Options{
			Source: consts.SourceFile, Idempotent: idempotent, BatchSize: 2,
			Suppress: fakeSuppressor{raw: map[string]bool{"raw-A": true}},
		}
		sum := sinkRun(t, store, opts, dedupEvent("A"), dedupEvent("B"), dedupEvent("A"))
		if sum.RecordsSuppressed != 2 || sum.RecordsRead != 3 {
			t.Errorf("idempotent %v: suppressed %d of %d read", idempotent, sum.RecordsSuppressed, sum.RecordsRead)
		}
		// 🔒 A suppressed record is not a duplicate either: it must not add to anything.
		if sum.RecordsDuplicate != 0 || sum.RecordsPersisted != 1 {
			t.Errorf("idempotent %v: %+v", idempotent, sum.Progress)
		}
		if counts := countByHash(t, store); len(counts) != 1 || counts["B"] != 1 {
			t.Errorf("idempotent %v: stored %v", idempotent, counts)
		}
		store.Close()
	}
}

// 🔒 The identity checked is the one the sink stamps, which includes the source. Checking the
// normalizer's hash instead would let every redacted record back in.
func TestSuppressionUsesTheStampedIdentity(t *testing.T) {
	first := graphene.OpenInMemory()
	defer first.Close()
	opts := Options{Source: consts.SourceFile, SourceIdentifier: `C:\cases\Security.evtx`, Idempotent: true}
	sinkRun(t, first, opts, dedupEvent("A"))
	stored, err := first.QueryEvents(graphene.EventFilter{Limit: 10})
	if err != nil || len(stored) != 1 {
		t.Fatalf("query: %v %d", err, len(stored))
	}
	if stored[0].HashNormalized == dedupEvent("A").HashNormalized {
		t.Fatal("fixture: the stamped identity equals the normalizer's, so this test proves nothing")
	}

	again := graphene.OpenInMemory()
	defer again.Close()
	opts.Suppress = fakeSuppressor{normalized: map[string]bool{stored[0].HashNormalized: true}}
	ev := dedupEvent("A")
	ev.HashRaw = "a raw record that differs"
	if sum := sinkRun(t, again, opts, ev); sum.RecordsSuppressed != 1 || sum.RecordsPersisted != 0 {
		t.Errorf("%+v", sum.Progress)
	}
}

// 🔒 A suppression list that cannot be read stops the ingest before it reads a record.
func TestAnUnreadableSuppressionListRefusesTheIngest(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	broken := errors.New("the redaction journal journal.json cannot be read")
	_, err := Ingest(context.Background(), Options{
		Source: consts.SourceFile, Path: "testdata/Security.evtx",
		Suppress: fakeSuppressor{err: broken},
	}, store, nil)
	if err == nil || !strings.Contains(err.Error(), "journal.json") {
		t.Fatalf("Ingest = %v", err)
	}
	if n, _, _ := store.Stats(); n != 0 {
		t.Errorf("%d events written", n)
	}
}

func TestNoSuppressorSuppressesNothing(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	sum := sinkRun(t, store, Options{Source: consts.SourceFile}, dedupEvent("A"))
	if sum.RecordsSuppressed != 0 || sum.RecordsPersisted != 1 {
		t.Errorf("%+v", sum.Progress)
	}
}
