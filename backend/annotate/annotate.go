// Package annotate holds the analyst's own marks on a graph: notes pinned to events, regions
// drawn around parts of the canvas, and arrows between two points, organised into layers.
//
// It follows the findings precedent exactly — a JSON sidecar beside the case, plain and readable,
// hash-keyed where it attaches to an event — for the same reason: this is authored, not derived,
// and opinion belongs beside the evidence rather than inside it.
//
// The difference from findings is SCOPE, and it is worth being precise about because the two
// look similar. A finding is about an EVENT and follows it into every graph it appears in. An
// annotation is about a PICTURE and belongs to the graph it was drawn on. "This 4624 is the
// account takeover" is a finding. "This cluster is the lateral movement" is an annotation — it
// means nothing outside the arrangement it was drawn over.
//
// 🔒 An annotation anchored to an event stores that event's hash_normalized, never its node id.
// Node ids are assignment-order and a re-ingest hands the same id to a different event, so an
// id-anchored note would silently end up describing an unrelated record.
package annotate

import (
	"encoding/json"
	"errors"
	"fmt"
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

// ErrNotFound is returned for a layer or annotation id that does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalid marks a request the store refuses: a field it does not accept, or a limit reached.
// The binding codes it parse_error, the request's fault; any other failure is the disk's.
var ErrInvalid = errors.New("annotation request refused")

// refusal is a refused request. Its message is the refusal's own, so wrapping it in ErrInvalid
// changes nothing the analyst reads.
type refusal struct{ msg string }

func (r refusal) Error() string { return r.msg }
func (r refusal) Unwrap() error { return ErrInvalid }

// refuse builds a refusal, formatted like fmt.Errorf.
func refuse(format string, args ...any) error {
	return refusal{fmt.Sprintf(format, args...)}
}

// Layer groups annotations so they can be shown, hidden and ordered together.
type Layer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Colour  string `json:"colour,omitempty"`
	Visible bool   `json:"visible"`
	// Z orders layers against each other. Lower draws first.
	Z int `json:"z"`
}

// Anchor is where an annotation is attached.
//
// An event anchor follows its event: the canvas resolves the hash to whatever node holds it now,
// so a note moves with the card when the layout changes. A world anchor is a fixed rectangle on
// the canvas, for a mark about a region rather than about a record.
type Anchor struct {
	Kind string `json:"kind"`
	// Hash is the event's hash_normalized, for an event anchor.
	Hash string `json:"hash,omitempty"`
	// X/Y/W/H are world coordinates, for a world anchor. W/H are zero for a point.
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	W float64 `json:"w,omitempty"`
	H float64 `json:"h,omitempty"`
}

