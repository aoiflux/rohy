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

// Signing, ledger and checkpoint postures (plan §14.8, detectors 14, 16 and 17; item 43).
//
// Like the rest of the posture checks these are mostly consequences of how a case was written, not
// faults. The tests below pin three things: a posture rohy could not read is reported as unknown,
// never as fine; what a signature establishes is stated no wider than it is; and a session that
// cannot write is never offered an action that would.

// posture is the Deps of a compacted, unsigned case whose session writes and audits, with an empty
// ledger and no checkpoint: the plain state of a case made by this build.
func posture() (Deps, *fakeForensics) {
	f := compactedAndClean()
	f.status.Audit = true
	return Deps{
		Forensics:   f,
		Signing:     &SigningPosture{State: consts.SigningUnsigned},
		Ledger:      &graphene.LedgerState{},
		Checkpoints: &Anchoring{},
	}, f
}

func signedSession(d Deps, f *fakeForensics) Deps {
	f.status.Signed, f.status.RequireSigned = true, true
	f.status.Mode = consts.CaseModeSigned
	d.Signing = &SigningPosture{
		State: consts.SigningSigned,
		Seal:  SealVerdict{State: consts.CustodyEstablished, Message: "signed by A's key F"},
	}
	return d
}

func run(d Deps) Report { return Run(context.Background(), d, Options{Deep: true}) }

// --- detector 14: who signs ---

func TestAnUnsignedCaseSaysHowManySealsAreUnsignedAndOffersSigning(t *testing.T) {
	d, _ := posture()
	d.Signing.Seals, d.Signing.UnsignedSeals = 7, 7
	got := has(run(d), consts.IntegrityUnsignedCommits)
	if got == nil {
		t.Fatal("an unsigned case did not say so")
	}
	if got.Severity != consts.IntegritySevWarn || got.Count != 7 {
		t.Errorf("severity %q count %d, want warning and 7", got.Severity, got.Count)
	}
	if !strings.Contains(got.Message, "7 of the 7") {
		t.Errorf("message %q does not say how many seals are unsigned", got.Message)
	}
	// 🔒 The action is advice, not a button: signing takes effect at the next start and cannot reach
	// what is already written.
	if got.Action != consts.IntegrityActionSignCase {
		t.Errorf("action = %q, want %q", got.Action, consts.IntegrityActionSignCase)
	}
}

// 🔒 The copy no longer claims nothing can be attributed: since item 41 every write names the
// analyst. What it must still say is that the name is the case's own record.
func TestTheUnsignedMessageDoesNotClaimWritesAreUnattributed(t *testing.T) {
	d, _ := posture()
	got := has(run(d), consts.IntegrityUnsignedCommits)
	if got == nil {
		t.Fatal("not reported")
	}
	if strings.Contains(got.Message, "cannot be attributed") || strings.Contains(got.Message, "stays unattributed") {
		t.Errorf("message %q says writes are unattributed; they name the analyst", got.Message)
	}
	if !strings.Contains(got.Message, "case's own record") {
		t.Errorf("message %q does not say the name is the case's own record", got.Message)
	}
}

func TestNoSealHistoryLeavesTheCountOut(t *testing.T) {
	d, _ := posture()
	got := has(run(d), consts.IntegrityUnsignedCommits)
	if got == nil || got.Count != 0 || strings.Contains(got.Message, " of the ") {
		t.Errorf("finding %+v counts seals nobody recorded", got)
	}
}

func TestASignedSealIsInfoAndCarriesTheVerdict(t *testing.T) {
	d, f := posture()
	d = signedSession(d, f)
	rep := run(d)
	if has(rep, consts.IntegrityUnsignedCommits) != nil {
		t.Error("a signed case was reported unsigned")
	}
	got := has(rep, consts.IntegritySealSigned)
	if got == nil || got.Severity != consts.IntegritySevInfo || got.Message != "signed by A's key F" {
		t.Errorf("seal finding = %+v, want info with the custody verdict", got)
	}
}

// One signed seal the register did not record is the custody report's not-established verdict. It is
// stated, at info, with that verdict's own words.
func TestAnUnrecordedSignedSealIsStatedAsInfo(t *testing.T) {
	d, f := posture()
	d = signedSession(d, f)
	d.Signing.Seal = SealVerdict{State: consts.CustodyNotEstablished, Message: "not recorded"}
	got := has(run(d), consts.IntegritySealSigned)
	if got == nil || got.Severity != consts.IntegritySevInfo || got.Message != "not recorded" {
		t.Errorf("finding = %+v", got)
	}
}

