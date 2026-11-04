package caseintegrity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// fakeRoots is the fingerprint history, reduced to the one method the check reads.
//
// 🔒 The method is ExportedAt, not RetainedAt. rohy can see that a copy left the app and cannot see
// whether one is held anywhere, and the interface is named for what is observable.
type fakeRoots struct {
	exported map[string]time.Time
}

func (f *fakeRoots) ExportedAt(snapshot string) (time.Time, bool) {
	at, ok := f.exported[snapshot]
	return at, ok
}

// withRoots returns a posture carrying a fingerprint, so the retained-root check has something to
// look up.
func withRoots(snapshot string) *fakeForensics {
	f := compactedAndClean()
	f.status.Roots = &graphene.SnapshotRoots{Snapshot: snapshot, NodeRoot: "n", EdgeRoot: "e"}
	return f
}

// Forensic posture detectors (plan §14.8).
//
// These live in their own file because they answer a different question from every other detector
// in this package. The rest ask "is this case broken"; these ask "what can this case prove", and
// for an existing case the answer is mostly a consequence of when it was written rather than of
// anything being wrong. The severities reflect that, and several tests below exist specifically to
// stop a posture from being reported as a fault.

type fakeForensics struct {
	status   graphene.ForensicStatus
	check    graphene.ImageCheck
	checkErr error
	verifies int
}

func (f *fakeForensics) ForensicStatus() graphene.ForensicStatus { return f.status }
func (f *fakeForensics) VerifyImage() (graphene.ImageCheck, error) {
	f.verifies++
	return f.check, f.checkErr
}

// compactedAndClean is the best posture reachable without a signing key: compacted, and the image
// verifies.
func compactedAndClean() *fakeForensics {
	return &fakeForensics{
		status: graphene.ForensicStatus{Available: true, Compacted: true},
		check:  graphene.ImageCheck{Digest: graphene.DigestMatch, RootsChecked: true},
	}
}

// 🔒 The invariant the file is built on: a posture that could not be READ is reported as unknown,
// never as fine. A forensic all-clear from a check that did not look is the most damaging thing
// this report could produce, and a nil dependency is the easiest way to get one.
func TestMissingForensicDepsReportsUnknownNotClean(t *testing.T) {
	rep := Run(context.Background(), Deps{}, Options{})

	if len(rep.Errors) == 0 {
		t.Fatal("no forensic store and no note about it — the section reads as clean")
	}
	if joined := strings.Join(rep.Errors, " | "); !strings.Contains(joined, "posture") {
		t.Errorf("errors %q do not mention the posture", joined)
	}
	// And it must not invent findings about a store it cannot see.
	for _, code := range []string{
		consts.IntegrityNeverCompacted, consts.IntegrityUnsignedCommits,
		consts.IntegrityImageUnverified, consts.IntegrityNoRootRetained,
	} {
		if has(rep, code) != nil {
			t.Errorf("reported %q with no forensic store to read it from", code)
		}
	}
}

// The in-memory backend has no image to hash. That is INFO, not a warning: nothing is wrong, but
// staying silent would leave the section looking verified.
func TestUnavailableForensicsIsReportedAsInfoNotSilence(t *testing.T) {
	f := &fakeForensics{status: graphene.ForensicStatus{Available: false, Reason: "no image"}}
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: true})

	got := has(rep, consts.IntegrityForensicsAbsent)
	if got == nil {
		t.Fatalf("unavailable forensics not reported; codes = %v", codes(rep))
	}
	if got.Severity != consts.IntegritySevInfo {
		t.Errorf("severity = %q, want %q — an absent backend is not a fault",
			got.Severity, consts.IntegritySevInfo)
	}
	if !strings.Contains(got.Message, "no image") {
		t.Errorf("message %q does not carry the reason", got.Message)
	}
	// 🔒 And it must not have tried to verify a file that does not exist.
	if f.verifies != 0 {
		t.Errorf("VerifyImage called %d times on an unavailable backend", f.verifies)
	}
}

