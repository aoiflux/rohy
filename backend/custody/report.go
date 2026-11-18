package custody

// The custody report for one event.
//
// 🔒 GAPS, NEVER A VERDICT. Each layer says what it established and what it did not, and the report
// never adds them up into "custody accounted for". Every check rohy can run compares the case
// against itself; the one layer that can compare it against something else, the fingerprint the
// analyst kept, says which of the two forms the report took.
//
// Pure: it decides and words, over values gathered by the API under the seal lock. That is what
// lets every state be tested against literals.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// ErrMalformedFingerprint is returned for a kept fingerprint that is not one hash in hex.
var ErrMalformedFingerprint = errors.New(consts.MsgCustodyKeptMalformed)

// Layer is one line of the report.
type Layer struct {
	// Layer is a consts.CustodyEventLayer*, a consts.CustodyLayer* for the case records, or an
	// engine layer this build does not know.
	Layer string `json:"layer"`
	// State is consts.CustodyEstablished, CustodyNotEstablished or CustodyBroken.
	State   string   `json:"state"`
	Message string   `json:"message"`
	Notes   []string `json:"notes,omitempty"`
	// Remedy is what would establish the layer, when rohy can offer one. 🔒 Never set while the
	// history is broken: a seal then would build on an image that is not the one rohy last sealed.
	Remedy string `json:"remedy,omitempty"`
	// Cause is the engine's own words for what broke, or for a gap this build has no words for.
	Cause string `json:"cause,omitempty"`
}

// Report is the custody account for one event.
type Report struct {
	Event uint64 `json:"event"`
	// Presence is a consts.CustodyPresence*: where the event is, before anything is checked.
	Presence        string `json:"presence"`
	PresenceMessage string `json:"presence_message"`
	// Form says what the report compared the case against (consts.CustodyForm*).
	Form string `json:"form"`
	// Snapshot is the case's current fingerprint when the report was made, empty when it has none.
	// A report describes the case as it was then; the UI drops it when a seal, an ingest or a
	// maintenance pass changes the case.
	Snapshot string `json:"snapshot,omitempty"`

	// Layers are about this event; CaseRecords are about the whole case and read the same for every
	// event, which is why they are kept apart rather than repeated among the event's own.
	Layers      []Layer `json:"layers"`
	CaseRecords []Layer `json:"case_records"`

	Broken         int `json:"broken"`
	NotEstablished int `json:"not_established"`

	Establishes      []string `json:"establishes"`
	DoesNotEstablish []string `json:"does_not_establish"`
}

// ReportInput is everything the report decides on, gathered under the seal lock.
type ReportInput struct {
	Event uint64
	// Unavailable: the backend keeps no image, so nothing else here was gathered.
	Unavailable bool
	Engine      graphene.EventCustody
	// Current is the case's image now, nil when it has no fingerprint.
	Current *graphene.SnapshotRoots
	Chain   ChainReport
	// NoHistory: no fingerprint history was available to consult.
	NoHistory bool
	// Exported: a copy of the current fingerprint has been exported.
	Exported bool
	// Kept is the fingerprint the analyst supplied, normalized; empty when none. KeptKnown says the
	// history records it, and KeptAncestor that the current image descends from it (DescendsFrom).
	Kept         string
	KeptKnown    bool
	KeptAncestor bool
	// KeptVia is set when the value kept was a checkpoint digest: Kept is then the seal the
	// checkpoint names, found through a chain that checks.
	KeptVia *KeptCheckpoint
	// Signing is what the case records about its signing. Nil leaves the signing layer
	// out, for a caller with no analyst register.
	Signing *SigningInput
}

