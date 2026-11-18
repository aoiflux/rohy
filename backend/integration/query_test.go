//go:build integration

package integration

import (
	"maps"
	"strings"
	"testing"

	"rohy/backend/api"
	"rohy/backend/consts"
	"rohy/backend/query"
)

// The query console over the real case: what it counts is what the golden file says the
// pipeline produced, through the binding the page calls.
func TestTheConsoleAgreesWithTheGoldenCase(t *testing.T) {
	c := newCase(t)
	c.ingest(nil)
	c.build(now)
	g := readGolden(t)
	q := api.NewQueryAPI(c.store)
	run := func(spec query.Spec) query.Result {
		t.Helper()
		res, err := q.Run(spec)
		if err != nil {
			t.Fatalf("%+v: %v", spec, err)
		}
		if res.Changed || res.Truncated {
			t.Fatalf("%+v: changed %v, truncated %v", spec, res.Changed, res.Truncated)
		}
		return res
	}
	counts := func(res query.Result) map[string]int {
		out := map[string]int{}
		for _, gr := range res.Groups {
			if gr.Redacted {
				t.Fatalf("a redacted group in a case nothing was redacted from: %+v", gr)
			}
			out[gr.Value] = gr.Count
		}
		return out
	}

	byID := run(query.Spec{Subject: consts.QuerySubjectEvents, GroupBy: consts.PropEventID, Limit: consts.QueryMaxLimit})
	if got := counts(byID); !maps.Equal(got, g.ByEventID) || byID.Total != g.Events || byID.Undated != consts.QueryUndatedInclude {
		t.Errorf("events by id: %v (total %d, undated %q), want %v (total %d)", got, byID.Total, byID.Undated, g.ByEventID, g.Events)
	}

	wantRules := map[string]int{}
	for id, o := range g.Rules {
		if o.Relations > 0 {
			wantRules[id] = o.Relations
		}
	}
	byRule := run(query.Spec{Subject: consts.QuerySubjectRelations, GroupBy: consts.PropRuleID})
	if got := counts(byRule); !maps.Equal(got, wantRules) {
		t.Errorf("relations by rule: %v, want %v", got, wantRules)
	}

	wantAlgos := map[string]int{}
	for _, key := range g.Relations {
		wantAlgos[strings.Split(key, "|")[1]]++
	}
	byAlgo := run(query.Spec{Subject: consts.QuerySubjectRelations, GroupBy: consts.PropAlgorithm})
	if got := counts(byAlgo); !maps.Equal(got, wantAlgos) || byAlgo.Total != len(g.Relations) {
		t.Errorf("relations by algorithm: %v (total %d), want %v (total %d)", got, byAlgo.Total, wantAlgos, len(g.Relations))
	}

	// One rule's rows, through the index, carry the rule that made them.
	const rule = "process-ancestry"
	rows := run(query.Spec{Subject: consts.QuerySubjectRelations, Where: []query.Condition{{Field: consts.PropRuleID, Op: consts.QueryOpEq, Value: rule}}})
	if rows.Total != g.Rules[rule].Relations || len(rows.Relations) != rows.Total {
		t.Fatalf("%s: %d rows of %d, want %d", rule, len(rows.Relations), rows.Total, g.Rules[rule].Relations)
	}
	for _, r := range rows.Relations {
		if r.RuleID != rule || r.Algorithm != consts.AlgoLineage || r.MatchID == "" || len(r.Basis) == 0 {
			t.Errorf("row %+v", r)
		}
	}

	// A capped list says so, and its total is the case's.
	capped, err := q.Run(query.Spec{Subject: consts.QuerySubjectEvents, Limit: 5})
	if err != nil || len(capped.Events) != 5 || capped.Total != g.Events || !capped.Truncated {
		t.Errorf("%d rows of %d, truncated %v, %v", len(capped.Events), capped.Total, capped.Truncated, err)
	}
}
