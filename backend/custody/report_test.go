package custody

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// The custody report for one event (plan §14.4, §15.6), against literals: what each layer says for
// each state, and the rules no state may break.

const (
	snapA = "aa00000000000000000000000000000000000000000000000000000000000000"
	snapP = "bb00000000000000000000000000000000000000000000000000000000000000"
	snapX = "cc00000000000000000000000000000000000000000000000000000000000000"
)

// postureGaps is what the engine reports for every event of a case opened the way rohy opens one
// (pinned against the real engine by TestRohysPostureProducesExactlyTheseGaps).
func postureGaps() []graphene.CustodyGap {
	return []graphene.CustodyGap{
		{Layer: consts.CustodyLayerAttestation, Detail: "the snapshot carries no attestation"},
		{Layer: consts.CustodyLayerSegments, Detail: "no retired WAL segments. Set Options.Retention"},
		{Layer: consts.CustodyLayerAudit, Detail: "no audit log. Set Options.Audit"},
		{Layer: consts.CustodyLayerRoles, Detail: "no role grants. Set Options.Roles"},
		{Layer: consts.CustodyLayerExternal, Detail: "Use CustodyForAnchored to close this"},
	}
}

func sealedInput() ReportInput {
	return ReportInput{
		Event: 7,
		Engine: graphene.EventCustody{
			Live: true, InSnapshot: true, HasFingerprint: true, Snapshot: snapA,
			ProofState: consts.EventProofOK, RawBound: true, Gaps: postureGaps(),
		},
		Current:  &graphene.SnapshotRoots{Snapshot: snapA, Prev: snapP},
		Chain:    ChainReport{State: consts.ChainIntact, Records: 2},
		Exported: true,
	}
}

func layerOf(t *testing.T, ls []Layer, name string) Layer {
	t.Helper()
	for _, l := range ls {
		if l.Layer == name {
			return l
		}
	}
	t.Fatalf("no %q layer in %+v", name, ls)
	return Layer{}
}

func hasLayer(ls []Layer, name string) bool {
	return slices.ContainsFunc(ls, func(l Layer) bool { return l.Layer == name })
}

func TestPresenceKeepsEveryStateApart(t *testing.T) {
	removal := &graphene.CustodyRedaction{Scope: consts.RedactionScopeNode, At: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC), Reason: "erasure request 12"}
	for _, tc := range []struct {
		name string
		edit func(*ReportInput)
		want string
	}{
		{"in the seal", func(*ReportInput) {}, consts.CustodyPresenceInSeal},
		{"unavailable", func(in *ReportInput) { *in = ReportInput{Event: 7, Unavailable: true} }, consts.CustodyPresenceUnavailable},
		{"written since the seal", func(in *ReportInput) {
			in.Engine.InSnapshot, in.Engine.Snapshot, in.Engine.ProofState = false, "", consts.EventProofWrittenSinceSeal
		}, consts.CustodyPresenceWrittenSinceSeal},
		{"no fingerprint", func(in *ReportInput) {
			in.Engine = graphene.EventCustody{Live: true, ProofState: consts.EventProofNotSealed, Gaps: postureGaps()}
			in.Current, in.Chain = nil, ChainReport{State: consts.ChainNoImage}
		}, consts.CustodyPresenceNoFingerprint},
		{"deleted since the seal", func(in *ReportInput) {
			in.Engine.Live, in.Engine.ProofState, in.Engine.RawBound = false, "", false
		}, consts.CustodyPresenceRemovedSinceSeal},
		{"redacted", func(in *ReportInput) {
			in.Engine = graphene.EventCustody{HasFingerprint: true, Redacted: removal, Gaps: postureGaps()}
		}, consts.CustodyPresenceRedacted},
		{"never held", func(in *ReportInput) {
			in.Engine = graphene.EventCustody{HasFingerprint: true, Gaps: postureGaps()}
		}, consts.CustodyPresenceNotInCase},
		// 🔒 Absence is decided before the seal. An unsealed case asked about an id it never held
		// must not answer "no fingerprint yet", which claims the event exists.
		{"never held, in a case never sealed", func(in *ReportInput) {
			in.Engine = graphene.EventCustody{Gaps: postureGaps()}
			in.Current, in.Chain = nil, ChainReport{State: consts.ChainNoImage}
		}, consts.CustodyPresenceNotInCase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := sealedInput()
			tc.edit(&in)
			r := EventReport(in)
			if r.Presence != tc.want {
				t.Fatalf("presence = %q, want %q", r.Presence, tc.want)
			}
			if r.PresenceMessage == "" || r.Event != 7 {
				t.Errorf("presence message %q, event %d", r.PresenceMessage, r.Event)
			}
		})
	}
}

