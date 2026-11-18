// Package custody owns the case-fingerprint history: which snapshot roots this case has had, and
// which of them the analyst has taken a copy of.
//
// # Why a sidecar at all, when the roots are already in the image
//
// Two things it can do that reading the current image cannot.
//
// **Chaining.** A snapshot root binds its predecessor, so a substituted image breaks the link even
// when the substitute is internally consistent. Checking that needs the earlier root, and after a
// compaction the earlier image is gone. Keeping the history is what makes "this image belongs in
// this case's history" a question rohy can ask at all.
//
// **Knowing whether a copy was taken.** Which is the honest framing, and worth being pedantic
// about, because the obvious framing is wrong:
//
//	🔒 rohy CANNOT know whether a fingerprint is retained externally. It knows only whether
//	somebody pressed Export. Whether the copy still exists, whether it went somewhere beyond
//	the reach of whoever can rewrite the case, whether it was transcribed correctly — none of
//	that is observable from in here, and a flag named `retained` would quietly claim all three.
//
// So the field is `exported`, the message says a copy was taken rather than that one is held, and
// even the satisfied state stays informational. That is not hedging: the value of an external copy
// comes precisely from being outside what this process can see, so a process that could confirm it
// would be confirming something worth less.
//
// # What this package deliberately does not do
//
// 🔒 It never opens the database and never holds a key. It composes what backend/graphene exposes
// and records what happened. Same construction as caseintegrity, and testable against literals for
// the same reason.
package custody

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"rohy/backend/consts"
	"rohy/backend/durable"
)

// ErrNoRoot is returned when an export is asked for and the case has no fingerprint — it has never
// been compacted. A distinct error because the remedy is specific and the state is normal for a new
// case.
var ErrNoRoot = errors.New(consts.MsgCustodyNoRoot)

// Record is one fingerprint this case has had.
//
// It carries the context that makes the bare hash meaningful later: how big the case was, which
// build produced it, and when. An analyst comparing a copy from six weeks ago against the case now
// needs to know which of several roots they are holding.
type Record struct {
	// Snapshot is the fingerprint — the value worth keeping outside the case.
	Snapshot string `json:"snapshot"`
	// Prev is the fingerprint of the image this one replaced, empty for the first. This is the
	// chain.
	Prev string `json:"prev,omitempty"`

	NodeRoot      string `json:"node_root,omitempty"`
	EdgeRoot      string `json:"edge_root,omitempty"`
	IndexRoot     string `json:"index_root,omitempty"`
	TombstoneRoot string `json:"tombstone_root,omitempty"`
	BodyVersion   int    `json:"body_version,omitempty"`

	// SealedAt is when rohy first observed this fingerprint, which is not necessarily when the
	// compaction happened — an image can be observed on a later launch. Named for what it is.
	SealedAt time.Time `json:"sealed_at"`
	// StoreVersion and Events describe the case at that point, so a fingerprint can be placed.
	StoreVersion uint64 `json:"store_version,omitempty"`
	Events       int    `json:"events,omitempty"`
	AppVersion   string `json:"app_version,omitempty"`

	// Who sealed, and whether the seal is signed. ActorID is the session's analyst, 0 for
	// a seal made before analysts were recorded. Attestation is the id of the signature the seal
	// carries, and KeyID the key that made it; both are empty for an unsigned seal. Recorded by rohy
	// when the seal ran, so a later image that lost its signature disagrees with its own record.
	ActorID     uint64 `json:"actor_id,omitempty"`
	KeyID       uint64 `json:"key_id,omitempty"`
	Attestation string `json:"attestation,omitempty"`

	// Exported records that a copy was taken out of the app, and when.
	//
	// 🔒 It does NOT record that a copy is held anywhere. See the package comment: rohy cannot
	// observe that, and a field implying otherwise would be the one false claim in a feature
	// whose entire purpose is not making false claims.
	Exported   bool       `json:"exported,omitempty"`
	ExportedAt *time.Time `json:"exported_at,omitempty"`
}

// document is the on-disk shape.
type document struct {
	Version int      `json:"roots_version"`
	Records []Record `json:"records"`
	// Kept lists earlier history documents this one replaced because they could not be used, and
	// where each was kept. Inside the document so every later open still says the history
	// started again, not only the open that found it. Older builds ignore the field.
	Kept []Damage `json:"kept,omitempty"`
}

