package api

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/findings"
	"rohy/backend/graphene"
)

// findingsFixture ingests the Security fixture and returns the events binding, a findings
// binding sharing one sidecar, and the first few ingested events to annotate.
func findingsFixture(t *testing.T) (*EventsAPI, *FindingsAPI, []*graphene.Event) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	fs := mustFindings(t)
	events := NewEventsAPI(store, mustCapture(t), fs)
	em := newCaptureEmitter()
	events.setEmitter(em)

	if err := events.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventIngestComplete, 10*time.Second) {
		t.Fatal("ingest did not complete")
	}

	all, err := events.QueryEvents(EventQuery{Limit: 3})
	if err != nil || len(all) < 3 {
		t.Fatalf("need >=3 events, got %d err=%v", len(all), err)
	}
	return events, NewFindingsAPI(fs, store), all
}

func mustSetFinding(t *testing.T, api *FindingsAPI, req FindingRequest) *findings.Finding {
	t.Helper()
	f, err := api.SetFinding(req)
	if err != nil {
		t.Fatalf("SetFinding: %v", err)
	}
	return f
}

// queryIDs runs a query and returns the matching event ids.
func queryIDs(t *testing.T, api *EventsAPI, q EventQuery) []uint64 {
	t.Helper()
	got, err := api.QueryEvents(q)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	ids := make([]uint64, len(got))
	for i, e := range got {
		ids[i] = e.ID
	}
	return ids
}

func TestSetFindingRoundTripsThroughTheBinding(t *testing.T) {
	_, fa, all := findingsFixture(t)
	got := mustSetFinding(t, fa, FindingRequest{
		Key:     all[0].HashNormalized,
		Flagged: true,
		Tags:    []string{"Initial Access"},
		Note:    "first logon",
	})
	if got == nil || !got.Flagged || got.Note != "first logon" {
		t.Fatalf("finding = %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("binding did not stamp the timestamps")
	}
	if back := fa.GetFinding(all[0].HashNormalized); back == nil || back.Note != "first logon" {
		t.Fatalf("GetFinding = %+v", back)
	}
}

// The frontend annotates by content hash, so the finding must be reachable from any event
// carrying that hash — that is what makes annotations survive a re-ingest.
func TestFindingIsKeyedByContentHashNotNodeID(t *testing.T) {
	_, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true})

	if fa.GetFinding(all[0].HashNormalized) == nil {
		t.Fatal("finding not reachable by its event's hash")
	}
	// A different event (different content) must not inherit it.
	if all[1].HashNormalized != all[0].HashNormalized && fa.GetFinding(all[1].HashNormalized) != nil {
		t.Error("finding leaked onto an unrelated event")
	}
}

func TestGetFindingsBatchesAPage(t *testing.T) {
	_, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true})

	keys := []string{all[0].HashNormalized, all[1].HashNormalized, all[2].HashNormalized}
	got := fa.GetFindings(keys)
	if got[all[0].HashNormalized] == nil {
		t.Error("annotated event missing from the batch")
	}
	if len(got) != 1 {
		t.Errorf("batch returned %d entries, want only the annotated one", len(got))
	}
}

func TestFlaggedFilterReturnsOnlyFlaggedEvents(t *testing.T) {
	ea, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true})

	ids := queryIDs(t, ea, EventQuery{FindingState: consts.FindingFilterFlagged})
	if len(ids) == 0 {
		t.Fatal("flagged filter returned nothing")
	}
	for _, id := range ids {
		e, err := ea.GetEvent(id)
		if err != nil {
			t.Fatal(err)
		}
		if e.HashNormalized != all[0].HashNormalized {
			t.Fatalf("event %d is not the flagged one", id)
		}
	}
}

// A filter with nothing to match must return nothing — not fall back to the whole case.
func TestFlaggedFilterWithNoFlagsReturnsNothing(t *testing.T) {
	ea, fa, all := findingsFixture(t)
	// A finding that exists but is not flagged must not satisfy the flagged filter.
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Note: "just a note"})

	if ids := queryIDs(t, ea, EventQuery{FindingState: consts.FindingFilterFlagged}); len(ids) != 0 {
		t.Fatalf("flagged filter returned %d events with nothing flagged", len(ids))
	}
}

func TestNotedAndAnnotatedFiltersDiffer(t *testing.T) {
	ea, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Note: "reasoning"})
	mustSetFinding(t, fa, FindingRequest{Key: all[1].HashNormalized, Flagged: true})

	noted := queryIDs(t, ea, EventQuery{FindingState: consts.FindingFilterNoted})
	annotated := queryIDs(t, ea, EventQuery{FindingState: consts.FindingFilterAnnotated})
	if len(noted) != 1 {
		t.Errorf("noted = %d events, want 1", len(noted))
	}
	if len(annotated) != 2 {
		t.Errorf("annotated = %d events, want 2", len(annotated))
	}
}

