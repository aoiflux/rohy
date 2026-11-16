package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rohy/backend/anchor"
	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
	"rohy/backend/custody"
)

// Checkpoints end to end through a real session (plan §14.7, item 42).

func (c *sessionCase) maintenance() *MaintenanceAPI {
	c.t.Helper()
	m := NewMaintenanceAPI(c.store).WithIntegrity(IntegrityDeps{
		Roots: c.roots, Receipts: anchor.Open(filepath.Join(filepath.Dir(c.db), consts.RootsSubdir)),
	})
	AttachSession(c.session, nil, m)
	return m
}

func (c *sessionCase) report(m *MaintenanceAPI) caseintegrity.Report {
	c.t.Helper()
	rep, err := m.CheckIntegrity(false)
	if err != nil {
		c.t.Fatal(err)
	}
	for _, e := range rep.Errors {
		if strings.Contains(e, "checkpoint") || strings.Contains(e, "receipt") {
			c.t.Errorf("posture not read: %s", e)
		}
	}
	return rep
}

func (c *sessionCase) publish(m *MaintenanceAPI) CheckpointPublished {
	c.t.Helper()
	p, err := m.PublishCheckpoint()
	if err != nil {
		c.t.Fatal(err)
	}
	return p
}

func unsignedCase(t *testing.T) *sessionCase {
	t.Helper()
	c := newSessionCase(t).open()
	c.begin(SessionRequest{Name: "Ada"})
	return c
}

func codeOf(err error) string {
	if ae, ok := errors.AsType[ErrorEvent](err); ok {
		return ae.Code
	}
	return ""
}

// One click: the unsealed write is sealed first, the checkpoint names that seal, the statement saves
// as JSON that carries its limits, and the analyst's profile remembers it.
func TestPublishingAnUnsealedCaseSealsItFirst(t *testing.T) {
	c := unsignedCase(t)
	c.write("a")
	m := c.maintenance()
	p := c.publish(m)
	if p.Sealed == nil || p.Sealed.State != consts.SealStateSealed || len(p.Gaps) != 0 {
		t.Fatalf("%+v", p)
	}
	roots, _ := c.store.SnapshotRoots()
	st := p.Statement
	if st.Checkpoint.Snapshot != roots.Snapshot || st.Digest != st.Checkpoint.Digest || st.Checkpoint.Seq != 1 ||
		st.UnsealedRecords == nil || *st.UnsealedRecords != 0 || st.CaseID == "" {
		t.Fatalf("%+v", st)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(p.Document), &doc); err != nil || doc["format"] != consts.CheckpointStatementFormat ||
		p.FileName != "rohy-checkpoint-1.json" {
		t.Fatalf("%v %v %s", doc, err, p.FileName)
	}
	if len(st.DoesNotEstablish) == 0 || !strings.Contains(p.Document, "proves nothing on its own") {
		t.Fatal("the statement does not carry its limits")
	}
	prof, _ := c.profiles.Get()
	if len(prof.Checkpoints) != 1 || prof.Checkpoints[0].Digest != st.Digest || prof.Checkpoints[0].CaseID != st.CaseID {
		t.Fatalf("profile %+v", prof.Checkpoints)
	}

	rep := c.report(m)
	got := finding(rep, consts.IntegrityCheckpointUnconfirmed)
	if got == nil || got.Action != consts.IntegrityActionRecordReceipt || finding(rep, consts.IntegrityCheckpointAbsent) != nil {
		t.Fatalf("findings %+v", rep.Findings)
	}

	// A sealed case publishes without sealing again.
	if p := c.publish(m); p.Sealed != nil || p.Statement.Checkpoint.Seq != 2 {
		t.Fatalf("second: %+v", p)
	}
}

