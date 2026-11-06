package query

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// Spec is one query, as the page sends it. Conditions are ANDed. Every Value is a string, whatever
// the field's kind, so the binding never has to guess a JSON type.
type Spec struct {
	Subject string      `json:"subject"`
	Where   []Condition `json:"where"`
	// GroupBy, when set, counts the matches by this field instead of listing them.
	GroupBy string `json:"group_by"`
	// Limit bounds the rows or groups shown: 0 means consts.QueryDefaultLimit.
	Limit int `json:"limit"`
}

// Condition is one test on one field.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// RefusedError is a spec the console will not run. The input is at fault, not the case.
type RefusedError struct{ msg string }

func (e *RefusedError) Error() string { return e.msg }

func refuse(format string, args ...any) error {
	return &RefusedError{fmt.Sprintf(format, args...)}
}

// Result is what a query found.
type Result struct {
	Subject string `json:"subject"`
	GroupBy string `json:"group_by"`
	// Undated is the undated policy an events query applied (consts.QueryUndated*), said back so a
	// count is never read without it.
	Undated   string        `json:"undated,omitempty"`
	Events    []EventRow    `json:"events"`
	Relations []RelationRow `json:"relations"`
	Groups    []Group       `json:"groups"`
	// Total is how many events or relations matched, however many are shown.
	Total int `json:"total"`
	// DistinctGroups is how many groups there were, however many are shown.
	DistinctGroups int `json:"distinct_groups"`
	// Truncated says the rows or groups shown stop at the limit.
	Truncated bool `json:"truncated"`
	// Changed says the case was written while the query ran (reads take no case lock), so its
	// numbers may not agree with each other. Running it again gives a consistent answer.
	Changed bool `json:"changed"`
}

// EventRow is one event as the console lists it: the record's short fields, never its raw XML or
// parsed fields, which a list read does not load.
type EventRow struct {
	ID uint64 `json:"id"`
	// Timestamp is RFC 3339, or "" for an undated event: a zero time would read as a date.
	Timestamp        string `json:"timestamp"`
	EventID          string `json:"event_id"`
	Provider         string `json:"provider"`
	Channel          string `json:"channel"`
	Computer         string `json:"computer"`
	User             string `json:"user"`
	SourceType       string `json:"source_type"`
	SourceIdentifier string `json:"source_identifier"`
	Occurrences      int    `json:"occurrences"`
	// Redacted says a redaction removed the record, so its empty fields are absence, not data.
	Redacted bool `json:"redacted"`
}

// RelationRow is one relation with its provenance.
type RelationRow struct {
	ID           uint64   `json:"id"`
	From         uint64   `json:"from"`
	To           uint64   `json:"to"`
	RuleID       string   `json:"rule_id"`
	Algorithm    string   `json:"algorithm"`
	RelationType string   `json:"relation_type"`
	Label        string   `json:"label"`
	CreatedBy    string   `json:"created_by"`
	Confidence   float64  `json:"confidence"`
	GraphID      uint64   `json:"graph_id"`
	MatchID      string   `json:"match_id"`
	Step         int      `json:"step"`
	Basis        []string `json:"basis"`
	RelVersion   int      `json:"rel_v"`
	CreatedAt    string   `json:"created_at"`
}

// Group is one value of the counted field and how many matches hold it.
type Group struct {
	Value string `json:"value"`
	Count int    `json:"count"`
	// Redacted marks the events whose record a redaction removed: they have no value to count by.
	Redacted bool `json:"redacted"`
}

// plan is a checked spec, ready to read.
type plan struct {
	subject string
	events  graphene.EventFilter
	rels    graphene.RelationFilter
	undated string
	groupBy string
	limit   int
}

// Validate checks spec against the vocabulary without reading anything. A refusal is a
// *RefusedError.
func Validate(spec Spec) error {
	_, err := compile(spec)
	return err
}

