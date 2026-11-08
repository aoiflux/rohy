package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gr "github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/store"

	"rohy/backend/analyst"
	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/graphene"
	"rohy/backend/keyfile"
)

const keyPass = "a long enough passphrase"

// sessionCase lays a case out the way the app does: a working folder holding rohy-data, a profile
// outside it, and a folder for keys outside both. open() is one start of the app.
type sessionCase struct {
	t                          *testing.T
	work, db, profile, keysDir string
	store                      *graphene.Store
	session                    *Session
	register                   *analyst.Register
	profiles                   *analyst.Profiles
	roots                      *custody.Store
}

func newSessionCase(t *testing.T) *sessionCase {
	t.Helper()
	root := t.TempDir()
	c := &sessionCase{
		t: t, work: filepath.Join(root, "work"), profile: filepath.Join(root, "profile"),
		keysDir: filepath.Join(root, "keys"),
	}
	c.db = filepath.Join(c.work, consts.DataDirName, consts.DBSubdir)
	for _, d := range []string{c.db, c.keysDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		c.close()
		theCase.setGate(nil)
		sealMu.Lock()
		sealObserver = nil
		sealMu.Unlock()
	})
	return c
}

func (c *sessionCase) open() *sessionCase {
	c.t.Helper()
	var err error
	data := filepath.Dir(c.db)
	c.store = graphene.OpenAwaitingSession(c.db)
	if c.profiles, err = analyst.OpenProfile(c.profile); err != nil {
		c.t.Fatal(err)
	}
	if c.register, err = analyst.OpenRegister(filepath.Join(data, consts.AnalystsSubdir)); err != nil {
		c.t.Fatal(err)
	}
	if c.roots, err = custody.Open(filepath.Join(data, consts.RootsSubdir)); err != nil {
		c.t.Fatal(err)
	}
	c.session = NewSession(SessionDeps{
		Store: c.store, Profiles: c.profiles, Register: c.register, Roots: c.roots,
		WorkDir: c.work, DBDir: c.db,
	})
	return c
}

// close ends this start the way the app does: the session closes, then the store.
func (c *sessionCase) close() {
	if c.session == nil {
		return
	}
	if err := c.session.Close(); err != nil {
		c.t.Errorf("session close: %v", err)
	}
	c.store.Close()
	theCase.setGate(nil)
	c.session = nil
}

func (c *sessionCase) begin(req SessionRequest) SessionView {
	c.t.Helper()
	v, err := c.session.Begin(req)
	if err != nil {
		c.t.Fatalf("begin %+v: %v (view %+v)", req, err, v)
	}
	return v
}

func (c *sessionCase) write(tag string) uint64 {
	c.t.Helper()
	ids, err := c.store.InsertEvents([]*graphene.Event{{
		EventID: "4624", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Computer: "H",
		Channel: "Security", HashNormalized: "hn-" + tag, RawXML: "<x>" + tag + "</x>", HashRaw: "hr-" + tag,
	}})
	if err != nil {
		c.t.Fatal(err)
	}
	return ids[0]
}

// signedCase is a case signed by Ada, closed cleanly.
func signedCase(t *testing.T) *sessionCase {
	t.Helper()
	c := newSessionCase(t).open()
	c.begin(SessionRequest{Name: "Ada"})
	c.write("a")
	if _, err := c.session.RequestSigning(filepath.Join(c.keysDir, "ada.key"), true, keyPass, ""); err != nil {
		t.Fatal(err)
	}
	c.close()

	c.open()
	if v := c.session.Status(); v.State != consts.SigningRequested || !slices.Contains(v.Choices, consts.SessionUnlock) {
		t.Fatalf("after requesting: %+v", v)
	}
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass})
	reg, _ := c.register.Get()
	if reg.State != consts.SigningSigned || reg.LastAttestation == "" {
		t.Fatalf("after the enabling start the register reads %+v", reg)
	}
	c.close()
	return c
}

