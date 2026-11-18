package redaction

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"rohy/backend/consts"
	"rohy/backend/payload"
)

// The redaction journal.
//
// The journal carries two duties, and each fails in its own way. As the record of a redaction in
// progress it must be on disk before anything is destroyed, or a crash loses the only copy of
// where the content was. As the suppression list it must never quietly stop suppressing: a journal
// that cannot be read refuses, it is not replaced.

var at = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

func mustOpen(t *testing.T, dir string) *Journal {
	t.Helper()
	j, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Err(); err != nil {
		t.Fatalf("a fresh journal is damaged: %v", err)
	}
	return j
}

func entry(targets ...Target) Entry {
	return Entry{Event: targets[0].Node, Reason: "erasure request 2026-118", At: at, Targets: targets}
}

func target(node uint64, hn, hr string) Target {
	return Target{Node: node, HashNormalized: hn, HashRaw: hr, Payload: payload.Ref{Offset: 4 * node, Length: 40}}
}

func mustBegin(t *testing.T, j *Journal, e Entry) Entry {
	t.Helper()
	got, err := j.Begin(e)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestABegunRedactionSuppressesItsIdentitiesAtOnce(t *testing.T) {
	j := mustOpen(t, t.TempDir())
	mustBegin(t, j, entry(target(7, "hn-7", "hr-7"), target(9, "hn-9", "")))
	for _, c := range []struct{ hn, hr string }{{"hn-7", ""}, {"", "hr-7"}, {"hn-9", "other"}, {"x", "hr-7"}} {
		if !j.Suppressed(c.hn, c.hr) {
			t.Errorf("Suppressed(%q, %q) = false", c.hn, c.hr)
		}
	}
	// 🔒 An event with no raw record must not match a target that had none either: the empty
	// hash is nobody's identity.
	for _, c := range []struct{ hn, hr string }{{"", ""}, {"hn-8", ""}, {"hn-8", "hr-8"}} {
		if j.Suppressed(c.hn, c.hr) {
			t.Errorf("Suppressed(%q, %q) = true", c.hn, c.hr)
		}
	}
}

func TestTheJournalSurvivesAReopen(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	first := mustBegin(t, j, entry(target(7, "hn-7", "hr-7")))
	second := mustBegin(t, j, entry(target(9, "hn-9", "hr-9")))
	if first.ID == 0 || second.ID <= first.ID {
		t.Fatalf("ids %d, %d", first.ID, second.ID)
	}
	if err := j.Step(first.ID, consts.RedactStepRelations, at, nil); err != nil {
		t.Fatal(err)
	}
	if err := j.Step(first.ID, consts.RedactStepRecords, at, errors.New("disk full")); err != nil {
		t.Fatal(err)
	}
	if err := j.Complete(second.ID, at); err != nil {
		t.Fatal(err)
	}

	again := mustOpen(t, dir)
	if !again.Suppressed("hn-7", "") || !again.Suppressed("", "hr-9") {
		t.Error("suppression did not survive the reopen")
	}
	pending := again.Pending()
	if len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("pending %+v", pending)
	}
	steps := pending[0].Steps
	if stepState(steps, consts.RedactStepRelations) != consts.RedactStepDone ||
		stepState(steps, consts.RedactStepRecords) != consts.RedactStepFailed ||
		stepState(steps, consts.RedactStepPayloads) != "" {
		t.Errorf("steps %+v", steps)
	}
	if e, ok := again.Get(second.ID); !ok || !e.Done {
		t.Errorf("completed entry %+v", e)
	}
	// A new entry after the reopen does not reuse an id.
	if third := mustBegin(t, again, entry(target(11, "hn-11", "hr-11"))); third.ID <= second.ID {
		t.Errorf("id %d reused", third.ID)
	}
}

func stepState(steps []Step, name string) string {
	for _, s := range steps {
		if s.Name == name {
			return s.State
		}
	}
	return ""
}