func compile(spec Spec) (plan, error) {
	fields := fieldsOf(spec.Subject)
	if fields == nil {
		return plan{}, refuse(consts.MsgQuerySubject, spec.Subject, consts.QuerySubjectEvents, consts.QuerySubjectRelations)
	}
	if len(spec.Where) > consts.QueryMaxConditions {
		return plan{}, refuse(consts.MsgQueryTooMany, consts.QueryMaxConditions)
	}
	p := plan{subject: spec.Subject, undated: consts.QueryUndatedInclude, groupBy: spec.GroupBy, limit: spec.Limit}
	if p.limit == 0 {
		p.limit = consts.QueryDefaultLimit
	}
	if p.limit < 1 || p.limit > consts.QueryMaxLimit {
		return plan{}, refuse(consts.MsgQueryLimit, consts.QueryMaxLimit, spec.Limit)
	}
	if spec.GroupBy != "" {
		if f, ok := findField(fields, spec.GroupBy); !ok || !f.Groupable {
			return plan{}, refuse(consts.MsgQueryGroupBy, spec.Subject, spec.GroupBy)
		}
	}

	seen := map[string]bool{}
	for _, c := range spec.Where {
		f, ok := findField(fields, c.Field)
		if !ok {
			return plan{}, refuse(consts.MsgQueryField, spec.Subject, c.Field)
		}
		if !slices.Contains(f.Ops, c.Op) {
			return plan{}, refuse(consts.MsgQueryOp, c.Field, c.Op, strings.Join(f.Ops, ", "))
		}
		// A field once, except a time range's two ends: two equalities on one field are either
		// redundant or match nothing, and neither is what anyone meant.
		key := c.Field
		if f.Kind == consts.QueryKindTime {
			key += "/" + c.Op
		}
		if seen[key] {
			return plan{}, refuse(consts.MsgQueryRepeated, c.Field)
		}
		seen[key] = true
		if err := p.apply(f, c); err != nil {
			return plan{}, err
		}
	}
	if from, to := p.events.TimeFrom, p.events.TimeTo; from != nil && to != nil && to.Before(*from) {
		return plan{}, refuse(consts.MsgQueryRange)
	}
	return p, nil
}

// apply checks one condition's value against its field's kind and sets it on the filter.
func (p *plan) apply(f Field, c Condition) error {
	v := c.Value
	if v == "" {
		return refuse(consts.MsgQueryEmptyValue, c.Field)
	}
	if len(v) > consts.QueryMaxValueLen {
		return refuse(consts.MsgQueryLongValue, c.Field, consts.QueryMaxValueLen)
	}
	switch f.Kind {
	case consts.QueryKindEnum:
		if !slices.Contains(f.Values, v) {
			return refuse(consts.MsgQueryEnum, c.Field, strings.Join(f.Values, ", "), v)
		}
	case consts.QueryKindTime:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return refuse(consts.MsgQueryTime, c.Field, v)
		}
		t = t.UTC()
		if c.Op == consts.QueryOpGte {
			p.events.TimeFrom = &t
		} else {
			p.events.TimeTo = &t
		}
		return nil
	case consts.QueryKindNumber:
		n, err := strconv.ParseUint(v, 10, 63)
		if err != nil {
			return refuse(consts.MsgQueryNumber, c.Field, v)
		}
		return p.applyNumber(c.Field, n)
	}

	e, r := &p.events, &p.rels
	switch c.Field {
	case consts.PropEventID:
		e.EventID = v
	case consts.PropProvider:
		e.Provider = v
	case consts.PropChannel:
		e.Channel = v
	case consts.PropComputer:
		e.Computer = v
	case consts.PropUser:
		e.User = v
	case consts.PropSourceType:
		e.SourceType = v
	case consts.PropSourceIdentifier:
		e.SourceIdentifier = v
	case fieldText:
		e.Search = v
	case fieldRelations:
		e.RelationState = v
	case fieldUndated:
		p.undated = v
	case consts.PropRuleID:
		r.RuleID = v
	case consts.PropAlgorithm:
		r.Algorithm = v
	case consts.PropRelationType:
		r.RelationType = v
	case consts.PropCreatedBy:
		r.CreatedBy = v
	case consts.PropMatchID:
		r.MatchID = v
	}
	return nil
}

func (p *plan) applyNumber(field string, n uint64) error {
	switch field {
	case fieldOccurrences:
		if n > uint64(1<<31-1) {
			return refuse(consts.MsgQueryNumber, field, strconv.FormatUint(n, 10))
		}
		p.events.MinDuplicateCount = int(n)
	case consts.PropGraphID:
		p.rels.GraphID = &n
	case fieldEvent:
		p.rels.EventID = &n
	}
	return nil
}

// undatedFilter maps the console's undated word to EventFilter's, whose exclude is "".
func undatedFilter(word string) string {
	switch word {
	case consts.QueryUndatedExclude:
		return consts.UndatedExclude
	case consts.QueryUndatedOnly:
		return consts.UndatedOnly
	}
	return consts.UndatedInclude
}

