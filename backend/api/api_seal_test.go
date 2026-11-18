package api

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

// Case sealing at the ingest boundary.
//
// The single number these tests defend: compaction costs ~1.3 s and 566 MB on a 100k case after
// adding only 500 events, because it rewrites the whole image regardless of how little was added
// (PERFORMANCE.md §18). It is therefore proportional to the CASE, not to the ingest — which makes
// exactly one placement correct, and several plausible ones expensive and pointless.

// waitIdle blocks until the ingest goroutine has finished its deferred cleanup.
func waitIdle(t *testing.T, api *EventsAPI) {
	t.Helper()
	for i := 0; i < 300 && api.IsIngesting(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if api.IsIngesting() {
		t.Fatal("still ingesting after the run should have finished")
	}
}

// 🔒 ONCE PER REQUEST, NOT ONCE PER FILE. This is the assertion the placement exists for: a
// twelve-file ingest that sealed per file would pay a full compaction twelve times and discard
// eleven of the results, since only the last roots survive.
func TestMultiFileIngestSealsOnceNotPerFile(t *testing.T) {
	api, em, _ := newEventsAPI(t)

	// The same fixture three times. Idempotent so the duplicate records collapse — what is being
	// counted is seal events per REQUEST, not events persisted.
	if err := api.StartIngest(IngestRequest{
		Source:     consts.SourceFile,
		Paths:      []string{fixtureSecurity, fixtureSecurity, fixtureSecurity},
		Idempotent: true,
	}); err != nil {
		t.Fatalf("StartIngest: %v", err)
	}
	if !em.waitFor(consts.EventCaseSealed, 30*time.Second) {
		t.Fatal("the case was never sealed after an ingest")
	}
	waitIdle(t, api)

	if got := em.count(consts.EventIngestStarted); got != 3 {
		t.Fatalf("Started events = %d, want 3 — the fixture setup is not what this test assumes", got)
	}
	if got := em.count(consts.EventCaseSealed); got != 1 {
		t.Errorf("sealed %d times for a 3-file request, want 1: each seal rewrites the whole "+
			"image, so the extra ones cost a full compaction and are then thrown away", got)
	}
	if got := em.count(consts.EventCaseSealing); got != 1 {
		t.Errorf("sealing announced %d times, want 1", got)
	}
}

// The wait is ANNOUNCED. On a large case sealing takes over a second, and a silent second after
// "ingest complete" is indistinguishable from a hang — the same reason the backfill reports
// progress rather than returning quietly.
func TestSealingIsAnnouncedBeforeItStarts(t *testing.T) {
	api, em, _ := newEventsAPI(t)

	if err := api.StartIngest(IngestRequest{
		Source: consts.SourceFile, Paths: []string{fixtureSecurity},
	}); err != nil {
		t.Fatalf("StartIngest: %v", err)
	}
	if !em.waitFor(consts.EventCaseSealed, 30*time.Second) {
		t.Fatal("the case was never sealed")
	}
	waitIdle(t, api)

	if em.count(consts.EventCaseSealing) == 0 {
		t.Error("sealing completed without ever announcing that it started")
	}
	// And it happens AFTER the ingest itself, not instead of it.
	if em.count(consts.EventIngestComplete) == 0 {
		t.Error("no ingest completion — sealing displaced the ingest rather than following it")
	}
}

// 🔒 A cancelled ingest is still sealed. What it wrote is durable and already in the case; leaving
// it unsealed would mean a cancelled ingest silently produced evidence that nothing covers, which
// is precisely the state this change exists to remove.
func TestCancelledIngestIsStillSealed(t *testing.T) {
	api, em, _ := newEventsAPI(t)

	if err := api.StartIngest(IngestRequest{
		Source: consts.SourceFile, Paths: []string{fixtureSecurity},
	}); err != nil {
		t.Fatalf("StartIngest: %v", err)
	}
	// Cancel as early as the binding allows. Whether any records landed first is timing-dependent
	// and deliberately not asserted — the invariant is that the seal happens either way.
	api.CancelIngestion()

	if !em.waitFor(consts.EventCaseSealed, 30*time.Second) {
		t.Fatal("a cancelled ingest was never sealed — anything it wrote is unaccounted for")
	}
	waitIdle(t, api)
}

// 🔒 A sealing failure does not fail the ingest. The events are persisted; sealing is what gives
// them a fingerprint. Reporting the ingest as failed would be false and would invite somebody to
// re-run an ingest that had already succeeded.
//
// The in-memory backend has no image to compact, so Compact is a no-op there and this test cannot
// force the error path. What it CAN pin is the shape that matters: the ingest reports success and
// the run reaches the sealed state regardless.
func TestSealingDoesNotMaskIngestSuccess(t *testing.T) {
	api, em, store := newEventsAPI(t)

	if err := api.StartIngest(IngestRequest{
		Source: consts.SourceFile, Paths: []string{fixtureSecurity},
	}); err != nil {
		t.Fatalf("StartIngest: %v", err)
	}
	if !em.waitFor(consts.EventCaseSealed, 30*time.Second) {
		t.Fatal("the case was never sealed")
	}
	waitIdle(t, api)

	if em.count(consts.EventIngestComplete) == 0 {
		t.Error("the ingest did not report completion")
	}
	nodes, _, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if nodes == 0 {
		t.Error("no events persisted — sealing must not be able to discard the ingest")
	}
}
