package autograph

import (
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// evk is ev plus a correlation projection, built from raw EventData so the tests exercise the
// same extractor ingestion uses rather than hand-writing slot values.
func evk(id uint64, eventID, computer string, offsetSec int, fields map[string]string) *graphene.Event {
	e := ev(id, eventID, computer, offsetSec)
	e.ParsedFields = fields
	e.ComputeCorrelationKeys()
	return e
}

func edgeSet(res Result) map[[2]uint64]bool {
	out := map[[2]uint64]bool{}
	for _, r := range res.Relations {
		out[[2]uint64{r.From, r.To}] = true
	}
	return out
}

// --- field correlation ---

func TestFieldCorrelationRequiresSharedValue(t *testing.T) {
	s := spec(t, `{"name":"same session","algorithm":"field",
		"sequence":["4625","4624"],"match_fields":["logon_id"]}`)

	// Two interleaved sessions. Sequence correlation would pair 1→2 (the first 4625 with the
	// first 4624 it sees) and call it a day; field correlation must pair within each session.
	events := []*graphene.Event{
		evk(1, "4625", "H", 0, map[string]string{"TargetLogonId": "0xAAA"}),
		evk(2, "4625", "H", 1, map[string]string{"TargetLogonId": "0xBBB"}),
		evk(3, "4624", "H", 2, map[string]string{"TargetLogonId": "0xBBB"}),
		evk(4, "4624", "H", 3, map[string]string{"TargetLogonId": "0xAAA"}),
	}
	res := Generate(s, events)

	if res.Matches != 2 {
		t.Fatalf("matches = %d, want 2 (one per session)", res.Matches)
	}
	got := edgeSet(res)
	if !got[[2]uint64{1, 4}] || !got[[2]uint64{2, 3}] {
		t.Errorf("edges = %v, want 1->4 (session AAA) and 2->3 (session BBB)", got)
	}
	// The pairing sequence correlation would have produced must NOT appear.
	if got[[2]uint64{1, 3}] {
		t.Error("1->3 crosses two logon sessions; field correlation must not emit it")
	}
}

// TestFieldCorrelationExcludesEventsWithNoValue is the single most important test in this
// file. Windows writes "-" and the null SID constantly; if an absent value were bucketed under
// the empty string, every event that happens not to carry the field would correlate with every
// other one. That failure looks exactly like a working rule.
func TestFieldCorrelationExcludesEventsWithNoValue(t *testing.T) {
	s := spec(t, `{"name":"same session","algorithm":"field",
		"sequence":["4625","4624"],"match_fields":["logon_id"]}`)

	events := []*graphene.Event{
		evk(1, "4625", "H", 0, map[string]string{"TargetLogonId": "-"}),
		evk(2, "4624", "H", 1, map[string]string{"TargetLogonId": "-"}),
		evk(3, "4625", "H", 2, map[string]string{"TargetLogonId": "S-1-0-0"}),
		evk(4, "4624", "H", 3, nil),
	}
	res := Generate(s, events)

	if res.Matches != 0 || len(res.Relations) != 0 {
		t.Fatalf("events with no logon id must not correlate: %d matches, %v",
			res.Matches, edgeSet(res))
	}
	if res.SkippedNoKeys != 4 {
		t.Errorf("skipped-no-keys = %d, want 4 — a run must be able to say that most of its "+
			"events could not be considered", res.SkippedNoKeys)
	}
}

func TestFieldCorrelationAcrossMultipleFields(t *testing.T) {
	s := spec(t, `{"name":"same session and user","algorithm":"field",
		"sequence":["4625","4624"],"match_fields":["logon_id","target_user"]}`)

	events := []*graphene.Event{
		evk(1, "4625", "H", 0, map[string]string{"TargetLogonId": "0xAAA", "TargetUserName": "alice"}),
		// Same session, different user: must not pair.
		evk(2, "4624", "H", 1, map[string]string{"TargetLogonId": "0xAAA", "TargetUserName": "bob"}),
		evk(3, "4624", "H", 2, map[string]string{"TargetLogonId": "0xAAA", "TargetUserName": "alice"}),
	}
	res := Generate(s, events)

	if res.Matches != 1 {
		t.Fatalf("matches = %d, want 1", res.Matches)
	}
	if !edgeSet(res)[[2]uint64{1, 3}] {
		t.Errorf("edges = %v, want 1->3", edgeSet(res))
	}
}

func TestFieldCorrelationRecordsItsBasis(t *testing.T) {
	s := spec(t, `{"name":"same session","algorithm":"field",
		"sequence":["4625","4624"],"match_fields":["logon_id"]}`)
	res := Generate(s, []*graphene.Event{
		evk(1, "4625", "H", 0, map[string]string{"TargetLogonId": "0x3E7"}),
		evk(2, "4624", "H", 1, map[string]string{"TargetLogonId": "0x3E7"}),
	})
	if len(res.Relations) != 1 {
		t.Fatalf("want one relation, got %d", len(res.Relations))
	}
	rel := res.Relations[0]
	if len(rel.Basis) != 1 || rel.Basis[0] != "logon_id=0x3e7" {
		t.Errorf("basis = %v, want [logon_id=0x3e7] — the edge has to be able to say why it exists",
			rel.Basis)
	}
	if rel.Algorithm != consts.AlgoField || rel.MatchID == "" || rel.RelVersion != consts.RelationSchemaVersion {
		t.Errorf("provenance not stamped: %+v", rel)
	}
}

// --- temporal correlation ---

// TestTemporalFindsTheMatchGreedyWouldMiss is the regression test the temporal algorithm
// exists for. A forward-greedy matcher anchors on the FIRST 4625, finds the 4624 outside the
// window, and gives up — walking straight past a valid pairing.
func TestTemporalFindsTheMatchGreedyWouldMiss(t *testing.T) {
	s := spec(t, `{"name":"burst then success","algorithm":"temporal",
		"sequence":["4625","4624"],"window_within":"1m"}`)

	events := []*graphene.Event{
		ev(1, "4625", "H", 0),   // 10:00:00 — greedy anchors here
		ev(2, "4625", "H", 600), // 10:10:00 — the anchor that actually works
		ev(3, "4624", "H", 630), // 10:10:30 — 30s after event 2, 10m30s after event 1
	}
	res := Generate(s, events)

	if res.Matches != 1 {
		t.Fatalf("matches = %d, want 1 — a greedy matcher anchored on event 1 and missed this",
			res.Matches)
	}
	if !edgeSet(res)[[2]uint64{2, 3}] {
		t.Errorf("edges = %v, want 2->3 (the pair actually inside the window)", edgeSet(res))
	}
}

func TestTemporalRejectsPairsOutsideTheWindow(t *testing.T) {
	s := spec(t, `{"name":"burst then success","algorithm":"temporal",
		"sequence":["4625","4624"],"window_within":"1m"}`)
	res := Generate(s, []*graphene.Event{
		ev(1, "4625", "H", 0),
		ev(2, "4624", "H", 3600), // an hour later
	})
	if res.Matches != 0 {
		t.Fatalf("matches = %d, want 0", res.Matches)
	}
}

func TestTemporalWindowBoundaryIsInclusive(t *testing.T) {
	s := spec(t, `{"name":"exact","algorithm":"temporal",
		"sequence":["4625","4624"],"window_within":"60s"}`)
	res := Generate(s, []*graphene.Event{
		ev(1, "4625", "H", 0),
		ev(2, "4624", "H", 60), // exactly the window
	})
	if res.Matches != 1 {
		t.Fatalf("a gap exactly equal to the window must match, got %d", res.Matches)
	}
}

func TestTemporalTotalWindowBoundsTheWholeChain(t *testing.T) {
	// Every individual gap is well inside window_within, but the chain spans more than
	// window_total — which is exactly the case window_total exists for.
	s := spec(t, `{"name":"long chain","algorithm":"temporal",
		"sequence":["4625","4625","4624"],"window_within":"5m","window_total":"6m"}`)

	res := Generate(s, []*graphene.Event{
		ev(1, "4625", "H", 0),
		ev(2, "4625", "H", 240), // +4m
		ev(3, "4624", "H", 480), // +4m, total 8m > 6m
	})
	if res.Matches != 0 {
		t.Fatalf("matches = %d, want 0 — the chain spans 8m against a 6m total", res.Matches)
	}
}

func TestTemporalIsNonOverlapping(t *testing.T) {
	s := spec(t, `{"name":"pairs","algorithm":"temporal",
		"sequence":["4625","4624"],"window_within":"5m"}`)
	res := Generate(s, []*graphene.Event{
		ev(1, "4625", "H", 0),
		ev(2, "4624", "H", 10),
		ev(3, "4625", "H", 20),
		ev(4, "4624", "H", 30),
	})
	if res.Matches != 2 {
		t.Fatalf("matches = %d, want 2", res.Matches)
	}
	got := edgeSet(res)
	if !got[[2]uint64{1, 2}] || !got[[2]uint64{3, 4}] {
		t.Errorf("edges = %v, want 1->2 and 3->4", got)
	}
}

func TestTemporalComposesWithFields(t *testing.T) {
	s := spec(t, `{"name":"session in a window","algorithm":"temporal",
		"sequence":["4625","4624"],"window_within":"5m","match_fields":["logon_id"]}`)

	events := []*graphene.Event{
		evk(1, "4625", "H", 0, map[string]string{"TargetLogonId": "0xAAA"}),
		// In the window, wrong session.
		evk(2, "4624", "H", 10, map[string]string{"TargetLogonId": "0xBBB"}),
		// Right session, in the window.
		evk(3, "4624", "H", 20, map[string]string{"TargetLogonId": "0xAAA"}),
	}
	res := Generate(s, events)
	if res.Matches != 1 || !edgeSet(res)[[2]uint64{1, 3}] {
		t.Fatalf("want exactly 1->3, got %d matches %v", res.Matches, edgeSet(res))
	}
	if len(res.Relations[0].Basis) < 2 {
		t.Errorf("basis = %v, want both the field and the gap", res.Relations[0].Basis)
	}
}

func TestTemporalScopeIsolation(t *testing.T) {
	s := spec(t, `{"name":"per host","algorithm":"temporal",
		"sequence":["4625","4624"],"window_within":"5m"}`)
	res := Generate(s, []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		ev(2, "4624", "HOST-B", 10),
	})
	if res.Matches != 0 {
		t.Fatalf("a chain must not be assembled across hosts, got %d matches", res.Matches)
	}
}

