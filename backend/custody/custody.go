// Package custody owns the case-fingerprint history: which snapshot roots this case has had, and
// which of them the analyst has taken a copy of (plan §14.2).
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
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"rohy/backend/consts"
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
}

// Store is the fingerprint-history sidecar.
type Store struct {
	dir string
	mu  sync.Mutex
	doc document
}

// Open loads (or initializes) the history rooted at dir.
//
// A missing or corrupt document is treated as an empty history rather than a failure. That is the
// right trade here for the same reason it is in the capture store — a damaged sidecar must not stop
// a case from opening — but the consequence is worth naming: a lost history means chaining starts
// again from the current image, and any record of what was exported is gone. It costs knowledge,
// never evidence.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, doc: document{Version: consts.RootsDocVersion}}
	data, err := os.ReadFile(s.file())
	if err != nil {
		return s, nil
	}
	var d document
	if json.Unmarshal(data, &d) == nil && d.Version == consts.RootsDocVersion {
		s.doc = d
	}
	return s, nil
}

func (s *Store) file() string {
	return filepath.Join(s.dir, consts.RootsStateFile)
}

// Observe records a fingerprint if it is new, and returns the record for it.
//
// Idempotent by snapshot value: seeing the same fingerprint again changes nothing, which matters
// because this is called whenever the posture is read and the posture is read on view mount.
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
	sort.SliceStable(out, func(i, j int) bool { return out[i].SealedAt.Before(out[j].SealedAt) })
	return out
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

// persist atomically writes the document (temp file + rename), so an interrupted write cannot leave
// a truncated history behind. Callers hold s.mu.
func (s *Store) persist() error {
	s.doc.Version = consts.RootsDocVersion
	data, err := json.MarshalIndent(s.doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}