// EventReport assembles the report.
func EventReport(in ReportInput) Report {
	r := Report{Event: in.Event, Form: consts.CustodyFormCaseOnly}
	if in.Kept != "" {
		r.Form = consts.CustodyFormKept
	}
	if in.Unavailable {
		// Nothing was compared, so the form must not say a supplied fingerprint was checked.
		r.Form = consts.CustodyFormNone
		r.Presence, r.PresenceMessage = consts.CustodyPresenceUnavailable, consts.MsgCustodyPresenceUnavailable
		r.Layers = []Layer{{Layer: consts.CustodyEventLayerSeal, State: consts.CustodyNotEstablished, Message: consts.MsgCustodyPresenceUnavailable}}
		r.DoesNotEstablish = []string{consts.MsgCustodyPresenceUnavailable}
		return r.counted()
	}
	if in.Current != nil {
		r.Snapshot = in.Current.Snapshot
	}

	e := in.Engine
	r.Presence, r.PresenceMessage = presence(e)
	r.Layers = append(r.Layers, sealLayer(e, r.Presence))
	if e.Live {
		r.Layers = append(r.Layers, recordLayer(e.ProofState), rawLayer(e.ProofState, e.RawBound, e.RawHashed))
	}
	if e.Redacted != nil {
		r.Layers = append(r.Layers, redactionLayer(e))
	}
	history := historyLayer(in)
	if history.State == consts.CustodyBroken {
		// 🔒 A broken history is never offered a seal as its remedy, on any layer.
		for i := range r.Layers {
			r.Layers[i].Remedy = ""
		}
		history.Notes = append(history.Notes, consts.MsgCustodyNoSealAdvice)
	}
	r.Layers = append(r.Layers, history)
	if in.Signing != nil {
		r.Layers = append(r.Layers, signingLayer(*in.Signing, e))
	}
	r.Layers = append(r.Layers, keptLayer(in))
	r.CaseRecords = caseRecords(e)

	r.Establishes = []string{}
	if stateOf(r.Layers, consts.CustodyEventLayerSeal) == consts.CustodyEstablished &&
		stateOf(r.Layers, consts.CustodyEventLayerRecord) == consts.CustodyEstablished {
		r.Establishes = append(r.Establishes, consts.CustodyEstablishesRecord)
		if stateOf(r.Layers, consts.CustodyEventLayerRaw) == consts.CustodyEstablished {
			r.Establishes = append(r.Establishes, consts.CustodyEstablishesRaw)
		}
	}
	if stateOf(r.Layers, consts.CustodyEventLayerKept) == consts.CustodyEstablished {
		r.Establishes = append(r.Establishes, consts.CustodyEstablishesKept)
	}
	if stateOf(r.Layers, consts.CustodyEventLayerSigning) == consts.CustodyEstablished {
		r.Establishes = append(r.Establishes, consts.CustodyEstablishesSigned)
	}
	if r.Form == consts.CustodyFormCaseOnly {
		r.DoesNotEstablish = append(r.DoesNotEstablish, consts.CustodyNotKept)
	}
	r.DoesNotEstablish = append(r.DoesNotEstablish,
		consts.CustodyNotHistory, consts.EventProofNotWho, consts.EventProofNotTrue,
		consts.EventProofNotParsed, consts.CustodyNotSourceFile)
	return r.counted()
}

func (r Report) counted() Report {
	for _, l := range append(slices.Clone(r.Layers), r.CaseRecords...) {
		switch l.State {
		case consts.CustodyBroken:
			r.Broken++
		case consts.CustodyNotEstablished:
			r.NotEstablished++
		}
	}
	return r
}

func stateOf(ls []Layer, name string) string {
	for _, l := range ls {
		if l.Layer == name {
			return l.State
		}
	}
	return ""
}