// Item is one annotation.
type Item struct {
	ID    string `json:"id"`
	Layer string `json:"layer"`
	Kind  string `json:"kind"`
	// Anchor is where it attaches. To is the second anchor, for an arrow.
	Anchor    Anchor    `json:"anchor"`
	To        *Anchor   `json:"to,omitempty"`
	Text      string    `json:"text,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Document is one graph's annotations.
type Document struct {
	AnnotationsVersion int     `json:"annotations_version"`
	GraphID            uint64  `json:"graph_id"`
	Layers             []Layer `json:"layers"`
	Items              []Item  `json:"items"`
}

// Store is the on-disk collection, one file per graph, loaded on demand.
type Store struct {
	mu  sync.Mutex
	dir string
	// docs caches loaded graphs. Annotations are edited interactively, so re-reading the file on
	// every keystroke would be the wrong shape; the cache is written through on every mutation,
	// and changes only once the write has succeeded (commit).
	docs map[uint64]*Document
}

// Open roots an annotation store at dir, creating it if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir, docs: map[uint64]*Document{}}, nil
}

func (s *Store) path(graphID uint64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d.json", graphID))
}

// load returns the graph's document, reading it from disk on first use. Caller holds the lock.
func (s *Store) load(graphID uint64) (*Document, error) {
	if doc, ok := s.docs[graphID]; ok {
		return doc, nil
	}
	doc := &Document{AnnotationsVersion: consts.AnnotationsVersion, GraphID: graphID}
	data, err := os.ReadFile(s.path(graphID))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// A graph with no annotations is a new one, not an error.
	} else {
		if err := json.Unmarshal(data, doc); err != nil {
			return nil, err
		}
		if doc.AnnotationsVersion > consts.AnnotationsVersion {
			return nil, fmt.Errorf(consts.MsgAnnotationBadVersion, doc.AnnotationsVersion)
		}
		doc.GraphID = graphID
	}
	if doc.Layers == nil {
		doc.Layers = []Layer{}
	}
	if doc.Items == nil {
		doc.Items = []Item{}
	}
	s.docs[graphID] = doc
	return doc, nil
}

// persist durably writes one graph's sidecar. Caller holds the lock.
func (s *Store) persist(doc *Document) error {
	doc.AnnotationsVersion = consts.AnnotationsVersion
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return durable.WriteFile(s.path(doc.GraphID), data)
}

// commit writes next and, only once it is on disk, makes it the graph's cached document. Caller
// holds the lock. Every mutation changes a copy (cloneDoc), never the cached document, so a failed
// write leaves the cache as the file is: what the canvas reloads is what a restart would read.
func (s *Store) commit(next *Document) error {
	if err := s.persist(next); err != nil {
		return err
	}
	s.docs[next.GraphID] = next
	return nil
}

// Get returns a copy of one graph's annotations. A copy, so a caller holding the result cannot
// mutate stored state through it.
func (s *Store) Get(graphID uint64) (*Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(graphID)
	if err != nil {
		return nil, err
	}
	return cloneDoc(doc), nil
}

// AddLayer creates a layer and returns it.
func (s *Store) AddLayer(graphID uint64, name, colour string, now time.Time) (*Layer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(graphID)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, refuse(consts.MsgAnnotationLayerName)
	}
	name = utils.CapRunes(name, consts.MaxLayerNameLen)
	if len(doc.Layers) >= consts.MaxAnnotationLayers {
		return nil, refuse(consts.MsgAnnotationLayerLimit, consts.MaxAnnotationLayers)
	}

	next := cloneDoc(doc)
	l := Layer{
		ID:      newID("layer", now, len(next.Layers), func(id string) bool { return hasLayer(next, id) }),
		Name:    name,
		Colour:  strings.TrimSpace(colour),
		Visible: true,
		Z:       nextZ(next.Layers),
	}
	next.Layers = append(next.Layers, l)
	if err := s.commit(next); err != nil {
		return nil, err
	}
	return &l, nil
}

// UpdateLayer renames, recolours, shows/hides or reorders a layer.
func (s *Store) UpdateLayer(graphID uint64, l Layer) (*Layer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(graphID)
	if err != nil {
		return nil, err
	}
	for i := range doc.Layers {
		if doc.Layers[i].ID != l.ID {
			continue
		}
		name := strings.TrimSpace(l.Name)
		if name == "" {
			return nil, refuse(consts.MsgAnnotationLayerName)
		}
		name = utils.CapRunes(name, consts.MaxLayerNameLen)
		next := cloneDoc(doc)
		next.Layers[i].Name = name
		next.Layers[i].Colour = strings.TrimSpace(l.Colour)
		next.Layers[i].Visible = l.Visible
		next.Layers[i].Z = l.Z
		if err := s.commit(next); err != nil {
			return nil, err
		}
		out := next.Layers[i]
		return &out, nil
	}
	return nil, fmt.Errorf("%w: "+consts.MsgAnnotationNoLayer, ErrNotFound, l.ID)
}

// DeleteLayer removes a layer AND everything on it.
//
// It is all-or-nothing on purpose. Orphaning the items onto a default layer would leave marks on
// the canvas the analyst thought they had just removed; leaving them on a layer that no longer
// exists would leave them invisible and unreachable. Deleting a layer means deleting what is on
// it, and the count is returned so the caller can say so before doing it.
func (s *Store) DeleteLayer(graphID uint64, layerID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(graphID)
	if err != nil {
		return 0, err
	}
	// Filtering in place is safe only over the clone, which owns its storage: over the cached
	// document it would rewrite the slices the cache still holds before the write is known to land.
	next := cloneDoc(doc)
	found := false
	layers := next.Layers[:0]
	for _, l := range next.Layers {
		if l.ID == layerID {
			found = true
			continue
		}
		layers = append(layers, l)
	}
	if !found {
		return 0, fmt.Errorf("%w: "+consts.MsgAnnotationNoLayer, ErrNotFound, layerID)
	}
	next.Layers = layers

	removed := 0
	items := next.Items[:0]
	for _, it := range next.Items {
		if it.Layer == layerID {
			removed++
			continue
		}
		items = append(items, it)
	}
	next.Items = items
	if err := s.commit(next); err != nil {
		return 0, err
	}
	return removed, nil
}

// CountOnLayer reports how many annotations a layer holds, so a caller can warn before deleting.
func (s *Store) CountOnLayer(graphID uint64, layerID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(graphID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range doc.Items {
		if it.Layer == layerID {
			n++
		}
	}
	return n, nil
}

// Put creates or updates one annotation. An empty Item.ID creates; a known id updates.
//
// A missing layer is created rather than refused: the common path is an analyst annotating
// something before they have thought about layers at all, and stopping them to make one first is
// friction with no purpose.
func (s *Store) Put(graphID uint64, it Item, now time.Time) (*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(graphID)
	if err != nil {
		return nil, err
	}
	if err := validate(&it); err != nil {
		return nil, err
	}

	// Everything below changes the clone, so a refusal (the item limit) or a failed write throws
	// it away, along with any default layer made on the way.
	next := cloneDoc(doc)
	if it.Layer == "" {
		l, err := s.ensureDefaultLayer(next, now)
		if err != nil {
			return nil, err
		}
		it.Layer = l.ID
	} else if !hasLayer(next, it.Layer) {
		return nil, fmt.Errorf("%w: "+consts.MsgAnnotationNoLayer, ErrNotFound, it.Layer)
	}

	for i := range next.Items {
		if next.Items[i].ID != it.ID || it.ID == "" {
			continue
		}
		it.CreatedAt = next.Items[i].CreatedAt
		it.UpdatedAt = now
		next.Items[i] = it
		if err := s.commit(next); err != nil {
			return nil, err
		}
		out := next.Items[i]
		return &out, nil
	}

	if len(next.Items) >= consts.MaxAnnotationItems {
		return nil, refuse(consts.MsgAnnotationItemLimit, consts.MaxAnnotationItems)
	}
	it.ID = newID("a", now, len(next.Items), func(id string) bool { return hasItem(next, id) })
	it.CreatedAt = now
	it.UpdatedAt = now
	next.Items = append(next.Items, it)
	if err := s.commit(next); err != nil {
		return nil, err
	}
	out := it
	return &out, nil
}

// Delete removes one annotation. Deleting one that is already gone is success.
func (s *Store) Delete(graphID uint64, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(graphID)
	if err != nil {
		return err
	}
	next := cloneDoc(doc)
	items := next.Items[:0]
	for _, it := range next.Items {
		if it.ID == id {
			continue
		}
		items = append(items, it)
	}
	if len(items) == len(next.Items) {
		return nil
	}
	next.Items = items
	return s.commit(next)
}

// DeleteGraph removes a graph's annotations entirely, for a graph that has been deleted.
func (s *Store) DeleteGraph(graphID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, graphID)
	if err := os.Remove(s.path(graphID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Hashes returns every event hash an annotation on this graph anchors to, so a caller can report
// annotations whose event has left the case rather than leaving them silently unanchored.
func (s *Store) Hashes(graphID uint64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(graphID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(doc.Items))
	for _, it := range doc.Items {
		for _, a := range anchorsOf(it) {
			if a.Kind == consts.AnchorEvent && a.Hash != "" && !seen[a.Hash] {
				seen[a.Hash] = true
				out = append(out, a.Hash)
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

func (s *Store) ensureDefaultLayer(doc *Document, now time.Time) (*Layer, error) {
	if len(doc.Layers) > 0 {
		out := doc.Layers[0]
		return &out, nil
	}
	l := Layer{
		ID:      newID("layer", now, 0, func(id string) bool { return hasLayer(doc, id) }),
		Name:    consts.DefaultLayerName,
		Visible: true,
		Z:       0,
	}
	doc.Layers = append(doc.Layers, l)
	return &l, nil
}

// validate enforces the contract. It refuses rather than correcting, for the same reason findings
// refuse an overlong note: silently discarding the tail of an analyst's reasoning is worse than
// telling them it did not fit.
func validate(it *Item) error {
	it.Text = strings.TrimSpace(it.Text)
	if utf8.RuneCountInString(it.Text) > consts.MaxAnnotationText {
		return refuse(consts.MsgAnnotationTextLong, consts.MaxAnnotationText)
	}
	if !known(consts.AnnotationKinds, it.Kind) {
		return refuse(consts.MsgAnnotationBadKind, it.Kind, strings.Join(consts.AnnotationKinds, ", "))
	}
	if err := validAnchor(it.Anchor); err != nil {
		return err
	}
	if it.Kind == consts.AnnotationArrow {
		if it.To == nil {
			return refuse(consts.MsgAnnotationNeedsTarget)
		}
		if err := validAnchor(*it.To); err != nil {
			return err
		}
	} else {
		// A second anchor on something that is not an arrow is dropped rather than stored: it
		// would be data nothing reads, and a later build adding a meaning for it would silently
		// start honouring values nobody chose.
		it.To = nil
	}
	return nil
}

func validAnchor(a Anchor) error {
	if !known(consts.AnchorKinds, a.Kind) {
		return refuse(consts.MsgAnnotationBadAnchor, a.Kind, strings.Join(consts.AnchorKinds, ", "))
	}
	if a.Kind == consts.AnchorEvent && strings.TrimSpace(a.Hash) == "" {
		// 🔒 Without the hash there is nothing to follow the event by, and the annotation would
		// attach to whatever node id happened to be reused.
		return refuse(consts.MsgAnnotationNeedsHash)
	}
	return nil
}

func anchorsOf(it Item) []Anchor {
	if it.To != nil {
		return []Anchor{it.Anchor, *it.To}
	}
	return []Anchor{it.Anchor}
}

func hasLayer(doc *Document, id string) bool {
	for _, l := range doc.Layers {
		if l.ID == id {
			return true
		}
	}
	return false
}

func hasItem(doc *Document, id string) bool {
	for _, it := range doc.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func nextZ(layers []Layer) int {
	z := 0
	for _, l := range layers {
		if l.Z >= z {
			z = l.Z + 1
		}
	}
	return z
}

func known(set []string, v string) bool {
	return slices.Contains(set, v)
}

// newID derives a stable, sortable id: the prefix, the creation time to the millisecond, and a
// sequence. Derived rather than random so the sidecar reads in creation order by eye, and so a
// test can predict it.
//
// The sequence starts at seq, the count already held, and steps past every id taken reports. The
// count alone is not unique: after a delete it can name a held id created in the same millisecond,
// and Delete and DeleteLayer remove every entry with an id, so a repeat would take both. taken is
// true for at most the ids held, so the loop ends within that many steps.
func newID(prefix string, at time.Time, seq int, taken func(string) bool) string {
	stamp := at.UTC().Format("20060102150405.000")
	for ; ; seq++ {
		if id := fmt.Sprintf("%s-%s-%d", prefix, stamp, seq); !taken(id) {
			return id
		}
	}
}

// cloneDoc copies a document deeply enough that changing the copy cannot reach the original. The
// mutators change a clone and commit it, so a clone is also what gets saved: both slices are made
// rather than appended to nil, so an empty one is written as [] and never as null.
func cloneDoc(doc *Document) *Document {
	out := *doc
	out.Layers = make([]Layer, len(doc.Layers))
	copy(out.Layers, doc.Layers)
	out.Items = make([]Item, len(doc.Items))
	copy(out.Items, doc.Items)
	for i := range out.Items {
		if out.Items[i].To != nil {
			to := *out.Items[i].To
			out.Items[i].To = &to
		}
	}
	return &out
}
