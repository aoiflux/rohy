package caseintegrity

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// Detector 17 over published checkpoints, receipts and the tripwires (item 42).

func checkpoint(seq uint64) graphene.Checkpoint {
	return graphene.Checkpoint{
		Seq: seq, CapturedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Heads: graphene.Heads{Snapshot: "s1", AuditHead: "h", AuditCount: 6, RedactionHead: "r", RedactionCount: 1},
	}
}

func heldOK() []graphene.LayerHeld {
	return []graphene.LayerHeld{
		{Layer: consts.AnchorLayerAudit, State: consts.HeldOK, At: 6, Now: 6},
		{Layer: consts.AnchorLayerLedger, State: consts.HeldOK, At: 1, Now: 1},
	}
}

// anchored is a case whose newest checkpoint has a receipt, and which has not moved since.
func anchored() Deps {
	d, _ := posture()
	k := checkpoint(3)
	d.Checkpoints = &Anchoring{
		CheckpointState: graphene.CheckpointState{Count: 3, Last: k.CapturedAt},
		Last:            &k, LastHeld: heldOK(), Receipted: &k, Refs: []string{"TSA serial 7"}, Current: k.Heads,
	}
	return d
}

func TestNoCheckpointOffersToPublishOneButNotInAReadOnlySession(t *testing.T) {
	d, f := posture()
	if got := has(run(d), consts.IntegrityCheckpointAbsent); got == nil || got.Action != consts.IntegrityActionPublishCheckpoint {
		t.Fatalf("%+v", got)
	}
	f.status.Mode = consts.CaseModeSigned
	d.Signing = &SigningPosture{State: consts.SigningSigned}
	f.status.ReadOnly = "the key is locked"
	if got := has(run(d), consts.IntegrityCheckpointAbsent); got == nil || got.Action != consts.IntegrityActionNone {
		t.Fatalf("read-only: %+v", got)
	}
}

func TestACheckpointWithoutAReceiptAsksForOneAndNamesTheLastThatHasOne(t *testing.T) {
	d := anchored()
	k := checkpoint(4)
	d.Checkpoints.Last, d.Checkpoints.Count = &k, 4
	rep := run(d)
	got := has(rep, consts.IntegrityCheckpointUnconfirmed)
	if got == nil || got.Action != consts.IntegrityActionRecordReceipt || !strings.Contains(got.Message, "checkpoint 4") ||
		!strings.Contains(got.Message, "last checkpoint with a receipt is 3") {
		t.Fatalf("%+v", got)
	}
	if has(rep, consts.IntegrityCheckpointCurrent) == nil {
		t.Error("the window against checkpoint 3 was not reported")
	}
}

// 🔒 The best a checkpoint can do here is info, and it says the receipt is the case's own record and
// where the real check is.
func TestAReceiptedCheckpointIsInfoAndSaysItCannotCheckTheAnchor(t *testing.T) {
	d := anchored()
	d.Checkpoints.Current.AuditCount = 9
	d.Checkpoints.Unsealed = 2
	got := has(run(d), consts.IntegrityCheckpointCurrent)
	if got == nil || got.Severity != consts.IntegritySevInfo || got.Action != consts.IntegrityActionNone {
		t.Fatalf("%+v", got)
	}
	for _, want := range []string{"TSA serial 7", "cannot check the anchor", "own record", "Check a digest you hold", "3 audit entries", "2 log records"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message lacks %q: %s", want, got.Message)
		}
	}
}

func TestACaseSealedOrRedactedSinceItsCheckpointIsStale(t *testing.T) {
	for name, move := range map[string]func(*graphene.Heads){
		"sealed":   func(h *graphene.Heads) { h.Snapshot = "s2" },
		"resigned": func(h *graphene.Heads) { h.Attestation = "a2" },
		"redacted": func(h *graphene.Heads) { h.RedactionCount = 2 },
		"granted":  func(h *graphene.Heads) { h.GrantCount = 1 },
	} {
		d := anchored()
		move(&d.Checkpoints.Current)
		rep := run(d)
		got := has(rep, consts.IntegrityCheckpointStale)
		if got == nil || got.Severity != consts.IntegritySevWarn || got.Action != consts.IntegrityActionPublishCheckpoint ||
			!strings.Contains(got.Message, "TSA serial 7") {
			t.Errorf("%s: %+v", name, got)
		}
		if has(rep, consts.IntegrityCheckpointCurrent) != nil {
			t.Errorf("%s: also reported as current", name)
		}
	}
}

