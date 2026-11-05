package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/evtx"
	"rohy/backend/graphene"
	"rohy/backend/redaction"
)

// Redaction through the bindings (plan §14.5, item 40).

// ingestFixture runs one file ingest to the end and returns its summary.
func ingestFixture(t *testing.T, api *EventsAPI, em *captureEmitter) evtx.Summary {
	t.Helper()
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventIngestComplete, 20*time.Second) {
		t.Fatal("the ingest did not complete")
	}
	waitForState(t, api, consts.IngestStateIdle)
	last, _ := em.last(consts.EventIngestComplete)
	sum, ok := last.(evtx.Summary)
	if !ok {
		t.Fatalf("completion payload %T", last)
	}
	return sum
}

func TestAnIngestLeavesOutWhatARedactionRemoved(t *testing.T) {
	first, em, store := newEventsAPI(t)
	ingestFixture(t, first, em)
	events, err := store.QueryEvents(graphene.EventFilter{Limit: 1})
	if err != nil || len(events) != 1 || events[0].HashRaw == "" {
		t.Fatalf("fixture: %v %+v", err, events)
	}
	gone := events[0]

	j, err := redaction.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Begin(redaction.Entry{
		Event: gone.ID, Reason: "erasure request", At: time.Now().UTC(),
		Targets: []redaction.Target{{Node: gone.ID, HashNormalized: gone.HashNormalized, HashRaw: gone.HashRaw}},
	}); err != nil {
		t.Fatal(err)
	}

	again, em2, store2 := newEventsAPI(t)
	AttachRedactionJournal(again, j)
	sum := ingestFixture(t, again, em2)
	if sum.RecordsSuppressed < 1 {
		t.Errorf("nothing suppressed: %+v", sum.Progress)
	}
	if id, found, err := store2.FindEventIDByHash(gone.HashNormalized); err != nil || found {
		t.Errorf("the redacted record came back as event %d (%v)", id, err)
	}
}

func TestADamagedJournalRefusesTheIngestAtOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, consts.RedactionJournalFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	j, err := redaction.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	api, _, store := newEventsAPI(t)
	AttachRedactionJournal(api, j)
	err = api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}})
	var ae ErrorEvent
	if !errors.As(err, &ae) || ae.Code != consts.ErrCodePersistence {
		t.Fatalf("StartIngest = %v", err)
	}
	if api.IsIngesting() {
		t.Error("a refused ingest is running")
	}
	if n, _, _ := store.Stats(); n != 0 {
		t.Errorf("%d events written", n)
	}
}

func TestANilJournalSuppressesNothing(t *testing.T) {
	api, em, _ := newEventsAPI(t)
	AttachRedactionJournal(api, nil)
	if sum := ingestFixture(t, api, em); sum.RecordsSuppressed != 0 || sum.RecordsPersisted == 0 {
		t.Errorf("%+v", sum.Progress)
	}
}

// 🔒 A finding written after a redaction cannot carry the content back in its descriptor: a drawer
// that was open across the redaction still holds the event as it was.
func TestAFindingOnARedactedEventKeepsNoDescriptor(t *testing.T) {
	j, err := redaction.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Begin(redaction.Entry{
		Event: 7, Reason: "erasure request", At: time.Now().UTC(),
		Targets: []redaction.Target{{Node: 7, HashNormalized: "gone"}},
	}); err != nil {
		t.Fatal(err)
	}
	fa := AttachFindingsRedactions(NewFindingsAPI(mustFindings(t), nil), j)
	f, err := fa.SetFinding(FindingRequest{Key: "gone", Note: "kept", Descriptor: "4624 HOST-A alice"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Descriptor != consts.RedactedDescriptor || f.Note != "kept" {
		t.Errorf("stored %+v", f)
	}
	if f, _ := fa.SetFinding(FindingRequest{Key: "live", Flagged: true, Descriptor: "4625 HOST-B"}); f.Descriptor != "4625 HOST-B" {
		t.Errorf("a live event's descriptor was replaced: %+v", f)
	}
}
