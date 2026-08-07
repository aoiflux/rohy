package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
)

var snapBase = time.Date(2026, 7, 27, 14, 11, 33, 0, time.UTC)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func doc(graphID uint64, at time.Time) *Document {
	return &Document{
		ID:        NewID(at),
		GraphID:   graphID,
		GraphName: "Chain",
		TakenAt:   at,
		Viewport:  Viewport{X: -120, Y: 40, Zoom: 0.85},
		Nodes: []Node{
			{ID: 1, Hash: "hash-a", X: 0, Y: 0, Descriptor: "4625 HOST-A"},
			{ID: 2, Hash: "hash-b", X: 200, Y: 0, Descriptor: "4624 HOST-A"},
		},
		Relations: []Relation{
			{ID: 10, FromHash: "hash-a", ToHash: "hash-b", RelationType: consts.RelationCorrelation,
				Label: "then succeeds", CreatedBy: consts.CreatedBySystem, RuleID: "brute-force"},
		},
	}
}

func TestSaveAndReadBack(t *testing.T) {
	s := openStore(t)
	if err := s.Save(doc(7, snapBase)); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.Get(7, NewID(snapBase))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SnapshotVersion != consts.SnapshotVersion {
		t.Errorf("version = %d, want %d", got.SnapshotVersion, consts.SnapshotVersion)
	}
	if len(got.Nodes) != 2 || len(got.Relations) != 1 {
		t.Errorf("contents = %d nodes / %d relations", len(got.Nodes), len(got.Relations))
	}
	if got.Viewport.Zoom != 0.85 {
		t.Errorf("viewport not preserved: %+v", got.Viewport)
	}
}

func TestSnapshotIsReadableJSONOnDisk(t *testing.T) {
	// An analyst's record may outlive this program. The file has to be legible without it — the
	// same commitment the findings sidecar makes.
	s := openStore(t)
	if err := s.Save(doc(7, snapBase)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, "7", NewID(snapBase)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "\n  \"id\"") {
		t.Errorf("snapshot is not indented for reading:\n%s", data)
	}
	var any map[string]any
	if err := json.Unmarshal(data, &any); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	// The endpoint hashes are what a restore trusts. If they are ever dropped from the document,
	// every restore silently falls back to matching ids.
	if !strings.Contains(string(data), "from_hash") {
		t.Error("relations were written without endpoint hashes")
	}
}