func TestARedactionSaysWhenAndWhy(t *testing.T) {
	in := sealedInput()
	in.Engine = graphene.EventCustody{HasFingerprint: true, Gaps: postureGaps(), Redacted: &graphene.CustodyRedaction{
		Scope: consts.RedactionScopeNode, At: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC), Reason: "erasure request 12",
	}}
	r := EventReport(in)
	if !strings.Contains(r.PresenceMessage, "2026-09-01T08:00:00Z") || !strings.Contains(r.PresenceMessage, "erasure request 12") {
		t.Errorf("presence message %q does not say when and why", r.PresenceMessage)
	}
	red := layerOf(t, r.Layers, consts.CustodyEventLayerRedaction)
	if red.State != consts.CustodyNotEstablished || !slices.Contains(red.Notes, consts.MsgCustodyRedactionUnbound) {
		t.Errorf("an unbound removal: %+v", red)
	}
}

func TestTheSealLayerFollowsThePresence(t *testing.T) {
	r := EventReport(sealedInput())
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerSeal); l.State != consts.CustodyEstablished {
		t.Errorf("in the seal: %+v", l)
	}

	in := sealedInput()
	in.Engine.Gaps = append(in.Engine.Gaps, graphene.CustodyGap{
		Layer: consts.CustodyLayerSnapshot, Fatal: true, Detail: "the inclusion proof does not resolve against the snapshot root: bad sibling",
	})
	l := layerOf(t, EventReport(in).Layers, consts.CustodyEventLayerSeal)
	if l.State != consts.CustodyBroken || !strings.Contains(l.Cause, "bad sibling") {
		t.Errorf("a proof that does not resolve: %+v", l)
	}

	in = sealedInput()
	in.Engine.Live, in.Engine.ProofState = false, ""
	r = EventReport(in)
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerSeal); l.State != consts.CustodyNotEstablished {
		t.Errorf("deleted since the seal: %+v", l)
	}
	if hasLayer(r.Layers, consts.CustodyEventLayerRecord) || hasLayer(r.Layers, consts.CustodyEventLayerRaw) {
		t.Errorf("an event the case no longer holds has record layers: %+v", r.Layers)
	}
}

func TestTheRecordAndRawLayersFollowTheProof(t *testing.T) {
	E, N, B := consts.CustodyEstablished, consts.CustodyNotEstablished, consts.CustodyBroken
	for _, tc := range []struct {
		proof       string
		bound       bool
		record, raw string
	}{
		{consts.EventProofOK, true, E, E},
		{consts.EventProofOK, false, E, N},
		{consts.EventProofTooLarge, true, E, E},
		{consts.EventProofChangedSinceSeal, false, N, N},
		{consts.EventProofImageOutdated, false, N, N},
		{consts.EventProofRawMismatch, false, E, B},
		{consts.EventProofRawUnreadable, false, N, B},
		{consts.EventProofWrittenSinceSeal, false, N, N},
		{consts.EventProofNotSealed, false, N, N},
		{"frobbed", true, N, N},
	} {
		in := sealedInput()
		in.Engine.ProofState, in.Engine.RawBound = tc.proof, tc.bound
		r := EventReport(in)
		rec, raw := layerOf(t, r.Layers, consts.CustodyEventLayerRecord), layerOf(t, r.Layers, consts.CustodyEventLayerRaw)
		if rec.State != tc.record || raw.State != tc.raw {
			t.Errorf("%s (bound %v): record %s, raw %s; want %s, %s", tc.proof, tc.bound, rec.State, raw.State, tc.record, tc.raw)
		}
		if tc.proof == "frobbed" && !strings.Contains(rec.Message, "frobbed") {
			t.Errorf("an unknown proof state is not named: %q", rec.Message)
		}
	}
}