func TestAFirstRunAsksForANameThenStartsUnsigned(t *testing.T) {
	c := newSessionCase(t).open()
	v := c.session.Status()
	if v.Need != consts.SessionNeedIdentity || v.Began {
		t.Fatalf("first run: %+v", v)
	}
	if _, err := c.session.Begin(SessionRequest{}); err == nil {
		t.Fatal("began with no name on a first run")
	}
	v = c.begin(SessionRequest{Name: "Ada Lovelace"})
	if v.Mode != consts.CaseModeUnsigned || v.ReadOnly != "" || v.ActorID == 0 {
		t.Fatalf("began %+v", v)
	}
	reg, ok := c.register.Get()
	if !ok || len(reg.Analysts) != 1 || reg.Analysts[0].Name != "Ada Lovelace" {
		t.Fatalf("register %+v", reg)
	}
	c.close()

	// The second start is silent: nothing to ask.
	c.open()
	if v := c.session.Status(); v.Need != consts.SessionNeedNone {
		t.Fatalf("second start needs %q", v.Need)
	}
}

func TestSigningACaseTakesEffectAtTheNextStartWithASignedSeal(t *testing.T) {
	c := signedCase(t)
	c.open()
	v := c.session.Status()
	if v.State != consts.SigningSigned || v.LogFlagged || !slices.Equal(v.Choices,
		[]string{consts.SessionUnlock, consts.SessionReadOnly, consts.SessionContinueUnsigned}) {
		t.Fatalf("a signed case after a clean close: %+v", v)
	}
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass})
	id := c.write("b")
	if _, err := sealAndRecord(c.store, c.roots, true); err != nil {
		t.Fatal(err)
	}
	maint := NewMaintenanceAPI(c.store).WithIntegrity(IntegrityDeps{Roots: c.roots})
	AttachSession(c.session, nil, maint)
	rep, err := maint.EventCustody(id, "")
	if err != nil {
		t.Fatal(err)
	}
	if !layerIs(rep, consts.CustodyEventLayerSigning, consts.CustodyEstablished) {
		t.Fatalf("custody signing layer: %+v", rep.Layers)
	}
	rec, ok := c.roots.Get(rep.Snapshot)
	if !ok || rec.Attestation == "" || rec.ActorID == 0 {
		t.Fatalf("the seal history does not say who sealed: %+v", rec)
	}
}

func TestALockedSignedCaseIsReadOnlyUntilUnlocked(t *testing.T) {
	c := signedCase(t)
	c.open()
	c.begin(SessionRequest{Choice: consts.SessionReadOnly})
	release, err := theCase.share("test write")
	if err == nil {
		release()
		t.Fatal("a write was allowed with the key locked")
	}
	var ae ErrorEvent
	if !errors.As(err, &ae) || ae.Code != consts.ErrCodeReadOnly {
		t.Fatalf("refusal %v, want %s", err, consts.ErrCodeReadOnly)
	}
	if rel, err := theCase.shareRead("verify"); err != nil {
		t.Fatalf("a read was refused: %v", err)
	} else {
		rel()
	}
	if err := c.session.Unlock("wrong passphrase!!"); err == nil {
		t.Fatal("unlocked with the wrong passphrase")
	}
	if err := c.session.Unlock(keyPass); err != nil {
		t.Fatal(err)
	}
	release, err = theCase.share("test write")
	if err != nil {
		t.Fatalf("still read-only after unlocking: %v", err)
	}
	release()
	// The engine under the session can write too: the unlock reopened it, and recorded itself.
	if !auditHas(t, c, "unlocked") {
		t.Fatal("the unlock was not recorded: the case did not reopen writable")
	}
}

// 🔒 Something written to the log while rohy was closed — with the files and without the key — is
// flagged at the next start, and nothing is signed over it until the analyst accepts it.
func TestALogEditedWhileClosedIsFlaggedAndMustBeAccepted(t *testing.T) {
	c := signedCase(t)
	g, err := gr.OpenWithOptions(c.db, disk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddNode(&store.Node{Labels: []store.NodeType{consts.NodeEvent}, Properties: []byte(`{"forged":true}`)}); err != nil {
		t.Fatal(err)
	}
	g.Close()

	c.open()
	v := c.session.Status()
	if !v.LogFlagged || v.Problem == "" {
		t.Fatalf("an edited log was not flagged: %+v", v)
	}
	if _, err := c.session.Begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass}); err == nil {
		t.Fatal("signed over a flagged log without accepting it")
	}
	if _, err := c.session.Begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass, AcceptLog: true}); err == nil {
		t.Fatal("accepted a log with no reason")
	}
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass, AcceptLog: true, Reason: "rohy crashed during the last ingest"})
	if !auditHas(t, c, "log of 1 record(s) accepted") {
		t.Fatal("accepting the log was not audited")
	}
}