// --- lineage ---

// proc builds a 4688-shaped creation event: ProcessId is the creator, NewProcessId the child.
func proc(id uint64, computer string, offsetSec int, creatorPID, newPID, image string) *graphene.Event {
	return evk(id, "4688", computer, offsetSec, map[string]string{
		"ProcessId":      creatorPID,
		"NewProcessId":   newPID,
		"NewProcessName": image,
	})
}

func TestLineageLinksChildToCreator(t *testing.T) {
	s := spec(t, `{"name":"ancestry","algorithm":"lineage"}`)
	events := []*graphene.Event{
		proc(1, "H", 0, "0x4", "0x100", `C:\Windows\explorer.exe`),
		proc(2, "H", 10, "0x100", "0x200", `C:\Windows\System32\cmd.exe`),
	}
	res := Generate(s, events)

	if res.Matches != 1 {
		t.Fatalf("matches = %d, want 1", res.Matches)
	}
	if !edgeSet(res)[[2]uint64{1, 2}] {
		t.Errorf("edges = %v, want 1->2 (explorer spawned cmd)", edgeSet(res))
	}
	rel := res.Relations[0]
	if rel.Algorithm != consts.AlgoLineage || rel.ConfidenceScore != consts.ConfidenceExactMatch {
		t.Errorf("provenance/confidence wrong: %+v", rel)
	}
}

