package autograph

import (
	"sort"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// sequenceAlgorithm correlates events by matching the rule's ordered event-ID sequence
// within a scope (the originating computer, in v1). Matching is greedy and non-overlapping:
// for each completed occurrence it emits one edge between each pair of consecutive matched
// events, stamped with the rule's optional per-connection label, then resumes scanning
// AFTER the occurrence. This makes evaluation a single linear pass per scope — bounded and
// never combinatorial.
type sequenceAlgorithm struct{}

func (sequenceAlgorithm) Generate(spec *rules.Spec, events []*graphene.Event) Result {
	var res Result
	if spec == nil || len(spec.Sequence) < consts.RuleMinSequence {
		return res
	}

	// Undated events cannot take part in a time-ordered match: they have no position in the
	// sequence, and ordering them by their zero timestamp would place them before every real
	// record and let them match chains they never participated in. They are excluded here —
	// in the algorithm itself, not only by the caller's filter — and counted so the run can
	// report what it left out rather than silently ignoring them.
	dated := make([]*graphene.Event, 0, len(events))
	for _, e := range events {
		if e.Timestamp.IsZero() {
			res.SkippedUndated++
			continue
		}
		dated = append(dated, e)
	}
	events = dated

	// Group by scope, then evaluate scopes in a deterministic (sorted) order so the global
	// match cap always drops the same tail regardless of input ordering.
	byScope := groupByScope(events)
	scopes := make([]string, 0, len(byScope))
	for scope := range byScope {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)

	for _, scope := range scopes {
		scoped := byScope[scope]
		sortChronological(scoped)
		matchScope(spec, scoped, &res)
	}
	return res
}

// matchScope runs the greedy non-overlapping subsequence match over one scope's
// chronologically-ordered events, appending edges to res until the sequence can no longer
// complete or the global match cap is hit.
func matchScope(spec *rules.Spec, events []*graphene.Event, res *Result) {
	seq := spec.Sequence
	n := len(events)
	start := 0

	for start+len(seq) <= n {
		matched := greedyMatch(events, seq, start)
		if matched == nil {
			return // no further occurrence can complete from here on
		}
		if res.Matches >= maxMatches {
			res.Truncated = true
			res.Dropped++
			// Keep scanning only to count how many more we drop, still non-overlapping.
			start = matched[len(matched)-1] + 1
			continue
		}
		res.Matches++
		for k := 0; k < len(matched)-1; k++ {
			res.Relations = append(res.Relations, graphene.Relation{
				From:            events[matched[k]].ID,
				To:              events[matched[k+1]].ID,
				RelationType:    spec.RelationType,
				Label:           spec.LabelFor(k),
				ConfidenceScore: consts.RuleMatchConfidence,
				CreatedBy:       consts.CreatedBySystem,
			})
		}
		// Non-overlapping: the next occurrence starts after this one's final event.
		start = matched[len(matched)-1] + 1
	}
}

// greedyMatch returns the indices of the earliest subsequence of events (at or after
// start) whose event IDs equal seq in order, or nil if no such subsequence exists.
func greedyMatch(events []*graphene.Event, seq []string, start int) []int {
	matched := make([]int, 0, len(seq))
	step := 0
	for i := start; i < len(events) && step < len(seq); i++ {
		if events[i].EventID == seq[step] {
			matched = append(matched, i)
			step++
		}
	}
	if step != len(seq) {
		return nil
	}
	return matched
}

// groupByScope buckets events by their correlation scope (computer). Events with an empty
// computer share a single scope so they still correlate among themselves.
func groupByScope(events []*graphene.Event) map[string][]*graphene.Event {
	byScope := make(map[string][]*graphene.Event)
	for _, e := range events {
		byScope[e.Computer] = append(byScope[e.Computer], e)
	}
	return byScope
}

// sortChronological orders events by timestamp, tie-breaking on node ID so the scan (and
// therefore the emitted edges) is deterministic even when timestamps collide.
func sortChronological(events []*graphene.Event) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].ID < events[j].ID
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
}
