package autograph

import (
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

var base = time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

// ev builds an event with a monotonic-ish timestamp derived from offset seconds.
func ev(id uint64, eventID, computer string, offsetSec int) *graphene.Event {
	return &graphene.Event{
		ID:        id,
		EventID:   eventID,
		Computer:  computer,
		Timestamp: base.Add(time.Duration(offsetSec) * time.Second),
	}
}

func spec(t *testing.T, body string) *rules.Spec {
	t.Helper()
	s, err := rules.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	return s
}

func TestSequenceBasicMatch(t *testing.T) {
	s := spec(t, `{"name":"logon chain","sequence":["4625","4624"]}`)
	events := []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		ev(2, "4624", "HOST-A", 1),
	}
	res := Generate(s, events)

	if res.Matches != 1 {
		t.Fatalf("matches = %d, want 1", res.Matches)
	}
	if len(res.Relations) != 1 {
		t.Fatalf("relations = %d, want 1", len(res.Relations))
	}
	r := res.Relations[0]
	if r.From != 1 || r.To != 2 {
		t.Errorf("edge = %d->%d, want 1->2", r.From, r.To)
	}
	if r.CreatedBy != consts.CreatedBySystem {
		t.Errorf("created_by = %q, want system", r.CreatedBy)
	}
	if r.ConfidenceScore != consts.RuleMatchConfidence {
		t.Errorf("confidence = %v, want %v", r.ConfidenceScore, consts.RuleMatchConfidence)
	}
	if r.RelationType != consts.RelationCorrelation {
		t.Errorf("relation type = %q, want correlation (default)", r.RelationType)
	}
	if r.GraphID != 0 || !r.CreatedAt.IsZero() {
		t.Errorf("graph id / created-at should be left for the caller, got %d / %v", r.GraphID, r.CreatedAt)
	}
}

func TestSequenceOptionalLabels(t *testing.T) {
	s := spec(t, `{"name":"three step","sequence":["4688","5156","3"],"labels":["","spawns"]}`)
	events := []*graphene.Event{
		ev(1, "4688", "HOST-A", 0),
		ev(2, "5156", "HOST-A", 1),
		ev(3, "3", "HOST-A", 2),
	}
	res := Generate(s, events)
	if len(res.Relations) != 2 {
		t.Fatalf("relations = %d, want 2", len(res.Relations))
	}
	if res.Relations[0].Label != "" {
		t.Errorf("edge 0 label = %q, want untagged", res.Relations[0].Label)
	}
	if res.Relations[1].Label != "spawns" {
		t.Errorf("edge 1 label = %q, want 'spawns'", res.Relations[1].Label)
	}
}

func TestSequenceScopeIsolation(t *testing.T) {
	// 4625 on HOST-A, 4624 on HOST-B — different computers must not correlate.
	s := spec(t, `{"name":"logon","sequence":["4625","4624"]}`)
	events := []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		ev(2, "4624", "HOST-B", 1),
	}
	res := Generate(s, events)
	if res.Matches != 0 || len(res.Relations) != 0 {
		t.Fatalf("cross-scope correlated: matches=%d relations=%d", res.Matches, len(res.Relations))
	}
}

func TestSequenceNonOverlapping(t *testing.T) {
	// Two full occurrences back to back → two matches, non-overlapping.
	s := spec(t, `{"name":"pair","sequence":["A","B"]}`)
	events := []*graphene.Event{
		ev(1, "A", "H", 0),
		ev(2, "B", "H", 1),
		ev(3, "A", "H", 2),
		ev(4, "B", "H", 3),
	}
	res := Generate(s, events)
	if res.Matches != 2 {
		t.Fatalf("matches = %d, want 2", res.Matches)
	}
	if res.Relations[0].From != 1 || res.Relations[0].To != 2 {
		t.Errorf("first edge = %d->%d, want 1->2", res.Relations[0].From, res.Relations[0].To)
	}
	if res.Relations[1].From != 3 || res.Relations[1].To != 4 {
		t.Errorf("second edge = %d->%d, want 3->4", res.Relations[1].From, res.Relations[1].To)
	}
}

