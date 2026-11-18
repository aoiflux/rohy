package graphene

// Checkpoints: the one record whose digest leaves the case.
//
// A checkpoint binds the head of every history the case keeps into one digest: the image, its
// attestation, the audit log, the removal ledger, the grant ledger. The analyst publishes the digest
// somewhere the case's rewriter cannot reach, and records the receipt they get back. A rewritten
// history then produces digests nobody outside ever saw.
//
// The engine publishes through an Anchor it is given, and ships none by design. rohy's is the
// hand-off below, which publishes nothing: the digest leaves the process in rohy's reply to the
// analyst, and only the analyst can take it further.
//
// This file reads and records. What a state means, and how it is worded, is decided in custody and
// caseintegrity.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/merkle"
	"github.com/aoiflux/graphene/store"
)

// ErrCheckpointChainBroken refuses to add a checkpoint to a chain that does not hold together: the
// new one would link to a predecessor nobody can vouch for.
var ErrCheckpointChainBroken = errors.New("checkpoint chain broken")

// Heads is where every history the case keeps stands: at a checkpoint, or now. A history that was
// never started has an empty head and a count of 0.
type Heads struct {
	Snapshot       string `json:"snapshot,omitempty"`
	Attestation    string `json:"attestation,omitempty"`
	SegmentHead    string `json:"segment_head,omitempty"`
	AuditHead      string `json:"audit_head,omitempty"`
	RedactionHead  string `json:"redaction_head,omitempty"`
	GrantHead      string `json:"grant_head,omitempty"`
	SegmentCount   uint64 `json:"segment_count"`
	AuditCount     uint64 `json:"audit_count"`
	RedactionCount uint64 `json:"redaction_count"`
	GrantCount     uint64 `json:"grant_count"`
}

// Checkpoint is one checkpoint of the case's chain, in hex.
type Checkpoint struct {
	Seq uint64 `json:"seq"`
	// CapturedAt is the case's own clock. When the digest was published is the anchor's to say.
	CapturedAt time.Time `json:"captured_at"`
	ActorID    uint64    `json:"actor_id"`
	Heads
	Prev   string `json:"prev,omitempty"`
	Digest string `json:"digest"`
}

func checkpointOf(c disk.Checkpoint) Checkpoint {
	return Checkpoint{
		Seq: c.Seq, CapturedAt: time.Unix(0, c.UnixNano).UTC(), ActorID: c.ActorID,
		Heads: Heads{
			Snapshot: hexOrEmpty(c.SnapshotRoot[:]), Attestation: hexOrEmpty(c.AttestationID[:]),
			SegmentHead: hexOrEmpty(c.SegmentHead[:]), AuditHead: hexOrEmpty(c.AuditHead[:]),
			RedactionHead: hexOrEmpty(c.RedactionHead[:]), GrantHead: hexOrEmpty(c.GrantHead[:]),
			SegmentCount: c.SegmentCount, AuditCount: c.AuditCount,
			RedactionCount: c.RedactionCount, GrantCount: c.GrantCount,
		},
		Prev: hexOrEmpty(c.Prev[:]), Digest: hex.EncodeToString(c.Digest[:]),
	}
}

// hexOrEmpty is a head in hex, or empty for one never started (all zero).
func hexOrEmpty(b []byte) string {
	for _, x := range b {
		if x != 0 {
			return hex.EncodeToString(b)
		}
	}
	return ""
}

// handoff is rohy's Anchor. It publishes nothing and witnesses nothing.
//
// 🔒 Its record carries no time and no reference, so nothing can read it as a receipt. The
// checkpoint it lets the engine record is "recorded, no receipt" until the analyst records one.
// Records refuses rather than returning nothing: an empty answer would make the engine's
// VerifyAgainstAnchor call every checkpoint unpublished.
type handoff struct{}

func (handoff) Publish(d merkle.Hash) (disk.AnchorRecord, error) {
	return disk.AnchorRecord{Digest: d}, nil
}

