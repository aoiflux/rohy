package graphene

import (
	"strings"
	"testing"

	"rohy/backend/consts"
)

// slotOf is a readability helper: tests name the slot rather than its index, so a
// renumbering breaks the vocabulary golden below rather than silently rewriting every
// assertion in this file.
func slotOf(t *testing.T, name string) int {
	t.Helper()
	i, ok := consts.CorrelationSlotIndex(name)
	if !ok {
		t.Fatalf("unknown correlation slot %q", name)
	}
	return i
}

// TestCorrelationSlotVocabularyGolden pins the wire format.
//
// Slot values are stored BY POSITION, so reordering or reusing a slot reinterprets every
// event already on disk: a value written as a logon id would be read back as a process id and
// nothing would report an error. This test is the tripwire — changing the vocabulary must be
// a deliberate act that also bumps CorrelationKeyVersion, not a tidy-up.
func TestCorrelationSlotVocabularyGolden(t *testing.T) {
	want := []string{
		"logon_id",
		"subject_logon_id",
		"process_id",
		"new_process_id",
		"parent_process_id",
		"process_name",
		"target_user",
		"subject_user",
		"ip_address",
		"service_name",
	}
	if len(consts.CorrelationSlots) != len(want) {
		t.Fatalf("slot count changed: got %d, want %d — bump consts.CorrelationKeyVersion and update this golden",
			len(consts.CorrelationSlots), len(want))
	}
	for i := range want {
		if consts.CorrelationSlots[i] != want[i] {
			t.Fatalf("slot %d changed: got %q, want %q — this reinterprets every stored event; bump consts.CorrelationKeyVersion",
				i, consts.CorrelationSlots[i], want[i])
		}
	}
	if len(consts.CorrelationSlots) != consts.CorrelationSlotCount {
		t.Fatalf("CorrelationSlotCount (%d) disagrees with the slot list (%d)",
			consts.CorrelationSlotCount, len(consts.CorrelationSlots))
	}
	// Every slot must have sources, a class and a length bound, or it can never be populated
	// or costed.
	for i, name := range consts.CorrelationSlots {
		if len(consts.CorrelationSlotSources[i]) == 0 {
			t.Errorf("slot %d (%s) has no source field names", i, name)
		}
		if consts.CorrelationSlotClasses[i] == "" {
			t.Errorf("slot %d (%s) has no canonicalization class", i, name)
		}
		n, ok := consts.CorrelationSlotMaxLen[i]
		if !ok || n <= 0 {
			t.Errorf("slot %d (%s) has no length bound, so it is not costed", i, name)
		}
		if n > consts.CorrelationValueMaxLen {
			t.Errorf("slot %d (%s) is bounded at %d, above the %d ceiling",
				i, name, n, consts.CorrelationValueMaxLen)
		}
	}
}

func TestExtractCorrelationKeys4688(t *testing.T) {
	// A representative 4688: the creator is ProcessId, the new process is NewProcessId.
	fields := map[string]string{
		"SubjectUserSid":     "S-1-5-21-1-2-3-1001",
		"SubjectUserName":    "Alice",
		"SubjectDomainName":  "CORP",
		"SubjectLogonId":     "0x3E7",
		"NewProcessId":       "0x1A4",
		"NewProcessName":     `C:\Windows\System32\cmd.exe`,
		"TokenElevationType": "%%1936",
		"ProcessId":          "0x2C8",
		"CommandLine":        `cmd.exe /c whoami`,
		"ParentProcessName":  `C:\Windows\explorer.exe`,
	}
	got := ExtractCorrelationKeys(fields)

	check := func(slot, want string) {
		t.Helper()
		i := slotOf(t, slot)
		if i >= len(got) || got[i] != want {
			var have string
			if i < len(got) {
				have = got[i]
			}
			t.Errorf("slot %s: got %q, want %q", slot, have, want)
		}
	}
	check("subject_logon_id", "0x3e7") // hex lowercased
	check("new_process_id", "0x1a4")   // the child
	check("process_id", "0x2c8")       // the creator, verbatim field
	check("process_name", "cmd.exe")   // basename only
	check("subject_user", "alice")

	// Fields the vocabulary does not cover contribute nothing.
	if i := slotOf(t, "logon_id"); i < len(got) && got[i] != "" {
		t.Errorf("logon_id should be empty on a 4688 that carries only SubjectLogonId, got %q", got[i])
	}
}