func TestABrokenSealSignatureIsAnError(t *testing.T) {
	d, f := posture()
	d = signedSession(d, f)
	d.Signing.Seal = SealVerdict{State: consts.CustodyBroken, Message: "sealed without the key"}
	rep := run(d)
	got := has(rep, consts.IntegritySealBroken)
	if got == nil || got.Severity != consts.IntegritySevError || got.Action != consts.IntegrityActionReview {
		t.Fatalf("finding = %+v, want an error to review", got)
	}
	if has(rep, consts.IntegritySealSigned) != nil {
		t.Error("a broken seal was also reported as signed")
	}
}

// A seal whose signature could not be read is not judged either way: the report says it did not look.
func TestAnUnreadableSealIsNotedNotJudged(t *testing.T) {
	d, f := posture()
	d = signedSession(d, f)
	d.Signing.SealErr = "attestation unreadable"
	rep := run(d)
	if has(rep, consts.IntegritySealSigned) != nil || has(rep, consts.IntegritySealBroken) != nil {
		t.Errorf("an unread seal was judged; codes = %v", codes(rep))
	}
	if !strings.Contains(strings.Join(rep.Errors, " | "), "attestation unreadable") {
		t.Errorf("errors = %v", rep.Errors)
	}
}

func TestStoppedSigningSaysWhenWhoAndWhy(t *testing.T) {
	d, _ := posture()
	d.Signing.State = consts.SigningUnsignedAfterSigned
	d.Signing.StoppedAt = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	d.Signing.StoppedBy, d.Signing.StoppedReason = "Ana", "key lost"
	rep := run(d)
	got := has(rep, consts.IntegritySigningStopped)
	if got == nil || got.Severity != consts.IntegritySevWarn || got.Action != consts.IntegrityActionSignCase {
		t.Fatalf("finding = %+v", got)
	}
	for _, want := range []string{"2026-11-05", "Ana", "key lost"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message %q lacks %q", got.Message, want)
		}
	}
	if has(rep, consts.IntegrityUnsignedCommits) != nil {
		t.Error("a stopped case was reported twice: stopped and unsigned")
	}
}

func TestRequestedSigningIsInfo(t *testing.T) {
	d, _ := posture()
	d.Signing.State = consts.SigningRequested
	rep := run(d)
	if got := has(rep, consts.IntegritySigningRequested); got == nil || got.Severity != consts.IntegritySevInfo {
		t.Errorf("finding = %+v", got)
	}
	if has(rep, consts.IntegrityUnsignedCommits) != nil {
		t.Error("a requested case was also reported unsigned")
	}
}

// 🔒 A register that cannot be read is unknown, never "unsigned": a case that signs would otherwise be
// reported as one that never did, and offered signing over the top.
func TestAnUnreadableRegisterIsUnknownNotUnsigned(t *testing.T) {
	d, _ := posture()
	d.Signing = &SigningPosture{RegisterErr: "register.json: bad json"}
	rep := run(d)
	got := has(rep, consts.IntegritySigningUnknown)
	if got == nil || got.Severity != consts.IntegritySevWarn || !strings.Contains(got.Message, "bad json") {
		t.Fatalf("finding = %+v", got)
	}
	if has(rep, consts.IntegrityUnsignedCommits) != nil {
		t.Error("an unknown register was reported as unsigned")
	}
}

func TestAMissingSigningPostureIsNotedNotClean(t *testing.T) {
	d, _ := posture()
	d.Signing = nil
	rep := run(d)
	if !strings.Contains(strings.Join(rep.Errors, " | "), "signing") {
		t.Errorf("no note about the signing posture: %v", rep.Errors)
	}
}

func TestUnboundKeysAreAWarning(t *testing.T) {
	d, _ := posture()
	d.Signing.UnboundKeys = 2
	if got := has(run(d), consts.IntegrityKeyUnbound); got == nil || got.Count != 2 || got.Severity != consts.IntegritySevWarn {
		t.Errorf("finding = %+v", got)
	}
}

// --- the session ---

