package custody

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// Placing the case's current image against its history of seals.
//
// Every root here is real: a disk case compacted repeatedly, so the chain is checked against what
// graphene actually binds, not against strings shaped like it.

// realSeals seals a fresh disk case n times, writing one event before each, and returns the roots
// each seal produced.
func realSeals(t *testing.T, n int) []*graphene.SnapshotRoots {
	t.Helper()
	return realSealsOf(t, n, "h")
}

// realSealsOf is realSeals over events whose content hashes start with salt, so two cases differ.
// Two cases holding identical content seal to identical roots — deterministically, as they should.
func realSealsOf(t *testing.T, n int, salt string) []*graphene.SnapshotRoots {
	t.Helper()
	s, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	out := make([]*graphene.SnapshotRoots, 0, n)
	for i := range n {
		if _, err := s.InsertEvents([]*graphene.Event{{
			EventID: "4624", Timestamp: base.Add(time.Duration(i) * time.Minute), HashNormalized: fmt.Sprintf("%s%d", salt, i),
		}}); err != nil {
			t.Fatal(err)
		}
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		r, err := s.SnapshotRoots()
		if err != nil || r == nil {
			t.Fatalf("seal %d left no roots (err = %v)", i, err)
		}
		out = append(out, r)
	}
	return out
}

func recordOf(r *graphene.SnapshotRoots) Record {
	return Record{
		Snapshot: r.Snapshot, Prev: r.Prev,
		NodeRoot: r.NodeRoot, EdgeRoot: r.EdgeRoot, IndexRoot: r.IndexRoot,
		TombstoneRoot: r.TombstoneRoot, BodyVersion: r.BodyVersion,
	}
}

// historyOf records the given seals, in order.
func historyOf(t *testing.T, seals ...*graphene.SnapshotRoots) *Store {
	t.Helper()
	s, _ := openStore(t)
	for _, r := range seals {
		if _, err := s.Observe(recordOf(r)); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestTheChainPlacesTheCurrentImage(t *testing.T) {
	r := realSeals(t, 3)
	elsewhere := realSealsOf(t, 1, "other")[0]

	cases := []struct {
		name    string
		history []*graphene.SnapshotRoots
		current *graphene.SnapshotRoots
		want    string
	}{
		{"never sealed", []*graphene.SnapshotRoots{r[0]}, nil, consts.ChainNoImage},
		{"nothing recorded yet", nil, r[0], consts.ChainEmpty},
		{"the last seal recorded", []*graphene.SnapshotRoots{r[0], r[1], r[2]}, r[2], consts.ChainIntact},
		// 🔒 The case this exists for: an image restored from a backup is internally perfect, and
		// every other check passes it.
		{"an earlier seal", []*graphene.SnapshotRoots{r[0], r[1], r[2]}, r[0], consts.ChainRolledBack},
		{"one seal past the record", []*graphene.SnapshotRoots{r[0], r[1]}, r[2], consts.ChainUnrecordedSeal},
		{"two seals past the record", []*graphene.SnapshotRoots{r[0]}, r[2], consts.ChainDiverged},
		{"another case's image", []*graphene.SnapshotRoots{r[0], r[1]}, elsewhere, consts.ChainDiverged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := historyOf(t, tc.history...).Chain(tc.current)
			if rep.State != tc.want {
				t.Errorf("state = %q, want %q", rep.State, tc.want)
			}
			if rep.Gaps != 0 {
				t.Errorf("gaps = %d in a history of consecutive seals", rep.Gaps)
			}
		})
	}
}

func TestARolledBackImageSaysHowFarBack(t *testing.T) {
	r := realSeals(t, 3)
	s := historyOf(t, r...)
	rep := s.Chain(r[0])
	if rep.Later != 2 {
		t.Errorf("later = %d, want 2 seals recorded after the one the image matches", rep.Later)
	}
	if want := s.History()[0].SealedAt; !rep.RolledBackTo.Equal(want) {
		t.Errorf("rolled back to %v, want %v", rep.RolledBackTo, want)
	}
}

// A missing seal in the middle of the history is a gap, reported beside where the image sits —
// a history can have a hole and still end exactly at the current image.
func TestAnUnrecordedSealInTheMiddleIsAGap(t *testing.T) {
	r := realSeals(t, 3)
	rep := historyOf(t, r[0], r[2]).Chain(r[2])
	if rep.State != consts.ChainIntact || rep.Gaps != 1 {
		t.Errorf("report = %+v, want intact with one gap", rep)
	}
}

// 🔒 A record whose components were altered is not a link, even with the right predecessor.
func TestAnAlteredRecordBreaksTheChain(t *testing.T) {
	r := realSeals(t, 2)
	altered := *r[1]
	altered.EdgeRoot = r[0].NodeRoot
	rep := historyOf(t, r[0], &altered).Chain(r[1])
	if rep.Gaps != 1 {
		t.Errorf("gaps = %d, want the altered record to break its link", rep.Gaps)
	}
}

// Asking about the chain records nothing.
func TestTheChainCheckWritesNothing(t *testing.T) {
	r := realSeals(t, 2)
	s := historyOf(t, r[0])
	s.Chain(r[1])
	if n := len(s.History()); n != 1 {
		t.Errorf("history holds %d records after a check, want 1", n)
	}
}

// A history that started again says so, so a short chain is not read as a whole one.
func TestTheChainCarriesHistoryDamage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, consts.RootsStateFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep := s.Chain(realSeals(t, 1)[0])
	if len(rep.Kept) != 1 || rep.Kept[0].KeptAt == "" || rep.Held != nil {
		t.Errorf("kept = %+v, held = %+v; want where the unreadable history was kept, not held", rep.Kept, rep.Held)
	}
	// And on every later open, not only the one that found it.
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep := reopened.Chain(nil); len(rep.Kept) != 1 {
		t.Errorf("a reopen lost the reset: kept = %+v", rep.Kept)
	}
}

