// Package findings holds the analyst's own judgements about events: a flag, tags, and a
// note. Everything else in rohy is machine-derived — events come from EVTX, edges
// come from rules, layout comes from an algorithm — and this is the one layer that is
// authored by a person.
//
// That difference drives the design. Findings are persisted as a JSON sidecar beside the
// case (the graphreg precedent) rather than as properties on the event node, so an ingested
// record always reads back exactly as it was ingested: opinion sits beside the evidence,
// never inside it. The sidecar is plain readable JSON on purpose — an analyst's notes may
// outlive this program, and they should not need it to be read back.
//
// This package owns only annotation data. It never touches events or relations, and it does
// not know what an event is beyond its identity hash.
package findings

import (
	"cmp"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"rohy/backend/consts"
	"rohy/backend/durable"
	"rohy/backend/utils"
)

const findingsFile = "findings.json"

// ErrNoKey is returned when an operation omits the event identity hash. Without it a
// finding has nothing to attach to, and writing it anyway would create an orphan.
var ErrNoKey = errors.New("finding requires an event hash")

// ErrTooLong is returned when a note holds more than consts.MaxFindingNoteLen characters
// (runes; the editor counts the same unit). The write is refused rather than truncated:
// silently discarding the tail of an analyst's reasoning is worse than telling them it did
// not fit.
var ErrTooLong = errors.New("note exceeds the maximum length")

