//go:build integration

package integration

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"rohy/backend/api"
	"rohy/backend/consts"
	"rohy/backend/evtx"
	"rohy/backend/graphbuild"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
)

var update = flag.Bool("update", false, "rewrite testdata/security-golden.json from this run")

// fixture is the committed log, as ONE absolute path. The path is part of a dated event's
// identity, so two cases are only comparable when both were ingested from the same string.
var fixture = func() string {
	p, err := filepath.Abs(filepath.Join("..", "evtx", "testdata", "Security.evtx"))
	if err != nil {
		panic(err)
	}
	return p
}()

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// testCase is one case folder: a disk store, its graph registry and its rule library.
type testCase struct {
	t      *testing.T
	dir    string
	store  *graphene.Store
	graphs *graphreg.Store
	rules  *rules.Registry
}

func newCase(t *testing.T) *testCase {
	t.Helper()
	c := &testCase{t: t, dir: t.TempDir()}
	c.open()
	// Registered after TempDir's own cleanup, so it runs first: the folder is removed only once
	// nothing holds a file in it, which Windows insists on.
	t.Cleanup(c.close)
	graphs, err := graphreg.Open(filepath.Join(c.dir, "graphs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graphs.EnsureDefault(consts.DefaultGraphName, now); err != nil {
		t.Fatal(err)
	}
	reg, err := rules.Open(filepath.Join(c.dir, "rules"))
	if err != nil {
		t.Fatal(err)
	}
	c.graphs, c.rules = graphs, reg
	return c
}

func (c *testCase) open() {
	c.t.Helper()
	s, err := graphene.Open(filepath.Join(c.dir, "store"))
	if err != nil {
		c.t.Fatal(err)
	}
	c.store = s
}

func (c *testCase) close() {
	if c.store == nil {
		return
	}
	if err := c.store.Close(); err != nil {
		c.t.Errorf("close: %v", err)
	}
	c.store = nil
}

// reopen closes the store WITHOUT compacting, as a crash or a kill would, and opens it again.
func (c *testCase) reopen() { c.close(); c.open() }

// ingest reads the fixture into sink, the case's own store when sink is nil, the way the app's file
// ingest does.
func (c *testCase) ingest(sink evtx.EventSink) evtx.Summary {
	c.t.Helper()
	if sink == nil {
		sink = c.store
	}
	sum, err := evtx.Ingest(context.Background(), evtx.Options{
		Source: consts.SourceFile, Path: fixture,
		SourceType: consts.SourceTypeSingleEVTX, SourceIdentifier: fixture, Idempotent: true,
	}, sink, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	return sum
}

// ingestThroughTheApp starts an ingest the way the frontend does and waits for it, seal included.
func (c *testCase) ingestThroughTheApp() {
	c.t.Helper()
	events := api.NewEventsAPI(c.store, nil, nil)
	if err := events.StartIngest(api.IngestRequest{Source: consts.SourceFile, Paths: []string{fixture}, Idempotent: true}); err != nil {
		c.t.Fatal(err)
	}
	// The binding cannot report to a package outside api, so the test polls. IsIngesting turns
	// false only after the run has sealed the case.
	for deadline := time.Now().Add(60 * time.Second); events.IsIngesting(); {
		if time.Now().After(deadline) {
			c.t.Fatal("the ingest did not finish within 60s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *testCase) build(at time.Time) graphbuild.Result {
	c.t.Helper()
	res, err := graphbuild.New(c.store, c.graphs, c.rules).Run(context.Background(), graphbuild.Request{}, at)
	if err != nil {
		c.t.Fatal(err)
	}
	return res
}

// events lists every event the case holds.
func (c *testCase) events() []*graphene.Event {
	c.t.Helper()
	evs, err := c.store.QueryEvents(graphene.EventFilter{Limit: 1 << 20, Undated: consts.UndatedInclude})
	if err != nil {
		c.t.Fatal(err)
	}
	return evs
}

// eventKey names an event by its raw record, which does not depend on where the file was read from.
func eventKey(e *graphene.Event) string { return e.HashRaw[:16] }

// idsToHashes lists the case's node ids with the record each one holds, in id order.
func (c *testCase) idsToHashes() []string {
	evs := c.events()
	slices.SortFunc(evs, func(a, b *graphene.Event) int { return cmp.Compare(a.ID, b.ID) })
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = fmt.Sprintf("%d=%s", e.ID, eventKey(e))
	}
	return out
}

func (c *testCase) relations() []*graphene.Relation {
	c.t.Helper()
	rels, err := c.store.GetRelations()
	if err != nil {
		c.t.Fatal(err)
	}
	return rels
}

// contentKeys describes each relation by what it says, with node ids replaced by the records they
// hold: comparable across two cases. The relation's own id and CreatedAt are left out, because a
// rebuild writes new edges at a new time.
func (c *testCase) contentKeys(rels []*graphene.Relation) []string {
	c.t.Helper()
	byID := map[uint64]string{}
	for _, e := range c.events() {
		byID[e.ID] = eventKey(e)
	}
	node := func(id uint64) string {
		k, ok := byID[id]
		if !ok {
			c.t.Fatalf("relation endpoint %d is not an event", id)
		}
		return k
	}
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, relationKey(r, node(r.From), node(r.To), contentMatchID(c.t, r.MatchID, node)))
	}
	slices.Sort(out)
	return out
}

// rawKeys describes each relation with its node ids, match_id and graph as stored: comparable
// within one case, or across cases whose ids must agree.
func rawKeys(rels []*graphene.Relation) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, fmt.Sprintf("g%d|", r.GraphID)+relationKey(r, strconv.FormatUint(r.From, 10), strconv.FormatUint(r.To, 10), r.MatchID))
	}
	slices.Sort(out)
	return out
}