func TestExtractCorrelationKeysIdentifierCanonicalization(t *testing.T) {
	// Decimal and hex spellings of the same identifier must land on the same stored value, or
	// a process id correlates with itself across providers only by luck.
	dec := ExtractCorrelationKeys(map[string]string{"ProcessId": "420"})
	hex := ExtractCorrelationKeys(map[string]string{"ProcessId": "0x1A4"})
	i := slotOf(t, "process_id")
	if dec[i] != hex[i] {
		t.Fatalf("decimal %q and hex %q did not canonicalize to the same value", dec[i], hex[i])
	}
	if dec[i] != "0x1a4" {
		t.Fatalf("got %q, want 0x1a4", dec[i])
	}
}

func TestExtractCorrelationKeysAbsentValues(t *testing.T) {
	// "-" and the null SID mean "not applicable". Storing them would bucket every event that
	// carries one together, which is how a field matcher becomes a false-positive engine.
	for _, absent := range []string{"-", "S-1-0-0", "null", "N/A", "(null)", "  ", ""} {
		got := ExtractCorrelationKeys(map[string]string{"TargetUserName": absent})
		if len(got) != 0 {
			t.Errorf("%q should project to nothing, got %v", absent, got)
		}
	}
}

func TestExtractCorrelationKeysZeroIsAbsentOnlyForIdentifiers(t *testing.T) {
	// A logon id of zero means "no logon session".
	if got := ExtractCorrelationKeys(map[string]string{"TargetLogonId": "0x0"}); len(got) != 0 {
		t.Errorf("logon id 0x0 means none; got %v", got)
	}
	if got := ExtractCorrelationKeys(map[string]string{"ProcessId": "0"}); len(got) != 0 {
		t.Errorf("process id 0 means none; got %v", got)
	}
	// Zero is rejected for IDENTIFIERS only. A plain-class slot keeps a numeric zero, because
	// zero is a real value outside the identifier domain and a universal rule would drop it.
	got := ExtractCorrelationKeys(map[string]string{"ServiceName": "0"})
	i := slotOf(t, "service_name")
	if i >= len(got) || got[i] != "0" {
		t.Fatalf("zero is only absent for identifiers, got %v", got)
	}
}

func TestExtractCorrelationKeysImageBasename(t *testing.T) {
	cases := map[string]string{
		`C:\Windows\System32\cmd.exe`:                      "cmd.exe",
		`\Device\HarddiskVolume2\Windows\System32\cmd.exe`: "cmd.exe",
		`/usr/bin/sudo`: "sudo",
		`cmd.exe`:       "cmd.exe",
	}
	i := slotOf(t, "process_name")
	for in, want := range cases {
		got := ExtractCorrelationKeys(map[string]string{"NewProcessName": in})
		if i >= len(got) || got[i] != want {
			t.Errorf("%q: got %v, want %q", in, got, want)
		}
	}
}

func TestExtractCorrelationKeysCaseInsensitiveFieldNames(t *testing.T) {
	// The binary-XML and rendered-XML representations of one record do not agree on case, and
	// providers vary further. Lookup must not care.
	lower := ExtractCorrelationKeys(map[string]string{"targetlogonid": "0x3e7"})
	upper := ExtractCorrelationKeys(map[string]string{"TARGETLOGONID": "0x3E7"})
	i := slotOf(t, "logon_id")
	if lower[i] != "0x3e7" || upper[i] != "0x3e7" {
		t.Fatalf("case-insensitive lookup failed: %v / %v", lower, upper)
	}
}

func TestExtractCorrelationKeysSourcePrecedence(t *testing.T) {
	// TargetLogonId is listed before LogonId, so it wins when both are present.
	got := ExtractCorrelationKeys(map[string]string{"LogonId": "0x111", "TargetLogonId": "0x222"})
	if i := slotOf(t, "logon_id"); got[i] != "0x222" {
		t.Fatalf("expected the first-listed source to win, got %q", got[i])
	}
	// A present-but-meaningless value falls through to the next spelling rather than blocking it.
	got = ExtractCorrelationKeys(map[string]string{"TargetLogonId": "-", "LogonId": "0x333"})
	if i := slotOf(t, "logon_id"); got[i] != "0x333" {
		t.Fatalf("expected fallthrough past an absent value, got %q", got[i])
	}
}

