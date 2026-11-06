package custody

// Checking a checkpoint digest the reader holds (plan §14.7, item 42).
//
// 🔒 The one check rohy runs that does not rest on the case: the digest came from outside it. The
// digest pins every head the checkpoint recorded, and each head pins the entries before it, so a
// history that still holds them is the history that was published. What happened after the
// checkpoint, and when or by whom the digest was published, it cannot say.
//
// Pure: it decides and words, over values the API gathers under the seal lock.

import (
	"fmt"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// AnchorInput is everything the check decides on.
type AnchorInput struct {
	// Digest is the value the reader holds, normalized.
	Digest string
	// Unavailable: the backend keeps no image, so nothing else was gathered.
	Unavailable bool
	// KnownSeal: the digest is not a checkpoint's but one of the case's seal fingerprints.
	KnownSeal bool
	// Found is graphene's consts.Checkpoint* for the digest, FoundDetail its reason, and K the
	// checkpoint when one carries the digest.
	Found       string
	FoundDetail string
	K           graphene.Checkpoint
	// Held is each history checked against K, Now where they stand now, and Unsealed how many log
	// records no seal covers yet.
	Held     []graphene.LayerHeld
	Now      graphene.Heads
	Unsealed int
	// Current is the image now, nil when there is none. Chain, NoHistory and Ancestor place it
	// against K's seal: Ancestor is DescendsFrom(Current, K.Snapshot).
	Current   *graphene.SnapshotRoots
	Chain     ChainReport
	NoHistory bool
	Ancestor  bool
	// Receipts are the references the case records for K. The case's own word.
	Receipts []string
}

// AnchorCheck is the answer.
type AnchorCheck struct {
	Digest string `json:"digest"`
	// State is one of consts.AnchorCheck*.
	State   string `json:"state"`
	Message string `json:"message"`
	Seq     uint64 `json:"seq,omitempty"`
	// CapturedAt is the case's clock when K was made.
	CapturedAt *time.Time `json:"captured_at,omitempty"`
	Layers     []Layer    `json:"layers"`
	Notes      []string   `json:"notes,omitempty"`

	Broken         int `json:"broken"`
	NotEstablished int `json:"not_established"`

	Establishes      []string `json:"establishes"`
	DoesNotEstablish []string `json:"does_not_establish"`
}

var anchorNouns = map[string]string{
	consts.AnchorLayerAudit:    consts.AnchorNounAudit,
	consts.AnchorLayerLedger:   consts.AnchorNounLedger,
	consts.AnchorLayerGrants:   consts.AnchorNounGrants,
	consts.AnchorLayerSegments: consts.AnchorNounSegments,
}

// AnchorReport checks the case against a checkpoint digest the reader holds.
func AnchorReport(in AnchorInput) AnchorCheck {
	r := AnchorCheck{Digest: in.Digest, Layers: []Layer{}, Establishes: []string{},
		DoesNotEstablish: []string{consts.AnchorNotWhen, consts.AnchorNotAfter, consts.AnchorNotTrue}}
	switch {
	case in.Unavailable:
		r.State, r.Message = consts.AnchorCheckUnavailable, consts.MsgAnchorUnavailable
		return r
	case in.Found == consts.CheckpointNotFound && in.KnownSeal:
		r.State, r.Message = consts.AnchorCheckSealFingerprint, consts.MsgAnchorSealFingerprint
		return r
	case in.Found == consts.CheckpointNotFound:
		r.State, r.Message, r.Broken = consts.AnchorCheckNotFound, consts.MsgAnchorNotFound, 1
		return r
	case in.Found == consts.CheckpointUnread:
		r.State, r.Message = consts.AnchorCheckUnread, fmt.Sprintf(consts.MsgAnchorChainUnread, in.FoundDetail)
		r.NotEstablished = 1
		return r
	case in.Found == consts.CheckpointChainBroken:
		r.State, r.Seq, r.Broken = consts.AnchorCheckBroken, in.K.Seq, 1
		r.Message = fmt.Sprintf(consts.MsgAnchorChainBroken, in.K.Seq, in.FoundDetail)
		return r
	case in.Found != consts.CheckpointFound:
		r.State, r.Message = consts.AnchorCheckUnread, fmt.Sprintf(consts.MsgCustodyUnknownState, in.Found)
		r.NotEstablished = 1
		return r
	}

	k := in.K
	captured := k.CapturedAt
	r.Seq, r.CapturedAt = k.Seq, &captured
	image, descends := anchorImageLayer(in)
	r.Layers = append(r.Layers, image)
	held := true
	for _, h := range in.Held {
		l := anchorHistoryLayer(k.Seq, h)
		if l.State != consts.CustodyEstablished {
			held = false
		}
		// A history neither side ever started says nothing worth a line.
		if h.State == consts.HeldOK && h.At == 0 && h.Now == 0 &&
			(h.Layer == consts.AnchorLayerGrants || h.Layer == consts.AnchorLayerSegments) {
			continue
		}
		r.Layers = append(r.Layers, l)
	}
	r.Layers = append(r.Layers, anchorSinceLayer(in))
	if len(in.Receipts) > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf(consts.MsgAnchorReceipts, strings.Join(in.Receipts, "; ")))
	} else {
		r.Notes = append(r.Notes, consts.MsgAnchorNoReceipt)
	}
	for _, l := range r.Layers {
		switch l.State {
		case consts.CustodyBroken:
			r.Broken++
		case consts.CustodyNotEstablished:
			r.NotEstablished++
		}
	}

	when := stamp(k.CapturedAt)
	switch {
	case r.Broken > 0:
		r.State, r.Message = consts.AnchorCheckBroken, fmt.Sprintf(consts.MsgAnchorBroken, k.Seq, when)
	case !held:
		r.State, r.Message = consts.AnchorCheckIncomplete, fmt.Sprintf(consts.MsgAnchorIncomplete, k.Seq, when)
	case image.State == consts.CustodyEstablished:
		r.State, r.Message = consts.AnchorCheckMatches, fmt.Sprintf(consts.MsgAnchorMatches, k.Seq, when)
	case descends:
		r.State, r.Message = consts.AnchorCheckDescends, fmt.Sprintf(consts.MsgAnchorDescends, k.Seq, when)
	default:
		r.State, r.Message = consts.AnchorCheckUnplaced, fmt.Sprintf(consts.MsgAnchorUnplaced, k.Seq, when)
	}
	if r.State == consts.AnchorCheckMatches || r.State == consts.AnchorCheckDescends {
		r.Establishes = append(r.Establishes, fmt.Sprintf(consts.AnchorEstablishes, k.Seq, k.AuditCount, k.RedactionCount))
	}
	return r
}

