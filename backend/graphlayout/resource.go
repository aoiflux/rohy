package graphlayout

import (
	"fmt"
	"sort"

	"rohy/backend/consts"
)

// resourceLayout puts one column per distinct value of a correlation slot: a column per logon
// session, per account, per host process. Within a column, chronological.
//
// It ignores edges entirely, and that is the point. The other profiles arrange by what rohy
// INFERRED; this one arranges by what the evidence RECORDS, so a column that fills up is a
// statement about the logs rather than about the rules that ran over them.
func resourceLayout(nodes []NodeInfo, opts Options) Result {
	out := emptyResult(consts.LayoutResource)
	if len(nodes) == 0 {
		return out
	}
	slot := opts.Slot
	if slot < 0 || slot >= len(consts.CorrelationSlots) {
		slot = 0 // logon_id: the slot that ties a session to everything done under it
	}

	// Absent is collected under ONE named column rather than being treated as a shared value.
	// Two events that both fail to record a logon id have nothing in common — that is the same
	// rule the correlation vocabulary itself follows (consts.CorrelationAbsentValues), and
	// breaking it here would arrange unrelated evidence into a column that looks like a session.
	columns := map[string][]NodeInfo{}
	absent := make([]NodeInfo, 0)
	for _, n := range nodes {
		v := ""
		if slot < len(n.CorrKeys) {
			v = n.CorrKeys[slot]
		}
		if v == "" {
			absent = append(absent, n)
			continue
		}
		columns[v] = append(columns[v], n)
	}

	// Columns read left to right in the order their first event happened, which is how an
	// analyst scans them. Value order breaks the tie so two columns starting at the same instant
	// — or two with no dated event at all — never swap between runs.
	values := make([]string, 0, len(columns))
	for v := range columns {
		values = append(values, v)
	}
	sort.Slice(values, func(i, j int) bool {
		a, aok := earliest(columns[values[i]])
		b, bok := earliest(columns[values[j]])
		if aok != bok {
			return aok
		}
		if aok && !a.Equal(b) {
			return a.Before(b)
		}
		return values[i] < values[j]
	})

	x := 0.0
	appendColumn := func(label string, members []NodeInfo, undated bool) {
		sort.SliceStable(members, byTimeThenID(members))
		ids := make([]uint64, 0, len(members))
		for i, n := range members {
			out.Positions[n.ID] = Point{X: x, Y: float64(i) * opts.gapY()}
			ids = append(ids, n.ID)
		}
		out.Groups = append(out.Groups, Group{Label: label, NodeIDs: ids, Undated: undated})
		x += opts.gapX()
	}
	for _, v := range values {
		appendColumn(v, columns[v], false)
	}
	// The absent column is always last regardless of its timestamps, so the columns that carry a
	// value are never pushed rightward by the ones that do not.
	if len(absent) > 0 {
		appendColumn(consts.LayoutAbsentLabel, absent, false)
	}

	switch {
	case len(columns) == 0:
		out.Note = fmt.Sprintf(consts.MsgLayoutNoProjection, consts.CorrelationSlots[slot], consts.LayoutAbsentLabel)
	case len(absent) > 0:
		out.Note = fmt.Sprintf("%d event(s) do not record %s and are collected in the %q column, not correlated with each other",
			len(absent), consts.CorrelationSlots[slot], consts.LayoutAbsentLabel)
	}
	return out
}