// "No findings" is the complement, so it must return the rest of the case rather than an
// empty list.
func TestNoneFilterReturnsTheUnannotatedRemainder(t *testing.T) {
	ea, fa, all := findingsFixture(t)
	total, err := ea.CountEvents(EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true})

	n, err := ea.CountEvents(EventQuery{FindingState: consts.FindingFilterNone})
	if err != nil {
		t.Fatal(err)
	}
	if n != total-1 {
		t.Fatalf("none-filter count = %d, want %d (all but the annotated event)", n, total-1)
	}
}

func TestTagFilterSelectsByTag(t *testing.T) {
	ea, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Tags: []string{"persistence"}})
	mustSetFinding(t, fa, FindingRequest{Key: all[1].HashNormalized, Tags: []string{"recon"}})

	if ids := queryIDs(t, ea, EventQuery{Tag: "persistence"}); len(ids) != 1 {
		t.Fatalf("tag filter matched %d events, want 1", len(ids))
	}
	// Tags are normalized on write, so a differently-cased filter still matches.
	if ids := queryIDs(t, ea, EventQuery{Tag: "PERSISTENCE"}); len(ids) != 1 {
		t.Fatalf("case-insensitive tag filter matched %d events, want 1", len(ids))
	}
}

// Two finding filters must narrow, not widen.
func TestTagAndStateFiltersIntersect(t *testing.T) {
	ea, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true, Tags: []string{"persistence"}})
	mustSetFinding(t, fa, FindingRequest{Key: all[1].HashNormalized, Tags: []string{"persistence"}})

	both := queryIDs(t, ea, EventQuery{FindingState: consts.FindingFilterFlagged, Tag: "persistence"})
	if len(both) != 1 {
		t.Fatalf("flagged+tagged matched %d events, want only the event that is both", len(both))
	}
}

// The count and the list must agree, or paging reports a total it cannot deliver.
func TestCountAgreesWithQueryUnderFindingFilters(t *testing.T) {
	ea, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true})
	mustSetFinding(t, fa, FindingRequest{Key: all[1].HashNormalized, Flagged: true})

	q := EventQuery{FindingState: consts.FindingFilterFlagged}
	n, err := ea.CountEvents(q)
	if err != nil {
		t.Fatal(err)
	}
	if ids := queryIDs(t, ea, q); len(ids) != n {
		t.Fatalf("count = %d but query returned %d", n, len(ids))
	}
}

// The ordered-id cache is keyed by filter; a findings filter applied after an unfiltered
// query must not be served the unfiltered ordering.
func TestFindingFilterIsNotServedAStaleOrderCache(t *testing.T) {
	ea, fa, all := findingsFixture(t)

	before := len(queryIDs(t, ea, EventQuery{}))
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true})

	flagged := queryIDs(t, ea, EventQuery{FindingState: consts.FindingFilterFlagged})
	if len(flagged) >= before {
		t.Fatalf("flagged query returned %d of %d events — the unfiltered order was reused", len(flagged), before)
	}
	// And the unfiltered query still works afterwards.
	if got := len(queryIDs(t, ea, EventQuery{})); got != before {
		t.Fatalf("unfiltered query now returns %d, want %d", got, before)
	}
}

// Removing the last content clears the finding, so the event stops matching finding filters.
func TestClearingAFindingRemovesItFromFilters(t *testing.T) {
	ea, fa, all := findingsFixture(t)
	key := all[0].HashNormalized
	mustSetFinding(t, fa, FindingRequest{Key: key, Flagged: true})
	if got := mustSetFinding(t, fa, FindingRequest{Key: key}); got != nil {
		t.Fatalf("cleared finding returned %+v, want nil", got)
	}
	if ids := queryIDs(t, ea, EventQuery{FindingState: consts.FindingFilterAnnotated}); len(ids) != 0 {
		t.Fatalf("cleared event still matches the annotated filter (%d)", len(ids))
	}
}

func TestRemoveFindingThroughTheBinding(t *testing.T) {
	_, fa, all := findingsFixture(t)
	key := all[0].HashNormalized
	mustSetFinding(t, fa, FindingRequest{Key: key, Flagged: true})
	if err := fa.RemoveFinding(key); err != nil {
		t.Fatalf("RemoveFinding: %v", err)
	}
	if fa.GetFinding(key) != nil {
		t.Error("finding survived removal")
	}
}

func TestSetFindingRejectsAMissingKey(t *testing.T) {
	_, fa, _ := findingsFixture(t)
	_, err := fa.SetFinding(FindingRequest{Note: "orphan"})
	if err == nil {
		t.Fatal("expected an error for a finding with no event")
	}
	if ee, ok := err.(ErrorEvent); !ok || ee.Code != consts.ErrCodeParse {
		t.Errorf("err = %#v, want a parse-coded ErrorEvent", err)
	}
}

