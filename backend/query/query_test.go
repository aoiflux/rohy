package query

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

var base = time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)

// seeded is a small case: eight events (one undated) and four relations from two rules.
func seeded(t testing.TB) (*graphene.Store, []uint64) {
	t.Helper()
	s := graphene.OpenInMemory()
	t.Cleanup(func() { s.Close() })
	var evs []*graphene.Event
	for i := range 8 {
		ts := base.Add(time.Duration(i) * time.Minute)
		if i == 7 {
			ts = time.Time{}
		}
		user := []string{"alice", "bob"}[i%2]
		evs = append(evs, &graphene.Event{
			EventID: []string{"4624", "4625", "4688"}[i%3], Timestamp: ts, Provider: "P", Channel: "Security",
			Computer: "HOST-1", User: user, HashRaw: "r" + string(rune('a'+i)), HashNormalized: "n" + string(rune('a'+i)),
			SourceType: consts.SourceTypeSingleEVTX, SourceIdentifier: `C:\logs\Security.evtx`,
		})
	}
	ids, err := s.InsertEvents(evs)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range []struct {
		from, to int
		rule     string
		algo     string
	}{{0, 1, "rule-a", consts.AlgoSequence}, {1, 2, "rule-a", consts.AlgoSequence}, {2, 3, "rule-b", consts.AlgoField}, {3, 4, "rule-a", consts.AlgoSequence}} {
		rel := &graphene.Relation{
			From: ids[r.from], To: ids[r.to], GraphID: 1, RelationType: consts.RelationTemporal,
			CreatedBy: consts.CreatedBySystem, CreatedAt: base, RuleID: r.rule, Algorithm: r.algo,
			MatchID: "m-" + string(rune('0'+i)), Basis: []string{"Δt ≤ 5m"},
		}
		if _, err := s.InsertRelation(rel); err != nil {
			t.Fatal(err)
		}
	}
	return s, ids
}

func run(t *testing.T, r Reader, spec Spec) Result {
	t.Helper()
	res, err := Run(r, spec)
	if err != nil {
		t.Fatalf("Run(%+v): %v", spec, err)
	}
	return res
}

func where(conds ...string) []Condition {
	var out []Condition
	for i := 0; i+2 < len(conds); i += 3 {
		out = append(out, Condition{Field: conds[i], Op: conds[i+1], Value: conds[i+2]})
	}
	return out
}

// sample is a value of field's kind that Run accepts.
func sample(f Field) string {
	switch f.Kind {
	case consts.QueryKindEnum:
		return f.Values[0]
	case consts.QueryKindTime:
		return "2026-04-01T08:00:00Z"
	case consts.QueryKindNumber:
		return "1"
	}
	return "x"
}

// The vocabulary is the page's only source, so everything it offers must run.
func TestEverythingDescribeOffersRuns(t *testing.T) {
	s, _ := seeded(t)
	for _, sub := range Describe().Subjects {
		for _, f := range sub.Fields {
			for _, op := range f.Ops {
				run(t, s, Spec{Subject: sub.Key, Where: where(f.Key, op, sample(f))})
			}
			if f.Groupable {
				run(t, s, Spec{Subject: sub.Key, GroupBy: f.Key})
			}
		}
	}
}

func TestDescribeCannotBeChangedByItsCaller(t *testing.T) {
	v := Describe()
	v.Subjects[0].Fields[0].Ops[0] = "drop"
	v.Subjects[1].Fields[1].Values[0] = "evil"
	if again := Describe(); !reflect.DeepEqual(again.Subjects[0].Fields[0].Ops, []string{consts.QueryOpEq}) ||
		again.Subjects[1].Fields[1].Values[0] == "evil" {
		t.Fatal("a caller's edit to the vocabulary changed what Run accepts")
	}
}

