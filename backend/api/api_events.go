package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"rohy/backend/consts"
	"rohy/backend/evtx"
	"rohy/backend/findings"
	"rohy/backend/graphene"
)

// EventsAPI is the Wails binding for event ingestion and querying. It owns the
// lifecycle of the single active ingestion job (start/cancel) and exposes thin
// query delegates over the persistence layer. It holds no parsing/persistence logic
// of its own — everything is delegated to evtx and graphene.
type EventsAPI struct {
	store     *graphene.Store
	positions evtx.PositionStore
	// findings resolves the analyst-findings filters into content hashes before a query runs
	// (P25). It may be nil in tests that do not exercise those filters.
	findings  *findings.Store
	mu        sync.Mutex
	emitter   Emitter
	appCtx    context.Context
	cancel    context.CancelFunc
	running   bool
	// Live-capture session state, mirrored for the capture indicator (P7).
	liveChannels   []string
	liveContinuous bool
	// Ingestion lifecycle (P8). state is backend-authoritative — the frontend renders it
	// rather than inferring it. gate pauses the running pipeline; done is closed when the
	// run goroutine exits, so shutdown can wait for a clean flush.
	state string
	gate  *evtx.Gate
	done  chan struct{}
}

// NewEventsAPI constructs the binding over an open store, the durable capture bookmark
// store, and the analyst-findings sidecar (which may be nil where findings filters are not
// used). Until SetEmitter is called (at app startup) events are discarded.
func NewEventsAPI(store *graphene.Store, positions evtx.PositionStore, findingStore *findings.Store) *EventsAPI {
	return &EventsAPI{
		store:     store,
		positions: positions,
		findings:  findingStore,
		emitter:   noopEmitter{},
		state:     consts.IngestStateIdle,
	}
}

// setState records the new lifecycle state and announces it, so the UI never has to infer
// "paused" from progress going quiet. Callers must NOT hold a.mu.
func (a *EventsAPI) setState(state string) {
	a.mu.Lock()
	if a.state == state {
		a.mu.Unlock()
		return
	}
	a.state = state
	emitter := a.emitter
	a.mu.Unlock()
	emitter.Emit(consts.EventIngestState, state)
}

// IngestState reports the current lifecycle state (consts.IngestState*). It is the single
// source of truth for the UI's ingestion controls.
func (a *EventsAPI) IngestState() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// PauseIngestion halts the running pipeline at its next batch boundary. The in-flight
// batch is written and its capture position persisted before the pipeline idles, so a
// pause always leaves the store at a consistent point — even if the app is closed while
// paused.
func (a *EventsAPI) PauseIngestion() error {
	a.mu.Lock()
	gate, running := a.gate, a.running
	a.mu.Unlock()
	if !running || gate == nil {
		return AsError(consts.ErrCodeInternal, errors.New(consts.MsgNoIngestionRunning))
	}
	gate.Pause()
	a.setState(consts.IngestStatePaused)
	return nil
}

// ResumeIngestion releases a paused pipeline. Because the pause left the store consistent
// and the bookmark current, resuming continues without a gap or a duplicate.
func (a *EventsAPI) ResumeIngestion() error {
	a.mu.Lock()
	gate, running := a.gate, a.running
	a.mu.Unlock()
	if !running || gate == nil {
		return AsError(consts.ErrCodeInternal, errors.New(consts.MsgNoIngestionRunning))
	}
	if !gate.Paused() {
		return AsError(consts.ErrCodeInternal, errors.New(consts.MsgNotPaused))
	}
	gate.Resume()
	a.setState(consts.IngestStateActive)
	return nil
}

// Shutdown stops any running ingestion and waits (briefly) for it to flush and persist, so
// a clean exit never abandons buffered events or a stale capture position. A run that is
// paused is resumed first — otherwise it would sit blocked instead of unwinding.
func (a *EventsAPI) Shutdown() {
	a.mu.Lock()
	cancel, gate, done, running := a.cancel, a.gate, a.done, a.running
	a.mu.Unlock()
	if !running {
		return
	}
	gate.Resume()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(consts.ShutdownDrainTimeout):
		// The pipeline is wedged; the store still closes cleanly and the bookmark simply
		// lags, which costs a re-read rather than lost data.
	}
}

// Startup installs the Wails-backed event sink. The app lifecycle calls it once with
// the application context; Wails injects that context and does not expose it as a JS
// argument, so this stays out of the frontend's callable surface.
func (a *EventsAPI) Startup(ctx context.Context) {
	a.mu.Lock()
	a.appCtx = ctx
	a.mu.Unlock()
	a.setEmitter(NewWailsEmitter(ctx))
}

