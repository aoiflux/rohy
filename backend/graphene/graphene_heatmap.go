package graphene

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"rohy/backend/consts"
)

// The relationship heatmap (P29).
//
// The timeline answers "when did events happen". This answers the next question: "when did the
// things rohy INFERRED happen, and what kind were they" — relation counts per (time bucket ×
// group). It is drawn as a strip over the timeline and as a standalone matrix, which is exactly
// why it reuses `bucketing`: two views stacked on each other must agree, to the pixel, about
// where a bucket starts.

// HeatmapSummary is the matrix, plus everything needed to read it honestly.
type HeatmapSummary struct {
	// From/To bound the PLACEABLE relations. Zero when none could be placed.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Total is every relation considered; Placed is how many landed in a bucket; Undated is how
	// many could not be placed at all; Outside is how many fell beyond an explicitly requested
	// window. All reported rather than hidden, the same rule the timeline follows: a view states
	// what it is leaving out.
	Total   int `json:"total"`
	Placed  int `json:"placed"`
	Undated int `json:"undated"`
	Outside int `json:"outside"`
	// Buckets partition [From, To], with Count as the relation total per bucket. Same type as the
	// timeline's, so the strip and the histogram are drawn by the same code.
	Buckets []TimelineBucket `json:"buckets"`

	GroupBy string         `json:"group_by"`
	Lanes   []TimelineLane `json:"lanes"`
	// Max is the largest single cell in the matrix, so the frontend can scale its colour ramp
	// without a second pass over every lane. Zero when the matrix is empty.
	Max int `json:"max"`
}

// HeatmapQuery selects what to bucket and over what window.
type HeatmapQuery struct {
	// GraphID 0 covers every relation in the case.
	GraphID uint64
	Buckets int
	GroupBy string
	// From/To pin the time window. Leave them nil to take the extent of the relations
	// themselves; set them when the heatmap is drawn OVER the timeline, so the strip and the
	// histogram beneath it share an axis exactly rather than approximately.
	From *time.Time
	To   *time.Time
}

// RelationHeatmap buckets a graph's relations over time.
//
// 🔒 A relation is placed at its LATER ENDPOINT'S timestamp — the moment the relationship became
// true — never at CreatedAt. CreatedAt is when the rule ran, so a heatmap keyed on it would show
// one spike at build time and say nothing whatsoever about the case. This is the same rule replay
// follows, and for the same reason.
func (s *Store) RelationHeatmap(q HeatmapQuery) (HeatmapSummary, error) {
	groupBy := q.GroupBy
	if groupBy == "" {
		groupBy = consts.DefaultHeatmapGroup
	}
	if !validHeatmapGroup(groupBy) {
		return HeatmapSummary{}, fmt.Errorf(consts.MsgHeatmapUnknownGroup, groupBy,
			strings.Join(consts.HeatmapGroups, ", "))
	}
	buckets := q.Buckets
	if buckets <= 0 {
		buckets = defaultTimelineBuckets
	}
	if buckets > maxTimelineBuckets {
		buckets = maxTimelineBuckets
	}

	var (
		rels []*Relation
		err  error
	)
	if q.GraphID == 0 {
		rels, err = s.GetRelations()
	} else {
		rels, err = s.RelationsByGraph(q.GraphID)
	}
	if err != nil {
		return HeatmapSummary{}, err
	}

	out := HeatmapSummary{GroupBy: groupBy, Total: len(rels)}
	if len(rels) == 0 {
		return out, nil
	}

	// One scan of the events, not a lookup per endpoint. A graph can hold hundreds of thousands
	// of edges, and the sort view decodes only the short scalars — the same minimal decode the
	// ordering and timeline paths use.
	scan := EventFilter{Undated: consts.UndatedInclude}
	rows, err := s.matchingRows(scan)
	if err != nil {
		return HeatmapSummary{}, err
	}
	views := make(map[uint64]eventSortView, len(rows))
	for _, r := range rows {
		views[r.id] = r.view
	}

	type placedRel struct {
		at  time.Time
		key string
	}
	placeable := make([]placedRel, 0, len(rels))
	for _, r := range rels {
		if r == nil {
			continue
		}
		at, anchor, ok := relationTime(r, views)
		if !ok {
			// An endpoint with no timestamp, or one that is no longer in the store. Either way the
			// relation has no position in time and is counted rather than quietly dropped.
			out.Undated++
			continue
		}
		// A relation outside an explicitly requested window is counted and dropped, not clamped
		// into the nearest bucket. Clamping would pile everything after a zoomed window into the
		// right-most column — activity shown at a time it did not happen, which is the exact
		// failure sharing an axis with the timeline is meant to prevent.
		if (q.From != nil && at.Before(*q.From)) || (q.To != nil && at.After(*q.To)) {
			out.Outside++
			continue
		}
		placeable = append(placeable, placedRel{at: at, key: heatmapKey(groupBy, r, anchor, views)})
	}
	out.Placed = len(placeable)
	if len(placeable) == 0 {
		return out, nil
	}

	from, to := placeable[0].at, placeable[0].at
	for _, p := range placeable[1:] {
		if p.at.Before(from) {
			from = p.at
		}
		if p.at.After(to) {
			to = p.at
		}
	}
	// An explicit window wins over the data's own extent: the caller asked for THIS axis, and
	// silently narrowing it to where the relations happen to be would misalign the strip.
	if q.From != nil {
		from = *q.From
	}
	if q.To != nil {
		to = *q.To
	}
	out.From, out.To = from, to

	slices := newBucketing(from, to, buckets)
	out.Buckets = slices.buckets

	laneCounts := map[string][]int{}
	for _, p := range placeable {
		idx := slices.indexOf(p.at)
		if idx < 0 {
			continue
		}
		out.Buckets[idx].Count++
		counts, ok := laneCounts[p.key]
		if !ok {
			counts = make([]int, slices.count)
			laneCounts[p.key] = counts
		}
		counts[idx]++
	}

	out.Lanes = buildLanes(laneCounts, slices.count)
	for _, l := range out.Lanes {
		for _, c := range l.Counts {
			if c > out.Max {
				out.Max = c
			}
		}
	}
	return out, nil
}

