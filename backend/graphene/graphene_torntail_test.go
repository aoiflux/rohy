package graphene

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoiflux/graphene/disk"

	"rohy/backend/consts"
)

// A torn tail is the shape a crash leaves at the end of an append-only file: bytes that are not a
// whole record. Every writable open cuts them; rohy keeps them beside the file first.

var junk = []byte{0x07, 0x5a, 0x07}

// tornCase is a closed case whose audit log, checkpoint chain, redaction ledger and log each end in
// junk, as after a crash mid-append.
func tornCase(t *testing.T) string {
	t.Helper()
	s, dir := attributedStore(t)
	a := oneEvent(t, s, "a")
	oneEvent(t, s, "b")
	if err := s.RedactEventRecord(a, "hn-a", why); err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	publishOne(t, s)
	oneEvent(t, s, "c") // something in the log after the seal
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range tornFiles() {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(junk); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func tornFiles() []string {
	return []string{consts.AuditFile, consts.CheckpointFile, consts.RedactionLedgerFile, consts.WALFile}
}

func snapshotFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, name := range tornFiles() {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		out[name] = b
	}
	return out
}

// 🔒 A writable open keeps every torn tail it cuts: each file is cut back to its whole records, and
// exactly the cut bytes sit beside it.
func TestAWritableOpenKeepsEveryTornTailAside(t *testing.T) {
	dir := tornCase(t)
	before := snapshotFiles(t, dir)

	s := OpenAwaitingSession(dir)
	t.Cleanup(func() { s.Close() })
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned, Actor: testActor}); err != nil {
		t.Fatal(err)
	}

	kept := map[string]KeptTail{}
	for _, k := range s.KeptAside() {
		kept[k.File] = k
	}
	for _, name := range tornFiles() {
		k, ok := kept[name]
		if !ok {
			t.Errorf("%s: no tail kept; kept %+v", name, s.KeptAside())
			continue
		}
		aside, err := os.ReadFile(k.Aside)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(aside, junk) || k.Bytes != int64(len(junk)) {
			t.Errorf("%s: kept %x (%d bytes), want %x", name, aside, k.Bytes, junk)
		}
		now, _ := os.ReadFile(filepath.Join(dir, name))
		if !bytes.HasPrefix(before[name], now[:min(len(now), len(before[name])-len(junk))]) {
			t.Errorf("%s: the whole records before the tail changed", name)
		}
	}
	// The case reads whole after the cut: the event written after the seal replayed.
	if n, err := s.CountEvents(EventFilter{Undated: consts.UndatedInclude}); err != nil || n != 3 {
		t.Fatalf("after the cut: %d events, %v", n, err)
	}
}

// 🔒 The engine's own record of each cut names the same bytes rohy kept: the audit entry's digest is
// the digest of the copy.
func TestTheKeptTailIsTheOneTheEngineRecorded(t *testing.T) {
	dir := tornCase(t)
	s := OpenAwaitingSession(dir)
	t.Cleanup(func() { s.Close() })
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned, Actor: testActor}); err != nil {
		t.Fatal(err)
	}
	entries, err := disk.ReadAuditLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	digests := map[string]bool{}
	for _, k := range s.KeptAside() {
		b, err := os.ReadFile(k.Aside)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		digests[hex.EncodeToString(sum[:])] = true
	}
	cuts := 0
	for _, e := range entries {
		if e.Kind != disk.AuditTornTail {
			continue
		}
		cuts++
		i := strings.LastIndex(e.Detail, "sha256 ")
		if i < 0 || !digests[strings.TrimSuffix(e.Detail[i+len("sha256 "):], ")")] {
			t.Errorf("the engine cut bytes rohy did not keep: %s", e.Detail)
		}
	}
	// The audit log and the checkpoint chain are framed by rohy and kept aside before the open, so
	// the engine finds nothing to cut in them. The other two it cuts and records.
	if cuts != 2 {
		t.Fatalf("the engine recorded %d cuts, want 2 (the redaction ledger and the log)", cuts)
	}
}

// 🔒 A read-only open changes nothing: no tail is cut or kept, and every file reads as it was.
func TestAReadOnlyOpenLeavesTornTailsInPlace(t *testing.T) {
	dir := tornCase(t)
	before := snapshotFiles(t, dir)

	s := OpenAwaitingSession(dir)
	t.Cleanup(func() { s.Close() })
	if err := s.BeginSession(Posture{Mode: consts.CaseModeRecovery, Actor: testActor, Reason: "inspection"}); err != nil {
		t.Fatal(err)
	}
	if kept := s.KeptAside(); len(kept) != 0 {
		t.Fatalf("a read-only open kept %+v", kept)
	}
	if n, err := s.CountEvents(EventFilter{Undated: consts.UndatedInclude}); err != nil || n != 3 {
		t.Fatalf("a read-only open reads %d events, %v", n, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if after := snapshotFiles(t, dir); !equalFiles(before, after) {
		t.Fatal("a read-only session changed a file")
	}
	if kept, _ := filepath.Glob(filepath.Join(dir, "*.torn-*")); len(kept) != 0 {
		t.Fatalf("a read-only session kept tails: %v", kept)
	}
}

func equalFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return true
}
