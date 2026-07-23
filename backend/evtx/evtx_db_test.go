package evtx

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"

	_ "modernc.org/sqlite"
)

// buildDB writes a SQLite file with the given statements.
func buildDB(t *testing.T, name string, stmts ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open(consts.DBDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	return path
}

func ingestPath(t *testing.T, store *graphene.Store, path string, idempotent bool) (Summary, error) {
	t.Helper()
	return Ingest(context.Background(), Options{
		Source:           consts.SourceFile,
		Path:             path,
		SourceType:       consts.SourceTypeSQLiteDB,
		SourceIdentifier: path,
		Idempotent:       idempotent,
	}, store, NoopReporter{})
}

func TestIngestDBRoundTrip(t *testing.T) {
	path := buildDB(t, "case.db",
		`CREATE TABLE events (event_id TEXT, timestamp TEXT, provider TEXT, channel TEXT, computer TEXT, user TEXT, raw_xml TEXT)`,
		`INSERT INTO events VALUES
			('4624','2026-07-21T10:00:00Z','Microsoft-Windows-Security-Auditing','Security','HOST-A','S-1-5-18','<Event><EventData><Data Name="TargetUserName">alice</Data></EventData></Event>'),
			('4625','2026-07-21T10:00:05Z','Microsoft-Windows-Security-Auditing','Security','HOST-A','S-1-5-18','<Event/>')`,
	)

	store := graphene.OpenInMemory()
	defer store.Close()

	summary, err := ingestPath(t, store, path, true)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if summary.RecordsPersisted != 2 {
		t.Errorf("persisted = %d, want 2", summary.RecordsPersisted)
	}

	events, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("stored = %d, want 2", len(events))
	}
	first := events[0]
	if first.EventID != "4624" || first.Channel != "Security" || first.Computer != "HOST-A" {
		t.Errorf("event not mapped: %+v", first)
	}
	if first.SourceType != consts.SourceTypeSQLiteDB {
		t.Errorf("source_type = %q, want %q", first.SourceType, consts.SourceTypeSQLiteDB)
	}
	// A raw_xml column that parses should enrich the parsed fields.
	if first.ParsedFields["TargetUserName"] != "alice" {
		t.Errorf("parsed fields not extracted from raw_xml: %+v", first.ParsedFields)
	}
}