// The receipt makes the checkpoint current (info, naming the receipt); a later seal makes it stale.
func TestAReceiptRoundTrip(t *testing.T) {
	c := unsignedCase(t)
	c.write("a")
	m := c.maintenance()
	c.publish(m)
	// The receipt's time is read off the clock, not written down: a receipt must fall between its
	// checkpoint's capture and now (each give or take consts.ReceiptTimeSlack), and the checkpoint is
	// captured when the test runs, so a fixed date passes for one day and then fails for good.
	publishedAt := time.Now().UTC().Format(time.RFC3339)
	r, err := m.RecordCheckpointReceipt(1, "TSA serial 0x2A", publishedAt)
	if err != nil || r.Receipt.Ref != "TSA serial 0x2A" || r.AuditGap != "" {
		t.Fatalf("%+v %v", r, err)
	}
	list, err := m.Checkpoints()
	if err != nil || len(list.Checkpoints) != 1 || len(list.Checkpoints[0].Receipts) != 1 || list.ChainError != "" {
		t.Fatalf("list %+v %v", list, err)
	}
	rep := c.report(m)
	if got := finding(rep, consts.IntegrityCheckpointCurrent); got == nil || got.Severity != consts.IntegritySevInfo ||
		!strings.Contains(got.Message, "TSA serial 0x2A") {
		t.Fatalf("findings %+v", rep.Findings)
	}
	if finding(rep, consts.IntegrityCheckpointUnconfirmed) != nil {
		t.Error("still unconfirmed")
	}

	for name, call := range map[string]func() error{
		"bad time":    func() error { _, err := m.RecordCheckpointReceipt(1, "x", "yesterday"); return err },
		"no such one": func() error { _, err := m.RecordCheckpointReceipt(5, "x", ""); return err },
		"bad ref":     func() error { _, err := m.RecordCheckpointReceipt(1, "a\nb", ""); return err },
	} {
		if err := call(); codeOf(err) != consts.ErrCodeParse {
			t.Errorf("%s: %v", name, err)
		}
	}

	c.write("b")
	if _, err := sealAndRecord(c.store, c.roots, false); err != nil {
		t.Fatal(err)
	}
	if got := finding(c.report(m), consts.IntegrityCheckpointStale); got == nil || got.Action != consts.IntegrityActionPublishCheckpoint {
		t.Fatalf("after a seal: %+v", got)
	}
}

// 🔒 The digest the reader holds: it matches where the case stands, descends after a seal, and is
// told apart from a seal fingerprint and from a digest the case never held.
func TestCheckingADigestYouHold(t *testing.T) {
	c := unsignedCase(t)
	c.write("a")
	m := c.maintenance()
	d := c.publish(m).Statement.Digest

	check := func(v string) custody.AnchorCheck {
		t.Helper()
		r, err := m.CheckCheckpoint(v)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := check("  " + strings.ToUpper(d) + "  "); r.State != consts.AnchorCheckMatches || r.Seq != 1 {
		t.Fatalf("%+v", r)
	}
	roots, _ := c.store.SnapshotRoots()
	if r := check(roots.Snapshot); r.State != consts.AnchorCheckSealFingerprint {
		t.Fatalf("a seal fingerprint: %s", r.State)
	}
	if r := check(strings.Repeat("ef", 32)); r.State != consts.AnchorCheckNotFound || r.Broken == 0 {
		t.Fatalf("unknown: %+v", r)
	}
	if _, err := m.CheckCheckpoint("nope"); codeOf(err) != consts.ErrCodeParse {
		t.Fatalf("malformed: %v", err)
	}

	c.write("b")
	if _, err := sealAndRecord(c.store, c.roots, false); err != nil {
		t.Fatal(err)
	}
	if r := check(d); r.State != consts.AnchorCheckDescends {
		t.Fatalf("after a seal: %+v", r)
	}
}

// 🔒 Removing the checkpoint record does not make the case read as never anchored: the audit log's
// announcement and the analyst's profile both remember it.
func TestADeletedCheckpointRecordIsCaught(t *testing.T) {
	c := unsignedCase(t)
	c.write("a")
	m := c.maintenance()
	c.publish(m)
	if err := os.Remove(filepath.Join(c.db, consts.CheckpointFile)); err != nil {
		t.Fatal(err)
	}
	rep := c.report(m)
	for _, code := range []string{consts.IntegrityCheckpointUnrecorded, consts.IntegrityCheckpointMissing} {
		if got := finding(rep, code); got == nil || got.Severity != consts.IntegritySevError {
			t.Errorf("%s: %+v", code, got)
		}
	}
	if finding(rep, consts.IntegrityCheckpointAbsent) != nil {
		t.Error("read as never anchored")
	}
}

// With the register and the audit log gone too, the profile still recognises the case by the seal
// the checkpoint named.
func TestTheProfileRecognisesTheCaseByItsSeal(t *testing.T) {
	c := unsignedCase(t)
	c.write("a")
	c.publish(c.maintenance())
	c.close()
	data := filepath.Dir(c.db)
	for _, p := range []string{
		filepath.Join(c.db, consts.CheckpointFile),
		filepath.Join(c.db, consts.AuditFile),
		filepath.Join(data, consts.AnalystsSubdir, consts.RegisterFile),
	} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	c.open()
	c.begin(SessionRequest{})
	rep := c.report(c.maintenance())
	if got := finding(rep, consts.IntegrityCheckpointMissing); got == nil {
		t.Fatalf("findings %+v", rep.Findings)
	}
}

func TestAReadOnlySessionPublishesNothingButChecksADigest(t *testing.T) {
	c := signedCase(t)
	c.open()
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass})
	m := c.maintenance()
	d := c.publish(m).Statement.Digest
	c.close()

	c.open()
	c.begin(SessionRequest{Choice: consts.SessionReadOnly})
	m = c.maintenance()
	if _, err := m.PublishCheckpoint(); codeOf(err) != consts.ErrCodeReadOnly {
		t.Fatalf("publish: %v", err)
	}
	if _, err := m.RecordCheckpointReceipt(1, "x", ""); codeOf(err) != consts.ErrCodeReadOnly {
		t.Fatalf("receipt: %v", err)
	}
	if r, err := m.CheckCheckpoint(d); err != nil || r.State == consts.AnchorCheckBroken || r.State == consts.AnchorCheckNotFound {
		t.Fatalf("check: %+v %v", r, err)
	}
	if exp, err := m.CheckpointStatement(1); err != nil || exp.Statement.Digest != d || exp.Statement.UnsealedRecords != nil {
		t.Fatalf("statement: %+v %v", exp, err)
	}
	for _, f := range c.report(m).Findings {
		if writeActionOffered(f.Action) {
			t.Errorf("%s offers %s in a read-only session", f.Code, f.Action)
		}
	}
}

