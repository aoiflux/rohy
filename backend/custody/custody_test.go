package custody

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

// unreadableOpen writes doc as the history, opens it, and checks the two things every unreadable
// history must get: the case still opens, and the bytes are kept, byte for byte, where Damage says.
func unreadableOpen(t *testing.T, doc string) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, consts.RootsStateFile), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("an unreadable history failed the open: %v", err)
	}
	d := s.Damage()
	if d == nil {
		t.Fatal("an unreadable history was not reported")
	}
	if d.Reason == "" || d.KeptAt == "" {
		t.Fatalf("damage = %+v, want a reason and where the document was kept", d)
	}
	kept, err := os.ReadFile(d.KeptAt)
	if err != nil {
		t.Fatalf("the unreadable document is not where Damage says: %v", err)
	}
	if string(kept) != doc {
		t.Errorf("the kept document was altered: %q", kept)
	}
	return s, dir
}

// 🔒 An unreadable history is KEPT, never written over. It used to open as empty and then be
// replaced by the next seal, which silently destroyed the record of which fingerprints had been
// exported — the one thing in it rohy cannot reconstruct. The case still opens (a damaged sidecar
// must not stop that), the history starts again, and the integrity check says where the old one is.
func TestAnUnreadableHistoryIsKeptAndReportedNotOverwritten(t *testing.T) {
	s, dir := unreadableOpen(t, "{not json")

	if n := len(s.History()); n != 0 {
		t.Errorf("history holds %d records from an unreadable file", n)
	}
	if s.Exported("anything") {
		t.Error("an unreadable history reported an export — it must default to the caveat")
	}

	// A seal after it writes a fresh document beside the kept one, not over it.
	if _, err := s.Observe(rec("aaa", "")); err != nil {
		t.Fatal(err)
	}
	if kept, err := os.ReadFile(s.Damage().KeptAt); err != nil || string(kept) != "{not json" {
		t.Errorf("the kept document did not survive a seal (err = %v)", err)
	}
	reopened, err := Open(dir)
	if err != nil || len(reopened.History()) != 1 {
		t.Fatalf("the fresh history did not persist (err = %v)", err)
	}
	// The report belongs to the open that found the damage, not to every open after it.
	if reopened.Damage() != nil {
		t.Errorf("a healthy history reported damage: %+v", reopened.Damage())
	}
}

// A document written by a future build is not read as if it were this one — and, the reason this
// matters, it is not destroyed either: opening a case in an older build must not erase the history
// a newer build kept.
func TestAFutureHistoryIsKeptNotReadOrDestroyed(t *testing.T) {
	future := `{"roots_version": 99, "records": [{"snapshot": "aaa", "exported": true}]}`
	s, _ := unreadableOpen(t, future)

	if s.Exported("aaa") {
		t.Error("a record from an unknown document version was trusted")
	}
}

// Two unreadable documents in the same second must not overwrite each other where they are kept.
func TestKeepingAsideNeverOverwritesAnEarlierKeptDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, consts.RootsStateFile)
	var kept []string
	for _, doc := range []string{"{first", "{second"} {
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		kept = append(kept, s.Damage().KeptAt)
	}
	if kept[0] == kept[1] {
		t.Fatalf("both documents were kept at %s", kept[0])
	}
	for i, want := range []string{"{first", "{second"} {
		if got, err := os.ReadFile(kept[i]); err != nil || string(got) != want {
			t.Errorf("kept[%d] = %q (err = %v), want %q", i, got, err, want)
		}
	}
}

// 🔒 When an unreadable document could not even be moved aside, the history refuses to write rather
// than replace it. A rename failure cannot be staged portably, so this sets the state directly.
func TestAHistoryThatCouldNotBeMovedIsNeverWrittenOver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, consts.RootsStateFile)
	if err := os.WriteFile(path, []byte("{held"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Store{dir: dir, doc: document{Version: consts.RootsDocVersion}, damage: &Damage{Reason: "test"}}

	if _, err := s.Observe(rec("aaa", "")); err == nil {
		t.Fatal("a seal was recorded over a history that could not be moved aside")
	}
	if got, _ := os.ReadFile(path); string(got) != "{held" {
		t.Errorf("the held document was written over: %q", got)
	}
}

// 🔒 A document that could not be READ is left where it is. A read can fail for reasons that pass —
// another process holding the file, a scanner — and moving a valid history aside on one would
// lose it as surely as overwriting it. The history holds instead: nothing is written this session.
func TestAHistoryThatCannotBeReadIsLeftInPlaceAndHeld(t *testing.T) {
	dir := t.TempDir()
	// A directory where the document should be: ReadFile fails, and it is not a parse error.
	if err := os.Mkdir(filepath.Join(dir, consts.RootsStateFile), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("an unreadable history failed the open: %v", err)
	}
	d := s.Damage()
	if d == nil || d.KeptAt != "" {
		t.Fatalf("damage = %+v, want held in place (no KeptAt)", d)
	}
	if fi, err := os.Stat(filepath.Join(dir, consts.RootsStateFile)); err != nil || !fi.IsDir() {
		t.Error("the unreadable document was moved")
	}
	if _, err := s.Observe(rec("aaa", "")); err == nil {
		t.Error("a seal was recorded over a history that could not be read")
	}
}

// The reset is recorded INSIDE the new history, so the next open still says the history started
// again — and where the old one is — rather than only the session that found it.
func TestAResetHistorySaysSoOnEveryOpen(t *testing.T) {
	_, dir := unreadableOpen(t, "{not json")
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	kept := reopened.Kept()
	if len(kept) != 1 || kept[0].KeptAt == "" || kept[0].Reason == "" {
		t.Fatalf("kept = %+v, want the earlier reset with its reason and path", kept)
	}
	if reopened.Damage() != nil {
		t.Errorf("a healthy reopen reported damage of its own: %+v", reopened.Damage())
	}
}

// The rename-failure branch itself: a corrupt document that cannot be moved is held in place, and
// nothing is written over it. On Windows a rename of a file another handle has open fails; on Unix
// a directory without write permission refuses it.
func TestACorruptHistoryThatCannotBeMovedIsHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, consts.RootsStateFile)
	if err := os.WriteFile(path, []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
	} else {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(dir, 0o755)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("a history that cannot be moved failed the open: %v", err)
	}
	if d := s.Damage(); d == nil || d.KeptAt != "" {
		t.Fatalf("damage = %+v, want held in place (no KeptAt)", d)
	}
	if _, err := s.Observe(rec("aaa", "")); err == nil {
		t.Error("a seal was recorded over a history that could not be moved aside")
	}
	if got, _ := os.ReadFile(path); string(got) != "{corrupt" {
		t.Errorf("the held document was written over: %q", got)
	}
}