// TestLineageDoesNotLinkAcrossPIDReuse is the correctness test this algorithm was designed
// around. Windows recycles PIDs constantly; joining on the parent PID alone produces a
// confidently wrong graph, and every wrong edge looks exactly like a right one.
func TestLineageDoesNotLinkAcrossPIDReuse(t *testing.T) {
	s := spec(t, `{"name":"ancestry","algorithm":"lineage"}`)
	events := []*graphene.Event{
		// PID 0x100 is explorer.exe, created at t=0.
		proc(1, "H", 0, "0x4", "0x100", `C:\Windows\explorer.exe`),
		// It exits at t=100.
		evk(2, "4689", "H", 100, map[string]string{"ProcessId": "0x100"}),
		// PID 0x100 is REUSED at t=200, now by svchost.exe.
		proc(3, "H", 200, "0x4", "0x100", `C:\Windows\System32\svchost.exe`),
		// A child created at t=300 names parent 0x100 — that is the SECOND one.
		proc(4, "H", 300, "0x100", "0x300", `C:\Windows\System32\cmd.exe`),
	}
	res := Generate(s, events)

	got := edgeSet(res)
	if !got[[2]uint64{3, 4}] {
		t.Errorf("edges = %v, want 3->4 (the svchost that actually held PID 0x100 at t=300)", got)
	}
	if got[[2]uint64{1, 4}] {
		t.Error("linked to the explorer.exe that held PID 0x100 earlier and had already exited — " +
			"this is the PID-reuse error the lifetime table exists to prevent")
	}
}

