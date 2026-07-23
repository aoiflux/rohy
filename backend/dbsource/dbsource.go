// Package dbsource reads EVTX event data out of a SQLite .db file (P17).
//
// There is no universal standard for "EVTX stored in SQLite", so this package does NOT try
// to auto-detect an arbitrary schema — mis-mapping columns would silently corrupt forensic
// evidence, which is worse than refusing the file. Instead it validates the database
// against the known, documented shape in consts (an events table with a column per EVTX
// field, matched case-insensitively with a small set of conventional aliases). A database
// that does not align is rejected with a precise reason and nothing is ingested.
//
// The package only reads and maps rows; normalization, hashing, dedup, batching and
// persistence all stay in the shared ingestion pipeline, so a .db-sourced event is
// indistinguishable from the same event read out of an .evtx file.
package dbsource

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"rohy/backend/consts"

	_ "modernc.org/sqlite" // pure-Go driver, registered as consts.DBDriver
)

// SchemaError means the file IS a SQLite database but does not carry a recognized EVTX
// structure. It is distinct from an open/read failure so the UI can tell the user which
// problem they actually have.
type SchemaError struct {
	Path   string
	Reason string
}

func (e *SchemaError) Error() string {
	return fmt.Sprintf(consts.MsgDBInvalidSchema, e.Path, e.Reason)
}

// IsSchemaError reports whether err is the "valid database, wrong schema" case.
func IsSchemaError(err error) bool {
	var se *SchemaError
	return errors.As(err, &se)
}

// errEmptyEventID marks a row with no usable event id — the one field no schema can do
// without, since it is what identifies the record at all.
var errEmptyEventID = errors.New("empty event id")

// Row is one extracted event, still in source form. Turning it into a normalized
// graphene.Event (and hashing it) is the ingestion pipeline's job, not this package's.
//
// A zero Timestamp means the source genuinely carries no time for this row (the message
// catalogue schema has none). That is recorded as unknown and never guessed: inventing a
// timestamp would fabricate evidence, and downstream the event is kept out of timeline
// analysis precisely because it has none.
type Row struct {
	EventID   string
	Timestamp time.Time
	Provider  string
	Channel   string
	Computer  string
	User      string
	RawXML    string
	// Message is the human-readable description from a catalogue database (P22); empty for
	// the events schema.
	Message string
}

// Kind identifies which known shape a database matched, so the caller can normalize and
// label its rows appropriately.
type Kind string

const (
	// KindEvents is the P17 events table: real, dated event records.
	KindEvents Kind = "events"
	// KindMessageCatalogue is the P22 messages+providers pair: undated descriptions of what
	// event ids mean.
	KindMessageCatalogue Kind = "message_catalogue"
)

// Source is an opened, schema-validated .db ready to stream rows.
type Source struct {
	db   *sql.DB
	path string
	kind Kind
	// table and cols describe the resolved events table (KindEvents only).
	table string
	cols  map[string]string
	// msg describes the resolved catalogue tables (KindMessageCatalogue only).
	msg *messageSchema
}

// Open opens the database and resolves it against the known schemas, in order. It returns a
// *SchemaError when the file is a database but matches none of them, and a plain error when
// it cannot be opened or read as SQLite at all. A non-nil Source must be closed by the caller.
func Open(path string) (*Source, error) {
	db, err := sql.Open(consts.DBDriver, dsn(path))
	if err != nil {
		return nil, fmt.Errorf(consts.MsgDBNotSQLite, path, err)
	}
	// sql.Open is lazy; force a read so a non-SQLite file fails here rather than mid-stream.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf(consts.MsgDBNotSQLite, path, err)
	}

	tables, err := listTables(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf(consts.MsgDBNotSQLite, path, err)
	}

	// The events schema is tried FIRST: it carries real dated evidence, so a file that
	// could satisfy both should be read as events rather than as a catalogue.
	events := resolveEventsSchema(db, tables)
	if events.ok() {
		return &Source{db: db, path: path, kind: KindEvents, table: events.table, cols: events.cols}, nil
	}
	if ms, ok := resolveMessageSchema(db, tables); ok {
		return &Source{db: db, path: path, kind: KindMessageCatalogue, msg: ms}, nil
	}

	db.Close()
	// A file with an events table but missing columns gets the specific diagnostic: it was
	// clearly trying to be an events database, so naming the absent columns is far more
	// useful than telling the user it matched nothing.
	if events.found {
		return nil, &SchemaError{
			Path:   path,
			Reason: fmt.Sprintf(consts.MsgDBMissingColumns, events.table, strings.Join(events.missing, ", ")),
		}
	}
	return nil, &SchemaError{
		Path: path,
		Reason: fmt.Sprintf(consts.MsgDBNoKnownSchema,
			strings.Join([]string{consts.MsgDBSchemaEvents, consts.MsgDBSchemaMessage}, "; ")),
	}
}

