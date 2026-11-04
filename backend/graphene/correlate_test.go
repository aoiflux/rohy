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
		// Network slots, appended for multi-source ingestion at CorrelationKeyVersion 3. Note
		// there is no source-address slot: ip_address already IS one, since every alias it reads
		// names the party that initiated the interaction.
		"dst_ip",
		"dst_port",
		"protocol",
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

	// logon_id falls back to SubjectLogonId, and that is the whole point of the slot.
	//
	// A 4624 records the session it created as TargetLogonId; a 4688 records the session that
	// created the process as SubjectLogonId. If logon_id only read TargetLogonId it would be
	// populated on the logon and empty here — so a rule correlating "this logon, then what it
	// ran" would exclude every 4688 for having no value and match nothing, silently.
	check("logon_id", "0x3e7")
	check("subject_logon_id", "0x3e7")
}

// TestLogonIDUnifiesTheSessionAcrossEventShapes is the assertion that keeps the most valuable
// field correlation working: a logon and the things done under it must produce the SAME
// logon_id even though Windows names the session differently on each.
func TestLogonIDUnifiesTheSessionAcrossEventShapes(t *testing.T) {
	logon := ExtractCorrelationKeys(map[string]string{ // 4624: the session it created
		"TargetLogonId": "0x1a2b3c", "SubjectLogonId": "0x3e7", "TargetUserName": "alice",
	})
	spawned := ExtractCorrelationKeys(map[string]string{ // 4688: the session that ran it
		"SubjectLogonId": "0x1a2b3c", "NewProcessId": "0x1a4",
	})

	i := slotOf(t, "logon_id")
	if logon[i] != spawned[i] {
		t.Fatalf("a logon (%q) and a process it spawned (%q) must share one logon_id, or "+
			"correlating on the session cannot work", logon[i], spawned[i])
	}
	if logon[i] != "0x1a2b3c" {
		t.Fatalf("logon_id = %q, want the session the logon created", logon[i])
	}
	// subject_logon_id still distinguishes actor from target where the event does.
	j := slotOf(t, "subject_logon_id")
	if logon[j] != "0x3e7" {
		t.Errorf("subject_logon_id = %q, want the session that requested the logon", logon[j])
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
		// The longest CANONICAL IPv6 form. Not the longest literal: the address class
		// canonicalizes to RFC 5952, and Addr.String() never emits more than this — which is
		// exactly why the bound is 39 rather than the 45 an uncanonicalized literal needs.
		"ip_address": "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		"dst_ip":     "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		"dst_port":   "65535",
		"protocol":   "ipv6-icmp",
	}
	sources := map[string]string{
		"logon_id":          "TargetLogonId",
		"subject_logon_id":  "SubjectLogonId",
		"process_id":        "ProcessId",
		"new_process_id":    "NewProcessId",
		"parent_process_id": "ParentProcessId",
		"ip_address":        "IpAddress",
		"dst_ip":            "DestinationIp",
		"dst_port":          "DestinationPort",
		"protocol":          "Protocol",
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

// --- address, port and protocol canonicalization (CorrelationKeyVersion 3) ---

// 🔒 The join this exists for. Windows writes some addresses v4-mapped; Suricata, nginx and
// iptables write the plain form. Under the old plain class those were two different correlation
// keys and any cross-source rule matching on ip_address silently found nothing.
func TestAddressSpellingsUnifyIntoOneKey(t *testing.T) {
	same := []string{
		"10.0.0.5",
		"::ffff:10.0.0.5",
		"::FFFF:10.0.0.5",
		"  10.0.0.5  ",
		"10.0.0.5:445", // some producers write the endpoint; the port has its own slot
	}
	i := slotOf(t, "ip_address")
	want := ""
	for n, v := range same {
		got := ExtractCorrelationKeys(map[string]string{"IpAddress": v})
		if i >= len(got) || got[i] == "" {
			t.Fatalf("%q projected to nothing", v)
		}
		if n == 0 {
			want = got[i]
			continue
		}
		if got[i] != want {
			t.Errorf("%q projected %q, want %q — these are the same host and must be one key", v, got[i], want)
		}
	}

	v6 := []string{"2001:0db8:0000:0000:0000:0000:0000:0001", "2001:DB8::1", "2001:db8::1"}
	want = ""
	for n, v := range v6 {
		got := ExtractCorrelationKeys(map[string]string{"IpAddress": v})
		if n == 0 {
			want = got[i]
			continue
		}
		if got[i] != want {
			t.Errorf("%q projected %q, want %q", v, got[i], want)
		}
	}
}

// 🔒 The false-positive bug this fixes. 0.0.0.0 on a 4624 means "not a network logon" — it is a
// SENTINEL, not an address. It is not in consts.CorrelationAbsentValues, so before the address
// class every local logon in a case shared one ip_address key and a field rule matching on it
// correlated all of them with each other.
func TestUnspecifiedAddressIsAbsentNotAKey(t *testing.T) {
	i := slotOf(t, "ip_address")
	for _, v := range []string{"0.0.0.0", "::", "0:0:0:0:0:0:0:0", "::ffff:0.0.0.0"} {
		got := ExtractCorrelationKeys(map[string]string{"IpAddress": v})
		if i < len(got) && got[i] != "" {
			t.Errorf("%q projected %q — the unspecified address is a sentinel, and treating it "+
				"as a value buckets every local logon in the case together", v, got[i])
		}
	}
}

// A zone is host-local, so it cannot mean anything across two hosts' records.
func TestAddressZoneIsStripped(t *testing.T) {
	i := slotOf(t, "ip_address")
	withZone := ExtractCorrelationKeys(map[string]string{"IpAddress": "fe80::1%eth0"})
	plain := ExtractCorrelationKeys(map[string]string{"IpAddress": "fe80::1"})
	if i >= len(withZone) || withZone[i] == "" {
		t.Fatal("a zoned address projected to nothing")
	}
	if withZone[i] != plain[i] {
		t.Errorf("zoned %q != plain %q", withZone[i], plain[i])
	}
}

// 🔒 A non-address is dropped, NOT truncated. This diverges deliberately from the identifier
// class, which keeps unparseable values verbatim: a 39-byte slot cannot hold a hostname, and a
// truncated one manufactures a correlation key that is wrong rather than merely short.
func TestNonAddressProjectsNothingRatherThanTruncating(t *testing.T) {
	i := slotOf(t, "ip_address")
	for _, v := range []string{
		"workstation-01.corp.example.com",
		"not-an-address",
		"999.999.999.999",
	} {
		got := ExtractCorrelationKeys(map[string]string{"IpAddress": v})
		if i < len(got) && got[i] != "" {
			t.Errorf("%q projected %q — a hostname is not an address and must not become a key",
				v, got[i])
		}
	}
}

// 🔒 Sysmon 3 writes Protocol=tcp; Windows 5156 writes Protocol=6. Without the number-to-keyword
// mapping the same protocol never correlates across those two providers.
func TestProtocolNumbersAndNamesUnify(t *testing.T) {
	i := slotOf(t, "protocol")
	byNumber := ExtractCorrelationKeys(map[string]string{"Protocol": "6"})
	byName := ExtractCorrelationKeys(map[string]string{"Protocol": "TCP"})
	if i >= len(byNumber) || byNumber[i] == "" {
		t.Fatal("a numeric protocol projected to nothing")
	}
	if byNumber[i] != byName[i] {
		t.Errorf("Protocol=6 projected %q but Protocol=TCP projected %q", byNumber[i], byName[i])
	}
	if byNumber[i] != "tcp" {
		t.Errorf("protocol 6 projected %q, want tcp", byNumber[i])
	}
	// An unlisted number stays decimal rather than being given an invented name.
	odd := ExtractCorrelationKeys(map[string]string{"Protocol": "253"})
	if odd[i] != "253" {
		t.Errorf("unlisted protocol projected %q, want the decimal verbatim", odd[i])
	}
}

func TestPortCanonicalization(t *testing.T) {
	i := slotOf(t, "dst_port")
	cases := map[string]string{
		"443":   "443",
		"00443": "443", // leading zeros are the same port
		"0":     "",    // port 0 means "none"
		"https": "",    // a service name is not a port
		"70000": "",    // out of range
		"":      "",
	}
	for in, want := range cases {
		got := ExtractCorrelationKeys(map[string]string{"DestinationPort": in})
		var have string
		if i < len(got) {
			have = got[i]
		}
		if have != want {
			t.Errorf("DestinationPort=%q projected %q, want %q", in, have, want)
		}
	}
}

// The Suricata field spellings must reach the slots, or every EVE event projects nothing and no
// field rule can ever match one — silently.
func TestSuricataFieldNamesProjectIntoTheNetworkSlots(t *testing.T) {
	got := ExtractCorrelationKeys(map[string]string{
		"src_ip": "10.0.0.5", "dest_ip": "93.184.216.34", "dest_port": "443", "proto": "TCP",
	})
	for slot, want := range map[string]string{
		"ip_address": "10.0.0.5",
		"dst_ip":     "93.184.216.34",
		"dst_port":   "443",
		"protocol":   "tcp",
	} {
		i := slotOf(t, slot)
		if i >= len(got) || got[i] != want {
			var have string
			if i < len(got) {
				have = got[i]
			}
			t.Errorf("%s projected %q, want %q", slot, have, want)
		}
	}
}

// 🔒 A Windows 4624 and a Suricata alert naming the same host must land on the SAME key. This is
// the whole point of the vocabulary change, asserted directly rather than inferred.
func TestWindowsAndSuricataAgreeOnAnAddressKey(t *testing.T) {
	i := slotOf(t, "ip_address")
	win := ExtractCorrelationKeys(map[string]string{"IpAddress": "::ffff:203.0.113.9"})
	eve := ExtractCorrelationKeys(map[string]string{"src_ip": "203.0.113.9"})
	if i >= len(win) || win[i] == "" {
		t.Fatal("the Windows record projected no address")
	}
	if win[i] != eve[i] {
		t.Errorf("Windows projected %q, Suricata projected %q — the cross-source join would "+
			"silently find nothing", win[i], eve[i])
	}
}

// A rule author may write src_ip; it resolves to the ip_address slot rather than costing one.
func TestSrcIPResolvesToTheIPAddressSlot(t *testing.T) {
	i, ok := consts.CorrelationSlotIndex("src_ip")
	if !ok {
		t.Fatal("src_ip does not resolve — a network analyst's first guess is refused")
	}
	if i != consts.SlotIPAddress {
		t.Errorf("src_ip resolves to slot %d, want %d (ip_address)", i, consts.SlotIPAddress)
	}
}

// 🔒 A protocol number is one byte. Kept verbatim, an unlisted number had no length bound —
// "000…0253" of any length went into a 10-byte slot — and "0253" and "253" were two keys.
func TestProtocolNumbersAreBoundedAndCanonical(t *testing.T) {
	i := slotOf(t, "protocol")
	cases := map[string]string{
		"253":                          "253",
		"0253":                         "253",
		"0000000000000000000000000253": "253",
		"256":                          "", // not a protocol number
		"99999999999999999999999":      "",
		"0":                            "", // the "no protocol" of a bind or listen record
		"58":                           "ipv6-icmp",
		"ICMPv6":                       "ipv6-icmp", // iptables' spelling of 58
		"ipv6-icmp":                    "ipv6-icmp",
	}
	for in, want := range cases {
		got := ExtractCorrelationKeys(map[string]string{"Protocol": in})
		var have string
		if i < len(got) {
			have = got[i]
		}
		if have != want {
			t.Errorf("Protocol=%q projected %q, want %q", in, have, want)
		}
		if len(have) > consts.CorrelationSlotMaxLen[i] {
			t.Errorf("Protocol=%q projected %d bytes, past the slot bound %d", in, len(have), consts.CorrelationSlotMaxLen[i])
		}
	}
}

// Bracketed endpoints are how IPv6 addresses with a port are written. Brackets are stripped only
// as a pair, so a stray one is not quietly accepted as an address.
func TestBracketedAddressesCanonicalize(t *testing.T) {
	i := slotOf(t, "ip_address")
	cases := map[string]string{
		"[::1]":                 "::1",
		"[::1]:80":              "::1",
		"[fe80::1%eth0]:443":    "fe80::1",
		"[::ffff:10.0.0.5]:445": "10.0.0.5",
		"10.0.0.5]":             "",
		"[10.0.0.5":             "",
	}
	for in, want := range cases {
		got := ExtractCorrelationKeys(map[string]string{"IpAddress": in})
		var have string
		if i < len(got) {
			have = got[i]
		}
		if have != want {
			t.Errorf("IpAddress=%q projected %q, want %q", in, have, want)
		}
	}
}

// 🔒 Loopback is evidence, not a sentinel: a 4624 from 127.0.0.1 is how a tunnelled RDP logon
// looks. It stays a key. The cost — global-scope rules on an address field link loopback activity
// across hosts — is documented in RULES.md §9 rather than paid by hiding the evidence.
func TestLoopbackIsAKey(t *testing.T) {
	i := slotOf(t, "ip_address")
	for _, v := range []string{"127.0.0.1", "::1"} {
		got := ExtractCorrelationKeys(map[string]string{"IpAddress": v})
		if i >= len(got) || got[i] != v {
			t.Errorf("IpAddress=%q projected %v, want it kept as %q", v, got, v)
		}
	}
}

// 🔒 WFP names endpoints by the local side, so an inbound 5156 lists this machine's listener as
// the "source". ip_address means the initiator, so inbound records are oriented before
// projection. The inbound fields are Microsoft's own 5156 example.
func TestInboundWFPIsOrientedByInitiator(t *testing.T) {
	wfp := func(direction string) map[string]string {
		return map[string]string{
			"Direction": direction, "Application": `\device\harddiskvolume2\documents\listener.exe`,
			"SourceAddress": "10.0.0.10", "SourcePort": "3333",
			"DestAddress": "10.0.0.100", "DestPort": "49278", "Protocol": "6",
		}
	}
	check := func(name string, got []string, want map[string]string) {
		t.Helper()
		for slot, w := range want {
			j := slotOf(t, slot)
			var have string
			if j < len(got) {
				have = got[j]
			}
			if have != w {
				t.Errorf("%s: %s = %q, want %q", name, slot, have, w)
			}
		}
	}

	inbound := map[string]string{"ip_address": "10.0.0.100", "dst_ip": "10.0.0.10", "dst_port": "3333", "protocol": "tcp"}
	check("inbound", ExtractCorrelationKeys(wfp("%%14592")), inbound)
	check("inbound, rendered", ExtractCorrelationKeys(wfp("Inbound")), inbound)
	check("outbound", ExtractCorrelationKeys(wfp("%%14593")),
		map[string]string{"ip_address": "10.0.0.10", "dst_ip": "10.0.0.100", "dst_port": "49278", "protocol": "tcp"})

	// Sysmon 3 has no Direction field; its spellings are left as written.
	sysmon := ExtractCorrelationKeys(map[string]string{
		"Initiated": "false", "SourceIp": "203.0.113.9", "DestinationIp": "10.0.0.10", "DestinationPort": "3389",
	})
	check("sysmon", sysmon, map[string]string{"ip_address": "203.0.113.9", "dst_ip": "10.0.0.10", "dst_port": "3389"})
}