// Store is the fingerprint-history sidecar.
type Store struct {
	dir    string
	mu     sync.Mutex
	doc    document
	damage *Damage
	// aside is set while an unusable document waits to be moved aside (OpenDeferred, Settle).
	aside bool
}

// holdAside records an unusable document and holds it in place until Settle.
func (s *Store) holdAside(reason string) {
	s.damage, s.aside = &Damage{Reason: reason}, true
}

// Settle moves aside the unusable document an OpenDeferred held, if there is one, and starts the
// new history with a note of it.
func (s *Store) Settle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.aside {
		return
	}
	s.aside = false
	s.keepAside(s.damage.Reason)
}

// Damage describes a history document that could not be used when the case opened.
type Damage struct {
	// Reason says what was wrong with it.
	Reason string `json:"reason"`
	// KeptAt is where the document now is. Empty when it was left in place — it could not be read,
	// or could not be moved — and then the history refuses every write this session rather than
	// replace it.
	KeptAt string `json:"kept_at,omitempty"`
}

// Open loads (or initializes) the history rooted at dir.
//
// A document that cannot be used — corrupt, unreadable, or written by a newer build — does not stop
// the case from opening, for the same reason a damaged capture store does not. The history starts
// again from the next seal.
//
// 🔒 But the document is KEPT. It is moved aside under a name that says why, and Damage reports
// where. It used to be treated as empty and then replaced by the next seal, which destroyed the
// record of which fingerprints had been exported — the one thing in it rohy cannot reconstruct, and
// a newer build's history erased by opening the case in an older one. If it cannot even be moved,
// the history holds it in place and refuses to write rather than write over it.
//
// 🔒 A document that could not be READ is never moved, only held. A read can fail for reasons that
// pass — another process holding the file, a scanner — and moving a valid history aside on one
// would lose it as surely as overwriting it. Only a document that was read and is not usable
// (corrupt, or a newer format) is moved.
func Open(dir string) (*Store, error) {
	s, err := OpenDeferred(dir)
	if err != nil {
		return nil, err
	}
	s.Settle()
	return s, nil
}

// OpenDeferred is Open without the move: an unusable document is reported by Damage and HELD in
// place — every write refused — until Settle moves it aside. A session that cannot write
// never settles, so it leaves the case folder exactly as it found it.
func OpenDeferred(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, doc: document{Version: consts.RootsDocVersion}}
	data, err := os.ReadFile(s.file())
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		s.damage = &Damage{Reason: fmt.Sprintf(consts.MsgCustodyHistoryReadFail, err)}
		return s, nil
	}
	var d document
	if err := json.Unmarshal(data, &d); err != nil {
		s.holdAside(fmt.Sprintf(consts.MsgCustodyHistoryCorrupt, err))
		return s, nil
	}
	// Every older format reads as this one: version 2 only added who sealed and how, and
	// the next write records the document at the current version. A NEWER format is kept aside — an
	// older build does the same with this one, which is what bumping the version buys: an older build
	// never re-writes a history and silently drops the fields it does not know.
	if d.Version < 1 || d.Version > consts.RootsDocVersion {
		s.holdAside(fmt.Sprintf(consts.MsgCustodyHistoryNewer, d.Version, consts.RootsDocVersion))
		return s, nil
	}
	s.doc = d
	return s, nil
}

// Damage reports the history document this open could not use, or nil. It describes this open
// only: once a fresh history has been written, the next open finds nothing wrong.
func (s *Store) Damage() *Damage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.damage == nil {
		return nil
	}
	d := *s.damage
	return &d
}

// Kept lists the earlier history documents this history replaced, and where each was kept.
func (s *Store) Kept() []Damage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.doc.Kept)
}

// keepAside moves the unusable document to a name no earlier kept document holds, and starts the
// new history with a note of it. Called from Settle, under mu.
func (s *Store) keepAside(reason string) {
	s.damage = &Damage{Reason: reason}
	base := s.file() + consts.RootsKeptInfix + time.Now().UTC().Format(consts.RootsKeptStamp)
	for i := range 1000 {
		kept := base
		if i > 0 {
			kept = fmt.Sprintf("%s-%d", base, i)
		}
		// 🔒 Never onto an existing name: Go's os.Rename replaces its target on every platform
		// (on Windows too), which would destroy an earlier kept document to save this one.
		if _, err := os.Lstat(kept); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if os.Rename(s.file(), kept) != nil {
			return
		}
		s.damage.KeptAt = kept
		s.doc.Kept = append(s.doc.Kept, *s.damage)
		// Written now, not at the next seal, so the note survives a session that seals nothing.
		// If it fails the note is still held here and the next write carries it.
		_ = s.persist()
		return
	}
}

