package graphene

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/merkle"

	"rohy/backend/consts"
)

// Checkpoints (item 42): what rohy records, and what it reads back to check one.

func publishOne(t *testing.T, s *Store) Checkpoint {
	t.Helper()
	if _, _, err := s.ReadyCheckpointChain(time.Now()); err != nil {
		t.Fatal(err)
	}
	c, err := s.PublishCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func histories(t *testing.T, s *Store) *Histories {
	t.Helper()
	h, err := s.Histories()
	if err != nil {
		t.Fatal(err)
	}
	if u := h.Unread(); len(u) != 0 {
		t.Fatalf("unread: %v", u)
	}
	return h
}

func sealedWithOne(t *testing.T) (*Store, string) {
	t.Helper()
	s, dir := attributedStore(t)
	oneEvent(t, s, "a")
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func heldStates(held []LayerHeld) map[string]string {
	out := map[string]string{}
	for _, l := range held {
		out[l.Layer] = l.State
	}
	return out
}

// The checkpoint handed out is the one the chain holds, its digest checks, and it covers the
// engine's own announcement of it — the entry pinned here is the one Unrecorded parses.
func TestAPublishedCheckpointIsTheOneTheChainHolds(t *testing.T) {
	s, _ := sealedWithOne(t)
	c := publishOne(t, s)

	roots, err := s.SnapshotRoots()
	if err != nil || roots == nil {
		t.Fatal(roots, err)
	}
	if c.Seq != 1 || c.Snapshot != roots.Snapshot || c.Prev != "" || len(c.Digest) != 64 || c.ActorID != testActor {
		t.Fatalf("%+v", c)
	}
	h := histories(t, s)
	if chain := h.Chain(); len(chain) != 1 || chain[0] != c {
		t.Fatalf("the chain holds %+v, the reply was %+v", chain, c)
	}
	if got, state, _ := h.Find(c.Digest); state != consts.CheckpointFound || got != c {
		t.Fatalf("find: %s %+v", state, got)
	}
	held, err := h.Holds(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range held {
		if l.State != consts.HeldOK {
			t.Errorf("%+v", l)
		}
	}
	last := h.audit[c.AuditCount-1]
	if last.Kind != disk.AuditCheckpoint || last.Detail != fmt.Sprintf(consts.MsgCheckpointAnnounced, 1) {
		t.Fatalf("the checkpoint's last audit entry is %v %q: the engine's announcement changed", last.Kind, last.Detail)
	}
	if st := h.ChainState(); st.Count != 1 || st.ChainErr != "" {
		t.Fatalf("%+v", st)
	}
}

// 🔒 Writes after a seal are in no checkpoint: they do not move a capture, and are counted so the
// report can say so.
func TestLogWritesMoveNoHeadAndAreCounted(t *testing.T) {
	s, _ := sealedWithOne(t)
	c := publishOne(t, s)
	oneEvent(t, s, "b")
	h := histories(t, s)
	if h.Current() != c.Heads {
		t.Fatalf("now %+v, at the checkpoint %+v", h.Current(), c.Heads)
	}
	if h.UnsealedRecords() == 0 {
		t.Fatal("the write after the seal was not counted")
	}
}

// rohy's own audit entries move the audit head and nothing else: what the report reads as "only
// audit entries since".
func TestAnAuditEntryMovesOnlyTheAuditHead(t *testing.T) {
	s, _ := sealedWithOne(t)
	c := publishOne(t, s)
	if err := s.RecordAction(consts.AuditActionIngest, "ingest finished"); err != nil {
		t.Fatal(err)
	}
	now := histories(t, s).Current()
	if now.AuditCount != c.AuditCount+1 || now.AuditHead == c.AuditHead {
		t.Fatalf("audit: %d %s", now.AuditCount, now.AuditHead)
	}
	now.AuditCount, now.AuditHead = c.AuditCount, c.AuditHead
	if now != c.Heads {
		t.Fatalf("something besides the audit log moved: %+v", now)
	}
}

// 🔒 A torn tail is kept aside before the next checkpoint, or that checkpoint would be read back out
// of alignment and its digest never found.
func TestATornChainIsKeptAsideBeforeTheNextCheckpoint(t *testing.T) {
	s, dir := sealedWithOne(t)
	publishOne(t, s)
	path := filepath.Join(dir, consts.CheckpointFile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{9, 9, 9}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	aside, next, err := s.ReadyCheckpointChain(time.Now())
	if err != nil || next != 2 {
		t.Fatal(next, err)
	}
	if b, err := os.ReadFile(aside); err != nil || !bytes.Equal(b, []byte{9, 9, 9}) {
		t.Fatalf("the tail kept aside: %v %v", b, err)
	}
	c, err := s.PublishCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	h := histories(t, s)
	if _, state, _ := h.Find(c.Digest); state != consts.CheckpointFound || h.ChainState().ChainErr != "" {
		t.Fatalf("after the keep-aside: %s %+v", state, h.ChainState())
	}
	if chain, _ := disk.ReadCheckpoints(dir); len(chain) != 2 {
		t.Fatalf("%d checkpoints", len(chain))
	}
}

// Pins what the keep-aside guards against: the engine appends after a torn tail, and the checkpoint
// it just recorded is then lost to every reader.
func TestTheEngineLosesACheckpointAppendedAfterATornTail(t *testing.T) {
	s, dir := sealedWithOne(t)
	publishOne(t, s)
	f, err := os.OpenFile(filepath.Join(dir, consts.CheckpointFile), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{9, 9, 9}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	c, err := s.PublishCheckpoint() // not readied
	if err != nil {
		t.Fatal(err)
	}
	if _, state, _ := histories(t, s).Find(c.Digest); state == consts.CheckpointFound {
		t.Fatal("the engine now reads past a torn tail: the keep-aside may be unnecessary")
	}
}

func TestACheckpointIsReadBackByNumberOnlyFromAChainThatChecks(t *testing.T) {
	s, dir := sealedWithOne(t)
	c := publishOne(t, s)
	if got, err := s.CheckpointAt(1); err != nil || got != c {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.CheckpointAt(2); !errors.Is(err, ErrNoCheckpoint) {
		t.Fatalf("%v", err)
	}
	path := filepath.Join(dir, consts.CheckpointFile)
	b, _ := os.ReadFile(path)
	b[24] ^= 0xFF
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckpointAt(1); !errors.Is(err, ErrCheckpointChainBroken) {
		t.Fatalf("%v", err)
	}
}

func TestABrokenChainIsNotAddedTo(t *testing.T) {
	s, dir := sealedWithOne(t)
	publishOne(t, s)
	path := filepath.Join(dir, consts.CheckpointFile)
	b, _ := os.ReadFile(path)
	b[30] ^= 0xFF // inside the image root
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadyCheckpointChain(time.Now()); !errors.Is(err, ErrCheckpointChainBroken) {
		t.Fatalf("err = %v", err)
	}
}

// 🔒 A record whose image root was edited under its original digest is not trusted for being found:
// the digest is recomputed first.
func TestAnEditedRootUnderItsOwnDigestIsNotTrusted(t *testing.T) {
	s, dir := sealedWithOne(t)
	c := publishOne(t, s)
	path := filepath.Join(dir, consts.CheckpointFile)
	b, _ := os.ReadFile(path)
	b[24] ^= 0xFF // the first byte of the image root; the stored digest is untouched
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, state, detail := histories(t, s).Find(c.Digest)
	if state != consts.CheckpointChainBroken || detail == "" {
		t.Fatalf("%s %q", state, detail)
	}
	if got.Snapshot == c.Snapshot {
		t.Fatal("the edit did not reach the root")
	}
	if _, state, _ := histories(t, s).Find(strings.Repeat("ab", 32)); state != consts.CheckpointNotFound {
		t.Fatalf("an unknown digest: %s", state)
	}
}

// Each history is checked on its own against what the checkpoint covered: grown is held, an edited
// entry is changed, a removed file is shrank.
func TestEachHistoryIsCheckedAgainstTheCheckpoint(t *testing.T) {
	dir := t.TempDir()
	s := OpenAwaitingSession(dir)
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned, Actor: testActor}); err != nil {
		t.Fatal(err)
	}
	a, b := oneEvent(t, s, "a"), oneEvent(t, s, "b")
	if err := s.RedactEventRecord(a, "hn-a", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	c := publishOne(t, s)
	if c.RedactionCount != 1 || c.AuditCount == 0 {
		t.Fatalf("%+v", c.Heads)
	}
	if err := s.RedactEventRecord(b, "hn-b", "second"); err != nil {
		t.Fatal(err)
	}
	held, _ := histories(t, s).Holds(1)
	if st := heldStates(held); st[consts.AnchorLayerLedger] != consts.HeldOK || st[consts.AnchorLayerAudit] != consts.HeldOK {
		t.Fatalf("after more history: %v", st)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// An entry the checkpoint covered is edited: its own announcement.
	auditPath := filepath.Join(dir, consts.AuditFile)
	ab, _ := os.ReadFile(auditPath)
	i := bytes.Index(ab, fmt.Appendf(nil, consts.MsgCheckpointAnnounced, 1))
	if i < 0 {
		t.Fatal("announcement not found")
	}
	ab[i] = 'P'
	if err := os.WriteFile(auditPath, ab, 0o600); err != nil {
		t.Fatal(err)
	}
	// The removal ledger is removed.
	if err := os.Remove(filepath.Join(dir, consts.RedactionLedgerFile)); err != nil {
		t.Fatal(err)
	}

	r := OpenLazy(dir)
	t.Cleanup(func() { r.Close() })
	h, err := r.Histories()
	if err != nil {
		t.Fatal(err)
	}
	held, _ = h.Holds(1)
	st := heldStates(held)
	if st[consts.AnchorLayerAudit] != consts.HeldChanged || st[consts.AnchorLayerLedger] != consts.HeldShrank ||
		st[consts.AnchorLayerGrants] != consts.HeldOK || st[consts.AnchorLayerSegments] != consts.HeldOK {
		t.Fatalf("%v", st)
	}
}

// 🔒 The engine's announcement outlives the chain file, so a deleted chain is found. A publication
// rohy recorded as not recorded is not.
func TestAChainRemovedAfterItsAnnouncementIsFound(t *testing.T) {
	s, dir := sealedWithOne(t)
	publishOne(t, s)
	if got := histories(t, s).Unrecorded(); len(got) != 0 {
		t.Fatalf("with the chain in place: %v", got)
	}
	if err := os.Remove(filepath.Join(dir, consts.CheckpointFile)); err != nil {
		t.Fatal(err)
	}
	h := histories(t, s)
	if got := h.Unrecorded(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("unrecorded = %v", got)
	}
	if h.ChainState().Count != 0 {
		t.Fatal("the chain is still there")
	}
	if err := s.RecordAction(consts.AuditActionCheckpointNotRecorded, fmt.Sprintf(consts.MsgCheckpointNotRecorded, 1)); err != nil {
		t.Fatal(err)
	}
	if got := histories(t, s).Unrecorded(); len(got) != 0 {
		t.Fatalf("after the not-recorded entry: %v", got)
	}
}

func TestOnlyAnExactAnnouncementIsParsed(t *testing.T) {
	for detail, want := range map[string]bool{
		"publishing checkpoint 3":       true,
		"publishing checkpoint 3 again": false,
		"publishing checkpoint x":       false,
		"publishing checkpoint 03":      false,
	} {
		if _, ok := seqIn(consts.MsgCheckpointAnnounced, detail); ok != want {
			t.Errorf("%q: %v", detail, ok)
		}
	}
}

// 🔒 The hand-off claims no witness: no time, no reference, and nothing to read back.
func TestTheHandOffClaimsNoWitness(t *testing.T) {
	d := merkle.Hash{1, 2, 3}
	rec, err := handoff{}.Publish(d)
	if err != nil || rec.Digest != d || rec.UnixNano != 0 || rec.Ref != "" {
		t.Fatalf("%+v %v", rec, err)
	}
	if recs, err := (handoff{}).Records(); err == nil || recs != nil {
		t.Fatal("the hand-off answered as an anchor would")
	}
}

func TestASessionThatCannotWritePublishesNothing(t *testing.T) {
	p, _ := signedPosture(t)
	s := OpenAwaitingSession(t.TempDir())
	t.Cleanup(func() { s.Close() })
	if err := s.BeginSession(p); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadyCheckpointChain(time.Now()); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("ready: %v", err)
	}
	if _, err := s.PublishCheckpoint(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("publish: %v", err)
	}
	if _, err := s.Histories(); err != nil {
		t.Fatalf("reading is not writing: %v", err)
	}
}
