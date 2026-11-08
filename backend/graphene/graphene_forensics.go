package graphene

// Forensic integrity: what this case can establish about its own contents (§14).
//
// 🔒 This file exists here, in the only package that opens the database, because the machinery
// hangs off the engine's disk store and is reached through the graph — the same reason every
// other persistence need routes through this package. Policy and report assembly live in
// backend/custody; this is the accessor and the rohy-shaped types.
//
// WHAT THIS MACHINERY DOES, stated once so the layers above can quote it rather than reinvent
// it (graphene/SECURITY.md is the authority):
//
//	It can make modification of stored evidence DETECTABLE by a party holding an
//	independently retained reference value. It cannot PREVENT modification by code running
//	in this process, and it cannot attest to the TRUTH of what it was told.
//
// Two consequences shape every type below.
//
// First, 🔒 an unavailable mechanism is reported as unavailable, never as clean. Forensics()
// is false on the in-memory backend, which is what nearly the whole test suite uses, and a
// zero-valued "no problems found" would be a clean bill of health from a check that could not
// run. That is the same discipline caseintegrity already carries for a detector that failed.
//
// Second, 🔒 "absent" is a third outcome, not a shade of pass or fail. A pre-v8 image carries
// no digest; a case never compacted has no roots; a store with no signer has unsigned commits.
// None of those is damage, and none of them is fine either. Every type here keeps the three
// apart, because collapsing them is how somebody comes to trust an answer that was never
// computed.

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"

	"rohy/backend/consts"
	"rohy/backend/eventproof"
	"rohy/backend/payload"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/merkle"
	"github.com/aoiflux/graphene/store"
)

// ErrForensicsUnavailable is returned by every forensic read when the backend has no such
// machinery. It is a distinct error rather than a zero value so a caller cannot mistake
// "cannot answer" for "nothing to report".
var ErrForensicsUnavailable = errors.New(msgForensicsUnavailable)

const (
	msgForensicsUnavailable = "this case has no forensic machinery: the in-memory backend keeps " +
		"no image, so there is nothing to hash, sign, or prove"
	msgNeverCompacted = "this case has never been compacted, so nothing in it can be proven yet"
	msgBadRootHex     = "not a fingerprint: %q is not one hash in hex"
	msgBadBodyVersion = "not a fingerprint: body version %d is out of range"
)

// DigestState is the outcome of checking the compacted image against its own stored digest.
//
// FOUR values, and the last two are the ones that matter. Both mean "nothing was verified", and
// they are kept apart because they are different situations with the same non-answer:
//
//	DigestNoImage  nothing has been compacted, so there is no image at all. Nothing in this
//	               case can be proven YET. The remedy is to compact.
//	DigestAbsent   an image exists but predates digests and was never covered. The remedy is
//	               also to compact — which rewrites it — but the case is not new, and telling
//	               an analyst with a long-running case that it has "never been compacted" would
//	               be false.
//
// Collapsing either into a pass or a failure is the failure mode this whole file exists to
// avoid, and collapsing them into each other reports the wrong history.
type DigestState string

const (
	// DigestMatch: the stored digest describes the bytes on disk.
	DigestMatch DigestState = "match"
	// DigestMismatch: it does not. The file changed since it was written — by damage or by
	// edit, and the digest cannot tell which.
	DigestMismatch DigestState = "mismatch"
	// DigestAbsent: an image exists and predates digests, so it was never covered. Not a failure.
	DigestAbsent DigestState = "absent"
	// DigestNoImage: no compacted image exists. Not a failure either — just nothing yet.
	DigestNoImage DigestState = "no_image"
)

// SnapshotRoots is the Merkle root set over the compacted image, hex-encoded so it crosses the
// binding boundary as something a person can copy, compare and file.
//
// Snapshot is the one worth keeping. It binds the component roots AND the predecessor, so a
// substituted image breaks the chain even when the substitute is internally consistent — an
// isolated root proves only that a file is coherent, not that it belongs in this case's history.
type SnapshotRoots struct {
	// Snapshot is the value to retain OUTSIDE this case. Everything else here is detail.
	Snapshot string `json:"snapshot"`
	// Prev chains to the image this one replaced; empty for the first.
	Prev string `json:"prev,omitempty"`

	NodeRoot  string `json:"node_root"`
	EdgeRoot  string `json:"edge_root"`
	IndexRoot string `json:"index_root"`
	// TombstoneRoot covers this image's record of deliberate removals. It is committed to as
	// zero when nothing has been redacted, so "this image recorded no removals" is a claim the
	// root makes rather than the absence of one — which is why an empty string here means
	// "this image is too old to carry the claim", not "nothing was redacted".
	TombstoneRoot string `json:"tombstone_root,omitempty"`

	BodyVersion int `json:"body_version"`
}