// A history that was not read is noted, and not compared: an unread ledger is not "moved".
func TestAnUnreadHistoryIsNotedAndNotCompared(t *testing.T) {
	d := anchored()
	d.Checkpoints.Current.RedactionCount = 0
	d.Checkpoints.Unread = map[string]string{consts.AnchorLayerLedger: "misframed"}
	rep := run(d)
	if has(rep, consts.IntegrityCheckpointStale) != nil {
		t.Error("an unread ledger read as moved")
	}
	// 🔒 Nor as unchanged: "still describes the removal records" would be a claim nothing checked.
	if has(rep, consts.IntegrityCheckpointCurrent) != nil {
		t.Error("an unread ledger read as current")
	}
	d = anchored()
	d.Checkpoints.Unread = map[string]string{consts.AnchorLayerImage: "roots unreadable"}
	if has(run(d), consts.IntegrityCheckpointCurrent) != nil {
		t.Error("an unread image read as current")
	}
	if !strings.Contains(strings.Join(rep.Errors, " | "), "misframed") {
		t.Errorf("errors = %v", rep.Errors)
	}
}

// 🔒 The tripwires: a removed chain the audit log or the profile remembers, a receipt the chain does
// not hold, a history shrunk or edited under the newest checkpoint. Each is an error, and none lets the
// case read as never anchored.
func TestTheTripwiresAreErrors(t *testing.T) {
	cases := map[string]struct {
		set  func(*Anchoring)
		code string
		want string
	}{
		"announced, not held": {func(a *Anchoring) {
			*a = Anchoring{Unrecorded: []uint64{1, 2}}
		}, consts.IntegrityCheckpointUnrecorded, "1, 2"},
		"published, not held": {func(a *Anchoring) {
			*a = Anchoring{Missing: &Published{Seq: 5, At: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}}
		}, consts.IntegrityCheckpointMissing, "your profile"},
		"receipt, not held": {func(a *Anchoring) { a.Orphans = []uint64{9} }, consts.IntegrityReceiptOrphaned, "9"},
		"history edited": {func(a *Anchoring) {
			a.LastHeld[0].State = consts.HeldChanged
		}, consts.IntegrityCheckpointNotHeld, "audit changed"},
		"history shrank": {func(a *Anchoring) {
			a.LastHeld[1].State = consts.HeldShrank
		}, consts.IntegrityCheckpointNotHeld, "removals shrank"},
	}
	for name, c := range cases {
		d := anchored()
		c.set(d.Checkpoints)
		rep := run(d)
		got := has(rep, c.code)
		if got == nil || got.Severity != consts.IntegritySevError || !strings.Contains(got.Message, c.want) {
			t.Errorf("%s: %+v", name, got)
		}
		if has(rep, consts.IntegrityCheckpointAbsent) != nil {
			t.Errorf("%s: also reported as never anchored", name)
		}
	}
}

// A chain that does not hold is reported as such, and nothing is judged from it.
func TestABrokenChainStopsTheRestOfTheCheck(t *testing.T) {
	d := anchored()
	d.Checkpoints.ChainErr = "entry 2 does not link"
	d.Checkpoints.Current.Snapshot = "s9"
	rep := run(d)
	if has(rep, consts.IntegrityCheckpointBroken) == nil {
		t.Fatal("broken not reported")
	}
	for _, code := range []string{consts.IntegrityCheckpointStale, consts.IntegrityCheckpointCurrent, consts.IntegrityCheckpointUnconfirmed} {
		if has(rep, code) != nil {
			t.Errorf("%s judged from a broken chain", code)
		}
	}
}

func TestUnreadReceiptsAreNoted(t *testing.T) {
	d := anchored()
	d.Checkpoints.ReceiptsErr = errorString("receipts held")
	if rep := run(d); !strings.Contains(strings.Join(rep.Errors, " | "), "receipts held") {
		t.Errorf("errors = %v", rep.Errors)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

// The best reachable posture now: signed, the newest checkpoint receipted and current. It carries no
// error, and its checkpoint line is info, never a tick.
func TestAnAnchoredCaseIsInfoNeverClean(t *testing.T) {
	d := anchored()
	d.Ledger = ledger(consts.LedgerSigValid)
	rep := run(d)
	for _, fd := range rep.Findings {
		if fd.Severity == consts.IntegritySevError {
			t.Errorf("error %s", fd.Code)
		}
	}
	if got := has(rep, consts.IntegrityCheckpointCurrent); got == nil || got.Severity != consts.IntegritySevInfo {
		t.Fatalf("%+v", got)
	}
	if has(rep, consts.IntegrityNoRootRetained) == nil {
		t.Error("the retained-root caveat was lifted by a checkpoint")
	}
}