// 🔒 A fingerprint that cannot be read is a fault, and it must not present as "never compacted".
// That one is normal and offers a button; pressing it would write a fresh image over whatever
// made the read fail, and the analyst would never learn there had been a problem.
func TestUnreadableRootsAreAnErrorNotNeverCompacted(t *testing.T) {
	f := &fakeForensics{status: graphene.ForensicStatus{Available: true, RootsError: "roots: short read"}}
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: true})

	got := has(rep, consts.IntegrityRootsUnreadable)
	if got == nil {
		t.Fatalf("unreadable roots not reported; codes = %v", codes(rep))
	}
	if got.Severity != consts.IntegritySevError {
		t.Errorf("severity = %q, want %q", got.Severity, consts.IntegritySevError)
	}
	if !strings.Contains(got.Message, "short read") {
		t.Errorf("message %q does not carry the read error", got.Message)
	}
	if got.Action == consts.IntegrityActionCompact {
		t.Error("offered compaction as the fix for a fingerprint that could not be read")
	}
	if has(rep, consts.IntegrityNeverCompacted) != nil {
		t.Error("reported never_compacted for a case whose roots could not be read")
	}
	if f.verifies != 0 {
		t.Errorf("VerifyImage called %d times with an unreadable fingerprint", f.verifies)
	}
}

// 🔒 An uncompacted case produces ONE finding, not four. With no image, "unsigned", "unverified"
// and "no root retained" are all asking about a file that does not exist, and four findings for
// one situation is how a report becomes something people skim.
func TestNeverCompactedReportsOnceAndStopsThere(t *testing.T) {
	f := &fakeForensics{status: graphene.ForensicStatus{Available: true, Compacted: false}}
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: true})

	got := has(rep, consts.IntegrityNeverCompacted)
	if got == nil {
		t.Fatalf("never-compacted not reported; codes = %v", codes(rep))
	}
	if got.Action != consts.IntegrityActionCompact {
		t.Errorf("action = %q, want %q — the remedy is the whole point",
			got.Action, consts.IntegrityActionCompact)
	}
	for _, code := range []string{
		consts.IntegrityUnsignedCommits, consts.IntegrityImageUnverified,
		consts.IntegrityNoRootRetained, consts.IntegrityImageUncovered,
	} {
		if has(rep, code) != nil {
			t.Errorf("also reported %q about a case with no image at all", code)
		}
	}
	if f.verifies != 0 {
		t.Errorf("VerifyImage called %d times with no image to verify", f.verifies)
	}
}

// 🔒 The standing caveat is reported even on the cleanest reachable posture. Every check rohy can
// run compares the case against itself; if that is ever silent, a verified image reads as an
// unaltered one — precisely the overstatement graphene's SECURITY.md exists to prevent.
func TestRetainedRootCaveatIsReportedEvenWhenTheImageVerifies(t *testing.T) {
	rep := Run(context.Background(), Deps{Forensics: compactedAndClean()}, Options{Deep: true})

	got := has(rep, consts.IntegrityNoRootRetained)
	if got == nil {
		t.Fatalf("the retained-root caveat is missing from a verified case; codes = %v", codes(rep))
	}
	if got.Action != consts.IntegrityActionRetainRoot {
		t.Errorf("action = %q, want %q", got.Action, consts.IntegrityActionRetainRoot)
	}
	// The verified image must NOT be reported as a problem.
	if has(rep, consts.IntegrityImageAltered) != nil {
		t.Error("a matching digest was reported as altered")
	}
	if has(rep, consts.IntegrityImageUnverified) != nil {
		t.Error("a deep run reported the image as unverified")
	}
}

// 🔒 Deep only, and a quick run SAYS it did not look. Silence would read as "the image is fine".
func TestQuickRunSaysTheImageWasNotVerified(t *testing.T) {
	f := compactedAndClean()
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: false})

	got := has(rep, consts.IntegrityImageUnverified)
	if got == nil {
		t.Fatalf("a quick run stayed silent about the image; codes = %v", codes(rep))
	}
	if got.Action != consts.IntegrityActionVerifyImage {
		t.Errorf("action = %q, want %q", got.Action, consts.IntegrityActionVerifyImage)
	}
	// And the expensive check genuinely did not run: ~449 MB of churn on a 100k case is not
	// something a button labelled "check" should spend (PERFORMANCE.md §18).
	if f.verifies != 0 {
		t.Errorf("VerifyImage called %d times on a quick run", f.verifies)
	}
}

