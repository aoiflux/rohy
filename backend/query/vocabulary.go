// Package query is the read-only query console: a structured builder over
// the case's events and relations, with counts by field.
//
// 🔒 No expression language. A query is a subject, conditions ANDed together, an optional
// field to count by, and a limit — data, never code — and every part of it is checked against the
// vocabulary below before anything is read.
//
// 🔒 It reads only. Run takes a Reader, the read-only slice of the store, so nothing here can reach
// a write; api.QueryAPI holds the same interface, never *graphene.Store.
package query

import (
	"slices"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// Reader is the slice of the store the console reads. Every method is a read; the tests hold that.
type Reader interface {
	QueryEvents(f graphene.EventFilter) ([]*graphene.Event, error)
	CountEvents(f graphene.EventFilter) (int, error)
	CountEventsBy(f graphene.EventFilter, field string) (graphene.EventGroups, error)
	ScanRelations(f graphene.RelationFilter, visit func(*graphene.Relation) error) (graphene.ScanStats, error)
	Version() uint64
}

// Vocabulary is everything a query may say, for the page to build its controls from.
type Vocabulary struct {
	Subjects       []Subject `json:"subjects"`
	DefaultLimit   int       `json:"default_limit"`
	MaxLimit       int       `json:"max_limit"`
	MaxConditions  int       `json:"max_conditions"`
	MaxValueLength int       `json:"max_value_length"`
}

// Subject is what a query reads: events or relations.
type Subject struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Fields  []Field  `json:"fields"`
	Columns []Column `json:"columns"`
}

// Field is one thing a condition can test or a count can group by.
type Field struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // consts.QueryKind*
	// Ops are the operators a condition on this field may use; none means it can only be counted by.
	Ops    []string `json:"ops"`
	Values []string `json:"values,omitempty"` // an enum's choices
	// Default is the value applied when no condition names the field.
	Default   string `json:"default,omitempty"`
	Groupable bool   `json:"groupable"`
	Help      string `json:"help,omitempty"`
}

// Column is one column of a row result, in display order.
type Column struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Event fields beyond the stored property names (consts.Prop*): the ones that are conditions on the
// match rather than properties of one event.
const (
	fieldTime        = "time"
	fieldText        = "text"
	fieldOccurrences = "occurrences"
	fieldRelations   = "relations"
	fieldUndated     = "undated"
	fieldEvent       = "event"
)

var (
	eq       = []string{consts.QueryOpEq}
	timeOps  = []string{consts.QueryOpGte, consts.QueryOpLte}
	undatedW = []string{consts.QueryUndatedInclude, consts.QueryUndatedExclude, consts.QueryUndatedOnly}
)

var eventFields = []Field{
	{Key: consts.PropEventID, Label: "Event ID", Kind: consts.QueryKindText, Ops: eq, Groupable: true},
	{Key: consts.PropProvider, Label: "Provider", Kind: consts.QueryKindText, Ops: eq, Groupable: true},
	{Key: consts.PropChannel, Label: "Channel", Kind: consts.QueryKindText, Ops: eq, Groupable: true},
	{Key: consts.PropComputer, Label: "Computer", Kind: consts.QueryKindText, Ops: eq, Groupable: true},
	{Key: consts.PropUser, Label: "User", Kind: consts.QueryKindText, Ops: eq, Groupable: true},
	{Key: consts.PropSourceType, Label: "Source type", Kind: consts.QueryKindEnum, Ops: eq, Groupable: true,
		Values: []string{consts.SourceTypeSingleEVTX, consts.SourceTypeMultiEVTX, consts.SourceTypeLiveSystem,
			consts.SourceTypeSQLiteDB, consts.SourceTypeMessageDB}},
	{Key: consts.PropSourceIdentifier, Label: "Source", Kind: consts.QueryKindText, Ops: eq, Groupable: true,
		Help: "The file path or channel the event was read from, exactly."},
	{Key: fieldTime, Label: "Time", Kind: consts.QueryKindTime, Ops: timeOps,
		Help: "RFC 3339, for example 2026-01-31T09:00:00Z. Both ends are inclusive."},
	{Key: fieldText, Label: "Text", Kind: consts.QueryKindText, Ops: []string{consts.QueryOpContains},
		Help: "Anywhere in the event's searchable text, ignoring case."},
	{Key: fieldOccurrences, Label: "Occurrences", Kind: consts.QueryKindNumber, Ops: []string{consts.QueryOpGte},
		Help: "How many copies of the event were collapsed into it."},
	{Key: fieldRelations, Label: "Has relations", Kind: consts.QueryKindEnum, Ops: eq,
		Values: []string{consts.RelationFilterAny, consts.RelationFilterSystem, consts.RelationFilterUser},
		Help:   "any: some relation; system: one a rule made; user: one an analyst drew."},
	{Key: fieldUndated, Label: "Undated events", Kind: consts.QueryKindEnum, Ops: eq, Values: undatedW,
		Default: consts.QueryUndatedInclude,
		Help:    "Events with no timestamp, redacted ones among them. Included unless you say otherwise."},
}