func (handoff) Records() ([]disk.AnchorRecord, error) {
	return nil, errors.New(consts.MsgHandoffHoldsNothing)
}

// ReadyCheckpointChain makes the chain safe to add a checkpoint to. Call it under the lock every
// publication takes, just before PublishCheckpoint.
//
// 🔒 The engine appends after whatever the file holds, and reads it back in whole records from the
// start. A torn tail left there would shift the new checkpoint out of alignment, and the digest
// handed to the analyst would never be found in the case again. The tail is kept beside the file,
// never discarded, and its path returned. A chain that does not hold is refused.
//
// next is the sequence number the next checkpoint will carry.
func (s *Store) ReadyCheckpointChain(now time.Time) (aside string, next uint64, err error) {
	if err := s.Writable(); err != nil {
		return "", 0, err
	}
	if _, err := s.forensics(); err != nil {
		return "", 0, err
	}
	chain, err := disk.ReadCheckpoints(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", 1, nil
	}
	if err != nil {
		return "", 0, err
	}
	if err := disk.VerifyCheckpointChain(chain); err != nil {
		return "", 0, fmt.Errorf("%w: "+consts.MsgCheckpointChainBroken, ErrCheckpointChainBroken, err)
	}
	aside, _, err = keepCheckpointTailAside(s.dir, now)
	return aside, uint64(len(chain)) + 1, err
}

// ErrNoCheckpoint is a checkpoint number the chain does not hold.
var ErrNoCheckpoint = errors.New("no such checkpoint")

// CheckpointAt returns checkpoint seq, after checking the chain up to and including it. It reads
// only the chain.
func (s *Store) CheckpointAt(seq uint64) (Checkpoint, error) {
	if _, err := s.forensics(); err != nil {
		return Checkpoint{}, err
	}
	chain, err := disk.ReadCheckpoints(s.dir)
	if err != nil {
		return Checkpoint{}, err
	}
	if seq == 0 || seq > uint64(len(chain)) {
		return Checkpoint{}, fmt.Errorf("%w: %d of %d", ErrNoCheckpoint, seq, len(chain))
	}
	if err := disk.VerifyCheckpointChain(chain[:seq]); err != nil {
		return Checkpoint{}, fmt.Errorf("%w: "+consts.MsgCheckpointChainBroken, ErrCheckpointChainBroken, err)
	}
	return checkpointOf(chain[seq-1]), nil
}

// CheckpointChain reads the chain alone, for listing: the checkpoints as recorded, oldest first, and
// the whole chain's check. Unchecked records are only what they say; ChainErr says when that is all.
func (s *Store) CheckpointChain() ([]Checkpoint, CheckpointState, error) {
	if _, err := s.forensics(); err != nil {
		return nil, CheckpointState{}, err
	}
	chain, st, err := readCheckpointChain(s.dir)
	if err != nil {
		return nil, CheckpointState{}, err
	}
	out := make([]Checkpoint, len(chain))
	for i, c := range chain {
		out[i] = checkpointOf(c)
	}
	return out, st, nil
}

// UnsealedRecords is how many records the log holds that no seal covers yet.
func (s *Store) UnsealedRecords() (int, error) {
	if _, err := s.forensics(); err != nil {
		return 0, err
	}
	log, err := InspectLog(s.dir)
	return log.Records, err
}

// PublishCheckpoint records a checkpoint of the case as it stands and returns it, for the analyst
// to publish. The caller holds sealMu, so no seal lands inside it, and has readied the chain.
//
// The engine writes its own audit entry first (MsgCheckpointAnnounced), so the checkpoint covers
// the record of its own publication. 🔒 On any error no checkpoint is returned, even one the
// engine captured: a digest handed out that the case does not hold is one nobody can check.
func (s *Store) PublishCheckpoint() (Checkpoint, error) {
	if err := s.Writable(); err != nil {
		return Checkpoint{}, err
	}
	fs, err := s.forensics()
	if err != nil {
		return Checkpoint{}, err
	}
	c, _, err := fs.PublishCheckpoint(handoff{})
	if err != nil {
		return Checkpoint{}, err
	}
	return checkpointOf(c), nil
}

