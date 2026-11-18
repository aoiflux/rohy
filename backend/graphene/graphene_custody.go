package graphene

// The engine's custody account for one event, in rohy's shape.
//
// This file only gathers. What the account means, and how it is worded, is decided in
// backend/custody, which composes it with what the engine cannot know: whether the event's record
// still matches the sealed one, whether its raw record is covered, and where the image sits in the
// case's history of seals.
//
// 🔒 graphene's anchored form (CustodyForAnchored) is deliberately not used. It compares the anchor
// only when the entity is in the snapshot, and an entity missing from it is exactly when a kept
// fingerprint matters most: keep the fingerprint of a seal that holds the event, roll the image back
// to one that does not, and the one value able to show the rollback is never compared. rohy compares
// the kept fingerprint against the case's current image itself, whatever state the event is in.

import (
	"errors"
	"slices"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/store"
)

// CustodyGap is one gap the engine reported. Layer is one of consts.CustodyLayer*; Detail is the
// engine's own text, written for developers, and is shown only as the cause of a broken layer.
type CustodyGap struct {
	Layer  string
	Fatal  bool
	Detail string
}

// CustodyRedaction is the ledger record for a removal, when one exists.
type CustodyRedaction struct {
	Scope   string // consts.RedactionScope*
	At      time.Time
	ActorID uint64
	Reason  string
	// Seq is the record's place in the ledger. Signed reports a signature on it, and SignatureValid
	// that it checked out under the verifier the report was assembled with.
	Seq            uint64
	KeyID          uint64
	Signed         bool
	SignatureValid bool
}

// EventCustody is what the engine and the event's own proof say about one event.
//
// Every field the report decides on is structured, so nothing downstream reads the engine's text to
// learn what happened.
type EventCustody struct {
	// Live reports whether the case holds the event now, and InSnapshot whether the sealed image
	// does. Deleted since the seal is !Live && InSnapshot: the last seal still holds it.
	Live       bool
	InSnapshot bool
	// HasFingerprint is false when the case has no snapshot roots: never sealed, or sealed by a
	// build before fingerprints. Snapshot is the root the event was proved into.
	HasFingerprint bool
	Snapshot       string

	Attested            bool
	AttestationVerified bool

	SegmentsChecked     int
	AuditEntriesWalked  int
	CompactionsRecorded int
	RedactionsWalked    int
	GrantsWalked        int

	Redacted        *CustodyRedaction
	RemovalProvable bool

	Gaps []CustodyGap

	// ProofState is ProveEvent's state when the event is live, empty otherwise; RawBound and
	// RawHashed are its. Reusing ProveEvent keeps the report and the proof export on one code path.
	ProofState string
	RawBound   bool
	RawHashed  bool
}

// betweenCustodyReads runs between the engine's account and the event's proof. A test hook: it is
// what lets the delete-in-between branch be exercised deterministically.
var betweenCustodyReads = func() {}

// EventCustody assembles the engine's account for one event with no verifier: signature-dependent
// layers are reported as unestablished rather than failing.
func (s *Store) EventCustody(id uint64) (EventCustody, error) {
	return s.EventCustodyUnder(id, nil)
}

// EventCustodyUnder assembles the engine's account for one event, checking signatures under v —
// the keys the case lists. v may be nil.
//
// 🔒 v is the case's own list of keys, so a signature that checks out under it says the image was
// signed by a key this case names. Whether that key is the analyst's is for whoever compares its
// fingerprint with one they kept.
func (s *Store) EventCustodyUnder(id uint64, v store.Verifier) (EventCustody, error) {
	fs, err := s.forensics()
	if err != nil {
		return EventCustody{}, err
	}
	rep, err := fs.CustodyFor(store.NodeID(id), v)
	if err != nil {
		return EventCustody{}, err
	}
	out := EventCustody{
		Live:                rep.Live,
		InSnapshot:          rep.InSnapshot,
		Attested:            rep.Attested,
		AttestationVerified: rep.AttestationVerified,
		SegmentsChecked:     rep.SegmentsChecked,
		AuditEntriesWalked:  rep.AuditEntriesWalked,
		CompactionsRecorded: rep.CompactionsRecorded,
		RedactionsWalked:    rep.RedactionsWalked,
		GrantsWalked:        rep.GrantsWalked,
		RemovalProvable:     rep.RemovalProvable,
	}
	if rep.InSnapshot {
		out.Snapshot = hashHex(rep.SnapshotRoot)
	}
	if r := rep.Redacted; r != nil {
		out.Redacted = &CustodyRedaction{
			Scope:   scopeName(r.Scope),
			At:      time.Unix(0, r.UnixNano).UTC(),
			ActorID: r.ActorID,
			Reason:  r.Reason,
			Seq:     r.Seq,
			KeyID:   r.KeyID,
			Signed:  len(r.Signature) > 0,
		}
		// A ledger record is signed over its hash (disk/redact.go). Checked here one record at a
		// time; the engine's chain check skips an unsigned record without saying so.
		if v != nil && out.Redacted.Signed {
			out.Redacted.SignatureValid = v.Verify(r.KeyID, r.Hash[:], r.Signature) == nil
		}
	}
	for _, g := range rep.Gaps {
		out.Gaps = append(out.Gaps, CustodyGap{Layer: string(g.Layer), Fatal: g.Fatal, Detail: g.Detail})
	}

	// Read the way SnapshotRoots reads it, so the report and the posture agree on whether the case
	// has a fingerprint at all.
	switch roots, err := fs.SnapshotRoots(); {
	case errors.Is(err, disk.ErrNoSnapshotRoots):
		// No fingerprint: never sealed, or sealed by a build before fingerprints.
	case err != nil:
		return EventCustody{}, err
	default:
		out.HasFingerprint = !roots.Zero()
	}

	if !out.Live {
		return out, nil
	}
	// 🔒 The engine already found the seal does not check out. The record in a damaged image may
	// not even decode, so asking for its proof would fail the whole report — and the broken seal
	// line, which is the answer, would never be shown. Nothing in the image can be compared.
	if slices.ContainsFunc(out.Gaps, func(g CustodyGap) bool { return g.Layer == consts.CustodyLayerSnapshot && g.Fatal }) {
		out.ProofState = consts.EventProofImageDamaged
		return out, nil
	}
	betweenCustodyReads()
	p, err := s.ProveEvent(id)
	var nf *store.ErrNotFound
	switch {
	case errors.As(err, &nf):
		// Deleted between the two reads. It is no longer live; say that, rather than fail.
		out.Live = false
		return out, nil
	case err != nil:
		return EventCustody{}, err
	}
	out.ProofState, out.RawBound, out.RawHashed = p.State, p.RawBound, p.RawHashed
	return out, nil
}

func scopeName(s disk.RedactionScope) string {
	switch s {
	case disk.ScopeNode:
		return consts.RedactionScopeNode
	case disk.ScopeProperties:
		return consts.RedactionScopeProperties
	case disk.ScopeEdge:
		return consts.RedactionScopeEdge
	default:
		return consts.RedactionScopeUnknown
	}
}
