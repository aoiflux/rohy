package graphene

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gr "github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/signing"
	"github.com/aoiflux/graphene/store"

	"rohy/backend/consts"
)

const testActor = 4242

func signedPosture(t *testing.T) (Posture, *signing.Key) {
	t.Helper()
	key, pub, err := signing.GenerateKey(77)
	if err != nil {
		t.Fatal(err)
	}
	ring := signing.NewKeyring()
	if err := ring.Add(77, pub); err != nil {
		t.Fatal(err)
	}
	return Posture{Mode: consts.CaseModeSigned, Actor: testActor, Slot: NewKeySlot(77), Verifier: ring}, key
}

func oneEvent(t *testing.T, s *Store, tag string) uint64 {
	t.Helper()
	ids, err := s.InsertEvents([]*Event{{
		EventID: "4624", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Computer: "H",
		Channel: "Security", HashNormalized: "hn-" + tag, RawXML: "<x>" + tag + "</x>", HashRaw: "hr-" + tag,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return ids[0]
}

func TestACallBeforeTheSessionWaitsForIt(t *testing.T) {
	s := OpenAwaitingSession(t.TempDir())
	defer s.Close()
	done := make(chan error, 1)
	go func() { _, err := s.CountEvents(EventFilter{}); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("a call returned before any session began: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned, Actor: testActor}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the waiting call failed once the session began: %v", err)
	}
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned}); err == nil {
		t.Fatal("a second session began on an open store")
	}
}

// 🔒 Closing the app at the session step must not hang: waiters stop, and Close opens nothing.
func TestShutdownBeforeASessionReleasesEveryWaiter(t *testing.T) {
	dir := t.TempDir()
	s := OpenAwaitingSession(dir)
	done := make(chan error, 1)
	go func() { _, err := s.CountEvents(EventFilter{}); done <- err }()
	time.Sleep(20 * time.Millisecond)
	if s.Opened() {
		t.Fatal("opened without a session")
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung on a store that never opened")
	}
	if err := <-done; !errors.Is(err, ErrNoSession) {
		t.Fatalf("waiter got %v, want ErrNoSession", err)
	}
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a session began after shutdown: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a store that never opened created %d files", len(entries))
	}
}

// 🔒 A failed open keeps nothing: the same store opens again under another posture.
func TestAFailedSignedOpenCanBeRetriedForRecovery(t *testing.T) {
	dir := t.TempDir()
	// A log holding an unsigned commit, which a signed open refuses.
	unsigned, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	oneEvent(t, unsigned, "a")
	if err := unsigned.Close(); err != nil {
		t.Fatal(err)
	}

	s := OpenAwaitingSession(dir)
	defer s.Close()
	p, _ := signedPosture(t)
	if err := s.BeginSession(p); err == nil {
		t.Fatal("a signed open accepted a log holding an unsigned commit")
	}
	if s.Opened() {
		t.Fatal("a failed open left the store open")
	}
	rec := Posture{Mode: consts.CaseModeRecovery, Actor: testActor, Reason: "the log is not signed"}
	if err := s.BeginSession(rec); err != nil {
		t.Fatalf("recovery after a failed signed open: %v", err)
	}
	if n, err := s.CountEvents(EventFilter{}); err != nil || n != 1 {
		t.Fatalf("recovery reads %d events, %v", n, err)
	}
	if err := s.Writable(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("recovery is writable: %v", err)
	}
}

