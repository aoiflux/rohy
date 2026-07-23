package evtx

import (
	"context"
	"encoding/xml"
	"path/filepath"
	"strings"

	"rohy/backend/consts"
	"rohy/backend/dbsource"
	"rohy/backend/graphene"
	"rohy/backend/utils"
)

// SQLite (.db) ingestion (P17). A .db carrying EVTX data is read by the dbsource package
// and then joined to the SAME sink as the binary and live readers, so dedup, batching,
// progress, cancellation, pause/resume and persistence all behave identically regardless of
// where an event came from.

// ingestDB streams a schema-validated .db into the shared persistence sink. A file that is
// not a database, or is a database without a recognized EVTX schema, fails here — before
// anything is written — so a rejected file ingests nothing at all.
func ingestDB(ctx context.Context, opts Options, sink EventSink, reporter Reporter) (Summary, error) {
	src, err := dbsource.Open(opts.Path)
	if err != nil {
		return Summary{}, err
	}
	defer src.Close()

	// A row count up front lets the UI show a real progress fraction rather than an
	// indeterminate bar. A failure here is not fatal: it only costs the estimate.
	batches := 0
	if total, err := src.Count(); err == nil && total > 0 {
		batches = (total + consts.DBRowBatch - 1) / consts.DBRowBatch
	}
	reporter.Started(consts.SourceFile, batches)

	// A catalogue database describes event ids rather than recording occurrences, so its
	// rows are labelled distinctly and normalized differently (no fabricated timestamp).
	catalogue := src.Kind() == dbsource.KindMessageCatalogue
	if catalogue {
		opts.SourceType = consts.SourceTypeMessageDB
	}

	resultCh := make(chan chunkResult, consts.ChunkQueueDepth)
	go func() {
		defer close(resultCh)

		pending := make([]*graphene.Event, 0, consts.DBRowBatch)
		var skips []string

		send := func() bool {
			if len(pending) == 0 && len(skips) == 0 {
				return true
			}
			res := chunkResult{events: pending, normErrs: skips}
			select {
			case resultCh <- res:
				pending = make([]*graphene.Event, 0, consts.DBRowBatch)
				skips = nil
				return true
			case <-ctx.Done():
				return false
			}
		}

		streamErr := src.Stream(func(row dbsource.Row) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			pending = append(pending, normalizeDBRow(row, catalogue))
			if len(pending) >= consts.DBRowBatch && !send() {
				return context.Canceled
			}
			return nil
		}, func(msg string) { skips = append(skips, msg) })

		if streamErr != nil && ctx.Err() == nil {
			skips = append(skips, streamErr.Error())
		}
		send()
	}()

	return runSink(ctx, opts, sink, reporter, resultCh, batches)
}

// normalizeDBRow maps an extracted row to the persistence model, computing
// hash_normalized over the SAME ordered fields as the .evtx and live normalizers. That
// parity is what makes deduplication work ACROSS sources: the same event ingested from a
// .evtx file and from a .db collapses into one canonical node instead of two.
//
// The mapped columns are authoritative — they are the documented schema. A raw_xml column,
// when present and parseable, is used only to enrich ParsedFields; it never overrides a
// column, so a stray XML blob cannot quietly rewrite the evidence.
//
// Catalogue rows (P22) take a different identity: they carry no timestamp, computer or
// user, so the usual hash would make every message for one (provider, event id) collide and
// silently keep whichever arrived first. They are hashed over what they actually contain —
// provider, event id and message — so re-importing the same catalogue collapses while two
// genuinely different messages stay distinct.
func normalizeDBRow(row dbsource.Row, catalogue bool) *graphene.Event {
	ev := &graphene.Event{
		EventID:            row.EventID,
		Timestamp:          row.Timestamp,
		Provider:           row.Provider,
		Channel:            row.Channel,
		Computer:           row.Computer,
		User:               row.User,
		RawXML:             row.RawXML,
		ParsedFields:       fieldsFromRawXML(row.RawXML),
		DeduplicationCount: consts.DefaultDeduplicationCount,
	}

	if catalogue {
		// The message is the row's substance, so it is what gets stored and shown.
		ev.RawXML = row.Message
		ev.ParsedFields = map[string]string{consts.DBColMessageText: row.Message}
		ev.HashRaw = utils.HashString(row.Message)
		// A catalogue row's substance is its message, and it carries no timestamp, so the
		// message is the discriminator that keeps two distinct entries from collapsing into
		// one another under the undated rule.
		ev.ComputeNormalizedHash(row.Message)
		return ev
	}

	ev.HashRaw = utils.HashString(ev.RawXML)
	// Identity is owned by the schema, not by each normalizer: the rule has two branches
	// and three parsers, and three copies of it would drift. SourceIdentifier is empty here
	// and the sink recomputes once it is known.
	ev.ComputeNormalizedHash()
	return ev
}

// fieldsFromRawXML extracts EventData/UserData fields from a raw event XML payload, or nil
// when the column is absent or does not parse as event XML.
func fieldsFromRawXML(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	var doc xmlEventDoc
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil
	}
	return xmlEventFields(doc)
}

// IsDBPath reports whether a path should be read as a SQLite database rather than as an
// EVTX binary. Exported so the binding layer can classify a selection without duplicating
// the extension rule.
func IsDBPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), consts.DBExt)
}

// IsDBSchemaError reports whether an ingestion failure was "this is a SQLite database, but
// not one holding a recognized EVTX structure" — as opposed to an unreadable file. The
// binding layer uses it to pick the right error code, so the user is told which of the two
// problems they actually have without api needing to know about the dbsource package.
func IsDBSchemaError(err error) bool {
	return dbsource.IsSchemaError(err)
}