func TestAFlaggedReadOnlySessionCannotUnlockMidway(t *testing.T) {
	c := signedCase(t)
	g, _ := gr.OpenWithOptions(c.db, disk.Options{})
	_, _ = g.AddNode(&store.Node{Labels: []store.NodeType{consts.NodeEvent}})
	g.Close()
	c.open()
	c.begin(SessionRequest{Choice: consts.SessionReadOnly})
	if err := c.session.Unlock(keyPass); err == nil {
		t.Fatal("unlocked a flagged session without accepting its log")
	}
	c.close()
	// And closing a flagged read-only session does not record its log as how the analyst left it.
	c.open()
	if v := c.session.Status(); !v.LogFlagged {
		t.Fatal("a read-only session laundered the flag at close")
	}
}

// 🔒 The register alone never says a case is unsigned.
func TestARegisterThatSaysUnsignedOrIsGoneIsNotASilentStart(t *testing.T) {
	for name, edit := range map[string]func(c *sessionCase){
		"deleted": func(c *sessionCase) {
			_ = os.Remove(filepath.Join(filepath.Dir(c.db), consts.AnalystsSubdir, consts.RegisterFile))
		},
		"set to unsigned": func(c *sessionCase) {
			p := filepath.Join(filepath.Dir(c.db), consts.AnalystsSubdir, consts.RegisterFile)
			b, _ := os.ReadFile(p)
			var d analyst.Document
			_ = json.Unmarshal(b, &d)
			d.State, d.Intervals, d.LastAttestation = consts.SigningUnsigned, nil, ""
			out, _ := json.Marshal(d)
			_ = os.WriteFile(p, out, 0o644)
		},
		"damaged": func(c *sessionCase) {
			p := filepath.Join(filepath.Dir(c.db), consts.AnalystsSubdir, consts.RegisterFile)
			_ = os.WriteFile(p, []byte("{"), 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := signedCase(t)
			edit(c)
			c.open()
			v := c.session.Status()
			if v.Need != consts.SessionNeedChoice || slices.Contains(v.Choices, consts.SessionStart) || v.Problem == "" {
				t.Fatalf("a signed case whose register was %s: %+v", name, v)
			}
			if !slices.Contains(v.Evidence, consts.EvidenceImage) || !slices.Contains(v.Evidence, consts.EvidenceProfile) {
				t.Fatalf("evidence %v", v.Evidence)
			}
			c.begin(SessionRequest{Choice: consts.SessionRecovery})
			if c.store.Writable() == nil {
				t.Fatal("recovery is writable")
			}
		})
	}
}

func TestContinuingUnsignedIsRecordedAndThenQuiet(t *testing.T) {
	c := signedCase(t)
	c.open()
	if _, err := c.session.Begin(SessionRequest{Choice: consts.SessionContinueUnsigned}); err == nil {
		t.Fatal("continued unsigned with no reason")
	}
	v := c.begin(SessionRequest{Choice: consts.SessionContinueUnsigned, Reason: "the key was lost"})
	if v.Mode != consts.CaseModeAfterSigned || v.ReadOnly != "" {
		t.Fatalf("%+v", v)
	}
	if !auditHas(t, c, "signing stopped: the key was lost") {
		t.Fatal("not audited")
	}
	reg, _ := c.register.Get()
	if cur, _ := reg.Current(); reg.State != consts.SigningUnsignedAfterSigned || cur.Reason != "the key was lost" {
		t.Fatalf("register %+v", reg)
	}
	c.write("after")
	c.close()
	c.open()
	if v := c.session.Status(); v.Need != consts.SessionNeedNone || v.State != consts.SigningUnsignedAfterSigned {
		t.Fatalf("after continuing unsigned the next start %+v", v)
	}
}

