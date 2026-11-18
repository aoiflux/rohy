package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"rohy/backend/analyst"
	"rohy/backend/anchor"
	"rohy/backend/annotate"
	"rohy/backend/api"
	"rohy/backend/capture"
	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/findings"
	"rohy/backend/graphbuild"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/layout"
	"rohy/backend/redaction"
	"rohy/backend/rules"
	"rohy/backend/snapshot"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// App owns the application lifecycle: it opens the persistence store and constructs
// the Wails binding structs. The bindings (Events, Graph) are what the frontend
// calls; App itself only manages startup/shutdown and holds no business logic.
//
// It is the first Wails service (see services), so it starts first and stops last. Its only
// methods are the two Wails never binds.
type App struct {
	// ctx is cancelled as the app begins to quit.
	ctx context.Context
	// desktop is the window the bindings use; main sets it before the app runs.
	desktop api.Desktop
	// handoffPID is the unelevated rohy that relaunched this one elevated, or 0 (api.HandoffPID).
	handoffPID int
	// Retained for the deferred initialize() pass, which opens the case and gives a new one its
	// Default graph once the window is up rather than before it appears.
	store    *graphene.Store
	registry *graphreg.Store
	// session is who is working and how the case opened.
	session *api.Session

	Events   *api.EventsAPI
	Graph    *api.GraphAPI
	Rules    *api.RulesAPI
	Build    *api.BuildAPI
	Findings *api.FindingsAPI
	System   *api.SystemAPI
	// Maintenance is opt-in work over the whole case — currently the correlation-key backfill.
	// Separate from System because System never touches case data.
	Maintenance *api.MaintenanceAPI
	// Snapshots owns its own sidecar store and its own restore lifecycle, so it is separate from
	// Graph for the same reason Maintenance is separate from System.
	Snapshots *api.SnapshotAPI
	// Annotate owns the analyst's own marks on a graph — layers, note pins, regions, arrows.
	Annotate *api.AnnotateAPI
	// Query is the read-only console. It holds the store as query.Reader, so it can read
	// the case and nothing else.
	Query *api.QueryAPI
}

// NewApp wires the binding layer WITHOUT doing any slow work.
//
// Everything here is cheap: the case store waits for the session step to choose how it opens
// (graphene.OpenAwaitingSession), and the sidecar stores only create their directories. The expensive part — replaying the store's
// WAL — is deferred to initialize(), which runs in the
// background once the window is up. That is what lets the UI appear immediately instead of
// the user watching nothing while a large case loads.
func NewApp() (*App, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, err
	}
	// The store opens once the session has fixed how: graphene takes the signer and the
	// analyst at open. Until then a call that needs it waits.
	store := graphene.OpenAwaitingSession(dir)

	layoutStore, err := layout.Open(layoutDir(dir))
	if err != nil {
		return nil, err
	}
	registry, err := graphreg.Open(graphsDir(dir))
	if err != nil {
		return nil, err
	}
	ruleReg, err := rules.Open(rulesDir(dir))
	if err != nil {
		return nil, err
	}
	positions, err := capture.Open(captureDir(dir))
	if err != nil {
		return nil, err
	}
	findingStore, err := findings.Open(findingsDir(dir))
	if err != nil {
		return nil, err
	}
	snapStore, err := snapshot.Open(snapshotsDir(dir))
	if err != nil {
		return nil, err
	}
	noteStore, err := annotate.Open(annotationsDir(dir))
	if err != nil {
		return nil, err
	}
	// Deferred: a damaged seal history is moved aside only by a session that may write.
	rootStore, err := custody.OpenDeferred(rootsDir(dir))
	if err != nil {
		return nil, err
	}
	// A journal that cannot be read still opens: it reports itself, and ingest refuses until it is
	// restored. Only a directory that cannot be made stops the app.
	journal, err := redaction.Open(redactionsDir(dir))
	if err != nil {
		return nil, err
	}
	// The analyst's profile lives outside every case, in the OS's per-user configuration folder:
	// it is the one record of a signed case that whoever holds the case folder cannot edit.
	profileDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	profiles, err := analyst.OpenProfile(filepath.Join(profileDir, consts.ProfileDirName))
	if err != nil {
		return nil, err
	}
	register, err := analyst.OpenRegister(analystsDir(dir))
	if err != nil {
		return nil, err
	}
	session := api.NewSession(api.SessionDeps{
		Store: store, Profiles: profiles, Register: register, Roots: rootStore, Journal: journal,
		// The working folder holds rohy-data and is the case: no key may live in it.
		WorkDir: filepath.Dir(filepath.Dir(dir)),
		DBDir:   dir,
	})

	app := &App{
		store:    store,
		registry: registry,
		session:  session,
		Events: api.AttachRedactionJournal(
			api.AttachSealHistory(api.NewEventsAPI(store, positions, findingStore), rootStore), journal),
		Graph:    api.AttachGraphCascades(api.NewGraphAPI(store, layoutStore, registry), snapStore, noteStore),
		Rules:    api.NewRulesAPI(ruleReg),
		Build:    api.NewBuildAPI(graphbuild.New(store, registry, ruleReg).WithLayouts(layoutStore)),
		Findings: api.AttachFindingsRedactions(api.NewFindingsAPI(findingStore, store), journal),
		System:   api.NewSystemAPI(),
		Maintenance: api.AttachIntegrity(api.NewMaintenanceAPI(store), api.IntegrityDeps{
			Findings:  findingStore,
			Graphs:    registry,
			Rules:     ruleReg,
			Roots:     rootStore,
			LayoutDir: layoutDir(dir),
			Journal:   journal,
			Snapshots: snapStore,
			Receipts:  anchor.Open(rootsDir(dir)),
		}),
		Snapshots: api.NewSnapshotAPI(store, layoutStore, snapStore, registry),
		Annotate:  api.NewAnnotateAPI(store, noteStore, registry),
		Query:     api.NewQueryAPI(store),
	}
	api.AttachSession(session, app.System, app.Maintenance)
	return app, nil
}

