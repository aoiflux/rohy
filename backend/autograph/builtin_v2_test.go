package autograph

import (
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// The v2 built-in rules, run against events shaped like the records they are written for.
//
// TestBuiltinRulesDriveTheEngine already proves each shipped rule CAN fire, but it does so
// against a stream constructed to satisfy the rule — which cannot catch a rule that fires on
// anything. These tests do the opposite: they feed realistic events, including ones that
// should NOT match, and assert the rule discriminates.
//
// That distinction is the whole value of the field and temporal algorithms. A rule that
// matches everything is worse than no rule, because it looks like a finding.

func builtinByID(t *testing.T, id string) *rules.Rule {
	t.Helper()
	builtins, errs := rules.Builtins()
	if len(errs) != 0 {
		t.Fatalf("builtins failed to load: %+v", errs)
	}
	for _, r := range builtins {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no builtin %q", id)
	return nil
}

func withFields(e *graphene.Event, fields map[string]string) *graphene.Event {
	e.ParsedFields = fields
	e.ComputeCorrelationKeys()
	return e
}

func TestBuiltinLogonThenProcessCreationRequiresTheSameSession(t *testing.T) {
	rule := builtinByID(t, "logon-then-process-creation-same-session")

	// Two logons on one host, and a process created under the SECOND one. Windows names the
	// session differently on each event shape, which is exactly what this rule depends on
	// logon_id unifying.
	events := []*graphene.Event{
		withFields(ev(1, "4624", "HOST-A", 0), map[string]string{"TargetLogonId": "0xAAA"}),
		withFields(ev(2, "4624", "HOST-A", 1), map[string]string{"TargetLogonId": "0xBBB"}),
		withFields(ev(3, "4688", "HOST-A", 2), map[string]string{
			"SubjectLogonId": "0xBBB", "NewProcessId": "0x100", "NewProcessName": `C:\Windows\System32\cmd.exe`,
		}),
	}
	res := Generate(&rule.Spec, events)

	if res.Matches != 1 {
		t.Fatalf("matches = %d, want 1", res.Matches)
	}
	rel := res.Relations[0]
	if rel.From != 2 || rel.To != 3 {
		t.Errorf("edge %d->%d, want 2->3: the process belongs to the SECOND logon's session",
			rel.From, rel.To)
	}
	if len(rel.Basis) == 0 || rel.Basis[0] != "logon_id=0xbbb" {
		t.Errorf("basis = %v, want the session it matched on", rel.Basis)
	}
}

func TestBuiltinLogonThenProcessCreationIgnoresOtherSessions(t *testing.T) {
	rule := builtinByID(t, "logon-then-process-creation-same-session")

	// A logon, then a process created under a DIFFERENT session. Sequence correlation would
	// pair these; this rule must not.
	res := Generate(&rule.Spec, []*graphene.Event{
		withFields(ev(1, "4624", "HOST-A", 0), map[string]string{"TargetLogonId": "0xAAA"}),
		withFields(ev(2, "4688", "HOST-A", 1), map[string]string{"SubjectLogonId": "0xZZZ"}),
	})
	if len(res.Relations) != 0 {
		t.Fatalf("a process from another session must not pair with this logon: %d edges",
			len(res.Relations))
	}
}

func TestBuiltinPasswordResetThenLogonRequiresTheSameAccount(t *testing.T) {
	rule := builtinByID(t, "password-reset-then-logon-same-account")

	// A reset for alice, then a logon by bob, then a logon by alice. The ordering-only version
	// of this rule pairs the reset with bob — which on a busy domain controller is the normal
	// case and is exactly the false pairing match_fields removes.
	events := []*graphene.Event{
		withFields(ev(1, "4724", "DC-1", 0), map[string]string{"TargetUserName": "alice"}),
		withFields(ev(2, "4624", "DC-1", 1), map[string]string{"TargetUserName": "bob"}),
		withFields(ev(3, "4624", "DC-1", 2), map[string]string{"TargetUserName": "alice"}),
	}
	res := Generate(&rule.Spec, events)

	if res.Matches != 1 {
		t.Fatalf("matches = %d, want 1", res.Matches)
	}
	if rel := res.Relations[0]; rel.From != 1 || rel.To != 3 {
		t.Errorf("edge %d->%d, want 1->3 (alice's reset to alice's logon)", rel.From, rel.To)
	}
}

func TestBuiltinFailedLogonBurstRespectsItsWindow(t *testing.T) {
	rule := builtinByID(t, "failed-logon-burst-then-success-within-five-minutes")

	// Inside the window: a burst then a success, each step a minute apart.
	burst := []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		ev(2, "4625", "HOST-A", 60),
		ev(3, "4625", "HOST-A", 120),
		ev(4, "4624", "HOST-A", 180),
	}
	if res := Generate(&rule.Spec, burst); res.Matches != 1 {
		t.Fatalf("a burst inside the window should match: %d", res.Matches)
	}

	// The same events with the success a day later. The unbounded rule would still pair them,
	// which is a materially different claim.
	spread := []*graphene.Event{
		ev(1, "4625", "HOST-A", 0),
		ev(2, "4625", "HOST-A", 60),
		ev(3, "4625", "HOST-A", 120),
		ev(4, "4624", "HOST-A", 86400),
	}
	if res := Generate(&rule.Spec, spread); res.Matches != 0 {
		t.Fatalf("a success a day after the burst must not match a five-minute rule: %d", res.Matches)
	}
}

func TestBuiltinServiceInstalledThenLogClearedRespectsItsWindow(t *testing.T) {
	rule := builtinByID(t, "service-installed-then-security-log-cleared-within-an-hour")

	within := []*graphene.Event{ev(1, "7045", "SRV-1", 0), ev(2, "1102", "SRV-1", 1800)}
	if res := Generate(&rule.Spec, within); res.Matches != 1 {
		t.Fatalf("half an hour apart should match an hour window: %d", res.Matches)
	}
	beyond := []*graphene.Event{ev(1, "7045", "SRV-1", 0), ev(2, "1102", "SRV-1", 7200)}
	if res := Generate(&rule.Spec, beyond); res.Matches != 0 {
		t.Fatalf("two hours apart must not match an hour window: %d", res.Matches)
	}
}

func TestBuiltinProcessAncestryResolvesThroughPIDLifetimes(t *testing.T) {
	rule := builtinByID(t, "process-ancestry")

	// The reuse case, as a shipped rule would meet it: PID 0x100 is explorer, exits, and is
	// handed to svchost before a child names it as parent.
	events := []*graphene.Event{
		withFields(ev(1, "4688", "HOST-A", 0), map[string]string{
			"ProcessId": "0x4", "NewProcessId": "0x100", "NewProcessName": `C:\Windows\explorer.exe`}),
		withFields(ev(2, "4689", "HOST-A", 100), map[string]string{"ProcessId": "0x100"}),
		withFields(ev(3, "4688", "HOST-A", 200), map[string]string{
			"ProcessId": "0x4", "NewProcessId": "0x100", "NewProcessName": `C:\Windows\System32\svchost.exe`}),
		withFields(ev(4, "4688", "HOST-A", 300), map[string]string{
			"ProcessId": "0x100", "NewProcessId": "0x300", "NewProcessName": `C:\Windows\System32\cmd.exe`}),
	}
	res := Generate(&rule.Spec, events)

	var linked bool
	for _, rel := range res.Relations {
		if rel.From == 1 && rel.To == 4 {
			t.Error("linked cmd.exe to the explorer.exe that had already released PID 0x100")
		}
		if rel.From == 3 && rel.To == 4 {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("cmd.exe should be linked to the svchost holding PID 0x100 at the time: %+v", res.Relations)
	}
	if rule.Spec.LabelFor(0) != "spawned" {
		t.Errorf("lineage edges should carry the rule's label, got %q", rule.Spec.LabelFor(0))
	}
}

// TestBuiltinsShareOneFormatVersion is the portability guard for the shipped library.
//
// The format has a single version, so every rule declares it — including the ones using the
// algorithms added in v0.2.0. That is safe because a build without those algorithms refuses
// them BY NAME, which is both sufficient and more useful than a version number: it says which
// matcher is missing. A rule declaring anything else would be refused by this build or would
// needlessly claim to be unreadable by another.
func TestBuiltinsShareOneFormatVersion(t *testing.T) {
	builtins, _ := rules.Builtins()
	for _, r := range builtins {
		if _, ok := consts.AlgorithmByName(r.AlgorithmOrDefault()); !ok {
			t.Fatalf("%s: unknown algorithm", r.ID)
		}
		if r.FormatVersion != consts.RuleFormatVersion {
			t.Errorf("%s: declares format_version %d, want %d",
				r.ID, r.FormatVersion, consts.RuleFormatVersion)
		}
	}
}
