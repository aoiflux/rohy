// Package caseintegrity checks a case for the ways it can quietly go wrong, and says what it
// found (P30).
//
// 🔒 It READS and REPORTS. It repairs nothing on its own and deletes nothing, ever — the same
// principle that keeps orphaned findings on disk. A checker that tidied up as it went would
// destroy the evidence that something went wrong, which is the only thing a report is for. Each
// finding names a suggested action, and the analyst runs it.
//
// The distinction the whole package is built around is between a case that is BROKEN and a case
// that is merely EMPTY. "This rule found nothing" and "this rule could not possibly have found
// anything" look identical from the outside — a zero — and reporting them the same way is how an
// analyst comes to trust an answer that was never computed.
//
// It depends on narrow interfaces rather than on the concrete stores, so every detector can be
// tested against a handful of literals instead of against a live case.
package caseintegrity

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// Finding is one thing the check noticed.
type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	// Subject names what the finding is about — a rule id, a graph name, a channel. Empty when it
	// is about the case as a whole.
	Subject string `json:"subject,omitempty"`
	Message string `json:"message"`
	// Action is the fix the UI should offer, if any (consts.IntegrityAction*).
	Action string `json:"action,omitempty"`
	// Count is the size of the problem, where it has one. Zero means "not a count".
	Count int `json:"count,omitempty"`
}

// Counts is the shape of the case, so a report leads with what it looked at rather than only with
// what it disliked.
type Counts struct {
	Events         int `json:"events"`
	Relations      int `json:"relations"`
	Graphs         int `json:"graphs"`
	EnabledRules   int `json:"enabled_rules"`
	Channels       int `json:"channels"`
	Findings       int `json:"findings"`
	RulesUnchecked int `json:"rules_unchecked"`
}

// Report is one run.
type Report struct {
	RanAt time.Time `json:"ran_at"`
	// Deep reports whether the expensive index verification was run. A quick report that stayed
	// silent about it would read as "the index is fine".
	Deep       bool      `json:"deep"`
	Findings   []Finding `json:"findings"`
	Counts     Counts    `json:"counts"`
	DurationMs int64     `json:"duration_ms"`
	// Errors are checks that could not run. Reported rather than skipped: a detector that failed
	// silently would leave a clean-looking report that had not looked.
	Errors []string `json:"errors,omitempty"`
}

// Options selects how much work to do.
type Options struct {
	// Deep runs the index verification, which is proportional to the whole store. Off by default:
	// it taxes every run to re-prove something almost always true (PERFORMANCE.md §12a).
	Deep bool
}

// EventStore is the slice of the graph store the checks need.
type EventStore interface {
	Inventory() (graphene.Inventory, error)
	CountUnindexedRelations() (int, error)
	DanglingRelations() ([]*graphene.Relation, error)
	GraphRelationCounts() (map[uint64]int, error)
	EventHashes() (map[uint64]string, error)
	CountEvents(f graphene.EventFilter) (int, error)
	VerifyIndexes() error
}

// FindingStore is the slice of the findings sidecar the audit needs.
type FindingStore interface {
	AllKeys() []string
	Stale() bool
}

// GraphRegistry is the slice of the graph registry the graph checks need.
type GraphRegistry interface {
	List() []*GraphInfo
}

// GraphInfo is a registered graph, reduced to what the checks read.
type GraphInfo struct {
	ID   uint64
	Name string
}

// RuleSource is the slice of the rule registry the rule checks need.
type RuleSource interface {
	Enabled() []*rules.Rule
}

// Deps is everything a run reads. Any of them may be nil; the checks that need one are skipped
// and said to be skipped, rather than the run failing.
type Deps struct {
	Events    EventStore
	Findings  FindingStore
	Graphs    GraphRegistry
	Rules     RuleSource
	LayoutDir string
	// PayloadSize and PayloadUsed describe the cold store: how many bytes the log holds, and how
	// far the furthest referenced record extends. The difference is orphaned tail.
	PayloadSize uint64
	PayloadUsed uint64
}