// Histories is every history a checkpoint commits to, each read once. The caller holds sealMu, so
// the image and the histories describe one moment; writes outside a seal may still land.
//
// A history that could not be read in full keeps what was read before the fault, and the fault is
// in Unread. A check that needs only that prefix still runs.
type Histories struct {
	chain      []disk.Checkpoint
	chainState CheckpointState
	audit      []disk.AuditEntry
	ledger     []disk.RedactionRecord
	grants     []disk.RoleGrant
	segments   []disk.SegmentInfo
	snapshot   merkle.Hash
	attest     string
	unsealed   int
	unread     map[string]error
}

// Histories reads the case's histories for the checkpoint checks.
func (s *Store) Histories() (*Histories, error) {
	fs, err := s.forensics()
	if err != nil {
		return nil, err
	}
	h := &Histories{unread: map[string]error{}}
	if h.chain, h.chainState, err = readCheckpointChain(s.dir); err != nil {
		h.unread[consts.AnchorLayerChain] = err
	}
	if h.audit, err = disk.ReadAuditLog(s.dir); err != nil {
		h.unread[consts.AnchorLayerAudit] = err
	}
	if h.ledger, err = disk.ReadRedactions(s.dir); err != nil {
		h.unread[consts.AnchorLayerLedger] = err
	}
	if h.grants, err = disk.ReadGrants(s.dir); err != nil {
		h.unread[consts.AnchorLayerGrants] = err
	}
	if h.segments, err = disk.ListSegments(s.dir); err != nil {
		h.unread[consts.AnchorLayerSegments] = err
	}
	switch roots, err := fs.SnapshotRoots(); {
	case err == nil:
		h.snapshot = roots.Snapshot
	case !errors.Is(err, disk.ErrNoSnapshotRoots):
		h.unread[consts.AnchorLayerImage] = err
	}
	switch att, err := fs.SnapshotAttestation(); {
	case err == nil:
		h.attest = hexOrEmpty(att.ID[:])
	case !errors.Is(err, disk.ErrNoAttestation) && !errors.Is(err, disk.ErrNoSnapshotRoots):
		h.unread[consts.AnchorLayerImage] = err
	}
	if log, err := InspectLog(s.dir); err != nil {
		h.unread[consts.AnchorLayerLog] = err
	} else {
		h.unsealed = log.Records
	}
	return h, nil
}

// Chain is the case's checkpoint chain as recorded, oldest first. Unchecked: a record's digest is
// only what it says until ChainState or Find has checked it.
func (h *Histories) Chain() []Checkpoint {
	out := make([]Checkpoint, len(h.chain))
	for i, c := range h.chain {
		out[i] = checkpointOf(c)
	}
	return out
}

// ChainState is the chain's count, newest capture and whole-chain check.
func (h *Histories) ChainState() CheckpointState { return h.chainState }

// Unread names each history that could not be read in full, with why.
func (h *Histories) Unread() map[string]string {
	out := make(map[string]string, len(h.unread))
	for layer, err := range h.unread {
		out[layer] = err.Error()
	}
	return out
}

// UnsealedRecords is how many records the log holds that no seal has folded into the image yet.
// No checkpoint covers them.
func (h *Histories) UnsealedRecords() int { return h.unsealed }