// TestLineageRespectsExitBeforeReuse tightens the interval: a child created after the parent
// exited but before the PID is reused has no parent at all, and must not be attached to the
// process that previously held the number.
func TestLineageRespectsExitBeforeReuse(t *testing.T) {
	s := spec(t, `{"name":"ancestry","algorithm":"lineage"}`)
	events := []*graphene.Event{
		proc(1, "H", 0, "0x4", "0x100", `C:\Windows\explorer.exe`),
		evk(2, "4689", "H", 50, map[string]string{"ProcessId": "0x100"}),
		proc(3, "H", 100, "0x100", "0x300", `C:\Windows\System32\cmd.exe`),
	}
	res := Generate(s, events)

	if len(res.Relations) != 0 {
		t.Errorf("edges = %v, want none — PID 0x100 had exited by t=100", edgeSet(res))
	}
	// Two, not one. Event 3's parent had exited, and event 1's own parent (PID 0x4, System)
	// was created before the log begins — which is the ordinary case at the start of any case
	// and is exactly what this counter is for. Both are reported; neither is guessed.
	if res.UnresolvedParents != 2 {
		t.Errorf("unresolved-parents = %d, want 2 — an unresolvable parent must be counted, "+
			"never guessed", res.UnresolvedParents)
	}
}

func TestLineageCountsUnresolvedParentsRatherThanGuessing(t *testing.T) {
	s := spec(t, `{"name":"ancestry","algorithm":"lineage"}`)
	// The parent was created before the ingest window opened — overwhelmingly the common case
	// at the start of any case.
	res := Generate(s, []*graphene.Event{
		proc(1, "H", 0, "0x999", "0x100", `C:\Windows\System32\cmd.exe`),
	})
	if len(res.Relations) != 0 {
		t.Errorf("no edge may be emitted for an unresolvable parent, got %v", edgeSet(res))
	}
	if res.UnresolvedParents != 1 {
		t.Errorf("unresolved-parents = %d, want 1", res.UnresolvedParents)
	}
}

// TestLineageReadsSysmonShape checks the derived child/parent pairing against the other
// provider convention: ProcessId is the CHILD and ParentProcessId names the parent, the
// opposite of a Windows 4688.
func TestLineageReadsSysmonShape(t *testing.T) {
	s := spec(t, `{"name":"ancestry","algorithm":"lineage",
		"lineage_create_ids":["1"]}`)
	events := []*graphene.Event{
		evk(1, "1", "H", 0, map[string]string{"ProcessId": "0x100", "Image": `C:\Windows\explorer.exe`}),
		evk(2, "1", "H", 10, map[string]string{
			"ProcessId": "0x200", "ParentProcessId": "0x100", "Image": `C:\Windows\System32\cmd.exe`}),
	}
	res := Generate(s, events)
	if !edgeSet(res)[[2]uint64{1, 2}] {
		t.Errorf("edges = %v, want 1->2 under the Sysmon field convention", edgeSet(res))
	}
}

