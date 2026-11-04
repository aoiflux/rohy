package eventproof

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"rohy/backend/consts"
	"rohy/backend/utils"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/merkle"
)

// The two ways a file can fail, kept apart as graphene keeps them: not a readable proof at all, and
// a readable proof that does not verify. Every error Verify returns wraps exactly one of them.
var (
	ErrMalformed   = errors.New(consts.MsgEventProofMalformed)
	ErrNotVerified = errors.New(consts.MsgEventProofNotVerified)

	errNotEvent  = errors.New(consts.MsgEventProofNotEvent)
	errNoHashRaw = errors.New(consts.MsgEventProofNoHashRaw)
)

// Subject is what the proven stored record says, decoded for a person to read. Only ever built
// from a record the proof has already been bound to.
type Subject struct {
	Node             uint64 `json:"node"`
	EventID          string `json:"event_id"`
	Timestamp        string `json:"timestamp"`
	Provider         string `json:"provider"`
	Channel          string `json:"channel"`
	Computer         string `json:"computer"`
	User             string `json:"user"`
	SourceType       string `json:"source_type"`
	SourceIdentifier string `json:"source_identifier"`
}

// Report is a successful verification.
type Report struct {
	Subject Subject
	// Root is the root the proof was checked against — the caller's, in hex.
	Root string
	// RawCarried reports whether the raw record travelled and was checked.
	RawCarried bool
	// The verifier's own statement of what this establishes. Never the file's copy.
	Establishes      []string
	DoesNotEstablish []string
}

// Verify checks an event proof document against a snapshot root the caller obtained
// independently, and returns what it proves.
//
// The checks run in order and stop at the first failure:
//
//  1. the document parses, strictly (known fields only, each once, nothing after it)
//  2. graphene's proof decodes and is an inclusion proof
//  3. graphene verifies it against root
//  4. the proven leaf is an event's, and commits to exactly the stored record in the file
//  5. the stored record is an event record
//  6. the raw record, when present, hashes to the stored record's hash_raw
func Verify(root merkle.Hash, data []byte) (Report, error) {
	b, err := decodeBundle(data)
	if err != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	exported, err := disk.UnmarshalProof(b.Proof)
	if err != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if exported.Kind != disk.ProofKindNodeInclusion || exported.Node == nil {
		return Report{}, fmt.Errorf("%w: "+consts.MsgEventProofKind, ErrMalformed, exported.Kind)
	}

	// 🔒 graphene first, against the caller's root. Nothing else in the file is worth reading until
	// the proof resolves to a root the reader trusts; after that, every byte checked below is one
	// the root covers.
	if err := disk.VerifyExportedProof(root, exported); err != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrNotVerified, err)
	}
	subject, err := bindContent(*exported.Node, b)
	if err != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrNotVerified, err)
	}

	rawCarried := len(b.RawRecord) > 0
	est, not := claims(rawCarried)
	return Report{
		Subject:          subject,
		Root:             hex.EncodeToString(root[:]),
		RawCarried:       rawCarried,
		Establishes:      est,
		DoesNotEstablish: not,
	}, nil
}

// bindContent ties a proven node to the stored record and raw record a bundle carries. It assumes
// nothing about the proof beyond what graphene's verifier established, and re-reads the leaf for
// the rest.
func bindContent(node disk.NodeInclusionProof, b Bundle) (Subject, error) {
	if b.Event != uint64(node.NodeID) {
		return Subject{}, fmt.Errorf(consts.MsgEventProofEventLabel, b.Event, node.NodeID)
	}
	leaf, err := parseNodeLeaf(node.LeafData)
	if err != nil {
		return Subject{}, err
	}
	if node.Roots.BodyVersion != snapshotBodyVersion {
		return Subject{}, fmt.Errorf("%w: it states %d", errBodyVersion, node.Roots.BodyVersion)
	}
	// graphene checks this too; restated so the function does not lean on a check made elsewhere.
	if leaf.id != uint64(node.NodeID) {
		return Subject{}, errLeafShape
	}
	if !leaf.hasLabel(consts.NodeEvent) {
		return Subject{}, errNotEvent
	}
	if propertiesHash(b.Properties) != leaf.propertiesHash {
		return Subject{}, ErrRecordMismatch
	}

	var rec map[string]json.RawMessage
	if err := json.Unmarshal(b.Properties, &rec); err != nil {
		return Subject{}, fmt.Errorf(consts.MsgEventProofRecordShape, err)
	}
	if rec == nil {
		return Subject{}, fmt.Errorf(consts.MsgEventProofRecordShape, "null")
	}
	if len(b.RawRecord) > 0 {
		hashRaw := field(rec, consts.PropHashRaw)
		if hashRaw == "" {
			return Subject{}, errNoHashRaw
		}
		if utils.HashBytes(b.RawRecord) != hashRaw {
			return Subject{}, ErrRawMismatch
		}
	}
	return Subject{
		Node:             uint64(node.NodeID),
		EventID:          field(rec, consts.PropEventID),
		Timestamp:        field(rec, consts.PropTimestamp),
		Provider:         field(rec, consts.PropProvider),
		Channel:          field(rec, consts.PropChannel),
		Computer:         field(rec, consts.PropComputer),
		User:             field(rec, consts.PropUser),
		SourceType:       field(rec, consts.PropSourceType),
		SourceIdentifier: field(rec, consts.PropSourceIdentifier),
	}, nil
}

// field reads one string field of a stored record; anything absent or not a string reads as "".
func field(rec map[string]json.RawMessage, key string) string {
	var s string
	if err := json.Unmarshal(rec[key], &s); err != nil {
		return ""
	}
	return s
}

// decodeBundle parses a proof document strictly.
//
// 🔒 Stricter than encoding/json on purpose. The standard decoder lets a repeated key's last value
// win and matches names case-insensitively, so one file could show a person reading it one raw
// record while the verifier checks another. In a file whose whole job is to be read by people who
// do not run the verifier, that ambiguity is the attack. So every top-level key must be one of
// Bundle's exact names, and appear once.
func decodeBundle(data []byte) (Bundle, error) {
	if len(data) > consts.EventProofMaxBytes {
		return Bundle{}, fmt.Errorf(consts.MsgEventProofTooLarge, consts.EventProofMaxBytes)
	}
	if err := checkKeys(data); err != nil {
		return Bundle{}, err
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return Bundle{}, err
	}
	if b.Format != consts.EventProofFormat {
		return Bundle{}, fmt.Errorf(consts.MsgEventProofFormat, b.Format, consts.EventProofFormat)
	}
	if b.FormatVersion != consts.EventProofFormatVersion {
		return Bundle{}, fmt.Errorf(consts.MsgEventProofVersion, b.FormatVersion, consts.EventProofFormatVersion)
	}
	return b, nil
}

// checkKeys walks the top-level object: known keys only, each once, and nothing after it.
func checkKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New(consts.MsgEventProofNotObject)
	}
	seen := make(map[string]bool, len(bundleKeys))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string) // inside an object the decoder only yields string keys
		if !bundleKeys[key] {
			return fmt.Errorf(consts.MsgEventProofUnknownKey, key)
		}
		if seen[key] {
			return fmt.Errorf(consts.MsgEventProofDuplicate, key)
		}
		seen[key] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New(consts.MsgEventProofTrailing)
	}
	return nil
}
