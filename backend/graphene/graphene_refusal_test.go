package graphene

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/store"

	"rohy/backend/consts"
)

// What graphene v0.10 refuses, and what rohy tells the analyst about it: a case another program
// holds, a log damaged before its end, and a log that stopped taking writes.

func unsigned() Posture { return Posture{Mode: consts.CaseModeUnsigned, Actor: testActor} }

func recovery() Posture {
	return Posture{Mode: consts.CaseModeRecovery, Actor: testActor, Reason: "test"}
}

// 🔒 A case another open holds is refused as held, and no other posture is offered as a way past
// it: a read-only open needs a lock the holder will not share either.
func TestACaseHeldElsewhereIsRefusedAsHeld(t *testing.T) {
	s, dir := attributedStore(t)
	oneEvent(t, s, "a")

	for _, p := range []Posture{unsigned(), recovery()} {
		other := OpenAwaitingSession(dir)
		err := other.BeginSession(p)
		other.Close()
		if !errors.Is(err, ErrCaseHeld) || !errors.Is(err, disk.ErrStoreLocked) {
			t.Fatalf("%s open of a held case: %v, want ErrCaseHeld wrapping disk.ErrStoreLocked", p.Mode, err)
		}
		if !NoPostureOpens(err) {
			t.Fatalf("%s: a held case offers another posture as a way in", p.Mode)
		}
	}
}

// damagedLogCase is a closed case whose log has a byte flipped in the middle of a record with whole
// records after it: damage, not a crash.
func damagedLogCase(t *testing.T) string {
	t.Helper()
	s, dir := attributedStore(t)
	for _, tag := range []string{"a", "b", "c", "d", "e"} {
		oneEvent(t, s, tag)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := disk.InspectWAL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Records) < 4 {
		t.Fatalf("the log holds %d records, want several", len(info.Records))
	}
	rec := info.Records[1]
	path := filepath.Join(dir, consts.WALFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[rec.Offset+(rec.End()-rec.Offset)/2] ^= 0xff
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// 🔒 A log damaged before its end is refused by every posture and left exactly as it was: the
// records after the damage were acknowledged, and only the analyst may decide to give them up.
func TestALogDamagedBeforeItsEndIsRefusedAndLeftWhole(t *testing.T) {
	dir := damagedLogCase(t)
	path := filepath.Join(dir, consts.WALFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range []Posture{unsigned(), recovery()} {
		s := OpenAwaitingSession(dir)
		err := s.BeginSession(p)
		s.Close()
		if !errors.Is(err, ErrLogRefused) || !errors.Is(err, disk.ErrLogMalformed) {
			t.Fatalf("%s open of a damaged log: %v, want ErrLogRefused wrapping disk.ErrLogMalformed", p.Mode, err)
		}
		if !NoPostureOpens(err) {
			t.Fatalf("%s: a damaged log offers another posture as a way in", p.Mode)
		}
		if !strings.Contains(err.Error(), "wal cut") {
			t.Fatalf("%s: the refusal does not name the repair: %v", p.Mode, err)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a refused open changed the log")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), consts.WALFile+".") {
			t.Fatalf("a refused open left %s beside the log", e.Name())
		}
	}
}

// 🔒 A case image that does not parse is refused by every posture, named as the case's damage
// rather than the engine's fault, and left exactly as it was: a recovery open reads the same image.
func TestAMalformedImageIsRefusedAndLeftWhole(t *testing.T) {
	s, dir := attributedStore(t)
	oneEvent(t, s, "a")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "graphene.csr")
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cut := whole[:len(whole)/2]
	if err := os.WriteFile(path, cut, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, p := range []Posture{unsigned(), recovery()} {
		s := OpenAwaitingSession(dir)
		err := s.BeginSession(p)
		s.Close()
		if !errors.Is(err, ErrImageRefused) || !errors.Is(err, disk.ErrImageMalformed) {
			t.Fatalf("%s open of a malformed image: %v, want ErrImageRefused wrapping disk.ErrImageMalformed", p.Mode, err)
		}
		if !NoPostureOpens(err) {
			t.Fatalf("%s: a malformed image offers another posture as a way in", p.Mode)
		}
		if !strings.Contains(err.Error(), "backup") {
			t.Fatalf("%s: the refusal does not name the remedy: %v", p.Mode, err)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cut, after) {
		t.Fatal("a refused open changed the image")
	}
}

// 🔒 A log that stopped taking writes makes the session read-only with a reason that says how to
// recover, and a reopen is that recovery. An error that is not a stop changes nothing.
func TestAStoppedLogMakesTheSessionReadOnlyUntilReopened(t *testing.T) {
	s, _ := attributedStore(t)
	oneEvent(t, s, "a")

	s.noteStopped(errors.New("a write that failed for another reason"))
	if err := s.Writable(); err != nil {
		t.Fatalf("an error that is not a stop made the session read-only: %v", err)
	}

	s.noteStopped(fmt.Errorf("commit: %w", store.ErrStopped))
	err := s.Writable()
	if !errors.Is(err, ErrReadOnly) || !strings.Contains(err.Error(), "restart rohy") {
		t.Fatalf("after a stop: %v, want read-only naming the restart", err)
	}

	if err := s.Reopen(s.posture()); err != nil {
		t.Fatal(err)
	}
	if err := s.Writable(); err != nil {
		t.Fatalf("a reopen did not recover the stopped session: %v", err)
	}
	oneEvent(t, s, "b")
}

// A shutdown refuses a reopen, so nothing opens a case behind a closing app; Close is idempotent,
// and a read after it is refused as closed.
func TestAClosedStoreStaysClosed(t *testing.T) {
	s, _ := attributedStore(t)
	id := oneEvent(t, s, "a")
	s.Shutdown()
	if err := s.Reopen(s.posture()); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("a reopen after shutdown began: %v, want ErrNotOpen", err)
	}
	for range 2 {
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if _, err := s.GetEvent(id); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("a read after Close: %v, want store.ErrClosed", err)
	}
}
