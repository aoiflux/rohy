package dbsource

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
)

// makeDB creates a SQLite file and runs the given statements against it.
func makeDB(t *testing.T, name string, stmts ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open(consts.DBDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	return path
}

// wellFormed builds a .db matching the documented schema exactly.
func wellFormed(t *testing.T) string {
	t.Helper()
	return makeDB(t, "good.db",
		`CREATE TABLE events (
			event_id TEXT, timestamp TEXT, provider TEXT,
			channel TEXT, computer TEXT, user TEXT, raw_xml TEXT
		)`,
		`INSERT INTO events VALUES
			('4624','2026-07-21T10:00:00Z','Microsoft-Windows-Security-Auditing','Security','HOST-A','S-1-5-18','<Event/>'),
			('4625','2026-07-21T10:00:05Z','Microsoft-Windows-Security-Auditing','Security','HOST-A','S-1-5-18','<Event/>')`,
	)
}

func collect(t *testing.T, s *Source) ([]Row, []string) {
	t.Helper()
	var rows []Row
	var skipped []string
	if err := s.Stream(func(r Row) error {
		rows = append(rows, r)
		return nil
	}, func(msg string) { skipped = append(skipped, msg) }); err != nil {
		t.Fatalf("stream: %v", err)
	}
	return rows, skipped
}

func TestOpenAndStreamWellFormedDB(t *testing.T) {
	s, err := Open(wellFormed(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if n, err := s.Count(); err != nil || n != 2 {
		t.Fatalf("count = %d (err %v), want 2", n, err)
	}
	rows, skipped := collect(t, s)
	if len(skipped) != 0 {
		t.Errorf("unexpected skips: %v", skipped)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	want := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	if !rows[0].Timestamp.Equal(want) {
		t.Errorf("timestamp = %v, want %v", rows[0].Timestamp, want)
	}
	if rows[0].EventID != "4624" || rows[0].Channel != "Security" || rows[0].Computer != "HOST-A" {
		t.Errorf("row not mapped: %+v", rows[0])
	}
	if rows[0].User != "S-1-5-18" || rows[0].RawXML != "<Event/>" {
		t.Errorf("optional columns not mapped: %+v", rows[0])
	}
}

func TestColumnAndTableAliasesAlign(t *testing.T) {
	// A conventional export using alternative spellings must still align.
	path := makeDB(t, "alias.db",
		`CREATE TABLE evtx_events (
			EventID INTEGER, TimeCreated TEXT, ProviderName TEXT,
			LogName TEXT, ComputerName TEXT, UserName TEXT, Xml TEXT
		)`,
		`INSERT INTO evtx_events VALUES (4688,'2026-07-21 11:00:00','Sysmon','Microsoft-Windows-Sysmon/Operational','HOST-B','alice','<E/>')`,
	)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("aliased schema should align: %v", err)
	}
	defer s.Close()
	if s.Table() != "evtx_events" {
		t.Errorf("table = %q, want evtx_events", s.Table())
	}
	rows, _ := collect(t, s)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	// An integer event id column must still map to the string field.
	if rows[0].EventID != "4688" || rows[0].Provider != "Sysmon" || rows[0].User != "alice" {
		t.Errorf("aliased row not mapped: %+v", rows[0])
	}
}

func TestOptionalColumnsMayBeAbsent(t *testing.T) {
	path := makeDB(t, "minimal.db",
		`CREATE TABLE events (event_id TEXT, timestamp TEXT, provider TEXT, channel TEXT, computer TEXT)`,
		`INSERT INTO events VALUES ('1102','2026-07-21T12:00:00Z','P','Security','HOST-C')`,
	)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("a db without the optional columns should still align: %v", err)
	}
	defer s.Close()
	rows, _ := collect(t, s)
	if len(rows) != 1 || rows[0].User != "" || rows[0].RawXML != "" {
		t.Errorf("expected one row with empty optional fields, got %+v", rows)
	}
}

func TestWrongSchemaIsRejectedAsSchemaError(t *testing.T) {
	cases := map[string]string{
		"no event table":   `CREATE TABLE something_else (a TEXT, b TEXT)`,
		"missing columns":  `CREATE TABLE events (event_id TEXT, timestamp TEXT)`,
		"unrelated schema": `CREATE TABLE users (id INTEGER, name TEXT)`,
	}
	for label, ddl := range cases {
		path := makeDB(t, "wrong.db", ddl)
		s, err := Open(path)
		if err == nil {
			s.Close()
			t.Errorf("%s: expected rejection, got a usable source", label)
			continue
		}
		if !IsSchemaError(err) {
			t.Errorf("%s: error = %v, want a schema error", label, err)
		}
		// The message must name the file and say it IS a database with a bad structure —
		// that distinction is the whole point for the user.
		if !strings.Contains(err.Error(), "SQLite database") || !strings.Contains(err.Error(), "wrong.db") {
			t.Errorf("%s: unhelpful message %q", label, err.Error())
		}
	}
}

func TestMissingColumnsAreNamed(t *testing.T) {
	path := makeDB(t, "partial.db", `CREATE TABLE events (event_id TEXT, timestamp TEXT)`)
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected a schema error")
	}
	for _, col := range []string{consts.DBColProvider, consts.DBColChannel, consts.DBColComputer} {
		if !strings.Contains(err.Error(), col) {
			t.Errorf("error should name the missing column %q: %s", col, err.Error())
		}
	}
}

func TestNonSQLiteFileIsNotASchemaError(t *testing.T) {
	// "This isn't a database at all" must be reported differently from "wrong schema".
	path := filepath.Join(t.TempDir(), "notadb.db")
	if err := os.WriteFile(path, []byte("this is plainly not a sqlite file"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected an open failure")
	}
	if IsSchemaError(err) {
		t.Errorf("a non-database should not be reported as a schema mismatch: %v", err)
	}
	if !strings.Contains(err.Error(), "not a readable SQLite database") {
		t.Errorf("unhelpful message: %v", err)
	}
}

func TestTimestampFormatsAccepted(t *testing.T) {
	want := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	path := makeDB(t, "times.db",
		`CREATE TABLE events (event_id TEXT, timestamp TEXT, provider TEXT, channel TEXT, computer TEXT)`,
		`INSERT INTO events VALUES
			('1','2026-07-21T10:00:00Z','p','c','h'),
			('2','2026-07-21 10:00:00','p','c','h'),
			('3','1784628000','p','c','h'),
			('4','1784628000000','p','c','h')`,
	)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, skipped := collect(t, s)
	if len(skipped) != 0 {
		t.Errorf("unexpected skips: %v", skipped)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	for i, r := range rows {
		if !r.Timestamp.Equal(want) {
			t.Errorf("row %d timestamp = %v, want %v", i+1, r.Timestamp, want)
		}
	}
}

func TestBadRowsAreSkippedNotFatal(t *testing.T) {
	// One unusable row must not cost the rest of the file — same tolerance as the EVTX path.
	path := makeDB(t, "mixed.db",
		`CREATE TABLE events (event_id TEXT, timestamp TEXT, provider TEXT, channel TEXT, computer TEXT)`,
		`INSERT INTO events VALUES
			('4624','2026-07-21T10:00:00Z','p','Security','HOST'),
			('','2026-07-21T10:00:01Z','p','Security','HOST'),
			('4625','not-a-timestamp','p','Security','HOST'),
			('4634','2026-07-21T10:00:02Z','p','Security','HOST')`,
	)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, skipped := collect(t, s)
	if len(rows) != 2 {
		t.Errorf("rows = %d, want the 2 good ones", len(rows))
	}
	if len(skipped) != 2 {
		t.Errorf("skipped = %d (%v), want 2", len(skipped), skipped)
	}
}

func TestStreamStopsWhenTheCallbackFails(t *testing.T) {
	s, err := Open(wellFormed(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	stop := errors.New("stop")
	seen := 0
	err = s.Stream(func(Row) error {
		seen++
		return stop
	}, nil)
	if !errors.Is(err, stop) {
		t.Errorf("err = %v, want the callback's error", err)
	}
	if seen != 1 {
		t.Errorf("callback ran %d times after failing, want 1", seen)
	}
}

func TestOpenIsReadOnly(t *testing.T) {
	// Evidence must never be modified by being read: reading must not leave -wal/-shm
	// siblings or change the file.
	path := wellFormed(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, s)
	s.Close()

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("reading modified the evidence file")
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(sidecar); err == nil {
			t.Errorf("reading created %s beside the evidence file", filepath.Base(sidecar))
		}
	}
}
