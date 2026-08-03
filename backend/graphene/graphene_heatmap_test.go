package graphene

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
)

var hmBase = time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

// heatmapFixture builds two rule-created chains an hour apart, one hand-drawn edge, and one edge
// touching an undated catalogue row — the four shapes the summary has to account for separately.
func heatmapFixture(t *testing.T) (*Store, uint64, []*Event) {
	t.Helper()
	s := OpenInMemory()
	t.Cleanup(func() { s.Close() })

	events := []*Event{
		{EventID: "4625", Computer: "HOST-A", Timestamp: hmBase, HashNormalized: "h1"},
		{EventID: "4624", Computer: "HOST-A", Timestamp: hmBase.Add(time.Minute), HashNormalized: "h2"},
		{EventID: "4625", Computer: "HOST-B", Timestamp: hmBase.Add(time.Hour), HashNormalized: "h3"},
		{EventID: "4624", Computer: "HOST-B", Timestamp: hmBase.Add(time.Hour + time.Minute), HashNormalized: "h4"},
		{EventID: "1000", Provider: "catalogue", HashNormalized: "h5"}, // no timestamp
	}
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	const graphID = uint64(7)
	add := func(from, to uint64, stamp func(*Relation)) {
		rel := &Relation{
			From: from, To: to, GraphID: graphID,
			RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
			CreatedAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), // deliberately far from the events
		}
		if stamp != nil {
			stamp(rel)
		}
		if _, err := s.InsertRelation(rel); err != nil {
			t.Fatalf("insert relation: %v", err)
		}
	}

	add(events[0].ID, events[1].ID, func(r *Relation) {
		r.StampProvenance("brute-force", consts.AlgoSequence, "m-1", 0, nil)
	})
	add(events[2].ID, events[3].ID, func(r *Relation) {
		r.StampProvenance("log-cleared", consts.AlgoSequence, "m-2", 1, nil)
	})
	// Hand-drawn: no rule, no provenance.
	add(events[0].ID, events[3].ID, func(r *Relation) {
		r.CreatedBy = consts.CreatedByUser
		r.RelationType = consts.RelationDefault
	})
	// Touches the undated catalogue row, so it has no moment at which it holds.
	add(events[1].ID, events[4].ID, nil)

	return s, graphID, events
}

func TestHeatmapPlacesRelationsAtTheirLaterEndpointNotAtBuildTime(t *testing.T) {
	// 🔒 CreatedAt is when the rule RAN. A heatmap keyed on it shows one spike at build time and
	// says nothing about the case. The fixture's CreatedAt is in 2030 precisely so a regression
	// here is impossible to miss.
	s, graphID, events := heatmapFixture(t)

	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: consts.HeatmapGroupRule})
	if err != nil {
		t.Fatal(err)
	}
	if got.From.Year() == 2030 || got.To.Year() == 2030 {
		t.Fatalf("the heatmap is keyed on CreatedAt: %v..%v", got.From, got.To)
	}
	// The earliest placeable relation ends at the first chain's 4624.
	if !got.From.Equal(events[1].Timestamp) {
		t.Errorf("From = %v, want the first chain's later endpoint %v", got.From, events[1].Timestamp)
	}
	if !got.To.Equal(events[3].Timestamp) {
		t.Errorf("To = %v, want the last chain's later endpoint %v", got.To, events[3].Timestamp)
	}
}

func TestHeatmapCountsWhatItCouldNotPlaceRatherThanDroppingIt(t *testing.T) {
	// A relation to an undated event has no moment at which it holds — the second event's time is
	// not "unknown but later", it is absent. Silently omitting it would make the counts fail to
	// add up, which is exactly the kind of quiet subtraction this app refuses elsewhere.
	s, graphID, _ := heatmapFixture(t)

	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: consts.HeatmapGroupRule})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 4 {
		t.Errorf("Total = %d, want the 4 relations in the graph", got.Total)
	}
	if got.Undated != 1 {
		t.Errorf("Undated = %d, want the 1 edge touching a catalogue row", got.Undated)
	}
	if got.Placed+got.Undated != got.Total {
		t.Errorf("%d placed + %d undated != %d total", got.Placed, got.Undated, got.Total)
	}
}

func TestHeatmapLanesAddUpToWhatWasPlaced(t *testing.T) {
	// Every grouping must account for every placed relation. A lane key that silently swallowed
	// an empty value would make the matrix quietly smaller than the graph.
	s, graphID, _ := heatmapFixture(t)

	for _, group := range consts.HeatmapGroups {
		got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: group})
		if err != nil {
			t.Fatalf("%s: %v", group, err)
		}
		total := 0
		for _, l := range got.Lanes {
			total += l.Total
		}
		if total != got.Placed {
			t.Errorf("%s: lanes total %d, placed %d", group, total, got.Placed)
		}
		buckets := 0
		for _, b := range got.Buckets {
			buckets += b.Count
		}
		if buckets != got.Placed {
			t.Errorf("%s: buckets total %d, placed %d", group, buckets, got.Placed)
		}
	}
}

