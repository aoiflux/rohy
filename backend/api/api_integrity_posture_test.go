package api

import (
	"strings"
	"testing"

	gr "github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/store"

	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
)

// The integrity report's signing, ledger and checkpoint postures, end to end through a real session
// (plan §14.8, item 43).

func (c *sessionCase) check() caseintegrity.Report {
	c.t.Helper()
	maint := NewMaintenanceAPI(c.store).WithIntegrity(IntegrityDeps{Roots: c.roots})
	AttachSession(c.session, nil, maint)
	rep, err := maint.CheckIntegrity(false)
	if err != nil {
		c.t.Fatal(err)
	}
	// Every posture was read: nothing about signing, the ledger or checkpoints is missing.
	for _, e := range rep.Errors {
		for _, w := range []string{"signing", "ledger", "checkpoint", "seal"} {
			if strings.Contains(e, w) {
				c.t.Errorf("posture not read: %s", e)
			}
		}
	}
	return rep
}

func finding(r caseintegrity.Report, code string) *caseintegrity.Finding {
	for i := range r.Findings {
		if r.Findings[i].Code == code {
			return &r.Findings[i]
		}
	}
	return nil
}

func TestAnUnsignedCaseReportsItsUnsignedSealsAndNoCheckpoint(t *testing.T) {
	c := newSessionCase(t).open()
	c.begin(SessionRequest{Name: "Ada"})
	c.write("a")
	if _, err := sealAndRecord(c.store, c.roots, true); err != nil {
		t.Fatal(err)
	}
	rep := c.check()
	got := finding(rep, consts.IntegrityUnsignedCommits)
	if got == nil || got.Count != 1 || got.Action != consts.IntegrityActionSignCase || !strings.Contains(got.Message, "1 of the 1") {
		t.Fatalf("unsigned = %+v", got)
	}
	if finding(rep, consts.IntegrityCheckpointAbsent) == nil {
		t.Error("no checkpoint was not reported")
	}
	if finding(rep, consts.IntegritySessionReadOnly) != nil || finding(rep, consts.IntegrityAuditOff) != nil {
		t.Errorf("a plain writing session reported as limited; findings %+v", rep.Findings)
	}
}

// A signed case's seal and its removal record are both stated as signed by a key the case lists.
func TestASignedCaseReportsItsSealAndItsLedgerAsSigned(t *testing.T) {
	c := signedCase(t)
	c.open()
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass})
	id := c.write("b")
	if err := c.store.RedactEventRecord(id, "hn-b", "lawful erasure"); err != nil {
		t.Fatal(err)
	}
	if _, err := sealAndRecord(c.store, c.roots, true); err != nil {
		t.Fatal(err)
	}
	rep := c.check()
	if got := finding(rep, consts.IntegritySealSigned); got == nil || !strings.Contains(got.Message, "Ada") {
		t.Errorf("seal = %+v", got)
	}
	if got := finding(rep, consts.IntegrityLedgerSigned); got == nil || got.Count != 1 {
		t.Errorf("ledger = %+v; findings %+v", got, rep.Findings)
	}
	for _, code := range []string{consts.IntegrityUnsignedCommits, consts.IntegritySealBroken, consts.IntegrityLedgerStripped} {
		if finding(rep, code) != nil {
			t.Errorf("%s on a signed case", code)
		}
	}
}

// 🔒 A removal recorded with the files and without the key lands inside the signed interval unsigned.
// graphene's chain still holds — signatures sit outside it — so only the interval shows it.
func TestARemovalRecordedWithoutTheKeyIsAnError(t *testing.T) {
	c := signedCase(t)
	c.open()
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass})
	id := c.write("b")
	if _, err := sealAndRecord(c.store, c.roots, true); err != nil {
		t.Fatal(err)
	}
	c.close()

	g, err := gr.OpenWithOptions(c.db, disk.Options{Redaction: true})
	if err != nil {
		t.Fatal(err)
	}
	fs, _ := g.Forensics()
	if _, err := fs.RedactNodeProperties(store.NodeID(id), disk.RedactionRequest{ActorID: 1, Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	g.Close()

	c.open()
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass, AcceptLog: true, Reason: "checking what changed"})
	rep := c.check()
	if got := finding(rep, consts.IntegrityLedgerStripped); got == nil || got.Count != 1 || got.Severity != consts.IntegritySevError {
		t.Fatalf("stripped = %+v; findings %+v", got, rep.Findings)
	}
	if got := finding(rep, consts.IntegrityLogAccepted); got == nil || !strings.Contains(got.Message, "checking what changed") {
		t.Errorf("accepted log = %+v", got)
	}
}

func TestAReadOnlySessionIsOfferedNoWrites(t *testing.T) {
	c := signedCase(t)
	c.open()
	c.begin(SessionRequest{Choice: consts.SessionReadOnly})
	rep := c.check()
	if finding(rep, consts.IntegritySessionReadOnly) == nil {
		t.Fatalf("read-only not reported; findings %+v", rep.Findings)
	}
	if finding(rep, consts.IntegritySealSigned) == nil {
		t.Error("the seal was not checked in a read-only session")
	}
	for _, f := range rep.Findings {
		if writeActionOffered(f.Action) {
			t.Errorf("%s offers %q in a read-only session", f.Code, f.Action)
		}
	}
}

func writeActionOffered(a string) bool {
	switch a {
	case consts.IntegrityActionBackfill, consts.IntegrityActionRepair, consts.IntegrityActionRebuild,
		consts.IntegrityActionIngest, consts.IntegrityActionCompact, consts.IntegrityActionFinishRedaction,
		consts.IntegrityActionSignCase, consts.IntegrityActionPublishCheckpoint, consts.IntegrityActionRecordReceipt:
		return true
	}
	return false
}

func TestAContinuedUnsignedCaseSaysWhenByWhomAndWhy(t *testing.T) {
	c := signedCase(t)
	c.open()
	c.begin(SessionRequest{Choice: consts.SessionContinueUnsigned, Reason: "key lost in transit"})
	rep := c.check()
	got := finding(rep, consts.IntegritySigningStopped)
	if got == nil || !strings.Contains(got.Message, "key lost in transit") || !strings.Contains(got.Message, "Ada") {
		t.Fatalf("stopped = %+v; findings %+v", got, rep.Findings)
	}
	if finding(rep, consts.IntegrityUnsignedCommits) != nil {
		t.Error("a stopped case was also reported as never signed")
	}
}
