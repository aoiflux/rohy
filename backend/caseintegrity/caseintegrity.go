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

// ForensicStore is the slice of the store the posture checks read (plan §14.8).
//
// It is a SEPARATE interface from EventStore, and deliberately optional, because the questions
// differ in kind: EventStore answers "is this case broken", this answers "what can this case
// prove". A Deps with no ForensicStore reports the posture as unknown rather than as fine.
type ForensicStore interface {
	ForensicStatus() graphene.ForensicStatus
	VerifyImage() (graphene.ImageCheck, error)
}

// RootHistory is the slice of the fingerprint sidecar the retained-root check reads.
//
// 🔒 The method is `Exported`, not `Retained`, and the difference is the whole point: rohy can see
// that a copy was taken out of the app and cannot see whether one is held anywhere. A nil
// RootHistory reports the caveat unconditionally, which is the safe direction — it produces the
// warning rather than suppressing it.
//
// It returns a time and a ChainCheck rather than custody's types so this package needs no
// dependency on the sidecar at all — the same reason every other interface here is two methods wide.
type RootHistory interface {
	ExportedAt(snapshot string) (time.Time, bool)
	// Chain places the current image (nil when never sealed) against the recorded seals.
	Chain(current *graphene.SnapshotRoots) ChainCheck
}

// ChainCheck is where the case's current image sits against its history of seals (plan §15.5),
// reduced to what the findings say.
type ChainCheck struct {
	// State is one of consts.Chain*.
	State string
	// Gaps counts recorded seals that do not directly follow the one recorded before them.
	Gaps int
	// RolledBackTo and Later describe a rolled-back image: when the seal it matches was recorded,
	// and how many seals were recorded after it.
	RolledBackTo time.Time
	Later        int
	// Forks counts recorded seals made from an image older than the seal recorded before them — a
	// rollback that was sealed again. ForkFrom is when that older seal was recorded, ForkSkipped how
	// many recorded seals the first fork left out.
	Forks       int
	ForkFrom    time.Time
	ForkSkipped int
	// Kept lists earlier history documents that could not be used and were kept aside.
	Kept []KeptHistory
	// Held is why this open left an unusable history in place and is recording nothing; empty when
	// it is not held.
	Held string
}