func TestExtractCorrelationKeysTrailingSlotsTrimmed(t *testing.T) {
	// An event carrying only a logon id must cost one entry, not twelve.
	got := ExtractCorrelationKeys(map[string]string{"TargetLogonId": "0x3e7"})
	if len(got) != 1 {
		t.Fatalf("expected trailing empties trimmed, got %d entries: %v", len(got), got)
	}
	if ExtractCorrelationKeys(nil) != nil {
		t.Fatal("no fields must project to nil, not an empty slice")
	}
	if ExtractCorrelationKeys(map[string]string{"Irrelevant": "x"}) != nil {
		t.Fatal("no matching fields must project to nil")
	}
}

func TestExtractCorrelationKeysTruncation(t *testing.T) {
	i := slotOf(t, "target_user")
	limit := consts.CorrelationSlotMaxLen[i]

	long := strings.Repeat("a", limit*2)
	got := ExtractCorrelationKeys(map[string]string{"TargetUserName": long})
	if len(got[i]) > limit {
		t.Fatalf("value not truncated to the slot's own bound: %d bytes, limit %d", len(got[i]), limit)
	}
	// Truncation must not split a rune, or the basis it feeds renders as replacement chars.
	multi := strings.Repeat("é", limit)
	got = ExtractCorrelationKeys(map[string]string{"TargetUserName": multi})
	if v := got[i]; len(v) > limit || !isValidUTF8(v) {
		t.Fatalf("truncation split a rune or overran: %q (%d bytes)", v, len(v))
	}
}

