// Package autograph is rohy's auto-graphing engine (P3). Given a correlation rule and a
// set of events, it deterministically produces the relations (edges) the rule implies —
// returned UNPERSISTED, so the caller (the P6 workflow) owns graph scoping, timestamps,
// and persistence. Algorithm types are pluggable behind the Algorithm interface; only
// sequence correlation ships in v1, with field-correlation and temporal-window reserved
// as future registered types. This package sits above rules + graphene and never writes
// to the store itself.
package autograph

import (
	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// Result is the outcome of running one rule over an event set. Relations carry From/To,
// RelationType, Label, ConfidenceScore, and CreatedBy=system; GraphID and CreatedAt are
// intentionally left zero for the caller to stamp at persist time. Truncated/Dropped
// report a hit match cap so the caller can log it (never a silent truncation).
type Result struct {
	Relations []graphene.Relation
	Matches   int
	Truncated bool
	Dropped   int
	// SkippedUndated counts events the algorithm refused to consider because they carry no
	// timestamp. Sequence correlation is time-ordered, so an undated event has no position
	// in a chain; it is excluded rather than ordered by its zero time, and reported so a
	// run can say what it left out.
	SkippedUndated int
	// SkippedNoKeys counts events excluded because they carry no value for a required
	// correlation field. Reported because the natural reading of a small result — "this
	// pattern is rare" — is very different from the true one, "most of your events could not
	// be considered", and nothing else in the outcome would distinguish them.
	SkippedNoKeys int
	// StaleCorrelationKeys is how many evaluated events carry no projection from the current
	// extraction recipe, carried up from the dataset so a rule's own outcome can explain an
	// under-report instead of the reader having to know to look elsewhere.
	StaleCorrelationKeys int
	// UnresolvedParents counts process-creation events whose creator could not be resolved to
	// a process that was alive at the time. No edge is emitted for them and none is guessed.
	UnresolvedParents int
}

// Algorithm turns a rule spec + a prepared dataset into relations. Implementations must be
// pure and deterministic: same inputs → same output, independent of map iteration or wall
// clock, and they must not mutate the dataset — it is shared by every rule in a build.
type Algorithm interface {
	Generate(spec *rules.Spec, ds *Dataset) Result
}

// registry maps an algorithm type (consts.Algo*) to its implementation. Adding a new
// correlation strategy is a matter of registering it here and accepting its type name in
// rule validation — no caller changes required.
var registry = map[string]Algorithm{
	consts.AlgoSequence: sequenceAlgorithm{},
	consts.AlgoField:    fieldAlgorithm{},
	consts.AlgoTemporal: temporalAlgorithm{},
	consts.AlgoLineage:  lineageAlgorithm{},
}

// maxMatches is the completed-match cap, seeded from consts so the default is const-driven;
// it is a var only so tests can lower it to exercise truncation without generating 100k
// matches.
var maxMatches = consts.AutoGraphMaxMatches

// For returns the algorithm registered for the given type, or (nil, false) if unknown.
func For(algoType string) (Algorithm, bool) {
	a, ok := registry[algoType]
	return a, ok
}

// Generate runs a rule over a raw event slice, preparing a dataset for just this rule.
//
// It is the one-off entry point — a single rule, a single evaluation — used by tests and by
// any caller that has events rather than a build. A build should use GenerateWith instead:
// preparing per rule is exactly the duplicated work the dataset exists to remove, and it costs
// a full sort of the case for every rule.
func Generate(spec *rules.Spec, events []*graphene.Event) Result {
	ds := Prepare(events, RequirementsFor([]*rules.Spec{spec}))
	return GenerateWith(spec, ds)
}

// GenerateWith runs the algorithm selected by the rule (defaulting to sequence correlation)
// against an already-prepared dataset, returning the relations it would create.
//
// An unrecognized algorithm yields an empty result rather than an error, because rule
// validation already rejects unknown algorithm types at load time — this is a defensive
// default, not the error path.
func GenerateWith(spec *rules.Spec, ds *Dataset) Result {
	if spec == nil || ds == nil {
		return Result{}
	}
	algo, ok := For(spec.AlgorithmOrDefault())
	if !ok {
		return Result{}
	}
	return algo.Generate(spec, ds)
}