var eventColumns = []Column{
	{"id", "ID"}, {"timestamp", "Time"}, {"event_id", "Event ID"}, {"provider", "Provider"},
	{"channel", "Channel"}, {"computer", "Computer"}, {"user", "User"}, {"source_type", "Source type"},
	{"source_identifier", "Source"}, {"occurrences", "Occurrences"},
}

var relationFields = []Field{
	{Key: consts.PropRuleID, Label: "Rule", Kind: consts.QueryKindText, Ops: eq, Groupable: true,
		Help: "The rule's id, as the rule library shows it."},
	{Key: consts.PropAlgorithm, Label: "Algorithm", Kind: consts.QueryKindEnum, Ops: eq, Groupable: true,
		Values: consts.AlgorithmNames()},
	{Key: consts.PropRelationType, Label: "Type", Kind: consts.QueryKindText, Ops: eq, Groupable: true},
	{Key: consts.PropCreatedBy, Label: "Made by", Kind: consts.QueryKindEnum, Ops: eq, Groupable: true,
		Values: []string{consts.CreatedBySystem, consts.CreatedByUser},
		Help:   "system: inferred by a rule; user: asserted by an analyst."},
	{Key: consts.PropGraphID, Label: "Graph", Kind: consts.QueryKindNumber, Ops: eq, Groupable: true},
	{Key: consts.PropMatchID, Label: "Match", Kind: consts.QueryKindText, Ops: eq,
		Help: "Every edge one occurrence of a rule produced."},
	{Key: fieldEvent, Label: "Event", Kind: consts.QueryKindNumber, Ops: eq,
		Help: "An event's ID: relations at either end of it."},
}

var relationColumns = []Column{
	{"id", "ID"}, {"from", "From"}, {"to", "To"}, {"rule_id", "Rule"}, {"algorithm", "Algorithm"},
	{"relation_type", "Type"}, {"created_by", "Made by"}, {"confidence", "Confidence"}, {"graph_id", "Graph"},
	{"match_id", "Match"}, {"step", "Step"}, {"basis", "Basis"},
}

// Describe returns the vocabulary. It is built fresh, so a caller cannot change what Run accepts.
func Describe() Vocabulary {
	return Vocabulary{
		Subjects: []Subject{
			{Key: consts.QuerySubjectEvents, Label: "Events", Fields: clone(eventFields), Columns: slices.Clone(eventColumns)},
			{Key: consts.QuerySubjectRelations, Label: "Relations", Fields: clone(relationFields), Columns: slices.Clone(relationColumns)},
		},
		DefaultLimit:   consts.QueryDefaultLimit,
		MaxLimit:       consts.QueryMaxLimit,
		MaxConditions:  consts.QueryMaxConditions,
		MaxValueLength: consts.QueryMaxValueLen,
	}
}

func clone(fields []Field) []Field {
	out := slices.Clone(fields)
	for i := range out {
		out[i].Ops = slices.Clone(out[i].Ops)
		out[i].Values = slices.Clone(out[i].Values)
	}
	return out
}

// fieldsOf is the field table for a subject, or nil for an unknown one.
func fieldsOf(subject string) []Field {
	switch subject {
	case consts.QuerySubjectEvents:
		return eventFields
	case consts.QuerySubjectRelations:
		return relationFields
	}
	return nil
}

func findField(fields []Field, key string) (Field, bool) {
	i := slices.IndexFunc(fields, func(f Field) bool { return f.Key == key })
	if i < 0 {
		return Field{}, false
	}
	return fields[i], true
}
