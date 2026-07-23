package api

import (
	"time"

	"rohy/backend/consts"
	"rohy/backend/findings"
	"rohy/backend/graphene"
)

// FindingsAPI is the Wails binding over the analyst findings sidecar (P25). It carries no
// logic of its own: normalization, validation, and persistence live in the findings package,
// and this struct only adapts them to the binding layer and stamps the clock.
//
// Timestamps are stamped here rather than accepted from the caller, for the same reason
// relation provenance is: a record of when a judgement was made is only worth keeping if the
// frontend cannot choose it.
type FindingsAPI struct {
	store *findings.Store
	// events is needed only to reconcile findings against the events actually in the case
	// (see AuditFindings). The findings package itself never learns what an event is.
	events *graphene.Store
}

// NewFindingsAPI constructs the binding over an open findings store and the event store it
// reconciles against.
func NewFindingsAPI(store *findings.Store, events *graphene.Store) *FindingsAPI {
	return &FindingsAPI{store: store, events: events}
}

// FindingRequest writes one annotation. Key is the event's hash_normalized — its content
// identity — which the frontend already has on every event it renders.
type FindingRequest struct {
	Key        string   `json:"key"`
	Flagged    bool     `json:"flagged"`
	Tags       []string `json:"tags"`
	Note       string   `json:"note"`
	Descriptor string   `json:"descriptor"`
}

// GetFinding returns one event's finding, or null when it carries none.
func (a *FindingsAPI) GetFinding(key string) *findings.Finding {
	return a.store.Get(key)
}

// GetFindings returns the findings for a set of events, keyed by event hash. The events list
// calls this once per page instead of once per row.
func (a *FindingsAPI) GetFindings(keys []string) map[string]*findings.Finding {
	return a.store.GetMany(keys)
}

// SetFinding writes an annotation and returns the stored result — or null when the request
// cleared the last of its content, in which case the finding is removed rather than kept as
// an empty shell that would still count as annotated.
func (a *FindingsAPI) SetFinding(req FindingRequest) (*findings.Finding, error) {
	f, err := a.store.Set(findings.Finding{
		Key:        req.Key,
		Flagged:    req.Flagged,
		Tags:       req.Tags,
		Note:       req.Note,
		Descriptor: req.Descriptor,
	}, time.Now().UTC())
	if err != nil {
		return nil, AsError(findingErrorCode(err), err)
	}
	return f, nil
}

// RemoveFinding deletes an event's annotation.
func (a *FindingsAPI) RemoveFinding(key string) error {
	if err := a.store.Remove(key); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// ListFindings returns every finding, most recently updated first. This is the analyst's
// own worklist, and the backbone of anything that reports on the case.
func (a *FindingsAPI) ListFindings() []*findings.Finding {
	return a.store.List()
}

// ListTags returns the tags in use with their counts, so the UI can offer the vocabulary
// already in the case instead of letting each event invent a new spelling.
func (a *FindingsAPI) ListTags() []findings.TagCount {
	return a.store.Tags()
}

// FindingStats returns the case's finding counts for the dashboard. It reads only the
// sidecar, so it is cheap enough to call after every write.
func (a *FindingsAPI) FindingStats() findings.Summary {
	return a.store.Stats()
}

// FindingsAudit reconciles the sidecar against the events actually in the case.
//
// Findings outlive the events they describe: clearing the store and ingesting a different
// dataset into the same case folder leaves every previous finding on disk, keyed to hashes
// no event can produce. The queries stay correct — a phantom hash simply matches nothing —
// but the counts would claim work that is not there, so the case has to be able to say
// "9 of these 12 findings refer to events that are not here" rather than quietly inflating
// its own numbers.
//
// Orphans are reported, never auto-deleted. Re-ingesting the missing source brings the
// events back and the findings reattach; deleting them would destroy irreplaceable analyst
// work to tidy a count.
type FindingsAudit struct {
	Total   int                 `json:"total"`
	Live    int                 `json:"live"`
	Orphans []*findings.Finding `json:"orphans"`
	// Stale reports that the sidecar was written against a different hash_normalized recipe
	// than this build produces. When true EVERY finding orphans at once, and the cause is the
	// build rather than the data — a distinction the UI has to be able to draw.
	Stale       bool `json:"stale"`
	HashVersion int  `json:"hash_version"`
}

// AuditFindings resolves every finding's key against the event store. Cost tracks the number
// of findings (analyst-authored, so small) rather than the size of the case, because each key
// is an indexed lookup rather than a scan.
func (a *FindingsAPI) AuditFindings() (FindingsAudit, error) {
	all := a.store.List()
	out := FindingsAudit{
		Total:       len(all),
		Orphans:     []*findings.Finding{},
		Stale:       a.store.Stale(),
		HashVersion: a.store.HashVersion(),
	}
	if a.events == nil {
		return out, nil
	}
	for _, f := range all {
		_, ok, err := a.events.FindEventIDByHash(f.Key)
		if err != nil {
			return out, AsError(consts.ErrCodePersistence, err)
		}
		if ok {
			out.Live++
			continue
		}
		out.Orphans = append(out.Orphans, f)
	}
	return out, nil
}

// findingErrorCode maps a store error onto the uniform error vocabulary. A refused note or a
// missing key is the caller's input being wrong, not the disk failing, and the frontend
// shows a different message for each.
func findingErrorCode(err error) string {
	switch err {
	case findings.ErrNoKey, findings.ErrTooLong:
		return consts.ErrCodeParse
	default:
		return consts.ErrCodePersistence
	}
}