func TestAKeyInsideTheWorkingFolderIsRefused(t *testing.T) {
	c := newSessionCase(t).open()
	c.begin(SessionRequest{Name: "Ada"})
	_, err := c.session.RequestSigning(filepath.Join(c.work, "ada.key"), true, keyPass, "")
	if err == nil || !strings.Contains(err.Error(), "inside the case") {
		t.Fatalf("%v", err)
	}
	if reg, _ := c.register.Get(); reg.State != consts.SigningUnsigned || len(reg.Keys) != 0 {
		t.Fatalf("a refused key changed the register: %+v", reg)
	}
}

// A second analyst adds their own key to a signed case; their seals verify, and the first
// analyst's key stays listed.
func TestASecondAnalystAddsTheirKey(t *testing.T) {
	c := signedCase(t)
	reg, _ := c.register.Get()
	adaKey := reg.Keys[0].KeyID

	// Grace, on another account, with her own key.
	c.profile = filepath.Join(t.TempDir(), "profile-grace")
	grace, err := analyst.OpenProfile(c.profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := grace.SetIdentity("Grace"); err != nil {
		t.Fatal(err)
	}
	gKey := filepath.Join(c.keysDir, "grace.key")
	if _, err := keyfile.Create(gKey, c.work, keyPass, "Grace", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := grace.SetKeyPath(gKey); err != nil {
		t.Fatal(err)
	}

	c.open()
	v := c.session.Status()
	if !slices.Contains(v.Choices, consts.SessionAddKey) || slices.Contains(v.Choices, consts.SessionUnlock) {
		t.Fatalf("Grace on Ada's case: %+v", v.Choices)
	}
	c.begin(SessionRequest{Choice: consts.SessionAddKey, Passphrase: keyPass})
	id := c.write("grace")
	if _, err := sealAndRecord(c.store, c.roots, true); err != nil {
		t.Fatal(err)
	}
	maint := NewMaintenanceAPI(c.store).WithIntegrity(IntegrityDeps{Roots: c.roots})
	AttachSession(c.session, nil, maint)
	rep, err := maint.EventCustody(id, "")
	if err != nil {
		t.Fatal(err)
	}
	if !layerIs(rep, consts.CustodyEventLayerSigning, consts.CustodyEstablished) {
		t.Fatalf("Grace's seal: %+v", rep.Layers)
	}
	reg, _ = c.register.Get()
	if len(reg.Keys) != 2 || !reg.Bound(reg.Keys[0]) || !reg.Bound(reg.Keys[1]) || reg.Keys[0].KeyID != adaKey {
		t.Fatalf("register keys %+v", reg.Keys)
	}
	if !auditHas(t, c, "added to the case by its holder") {
		t.Fatal("adding the key was not audited")
	}
}

// 🔒 Nothing rohy writes to the audit log holds evidence: it is never redacted.
func TestTheAuditLogHoldsNoEvidence(t *testing.T) {
	c := signedCase(t)
	c.open()
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass})
	c.write("needle-7d1e-secret")
	if _, err := sealAndRecord(c.store, c.roots, true); err != nil {
		t.Fatal(err)
	}
	c.close()
	b, err := os.ReadFile(filepath.Join(c.db, consts.AuditFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("needle-7d1e")) || bytes.Contains(b, []byte(c.keysDir)) {
		t.Fatal("the audit log holds evidence content or a path")
	}
}

func layerIs(r custody.Report, layer, state string) bool {
	for _, l := range r.Layers {
		if l.Layer == layer {
			return l.State == state
		}
	}
	return false
}

func auditHas(t *testing.T, c *sessionCase, detail string) bool {
	t.Helper()
	entries, err := disk.ReadAuditLog(c.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Detail, detail) {
			return true
		}
	}
	return false
}

var _ = bytes.Contains