func TestDedupCollapsesAcrossEVTXAndDB(t *testing.T) {
	// The parity guarantee: the same event read from a .evtx file and from a .db must
	// collapse into ONE canonical node, not two.
	store := graphene.OpenInMemory()
	defer store.Close()

	if _, err := Ingest(context.Background(), Options{
		Source:           consts.SourceFile,
		Path:             "testdata/Security.evtx",
		SourceType:       consts.SourceTypeSingleEVTX,
		SourceIdentifier: "testdata/Security.evtx",
		Idempotent:       true,
	}, store, NoopReporter{}); err != nil {
		t.Fatalf("evtx ingest: %v", err)
	}

	events, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("fixture produced no events")
	}
	before := len(events)
	sample := events[0]

	// Re-present that exact event through the .db path.
	path := buildDB(t, "same.db",
		`CREATE TABLE events (event_id TEXT, timestamp TEXT, provider TEXT, channel TEXT, computer TEXT, user TEXT)`,
	)
	db, err := sql.Open(consts.DBDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO events VALUES (?, ?, ?, ?, ?, ?)`,
		sample.EventID,
		sample.Timestamp.UTC().Format(consts.TimestampIndexLayout),
		sample.Provider, sample.Channel, sample.Computer, sample.User,
	); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := ingestPath(t, store, path, true); err != nil {
		t.Fatalf("db ingest: %v", err)
	}

	after, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != before {
		t.Errorf("node count went %d → %d: the cross-source duplicate was not collapsed", before, len(after))
	}
	// And the canonical node's occurrence count reflects the second sighting.
	for _, e := range after {
		if e.ID == sample.ID && e.DeduplicationCount < 2 {
			t.Errorf("dedup count = %d, want ≥2 after re-ingesting the same event from a .db", e.DeduplicationCount)
		}
	}
}

func TestIngestDBWrongSchemaIngestsNothing(t *testing.T) {
	path := buildDB(t, "wrong.db", `CREATE TABLE unrelated (a TEXT, b TEXT)`)

	store := graphene.OpenInMemory()
	defer store.Close()

	_, err := ingestPath(t, store, path, true)
	if err == nil {
		t.Fatal("expected a schema rejection")
	}
	if !IsDBSchemaError(err) {
		t.Errorf("err = %v, want a schema error", err)
	}
	if !strings.Contains(err.Error(), "SQLite database") {
		t.Errorf("message should tell the user the file IS a database: %v", err)
	}

	events, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("a rejected .db must ingest nothing, got %d events", len(events))
	}
}

func TestIngestNonDatabaseFileIsNotASchemaError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bogus.db")
	if err := os.WriteFile(path, []byte("definitely not sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := graphene.OpenInMemory()
	defer store.Close()

	_, err := ingestPath(t, store, path, true)
	if err == nil {
		t.Fatal("expected a read failure")
	}
	if IsDBSchemaError(err) {
		t.Errorf("an unreadable file should not be reported as a schema mismatch: %v", err)
	}
}

func TestIngestMessageCatalogue(t *testing.T) {
	// The P22 shape: descriptions of what event ids mean, with no timestamps at all.
	path := buildDB(t, "catalogue.db",
		`CREATE TABLE messages  ( id INTEGER NOT NULL, event_id INTEGER NOT NULL,
		                          provider_id INTEGER NOT NULL, message TEXT )`,
		`CREATE TABLE providers ( id INTEGER NOT NULL, name TEXT )`,
		`CREATE INDEX message_idx ON messages (event_id, provider_id)`,
		`INSERT INTO providers VALUES (1,'Microsoft-Windows-Security-Auditing')`,
		`INSERT INTO messages VALUES
			(1, 4624, 1, 'An account was successfully logged on.'),
			(2, 4625, 1, 'An account failed to log on.')`,
	)

	store := graphene.OpenInMemory()
	defer store.Close()

	summary, err := ingestPath(t, store, path, true)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if summary.RecordsPersisted != 2 {
		t.Fatalf("persisted = %d, want 2", summary.RecordsPersisted)
	}
	// The count that lets the UI explain the exclusion instead of hiding it.
	if summary.RecordsUndated != 2 {
		t.Errorf("undated = %d, want 2", summary.RecordsUndated)
	}

	// Undated rows must NOT appear in the default (timeline) view...
	timeline, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 0 {
		t.Errorf("catalogue rows leaked into the timeline: %+v", timeline)
	}

	// ...but must be fully retrievable when asked for.
	all, err := store.QueryEvents(graphene.EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("stored = %d, want 2", len(all))
	}
	for _, e := range all {
		if e.SourceType != consts.SourceTypeMessageDB {
			t.Errorf("source_type = %q, want %q", e.SourceType, consts.SourceTypeMessageDB)
		}
		if !e.Timestamp.IsZero() {
			t.Errorf("event %s got a fabricated timestamp %v", e.EventID, e.Timestamp)
		}
		if e.Provider != "Microsoft-Windows-Security-Auditing" {
			t.Errorf("provider not resolved: %q", e.Provider)
		}
		if e.RawXML == "" {
			t.Errorf("message text was not stored for %s", e.EventID)
		}
	}
}

func TestCatalogueReimportCollapsesButKeepsDistinctMessages(t *testing.T) {
	tmp := t.TempDir()
	makeCat := func(name, message string) string {
		p := filepath.Join(tmp, name)
		db, err := sql.Open(consts.DBDriver, p)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		for _, stmt := range []string{
			`CREATE TABLE messages  ( id INTEGER, event_id INTEGER, provider_id INTEGER, message TEXT )`,
			`CREATE TABLE providers ( id INTEGER, name TEXT )`,
			`INSERT INTO providers VALUES (1,'P')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(`INSERT INTO messages VALUES (1, 4624, 1, ?)`, message); err != nil {
			t.Fatal(err)
		}
		return p
	}

	store := graphene.OpenInMemory()
	defer store.Close()

	same := "An account was successfully logged on."
	first := makeCat("a.db", same)
	if _, err := ingestPath(t, store, first, true); err != nil {
		t.Fatal(err)
	}
	// Re-importing the SAME catalogue must collapse rather than duplicate.
	if _, err := ingestPath(t, store, makeCat("b.db", same), true); err != nil {
		t.Fatal(err)
	}
	all, err := store.QueryEvents(graphene.EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("re-import created %d nodes, want 1 (collapsed)", len(all))
	}

	// A DIFFERENT message for the same (provider, event id) is different information and
	// must survive — the old whole-event hash would have silently dropped it.
	if _, err := ingestPath(t, store, makeCat("c.db", "Totally different wording."), true); err != nil {
		t.Fatal(err)
	}
	all, err = store.QueryEvents(graphene.EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("distinct messages collapsed into %d node(s), want 2", len(all))
	}
}

func TestIsDBPath(t *testing.T) {
	for _, p := range []string{"a.db", "A.DB", "dir/case.Db"} {
		if !IsDBPath(p) {
			t.Errorf("%s should be treated as a database", p)
		}
	}
	for _, p := range []string{"a.evtx", "a.db.evtx", "a.sqlite", "a"} {
		if IsDBPath(p) {
			t.Errorf("%s should not be treated as a database", p)
		}
	}
}