func TestTheHistoryLayer(t *testing.T) {
	E, N, B := consts.CustodyEstablished, consts.CustodyNotEstablished, consts.CustodyBroken
	kept := []Damage{{Reason: "corrupt", KeptAt: `C:\case\roots\roots.json.unreadable-20261101T000000Z`}}
	for _, tc := range []struct {
		name  string
		chain ChainReport
		none  bool
		want  string
	}{
		{"intact", ChainReport{State: consts.ChainIntact}, false, E},
		{"one unrecorded seal", ChainReport{State: consts.ChainUnrecordedSeal}, false, E},
		{"rolled back", ChainReport{State: consts.ChainRolledBack, Later: 2}, false, B},
		{"intact after a fork", ChainReport{State: consts.ChainIntact, Forks: 1, ForkSkipped: 2}, false, B},
		{"diverged", ChainReport{State: consts.ChainDiverged}, false, N},
		{"intact with gaps", ChainReport{State: consts.ChainIntact, Gaps: 3}, false, N},
		{"intact since a reset", ChainReport{State: consts.ChainIntact, Kept: kept}, false, N},
		{"held", ChainReport{State: consts.ChainEmpty, Held: &Damage{Reason: "sharing violation"}}, false, N},
		{"empty", ChainReport{State: consts.ChainEmpty}, false, N},
		{"no image", ChainReport{State: consts.ChainNoImage}, false, N},
		{"no history at all", ChainReport{}, true, N},
		{"a state this build does not know", ChainReport{State: "sideways"}, false, N},
	} {
		in := sealedInput()
		in.Chain, in.NoHistory = tc.chain, tc.none
		l := layerOf(t, EventReport(in).Layers, consts.CustodyEventLayerHistory)
		if l.State != tc.want {
			t.Errorf("%s: %s (%s), want %s", tc.name, l.State, l.Message, tc.want)
		}
	}
}

