package autograph

import (
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// temporalAlgorithm correlates an ordered event-ID sequence where each consecutive pair falls
// within a bounded time window, optionally also requiring shared correlation fields.
//
// # WHY THIS IS NOT THE SEQUENCE MATCHER WITH A TIME CHECK BOLTED ON
//
// The sequence matcher takes the EARLIEST subsequence: it scans forward, accepts each step the
// moment it sees it, and never reconsiders. That is correct when the only constraint is order,
// because if any valid subsequence exists then the greedy one does too.
//
// Add a window and that stops being true. Consider seq = [A, B] with within = 1m and events
//
//	A@00:00   A@10:00   B@10:30
//
// Greedy anchors on A@00:00, finds B@10:30, sees 10m30s > 1m, and rejects. But A@10:00 → B@10:30
// IS a valid match, and the greedy matcher has already walked past it. Two obvious repairs are
// both wrong: abandoning the partition drops real matches, and restarting the scan from every
// failed anchor is quadratic on a log where the anchor event is common (4625 is very common).
//
// So this is a different algorithm, not a variant. It sweeps the partition once, and for each
// step of the sequence keeps only the MOST RECENT event that completed it. That is provably
// enough: a later completion has a larger timestamp, so it satisfies t[i] - t[j] <= within for
// strictly more future i than any earlier one — an earlier completion can never win a race the
// later one loses. One pass, no backtracking, no restarts.
//
// The consequence to know about, and to document for rule authors: this matcher is
// LATEST-anchored where the sequence matcher is EARLIEST-anchored. Given an unbounded window
// the two would not always emit the same edges. They are separate algorithms with separate
// names, so that is allowed — but it must be stated rather than discovered.
type temporalAlgorithm struct{}

func (temporalAlgorithm) Generate(spec *rules.Spec, ds *Dataset) Result {
	var res Result
	if spec == nil || ds == nil || len(spec.Sequence) < consts.RuleMinSequence {
		return res
	}
	within, total := spec.Window()
	if within <= 0 {
		// Validation requires a positive window, so this is a spec that never passed the
		// loader. An unbounded temporal rule is a sequence rule, and silently behaving like one
		// would make the rule claim a precision it is not applying.
		return res
	}
	res.SkippedUndated = ds.SkippedUndated
	res.StaleCorrelationKeys = ds.StaleCorrelationKeys

	// A temporal rule may also require shared fields. Composing the two gives the most precise
	// matcher rohy has: same entity, right order, inside a bounded window.
	slots, ok := spec.MatchSlots()
	if !ok {
		return res
	}

	for _, group := range ds.Groups(spec.ScopeOrDefault()) {
		if len(slots) == 0 {
			matchWindowed(spec, group.Events, within, total, nil, &res)
			continue
		}
		buckets, order := bucketByKey(group.Events, slots, &res)
		for _, key := range order {
			bucket := buckets[key]
			matchWindowed(spec, bucket, within, total, fieldBasis(bucket[0], slots), &res)
		}
	}
	return res
}

// matchWindowed sweeps one chronologically ordered partition, emitting every non-overlapping
// occurrence whose consecutive steps fall inside the window.
//
// last[s]   the index of the most recent event that completed step s, or -1
// prev[s]   the index that step s-1 was satisfied at when step s was completed, so a finished
//
//	match can be walked back into the chain that produced it
func matchWindowed(spec *rules.Spec, events []*graphene.Event, within, total time.Duration,
	basis []string, res *Result) {

	seq := spec.Sequence
	n := len(seq)

	// Which steps each event id can satisfy. Built once per partition so the inner loop is
	// proportional to how many steps an id appears at, not to the sequence length.
	stepsFor := make(map[string][]int, n)
	for s, id := range seq {
		stepsFor[id] = append(stepsFor[id], s)
	}

	last := make([]int, n)
	prev := make([]int, n)
	reset := func() {
		for s := range last {
			last[s], prev[s] = -1, -1
		}
	}
	reset()

	for i, e := range events {
		steps := stepsFor[e.EventID]
		if len(steps) == 0 {
			continue
		}
		// DESCENDING through the steps this event could satisfy. Ascending would let one event
		// advance two steps of the same match in a single visit — it would complete step s,
		// and then immediately be read as the "most recent completion of s" that satisfies
		// step s+1, matching itself against itself.
		for k := len(steps) - 1; k >= 0; k-- {
			s := steps[k]
			if s == 0 {
				last[0], prev[0] = i, -1
				continue
			}
			j := last[s-1]
			if j < 0 {
				continue
			}
			if events[i].Timestamp.Sub(events[j].Timestamp) > within {
				continue
			}
			if total > 0 && events[i].Timestamp.Sub(events[anchorOf(prev, last, s-1, j)].Timestamp) > total {
				continue
			}
			last[s], prev[s] = i, j
		}

		// The final step completing on this event is a finished occurrence.
		if last[n-1] != i {
			continue
		}
		matched := walkBack(prev, last, n, i)
		if res.Matches >= maxMatches {
			res.Truncated = true
			res.Dropped++
			reset()
			continue
		}
		res.Matches++
		emitChain(spec, events, matched, temporalBasis(events, matched, within, basis), res)
		// Non-overlapping: nothing from this occurrence may take part in the next one.
		reset()
	}
}

// anchorOf walks the parent chain back from a completed step to the index of step 0, which is
// what the total-window bound is measured from.
func anchorOf(prev, last []int, step, at int) int {
	idx := at
	for s := step; s > 0; s-- {
		p := prev[s]
		if p < 0 {
			break
		}
		idx = p
	}
	return idx
}

// walkBack reconstructs the matched indices, first step to last, from the parent pointers.
func walkBack(prev, last []int, n, end int) []int {
	matched := make([]int, n)
	matched[n-1] = end
	for s := n - 1; s > 0; s-- {
		matched[s-1] = prev[s]
	}
	return matched
}

// temporalBasis states the gap that satisfied the window, appended to any field basis.
//
// It records the WIDEST gap in the chain rather than each one: that is the number that decides
// whether the match survives the window, and four entries of "Δt=2s" would push the field
// basis out of the per-edge cap without telling the reader anything.
func temporalBasis(events []*graphene.Event, matched []int, within time.Duration, fields []string) []string {
	var widest time.Duration
	for k := 0; k+1 < len(matched); k++ {
		if gap := events[matched[k+1]].Timestamp.Sub(events[matched[k]].Timestamp); gap > widest {
			widest = gap
		}
	}
	return append(append([]string{}, fields...), "max Δt="+widest.String()+" ≤ "+within.String())
}