// 🔒 A failed call's raw message — arguments, passphrase and all — never reaches the log.
func TestTheLogNeverSeesACallsArguments(t *testing.T) {
	msg := `process message error: C{"name":"main.MaintenanceAPI.UnlockKey","args":[{"passphrase":"hunter2 -> hunter2"}]} -> error parsing arguments: json: cannot unmarshal`
	got := scrub(msg)
	if strings.Contains(got, "hunter2") || !strings.Contains(got, "error parsing arguments") {
		t.Fatalf("scrubbed to %q", got)
	}
	if scrub("an ordinary line") != "an ordinary line" {
		t.Fatal("an ordinary line was changed")
	}
}

// A signed case whose log holds a commit the strict open refuses offers recovery, and recovery
// opens it read-only.
func TestAStrictOpenThatFailsOffersRecovery(t *testing.T) {
	c := signedCase(t)
	// An unsigned batch, written without the key.
	g, err := gr.OpenWithOptions(c.db, disk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddNodes([]*store.Node{{Labels: []store.NodeType{consts.NodeEvent}}}); err != nil {
		t.Fatal(err)
	}
	g.Close()
	c.open()
	if _, err := c.session.Begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass, AcceptLog: true, Reason: "crash"}); err == nil {
		t.Fatal("a strict open accepted an unsigned commit")
	}
	v := c.session.Status()
	if v.OpenError == "" || !slices.Contains(v.Choices, consts.SessionRecovery) || v.Began {
		t.Fatalf("after a failed open: %+v", v)
	}
	c.begin(SessionRequest{Choice: consts.SessionRecovery})
	if n, err := c.store.CountEvents(graphene.EventFilter{}); err != nil || n == 0 {
		t.Fatalf("recovery reads %d events: %v", n, err)
	}
	if c.store.Writable() == nil {
		t.Fatal("recovery is writable")
	}
}

// 🔒 A case another program holds is refused with a message that says so, and recovery is not
// offered: a recovery open would meet the same lock.
func TestACaseHeldElsewhereOffersNoRecovery(t *testing.T) {
	c := newSessionCase(t).open()
	c.begin(SessionRequest{Name: "Ada"})
	c.write("a")
	c.close()

	holder, err := graphene.Open(c.db)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	c.open()
	_, err = c.session.Begin(SessionRequest{})
	var ev ErrorEvent
	if !errors.As(err, &ev) || ev.Code != consts.ErrCodeSession || !strings.Contains(ev.Message, "another program") {
		t.Fatalf("a held case: %v, want a session error naming another program", err)
	}
	v := c.session.Status()
	if v.Began || v.OpenError == "" || slices.Contains(v.Choices, consts.SessionRecovery) {
		t.Fatalf("after a held case refused: %+v", v)
	}

	holder.Close()
	c.begin(SessionRequest{})
}

// Closing the app at the session step opens nothing and seals nothing.
func TestClosingAtTheSessionStepOpensNothing(t *testing.T) {
	c := signedCase(t)
	c.open()
	done := make(chan error, 1)
	go func() { done <- c.session.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing at the session step hung")
	}
	if c.store.Opened() {
		t.Fatal("closing at the session step opened the case")
	}
	c.session = nil
}

// A torn audit tail is moved aside by a session that writes, and left alone by one that does not.
func TestATornAuditTailIsKeptAsideOnlyByAWritingSession(t *testing.T) {
	c := signedCase(t)
	path := filepath.Join(c.db, consts.AuditFile)
	clean, _ := os.ReadFile(path)
	torn := append(append([]byte{}, clean...), 9, 9, 9)
	_ = os.WriteFile(path, torn, 0o600)

	c.open()
	c.begin(SessionRequest{Choice: consts.SessionReadOnly})
	if b, _ := os.ReadFile(path); !bytes.Equal(b, torn) {
		t.Fatal("a read-only session changed the audit log")
	}
	c.close()

	c.open()
	c.begin(SessionRequest{Choice: consts.SessionUnlock, Passphrase: keyPass})
	if b, _ := os.ReadFile(path); bytes.HasSuffix(b, []byte{9, 9, 9}) {
		t.Fatal("a writing session left the torn tail in the audit log")
	}
	if !auditHas(t, c, "audit tail of 3 bytes kept aside") {
		t.Fatal("keeping the tail aside was not audited")
	}
}