func TestEveryRefusalSaysWhyAndIsTheInputsFault(t *testing.T) {
	s, _ := seeded(t)
	ev, rel := consts.QuerySubjectEvents, consts.QuerySubjectRelations
	tooMany := make([]Condition, consts.QueryMaxConditions+1)
	for i := range tooMany {
		tooMany[i] = Condition{Field: consts.PropUser, Op: consts.QueryOpEq, Value: "a"}
	}
	for _, tc := range []struct {
		name string
		spec Spec
		says string
	}{
		{"unknown subject", Spec{Subject: "nodes"}, "unknown query subject"},
		{"no subject", Spec{}, "unknown query subject"},
		{"unknown field", Spec{Subject: ev, Where: where("raw_xml", "eq", "x")}, `no field "raw_xml"`},
		{"a relation field on events", Spec{Subject: ev, Where: where(consts.PropRuleID, "eq", "x")}, "no field"},
		{"wrong op", Spec{Subject: ev, Where: where(consts.PropUser, "contains", "x")}, "does not take"},
		{"an op nobody takes", Spec{Subject: ev, Where: where(consts.PropUser, "matches", ".*")}, "does not take"},
		{"lte on occurrences", Spec{Subject: ev, Where: where(fieldOccurrences, "lte", "3")}, "does not take"},
		{"a field twice", Spec{Subject: ev, Where: where(consts.PropUser, "eq", "a", consts.PropUser, "eq", "b")}, "used twice"},
		{"a time end twice", Spec{Subject: ev, Where: where(fieldTime, "gte", "2026-01-01T00:00:00Z", fieldTime, "gte", "2026-02-01T00:00:00Z")}, "used twice"},
		{"too many", Spec{Subject: ev, Where: tooMany}, "at most"},
		{"empty value", Spec{Subject: ev, Where: where(consts.PropUser, "eq", "")}, "needs a value"},
		{"long value", Spec{Subject: ev, Where: where(consts.PropUser, "eq", strings.Repeat("a", consts.QueryMaxValueLen+1))}, "longer than"},
		{"enum outside", Spec{Subject: rel, Where: where(consts.PropCreatedBy, "eq", "robot")}, "is one of"},
		{"not a time", Spec{Subject: ev, Where: where(fieldTime, "gte", "yesterday")}, "needs a time"},
		{"a negative number", Spec{Subject: rel, Where: where(consts.PropGraphID, "eq", "-1")}, "whole number"},
		{"a fraction", Spec{Subject: ev, Where: where(fieldOccurrences, "gte", "1.5")}, "whole number"},
		{"an overflow", Spec{Subject: ev, Where: where(fieldOccurrences, "gte", "99999999999")}, "whole number"},
		{"a backwards range", Spec{Subject: ev, Where: where(fieldTime, "gte", "2026-02-01T00:00:00Z", fieldTime, "lte", "2026-01-01T00:00:00Z")}, "ends before"},
		{"group by an ungroupable field", Spec{Subject: ev, GroupBy: fieldTime}, "cannot be counted by"},
		{"group by an unknown field", Spec{Subject: rel, GroupBy: consts.PropUser}, "cannot be counted by"},
		{"a negative limit", Spec{Subject: ev, Limit: -1}, "the limit is"},
		{"a limit over the cap", Spec{Subject: ev, Limit: consts.QueryMaxLimit + 1}, "the limit is"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Run(s, tc.spec)
			var refused *RefusedError
			if !errors.As(err, &refused) {
				t.Fatalf("got %v, want a refusal", err)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("%q does not say %q", err, tc.says)
			}
		})
	}
}

func TestUndatedEventsAreIncludedUnlessAskedAndSaidBack(t *testing.T) {
	s, _ := seeded(t)
	for word, want := range map[string]int{"": 8, consts.QueryUndatedInclude: 8, consts.QueryUndatedExclude: 7, consts.QueryUndatedOnly: 1} {
		spec := Spec{Subject: consts.QuerySubjectEvents}
		if word != "" {
			spec.Where = where(fieldUndated, "eq", word)
		}
		res := run(t, s, spec)
		said := word
		if said == "" {
			said = consts.QueryUndatedInclude
		}
		if res.Total != want || res.Undated != said {
			t.Errorf("undated %q: total %d, said %q; want %d, %q", word, res.Total, res.Undated, want, said)
		}
	}
	res := run(t, s, Spec{Subject: consts.QuerySubjectEvents, Where: where(fieldUndated, "eq", consts.QueryUndatedOnly)})
	if len(res.Events) != 1 || res.Events[0].Timestamp != "" {
		t.Errorf("an undated row must not carry a date: %+v", res.Events)
	}
}