// Run checks spec and answers it from r. A refusal is a *RefusedError; anything else is the
// store's.
func Run(r Reader, spec Spec) (Result, error) {
	p, err := compile(spec)
	if err != nil {
		return Result{}, err
	}
	before := r.Version()
	res := Result{Subject: p.subject, GroupBy: p.groupBy, Events: []EventRow{}, Relations: []RelationRow{}, Groups: []Group{}}
	if p.subject == consts.QuerySubjectEvents {
		err = p.runEvents(r, &res)
	} else {
		err = p.runRelations(r, &res)
	}
	if err != nil {
		return Result{}, err
	}
	if r.Version() != before {
		res.Changed = true
	}
	return res, nil
}

func (p plan) runEvents(r Reader, res *Result) error {
	f := p.events
	f.Undated = undatedFilter(p.undated)
	res.Undated = p.undated
	if p.groupBy != "" {
		g, err := r.CountEventsBy(f, p.groupBy)
		if err != nil {
			return err
		}
		var all []Group
		for v, n := range g.Counts {
			all = append(all, Group{Value: v, Count: n})
			res.Total += n
		}
		if g.Redacted > 0 {
			all = append(all, Group{Count: g.Redacted, Redacted: true})
			res.Total += g.Redacted
		}
		p.keepGroups(res, all)
		return nil
	}
	total, err := r.CountEvents(f)
	if err != nil {
		return err
	}
	f.Limit = p.limit
	evs, err := r.QueryEvents(f)
	if err != nil {
		return err
	}
	for _, e := range evs {
		res.Events = append(res.Events, eventRow(e))
	}
	res.Total = total
	res.Truncated = total > len(res.Events)
	return nil
}

func (p plan) runRelations(r Reader, res *Result) error {
	counts := map[string]int{}
	st, err := r.ScanRelations(p.rels, func(rel *graphene.Relation) error {
		res.Total++
		if p.groupBy != "" {
			counts[relationField(rel, p.groupBy)]++
		} else if len(res.Relations) < p.limit {
			res.Relations = append(res.Relations, relationRow(rel))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if st.Vanished > 0 {
		res.Changed = true
	}
	if p.groupBy != "" {
		all := make([]Group, 0, len(counts))
		for v, n := range counts {
			all = append(all, Group{Value: v, Count: n})
		}
		p.keepGroups(res, all)
		return nil
	}
	res.Truncated = res.Total > len(res.Relations)
	return nil
}

// keepGroups orders the groups — the most common first, ties by value, the redacted bucket last
// among equals — and keeps the first limit of them.
func (p plan) keepGroups(res *Result, all []Group) {
	slices.SortFunc(all, func(a, b Group) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), boolOrder(a.Redacted, b.Redacted), strings.Compare(a.Value, b.Value))
	})
	res.DistinctGroups = len(all)
	res.Truncated = len(all) > p.limit
	res.Groups = all[:min(len(all), p.limit)]
}

func boolOrder(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func relationField(r *graphene.Relation, field string) string {
	switch field {
	case consts.PropRuleID:
		return r.RuleID
	case consts.PropAlgorithm:
		return r.Algorithm
	case consts.PropRelationType:
		return r.RelationType
	case consts.PropCreatedBy:
		return r.CreatedBy
	default: // consts.PropGraphID; compile has checked the field
		return strconv.FormatUint(r.GraphID, 10)
	}
}

func eventRow(e *graphene.Event) EventRow {
	row := EventRow{
		ID: e.ID, EventID: e.EventID, Provider: e.Provider, Channel: e.Channel, Computer: e.Computer,
		User: e.User, SourceType: e.SourceType, SourceIdentifier: e.SourceIdentifier,
		Occurrences: e.DeduplicationCount, Redacted: e.Redacted,
	}
	if !e.Timestamp.IsZero() {
		row.Timestamp = e.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return row
}

func relationRow(r *graphene.Relation) RelationRow {
	row := RelationRow{
		ID: r.ID, From: r.From, To: r.To, RuleID: r.RuleID, Algorithm: r.Algorithm,
		RelationType: r.RelationType, Label: r.Label, CreatedBy: r.CreatedBy, Confidence: r.ConfidenceScore,
		GraphID: r.GraphID, MatchID: r.MatchID, Step: r.StepIndex, Basis: slices.Clone(r.Basis),
		RelVersion: r.RelVersion,
	}
	if row.Basis == nil {
		row.Basis = []string{}
	}
	if !r.CreatedAt.IsZero() {
		row.CreatedAt = r.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return row
}
