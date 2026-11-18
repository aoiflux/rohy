package api

import (
	"errors"
	"fmt"
	"sync"

	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/evtx"
	"rohy/backend/graphene"
	"rohy/backend/redaction"
	"rohy/backend/version"
)

// Sealing a case: one place for every seal, so every seal is recorded.
//
// A case is sealed after an ingest or a correlation backfill, from the Maintenance page, and on
// close. The seals used to call Compact each on its own. Only the fingerprint export recorded
// anything, so the history held the fingerprints somebody happened to export rather than the case's
// actual sequence of images, and the chain it exists to check could not be checked.
//
// 🔒 A compaction ALWAYS makes a new fingerprint. The new image names the old one as its
// predecessor and that link is bound into the root, so even a compaction that changed nothing moves
// the value an analyst exported. The automatic seals therefore skip an unchanged case; the explicit
// one does not, because it is also how an image in an older format gets rewritten.

// SealOutcome is what a seal did. It is the payload of case:sealed.
type SealOutcome struct {
	// State is one of consts.SealState*.
	State string `json:"state"`
	// Snapshot is the case fingerprint after the seal, when there is one.
	Snapshot string `json:"snapshot,omitempty"`
	// Signed is set when the seal that ran carries the analyst's signature.
	Signed bool `json:"signed,omitempty"`
}

// sealMu serialises every seal in the process, from check to record.
//
// 🔒 The seal points run on different goroutines — the ingest's, a Maintenance pass or click, app
// shutdown — and the maintenance lock does not exclude an ingest. Interleaved, two seals can each
// compact and then both read the second image's fingerprint, so the first image is never recorded
// and a clean case reports a gap in its history. Compaction is already exclusive inside the engine;
// this extends that to the record, which is what makes the history the sequence of seals.
var sealMu sync.Mutex

// sealAndRecord seals the case if it needs it (always, when forced) and records the fingerprint.
//
// The two halves fail differently and are worded differently. A compaction that failed is a failed
// seal; a fingerprint that could not be recorded after a good compaction is a sealed case with a gap
// in its history, and reporting it as a failed seal would invite somebody to seal again.
func sealAndRecord(store *graphene.Store, hist *custody.Store, force bool) (SealOutcome, error) {
	if store == nil {
		return SealOutcome{State: consts.SealStateUnavailable}, nil
	}
	sealMu.Lock()
	defer sealMu.Unlock()
	return sealAndRecordLocked(store, hist, force)
}

// sealAndRecordLocked is sealAndRecord for a caller that already holds sealMu: a checkpoint seals
// first and publishes under the same hold, so no other seal lands between the two.
func sealAndRecordLocked(store *graphene.Store, hist *custody.Store, force bool) (SealOutcome, error) {
	if store == nil {
		return SealOutcome{State: consts.SealStateUnavailable}, nil
	}
	if !force {
		unsealed, err := store.Unsealed()
		if errors.Is(err, graphene.ErrForensicsUnavailable) {
			return SealOutcome{State: consts.SealStateUnavailable}, nil
		}
		// Any other error comes back with unsealed == true: a seal that cannot tell, seals.
		if err == nil && !unsealed {
			return recordSeal(store, hist, consts.SealStateUnchanged)
		}
	}
	if err := store.Compact(); err != nil {
		// A compaction can fail after its commit point: the engine records the seal in the audit log
		// after the new image is in place. Then the image on disk is not the one this session holds,
		// and writing on would build on a copy that no longer exists.
		if same, cerr := store.ImageMatchesSession(); cerr == nil && !same {
			store.Poison()
			return SealOutcome{State: consts.SealStateFailed}, fmt.Errorf(consts.MsgSealPoisoned, err)
		}
		return SealOutcome{State: consts.SealStateFailed}, fmt.Errorf(consts.MsgSealCompactFailed, err)
	}
	out, err := recordSeal(store, hist, consts.SealStateSealed)
	if err != nil {
		return out, err
	}
	// A signed seal is recorded where the case's signing is checked from: the register and the
	// analyst's profile keep the id of the last signed seal, which the next one must name.
	if sealObserver != nil {
		sig, serr := store.SealSignature(nil)
		if serr != nil {
			return out, fmt.Errorf(consts.MsgSealRecordFailed, serr)
		}
		if sig != nil {
			out.Signed = true
			if err := sealObserver(sig); err != nil {
				return out, fmt.Errorf(consts.MsgSealRecordFailed, err)
			}
		}
	}
	return out, nil
}