func TestLineageTransitiveEdgesAreLowerConfidence(t *testing.T) {
	s := spec(t, `{"name":"ancestry","algorithm":"lineage","lineage_depth":2}`)
	events := []*graphene.Event{
		proc(1, "H", 0, "0x4", "0x100", `C:\Windows\explorer.exe`),
		proc(2, "H", 10, "0x100", "0x200", `C:\Windows\System32\cmd.exe`),
		proc(3, "H", 20, "0x200", "0x300", `C:\Windows\System32\whoami.exe`),
	}
	res := Generate(s, events)

	got := edgeSet(res)
	if !got[[2]uint64{2, 3}] {
		t.Fatalf("missing the direct edge 2->3: %v", got)
	}
	if !got[[2]uint64{1, 3}] {
		t.Fatalf("missing the transitive edge 1->3 at depth 2: %v", got)
	}
	for _, r := range res.Relations {
		if r.From == 1 && r.To == 3 {
			if r.ConfidenceScore != consts.ConfidenceLineageTransitive {
				t.Errorf("transitive edge confidence = %v, want %v — a derived link must not "+
					"present identically to one read from a record",
					r.ConfidenceScore, consts.ConfidenceLineageTransitive)
			}
		}
	}
}

func TestLineageDefaultDepthEmitsOnlyDirectEdges(t *testing.T) {
	s := spec(t, `{"name":"ancestry","algorithm":"lineage"}`)
	res := Generate(s, []*graphene.Event{
		proc(1, "H", 0, "0x4", "0x100", `a.exe`),
		proc(2, "H", 10, "0x100", "0x200", `b.exe`),
		proc(3, "H", 20, "0x200", "0x300", `c.exe`),
	})
	if got := edgeSet(res); got[[2]uint64{1, 3}] {
		t.Errorf("depth 0 must emit direct edges only, got %v", got)
	}
}

func TestLineageScopeIsolation(t *testing.T) {
	s := spec(t, `{"name":"ancestry","algorithm":"lineage"}`)
	// The same PID on two hosts is two different processes.
	res := Generate(s, []*graphene.Event{
		proc(1, "HOST-A", 0, "0x4", "0x100", `explorer.exe`),
		proc(2, "HOST-B", 10, "0x100", "0x200", `cmd.exe`),
	})
	if len(res.Relations) != 0 {
		t.Errorf("PIDs are per host; got cross-host edges %v", edgeSet(res))
	}
}

// --- shared properties ---

func TestAllAlgorithmsReportStaleCorrelationKeys(t *testing.T) {
	// An event ingested before the projection existed carries no keys and no version.
	stale := ev(1, "4625", "H", 0)
	fresh := evk(2, "4624", "H", 1, map[string]string{"TargetLogonId": "0x3E7"})

	s := spec(t, `{"name":"session","algorithm":"field",
		"sequence":["4625","4624"],"match_fields":["logon_id"]}`)
	res := Generate(s, []*graphene.Event{stale, fresh})

	if res.StaleCorrelationKeys != 1 {
		t.Errorf("stale-correlation-keys = %d, want 1 — a run over an un-backfilled case must "+
			"say it is under-reporting rather than return the short answer as the whole one",
			res.StaleCorrelationKeys)
	}
}

func TestPreparePartitionsAreChronologicalWithoutResorting(t *testing.T) {
	// Shuffled input; every partition must still come out in time order, because the whole
	// dataset design rests on partitions inheriting the single global sort.
	events := []*graphene.Event{
		ev(3, "A", "H2", 30),
		ev(1, "A", "H1", 10),
		ev(4, "A", "H2", 5),
		ev(2, "A", "H1", 20),
	}
	ds := Prepare(events, Requirements{Scopes: []string{consts.ScopeComputer}})
	for _, g := range ds.Groups(consts.ScopeComputer) {
		for i := 1; i < len(g.Events); i++ {
			if g.Events[i].Timestamp.Before(g.Events[i-1].Timestamp) {
				t.Fatalf("partition %q is not chronological: %v after %v",
					g.Value, g.Events[i].Timestamp, g.Events[i-1].Timestamp)
			}
		}
	}
}

func TestGlobalScopeIsOnePartition(t *testing.T) {
	events := []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		ev(2, "4624", "HOST-B", 10),
	}
	ds := Prepare(events, Requirements{Scopes: []string{consts.ScopeGlobal}})
	groups := ds.Groups(consts.ScopeGlobal)
	if len(groups) != 1 || len(groups[0].Events) != 2 {
		t.Fatalf("global scope must be one partition holding everything, got %d groups", len(groups))
	}

	s := spec(t, `{"name":"cross host","sequence":["4625","4624"],
		"match_scope":"global"}`)
	res := GenerateWith(s, ds)
	if res.Matches != 1 {
		t.Fatalf("global scope should correlate across hosts, got %d matches", res.Matches)
	}
}

