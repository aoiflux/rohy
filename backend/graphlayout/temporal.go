package graphlayout

import (
	"fmt"
	"sort"
	"time"

	"rohy/backend/consts"
)

// temporalLayout maps timestamp to x and packs nodes into as few lanes as they fit in, so the
// canvas becomes a scatter over real time rather than a grid.
//
// Lanes are assigned greedily in time order: a node takes the first lane whose last card has
// already ended before it starts. Greedy-by-start-time is optimal for interval packing (it is
// the classic minimum-track assignment), so this uses the fewest lanes possible — which matters
// because every extra lane is vertical distance the analyst has to scan.
func temporalLayout(nodes []NodeInfo, opts Options) Result {
	out := emptyResult(consts.LayoutTemporal)
	if len(nodes) == 0 {
		return out
	}

	dated := make([]NodeInfo, 0, len(nodes))
	undated := make([]NodeInfo, 0)
	for _, n := range nodes {
		if n.Dated {
			dated = append(dated, n)
		} else {
			undated = append(undated, n)
		}
	}
	sort.SliceStable(dated, byTimeThenID(dated))
	sort.Slice(undated, func(i, j int) bool { return undated[i].ID < undated[j].ID })

	var (
		lanes    [][]uint64 // lane index -> node ids, in time order
		laneEnds []float64  // the x at which each lane's last card stops occluding
	)
	if len(dated) > 0 {
		first := dated[0].Timestamp
		last := dated[len(dated)-1].Timestamp
		span := last.Sub(first)

		for _, n := range dated {
			x := 0.0
			if span > 0 {
				// float64 of a Duration is exact well past any range a case can span.
				x = opts.span() * (float64(n.Timestamp.Sub(first)) / float64(span))
			}
			lane := -1
			for i, end := range laneEnds {
				if x >= end {
					lane = i
					break
				}
			}
			if lane == -1 {
				lane = len(laneEnds)
				laneEnds = append(laneEnds, 0)
				lanes = append(lanes, nil)
			}
			laneEnds[lane] = x + opts.gapX()
			lanes[lane] = append(lanes[lane], n.ID)
			out.Positions[n.ID] = Point{X: x, Y: float64(lane) * opts.gapY()}
		}
	}

	for i, ids := range lanes {
		out.Groups = append(out.Groups, Group{Label: fmt.Sprintf("Lane %d", i+1), NodeIDs: ids})
	}

	// 🔒 Undated events are laid out in a tray of their own, separated from the last lane by a
	// blank row, and never given an x derived from a time they do not have. Placing them at the
	// left of the axis — which is what a zero timestamp would do — would assert that they are
	// the earliest events in the case. They are not early; they are untimed, and the two must
	// not render alike.
	if len(undated) > 0 {
		trayY := float64(len(lanes)+1) * opts.gapY()
		ids := make([]uint64, 0, len(undated))
		for i, n := range undated {
			out.Positions[n.ID] = Point{X: float64(i) * opts.gapX(), Y: trayY}
			ids = append(ids, n.ID)
		}
		out.Groups = append(out.Groups, Group{Label: consts.LayoutUndatedLabel, NodeIDs: ids, Undated: true})
		out.Note = fmt.Sprintf(
			"%d event(s) carry no timestamp and cannot be placed in time — they are shown in a "+
				"separate tray below, not at the start of the range", len(undated))
	}
	return out
}

// earliest returns the earliest dated timestamp among a set, and whether there was one at all.
// A set with no dated member is a real case (a column of catalogue rows) and is ordered last
// rather than treated as having happened in 1601.
func earliest(nodes []NodeInfo) (time.Time, bool) {
	var best time.Time
	found := false
	for _, n := range nodes {
		if !n.Dated {
			continue
		}
		if !found || n.Timestamp.Before(best) {
			best, found = n.Timestamp, true
		}
	}
	return best, found
}
