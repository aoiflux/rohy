package graphene

// What the case's own histories say about who signed them, read for the integrity report (plan
// §14.8, detectors 16 and 17; item 43).
//
// This file only reads and checks. What a state means, and how it is worded, is decided in
// backend/caseintegrity.

import (
	"errors"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/signing"
	"github.com/aoiflux/graphene/store"
)

// LedgerEntry is one redaction-ledger record's signature, checked under the keys the case lists.
type LedgerEntry struct {
	Seq   uint64
	KeyID uint64
	// Signature is one of consts.LedgerSig*.
	Signature string
}

// LedgerState is the redaction ledger as read for the integrity report.
type LedgerState struct {
	// ChainErr is why the ledger's hash chain does not hold, checked without any key. Empty when it
	// holds.
	ChainErr string
	Entries  []LedgerEntry
}

// LedgerState reads the redaction ledger and checks each record's signature under v, the keys the
// case lists. A nil v means the case's keys could not be read: signatures are then left unchecked
// and say so, rather than being reported as failing.
//
// Each signature is checked on its own. The engine's chain check skips an unsigned record without
// saying so, and stops at the first key it does not know, where the report needs to count.
func (s *Store) LedgerState(v store.Verifier) (LedgerState, error) {
	fs, err := s.forensics()
	if err != nil {
		return LedgerState{}, err
	}
	recs, err := fs.Redactions()
	if err != nil {
		return LedgerState{}, err
	}
	return ledgerStateOf(recs, v), nil
}

func ledgerStateOf(recs []disk.RedactionRecord, v store.Verifier) LedgerState {
	st := LedgerState{Entries: make([]LedgerEntry, 0, len(recs))}
	if err := disk.VerifyRedactionChain(recs, nil); err != nil {
		st.ChainErr = err.Error()
	}
	for _, r := range recs {
		e := LedgerEntry{Seq: r.Seq, KeyID: r.KeyID, Signature: consts.LedgerSigNone}
		if len(r.Signature) > 0 {
			e.Signature = ledgerSignature(v, r)
		}
		st.Entries = append(st.Entries, e)
	}
	return st
}

// ledgerSignature judges one signed record. A record is signed over its hash (disk/redact.go).
func ledgerSignature(v store.Verifier, r disk.RedactionRecord) string {
	if v == nil {
		return consts.LedgerSigUnchecked
	}
	switch err := v.Verify(r.KeyID, r.Hash[:], r.Signature); {
	case err == nil:
		return consts.LedgerSigValid
	case errors.Is(err, signing.ErrUnknownKey):
		return consts.LedgerSigUnknownKey
	default:
		return consts.LedgerSigBad
	}
}

// CheckpointState is the case's local checkpoint chain: how many, the newest, and whether it holds.
type CheckpointState struct {
	Count int
	// Last is when the newest checkpoint says it was captured: the case's own clock, not an anchor's.
	Last time.Time
	// ChainErr is why the chain does not hold, checked without any key. Empty when it holds.
	ChainErr string
}

// checkpointRecordBytes is the size of one checkpoint in graphene.checkpoints (disk/anchor.go).
// Pinned by a test that publishes one, because the engine's constant is not exported.
const checkpointRecordBytes = 296

// CheckpointState reads the case's local checkpoint chain.
func (s *Store) CheckpointState() (CheckpointState, error) {
	if _, err := s.forensics(); err != nil {
		return CheckpointState{}, err
	}
	return InspectCheckpoints(s.dir)
}

// InspectCheckpoints reads the checkpoint chain in dir without opening the store.
//
// 🔒 The engine drops a torn tail without saying so. Reported here: a checkpoint published and then
// cut short on disk is exactly the record this chain exists to keep.
func InspectCheckpoints(dir string) (CheckpointState, error) {
	_, st, err := readCheckpointChain(dir)
	return st, err
}