func TestPrepareSortsOnceForManyRules(t *testing.T) {
	// Not a timing assertion — a structural one. Two rules over one dataset must see the
	// identical partition slices, which is only true if preparation happened once.
	events := []*graphene.Event{ev(1, "4625", "H", 0), ev(2, "4624", "H", 1)}
	ds := Prepare(events, Requirements{Scopes: []string{consts.ScopeComputer}})

	a := ds.Groups(consts.ScopeComputer)
	b := ds.Groups(consts.ScopeComputer)
	if len(a) != len(b) || &a[0].Events[0] != &b[0].Events[0] {
		t.Error("Groups returned freshly built partitions; the dataset must prepare once and share")
	}
}

func TestMatchIDIsStableAcrossRuns(t *testing.T) {
	s := spec(t, `{"name":"chain","sequence":["4625","4624"]}`)
	events := []*graphene.Event{ev(1, "4625", "H", 0), ev(2, "4624", "H", 1)}

	first := Generate(s, events)
	second := Generate(s, events)
	if first.Relations[0].MatchID != second.Relations[0].MatchID {
		t.Errorf("match id changed between runs (%q vs %q) — a rebuild must reproduce a graph "+
			"exactly, or two builds of one case cannot be compared",
			first.Relations[0].MatchID, second.Relations[0].MatchID)
	}
	if first.Relations[0].MatchID == "" {
		t.Error("no match id stamped")
	}
}

func TestMatchIDGroupsOneOccurrence(t *testing.T) {
	s := spec(t, `{"name":"chain","sequence":["4625","4625","4624"]}`)
	res := Generate(s, []*graphene.Event{
		ev(1, "4625", "H", 0), ev(2, "4625", "H", 1), ev(3, "4624", "H", 2),
		ev(4, "4625", "H", 3), ev(5, "4625", "H", 4), ev(6, "4624", "H", 5),
	})
	if res.Matches != 2 || len(res.Relations) != 4 {
		t.Fatalf("expected 2 matches / 4 relations, got %d / %d", res.Matches, len(res.Relations))
	}
	byMatch := map[string]int{}
	for _, r := range res.Relations {
		byMatch[r.MatchID]++
	}
	if len(byMatch) != 2 {
		t.Fatalf("expected 2 distinct match ids, got %d: %v", len(byMatch), byMatch)
	}
	for id, n := range byMatch {
		if n != 2 {
			t.Errorf("match %s has %d edges, want 2", id, n)
		}
	}
	// And step indices must address the sequence, so the editor can highlight the right step.
	steps := map[int]bool{}
	for _, r := range res.Relations {
		steps[r.StepIndex] = true
	}
	if !steps[0] || !steps[1] {
		t.Errorf("step indices = %v, want both 0 and 1", steps)
	}
}

func TestUnknownAlgorithmYieldsNothingRatherThanPanicking(t *testing.T) {
	// Validation refuses this at load, so reaching Generate means a hand-built spec.
	s := spec(t, `{"name":"chain","sequence":["4625","4624"]}`)
	s.Algorithm = "not-registered"
	res := Generate(s, []*graphene.Event{ev(1, "4625", "H", 0), ev(2, "4624", "H", 1)})
	if len(res.Relations) != 0 {
		t.Errorf("want no relations, got %d", len(res.Relations))
	}
}

func TestPrepareDropsUndatedForEveryAlgorithm(t *testing.T) {
	undated := &graphene.Event{ID: 9, EventID: "4625", Computer: "H"} // zero timestamp
	events := []*graphene.Event{undated, ev(1, "4625", "H", 0), ev(2, "4624", "H", 1)}

	ds := Prepare(events, Requirements{Scopes: []string{consts.ScopeComputer}})
	if ds.SkippedUndated != 1 {
		t.Fatalf("skipped-undated = %d, want 1", ds.SkippedUndated)
	}
	for _, e := range ds.Events {
		if e.Timestamp.IsZero() {
			t.Fatal("an undated event reached the prepared dataset")
		}
	}
	_ = time.Now // keep the time import meaningful if the assertions above change
}