func relationKey(r *graphene.Relation, from, to, matchID string) string {
	return strings.Join([]string{
		r.RuleID, r.Algorithm, r.RelationType, r.Label, strconv.FormatFloat(r.ConfidenceScore, 'g', -1, 64), r.CreatedBy,
		from, to, strconv.Itoa(r.StepIndex), strings.Join(r.Basis, ";"), matchID, strconv.Itoa(r.RelVersion),
	}, "|")
}

// contentMatchID rewrites a match id, m-<first node>-<last node>-<steps>, through the records.
func contentMatchID(t *testing.T, id string, node func(uint64) string) string {
	t.Helper()
	parts := strings.Split(id, "-")
	if len(parts) != 4 || parts[0] != "m" {
		t.Fatalf("match id %q is not m-<first>-<last>-<steps>", id)
	}
	first, err1 := strconv.ParseUint(parts[1], 10, 64)
	last, err2 := strconv.ParseUint(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		t.Fatalf("match id %q does not name two nodes", id)
	}
	return "m-" + node(first) + "-" + node(last) + "-" + parts[3]
}

// golden is what the built-in rules find in the fixture.
type golden struct {
	Events    int                    `json:"events"`
	ByEventID map[string]int         `json:"by_event_id"`
	Rules     map[string]ruleOutcome `json:"rules"`
	Relations []string               `json:"relations"`
}

type ruleOutcome struct {
	Matches   int  `json:"matches"`
	Relations int  `json:"relations"`
	Truncated bool `json:"truncated"`
}

const goldenPath = "testdata/security-golden.json"

func readGolden(t *testing.T) golden {
	t.Helper()
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	var g golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// observe records what a case holds after a build, in the golden file's terms. Only built-in rules
// are counted: a rule a test adds is that test's business.
func (c *testCase) observe(res graphbuild.Result) golden {
	c.t.Helper()
	g := golden{ByEventID: map[string]int{}, Rules: map[string]ruleOutcome{}}
	for _, e := range c.events() {
		g.Events++
		g.ByEventID[e.EventID]++
	}
	builtin := map[string]bool{}
	for _, r := range c.rules.List() {
		if r.Source == consts.RuleSourceBuiltin {
			builtin[r.ID] = true
		}
	}
	for _, o := range res.Outcomes {
		if builtin[o.RuleID] {
			g.Rules[o.RuleID] = ruleOutcome{Matches: o.Matches, Relations: o.Relations, Truncated: o.Truncated}
		}
	}
	var rels []*graphene.Relation
	for _, r := range c.relations() {
		if builtin[r.RuleID] {
			rels = append(rels, r)
		}
	}
	g.Relations = c.contentKeys(rels)
	return g
}

// checkGolden compares an observation with the golden file, or rewrites the file under -update.
func checkGolden(t *testing.T, got golden) {
	t.Helper()
	if *update {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s", goldenPath)
		return
	}
	want := readGolden(t)
	if got.Events != want.Events || !reflect.DeepEqual(got.ByEventID, want.ByEventID) {
		t.Errorf("events: %d %v, golden %d %v", got.Events, got.ByEventID, want.Events, want.ByEventID)
	}
	for id, w := range want.Rules {
		if g, ok := got.Rules[id]; !ok || g != w {
			t.Errorf("rule %s: %+v, golden %+v", id, got.Rules[id], w)
		}
	}
	for id := range got.Rules {
		if _, ok := want.Rules[id]; !ok {
			t.Errorf("rule %s is not in the golden file", id)
		}
	}
	diffSets(t, "relations against the golden file", got.Relations, want.Relations)
}

// diffSets reports what one sorted set has that the other lacks.
func diffSets(t *testing.T, what string, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	in := func(s []string) map[string]bool {
		m := map[string]bool{}
		for _, v := range s {
			m[v] = true
		}
		return m
	}
	g, w := in(got), in(want)
	for _, v := range got {
		if !w[v] {
			t.Errorf("%s: extra   %s", what, v)
		}
	}
	for _, v := range want {
		if !g[v] {
			t.Errorf("%s: missing %s", what, v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s: %d entries, want %d", what, len(got), len(want))
	}
}

// relationsOfRules lists the relations whose rule is one of ids.
func relationsOfRules(rels []*graphene.Relation, ids ...string) []*graphene.Relation {
	var out []*graphene.Relation
	for _, r := range rels {
		if slices.Contains(ids, r.RuleID) {
			out = append(out, r)
		}
	}
	return out
}

// noDuplicates checks the edges the store counts against the edges the rule index lists: a
// duplicate written by a rebuild or a repair shows up as a difference.
func (c *testCase) noDuplicates(res graphbuild.Result) {
	c.t.Helper()
	_, edges, err := c.store.Stats()
	if err != nil {
		c.t.Fatal(err)
	}
	listed := 0
	for _, o := range res.Outcomes {
		rels, err := c.store.RelationsByRule(o.RuleID)
		if err != nil {
			c.t.Fatal(err)
		}
		listed += len(rels)
	}
	if int(edges) != listed {
		c.t.Errorf("the store holds %d edges; the rules' indexes list %d", edges, listed)
	}
}