// presence places the event. 🔒 Absence is decided first: an id the case does not hold must never
// be described by the state of the seal, which would claim the event exists.
func presence(e graphene.EventCustody) (string, string) {
	switch {
	case !e.Live && e.Redacted != nil && e.Redacted.Scope == consts.RedactionScopeNode:
		return consts.CustodyPresenceRedacted, fmt.Sprintf(consts.MsgCustodyPresenceRedacted, stamp(e.Redacted.At), e.Redacted.Reason)
	case !e.Live && e.InSnapshot:
		return consts.CustodyPresenceRemovedSinceSeal, consts.MsgCustodyPresenceRemovedSinceSeal
	case !e.Live:
		return consts.CustodyPresenceNotInCase, consts.MsgCustodyPresenceNotInCase
	// Still in the case, its content removed by a recorded redaction. Decided before the seal's
	// state: an event that holds nothing must not be described as an event in the seal.
	case e.Redacted != nil && e.Redacted.Scope == consts.RedactionScopeProperties && e.ProofState == consts.EventProofRedacted:
		return consts.CustodyPresenceRedactedContent, fmt.Sprintf(consts.MsgCustodyPresenceRedactedContent, stamp(e.Redacted.At), e.Redacted.Reason)
	case e.InSnapshot:
		return consts.CustodyPresenceInSeal, consts.MsgCustodyPresenceInSeal
	case e.HasFingerprint:
		return consts.CustodyPresenceWrittenSinceSeal, consts.MsgCustodyPresenceWrittenSinceSeal
	default:
		return consts.CustodyPresenceNoFingerprint, consts.MsgCustodyPresenceNoFingerprint
	}
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func sealLayer(e graphene.EventCustody, presence string) Layer {
	l := Layer{Layer: consts.CustodyEventLayerSeal, State: consts.CustodyNotEstablished}
	if cause := causes(e.Gaps, consts.CustodyLayerSnapshot, true); cause != "" {
		l.State, l.Message, l.Cause = consts.CustodyBroken, consts.MsgCustodySealBroken, cause
		return l
	}
	switch presence {
	case consts.CustodyPresenceInSeal:
		l.State, l.Message = consts.CustodyEstablished, consts.MsgCustodySealIn
	case consts.CustodyPresenceRedactedContent:
		// Here a seal IS the fix: until one, the image keeps what the redaction removed.
		if e.RemovalProvable {
			l.State, l.Message = consts.CustodyEstablished, consts.MsgCustodySealRedacted
		} else {
			l.Message, l.Remedy = consts.MsgCustodySealStillHolds, consts.MsgCustodyRemedySealRedaction
		}
	case consts.CustodyPresenceWrittenSinceSeal:
		l.Message, l.Remedy = consts.MsgCustodySealWritten, consts.MsgCustodyRemedySeal
	case consts.CustodyPresenceNoFingerprint:
		l.Message, l.Remedy = consts.MsgCustodySealNone, consts.MsgCustodyRemedySeal
	default:
		// Removed or redacted. Whether the seal still holds it is the engine's answer, not the
		// presence's: a removal since the seal leaves the event in it.
		if e.InSnapshot {
			l.Message = consts.MsgCustodySealRemoved
		} else {
			l.Message = consts.MsgCustodySealAbsent
		}
	}
	return l
}

// recordLayer reads ProveEvent's state: is the event's stored record the sealed one.
func recordLayer(proof string) Layer {
	l := Layer{Layer: consts.CustodyEventLayerRecord, State: consts.CustodyNotEstablished}
	switch proof {
	// raw_mismatch is found only after the record was bound, so the record itself is sealed.
	case consts.EventProofOK, consts.EventProofTooLarge, consts.EventProofRawMismatch:
		l.State, l.Message = consts.CustodyEstablished, consts.MsgCustodyRecordSealed
	case consts.EventProofChangedSinceSeal:
		l.Message, l.Remedy = consts.MsgCustodyRecordChanged, consts.MsgCustodyRemedyReseal
	case consts.EventProofImageOutdated:
		l.Message, l.Remedy = consts.MsgCustodyRecordOutdated, consts.MsgCustodyRemedyReseal
	case consts.EventProofNotSealed, consts.EventProofWrittenSinceSeal:
		l.Message = consts.MsgCustodyRecordNotSealed
	case consts.EventProofRawUnreadable:
		l.Message = consts.MsgCustodyRecordNotChecked
	case consts.EventProofImageDamaged:
		l.Message = consts.MsgCustodyRecordDamaged
	case consts.EventProofRedacted:
		l.Message = consts.MsgCustodyRecordRedacted
	case consts.EventProofRawRedacted:
		l.Message = consts.MsgCustodyRecordRedactionPending
	case consts.EventProofRecordEmpty:
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodyRecordEmpty
	default:
		l.Message = fmt.Sprintf(consts.MsgCustodyUnknownState, proof)
	}
	return l
}

// rawLayer reads whether the seal covers the raw record in the payload store.
func rawLayer(proof string, bound, hashed bool) Layer {
	l := Layer{Layer: consts.CustodyEventLayerRaw, State: consts.CustodyNotEstablished}
	switch proof {
	case consts.EventProofOK, consts.EventProofTooLarge:
		switch {
		case bound:
			l.State, l.Message = consts.CustodyEstablished, consts.MsgCustodyRawBound
		case hashed:
			l.Message = consts.MsgCustodyRawMissing
		default:
			l.Message = consts.MsgCustodyRawNoHash
		}
	case consts.EventProofRawMismatch:
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodyRawMismatch
	case consts.EventProofRawUnreadable:
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodyRawUnreadable
	case consts.EventProofChangedSinceSeal, consts.EventProofImageOutdated, consts.EventProofImageDamaged,
		consts.EventProofNotSealed, consts.EventProofWrittenSinceSeal:
		l.Message = consts.MsgCustodyRawNotChecked
	case consts.EventProofRedacted:
		l.Message = consts.MsgCustodyRawRedacted
	case consts.EventProofRawRedacted:
		l.Message = consts.MsgCustodyRawRedactionPending
	case consts.EventProofRecordEmpty:
		l.Message = consts.MsgCustodyRawNoRecord
	default:
		l.Message = fmt.Sprintf(consts.MsgCustodyUnknownState, proof)
	}
	return l
}

func redactionLayer(e graphene.EventCustody) Layer {
	red := e.Redacted
	l := Layer{Layer: consts.CustodyEventLayerRedaction, State: consts.CustodyNotEstablished}
	switch red.Scope {
	case consts.RedactionScopeProperties:
		l.Message = fmt.Sprintf(consts.MsgCustodyRedactedProps, stamp(red.At), red.Reason)
	case consts.RedactionScopeNode:
		l.Message = fmt.Sprintf(consts.MsgCustodyRedactedNode, stamp(red.At), red.Reason)
	default:
		l.Message = fmt.Sprintf(consts.MsgCustodyRedactedOther, red.Scope, stamp(red.At), red.Reason)
	}
	// 🔒 A recorded removal of the content beside an event that holds a record: the redaction did
	// not finish, or something wrote the record back. Either way the ledger and the case disagree.
	if red.Scope == consts.RedactionScopeProperties && e.Live && e.ProofState != consts.EventProofRedacted {
		l.State = consts.CustodyBroken
		l.Notes = append(l.Notes, consts.MsgCustodyRedactionUndone)
	}
	if !e.RemovalProvable {
		l.Notes = append(l.Notes, consts.MsgCustodyRedactionUnbound)
	}
	if red.ActorID == 0 {
		l.Notes = append(l.Notes, consts.MsgCustodyRedactionNoActor)
	}
	return l
}

// historyLayer places the image in the case's history of seals, at caseintegrity's weights: a
// rollback or a fork is broken, and never offered a seal as its remedy.
func historyLayer(in ReportInput) Layer {
	c := in.Chain
	l := Layer{Layer: consts.CustodyEventLayerHistory, State: consts.CustodyNotEstablished}
	switch {
	case in.NoHistory:
		l.Message = consts.MsgCustodyHistoryNone
		return l
	case c.Held != nil:
		l.Message = fmt.Sprintf(consts.MsgCustodyChainHeld, c.Held.Reason)
		return l
	}
	switch c.State {
	case consts.ChainRolledBack:
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgCustodyHistoryRolledBack, c.Later)
	case consts.ChainIntact:
		l.State, l.Message = consts.CustodyEstablished, consts.MsgCustodyHistoryIntact
	case consts.ChainUnrecordedSeal:
		l.State, l.Message = consts.CustodyEstablished, consts.MsgCustodyHistoryUnrecorded
	case consts.ChainDiverged:
		l.Message = consts.MsgCustodyHistoryDiverged
	case consts.ChainEmpty:
		l.Message = consts.MsgCustodyHistoryEmpty
	case consts.ChainNoImage:
		l.Message = consts.MsgCustodyHistoryNoImage
	default:
		l.Message = fmt.Sprintf(consts.MsgCustodyUnknownState, c.State)
	}
	if c.Forks > 0 {
		// The history proves the fork however many seals follow it, so it outranks the state.
		l.State = consts.CustodyBroken
		l.Notes = append(l.Notes, fmt.Sprintf(consts.MsgCustodyHistoryForked, c.ForkSkipped))
	}
	if c.Gaps > 0 {
		l.Notes = append(l.Notes, fmt.Sprintf(consts.MsgCustodyHistoryGaps, c.Gaps))
	}
	for _, k := range c.Kept {
		l.Notes = append(l.Notes, fmt.Sprintf(consts.MsgCustodyHistoryReset, k.Reason, k.KeptAt))
	}
	if l.State == consts.CustodyEstablished && (c.Gaps > 0 || len(c.Kept) > 0) {
		l.State = consts.CustodyNotEstablished
	}
	return l
}