// ImageCheck is one on-demand verification of the compacted image.
//
// 🔒 It is on demand by construction. Both underlying calls are package-level, take a path, and
// open no store, so this is a button in the deep integrity check rather than a tax on every
// launch — the same judgement VerifyIndexes carries, and upstream's own stated reason for
// offering the call this way.
type ImageCheck struct {
	// Digest is whether the bytes are unchanged since they were written.
	Digest DigestState `json:"digest"`
	// Computed is the digest derived from the bytes now. Reported even on a match, because it
	// is the value an analyst compares against one they retained elsewhere — and that
	// comparison, not this field, is what turns damage detection into tamper detection.
	Computed string `json:"computed,omitempty"`

	// RootsChecked reports whether the Merkle roots were re-derived, and RootsError what went
	// wrong if they were and it did. Roots are checked only when a digest exists to check them
	// against; on an uncovered image the answer is "not checked", not "passed".
	RootsChecked bool   `json:"roots_checked"`
	RootsError   string `json:"roots_error,omitempty"`
}

// OK reports whether the image verified. 🔒 An uncovered image is NOT ok — it is unproven, and
// this method must never be the thing that decides how the UI phrases it. Read Digest for that.
func (c ImageCheck) OK() bool {
	return c.Digest == DigestMatch && c.RootsChecked && c.RootsError == ""
}

// ForensicStatus is the posture of this case: what it can establish, and what it cannot.
//
// Every field is a statement about configuration or history, not a fault. An existing case is
// unsigned and unattested because it was written before any of this existed, which is not
// something the analyst did wrong — 🔒 and the copy above this must not read as an accusation.
type ForensicStatus struct {
	// Available is false on the in-memory backend. When it is false every other field is
	// meaningless and Reason says why.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`

	// Compacted reports whether any image exists. Until the first compaction, roots are absent
	// and NOTHING in the case can be proven — every event is live but unaccounted for, which is
	// a different thing from absent.
	Compacted bool           `json:"compacted"`
	Roots     *SnapshotRoots `json:"roots,omitempty"`
	// RootsError is set when the roots could not be read. Kept apart from Compacted on purpose:
	// "never sealed" is normal and has a remedy, while an unreadable fingerprint is a fault, and
	// reporting it as the first would hide it behind a button that writes a fresh image.
	RootsError string `json:"roots_error,omitempty"`

	// Signed reports whether commits made by THIS session carry a signature, and RequireSigned
	// whether an unsigned commit in the log would be refused.
	//
	// 🔒 RequireSigned is not implied by signing, and the gap matters: without it, stripping a
	// signature turns a signed commit into an ordinary unsigned one and verification is simply
	// skipped. Reported separately so a half-configured posture cannot present as a whole one.
	Signed        bool `json:"signed"`
	RequireSigned bool `json:"require_signed"`

	// VerifyOnOpen reports whether the image is re-derived at every open. Independent of
	// ImageCheck, which is the on-demand form.
	VerifyOnOpen bool `json:"verify_on_open"`

	// Audit and Retention are off by default upstream, and both defaults lose something: without
	// retention, every pre-compaction actor, timestamp, signature and key rotation is discarded
	// at each compaction.
	Audit     bool `json:"audit"`
	Retention bool `json:"retention"`

	// Redaction reports whether attributed removal is possible at all. Without it RedactNode
	// refuses rather than degrading to the unattributed DeleteNode.
	Redaction bool `json:"redaction"`

	// Mode is how this session opened the case (consts.CaseMode*), Actor who it records, and
	// ReadOnly why nothing can be written, when that is so.
	Mode     string `json:"mode"`
	Actor    uint64 `json:"actor,omitempty"`
	ReadOnly string `json:"read_only,omitempty"`
	// RetentionReason says why retention is off.
	RetentionReason string `json:"retention_reason,omitempty"`
}

// forensicOptions is the posture rohy opens a case under. Zero value is the permissive default —
// the historical behaviour, and 🔒 the honest default: no key means no attribution, which is
// reported rather than faked.
type forensicOptions struct {
	verifyOnOpen bool
	// posture is the session's: its mode, actor, key slot and verifier (graphene_signing.go). 🔒 The
	// key itself lives outside the case (backend/keyfile); this holds only the slot it is put in.
	posture Posture
}