func TestRowsStopAtTheLimitAndTheTotalDoesNot(t *testing.T) {
	s, ids := seeded(t)
	res := run(t, s, Spec{Subject: consts.QuerySubjectEvents, Limit: 3})
	if len(res.Events) != 3 || res.Total != 8 || !res.Truncated {
		t.Fatalf("%d rows of %d, truncated %v", len(res.Events), res.Total, res.Truncated)
	}
	if got := []uint64{res.Events[0].ID, res.Events[1].ID, res.Events[2].ID}; !slices.Equal(got, ids[:3]) {
		t.Errorf("rows %v, want the first three in time order %v", got, ids[:3])
	}
	if res.Events[0].Timestamp != base.Format(time.RFC3339Nano) || res.Events[0].Occurrences != 1 {
		t.Errorf("row %+v", res.Events[0])
	}
	all := run(t, s, Spec{Subject: consts.QuerySubjectEvents})
	if len(all.Events) != 8 || all.Truncated {
		t.Errorf("the default limit truncated eight rows: %d, %v", len(all.Events), all.Truncated)
	}
}

func TestEventConditionsNarrowTheMatch(t *testing.T) {
	s, _ := seeded(t)
	for _, tc := range []struct {
		conds []Condition
		want  int
	}{
		{where(consts.PropUser, "eq", "alice"), 4},
		{where(consts.PropEventID, "eq", "4624", consts.PropUser, "eq", "alice"), 2}, // events 0 and 6
		{where(consts.PropComputer, "eq", "HOST-2"), 0},
		{where(fieldTime, "gte", base.Add(2*time.Minute).Format(time.RFC3339), fieldTime, "lte", base.Add(4*time.Minute).Format(time.RFC3339)), 3},
		{where(fieldRelations, "eq", consts.RelationFilterSystem), 5},
		{where(fieldText, "contains", "ALICE"), 4},
	} {
		if res := run(t, s, Spec{Subject: consts.QuerySubjectEvents, Where: tc.conds}); res.Total != tc.want {
			t.Errorf("%+v: %d, want %d", tc.conds, res.Total, tc.want)
		}
	}
}

func TestGroupsAreMostCommonFirstAndCapped(t *testing.T) {
	s, _ := seeded(t)
	res := run(t, s, Spec{Subject: consts.QuerySubjectEvents, GroupBy: consts.PropEventID})
	want := []Group{{Value: "4624", Count: 3}, {Value: "4625", Count: 3}, {Value: "4688", Count: 2}}
	if !reflect.DeepEqual(res.Groups, want) || res.Total != 8 || res.DistinctGroups != 3 || res.Truncated {
		t.Fatalf("%+v", res)
	}
	capped := run(t, s, Spec{Subject: consts.QuerySubjectEvents, GroupBy: consts.PropEventID, Limit: 2})
	if len(capped.Groups) != 2 || capped.DistinctGroups != 3 || !capped.Truncated || capped.Total != 8 {
		t.Fatalf("%+v", capped)
	}
	if len(res.Events) != 0 || res.Events == nil {
		t.Error("a count lists no rows, and says so with an empty list, not null")
	}
}

func TestRelationsAreFoundByProvenanceAndCounted(t *testing.T) {
	s, ids := seeded(t)
	res := run(t, s, Spec{Subject: consts.QuerySubjectRelations, Where: where(consts.PropRuleID, "eq", "rule-a")})
	if res.Total != 3 || len(res.Relations) != 3 {
		t.Fatalf("%+v", res)
	}
	r0 := res.Relations[0]
	if r0.From != ids[0] || r0.RuleID != "rule-a" || r0.Algorithm != consts.AlgoSequence || r0.MatchID != "m-0" ||
		!slices.Equal(r0.Basis, []string{"Δt ≤ 5m"}) || r0.CreatedAt == "" {
		t.Errorf("row %+v", r0)
	}
	byAlgo := run(t, s, Spec{Subject: consts.QuerySubjectRelations, GroupBy: consts.PropAlgorithm})
	if want := []Group{{Value: consts.AlgoSequence, Count: 3}, {Value: consts.AlgoField, Count: 1}}; !reflect.DeepEqual(byAlgo.Groups, want) {
		t.Errorf("%+v", byAlgo.Groups)
	}
	byGraph := run(t, s, Spec{Subject: consts.QuerySubjectRelations, GroupBy: consts.PropGraphID})
	if want := []Group{{Value: "1", Count: 4}}; !reflect.DeepEqual(byGraph.Groups, want) {
		t.Errorf("%+v", byGraph.Groups)
	}
	around := run(t, s, Spec{Subject: consts.QuerySubjectRelations, Where: where(fieldEvent, "eq", strconv.FormatUint(ids[1], 10))})
	if around.Total != 2 {
		t.Errorf("relations at event 2: %d", around.Total)
	}
	capped := run(t, s, Spec{Subject: consts.QuerySubjectRelations, Limit: 1})
	if len(capped.Relations) != 1 || capped.Total != 4 || !capped.Truncated {
		t.Errorf("%+v", capped)
	}
}

