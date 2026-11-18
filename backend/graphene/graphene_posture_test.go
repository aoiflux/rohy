package graphene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/signing"

	"rohy/backend/consts"
)

// The ledger and checkpoint readers behind detectors 16 and 17.

func ledgerSigs(t *testing.T, s *Store, ring *signing.Keyring) []string {
	t.Helper()
	var l LedgerState
	var err error
	if ring == nil {
		l, err = s.LedgerState(nil)
	} else {
		l, err = s.LedgerState(ring)
	}
	if err != nil {
		t.Fatal(err)
	}
	if l.ChainErr != "" {
		t.Fatalf("chain: %s", l.ChainErr)
	}
	out := make([]string, len(l.Entries))
	for i, e := range l.Entries {
		out[i] = e.Signature
	}
	return out
}

// Each record's signature is judged on its own, under the keys given: an unsigned record, one signed by
// a listed key, by an unlisted one, by a different key under a listed id, and none checked at all.
func TestEachLedgerSignatureIsJudgedUnderTheCasesKeys(t *testing.T) {
	dir := t.TempDir()
	u := OpenAwaitingSession(dir)
	if err := u.BeginSession(Posture{Mode: consts.CaseModeUnsigned, Actor: testActor}); err != nil {
		t.Fatal(err)
	}
	a := oneEvent(t, u, "a")
	b := oneEvent(t, u, "b")
	if err := u.RedactEventRecord(a, "hn-a", "first"); err != nil {
		t.Fatal(err)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}

	p, key := signedPosture(t)
	p.Enabling = true
	s := signedSession(t, dir, p, key)
	t.Cleanup(func() { s.Close() })
	if err := s.RedactEventRecord(b, "hn-b", "second"); err != nil {
		t.Fatal(err)
	}

	listed := p.Verifier.(*signing.Keyring)
	if got := ledgerSigs(t, s, listed); strings.Join(got, ",") != consts.LedgerSigNone+","+consts.LedgerSigValid {
		t.Errorf("under the case's keys: %v", got)
	}
	if got := ledgerSigs(t, s, signing.NewKeyring()); got[1] != consts.LedgerSigUnknownKey {
		t.Errorf("under no keys: %v", got)
	}
	_, other, _ := signing.GenerateKey(77)
	impostor := signing.NewKeyring()
	if err := impostor.Add(77, other); err != nil {
		t.Fatal(err)
	}
	if got := ledgerSigs(t, s, impostor); got[1] != consts.LedgerSigBad {
		t.Errorf("under another key with the same id: %v", got)
	}
	if got := ledgerSigs(t, s, nil); got[1] != consts.LedgerSigUnchecked || got[0] != consts.LedgerSigNone {
		t.Errorf("with no keys read: %v", got)
	}
}

func TestAnEmptyLedgerHasNoEntries(t *testing.T) {
	s, _ := attributedStore(t)
	oneEvent(t, s, "a")
	l, err := s.LedgerState(signing.NewKeyring())
	if err != nil || len(l.Entries) != 0 || l.ChainErr != "" {
		t.Fatalf("%+v, %v", l, err)
	}
}

// The keyless chain check catches a record edited after it was written.
func TestAnEditedLedgerRecordBreaksTheChain(t *testing.T) {
	dir := t.TempDir()
	s := OpenAwaitingSession(dir)
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned, Actor: testActor}); err != nil {
		t.Fatal(err)
	}
	a := oneEvent(t, s, "a")
	if err := s.RedactEventRecord(a, "hn-a", "reason-AAAA"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, consts.RedactionLedgerFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(b), "reason-AAAA")
	if i < 0 {
		t.Fatal("reason not found in the ledger")
	}
	b[i+7] = 'B'
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	r := OpenLazy(dir)
	t.Cleanup(func() { r.Close() })
	l, err := r.LedgerState(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(l.ChainErr, "edited") {
		t.Fatalf("chain error = %q, want the edit found", l.ChainErr)
	}
}

func TestNoCheckpointFileIsAnEmptyChain(t *testing.T) {
	st, err := InspectCheckpoints(t.TempDir())
	if err != nil || st.Count != 0 || st.ChainErr != "" {
		t.Fatalf("%+v, %v", st, err)
	}
}

// Pins the record size the torn-tail check uses against a checkpoint the engine writes, then checks
// a torn tail and an edit are both reported.
func TestACheckpointChainIsReadAndItsDamageReported(t *testing.T) {
	s, dir := attributedStore(t)
	oneEvent(t, s, "a")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	fs, err := s.forensics()
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := disk.NewInsecureLocalAnchor(filepath.Join(t.TempDir(), "anchor"), dir)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Minute)
	if _, _, err := fs.PublishCheckpoint(anchor); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, consts.CheckpointFile)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != checkpointRecordBytes {
		t.Fatalf("one checkpoint is %d bytes, the reader assumes %d", fi.Size(), checkpointRecordBytes)
	}
	st, err := s.CheckpointState()
	if err != nil || st.Count != 1 || st.ChainErr != "" || st.Last.Before(before) {
		t.Fatalf("%+v, %v", st, err)
	}

	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(append([]byte{}, b...), 1, 2, 3), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := InspectCheckpoints(dir); !strings.Contains(st.ChainErr, "3 byte") || st.Count != 1 {
		t.Errorf("torn tail: %+v", st)
	}

	b[0] ^= 0xFF // the sequence number
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := InspectCheckpoints(dir); st.ChainErr == "" {
		t.Errorf("an edited checkpoint was not found: %+v", st)
	}
}