// An altered image is an ERROR — the one finding here that is a fault rather than a posture.
func TestAlteredImageIsAnError(t *testing.T) {
	f := &fakeForensics{
		status: graphene.ForensicStatus{Available: true, Compacted: true},
		check:  graphene.ImageCheck{Digest: graphene.DigestMismatch},
	}
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: true})

	got := has(rep, consts.IntegrityImageAltered)
	if got == nil {
		t.Fatalf("an altered image was not reported; codes = %v", codes(rep))
	}
	if got.Severity != consts.IntegritySevError {
		t.Errorf("severity = %q, want %q", got.Severity, consts.IntegritySevError)
	}
}

// 🔒 A coherent rewrite — digest matches, roots do not — is the serious combination, and it must
// not be swallowed by the matching digest. This is the case where someone edited the records and
// repaired the digest behind them, which is the exact gap VerifyCSRRoots closes.
func TestMatchingDigestWithFailingRootsIsStillReportedAsAltered(t *testing.T) {
	f := &fakeForensics{
		status: graphene.ForensicStatus{Available: true, Compacted: true},
		check: graphene.ImageCheck{
			Digest: graphene.DigestMatch, RootsChecked: true, RootsError: "node root mismatch",
		},
	}
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: true})

	got := has(rep, consts.IntegrityImageAltered)
	if got == nil {
		t.Fatalf("a roots failure behind a matching digest was not reported; codes = %v", codes(rep))
	}
	if !strings.Contains(got.Message, "node root mismatch") {
		t.Errorf("message %q drops the underlying reason", got.Message)
	}
}

// 🔒 An uncovered image is neither a pass nor a failure, and it is NOT "never compacted". Telling
// an analyst with a long-running case that it was never compacted would be false.
func TestUncoveredImageIsDistinctFromNeverCompactedAndFromDamage(t *testing.T) {
	f := &fakeForensics{
		status: graphene.ForensicStatus{Available: true, Compacted: true},
		check:  graphene.ImageCheck{Digest: graphene.DigestAbsent},
	}
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: true})

	if has(rep, consts.IntegrityImageUncovered) == nil {
		t.Fatalf("an uncovered image was not reported; codes = %v", codes(rep))
	}
	if has(rep, consts.IntegrityImageAltered) != nil {
		t.Error("an uncovered image was reported as altered — absence is not damage")
	}
	if has(rep, consts.IntegrityNeverCompacted) != nil {
		t.Error("an uncovered image was reported as never compacted — it has been")
	}
}

// A verification that could not run is noted, not silently skipped, and no verdict is guessed.
func TestVerifyFailureIsNotedRatherThanSkipped(t *testing.T) {
	f := &fakeForensics{
		status:   graphene.ForensicStatus{Available: true, Compacted: true},
		checkErr: errors.New("image unreadable"),
	}
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: true})

	if len(rep.Errors) == 0 {
		t.Fatal("a failed verification produced no note")
	}
	if !strings.Contains(strings.Join(rep.Errors, " | "), "image unreadable") {
		t.Errorf("errors %v do not carry the reason", rep.Errors)
	}
	if has(rep, consts.IntegrityImageAltered) != nil {
		t.Error("a failed verification was reported as an altered image")
	}
}

// The unsigned posture is a WARNING with no action, because there is nothing to press: signing
// cannot be applied to writes that already happened.
func TestUnsignedCommitsIsAWarningWithNoAction(t *testing.T) {
	rep := Run(context.Background(), Deps{Forensics: compactedAndClean()}, Options{Deep: true})

	got := has(rep, consts.IntegrityUnsignedCommits)
	if got == nil {
		t.Fatalf("unsigned commits not reported; codes = %v", codes(rep))
	}
	if got.Severity != consts.IntegritySevWarn {
		t.Errorf("severity = %q, want %q", got.Severity, consts.IntegritySevWarn)
	}
	if got.Action != consts.IntegrityActionNone {
		t.Errorf("action = %q, want none — a button here would imply existing writes could be "+
			"attributed retrospectively, and they cannot", got.Action)
	}
}

