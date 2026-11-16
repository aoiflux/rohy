package graphene

import (
	"errors"
	"testing"
	"time"
)

// Whether a case has anything to seal (plan §14.2, §15.5).
//
// Every compaction writes a new fingerprint, even when nothing changed, because the new image names
// the old one as its predecessor and that link is part of the root. So the automatic seals — after
// an ingest, and on close — ask this first. If it says "nothing to seal" when something was
// written, that write is left outside every fingerprint; if it says "seal" when nothing was, the
// fingerprint an analyst exported goes stale for no reason. Both directions are pinned here.

func wantUnsealed(t *testing.T, s *Store, want bool, when string) {
	t.Helper()
	got, err := s.Unsealed()
	if err != nil {
		t.Fatalf("%s: %v", when, err)
	}
	if got != want {
		t.Fatalf("%s: Unsealed() = %v, want %v", when, got, want)
	}
}

func TestACaseNeedsSealingUntilCompactedAndAgainAfterAWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Closes whichever store s holds at the end, so a failure before the reopen still releases the
	// files (Windows will not remove a temp dir with the log open).
	t.Cleanup(func() {
		if s != nil {
			s.Close()
		}
	})
	wantUnsealed(t, s, true, "an empty case that was never compacted")

	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("4624", "Microsoft-Windows-Security-Auditing", "Security", "alice", base, "h1"),
		mkEvent("4688", "Microsoft-Windows-Security-Auditing", "Security", "alice", base.Add(time.Minute), "h2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantUnsealed(t, s, true, "events written, never compacted")

	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	wantUnsealed(t, s, false, "just compacted")

	// 🔒 Reopening must not read as a change. The seal on close is the case this decision is for:
	// open a case, look at it, close it — and the fingerprint the analyst exported is still the
	// fingerprint.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s, err = Open(dir); err != nil {
		t.Fatal(err)
	}
	wantUnsealed(t, s, false, "reopened with nothing written")

	// Each kind of write counts: an update, a delete, an insert.
	if err := s.IncrementDedupCounts(map[uint64]map[string]int{ids[0]: {`D:\copy\Security.evtx`: 1}}); err != nil {
		t.Fatal(err)
	}
	wantUnsealed(t, s, true, "an event's counts were updated")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	wantUnsealed(t, s, false, "resealed")

	if err := s.DeleteEvent(ids[1]); err != nil {
		t.Fatal(err)
	}
	wantUnsealed(t, s, true, "an event was deleted")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.InsertEvents([]*Event{
		mkEvent("4625", "Microsoft-Windows-Security-Auditing", "Security", "bob", base.Add(2*time.Minute), "h3"),
	}); err != nil {
		t.Fatal(err)
	}
	wantUnsealed(t, s, true, "an event was inserted")
}

// 🔒 The in-memory backend cannot answer, and says so rather than answering "nothing to seal" —
// the same discipline as every other forensic read.
func TestAnInMemoryCaseCannotSayWhetherItIsSealed(t *testing.T) {
	_, err := OpenInMemory().Unsealed()
	if !errors.Is(err, ErrForensicsUnavailable) {
		t.Fatalf("Unsealed on the in-memory backend: err = %v, want ErrForensicsUnavailable", err)
	}
}

// sealedThrice compacts a disk case three times, writing between seals, and returns the roots each
// seal produced — real values, so the chain check is pinned against what graphene actually writes.
func sealedThrice(t *testing.T) []*SnapshotRoots {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	var out []*SnapshotRoots
	for i, h := range []string{"c1", "c2", "c3"} {
		if _, err := s.InsertEvents([]*Event{
			mkEvent("4624", "Microsoft-Windows-Security-Auditing", "Security", "alice", base.Add(time.Duration(i)*time.Minute), h),
		}); err != nil {
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

func TestVerifyChainFollowsRealSeals(t *testing.T) {
	r := sealedThrice(t)

	for i := 1; i < len(r); i++ {
		if err := VerifyChain(*r[i-1], *r[i]); err != nil {
			t.Errorf("seal %d does not follow seal %d: %v", i, i-1, err)
		}
	}

	// The link is directional and not transitive: a chain is walked one seal at a time.
	if VerifyChain(*r[1], *r[0]) == nil {
		t.Error("an earlier seal verified as following a later one")
	}
	if VerifyChain(*r[0], *r[2]) == nil {
		t.Error("a seal verified as directly following one two seals back")
	}

	// 🔒 The later record's components must bind to its fingerprint. A record whose prev link is
	// right but whose contents were swapped is not a link.
	forged := *r[1]
	forged.NodeRoot = r[2].NodeRoot
	if VerifyChain(*r[0], forged) == nil {
		t.Error("a record with altered components verified as a link")
	}

	bad := *r[1]
	bad.EdgeRoot = "not hex"
	if VerifyChain(*r[0], bad) == nil {
		t.Error("a record with an unreadable root verified as a link")
	}
	if VerifyChain(SnapshotRoots{}, *r[0]) == nil {
		t.Error("a link from nothing verified")
	}
}
