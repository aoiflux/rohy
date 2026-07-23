// Package api is rohy's Wails binding surface: the thin boundary the Svelte
// frontend calls. Binding methods contain NO business logic — they validate/adapt
// arguments and delegate to the ingestion (evtx) and persistence (graphene) layers,
// and they surface progress/errors/lifecycle as Wails runtime events. This file is
// the executable contract catalog (P0.3): every request/response DTO, the uniform
// error shape, the event channel set, and the Emitter abstraction live here.
package api

import (
	"rohy/backend/consts"
	"rohy/backend/evtx"
)

// Emitter abstracts Wails' runtime.EventsEmit so the binding layer can be unit
// tested without a live application. The production implementation (WailsEmitter)
// forwards to the Wails runtime; tests inject a capturing fake.
type Emitter interface {
	Emit(channel string, data interface{})
}

// noopEmitter is the default before the app has started (or in tests that ignore
// events). It discards everything.
type noopEmitter struct{}

func (noopEmitter) Emit(string, interface{}) {}

// --- Event contract catalog (backend → frontend) ---
//
// Channel names are defined once in consts and reused here so the JS side and Go
// side cannot drift. Payload shapes:
//
//	consts.EventIngestStarted   → StartedEvent
//	consts.EventIngestProgress  → evtx.Progress
//	consts.EventIngestError     → ErrorEvent   (non-fatal, per-record/chunk)
//	consts.EventIngestComplete  → evtx.Summary
//	consts.EventIngestCancelled → evtx.Summary
//	consts.EventPermissionWarn  → evtx.AccessDecision

// StartedEvent announces the beginning of one file's ingestion.
//
// FileIndex/FileTotal place that file within the whole request. A folder or multi-select
// ingest runs one evtx.Ingest per file, each with its own chunk total and its own
// completion — so without this the UI can only ever describe the file in front of it, and
// cannot say how much of the JOB is done. They are 1-based and equal (1, 1) for a single
// file, so a caller can treat every run the same way.
type StartedEvent struct {
	Source      string `json:"source"`
	Path        string `json:"path"`
	ChunksTotal int    `json:"chunks_total"`
	FileIndex   int    `json:"file_index"`
	FileTotal   int    `json:"file_total"`
}

// ErrorEvent is the uniform error shape for every backend error surfaced to the
// frontend, whether emitted as an event or returned from a binding (see AsError).
type ErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e ErrorEvent) Error() string { return e.Message }

// AsError wraps a Go error in the uniform shape with the given code.
func AsError(code string, err error) ErrorEvent {
	return ErrorEvent{Code: code, Message: err.Error()}
}

// --- Binding request DTOs ---

// IngestRequest starts an ingestion job. Source selects the pipeline; Paths lists
// one or more .evtx files (file source); Channels lists live channels (live source,
// Windows only). Idempotent enables hash-based duplicate skipping for resume.
type IngestRequest struct {
	Source     string   `json:"source"`
	Paths      []string `json:"paths"`
	Channels   []string `json:"channels"`
	Idempotent bool     `json:"idempotent"`
	// Continuous turns a live run into an open-ended capture that keeps streaming new
	// records until it is stopped, resuming each channel from its durable bookmark (P7).
	Continuous bool `json:"continuous"`
}

// CaptureStatus reports the live-capture session for the UI's capture indicator: whether
// a run is active, whether it is continuous, which channels it covers, and the durable
// per-channel positions reached so far.
type CaptureStatus struct {
	Active     bool              `json:"active"`
	Continuous bool              `json:"continuous"`
	Channels   []string          `json:"channels"`
	Positions  map[string]uint64 `json:"positions"`
}