func TestASignedSessionAttestsItsSealsAndSignsItsCommits(t *testing.T) {
	dir := t.TempDir()
	s := OpenAwaitingSession(dir)
	p, key := signedPosture(t)
	p.Enabling = true
	if err := s.BeginSession(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Writable(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("a locked signed session is writable: %v", err)
	}
	if err := p.Slot.Unlock(key); err != nil {
		t.Fatal(err)
	}
	if err := s.Writable(); err != nil {
		t.Fatalf("unlocked, still read-only: %v", err)
	}
	oneEvent(t, s, "a")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.forensics()
	att, err := fs.SnapshotAttestation()
	if err != nil {
		t.Fatalf("a signed seal carries no attestation: %v", err)
	}
	if att.KeyID != 77 || att.ActorID != testActor {
		t.Fatalf("attested by key %d, actor %d", att.KeyID, att.ActorID)
	}
	if err := disk.VerifyAttestation(p.Verifier, att); err != nil {
		t.Fatalf("the attestation does not verify: %v", err)
	}
	oneEvent(t, s, "b")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// The next session requires every commit to be signed: it was.
	again := OpenAwaitingSession(dir)
	defer again.Close()
	p2, _ := signedPosture(t)
	p2.Verifier = p.Verifier
	if err := again.BeginSession(p2); err != nil {
		t.Fatalf("a strict reopen of a signed session's log: %v", err)
	}
	if st := again.ForensicStatus(); !st.RequireSigned || st.Signed || st.ReadOnly == "" {
		t.Fatalf("locked strict session reports %+v", st)
	}
}

// 🔒 A locked key: the engine itself refuses a signed batch and a seal, so nothing unsigned is
// written by a case that signs.
func TestALockedKeyRefusesBatchesAndSeals(t *testing.T) {
	s := OpenAwaitingSession(t.TempDir())
	defer s.Close()
	p, _ := signedPosture(t)
	p.Enabling = true
	if err := s.BeginSession(p); err != nil {
		t.Fatal(err)
	}
	g, _ := s.graph()
	if _, err := g.AddNodes([]*store.Node{{Labels: []store.NodeType{consts.NodeEvent}}}); !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("a batch with the key locked: %v", err)
	}
	if err := s.Compact(); !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("a seal with the key locked: %v", err)
	}
}

func TestAKeySlotTakesOnlyItsOwnKey(t *testing.T) {
	slot := NewKeySlot(5)
	other, _, _ := signing.GenerateKey(6)
	if err := slot.Unlock(other); err == nil || slot.Unlocked() {
		t.Fatal("a slot took another key")
	}
	if slot.KeyID() != 5 {
		t.Fatal("a locked slot does not answer its key id")
	}
	if _, err := slot.Sign([]byte("x")); !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("a locked slot signed: %v", err)
	}
}

func TestAPostureThatDoesNotHoldTogetherIsRefused(t *testing.T) {
	p, _ := signedPosture(t)
	for name, bad := range map[string]Posture{
		"no mode":           {},
		"signed, no slot":   {Mode: consts.CaseModeSigned, Verifier: p.Verifier},
		"unsigned, a slot":  {Mode: consts.CaseModeUnsigned, Slot: p.Slot},
		"recovery enabling": {Mode: consts.CaseModeRecovery, Enabling: true},
	} {
		s := OpenAwaitingSession(t.TempDir())
		if err := s.BeginSession(bad); err == nil {
			t.Errorf("%s: began", name)
		}
		s.Close()
	}
}

