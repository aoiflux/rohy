// Package graphbuild is the rule-driven graph creation workflow (P6): it is the only
// place where rules, events, the auto-graphing engine, and persistence meet. Given a rule
// selection and a dataset scope (all events or the active forensic filter), it runs each
// rule's algorithm and writes the resulting edges into that rule's own graph — "one rule =
// one graph, N rules = N graphs".
//
// Rebuilds are idempotent by construction: a rule's graph is located by rule id, its
// existing edges are cleared, and the fresh result is written. Re-running therefore
// replaces rather than appends, so a graph can never accumulate duplicates.
package graphbuild

import (
	"context"
	"time"

	"rohy/backend/autograph"
	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
)

// Request selects what to build. An empty RuleIDs runs every enabled rule; naming rules
// explicitly runs exactly those, whether or not they are enabled (an explicit pick is a
// stronger signal than the toggle). Filter scopes the dataset — the zero filter means all
// events.
type Request struct {
	RuleIDs []string             `json:"rule_ids"`
	Filter  graphene.EventFilter `json:"-"`
}

// RuleOutcome reports what one rule produced. Removed is the number of pre-existing edges
// cleared for the rebuild; Truncated/Dropped surface an engine match cap so a large run is
// never silently truncated.
type RuleOutcome struct {
	RuleID    string `json:"rule_id"`
	RuleName  string `json:"rule_name"`
	GraphID   uint64 `json:"graph_id"`
	GraphName string `json:"graph_name"`
	Matches   int    `json:"matches"`
	Relations int    `json:"relations"`
	Removed   int    `json:"removed"`
	Truncated bool   `json:"truncated"`
	Dropped   int    `json:"dropped"`
}

// Result is the outcome of a whole run: one entry per rule, plus the size of the dataset
// the rules were evaluated against.
type Result struct {
	Outcomes []RuleOutcome `json:"outcomes"`
	Events   int           `json:"events"`
	// SkippedUndated is how many events matching the filter were left out because they have
	// no timestamp and so cannot take part in time-ordered correlation. Reported so a run
	// that "missed" events can explain itself instead of looking arbitrary (P23.5).
	SkippedUndated int `json:"skipped_undated"`
	// RepairedRelations is how many relations this run had to re-index before it could
	// start, because a previous run was interrupted between committing its edges and
	// registering their index entries. Normally zero. A non-zero value is not an error, but
	// it does mean a previous build did not finish, so it is surfaced rather than absorbed.
	RepairedRelations int `json:"repaired_relations"`
}

// Progress reports how far a run has got, so a long build can show movement rather than
// leaving the UI frozen on "running" until everything finishes.
type Progress struct {
	Rule      string `json:"rule"`
	RuleIndex int    `json:"rule_index"` // 1-based, for "3 of 8"
	RuleTotal int    `json:"rule_total"`
	Relations int    `json:"relations"` // cumulative across the run
	Events    int    `json:"events"`
}

// Reporter receives progress callbacks. The API layer adapts these to Wails events; tests
// use a capturing implementation or nil.
type Reporter interface {
	Progress(Progress)
}

// Builder wires the three stores the workflow composes. It owns no state of its own.
type Builder struct {
	store  *graphene.Store
	graphs *graphreg.Store
	rules  *rules.Registry
}

// New constructs the workflow over the open stores.
func New(store *graphene.Store, graphs *graphreg.Store, registry *rules.Registry) *Builder {
	return &Builder{store: store, graphs: graphs, rules: registry}
}

// Run executes the request. The dataset is read once and shared by every rule, so N rules
// cost one query rather than N. Cancellation is honoured between rules and between edge
// writes, and a cancelled run returns what it completed so far along with ctx.Err().
func (b *Builder) Run(ctx context.Context, req Request, now time.Time) (Result, error) {
	return b.RunWithProgress(ctx, req, now, nil)
}

