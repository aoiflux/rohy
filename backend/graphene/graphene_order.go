package graphene

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// Ordered-page machinery for the events list.
//
// The naive read path answered ONE page by hydrating every matching event — a full JSON
// decode per node, including the multi-kilobyte RawXML payload — purely to learn each
// event's timestamp so the set could be sorted. Progressive loading then repeated that
// whole scan for every page the user scrolled to, so cost grew with the dataset and was
// paid again and again.
//
// Two changes fix that:
//
//   1. Ordering decodes a MINIMAL view of each node (timestamp plus whatever the active
//      post-hydration filters need) instead of the whole Event, so the big fields are
//      never materialized just to sort.
//   2. The resulting id order is CACHED against the filter and a store version counter, so
//      paging through the same filter hydrates only the page it returns. Any write bumps
//      the version and invalidates the cache, so a stale order can never be served.

// eventSortView is the slice of an event's stored JSON that ordering and post-hydration
// filtering actually need. Decoding into this instead of Event avoids allocating RawXML
// and ParsedFields for every candidate row.
type eventSortView struct {
	Timestamp          time.Time `json:"timestamp"`
	SourceIdentifier   string    `json:"source_identifier"`
	DeduplicationCount int       `json:"deduplication_count"`
	// Lane fields for the timeline's grouping (P24). These are short scalars, so decoding
	// them costs almost nothing — unlike RawXML and ParsedFields, which stay excluded.
	Provider string `json:"provider"`
	Channel  string `json:"channel"`
	User     string `json:"user"`
	Computer string `json:"computer"`
	// HashNormalized is the event's content identity, which the findings filters match on
	// (P25). Also a short scalar.
	HashNormalized string `json:"hash_normalized"`
}

// orderedIDs is a filter's matching event ids in presentation order.
type orderCache struct {
	mu      sync.Mutex
	version uint64
	key     string
	ids     []uint64
	valid   bool
}

// bumpVersion invalidates any cached ordering. Every write path calls it: relations count
// too, because the relation-state filter selects events by their edges.
func (s *Store) bumpVersion() {
	s.order.mu.Lock()
	s.order.version++
	s.order.valid = false
	s.order.ids = nil
	s.order.mu.Unlock()
}

// lookupOrder returns the cached ordering for a filter, if it is still current.
func (s *Store) lookupOrder(key string) ([]uint64, bool) {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()
	if !s.order.valid || s.order.key != key {
		return nil, false
	}
	return s.order.ids, true
}

// storeOrder caches an ordering, stamped with the version it was computed at. A write that
// lands mid-computation bumps the version, and the check here drops the now-stale result
// rather than caching it.
func (s *Store) storeOrder(key string, version uint64, ids []uint64) {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()
	if s.order.version != version {
		return
	}
	s.order.key = key
	s.order.ids = ids
	s.order.valid = true
}

// currentVersion reads the write counter.
func (s *Store) currentVersion() uint64 {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()
	return s.order.version
}

// orderKey fingerprints everything about a filter that changes WHICH events match or in
// what order — deliberately excluding Offset and Limit, since paging must reuse one
// ordering rather than recompute it per page.
func (f EventFilter) orderKey() string {
	var b strings.Builder
	add := func(parts ...string) {
		for _, p := range parts {
			b.WriteString(p)
			b.WriteByte(0x1f) // unit separator: keeps adjacent fields unambiguous
		}
	}
	add(f.EventID, f.Provider, f.Channel, f.User, f.Search, f.SourceType, f.SourceIdentifier, f.RelationState, f.Undated)
	add(fmt.Sprint(f.MinDuplicateCount), fmt.Sprint(f.Descending))
	if f.TimeFrom != nil {
		add("from:" + f.TimeFrom.UTC().Format(time.RFC3339Nano))
	}
	if f.TimeTo != nil {
		add("to:" + f.TimeTo.UTC().Format(time.RFC3339Nano))
	}
	// The hash sets change WHICH events match, so they must be part of the key — otherwise
	// paging a findings-filtered list would be served the unfiltered ordering left in the
	// cache by the previous query. Fingerprinting is exact (sorted, not a digest) because a
	// collision here would silently serve the wrong events, and the sets are analyst-authored
	// so their size tracks how much has been annotated, not how large the case is.
	add("in:"+hashSetKey(f.HashIn), "out:"+hashSetKey(f.HashNotIn))
	return b.String()
}

