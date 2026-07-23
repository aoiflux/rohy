package dbsource

import (
	"database/sql"
	"fmt"
	"strings"

	"rohy/backend/consts"
)

// The provider/message catalogue schema (P22):
//
//	messages  ( id, event_id, provider_id, message )
//	providers ( id, name )
//
// It maps (provider, event id) → description. Note what it does NOT contain: no timestamp,
// no computer, no user, no channel. A row therefore says what event 4624 from provider X
// *means*, not that a logon *happened*. Rows are still worth ingesting — the ids and
// provider names are real — but they are undated by nature, which is why the pipeline tags
// them distinctly and keeps them out of timeline analysis instead of parking them at the
// epoch alongside genuine evidence.

// messageSchema records the resolved table and column names for one catalogue database.
type messageSchema struct {
	messages  string
	providers string

	eventID    string
	providerID string
	message    string

	provIDCol   string
	provNameCol string
}

// resolveMessageSchema reports whether the database matches the catalogue shape, resolving
// the real table/column names (case-insensitively, with the documented aliases).
func resolveMessageSchema(db *sql.DB, tables []string) (*messageSchema, bool) {
	byLower := map[string]string{}
	for _, t := range tables {
		byLower[strings.ToLower(t)] = t
	}
	messages, ok := byLower[consts.DBMessagesTable]
	if !ok {
		return nil, false
	}
	providers, ok := byLower[consts.DBProvidersTable]
	if !ok {
		return nil, false
	}

	mCols, err := listColumns(db, messages)
	if err != nil {
		return nil, false
	}
	pCols, err := listColumns(db, providers)
	if err != nil {
		return nil, false
	}

	find := func(cols []string, canonical string) (string, bool) {
		lower := map[string]string{}
		for _, c := range cols {
			lower[strings.ToLower(c)] = c
		}
		if c, ok := lower[canonical]; ok {
			return c, true
		}
		for _, alias := range consts.DBMessageColumnAliases[canonical] {
			if c, ok := lower[alias]; ok {
				return c, true
			}
		}
		return "", false
	}

	s := &messageSchema{messages: messages, providers: providers}
	var found bool
	if s.eventID, found = find(mCols, consts.DBColMessageEventID); !found {
		return nil, false
	}
	if s.providerID, found = find(mCols, consts.DBColMessageProviderI); !found {
		return nil, false
	}
	if s.message, found = find(mCols, consts.DBColMessageText); !found {
		return nil, false
	}
	if s.provIDCol, found = find(pCols, consts.DBColProviderID); !found {
		return nil, false
	}
	if s.provNameCol, found = find(pCols, consts.DBColProviderName); !found {
		return nil, false
	}
	return s, true
}

// streamCatalogue reads the catalogue, resolving each row's provider name through the join.
//
// The join is a LEFT join deliberately: a message row whose provider_id has no matching
// providers entry is still real data (the event id and message are intact), so it is
// ingested with an empty provider rather than silently disappearing — dropping rows because
// a lookup table is incomplete would quietly lose evidence.
func (s *Source) streamCatalogue(fn func(Row) error, onSkip func(string)) error {
	m := s.msg
	q := fmt.Sprintf(
		"SELECT m.%s, p.%s, m.%s FROM %s m LEFT JOIN %s p ON p.%s = m.%s ORDER BY m.rowid",
		quoteIdent(m.eventID), quoteIdent(m.provNameCol), quoteIdent(m.message),
		quoteIdent(m.messages), quoteIdent(m.providers),
		quoteIdent(m.provIDCol), quoteIdent(m.providerID),
	)
	rows, err := s.db.Query(q)
	if err != nil {
		return fmt.Errorf(consts.MsgDBQueryFailed, s.path, err)
	}
	defer rows.Close()

	for rows.Next() {
		var eventIDVal, providerVal, messageVal any
		if err := rows.Scan(&eventIDVal, &providerVal, &messageVal); err != nil {
			if onSkip != nil {
				onSkip(fmt.Sprintf(consts.MsgDBRowFail, err))
			}
			continue
		}
		// event_id is INTEGER in this schema but a string in the event model; normalizing
		// to its decimal form is what lets 4624 from a .db match 4624 from an .evtx.
		eventID := strings.TrimSpace(asString(eventIDVal))
		if eventID == "" {
			if onSkip != nil {
				onSkip(fmt.Sprintf(consts.MsgDBRowFail, errEmptyEventID))
			}
			continue
		}
		// Timestamp is deliberately left zero: the schema has none, and guessing one would
		// fabricate evidence.
		if err := fn(Row{
			EventID:  eventID,
			Provider: strings.TrimSpace(asString(providerVal)),
			Message:  asString(messageVal),
		}); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf(consts.MsgDBQueryFailed, s.path, err)
	}
	return nil
}