// Run performs the checks and returns the report.
//
// A cancelled context stops between detectors and returns what was completed, marked with the
// cancellation. A partial report is more useful than none, provided it says it is partial.
func Run(ctx context.Context, deps Deps, opts Options) Report {
	started := time.Now()
	rep := Report{RanAt: started.UTC(), Deep: opts.Deep, Findings: []Finding{}}

	inv, ok := gatherInventory(&rep, deps)

	steps := []func(){
		func() { checkIndexes(&rep, deps, opts) },
		func() { checkUnindexedRelations(&rep, deps) },
		func() { checkDanglingRelations(&rep, deps) },
		func() { checkFindings(&rep, deps) },
		func() { checkGraphs(&rep, deps) },
		func() { checkLayouts(&rep, deps) },
		func() { checkPayloadTail(&rep, deps) },
		func() { checkProjection(&rep, inv, ok) },
		func() { checkRules(&rep, deps, inv, ok) },
	}
	for _, step := range steps {
		if ctx != nil && ctx.Err() != nil {
			rep.Errors = append(rep.Errors, "the check was cancelled before it finished")
			break
		}
		step()
	}

	sortFindings(rep.Findings)
	rep.DurationMs = time.Since(started).Milliseconds()
	return rep
}

func gatherInventory(rep *Report, deps Deps) (graphene.Inventory, bool) {
	if deps.Events == nil {
		return graphene.Inventory{}, false
	}
	inv, err := deps.Events.Inventory()
	if err != nil {
		rep.note("could not read the case inventory: " + err.Error())
		return graphene.Inventory{}, false
	}
	rep.Counts.Events = inv.Events
	rep.Counts.Channels = len(inv.Channels)
	return inv, true
}

// --- detectors ---

func checkIndexes(rep *Report, deps Deps, opts Options) {
	if !opts.Deep || deps.Events == nil {
		return
	}
	if err := deps.Events.VerifyIndexes(); err != nil {
		rep.add(Finding{
			Code: consts.IntegrityIndexDamaged, Severity: consts.IntegritySevError,
			Message: fmt.Sprintf(consts.MsgIntegrityIndexDamaged, err),
			Action:  consts.IntegrityActionRebuild,
		})
	}
}

func checkUnindexedRelations(rep *Report, deps Deps) {
	if deps.Events == nil {
		return
	}
	n, err := deps.Events.CountUnindexedRelations()
	if err != nil {
		rep.note("could not check the relation index: " + err.Error())
		return
	}
	if n > 0 {
		rep.add(Finding{
			Code: consts.IntegrityUnindexedRelation, Severity: consts.IntegritySevError,
			Message: fmt.Sprintf(consts.MsgIntegrityUnindexed, n),
			Action:  consts.IntegrityActionRepair, Count: n,
		})
	}
}

func checkDanglingRelations(rep *Report, deps Deps) {
	if deps.Events == nil {
		return
	}
	rels, err := deps.Events.DanglingRelations()
	if err != nil {
		rep.note("could not check relation endpoints: " + err.Error())
		return
	}
	if len(rels) > 0 {
		rep.add(Finding{
			Code: consts.IntegrityDanglingRelation, Severity: consts.IntegritySevError,
			Message: fmt.Sprintf(consts.MsgIntegrityDangling, len(rels)),
			Action:  consts.IntegrityActionReview, Count: len(rels),
		})
	}
}

func checkFindings(rep *Report, deps Deps) {
	if deps.Findings == nil || deps.Events == nil {
		return
	}
	keys := deps.Findings.AllKeys()
	rep.Counts.Findings = len(keys)
	if len(keys) == 0 {
		return
	}
	// A stale hash recipe orphans EVERY finding at once, and the cause is not the case — so it is
	// reported as its own thing rather than as a very large orphan count.
	if deps.Findings.Stale() {
		rep.add(Finding{
			Code: consts.IntegrityStaleFindings, Severity: consts.IntegritySevWarn,
			Message: consts.MsgIntegrityStaleFinds, Action: consts.IntegrityActionReview,
			Count: len(keys),
		})
		return
	}
	hashes, err := deps.Events.EventHashes()
	if err != nil {
		rep.note("could not audit findings: " + err.Error())
		return
	}
	live := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		live[h] = true
	}
	orphans := 0
	for _, k := range keys {
		if !live[k] {
			orphans++
		}
	}
	if orphans > 0 {
		rep.add(Finding{
			Code: consts.IntegrityOrphanFindings, Severity: consts.IntegritySevWarn,
			Message: fmt.Sprintf(consts.MsgIntegrityOrphanFinds, orphans),
			Action:  consts.IntegrityActionIngest, Count: orphans,
		})
	}
}

