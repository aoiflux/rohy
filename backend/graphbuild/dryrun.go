package graphbuild

import (
	"sync"
	"time"

	"rohy/backend/autograph"
	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// The rule testbench: run a rule against the real case and report what it WOULD do.
//
// This is a small amount of code for the size of the feature, and that is not an accident. It
// is the payoff of a decision made long before it: autograph is a pure function that returns
// unpersisted relations and cannot write. So "try this rule without committing to it" needs no
// sandbox, no transaction to roll back, and no second evaluation path that might diverge from
// the real one — it is the SAME call the build makes, with the persistence simply not done.
//
// Nothing here calls EnsureForRule, DeleteGraphRelations or InsertRelations. That is the whole
// safety argument, and it is worth keeping true: a dry run that could touch a graph would be a
// dry run an analyst has to think about before pressing.

// DryRunRequest asks what a rule would produce. Source is the rule text as the editor holds
// it — not a saved rule — so an author can try something that is not on disk yet.
type DryRunRequest struct {
	Source string               `json:"source"`
	Filter graphene.EventFilter `json:"-"`
	// Samples caps how many matched occurrences are returned in full. The counts always cover
	// the whole run; this bounds only what is shipped back for display.
	Samples int `json:"samples"`
}

// DryRunEvent is the scalar summary of one matched event. It carries no raw record: the cold
// store is not read, because a testbench showing twenty matches would otherwise pay twenty
// payload reads for information the author is not looking at.
type DryRunEvent struct {
	ID        uint64    `json:"id"`
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	Computer  string    `json:"computer"`
	Provider  string    `json:"provider"`
	Channel   string    `json:"channel"`
	User      string    `json:"user"`
}

// DryRunMatch is one occurrence: the events it joined, and why.
type DryRunMatch struct {
	MatchID string        `json:"match_id"`
	Basis   []string      `json:"basis"`
	Events  []DryRunEvent `json:"events"`
}

// DryRunResult is what the testbench shows.
//
// The counts are as important as the matches. A rule that returns three results because the
// pattern is rare and one that returns three because most of the case could not be considered
// look identical unless the run says which it was — so SkippedNoKeys, StaleCorrelationKeys and
// UnresolvedParents are first-class here rather than diagnostics.
type DryRunResult struct {
	// Valid and Problems come from the same validator that decides whether a saved rule loads.
	Valid     bool                   `json:"valid"`
	Problems  rules.ValidationReport `json:"problems"`
	Matches   int                    `json:"matches"`
	Relations int                    `json:"relations"`
	Truncated bool                   `json:"truncated"`
	Dropped   int                    `json:"dropped"`
	// Events is how many the filter selected and the engine could actually use.
	Events               int `json:"events"`
	SkippedUndated       int `json:"skipped_undated"`
	SkippedNoKeys        int `json:"skipped_no_keys"`
	UnresolvedParents    int `json:"unresolved_parents"`
	StaleCorrelationKeys int `json:"stale_correlation_keys"`
	// ElapsedMs times the evaluation only, not the validation or the dataset read, so tuning a
	// rule shows the rule's own cost rather than the cache's warmth.
	ElapsedMs int64         `json:"elapsed_ms"`
	Samples   []DryRunMatch `json:"samples"`
}

// datasetCache holds ONE prepared dataset.
//
// Capacity is one deliberately. The dataset is the whole matching event set for a case, so
// holding several would trade the memory discipline the cold store exists to protect for a hit
// rate nothing needs: the testbench re-runs the same filter over and over while a rule is being
// tuned, which one entry serves completely.
//
// It is invalidated by the store's own write counter rather than by anything this package
// tracks, so a cached dataset cannot survive an ingest — the same mechanism, and the same
// guarantee, as the order cache it is keyed alongside.
type datasetCache struct {
	mu      sync.Mutex
	key     string
	version uint64
	ds      *autograph.Dataset
	events  int
}

// get returns a dataset for the filter and requirements, preparing one when what is held does
// not match. The bool reports whether the cache was used, which the tests assert on.
func (c *datasetCache) get(store *graphene.Store, filter graphene.EventFilter, req autograph.Requirements) (*autograph.Dataset, int, bool, error) {
	key := filter.CacheKey() + "\x1f" + req.Fingerprint()
	version := store.Version()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ds != nil && c.key == key && c.version == version {
		return c.ds, c.events, true, nil
	}

	events, err := store.QueryEvents(filter)
	if err != nil {
		return nil, 0, false, err
	}
	ds := autograph.Prepare(events, req)

	// Re-read the version AFTER the work: a write that landed while the query was running
	// would make this dataset stale the moment it was stored, and serving it later would show
	// an author results computed against events that have since changed. Caching only when the
	// version held still means a racing write costs a rebuild rather than a wrong answer —
	// the same reasoning the order cache uses.
	if store.Version() == version {
		c.key, c.version, c.ds, c.events = key, version, ds, len(events)
	}
	return ds, len(events), false, nil
}

// invalidate drops the held dataset. A build calls it because a build writes relations, and
// while relations are not part of a dataset, letting a stale entry outlive a run that changed
// the store is the kind of thing that is only wrong later.
func (c *datasetCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key, c.version, c.ds, c.events = "", 0, nil, 0
}

// DryRun evaluates rule text against the case and reports what it would produce, WITHOUT
// writing anything.
//
// An invalid rule is not an error: text that does not yet parse is the normal state while
// somebody is writing it. The report comes back with Valid false and the same located problems
// the editor already shows, and no evaluation is attempted — running a rule the loader would
// refuse would tell the author about a rule they cannot save.
func (b *Builder) DryRun(req DryRunRequest) (DryRunResult, error) {
	var out DryRunResult

	report := rules.ValidateSource([]byte(req.Source))
	out.Problems = report
	out.Valid = report.Valid
	if !report.Valid || report.Normalized == nil {
		return out, nil
	}
	spec := report.Normalized

	// The same scoping a build applies: the whole matching set, never a page, and undated
	// events excluded because correlation is time-ordered.
	filter := req.Filter
	filter.Offset = 0
	filter.Limit = 0
	filter.Undated = consts.UndatedExclude

	reqs := autograph.RequirementsFor([]*rules.Spec{spec})
	ds, events, _, err := b.datasets.get(b.store, filter, reqs)
	if err != nil {
		return out, err
	}
	out.Events = events

	started := time.Now()
	gen := autograph.GenerateWith(spec, ds)
	out.ElapsedMs = time.Since(started).Milliseconds()

	out.Matches, out.Relations = gen.Matches, len(gen.Relations)
	out.Truncated, out.Dropped = gen.Truncated, gen.Dropped
	out.SkippedUndated, out.SkippedNoKeys = gen.SkippedUndated, gen.SkippedNoKeys
	out.UnresolvedParents = gen.UnresolvedParents
	out.StaleCorrelationKeys = gen.StaleCorrelationKeys
	out.Samples = sampleMatches(ds, gen.Relations, req.Samples)
	return out, nil
}

// sampleMatches turns the first N occurrences into displayable chains.
//
// Relations arrive grouped by occurrence through their match id, and in emission order, so the
// first N distinct ids are the first N matches — no sorting needed, and the sample is stable
// across runs for the same reason the match id is.
func sampleMatches(ds *autograph.Dataset, relations []graphene.Relation, limit int) []DryRunMatch {
	if limit <= 0 || len(relations) == 0 {
		return nil
	}

	order := make([]string, 0, limit)
	byMatch := map[string][]graphene.Relation{}
	for _, rel := range relations {
		if _, seen := byMatch[rel.MatchID]; !seen {
			if len(order) == limit {
				continue // past the sample; the COUNTS above still cover the whole run
			}
			order = append(order, rel.MatchID)
		}
		byMatch[rel.MatchID] = append(byMatch[rel.MatchID], rel)
	}

	// Only the events the samples actually reference are looked up, so a 100k-event dataset
	// costs one pass rather than a map of everything.
	needed := map[uint64]bool{}
	for _, id := range order {
		for _, rel := range byMatch[id] {
			needed[rel.From], needed[rel.To] = true, true
		}
	}
	found := make(map[uint64]*graphene.Event, len(needed))
	for _, e := range ds.Events {
		if needed[e.ID] {
			found[e.ID] = e
		}
	}

	out := make([]DryRunMatch, 0, len(order))
	for _, id := range order {
		rels := byMatch[id]
		match := DryRunMatch{MatchID: id, Basis: rels[0].Basis}
		// A chain of n events is n-1 edges: walk the froms, then close with the final to.
		for _, rel := range rels {
			if e := found[rel.From]; e != nil {
				match.Events = append(match.Events, summarize(e))
			}
		}
		if e := found[rels[len(rels)-1].To]; e != nil {
			match.Events = append(match.Events, summarize(e))
		}
		out = append(out, match)
	}
	return out
}

func summarize(e *graphene.Event) DryRunEvent {
	return DryRunEvent{
		ID:        e.ID,
		EventID:   e.EventID,
		Timestamp: e.Timestamp,
		Computer:  e.Computer,
		Provider:  e.Provider,
		Channel:   e.Channel,
		User:      e.User,
	}
}
