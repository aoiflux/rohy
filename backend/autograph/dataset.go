package autograph

import (
	"sort"
	"strings"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// The prepared dataset.
//
// Before this existed, every rule did the same three things to the same slice: drop undated
// events, group by computer, and sort each group chronologically. A build of twenty rules
// therefore sorted the case twenty times. The dataset does that work ONCE and hands every rule
// the result.
//
// Two properties make it safe to share:
//
//   - It is IMMUTABLE after construction. There is no lazy memoization of partitions computed
//     on first use, deliberately: a mutable structure shared across rules is a data race the
//     moment anything evaluates two rules concurrently, and the purity of this package is what
//     makes the whole engine testable and the rule testbench a safe thing to offer.
//   - It is built to REQUIREMENTS. A build of sequence-only rules never pays to partition by a
//     scope nothing asked for. What is prepared is the union of what the selected rules
//     actually read.
//
// The ordering trick worth noticing: partitions are built by a STABLE pass over the globally
// sorted slice, so every partition is already chronological. There is no per-partition sort at
// all — which is what keeps preparation linear in the number of partitions rather than
// O(n log n) again for each one.

// Group is one partition of the dataset: the scope value that defines it, and its events in
// chronological order.
type Group struct {
	Value  string
	Events []*graphene.Event
}

// Requirements states what a set of rules needs prepared. It is the union over the rules of a
// build, so preparation is paid for once and only for what is read.
type Requirements struct {
	// Scopes are the correlation scopes (consts.CorrelationScopes) any selected rule uses.
	Scopes []string
}

// Fingerprint renders what was prepared, so a cached dataset can be checked against what a
// later run needs rather than assumed to cover it.
//
// A dataset prepared for computer-scoped rules genuinely cannot serve a global-scoped one — it
// holds no such partition — so reusing it would silently correlate nothing. The fingerprint is
// what turns that into a cache miss.
func (r Requirements) Fingerprint() string {
	return "scopes:" + strings.Join(r.Scopes, ",")
}

// RequirementsFor computes what a set of rule specs collectively needs.
func RequirementsFor(specs []*rules.Spec) Requirements {
	seen := map[string]bool{}
	var req Requirements
	for _, spec := range specs {
		if spec == nil {
			continue
		}
		if scope := spec.ScopeOrDefault(); !seen[scope] {
			seen[scope] = true
			req.Scopes = append(req.Scopes, scope)
		}
	}
	sort.Strings(req.Scopes) // deterministic, so a dataset's shape does not depend on rule order
	return req
}

// Dataset is the shared, prepared view of a build's events.
type Dataset struct {
	// Events are the dated events in chronological order, tie-broken by node id.
	Events []*graphene.Event
	// SkippedUndated is how many events were dropped for having no timestamp. Counted here
	// rather than per rule, because it is a property of the dataset and every rule that reads
	// it would otherwise report the same number as if it had discovered it.
	SkippedUndated int
	// StaleCorrelationKeys is how many events carry no projection from the current recipe.
	// A field, temporal or lineage rule over these under-reports, so a run states it rather
	// than returning the short answer as though it were the whole one.
	StaleCorrelationKeys int

	// partitions holds one index per prepared scope name.
	partitions map[string][]Group
}

// Prepare builds the shared dataset. It is pure and deterministic: same events and
// requirements in, identical dataset out, independent of input order or map iteration.
func Prepare(events []*graphene.Event, req Requirements) *Dataset {
	ds := &Dataset{partitions: map[string][]Group{}}

	// Undated events cannot take part in time-ordered correlation: they have no position in a
	// chain, and ordering them by their zero timestamp would place them before every real
	// record and let them match chains they never participated in. They are excluded HERE, in
	// the engine, as well as by the caller's filter — two independent checks, because a
	// timeless record in a time-ordered matcher is a correctness bug, not a display one.
	dated := make([]*graphene.Event, 0, len(events))
	for _, e := range events {
		if e.Timestamp.IsZero() {
			ds.SkippedUndated++
			continue
		}
		if !e.HasCurrentCorrelationKeys() {
			ds.StaleCorrelationKeys++
		}
		dated = append(dated, e)
	}

	// The one sort. Everything below inherits this order.
	sortChronological(dated)
	ds.Events = dated

	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = []string{consts.DefaultScope}
	}
	for _, scope := range scopes {
		ds.partitions[scope] = partitionBy(dated, scope)
	}
	return ds
}

// Groups returns the partitions for a scope, in deterministic (sorted) order.
//
// The sorted order is not cosmetic: it is what makes the global match cap drop the same tail
// on every run regardless of how the events arrived. A cap that truncates a different set each
// time would make two builds of the same case disagree.
//
// A scope that was not prepared is partitioned on demand. That should not happen in a build —
// requirements are computed from the same rules that are about to run — but returning nothing
// would silently produce an empty graph, and a rule finding nothing because of a wiring
// mistake must not look like a rule finding nothing because the pattern is absent.
func (d *Dataset) Groups(scope string) []Group {
	if groups, ok := d.partitions[scope]; ok {
		return groups
	}
	groups := partitionBy(d.Events, scope)
	d.partitions[scope] = groups
	return groups
}

// partitionBy splits chronologically ordered events into scope groups.
//
// The append pass is stable, so each group comes out chronological without being sorted:
// preparation costs one O(n log n) sort for the whole dataset and O(n) per scope, rather than
// O(n log n) per group.
func partitionBy(events []*graphene.Event, scope string) []Group {
	if scope == consts.ScopeGlobal {
		// One partition holding everything. A global rule is only meaningful alongside
		// match_fields — on its own it correlates across the whole case — which is what the
		// field's guidance says and what the editor repeats.
		if len(events) == 0 {
			return nil
		}
		return []Group{{Value: "", Events: events}}
	}

	byValue := map[string][]*graphene.Event{}
	for _, e := range events {
		key := scopeValue(e, scope)
		byValue[key] = append(byValue[key], e)
	}
	values := make([]string, 0, len(byValue))
	for v := range byValue {
		values = append(values, v)
	}
	sort.Strings(values)

	out := make([]Group, 0, len(values))
	for _, v := range values {
		out = append(out, Group{Value: v, Events: byValue[v]})
	}
	return out
}

// scopeValue reads the field a scope partitions on. Events with an empty value share a single
// partition so they still correlate among themselves rather than each becoming a group of one.
func scopeValue(e *graphene.Event, scope string) string {
	switch scope {
	case consts.ScopeComputer:
		return e.Computer
	default:
		// An unrecognized scope cannot reach here from a loaded rule — validation refuses it
		// rather than defaulting, precisely so that a scope never silently becomes a different
		// one. Falling back to the default keeps a hand-built spec from panicking.
		return e.Computer
	}
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