func checkGraphs(rep *Report, deps Deps) {
	if deps.Events == nil {
		return
	}
	counts, err := deps.Events.GraphRelationCounts()
	if err != nil {
		rep.note("could not read graph membership: " + err.Error())
		return
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	rep.Counts.Relations = total

	if deps.Graphs == nil {
		return
	}
	registered := map[uint64]string{}
	for _, g := range deps.Graphs.List() {
		if g != nil {
			registered[g.ID] = g.Name
		}
	}
	rep.Counts.Graphs = len(registered)

	// A graph id in the store with no registry entry: its relations exist and nothing in the app
	// can reach them.
	ids := make([]uint64, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if _, ok := registered[id]; !ok && counts[id] > 0 {
			rep.add(Finding{
				Code: consts.IntegrityOrphanGraph, Severity: consts.IntegritySevWarn,
				Subject: fmt.Sprintf("%d", id),
				Message: fmt.Sprintf(consts.MsgIntegrityOrphanGraph, id, counts[id]),
				Action:  consts.IntegrityActionReview, Count: counts[id],
			})
		}
	}

	// An empty graph is not damage — it is what a rule that matched nothing leaves behind — so it
	// is information, not a warning.
	names := make([]uint64, 0, len(registered))
	for id := range registered {
		names = append(names, id)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	for _, id := range names {
		if counts[id] == 0 {
			rep.add(Finding{
				Code: consts.IntegrityEmptyGraph, Severity: consts.IntegritySevInfo,
				Subject: registered[id],
				Message: fmt.Sprintf(consts.MsgIntegrityEmptyGraph, registered[id]),
			})
		}
	}
}

func checkLayouts(rep *Report, deps Deps) {
	if deps.LayoutDir == "" || deps.Graphs == nil {
		return
	}
	entries, err := os.ReadDir(deps.LayoutDir)
	if err != nil {
		if !os.IsNotExist(err) {
			rep.note("could not read the layout directory: " + err.Error())
		}
		return
	}
	registered := map[string]bool{}
	for _, g := range deps.Graphs.List() {
		if g != nil {
			registered[fmt.Sprintf("canvas-%d.json", g.ID)] = true
		}
	}
	orphans := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "canvas-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if !registered[name] {
			orphans++
		}
	}
	if orphans > 0 {
		rep.add(Finding{
			Code: consts.IntegrityOrphanLayout, Severity: consts.IntegritySevInfo,
			Message: fmt.Sprintf(consts.MsgIntegrityOrphanLayout, orphans),
			Action:  consts.IntegrityActionReview, Count: orphans,
		})
	}
}

func checkPayloadTail(rep *Report, deps Deps) {
	if deps.PayloadSize == 0 || deps.PayloadSize <= deps.PayloadUsed {
		return
	}
	tail := deps.PayloadSize - deps.PayloadUsed
	// Waste, not damage: the payload log is written BEFORE the record that references it, so an
	// interrupted ingest leaves bytes nothing points at. That ordering is deliberate — the
	// reverse leaves a record pointing at a payload that was never written, which cannot be
	// recovered (PERFORMANCE.md §14).
	rep.add(Finding{
		Code: consts.IntegrityPayloadTail, Severity: consts.IntegritySevInfo,
		Message: fmt.Sprintf(consts.MsgIntegrityPayloadTail, tail),
		Count:   int(tail),
	})
}

func checkProjection(rep *Report, inv graphene.Inventory, ok bool) {
	if !ok || inv.StaleProjection == 0 {
		return
	}
	rep.add(Finding{
		Code: consts.IntegrityStaleProjection, Severity: consts.IntegritySevWarn,
		Message: fmt.Sprintf(consts.MsgIntegrityStaleProj, inv.StaleProjection),
		Action:  consts.IntegrityActionBackfill, Count: inv.StaleProjection,
	})
}

