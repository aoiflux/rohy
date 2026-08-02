package graphene

import (
	"context"
	"encoding/json"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/store"
)

// Correlation-key backfill.
//
// The projection is computed at ingest time, so every event ingested by this build already
// has it. Events ingested by an earlier build do not, and neither do events whose projection
// was written by a different vocabulary. Without a way to fill those in, correlating on
// fields in an existing case would return a small number of matches and look like a clean
// result — which is the failure mode this project treats as worse than an error.
//
// Three properties, each deliberate:
//
//   - OPT-IN, never a startup step. The work is proportional to the whole store and pays a
//     cold-store read per event. That is the same reason VerifyIndexes is not run on every
//     open: a cost every user pays on every launch, to redo something that is almost always
//     already done, is not a safety feature.
//   - RESUMABLE, by construction rather than by bookkeeping. Each event carries the recipe
//     version it was projected under, so a run skips what is already current. A cancelled run
//     is resumed by running it again — there is no checkpoint file to keep honest.
//   - IDEMPOTENT. A second run over a finished store reads nothing and writes nothing.

// BackfillResult reports what a run did. Projected + AlreadyCurrent + Failed == Examined.
type BackfillResult struct {
	// Examined is how many event nodes the run looked at.
	Examined int `json:"examined"`
	// Projected is how many events were given (or re-given) a correlation projection.
	Projected int `json:"projected"`
	// AlreadyCurrent is how many were skipped because they were projected by this recipe.
	AlreadyCurrent int `json:"already_current"`
	// Failed is how many could not be projected because their payload could not be read.
	// They are LEFT ALONE rather than stamped: stamping would record "projected, nothing
	// found", which is a different and false claim, and would stop a later run retrying them.
	Failed int `json:"failed"`
	// Cancelled reports that the run stopped early. What it completed is durable, and
	// re-running continues from there.
	Cancelled bool `json:"cancelled"`
}

// CorrelationKeyStatus counts how much of the store carries a current projection.
//
// It reads the minimal per-node view rather than whole events, so asking "does this case need
// a backfill" does not itself cost a full decode of every record.
type CorrelationKeyStatus struct {
	Total   int `json:"total"`
	Current int `json:"current"`
	Stale   int `json:"stale"`
}

// NeedsBackfill reports whether any event lacks a current projection. A field, temporal or
// lineage build over such a store under-reports, so the run says so rather than returning the
// short answer as if it were the whole one.
func (st CorrelationKeyStatus) NeedsBackfill() bool { return st.Stale > 0 }

// correlationStatusView is the slice of a stored event this status check needs. Decoding into
// it instead of Event is what keeps the check cheap: it never allocates the hashes, the source
// breakdown, or the payload reference.
type correlationStatusView struct {
	CorrKeyVersion int `json:"ckv"`
}

// CorrelationKeyStatus reports how many events carry a projection from the current recipe.
func (s *Store) CorrelationKeyStatus() (CorrelationKeyStatus, error) {
	var st CorrelationKeyStatus
	g, err := s.graph()
	if err != nil {
		return st, err
	}
	ids, err := g.QueryNodeIDs(store.NodeQuery{Types: []store.NodeType{consts.NodeEvent}})
	if err != nil {
		return st, err
	}
	st.Total = len(ids)
	if st.Total == 0 {
		return st, nil
	}

	for start := 0; start < len(ids); start += consts.EventBatchSize {
		end := min(start+consts.EventBatchSize, len(ids))
		nodes, _, err := g.GetNodes(ids[start:end])
		if err != nil {
			return st, err
		}
		for _, n := range nodes {
			var v correlationStatusView
			if len(n.Properties) > 0 {
				if err := json.Unmarshal(n.Properties, &v); err != nil {
					return st, err
				}
			}
			if v.CorrKeyVersion == consts.CorrelationKeyVersion {
				st.Current++
			} else {
				st.Stale++
			}
		}
	}
	return st, nil
}

// BackfillCorrelationKeys projects events that do not yet carry a current correlation
// projection, reading each one's parsed fields back from the cold store.
//
// progress may be nil. When set it is called after each batch with (done, total) so a long
// run can show movement; it is called from this goroutine, so it needs no synchronization.
//
// A cancelled run returns what it completed along with ctx.Err(). The completed part is
// durable and a later run resumes from it.
func (s *Store) BackfillCorrelationKeys(ctx context.Context, progress func(done, total int)) (BackfillResult, error) {
	var res BackfillResult
	g, err := s.graph()
	if err != nil {
		return res, err
	}
	ids, err := g.QueryNodeIDs(store.NodeQuery{Types: []store.NodeType{consts.NodeEvent}})
	if err != nil {
		return res, err
	}
	if len(ids) == 0 {
		return res, nil
	}

	// Any write invalidates a cached ordering. The projection is not an ordering or filtering
	// input today, so this is defensive rather than load-bearing — but every write path in
	// this package bumps, and a path that quietly does not is exactly how the one that matters
	// gets missed later.
	defer s.bumpVersion()

	for start := 0; start < len(ids); start += consts.EventBatchSize {
		if err := ctx.Err(); err != nil {
			res.Cancelled = true
			return res, err
		}
		end := min(start+consts.EventBatchSize, len(ids))
		batch, err := s.backfillBatch(g, ids[start:end])
		res.Examined += batch.Examined
		res.Projected += batch.Projected
		res.AlreadyCurrent += batch.AlreadyCurrent
		res.Failed += batch.Failed
		if err != nil {
			return res, err
		}
		if progress != nil {
			progress(end, len(ids))
		}
	}
	return res, nil
}

// backfillBatch projects one bounded batch and commits it in a single transaction.
//
// One commit per batch rather than one per event, for the reason the whole write layer is
// shaped around: a durable commit is what crash-safety costs and it is not tunable, so the
// number of commits is the cost. The correlation keys are not an indexed property, so the
// record can be replaced inside a transaction with no index entry to register alongside it —
// the same reason IncrementDedupCounts can batch.
func (s *Store) backfillBatch(g *graphene.Graph, ids []store.NodeID) (BackfillResult, error) {
	var res BackfillResult

	nodes, _, err := g.GetNodes(ids)
	if err != nil {
		return res, err
	}

	tx := g.Begin()
	pending := 0
	for _, n := range nodes {
		res.Examined++
		e, err := eventFromNode(n)
		if err != nil {
			return res, err
		}
		if e.HasCurrentCorrelationKeys() {
			res.AlreadyCurrent++
			continue
		}
		// The parsed fields live in the cold store, which is exactly why this is a separate
		// maintenance pass and not something a query can do on the fly.
		if err := s.hydrate(e); err != nil {
			// One unreadable payload must not abort the case's backfill. The event is left
			// unstamped so a later run retries it, and the count says how many there were.
			res.Failed++
			continue
		}
		e.ComputeCorrelationKeys()

		updated, err := e.toNode()
		if err != nil {
			return res, err
		}
		updated.ID = n.ID
		tx.UpdateNode(updated)
		pending++
		res.Projected++
	}

	if pending == 0 {
		return res, nil
	}
	if err := tx.Commit(); err != nil {
		// Nothing in the batch was applied, so nothing was projected.
		res.Projected -= pending
		return res, err
	}
	return res, nil
}
