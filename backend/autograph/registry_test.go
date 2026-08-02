package autograph

import (
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// TestRegistryMatchesTheVocabulary is the guard that closes the gap between what the loader
// ACCEPTS and what the engine can RUN.
//
// The registry has always been described as the extension point, but until v0.2.0 the
// validator checked the algorithm name against a hardcoded constant. Nothing stopped an
// implementation being registered and then rejected at load, or a name being accepted at load
// with no implementation behind it — and the second failure is silent: Generate returns an
// empty result for an unknown algorithm, so the rule would build a graph with no edges and
// look like a rule that simply did not match.
//
// The validator and the engine now derive from one list. This asserts the derivation holds in
// BOTH directions, which is the only version of the check that is worth anything.
func TestRegistryMatchesTheVocabulary(t *testing.T) {
	for _, algo := range consts.Algorithms {
		if _, ok := For(algo.Name); !ok {
			t.Errorf("consts.Algorithms accepts %q at load but no implementation is registered — "+
				"a rule using it would build an empty graph and look like a rule that found nothing",
				algo.Name)
		}
	}
	for name := range registry {
		if _, ok := consts.AlgorithmByName(name); !ok {
			t.Errorf("%q is registered but the validator does not accept it, so no rule can ever "+
				"select it", name)
		}
	}
	if len(registry) != len(consts.Algorithms) {
		t.Errorf("registry has %d implementations, vocabulary has %d", len(registry), len(consts.Algorithms))
	}
}

// TestEveryRegisteredAlgorithmIsPure pins the contract the Algorithm interface states: an
// implementation must not mutate the dataset it is handed. The dataset is shared by every rule
// in a build, so a mutation is not a local bug — it changes what the NEXT rule matches.
func TestEveryRegisteredAlgorithmDoesNotMutateTheDataset(t *testing.T) {
	events := []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		ev(2, "4624", "HOST-A", 1),
		ev(3, "4625", "HOST-B", 2),
	}
	for name := range registry {
		algo, _ := consts.AlgorithmByName(name)
		spec := &rules.Spec{
			Name:      "Purity Probe",
			Algorithm: name,
			Sequence:  []string{"4625", "4624"},
		}
		if algo.Name == consts.AlgoField {
			spec.MatchFields = []string{consts.CorrelationSlots[0]}
		}
		if algo.Name == consts.AlgoTemporal {
			spec.WindowWithin = "5m"
		}

		ds := Prepare(events, RequirementsFor([]*rules.Spec{spec}))
		before := snapshotDataset(ds)
		GenerateWith(spec, ds)
		GenerateWith(spec, ds)
		if after := snapshotDataset(ds); after != before {
			t.Errorf("%s mutated the shared dataset: %q -> %q", name, before, after)
		}
	}
}

// snapshotDataset renders the dataset's observable content, so a mutation anywhere in it shows
// up as a changed string rather than needing a field-by-field comparison that a new field
// could silently escape.
func snapshotDataset(ds *Dataset) string {
	out := ""
	for _, e := range ds.Events {
		out += e.EventID + "@" + e.Computer + "#" + e.Timestamp.String() + ";"
	}
	for _, scope := range consts.CorrelationScopes {
		for _, g := range ds.Groups(scope) {
			out += "|" + scope + ":" + g.Value + "="
			for _, e := range g.Events {
				out += e.EventID + ","
			}
		}
	}
	return out
}

// TestGenerateIsDeterministicAcrossInputOrder pins the property every algorithm must hold:
// the same events in a different order produce the same edges. Without it, two builds of one
// case could disagree, and the match cap would drop a different tail each time.
func TestGenerateIsDeterministicAcrossInputOrder(t *testing.T) {
	forward := []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		ev(2, "4625", "HOST-B", 1),
		ev(3, "4624", "HOST-A", 2),
		ev(4, "4624", "HOST-B", 3),
	}
	reversed := make([]*graphene.Event, len(forward))
	for i, e := range forward {
		reversed[len(forward)-1-i] = e
	}

	spec := &rules.Spec{Name: "Order Probe", Sequence: []string{"4625", "4624"}}
	a := Generate(spec, forward)
	b := Generate(spec, reversed)

	if a.Matches != b.Matches || len(a.Relations) != len(b.Relations) {
		t.Fatalf("input order changed the result: %d/%d vs %d/%d",
			a.Matches, len(a.Relations), b.Matches, len(b.Relations))
	}
	for i := range a.Relations {
		if a.Relations[i].From != b.Relations[i].From || a.Relations[i].To != b.Relations[i].To {
			t.Fatalf("relation %d differs: %d->%d vs %d->%d", i,
				a.Relations[i].From, a.Relations[i].To, b.Relations[i].From, b.Relations[i].To)
		}
	}
}