// relationTime resolves when a relation became true — the LATER of its two endpoints — and
// returns which endpoint that was, so everything else about the cell is read from the same event.
//
// Both endpoints must be dated. A chain from a dated event to an undated one has no moment at
// which it holds: the second event's time is not "unknown but later", it is absent. So the
// relation is reported as unplaceable rather than pinned to the one timestamp that does exist.
func relationTime(r *Relation, views map[uint64]eventSortView) (time.Time, uint64, bool) {
	a, aok := views[r.From]
	b, bok := views[r.To]
	if !aok || !bok || a.Timestamp.IsZero() || b.Timestamp.IsZero() {
		return time.Time{}, 0, false
	}
	if b.Timestamp.After(a.Timestamp) {
		return b.Timestamp, r.To, true
	}
	return a.Timestamp, r.From, true
}

// heatmapKey extracts one relation's lane. An empty value becomes an explicit "(none)" lane
// rather than being dropped, so the lanes always add up to Placed.
func heatmapKey(groupBy string, r *Relation, anchor uint64, views map[uint64]eventSortView) string {
	var key string
	switch groupBy {
	case consts.HeatmapGroupRule:
		key = r.RuleID
	case consts.HeatmapGroupRelationType:
		key = r.RelationType
	case consts.HeatmapGroupCreatedBy:
		key = r.CreatedBy
	case consts.HeatmapGroupStep:
		// Step is only meaningful for an algorithm that matches a sequence. Lineage has none, and
		// neither does a hand-drawn edge, so both land in "(none)" instead of all piling into a
		// "step 1" they were never part of.
		if r.RelVersion == 0 || r.RuleID == "" || r.Algorithm == consts.AlgoLineage {
			return consts.TimelineLaneNone
		}
		key = "step " + strconv.Itoa(r.StepIndex+1)
	case consts.HeatmapGroupComputer:
		// The ANCHOR endpoint's computer — the same event the relation's time came from. Under the
		// default computer scope both endpoints agree; under global scope they need not, and
		// reading the row and the column off different events would make one cell describe two.
		if v, ok := views[anchor]; ok {
			key = v.Computer
		}
	}
	if strings.TrimSpace(key) == "" {
		return consts.TimelineLaneNone
	}
	return key
}

func validHeatmapGroup(g string) bool {
	for _, v := range consts.HeatmapGroups {
		if v == g {
			return true
		}
	}
	return false
}