// moving is a store being written while a query reads it.
type moving struct {
	Reader
	version  uint64
	vanished int
}

func (m *moving) Version() uint64 { m.version++; return m.version }

func (m *moving) ScanRelations(f graphene.RelationFilter, visit func(*graphene.Relation) error) (graphene.ScanStats, error) {
	st, err := m.Reader.ScanRelations(f, visit)
	st.Vanished += m.vanished
	return st, err
}

type still struct{ Reader }

func (still) Version() uint64 { return 7 }

func TestAQueryOverAMovingCaseSaysSo(t *testing.T) {
	s, _ := seeded(t)
	if res := run(t, &moving{Reader: s}, Spec{Subject: consts.QuerySubjectEvents}); !res.Changed {
		t.Error("a write during the query went unreported")
	}
	if res := run(t, still{&moving{Reader: s, vanished: 1}}, Spec{Subject: consts.QuerySubjectRelations}); !res.Changed {
		t.Error("an edge that vanished mid-scan went unreported")
	}
	if res := run(t, s, Spec{Subject: consts.QuerySubjectRelations}); res.Changed {
		t.Error("a still case reported a change")
	}
}

type redactedRows struct{ Reader }

func (redactedRows) QueryEvents(graphene.EventFilter) ([]*graphene.Event, error) {
	return []*graphene.Event{{ID: 9, Redacted: true}}, nil
}

func TestARedactedEventSaysSo(t *testing.T) {
	s, _ := seeded(t)
	res := run(t, redactedRows{s}, Spec{Subject: consts.QuerySubjectEvents})
	if len(res.Events) != 1 || !res.Events[0].Redacted || res.Events[0].Timestamp != "" {
		t.Errorf("%+v", res.Events)
	}
}

// 🔒 The console reads only. Every method of Reader must be one of these; a method added to the
// interface fails here until someone has looked at whether it writes.
func TestTheReaderOnlyReads(t *testing.T) {
	reads := []string{"CountEvents", "CountEventsBy", "QueryEvents", "ScanRelations", "Version"}
	rt := reflect.TypeFor[Reader]()
	for i := range rt.NumMethod() {
		if name := rt.Method(i).Name; !slices.Contains(reads, name) {
			t.Errorf("Reader.%s is not a known read", name)
		}
	}
}

// FuzzRun feeds arbitrary specs to Run. Whatever arrives, nothing panics, nothing is written, and a
// result never shows more than it says it matched or more than the cap.
func FuzzRun(f *testing.F) {
	for _, spec := range []Spec{
		{Subject: consts.QuerySubjectEvents},
		{Subject: consts.QuerySubjectEvents, Where: where(consts.PropUser, "eq", "alice"), Limit: 2},
		{Subject: consts.QuerySubjectEvents, GroupBy: consts.PropUser},
		{Subject: consts.QuerySubjectEvents, Where: where(fieldTime, "gte", "2026-04-01T08:01:00Z", fieldUndated, "eq", "only")},
		{Subject: consts.QuerySubjectRelations, Where: where(consts.PropRuleID, "eq", "rule-a", fieldEvent, "eq", "2")},
		{Subject: consts.QuerySubjectRelations, GroupBy: consts.PropGraphID, Limit: 1000},
	} {
		data, _ := json.Marshal(spec)
		f.Add(data)
	}
	f.Add([]byte(`{"subject":"events","where":[{"field":"occurrences","op":"gte","value":"18446744073709551615"}]}`))
	f.Add([]byte(`{"subject":"relations","limit":-9223372036854775808}`))
	s, _ := seeded(f)
	before := s.Version()
	f.Fuzz(func(t *testing.T, data []byte) {
		var spec Spec
		if json.Unmarshal(data, &spec) != nil {
			return
		}
		res, err := Run(s, spec)
		if err != nil {
			if _, ok := errors.AsType[*RefusedError](err); !ok {
				t.Fatalf("%+v: a store error from a still store: %v", spec, err)
			}
			return
		}
		shown := len(res.Events) + len(res.Relations) + len(res.Groups)
		if shown > consts.QueryMaxLimit || (res.GroupBy == "" && res.Total < shown) || res.Changed {
			t.Fatalf("%+v: %+v", spec, res)
		}
		if s.Version() != before {
			t.Fatalf("%+v wrote to the case", spec)
		}
	})
}