// checkRules is detectors 10 and 11: missing channels, and rule inertness.
//
// The three verdicts it distinguishes are the point of the whole feature:
//
//	inert   — a step has NO matching events, so the rule cannot fire at all
//	blocked — every step has events, but no event carries a field the rule matches on
//	unmatched — everything is present and the pattern simply does not occur
//
// Only the third is a clean result. Reporting all three as "0 matches" is how an analyst comes to
// trust an answer that was never computed.
func checkRules(rep *Report, deps Deps, inv graphene.Inventory, haveInv bool) {
	if deps.Rules == nil || deps.Events == nil {
		return
	}
	enabled := deps.Rules.Enabled()
	rep.Counts.EnabledRules = len(enabled)

	undeclared := 0
	// Counting per event id once, rather than per rule, because rules overlap heavily — thirty
	// built-ins share a handful of ids between them.
	counts := map[string]int{}
	countOf := func(eventID string) int {
		if n, ok := counts[eventID]; ok {
			return n
		}
		n, err := deps.Events.CountEvents(graphene.EventFilter{
			EventID: eventID, Undated: consts.UndatedInclude,
		})
		if err != nil {
			rep.note(fmt.Sprintf("could not count %s events: %v", eventID, err))
			n = -1
		}
		counts[eventID] = n
		return n
	}

	for _, r := range enabled {
		if r == nil {
			continue
		}
		// --- missing channel ---
		//
		// ⚠️ A rule with no declared channels is NOT reported. Silence here means "not declared",
		// never "fine" — and the report says how many were unchecked for exactly that reason.
		if len(r.Channels) == 0 {
			undeclared++
		} else if haveInv {
			for _, ch := range r.Channels {
				if ch == "" {
					continue
				}
				if inv.Channels[ch] == 0 {
					rep.add(Finding{
						Code: consts.IntegrityMissingChannel, Severity: consts.IntegritySevWarn,
						Subject: r.ID,
						Message: fmt.Sprintf(consts.MsgIntegrityMissingChan, r.Name, ch),
						Action:  consts.IntegrityActionIngest,
					})
				}
			}
		}

		// --- inertness ---
		missing := missingSteps(r, countOf)
		if len(missing) > 0 {
			rep.add(Finding{
				Code: consts.IntegrityRuleInert, Severity: consts.IntegritySevWarn,
				Subject: r.ID,
				Message: fmt.Sprintf(consts.MsgIntegrityRuleInert, r.Name, strings.Join(missing, ", ")),
				Action:  consts.IntegrityActionIngest,
			})
			continue // it cannot fire, so asking whether it is blocked adds nothing
		}

		// --- live but blocked ---
		if haveInv {
			if slot, blocked := blockedOn(r, inv); blocked {
				rep.add(Finding{
					Code: consts.IntegrityRuleBlocked, Severity: consts.IntegritySevWarn,
					Subject: r.ID,
					Message: fmt.Sprintf(consts.MsgIntegrityRuleBlocked, r.Name, slot),
					Action:  consts.IntegrityActionBackfill,
				})
			}
		}
	}

	rep.Counts.RulesUnchecked = undeclared
	if undeclared > 0 {
		rep.add(Finding{
			Code: consts.IntegrityChannelUndeclared, Severity: consts.IntegritySevInfo,
			Message: fmt.Sprintf(consts.MsgIntegrityUndeclared, undeclared),
			Count:   undeclared,
		})
	}
}

// missingSteps returns the distinct event ids a rule needs that the case does not hold. A count
// that could not be read (-1) is treated as present: reporting a rule inert because a query
// failed would be a wrong finding produced by an internal error.
func missingSteps(r *rules.Rule, countOf func(string) int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 2)
	for _, id := range stepIDs(r) {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if countOf(id) == 0 {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// stepIDs is the event ids a rule depends on. Lineage has no sequence — it depends on its
// creation ids instead — so reading Sequence alone would report every lineage rule as trivially
// satisfiable.
func stepIDs(r *rules.Rule) []string {
	if r.AlgorithmOrDefault() == consts.AlgoLineage {
		return r.LineageCreateIDsOrDefault()
	}
	return r.Sequence
}

// blockedOn reports the first correlation field a rule matches on that no event in the case
// carries. A rule that is blocked will report zero matches, which is not the same as a clean
// result — and on an un-backfilled case it is the usual reason a field rule looks empty.
func blockedOn(r *rules.Rule, inv graphene.Inventory) (string, bool) {
	slots, ok := r.MatchSlots()
	if !ok {
		return "", false
	}
	for i, slot := range slots {
		if slot < 0 || slot >= len(inv.SlotCoverage) {
			continue
		}
		if inv.SlotCoverage[slot] == 0 {
			return r.MatchFields[i], true
		}
	}
	return "", false
}

// --- report helpers ---

func (r *Report) add(f Finding) { r.Findings = append(r.Findings, f) }

// note records a check that could not run. Reported rather than swallowed: a detector that failed
// silently would leave a clean-looking report that had not actually looked.
func (r *Report) note(msg string) { r.Errors = append(r.Errors, msg) }

// sortFindings groups by severity, then by code, then by subject, so the same case produces the
// same report every time and the most serious thing is at the top.
func sortFindings(fs []Finding) {
	rank := map[string]int{}
	for i, s := range consts.IntegritySeverities {
		rank[s] = i
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if rank[fs[i].Severity] != rank[fs[j].Severity] {
			return rank[fs[i].Severity] < rank[fs[j].Severity]
		}
		if fs[i].Code != fs[j].Code {
			return fs[i].Code < fs[j].Code
		}
		return fs[i].Subject < fs[j].Subject
	})
}