// RunWithProgress is Run with per-rule progress callbacks. Progress is reported AFTER each
// rule completes rather than during it: a rule is the smallest unit whose result is
// meaningful, and inventing sub-rule percentages would be precision the workflow does not
// actually have.
func (b *Builder) RunWithProgress(ctx context.Context, req Request, now time.Time, reporter Reporter) (Result, error) {
	var res Result

	selected, err := b.selectRules(req.RuleIDs)
	if err != nil {
		return res, err
	}
	if len(selected) == 0 {
		return res, nil
	}

	// Repair any relation whose index entries never got registered, before clearing
	// anything. This is the recovery point for a build interrupted between committing its
	// edges and indexing them: such a relation is invisible to graph-scoped queries, so the
	// clear below would step straight past it and the rebuild would silently add a second
	// copy alongside it. Repairing first makes the previous run's leftovers visible, which
	// is what keeps the rebuild genuinely idempotent across a crash.
	//
	// It runs ONCE per build rather than per rule, so its cost is amortized across the whole
	// run, and it does nothing at all on a healthy store. A failure to repair is not worth
	// failing the build over — it costs idempotency in a rare case, not correctness of the
	// relations about to be written — so it is reported and the run continues.
	if repaired, err := b.store.RepairRelationIndex(); err == nil {
		res.RepairedRelations = repaired
	}

	// Read the dataset once. Offset/limit are cleared: a build always evaluates the whole
	// matching set, never just the page the UI happens to be showing.
	//
	// Undated events are excluded EXPLICITLY rather than relying on the caller's filter.
	// The events view now includes them by default (P23), so a filter arriving from the UI
	// would otherwise feed timeless records into a time-ordered matcher. The exclusion is
	// stated here, and the algorithm guards it again independently.
	filter := req.Filter
	filter.Offset = 0
	filter.Limit = 0
	filter.Undated = consts.UndatedExclude
	events, err := b.store.QueryEvents(filter)
	if err != nil {
		return res, err
	}
	res.Events = len(events)

	// How many the filter matched but correlation cannot use. A failure to count is not
	// worth failing the run over — it only costs the explanation.
	undatedFilter := filter
	undatedFilter.Undated = consts.UndatedOnly
	if n, err := b.store.CountEvents(undatedFilter); err == nil {
		res.SkippedUndated = n
	}

	relations := 0
	for i, rule := range selected {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		outcome, err := b.runRule(ctx, rule, events, now)
		if err != nil {
			return res, err
		}
		res.Outcomes = append(res.Outcomes, outcome)
		relations += outcome.Relations

		if reporter != nil {
			reporter.Progress(Progress{
				Rule:      rule.Name,
				RuleIndex: i + 1,
				RuleTotal: len(selected),
				Relations: relations,
				Events:    res.Events,
			})
		}
	}
	return res, nil
}

// runRule builds one rule's graph: locate/create it, clear it, generate, persist.
func (b *Builder) runRule(ctx context.Context, rule *rules.Rule, events []*graphene.Event, now time.Time) (RuleOutcome, error) {
	out := RuleOutcome{RuleID: rule.ID, RuleName: rule.Name}

	graph, err := b.graphs.EnsureForRule(rule.ID, rule.Name, rule.Description, now)
	if err != nil {
		return out, err
	}
	out.GraphID, out.GraphName = graph.ID, graph.Name

	// Idempotent rebuild: drop what a previous run wrote before writing the new result.
	removed, err := b.store.DeleteGraphRelations(graph.ID)
	if err != nil {
		return out, err
	}
	out.Removed = removed

	gen := autograph.Generate(&rule.Spec, events)
	out.Matches, out.Truncated, out.Dropped = gen.Matches, gen.Truncated, gen.Dropped

	// Persist in chunks rather than one relation at a time. Each write is durable, so a
	// per-relation loop pays a separate committed write for every edge; batching folds a
	// chunk into a single commit. The chunk size bounds the memory a commit buffers and
	// stays the point at which cancellation is honoured, so batching does not cost the
	// build its responsiveness.
	batch := make([]*graphene.Relation, 0, consts.RelationBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := b.store.InsertRelations(batch); err != nil {
			return err
		}
		out.Relations += len(batch)
		batch = batch[:0]
		return nil
	}

	for i := range gen.Relations {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		rel := gen.Relations[i]
		rel.GraphID = graph.ID
		rel.CreatedAt = now
		batch = append(batch, &rel)
		if len(batch) >= consts.RelationBatchSize {
			if err := flush(); err != nil {
				return out, err
			}
		}
	}
	if err := flush(); err != nil {
		return out, err
	}
	return out, nil
}

// selectRules resolves the requested ids, or returns every enabled rule when none are
// named. An unknown id is an error rather than a silent skip, so a stale UI selection is
// reported instead of quietly producing nothing.
func (b *Builder) selectRules(ids []string) ([]*rules.Rule, error) {
	if len(ids) == 0 {
		return b.rules.Enabled(), nil
	}
	byID := map[string]*rules.Rule{}
	for _, rule := range b.rules.List() {
		byID[rule.ID] = rule
	}
	out := make([]*rules.Rule, 0, len(ids))
	for _, id := range ids {
		rule, ok := byID[id]
		if !ok {
			return nil, rules.ErrRuleNotFound
		}
		out = append(out, rule)
	}
	return out, nil
}
