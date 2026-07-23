package graphene

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

func seedTimeline(t *testing.T) *Store {
	t.Helper()
	s := OpenInMemory()
	t.Cleanup(func() { s.Close() })

	base := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	events := []*Event{}
	// 10 events one hour apart, plus two undated ones.
	for i := 0; i < 10; i++ {
		events = append(events, mkEvent("4624", "p", "Security", "u", base.Add(time.Duration(i)*time.Hour), string(rune('a'+i))))
	}
	events = append(events,
		mkEvent("X", "p", "Security", "u", time.Time{}, "u1"),
		mkEvent("Y", "p", "Security", "u", time.Time{}, "u2"),
	)
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTimelineExtentAndCounts(t *testing.T) {
	s := seedTimeline(t)

	sum, err := s.Timeline(EventFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Dated != 10 {
		t.Errorf("dated = %d, want 10", sum.Dated)
	}
	// Undated events must be COUNTED even though they cannot be placed — the timeline says
	// what it is leaving out rather than quietly dropping it.
	if sum.Undated != 2 {
		t.Errorf("undated = %d, want 2", sum.Undated)
	}
	base := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	if !sum.From.Equal(base) {
		t.Errorf("from = %v, want %v", sum.From, base)
	}
	if !sum.To.Equal(base.Add(9 * time.Hour)) {
		t.Errorf("to = %v, want %v", sum.To, base.Add(9*time.Hour))
	}
}

func TestTimelineBucketsCoverEveryDatedEvent(t *testing.T) {
	s := seedTimeline(t)
	for _, n := range []int{1, 5, 10, 37} {
		sum, err := s.Timeline(EventFilter{}, n)
		if err != nil {
			t.Fatal(err)
		}
		total := 0
		for _, b := range sum.Buckets {
			total += b.Count
		}
		if total != sum.Dated {
			t.Errorf("buckets=%d: counted %d across buckets, want %d — an event fell outside every bucket",
				n, total, sum.Dated)
		}
		if len(sum.Buckets) != n {
			t.Errorf("buckets=%d: got %d buckets", n, len(sum.Buckets))
		}
	}
}

func TestTimelineHonoursFilters(t *testing.T) {
	s := seedTimeline(t)
	base := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	from := base.Add(2 * time.Hour)
	to := base.Add(4 * time.Hour)

	sum, err := s.Timeline(EventFilter{TimeFrom: &from, TimeTo: &to}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Dated != 3 { // hours 2, 3, 4
		t.Errorf("dated in range = %d, want 3", sum.Dated)
	}
	if !sum.From.Equal(from) || !sum.To.Equal(to) {
		t.Errorf("window = %v..%v, want %v..%v", sum.From, sum.To, from, to)
	}

	// A non-time filter narrows it too.
	byID, err := s.Timeline(EventFilter{EventID: "X"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if byID.Dated != 0 || byID.Undated != 1 {
		t.Errorf("event-id filter = %d dated / %d undated, want 0/1", byID.Dated, byID.Undated)
	}
}

func TestTimelineCountsUndatedRegardlessOfPolicy(t *testing.T) {
	// The caller's undated policy must not change the reported undated count: the timeline
	// always knows how many events it cannot show.
	s := seedTimeline(t)
	for _, policy := range []string{consts.UndatedExclude, consts.UndatedInclude, consts.UndatedOnly} {
		sum, err := s.Timeline(EventFilter{Undated: policy}, 8)
		if err != nil {
			t.Fatal(err)
		}
		if sum.Undated != 2 {
			t.Errorf("policy %q: undated = %d, want 2", policy, sum.Undated)
		}
		if sum.Dated != 10 {
			t.Errorf("policy %q: dated = %d, want 10", policy, sum.Dated)
		}
	}
}

func TestTimelineLanesSumToTheTotal(t *testing.T) {
	// The lanes must account for every dated event: a grouping that quietly drops rows
	// would make the picture disagree with the count beside it.
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	events := []*Event{}
	for i := 0; i < 6; i++ {
		e := mkEvent("4624", "prov-A", "Security", "alice", base.Add(time.Duration(i)*time.Hour), string(rune('a'+i)))
		if i%2 == 1 {
			e.Provider = "prov-B"
			e.Channel = "System"
		}
		events = append(events, e)
	}
	// One event with no user at all — it must still be represented, in a "(none)" lane.
	noUser := mkEvent("1", "prov-A", "Security", "", base.Add(9*time.Hour), "nouser")
	events = append(events, noUser)
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatal(err)
	}

	for _, group := range []string{
		consts.TimelineGroupProvider,
		consts.TimelineGroupChannel,
		consts.TimelineGroupUser,
		consts.TimelineGroupComputer,
	} {
		sum, err := s.TimelineGrouped(EventFilter{}, 12, group)
		if err != nil {
			t.Fatal(err)
		}
		if sum.GroupBy != group {
			t.Errorf("group %q: summary echoed %q", group, sum.GroupBy)
		}
		total := 0
		for _, lane := range sum.Lanes {
			if len(lane.Counts) != len(sum.Buckets) {
				t.Errorf("group %q lane %q: %d counts for %d buckets — they must align",
					group, lane.Key, len(lane.Counts), len(sum.Buckets))
			}
			laneSum := 0
			for _, c := range lane.Counts {
				laneSum += c
			}
			if laneSum != lane.Total {
				t.Errorf("group %q lane %q: total %d but counts sum to %d", group, lane.Key, lane.Total, laneSum)
			}
			total += lane.Total
		}
		if total != sum.Dated {
			t.Errorf("group %q: lanes account for %d events, want %d", group, total, sum.Dated)
		}
	}
}

func TestTimelineLanesSeparateValues(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	base := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	a := mkEvent("1", "prov-A", "Security", "u", base, "h1")
	b := mkEvent("2", "prov-B", "Security", "u", base.Add(time.Hour), "h2")
	c := mkEvent("3", "prov-B", "Security", "u", base.Add(2*time.Hour), "h3")
	if _, err := s.InsertEvents([]*Event{a, b, c}); err != nil {
		t.Fatal(err)
	}

	sum, err := s.TimelineGrouped(EventFilter{}, 6, consts.TimelineGroupProvider)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Lanes) != 2 {
		t.Fatalf("lanes = %d, want 2", len(sum.Lanes))
	}
	// Busiest lane first, so the eye lands on the dominant group.
	if sum.Lanes[0].Key != "prov-B" || sum.Lanes[0].Total != 2 {
		t.Errorf("first lane = %+v, want prov-B with 2", sum.Lanes[0])
	}
}

func TestTimelineGroupsByGraph(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	base := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("1", "p", "c", "u", base, "h1"),
		mkEvent("2", "p", "c", "u", base.Add(time.Hour), "h2"),
		mkEvent("3", "p", "c", "u", base.Add(2*time.Hour), "h3"), // uncorrelated
	})
	if err != nil {
		t.Fatal(err)
	}
	// Events 0 and 1 correlated inside graph 7.
	if _, err := s.InsertRelation(&Relation{
		From: ids[0], To: ids[1], GraphID: 7,
		CreatedBy: consts.CreatedBySystem, CreatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}

	sum, err := s.TimelineGrouped(EventFilter{}, 8, consts.TimelineGroupGraph)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]int{}
	for _, l := range sum.Lanes {
		byKey[l.Key] = l.Total
	}
	if byKey["7"] != 2 {
		t.Errorf("graph-7 lane = %d, want the 2 correlated events (lanes: %+v)", byKey["7"], byKey)
	}
	// An event in no graph still gets a lane, so the picture accounts for everything.
	if byKey[consts.TimelineLaneNone] != 1 {
		t.Errorf("uncorrelated lane = %d, want 1", byKey[consts.TimelineLaneNone])
	}
}

func TestTimelineGraphGroupingCountsMultiGraphMembershipOnceperGraph(t *testing.T) {
	// An event correlated into two graphs belongs in BOTH lanes — hiding it in one would
	// misrepresent which rules matched it. Lane totals may therefore exceed the event count,
	// which is correct for this grouping and only this one.
	s := OpenInMemory()
	defer s.Close()
	base := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		mkEvent("1", "p", "c", "u", base, "h1"),
		mkEvent("2", "p", "c", "u", base.Add(time.Hour), "h2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []uint64{7, 9} {
		if _, err := s.InsertRelation(&Relation{
			From: ids[0], To: ids[1], GraphID: g,
			CreatedBy: consts.CreatedBySystem, CreatedAt: base,
		}); err != nil {
			t.Fatal(err)
		}
	}

	sum, err := s.TimelineGrouped(EventFilter{}, 8, consts.TimelineGroupGraph)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]int{}
	for _, l := range sum.Lanes {
		byKey[l.Key] = l.Total
	}
	if byKey["7"] != 2 || byKey["9"] != 2 {
		t.Errorf("both graphs should list both events, got %+v", byKey)
	}
	// And a duplicate relation in the SAME graph must not double-count.
	if len(sum.Lanes) != 2 {
		t.Errorf("lanes = %d, want exactly 2", len(sum.Lanes))
	}
}