func TestListIsNewestFirstAndScopedToItsGraph(t *testing.T) {
	s := openStore(t)
	for i, at := range []time.Time{snapBase, snapBase.Add(time.Minute), snapBase.Add(2 * time.Minute)} {
		d := doc(7, at)
		d.Label = string(rune('a' + i))
		if err := s.Save(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(doc(9, snapBase)); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("listed %d snapshots for graph 7, want 3", len(got))
	}
	if got[0].Label != "c" {
		t.Errorf("newest first violated: %v", got[0])
	}
	if got[0].Nodes != 2 || got[0].Relations != 1 {
		t.Errorf("summary counts = %d/%d", got[0].Nodes, got[0].Relations)
	}
	other, err := s.List(9)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 {
		t.Errorf("graph 9 listed %d, want its own 1", len(other))
	}
}

func TestListOfAGraphWithNoSnapshots(t *testing.T) {
	s := openStore(t)
	got, err := s.List(404)
	if err != nil {
		t.Fatalf("a graph with no snapshots is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d", len(got))
	}
}

func TestOneUnreadableFileDoesNotHideTheRest(t *testing.T) {
	s := openStore(t)
	if err := s.Save(doc(7, snapBase)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "7", "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(7)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("listed %d, want the 1 readable snapshot", len(got))
	}
}

func TestSaveRefusesPastTheCapRatherThanEvictingTheOldest(t *testing.T) {
	// 🔒 A snapshot is something an analyst deliberately took. Quietly deleting one to make room
	// destroys work without asking — the opposite of what a snapshot feature is for.
	s := openStore(t)
	for i := 0; i < consts.MaxSnapshotsPerGraph; i++ {
		if err := s.Save(doc(7, snapBase.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	err := s.Save(doc(7, snapBase.Add(time.Hour)))
	if err == nil {
		t.Fatal("saving past the cap must be refused")
	}
	if !strings.Contains(err.Error(), "delete one") {
		t.Errorf("the message does not say what to do: %v", err)
	}
	got, _ := s.List(7)
	if len(got) != consts.MaxSnapshotsPerGraph {
		t.Errorf("the refused save changed the collection: %d", len(got))
	}
}

func TestSaveRefusesAnOverlongLabel(t *testing.T) {
	s := openStore(t)
	d := doc(7, snapBase)
	d.Label = strings.Repeat("x", consts.MaxSnapshotLabelLen+1)
	if err := s.Save(d); err == nil {
		t.Fatal("an overlong label must be refused rather than truncated")
	}
}

func TestGetMissingSnapshot(t *testing.T) {
	s := openStore(t)
	_, err := s.Get(7, "snap-nope")
	if err == nil {
		t.Fatal("a missing snapshot must be an error")
	}
	if !strings.Contains(err.Error(), "snap-nope") {
		t.Errorf("the error does not name what was asked for: %v", err)
	}
}

func TestGetRefusesAFutureFormatVersion(t *testing.T) {
	// A snapshot written by a later build may mean things this one would misread. Refusing beats
	// loading it partially and restoring a graph from fields that were not understood.
	s := openStore(t)
	d := doc(7, snapBase)
	if err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, "7", d.ID+".json")
	data, _ := os.ReadFile(path)
	bumped := strings.Replace(string(data), `"snapshot_version": 1`, `"snapshot_version": 99`, 1)
	if err := os.WriteFile(path, []byte(bumped), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(7, d.ID); err == nil {
		t.Fatal("a future format version must be refused")
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	s := openStore(t)
	if err := s.Save(doc(7, snapBase)); err != nil {
		t.Fatal(err)
	}
	id := NewID(snapBase)
	if err := s.Delete(7, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Delete(7, id); err != nil {
		t.Errorf("deleting an already-gone snapshot must be success: %v", err)
	}
	got, _ := s.List(7)
	if len(got) != 0 {
		t.Errorf("still listed after delete: %v", got)
	}
}

func TestIDsSortChronologically(t *testing.T) {
	// The id doubles as the filename, so a directory listing reads in the order the snapshots were
	// taken — which is how somebody reading the case folder by hand will encounter them.
	a := NewID(snapBase)
	b := NewID(snapBase.Add(time.Millisecond))
	if a == b {
		t.Fatal("two snapshots a millisecond apart share an id")
	}
	if !(a < b) {
		t.Errorf("ids do not sort chronologically: %q then %q", a, b)
	}
}

func TestTwoSnapshotsInTheSameMillisecondBothSurvive(t *testing.T) {
	// 🔒 The regression this exists for. Millisecond ids collide on any machine quick enough to
	// take two snapshots inside one — which is one double-click on a Linux CI runner, and was a
	// silent overwrite until CI caught it. Driven from a FIXED clock rather than by racing the real
	// one, so it fails everywhere rather than only where the timing happens to line up.
	s := openStore(t)
	first := doc(7, snapBase)
	first.Label = "first"
	second := doc(7, snapBase) // same instant, therefore the same base id
	second.Label = "second"

	if err := s.Save(first); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(second); err != nil {
		t.Fatal(err)
	}

	if first.ID == second.ID {
		t.Fatalf("both snapshots kept the id %q, so one overwrote the other", first.ID)
	}
	// The id the caller is handed must be the one actually on disk, or a restore cannot find it.
	for _, want := range []*Document{first, second} {
		got, err := s.Get(7, want.ID)
		if err != nil {
			t.Fatalf("%s: %v", want.Label, err)
		}
		if got.Label != want.Label {
			t.Errorf("id %q holds %q, want %q", want.ID, got.Label, want.Label)
		}
	}
	list, err := s.List(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("listed %d, want both snapshots", len(list))
	}
	// Still sortable: the disambiguated id sorts immediately after the one it followed.
	if !(first.ID < second.ID) {
		t.Errorf("disambiguated id breaks the ordering: %q then %q", first.ID, second.ID)
	}
}

func TestIDsCannotEscapeTheSnapshotDirectory(t *testing.T) {
	// The id crosses the API boundary, so it is sanitized where it becomes a path rather than
	// trusted because of where it usually comes from.
	s := openStore(t)
	if _, err := s.Get(7, "../../etc/passwd"); err == nil {
		t.Fatal("a traversing id must not resolve")
	}
	if err := s.Delete(7, "../../etc/passwd"); err != nil {
		t.Errorf("delete of a traversing id should be a harmless no-op: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "..", "..", "etc")); err == nil {
		t.Error("a traversing id reached outside the store")
	}
}