func TestTheKeptFingerprint(t *testing.T) {
	E, N, B := consts.CustodyEstablished, consts.CustodyNotEstablished, consts.CustodyBroken
	for _, tc := range []struct {
		name      string
		edit      func(*ReportInput)
		want      string
		form      string
		wantNote  string
		wantInMsg string
	}{
		{"none supplied", func(*ReportInput) {}, N, consts.CustodyFormCaseOnly, consts.MsgCustodyKeptNoneExported, ""},
		{"none supplied, never exported", func(in *ReportInput) { in.Exported = false }, N, consts.CustodyFormCaseOnly, consts.MsgCustodyKeptNoneNever, ""},
		{"the current image", func(in *ReportInput) { in.Kept = snapA }, E, consts.CustodyFormKept, "", ""},
		{"the current image, never exported", func(in *ReportInput) { in.Kept, in.Exported = snapA, false }, E, consts.CustodyFormKept, consts.MsgCustodyKeptMatchNotOut, ""},
		{"the current image, event not in it", func(in *ReportInput) {
			in.Kept = snapA
			in.Engine.InSnapshot, in.Engine.Snapshot, in.Engine.ProofState = false, "", consts.EventProofWrittenSinceSeal
		}, E, consts.CustodyFormKept, consts.MsgCustodyKeptEventOutside, ""},
		{"an ancestor", func(in *ReportInput) { in.Kept, in.KeptKnown, in.KeptAncestor = snapP, true, true }, N, consts.CustodyFormKept, "", consts.MsgCustodyKeptAncestor},
		{"recorded, but not an ancestor", func(in *ReportInput) { in.Kept, in.KeptKnown = snapX, true }, B, consts.CustodyFormKept, "", consts.MsgCustodyKeptRecordedOff},
		{"unknown", func(in *ReportInput) { in.Kept = snapX }, B, consts.CustodyFormKept, "", consts.MsgCustodyKeptUnknown},
		{"unknown, after a reset", func(in *ReportInput) {
			in.Kept = snapX
			in.Chain.Kept = []Damage{{Reason: "corrupt", KeptAt: `C:\case\roots\roots.json.unreadable-20261101T000000Z`}}
		}, B, consts.CustodyFormKept, "", "unreadable-20261101T000000Z"},
		// 🔒 Broken, not unmade: the image is not the one kept. Softening it would let whoever can
		// make the history unreadable turn a rollback into a shrug.
		{"held history", func(in *ReportInput) {
			in.Kept = snapX
			in.Chain.Held = &Damage{Reason: "sharing violation"}
		}, B, consts.CustodyFormKept, "", "sharing violation"},
		{"no history at all", func(in *ReportInput) { in.Kept, in.NoHistory = snapX, true }, B, consts.CustodyFormKept, "", consts.MsgCustodyHistoryNone},
		// A walk that stops at a gap may not be a rollback; the message says both.
		{"recorded, but behind a gap", func(in *ReportInput) {
			in.Kept, in.KeptKnown = snapX, true
			in.Chain.Gaps = 1
		}, B, consts.CustodyFormKept, "", consts.MsgCustodyKeptRecordedGaps},
		{"unknown, in a history with gaps", func(in *ReportInput) {
			in.Kept = snapX
			in.Chain.Gaps = 2
		}, B, consts.CustodyFormKept, "", "2 recorded seals do not follow"},
		{"no image at all", func(in *ReportInput) {
			in.Kept = snapX
			in.Current, in.Chain = nil, ChainReport{State: consts.ChainNoImage}
			in.Engine = graphene.EventCustody{Live: true, ProofState: consts.EventProofNotSealed, Gaps: postureGaps()}
		}, B, consts.CustodyFormKept, "", consts.MsgCustodyKeptNoImage},
		// 🔒 The kept fingerprint is compared whatever the event's state: an event missing from a
		// rolled-back image is exactly when it matters.
		{"unknown, event not in the case", func(in *ReportInput) {
			in.Kept = snapX
			in.Engine = graphene.EventCustody{HasFingerprint: true, Gaps: postureGaps()}
		}, B, consts.CustodyFormKept, "", consts.MsgCustodyKeptUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := sealedInput()
			tc.edit(&in)
			r := EventReport(in)
			l := layerOf(t, r.Layers, consts.CustodyEventLayerKept)
			if l.State != tc.want || r.Form != tc.form {
				t.Fatalf("state %s, form %s; want %s, %s (%+v)", l.State, r.Form, tc.want, tc.form, l)
			}
			if tc.wantNote != "" && !slices.Contains(l.Notes, tc.wantNote) {
				t.Errorf("notes %q lack %q", l.Notes, tc.wantNote)
			}
			if tc.wantInMsg != "" && !strings.Contains(l.Message+strings.Join(l.Notes, " "), tc.wantInMsg) {
				t.Errorf("message %q, notes %q lack %q", l.Message, l.Notes, tc.wantInMsg)
			}
			if (l.State == consts.CustodyEstablished) != slices.Contains(r.Establishes, consts.CustodyEstablishesKept) {
				t.Errorf("establishes %q disagrees with the layer %s", r.Establishes, l.State)
			}
		})
	}
}

// combos is every presence × proof × chain combination worth crossing, for the invariants below.
func combos() []ReportInput {
	engines := []graphene.EventCustody{
		{Live: true, InSnapshot: true, HasFingerprint: true, Snapshot: snapA},
		{Live: true, HasFingerprint: true},
		{Live: true},
		{InSnapshot: true, HasFingerprint: true, Snapshot: snapA},
		{HasFingerprint: true},
		{},
	}
	proofs := []string{"", consts.EventProofOK, consts.EventProofChangedSinceSeal, consts.EventProofRawMismatch, "frobbed"}
	chains := []ChainReport{
		{State: consts.ChainIntact}, {State: consts.ChainRolledBack}, {State: consts.ChainDiverged},
		{State: consts.ChainIntact, Forks: 1}, {State: consts.ChainEmpty}, {State: "sideways"},
	}
	var out []ReportInput
	for _, e := range engines {
		for _, p := range proofs {
			for _, c := range chains {
				for _, exported := range []bool{false, true} {
					e := e
					e.ProofState, e.RawBound, e.Gaps = p, p == consts.EventProofOK, postureGaps()
					in := ReportInput{Event: 1, Engine: e, Chain: c, Exported: exported}
					if e.HasFingerprint {
						in.Current = &graphene.SnapshotRoots{Snapshot: snapA, Prev: snapP}
					}
					out = append(out, in)
				}
			}
		}
	}
	return out
}

