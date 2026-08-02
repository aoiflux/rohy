package autograph

import (
	"strconv"

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

func (sequenceAlgorithm) Generate(spec *rules.Spec, ds *Dataset) Result {
	var res Result
	if spec == nil || ds == nil || len(spec.Sequence) < consts.RuleMinSequence {
		return res
	}

	// Undated events were dropped once, by Prepare, for every rule in the build. The count is
	// reported here so a rule's own result can still say what the run left out — but it is
	// read from the dataset rather than recounted, because it is a property of the dataset and
	// twenty rules independently arriving at the same number is twenty chances to disagree.
	res.SkippedUndated = ds.SkippedUndated

	// Partitions arrive already chronological and in deterministic order, so the global match
	// cap always drops the same tail regardless of how the events arrived.
	for _, group := range ds.Groups(spec.ScopeOrDefault()) {
		matchScope(spec, group.Events, nil, &res)
	}
	return res
}

// emitChain turns one matched occurrence into edges — one per consecutive pair — stamped with
// the provenance that lets the result be read back afterwards.
//
// It is shared by every sequence-shaped algorithm (sequence, field, temporal) so that the edge
// shape, the labelling rule, and the provenance are written once. An algorithm supplies only
// what is specific to it: the events it matched, and the basis explaining why.
//
// matched holds INDICES into events, because the matchers work in index space to keep the
// non-overlapping resume rule expressible.
func emitChain(spec *rules.Spec, events []*graphene.Event, matched []int, basis []string, res *Result) {
	chain := make([]*graphene.Event, len(matched))
	for i, idx := range matched {
		chain[i] = events[idx]
	}
	id := matchID(chain)

	for k := 0; k < len(chain)-1; k++ {
		rel := graphene.Relation{
			From:            chain[k].ID,
			To:              chain[k+1].ID,
			RelationType:    spec.RelationType,
			Label:           spec.LabelFor(k),
			ConfidenceScore: consts.ConfidenceExactMatch,
			CreatedBy:       consts.CreatedBySystem,
		}
		// RuleID is left empty here on purpose: the algorithm does not know which rule is
		// running it, and inventing one would be provenance the caller could not trust. The
		// build workflow stamps it at persist time, alongside GraphID and CreatedAt.
		rel.StampProvenance("", spec.AlgorithmOrDefault(), id, k, basis)
		res.Relations = append(res.Relations, rel)
	}
}

// matchID names one occurrence of a rule, so every edge the occurrence produced can be found
// from any one of them ("highlight the other 3 edges of this match").
//
// It is DERIVED, never generated. A random or counter-based id would change on every rebuild,
// which would make two builds of the same case incomparable and would defeat the idempotency
// the whole build workflow is designed around — a rebuild replaces a graph, and the
// replacement should be identical when nothing changed. The endpoints plus the step count
// identify an occurrence uniquely within a rule, and a rule's edges are already scoped to its
// own graph, so nothing wider is needed.
func matchID(matched []*graphene.Event) string {
	if len(matched) == 0 {
		return ""
	}
	first, last := matched[0].ID, matched[len(matched)-1].ID
	return "m-" + strconv.FormatUint(first, 10) +
		"-" + strconv.FormatUint(last, 10) +
		"-" + strconv.Itoa(len(matched))
}

// matchScope runs the greedy non-overlapping subsequence match over one scope's
// chronologically-ordered events, appending edges to res until the sequence can no longer
// complete or the global match cap is hit.
// basis is the reason every match in this partition exists, or nil when the partition itself
// is the reason (plain sequence correlation, where ordering on one host IS the whole claim).
// It is constant across a partition because a partition is defined by the shared values, so it
// is computed once by the caller rather than per edge.
func matchScope(spec *rules.Spec, events []*graphene.Event, basis []string, res *Result) {
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
		emitChain(spec, events, matched, basis, res)
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

// Scope grouping and chronological ordering used to live here, done per rule. They now belong
// to the dataset (see dataset.go), which does both once for the whole build.
