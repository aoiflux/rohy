// Package graphlayout arranges a graph's nodes into positions. It is pure: node metadata and
// relations in, coordinates out. It opens nothing, reads nothing, and writes nothing.
//
// The arithmetic lives here rather than in the canvas component for three reasons, in order of
// how much they mattered:
//
//  1. A layout is a claim about the evidence. "This process spawned that one" and "this happened
//     before that" are readable off the arrangement, so an arrangement that is subtly wrong is a
//     wrong finding rendered convincingly. That needs tests, and a grid computed inside a Svelte
//     method has none.
//  2. Determinism. The same graph must produce the same picture every time, or an analyst cannot
//     return to a case and recognise it. Every ordering below is total — ties break on node id —
//     precisely so nothing depends on map iteration order.
//  3. Reuse. Snapshots and any future export need positions without a browser to compute them in.
//
// The canvas stays a renderer, and the existing layout sidecar persists whatever it is handed;
// this package adds no storage of its own.
package graphlayout

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// NodeInfo is the projection of an event this package needs. It is a local type rather than
// *graphene.Event so that the layouts can be tested against a handful of literals instead of
// against fully-formed store records, and so nothing here can accidentally start depending on
// the payload cold store.
type NodeInfo struct {
	ID uint64
	// Timestamp is meaningful only when Dated. An undated event (a catalogue row) has no time,
	// which is different from having a zero one, and the temporal profile is built around that
	// distinction rather than around a sentinel.
	Timestamp time.Time
	Dated     bool
	// CorrKeys is the correlation projection, read by the resource profile. An event ingested
	// before v0.2.0 has none, which the profile reports rather than silently grouping.
	CorrKeys []string
	// Label is a short human name for the node, used only to name a Group it heads (a lineage
	// tree's root). It never affects a position — an arrangement that changed when an event's
	// display text changed would be a layout of the labels, not of the evidence.
	Label string
}

// Point is a world-space position on the canvas, matching the layout sidecar's shape.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Group is a set of nodes the profile arranged together and can name: a resource column, a
// temporal lane, a tree root. The canvas uses it for headings and hulls, and it is what lets a
// layout explain itself instead of being an arrangement the analyst has to reverse-engineer.
type Group struct {
	Label   string   `json:"label"`
	NodeIDs []uint64 `json:"node_ids"`
	// Undated marks the group as the tray for nodes the profile could not place on its primary
	// axis. The canvas renders it apart and labelled, so a missing timestamp never reads as a
	// position on the timeline.
	Undated bool `json:"undated,omitempty"`
}

// Result is a computed layout. Positions is what the canvas applies; Groups and Note are what
// keep it honest — a profile that had to make a judgement call says so here rather than
// rendering it as fact.
type Result struct {
	Profile   string           `json:"profile"`
	Positions map[uint64]Point `json:"positions"`
	Groups    []Group          `json:"groups"`
	// Note is a plain-language caveat about this particular arrangement — undated events set
	// aside, a cycle broken, a slot nothing carries. Empty when there is nothing to say.
	Note string `json:"note,omitempty"`
}

// Options tunes a layout without changing what it means. Zero values take the consts defaults,
// so a caller that has no opinion passes Options{}.
type Options struct {
	GapX float64
	GapY float64
	// Slot is the correlation slot index the resource profile groups by. Use SlotByName to
	// resolve it from a rule-author-facing name.
	Slot int
	// Span is the world-space width the temporal profile maps the whole time range onto.
	Span float64
}

func (o Options) gapX() float64 {
	if o.GapX > 0 {
		return o.GapX
	}
	return consts.LayoutGapX
}

func (o Options) gapY() float64 {
	if o.GapY > 0 {
		return o.GapY
	}
	return consts.LayoutGapY
}

func (o Options) span() float64 {
	if o.Span > 0 {
		return o.Span
	}
	return consts.LayoutTemporalSpan
}

// SlotByName resolves a correlation field name to the slot index the resource profile groups by.
// It is here rather than at the call site so the API layer never has to know how the projection
// is indexed.
func SlotByName(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1, nil
	}
	i, ok := consts.CorrelationSlotIndex(name)
	if !ok {
		return -1, fmt.Errorf(consts.MsgLayoutUnknownSlot, name, strings.Join(consts.CorrelationSlots, ", "))
	}
	return i, nil
}

