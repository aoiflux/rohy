package caseintegrity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"rohy/backend/consts"
)

// The checks after a redaction. A redaction removes on purpose what the
// checks would otherwise read as damage, waste, or work to do.

func redactedHash(h string) bool { return h == "hash-redacted" }

// A finding on a redacted event is not an orphan to fix by ingesting: the ingest would refuse it.
func TestAFindingOnARedactedEventIsNotAnOrphan(t *testing.T) {
	deps := healthy()
	deps.Findings = &fakeFindings{keys: []string{"hash-a", "hash-redacted", "hash-gone"}}
	deps.Redacted = redactedHash
	got := Run(context.Background(), deps, Options{})
	if f := has(got, consts.IntegrityOrphanFindings); f == nil || f.Count != 1 {
		t.Errorf("orphans %+v", f)
	}
	f := has(got, consts.IntegrityRedactedFindings)
	if f == nil || f.Count != 1 || f.Action != consts.IntegrityActionReview || f.Severity != consts.IntegritySevInfo {
		t.Fatalf("redacted findings %+v", f)
	}
}

func TestAllFindingsOnRedactedEventsRaiseNoOrphanFinding(t *testing.T) {
	deps := healthy()
	deps.Findings = &fakeFindings{keys: []string{"hash-a", "hash-redacted"}}
	deps.Redacted = redactedHash
	if f := has(Run(context.Background(), deps, Options{}), consts.IntegrityOrphanFindings); f != nil {
		t.Errorf("orphans %+v", f)
	}
}

// Tombstoned records at the end of the log are a redaction's, not an interrupted ingest's.
func TestTombstonesAreNotPayloadTail(t *testing.T) {
	deps := healthy()
	deps.PayloadSize, deps.PayloadUsed, deps.RedactedPayloadEnd = 4096, 4000, 4096
	if f := has(Run(context.Background(), deps, Options{}), consts.IntegrityPayloadTail); f != nil {
		t.Errorf("tail %+v", f)
	}
	deps.RedactedPayloadEnd = 4050
	if f := has(Run(context.Background(), deps, Options{}), consts.IntegrityPayloadTail); f == nil || f.Count != 46 {
		t.Errorf("tail past the tombstones %+v", f)
	}
}

func TestAnUnfinishedRedactionIsAWarningWithItsRemedy(t *testing.T) {
	deps := healthy()
	deps.RedactionsPending = 2
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityRedactionPending)
	if f == nil || f.Count != 2 || f.Severity != consts.IntegritySevWarn || f.Action != consts.IntegrityActionFinishRedaction {
		t.Fatalf("%+v", f)
	}
}

// 🔒 A journal that cannot be read means ingest is refused and nothing is suppressed: an error.
func TestAnUnreadableJournalIsAnError(t *testing.T) {
	deps := healthy()
	deps.RedactionJournalErr = errors.New("the redaction journal journal.json is not valid")
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityRedactionJournal)
	if f == nil || f.Severity != consts.IntegritySevError || !strings.Contains(f.Message, "journal.json") {
		t.Fatalf("%+v", f)
	}
}

func TestNoRedactionsNoFindings(t *testing.T) {
	got := Run(context.Background(), healthy(), Options{})
	for _, code := range []string{consts.IntegrityRedactedFindings, consts.IntegrityRedactionPending, consts.IntegrityRedactionJournal} {
		if has(got, code) != nil {
			t.Errorf("%s on a case with no redactions", code)
		}
	}
}