// forkedSeals stages a rollback that was then written to and sealed: seals r0, r1, r2 in order,
// then the image as it was at r0 is put back and sealed again, giving f — whose predecessor is r0,
// two seals back. Real compactions throughout.
func forkedSeals(t *testing.T) (r []*graphene.SnapshotRoots, f *graphene.SnapshotRoots) {
	t.Helper()
	dir, backup := t.TempDir(), filepath.Join(t.TempDir(), "db")
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	sealOne := func(i int, hash string) *graphene.SnapshotRoots {
		s, err := graphene.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if _, err := s.InsertEvents([]*graphene.Event{{
			EventID: "4624", Timestamp: base.Add(time.Duration(i) * time.Minute), HashNormalized: hash,
		}}); err != nil {
			t.Fatal(err)
		}
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		roots, err := s.SnapshotRoots()
		if err != nil || roots == nil {
			t.Fatalf("seal %s left no roots (err = %v)", hash, err)
		}
		return roots
	}
	r = append(r, sealOne(0, "a0"))
	if err := os.CopyFS(backup, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	r = append(r, sealOne(1, "a1"), sealOne(2, "a2"))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dir, os.DirFS(backup)); err != nil {
		t.Fatal(err)
	}
	return r, sealOne(3, "after-restore")
}

// 🔒 A rollback that was written to and sealed again is still a rollback. The history ends at the
// new image, so "is the current image an earlier record" no longer answers; the record itself
// does: it names a predecessor two seals back. Unlike a gap, the history proves this one.
func TestARollbackThatWasSealedAgainIsStillReported(t *testing.T) {
	r, f := forkedSeals(t)
	if f.Prev != r[0].Snapshot {
		t.Fatalf("the staging is wrong: the post-restore seal names %s, want r0 %s", f.Prev, r[0].Snapshot)
	}
	s := historyOf(t, r[0], r[1], r[2], f)
	rep := s.Chain(f)

	if rep.State != consts.ChainIntact {
		t.Errorf("state = %q, want intact: the current image is the last seal recorded", rep.State)
	}
	if rep.Forks != 1 || rep.Gaps != 0 {
		t.Fatalf("forks = %d, gaps = %d; want one fork and no gap", rep.Forks, rep.Gaps)
	}
	h := s.History()
	if !rep.ForkFrom.Equal(h[0].SealedAt) || rep.ForkSkipped != 2 {
		t.Errorf("fork from %v skipping %d, want from %v skipping 2", rep.ForkFrom, rep.ForkSkipped, h[0].SealedAt)
	}
}