// ServiceStartup is called by Wails as the app runs, before the window opens: it connects the
// bindings to the window, then starts initialization in the background so the window paints
// immediately.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	api.Start(a.desktop, a.Events, a.Rules, a.Build, a.System, a.Maintenance)
	go a.initialize()
	return nil
}

// initialize performs the slow startup work, reporting each stage so the splash can show
// real progress rather than a fixed timer.
//
// A failure here is reported and left visible instead of killing the process: the user
// gets a readable reason in the window, which is far more useful than the app vanishing.
func (a *App) initialize() {
	// A rohy relaunched elevated starts while the one it replaces still holds the case, sealing and
	// closing it as it quits. Waiting here, before anything reads the case, means this one opens
	// the case as the old one left it rather than meeting its lock.
	if a.handoffPID != 0 {
		api.InitStage(a.System, consts.MsgInitHandoff)
		api.AwaitExit(a.handoffPID, consts.HandoffWait)
	}

	// The session step. A case with nothing to ask — a known analyst, never signed —
	// starts as it always did; otherwise the splash shows the step and this waits for the answer.
	if v := a.session.Status(); v.Need == consts.SessionNeedNone {
		if _, err := a.session.Begin(api.SessionRequest{}); err != nil {
			api.InitFailed(a.System, err)
			return
		}
	} else {
		api.AwaitSession(a.System)
		select {
		case <-a.session.Begun():
		case <-a.ctx.Done():
			return
		}
	}

	api.InitStage(a.System, consts.MsgInitStore)
	if err := a.store.Warm(); err != nil {
		api.InitFailed(a.System, err)
		return
	}

	// A new case gets its Default graph here. That is a write, and a session that cannot write
	// leaves the case as it is. EnsureDefault is idempotent, so it is safe on every startup.
	if a.store.Writable() == nil {
		api.InitStage(a.System, consts.MsgInitGraphs)
		if _, err := a.registry.EnsureDefault(consts.DefaultGraphName, time.Now().UTC()); err != nil {
			api.InitFailed(a.System, err)
			return
		}
	}

	// Rules are already loaded (the registry scan is cheap); the stage exists so the
	// splash reflects the real sequence rather than inventing steps.
	api.InitStage(a.System, consts.MsgInitRules)
	api.InitReady(a.System)
}