func TestSequenceGreedyWithNoise(t *testing.T) {
	// Noise events between the sequence steps must be skipped, not break the match.
	s := spec(t, `{"name":"chain","sequence":["A","B","C"]}`)
	events := []*graphene.Event{
		ev(1, "A", "H", 0),
		ev(2, "X", "H", 1),
		ev(3, "B", "H", 2),
		ev(4, "Y", "H", 3),
		ev(5, "C", "H", 4),
	}
	res := Generate(s, events)
	if res.Matches != 1 || len(res.Relations) != 2 {
		t.Fatalf("matches=%d relations=%d, want 1/2", res.Matches, len(res.Relations))
	}
	if res.Relations[0].From != 1 || res.Relations[0].To != 3 || res.Relations[1].To != 5 {
		t.Errorf("edges skipped noise incorrectly: %+v", res.Relations)
	}
}

func TestSequenceIncompleteNoMatch(t *testing.T) {
	s := spec(t, `{"name":"chain","sequence":["A","B","C"]}`)
	events := []*graphene.Event{
		ev(1, "A", "H", 0),
		ev(2, "B", "H", 1), // no C
	}
	res := Generate(s, events)
	if res.Matches != 0 || len(res.Relations) != 0 {
		t.Fatalf("partial sequence matched: %+v", res)
	}
}

func TestSequenceChronologicalRegardlessOfInputOrder(t *testing.T) {
	s := spec(t, `{"name":"pair","sequence":["A","B"]}`)
	// Supplied out of time order: B's node id is lower but it occurs later.
	events := []*graphene.Event{
		ev(2, "B", "H", 5),
		ev(1, "A", "H", 0),
	}
	res := Generate(s, events)
	if res.Matches != 1 {
		t.Fatalf("matches = %d, want 1", res.Matches)
	}
	if res.Relations[0].From != 1 || res.Relations[0].To != 2 {
		t.Errorf("edge = %d->%d, want 1->2 (chronological)", res.Relations[0].From, res.Relations[0].To)
	}
}

func TestSequenceMatchCapTruncates(t *testing.T) {
	orig := maxMatches
	maxMatches = 2
	defer func() { maxMatches = orig }()

	s := spec(t, `{"name":"pair","sequence":["A","B"]}`)
	var events []*graphene.Event
	var id uint64
	for i := 0; i < 5; i++ { // 5 non-overlapping occurrences
		id++
		events = append(events, ev(id, "A", "H", i*2))
		id++
		events = append(events, ev(id, "B", "H", i*2+1))
	}
	res := Generate(s, events)
	if res.Matches != 2 {
		t.Errorf("matches = %d, want 2 (capped)", res.Matches)
	}
	if len(res.Relations) != 2 {
		t.Errorf("relations = %d, want 2 (capped)", len(res.Relations))
	}
	if !res.Truncated {
		t.Errorf("expected Truncated=true")
	}
	if res.Dropped != 3 {
		t.Errorf("dropped = %d, want 3", res.Dropped)
	}
}

// TestBuiltinRulesDriveTheEngine proves every shipped rule actually fires.
//
// The check is that a rule matches a stream BUILT TO SATISFY IT — which now depends on the
// algorithm, because satisfying a rule is no longer just "emit its event IDs in order". A
// field rule needs its events to share a correlation value; a temporal rule needs them close
// enough together; a lineage rule needs creation records with a resolvable parent and has no
// sequence at all. Feeding all four the old bare-sequence stream would have reported three
// shipped rules as broken when what was broken was the fixture.
func TestBuiltinRulesDriveTheEngine(t *testing.T) {
	builtins, errs := rules.Builtins()
	if len(errs) != 0 {
		t.Fatalf("builtins failed to load: %+v", errs)
	}
	for _, rule := range builtins {
		events := satisfyingStream(t, &rule.Spec)
		res := Generate(&rule.Spec, events)

		if res.Matches < 1 {
			t.Errorf("%s (%s): matches = %d, want at least 1 — a shipped rule must be able to fire",
				rule.ID, rule.AlgorithmOrDefault(), res.Matches)
			continue
		}
		if len(res.Relations) == 0 {
			t.Errorf("%s: matched but emitted no relations", rule.ID)
			continue
		}
		if rule.AlgorithmOrDefault() == consts.AlgoLineage {
			continue // no sequence, so no per-step labels to check
		}
		want := len(rule.Sequence) - 1
		if len(res.Relations) != want {
			t.Errorf("%s: relations = %d, want %d", rule.ID, len(res.Relations), want)
			continue
		}
		for k, rel := range res.Relations {
			if rel.Label != rule.LabelFor(k) {
				t.Errorf("%s: edge %d label = %q, want %q", rule.ID, k, rel.Label, rule.LabelFor(k))
			}
		}
	}
}

