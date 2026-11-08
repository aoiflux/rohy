package graphene

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/aoiflux/graphene/store"

	"rohy/backend/consts"
)

func labelsPath(dir string) string { return filepath.Join(dir, consts.TypeNamesFile) }

// namedCase is a closed case a writable open has named.
func namedCase(t *testing.T) string {
	t.Helper()
	s, dir := attributedStore(t)
	oneEvent(t, s, "a")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A writable open records rohy's type names beside the image, so the case reads with names in
// graphene's own tools, and the engine renders them in its errors.
func TestAWritableOpenNamesRohysTypes(t *testing.T) {
	dir := namedCase(t)
	if named, err := labelsNameRohysTypes(dir); err != nil || !named {
		t.Fatalf("the labels file does not name rohy's types: %v", err)
	}
	if got := consts.NodeEvent.String(); got != consts.NodeEventName {
		t.Fatalf("the event type renders as %q", got)
	}
	if got := consts.EdgeRelation.String(); got != consts.EdgeRelationName {
		t.Fatalf("the relation type renders as %q", got)
	}
}

// Recording the names rewrites the file durably, so an open whose case already names them leaves
// the file alone.
func TestAnOpenOfANamedCaseDoesNotRewriteItsLabels(t *testing.T) {
	dir := namedCase(t)
	before, err := os.Stat(labelsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	s := OpenAwaitingSession(dir)
	defer s.Close()
	if err := s.BeginSession(unsigned()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(labelsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an open of a named case rewrote its labels file")
	}
}

// 🔒 A read-only open names nothing on disk.
func TestAReadOnlyOpenWritesNoLabels(t *testing.T) {
	dir := namedCase(t)
	if err := os.Remove(labelsPath(dir)); err != nil {
		t.Fatal(err)
	}
	s := OpenAwaitingSession(dir)
	defer s.Close()
	if err := s.BeginSession(recovery()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(labelsPath(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a read-only open wrote a labels file: %v", err)
	}
}

// 🔒 A labels file that names rohy's event type otherwise is refused, not rendered wrongly: two
// names for one number mean the file has drifted from the data it describes.
func TestALabelsFileNamingRohysTypesOtherwiseIsRefused(t *testing.T) {
	dir := namedCase(t)
	data, err := os.ReadFile(labelsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	header, _, _ := bytes.Cut(data, []byte("\n"))
	forged := append(append(header, '\n'), []byte("node\t32768\tNotAnEvent\n")...)
	if err := os.WriteFile(labelsPath(dir), forged, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Posture{unsigned(), recovery()} {
		s := OpenAwaitingSession(dir)
		err := s.BeginSession(p)
		s.Close()
		if !errors.Is(err, ErrLabelsDisagree) || !errors.Is(err, store.ErrTypeNameConflict) || !NoPostureOpens(err) {
			t.Fatalf("%s open of a case whose labels disagree: %v, want ErrLabelsDisagree wrapping ErrTypeNameConflict", p.Mode, err)
		}
	}
}

// parityCase is a closed case of events with tied, undated and year-straddling timestamps, written
// without compaction, so its ordering lives only in graphene.schema and the log.
func parityCase(t *testing.T) string {
	t.Helper()
	s, dir := attributedStore(t)
	base := time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC)
	events := make([]*Event, 0, 3000)
	for i := range 3000 {
		ts := base.Add(time.Duration(i/3) * 7 * time.Second) // three events per instant
		if i%97 == 0 {
			ts = time.Time{}
		}
		events = append(events, mkEvent("4624", "p", "c", "u", ts, fmt.Sprintf("parity-%d", i)))
	}
	for chunk := range slices.Chunk(events, 500) {
		if _, err := s.InsertEvents(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

type window struct {
	name     string
	from, to *time.Time
}

func parityWindows() []window {
	at := func(d time.Duration) *time.Time {
		v := time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC).Add(d)
		return &v
	}
	return []window{
		{"narrow", at(700 * time.Second), at(800 * time.Second)},
		{"one tied instant", at(70 * time.Second), at(70 * time.Second)},
		{"between instants", at(71 * time.Second), at(76 * time.Second)},
		{"across the year", at(55 * time.Minute), at(65 * time.Minute)},
		{"wide", at(0), at(2 * time.Hour)},
		{"from only", at(3 * time.Hour), nil},
		{"to only", nil, at(10 * time.Minute)},
		{"before every event", at(-time.Hour), at(-time.Minute)},
	}
}

// assertRangesMatchTheOracle checks every window against the unfiltered events filtered here, so
// what the plan does cannot decide what counts as right.
func assertRangesMatchTheOracle(t *testing.T, stage string, s *Store) {
	t.Helper()
	all, err := s.QueryEvents(EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range parityWindows() {
		var want []uint64
		for _, e := range all {
			if e.Timestamp.IsZero() || (w.from != nil && e.Timestamp.Before(*w.from)) || (w.to != nil && e.Timestamp.After(*w.to)) {
				continue
			}
			want = append(want, e.ID)
		}
		got, err := s.QueryEvents(EventFilter{TimeFrom: w.from, TimeTo: w.to})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]uint64, len(got))
		for i, e := range got {
			ids[i] = e.ID
		}
		if !slices.Equal(ids, want) {
			t.Errorf("%s, %s: %d events, want %d in the oracle's order", stage, w.name, len(ids), len(want))
		}
		n, err := s.CountEvents(EventFilter{TimeFrom: w.from, TimeTo: w.to})
		if err != nil || n != len(want) {
			t.Errorf("%s, %s: count %d (%v), want %d", stage, w.name, n, err, len(want))
		}
	}
}

func timestampDriver(t *testing.T, s *Store) store.DriverKind {
	t.Helper()
	g, err := s.graph()
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	plan, err := g.ExplainNodeQuery(store.NodeQuery{
		Types: []store.NodeType{consts.NodeEvent},
		Filters: []store.PropertyFilter{{
			Key: consts.PropTimestamp, Op: store.PropertyOpBetweenInclusive,
			Value: []byte(timestampIndex(from)), ValueUpper: []byte(timestampIndex(from.Add(time.Minute))),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan.Driver
}

// 🔒 Declaring the timestamp ordered changes how a range is found, never what it finds: a case
// declared from its first write, the same case compacted, a case written before the declaration
// read through the plain plan, the same case declared on a writable open, and compacted after it,
// all answer every window exactly as the oracle does.
func TestTheOrderedTimestampAnswersEveryRangeAsThePlainPlanDoes(t *testing.T) {
	dir := parityCase(t)
	stage := func(name string, p Posture, compact bool, want store.DriverKind) {
		t.Helper()
		s := OpenAwaitingSession(dir)
		defer s.Close()
		if err := s.BeginSession(p); err != nil {
			t.Fatal(err)
		}
		if got := timestampDriver(t, s); got != want {
			t.Fatalf("%s: driver %s, want %s", name, got, want)
		}
		assertRangesMatchTheOracle(t, name, s)
		if compact {
			if err := s.Compact(); err != nil {
				t.Fatal(err)
			}
			assertRangesMatchTheOracle(t, name+", compacted", s)
		}
	}

	stage("declared from the first write", unsigned(), true, store.DriverOrdered)

	// A case from before the declaration: its ordering is in neither the schema nor the image.
	old := parityCase(t)
	if err := os.Remove(filepath.Join(old, "graphene.schema")); err != nil {
		t.Fatal(err)
	}
	dir = old
	stage("an old case, read-only", recovery(), false, store.DriverLabels)
	stage("an old case, declared on a writable open", unsigned(), true, store.DriverOrdered)
	stage("the old case, reopened after its compaction", unsigned(), false, store.DriverOrdered)
}
