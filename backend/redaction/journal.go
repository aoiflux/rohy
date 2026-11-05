// Package redaction removes an event's content from a case, attributably (plan §14.5, item 40).
//
// This file is the journal: one entry per redaction, kept beside the case in
// rohy-data/redactions/journal.json. It has two duties.
//
// It is the record of a redaction in progress. An event's payload reference lives only in its
// properties, and the strip destroys them; graphene records a redaction before it destroys, but
// records the node, not where rohy put the content. So the entry is written, durably, before any
// destructive step, and each step's outcome is added as it lands. A redaction that stopped part way
// is finished from its entry.
//
// It is the suppression list. A stripped event has no index entries, so dedup cannot see it, and
// a re-ingest would bring the content straight back. Ingest skips any record whose hash_normalized
// or hash_raw an entry names — pending or done — and counts it.
//
// 🔒 Fail closed. A journal that cannot be read is reported and left exactly as found; it is never
// set aside and replaced the way the fingerprint history is, because a fresh journal would quietly
// stop suppressing every event the old one named. Until it is restored, ingest and redaction refuse.
//
// 🔒 Disk first, then memory. A write that fails leaves the journal held in memory unchanged, so
// what suppresses is exactly what is on disk.
//
// What it keeps: node ids, the two identity hashes, payload offsets, and the reason in plain text.
// The hashes are unsalted, over fields an investigator could guess; they are pseudonymous data the
// case keeps on purpose. It never keeps an event's content.
package redaction

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"rohy/backend/consts"
	"rohy/backend/durable"
	"rohy/backend/payload"
)

// maxEntryID keeps ids exact in the frontend, which reads JSON numbers as doubles.
const maxEntryID = 1 << 53

// Target is one event a redaction removes.
type Target struct {
	Node           uint64      `json:"node"`
	HashNormalized string      `json:"hash_normalized"`
	HashRaw        string      `json:"hash_raw,omitempty"`
	Payload        payload.Ref `json:"payload,omitzero"`
	// Copies are other payload records holding this event's raw record: left by a failed batch, a
	// resumed ingest or a deleted event. Each is tombstoned with Node as its owner.
	Copies []payload.Ref `json:"copies,omitempty"`
}

// Step is how far one stage of a redaction got. A step not listed was not reached.
type Step struct {
	Name  string    `json:"name"`
	State string    `json:"state"`
	Error string    `json:"error,omitempty"`
	At    time.Time `json:"at"`
}