// anchorImageLayer places the case's image against the seal K names, and says whether the image
// descends from it.
func anchorImageLayer(in AnchorInput) (Layer, bool) {
	k := in.K
	l := Layer{Layer: consts.AnchorLayerImage, State: consts.CustodyNotEstablished}
	switch {
	case k.Snapshot == "":
		l.Message = fmt.Sprintf(consts.MsgAnchorImageNoSeal, k.Seq)
	case in.Current == nil:
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgAnchorImageNone, k.Seq)
	case in.Current.Snapshot == k.Snapshot && k.Attestation != in.Now.Attestation:
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgAnchorImageResigned, k.Seq)
	case in.Current.Snapshot == k.Snapshot:
		l.State, l.Message = consts.CustodyEstablished, fmt.Sprintf(consts.MsgAnchorImageSame, k.Seq)
	case in.Chain.State == consts.ChainRolledBack || in.Chain.Forks > 0:
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgAnchorImageRolledBack, k.Seq)
	case in.Ancestor:
		l.Message = fmt.Sprintf(consts.MsgAnchorImageDescends, k.Seq)
		return l, true
	default:
		// 🔒 Not broken: a seal whose record failed, or a history kept aside, also stops the trace. Only
		// the history itself showing a rollback or a fork says the image went somewhere else.
		l.Message = fmt.Sprintf(consts.MsgAnchorImageUnplaced, k.Seq)
		switch {
		case in.NoHistory:
			l.Notes = append(l.Notes, consts.MsgCustodyHistoryNone)
		case in.Chain.Held != nil:
			l.Notes = append(l.Notes, fmt.Sprintf(consts.MsgCustodyChainHeld, in.Chain.Held.Reason))
		case in.Chain.Gaps > 0:
			l.Notes = append(l.Notes, fmt.Sprintf(consts.MsgCustodyHistoryGaps, in.Chain.Gaps))
		}
	}
	return l, false
}

