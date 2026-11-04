package graphene

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/payload"

	"github.com/aoiflux/graphene/disk"
)

func mustCustody(t *testing.T, s *Store, id uint64) EventCustody {
	t.Helper()
	c, err := s.EventCustody(id)
	if err != nil {
		t.Fatalf("EventCustody(%d): %v", id, err)
	}
	return c
}

// gapLayers is the set of layers the engine reported a gap in, and whether any was fatal.
func gapLayers(c EventCustody) ([]string, bool) {
	var out []string
	fatal := false
	for _, g := range c.Gaps {
		if !slices.Contains(out, g.Layer) {
			out = append(out, g.Layer)
		}
		fatal = fatal || g.Fatal
	}
	slices.Sort(out)
	return out, fatal
}

// 🔒 The states §14.4 says rohy must not flatten, plus the one it missed: an event deleted since the
// seal is no longer live but is still in the sealed image, until the next seal drops it.
func TestEventCustodyKeepsEveryPresenceStateApart(t *testing.T) {
	late := time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC)
	type want struct {
		live, inSnapshot, fingerprint bool
		proof                         string
	}
	check := func(t *testing.T, c EventCustody, w want) {
		t.Helper()
		got := want{c.Live, c.InSnapshot, c.HasFingerprint, c.ProofState}
		if got != w {
			t.Fatalf("got %+v, want %+v", got, w)
		}
		if c.InSnapshot == (c.Snapshot == "") {
			t.Errorf("snapshot %q with in-snapshot %v", c.Snapshot, c.InSnapshot)
		}
	}

	t.Run("never sealed", func(t *testing.T) {
		s, ids := provableCase(t)
		check(t, mustCustody(t, s, ids[0]), want{true, false, false, consts.EventProofNotSealed})
	})
	t.Run("in the seal", func(t *testing.T) {
		s, ids := provableCase(t)
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		c := mustCustody(t, s, ids[0])
		check(t, c, want{true, true, true, consts.EventProofOK})
		roots, err := s.SnapshotRoots()
		if err != nil {
			t.Fatal(err)
		}
		if c.Snapshot != roots.Snapshot || !c.RawBound {
			t.Errorf("proved into %q (case is %q), raw bound %v", c.Snapshot, roots.Snapshot, c.RawBound)
		}
	})
	t.Run("written since the seal", func(t *testing.T) {
		s, _ := provableCase(t)
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		ids, err := s.InsertEvents([]*Event{provable("4634", "p3", late)})
		if err != nil {
			t.Fatal(err)
		}
		check(t, mustCustody(t, s, ids[0]), want{true, false, true, consts.EventProofWrittenSinceSeal})
	})
	t.Run("deleted since the seal", func(t *testing.T) {
		s, ids := provableCase(t)
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteEvent(ids[0]); err != nil {
			t.Fatal(err)
		}
		check(t, mustCustody(t, s, ids[0]), want{false, true, true, ""})
	})
	t.Run("deleted and sealed over", func(t *testing.T) {
		s, ids := provableCase(t)
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteEvent(ids[0]); err != nil {
			t.Fatal(err)
		}
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		check(t, mustCustody(t, s, ids[0]), want{false, false, true, ""})
	})
	t.Run("an id the case never held", func(t *testing.T) {
		s, _ := provableCase(t)
		for _, id := range []uint64{0, 999_999} {
			check(t, mustCustody(t, s, id), want{false, false, false, ""})
		}
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
		for _, id := range []uint64{0, 999_999} {
			check(t, mustCustody(t, s, id), want{false, false, true, ""})
		}
	})
}

func TestEventCustodyRefusesANodeThatIsNotAnEvent(t *testing.T) {
	s, _ := provableCase(t)
	g, err := s.graph()
	if err != nil {
		t.Fatal(err)
	}
	other, err := g.AddNodes(notAnEvent())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if c, err := s.EventCustody(uint64(other[0])); err == nil {
		t.Fatalf("a node that is not an event was accounted for as one: %+v", c)
	}
}