func TestTimelineUngroupedHasNoLanes(t *testing.T) {
	s := seedTimeline(t)
	sum, err := s.TimelineGrouped(EventFilter{}, 8, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Lanes) != 0 {
		t.Errorf("ungrouped summary returned %d lanes", len(sum.Lanes))
	}
	// An unknown grouping degrades to ungrouped rather than failing or hiding data.
	unknown, err := s.TimelineGrouped(EventFilter{}, 8, "not-a-field")
	if err != nil {
		t.Fatalf("unknown grouping should not error: %v", err)
	}
	if unknown.Dated != sum.Dated {
		t.Errorf("unknown grouping changed the dated count (%d vs %d)", unknown.Dated, sum.Dated)
	}
}

func TestTimelineEmptyAndSingleInstant(t *testing.T) {
	empty := OpenInMemory()
	defer empty.Close()
	sum, err := empty.Timeline(EventFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Dated != 0 || len(sum.Buckets) != 0 {
		t.Errorf("empty store returned %+v", sum)
	}

	// Every event at the same instant: one bucket is the truthful answer rather than
	// spreading identical timestamps across an invented range.
	one := OpenInMemory()
	defer one.Close()
	at := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if _, err := one.InsertEvents([]*Event{
		mkEvent("1", "p", "c", "u", at, "h1"),
		mkEvent("2", "p", "c", "u", at, "h2"),
	}); err != nil {
		t.Fatal(err)
	}
	sum, err = one.Timeline(EventFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Buckets) != 1 || sum.Buckets[0].Count != 2 {
		t.Errorf("single-instant timeline = %+v, want one bucket of 2", sum.Buckets)
	}
}
