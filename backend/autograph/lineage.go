package autograph

import (
	"sort"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// lineageAlgorithm reconstructs process ancestry from process-creation records, linking each
// creation event to the creation event of the process that spawned it.
//
// WHY THIS IS NOT "JOIN ON PARENT PID"
//
// Windows reuses process IDs, aggressively and constantly. A busy host cycles through the PID
// space in hours. So over a case spanning days, "the 4688 whose NewProcessId equals this
// event's parent PID" matches many events, most of them unrelated processes that merely held
// the same number at a different time. Joining on the PID alone does not produce a slightly
// noisy graph — it produces a confidently wrong one, and the wrongness is invisible, because
// every edge looks exactly like a correct edge.
//
// The fix is to treat a PID as an identifier that is only valid for an interval. Each creation
// event opens an interval for its new PID; the interval closes at the next creation of that
// same PID on the same host, or at its exit event, or not at all. A child's parent is then the
// process whose interval CONTAINED the child's creation time — a lookup in time, not in
// number.
//
// Where no such interval exists — overwhelmingly because the parent was created before the
// ingest window opened — nothing is emitted and the count is reported. Guessing the nearest
// candidate would be the same class of error the interval table exists to prevent.
//
// The child/parent pairing is derived rather than assumed, because providers disagree about
// which field means what:
//
//	child  pid = new_process_id    if present, else process_id
//	parent pid = parent_process_id if present, else process_id when new_process_id is present
//
// A Windows 4688 carries NewProcessId (the child) and ProcessId (the creator); a Sysmon
// process-creation event carries ProcessId (the child) and ParentProcessId. Both read
// correctly through the rules above without either being special-cased.
type lineageAlgorithm struct{}

// procInterval is one lifetime of one PID on one host.
type procInterval struct {
	start time.Time
	end   time.Time // zero means "still open"
	event *graphene.Event
}

func (lineageAlgorithm) Generate(spec *rules.Spec, ds *Dataset) Result {
	var res Result
	if spec == nil || ds == nil {
		return res
	}
	res.SkippedUndated = ds.SkippedUndated
	res.StaleCorrelationKeys = ds.StaleCorrelationKeys

	createIDs := map[string]bool{}
	for _, id := range spec.LineageCreateIDsOrDefault() {
		createIDs[id] = true
	}

	for _, group := range ds.Groups(spec.ScopeOrDefault()) {
		lineageGroup(spec, group.Events, createIDs, &res)
	}
	return res
}

// lineageGroup builds the lifetime table for one host and resolves each creation event's
// parent against it.
func lineageGroup(spec *rules.Spec, events []*graphene.Event, createIDs map[string]bool, res *Result) {
	lifetimes, creations := buildLifetimes(events, createIDs)

	for _, child := range creations {
		parentPID := lineageParentPID(child)
		if parentPID == "" {
			// No parent recorded at all. Not an unresolved parent — there was nothing to
			// resolve — so it is not counted as one.
			continue
		}
		parent := resolveParent(lifetimes[parentPID], child.Timestamp)
		if parent == nil {
			res.UnresolvedParents++
			continue
		}
		if res.Matches >= maxMatches {
			res.Truncated = true
			res.Dropped++
			continue
		}
		res.Matches++
		emitLineageEdge(spec, parent, child, parentPID, res)

		// Transitive ancestry, when the rule asks for it. Off by default: these links are
		// derivable by walking the direct ones, so emitting them multiplies the edge count
		// without adding information — and they are stamped at a lower confidence because they
		// are derived rather than read from a record.
		if spec.LineageDepth > 0 {
			emitAncestors(spec, lifetimes, parent, child, res)
		}
	}
}

// buildLifetimes returns, per PID, the ordered intervals during which that PID was in use, and
// the creation events in chronological order.
func buildLifetimes(events []*graphene.Event, createIDs map[string]bool) (map[string][]procInterval, []*graphene.Event) {
	lifetimes := map[string][]procInterval{}
	var creations []*graphene.Event

	for _, e := range events {
		switch {
		case createIDs[e.EventID]:
			creations = append(creations, e)
			pid := lineageChildPID(e)
			if pid == "" {
				continue
			}
			// A new creation of a PID closes the previous interval for it. Events arrive
			// chronologically, so the interval to close is always the last one recorded.
			if prior := lifetimes[pid]; len(prior) > 0 && prior[len(prior)-1].end.IsZero() {
				lifetimes[pid][len(prior)-1].end = e.Timestamp
			}
			lifetimes[pid] = append(lifetimes[pid], procInterval{start: e.Timestamp, event: e})

		case e.EventID == consts.LineageExitID:
			// An exit closes the open interval for that PID, which tightens every subsequent
			// lookup: without it a PID's interval runs until its next creation, and a child
			// created in the gap would resolve to a process that had already exited.
			pid := lineageChildPID(e)
			if pid == "" {
				pid = e.CorrelationKey(consts.SlotProcessID)
			}
			if prior := lifetimes[pid]; len(prior) > 0 && prior[len(prior)-1].end.IsZero() {
				lifetimes[pid][len(prior)-1].end = e.Timestamp
			}
		}
	}
	return lifetimes, creations
}

// resolveParent finds the interval containing t, or nil when the PID was not in use then.
//
// Intervals are built in chronological order, so a binary search finds the candidate whose
// start is at or before t; the containment check then rejects it if that lifetime had already
// ended. Returning nil is a real answer and the common one at the start of a case, where every
// long-running process was created before the log begins.
func resolveParent(intervals []procInterval, t time.Time) *graphene.Event {
	if len(intervals) == 0 {
		return nil
	}
	i := sort.Search(len(intervals), func(i int) bool {
		return intervals[i].start.After(t)
	}) - 1
	if i < 0 {
		return nil
	}
	iv := intervals[i]
	if !iv.end.IsZero() && !t.Before(iv.end) {
		return nil // that PID's lifetime had ended before the child was created
	}
	return iv.event
}

// emitLineageEdge records one parent → child link.
func emitLineageEdge(spec *rules.Spec, parent, child *graphene.Event, parentPID string, res *Result) {
	rel := graphene.Relation{
		From:            parent.ID,
		To:              child.ID,
		RelationType:    spec.RelationType,
		Label:           spec.LabelFor(0),
		ConfidenceScore: consts.ConfidenceExactMatch,
		CreatedBy:       consts.CreatedBySystem,
	}
	basis := []string{"ppid=" + parentPID}
	if image := child.CorrelationKey(consts.SlotProcessName); image != "" {
		basis = append(basis, "image="+image)
	}
	if parentImage := parent.CorrelationKey(consts.SlotProcessName); parentImage != "" {
		basis = append(basis, "parent="+parentImage)
	}
	rel.StampProvenance("", consts.AlgoLineage, matchID([]*graphene.Event{parent, child}), 0, basis)
	res.Relations = append(res.Relations, rel)
}

// emitAncestors walks up from a resolved parent, emitting derived links to further ancestors.
func emitAncestors(spec *rules.Spec, lifetimes map[string][]procInterval,
	parent, child *graphene.Event, res *Result) {

	current := parent
	for depth := 1; depth <= spec.LineageDepth && depth <= consts.LineageMaxDepth; depth++ {
		pid := lineageParentPID(current)
		if pid == "" {
			return
		}
		ancestor := resolveParent(lifetimes[pid], current.Timestamp)
		if ancestor == nil {
			return
		}
		rel := graphene.Relation{
			From:            ancestor.ID,
			To:              child.ID,
			RelationType:    spec.RelationType,
			ConfidenceScore: consts.ConfidenceLineageTransitive,
			CreatedBy:       consts.CreatedBySystem,
		}
		// Below full confidence, and the basis says why: this link was DERIVED by walking
		// direct links, not read from any single record. Presenting it identically to a direct
		// parentage would overstate what the evidence says.
		rel.StampProvenance("", consts.AlgoLineage,
			matchID([]*graphene.Event{ancestor, child}), depth,
			[]string{"ancestor depth=" + itoa(depth), "derived from direct lineage"})
		res.Relations = append(res.Relations, rel)
		current = ancestor
	}
}

// lineageChildPID is the PID of the process a creation event is ABOUT.
func lineageChildPID(e *graphene.Event) string {
	if pid := e.CorrelationKey(consts.SlotNewProcessID); pid != "" {
		return pid
	}
	return e.CorrelationKey(consts.SlotProcessID)
}

// lineageParentPID is the PID of the process that created it.
//
// An explicit parent field wins. Otherwise, on an event that names a NEW process, the plain
// ProcessId field is the creator — that is what a Windows 4688 means by it. On an event that
// does NOT name a new process, ProcessId is the subject itself and must not be read as a
// parent, or every process would be recorded as its own creator.
func lineageParentPID(e *graphene.Event) string {
	if pid := e.CorrelationKey(consts.SlotParentProcessID); pid != "" {
		return pid
	}
	if e.CorrelationKey(consts.SlotNewProcessID) != "" {
		return e.CorrelationKey(consts.SlotProcessID)
	}
	return ""
}

// itoa avoids pulling strconv into this file for one call site in a basis string.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