func writeAudit(t *testing.T, dir string) []byte {
	t.Helper()
	g, err := gr.OpenWithOptions(dir, disk.Options{Audit: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddNodes([]*store.Node{{Labels: []store.NodeType{consts.NodeEvent}}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := g.Compact(); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, consts.AuditFile))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAuditFramingFindsATornTailAndKeepsItAside(t *testing.T) {
	dir := t.TempDir()
	clean := writeAudit(t, dir)
	if f, err := AuditFraming(dir); err != nil || f.Torn() || f.Err != nil || f.End != int64(len(clean)) {
		t.Fatalf("a clean log: %+v %v", f, err)
	}
	torn := append(append([]byte{}, clean...), 0x40, 0, 0, 0, 1, 2, 3)
	_ = os.WriteFile(filepath.Join(dir, consts.AuditFile), torn, 0o600)
	f, _ := AuditFraming(dir)
	if !f.Torn() || f.End != int64(len(clean)) {
		t.Fatalf("a torn log: %+v", f)
	}
	aside, n, err := KeepAuditTailAside(dir, time.Now())
	if err != nil || n != 7 {
		t.Fatalf("kept %d bytes aside: %v", n, err)
	}
	if b, _ := os.ReadFile(aside); len(b) != 7 || b[0] != 0x40 {
		t.Fatalf("the kept-aside tail is %x", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, consts.AuditFile)); string(b) != string(clean) {
		t.Fatal("the log was not cut back to its last complete entry")
	}
	// And the engine reads every entry, and appends after them, again.
	g, err := gr.OpenWithOptions(dir, disk.Options{Audit: true})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if err := g.Compact(); err != nil {
		t.Fatal(err)
	}
	fs, _ := g.Forensics()
	entries, err := fs.AuditEntries()
	if err != nil || len(entries) != 3 || disk.VerifyAuditChain(entries) != nil {
		t.Fatalf("%d entries after repair: %v / %v", len(entries), err, disk.VerifyAuditChain(entries))
	}
}

func TestAMisframedAuditLogIsReported(t *testing.T) {
	dir := t.TempDir()
	clean := writeAudit(t, dir)
	// The first entry's detail length, made larger than its record.
	bad := append([]byte{}, clean...)
	bad[4+8+8+8+2] = 0xff
	bad[4+8+8+8+2+1] = 0xff
	_ = os.WriteFile(filepath.Join(dir, consts.AuditFile), bad, 0o600)
	f, err := AuditFraming(dir)
	if err != nil || f.Err == nil || !strings.Contains(f.Err.Error(), "does not fit") {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err := gr.OpenWithOptions(dir, disk.Options{Audit: true}); err == nil {
		t.Fatal("the engine opened what AuditFraming calls misframed: the two disagree")
	}
	if _, _, err := KeepAuditTailAside(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, consts.AuditFile)); string(b) != string(bad) {
		t.Fatal("a misframed log was cut: only a torn tail is")
	}
}

func signedSession(t *testing.T, dir string, p Posture, key *signing.Key) *Store {
	t.Helper()
	s := OpenAwaitingSession(dir)
	if err := s.BeginSession(p); err != nil {
		t.Fatal(err)
	}
	if err := p.Slot.Unlock(key); err != nil {
		t.Fatal(err)
	}
	return s
}

// 🔒 What the "signed since" rule rests on: a signed seal names the signed seal before it, and a
// seal made in between without the key leaves the next one naming none.
func TestASignedSealNamesTheSignedSealBeforeIt(t *testing.T) {
	dir := t.TempDir()
	p, key := signedPosture(t)
	p.Enabling = true
	s := signedSession(t, dir, p, key)
	oneEvent(t, s, "a")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	first, err := s.SealSignature(p.Verifier)
	if err != nil || first == nil || !first.Verified || first.Prev != "" || first.ActorID != testActor {
		t.Fatalf("first seal %+v, %v", first, err)
	}
	oneEvent(t, s, "b")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	second, _ := s.SealSignature(p.Verifier)
	if second.Prev != first.ID {
		t.Fatalf("the second seal names %q, want the first %q", second.Prev, first.ID)
	}
	if same, err := s.ImageMatchesSession(); err != nil || !same {
		t.Fatalf("after a good seal the image and the session disagree: %v", err)
	}
	other := signing.NewKeyring()
	_, pub, _ := signing.GenerateKey(77)
	_ = other.Add(77, pub)
	if sig, _ := s.SealSignature(other); sig.Verified || sig.VerifyError == "" {
		t.Fatalf("a signature verified under the wrong key: %+v", sig)
	}
	s.Close()

	// Someone with the files and no key seals in between.
	g, err := gr.OpenWithOptions(dir, disk.Options{Redaction: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Compact(); err != nil {
		t.Fatal(err)
	}
	g.Close()

	p2 := p
	p2.Slot, p2.Enabling = NewKeySlot(77), false
	s = signedSession(t, dir, p2, key)
	defer s.Close()
	if sig, _ := s.SealSignature(p.Verifier); sig != nil {
		t.Fatalf("an unsigned seal reads as signed: %+v", sig)
	}
	oneEvent(t, s, "c")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	third, _ := s.SealSignature(p.Verifier)
	if third.Prev != "" {
		t.Fatalf("after an unsigned seal the next names %q: the gap would not show", third.Prev)
	}
}
