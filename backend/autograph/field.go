package autograph

import (
	"strings"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// fieldAlgorithm correlates events that match the rule's ordered event-ID sequence AND share a
// value for every named correlation field.
//
// This is the algorithm that retires the caveat v0.1.0 had to state about every rule it
// shipped: that a match established a temporally ordered pairing on one host and nothing more
// — not the same user, not the same account, not the same logon session. A field match
// establishes the linkage directly, from values read off the records themselves.
//
// The whole design rests on one decision, in bucketKey below: an event that carries NO value
// for a required field is excluded from matching, never bucketed under the empty string.
type fieldAlgorithm struct{}

func (fieldAlgorithm) Generate(spec *rules.Spec, ds *Dataset) Result {
	var res Result
	if spec == nil || ds == nil || len(spec.Sequence) < consts.RuleMinSequence {
		return res
	}
	slots, ok := spec.MatchSlots()
	if !ok || len(slots) == 0 {
		// Validation requires at least one valid field, so reaching here means a spec that
		// never passed the loader. Returning nothing is the safe answer: correlating on no
		// fields would silently be sequence correlation claiming to be more.
		return res
	}
	res.SkippedUndated = ds.SkippedUndated
	res.StaleCorrelationKeys = ds.StaleCorrelationKeys

	for _, group := range ds.Groups(spec.ScopeOrDefault()) {
		// Buckets are filled by a single stable pass over an already-chronological slice, so
		// every bucket is chronological for free. There is no per-bucket sort anywhere in this
		// algorithm — that is the payoff of the dataset sorting once.
		buckets, order := bucketByKey(group.Events, slots, &res)
		for _, key := range order {
			bucket := buckets[key]
			// The basis is constant within a bucket — every event in it shares the same values,
			// which is what defines the bucket — so it is rendered once from any member rather
			// than recomputed per edge. It is still attached to every relation, because an
			// analyst reading one edge should not have to find its siblings to learn why it
			// exists.
			matchScope(spec, bucket, fieldBasis(bucket[0], slots), &res)
		}
	}
	return res
}

// bucketByKey partitions events by their values in the required slots.
//
// Events lacking any required value are EXCLUDED and counted. This is the single most
// important line in the algorithm. Windows writes "-" and the null SID constantly, and the
// extractor already maps those to absent — so bucketing an absent value under "" would gather
// every event that happens not to carry the field into one enormous bucket and correlate them
// all with each other. That is not a smaller result or a slower one; it is a false-positive
// engine, and it would look exactly like a working rule.
func bucketByKey(events []*graphene.Event, slots []int, res *Result) (map[string][]*graphene.Event, []string) {
	buckets := map[string][]*graphene.Event{}
	var order []string

	for _, e := range events {
		key, ok := bucketKey(e, slots)
		if !ok {
			res.SkippedNoKeys++
			continue
		}
		if _, seen := buckets[key]; !seen {
			// First-appearance order, which is chronological because the input is. Recording it
			// here rather than sorting the keys afterwards keeps the whole pass linear AND
			// deterministic — two runs over the same events visit buckets in the same order, so
			// the global match cap drops the same tail.
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], e)
	}
	return buckets, order
}

// bucketKey builds the correlation key for one event, or reports that the event cannot take
// part because a required value is missing.
func bucketKey(e *graphene.Event, slots []int) (string, bool) {
	var b strings.Builder
	for _, slot := range slots {
		v := e.CorrelationKey(slot)
		if v == "" {
			return "", false
		}
		b.WriteString(v)
		// The ASCII unit separator, for the same reason the hashing uses it: a control
		// character cannot occur in event text, so field boundaries can never collide by
		// concatenation. Without it {"AB","C"} and {"A","BC"} would be the same key.
		b.WriteString(consts.FieldSeparator)
	}
	return b.String(), true
}

// fieldBasis renders why a set of events was joined, one entry per matched field.
func fieldBasis(e *graphene.Event, slots []int) []string {
	out := make([]string, 0, len(slots))
	for _, slot := range slots {
		out = append(out, consts.CorrelationSlots[slot]+"="+e.CorrelationKey(slot))
	}
	return out
}
