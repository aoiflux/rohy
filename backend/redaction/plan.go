package redaction

import (
	"fmt"
	"slices"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/payload"
)

// Source is the read side of the case a redaction is planned against. *graphene.Store satisfies
// it.
type Source interface {
	RedactionSubject(id uint64) (graphene.RedactionSubject, error)
	EventsSharing(hashNormalized, hashRaw string) ([]uint64, error)
	PayloadCopies(hashRaw string, own payload.Ref) ([]payload.Ref, error)
}

// Plan is a redaction resolved against the case: the entry to journal, and what it leaves.
type Plan struct {
	Entry Entry `json:"entry"`
	// AnalystRelations touch a target and are kept: a person drew them, and removing one is
	// theirs to decide. Listed so the preview can say so.
	AnalystRelations []*graphene.Relation `json:"analyst_relations"`
	// PayloadCopies counts the payload records the redaction erases, the targets' own included.
	PayloadCopies int `json:"payload_copies"`
}

// Prepare resolves a redaction of event id.
//
// Its targets are the event and every event holding the same content — the same identity hash, or
// the same raw record read from another path — because redacting one copy while another holds the
// same text removes nothing. Each target's payload record is erased, with every unreferenced copy of
// it in the log. Every rule-made relation touching a target is removed; the analyst's are kept.
//
// It writes nothing.
func Prepare(src Source, id uint64, reason string, at time.Time) (Plan, error) {
	r, err := ValidReason(reason)
	if err != nil {
		return Plan{}, err
	}
	p, err := Resolve(src, id)
	if err != nil {
		return Plan{}, err
	}
	p.Entry.Reason, p.Entry.At = r, at
	return p, nil
}

// Resolve is Prepare without the reason: what a redaction of id would take and leave, for the
// preview. It writes nothing.
func Resolve(src Source, id uint64) (Plan, error) {
	sub, err := src.RedactionSubject(id)
	if err != nil {
		return Plan{}, err
	}
	if sub.Stripped {
		return Plan{}, fmt.Errorf(consts.MsgRedactAlready, id)
	}
	ids, err := src.EventsSharing(sub.HashNormalized, sub.HashRaw)
	if err != nil {
		return Plan{}, err
	}
	if !slices.Contains(ids, id) {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	plan := Plan{Entry: Entry{Event: id}}
	rules := map[uint64]bool{}
	drawn := map[uint64]bool{}
	for _, sid := range ids {
		s := sub
		if sid != id {
			if s, err = src.RedactionSubject(sid); err != nil {
				return Plan{}, err
			}
			if s.Stripped {
				continue // redacted before; nothing left to take
			}
		}
		plan.Entry.Targets = append(plan.Entry.Targets, Target{
			Node: sid, HashNormalized: s.HashNormalized, HashRaw: s.HashRaw, Payload: s.Payload,
		})
		for _, rid := range s.RuleRelations {
			rules[rid] = true
		}
		for _, rel := range s.AnalystRelations {
			if !drawn[rel.ID] {
				drawn[rel.ID] = true
				plan.AnalystRelations = append(plan.AnalystRelations, rel)
			}
		}
	}
	for rid := range rules {
		plan.Entry.Relations = append(plan.Entry.Relations, rid)
	}
	slices.Sort(plan.Entry.Relations)

	// Each record is erased once, naming one owner: its own event when it is one's, otherwise the
	// first target whose content it holds.
	owned := map[payload.Ref]bool{}
	for _, t := range plan.Entry.Targets {
		if !t.Payload.IsZero() {
			owned[t.Payload] = true
		}
	}
	plan.PayloadCopies = len(owned)
	for i := range plan.Entry.Targets {
		t := &plan.Entry.Targets[i]
		copies, err := src.PayloadCopies(t.HashRaw, t.Payload)
		if err != nil {
			return Plan{}, err
		}
		for _, c := range copies {
			if c.IsZero() || owned[c] {
				continue
			}
			owned[c] = true
			t.Copies = append(t.Copies, c)
			plan.PayloadCopies++
		}
	}
	return plan, nil
}