// WithVerifyOnOpen asks for the image to be re-derived at every open, and returns the receiver so
// it can be chained at construction. It must be called BEFORE first use; the posture is read once,
// when the store opens.
//
// 🟡 Not the default, and not defaulted on without a measurement. Upstream measures roughly 5× on
// a 10k-node open; rohy's scale test is 100k events, in a project whose PERFORMANCE.md exists
// because a textbook optimisation made cold start 10× worse. The on-demand VerifyImage covers the
// same ground as an explicit action at no startup cost, which is why this is the secondary route
// and not the primary one.
func (s *Store) WithVerifyOnOpen(on bool) *Store {
	s.forensic.verifyOnOpen = on
	return s
}

// forensics returns the engine's forensic store, or ErrForensicsUnavailable.
//
// 🔒 The error is the point. Returning (nil, false) would leave every caller to invent its own
// way of saying "could not look", and one of them would eventually say "nothing found".
func (s *Store) forensics() (*disk.Store, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	fs, ok := g.Forensics()
	if !ok {
		return nil, ErrForensicsUnavailable
	}
	return fs, nil
}

// ForensicsAvailable reports whether this case has the machinery at all, without opening
// anything that is not already open. Cheap enough for a view to call on mount.
func (s *Store) ForensicsAvailable() bool {
	_, err := s.forensics()
	return err == nil
}

// SnapshotRoots returns the Merkle roots over the current compacted image.
//
// A case that has never been compacted returns nil with no error: there is no image, which is a
// state to report rather than a failure to raise. Callers distinguish it by the nil.
func (s *Store) SnapshotRoots() (*SnapshotRoots, error) {
	fs, err := s.forensics()
	if err != nil {
		return nil, err
	}
	roots, err := fs.SnapshotRoots()
	// 🔒 "Never compacted" is a STATE, not a failure, so it is translated into the nil return
	// rather than passed up as an error. A caller that had to string-match an error to find out
	// whether a case is simply new is a caller that will eventually get it wrong — and the wrong
	// answer here is "this case is broken" about a case that is fine.
	if errors.Is(err, disk.ErrNoSnapshotRoots) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if roots.Zero() {
		return nil, nil
	}
	return &SnapshotRoots{
		Snapshot:      hashHex(roots.Snapshot),
		Prev:          hashHex(roots.PrevRoot),
		NodeRoot:      hashHex(roots.NodeRoot),
		EdgeRoot:      hashHex(roots.EdgeRoot),
		IndexRoot:     hashHex(roots.IndexRoot),
		TombstoneRoot: hashHex(roots.TombstoneRoot),
		BodyVersion:   int(roots.BodyVersion),
	}, nil
}

// Unsealed reports whether this case holds anything a seal would cover: no image yet, or any write
// since the last compaction.
//
// It exists because a compaction ALWAYS produces a new fingerprint. The new image names the old
// one as its predecessor and that link is bound into the root, so even a compaction that changed
// nothing moves the value an analyst exported. The automatic seals (after an ingest, on close) ask
// this first so a fingerprint stays put until the case actually changes (plan §15.5).
//
// 🔒 It errs towards sealing. Any record in the write-ahead log, or any record held in the delta
// layer, counts as a change — including writes that turn out to be no-ops — because the cost of a
// needless seal is a stale export, and the cost of a missed one is evidence no fingerprint covers.
// For the same reason, an error other than ErrForensicsUnavailable comes back with true: a caller
// that cannot tell should seal.
//
// The log's size alone cannot answer it. A compaction leaves the log holding its container header
// and nothing else, so an "empty" log is not zero bytes; asking whether it holds any RECORD is the
// question, and InspectWAL answers it without depending on how big that header is.
func (s *Store) Unsealed() (bool, error) {
	fs, err := s.forensics()
	if errors.Is(err, ErrForensicsUnavailable) {
		return false, err
	}
	if err != nil {
		return true, err
	}
	roots, err := fs.SnapshotRoots()
	if errors.Is(err, disk.ErrNoSnapshotRoots) || (err == nil && roots.Zero()) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	st := fs.StorageStats()
	if st.DeltaNodes+st.DeltaEdges+st.DeletedNodes+st.DeletedEdges > 0 {
		return true, nil
	}
	if st.WALBytes == 0 {
		return false, nil
	}
	wal, err := disk.InspectWAL(s.dir)
	if err != nil {
		return true, err
	}
	// A torn tail is not nothing: it is the shape a crash leaves, and sealing is what retires it.
	return len(wal.Records) > 0 || wal.Truncated, nil
}