// Finding is one analyst annotation, keyed by the event's hash_normalized.
//
// Descriptor is a human-readable summary of the event as it was when annotated. It exists so
// a finding whose event is no longer in the case is still meaningful — an orphaned bare hash
// tells the reader nothing, whereas "4624 Microsoft-Windows-Security-Auditing 2026-07-14"
// still describes what was marked. It is deliberately a copy, not a live lookup: it records
// what the analyst was looking at when they wrote the note.
type Finding struct {
	Key        string    `json:"key"` // the event's hash_normalized
	Flagged    bool      `json:"flagged"`
	Tags       []string  `json:"tags"`
	Note       string    `json:"note"`
	Descriptor string    `json:"descriptor,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IsEmpty reports whether a finding carries no analyst content at all. An empty finding is
// deleted rather than stored, so clearing the last tag off an event leaves no residue that
// would make it look annotated in filters and counts.
func (f *Finding) IsEmpty() bool {
	return !f.Flagged && len(f.Tags) == 0 && strings.TrimSpace(f.Note) == ""
}

// state is the on-disk document. Findings are stored as a map so a lookup during list
// rendering is O(1); order is imposed at read time by the List accessors.
//
// HashVersion records the hash_normalized recipe the keys were written against
// (consts.FindingsHashVersion). Keys are only meaningful relative to that recipe, so the
// file states which one it used rather than leaving a future reader to guess.
type state struct {
	HashVersion int                 `json:"hash_version"`
	Findings    map[string]*Finding `json:"findings"`
}

// Store reads and writes the findings sidecar under a directory.
type Store struct {
	dir string
	mu  sync.Mutex
	s   state
	// stale is set when the sidecar was written against a different hash recipe than this
	// build uses. The findings are still loaded — they are irreplaceable and a reader can
	// still make sense of them via their descriptors — but nothing will match a live event,
	// and callers are told so instead of being shown an empty result they cannot explain.
	stale bool
}

// Open loads (or initializes) the findings sidecar rooted at dir, creating the directory if
// needed. A missing file is a new case, not an error.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	st := &Store{dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, findingsFile))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else if err := json.Unmarshal(data, &st.s); err != nil {
		return nil, err
	}
	if st.s.Findings == nil {
		st.s.Findings = map[string]*Finding{}
	}
	// A sidecar with no version is either brand new or predates versioning. An empty one is
	// simply adopted; a populated one is assumed to have been written by the build that
	// introduced versioning, since that is the only version that has ever shipped.
	if st.s.HashVersion == 0 {
		st.s.HashVersion = consts.FindingsHashVersion
	}
	st.stale = st.s.HashVersion != consts.FindingsHashVersion
	return st, nil
}

// HashVersion reports the recipe the stored keys were written against.
func (s *Store) HashVersion() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.s.HashVersion
}

// Stale reports whether the sidecar was written against a different hash recipe than this
// build produces, in which case no stored finding can match a live event.
func (s *Store) Stale() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stale
}

// persist durably writes the sidecar, indented so the file stays readable by hand.
func (s *Store) persist() error {
	data, err := json.MarshalIndent(&s.s, "", "  ")
	if err != nil {
		return err
	}
	return durable.WriteFile(filepath.Join(s.dir, findingsFile), data)
}

// ScrubDescriptors replaces the descriptor of each finding keyed by one of keys with replacement,
// and returns the keys it changed. A descriptor is a copy rohy made of an event's
// content, so a redaction removes it; the flag, tags and note are the analyst's own words and are
// left for the analyst.
//
// The write is durable: the redaction records this step as done, and a power cut must not bring
// the descriptor back behind that record. A temporary copy left by an interrupted earlier write
// holds the same text, and is removed.
func (s *Store) ScrubDescriptors(keys []string, replacement string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var changed []string
	next := make(map[string]*Finding, len(s.s.Findings))
	maps.Copy(next, s.s.Findings)
	for _, k := range keys {
		f, ok := next[k]
		if !ok || f.Descriptor == replacement {
			continue
		}
		c := clone(f)
		c.Descriptor = replacement
		next[k] = c
		changed = append(changed, k)
	}
	if len(changed) > 0 {
		doc := s.s
		doc.Findings = next
		data, err := json.MarshalIndent(&doc, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := durable.WriteFile(filepath.Join(s.dir, findingsFile), data); err != nil {
			return nil, err
		}
		s.s = doc
	}
	if err := durable.RemoveTemps(filepath.Join(s.dir, findingsFile)); err != nil {
		return changed, err
	}
	slices.Sort(changed)
	return changed, nil
}

// clone returns a defensive copy, including the tag slice, so callers cannot mutate stored
// state through the value they were handed.
func clone(f *Finding) *Finding {
	c := *f
	c.Tags = append([]string(nil), f.Tags...)
	return &c
}

// normalizeTags trims, lowercases, drops empties, removes duplicates, and sorts. Tags are a
// grouping mechanism, so "Lateral Movement" and "lateral movement" must be the same tag —
// otherwise the tag list fragments into near-identical entries and stops grouping anything.
// Over-long tags are cut to the cap here, between characters, rather than rejected: unlike a
// note, a tag is a label, and a clipped label still labels. The cut is trimmed again, because
// a cut can end on a space, and a tag ending in one would match no filter: the filter bar and
// KeysWithTag both trim what they look up.
func normalizeTags(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		t = strings.TrimSpace(utils.CapRunes(t, consts.MaxFindingTagLen))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= consts.MaxFindingTags {
			break
		}
	}
	slices.Sort(out)
	return out
}

// Get returns the finding for an event hash, or nil when the event carries none.
func (s *Store) Get(key string) *Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.s.Findings[key]; ok {
		return clone(f)
	}
	return nil
}

// GetMany returns the findings for a set of event hashes, omitting those with none. It backs
// list rendering, where asking per row would mean one lock acquisition per event.
func (s *Store) GetMany(keys []string) map[string]*Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]*Finding, len(keys))
	for _, k := range keys {
		if f, ok := s.s.Findings[k]; ok {
			out[k] = clone(f)
		}
	}
	return out
}

// Set writes an annotation, creating it or updating it in place. A finding with no flag, no
// tags, and no note is removed instead of stored (see Finding.IsEmpty).
//
// CreatedAt is preserved across updates while UpdatedAt moves, so the sidecar records when a
// judgement was first made as well as when it was last revised — for an analyst artifact,
// when you decided something is part of the record.
func (s *Store) Set(f Finding, now time.Time) (*Finding, error) {
	key := strings.TrimSpace(f.Key)
	if key == "" {
		return nil, ErrNoKey
	}
	// Counted before the trim, as the editor counts it: Go's TrimSpace and JS's trim() disagree
	// on U+0085 and U+FEFF, so a count after the trim could differ between the two sides.
	if utf8.RuneCountInString(f.Note) > consts.MaxFindingNoteLen {
		return nil, ErrTooLong
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := Finding{
		Key:        key,
		Flagged:    f.Flagged,
		Tags:       normalizeTags(f.Tags),
		Note:       strings.TrimSpace(f.Note),
		Descriptor: strings.TrimSpace(f.Descriptor),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if prev, ok := s.s.Findings[key]; ok {
		next.CreatedAt = prev.CreatedAt
		// A caller editing only the note must not blank the descriptor recorded when the
		// finding was first written.
		if next.Descriptor == "" {
			next.Descriptor = prev.Descriptor
		}
	}

	// persist writes the held map, so the change goes in first; a failed write puts the one key
	// back, and the findings shown stay the findings on disk.
	prev, had := s.s.Findings[key]
	if next.IsEmpty() {
		delete(s.s.Findings, key)
		if err := s.persist(); err != nil {
			s.restore(key, prev, had)
			return nil, err
		}
		return nil, nil
	}

	s.s.Findings[key] = &next
	if err := s.persist(); err != nil {
		s.restore(key, prev, had)
		return nil, err
	}
	return clone(&next), nil
}

// restore puts one key back as it was before a write that failed: the finding it held, or no
// entry at all. Callers hold s.mu.
func (s *Store) restore(key string, prev *Finding, had bool) {
	if had {
		s.s.Findings[key] = prev
		return
	}
	delete(s.s.Findings, key)
}

// Remove deletes an event's finding. Removing one that does not exist is not an error — the
// caller's intent (this event should carry no finding) is satisfied either way.
func (s *Store) Remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.s.Findings[key]
	if !ok {
		return nil
	}
	delete(s.s.Findings, key)
	if err := s.persist(); err != nil {
		s.restore(key, prev, true)
		return err
	}
	return nil
}

// List returns every finding, most recently updated first — the analyst's working order.
func (s *Store) List() []*Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Finding, 0, len(s.s.Findings))
	for _, f := range s.s.Findings {
		out = append(out, clone(f))
	}
	slices.SortFunc(out, func(a, b *Finding) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.Key, b.Key))
	})
	return out
}

// Keys returns the event hashes matching a finding filter (consts.FindingFilter*), for
// scoping an event query to annotated events. An unknown filter returns every annotated key
// rather than nothing, so a stale UI value degrades to "annotated" instead of showing an
// empty case. FindingFilterNone is not expressible as a key set (it is the complement) and
// returns nil with ok=false so the caller can handle it as an exclusion instead.
func (s *Store) Keys(filter string) ([]string, bool) {
	if filter == consts.FindingFilterNone {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.s.Findings))
	for k, f := range s.s.Findings {
		switch filter {
		case consts.FindingFilterFlagged:
			if !f.Flagged {
				continue
			}
		case consts.FindingFilterNoted:
			if f.Note == "" {
				continue
			}
		}
		out = append(out, k)
	}
	slices.Sort(out)
	return out, true
}

// AllKeys returns every annotated event hash, used to build the exclusion set for
// FindingFilterNone.
func (s *Store) AllKeys() []string {
	keys, _ := s.Keys(consts.FindingFilterAnnotated)
	return keys
}

// Tags returns every tag in use with its event count, most used first. It powers tag
// autocomplete and the tag filter, so the analyst reuses an existing vocabulary rather than
// inventing a new spelling each time.
func (s *Store) Tags() []TagCount {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := map[string]int{}
	for _, f := range s.s.Findings {
		for _, t := range f.Tags {
			counts[t]++
		}
	}
	out := make([]TagCount, 0, len(counts))
	for t, n := range counts {
		out = append(out, TagCount{Tag: t, Count: n})
	}
	slices.SortFunc(out, func(a, b TagCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Tag, b.Tag))
	})
	return out
}

// TagCount is one tag and how many events carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// KeysWithTag returns the event hashes carrying a tag (normalized the same way as on write,
// so a filter typed in any case still matches).
func (s *Store) KeysWithTag(tag string) []string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	if tag == "" {
		return out
	}
	for k, f := range s.s.Findings {
		if slices.Contains(f.Tags, tag) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Summary counts the findings in the case for the dashboard and the report header.
type Summary struct {
	Total   int `json:"total"`
	Flagged int `json:"flagged"`
	Noted   int `json:"noted"`
	Tagged  int `json:"tagged"`
}

// Stats returns the case's finding counts.
func (s *Store) Stats() Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Summary
	for _, f := range s.s.Findings {
		out.Total++
		if f.Flagged {
			out.Flagged++
		}
		if f.Note != "" {
			out.Noted++
		}
		if len(f.Tags) > 0 {
			out.Tagged++
		}
	}
	return out
}