func TestHeatmapGroupsByRule(t *testing.T) {
	s, graphID, _ := heatmapFixture(t)

	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: consts.HeatmapGroupRule})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]int{}
	for _, l := range got.Lanes {
		keys[l.Key] = l.Total
	}
	if keys["brute-force"] != 1 || keys["log-cleared"] != 1 {
		t.Errorf("rule lanes = %v", keys)
	}
	// The hand-drawn edge belongs to no rule, and says so rather than being dropped.
	if keys[consts.TimelineLaneNone] != 1 {
		t.Errorf("the hand-drawn edge is not in the %q lane: %v", consts.TimelineLaneNone, keys)
	}
}

func TestHeatmapStepGroupingExcludesEdgesWithNoStep(t *testing.T) {
	// 🔒 Lineage has no sequence and a hand-drawn edge has no rule. Defaulting them to the zero
	// step would pile them into "step 1" — a step they were never part of, in a lane that would
	// then read as the busiest stage of the case.
	s, graphID, _ := heatmapFixture(t)

	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: consts.HeatmapGroupStep})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]int{}
	for _, l := range got.Lanes {
		keys[l.Key] = l.Total
	}
	if keys["step 1"] != 1 || keys["step 2"] != 1 {
		t.Errorf("step lanes = %v, want one edge at each of steps 1 and 2", keys)
	}
	if keys[consts.TimelineLaneNone] != 1 {
		t.Errorf("the stepless edge is not in the %q lane: %v", consts.TimelineLaneNone, keys)
	}
}

func TestHeatmapGroupsByComputerFromTheEndpointItsTimeCameFrom(t *testing.T) {
	// Reading the row off one endpoint and the column off the other would make a single cell
	// describe two different events.
	s, graphID, _ := heatmapFixture(t)

	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: consts.HeatmapGroupComputer})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]int{}
	for _, l := range got.Lanes {
		keys[l.Key] = l.Total
	}
	// The hand-drawn edge runs HOST-A → HOST-B and is anchored at the later (HOST-B) end.
	if keys["HOST-B"] != 2 || keys["HOST-A"] != 1 {
		t.Errorf("computer lanes = %v, want HOST-A:1 HOST-B:2", keys)
	}
}

func TestHeatmapReportsTheBusiestCellSoTheRampNeedsNoSecondPass(t *testing.T) {
	s, graphID, _ := heatmapFixture(t)

	// One bucket forces every placed relation of a lane into one cell.
	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 1, GroupBy: consts.HeatmapGroupCreatedBy})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Buckets) != 1 {
		t.Fatalf("want a single bucket, got %d", len(got.Buckets))
	}
	if got.Max != 2 {
		t.Errorf("Max = %d, want the 2 system-created edges in one cell", got.Max)
	}
}

func TestHeatmapRefusesAnUnknownGroupingAndNamesTheSet(t *testing.T) {
	s, graphID, _ := heatmapFixture(t)

	_, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: "vibes"})
	if err == nil {
		t.Fatal("an unknown grouping must be refused rather than silently defaulted")
	}
	for _, g := range consts.HeatmapGroups {
		if !strings.Contains(err.Error(), g) {
			t.Errorf("error does not name the %q grouping: %v", g, err)
		}
	}
}

func TestHeatmapDefaultsAnEmptyGrouping(t *testing.T) {
	s, graphID, _ := heatmapFixture(t)
	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: ""})
	if err != nil {
		t.Fatal(err)
	}
	if got.GroupBy != consts.DefaultHeatmapGroup {
		t.Errorf("GroupBy = %q, want %q", got.GroupBy, consts.DefaultHeatmapGroup)
	}
}

func TestHeatmapScopesToOneGraphAndZeroMeansAll(t *testing.T) {
	s, graphID, events := heatmapFixture(t)

	// A second graph the first must not see.
	other := &Relation{
		From: events[0].ID, To: events[2].ID, GraphID: graphID + 1,
		RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
	}
	if _, err := s.InsertRelation(other); err != nil {
		t.Fatal(err)
	}

	scoped, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 60, GroupBy: consts.HeatmapGroupRule})
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Total != 4 {
		t.Errorf("scoped Total = %d, want 4 — the other graph's edge leaked in", scoped.Total)
	}
	all, err := s.RelationHeatmap(HeatmapQuery{Buckets: 60, GroupBy: consts.HeatmapGroupRule})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 5 {
		t.Errorf("unscoped Total = %d, want every relation in the case", all.Total)
	}
}

func TestHeatmapOnAGraphWithNoRelations(t *testing.T) {
	s := OpenInMemory()
	t.Cleanup(func() { s.Close() })

	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: 1, Buckets: 60, GroupBy: consts.HeatmapGroupRule})
	if err != nil {
		t.Fatalf("an empty graph is not an error: %v", err)
	}
	if got.Total != 0 || len(got.Buckets) != 0 || got.Max != 0 {
		t.Errorf("empty graph produced %+v", got)
	}
}

