package custody

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// The custody report for a redacted event (plan §14.5, item 40).

var redactedAt = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

// redactedInput is an event whose content a recorded redaction removed, before or after the seal
// that takes the content out of the image.
func redactedInput(sealed bool) ReportInput {
	in := sealedInput()
	in.Engine.ProofState = consts.EventProofRedacted
	in.Engine.RawBound, in.Engine.RawHashed = false, false
	in.Engine.Redacted = &graphene.CustodyRedaction{
		Scope: consts.RedactionScopeProperties, At: redactedAt, Reason: "erasure request",
	}
	in.Engine.RemovalProvable = sealed
	return in
}

func TestARedactedEventIsPlacedAsRedacted(t *testing.T) {
	for _, sealed := range []bool{false, true} {
		r := EventReport(redactedInput(sealed))
		if r.Presence != consts.CustodyPresenceRedactedContent {
			t.Fatalf("sealed %v: presence %s", sealed, r.Presence)
		}
		if !strings.Contains(r.PresenceMessage, "erasure request") || !strings.Contains(r.PresenceMessage, "2026-11-04") {
			t.Errorf("presence does not say when and why: %q", r.PresenceMessage)
		}
		for _, name := range []string{consts.CustodyEventLayerRecord, consts.CustodyEventLayerRaw} {
			if l := layerOf(t, r.Layers, name); l.State != consts.CustodyNotEstablished || l.Remedy != "" {
				t.Errorf("sealed %v, %s: %+v", sealed, name, l)
			}
		}
		if contains(r.Establishes, consts.CustodyEstablishesRecord) || contains(r.Establishes, consts.CustodyEstablishesRaw) {
			t.Errorf("sealed %v: a redacted event claims its record: %v", sealed, r.Establishes)
		}
	}
}

// Until a seal, the image keeps what the redaction removed — and here a seal is the fix.
func TestTheSealIsTheRemedyUntilTheRemovalIsBoundIn(t *testing.T) {
	before := layerOf(t, EventReport(redactedInput(false)).Layers, consts.CustodyEventLayerSeal)
	if before.State != consts.CustodyNotEstablished || before.Remedy != consts.MsgCustodyRemedySealRedaction ||
		before.Message != consts.MsgCustodySealStillHolds {
		t.Errorf("before the seal: %+v", before)
	}
	after := layerOf(t, EventReport(redactedInput(true)).Layers, consts.CustodyEventLayerSeal)
	if after.State != consts.CustodyEstablished || after.Remedy != "" {
		t.Errorf("after the seal: %+v", after)
	}
	red := layerOf(t, EventReport(redactedInput(false)).Layers, consts.CustodyEventLayerRedaction)
	if !contains(red.Notes, consts.MsgCustodyRedactionUnbound) {
		t.Errorf("before the seal the redaction layer does not say it is unbound: %+v", red)
	}
}

func TestARedactionWithNoActorSaysSo(t *testing.T) {
	red := layerOf(t, EventReport(redactedInput(true)).Layers, consts.CustodyEventLayerRedaction)
	if !contains(red.Notes, consts.MsgCustodyRedactionNoActor) {
		t.Errorf("notes %v", red.Notes)
	}
	in := redactedInput(true)
	in.Engine.Redacted.ActorID = 9
	if red := layerOf(t, EventReport(in).Layers, consts.CustodyEventLayerRedaction); contains(red.Notes, consts.MsgCustodyRedactionNoActor) {
		t.Errorf("an attributed redaction says no actor: %v", red.Notes)
	}
}

// 🔒 The ledger says the content went; the event holds a record. Never read as an ordinary event.
func TestARecordedRemovalBesideARecordIsBroken(t *testing.T) {
	in := redactedInput(true)
	in.Engine.ProofState = consts.EventProofOK
	in.Engine.RawBound = true
	r := EventReport(in)
	if r.Presence == consts.CustodyPresenceRedactedContent {
		t.Fatal("an event holding a record is placed as redacted")
	}
	if red := layerOf(t, r.Layers, consts.CustodyEventLayerRedaction); red.State != consts.CustodyBroken ||
		!contains(red.Notes, consts.MsgCustodyRedactionUndone) {
		t.Errorf("redaction layer %+v", red)
	}
}

func TestARedactionThatDidNotFinishSaysSo(t *testing.T) {
	in := sealedInput()
	in.Engine.ProofState = consts.EventProofRawRedacted
	r := EventReport(in)
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerRaw); l.Message != consts.MsgCustodyRawRedactionPending ||
		l.State != consts.CustodyNotEstablished {
		t.Errorf("raw %+v", l)
	}
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerRecord); l.State != consts.CustodyNotEstablished {
		t.Errorf("record %+v", l)
	}
}

func TestAnEmptyRecordNobodyRedactedIsDamage(t *testing.T) {
	in := sealedInput()
	in.Engine.ProofState = consts.EventProofRecordEmpty
	r := EventReport(in)
	if l := layerOf(t, r.Layers, consts.CustodyEventLayerRecord); l.State != consts.CustodyBroken {
		t.Errorf("record %+v", l)
	}
	if r.Presence == consts.CustodyPresenceRedactedContent {
		t.Error("an empty record with no ledger record is placed as redacted")
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
