package custody

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

const (
	sealA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sealB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sealC = "cccccccccccccccccccccccccccccccc"
)

func signedIn(seal *graphene.SealSignature, last string) SigningInput {
	return SigningInput{
		State: consts.SigningSigned, Seal: seal, LastRecorded: last,
		SignerName: "Ada", SignerFingerprint: "f1f2",
	}
}

func TestTheSigningLayer(t *testing.T) {
	good := &graphene.SealSignature{ID: sealB, Prev: sealA, Verified: true}
	for _, tc := range []struct {
		name  string
		in    SigningInput
		state string
		says  string
	}{
		{"unsigned case", SigningInput{State: consts.SigningUnsigned}, consts.CustodyNotEstablished, "does not sign"},
		{"requested", SigningInput{State: consts.SigningRequested}, consts.CustodyNotEstablished, "next time"},
		{"register unreadable", SigningInput{RegisterErr: "boom"}, consts.CustodyNotEstablished, "boom"},
		{"signed, as recorded", signedIn(good, sealB), consts.CustodyEstablished, "Ada's key f1f2"},
		{"signed, no signature", signedIn(nil, sealB), consts.CustodyBroken, "without the key"},
		{"signed, bad signature", signedIn(&graphene.SealSignature{ID: sealB, VerifyError: "forged"}, sealB), consts.CustodyBroken, "forged"},
		{"one unrecorded seal", signedIn(good, sealA), consts.CustodyNotEstablished, "did not record"},
		{"a seal outside the record", signedIn(&graphene.SealSignature{ID: sealC, Verified: true}, sealA), consts.CustodyBroken, "outside a signed session"},
		{"stopped", SigningInput{State: consts.SigningUnsignedAfterSigned, StoppedAt: time.Now(), StoppedBy: "Ada", StoppedReason: "key lost"}, consts.CustodyNotEstablished, "key lost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := signingLayer(tc.in, graphene.EventCustody{})
			if l.State != tc.state || !strings.Contains(l.Message, tc.says) {
				t.Fatalf("%s: %q", l.State, l.Message)
			}
		})
	}
}

// 🔒 A removal recorded while the case signed must carry a signature that checks out.
func TestARedactionInASignedIntervalMustBeSigned(t *testing.T) {
	in := signedIn(&graphene.SealSignature{ID: sealB, Verified: true}, sealB)
	in.RedactionInSigned = true
	for _, tc := range []struct {
		red   graphene.CustodyRedaction
		state string
	}{
		{graphene.CustodyRedaction{Signed: true, SignatureValid: true}, consts.CustodyEstablished},
		{graphene.CustodyRedaction{Signed: true}, consts.CustodyBroken},
		{graphene.CustodyRedaction{}, consts.CustodyBroken},
	} {
		red := tc.red
		l := signingLayer(in, graphene.EventCustody{Redacted: &red})
		if l.State != tc.state {
			t.Errorf("%+v: %s %v", red, l.State, l.Notes)
		}
	}
	// Outside a signed interval, an unsigned removal is what every removal before v0.3.0 is.
	in.RedactionInSigned = false
	if l := signingLayer(in, graphene.EventCustody{Redacted: &graphene.CustodyRedaction{}}); l.State != consts.CustodyEstablished {
		t.Errorf("an unsigned removal from before signing broke the layer: %s", l.State)
	}
}

func TestASignedSealIsListedAmongWhatTheReportEstablishes(t *testing.T) {
	in := ReportInput{Event: 1, Engine: graphene.EventCustody{Live: true, InSnapshot: true, HasFingerprint: true}}
	s := signedIn(&graphene.SealSignature{ID: sealB, Verified: true}, sealB)
	in.Signing = &s
	r := EventReport(in)
	found := false
	for _, e := range r.Establishes {
		found = found || e == consts.CustodyEstablishesSigned
	}
	if !found || stateOf(r.Layers, consts.CustodyEventLayerSigning) != consts.CustodyEstablished {
		t.Fatalf("establishes %v", r.Establishes)
	}
}

// The integrity report's seal verdict is the signing layer's, and an event's redaction never leaks
// into it.
func TestTheSealVerdictIsTheSigningLayersWithoutAnEvent(t *testing.T) {
	good := &graphene.SealSignature{ID: sealB, Prev: sealA, Verified: true}
	in := signedIn(good, sealB)
	in.RedactionInSigned = true
	if state, msg := SealVerdict(in); state != consts.CustodyEstablished || !strings.Contains(msg, "Ada's key f1f2") {
		t.Errorf("%s: %q", state, msg)
	}
	if state, _ := SealVerdict(signedIn(nil, sealB)); state != consts.CustodyBroken {
		t.Errorf("a signed case with no seal signature is %s", state)
	}
}
