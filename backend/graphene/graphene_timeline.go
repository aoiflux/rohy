package graphene

import (
	"sort"
	"strconv"
	"time"

	"rohy/backend/consts"
)

// Timeline data (P24).
//
// The timeline must stay responsive on a case with hundreds of thousands of events, which
// rules out shipping every event to the frontend to be drawn. Instead the backend answers
// with a DENSITY HISTOGRAM: the time extent of the matching set plus per-bucket counts.
// The page draws that, and only fetches individual events for the narrow range the user has
// actually zoomed into (via the ordinary event query with time bounds).
//
// This is the same honesty rule the rest of the app follows: show the shape of the data at
// the resolution you actually have, rather than pretending to plot a million points.

// TimelineBucket is one time slice and how many events fall in it.
type TimelineBucket struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Count int       `json:"count"`
}

// TimelineLane is one grouping row: a key and its per-bucket counts, aligned index-for-index
// with TimelineSummary.Buckets. Counts travel as a bare []int rather than repeating the
// bucket boundaries per lane, which keeps the payload small when there are many lanes.
type TimelineLane struct {
	Key    string `json:"key"`
	Total  int    `json:"total"`
	Counts []int  `json:"counts"`
}

// TimelineSummary describes the matching set's shape over time.
type TimelineSummary struct {
	// From/To bound the DATED events that matched. Zero when nothing is dated.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Dated is how many matching events can be placed on a timeline; Undated is how many
	// cannot. Undated is reported rather than hidden — the timeline states what it is
	// leaving out (P22/P23).
	Dated   int `json:"dated"`
	Undated int `json:"undated"`
	// Buckets partition [From, To] into equal slices. Empty when nothing is dated.
	Buckets []TimelineBucket `json:"buckets"`
	// GroupBy echoes the requested grouping, and Lanes carries one row per group. Lanes are
	// capped: beyond the cap the smallest are folded into a single "other" row, because a
	// timeline with four hundred lanes is unreadable and the payload is wasted.
	GroupBy string         `json:"group_by"`
	Lanes   []TimelineLane `json:"lanes"`
}

// Timeline summarizes the filtered event set over time, in `buckets` equal slices.
//
// The filter's own time bounds, when set, define the window; otherwise the window is the
// full extent of the matching data. Undated events are counted but never bucketed — they
// have no position to occupy.
func (s *Store) Timeline(f EventFilter, buckets int) (TimelineSummary, error) {
	return s.TimelineGrouped(f, buckets, "")
}

// TimelineGrouped is Timeline plus lane grouping by an event field (consts.TimelineGroup*).
// An empty or unknown groupBy returns the ungrouped summary rather than an error, so a
// stale UI value degrades to the plain view instead of failing.
func (s *Store) TimelineGrouped(f EventFilter, buckets int, groupBy string) (TimelineSummary, error) {
	if buckets <= 0 {
		buckets = defaultTimelineBuckets
	}
	if buckets > maxTimelineBuckets {
		buckets = maxTimelineBuckets
	}

	// Ask for everything the filter matches, dated or not, so the undated count is honest
	// regardless of the caller's undated policy.
	scan := f
	scan.Undated = consts.UndatedInclude
	rows, err := s.matchingRows(scan)
	if err != nil {
		return TimelineSummary{}, err
	}

	out := TimelineSummary{GroupBy: groupBy}
	dated := make([]eventRow, 0, len(rows))
	for _, r := range rows {
		if r.ts.IsZero() {
			out.Undated++
			continue
		}
		dated = append(dated, r)
	}
	out.Dated = len(dated)
	if len(dated) == 0 {
		return out, nil
	}

	sort.Slice(dated, func(i, j int) bool { return dated[i].ts.Before(dated[j].ts) })
	from, to := dated[0].ts, dated[len(dated)-1].ts
	if f.TimeFrom != nil && f.TimeFrom.After(from) {
		from = *f.TimeFrom
	}
	if f.TimeTo != nil && f.TimeTo.Before(to) {
		to = *f.TimeTo
	}
	out.From, out.To = from, to

	span := to.Sub(from)
	width := span / time.Duration(buckets)
	if span <= 0 || width <= 0 {
		// Every event shares one instant (or the span is finer than a bucket): a single
		// bucket is the truthful rendering, rather than spreading identical timestamps
		// across a fabricated range.
		buckets = 1
		width = time.Nanosecond
	}

	out.Buckets = make([]TimelineBucket, buckets)
	for i := range out.Buckets {
		start := from.Add(time.Duration(i) * width)
		end := start.Add(width)
		if buckets == 1 {
			end = to
		}
		out.Buckets[i] = TimelineBucket{Start: start, End: end}
	}

	// bucketOf maps a timestamp onto its slice, clamping the final instant into the last
	// bucket instead of past the end of the array.
	bucketOf := func(ts time.Time) int {
		if buckets == 1 {
			return 0
		}
		idx := int(ts.Sub(from) / width)
		if idx < 0 {
			return -1 // outside an explicit lower bound
		}
		if idx >= buckets {
			return buckets - 1
		}
		return idx
	}

	// Grouping by graph is resolved from the edge index once, not per event: an event's
	// graph membership lives on its relations, so it cannot be read off the node.
	var graphsOf map[uint64][]string
	if groupBy == consts.TimelineGroupGraph {
		graphsOf, err = s.eventGraphMembership()
		if err != nil {
			return TimelineSummary{}, err
		}
	}

	laneCounts := map[string][]int{}
	addTo := func(key string, idx int) {
		counts, ok := laneCounts[key]
		if !ok {
			counts = make([]int, buckets)
			laneCounts[key] = counts
		}
		counts[idx]++
	}

	for _, r := range dated {
		idx := bucketOf(r.ts)
		if idx < 0 {
			continue
		}
		out.Buckets[idx].Count++

		if groupBy == "" {
			continue
		}
		if groupBy == consts.TimelineGroupGraph {
			keys := graphsOf[r.id]
			if len(keys) == 0 {
				addTo(consts.TimelineLaneNone, idx)
				continue
			}
			// An event correlated into several graphs appears in each of their lanes —
			// that is the truth of the data, and hiding it in one arbitrary lane would
			// misrepresent which rules actually matched it.
			for _, k := range keys {
				addTo(k, idx)
			}
			continue
		}
		addTo(laneKey(groupBy, r.view), idx)
	}

	if groupBy != "" {
		out.Lanes = buildLanes(laneCounts, buckets)
	}
	return out, nil
}