func TestHeatmapWithNothingPlaceable(t *testing.T) {
	// Every relation touches an undated event. There is no time axis, and the summary has to say
	// so rather than inventing one.
	s := OpenInMemory()
	t.Cleanup(func() { s.Close() })

	events := []*Event{
		{EventID: "1000", Provider: "catalogue", HashNormalized: "u1"},
		{EventID: "1001", Provider: "catalogue", HashNormalized: "u2"},
	}
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertRelation(&Relation{
		From: events[0].ID, To: events[1].ID, GraphID: 1,
		RelationType: consts.RelationDefault, CreatedBy: consts.CreatedByUser,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: 1, Buckets: 60, GroupBy: consts.HeatmapGroupRule})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Undated != 1 || got.Placed != 0 {
		t.Errorf("got %+v, want 1 total / 1 undated / 0 placed", got)
	}
	if !got.From.IsZero() || len(got.Buckets) != 0 {
		t.Errorf("a heatmap with nothing to place must not invent a range: %v..%v", got.From, got.To)
	}
}

func TestHeatmapAndTimelineAgreeOnBucketBoundaries(t *testing.T) {
	// 🔒 The strip is drawn OVER the histogram. Two bucketings that agreed today would drift, and
	// the failure would be a column that did not line up — which reads as activity at a time it
	// did not happen. Both go through newBucketing, and this is what holds them there.
	s, graphID, _ := heatmapFixture(t)

	heat, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 24, GroupBy: consts.HeatmapGroupRule})
	if err != nil {
		t.Fatal(err)
	}
	// Ask the timeline for the SAME window, so any difference is the bucketing itself.
	from, to := heat.From, heat.To
	tl, err := s.Timeline(EventFilter{TimeFrom: &from, TimeTo: &to}, 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Buckets) != len(heat.Buckets) {
		t.Fatalf("bucket counts differ: timeline %d, heatmap %d", len(tl.Buckets), len(heat.Buckets))
	}
	for i := range heat.Buckets {
		if !tl.Buckets[i].Start.Equal(heat.Buckets[i].Start) || !tl.Buckets[i].End.Equal(heat.Buckets[i].End) {
			t.Errorf("bucket %d: timeline %v..%v, heatmap %v..%v", i,
				tl.Buckets[i].Start, tl.Buckets[i].End, heat.Buckets[i].Start, heat.Buckets[i].End)
		}
	}
}

func TestBucketingClampsTheFinalInstantIntoTheLastBucket(t *testing.T) {
	from := hmBase
	to := hmBase.Add(10 * time.Second)
	b := newBucketing(from, to, 10)

	if got := b.indexOf(to); got != 9 {
		t.Errorf("the final instant landed in bucket %d, want the last (9)", got)
	}
	if got := b.indexOf(from); got != 0 {
		t.Errorf("the first instant landed in bucket %d, want 0", got)
	}
	if got := b.indexOf(from.Add(-time.Second)); got != -1 {
		t.Errorf("a timestamp before the window returned %d, want -1", got)
	}
}

func TestBucketingCollapsesAZeroSpanToOneBucket(t *testing.T) {
	// Every event at one instant. Spreading them across a fabricated range would draw a shape the
	// evidence does not have.
	b := newBucketing(hmBase, hmBase, 240)
	if b.count != 1 || len(b.buckets) != 1 {
		t.Fatalf("count = %d, want 1", b.count)
	}
	if b.indexOf(hmBase) != 0 {
		t.Error("the only instant did not land in the only bucket")
	}
}

func TestHeatmapHonoursAnExplicitWindowRatherThanClampingIntoIt(t *testing.T) {
	// 🔒 The timeline passes its own view bounds so the strip shares the histogram's axis. A
	// relation outside that window must be counted and dropped, never clamped into the nearest
	// column — clamping would pile everything after a zoomed window into the right-most cell and
	// show activity at a time it did not happen.
	s, graphID, events := heatmapFixture(t)

	// A window covering only the first chain.
	from := events[0].Timestamp
	to := events[1].Timestamp.Add(time.Minute)

	got, err := s.RelationHeatmap(HeatmapQuery{
		GraphID: graphID, Buckets: 12, GroupBy: consts.HeatmapGroupRule, From: &from, To: &to,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Placed != 1 {
		t.Errorf("Placed = %d, want only the first chain's edge", got.Placed)
	}
	if got.Outside != 2 {
		t.Errorf("Outside = %d, want the 2 edges beyond the window", got.Outside)
	}
	if got.Placed+got.Outside+got.Undated != got.Total {
		t.Errorf("%d placed + %d outside + %d undated != %d total",
			got.Placed, got.Outside, got.Undated, got.Total)
	}
	// 🔒 The axis is the one that was ASKED for, not the one the surviving data happens to span.
	// Narrowing it to the data would misalign every column against the histogram beneath.
	if !got.From.Equal(from) || !got.To.Equal(to) {
		t.Errorf("window = %v..%v, want the requested %v..%v", got.From, got.To, from, to)
	}
}

func TestHeatmapWithNoWindowReportsNothingOutside(t *testing.T) {
	s, graphID, _ := heatmapFixture(t)
	got, err := s.RelationHeatmap(HeatmapQuery{GraphID: graphID, Buckets: 12})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outside != 0 {
		t.Errorf("Outside = %d without a window, want 0", got.Outside)
	}
}