// Current is where every history stands now, as read. A history in Unread is shown as far as it was
// read, which is not where it stands.
func (h *Histories) Current() Heads {
	out := Heads{
		Snapshot: hashHex(h.snapshot), Attestation: h.attest,
		SegmentCount: uint64(len(h.segments)), AuditCount: uint64(len(h.audit)),
		RedactionCount: uint64(len(h.ledger)), GrantCount: uint64(len(h.grants)),
	}
	if n := len(h.segments); n > 0 {
		out.SegmentHead = hexOrEmpty(h.segments[n-1].Digest[:])
	}
	if n := len(h.audit); n > 0 {
		out.AuditHead = hexOrEmpty(h.audit[n-1].Hash[:])
	}
	if n := len(h.ledger); n > 0 {
		out.RedactionHead = hexOrEmpty(h.ledger[n-1].Hash[:])
	}
	if n := len(h.grants); n > 0 {
		out.GrantHead = hexOrEmpty(h.grants[n-1].Hash[:])
	}
	return out
}

// Find returns the checkpoint whose digest is digest (lowercase hex), with one of
// consts.Checkpoint*.
//
// 🔒 The chain is checked up to and including it before it is returned: a record's stored digest
// is only a claim until it is recomputed, and an edited image root under the original digest would
// otherwise steer every check that follows.
func (h *Histories) Find(digest string) (Checkpoint, string, string) {
	for i, c := range h.chain {
		if hex.EncodeToString(c.Digest[:]) != digest {
			continue
		}
		if err := disk.VerifyCheckpointChain(h.chain[:i+1]); err != nil {
			return checkpointOf(c), consts.CheckpointChainBroken, err.Error()
		}
		return checkpointOf(c), consts.CheckpointFound, ""
	}
	if err, ok := h.unread[consts.AnchorLayerChain]; ok {
		return Checkpoint{}, consts.CheckpointUnread, err.Error()
	}
	return Checkpoint{}, consts.CheckpointNotFound, ""
}

// LayerHeld is whether one history still holds what a checkpoint committed to.
type LayerHeld struct {
	Layer string `json:"layer"`
	// State is one of consts.Held*.
	State string `json:"state"`
	// At is how many entries the checkpoint covered, Now how many were read now.
	At     uint64 `json:"at"`
	Now    uint64 `json:"now"`
	Detail string `json:"detail,omitempty"`
}

// Holds checks every history against checkpoint seq: each must still hold, unchanged, the entries
// the checkpoint covered. The caller has checked the chain up to seq (Find, or ChainState).
//
// 🔒 What a held history is worth depends on where the checkpoint came from. Against a digest kept
// outside the case it is evidence: the digest pins the head, and the head pins everything before it.
// Against the case's own chain it catches only a careless edit, since a rewriter can recompute both.
func (h *Histories) Holds(seq uint64) ([]LayerHeld, error) {
	if seq == 0 || seq > uint64(len(h.chain)) {
		return nil, fmt.Errorf("no checkpoint %d in a chain of %d", seq, len(h.chain))
	}
	k := h.chain[seq-1]
	return []LayerHeld{
		h.holds(consts.AnchorLayerAudit, len(h.audit), k.AuditCount, k.AuditHead,
			func(n int) error { return disk.VerifyAuditChain(h.audit[:n]) },
			func(i int) [32]byte { return h.audit[i].Hash }),
		h.holds(consts.AnchorLayerLedger, len(h.ledger), k.RedactionCount, k.RedactionHead,
			func(n int) error { return disk.VerifyRedactionChain(h.ledger[:n], nil) },
			func(i int) [32]byte { return h.ledger[i].Hash }),
		h.holds(consts.AnchorLayerGrants, len(h.grants), k.GrantCount, k.GrantHead,
			func(n int) error { return disk.VerifyGrantChain(h.grants[:n], nil) },
			func(i int) [32]byte { return h.grants[i].Hash }),
		// Retired segments carry no chain of their own here; their digests are compared in place.
		// Retention is off in rohy, so a checkpoint it made covers none.
		h.holds(consts.AnchorLayerSegments, len(h.segments), k.SegmentCount, k.SegmentHead,
			func(int) error { return nil },
			func(i int) [32]byte { return h.segments[i].Digest }),
	}, nil
}