// 🔒 A read-only session is offered nothing that writes. Each such action would be refused, and a
// button that is refused teaches the analyst to ignore the report.
func TestAReadOnlySessionWithholdsEveryWrite(t *testing.T) {
	d, f := posture()
	f.status.ReadOnly = "the key is locked"
	f.status.Compacted = false
	rep := run(d)

	nc := has(rep, consts.IntegrityNeverCompacted)
	if nc == nil || nc.Action != consts.IntegrityActionNone {
		t.Errorf("never compacted = %+v, want no action in a read-only session", nc)
	}
	ro := has(rep, consts.IntegritySessionReadOnly)
	if ro == nil || ro.Severity != consts.IntegritySevInfo || !strings.Contains(ro.Message, "the key is locked") {
		t.Errorf("read-only finding = %+v", ro)
	}
	for _, fd := range rep.Findings {
		if writes(fd.Action) {
			t.Errorf("%s offers %q in a read-only session", fd.Code, fd.Action)
		}
	}
}

func TestAReadOnlySessionKeepsTheReadsItCanDo(t *testing.T) {
	d, f := posture()
	f.status.ReadOnly = "locked"
	rep := Run(context.Background(), d, Options{}) // quick: the image is not verified
	if got := has(rep, consts.IntegrityImageUnverified); got == nil || got.Action != consts.IntegrityActionVerifyImage {
		t.Errorf("image unverified = %+v, want its verify action kept", got)
	}
	if got := has(rep, consts.IntegrityUnsignedCommits); got == nil || got.Action != consts.IntegrityActionNone {
		t.Errorf("unsigned = %+v, want signing withheld while read-only", got)
	}
}

func TestARecoverySessionIsAWarningAndNotAlsoReadOnly(t *testing.T) {
	d, f := posture()
	f.status.Mode, f.status.ReadOnly = consts.CaseModeRecovery, "recovery"
	rep := run(d)
	if got := has(rep, consts.IntegrityRecoverySession); got == nil || got.Severity != consts.IntegritySevWarn {
		t.Errorf("recovery = %+v", got)
	}
	if has(rep, consts.IntegritySessionReadOnly) != nil {
		t.Error("recovery reported twice: also as read-only")
	}
}

func TestAnAuditLogThatIsOffIsAWarning(t *testing.T) {
	d, f := posture()
	f.status.Audit = false
	if got := has(run(d), consts.IntegrityAuditOff); got == nil || got.Severity != consts.IntegritySevWarn {
		t.Errorf("audit off = %+v", got)
	}
	d, _ = posture()
	if has(run(d), consts.IntegrityAuditOff) != nil {
		t.Error("audit off reported for a session that audits")
	}
}

func TestAuditGapsAreCountedAndCarryTheFirstCause(t *testing.T) {
	d, _ := posture()
	d.Signing.AuditGaps = []string{"disk full", "disk full again"}
	got := has(run(d), consts.IntegrityAuditGap)
	if got == nil || got.Count != 2 || !strings.Contains(got.Message, "disk full") {
		t.Errorf("audit gap = %+v", got)
	}
}

func TestAChangedLogIsAWarningUntilAcceptedThenStatedWithItsReason(t *testing.T) {
	d, _ := posture()
	d.Signing.LogFlagged = true
	if got := has(run(d), consts.IntegrityLogChanged); got == nil || got.Severity != consts.IntegritySevWarn {
		t.Errorf("changed log = %+v", got)
	}
	d.Signing.LogAccepted = "crash during ingest"
	rep := run(d)
	if has(rep, consts.IntegrityLogChanged) != nil {
		t.Error("an accepted log is still reported as changed")
	}
	got := has(rep, consts.IntegrityLogAccepted)
	if got == nil || got.Severity != consts.IntegritySevInfo || !strings.Contains(got.Message, "crash during ingest") {
		t.Errorf("accepted log = %+v", got)
	}
}

// With no image, the image's postures stay quiet — one finding for one situation — but the session's
// own state is not about the image and is still said.
func TestANeverCompactedCaseStillReportsTheSession(t *testing.T) {
	d, f := posture()
	f.status.Compacted = false
	f.status.Mode, f.status.ReadOnly = consts.CaseModeRecovery, "recovery"
	d.Signing.AuditGaps = []string{"x"}
	rep := run(d)
	for _, code := range []string{consts.IntegrityRecoverySession, consts.IntegrityAuditGap, consts.IntegrityNeverCompacted} {
		if has(rep, code) == nil {
			t.Errorf("%s missing; codes = %v", code, codes(rep))
		}
	}
	for _, code := range []string{consts.IntegrityUnsignedCommits, consts.IntegrityCheckpointAbsent, consts.IntegritySealSigned} {
		if has(rep, code) != nil {
			t.Errorf("%s reported about a case with no image", code)
		}
	}
}

