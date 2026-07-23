package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphbuild"
)

// errRunInProgress guards against two concurrent builds racing on the same graphs.
var errRunInProgress = errors.New(consts.MsgRuleRunInProgress)

// BuildAPI is the Wails binding for the rule-driven graph creation workflow (P6). It is a
// thin adapter: request validation and the actual composition of rules + events + graphs
// live in the graphbuild package. One run may build several graphs, so the result is a
// per-rule summary the frontend can report and navigate from.
//
// The call is synchronous — the promise resolves with the full result — but progress is
// published as events while it runs, so a build over many rules shows movement instead of
// freezing the UI on "running" until it finishes. A separate cancel binding can stop it,
// which works because Wails dispatches bindings concurrently.
type BuildAPI struct {
	builder *graphbuild.Builder

	mu      sync.Mutex
	emitter Emitter
	cancel  context.CancelFunc
	running bool
}

// NewBuildAPI constructs the binding over the workflow.
func NewBuildAPI(builder *graphbuild.Builder) *BuildAPI {
	return &BuildAPI{builder: builder, emitter: noopEmitter{}}
}

// Startup installs the Wails event sink once the runtime is ready.
func (a *BuildAPI) Startup(ctx context.Context) {
	a.setEmitter(NewWailsEmitter(ctx))
}

// setEmitter installs the event sink. Unexported so the interface never leaks into the
// generated bindings; tests inject a fake.
func (a *BuildAPI) setEmitter(e Emitter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emitter = e
}

// BuildRequest selects what to build. An empty RuleIDs runs every enabled rule; Filter is
// the same forensic filter the events view uses, so a run can be scoped to what the user is
// currently looking at (its paging fields are ignored — a build always evaluates the whole
// matching set, and undated events are always excluded because correlation is time-ordered).
type BuildRequest struct {
	RuleIDs []string   `json:"rule_ids"`
	Filter  EventQuery `json:"filter"`
}

// buildReporter forwards workflow progress to the frontend.
type buildReporter struct{ emitter Emitter }

func (r buildReporter) Progress(p graphbuild.Progress) {
	r.emitter.Emit(consts.EventRulesProgress, p)
}

// RunRules applies the selected rules and returns a per-rule summary (graph, match count,
// edges written, edges cleared by the rebuild, and whether the engine's match cap was hit).
// Re-running replaces each rule's graph contents rather than appending to them.
func (a *BuildAPI) RunRules(req BuildRequest) (graphbuild.Result, error) {
	filter, err := req.Filter.toFilter()
	if err != nil {
		return graphbuild.Result{}, AsError(consts.ErrCodeInternal, err)
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return graphbuild.Result{}, AsError(consts.ErrCodeInternal, errRunInProgress)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.running = true
	emitter := a.emitter
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.cancel = nil
		a.mu.Unlock()
		cancel()
	}()

	emitter.Emit(consts.EventRulesStarted, len(req.RuleIDs))
	res, err := a.builder.RunWithProgress(ctx, graphbuild.Request{
		RuleIDs: req.RuleIDs,
		Filter:  filter,
	}, time.Now().UTC(), buildReporter{emitter: emitter})
	if err != nil {
		// A cancelled run is not a failure to report as one: it did what was asked.
		if ctx.Err() != nil {
			emitter.Emit(consts.EventRulesCancelled, res)
			return res, nil
		}
		emitter.Emit(consts.EventRulesComplete, res)
		return res, AsError(consts.ErrCodeRule, err)
	}
	emitter.Emit(consts.EventRulesComplete, res)
	return res, nil
}

// CancelRuleRun stops an in-flight run. It is a no-op when nothing is running. The partial
// result is still returned to the original caller, so graphs already rebuilt are kept
// rather than being silently discarded.
func (a *BuildAPI) CancelRuleRun() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// IsRunningRules reports whether a build is in flight, so a view opened mid-run shows the
// right state instead of assuming idle.
func (a *BuildAPI) IsRunningRules() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}
