package graphene

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/eventproof"

	"github.com/aoiflux/graphene/disk"
)

// A case written by an older engine must keep working under this one. testdata/v8-case was written
// by rohy d4e4b54 on graphene v0.4.0, whose image format is v8; this engine writes v9, and the
// first seal under it converts the case for good (the owner accepted that one-way step for v0.3.0).

type v8Manifest struct {
	SealedRoot    string   `json:"sealed_root"`
	Events        []uint64 `json:"events"`
	Relations     []uint64 `json:"relations"`
	RedactedEvent uint64   `json:"redacted_event"`
	WALEvent      uint64   `json:"wal_event"`
	Checkpoint    struct {
		Digest string `json:"digest"`
	} `json:"checkpoint"`
}

// openV8Case opens a private copy of the fixture, so no test can change the committed bytes.
func openV8Case(t *testing.T) (*Store, v8Manifest) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "v8-case", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m v8Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	dir := copyStoreDir(t, filepath.Join("testdata", "v8-case", "case"))
	s := OpenAwaitingSession(dir)
	t.Cleanup(func() { s.Close() })
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned, Actor: testActor}); err != nil {
		t.Fatalf("a v0.4.0 case does not open: %v", err)
	}
	return s, m
}

// imageVersion reads the version of the image on disk. StorageStats reports the image the store
// loaded at open, which a seal does not change.
func imageVersion(t *testing.T, s *Store) uint16 {
	t.Helper()
	info, err := disk.InspectCSR(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	return info.Version
}

// 🔒 Everything the old engine wrote reads back: the sealed events, the event written after the
// seal (replayed from the log), the relations, the redaction, and the checkpoint chain.
func TestAV8CaseReadsBackWhole(t *testing.T) {
	s, m := openV8Case(t)
	if v := imageVersion(t, s); v != 8 {
		t.Fatalf("image version %d at open, want the fixture's 8", v)
	}
	for _, id := range append(m.Events[:2:2], m.WALEvent) {
		if e, err := s.GetEvent(id); err != nil || e.Redacted {
			t.Fatalf("event %d: %+v, %v", id, e, err)
		}
	}
	if e, err := s.GetEvent(m.RedactedEvent); err != nil || !e.Redacted {
		t.Fatalf("redacted event %d: %+v, %v", m.RedactedEvent, e, err)
	}
	for _, id := range m.Relations {
		if _, err := s.GetRelation(id); err != nil {
			t.Fatalf("relation %d: %v", id, err)
		}
	}
	h := histories(t, s)
	if _, state, _ := h.Find(m.Checkpoint.Digest); state != consts.CheckpointFound || h.ChainState().ChainErr != "" {
		t.Fatalf("checkpoint: %s %+v", state, h.ChainState())
	}
}

// 🔒 The fingerprint an analyst kept from the old build still holds: this engine re-derives the same
// roots from the v8 image, and a proof built from it verifies against the root the old build sealed.
func TestAV8CaseKeepsItsFingerprint(t *testing.T) {
	s, m := openV8Case(t)
	check, err := s.VerifyImage()
	if err != nil {
		t.Fatal(err)
	}
	if !check.OK() || !check.RootsChecked || check.RootsError != "" {
		t.Fatalf("the v8 image does not verify under this engine: %+v", check)
	}
	root := rootOf(t, s)
	if got := hex.EncodeToString(root[:]); got != m.SealedRoot {
		t.Fatalf("root %s, the old build sealed %s", got, m.SealedRoot)
	}
	p := mustProve(t, s, m.Events[1])
	if _, err := eventproof.Verify(root, p.Document); err != nil {
		t.Fatalf("a proof from the v8 image does not verify against its root: %v", err)
	}
}

// 🔒 The first seal converts the case to v9, and the converted case is whole and provable.
func TestSealingAV8CaseConvertsItToV9(t *testing.T) {
	s, m := openV8Case(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if v := imageVersion(t, s); v != 9 {
		t.Fatalf("image version %d after the seal, want 9", v)
	}
	check, err := s.VerifyImage()
	if err != nil || !check.OK() || check.RootsError != "" {
		t.Fatalf("the converted image: %+v, %v", check, err)
	}
	root := rootOf(t, s)
	for _, id := range []uint64{m.Events[0], m.WALEvent} {
		p := mustProve(t, s, id)
		if _, err := eventproof.Verify(root, p.Document); err != nil {
			t.Fatalf("event %d after the conversion: %v", id, err)
		}
	}
}
