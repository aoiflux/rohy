package graphbuild

import (
	"testing"
	"time"

	"rohy/backend/autograph"
	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// The v0.1.0 compatibility gate.
//
// v0.2.0 changes a great deal underneath a rule: the algorithm interface now takes a prepared
// dataset, scope grouping and chronological sorting moved out of the matcher, every relation
// gained provenance, and the rule format went to version 2.
//
// None of that is allowed to change what an existing rule MATCHES. A case built with v0.1.0
// and rebuilt with v0.2.0 must produce the same edges between the same events — otherwise the
// upgrade silently rewrites conclusions an analyst has already drawn, which is the one failure
// this project cannot absorb.
//
// These tests pin that, and they are the reason the sequence algorithm's own test file was
// left untouched by the refactor.

// TestV1RulesProduceIdenticalEdges runs every built-in rule — all of which are format version
// 1 — and asserts the edges match what the pre-refactor matcher produced.
func TestV1RulesProduceIdenticalEdges(t *testing.T) {
	reg := loadBuiltinRegistry(t)

	// A dataset with several hosts, interleaved so that scope isolation and chronological
	// ordering both matter, and deliberately supplied out of order.
	events := v1Fixture()

	for _, rule := range reg.List() {
		// Filtered by ALGORITHM, not by format version. The format has one version, so a
		// version filter now selects everything — including the field, temporal and lineage
		// rules, which this reference implementation does not model and which it would either
		// mis-compare or (for a rule with no sequence at all) index straight off the end of.
		//
		// v0.1.0 had exactly one matcher, so "what v0.1.0 would have produced" is defined only
		// for sequence rules. That is what this test is about.
		if rule.AlgorithmOrDefault() != consts.AlgoSequence {
			continue
		}
		// The reference: match the rule by hand, with the semantics v0.1.0 had — group by
		// computer, sort chronologically, greedy non-overlapping subsequence.
		want := referenceSequenceMatch(rule.Sequence, events)

		got := runRuleEdges(t, &rule.Spec, events)
		if len(got) != len(want) {
			t.Errorf("%s: %d edges, reference produced %d", rule.ID, len(got), len(want))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: edge %d = %v, reference %v", rule.ID, i, got[i], want[i])
			}
		}
	}
}

// TestV1RuleUnaffectedByV2FieldsBeingPresent checks the other half of the promise: the new
// fields exist on every Spec now, and an algorithm that does not read them must be unaffected
// by their presence.
func TestV1RuleUnaffectedByV2FieldsBeingPresent(t *testing.T) {
	events := v1Fixture()

	plain := &rules.Spec{Name: "Plain", Sequence: []string{"4625", "4624"},
		RelationType: consts.RelationCorrelation, Algorithm: consts.AlgoSequence}
	// The same rule carrying fields only other algorithms read. Validation reports these as
	// advisories; the matcher must ignore them completely.
	decorated := *plain
	decorated.MatchFields = []string{"logon_id"}
	decorated.WindowWithin = "5m"
	decorated.LineageDepth = 3

	a := runRuleEdges(t, plain, events)
	b := runRuleEdges(t, &decorated, events)

	if len(a) != len(b) {
		t.Fatalf("inert fields changed the result: %d vs %d edges", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("inert fields changed edge %d: %v vs %v", i, a[i], b[i])
		}
	}
}

// TestRebuildIsByteIdenticalIncludingProvenance pins idempotency at its strongest: running the
// same rule twice over the same events produces not just the same edges but the same match
// ids. Without that, a rebuild would look like a change and two builds could not be compared.
func TestRebuildIsByteIdenticalIncludingProvenance(t *testing.T) {
	events := v1Fixture()
	spec := &rules.Spec{Name: "Repeatable", Sequence: []string{"4625", "4624"},
		RelationType: consts.RelationCorrelation}

	first := runRuleRelations(t, spec, events)
	second := runRuleRelations(t, spec, events)

	if len(first) != len(second) {
		t.Fatalf("rebuild produced a different edge count: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].From != second[i].From || first[i].To != second[i].To ||
			first[i].MatchID != second[i].MatchID || first[i].StepIndex != second[i].StepIndex {
			t.Fatalf("rebuild differs at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
}

// --- helpers ---

func loadBuiltinRegistry(t *testing.T) *rules.Registry {
	t.Helper()
	reg, err := rules.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	return reg
}

// v1Fixture is a deliberately awkward event set: three hosts, interleaved, supplied in an
// order that is neither chronological nor grouped.
func v1Fixture() []*graphene.Event {
	base := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	ids := []string{"4625", "4624", "4720", "4732", "1102", "7045", "4672", "4698", "4726", "4740"}
	hosts := []string{"HOST-A", "HOST-B", "HOST-C"}

	var out []*graphene.Event
	for i := range 300 {
		out = append(out, &graphene.Event{
			ID:        uint64(i + 1),
			EventID:   ids[(i*7)%len(ids)],
			Computer:  hosts[(i*3)%len(hosts)],
			Timestamp: base.Add(time.Duration((i*37)%1000) * time.Second),
		})
	}
	return out
}

// runRuleRelations evaluates a rule through the real engine — the same call the build
// workflow makes — and returns the unpersisted relations. Persistence is covered by the
// builder's own tests; what matters here is which edges the matcher produces.
func runRuleRelations(t *testing.T, spec *rules.Spec, events []*graphene.Event) []graphene.Relation {
	t.Helper()
	return autograph.Generate(spec, events).Relations
}

func runRuleEdges(t *testing.T, spec *rules.Spec, events []*graphene.Event) [][2]uint64 {
	t.Helper()
	rels := runRuleRelations(t, spec, events)
	out := make([][2]uint64, len(rels))
	for i, r := range rels {
		out[i] = [2]uint64{r.From, r.To}
	}
	return out
}

// referenceSequenceMatch is an independent reimplementation of v0.1.0's matcher, kept
// deliberately naive: group by computer, sort, greedy non-overlapping scan. Its whole value is
// that it shares no code with the implementation it checks.
func referenceSequenceMatch(seq []string, events []*graphene.Event) [][2]uint64 {
	byHost := map[string][]*graphene.Event{}
	for _, e := range events {
		if e.Timestamp.IsZero() {
			continue
		}
		byHost[e.Computer] = append(byHost[e.Computer], e)
	}
	hosts := make([]string, 0, len(byHost))
	for h := range byHost {
		hosts = append(hosts, h)
	}
	sortStrings(hosts)

	var out [][2]uint64
	for _, h := range hosts {
		scoped := byHost[h]
		sortEvents(scoped)
		start := 0
		for start+len(seq) <= len(scoped) {
			matched := make([]int, 0, len(seq))
			step := 0
			for i := start; i < len(scoped) && step < len(seq); i++ {
				if scoped[i].EventID == seq[step] {
					matched = append(matched, i)
					step++
				}
			}
			if step != len(seq) {
				break
			}
			for k := 0; k+1 < len(matched); k++ {
				out = append(out, [2]uint64{scoped[matched[k]].ID, scoped[matched[k+1]].ID})
			}
			start = matched[len(matched)-1] + 1
		}
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sortEvents(e []*graphene.Event) {
	for i := 1; i < len(e); i++ {
		for j := i; j > 0 && lessEvent(e[j], e[j-1]); j-- {
			e[j], e[j-1] = e[j-1], e[j]
		}
	}
}

func lessEvent(a, b *graphene.Event) bool {
	if a.Timestamp.Equal(b.Timestamp) {
		return a.ID < b.ID
	}
	return a.Timestamp.Before(b.Timestamp)
}
