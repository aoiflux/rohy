package custody

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rohy/backend/consts"
)

// Case-fingerprint history (plan §14.2).
//
// The invariant most of these defend is a naming one that is really a truthfulness one: rohy records
// that a copy was EXPORTED, never that one is RETAINED. It cannot observe whether the copy survives,
// whether it went beyond the reach of anyone able to rewrite the case, or whether it was transcribed
// correctly — and the value of an external copy comes precisely from being outside what this process
// can see.

func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func rec(snapshot, prev string) Record {
	return Record{Snapshot: snapshot, Prev: prev, Events: 100, AppVersion: "0.2.0"}
}

func TestObserveRecordsAFingerprintAndIsIdempotent(t *testing.T) {
	s, _ := openStore(t)

	got, err := s.Observe(rec("aaa", ""))
	if err != nil {
		t.Fatal(err)
	}
	if got.Snapshot != "aaa" {
		t.Errorf("snapshot = %q, want aaa", got.Snapshot)
	}
	if got.SealedAt.IsZero() {
		t.Error("SealedAt was not stamped — a fingerprint with no time cannot be placed later")
	}

	// Observing the same value again must not append a second record.
	if _, err := s.Observe(rec("aaa", "")); err != nil {
		t.Fatal(err)
	}
	if n := len(s.History()); n != 1 {
		t.Errorf("history holds %d records for one fingerprint, want 1", n)
	}
}

// 🔒 Re-observing must never clear the exported flag. The posture is read on view mount, so if it
// did, simply opening the Maintenance page would silently retract the fact that a copy was taken.
func TestReObservingDoesNotRetractAnExport(t *testing.T) {
	s, _ := openStore(t)

	if _, err := s.Observe(rec("aaa", "")); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkExported("aaa"); err != nil {
		t.Fatal(err)
	}
	if !s.Exported("aaa") {
		t.Fatal("the export was not recorded")
	}

	// What opening the view does, repeatedly.
	for range 3 {
		if _, err := s.Observe(rec("aaa", "")); err != nil {
			t.Fatal(err)
		}
	}
	if !s.Exported("aaa") {
		t.Error("re-observing the fingerprint retracted the export")
	}
}

// 🔒 An unknown fingerprint reports NOT exported. The safe direction: it produces the caveat rather
// than suppressing it. Defaulting the other way would mean a history lost to a corrupt sidecar
// silently reads as "a copy was taken".
func TestUnknownFingerprintIsNotExported(t *testing.T) {
	s, _ := openStore(t)

	if s.Exported("never-seen") {
		t.Error("an unknown fingerprint reported as exported")
	}
	if _, ok := s.ExportedAt("never-seen"); ok {
		t.Error("an unknown fingerprint reported an export time")
	}
	// And it cannot be marked, either — that would set the flag for a root this case never had.
	if err := s.MarkExported("never-seen"); !errors.Is(err, ErrNoRoot) {
		t.Errorf("MarkExported on an unknown fingerprint = %v, want ErrNoRoot", err)
	}
}

func TestExportedAtReportsWhen(t *testing.T) {
	s, _ := openStore(t)
	if _, err := s.Observe(rec("aaa", "")); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Add(-time.Second)
	if err := s.MarkExported("aaa"); err != nil {
		t.Fatal(err)
	}
	at, ok := s.ExportedAt("aaa")
	if !ok {
		t.Fatal("no export time after marking")
	}
	if at.Before(before) {
		t.Errorf("export time %v predates the call", at)
	}
	// Marking twice must not move the time: the first export is when the copy was taken.
	if err := s.MarkExported("aaa"); err != nil {
		t.Fatal(err)
	}
	again, _ := s.ExportedAt("aaa")
	if !again.Equal(at) {
		t.Errorf("a second mark moved the export time from %v to %v", at, again)
	}
}

// An empty fingerprint is refused rather than stored. A record with no value would sit in the
// history as a fingerprint that cannot be compared against anything.
func TestObserveRefusesAnEmptyFingerprint(t *testing.T) {
	s, _ := openStore(t)
	if _, err := s.Observe(rec("", "")); !errors.Is(err, ErrNoRoot) {
		t.Errorf("Observe with no snapshot = %v, want ErrNoRoot", err)
	}
	if n := len(s.History()); n != 0 {
		t.Errorf("history holds %d records after a refused observe", n)
	}
}

// The history survives a reopen, which is what makes chaining possible at all: after a compaction
// the earlier image is gone, so the earlier fingerprint exists only here.
func TestHistorySurvivesReopenAndKeepsTheChain(t *testing.T) {
	s, dir := openStore(t)

	if _, err := s.Observe(rec("first", "")); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkExported("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Observe(rec("second", "first")); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	hist := reopened.History()
	if len(hist) != 2 {
		t.Fatalf("history holds %d records after reopen, want 2", len(hist))
	}
	if hist[0].Snapshot != "first" || hist[1].Snapshot != "second" {
		t.Errorf("history order = %q, %q; want first, second", hist[0].Snapshot, hist[1].Snapshot)
	}
	if hist[1].Prev != "first" {
		t.Errorf("the chain was lost: second.Prev = %q, want first", hist[1].Prev)
	}
	if !reopened.Exported("first") {
		t.Error("the export record did not survive a reopen")
	}
	if reopened.Exported("second") {
		t.Error("an unexported fingerprint came back as exported")
	}
}

// 🔒 A corrupt sidecar costs KNOWLEDGE, never evidence: the case still opens, the history is simply
// empty — and empty reports "not exported", which produces the caveat rather than hiding it.
func TestCorruptHistoryOpensEmptyRatherThanFailing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, consts.RootsStateFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("a corrupt history failed the open: %v", err)
	}
	if n := len(s.History()); n != 0 {
		t.Errorf("history holds %d records from a corrupt file", n)
	}
	if s.Exported("anything") {
		t.Error("a corrupt history reported an export — it must default to the caveat")
	}
	// And it recovers: a fresh observation writes a valid document.
	if _, err := s.Observe(rec("aaa", "")); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(dir); err != nil || len(reopened.History()) != 1 {
		t.Errorf("the store did not recover after a corrupt read (err = %v)", err)
	}
}

// A document written by a future version is not read as if it were this one.
func TestFutureDocumentVersionIsNotRead(t *testing.T) {
	dir := t.TempDir()
	future := `{"roots_version": 99, "records": [{"snapshot": "aaa", "exported": true}]}`
	if err := os.WriteFile(filepath.Join(dir, consts.RootsStateFile), []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Exported("aaa") {
		t.Error("a record from an unknown document version was trusted")
	}
}