// VerifyImage re-derives the compacted image's digest and Merkle roots.
//
// This is the expensive, honest check, and it is proportional to the file. It opens no store and
// needs no key: both underlying calls read the image directly.
//
// 🔒 It must not open the store, because the image it checks is the one an open would load. The
// image carries the property index, so a damaged image is refused at open, and a check reached
// through the open could never report the damage it exists to find.
//
// 🔒 What a match establishes, and the UI must say no more than this: the file has not changed
// since it was written. It does NOT establish who wrote it — anyone who can rewrite the body can
// rewrite the digest. It becomes tamper detection only for someone comparing Computed against a
// digest they retained independently.
func (s *Store) VerifyImage() (ImageCheck, error) {
	if s.inMemory {
		return ImageCheck{}, ErrForensicsUnavailable
	}

	status, computed, err := disk.VerifyCSRDigest(s.dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// No image file: nobody has compacted this case yet. Reported as that, not as damage.
		return ImageCheck{Digest: DigestNoImage}, nil
	case err != nil:
		return ImageCheck{}, err
	}
	out := ImageCheck{Computed: hex.EncodeToString(computed[:])}

	switch status {
	case disk.DigestMatch:
		out.Digest = DigestMatch
	case disk.DigestMismatch:
		out.Digest = DigestMismatch
	default:
		// Uncovered image: there is no digest to have matched, so there is nothing to check the
		// roots against either. Reported as absent, and the roots as unchecked — not as a pass.
		out.Digest = DigestAbsent
		out.Computed = ""
		return out, nil
	}

	out.RootsChecked = true
	if err := disk.VerifyCSRRoots(s.dir); err != nil {
		out.RootsError = err.Error()
	}
	return out, nil
}

// ForensicStatus reports the case's posture. Cheap: it reads configuration and, at most, the
// roots already computed by the last compaction.
func (s *Store) ForensicStatus() ForensicStatus {
	if _, err := s.forensics(); err != nil {
		return ForensicStatus{Available: false, Reason: err.Error()}
	}

	p := s.posture()
	opts := p.diskOptions(s.forensic.verifyOnOpen)
	st := ForensicStatus{
		Available:    true,
		VerifyOnOpen: s.forensic.verifyOnOpen,
		// Signed is what THIS session's commits and seals carry: a signed case whose key is still
		// locked signs nothing, and says so.
		Signed:          p.Mode == consts.CaseModeSigned && p.Slot.Unlocked(),
		RequireSigned:   opts.RequireSignedCommits,
		Audit:           opts.Audit,
		Retention:       false,
		RetentionReason: consts.MsgRetentionOff,
		// Always on for a disk case (diskOptions, item 40).
		Redaction: true,
		Mode:      p.Mode,
		Actor:     p.Actor,
	}
	if err := s.Writable(); err != nil {
		st.ReadOnly = err.Error()
	}

	// A roots read that fails is not allowed to present as "never compacted": the two are
	// different, and only one of them is normal.
	roots, err := s.SnapshotRoots()
	if err != nil {
		st.RootsError = err.Error()
		return st
	}
	if roots == nil {
		st.Reason = msgNeverCompacted
		return st
	}
	st.Compacted, st.Roots = true, roots
	return st
}

// hashHex renders a merkle hash, mapping the zero hash to the empty string.
//
// The distinction is load-bearing: sixty-four zeros looks like a value and is not one. An empty
// string is how every caller above tells "this component was not committed to" from "this
// component hashed to something".
func hashHex(h merkle.Hash) string {
	var zero merkle.Hash
	if h == zero {
		return ""
	}
	return hex.EncodeToString(h[:])
}