// KeptCheckpoint is a checkpoint digest given as the kept value, resolved to the seal it names.
type KeptCheckpoint struct {
	Seq uint64
	// Off says why the case no longer holds what the checkpoint committed to; empty when it does.
	Off string
	// Unread says why what the checkpoint committed to could not be read in full.
	Unread string
	// NoSeal: the checkpoint was made before the case was first sealed, so it names no seal.
	NoSeal bool
}

// keptLayer compares the value the analyst kept against the case's image NOW. A checkpoint digest
// is checked as the seal it names, once the case is shown to still hold what it committed to: the
// digest pins that seal, as the seal's own fingerprint would.
func keptLayer(in ReportInput) Layer {
	v := in.KeptVia
	if v == nil {
		return keptSealLayer(in)
	}
	l := Layer{Layer: consts.CustodyEventLayerKept, State: consts.CustodyNotEstablished}
	switch {
	case v.Off != "":
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgCustodyKeptCheckpointOff, v.Seq, v.Off)
		return l
	case v.Unread != "":
		l.Message = fmt.Sprintf(consts.MsgCustodyKeptCheckpointUnread, v.Seq, v.Unread)
		return l
	case v.NoSeal:
		l.Message = fmt.Sprintf(consts.MsgCustodyKeptCheckpointNoSeal, v.Seq)
		return l
	}
	l = keptSealLayer(in)
	l.Notes = append([]string{fmt.Sprintf(consts.MsgCustodyKeptViaCheckpoint, v.Seq)}, l.Notes...)
	return l
}

