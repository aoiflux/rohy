package graphene

// The engine's half of redacting an event.
//
// These are primitives, each one step, each safe to repeat. The order they run in — and the
// journal that makes a redaction stopped part-way finishable — belongs to backend/redaction, which
// composes them. Nothing here decides what to redact; everything here makes one removal happen,
// recorded, or says why it did not.
//
// 🔒 graphene's property redaction strips an event's WHOLE record: the node keeps its id, its label
// and its edges, and nothing else. For a rohy event that is all of its content — timestamp,
// computer, user, hashes, correlation keys, and the reference to its raw record. Writing the safe
// fields back afterwards would be an unrecorded edit, and would break the ledger's record of what
// survived, so nothing in rohy ever writes a record onto a stripped event (isStrippedEvent).

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/payload"
	"rohy/backend/utils"

	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/store"
)

// RedactionRecord is one ledger entry as rohy reads it.
type RedactionRecord struct {
	Seq    uint64    `json:"seq"`
	Scope  string    `json:"scope"` // consts.RedactionScope*
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
	NodeID uint64    `json:"node_id,omitempty"`
	EdgeID uint64    `json:"edge_id,omitempty"`
}

// RedactionSubject is what redacting one event touches, read before anything changes.
type RedactionSubject struct {
	ID             uint64
	HashNormalized string
	HashRaw        string
	Payload        payload.Ref
	// Stripped says the event's record is already gone: a redaction that got that far before.
	Stripped bool
	// RuleRelations are the incident relations a rule made. Their basis quotes the event's values,
	// so they go with it (owner decision, 2026-11-04).
	RuleRelations []uint64
	// AnalystRelations are every other incident relation: drawn or restored by an analyst. They
	// stay, and the preview shows them.
	AnalystRelations []*Relation
}

// isStrippedEvent reports whether n is an event a redaction stripped: labelled an event, holding no
// record. toNode always writes the full record, so nothing else in rohy produces one.
func isStrippedEvent(n *store.Node) bool {
	return len(n.Properties) == 0 && slices.Contains(n.Labels, consts.NodeEvent)
}

// eventNode reads id and refuses anything that is not an event.
func (s *Store) eventNode(id uint64) (*store.Node, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	n, err := g.GetNode(store.NodeID(id))
	if err != nil {
		return nil, err
	}
	if !slices.Contains(n.Labels, consts.NodeEvent) {
		return nil, fmt.Errorf(consts.MsgNotAnEvent, id)
	}
	return n, nil
}

// GoneError reports an event or relation that no longer exists, with no recorded redaction of it:
// something removed it outside a redaction. Its content leaves the image at the next seal, but the
// ledger does not record the removal.
type GoneError struct{ msg string }

func (e *GoneError) Error() string { return e.msg }

// RedactionSubject reads what a redaction of id would touch.
func (s *Store) RedactionSubject(id uint64) (RedactionSubject, error) {
	n, err := s.eventNode(id)
	if err != nil {
		return RedactionSubject{}, err
	}
	sub := RedactionSubject{ID: id, Stripped: isStrippedEvent(n)}
	if !sub.Stripped {
		e, err := eventFromNode(n)
		if err != nil {
			return RedactionSubject{}, err
		}
		sub.HashNormalized, sub.HashRaw, sub.Payload = e.HashNormalized, e.HashRaw, e.Payload
	}
	rels, err := s.RelationsOf(id)
	if err != nil {
		return RedactionSubject{}, err
	}
	for _, r := range rels {
		// Exactly the rule-made ones. A relation of any other origin — drawn, restored from a
		// snapshot, or from a build this one does not know — is kept: removing it is the
		// irreversible direction, and the preview lists it.
		if r.CreatedBy == consts.CreatedBySystem {
			sub.RuleRelations = append(sub.RuleRelations, r.ID)
		} else {
			sub.AnalystRelations = append(sub.AnalystRelations, r)
		}
	}
	return sub, nil
}

