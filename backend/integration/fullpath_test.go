//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"rohy/backend/api"
	"rohy/backend/consts"
	"rohy/backend/graphbuild"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// userRuleID is the rule the suite adds to a case, so a rule export carries something the
// receiving case does not already have built in.
const userRuleID = "explicit-credentials-then-logon"

// 🔒 The whole path an analyst takes, through the app's own bindings on the disk backend: ingest
// the fixture, query it, run every built-in rule, export the rule library, import it into a second
// case, and get the same graph there.
func TestTheRealPipelineEndToEnd(t *testing.T) {
	a := newCase(t)
	addUserRule(t, a)
	a.ingestThroughTheApp()

	n, err := a.store.CountEvents(graphene.EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if n < 700 {
		t.Fatalf("the case holds %d events", n)
	}

	resA, err := api.NewBuildAPI(graphbuild.New(a.store, a.graphs, a.rules)).RunRules(api.BuildRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range resA.Outcomes {
		if o.Truncated {
			t.Errorf("%s reached the match cap", o.RuleID)
		}
	}
	checkGolden(t, a.observe(resA))
	a.noDuplicates(resA)
	if len(relationsOfRules(a.relations(), userRuleID)) == 0 {
		t.Fatal("the added rule matched nothing, so the export below would prove nothing about it")
	}

	// Export the whole library, write each rule where an analyst would save it, and import it.
	bundle, err := api.NewRulesAPI(a.rules).ExportRules(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Missing) > 0 || len(bundle.Rules) != len(a.rules.List()) {
		t.Fatalf("exported %d of %d rules; missing %v", len(bundle.Rules), len(a.rules.List()), bundle.Missing)
	}
	out := t.TempDir()
	var paths []string
	for i, src := range bundle.Rules {
		p := filepath.Join(out, fmt.Sprintf("%02d-%s", i, src.File))
		if err := os.WriteFile(p, []byte(src.Source), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	b := newCase(t)
	imported, err := b.rules.Import(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Errors) > 0 {
		t.Fatalf("import refused rules: %+v", imported.Errors)
	}
	for _, src := range bundle.Rules {
		got, err := b.rules.Source(src.ID)
		if err != nil {
			t.Fatalf("%s did not arrive: %v", src.ID, err)
		}
		// Byte for byte: the bundle carries each file as stored, never a re-serialization.
		if got.Source != src.Source {
			t.Errorf("%s changed on the way through", src.ID)
		}
		if got.Origin != consts.RuleSourceUser {
			t.Errorf("%s arrived as %s, want a user rule", src.ID, got.Origin)
		}
	}

	b.ingestThroughTheApp()
	if _, err := api.NewBuildAPI(graphbuild.New(b.store, b.graphs, b.rules)).RunRules(api.BuildRequest{}); err != nil {
		t.Fatal(err)
	}
	diffSets(t, "case B against case A", b.contentKeys(b.relations()), a.contentKeys(a.relations()))
}

// addUserRule writes the suite's own rule into the case's library.
func addUserRule(t *testing.T, c *testCase) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "user-rule.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.rules.Dir(), userRuleID+consts.RuleFileExt), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.rules.Reload(); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(c.rules.List(), func(r *rules.Rule) bool {
		return r.ID == userRuleID && r.Source == consts.RuleSourceUser && r.Enabled
	}) {
		t.Fatalf("%s did not load: %+v", userRuleID, c.rules.Invalids())
	}
}