// satisfyingStream builds the minimal event stream that the given rule should match.
func satisfyingStream(t *testing.T, spec *rules.Spec) []*graphene.Event {
	t.Helper()

	if spec.AlgorithmOrDefault() == consts.AlgoLineage {
		// A parent creation and a child naming it, both on one host: the smallest stream with
		// a resolvable ancestry.
		createID := spec.LineageCreateIDsOrDefault()[0]
		parent := ev(1, createID, "HOST-A", 0)
		parent.ParsedFields = map[string]string{"ProcessId": "0x4", "NewProcessId": "0x100"}
		parent.ComputeCorrelationKeys()
		child := ev(2, createID, "HOST-A", 1)
		child.ParsedFields = map[string]string{"ProcessId": "0x100", "NewProcessId": "0x200"}
		child.ComputeCorrelationKeys()
		return []*graphene.Event{parent, child}
	}

	// Every matched event carries the same value for each required field, so a field rule can
	// group them; the values are arbitrary because the algorithm tests equality, not meaning.
	shared := map[string]string{}
	for _, name := range spec.MatchFields {
		switch name {
		case "logon_id":
			shared["TargetLogonId"] = "0x1a2b"
		case "target_user":
			shared["TargetUserName"] = "probe"
		case "subject_user":
			shared["SubjectUserName"] = "probe"
		case "process_name":
			shared["NewProcessName"] = `C:\probe.exe`
		case "ip_address":
			shared["IpAddress"] = "10.0.0.1"
		case "service_name":
			shared["ServiceName"] = "probe"
		default:
			t.Fatalf("no probe value for match field %q — add one so this rule stays covered", name)
		}
	}

	var events []*graphene.Event
	for i, eventID := range spec.Sequence {
		// One second apart, so any window a shipped rule declares is comfortably satisfied.
		e := ev(uint64(i+1), eventID, "HOST-A", i)
		if len(shared) > 0 {
			e.ParsedFields = shared
			e.ComputeCorrelationKeys()
		}
		events = append(events, e)
	}
	return events
}

func TestUndatedEventsNeverJoinASequence(t *testing.T) {
	// The trap: an undated event sorts at the zero time, i.e. BEFORE every real record. If
	// it were allowed into a time-ordered match it would happily open a chain it never
	// participated in — a fabricated correlation, which is the worst kind of bug here.
	s := spec(t, `{"name":"chain","sequence":["4625","4624"]}`)
	undated := ev(1, "4625", "HOST-A", 0)
	undated.Timestamp = time.Time{}

	res := Generate(s, []*graphene.Event{
		undated,
		ev(2, "4624", "HOST-A", 1),
	})
	if res.Matches != 0 || len(res.Relations) != 0 {
		t.Errorf("an undated event was matched into a sequence: %+v", res.Relations)
	}
	if res.SkippedUndated != 1 {
		t.Errorf("skipped-undated = %d, want 1 — the run must be able to report it", res.SkippedUndated)
	}
}

func TestUndatedEventsDoNotBreakSurroundingMatches(t *testing.T) {
	// Excluding them must not disturb the dated events around them.
	s := spec(t, `{"name":"chain","sequence":["4625","4624"]}`)
	undated := ev(9, "4625", "HOST-A", 0)
	undated.Timestamp = time.Time{}

	res := Generate(s, []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		undated,
		ev(2, "4624", "HOST-A", 1),
	})
	if res.Matches != 1 || len(res.Relations) != 1 {
		t.Fatalf("dated pair did not match: %+v", res)
	}
	if res.Relations[0].From != 1 || res.Relations[0].To != 2 {
		t.Errorf("edge = %d->%d, want 1->2", res.Relations[0].From, res.Relations[0].To)
	}
	if res.SkippedUndated != 1 {
		t.Errorf("skipped-undated = %d, want 1", res.SkippedUndated)
	}
}

func TestGenerateUnknownAlgorithmEmpty(t *testing.T) {
	// A spec with an unknown algorithm never reaches Generate in practice (validation
	// rejects it), but the defensive default is an empty result, not a panic.
	s := &rules.Spec{Algorithm: "nope", Sequence: []string{"A", "B"}}
	res := Generate(s, []*graphene.Event{ev(1, "A", "H", 0), ev(2, "B", "H", 1)})
	if len(res.Relations) != 0 {
		t.Errorf("unknown algorithm produced %d relations, want 0", len(res.Relations))
	}
}