// 🔒 §14.4: the unanchored form can never read as complete. Whatever else holds, a report made
// against the case alone says its kept-fingerprint layer is not established, and says why.
func TestTheCaseOnlyFormNeverEstablishesTheKeptLayer(t *testing.T) {
	for _, in := range combos() {
		r := EventReport(in)
		l := layerOf(t, r.Layers, consts.CustodyEventLayerKept)
		if r.Form != consts.CustodyFormCaseOnly || l.State != consts.CustodyNotEstablished {
			t.Fatalf("case only, %+v: form %s, kept layer %s", in.Engine, r.Form, l.State)
		}
		if !slices.Contains(r.DoesNotEstablish, consts.CustodyNotKept) {
			t.Fatalf("case only, %+v: does-not-establish lacks the self-comparison caveat", in.Engine)
		}
	}
}

// 🔒 A rollback is never softened: a rolled-back or forked history is broken in every combination,
// and NO layer of the report offers a seal as its remedy — a seal then would build on an image that
// is not the one rohy last sealed.
func TestARolledBackHistoryIsAlwaysBroken(t *testing.T) {
	offered := 0
	for _, in := range combos() {
		r := EventReport(in)
		broken := in.Chain.State == consts.ChainRolledBack || in.Chain.Forks > 0
		if l := layerOf(t, r.Layers, consts.CustodyEventLayerHistory); broken && l.State != consts.CustodyBroken {
			t.Fatalf("%+v: history %s", in.Chain, l.State)
		}
		for _, l := range r.Layers {
			if l.Remedy == "" {
				continue
			}
			if broken {
				t.Fatalf("%+v: layer %s offered %q over a broken history", in.Chain, l.Layer, l.Remedy)
			}
			offered++
		}
		for _, l := range r.Layers {
			if text := strings.ToLower(l.Message + strings.Join(l.Notes, " ")); strings.Contains(text, "seal the case") || strings.Contains(text, "seal again") {
				t.Fatalf("layer %s puts a remedy in its message rather than in Remedy: %q", l.Layer, l.Message)
			}
		}
	}
	if offered == 0 {
		t.Fatal("no combination offered a remedy at all, so this test checks nothing")
	}
}

func TestARemedyIsOfferedWhereSealingIsTheFix(t *testing.T) {
	in := sealedInput()
	in.Engine.InSnapshot, in.Engine.Snapshot, in.Engine.ProofState = false, "", consts.EventProofWrittenSinceSeal
	if l := layerOf(t, EventReport(in).Layers, consts.CustodyEventLayerSeal); l.Remedy != consts.MsgCustodyRemedySeal {
		t.Errorf("written since the seal: remedy %q", l.Remedy)
	}
	in = sealedInput()
	in.Engine.ProofState = consts.EventProofChangedSinceSeal
	if l := layerOf(t, EventReport(in).Layers, consts.CustodyEventLayerRecord); l.Remedy != consts.MsgCustodyRemedyReseal {
		t.Errorf("changed since the seal: remedy %q", l.Remedy)
	}
	// Damage is not repaired by a seal.
	for _, proof := range []string{consts.EventProofRawMismatch, consts.EventProofRawUnreadable, consts.EventProofImageDamaged} {
		in = sealedInput()
		in.Engine.ProofState, in.Engine.RawBound = proof, false
		for _, l := range EventReport(in).Layers {
			if l.Remedy != "" {
				t.Errorf("%s: layer %s offered %q", proof, l.Layer, l.Remedy)
			}
		}
	}
}