// keptSealLayer compares a kept seal fingerprint against the case's image NOW, whatever state the
// event is in (§15.6: an event missing from a rolled-back image is exactly when it matters).
func keptSealLayer(in ReportInput) Layer {
	l := Layer{Layer: consts.CustodyEventLayerKept, State: consts.CustodyNotEstablished}
	switch {
	case in.Kept == "":
		l.Message = consts.MsgCustodyKeptNone
		if in.Current != nil {
			if in.Exported {
				l.Notes = append(l.Notes, consts.MsgCustodyKeptNoneExported)
			} else {
				l.Notes = append(l.Notes, consts.MsgCustodyKeptNoneNever)
			}
		}
	case in.Current != nil && in.Kept == in.Current.Snapshot:
		l.State, l.Message = consts.CustodyEstablished, consts.MsgCustodyKeptMatch
		if !in.Exported {
			l.Notes = append(l.Notes, consts.MsgCustodyKeptMatchNotOut)
		}
		if !in.Engine.InSnapshot {
			l.Notes = append(l.Notes, consts.MsgCustodyKeptEventOutside)
		}
	case in.Current == nil:
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodyKeptNoImage
	case in.Chain.Held != nil || in.NoHistory:
		// 🔒 Broken, not unmade. The image is not the one kept, which is a fact; only descent is
		// unknown. Softening it here would let anyone able to make the history unreadable turn
		// the one comparison that catches a rollback into a shrug.
		reason := consts.MsgCustodyHistoryNone
		if in.Chain.Held != nil {
			reason = in.Chain.Held.Reason
		}
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgCustodyKeptHeld, reason)
	case in.KeptAncestor:
		l.Message = consts.MsgCustodyKeptAncestor
	case in.KeptKnown && in.Chain.Gaps > 0:
		// The walk may have stopped at a gap rather than at a rollback; say both.
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodyKeptRecordedGaps
	case in.KeptKnown:
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodyKeptRecordedOff
	default:
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodyKeptUnknown
		if in.Chain.Gaps > 0 {
			l.Notes = append(l.Notes, fmt.Sprintf(consts.MsgCustodyHistoryGaps, in.Chain.Gaps))
		}
		for _, k := range in.Chain.Kept {
			l.Notes = append(l.Notes, fmt.Sprintf(consts.MsgCustodyHistoryReset, k.Reason, k.KeptAt))
		}
	}
	return l
}