// Kind reports which known schema the database matched.
func (s *Source) Kind() Kind { return s.kind }

// dsn builds a read-only connection string: an evidence file must never be modified by
// being read, and read-only also avoids creating -wal/-shm siblings next to it.
func dsn(path string) string {
	return "file:" + path + "?mode=ro&_pragma=query_only(true)"
}

// Close releases the database handle.
func (s *Source) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Table returns the event table this source resolved to (useful in diagnostics).
func (s *Source) Table() string { return s.table }

// Count returns the number of rows, so ingestion can report a total up front.
func (s *Source) Count() (int, error) {
	table := s.table
	if s.kind == KindMessageCatalogue {
		table = s.msg.messages
	}
	var n int
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(table))
	if err := s.db.QueryRow(q).Scan(&n); err != nil {
		return 0, fmt.Errorf(consts.MsgDBQueryFailed, s.path, err)
	}
	return n, nil
}

// Stream reads every row in order, invoking fn for each. Rows are streamed through a cursor
// rather than materialized, so a huge .db costs bounded memory exactly like the EVTX path.
// A row that cannot be mapped is reported through onSkip and skipped, mirroring the EVTX
// reader's per-record tolerance; fn returning an error stops the scan.
func (s *Source) Stream(fn func(Row) error, onSkip func(string)) error {
	if s.kind == KindMessageCatalogue {
		return s.streamCatalogue(fn, onSkip)
	}
	selected := s.selectColumns()
	q := fmt.Sprintf("SELECT %s FROM %s", strings.Join(quoteAll(selected), ", "), quoteIdent(s.table))
	rows, err := s.db.Query(q)
	if err != nil {
		return fmt.Errorf(consts.MsgDBQueryFailed, s.path, err)
	}
	defer rows.Close()

	for rows.Next() {
		// Scan into raw any values: SQLite columns are dynamically typed, so a timestamp
		// may arrive as text or as an integer depending on who wrote the file.
		vals := make([]any, len(selected))
		ptrs := make([]any, len(selected))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			if onSkip != nil {
				onSkip(fmt.Sprintf(consts.MsgDBRowFail, err))
			}
			continue
		}
		row, err := s.mapRow(selected, vals)
		if err != nil {
			if onSkip != nil {
				onSkip(fmt.Sprintf(consts.MsgDBRowFail, err))
			}
			continue
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf(consts.MsgDBQueryFailed, s.path, err)
	}
	return nil
}

// selectColumns returns the concrete column names to read, in canonical order.
func (s *Source) selectColumns() []string {
	var out []string
	for _, canonical := range append(append([]string{}, consts.DBRequiredColumns...), consts.DBOptionalColumns...) {
		if actual, ok := s.cols[canonical]; ok {
			out = append(out, actual)
		}
	}
	return out
}

// mapRow turns one scanned row into a Row, applying the canonical column order used to
// build the query.
func (s *Source) mapRow(selected []string, vals []any) (Row, error) {
	byCanonical := map[string]any{}
	i := 0
	for _, canonical := range append(append([]string{}, consts.DBRequiredColumns...), consts.DBOptionalColumns...) {
		if _, ok := s.cols[canonical]; !ok {
			continue
		}
		byCanonical[canonical] = vals[i]
		i++
	}

	ts, err := parseTime(byCanonical[consts.DBColTimestamp])
	if err != nil {
		return Row{}, err
	}
	eventID := strings.TrimSpace(asString(byCanonical[consts.DBColEventID]))
	if eventID == "" {
		return Row{}, errEmptyEventID
	}
	return Row{
		EventID:   eventID,
		Timestamp: ts,
		Provider:  asString(byCanonical[consts.DBColProvider]),
		Channel:   asString(byCanonical[consts.DBColChannel]),
		Computer:  asString(byCanonical[consts.DBColComputer]),
		User:      asString(byCanonical[consts.DBColUser]),
		RawXML:    asString(byCanonical[consts.DBColRawXML]),
	}, nil
}