// EventsSharing returns every event holding the same content as a subject: the same identity hash
// (the same record ingested twice without dedup) or the same raw record hash (the same record read
// from another path, or from the live channel). The subject itself is included.
//
// The raw hash is not indexed, so this reads every event's record. It runs once per redaction.
func (s *Store) EventsSharing(hashNormalized, hashRaw string) ([]uint64, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	seen := map[uint64]bool{}
	if hashNormalized != "" {
		ids, err := g.NodesByProperty(consts.PropHashNormalized, []byte(hashNormalized))
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			seen[uint64(id)] = true
		}
	}
	if hashRaw != "" {
		all, err := g.QueryNodeIDs(store.NodeQuery{Types: []store.NodeType{consts.NodeEvent}})
		if err != nil {
			return nil, err
		}
		err = eachNode(g, all, consts.ReadBatchBytes, func(n *store.Node) error {
			var h struct {
				HashRaw string `json:"hash_raw"`
			}
			if len(n.Properties) == 0 || json.Unmarshal(n.Properties, &h) != nil {
				return nil
			}
			if h.HashRaw == hashRaw {
				seen[uint64(n.ID)] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]uint64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	slices.Sort(out)
	return out, nil
}

// PayloadCopies returns every record in the payload log holding own's content: own itself, and
// copies nothing references any more — left by a batch whose commit failed, a resumed ingest, or a
// deleted event. A copy is the same bytes, or a record whose raw record hashes to hashRaw.
//
// It reads the whole log. It runs once per redaction.
func (s *Store) PayloadCopies(hashRaw string, own payload.Ref) ([]payload.Ref, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	var ownBytes []byte
	if !own.IsZero() {
		b, err := s.payloadLog().Read(own)
		var red *payload.RedactedError
		switch {
		case errors.As(err, &red):
			// Already tombstoned: the copies are what is left to find.
		case err != nil:
			return nil, err
		default:
			ownBytes = b
		}
	}
	var out []payload.Ref
	_, err := s.payloadLog().Walk(func(r payload.Ref, b []byte) error {
		if ownBytes != nil && bytes.Equal(b, ownBytes) {
			out = append(out, r)
			return nil
		}
		if hashRaw == "" || len(b) == 0 || b[0] != '{' {
			return nil
		}
		var p payloadBlob
		if json.Unmarshal(b, &p) == nil && p.RawXML != "" && utils.HashString(p.RawXML) == hashRaw {
			out = append(out, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The reference the event holds is always in the set, even if the walk could not reach it, so
	// a log damaged earlier on cannot make the event's own record the one copy left behind.
	if !own.IsZero() && !slices.Contains(out, own) {
		out = append(out, own)
	}
	return out, nil
}

// TombstonePayload erases each record in place, naming owner (payload.Store.Redact).
func (s *Store) TombstonePayload(owner uint64, refs ...payload.Ref) error {
	if err := s.ensure(); err != nil {
		return err
	}
	return s.payloadLog().Redact(owner, refs...)
}

// RedactRelation removes one relation, recorded in the ledger. A relation already removed by a
// recorded redaction is success, so a retry is safe; one that never existed is not.
func (s *Store) RedactRelation(id uint64, reason string) error {
	fs, err := s.forensics()
	if err != nil {
		return err
	}
	defer s.bumpVersion()
	_, err = fs.RedactEdge(store.EdgeID(id), disk.RedactionRequest{ActorID: s.posture().Actor, Reason: reason})
	err = s.noteStopped(err)
	if _, ok := errors.AsType[*store.ErrNotFound](err); ok {
		recs, rerr := s.redactionRecords()
		if rerr != nil {
			return rerr
		}
		for _, r := range recs {
			if r.EdgeID == id && r.Scope == consts.RedactionScopeEdge {
				return nil
			}
		}
		return &GoneError{fmt.Sprintf(consts.MsgRedactRelationGone, id)}
	}
	return err
}

// RedactEventRecord strips the event's record, recorded in the ledger, and purges its index
// entries. hashNormalized is the identity the event had, from the journal: after the strip it is
// the one way left to check that no index entry still finds the event.
//
// 🔒 Safe to repeat, and a repeat FINISHES rather than skips. The engine strips the record and purges
// the index as two writes; a crash between them leaves a bare node whose index still holds its
// values, which the next seal would carry into the image. A bare node with a ledger record behind
// it is therefore purged again; a bare node with none is damage, and refused.
func (s *Store) RedactEventRecord(id uint64, hashNormalized, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return disk.ErrRedactionUnexplained
	}
	fs, err := s.forensics()
	if err != nil {
		return err
	}
	n, err := s.eventNode(id)
	if _, ok := errors.AsType[*store.ErrNotFound](err); ok {
		return &GoneError{fmt.Sprintf(consts.MsgRedactEventGone, id)}
	}
	if err != nil {
		return err
	}
	defer s.bumpVersion()

	if isStrippedEvent(n) {
		if _, ok, err := s.RedactionFor(id); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf(consts.MsgRedactBareUnrecorded, id)
		}
		if err := fs.PurgeNodeIndex(store.NodeID(id)); err != nil {
			return err
		}
	} else if _, err := fs.RedactNodeProperties(store.NodeID(id), disk.RedactionRequest{ActorID: s.posture().Actor, Reason: reason}); s.noteStopped(err) != nil {
		return err
	}

	if hashNormalized != "" {
		g, err := s.graph()
		if err != nil {
			return err
		}
		ids, err := g.NodesByProperty(consts.PropHashNormalized, []byte(hashNormalized))
		if err != nil {
			return err
		}
		if slices.Contains(ids, store.NodeID(id)) {
			return fmt.Errorf(consts.MsgRedactIndexSurvived, id)
		}
	}
	return nil
}

// RedactionFor returns the latest ledger record of a redaction of event id, if there is one.
func (s *Store) RedactionFor(id uint64) (RedactionRecord, bool, error) {
	recs, err := s.redactionRecords()
	if err != nil {
		return RedactionRecord{}, false, err
	}
	for _, rec := range slices.Backward(recs) {
		if rec.NodeID == id && rec.EdgeID == 0 {
			return rec, true, nil
		}
	}
	return RedactionRecord{}, false, nil
}

// redactionRecords reads the ledger, oldest first.
func (s *Store) redactionRecords() ([]RedactionRecord, error) {
	fs, err := s.forensics()
	if err != nil {
		return nil, err
	}
	recs, err := fs.Redactions()
	if err != nil {
		return nil, err
	}
	out := make([]RedactionRecord, len(recs))
	for i, r := range recs {
		out[i] = RedactionRecord{
			Seq: r.Seq, Scope: scopeName(r.Scope), At: time.Unix(0, r.UnixNano).UTC(),
			Reason: r.Reason, NodeID: uint64(r.NodeID), EdgeID: uint64(r.EdgeID),
		}
	}
	return out, nil
}

// LedgerFramingClean checks that the ledger's length-prefixed records end exactly where the file
// does. The engine appends after whatever is there, so a torn tail left by an interrupted write
// would misframe the next record — and every compaction after it, which reads the ledger, would
// fail. Nothing is appended to a ledger that does not pass. (Upstream note: graphene does not
// truncate a torn tail on open.)
func (s *Store) LedgerFramingClean() error {
	if _, err := s.forensics(); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, consts.RedactionLedgerFile))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	end := 0
	for end+4 <= len(data) {
		n := int(binary.LittleEndian.Uint32(data[end:]))
		if n < 0 || end+4+n > len(data) {
			break
		}
		end += 4 + n
	}
	if end != len(data) {
		return fmt.Errorf(consts.MsgRedactLedgerTorn, len(data), end)
	}
	return nil
}