func (h *Histories) holds(layer string, read int, at uint64, head [32]byte, verify func(int) error, hashAt func(int) [32]byte) LayerHeld {
	out := LayerHeld{Layer: layer, At: at, Now: uint64(read)}
	readErr := h.unread[layer]
	switch {
	case uint64(read) < at && readErr != nil:
		out.State, out.Detail = consts.HeldUnread, readErr.Error()
	case uint64(read) < at:
		out.State = consts.HeldShrank
	case at == 0 && head != [32]byte{}:
		out.State, out.Detail = consts.HeldChanged, "the checkpoint names a head for a history it says was empty"
	case at == 0:
		out.State = consts.HeldOK
	default:
		if err := verify(int(at)); err != nil {
			out.State, out.Detail = consts.HeldChanged, err.Error()
		} else if hashAt(int(at)-1) != head {
			out.State, out.Detail = consts.HeldChanged, fmt.Sprintf("entry %d is not the one the checkpoint names", at)
		} else {
			out.State = consts.HeldOK
		}
	}
	return out
}

// Unrecorded lists the checkpoints the audit log says the engine began to publish that the chain
// does not hold, leaving out those rohy recorded as not recorded (MsgCheckpointNotRecorded).
//
// 🔒 The engine's announcement is an entry no caller can write (disk/store.go RecordAudit), and it
// outlives the chain file. Deleting the chain to make the case look as if it was never anchored
// leaves these behind. A rewriter who rewrites the whole audit log removes them too; that is what a
// digest kept outside the case is for.
func (h *Histories) Unrecorded() []uint64 {
	announced, failed := map[uint64]bool{}, map[uint64]bool{}
	notRecorded := disk.AuditCustom + disk.AuditKind(consts.AuditActionCheckpointNotRecorded)
	for _, e := range h.audit {
		switch e.Kind {
		case disk.AuditCheckpoint:
			if n, ok := seqIn(consts.MsgCheckpointAnnounced, e.Detail); ok {
				announced[n] = true
			}
		case notRecorded:
			if n, ok := seqIn(consts.MsgCheckpointNotRecorded, e.Detail); ok {
				failed[n] = true
			}
		}
	}
	var out []uint64
	for n := range announced {
		if n > uint64(len(h.chain)) && !failed[n] {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// seqIn reads the checkpoint number from detail, which must be exactly format with one %d.
func seqIn(format, detail string) (uint64, bool) {
	var n uint64
	if _, err := fmt.Sscanf(detail, format, &n); err != nil {
		return 0, false
	}
	return n, fmt.Sprintf(format, n) == detail
}

// LedgerState is Store.LedgerState over the ledger these histories read.
func (h *Histories) LedgerState(v store.Verifier) (LedgerState, error) {
	if err, ok := h.unread[consts.AnchorLayerLedger]; ok {
		return LedgerState{}, err
	}
	return ledgerStateOf(h.ledger, v), nil
}

// readCheckpointChain reads the chain in dir and checks it, reporting a torn tail.
func readCheckpointChain(dir string) ([]disk.Checkpoint, CheckpointState, error) {
	chain, err := disk.ReadCheckpoints(dir)
	if err != nil {
		return nil, CheckpointState{}, err
	}
	st := CheckpointState{Count: len(chain)}
	if n := len(chain); n > 0 {
		st.Last = time.Unix(0, chain[n-1].UnixNano).UTC()
	}
	if err := disk.VerifyCheckpointChain(chain); err != nil {
		st.ChainErr = err.Error()
	}
	fi, err := os.Stat(filepath.Join(dir, consts.CheckpointFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, CheckpointState{}, err
	case fi.Size()%checkpointRecordBytes != 0 && st.ChainErr == "":
		st.ChainErr = fmt.Sprintf(consts.MsgCheckpointTorn, fi.Size()%checkpointRecordBytes)
	}
	return chain, st, nil
}