// The drawer takes a checkpoint digest as the kept value, and reads it as broken once a history the
// checkpoint covered has been edited.
func TestTheDrawerChecksAnEventAgainstACheckpointDigest(t *testing.T) {
	c := unsignedCase(t)
	id := c.write("a")
	m := c.maintenance()
	d := c.publish(m).Statement.Digest
	kept := func(m *MaintenanceAPI) custody.Layer {
		t.Helper()
		r, err := m.EventCustody(id, d)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range r.Layers {
			if l.Layer == consts.CustodyEventLayerKept {
				return l
			}
		}
		t.Fatal("no kept layer")
		return custody.Layer{}
	}
	if l := kept(m); l.State != consts.CustodyEstablished || len(l.Notes) == 0 || !strings.Contains(l.Notes[0], "checkpoint 1") {
		t.Fatalf("%+v", l)
	}
	c.close()

	path := filepath.Join(c.db, consts.AuditFile)
	b, _ := os.ReadFile(path)
	i := bytes.Index(b, fmt.Appendf(nil, consts.MsgCheckpointAnnounced, 1))
	if i < 0 {
		t.Fatal("announcement not found")
	}
	b[i] = 'P'
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c.open()
	c.begin(SessionRequest{})
	if l := kept(c.maintenance()); l.State != consts.CustodyBroken {
		t.Fatalf("after the edit: %+v", l)
	}
}

// A torn tail is kept aside before the next checkpoint, and that one is found.
func TestPublishingOverATornTailKeepsItAside(t *testing.T) {
	c := unsignedCase(t)
	c.write("a")
	m := c.maintenance()
	c.publish(m)
	f, err := os.OpenFile(filepath.Join(c.db, consts.CheckpointFile), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{1, 2, 3})
	f.Close()
	p := c.publish(m)
	if p.KeptAside == "" {
		t.Fatal("the tail was not kept aside")
	}
	if r, _ := m.CheckCheckpoint(p.Statement.Digest); r.State != consts.AnchorCheckMatches || r.Seq != 2 {
		t.Fatalf("%+v", r)
	}
}