// caseRecords are the engine's store-wide layers, read from its structured fields. The engine's
// text appears only as the cause of a break, or for a layer this build has no words for.
func caseRecords(e graphene.EventCustody) []Layer {
	out := []Layer{
		record(e, consts.CustodyLayerAttestation, func() (string, string) {
			switch {
			case e.AttestationVerified:
				return consts.CustodyEstablished, consts.MsgCustodyAttestationVerified
			case e.Attested:
				return consts.CustodyNotEstablished, consts.MsgCustodyAttestationUnkeyed
			default:
				return consts.CustodyNotEstablished, consts.MsgCustodyAttestationNone
			}
		}),
		record(e, consts.CustodyLayerSegments, func() (string, string) {
			if e.SegmentsChecked == 0 {
				return consts.CustodyNotEstablished, consts.MsgCustodySegmentsNone
			}
			return consts.CustodyEstablished, fmt.Sprintf(consts.MsgCustodySegmentsKept, e.SegmentsChecked)
		}),
		record(e, consts.CustodyLayerAudit, func() (string, string) {
			if e.AuditEntriesWalked == 0 {
				return consts.CustodyNotEstablished, consts.MsgCustodyAuditNone
			}
			return consts.CustodyEstablished, fmt.Sprintf(consts.MsgCustodyAuditKept, e.AuditEntriesWalked)
		}),
		// 🔒 With no key, the engine skips grant and removal signatures without reporting a gap. A
		// ledger that exists is therefore never established here, only unchecked.
		record(e, consts.CustodyLayerRoles, func() (string, string) {
			if e.GrantsWalked == 0 {
				return consts.CustodyNotEstablished, consts.MsgCustodyRolesNone
			}
			return consts.CustodyNotEstablished, fmt.Sprintf(consts.MsgCustodyRolesUnkeyed, e.GrantsWalked)
		}),
		removalsRecord(e),
	}
	known := []string{
		consts.CustodyLayerSnapshot, consts.CustodyLayerAttestation, consts.CustodyLayerSegments,
		consts.CustodyLayerAudit, consts.CustodyLayerRoles, consts.CustodyLayerRedaction,
	}
	var unknown []string
	for _, g := range e.Gaps {
		switch {
		case slices.Contains(known, g.Layer), slices.Contains(unknown, g.Layer):
		// The placeholder for the comparison the kept-fingerprint layer makes. Anything else from
		// the external layer is a check this build does not know.
		case g.Layer == consts.CustodyLayerExternal && !g.Fatal:
		default:
			unknown = append(unknown, g.Layer)
		}
	}
	for _, name := range unknown {
		l := Layer{Layer: name, State: consts.CustodyNotEstablished, Message: fmt.Sprintf(consts.MsgCustodyEngineUnknown, name)}
		if fatal := causes(e.Gaps, name, true); fatal != "" {
			l.State, l.Cause = consts.CustodyBroken, fatal
		} else {
			l.Cause = causes(e.Gaps, name, false)
		}
		out = append(out, l)
	}
	return out
}

// record builds one case-record line: broken when the engine reports a fatal gap in that layer,
// otherwise what decide reads from the structured fields.
func record(e graphene.EventCustody, name string, decide func() (string, string)) Layer {
	if cause := causes(e.Gaps, name, true); cause != "" {
		return Layer{Layer: name, State: consts.CustodyBroken, Message: consts.MsgCustodyRecordBroken, Cause: cause}
	}
	state, msg := decide()
	return Layer{Layer: name, State: state, Message: msg}
}

// removalsRecord is the removal ledger. The engine reports its gaps under the redaction layer; the
// fatal ones are about the ledger, the others about one entity (the event's redaction layer).
func removalsRecord(e graphene.EventCustody) Layer {
	l := record(e, consts.CustodyLayerRedaction, func() (string, string) {
		if e.RedactionsWalked == 0 {
			return consts.CustodyNotEstablished, consts.MsgCustodyRemovalsNone
		}
		return consts.CustodyNotEstablished, fmt.Sprintf(consts.MsgCustodyRemovalsUnkeyed, e.RedactionsWalked)
	})
	l.Layer = consts.CustodyRecordRemovals
	return l
}

// causes joins the engine's details for one layer's gaps of one severity.
func causes(gaps []graphene.CustodyGap, layer string, fatal bool) string {
	var out []string
	for _, g := range gaps {
		if g.Layer == layer && g.Fatal == fatal {
			out = append(out, g.Detail)
		}
	}
	return strings.Join(out, "; ")
}

// NormalizeFingerprint reads a fingerprint an analyst pasted: surrounding space is ignored, and what
// is left must be one hash in hex. It returns the lowercase form the history stores, so a match is a
// plain comparison that case or padding cannot defeat; empty means none was supplied.
//
// 🔒 The all-zero hash is refused: it is what "no fingerprint" encodes to, and accepting it would let
// an empty value match an image with no roots.
func NormalizeFingerprint(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return "", ErrMalformedFingerprint
	}
	if !slices.ContainsFunc(b, func(c byte) bool { return c != 0 }) {
		return "", ErrMalformedFingerprint
	}
	return hex.EncodeToString(b), nil
}