func (s *Store) file() string {
	return filepath.Join(s.dir, consts.RootsStateFile)
}

// Observe records a fingerprint if it is new, and returns the record for it.
//
// Idempotent by snapshot value: seeing the same fingerprint again changes nothing, which matters
// because it is called at every seal and every export, and both can meet a fingerprint already held.
//
// 🔒 An existing record is never overwritten. Re-observing must not clear the `exported` flag, or
// simply opening the Maintenance page would silently retract the fact that a copy was taken.
func (s *Store) Observe(r Record) (Record, error) {
	if r.Snapshot == "" {
		return Record{}, ErrNoRoot
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if i := s.indexOf(r.Snapshot); i >= 0 {
		return s.doc.Records[i], nil
	}
	if r.SealedAt.IsZero() {
		r.SealedAt = time.Now().UTC()
	}
	s.doc.Records = append(s.doc.Records, r)
	if err := s.persist(); err != nil {
		return r, err
	}
	return r, nil
}

// MarkExported records that a copy of a fingerprint was taken out of the app.
//
// The fingerprint must already be known: marking an unobserved value would let the flag be set for
// a root this case never had.
func (s *Store) MarkExported(snapshot string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.indexOf(snapshot)
	if i < 0 {
		return ErrNoRoot
	}
	if s.doc.Records[i].Exported {
		return nil
	}
	now := time.Now().UTC()
	s.doc.Records[i].Exported = true
	s.doc.Records[i].ExportedAt = &now
	return s.persist()
}

// Get returns the record for a fingerprint, and whether it is known.
func (s *Store) Get(snapshot string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.indexOf(snapshot); i >= 0 {
		return s.doc.Records[i], true
	}
	return Record{}, false
}

// Exported reports whether a copy of this fingerprint has been taken.
//
// 🔒 An UNKNOWN fingerprint reports false, which is the safe direction: it produces the caveat
// rather than suppressing it. Defaulting the other way would mean a history lost to a corrupt
// sidecar silently reads as "a copy was taken".
func (s *Store) Exported(snapshot string) bool {
	_, ok := s.ExportedAt(snapshot)
	return ok
}

// ExportedAt returns when a copy of this fingerprint was taken, and whether one was.
//
// This is the form the integrity check consumes, so that package can depend on a one-method
// interface instead of on this one's types.
func (s *Store) ExportedAt(snapshot string) (time.Time, bool) {
	r, ok := s.Get(snapshot)
	if !ok || !r.Exported || r.ExportedAt == nil {
		return time.Time{}, false
	}
	return *r.ExportedAt, true
}

// History returns every known fingerprint, oldest first.
func (s *Store) History() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, len(s.doc.Records))
	copy(out, s.doc.Records)
	slices.SortStableFunc(out, func(a, b Record) int { return a.SealedAt.Compare(b.SealedAt) })
	return out
}

// SealCounts counts the seals the history records, and how many of them carry no signature (item
// 43). A seal recorded by a build before signing carries none, which is what it was.
func (s *Store) SealCounts() (seals, unsigned int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.doc.Records {
		seals++
		if r.Attestation == "" {
			unsigned++
		}
	}
	return seals, unsigned
}

// indexOf locates a fingerprint. Callers hold s.mu.
func (s *Store) indexOf(snapshot string) int {
	for i := range s.doc.Records {
		if s.doc.Records[i].Snapshot == snapshot {
			return i
		}
	}
	return -1
}

// persist durably writes the document, so an interrupted write cannot leave a truncated history
// behind. Callers hold s.mu.
func (s *Store) persist() error {
	if s.damage != nil && s.damage.KeptAt == "" {
		return fmt.Errorf(consts.MsgCustodyHistoryHeld, s.damage.Reason)
	}
	s.doc.Version = consts.RootsDocVersion
	data, err := json.MarshalIndent(s.doc, "", "  ")
	if err != nil {
		return err
	}
	return durable.WriteFile(s.file(), data)
}