// anchorHistoryLayer words one history checked against checkpoint seq.
func anchorHistoryLayer(seq uint64, h graphene.LayerHeld) Layer {
	noun := anchorNouns[h.Layer]
	if noun == "" {
		noun = h.Layer
	}
	l := Layer{Layer: h.Layer}
	switch h.State {
	case consts.HeldOK:
		l.State = consts.CustodyEstablished
		if h.At == 0 {
			l.Message = fmt.Sprintf(consts.MsgAnchorHeldNone, seq, noun)
		} else {
			l.Message = fmt.Sprintf(consts.MsgAnchorHeld, h.At, noun, seq)
		}
		if h.Now > h.At {
			l.Notes = append(l.Notes, fmt.Sprintf(consts.MsgAnchorHeldMore, h.Now-h.At))
		}
	case consts.HeldChanged:
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgAnchorChanged, noun, seq, h.Detail)
	case consts.HeldShrank:
		l.State, l.Message = consts.CustodyBroken, fmt.Sprintf(consts.MsgAnchorShrank, h.Now, noun, seq, h.At)
	case consts.HeldUnread:
		l.State, l.Message = consts.CustodyNotEstablished, fmt.Sprintf(consts.MsgAnchorUnreadable, noun, h.Detail)
	default:
		l.State, l.Message = consts.CustodyNotEstablished, fmt.Sprintf(consts.MsgCustodyUnknownState, h.State)
	}
	return l
}

// anchorSinceLayer says what the case recorded after K: never established unless nothing was.
func anchorSinceLayer(in AnchorInput) Layer {
	k, now := in.K, in.Now
	var parts []string
	if now.Snapshot != k.Snapshot || now.Attestation != k.Attestation {
		parts = append(parts, consts.MsgAnchorSinceSealed)
	}
	for _, c := range []struct {
		noun     string
		at, held uint64
	}{
		{consts.AnchorNounAudit, k.AuditCount, now.AuditCount},
		{consts.AnchorNounLedger, k.RedactionCount, now.RedactionCount},
		{consts.AnchorNounGrants, k.GrantCount, now.GrantCount},
		{consts.AnchorNounSegments, k.SegmentCount, now.SegmentCount},
	} {
		if c.held > c.at {
			parts = append(parts, fmt.Sprintf(consts.MsgAnchorSinceCount, c.held-c.at, c.noun))
		}
	}
	if in.Unsealed > 0 {
		parts = append(parts, fmt.Sprintf(consts.MsgAnchorSinceUnsealed, in.Unsealed))
	}
	l := Layer{Layer: consts.AnchorLayerSince}
	if len(parts) == 0 {
		l.State, l.Message = consts.CustodyEstablished, fmt.Sprintf(consts.MsgAnchorSinceNothing, k.Seq)
		return l
	}
	l.State, l.Message = consts.CustodyNotEstablished, fmt.Sprintf(consts.MsgAnchorSince, k.Seq, strings.Join(parts, ", "))
	return l
}