func TestADamagedImageIsNotCheckedFurther(t *testing.T) {
	in := sealedInput()
	in.Engine.ProofState, in.Engine.RawBound = consts.EventProofImageDamaged, false
	in.Engine.Gaps = append(in.Engine.Gaps, graphene.CustodyGap{Layer: consts.CustodyLayerSnapshot, Fatal: true, Detail: "does not resolve"})
	r := EventReport(in)
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerSeal); l.State != consts.CustodyBroken {
		t.Errorf("seal: %+v", l)
	}
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerRecord); l.State != consts.CustodyNotEstablished || l.Message != consts.MsgCustodyRecordDamaged {
		t.Errorf("record: %+v", l)
	}
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerRaw); l.State != consts.CustodyNotEstablished {
		t.Errorf("raw: %+v", l)
	}
}

// A stored record with a raw-record hash and no raw record is not one with no hash.
func TestAHashWithNoRawRecordIsNotCalledUnhashed(t *testing.T) {
	in := sealedInput()
	in.Engine.RawBound, in.Engine.RawHashed = false, true
	if l := layerOf(t, EventReport(in).Layers, consts.CustodyEventLayerRaw); l.Message != consts.MsgCustodyRawMissing {
		t.Errorf("raw: %+v", l)
	}
	in.Engine.RawHashed = false
	if l := layerOf(t, EventReport(in).Layers, consts.CustodyEventLayerRaw); l.Message != consts.MsgCustodyRawNoHash {
		t.Errorf("raw: %+v", l)
	}
}

// A redaction since the seal leaves the event in it; the seal layer must not say otherwise.
func TestARedactedEventStillInTheSealIsNotCalledAbsent(t *testing.T) {
	in := sealedInput()
	in.Engine = graphene.EventCustody{InSnapshot: true, HasFingerprint: true, Snapshot: snapA, Gaps: postureGaps(),
		Redacted: &graphene.CustodyRedaction{Scope: consts.RedactionScopeNode, Reason: "erasure"}}
	r := EventReport(in)
	if r.Presence != consts.CustodyPresenceRedacted {
		t.Fatalf("presence %s", r.Presence)
	}
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerSeal); l.Message != consts.MsgCustodySealRemoved {
		t.Errorf("seal: %q", l.Message)
	}
}

// What no report establishes, whatever it found: who did it, whether it is true, the parsed
// fields, and the source file.
func TestEveryReportSaysWhatItDoesNotEstablish(t *testing.T) {
	for _, in := range combos() {
		in.Kept = snapA
		r := EventReport(in)
		for _, want := range []string{
			consts.EventProofNotWho, consts.EventProofNotTrue, consts.EventProofNotParsed,
			consts.CustodyNotSourceFile, consts.CustodyNotHistory,
		} {
			if !slices.Contains(r.DoesNotEstablish, want) {
				t.Fatalf("%+v: does-not-establish lacks %q", in.Engine, want)
			}
		}
		broken, unestablished := 0, 0
		for _, l := range append(slices.Clone(r.Layers), r.CaseRecords...) {
			switch l.State {
			case consts.CustodyBroken:
				broken++
			case consts.CustodyNotEstablished:
				unestablished++
			case consts.CustodyEstablished:
			default:
				t.Fatalf("layer %q has state %q", l.Layer, l.State)
			}
			if l.Message == "" {
				t.Fatalf("layer %q has no message", l.Layer)
			}
		}
		if r.Broken != broken || r.NotEstablished != unestablished {
			t.Fatalf("counts %d/%d, layers say %d/%d", r.Broken, r.NotEstablished, broken, unestablished)
		}
	}
}