// setEmitter installs the event sink. Kept unexported so the emitter (an interface)
// never leaks into the generated bindings; tests in this package inject a fake.
func (a *EventsAPI) setEmitter(e Emitter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emitter = e
}

// CheckPermissions reports the current process privilege state (refreshed on every
// call, so the frontend never acts on a stale snapshot).
func (a *EventsAPI) CheckPermissions() evtx.PermissionStatus {
	return evtx.CheckPermissions()
}

// EvaluateAccess reports whether the requested channels are readable under current
// privileges, and the warning to show if not.
func (a *EventsAPI) EvaluateAccess(channels []string) evtx.AccessDecision {
	return evtx.EvaluateAccess(channels, evtx.CheckPermissions())
}

// IsIngesting reports whether an ingestion job is currently running.
func (a *EventsAPI) IsIngesting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// StartIngest launches an ingestion job and returns immediately; progress, errors,
// and lifecycle flow through Wails events (see the contract catalog). It returns a
// validation error synchronously if the request is malformed, a job is already
// running, or required privileges are missing. Permissions are refreshed here so a
// stale grant can never start a blocked live read (P1/P4).
func (a *EventsAPI) StartIngest(req IngestRequest) error {
	a.mu.Lock()

	if a.running {
		a.mu.Unlock()
		return AsError(consts.ErrCodeInternal, errors.New("an ingestion is already running"))
	}
	if err := validateIngest(req); err != nil {
		a.mu.Unlock()
		return err
	}
	if req.Source == consts.SourceLive {
		decision := evtx.EvaluateAccess(req.Channels, evtx.CheckPermissions())
		if decision.Needed {
			emitter := a.emitter
			a.mu.Unlock()
			emitter.Emit(consts.EventPermissionWarn, decision)
			return AsError(consts.ErrCodePermission, errors.New(decision.Message))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.running = true
	a.gate = evtx.NewGate()
	a.done = make(chan struct{})
	gate, done := a.gate, a.done
	if req.Source == consts.SourceLive {
		a.liveChannels = append([]string(nil), req.Channels...)
		a.liveContinuous = req.Continuous
	} else {
		a.liveChannels, a.liveContinuous = nil, false
	}
	emitter := a.emitter
	a.mu.Unlock()

	// Announce the state transition outside the lock: emitting re-enters the runtime, and
	// holding the mutex across it would serialize the UI against the pipeline.
	a.setState(consts.IngestStateActive)
	go a.runIngestion(ctx, req, gate, done, emitter)
	return nil
}

// CaptureStatus reports the live-capture session so the UI can show an accurate capture
// indicator and the per-channel positions a resumed capture would continue from.
func (a *EventsAPI) CaptureStatus() CaptureStatus {
	a.mu.Lock()
	status := CaptureStatus{
		Active:     a.running && len(a.liveChannels) > 0,
		Continuous: a.liveContinuous,
		Channels:   append([]string(nil), a.liveChannels...),
	}
	a.mu.Unlock()

	status.Positions = map[string]uint64{}
	if store, ok := a.positions.(interface{ Positions() map[string]uint64 }); ok && store != nil {
		status.Positions = store.Positions()
	}
	return status
}

// ResetCapturePositions clears the durable capture bookmarks so the next live run reads
// its channels from the beginning again. Passing an empty channel clears every position.
func (a *EventsAPI) ResetCapturePositions(channel string) error {
	resetter, ok := a.positions.(interface{ Reset(string) error })
	if !ok || resetter == nil {
		return nil
	}
	if err := resetter.Reset(channel); err != nil {
		return AsError(consts.ErrCodeIO, err)
	}
	return nil
}

// CancelIngestion signals the active job to stop gracefully. It is a no-op if none
// is running. The pipeline emits a Cancelled event with a partial summary.
func (a *EventsAPI) CancelIngestion() {
	a.mu.Lock()
	cancel, gate, running := a.cancel, a.gate, a.running
	a.mu.Unlock()
	if !running || cancel == nil {
		return
	}
	a.setState(consts.IngestStateStopping)
	// Release a paused run first: it is blocked at its pause boundary and would otherwise
	// never observe the cancellation.
	gate.Resume()
	cancel()
}

// runIngestion executes the job off the caller's goroutine and clears running state
// on exit. Per-file Started/Progress/Complete events are emitted by the reporter.
func (a *EventsAPI) runIngestion(ctx context.Context, req IngestRequest, gate *evtx.Gate, done chan struct{}, emitter Emitter) {
	defer func() {
		a.mu.Lock()
		a.running = false
		a.cancel = nil
		a.gate = nil
		a.done = nil
		a.liveChannels, a.liveContinuous = nil, false
		a.mu.Unlock()
		// Back to idle, then release anyone waiting on a clean shutdown.
		a.setState(consts.IngestStateIdle)
		close(done)
	}()

	switch req.Source {
	case consts.SourceFile:
		// A request carrying more than one file (multi-select or a folder) tags its
		// members as part of a collection; a single file is tagged as such. Each
		// event records the concrete member path as its source identifier.
		collection := len(req.Paths) > 1
		for i, path := range req.Paths {
			if ctx.Err() != nil {
				return
			}
			// Each file reports its own position in the request, so the UI can say "3 of 12"
			// and size an overall bar. Ingest runs per file and so knows nothing about the
			// others; this is the only layer that does.
			reporter := &ingestReporter{
				emitter: emitter, source: consts.SourceFile, path: path,
				fileIndex: i + 1, fileTotal: len(req.Paths),
			}
			opts := evtx.Options{
				Source:           consts.SourceFile,
				Path:             path,
				SourceType:       fileSourceType(path, collection),
				SourceIdentifier: path,
				Idempotent:       req.Idempotent,
				Gate:             gate,
			}
			if _, err := evtx.Ingest(ctx, opts, a.store, reporter); err != nil && ctx.Err() == nil {
				// A .db that is a real database with an unrecognized structure is a
				// user-actionable schema problem, not an I/O failure — code it as such so
				// the UI can say which of the two actually happened (P17).
				code := consts.ErrCodeIO
				if evtx.IsDBSchemaError(err) {
					code = consts.ErrCodeSchema
				}
				emitter.Emit(consts.EventIngestError, AsError(code, err))
			}
		}
	case consts.SourceLive:
		reporter := &ingestReporter{emitter: emitter, source: consts.SourceLive}
		opts := evtx.Options{
			Source:           consts.SourceLive,
			Channels:         req.Channels,
			SourceType:       consts.SourceTypeLiveSystem,
			SourceIdentifier: strings.Join(req.Channels, consts.SourceIdentifierSeparator),
			Idempotent:       req.Idempotent,
			// Continuous capture resumes each channel from its durable bookmark and keeps
			// streaming until cancelled (P7).
			Continuous: req.Continuous,
			Positions:  a.positions,
			Gate:       gate,
		}
		if _, err := evtx.Ingest(ctx, opts, a.store, reporter); err != nil && ctx.Err() == nil {
			emitter.Emit(consts.EventIngestError, AsError(consts.ErrCodeInternal, err))
		}
	}
}

// filterFor builds the persistence-layer filter for a query, including resolving the
// analyst-findings filters (P25) into the content-hash sets graphene matches on. Every read
// path goes through it so the events list, the count, and the timeline can never disagree
// about what the same query means.
func (a *EventsAPI) filterFor(q EventQuery) (graphene.EventFilter, error) {
	filter, err := q.toFilter()
	if err != nil {
		return filter, err
	}
	a.applyFindingFilters(&filter, q)
	return filter, nil
}

// applyFindingFilters translates the finding state and tag selections into hash sets.
//
// "No findings" is the complement of a set, so it becomes an exclusion rather than an
// inclusion. A tag combines with the state as an intersection: asking for flagged events
// tagged "persistence" means both, not either.
func (a *EventsAPI) applyFindingFilters(filter *graphene.EventFilter, q EventQuery) {
	if a.findings == nil || (q.FindingState == "" && q.Tag == "") {
		return
	}

	intersect := func(keys []string) {
		set := make(map[string]bool, len(keys))
		for _, k := range keys {
			// Once a set is already in play, keep only members present in both, so two
			// finding filters narrow rather than widen.
			if filter.HashIn != nil && !filter.HashIn[k] {
				continue
			}
			set[k] = true
		}
		filter.HashIn = set
	}

	if q.FindingState != "" {
		if keys, ok := a.findings.Keys(q.FindingState); ok {
			intersect(keys)
		} else {
			// FindingFilterNone: everything except the annotated events.
			excluded := make(map[string]bool)
			for _, k := range a.findings.AllKeys() {
				excluded[k] = true
			}
			filter.HashNotIn = excluded
		}
	}
	if q.Tag != "" {
		intersect(a.findings.KeysWithTag(q.Tag))
	}
}

// QueryEvents returns events matching the forensic filter, chronologically ordered
// and paginated by the persistence layer.
func (a *EventsAPI) QueryEvents(q EventQuery) ([]*graphene.Event, error) {
	filter, err := a.filterFor(q)
	if err != nil {
		return nil, AsError(consts.ErrCodeInternal, err)
	}
	events, err := a.store.QueryEvents(filter)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return events, nil
}

// CountEvents returns the total number of events matching the forensic filter (ignoring
// paging), so the frontend can show an accurate "X of N" and drive progressive loading.
func (a *EventsAPI) CountEvents(q EventQuery) (int, error) {
	filter, err := a.filterFor(q)
	if err != nil {
		return 0, AsError(consts.ErrCodeInternal, err)
	}
	n, err := a.store.CountEvents(filter)
	if err != nil {
		return 0, AsError(consts.ErrCodePersistence, err)
	}
	return n, nil
}

// Timeline summarizes the filtered event set over time for the timeline page (P24): the
// time extent, how many events are dated vs undated, and a density histogram.
//
// It returns COUNTS, not events. A case with hundreds of thousands of records cannot ship
// every point to the UI to be drawn, so the page renders density and fetches individual
// events only for the range the user zooms into.
func (a *EventsAPI) Timeline(q EventQuery, buckets int, groupBy string) (graphene.TimelineSummary, error) {
	filter, err := a.filterFor(q)
	if err != nil {
		return graphene.TimelineSummary{}, AsError(consts.ErrCodeInternal, err)
	}
	sum, err := a.store.TimelineGrouped(filter, buckets, groupBy)
	if err != nil {
		return sum, AsError(consts.ErrCodePersistence, err)
	}
	return sum, nil
}

// GetEvent returns a single event by node id.
func (a *EventsAPI) GetEvent(id uint64) (*graphene.Event, error) {
	e, err := a.store.GetEvent(id)
	if err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	return e, nil
}

// Stats returns event/relation counts for dashboard display.
func (a *EventsAPI) Stats() (StatsResult, error) {
	nodes, edges, err := a.store.Stats()
	if err != nil {
		return StatsResult{}, AsError(consts.ErrCodePersistence, err)
	}
	return StatsResult{Events: nodes, Relations: edges}, nil
}

// fileSourceType classifies where an ingested event came from: a SQLite database, a single
// EVTX file, or one member of a multi-file/folder selection. It is recorded per event so
// the UI can show and filter by origin.
func fileSourceType(path string, collection bool) string {
	switch {
	case evtx.IsDBPath(path):
		return consts.SourceTypeSQLiteDB
	case collection:
		return consts.SourceTypeMultiEVTX
	default:
		return consts.SourceTypeSingleEVTX
	}
}

// validateIngest checks the request shape before any work is scheduled.
func validateIngest(req IngestRequest) error {
	switch req.Source {
	case consts.SourceFile:
		if len(req.Paths) == 0 {
			return AsError(consts.ErrCodeInternal, errors.New("no EVTX files specified"))
		}
	case consts.SourceLive:
		if len(req.Channels) == 0 {
			return AsError(consts.ErrCodeInternal, errors.New("no channels specified"))
		}
	default:
		return AsError(consts.ErrCodeInternal, fmt.Errorf("unknown ingestion source %q", req.Source))
	}
	return nil
}

// toFilter maps the query DTO to the persistence filter, parsing RFC3339 bounds.
func (q EventQuery) toFilter() (graphene.EventFilter, error) {
	f := graphene.EventFilter{
		EventID:           q.EventID,
		Provider:          q.Provider,
		Channel:           q.Channel,
		User:              q.User,
		Search:            q.Search,
		SourceType:        q.SourceType,
		SourceIdentifier:  q.SourceIdentifier,
		MinDuplicateCount: q.MinDuplicateCount,
		RelationState:     q.RelationState,
		Undated:           q.Undated,
		Offset:            q.Offset,
		Limit:             q.Limit,
		Descending:        q.Descending,
	}
	if q.TimeFrom != "" {
		t, err := time.Parse(time.RFC3339, q.TimeFrom)
		if err != nil {
			return f, fmt.Errorf("invalid time_from %q: %w", q.TimeFrom, err)
		}
		f.TimeFrom = &t
	}
	if q.TimeTo != "" {
		t, err := time.Parse(time.RFC3339, q.TimeTo)
		if err != nil {
			return f, fmt.Errorf("invalid time_to %q: %w", q.TimeTo, err)
		}
		f.TimeTo = &t
	}
	return f, nil
}
