package redaction

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/payload"
	"rohy/backend/snapshot"
)

// Running a redaction.
//
// The steps run in a fixed order, each recorded in the journal as it ends:
//
//  1. relations — each rule-made relation touching a target, removed by a recorded edge redaction
//  2. records   — each target's record stripped, recorded, and its index entries purged
//  3. payloads  — each target's payload record, and every copy of it, tombstoned in place
//  4. sidecars  — descriptors and basis copies scrubbed from findings and snapshots
//  5. seal      — the case sealed, so the image no longer holds what was removed
//
// 🔒 Every step is safe to run again, and finishing a redaction is running it again: a step that
// landed does nothing the second time, and one that did not lands. Nothing is skipped on the
// strength of the journal saying it was done, because what the journal records is what was true
// then.
//
// A failure stops the run at that step. Later steps are reported as not reached, never as done.

// Engine is the case store's write side. *graphene.Store satisfies it.
type Engine interface {
	RedactRelation(id uint64, reason string) error
	RedactEventRecord(id uint64, hashNormalized, reason string) error
	TombstonePayload(owner uint64, refs ...payload.Ref) error
}

// FindingScrubber is the findings sidecar. *findings.Store satisfies it.
type FindingScrubber interface {
	ScrubDescriptors(keys []string, replacement string) ([]string, error)
}

// SnapshotScrubber is the snapshot sidecar. *snapshot.Store satisfies it.
type SnapshotScrubber interface {
	Scrub(hashes []string, replacement string) (snapshot.ScrubResult, error)
}

// Deps is everything a run touches. All are required.
type Deps struct {
	Journal   *Journal
	Engine    Engine
	Findings  FindingScrubber
	Snapshots SnapshotScrubber
	// Seal seals the case if it needs it and records the fingerprint, returning the seal state.
	Seal func() (string, error)
	Now  func() time.Time
}

func (d Deps) missing() string {
	switch {
	case d.Journal == nil:
		return "the redaction journal"
	case d.Engine == nil:
		return "the case store"
	case d.Findings == nil:
		return "the findings sidecar"
	case d.Snapshots == nil:
		return "the snapshot sidecar"
	case d.Seal == nil:
		return "the seal"
	case d.Now == nil:
		return "the clock"
	}
	return ""
}

// StepOutcome is how one step of a run ended.
type StepOutcome struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// Count is how many things the step changed: relations removed, records stripped, payload
	// records erased, sidecar files rewritten.
	Count int      `json:"count"`
	Notes []string `json:"notes,omitempty"`
}

// Outcome is what a run did, step by step.
type Outcome struct {
	Entry uint64        `json:"entry"`
	Event uint64        `json:"event"`
	Done  bool          `json:"done"`
	Steps []StepOutcome `json:"steps"`
	// Error is a failure outside any one step: the journal could not mark a finished run done.
	Error string `json:"error,omitempty"`
}

// Run runs, or finishes, journal entry id.
//
// The error is for a run that could not start: a missing dependency, an unknown entry. Everything
// that happens once it starts is in the Outcome.
func Run(d Deps, id uint64) (Outcome, error) {
	if m := d.missing(); m != "" {
		return Outcome{}, fmt.Errorf(consts.MsgRedactUnavailable, m)
	}
	if err := d.Journal.Err(); err != nil {
		return Outcome{}, err
	}
	e, ok := d.Journal.Get(id)
	if !ok {
		return Outcome{}, fmt.Errorf(consts.MsgJournalNoEntry, id)
	}
	out := Outcome{Entry: e.ID, Event: e.Event}
	run := []struct {
		name string
		fn   func(Deps, Entry, *StepOutcome) error
	}{
		{consts.RedactStepRelations, stepRelations},
		{consts.RedactStepRecords, stepRecords},
		{consts.RedactStepPayloads, stepPayloads},
		{consts.RedactStepSidecars, stepSidecars},
		{consts.RedactStepSeal, stepSeal},
	}
	stopped := false
	for _, s := range run {
		so := StepOutcome{Name: s.name, State: consts.RedactStepNotReached}
		if !stopped {
			err := s.fn(d, e, &so)
			so.State = consts.RedactStepDone
			if err != nil {
				so.State, so.Error = consts.RedactStepFailed, err.Error()
			}
			if jerr := d.Journal.Step(e.ID, s.name, d.Now(), err); jerr != nil {
				so.State, so.Error = consts.RedactStepFailed, joinErr(so.Error, fmt.Sprintf(consts.MsgRedactStepUnrecorded, jerr))
			}
			stopped = so.State != consts.RedactStepDone
		}
		out.Steps = append(out.Steps, so)
	}
	if stopped {
		return out, nil
	}
	if err := d.Journal.Complete(e.ID, d.Now()); err != nil {
		out.Error = fmt.Sprintf(consts.MsgRedactUnfinished, err)
		return out, nil
	}
	out.Done = true
	return out, nil
}

