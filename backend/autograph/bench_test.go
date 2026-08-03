package autograph

import (
	"fmt"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// Correlation benchmarks.
//
// The claim v0.2.0 makes about the prepared dataset is specific and measurable: N rules should
// cost ONE sort of the case, not N. Before the dataset existed, every rule dropped undated
// events, grouped by scope and sorted each group itself, so a build of twenty rules sorted the
// same data twenty times.
//
// BenchmarkBuild below is what holds that claim to account. It is a comparison, not an absolute
// number: run it and the per-rule cost should fall as the rule count rises, because the sort is
// amortised across the whole run rather than repeated per rule.
//
// Run: go test ./backend/autograph/ -bench=. -benchmem -run=XXX

var benchBase = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

// benchEvents builds a realistic-ish case: several hosts, a spread of event ids, correlation
// keys populated so the field and lineage matchers have something to work with.
func benchEvents(n int) []*graphene.Event {
	ids := []string{"4624", "4625", "4688", "4672", "4634", "4720", "4732", "1102", "7045", "4689"}
	hosts := []string{"HOST-A", "HOST-B", "HOST-C", "HOST-D"}

	out := make([]*graphene.Event, 0, n)
	for i := range n {
		e := &graphene.Event{
			ID:        uint64(i + 1),
			EventID:   ids[i%len(ids)],
			Computer:  hosts[i%len(hosts)],
			Timestamp: benchBase.Add(time.Duration(i%9973) * time.Second),
		}
		// A bounded number of distinct sessions and PIDs, so buckets have real depth rather
		// than every event landing in a bucket of one.
		e.ParsedFields = map[string]string{
			"TargetLogonId":  fmt.Sprintf("0x%x", 0x1000+i%512),
			"SubjectLogonId": fmt.Sprintf("0x%x", 0x1000+i%512),
			"TargetUserName": fmt.Sprintf("user%d", i%64),
			"ProcessId":      fmt.Sprintf("0x%x", 0x100+i%256),
			"NewProcessId":   fmt.Sprintf("0x%x", 0x100+(i+1)%256),
			"NewProcessName": `C:\Windows\System32\svchost.exe`,
		}
		e.ComputeCorrelationKeys()
		out = append(out, e)
	}
	return out
}

func benchSpec(algorithm string, i int) *rules.Spec {
	s := &rules.Spec{
		Name:         fmt.Sprintf("bench %s %d", algorithm, i),
		Algorithm:    algorithm,
		RelationType: consts.RelationCorrelation,
		Sequence:     []string{"4625", "4624"},
	}
	switch algorithm {
	case consts.AlgoField:
		s.MatchFields = []string{"logon_id"}
	case consts.AlgoTemporal:
		s.WindowWithin = "5m"
	case consts.AlgoLineage:
		s.Sequence = nil
	}
	return s
}

func BenchmarkPrepare(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		events := benchEvents(n)
		req := Requirements{Scopes: []string{consts.ScopeComputer}}
		b.Run(fmt.Sprintf("%dk", n/1000), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				Prepare(events, req)
			}
		})
	}
}

func BenchmarkGenerate(b *testing.B) {
	events := benchEvents(50_000)
	for _, algorithm := range consts.AlgorithmNames() {
		spec := benchSpec(algorithm, 0)
		ds := Prepare(events, RequirementsFor([]*rules.Spec{spec}))
		b.Run(algorithm, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				GenerateWith(spec, ds)
			}
		})
	}
}

// BenchmarkBuild is the one that matters: the whole per-run cost for a growing number of rules,
// preparing ONCE the way graphbuild does.
//
// Divide each result by its rule count and the per-rule cost should drop as the count rises —
// that falling curve IS the shared sort. If preparation ever moves back inside the per-rule
// loop, the per-rule cost goes flat and this benchmark is where it shows.
func BenchmarkBuild(b *testing.B) {
	events := benchEvents(50_000)

	for _, count := range []int{1, 5, 20} {
		specs := make([]*rules.Spec, count)
		for i := range specs {
			// A mix, so the measurement is not one algorithm's cost wearing a build's name.
			specs[i] = benchSpec(consts.AlgorithmNames()[i%len(consts.AlgorithmNames())], i)
		}
		b.Run(fmt.Sprintf("%drules", count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				ds := Prepare(events, RequirementsFor(specs))
				for _, spec := range specs {
					GenerateWith(spec, ds)
				}
			}
		})
	}
}

// BenchmarkBuildPreparingPerRule is the shape v0.1.0 had, kept as the comparison that gives
// BenchmarkBuild its meaning. It is not how the engine runs — it is what the engine used to
// cost, so the win can be stated as a number rather than asserted in a comment.
func BenchmarkBuildPreparingPerRule(b *testing.B) {
	events := benchEvents(50_000)

	for _, count := range []int{1, 5, 20} {
		specs := make([]*rules.Spec, count)
		for i := range specs {
			specs[i] = benchSpec(consts.AlgorithmNames()[i%len(consts.AlgorithmNames())], i)
		}
		b.Run(fmt.Sprintf("%drules", count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, spec := range specs {
					Generate(spec, events) // prepares its own dataset, as every rule used to
				}
			}
		})
	}
}
