package findings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Scrubbing the descriptor copies a redaction removes (plan §14.5, item 40).

const redactedMark = "(redacted event)"

func TestScrubReplacesTheDescriptorAndKeepsTheAnalystsWords(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	for _, f := range []Finding{
		{Key: "gone", Note: "logon from the contractor laptop", Tags: []string{"lateral"}, Descriptor: "4624 HOST-A 2026-07-14 alice"},
		{Key: "kept", Flagged: true, Descriptor: "4625 HOST-B 2026-07-14"},
	} {
		if _, err := s.Set(f, now); err != nil {
			t.Fatal(err)
		}
	}
	// A temporary copy left by an interrupted write holds the same text: under a random name from
	// this build, under the bare suffix from a build before it.
	stales := []string{
		filepath.Join(dir, findingsFile+".tmp2718281828"),
		filepath.Join(dir, findingsFile+".tmp"),
	}
	for _, stale := range stales {
		if err := os.WriteFile(stale, []byte(`{"descriptor":"4624 HOST-A 2026-07-14 alice"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	changed, err := s.ScrubDescriptors([]string{"gone", "absent"}, redactedMark)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "gone" {
		t.Errorf("changed %v", changed)
	}
	got := s.Get("gone")
	if got.Descriptor != redactedMark || got.Note != "logon from the contractor laptop" || len(got.Tags) != 1 {
		t.Errorf("scrubbed finding %+v", got)
	}
	if s.Get("kept").Descriptor != "4625 HOST-B 2026-07-14" {
		t.Error("another finding's descriptor changed")
	}
	for _, stale := range stales {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Errorf("the stale temporary copy %s is still there: %v", filepath.Base(stale), err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, findingsFile))
	if strings.Contains(string(data), "alice") {
		t.Error("the descriptor text is still in the file")
	}
	again, err := Open(dir)
	if err != nil || again.Get("gone").Descriptor != redactedMark {
		t.Errorf("after a reopen: %v %+v", err, again.Get("gone"))
	}

	// Idempotent: a second scrub changes nothing and says so.
	if changed, err := s.ScrubDescriptors([]string{"gone"}, redactedMark); err != nil || len(changed) != 0 {
		t.Errorf("second scrub %v %v", changed, err)
	}
}

func TestScrubOfNothingWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if changed, err := s.ScrubDescriptors(nil, redactedMark); err != nil || len(changed) != 0 {
		t.Fatalf("%v %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, findingsFile)); !os.IsNotExist(err) {
		t.Error("a scrub of nothing wrote the file")
	}
}