// Compute arranges nodes according to a profile.
//
// Relations are read for structure only — direction and endpoints. Edges naming a node that is
// not in the node set are ignored rather than rejected: a graph's edge set and its canvas
// membership are maintained separately, so they can legitimately disagree, and refusing to lay
// out a graph because of that would fail in exactly the situation the analyst most wants a
// layout.
func Compute(profile string, nodes []NodeInfo, rels []*graphene.Relation, opts Options) (Result, error) {
	if profile == "" {
		profile = consts.DefaultLayoutProfile
	}
	if len(nodes) > consts.LayoutMaxNodes {
		return Result{}, fmt.Errorf(consts.MsgLayoutTooManyNodes, len(nodes), consts.LayoutMaxNodes)
	}

	// A deterministic starting order, so every profile below can rely on it. Sorting once here
	// rather than inside each profile is the same "prepare once" shape autograph uses.
	ordered := append([]NodeInfo(nil), nodes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	switch profile {
	case consts.LayoutSequence:
		return sequenceLayout(ordered, rels, opts), nil
	case consts.LayoutLineage:
		return lineageLayout(ordered, rels, opts), nil
	case consts.LayoutResource:
		return resourceLayout(ordered, opts), nil
	case consts.LayoutTemporal:
		return temporalLayout(ordered, opts), nil
	default:
		return Result{}, fmt.Errorf(consts.MsgLayoutUnknownProfile, profile, strings.Join(consts.LayoutProfiles, ", "))
	}
}

// --- shared helpers ---

// edgeSet reduces relations to the adjacency this package needs, dropping edges whose endpoints
// are not both on the canvas and dropping self-loops (a node cannot rank after itself, and a
// self-loop in a tidy tree is an infinite descent).
//
// Duplicate edges are kept as one: two rules can both link the same pair, and for RANKING that
// is one constraint, not two.
type edgeSet struct {
	out map[uint64][]uint64
	in  map[uint64][]uint64
}

func buildEdges(nodes []NodeInfo, rels []*graphene.Relation) edgeSet {
	present := make(map[uint64]bool, len(nodes))
	for _, n := range nodes {
		present[n.ID] = true
	}
	seen := make(map[[2]uint64]bool, len(rels))
	es := edgeSet{out: map[uint64][]uint64{}, in: map[uint64][]uint64{}}
	for _, r := range rels {
		if r == nil || r.From == r.To || !present[r.From] || !present[r.To] {
			continue
		}
		key := [2]uint64{r.From, r.To}
		if seen[key] {
			continue
		}
		seen[key] = true
		es.out[r.From] = append(es.out[r.From], r.To)
		es.in[r.To] = append(es.in[r.To], r.From)
	}
	// Adjacency is walked in id order everywhere below, so sort it once here.
	for k := range es.out {
		sort.Slice(es.out[k], func(i, j int) bool { return es.out[k][i] < es.out[k][j] })
	}
	for k := range es.in {
		sort.Slice(es.in[k], func(i, j int) bool { return es.in[k][i] < es.in[k][j] })
	}
	return es
}

// byTimeThenID is the total order every profile falls back to. Undated nodes sort AFTER dated
// ones rather than at the epoch: an event with no timestamp is not an event from 1601, and
// sorting it first would put it at the head of a chain it has no claim to.
func byTimeThenID(nodes []NodeInfo) func(i, j int) bool {
	return func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.Dated != b.Dated {
			return a.Dated
		}
		if a.Dated && !a.Timestamp.Equal(b.Timestamp) {
			return a.Timestamp.Before(b.Timestamp)
		}
		return a.ID < b.ID
	}
}

// indexByID maps ids back to their metadata. Every profile needs it and none of them should
// build it twice.
func indexByID(nodes []NodeInfo) map[uint64]NodeInfo {
	m := make(map[uint64]NodeInfo, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n
	}
	return m
}

func emptyResult(profile string) Result {
	return Result{Profile: profile, Positions: map[uint64]Point{}}
}
