//go:build integration

package integration

import (
	"slices"
	"testing"

	"rohy/backend/api"
	"rohy/backend/graphene"
)

// legacySink stores events the way a build before correlation keys did: without them.
type legacySink struct{ *graphene.Store }

func (s legacySink) InsertEvents(evs []*graphene.Event) ([]uint64, error) {
	for _, e := range evs {
		e.CorrKeys, e.CorrKeyVersion = nil, 0
	}
	return s.Store.InsertEvents(evs)
}

// The rules that read correlation keys: the field rule matches on the logon session, and lineage
// resolves parents through process ids.
var keyedRules = []string{"logon-then-process-creation-same-session", "process-ancestry"}

// 🔒 A case ingested without correlation keys says its keyed rules are blocked rather than that
// they found nothing, and a backfill gives it exactly the graph and the keys of a case ingested
// with them.
func TestABackfilledCaseMatchesOneIngestedWithKeys(t *testing.T) {
	want := readGolden(t)

	c := newCase(t)
	c.ingest(legacySink{c.store})
	status, err := c.store.CorrelationKeyStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.Total != want.Events || status.Stale != status.Total {
		t.Fatalf("status %+v: the case was not left without keys, so the test would prove nothing", status)
	}

	before := c.build(now)
	if before.StaleCorrelationKeys != want.Events {
		t.Errorf("the build reported %d events without keys, want %d", before.StaleCorrelationKeys, want.Events)
	}
	for _, o := range before.Outcomes {
		w := want.Rules[o.RuleID]
		switch {
		case slices.Contains(keyedRules, o.RuleID):
			if w.Relations == 0 {
				t.Fatalf("%s finds nothing in the fixture even with keys", o.RuleID)
			}
			if o.Relations != 0 {
				t.Errorf("%s wrote %d relations without keys", o.RuleID, o.Relations)
			}
		case o.Relations != w.Relations:
			t.Errorf("%s: %d relations without keys, %d with them; it should not read keys", o.RuleID, o.Relations, w.Relations)
		}
	}

	c.reopen()
	res, err := api.NewMaintenanceAPI(c.store).BackfillCorrelationKeys()
	if err != nil {
		t.Fatal(err)
	}
	if res.Projected != want.Events || res.Failed != 0 {
		t.Fatalf("backfill %+v, want %d projected and none failed", res, want.Events)
	}
	checkGolden(t, c.observe(c.build(now)))

	normal := newCase(t)
	normal.ingest(nil)
	keys := func(tc *testCase) map[string][]string {
		out := map[string][]string{}
		for _, e := range tc.events() {
			out[eventKey(e)] = e.CorrKeys
		}
		return out
	}
	got, ref := keys(c), keys(normal)
	for k, v := range ref {
		if !slices.Equal(got[k], v) {
			t.Errorf("record %s: backfilled keys %q, ingested keys %q", k, got[k], v)
		}
	}
}
