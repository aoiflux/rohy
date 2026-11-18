package api

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/query"
)

// 🔒 The console exposes reads only. A method added to the binding fails here until
// someone has decided, out loud, that it belongs.
func TestTheQueryBindingExposesOnlyDescribeAndRun(t *testing.T) {
	rt := reflect.TypeFor[*QueryAPI]()
	var names []string
	for method := range rt.Methods() {
		names = append(names, method.Name)
	}
	if !slices.Equal(names, []string{"Describe", "Run"}) {
		t.Fatalf("QueryAPI exposes %v, want exactly [Describe Run]", names)
	}
	if f, _ := reflect.TypeFor[QueryAPI]().FieldByName("reader"); f.Type != reflect.TypeFor[query.Reader]() {
		t.Fatalf("QueryAPI holds %v, want the read-only query.Reader", f.Type)
	}
}

func queryCase(t *testing.T) (*QueryAPI, *graphene.Store) {
	t.Helper()
	s := graphene.OpenInMemory()
	t.Cleanup(func() { s.Close() })
	base := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	if _, err := s.InsertEvents([]*graphene.Event{
		{EventID: "4624", Timestamp: base, User: "alice", HashRaw: "r1", HashNormalized: "n1"},
		{EventID: "4625", Timestamp: base.Add(time.Minute), User: "bob", HashRaw: "r2", HashNormalized: "n2"},
	}); err != nil {
		t.Fatal(err)
	}
	return NewQueryAPI(s), s
}

func TestTheQueryBindingAnswersAndCodesItsRefusals(t *testing.T) {
	a, s := queryCase(t)
	before := s.Version()
	res, err := a.Run(query.Spec{Subject: consts.QuerySubjectEvents, GroupBy: consts.PropUser})
	if err != nil || res.Total != 2 || len(res.Groups) != 2 {
		t.Fatalf("%+v, %v", res, err)
	}
	if _, err := a.Run(query.Spec{Subject: "nodes"}); codeOf(err) != consts.ErrCodeQuery {
		t.Errorf("a refused spec is %q (%v), want %q", codeOf(err), err, consts.ErrCodeQuery)
	}
	if s.Version() != before {
		t.Error("a query wrote to the case")
	}
	if v := a.Describe(); len(v.Subjects) != 2 || v.MaxLimit != consts.QueryMaxLimit {
		t.Errorf("%+v", v)
	}
}

type brokenReader struct{ query.Reader }

func (brokenReader) Version() uint64 { return 0 }
func (brokenReader) CountEvents(graphene.EventFilter) (int, error) {
	return 0, errors.New("the payload log is unreadable")
}

type panickingReader struct{ brokenReader }

func (panickingReader) CountEvents(graphene.EventFilter) (int, error) { panic("index out of range") }

func TestTheQueryBindingSaysWhoseFaultAFailureIs(t *testing.T) {
	a := NewQueryAPI(brokenReader{})
	if _, err := a.Run(query.Spec{Subject: consts.QuerySubjectEvents}); codeOf(err) != consts.ErrCodePersistence {
		t.Errorf("a store failure is %q (%v), want %q", codeOf(err), err, consts.ErrCodePersistence)
	}
	p := NewQueryAPI(panickingReader{})
	if _, err := p.Run(query.Spec{Subject: consts.QuerySubjectEvents}); codeOf(err) != consts.ErrCodeInternal {
		t.Errorf("a panic is %q (%v), want %q, and an answer", codeOf(err), err, consts.ErrCodeInternal)
	}
}