// 🔒 A damaged journal refuses everything and is left exactly as found. Starting a fresh one would
// quietly stop suppressing every event it named.
func TestADamagedJournalRefusesEverythingAndIsKept(t *testing.T) {
	for name, body := range map[string][]byte{
		"not json":  []byte("{\"version\":1,\"entries\":[{"),
		"newer":     []byte(`{"version":99,"entries":[]}`),
		"no format": []byte(`{"entries":[]}`),
		"bad entry": []byte(`{"version":1,"entries":[{"id":1,"targets":[{"node":0,"hash_normalized":""}]}]}`),
		"dup id": []byte(`{"version":1,"entries":[` +
			`{"id":1,"reason":"r","targets":[{"node":1,"hash_normalized":"a"}]},` +
			`{"id":1,"reason":"r","targets":[{"node":2,"hash_normalized":"b"}]}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, consts.RedactionJournalFile)
			if err := os.WriteFile(file, body, 0o644); err != nil {
				t.Fatal(err)
			}
			j, err := Open(dir)
			if err != nil {
				t.Fatalf("Open failed outright; it must open and report: %v", err)
			}
			if j.Err() == nil || !strings.Contains(j.Err().Error(), file) {
				t.Fatalf("Err() = %v, want one naming %s", j.Err(), file)
			}
			if _, err := j.Begin(entry(target(7, "hn-7", "hr-7"))); err == nil {
				t.Error("a damaged journal accepted a new entry")
			}
			if err := j.Step(1, consts.RedactStepRecords, at, nil); err == nil {
				t.Error("a damaged journal recorded a step")
			}
			if got, _ := os.ReadFile(file); !bytes.Equal(got, body) {
				t.Error("the damaged journal was changed")
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("files beside the journal: %v", entries)
			}
		})
	}
}

func TestANewerJournalSaysSo(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, consts.RedactionJournalFile), []byte(`{"version":99,"entries":[]}`), 0o644)
	j, _ := Open(dir)
	if j.Err() == nil || !strings.Contains(j.Err().Error(), "newer") {
		t.Errorf("Err() = %v", j.Err())
	}
}

func TestAJournalThatCannotBeReadRefuses(t *testing.T) {
	dir := t.TempDir()
	// A directory where the file should be: present, not readable as a file.
	if err := os.Mkdir(filepath.Join(dir, consts.RedactionJournalFile), 0o755); err != nil {
		t.Fatal(err)
	}
	j, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if j.Err() == nil {
		t.Fatal("an unreadable journal reads as usable")
	}
}

func TestAnOversizedJournalIsNotRead(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, consts.RedactionJournalFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(consts.MaxRedactionJournalSize + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	j, _ := Open(dir)
	if j.Err() == nil {
		t.Fatal("an oversized journal reads as usable")
	}
}

// 🔒 A write that fails changes nothing in memory either: the journal held is the journal on disk.
func TestAFailedWriteChangesNothing(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	kept := mustBegin(t, j, entry(target(7, "hn-7", "hr-7")))
	// Replace the file with a directory so the rename onto it fails.
	file := filepath.Join(dir, consts.RedactionJournalFile)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Begin(entry(target(9, "hn-9", "hr-9"))); err == nil {
		t.Fatal("the write did not fail")
	}
	if j.Suppressed("hn-9", "") {
		t.Error("an entry that was never written suppresses")
	}
	if err := j.Complete(kept.ID, at); err == nil {
		t.Fatal("the write did not fail")
	}
	if e, _ := j.Get(kept.ID); e.Done {
		t.Error("a completion that was never written is held")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp*")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestBeginRefusesAnEntryWithNothingToRedact(t *testing.T) {
	j := mustOpen(t, t.TempDir())
	for name, e := range map[string]Entry{
		"no targets":   {Event: 7, Reason: "r", At: at},
		"no node":      entry(target(0, "hn", "hr")),
		"no identity":  entry(target(7, "", "hr")),
		"no reason":    {Event: 7, Reason: " ", At: at, Targets: []Target{target(7, "hn", "hr")}},
		"long reason":  {Event: 7, Reason: strings.Repeat("x", consts.MaxRedactionReasonLen+1), At: at, Targets: []Target{target(7, "hn", "hr")}},
		"invalid text": {Event: 7, Reason: "\xff", At: at, Targets: []Target{target(7, "hn", "hr")}},
	} {
		if _, err := j.Begin(e); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(j.Pending()) != 0 {
		t.Error("a refused entry was kept")
	}
}

func TestStepsNameAKnownEntryAndStep(t *testing.T) {
	j := mustOpen(t, t.TempDir())
	e := mustBegin(t, j, entry(target(7, "hn-7", "hr-7")))
	if err := j.Step(e.ID+1, consts.RedactStepRecords, at, nil); err == nil {
		t.Error("a step on an unknown entry was recorded")
	}
	if err := j.Step(e.ID, "teleport", at, nil); err == nil {
		t.Error("an unknown step was recorded")
	}
	// A retried step overwrites its earlier state rather than adding a second one.
	_ = j.Step(e.ID, consts.RedactStepRecords, at, errors.New("first try"))
	_ = j.Step(e.ID, consts.RedactStepRecords, at, nil)
	got, _ := j.Get(e.ID)
	n := 0
	for _, s := range got.Steps {
		if s.Name == consts.RedactStepRecords {
			n++
			if s.State != consts.RedactStepDone || s.Error != "" {
				t.Errorf("retried step %+v", s)
			}
		}
	}
	if n != 1 {
		t.Errorf("step recorded %d times", n)
	}
}

func TestValidReason(t *testing.T) {
	got, err := ValidReason("  erasure request\n")
	if err != nil || got != "erasure request" {
		t.Errorf("ValidReason = %q, %v", got, err)
	}
	exact := strings.Repeat("é", consts.MaxRedactionReasonLen/2)
	if _, err := ValidReason(exact); err != nil {
		t.Errorf("a reason of exactly the limit: %v", err)
	}
	for _, bad := range []string{"", "  \t", exact + "x", "ok\xff"} {
		if _, err := ValidReason(bad); err == nil {
			t.Errorf("ValidReason(%q) accepted", bad)
		}
	}
}

// What callers get back is a copy: editing it does not edit the journal.
func TestEntriesAreCopies(t *testing.T) {
	j := mustOpen(t, t.TempDir())
	e := mustBegin(t, j, entry(target(7, "hn-7", "hr-7")))
	e.Targets[0].HashNormalized = "changed"
	got, _ := j.Get(e.ID)
	if got.Targets[0].HashNormalized != "hn-7" {
		t.Error("the returned entry aliases the journal")
	}
	p := j.Pending()
	p[0].Targets[0].HashRaw = "changed"
	if !j.Suppressed("", "hr-7") {
		t.Error("Pending aliases the journal")
	}
}

func TestTheJournalIsSafeForConcurrentUse(t *testing.T) {
	j := mustOpen(t, t.TempDir())
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for k := range 20 {
				n := uint64(i*100 + k + 1)
				hn := "hn-" + string(rune('a'+i)) + string(rune('a'+k))
				e, err := j.Begin(entry(target(n, hn, "")))
				if err != nil {
					t.Error(err)
					return
				}
				if !j.Suppressed(hn, "") {
					t.Error("not suppressed after Begin")
				}
				_ = j.Complete(e.ID, at)
			}
		})
	}
	wg.Wait()
	if n := len(j.Pending()); n != 0 {
		t.Errorf("%d left pending", n)
	}
}

// Whatever the file holds, decoding does not panic, everything it accepts is something Begin and
// Step could have written, and what it accepts round-trips unchanged.
func FuzzDecode(f *testing.F) {
	ok, _ := json.Marshal(document{Version: consts.RedactionJournalVersion, Entries: []Entry{{
		ID: 1, Event: 7, Reason: "r", At: at, Targets: []Target{target(7, "hn", "hr")},
		Steps: []Step{{Name: consts.RedactStepRecords, State: consts.RedactStepDone}},
	}}})
	f.Add(ok)
	f.Add([]byte(`{"version":1,"entries":[]}`))
	f.Add([]byte(`{"version":1}`))
	f.Add([]byte(`{"version":1,"entries":[{"id":9007199254740993,"reason":"r","targets":[{"node":1,"hash_normalized":"a"}]}]}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, body []byte) {
		d, err := decode("journal.json", body)
		if err != nil {
			return
		}
		ids := map[uint64]bool{}
		for _, e := range d.Entries {
			if e.ID == 0 || e.ID > maxEntryID || ids[e.ID] || len(e.Targets) == 0 || !utf8.ValidString(e.Reason) {
				t.Fatalf("accepted %+v", e)
			}
			ids[e.ID] = true
			if r, err := ValidReason(e.Reason); err != nil || r != e.Reason {
				t.Fatalf("accepted reason %q", e.Reason)
			}
			for _, tg := range e.Targets {
				if tg.Node == 0 || tg.HashNormalized == "" {
					t.Fatalf("accepted target %+v", tg)
				}
			}
		}
		again, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		d2, err := decode("journal.json", again)
		if err != nil {
			t.Fatalf("what was accepted does not decode again: %v", err)
		}
		if a, b := mustJSON(t, d), mustJSON(t, d2); a != b {
			t.Fatalf("round trip changed it:\n%s\n%s", a, b)
		}
	})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Open over whatever is on disk never panics, and once it accepts a journal the next write and
// reopen keep every entry.
func TestOpenThenWriteKeepsWhatWasThere(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	for i := range 3 {
		mustBegin(t, j, entry(target(uint64(i+1), "hn-"+string(rune('a'+i)), "")))
	}
	again := mustOpen(t, dir)
	mustBegin(t, again, entry(target(9, "hn-z", "")))
	if n := len(mustOpen(t, dir).Entries()); n != 4 {
		t.Errorf("%d entries, want 4", n)
	}
}

func TestPayloadEndCoversEveryCopy(t *testing.T) {
	j := mustOpen(t, t.TempDir())
	if j.PayloadEnd() != 0 {
		t.Fatal("an empty journal names payload")
	}
	tg := target(7, "hn-7", "hr-7") // payload 28..68
	tg.Copies = []payload.Ref{{Offset: 500, Length: 40}, {Offset: 100, Length: 40}}
	mustBegin(t, j, entry(tg))
	if got := j.PayloadEnd(); got != 540 {
		t.Errorf("PayloadEnd = %d, want 540", got)
	}
}

func TestPendingForFindsAnUnfinishedRedactionOfATarget(t *testing.T) {
	j := mustOpen(t, t.TempDir())
	e := mustBegin(t, j, entry(target(7, "hn-7", ""), target(9, "hn-9", "")))
	if got, ok := j.PendingFor(9); !ok || got.ID != e.ID {
		t.Errorf("PendingFor(9) = %+v, %v", got, ok)
	}
	if _, ok := j.PendingFor(8); ok {
		t.Error("an event no entry names is pending")
	}
	_ = j.Complete(e.ID, at)
	if _, ok := j.PendingFor(7); ok {
		t.Error("a finished redaction is pending")
	}
}