// sealObserver is told about each seal that ran and carries a signature. The session sets it
// (api_session.go); it is read and set only under sealMu.
var sealObserver func(*graphene.SealSignature) error

// recordSeal reads the fingerprint the case now has and adds it to the history.
//
// A seal that RAN is always recorded: it happened, and leaving it out would make the history
// something other than the sequence of seals. When a rollback was sealed again, the record that
// names an older seal as its predecessor is what keeps the rollback provable (custody.Chain).
//
// 🔒 A seal that did NOT run ("unchanged") records the image only where that agrees with the
// history: a history that has nothing yet, the image already at its head, or an image one
// unrecorded seal past it. That is how a case last sealed by a build that kept no history gets its
// fingerprint into one. An image the history does not lead to — rolled back, or from somewhere else
// — is left unrecorded, or simply closing the case would make it the newest seal and erase the
// finding with no seal having run at all.
func recordSeal(store *graphene.Store, hist *custody.Store, state string) (SealOutcome, error) {
	roots, err := store.SnapshotRoots()
	if errors.Is(err, graphene.ErrForensicsUnavailable) {
		return SealOutcome{State: consts.SealStateUnavailable}, nil
	}
	if err != nil {
		return SealOutcome{State: state}, fmt.Errorf(consts.MsgSealRecordFailed, err)
	}
	if roots == nil {
		return SealOutcome{State: state}, nil
	}
	out := SealOutcome{State: state, Snapshot: roots.Snapshot}
	if hist == nil {
		return out, nil
	}
	if state == consts.SealStateUnchanged {
		switch hist.Chain(roots).State {
		case consts.ChainEmpty, consts.ChainIntact, consts.ChainUnrecordedSeal:
		default:
			return out, nil
		}
	}
	if _, err := hist.Observe(fingerprintRecord(store, roots)); err != nil {
		return out, fmt.Errorf(consts.MsgSealRecordFailed, err)
	}
	return out, nil
}

// fingerprintRecord is the history entry for the case's current fingerprint. One builder, so a
// fingerprint recorded at a seal and the same one recorded at an export are the same record.
func fingerprintRecord(store *graphene.Store, roots *graphene.SnapshotRoots) custody.Record {
	rec := custody.Record{
		Snapshot: roots.Snapshot, Prev: roots.Prev,
		NodeRoot: roots.NodeRoot, EdgeRoot: roots.EdgeRoot, IndexRoot: roots.IndexRoot,
		TombstoneRoot: roots.TombstoneRoot, BodyVersion: roots.BodyVersion,
		// Current, not the raw stamp, so an unstamped build records "unreleased" rather than
		// nothing — the same value every other surface shows.
		StoreVersion: store.Version(), AppVersion: version.Current(consts.AppName).Version,
	}
	if nodes, _, err := store.Stats(); err == nil {
		rec.Events = int(nodes)
	}
	// Who sealed, and the signature the image carries. The signature is the one on the
	// image this session holds, which is the image whose roots these are.
	if p, ok := store.Session(); ok {
		rec.ActorID = p.Actor
	}
	if sig, err := store.SealSignature(nil); err == nil && sig != nil {
		rec.KeyID, rec.Attestation = sig.KeyID, sig.ID
	}
	return rec
}

// SealOnClose is the seal app shutdown runs: skipped when nothing changed, recorded when it ran.
func SealOnClose(store *graphene.Store, hist *custody.Store) error {
	_, err := sealAndRecord(store, hist, false)
	return err
}

// AttachSealHistory gives the ingest binding the fingerprint history its seals are recorded in.
//
// A package function rather than a method on purpose: Wails exposes every exported method of a
// bound struct to the frontend, and this is wiring, not an action the UI should be able to call.
func AttachSealHistory(e *EventsAPI, hist *custody.Store) *EventsAPI {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sealHistory = hist
	return e
}

// AttachRedactionJournal gives the ingest binding the journal whose entries it must not ingest
// again. A package function for the reason AttachSealHistory is one.
func AttachRedactionJournal(e *EventsAPI, j *redaction.Journal) *EventsAPI {
	e.mu.Lock()
	defer e.mu.Unlock()
	// 🔒 Never a nil *Journal inside the interface: it would read as a list to consult and panic.
	if j != nil {
		e.suppress = j
	}
	return e
}

// suppressor is the list the next ingest consults.
func (a *EventsAPI) suppressor() evtx.Suppressor {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.suppress
}