// ServiceShutdown is called by Wails as the app quits: it seals and closes the store so the WAL is
// truncated on a clean exit.
//
// Ingestion has already stopped by then. Wails stops services in reverse registration order and
// this is the first, so EventsAPI.ServiceShutdown has let a running ingest flush and persist its
// capture position. Draining before closing is what keeps a live capture's bookmark honest across
// an app close — otherwise the next session would re-read from a stale position.
//
// The seal is skipped when nothing was written since the last one. A compaction
// always makes a new fingerprint, so compacting an unchanged case on every close made the
// fingerprint an analyst had just exported stale by the next launch. There is no window left
// to report a failure in, so it is not reported; the next open replays the log either way.
func (a *App) ServiceShutdown() error {
	// The session seals what changed and records how a signed case was left. A session that never
	// began, or cannot write, seals nothing — closing at the session step opens nothing.
	if a.session != nil {
		_ = a.session.Close()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
	return nil
}

// services lists what Wails binds and starts, in order.
//
// 🔒 The app is first, so it stops last: Wails stops services in reverse order, and the store
// must not close under a running ingest.
func services(a *App) []application.Service {
	return []application.Service{
		application.NewService(a),
		application.NewService(a.Events),
		application.NewService(a.Graph),
		application.NewService(a.Rules),
		application.NewService(a.Build),
		application.NewService(a.Findings),
		application.NewService(a.System),
		application.NewService(a.Maintenance),
		application.NewService(a.Snapshots),
		application.NewService(a.Annotate),
		application.NewService(a.Query),
	}
}

// dataDir returns (creating if needed) the store directory under the current working
// directory: <cwd>/rohy-data/db. Keeping the data beside where the app is
// launched makes each working folder self-contained (portable case files).
func dataDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, consts.DataDirName, consts.DBSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// layoutDir returns the canvas-layout directory, a sibling of the DB directory
// (<cwd>/rohy-data/layout).
func layoutDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.LayoutSubdir)
}

// graphsDir returns the graph-registry directory, a sibling of the DB directory
// (<cwd>/rohy-data/graphs).
func graphsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.GraphsSubdir)
}

// rulesDir returns the user correlation-rules directory, a sibling of the DB directory
// (<cwd>/rohy-data/rules).
func rulesDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.RulesSubdir)
}

// captureDir returns the live-capture bookmark directory, a sibling of the DB directory
// (<cwd>/rohy-data/capture). Keeping it beside the data means a portable case folder
// carries its capture positions with it.
func captureDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.CaptureSubdir)
}

// findingsDir returns the analyst-findings directory, a sibling of the DB directory
// (<cwd>/rohy-data/findings). It sits beside the store rather than inside it precisely
// because findings are authored, not ingested: the evidence store stays exactly as it was
// written, and the analyst's own work travels with the case folder as readable JSON.
func findingsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.FindingsSubdir)
}

// snapshotsDir returns the graph-snapshot directory (<cwd>/rohy-data/snapshots). Same placement
// and same reasoning as findings: a snapshot records the analyst's working state, not the
// evidence, so it lives beside the store and travels with the case folder as readable JSON.
func snapshotsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.SnapshotsSubdir)
}

// annotationsDir returns the annotation-layer directory (<cwd>/rohy-data/annotations). Same
// placement and same reasoning again: authored, not ingested.
func annotationsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.AnnotationsSubdir)
}

// rootsDir returns the case-fingerprint history (<cwd>/rohy-data/roots).
//
// Beside the store like every other sidecar, and with one thing worth being explicit about: this
// copy is for CHAINING one image's fingerprint to the last, which is a check the case can run on
// itself. It is not the external copy that makes a fingerprint worth anything — a value that never
// left this folder establishes nothing against anyone able to rewrite the folder.
func rootsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.RootsSubdir)
}

// redactionsDir returns the redaction journal's directory (<cwd>/rohy-data/redactions). Beside the
// store, and kept with the case: it lists what must not be ingested again.
func redactionsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.RedactionsSubdir)
}

// analystsDir returns the case's analyst register directory, a sibling of the DB directory
// (<cwd>/rohy-data/analysts): who has worked on the case, and the public keys it has seen.
func analystsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.AnalystsSubdir)
}