// --- detector 16: the redaction ledger ---

func ledger(sigs ...string) *graphene.LedgerState {
	l := &graphene.LedgerState{}
	for i, s := range sigs {
		l.Entries = append(l.Entries, graphene.LedgerEntry{Seq: uint64(i + 1), KeyID: 9, Signature: s})
	}
	return l
}

func TestABrokenLedgerChainIsAnError(t *testing.T) {
	d, _ := posture()
	d.Ledger = ledger(consts.LedgerSigNone)
	d.Ledger.ChainErr = "record 1 was edited"
	got := has(run(d), consts.IntegrityLedgerBroken)
	if got == nil || got.Severity != consts.IntegritySevError || !strings.Contains(got.Message, "record 1 was edited") {
		t.Errorf("finding = %+v", got)
	}
}

// 🔒 graphene's ledger chain does not cover signatures, so a signature stripped afterwards leaves an
// unsigned record that still chains. Inside a signed interval that is an error, not a posture.
func TestUnsignedRecordsAreJudgedByTheIntervalTheyWereWrittenIn(t *testing.T) {
	d, f := posture()
	d = signedSession(d, f)
	d.Ledger = ledger(consts.LedgerSigNone, consts.LedgerSigNone, consts.LedgerSigNone)
	d.Signing.SignedAtLedgerSeq = func(seq uint64) bool { return seq >= 3 }
	rep := run(d)
	if got := has(rep, consts.IntegrityLedgerStripped); got == nil || got.Count != 1 || got.Severity != consts.IntegritySevError {
		t.Errorf("unsigned in signed = %+v", got)
	}
	if got := has(rep, consts.IntegrityLedgerUnverifiable); got == nil || got.Count != 2 || got.Severity != consts.IntegritySevWarn {
		t.Errorf("unverifiable = %+v", got)
	}
}

func TestWithoutKnownIntervalsUnsignedRecordsAreUnverifiable(t *testing.T) {
	d, _ := posture()
	d.Ledger = ledger(consts.LedgerSigNone)
	rep := run(d)
	if has(rep, consts.IntegrityLedgerStripped) != nil {
		t.Error("a record was judged stripped with no signed interval known")
	}
	if got := has(rep, consts.IntegrityLedgerUnverifiable); got == nil || got.Count != 1 {
		t.Errorf("unverifiable = %+v", got)
	}
}

func TestASignatureThatDoesNotCheckOutIsAnError(t *testing.T) {
	d, _ := posture()
	d.Ledger = ledger(consts.LedgerSigBad, consts.LedgerSigUnknownKey, consts.LedgerSigValid)
	got := has(run(d), consts.IntegrityLedgerBadSignature)
	if got == nil || got.Count != 2 || got.Severity != consts.IntegritySevError {
		t.Errorf("finding = %+v", got)
	}
}

// A signature nobody could check — the case's keys were unreadable — is neither failing nor fine. It
// is a note: the report did not look.
func TestAnUncheckedSignatureIsNotedNotFailed(t *testing.T) {
	d, _ := posture()
	d.Ledger = ledger(consts.LedgerSigUnchecked)
	rep := run(d)
	if has(rep, consts.IntegrityLedgerBadSignature) != nil || has(rep, consts.IntegrityLedgerSigned) != nil {
		t.Errorf("an unchecked signature was judged; codes = %v", codes(rep))
	}
	if !strings.Contains(strings.Join(rep.Errors, " | "), "not checked") {
		t.Errorf("errors = %v", rep.Errors)
	}
}

func TestAFullySignedLedgerIsInfoAndPointsAtTheFingerprints(t *testing.T) {
	d, _ := posture()
	d.Ledger = ledger(consts.LedgerSigValid, consts.LedgerSigValid)
	rep := run(d)
	got := has(rep, consts.IntegrityLedgerSigned)
	if got == nil || got.Severity != consts.IntegritySevInfo || got.Count != 2 || !strings.Contains(got.Message, "fingerprint") {
		t.Errorf("finding = %+v", got)
	}
	for _, code := range []string{consts.IntegrityLedgerUnverifiable, consts.IntegrityLedgerBadSignature, consts.IntegrityLedgerStripped} {
		if has(rep, code) != nil {
			t.Errorf("%s on a fully signed ledger", code)
		}
	}
}

