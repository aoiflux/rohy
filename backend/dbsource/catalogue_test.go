package dbsource

import (
	"strings"
	"testing"

	"rohy/backend/consts"
)

// catalogueDB builds a database in the provider/message catalogue shape (P22).
func catalogueDB(t *testing.T, name string, extra ...string) string {
	t.Helper()
	stmts := []string{
		`CREATE TABLE messages  ( id INTEGER NOT NULL, event_id INTEGER NOT NULL,
		                          provider_id INTEGER NOT NULL, message TEXT )`,
		`CREATE TABLE providers ( id INTEGER NOT NULL, name TEXT )`,
		`CREATE INDEX message_idx ON messages (event_id, provider_id)`,
		`INSERT INTO providers VALUES (1,'Microsoft-Windows-Security-Auditing'), (2,'Service Control Manager')`,
		`INSERT INTO messages VALUES
			(1, 4624, 1, 'An account was successfully logged on.'),
			(2, 4625, 1, 'An account failed to log on.'),
			(3, 7045, 2, 'A service was installed in the system.')`,
	}
	return makeDB(t, name, append(stmts, extra...)...)
}

func TestCatalogueSchemaIsRecognized(t *testing.T) {
	s, err := Open(catalogueDB(t, "catalogue.db"))
	if err != nil {
		t.Fatalf("catalogue schema should be recognized: %v", err)
	}
	defer s.Close()

	if s.Kind() != KindMessageCatalogue {
		t.Errorf("kind = %q, want %q", s.Kind(), KindMessageCatalogue)
	}
	if n, err := s.Count(); err != nil || n != 3 {
		t.Errorf("count = %d (err %v), want 3", n, err)
	}
}

func TestCatalogueRowsResolveProviderAndAreUndated(t *testing.T) {
	s, err := Open(catalogueDB(t, "catalogue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rows, skipped := collect(t, s)
	if len(skipped) != 0 {
		t.Errorf("unexpected skips: %v", skipped)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}

	first := rows[0]
	// An INTEGER event_id must normalize to the string form the event model uses, so 4624
	// from a .db matches 4624 from an .evtx.
	if first.EventID != "4624" {
		t.Errorf("event id = %q, want the decimal string \"4624\"", first.EventID)
	}
	if first.Provider != "Microsoft-Windows-Security-Auditing" {
		t.Errorf("provider not resolved through the join: %q", first.Provider)
	}
	if !strings.Contains(first.Message, "successfully logged on") {
		t.Errorf("message not captured: %q", first.Message)
	}
	// The schema carries no time; it must be left unknown rather than invented.
	for _, r := range rows {
		if !r.Timestamp.IsZero() {
			t.Errorf("event %s got a fabricated timestamp %v", r.EventID, r.Timestamp)
		}
		if r.Computer != "" || r.User != "" || r.Channel != "" {
			t.Errorf("event %s invented fields the schema does not have: %+v", r.EventID, r)
		}
	}
}

func TestCatalogueKeepsRowsWithUnknownProvider(t *testing.T) {
	// A dangling provider_id is still real data — the event id and message are intact — so
	// the row must survive with an empty provider rather than being silently dropped.
	path := catalogueDB(t, "dangling.db",
		`INSERT INTO messages VALUES (4, 1102, 99, 'The audit log was cleared.')`)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rows, _ := collect(t, s)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4 (the dangling row must not be dropped)", len(rows))
	}
	var orphan *Row
	for i := range rows {
		if rows[i].EventID == "1102" {
			orphan = &rows[i]
		}
	}
	if orphan == nil {
		t.Fatal("the row with an unknown provider_id was dropped")
	}
	if orphan.Provider != "" {
		t.Errorf("unknown provider = %q, want empty", orphan.Provider)
	}
	if !strings.Contains(orphan.Message, "audit log was cleared") {
		t.Errorf("message lost: %q", orphan.Message)
	}
}

func TestCatalogueColumnAliases(t *testing.T) {
	path := makeDB(t, "aliased.db",
		`CREATE TABLE messages  ( id INTEGER, EventID INTEGER, ProviderId INTEGER, Text TEXT )`,
		`CREATE TABLE providers ( Id INTEGER, ProviderName TEXT )`,
		`INSERT INTO providers VALUES (7,'Sysmon')`,
		`INSERT INTO messages VALUES (1, 4688, 7, 'A new process has been created.')`,
	)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("aliased catalogue should be recognized: %v", err)
	}
	defer s.Close()

	rows, _ := collect(t, s)
	if len(rows) != 1 || rows[0].EventID != "4688" || rows[0].Provider != "Sysmon" {
		t.Errorf("aliased catalogue not mapped: %+v", rows)
	}
}

func TestEventsSchemaWinsWhenBothPresent(t *testing.T) {
	// Real dated evidence takes priority over a catalogue when a file carries both.
	path := makeDB(t, "both.db",
		`CREATE TABLE events (event_id TEXT, timestamp TEXT, provider TEXT, channel TEXT, computer TEXT)`,
		`INSERT INTO events VALUES ('4624','2026-07-21T10:00:00Z','p','Security','HOST')`,
		`CREATE TABLE messages  ( id INTEGER, event_id INTEGER, provider_id INTEGER, message TEXT )`,
		`CREATE TABLE providers ( id INTEGER, name TEXT )`,
	)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Kind() != KindEvents {
		t.Errorf("kind = %q, want the events schema to win", s.Kind())
	}
}

func TestNeitherSchemaNamesBothShapes(t *testing.T) {
	path := makeDB(t, "neither.db", `CREATE TABLE unrelated (a TEXT, b TEXT)`)
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected a schema rejection")
	}
	if !IsSchemaError(err) {
		t.Fatalf("err = %v, want a schema error", err)
	}
	// The user should learn what rohy actually looked for, not just that it failed.
	for _, want := range []string{consts.MsgDBSchemaEvents, consts.MsgDBSchemaMessage} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %s", want, err.Error())
		}
	}
}

func TestNearMissEventsSchemaStillNamesMissingColumns(t *testing.T) {
	// Adding a second schema must not blunt the P17 diagnostic: a file that clearly meant
	// to be an events database should still be told exactly which columns are absent.
	path := makeDB(t, "partial.db", `CREATE TABLE events (event_id TEXT, timestamp TEXT)`)
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected a schema rejection")
	}
	for _, col := range []string{consts.DBColProvider, consts.DBColChannel, consts.DBColComputer} {
		if !strings.Contains(err.Error(), col) {
			t.Errorf("error should name the missing column %q, got: %s", col, err.Error())
		}
	}
}

func TestCatalogueMissingProvidersTableIsNotACatalogue(t *testing.T) {
	// messages alone is not the documented shape; without providers it must not be read as
	// one (guessing would risk mis-mapping).
	path := makeDB(t, "half.db",
		`CREATE TABLE messages ( id INTEGER, event_id INTEGER, provider_id INTEGER, message TEXT )`)
	if _, err := Open(path); err == nil {
		t.Error("a messages table without providers should not be accepted")
	}
}