func TestCaseRecordsAreReadFromTheEnginesFields(t *testing.T) {
	r := EventReport(sealedInput())
	for _, name := range []string{
		consts.CustodyLayerAttestation, consts.CustodyLayerSegments, consts.CustodyLayerAudit,
		consts.CustodyLayerRoles, consts.CustodyRecordRemovals,
	} {
		if l := layerOf(t, r.CaseRecords, name); l.State != consts.CustodyNotEstablished || l.Cause != "" {
			t.Errorf("%s in rohy's posture: %+v", name, l)
		}
	}
	for _, l := range r.CaseRecords {
		if strings.Contains(l.Message, "Options.") || strings.Contains(l.Message, "CustodyFor") {
			t.Errorf("%s carries the engine's developer text: %q", l.Layer, l.Message)
		}
	}
	if hasLayer(r.CaseRecords, consts.CustodyLayerExternal) || hasLayer(r.Layers, consts.CustodyLayerExternal) {
		t.Error("the engine's external placeholder became a line; the kept-fingerprint layer replaces it")
	}

	in := sealedInput()
	in.Engine.Attested = true
	in.Engine.SegmentsChecked, in.Engine.AuditEntriesWalked = 2, 5
	in.Engine.GrantsWalked, in.Engine.RedactionsWalked = 3, 4
	in.Engine.Gaps = []graphene.CustodyGap{
		{Layer: consts.CustodyLayerAttestation, Detail: "attested but no verifier"},
		{Layer: consts.CustodyLayerAudit, Fatal: true, Detail: "the audit chain is broken: entry 3"},
		{Layer: consts.CustodyLayerAudit, Fatal: true, Detail: "2 retired segments but only 1 recorded compactions"},
		{Layer: "provenance", Detail: "a layer from a later engine"},
		{Layer: consts.CustodyLayerExternal, Detail: "Use CustodyForAnchored to close this"},
	}
	r = EventReport(in)
	for name, want := range map[string]string{
		consts.CustodyLayerAttestation: consts.CustodyNotEstablished,
		consts.CustodyLayerSegments:    consts.CustodyEstablished,
		consts.CustodyLayerAudit:       consts.CustodyBroken,
		consts.CustodyLayerRoles:       consts.CustodyNotEstablished,
		consts.CustodyRecordRemovals:   consts.CustodyNotEstablished,
		"provenance":                   consts.CustodyNotEstablished,
	} {
		if l := layerOf(t, r.CaseRecords, name); l.State != want {
			t.Errorf("%s: %s, want %s (%+v)", name, l.State, want, l)
		}
	}
	audit := layerOf(t, r.CaseRecords, consts.CustodyLayerAudit)
	if !strings.Contains(audit.Cause, "entry 3") || !strings.Contains(audit.Cause, "recorded compactions") {
		t.Errorf("both audit causes should be carried: %q", audit.Cause)
	}
	if l := layerOf(t, r.CaseRecords, "provenance"); !strings.Contains(l.Cause, "later engine") {
		t.Errorf("an unknown engine layer must carry the engine's words: %+v", l)
	}
}

