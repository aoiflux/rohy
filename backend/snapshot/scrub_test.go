package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Scrubbing the copies a redaction removes from snapshots (plan §14.5, item 40).

const mark = "(redacted event)"

func scrubFixture(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	for graph, label := range map[uint64]string{1: "a", 2: "b"} {
		doc := &Document{
			ID: NewID(at), Label: label, GraphID: graph, TakenAt: at,
			Nodes: []Node{
				{ID: 1, Hash: "gone", Descriptor: "4624 HOST-A 2026-07-14 alice"},
				{ID: 2, Hash: "kept", Descriptor: "4625 HOST-B 2026-07-14"},
			},
			Relations: []Relation{
				{ID: 10, FromHash: "gone", ToHash: "kept", Basis: []string{"TargetUserName=alice"}, RuleID: "r"},
				{ID: 11, FromHash: "kept", ToHash: "kept", Basis: []string{"LogonId=0x3e7"}},
			},
		}
		if err := s.Save(doc); err != nil {
			t.Fatal(err)
		}
	}
	return s, dir
}

func TestScrubRemovesDescriptorsAndBasisCopiesInEverySnapshot(t *testing.T) {
	s, dir := scrubFixture(t)
	stale := filepath.Join(dir, "1", "snap-x.json.tmp1618033988")
	if err := os.WriteFile(stale, []byte(`{"descriptor":"alice"`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.Scrub([]string{"gone"}, mark)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changed) != 2 || len(res.Unreadable) != 0 {
		t.Fatalf("%+v", res)
	}
	for _, graph := range []uint64{1, 2} {
		list, _ := s.List(graph)
		doc, err := s.Get(graph, list[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Nodes[0].Descriptor != mark || doc.Nodes[1].Descriptor != "4625 HOST-B 2026-07-14" {
			t.Errorf("graph %d nodes %+v", graph, doc.Nodes)
		}
		if len(doc.Relations[0].Basis) != 0 || len(doc.Relations[1].Basis) != 1 {
			t.Errorf("graph %d relations %+v", graph, doc.Relations)
		}
		// What the edge was is kept; only the copied values go.
		if doc.Relations[0].RuleID != "r" || doc.Relations[0].FromHash != "gone" {
			t.Errorf("graph %d: more than the copy went: %+v", graph, doc.Relations[0])
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale temporary copy: %v", err)
	}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), "alice") {
				t.Errorf("%s still holds the content", path)
			}
		}
		return nil
	})
	if again, err := s.Scrub([]string{"gone"}, mark); err != nil || len(again.Changed) != 0 {
		t.Errorf("second scrub %+v %v", again, err)
	}
}

// 🔒 A file it cannot read may hold a copy; it is named, never skipped as if clean.
func TestScrubNamesWhatItCannotRead(t *testing.T) {
	s, dir := scrubFixture(t)
	bad := filepath.Join(dir, "2", "snap-broken.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := s.Scrub([]string{"gone"}, mark)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Unreadable) != 1 || res.Unreadable[0] != bad {
		t.Errorf("unreadable %v", res.Unreadable)
	}
	if len(res.Changed) != 2 {
		t.Errorf("the readable snapshots were not scrubbed: %+v", res)
	}
}

func TestScrubOfAnEmptyStore(t *testing.T) {
	s, _ := Open(t.TempDir())
	if res, err := s.Scrub([]string{"gone"}, mark); err != nil || len(res.Changed)+len(res.Unreadable) != 0 {
		t.Errorf("%+v %v", res, err)
	}
}
