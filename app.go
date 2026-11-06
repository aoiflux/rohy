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
	// roots is the fingerprint history, kept so the seal on close is recorded like every other.
	roots *custody.Store
	// session is who is working and how the case opened (item 41).
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
	// Query is the read-only console (item 33). It holds the store as query.Reader, so it can read
	// the case and nothing else.
	Query *api.QueryAPI
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
	// The store opens once the session has fixed how (item 41): graphene takes the signer and the
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
		store:       store,
		layoutStore: layoutStore,
		registry:    registry,
		roots:       rootStore,
		session:     session,
		Events: api.AttachRedactionJournal(
			api.AttachSealHistory(api.NewEventsAPI(store, positions, findingStore), rootStore), journal),
		Graph:    api.NewGraphAPI(store, layoutStore, registry).WithCascades(snapStore, noteStore),
		Rules:    api.NewRulesAPI(ruleReg),
		Build:    api.NewBuildAPI(graphbuild.New(store, registry, ruleReg).WithLayouts(layoutStore)),
		Findings: api.AttachFindingsRedactions(api.NewFindingsAPI(findingStore, store), journal),
		System:   api.NewSystemAPI(),
		Maintenance: api.NewMaintenanceAPI(store).WithIntegrity(api.IntegrityDeps{
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

// startup is invoked by Wails once the runtime is ready: it hands the app context to the
// event-emitting bindings, then kicks off initialization in the background so the window
// paints immediately.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.Events.Startup(ctx)
	a.Rules.Startup(ctx)
	a.Build.Startup(ctx)
	a.System.Startup(ctx)
	a.Maintenance.Startup(ctx)
	go a.initialize()
}

// initialize performs the slow startup work, reporting each stage so the splash can show
// real progress rather than a fixed timer.
//
// A failure here is reported and left visible instead of killing the process: the user
// gets a readable reason in the window, which is far more useful than the app vanishing.
func (a *App) initialize() {
	// The session step (item 41). A case with nothing to ask — a known analyst, never signed —
	// starts as it always did; otherwise the splash shows the step and this waits for the answer.
	if v := a.session.Status(); v.Need == consts.SessionNeedNone {
		if _, err := a.session.Begin(api.SessionRequest{}); err != nil {
			a.System.Failed(err)
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

	a.System.Stage(consts.MsgInitStore)
	if err := a.store.Warm(); err != nil {
		a.System.Failed(err)
		return
	}

	// The migrations write; a session that cannot write leaves the case as it is.
	if a.store.Writable() == nil {
		a.System.Stage(consts.MsgInitGraphs)
		if err := migrateGraphs(a.store, a.layoutStore, a.registry); err != nil {
			a.System.Failed(err)
			return
		}
	}

	// Rules are already loaded (the registry scan is cheap); the stage exists so the
	// splash reflects the real sequence rather than inventing steps.
	a.System.Stage(consts.MsgInitRules)
	a.System.Ready()
}

// shutdown stops any in-flight ingestion (letting it flush and persist its capture
// position first), then seals and closes the store so the WAL is truncated on a clean
// exit. Draining before closing is what keeps a live capture's bookmark honest across an
// app close — otherwise the next session would re-read from a stale position.
//
// The seal is skipped when nothing was written since the last one (plan §15.5). A compaction
// always makes a new fingerprint, so compacting an unchanged case on every close made the
// fingerprint an analyst had just exported stale by the next launch. There is no window left
// to report a failure in, so it is not reported; the next open replays the log either way.
func (a *App) shutdown(ctx context.Context) {
	if a.Events != nil {
		a.Events.Shutdown()
	}
	// The session seals what changed and records how a signed case was left. A session that never
	// began, or cannot write, seals nothing — closing at the session step opens nothing.
	if a.session != nil {
		_ = a.session.Close()
	}
	if a.store != nil {
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
// left this folder establishes nothing against anyone able to rewrite the folder. See §14.2.
func rootsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.RootsSubdir)
}

// redactionsDir returns the redaction journal's directory (<cwd>/rohy-data/redactions). Beside the
// store, and kept with the case: it lists what must not be ingested again (plan §14.5).
func redactionsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.RedactionsSubdir)
}

// analystsDir returns the case's analyst register directory, a sibling of the DB directory
// (<cwd>/rohy-data/analysts): who has worked on the case, and the public keys it has seen.
func analystsDir(dbDir string) string {
	return filepath.Join(filepath.Dir(dbDir), consts.AnalystsSubdir)
}