// eventsMatch is the outcome of testing a database against the P17 events shape. It carries
// the near-miss detail (table present, columns missing) so a file that clearly MEANT to be
// an events database gets a precise diagnostic instead of a generic "unrecognized" — a
// missing column is a far more actionable message than "no known schema".
type eventsMatch struct {
	table   string
	cols    map[string]string
	found   bool     // the events table exists
	missing []string // required columns absent from it
}

func (m eventsMatch) ok() bool { return m.found && len(m.missing) == 0 }

// resolveEventsSchema tests the database against the P17 events shape.
func resolveEventsSchema(db *sql.DB, tables []string) eventsMatch {
	table, ok := pickTable(tables)
	if !ok {
		return eventsMatch{}
	}
	actual, err := listColumns(db, table)
	if err != nil {
		return eventsMatch{}
	}
	cols, missing := matchColumns(actual)
	return eventsMatch{table: table, cols: cols, found: true, missing: missing}
}

// listTables returns the database's table names.
func listTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type IN ('table','view')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// pickTable chooses the first alias present in the database (case-insensitive), preserving
// the alias precedence in consts so resolution is deterministic.
func pickTable(tables []string) (string, bool) {
	byLower := map[string]string{}
	for _, t := range tables {
		byLower[strings.ToLower(t)] = t
	}
	for _, alias := range consts.DBTableAliases {
		if actual, ok := byLower[alias]; ok {
			return actual, true
		}
	}
	return "", false
}

// listColumns returns the column names of a table via PRAGMA table_info.
func listColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteIdent(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var (
			cid         int
			name, ctype string
			notnull, pk int
			dflt        sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// matchColumns maps canonical column names onto the database's actual ones and reports any
// REQUIRED canonical column that has no match.
func matchColumns(actual []string) (map[string]string, []string) {
	byLower := map[string]string{}
	for _, c := range actual {
		byLower[strings.ToLower(c)] = c
	}

	find := func(canonical string) (string, bool) {
		if c, ok := byLower[canonical]; ok {
			return c, true
		}
		for _, alias := range consts.DBColumnAliases[canonical] {
			if c, ok := byLower[alias]; ok {
				return c, true
			}
		}
		return "", false
	}

	cols := map[string]string{}
	var missing []string
	for _, canonical := range consts.DBRequiredColumns {
		if c, ok := find(canonical); ok {
			cols[canonical] = c
		} else {
			missing = append(missing, canonical)
		}
	}
	for _, canonical := range consts.DBOptionalColumns {
		if c, ok := find(canonical); ok {
			cols[canonical] = c
		}
	}
	sort.Strings(missing)
	return cols, missing
}

// asString renders a dynamically-typed SQLite value as text.
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(t)
	}
}

// parseTime interprets a timestamp column, which SQLite may hand back as text, as an
// integer (Unix seconds or milliseconds), or already as a time.Time depending on the
// driver and how the file was written.
func parseTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t.UTC(), nil
	case int64:
		return fromUnix(t), nil
	case float64:
		return fromUnix(int64(t)), nil
	}

	s := strings.TrimSpace(asString(v))
	if s == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	// A numeric string is an epoch value written as text.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return fromUnix(n), nil
	}
	for _, layout := range consts.DBTimeLayouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", s)
}

// fromUnix reads an epoch value as seconds or milliseconds.
func fromUnix(n int64) time.Time {
	if n > int64(consts.DBUnixMillisThreshold) {
		return time.UnixMilli(n).UTC()
	}
	return time.Unix(n, 0).UTC()
}

// quoteIdent quotes a SQL identifier so a table/column name can never be interpreted as
// syntax. Identifiers here come from the database's own catalog, but quoting keeps the
// generated SQL correct for names with spaces or reserved words.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quoteIdent(n)
	}
	return out
}
