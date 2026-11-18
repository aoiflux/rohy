package custody

import (
	"fmt"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// SigningInput is what the case records about its own signing, for the report's signing
// layer. Gathered by the API from the analyst register; decided here.
type SigningInput struct {
	// State is the register's signing state (consts.Signing*). Empty when the register cannot be
	// read, and RegisterErr then says why.
	State       string
	RegisterErr string
	// Seal is the signature on the current image, checked under the case's bound keys. Nil when it
	// carries none.
	Seal *graphene.SealSignature
	// LastRecorded is the id of the last signed seal the register recorded.
	LastRecorded string
	// SignerName and SignerFingerprint name the analyst and key behind Seal, when the case lists it.
	SignerName        string
	SignerFingerprint string
	// For a case that stopped signing: when, by whom, and why.
	StoppedAt     time.Time
	StoppedBy     string
	StoppedReason string
	// RedactionInSigned is set when the event's removal was recorded while the case signed its work,
	// so its ledger record must carry a valid signature.
	RedactionInSigned bool
}

// signingLayer checks what graphene does not require: that a case which signs its seals still
// carries an unbroken signed record.
//
// 🔒 What "established" says is narrow: the current seal checks out under a key THIS CASE lists.
// Whoever can rewrite the case can rewrite the list too, which is why the copy sends the reader to a
// fingerprint they kept.
func signingLayer(in SigningInput, e graphene.EventCustody) Layer {
	l := Layer{Layer: consts.CustodyEventLayerSigning, State: consts.CustodyNotEstablished}
	switch in.State {
	case "":
		l.Message = fmt.Sprintf(consts.MsgCustodySigningUnknown, in.RegisterErr)
	case consts.SigningUnsigned:
		l.Message = consts.MsgCustodySigningOff
	case consts.SigningRequested:
		l.Message = consts.MsgCustodySigningRequested
	case consts.SigningUnsignedAfterSigned:
		l.Message = fmt.Sprintf(consts.MsgCustodySigningStopped, stamp(in.StoppedAt), in.StoppedBy, in.StoppedReason)
	case consts.SigningSigned:
		l = signedSeal(in, l)
	default:
		l.Message = fmt.Sprintf(consts.MsgCustodySigningUnknown, in.State)
	}

	if r := e.Redacted; r != nil && in.RedactionInSigned {
		if r.Signed && r.SignatureValid {
			l.Notes = append(l.Notes, consts.MsgCustodySigningRedactOK)
		} else {
			l.State = consts.CustodyBroken
			l.Notes = append(l.Notes, consts.MsgCustodySigningRedaction)
		}
	}
	return l
}

// SealVerdict is the signing layer's verdict on the case's current seal alone, for the integrity
// report: the same rules an event's report applies, so the two never disagree. State is
// consts.CustodyEstablished, CustodyNotEstablished or CustodyBroken.
func SealVerdict(in SigningInput) (state, message string) {
	in.RedactionInSigned = false
	l := signingLayer(in, graphene.EventCustody{})
	return l.State, l.Message
}

// signedSeal judges the current seal of a case that signs.
func signedSeal(in SigningInput, l Layer) Layer {
	s := in.Seal
	switch {
	case s == nil:
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodySigningMissing
	case !s.Verified:
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgCustodySigningBad, s.VerifyError)
	case s.ID == in.LastRecorded:
		l.State = consts.CustodyEstablished
		if in.SignerName != "" {
			l.Message = fmt.Sprintf(consts.MsgCustodySigningVerified, in.SignerName, in.SignerFingerprint)
		} else {
			l.Message = fmt.Sprintf(consts.MsgCustodySigningUnnamed, in.SignerFingerprint)
		}
	case in.LastRecorded != "" && s.Prev == in.LastRecorded:
		// One signed seal the register did not record: a seal whose record failed. Signed, chained,
		// and not what the case says it last sealed.
		l.Message = consts.MsgCustodySigningUnrecorded
	default:
		l.State, l.Message = consts.CustodyBroken, consts.MsgCustodySigningGap
	}
	return l
}
