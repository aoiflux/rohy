package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"rohy/backend/api"
	"rohy/backend/capture"
	"rohy/backend/consts"
	"rohy/backend/findings"
	"rohy/backend/graphbuild"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/layout"
	"rohy/backend/rules"
)

// App owns the application lifecycle: it opens the persistence store and constructs
// the Wails binding structs. The bindings (Events, Graph) are what the frontend
// calls; App itself only manages startup/shutdown and holds no business logic.
type App struct {
	ctx context.Context
	// Retained for the deferred initialize() pass, which runs the graph migrations once
	// the window is up rather than before it appears.
	store       *graphene.Store
	layoutStore *layout.Store
	registry    *graphreg.Store

	Events   *api.EventsAPI
	Graph    *api.GraphAPI
	Rules    *api.RulesAPI
	Build    *api.BuildAPI
	Findings *api.FindingsAPI
	System   *api.SystemAPI
}

// migrateGraphs guarantees a Default graph exists and folds any pre-P15 single-graph
// data into it: relations that carry no graph id are assigned to the Default graph, and
// a legacy canvas.json layout is moved to the Default graph's per-graph file. All steps
// are idempotent, so this is safe to run on every startup.
func migrateGraphs(store *graphene.Store, layoutStore *layout.Store, registry *graphreg.Store) error {
	def, err := registry.EnsureDefault(consts.DefaultGraphName, time.Now().UTC())
	if err != nil {
		return err
	}
	if _, err := store.MigrateRelationsToGraph(def.ID); err != nil {
		return err
	}
	if _, err := layoutStore.MigrateLegacy(def.ID); err != nil {
		return err
	}
	return nil
}

// NewApp wires the binding layer WITHOUT doing any slow work.
//
// Everything here is cheap: the case store is opened lazily (graphene.OpenLazy), and the
// sidecar stores only create their directories. The expensive part — replaying the store's
// WAL and running the graph migrations — is deferred to initialize(), which runs in the
// background once the window is up. That is what lets the UI appear immediately instead of
// the user watching nothing while a large case loads.
func NewApp() (*App, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, err
	}
	store := graphene.OpenLazy(dir)

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

	return &App{
		store:       store,
		layoutStore: layoutStore,
		registry:    registry,
		Events:      api.NewEventsAPI(store, positions, findingStore),
		Graph:       api.NewGraphAPI(store, layoutStore, registry),
		Rules:       api.NewRulesAPI(ruleReg),
		Build:       api.NewBuildAPI(graphbuild.New(store, registry, ruleReg).WithLayouts(layoutStore)),
		Findings:    api.NewFindingsAPI(findingStore, store),
		System:      api.NewSystemAPI(),
	}, nil
}

// startup is invoked by Wails once the runtime is ready: it hands the app context to the
// event-emitting bindings, then kicks off initialization in the background so the window
// paints immediately.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.Events.Startup(ctx)
	a.Rules.Startup(ctx)
	a.Build.Startup(ctx)
	a.System.Startup(ctx)
	go a.initialize()
}

// initialize performs the slow startup work, reporting each stage so the splash can show
// real progress rather than a fixed timer.
//
// A failure here is reported and left visible instead of killing the process: the user
// gets a readable reason in the window, which is far more useful than the app vanishing.
func (a *App) initialize() {
	a.System.Stage(consts.MsgInitStore)
	if err := a.store.Warm(); err != nil {
		a.System.Failed(err)
		return
	}

	a.System.Stage(consts.MsgInitGraphs)
	if err := migrateGraphs(a.store, a.layoutStore, a.registry); err != nil {
		a.System.Failed(err)
		return
	}

	// Rules are already loaded (the registry scan is cheap); the stage exists so the
	// splash reflects the real sequence rather than inventing steps.
	a.System.Stage(consts.MsgInitRules)
	a.System.Ready()
}

// shutdown stops any in-flight ingestion (letting it flush and persist its capture
// position first), then compacts and closes the store so the WAL is truncated on a clean
// exit. Draining before closing is what keeps a live capture's bookmark honest across an
// app close — otherwise the next session would re-read from a stale position.
func (a *App) shutdown(ctx context.Context) {
	if a.Events != nil {
		a.Events.Shutdown()
	}
	if a.store != nil {
		_ = a.store.Compact()
		_ = a.store.Close()
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