// TestSlotsAreBoundedToTheirDomain checks that each slot's limit is wide enough for the
// widest legitimate value of that kind. A bound that is too tight truncates real evidence
// into a wrong correlation key, which is a worse failure than the memory it saves.
func TestSlotsAreBoundedToTheirDomain(t *testing.T) {
	widest := map[string]string{
		// The maximum uint64 identifier, which is as long as a Windows id can be.
		"logon_id":          "0xffffffffffffffff",
		"subject_logon_id":  "0xffffffffffffffff",
		"process_id":        "0xffffffffffffffff",
		"new_process_id":    "0xffffffffffffffff",
		"parent_process_id": "0xffffffffffffffff",
		// The longest IPv6 literal form.
		"ip_address": "0000:0000:0000:0000:0000:ffff:192.168.100.228",
	}
	sources := map[string]string{
		"logon_id":          "TargetLogonId",
		"subject_logon_id":  "SubjectLogonId",
		"process_id":        "ProcessId",
		"new_process_id":    "NewProcessId",
		"parent_process_id": "ParentProcessId",
		"ip_address":        "IpAddress",
	}
	for slot, value := range widest {
		i := slotOf(t, slot)
		got := ExtractCorrelationKeys(map[string]string{sources[slot]: value})
		if i >= len(got) || got[i] == "" {
			t.Fatalf("%s: widest legitimate value projected to nothing", slot)
		}
		if len(got[i]) < len(strings.ToLower(value)) {
			t.Errorf("%s: bound of %d truncated a legitimate value (%q -> %q)",
				slot, consts.CorrelationSlotMaxLen[i], value, got[i])
		}
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

func TestComputeCorrelationKeysStampsVersion(t *testing.T) {
	e := &Event{ParsedFields: map[string]string{"TargetLogonId": "0x3e7"}}
	e.ComputeCorrelationKeys()
	if e.CorrKeyVersion != consts.CorrelationKeyVersion {
		t.Fatalf("version not stamped: %d", e.CorrKeyVersion)
	}
	if !e.HasCurrentCorrelationKeys() {
		t.Fatal("freshly projected event reports stale keys")
	}
	if e.CorrelationKey(slotOf(t, "logon_id")) != "0x3e7" {
		t.Fatalf("accessor disagrees with the slice: %v", e.CorrKeys)
	}

	// An event with nothing to project is still FINISHED, not un-projected. Leaving the
	// version at zero would make it look permanently stale to the backfill.
	empty := &Event{}
	empty.ComputeCorrelationKeys()
	if empty.CorrKeys != nil {
		t.Fatalf("expected nil projection, got %v", empty.CorrKeys)
	}
	if !empty.HasCurrentCorrelationKeys() {
		t.Fatal("an event with no fields must still be stamped as projected")
	}

	// Out-of-range slots read as empty, so an event written by a shorter vocabulary does not
	// panic when a longer one asks it a question.
	if empty.CorrelationKey(consts.CorrelationSlotCount+5) != "" {
		t.Fatal("out-of-range slot must read as empty")
	}
	if empty.CorrelationKey(-1) != "" {
		t.Fatal("negative slot must read as empty")
	}
}

func TestCorrelationKeysSurviveRoundTrip(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	e := &Event{
		EventID:        "4688",
		Computer:       "HOST-1",
		HashNormalized: "h-ck-1",
		ParsedFields:   map[string]string{"SubjectLogonId": "0x3E7", "NewProcessId": "0x1a4"},
	}
	e.ComputeCorrelationKeys()
	if _, err := s.InsertEvents([]*Event{e}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := s.GetEvent(e.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CorrelationKey(slotOf(t, "subject_logon_id")) != "0x3e7" {
		t.Fatalf("projection lost in the node record: %v", got.CorrKeys)
	}
	if got.CorrKeyVersion != consts.CorrelationKeyVersion {
		t.Fatalf("version lost in the node record: %d", got.CorrKeyVersion)
	}
}

func TestStampProvenanceBounds(t *testing.T) {
	r := &Relation{}
	long := strings.Repeat("b", consts.MaxBasisLen*2)
	r.StampProvenance("rule-x", consts.AlgoField, "match-1", 2,
		[]string{"a", "", "  ", long, "c", "d", "e", "f"})

	if r.RelVersion != consts.RelationSchemaVersion {
		t.Fatalf("provenance version not stamped: %d", r.RelVersion)
	}
	if len(r.Basis) != consts.MaxBasisEntries {
		t.Fatalf("basis not capped at %d entries: %v", consts.MaxBasisEntries, r.Basis)
	}
	for _, b := range r.Basis {
		if len(b) > consts.MaxBasisLen {
			t.Fatalf("basis entry not truncated: %d bytes", len(b))
		}
		if strings.TrimSpace(b) == "" {
			t.Fatal("blank basis entries must be dropped, not stored")
		}
	}
	if r.RuleID != "rule-x" || r.Algorithm != consts.AlgoField || r.MatchID != "match-1" || r.StepIndex != 2 {
		t.Fatalf("provenance fields not recorded: %+v", r)
	}
}

func TestProvenanceSurvivesRoundTripAndIsIndexed(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	a := &Event{EventID: "4625", Computer: "H", HashNormalized: "pa"}
	b := &Event{EventID: "4624", Computer: "H", HashNormalized: "pb"}
	if _, err := s.InsertEvents([]*Event{a, b}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	rel := &Relation{From: a.ID, To: b.ID, GraphID: 1, RelationType: consts.RelationCorrelation,
		CreatedBy: consts.CreatedBySystem}
	rel.StampProvenance("brute-force-then-success", consts.AlgoTemporal, "m-1", 0, []string{"Δt=42s ≤ 5m"})
	if _, err := s.InsertRelation(rel); err != nil {
		t.Fatalf("insert relation: %v", err)
	}

	got, err := s.GetRelation(rel.ID)
	if err != nil {
		t.Fatalf("get relation: %v", err)
	}
	if got.RuleID != "brute-force-then-success" || got.Algorithm != consts.AlgoTemporal ||
		got.MatchID != "m-1" || len(got.Basis) != 1 || got.RelVersion != consts.RelationSchemaVersion {
		t.Fatalf("provenance lost in the edge record: %+v", got)
	}

	// rule_id is indexed, which is what finds a rule's edges without knowing their graph.
	byRule, err := s.RelationsByRule("brute-force-then-success")
	if err != nil {
		t.Fatalf("by rule: %v", err)
	}
	if len(byRule) != 1 || byRule[0].ID != rel.ID {
		t.Fatalf("rule_id index did not resolve the relation: %v", byRule)
	}
}

// TestLegacyRelationDecodesAsUnrecorded pins the compatibility statement the inspector makes.
// A relation written before provenance existed must be distinguishable from one whose rule
// recorded no basis — the first is "we did not track this yet", the second is "there was
// nothing to say", and showing them identically would be a quiet lie.
func TestLegacyRelationDecodesAsUnrecorded(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	a := &Event{EventID: "1", Computer: "H", HashNormalized: "la"}
	b := &Event{EventID: "2", Computer: "H", HashNormalized: "lb"}
	if _, err := s.InsertEvents([]*Event{a, b}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// No StampProvenance call: this is the shape v0.1.0 wrote.
	legacy := &Relation{From: a.ID, To: b.ID, GraphID: 1, CreatedBy: consts.CreatedByUser}
	if _, err := s.InsertRelation(legacy); err != nil {
		t.Fatalf("insert relation: %v", err)
	}
	got, err := s.GetRelation(legacy.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RelVersion != 0 {
		t.Fatalf("an unstamped relation must report version 0, got %d", got.RelVersion)
	}
}