// KeptHistory is one history document set aside because it could not be used.
type KeptHistory struct {
	Reason string
	Path   string
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
	Forensics ForensicStore
	Roots     RootHistory
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
		func() { checkForensicPosture(&rep, deps, opts) },
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

// checkForensicPosture reports what the case can and cannot prove (plan §14.8).
//
// 🔒 THESE ARE POSTURES, NOT FAULTS. Every one of them is the honest consequence of when and how
// the case was written, and most of them are the state of every case rohy has ever produced. They
// are reported because silence about them is what lets an analyst assume a guarantee that is not
// there — the same reason a quick integrity check says the deep index verification did not run.
//
// 🔒 A posture that could not be READ is reported as unknown, never as fine. That is the rule the
// whole package is built on, and it matters most here: a forensic all-clear from a check that did
// not look is the most damaging thing this report could produce.
func checkForensicPosture(rep *Report, deps Deps, opts Options) {
	if deps.Forensics == nil {
		rep.note("the case's integrity posture could not be read, so nothing here reports on it")
		return
	}

	st := deps.Forensics.ForensicStatus()
	if !st.Available {
		// Not a warning. There is no image to hash on this backend, so there is nothing wrong —
		// but reporting nothing at all would leave the section looking clean.
		rep.add(Finding{
			Code: consts.IntegrityForensicsAbsent, Severity: consts.IntegritySevInfo,
			Message: fmt.Sprintf(consts.MsgIntegrityForensicsAbsent, st.Reason),
		})
		return
	}

	// The history's own findings — a reset, a rollback that was sealed again, a gap — do not depend
	// on the image, so they are reported before the returns below rather than lost behind them.
	// Without an image the current-image placement simply has nothing to place.
	checkRootChain(rep, deps, st)

	// An unreadable fingerprint comes before "never compacted" and must not present as it: that
	// one is normal and offers compaction, which would write a fresh image over whatever made the
	// read fail. A fault, so an error, and no action — the remedy depends on the cause.
	if st.RootsError != "" {
		rep.add(Finding{
			Code: consts.IntegrityRootsUnreadable, Severity: consts.IntegritySevError,
			Message: fmt.Sprintf(consts.MsgIntegrityRootsUnreadable, st.RootsError),
		})
		return
	}

	// Never compacted comes first and returns: with no image, every check below it is asking
	// about a file that does not exist, and answering them would produce three findings for one
	// situation.
	if !st.Compacted {
		rep.add(Finding{
			Code: consts.IntegrityNeverCompacted, Severity: consts.IntegritySevWarn,
			Message: consts.MsgIntegrityNeverCompacted, Action: consts.IntegrityActionCompact,
		})
		return
	}

	if !st.Signed {
		rep.add(Finding{
			Code: consts.IntegrityUnsignedCommits, Severity: consts.IntegritySevWarn,
			Message: consts.MsgIntegrityUnsignedCommits,
			// No action: there is nothing to press. Signing is a decision about key custody made
			// when a case is created, and offering a button here would imply the existing writes
			// could be attributed retrospectively. They cannot.
		})
	}

	checkRetainedRoot(rep, deps, st)
	checkImage(rep, deps, opts)
}

// checkRootChain places the current image against the history of seals (plan §15.5).
//
// 🔒 This is the one check that catches an image which is internally perfect but is not the latest
// one rohy sealed: a database restored from a backup, or copied in from another case. Every other
// check passes such an image, because nothing about it is broken. What it cannot catch is a careful
// forger, who can rewrite the history as easily as the image — so none of these findings is phrased
// as a guarantee, and an intact chain adds no finding at all: the retained-root finding above
// already says what an internal check is worth.
//
// A nil history reports nothing here. The retained-root caveat already stands for it, and
// inventing chain findings from no history would be reporting a check that never ran.
func checkRootChain(rep *Report, deps Deps, st graphene.ForensicStatus) {
	if deps.Roots == nil {
		return
	}
	c := deps.Roots.Chain(st.Roots)

	for _, k := range c.Kept {
		rep.add(Finding{
			Code: consts.IntegrityRootsHistoryUnreadable, Severity: consts.IntegritySevWarn,
			Message: fmt.Sprintf(consts.MsgIntegrityRootsHistoryKept, k.Reason, k.Path),
		})
	}
	if c.Held != "" {
		rep.add(Finding{
			Code: consts.IntegrityRootsHistoryUnreadable, Severity: consts.IntegritySevWarn,
			Message: fmt.Sprintf(consts.MsgIntegrityRootsHistoryHeld, c.Held),
		})
	}

	// 🔒 A rollback is reported from the history itself when it was sealed again, so it is the same
	// error however many seals follow — not a gap it fades into. Review, never Compact: no seal
	// undoes it, and the action must not suggest one does.
	if c.Forks > 0 {
		rep.add(Finding{
			Code: consts.IntegrityRootsRolledBack, Severity: consts.IntegritySevError,
			Message: fmt.Sprintf(consts.MsgIntegrityRootsForked, c.ForkFrom.Format(time.RFC3339), c.ForkSkipped),
			Action:  consts.IntegrityActionReview,
		})
	}

	switch c.State {
	case consts.ChainRolledBack:
		rep.add(Finding{
			Code: consts.IntegrityRootsRolledBack, Severity: consts.IntegritySevError,
			Message: fmt.Sprintf(consts.MsgIntegrityRootsRolledBack, c.RolledBackTo.Format(time.RFC3339), c.Later),
			Action:  consts.IntegrityActionReview,
		})
	case consts.ChainDiverged:
		rep.add(Finding{
			Code: consts.IntegrityRootsDiverged, Severity: consts.IntegritySevWarn,
			Message: consts.MsgIntegrityRootsDiverged, Action: consts.IntegrityActionReview,
		})
	case consts.ChainUnrecordedSeal:
		rep.add(Finding{
			Code: consts.IntegrityRootsUnrecordedSeal, Severity: consts.IntegritySevInfo,
			Message: consts.MsgIntegrityRootsUnrecorded,
		})
	}

	if c.Gaps > 0 {
		rep.add(Finding{
			Code: consts.IntegrityRootsChainGap, Severity: consts.IntegritySevWarn,
			Message: fmt.Sprintf(consts.MsgIntegrityRootsChainGap, c.Gaps),
		})
	}
}

// checkRetainedRoot reports whether a copy of the case fingerprint has been taken out of the app.
//
// 🔒 Reported at every depth, and reported even when the image verifies. An internally-consistent
// check is the store vouching for itself, and that stays true however many of rohy's own checks
// pass — so silence here would let a verified image read as an unaltered one.
//
// 🔒 The satisfied state is INFO, not silence, and it says what rohy cannot see. Whether the copy
// still exists, whether it went anywhere beyond the reach of someone able to rewrite this case,
// whether it was transcribed correctly — none of that is observable from in here. A tick would
// claim all three. And the value of an external copy comes precisely from being outside what this
// process can observe, so a process that could confirm it would be confirming something worth less.
func checkRetainedRoot(rep *Report, deps Deps, st graphene.ForensicStatus) {
	snapshot := ""
	if st.Roots != nil {
		snapshot = st.Roots.Snapshot
	}

	// No history to consult, or no copy taken: the caveat stands. A nil Roots defaults to the
	// warning rather than suppressing it — a history lost to a corrupt sidecar must not read as
	// "a copy was taken".
	if deps.Roots == nil || snapshot == "" {
		rep.add(Finding{
			Code: consts.IntegrityNoRootRetained, Severity: consts.IntegritySevWarn,
			Message: consts.MsgIntegrityNoRootRetained, Action: consts.IntegrityActionRetainRoot,
		})
		return
	}

	at, ok := deps.Roots.ExportedAt(snapshot)
	if !ok {
		rep.add(Finding{
			Code: consts.IntegrityNoRootRetained, Severity: consts.IntegritySevWarn,
			Message: consts.MsgIntegrityNoRootRetained, Action: consts.IntegrityActionRetainRoot,
		})
		return
	}
	rep.add(Finding{
		Code: consts.IntegrityRootExported, Severity: consts.IntegritySevInfo,
		Message: fmt.Sprintf(consts.MsgIntegrityRootExported, at.Format(time.RFC3339)),
	})
}

// checkImage re-derives the image, in deep mode only.
//
// 🔒 Deep only, and a quick run SAYS it did not look rather than staying silent — exactly the
// judgement the index verification already carries. The measurement is in PERFORMANCE.md §18:
// ~626 ms and 449 MB of allocation churn on a 100k case, which is not something to spend on
// every press of a button labelled "check".
func checkImage(rep *Report, deps Deps, opts Options) {
	if !opts.Deep {
		rep.add(Finding{
			Code: consts.IntegrityImageUnverified, Severity: consts.IntegritySevWarn,
			Message: consts.MsgIntegrityImageUnverified, Action: consts.IntegrityActionVerifyImage,
		})
		return
	}

	check, err := deps.Forensics.VerifyImage()
	if err != nil {
		rep.note("could not verify the case image: " + err.Error())
		return
	}

	switch check.Digest {
	case graphene.DigestMismatch:
		rep.add(Finding{
			Code: consts.IntegrityImageAltered, Severity: consts.IntegritySevError,
			Message: consts.MsgIntegrityImageAltered, Action: consts.IntegrityActionReview,
		})
	case graphene.DigestAbsent:
		// An image digests never covered. Distinct from a case never compacted, and from a
		// failure: there was nothing to check, so nothing passed.
		rep.add(Finding{
			Code: consts.IntegrityImageUncovered, Severity: consts.IntegritySevWarn,
			Message: consts.MsgIntegrityImageUncovered, Action: consts.IntegrityActionCompact,
		})
	case graphene.DigestMatch:
		// The roots are the second, stronger check: the digest says the bytes are unchanged, the
		// roots say those bytes are the records they claim to be. A roots failure on a matching
		// digest is the serious combination — it means the file was rewritten coherently.
		if check.RootsError != "" {
			rep.add(Finding{
				Code: consts.IntegrityImageAltered, Severity: consts.IntegritySevError,
				Message: consts.MsgIntegrityImageAltered + " (" + check.RootsError + ")",
				Action:  consts.IntegrityActionReview,
			})
		}
	}
}

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