// A raw record that cannot be read is reported inside the account, not as a failure of it: the
// seal and the record layers are still worth knowing about.
func TestEventCustodyCarriesAnUnreadableRawRecord(t *testing.T) {
	s, ids := provableCase(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	unreadablePayloads(t, s)
	c := mustCustody(t, s, ids[0])
	if c.ProofState != consts.EventProofRawUnreadable || !c.InSnapshot {
		t.Fatalf("state %q, in snapshot %v", c.ProofState, c.InSnapshot)
	}
}

func TestEventCustodyIsUnavailableInMemory(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	ids, err := s.InsertEvents([]*Event{provable("4624", "m1", time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EventCustody(ids[0]); !errors.Is(err, ErrForensicsUnavailable) {
		t.Fatalf("err = %v, want ErrForensicsUnavailable", err)
	}
}

// 🔒 rohy writes its own words for each engine layer and matches them by name. If graphene renames
// one, rohy's copy would silently stop applying and every gap would fall to the generic line.
func TestRohyNamesTheEngineLayersAsTheEngineDoes(t *testing.T) {
	for engine, rohy := range map[disk.CustodyLayer]string{
		disk.LayerSnapshot:    consts.CustodyLayerSnapshot,
		disk.LayerAttestation: consts.CustodyLayerAttestation,
		disk.LayerSegments:    consts.CustodyLayerSegments,
		disk.LayerAudit:       consts.CustodyLayerAudit,
		disk.LayerRedaction:   consts.CustodyLayerRedaction,
		disk.LayerRoles:       consts.CustodyLayerRoles,
		disk.LayerExternal:    consts.CustodyLayerExternal,
	} {
		if string(engine) != rohy {
			t.Errorf("engine layer %q, rohy names it %q", engine, rohy)
		}
	}
}

// 🔒 Pins exactly what the engine reports for a case opened the way rohy opens one: no audit log,
// no retention, no roles, no signer. A graphene upgrade that adds, drops or hardens a gap fails
// here, so the report's copy is revisited rather than quietly describing a different engine.
func TestRohysPostureProducesExactlyTheseGaps(t *testing.T) {
	storeWide := []string{
		consts.CustodyLayerAttestation, consts.CustodyLayerAudit, consts.CustodyLayerExternal,
		consts.CustodyLayerRoles, consts.CustodyLayerSegments,
	}
	withSnapshot := slices.Sorted(slices.Values(append(slices.Clone(storeWide), consts.CustodyLayerSnapshot)))

	s, ids := provableCase(t)
	assertGaps := func(when string, c EventCustody, want []string) {
		t.Helper()
		got, fatal := gapLayers(c)
		if !slices.Equal(got, want) || fatal {
			t.Errorf("%s: gaps in %v (fatal %v), want %v, none fatal", when, got, fatal, want)
		}
		if c.Attested || c.AttestationVerified || c.SegmentsChecked != 0 || c.AuditEntriesWalked != 0 ||
			c.CompactionsRecorded != 0 || c.RedactionsWalked != 0 || c.GrantsWalked != 0 ||
			c.Redacted != nil || c.RemovalProvable {
			t.Errorf("%s: the engine walked records rohy never writes: %+v", when, c)
		}
	}
	assertGaps("never sealed", mustCustody(t, s, ids[0]), withSnapshot)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	assertGaps("in the seal", mustCustody(t, s, ids[0]), storeWide)
	assertGaps("never held", mustCustody(t, s, 999_999), withSnapshot)
}

// 🔒 Found by review, and wider than the review said. A damaged image made the report fail for
// every live event: the record in a damaged image may not even decode, and ProveEvent's self-check
// refuses what does. So the broken seal line, built for exactly this, could not appear. Swept over
// the image rather than one byte, because which bytes the proof reads is the engine's layout: every
// flip that still opens must produce a report, and one the engine finds broken says so.
func TestADamagedImageIsReportedNotFailed(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := s.InsertEvents([]*Event{
		provable("4624", "d1", time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)),
		provable("4688", "d2", time.Date(2026, 8, 9, 10, 1, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	csr := findCSR(t, dir)
	orig, err := os.ReadFile(csr)
	if err != nil {
		t.Fatal(err)
	}
	damaged := 0
	for at := 0; at < len(orig); at += 11 {
		copyDir := t.TempDir()
		if err := os.CopyFS(copyDir, os.DirFS(dir)); err != nil {
			t.Fatal(err)
		}
		data := slices.Clone(orig)
		data[at] ^= 0x01
		if err := os.WriteFile(filepath.Join(copyDir, filepath.Base(csr)), data, 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := Open(copyDir)
		if err != nil {
			continue // structural validation caught it at open; nothing to report on
		}
		for _, id := range ids {
			c, err := r.EventCustody(id)
			if err != nil {
				t.Errorf("byte %d, event %d: the report failed instead of saying what broke: %v", at, id, err)
				continue
			}
			if _, fatal := gapLayers(c); fatal && c.Live {
				if c.ProofState != consts.EventProofImageDamaged {
					t.Errorf("byte %d, event %d: a broken seal with proof state %q", at, id, c.ProofState)
				}
				damaged++
			}
		}
		r.Close()
	}
	if damaged == 0 {
		t.Fatal("no flip produced a broken seal, so this test checks nothing")
	}
}

// ProveEvent's own self-check, the export's side of the same damage: a proof that does not resolve
// to its image's root is a state, never a document.
func TestAProofThatDoesNotResolveIsImageDamage(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := s.InsertEvents([]*Event{provable("4624", "d1", time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	csr := findCSR(t, dir)
	orig, err := os.ReadFile(csr)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for at := 0; at < len(orig) && !seen; at++ {
		copyDir := t.TempDir()
		if err := os.CopyFS(copyDir, os.DirFS(dir)); err != nil {
			t.Fatal(err)
		}
		data := slices.Clone(orig)
		data[at] ^= 0x01
		if err := os.WriteFile(filepath.Join(copyDir, filepath.Base(csr)), data, 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := Open(copyDir)
		if err != nil {
			continue
		}
		if p, err := r.ProveEvent(ids[0]); err == nil && p.State == consts.EventProofImageDamaged {
			seen = true
			if len(p.Document) != 0 || p.Snapshot != "" {
				t.Errorf("byte %d: image damage handed out a document", at)
			}
		}
		r.Close()
	}
	if !seen {
		t.Fatal("no flip reached the self-check; the state is untested")
	}
}

// The branch a race takes: the event is deleted after the engine's account and before its proof.
// It is reported as gone, not as a failed report.
func TestEventCustodyReportsADeleteBetweenItsReads(t *testing.T) {
	s, ids := provableCase(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	betweenCustodyReads = func() {
		if err := s.DeleteEvent(ids[0]); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { betweenCustodyReads = func() {} })
	c := mustCustody(t, s, ids[0])
	if c.Live || !c.InSnapshot || c.ProofState != "" {
		t.Fatalf("live %v, in snapshot %v, proof %q", c.Live, c.InSnapshot, c.ProofState)
	}
}

// Only a raw record that is not there to read is damage. The case closing, or an I/O error that
// may pass, is not — and calling it damage would send the analyst looking for a fault.
func TestOnlyAMissingRawRecordIsDamage(t *testing.T) {
	var syntax error = &json.SyntaxError{}
	for err, want := range map[error]bool{
		&payload.OutOfRangeError{Offset: 9, Length: 4, End: 2}: true,
		fmt.Errorf("decode: %w", syntax):                       true,
		&json.UnmarshalTypeError{Value: "number"}:              true,
		payload.ErrClosed:                                      false,
		fmt.Errorf("read: %w", os.ErrPermission):               false,
		nil:                                                    false,
	} {
		if got := rawUnreadable(err); got != want {
			t.Errorf("%v: damage %v, want %v", err, got, want)
		}
	}
}

func TestRawHashedIsCarriedApartFromRawBound(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	e := provable("4624", "rh", time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
	e.RawXML = "" // the hash survives, the raw record does not
	ids, err := s.InsertEvents([]*Event{e})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	c := mustCustody(t, s, ids[0])
	if c.ProofState != consts.EventProofOK || c.RawBound || !c.RawHashed {
		t.Fatalf("proof %q, bound %v, hashed %v", c.ProofState, c.RawBound, c.RawHashed)
	}
}
