// Package eventproof makes and checks the file that proves one event was in a sealed case.
//
// The chain a verifier walks, root to raw record:
//
//	root you kept ─ graphene proof ─ leaf ─ SHA256(0x07 ‖ stored record) ─ hash_raw ─ raw record
//
// graphene covers the first three links and stops at a hash of the stored record. This package
// supplies the rest: the file carries the exact stored record and the raw record, and Verify checks
// that they are the ones the proof commits to. Without that, a verified proof says only that a node
// with SOME content was in the snapshot — true, and useless to someone asking about an event.
//
// 🔒 It never opens a case. It imports graphene's proof codec and verifier, which take bytes and a
// root and nothing else, so the recipient of evidence can check it on a machine that has never seen
// the case or the application. rohyctl's verify-proof is built on it for exactly that reason.
//
// 🔒 The root is always the caller's. The file names the snapshot it was built against so an
// analyst can pair it with the right fingerprint, and that value is never used to verify anything:
// checking a proof against a root its author supplied is circular.
package eventproof

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"rohy/backend/consts"
	"rohy/backend/version"

	"github.com/aoiflux/graphene/disk"
)

// Bundle is the event proof file.
//
// The byte fields are base64 (Go's []byte encoding), so each round-trips exactly: a stored record
// re-encoded on its way through would no longer hash to what the leaf holds.
type Bundle struct {
	Format        string `json:"format"`
	FormatVersion int    `json:"format_version"`
	AppVersion    string `json:"app_version,omitempty"`

	// Event is the node the proof is about. Checked against the proof, never trusted on its own.
	Event uint64 `json:"event"`
	// BuiltAgainst is the snapshot root the proof was built from, so the analyst can find the
	// matching fingerprint. 🔒 A pairing hint only: Verify takes the root from its caller.
	BuiltAgainst string `json:"built_against"`

	// Proof is graphene's exported inclusion proof (.gprf bytes).
	Proof []byte `json:"proof"`
	// Properties is the event's stored record, byte for byte as the case holds it.
	Properties []byte `json:"properties"`
	// RawRecord is the raw record from the payload store. Absent for an event that has none.
	RawRecord []byte `json:"raw_record,omitempty"`

	// For a reader without the verifier. The verifier prints its own: these are not covered by
	// the proof, and anyone can edit them.
	Establishes      []string `json:"establishes"`
	DoesNotEstablish []string `json:"does_not_establish"`
}

// bundleKeys are Bundle's JSON names, exactly. The parser accepts these and nothing else — see
// decodeBundle for why the standard decoder's leniency is not good enough here.
var bundleKeys = map[string]bool{
	"format": true, "format_version": true, "app_version": true, "event": true,
	"built_against": true, "proof": true, "properties": true, "raw_record": true,
	"establishes": true, "does_not_establish": true,
}

// Errors a caller maps to a state. Each also arrives wrapped in ErrNotVerified from Verify.
var (
	// ErrRecordMismatch: the stored record is not the one the proof commits to. From Assemble, it
	// means the event changed after the case was sealed.
	ErrRecordMismatch = errors.New(consts.MsgEventProofRecord)
	// ErrRawMismatch: the raw record does not hash to the value in the stored record.
	ErrRawMismatch = errors.New(consts.MsgEventProofRaw)
	// ErrTooLarge: the document would be larger than any verifier accepts (consts.EventProofMaxBytes).
	ErrTooLarge = fmt.Errorf(consts.MsgEventProofBuildTooLarge, consts.EventProofMaxBytes)
)

// Assemble builds an event proof document from graphene's exported proof, the event's stored
// record and its raw record (nil when it has none), and returns the exact bytes to hand over.
//
// It refuses to build a file that would not verify: ErrRecordMismatch, ErrRawMismatch,
// ErrImageOutdated and ErrTooLarge each name a state the caller reports instead. 🔒 The final step verifies the
// document's own bytes against the root they were built from. That is a check on this builder, not
// a claim of trust — the same root check by a recipient is circular, which is why they supply theirs.
func Assemble(proof, properties, raw []byte) ([]byte, error) {
	exported, err := disk.UnmarshalProof(proof)
	if err != nil {
		return nil, fmt.Errorf("eventproof: graphene's proof does not decode: %w", err)
	}
	if exported.Kind != disk.ProofKindNodeInclusion || exported.Node == nil {
		return nil, fmt.Errorf("eventproof: "+consts.MsgEventProofKind, exported.Kind)
	}
	node := *exported.Node
	if len(raw) == 0 {
		raw = nil
	}
	b := Bundle{
		Format:        consts.EventProofFormat,
		FormatVersion: consts.EventProofFormatVersion,
		AppVersion:    version.Current(consts.AppName).Version,
		Event:         uint64(node.NodeID),
		BuiltAgainst:  hex.EncodeToString(node.Roots.Snapshot[:]),
		Proof:         proof,
		Properties:    properties,
		RawRecord:     raw,
	}
	b.Establishes, b.DoesNotEstablish = claims(raw != nil)

	if _, err := bindContent(node, b); err != nil {
		return nil, err
	}

	doc, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("eventproof: encode: %w", err)
	}
	doc = append(doc, '\n')
	// Named before the self-check would refuse it, so the caller reports a size rather than a fault.
	if len(doc) > consts.EventProofMaxBytes {
		return nil, ErrTooLarge
	}
	if _, err := Verify(node.Roots.Snapshot, doc); err != nil {
		return nil, fmt.Errorf("eventproof: the assembled proof does not verify against the root "+
			"it was built from, so it was not handed out: %w", err)
	}
	return doc, nil
}

// claims is what a verified proof establishes and what it does not, given whether the raw record
// travels with it. One function for the file and the verifier, so the two cannot drift.
func claims(rawCarried bool) (establishes, doesNot []string) {
	establishes = []string{consts.EventProofEstablishesIncluded}
	if rawCarried {
		establishes = append(establishes, consts.EventProofEstablishesRaw)
	}
	doesNot = []string{
		consts.EventProofNotRoot,
		consts.EventProofNotWho,
		consts.EventProofNotTrue,
		consts.EventProofNotParsed,
		consts.EventProofNotOthers,
	}
	if !rawCarried {
		doesNot = append(doesNot, consts.EventProofNoRaw)
	}
	return establishes, doesNot
}