func TestSetFindingRejectsAnOverlongNote(t *testing.T) {
	_, fa, all := findingsFixture(t)
	_, err := fa.SetFinding(FindingRequest{
		Key:  all[0].HashNormalized,
		Note: strings.Repeat("x", consts.MaxFindingNoteLen+1),
	})
	if err == nil {
		t.Fatal("expected an error for an over-long note")
	}
}

func TestListFindingsAndTagsAndStats(t *testing.T) {
	_, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true, Tags: []string{"shared"}})
	mustSetFinding(t, fa, FindingRequest{Key: all[1].HashNormalized, Note: "n", Tags: []string{"shared"}})

	if got := fa.ListFindings(); len(got) != 2 {
		t.Errorf("ListFindings = %d, want 2", len(got))
	}
	tags := fa.ListTags()
	if len(tags) != 1 || tags[0].Tag != "shared" || tags[0].Count != 2 {
		t.Errorf("ListTags = %+v", tags)
	}
	stats := fa.FindingStats()
	if stats.Total != 2 || stats.Flagged != 1 || stats.Noted != 1 || stats.Tagged != 2 {
		t.Errorf("FindingStats = %+v", stats)
	}
}

// A finding on an event that IS in the case must audit as live.
func TestAuditCountsLiveFindings(t *testing.T) {
	_, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true})

	audit, err := fa.AuditFindings()
	if err != nil {
		t.Fatalf("AuditFindings: %v", err)
	}
	if audit.Total != 1 || audit.Live != 1 || len(audit.Orphans) != 0 {
		t.Fatalf("audit = %+v, want 1 total / 1 live / 0 orphans", audit)
	}
	if audit.Stale {
		t.Error("a sidecar written by this build audits as stale")
	}
}

// A finding whose event is not in the case is reported as an orphan rather than counted as
// real work. This is the case-switching scenario: the sidecar outlives the events.
func TestAuditReportsOrphansInsteadOfInflatingCounts(t *testing.T) {
	_, fa, all := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: all[0].HashNormalized, Flagged: true})
	mustSetFinding(t, fa, FindingRequest{
		Key:        "a-hash-no-event-in-this-case-can-produce",
		Flagged:    true,
		Descriptor: "4624 · Microsoft-Windows-Security-Auditing · 2026-07-14",
	})

	audit, err := fa.AuditFindings()
	if err != nil {
		t.Fatalf("AuditFindings: %v", err)
	}
	if audit.Total != 2 || audit.Live != 1 {
		t.Fatalf("audit = %d total / %d live, want 2 / 1", audit.Total, audit.Live)
	}
	if len(audit.Orphans) != 1 {
		t.Fatalf("orphans = %d, want 1", len(audit.Orphans))
	}
	// The descriptor is what makes an orphan meaningful — a bare hash tells the reader
	// nothing about what was marked.
	if audit.Orphans[0].Descriptor == "" {
		t.Error("orphan reported without the descriptor that explains what it marked")
	}
}

// Auditing must not delete anything: re-ingesting the missing source brings the events back
// and the findings reattach, so tidying a count would destroy irreplaceable work.
func TestAuditDoesNotRemoveOrphans(t *testing.T) {
	_, fa, _ := findingsFixture(t)
	mustSetFinding(t, fa, FindingRequest{Key: "orphan-hash", Note: "still mine"})

	if _, err := fa.AuditFindings(); err != nil {
		t.Fatalf("AuditFindings: %v", err)
	}
	if got := fa.GetFinding("orphan-hash"); got == nil || got.Note != "still mine" {
		t.Fatalf("audit destroyed an orphan finding: %+v", got)
	}
}

func TestAuditOnAnEmptyCase(t *testing.T) {
	_, fa, _ := findingsFixture(t)
	audit, err := fa.AuditFindings()
	if err != nil {
		t.Fatalf("AuditFindings: %v", err)
	}
	if audit.Total != 0 || audit.Live != 0 || len(audit.Orphans) != 0 {
		t.Fatalf("audit = %+v, want all zero", audit)
	}
	if audit.HashVersion != consts.FindingsHashVersion {
		t.Errorf("hash version = %d, want %d", audit.HashVersion, consts.FindingsHashVersion)
	}
}

// A binding constructed without a findings sidecar must ignore the filters rather than
// crash — the events list stays usable.
func TestFindingFiltersAreIgnoredWithoutASidecar(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	ea := NewEventsAPI(store, mustCapture(t), nil)
	if _, err := ea.QueryEvents(EventQuery{FindingState: consts.FindingFilterFlagged, Tag: "x"}); err != nil {
		t.Fatalf("query with no sidecar: %v", err)
	}
}