func joinErr(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

func stepRelations(d Deps, e Entry, so *StepOutcome) error {
	for _, rid := range e.Relations {
		err := d.Engine.RedactRelation(rid, e.Reason)
		var gone *graphene.GoneError
		switch {
		case errors.As(err, &gone):
			so.Notes = append(so.Notes, fmt.Sprintf(consts.MsgRedactNoteRelationGone, rid))
		case err != nil:
			return err
		default:
			so.Count++
		}
	}
	return nil
}

func stepRecords(d Deps, e Entry, so *StepOutcome) error {
	for _, t := range e.Targets {
		err := d.Engine.RedactEventRecord(t.Node, t.HashNormalized, e.Reason)
		var gone *graphene.GoneError
		switch {
		case errors.As(err, &gone):
			so.Notes = append(so.Notes, fmt.Sprintf(consts.MsgRedactNoteEventGone, t.Node))
		case err != nil:
			return err
		default:
			so.Count++
		}
	}
	return nil
}

// stepPayloads erases one record at a time, so a record another redaction already erased is a
// note rather than the failure of the rest.
func stepPayloads(d Deps, e Entry, so *StepOutcome) error {
	for _, t := range e.Targets {
		refs := append([]payload.Ref{t.Payload}, t.Copies...)
		for _, r := range refs {
			if r.IsZero() {
				continue
			}
			err := d.Engine.TombstonePayload(t.Node, r)
			var other *payload.TombstoneOwnerError
			switch {
			case errors.As(err, &other):
				so.Notes = append(so.Notes, fmt.Sprintf(consts.MsgRedactNoteOtherOwner, other.Offset, other.Owner))
			case err != nil:
				return err
			default:
				so.Count++
			}
		}
	}
	return nil
}

// stepSidecars scrubs both sidecars before reporting either's failure, so one unreadable snapshot
// does not leave every finding's descriptor in place.
func stepSidecars(d Deps, e Entry, so *StepOutcome) error {
	hashes := make([]string, 0, len(e.Targets))
	for _, t := range e.Targets {
		hashes = append(hashes, t.HashNormalized)
	}
	keys, ferr := d.Findings.ScrubDescriptors(hashes, consts.RedactedDescriptor)
	so.Count += len(keys)
	res, serr := d.Snapshots.Scrub(hashes, consts.RedactedDescriptor)
	so.Count += len(res.Changed)
	if serr == nil && len(res.Unreadable) > 0 {
		serr = fmt.Errorf(consts.MsgRedactSnapsUnreadable, len(res.Unreadable), strings.Join(res.Unreadable, ", "))
	}
	return errors.Join(ferr, serr)
}

func stepSeal(d Deps, _ Entry, so *StepOutcome) error {
	state, err := d.Seal()
	if state != "" {
		so.Notes = append(so.Notes, fmt.Sprintf(consts.MsgRedactNoteSeal, state))
	}
	return err
}