// Entry is one redaction.
type Entry struct {
	ID uint64 `json:"id"`
	// Event is the one the analyst chose; Targets also holds its siblings.
	Event   uint64    `json:"event"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
	Targets []Target  `json:"targets"`
	// Relations are the rule-made relations it removes.
	Relations []uint64  `json:"relations,omitempty"`
	Steps     []Step    `json:"steps,omitempty"`
	Done      bool      `json:"done"`
	DoneAt    time.Time `json:"done_at,omitzero"`
}

type document struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Journal is the redaction journal of one case. Safe for concurrent use.
type Journal struct {
	dir string

	mu     sync.Mutex
	doc    document
	damage error
	// normalized and raw are the suppressed identities, rebuilt from doc after every write.
	normalized map[string]struct{}
	raw        map[string]struct{}
}

// Open reads the journal in dir, creating dir if needed. It fails only when dir cannot be made; a
// journal that cannot be used is reported by Err, and the file is not touched.
func Open(dir string) (*Journal, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	j := &Journal{dir: dir}
	j.damage = j.load()
	if j.damage != nil {
		j.doc = document{}
	}
	j.reindex()
	return j, nil
}

func (j *Journal) file() string { return filepath.Join(j.dir, consts.RedactionJournalFile) }

// load reads the file into j.doc. Called from Open only, before j is shared.
func (j *Journal) load() error {
	file := j.file()
	info, err := os.Stat(file)
	if errors.Is(err, fs.ErrNotExist) {
		j.doc = document{Version: consts.RedactionJournalVersion}
		return nil
	}
	if err != nil {
		return fmt.Errorf(consts.MsgJournalUnreadable, file, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(consts.MsgJournalUnreadable, file, errors.New("not a regular file"))
	}
	if info.Size() > consts.MaxRedactionJournalSize {
		return fmt.Errorf(consts.MsgJournalTooLarge, file, info.Size(), consts.MaxRedactionJournalSize)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf(consts.MsgJournalUnreadable, file, err)
	}
	d, err := decode(file, data)
	if err != nil {
		return err
	}
	j.doc = d
	return nil
}

// decode parses and checks a journal document read from file.
func decode(file string, data []byte) (document, error) {
	var d document
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf(consts.MsgJournalCorrupt, file, err)
	}
	if d.Version > consts.RedactionJournalVersion {
		return d, fmt.Errorf(consts.MsgJournalNewer, file, d.Version, consts.RedactionJournalVersion)
	}
	if d.Version < 1 {
		return d, fmt.Errorf(consts.MsgJournalCorrupt, file, fmt.Errorf("format %d", d.Version))
	}
	seen := make(map[uint64]bool, len(d.Entries))
	for i := range d.Entries {
		e := &d.Entries[i]
		if seen[e.ID] {
			return d, fmt.Errorf(consts.MsgJournalCorrupt, file, fmt.Errorf(consts.MsgJournalDuplicateID, e.ID))
		}
		seen[e.ID] = true
		if err := checkStored(e); err != nil {
			return d, fmt.Errorf(consts.MsgJournalCorrupt, file, fmt.Errorf(consts.MsgJournalBadEntry, e.ID, err))
		}
	}
	return d, nil
}

// checkStored is what an entry read back must satisfy: what Begin and Step would have written.
func checkStored(e *Entry) error {
	if e.ID == 0 || e.ID > maxEntryID {
		return fmt.Errorf("id %d", e.ID)
	}
	if r, err := ValidReason(e.Reason); err != nil {
		return err
	} else if r != e.Reason {
		return errors.New("the reason is not as written")
	}
	if err := checkTargets(e.Targets); err != nil {
		return err
	}
	for _, s := range e.Steps {
		if !knownStep(s.Name) {
			return fmt.Errorf(consts.MsgJournalUnknownStep, s.Name)
		}
		if s.State != consts.RedactStepDone && s.State != consts.RedactStepFailed {
			return fmt.Errorf("step %s: state %q", s.Name, s.State)
		}
	}
	return nil
}

func checkTargets(ts []Target) error {
	if len(ts) == 0 {
		return errors.New(consts.MsgJournalNoTargets)
	}
	for i, t := range ts {
		if t.Node == 0 || t.HashNormalized == "" {
			return fmt.Errorf(consts.MsgJournalBadTarget, i)
		}
	}
	return nil
}

var steps = []string{
	consts.RedactStepRelations, consts.RedactStepRecords, consts.RedactStepPayloads,
	consts.RedactStepSidecars, consts.RedactStepSeal,
}

func knownStep(name string) bool { return slices.Contains(steps, name) }

// ValidReason trims a reason and checks it can be kept: present, valid text, and within
// consts.MaxRedactionReasonLen bytes.
func ValidReason(reason string) (string, error) {
	r := strings.TrimSpace(reason)
	switch {
	case r == "":
		return "", errors.New(consts.MsgRedactReasonEmpty)
	case !utf8.ValidString(r):
		return "", errors.New(consts.MsgRedactReasonUTF8)
	case len(r) > consts.MaxRedactionReasonLen:
		return "", fmt.Errorf(consts.MsgRedactReasonTooLong, len(r), consts.MaxRedactionReasonLen)
	}
	return r, nil
}

// Err reports why the journal cannot be used, or nil. While it is not nil, nothing may be ingested
// or redacted: the journal lists what must not come back.
func (j *Journal) Err() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.damage
}

// Suppressed reports whether a record with these identities was redacted. The empty hash is
// nobody's identity and never matches.
func (j *Journal) Suppressed(hashNormalized, hashRaw string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, ok := j.normalized[hashNormalized]; ok && hashNormalized != "" {
		return true
	}
	_, ok := j.raw[hashRaw]
	return ok && hashRaw != ""
}

// Begin records a redaction about to start and returns it as stored, with its id. From this moment
// its targets are suppressed.
func (j *Journal) Begin(e Entry) (Entry, error) {
	reason, err := ValidReason(e.Reason)
	if err != nil {
		return Entry{}, err
	}
	if err := checkTargets(e.Targets); err != nil {
		return Entry{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.damage != nil {
		return Entry{}, j.damage
	}
	var last uint64
	for _, x := range j.doc.Entries {
		last = max(last, x.ID)
	}
	if last >= maxEntryID {
		return Entry{}, fmt.Errorf(consts.MsgJournalWriteFailed, errors.New("entry ids exhausted"))
	}
	stored := clone(e)
	stored.ID, stored.Reason, stored.Steps, stored.Done = last+1, reason, nil, false
	next := j.doc
	next.Entries = append(slices.Clip(j.doc.Entries), stored)
	if err := j.commit(next); err != nil {
		return Entry{}, err
	}
	return clone(stored), nil
}

// Step records how one step of entry id ended: done when err is nil, failed with its message when
// not. A step run again replaces what it recorded before.
func (j *Journal) Step(id uint64, name string, at time.Time, stepErr error) error {
	if !knownStep(name) {
		return fmt.Errorf(consts.MsgJournalUnknownStep, name)
	}
	s := Step{Name: name, State: consts.RedactStepDone, At: at}
	if stepErr != nil {
		s.State, s.Error = consts.RedactStepFailed, stepErr.Error()
	}
	return j.update(id, func(e *Entry) {
		if i := slices.IndexFunc(e.Steps, func(x Step) bool { return x.Name == name }); i >= 0 {
			e.Steps[i] = s
			return
		}
		e.Steps = append(e.Steps, s)
	})
}

// Complete marks entry id done. Its targets stay suppressed.
func (j *Journal) Complete(id uint64, at time.Time) error {
	return j.update(id, func(e *Entry) { e.Done, e.DoneAt = true, at })
}

// update applies fn to a copy of entry id and commits the result.
func (j *Journal) update(id uint64, fn func(*Entry)) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.damage != nil {
		return j.damage
	}
	i := slices.IndexFunc(j.doc.Entries, func(e Entry) bool { return e.ID == id })
	if i < 0 {
		return fmt.Errorf(consts.MsgJournalNoEntry, id)
	}
	next := j.doc
	next.Entries = cloneAll(j.doc.Entries)
	fn(&next.Entries[i])
	return j.commit(next)
}

// commit writes next and, only once it is on disk, makes it the journal held. Callers hold j.mu.
func (j *Journal) commit(next document) error {
	next.Version = consts.RedactionJournalVersion
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf(consts.MsgJournalWriteFailed, err)
	}
	if err := durable.WriteFile(j.file(), data); err != nil {
		return fmt.Errorf(consts.MsgJournalWriteFailed, err)
	}
	j.doc = next
	j.reindex()
	return nil
}

// reindex rebuilds the suppressed identities from j.doc. Callers hold j.mu, or own j.
func (j *Journal) reindex() {
	j.normalized, j.raw = map[string]struct{}{}, map[string]struct{}{}
	for _, e := range j.doc.Entries {
		for _, t := range e.Targets {
			if t.HashNormalized != "" {
				j.normalized[t.HashNormalized] = struct{}{}
			}
			if t.HashRaw != "" {
				j.raw[t.HashRaw] = struct{}{}
			}
		}
	}
}

// Get returns entry id.
func (j *Journal) Get(id uint64) (Entry, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, e := range j.doc.Entries {
		if e.ID == id {
			return clone(e), true
		}
	}
	return Entry{}, false
}

// PendingFor returns the unfinished entry that names node as a target, if there is one.
func (j *Journal) PendingFor(node uint64) (Entry, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, e := range j.doc.Entries {
		if e.Done {
			continue
		}
		for _, t := range e.Targets {
			if t.Node == node {
				return clone(e), true
			}
		}
	}
	return Entry{}, false
}

// Entries returns every entry, oldest first.
func (j *Journal) Entries() []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	return cloneAll(j.doc.Entries)
}

// Pending returns the entries not yet done, oldest first.
func (j *Journal) Pending() []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []Entry
	for _, e := range j.doc.Entries {
		if !e.Done {
			out = append(out, clone(e))
		}
	}
	return out
}

func clone(e Entry) Entry {
	e.Targets = slices.Clone(e.Targets)
	for i := range e.Targets {
		e.Targets[i].Copies = slices.Clone(e.Targets[i].Copies)
	}
	e.Relations = slices.Clone(e.Relations)
	e.Steps = slices.Clone(e.Steps)
	return e
}

func cloneAll(es []Entry) []Entry {
	out := make([]Entry, len(es))
	for i, e := range es {
		out[i] = clone(e)
	}
	return out
}

// PayloadEnd is how far the furthest payload record any entry names extends: a redacted event no
// longer references its record, so this is what tells a tombstone from an interrupted ingest's tail.
func (j *Journal) PayloadEnd() uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	var end uint64
	for _, e := range j.doc.Entries {
		for _, t := range e.Targets {
			end = max(end, refEnd(t.Payload))
			for _, c := range t.Copies {
				end = max(end, refEnd(c))
			}
		}
	}
	return end
}

func refEnd(r payload.Ref) uint64 {
	if r.IsZero() {
		return 0
	}
	return r.Offset + uint64(r.Length)
}
