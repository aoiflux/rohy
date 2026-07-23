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
}

// Algorithm turns a rule spec + events into relations. Implementations must be pure and
// deterministic: same inputs → same output, independent of map iteration or wall clock.
type Algorithm interface {
	Generate(spec *rules.Spec, events []*graphene.Event) Result
}

// registry maps an algorithm type (consts.Algo*) to its implementation. Adding a new
// correlation strategy is a matter of registering it here and accepting its type name in
// rule validation — no caller changes required.
var registry = map[string]Algorithm{
	consts.AlgoSequence: sequenceAlgorithm{},
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

// Generate runs the algorithm selected by the rule (defaulting to sequence correlation)
// over events, returning the relations it would create. An unrecognized algorithm yields
// an empty result rather than an error, because rule validation already rejects unknown
// algorithm types at load time — this is a defensive default, not the error path.
func Generate(spec *rules.Spec, events []*graphene.Event) Result {
	algoType := spec.Algorithm
	if algoType == "" {
		algoType = consts.DefaultAlgorithm
	}
	algo, ok := For(algoType)
	if !ok {
		return Result{}
	}
	return algo.Generate(spec, events)
}