func TestAnUnavailableCaseSaysSoAndNothingElse(t *testing.T) {
	r := EventReport(ReportInput{Event: 3, Unavailable: true, Kept: snapA})
	if r.Presence != consts.CustodyPresenceUnavailable || r.Broken != 0 || len(r.Establishes) != 0 {
		t.Fatalf("%+v", r)
	}
	// Nothing was compared, so the report must not say the supplied fingerprint was checked.
	if r.Form != consts.CustodyFormNone {
		t.Errorf("form %q", r.Form)
	}
	for _, l := range r.Layers {
		if l.State != consts.CustodyNotEstablished {
			t.Errorf("an unavailable case established %q", l.Layer)
		}
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	upper := strings.ToUpper(snapA)
	for in, want := range map[string]string{
		"":                            "",
		"   ":                         "",
		snapA:                         snapA,
		"  " + upper + "\n":           snapA,
		"\t" + snapP + "\r\n":         snapP,
		strings.Repeat("0", 64):       "!",
		snapA[:63]:                    "!",
		snapA + "0":                   "!",
		"0x" + snapA[2:]:              "!",
		strings.Repeat("g", 64):       "!",
		snapA[:32] + " " + snapA[33:]: "!",
	} {
		got, err := NormalizeFingerprint(in)
		if want == "!" {
			if err == nil {
				t.Errorf("%q accepted as %q", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
}

// Whatever is accepted is one lowercase 64-character hash that normalizes to itself — the form the
// history stores, so a match is a string comparison that cannot be fooled by case or padding.
func FuzzNormalizeFingerprint(f *testing.F) {
	for _, s := range []string{"", snapA, strings.ToUpper(snapP), " " + snapX + " ", strings.Repeat("0", 64), "zz"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got, err := NormalizeFingerprint(in)
		if err != nil {
			return
		}
		if got == "" {
			if strings.TrimSpace(in) != "" {
				t.Fatalf("%q normalized to nothing", in)
			}
			return
		}
		if len(got) != 64 || strings.ToLower(got) != got || got == strings.Repeat("0", 64) {
			t.Fatalf("%q normalized to %q", in, got)
		}
		if again, err := NormalizeFingerprint(got); err != nil || again != got {
			t.Fatalf("%q is not stable: %q, %v", got, again, err)
		}
	})
}

// Descends is checked against real seals, because what it trusts is graphene's binding of each
// link, not the strings.
func TestDescendsFollowsOnlyRecordedVerifyingLinks(t *testing.T) {
	r := realSeals(t, 3)
	h := historyOf(t, r...)
	for _, tc := range []struct {
		name    string
		current *graphene.SnapshotRoots
		kept    string
		want    bool
	}{
		{"two back", r[2], r[0].Snapshot, true},
		{"one back", r[2], r[1].Snapshot, true},
		{"itself is not an ancestor", r[2], r[2].Snapshot, false},
		{"a later seal, after a rollback", r[0], r[1].Snapshot, false},
		{"unknown", r[2], snapX, false},
		{"no image", nil, r[0].Snapshot, false},
		{"nothing kept", r[2], "", false},
	} {
		if got := h.DescendsFrom(tc.current, tc.kept); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}

	// An image one seal past the history still walks back through it.
	if !historyOf(t, r[0], r[1]).DescendsFrom(r[2], r[0].Snapshot) {
		t.Error("an unrecorded current seal did not walk back through the history")
	}
	// A missing seal breaks the walk: nothing recorded vouches for the step over it.
	if historyOf(t, r[0], r[2]).DescendsFrom(r[2], r[0].Snapshot) {
		t.Error("the walk stepped over a seal the history does not hold")
	}
	// An altered record breaks it too.
	bad := historyOf(t, r[0])
	forged := recordOf(r[1])
	forged.NodeRoot = snapX
	if _, err := bad.Observe(forged); err != nil {
		t.Fatal(err)
	}
	if bad.DescendsFrom(r[2], r[0].Snapshot) {
		t.Error("the walk trusted a record whose components do not bind to it")
	}
}

// 🔒 After a fork, the image descends from the seal it was put back to, and NOT from the seals the
// rollback left out — though the history holds them.
func TestDescendsDoesNotCrossAFork(t *testing.T) {
	r, f := forkedSeals(t)
	h := historyOf(t, append(slices.Clone(r), f)...)
	if !h.DescendsFrom(f, r[0].Snapshot) {
		t.Error("the post-restore seal does not descend from the seal it was restored to")
	}
	for _, skipped := range r[1:] {
		if h.DescendsFrom(f, skipped.Snapshot) {
			t.Errorf("the post-restore seal claims to descend from %s, which the rollback left out", skipped.Snapshot[:8])
		}
	}
}

// A checkpoint digest kept outside the case is checked as the seal it names (item 42), once the case
// is shown to still hold what the checkpoint committed to.
func TestAKeptCheckpointDigestIsCheckedAsItsSeal(t *testing.T) {
	in := sealedInput()
	in.Kept, in.KeptVia = snapA, &KeptCheckpoint{Seq: 3}
	l := layerOf(t, EventReport(in).Layers, consts.CustodyEventLayerKept)
	if l.State != consts.CustodyEstablished || len(l.Notes) == 0 || !strings.Contains(l.Notes[0], "checkpoint 3") {
		t.Fatalf("%+v", l)
	}

	in.KeptVia = &KeptCheckpoint{Seq: 3, Off: "the audit entries were changed"}
	r := EventReport(in)
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerKept); l.State != consts.CustodyBroken || !strings.Contains(l.Message, "audit entries were changed") {
		t.Fatalf("off: %+v", l)
	}
	if slices.Contains(r.Establishes, consts.CustodyEstablishesKept) {
		t.Fatal("a checkpoint the case no longer holds established the kept layer")
	}

	in.Kept, in.KeptVia = strings.Repeat("cd", 32), &KeptCheckpoint{Seq: 1, NoSeal: true}
	r = EventReport(in)
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerKept); l.State != consts.CustodyNotEstablished || r.Form != consts.CustodyFormKept {
		t.Fatalf("no seal: %+v %s", l, r.Form)
	}
}
