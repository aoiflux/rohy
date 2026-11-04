package graphene

import (
	"encoding/hex"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/eventproof"
	"rohy/backend/utils"

	"github.com/aoiflux/graphene/merkle"
)

// provable is an event whose hash_raw really is the hash of its raw record, as every normalizer
// writes it. (mkEvent's hash_raw is a placeholder, which is exactly the damage raw_mismatch reports.)
func provable(eventID, hash string, ts time.Time) *Event {
	e := mkEvent(eventID, "Microsoft-Windows-Security-Auditing", "Security", "alice", ts, hash)
	e.RawXML = `{"Event":{"System":{"EventID":` + eventID + `}}}`
	e.HashRaw = utils.HashString(e.RawXML)
	e.SourceIdentifier = `C:\evidence\Security.evtx`
	return e
}

// provableCase is a disk case holding two provable events, not yet sealed.
func provableCase(t *testing.T) (*Store, []uint64) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		provable("4624", "p1", base),
		provable("4688", "p2", base.Add(time.Minute)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, ids
}

func mustProve(t *testing.T, s *Store, id uint64) EventProof {
	t.Helper()
	p, err := s.ProveEvent(id)
	if err != nil {
		t.Fatalf("ProveEvent(%d): %v", id, err)
	}
	return p
}

func rootOf(t *testing.T, s *Store) merkle.Hash {
	t.Helper()
	roots, err := s.SnapshotRoots()
	if err != nil || roots == nil {
		t.Fatalf("roots = %v, err = %v", roots, err)
	}
	raw, err := hex.DecodeString(roots.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var h merkle.Hash
	copy(h[:], raw)
	return h
}

// The positive case, end to end through rohy's own write path: the document verifies against the
// case's fingerprint, and names that fingerprint as the one it was built against.
func TestASealedEventProvesAgainstTheCaseFingerprint(t *testing.T) {
	s, ids := provableCase(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}

	p := mustProve(t, s, ids[1])
	if p.State != consts.EventProofOK {
		t.Fatalf("state = %q, want ok", p.State)
	}
	root := rootOf(t, s)
	if p.Snapshot != hex.EncodeToString(root[:]) {
		t.Errorf("built against %s, but the case fingerprint is %x", p.Snapshot, root)
	}
	rep, err := eventproof.Verify(root, p.Document)
	if err != nil {
		t.Fatalf("the document does not verify against the case fingerprint: %v", err)
	}
	if rep.Subject.EventID != "4688" || !rep.RawCarried {
		t.Errorf("subject = %+v, raw carried = %v", rep.Subject, rep.RawCarried)
	}
}

func TestUnprovableEventsSayWhy(t *testing.T) {
	t.Run("in-memory case", func(t *testing.T) {
		s := OpenInMemory()
		defer s.Close()
		ids, err := s.InsertEvents([]*Event{provable("4624", "m1", time.Now())})
		if err != nil {
			t.Fatal(err)
		}
		if p := mustProve(t, s, ids[0]); p.State != consts.EventProofUnavailable {
			t.Errorf("state = %q, want unavailable", p.State)
		}
	})

	t.Run("never sealed", func(t *testing.T) {
		s, ids := provableCase(t)
		if p := mustProve(t, s, ids[0]); p.State != consts.EventProofNotSealed {
			t.Errorf("state = %q, want not_sealed", p.State)
		}
	})

	t.Run("written since the seal", func(t *testing.T) {
		s, _ := provableCase(t)
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		late, err := s.InsertEvents([]*Event{provable("4634", "p3", time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC))})
		if err != nil {
			t.Fatal(err)
		}
		if p := mustProve(t, s, late[0]); p.State != consts.EventProofWrittenSinceSeal {
			t.Errorf("state = %q, want written_since_seal", p.State)
		}
	})

	// The case that motivates comparing by hash: re-ingesting a source moves an event's dedup
	// count, so its stored record is no longer the sealed one. Sealing again makes it provable.
	t.Run("changed since the seal, then sealed again", func(t *testing.T) {
		s, ids := provableCase(t)
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		if err := s.IncrementDedupCounts(map[uint64]map[string]int{ids[0]: {`D:\copy\Security.evtx`: 1}}); err != nil {
			t.Fatal(err)
		}
		if p := mustProve(t, s, ids[0]); p.State != consts.EventProofChangedSinceSeal {
			t.Fatalf("state = %q, want changed_since_seal", p.State)
		}
		if p := mustProve(t, s, ids[1]); p.State != consts.EventProofOK {
			t.Errorf("an untouched event in the same case: state = %q, want ok", p.State)
		}
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		p := mustProve(t, s, ids[0])
		if p.State != consts.EventProofOK {
			t.Fatalf("after sealing again: state = %q, want ok", p.State)
		}
		if _, err := eventproof.Verify(rootOf(t, s), p.Document); err != nil {
			t.Errorf("the re-sealed proof does not verify: %v", err)
		}
	})

	// A raw record that no longer hashes to the stored record's hash_raw is a finding. Exporting
	// it would hand over a file that fails, so the state says what is wrong instead.
	t.Run("raw record does not match its hash", func(t *testing.T) {
		s, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		ids, err := s.InsertEvents([]*Event{mkEvent("4624", "p", "Security", "alice", time.Now(), "x")})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		if p := mustProve(t, s, ids[0]); p.State != consts.EventProofRawMismatch {
			t.Errorf("state = %q, want raw_mismatch", p.State)
		}
	})
}

// A raw record whose stored record carries no hash_raw was never bound by the seal, so there is
// nothing to check it against. The proof still proves the stored fields, carries no raw record,
// and says so — rather than failing outright, or carrying a record nothing vouches for.
func TestARawRecordTheSealNeverBoundIsLeftOut(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := provable("4624", "u1", time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
	e.HashRaw = ""
	ids, err := s.InsertEvents([]*Event{e})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	p := mustProve(t, s, ids[0])
	if p.State != consts.EventProofOK {
		t.Fatalf("state = %q, want ok", p.State)
	}
	rep, err := eventproof.Verify(rootOf(t, s), p.Document)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RawCarried {
		t.Error("a raw record the seal never bound travelled with the proof")
	}
}

func TestProvingAnEventThatDoesNotExistIsAnError(t *testing.T) {
	s, _ := provableCase(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProveEvent(999_999); err == nil {
		t.Fatal("proving a node the case never held returned no error")
	}
}