// eventGraphMembership maps each event id to the graph ids its relations belong to.
//
// It reads every relation once and inverts them, rather than asking per event: one scan of
// the edges beats a lookup per candidate row, and edges are far fewer than events. Graph
// ids are returned as strings; resolving them to graph NAMES is the caller's job, because
// the graph registry lives outside this package.
func (s *Store) eventGraphMembership() (map[uint64][]string, error) {
	rels, err := s.GetRelations()
	if err != nil {
		return nil, err
	}
	out := make(map[uint64][]string, len(rels))
	seen := map[uint64]map[string]bool{}

	add := func(eventID, graphID uint64) {
		if graphID == 0 {
			return
		}
		key := strconv.FormatUint(graphID, 10)
		if seen[eventID] == nil {
			seen[eventID] = map[string]bool{}
		}
		if seen[eventID][key] {
			return
		}
		seen[eventID][key] = true
		out[eventID] = append(out[eventID], key)
	}

	for _, r := range rels {
		add(r.From, r.GraphID)
		add(r.To, r.GraphID)
	}
	return out, nil
}

// laneKey extracts the grouping value for one event. An empty value becomes an explicit
// "(none)" lane rather than being dropped — an event with no user is still an event, and
// silently omitting it would make the lanes fail to add up to the total.
func laneKey(groupBy string, v eventSortView) string {
	var key string
	switch groupBy {
	case consts.TimelineGroupProvider:
		key = v.Provider
	case consts.TimelineGroupChannel:
		key = v.Channel
	case consts.TimelineGroupUser:
		key = v.User
	case consts.TimelineGroupComputer:
		key = v.Computer
	default:
		return consts.TimelineLaneNone
	}
	if key == "" {
		return consts.TimelineLaneNone
	}
	return key
}

// buildLanes orders lanes by volume and folds everything past the cap into one "other" row,
// so a field with hundreds of distinct values stays readable and the payload stays bounded.
func buildLanes(laneCounts map[string][]int, buckets int) []TimelineLane {
	lanes := make([]TimelineLane, 0, len(laneCounts))
	for key, counts := range laneCounts {
		total := 0
		for _, c := range counts {
			total += c
		}
		lanes = append(lanes, TimelineLane{Key: key, Total: total, Counts: counts})
	}
	// Busiest first; ties broken by key so the order is stable between calls.
	sort.Slice(lanes, func(i, j int) bool {
		if lanes[i].Total == lanes[j].Total {
			return lanes[i].Key < lanes[j].Key
		}
		return lanes[i].Total > lanes[j].Total
	})
	if len(lanes) <= maxTimelineLanes {
		return lanes
	}

	kept := lanes[:maxTimelineLanes]
	other := TimelineLane{Key: consts.TimelineLaneOther, Counts: make([]int, buckets)}
	for _, l := range lanes[maxTimelineLanes:] {
		other.Total += l.Total
		for i, c := range l.Counts {
			other.Counts[i] += c
		}
	}
	return append(kept, other)
}

const (
	defaultTimelineBuckets = 240
	maxTimelineBuckets     = 2000
	// A timeline with hundreds of lanes is unreadable; beyond this the smallest are folded
	// into a single "other" row so the picture stays legible and the payload bounded.
	maxTimelineLanes = 12
)