// EventQuery is the forensic filter contract. Times are RFC3339 strings (empty =
// unbounded) so the JS/Go boundary stays explicit; the binding parses them into the
// persistence layer's time-range filter.
type EventQuery struct {
	EventID           string `json:"event_id"`
	Provider          string `json:"provider"`
	Channel           string `json:"channel"`
	User              string `json:"user"`
	TimeFrom          string `json:"time_from"`
	TimeTo            string `json:"time_to"`
	Search            string `json:"search"`
	SourceType        string `json:"source_type"`
	SourceIdentifier  string `json:"source_identifier"`
	MinDuplicateCount int    `json:"min_duplicate_count"`
	// RelationState is the relation-aware quick filter (consts.RelationFilter*): show only
	// events that have relations, or only those correlated by a rule (P11).
	RelationState string `json:"relation_state"`
	// Undated controls whether events with no timestamp are shown (consts.Undated*). They
	// are excluded by default because they cannot be placed on a timeline (P22).
	Undated string `json:"undated"`
	// FindingState narrows to the analyst's own marks (consts.FindingFilter*) and Tag to a
	// single tag (P25). They are resolved against the findings sidecar into a set of content
	// hashes before the query runs, so the persistence layer never learns what a finding is.
	FindingState string `json:"finding_state"`
	Tag          string `json:"tag"`
	Offset        int    `json:"offset"`
	Limit         int    `json:"limit"`
	Descending    bool   `json:"descending"`
}

// RelationUpdate edits an existing relation's type/label/confidence in place. The
// endpoints are immutable; to reconnect, delete and recreate.
type RelationUpdate struct {
	ID              uint64  `json:"id"`
	RelationType    string  `json:"relation_type"`
	Label           string  `json:"relation_label"`
	ConfidenceScore float64 `json:"confidence_score"`
}

// StatsResult reports high-level store counts for dashboard display.
type StatsResult struct {
	Events    uint64 `json:"events"`
	Relations uint64 `json:"relations"`
}

// RelationRequest creates a mapped edge between two persisted events. CreatedAt is
// stamped by the backend, not the caller, so provenance timestamps are trustworthy.
// GraphID scopes the edge to a named graph (0 = the active graph).
type RelationRequest struct {
	From            uint64  `json:"from"`
	To              uint64  `json:"to"`
	GraphID         uint64  `json:"graph_id"`
	RelationType    string  `json:"relation_type"`
	Label           string  `json:"relation_label"`
	ConfidenceScore float64 `json:"confidence_score"`
	CreatedBy       string  `json:"created_by"`
}

// GraphRequest creates or renames a named graph (multiple-graphs, P15). ID is ignored
// on create and required on rename.
type GraphRequest struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ingestReporter adapts the evtx.Reporter callbacks into Wails runtime events on the
// consts channels. evtx guarantees these callbacks fire from a single goroutine, so
// the adapter needs no synchronization.
type ingestReporter struct {
	emitter Emitter
	source  string
	path    string
	// fileIndex/fileTotal position this file within the whole request (1-based). A live
	// capture leaves them zero: it is not a file at all, and reporting it as "1 of 1" would
	// invite a progress bar over something that has no end.
	fileIndex int
	fileTotal int
}

func (r *ingestReporter) Started(source string, chunksTotal int) {
	r.emitter.Emit(consts.EventIngestStarted, StartedEvent{
		Source: source, Path: r.path, ChunksTotal: chunksTotal,
		FileIndex: r.fileIndex, FileTotal: r.fileTotal,
	})
}
func (r *ingestReporter) Progress(p evtx.Progress) {
	r.emitter.Emit(consts.EventIngestProgress, p)
}
func (r *ingestReporter) RecordError(code, message string) {
	r.emitter.Emit(consts.EventIngestError, ErrorEvent{Code: code, Message: message})
}
func (r *ingestReporter) Completed(s evtx.Summary) {
	r.emitter.Emit(consts.EventIngestComplete, s)
}
func (r *ingestReporter) Cancelled(s evtx.Summary) {
	r.emitter.Emit(consts.EventIngestCancelled, s)
}