// parseHashHex is hashHex's inverse: the empty string is the zero hash, anything else must be
// exactly one hash in hex.
func parseHashHex(s string) (merkle.Hash, error) {
	var h merkle.Hash
	if s == "" {
		return h, nil
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(h) {
		return h, fmt.Errorf(msgBadRootHex, s)
	}
	copy(h[:], b)
	return h, nil
}

// diskRoots converts the rohy-shaped roots back into the engine's, rejecting anything that does
// not decode rather than letting it become a zero hash.
func diskRoots(r SnapshotRoots) (disk.SnapshotRoots, error) {
	var out disk.SnapshotRoots
	if r.BodyVersion < 0 || r.BodyVersion > math.MaxUint8 {
		return out, fmt.Errorf(msgBadBodyVersion, r.BodyVersion)
	}
	out.BodyVersion = uint8(r.BodyVersion)
	for _, f := range []struct {
		dst *merkle.Hash
		src string
	}{
		{&out.Snapshot, r.Snapshot}, {&out.PrevRoot, r.Prev},
		{&out.NodeRoot, r.NodeRoot}, {&out.EdgeRoot, r.EdgeRoot},
		{&out.IndexRoot, r.IndexRoot}, {&out.TombstoneRoot, r.TombstoneRoot},
	} {
		h, err := parseHashHex(f.src)
		if err != nil {
			return out, err
		}
		*f.dst = h
	}
	return out, nil
}

// VerifyChain checks that later directly follows earlier: that later names earlier as the image it
// replaced, and that later's components bind to later's fingerprint (disk.VerifyChain).
//
// Pure and storeless — it checks two recorded values against each other, which is what lets the
// fingerprint history be checked without the images it describes, all but one of which are gone.
//
// 🔒 What a link establishes is narrow, and the history's report says so: that these two records
// are consistent with one seal following the other. Whoever can rewrite the case folder can write
// a consistent history too, so a chain checked here is the case vouching for itself — the same
// limit as every internal check. It detects a substituted or rolled-back image; it does not
// replace a fingerprint kept elsewhere.
func VerifyChain(earlier, later SnapshotRoots) error {
	e, err := diskRoots(earlier)
	if err != nil {
		return err
	}
	l, err := diskRoots(later)
	if err != nil {
		return err
	}
	return disk.VerifyChain(e, l)
}

// EventProof is one event's proof document, or the state that explains why there is none.
type EventProof struct {
	// State is consts.EventProofOK or the reason this event cannot be proven right now.
	State string
	// Snapshot is the root the proof was built against — the fingerprint a recipient must hold to
	// check it. Set only when State is OK.
	Snapshot string
	// Document is the exact bytes to hand over. eventproof.Assemble verified these bytes against
	// Snapshot before they were returned.
	Document []byte
	// RawBound reports whether the seal covers the raw record: the stored record carries its hash,
	// and the proof got far enough to bind it. Set on ok, and on too_large, which is refused only
	// after both bindings succeeded. The custody report's raw layer reads this.
	RawBound bool
	// RawHashed reports whether the stored record carries a raw-record hash at all, so a record
	// with a hash and no raw record is not described as one with no hash. Set with RawBound.
	RawHashed bool
}

// ProveEvent builds the proof document for one event (plan §14.3).
//
// The document carries graphene's inclusion proof, the event's stored record exactly as the case
// holds it, and its raw record from the payload store — what a recipient needs to check the event's
// CONTENT, not merely that some node of that id was sealed.
//
// 🔒 Every reason an event cannot be proven is a state, not an error, because each has a different
// remedy and none of them is damage except raw_mismatch, raw_unreadable and image_damaged:
//
//	unavailable         the in-memory backend keeps no image
//	not_sealed          the case was never sealed
//	written_since_seal  the event arrived after the last seal
//	changed_since_seal  its stored record changed after the last seal (a dedup count, a backfill)
//	image_outdated      the seal predates leaves that hash the stored record
//	raw_mismatch        the raw record no longer hashes to the value its stored record holds
//	raw_unreadable      the raw record cannot be read from the payload store at all
//	image_damaged       the sealed image does not check out: the proof does not resolve to its root
//	raw_redacted        a redaction removed the raw record and has not finished (not damage)
//	redacted            a recorded redaction removed the event's content (not damage)
//	record_empty        the event holds no record and no redaction is recorded (damage)
//	too_large           the document would exceed what any verifier reads
//
// The live record is read before the proof is built, and Assemble compares the two by hash, so a
// seal landing in between cannot produce a wrong document: either the record is unchanged and the
// proof is good against the seal it came from, or it changed and that is the state reported.
func (s *Store) ProveEvent(id uint64) (EventProof, error) {
	fs, err := s.forensics()
	if errors.Is(err, ErrForensicsUnavailable) {
		return EventProof{State: consts.EventProofUnavailable}, nil
	}
	if err != nil {
		return EventProof{}, err
	}
	g, err := s.graph()
	if err != nil {
		return EventProof{}, err
	}
	n, err := g.GetNode(store.NodeID(id))
	if err != nil {
		return EventProof{}, err
	}
	// 🔒 By label: eventFromNode decodes any JSON it is given, so a proof built over some other node
	// would carry its record labelled as an event's.
	if !slices.Contains(n.Labels, consts.NodeEvent) {
		return EventProof{}, fmt.Errorf(consts.MsgNotAnEvent, id)
	}
	// A redacted event keeps no record: nothing is left to prove, and the ledger record is what the
	// case holds. Checked before anything decodes the empty record. Without a ledger record behind
	// it, an empty record is damage — only a redaction produces one.
	if isStrippedEvent(n) {
		if _, ok, err := s.RedactionFor(id); err != nil {
			return EventProof{}, err
		} else if ok {
			return EventProof{State: consts.EventProofRedacted}, nil
		}
		return EventProof{State: consts.EventProofRecordEmpty}, nil
	}
	e, err := eventFromNode(n)
	if err != nil {
		return EventProof{}, err
	}
	// A raw record that is not there to read is damage, reported as a state. Anything else — the
	// case shutting down, an I/O error that may pass — stays an error, because calling it damage
	// would send the analyst looking for a fault that is not in the case.
	if err := s.hydrate(e); rawUnreadable(err) {
		return EventProof{State: consts.EventProofRawUnreadable}, nil
	} else if err != nil {
		return EventProof{}, err
	}
	// The raw record is gone but the stored record is not: a redaction that has not finished. A
	// proof would bind a raw record that no longer exists, and a seal would not finish it.
	if e.PayloadRedacted() {
		return EventProof{State: consts.EventProofRawRedacted}, nil
	}
	hashed := e.HashRaw != ""
	// The raw record travels only when the stored record carries its hash. Without one the seal
	// never bound it, so nothing could vouch for it: the proof then covers the stored fields alone,
	// and says so, rather than carrying a record a recipient might mistake for proven.
	var raw []byte
	if e.RawXML != "" && e.HashRaw != "" {
		raw = []byte(e.RawXML)
	}

	proof, err := fs.ExportNodeProof(n.ID)
	switch {
	case errors.Is(err, disk.ErrNoSnapshotRoots):
		return EventProof{State: consts.EventProofNotSealed}, nil
	case errors.Is(err, disk.ErrNotInSnapshot):
		return EventProof{State: consts.EventProofWrittenSinceSeal}, nil
	case err != nil:
		return EventProof{}, err
	}

	doc, err := eventproof.Assemble(proof, n.Properties, raw)
	switch {
	case errors.Is(err, eventproof.ErrRecordMismatch):
		return EventProof{State: consts.EventProofChangedSinceSeal}, nil
	case errors.Is(err, eventproof.ErrImageOutdated):
		return EventProof{State: consts.EventProofImageOutdated}, nil
	case errors.Is(err, eventproof.ErrRawMismatch):
		return EventProof{State: consts.EventProofRawMismatch}, nil
	case errors.Is(err, eventproof.ErrTooLarge):
		return EventProof{State: consts.EventProofTooLarge, RawBound: raw != nil, RawHashed: hashed}, nil
	// Assemble's self-check: the proof does not resolve to the root of the image it came from.
	case errors.Is(err, eventproof.ErrNotVerified):
		return EventProof{State: consts.EventProofImageDamaged}, nil
	case err != nil:
		return EventProof{}, err
	}
	// Assemble has decoded and verified this proof; decoding it again only reads the root out.
	exported, err := disk.UnmarshalProof(proof)
	if err != nil {
		return EventProof{}, err
	}
	return EventProof{
		State:     consts.EventProofOK,
		Snapshot:  hashHex(exported.Node.Roots.Snapshot),
		Document:  doc,
		RawBound:  raw != nil,
		RawHashed: hashed,
	}, nil
}

// rawUnreadable reports whether a payload read failed because the raw record is not there to read:
// the log is shorter than the reference, the record does not decode, or a tombstone stands where
// it should be that is not this event's clean removal. A clean tombstone is not damage (hydrate).
func rawUnreadable(err error) bool {
	var short *payload.OutOfRangeError
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	var dirty *payload.TombstoneDirtyError
	var other *payload.TombstoneOwnerError
	return errors.As(err, &short) || errors.As(err, &syntax) || errors.As(err, &shape) ||
		errors.As(err, &dirty) || errors.As(err, &other)
}