// A signed case does not carry the unsigned warning. Guards against the finding becoming
// unconditional noise once §14.6 lands.
func TestSignedCaseDoesNotReportUnsignedCommits(t *testing.T) {
	f := compactedAndClean()
	f.status.Signed, f.status.RequireSigned = true, true
	rep := Run(context.Background(), Deps{Forensics: f}, Options{Deep: true})

	if has(rep, consts.IntegrityUnsignedCommits) != nil {
		t.Error("a signed case was reported as unsigned")
	}
}

// --- the exported / not-exported split (plan §14.2) ---

// 🔒 A fingerprint whose copy WAS taken flips the caveat to informational — and the message still
// says what rohy cannot see. This is the one satisfied state in the whole feature, and it stays INFO
// rather than becoming silence or a tick, because whether the copy survives, went somewhere out of
// reach, or was transcribed correctly is all unobservable from in here.
func TestAnExportedFingerprintReportsInfoAndStillStatesTheLimit(t *testing.T) {
	at := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	rep := Run(context.Background(), Deps{
		Forensics: withRoots("abc123"),
		Roots:     &fakeRoots{exported: map[string]time.Time{"abc123": at}},
	}, Options{Deep: true})

	if has(rep, consts.IntegrityNoRootRetained) != nil {
		t.Error("the caveat is still reported after a copy was taken")
	}
	got := has(rep, consts.IntegrityRootExported)
	if got == nil {
		t.Fatalf("the export was not reported at all; codes = %v", codes(rep))
	}
	if got.Severity != consts.IntegritySevInfo {
		t.Errorf("severity = %q, want %q — this is a state, not a pass", got.Severity, consts.IntegritySevInfo)
	}
	if !strings.Contains(got.Message, "2026-08-09") {
		t.Errorf("message %q does not say WHEN the copy was taken", got.Message)
	}
	// 🔒 It must keep saying what rohy cannot confirm. A message that only announced the export
	// would read as a tick, which is the single false claim this feature must not make.
	if !strings.Contains(got.Message, "cannot tell") {
		t.Errorf("message %q drops the limit — it reads as confirmation", got.Message)
	}
	// And no action: there is nothing left to press.
	if got.Action != consts.IntegrityActionNone {
		t.Errorf("action = %q, want none", got.Action)
	}
}

// A DIFFERENT fingerprint having been exported does not satisfy the current one. Sealing again
// produces a new fingerprint, and a copy of the old one says nothing about the new image.
func TestAnExportOfAnEarlierFingerprintDoesNotSatisfyTheCurrentOne(t *testing.T) {
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	rep := Run(context.Background(), Deps{
		Forensics: withRoots("new-root"),
		Roots:     &fakeRoots{exported: map[string]time.Time{"old-root": at}},
	}, Options{Deep: true})

	if has(rep, consts.IntegrityNoRootRetained) == nil {
		t.Errorf("a copy of a superseded fingerprint satisfied the current one; codes = %v", codes(rep))
	}
	if has(rep, consts.IntegrityRootExported) != nil {
		t.Error("reported the current fingerprint as exported on the strength of an older one")
	}
}

// 🔒 No history to consult defaults to the CAVEAT, not to silence. A history lost to a corrupt
// sidecar must not read as "a copy was taken".
func TestAbsentHistoryDefaultsToTheCaveat(t *testing.T) {
	rep := Run(context.Background(), Deps{Forensics: withRoots("abc123")}, Options{Deep: true})

	if has(rep, consts.IntegrityNoRootRetained) == nil {
		t.Errorf("a nil history suppressed the caveat; codes = %v", codes(rep))
	}
	if has(rep, consts.IntegrityRootExported) != nil {
		t.Error("a nil history reported an export")
	}
}

// A posture with no fingerprint at all still produces the caveat rather than looking up "".
func TestNoFingerprintStillProducesTheCaveat(t *testing.T) {
	f := compactedAndClean() // compacted, but Roots left nil
	rep := Run(context.Background(), Deps{
		Forensics: f,
		Roots:     &fakeRoots{exported: map[string]time.Time{"": time.Now()}},
	}, Options{Deep: true})

	if has(rep, consts.IntegrityNoRootRetained) == nil {
		t.Errorf("an empty fingerprint did not produce the caveat; codes = %v", codes(rep))
	}
	if has(rep, consts.IntegrityRootExported) != nil {
		t.Error("an empty fingerprint matched an export record keyed on the empty string")
	}
}