func TestAnEmptyLedgerSaysNothing(t *testing.T) {
	d, _ := posture()
	rep := run(d)
	for _, fd := range rep.Findings {
		if strings.HasPrefix(fd.Code, "ledger_") {
			t.Errorf("%s on an empty ledger", fd.Code)
		}
	}
}

func TestAnUnreadableLedgerIsNoted(t *testing.T) {
	d, _ := posture()
	d.Ledger, d.LedgerErr = nil, errors.New("ledger unreadable")
	if !strings.Contains(strings.Join(run(d).Errors, " | "), "ledger unreadable") {
		t.Error("an unreadable ledger was not noted")
	}
	d.LedgerErr = nil
	if !strings.Contains(strings.Join(run(d).Errors, " | "), "ledger") {
		t.Error("a ledger that was never read was not noted")
	}
}

// --- detector 17: checkpoints ---

func TestNoCheckpointIsAWarningThatSaysWhatIsRewritable(t *testing.T) {
	d, _ := posture()
	got := has(run(d), consts.IntegrityCheckpointAbsent)
	if got == nil || got.Severity != consts.IntegritySevWarn {
		t.Fatalf("finding = %+v", got)
	}
	// It names what a kept fingerprint does not cover, so the retained-root finding is not read as
	// covering it.
	for _, want := range []string{"audit log", "fingerprint"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message %q lacks %q", got.Message, want)
		}
	}
}

func TestRecordedCheckpointsAreUnconfirmedWithoutAReceipt(t *testing.T) {
	d, _ := posture()
	k := checkpoint(2)
	d.Checkpoints = &Anchoring{CheckpointState: graphene.CheckpointState{Count: 2, Last: k.CapturedAt}, Last: &k, Current: k.Heads}
	rep := run(d)
	got := has(rep, consts.IntegrityCheckpointUnconfirmed)
	if got == nil || got.Count != 2 || got.Severity != consts.IntegritySevWarn || !strings.Contains(got.Message, "2026-11-01") {
		t.Errorf("finding = %+v", got)
	}
	if has(rep, consts.IntegrityCheckpointAbsent) != nil {
		t.Error("absent reported alongside recorded checkpoints")
	}
}

func TestABrokenCheckpointChainIsAnError(t *testing.T) {
	d, _ := posture()
	d.Checkpoints = &Anchoring{CheckpointState: graphene.CheckpointState{Count: 2, ChainErr: "entry 2 does not link"}}
	got := has(run(d), consts.IntegrityCheckpointBroken)
	if got == nil || got.Severity != consts.IntegritySevError || !strings.Contains(got.Message, "entry 2") {
		t.Errorf("finding = %+v", got)
	}
}

func TestUnreadableCheckpointsAreNoted(t *testing.T) {
	d, _ := posture()
	d.Checkpoints, d.CheckpointErr = nil, errors.New("checkpoints unreadable")
	rep := run(d)
	if !strings.Contains(strings.Join(rep.Errors, " | "), "checkpoints unreadable") {
		t.Errorf("errors = %v", rep.Errors)
	}
	if has(rep, consts.IntegrityCheckpointAbsent) != nil {
		t.Error("unread checkpoints were reported as absent")
	}
}

// --- the whole posture, once ---

// A case that was never anchored still reports the two caveats nothing inside the case can lift — no
// root kept elsewhere, no checkpoint published — and nothing at error.
func TestTheBestReachablePostureStillCarriesItsCaveats(t *testing.T) {
	d, f := posture()
	d = signedSession(d, f)
	d.Ledger = ledger(consts.LedgerSigValid)
	rep := run(d)
	for _, code := range []string{consts.IntegrityNoRootRetained, consts.IntegrityCheckpointAbsent} {
		if has(rep, code) == nil {
			t.Errorf("%s missing; codes = %v", code, codes(rep))
		}
	}
	for _, fd := range rep.Findings {
		if fd.Severity == consts.IntegritySevError {
			t.Errorf("error %s on the best reachable posture", fd.Code)
		}
	}
	if len(rep.Errors) != 0 {
		t.Errorf("notes on a fully read posture: %v", rep.Errors)
	}
}