// hashSetKey renders a hash set deterministically. A nil set (no filtering) and an empty
// non-nil set (nothing matches) must produce different keys, so they are marked apart.
func hashSetKey(set map[string]bool) string {
	if set == nil {
		return "nil"
	}
	keys := make([]string, 0, len(set))
	for k, ok := range set {
		if ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return fmt.Sprintf("%d:%s", len(keys), strings.Join(keys, ","))
}

// orderedIDs returns every matching event id in presentation order, using the cache when
// the filter and store are unchanged since it was built.
func (s *Store) orderedIDs(f EventFilter) ([]uint64, error) {
	key := f.orderKey()
	if ids, ok := s.lookupOrder(key); ok {
		return ids, nil
	}

	version := s.currentVersion()
	ids, err := s.computeOrder(f)
	if err != nil {
		return nil, err
	}
	s.storeOrder(key, version, ids)
	return ids, nil
}

// eventRow is a matching event reduced to what ordering and bucketing need.
type eventRow struct {
	id uint64
	ts time.Time
	// view is retained so the timeline can group into lanes without a second scan.
	view eventSortView
}

// matchingRows applies every filter and returns (id, timestamp) for each match, decoding
// only the minimal sort view. Ordering and the timeline's bucketing both build on it, so
// the two can never disagree about what matches.
func (s *Store) matchingRows(f EventFilter) ([]eventRow, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	nodes, err := g.QueryNodes(store.NodeQuery{
		Types:      []store.NodeType{nodeEventType()},
		Filters:    f.propertyFilters(),
		FilterMode: store.MatchAll,
	})
	if err != nil {
		return nil, err
	}

	related, err := s.relatedEventIDs(f.RelationState)
	if err != nil {
		return nil, err
	}

	rows := make([]eventRow, 0, len(nodes))
	for _, n := range nodes {
		id := uint64(n.ID)
		if related != nil && !related[id] {
			continue
		}
		var v eventSortView
		if len(n.Properties) > 0 {
			if err := json.Unmarshal(n.Properties, &v); err != nil {
				return nil, err
			}
		}
		if !f.matchesSortView(&v) {
			continue
		}
		if !f.matchesUndated(v.Timestamp.IsZero()) {
			continue
		}
		rows = append(rows, eventRow{id: id, ts: v.Timestamp, view: v})
	}
	return rows, nil
}

// computeOrder scans the index for matching nodes and orders them.
func (s *Store) computeOrder(f EventFilter) ([]uint64, error) {
	rows, err := s.matchingRows(f)
	if err != nil {
		return nil, err
	}

	// Chronological, tie-broken by id so paging is stable and never skips or duplicates.
	//
	// Undated events are sorted to the END regardless of direction (P23). They have no
	// chronological position at all, so letting the zero time order them would park them at
	// "1970" — ahead of every real record when ascending — which reads as a date rather than
	// as the absence of one.
	sort.Slice(rows, func(i, j int) bool {
		zi, zj := rows[i].ts.IsZero(), rows[j].ts.IsZero()
		if zi != zj {
			return zj // a dated event always precedes an undated one
		}
		if zi { // both undated: no time to compare, so order stably by id
			return rows[i].id < rows[j].id
		}
		if rows[i].ts.Equal(rows[j].ts) {
			return rows[i].id < rows[j].id
		}
		if f.Descending {
			return rows[i].ts.After(rows[j].ts)
		}
		return rows[i].ts.Before(rows[j].ts)
	})

	ids := make([]uint64, len(rows))
	for i, r := range rows {
		ids[i] = r.id
	}
	return ids, nil
}

// matchesUndated applies the undated policy: by default an event with no timestamp is not
// timeline evidence and is excluded, but it remains reachable by asking for it explicitly.
// An unrecognized value includes everything rather than hiding data.
func (f EventFilter) matchesUndated(undated bool) bool {
	switch f.Undated {
	case consts.UndatedOnly:
		return undated
	case consts.UndatedInclude:
		return true
	default: // consts.UndatedExclude
		return !undated
	}
}

// matchesSortView applies the non-indexed filters against the minimal decode, mirroring
// matchesPostHydration so both paths agree on what matches.
func (f EventFilter) matchesSortView(v *eventSortView) bool {
	if f.SourceIdentifier != "" && v.SourceIdentifier != f.SourceIdentifier {
		return false
	}
	if f.MinDuplicateCount > 0 {
		count := v.DeduplicationCount
		if count < 1 {
			count = 1 // legacy nodes predate the field; treat as a single occurrence
		}
		if count < f.MinDuplicateCount {
			return false
		}
	}
	if !f.matchesHash(v.HashNormalized) {
		return false
	}
	return true
}

// hydrateIDs loads the given ids as events, in the order given, using one batched fetch.
func (s *Store) hydrateIDs(ids []uint64) ([]*Event, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	nodeIDs := make([]store.NodeID, len(ids))
	for i, id := range ids {
		nodeIDs[i] = store.NodeID(id)
	}
	nodes, err := g.GetNodes(nodeIDs)
	if err != nil {
		return nil, err
	}
	out := make([]*Event, 0, len(nodes))
	for _, n := range nodes {
		e, err := eventFromNode(n)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
