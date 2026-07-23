// Package rules is rohy's correlation-rule engine (P2). A rule is a single portable JSON
// file — "1 file = 1 rule, 1 rule = 1 graph" — whose body is an ordered sequence of event
// IDs; the auto-graphing algorithm (P3) emits edges between consecutive matched events.
// A connection between two steps may be untagged or carry an optional custom label. The
// format reserves room for additional field matchers later. This package parses +
// validates rule files and owns the rule registry; it depends only on consts and never
// talks to the graph or event stores directly.
package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"rohy/backend/consts"
)

// Spec is the on-disk shape of a rule file. FormatVersion guards forward-compatibility;
// Sequence is the ordered list of event IDs to correlate. Fields beyond these are
// reserved for future matchers (provider, channel, user, time-window).
type Spec struct {
	FormatVersion int      `json:"format_version"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	RelationType  string   `json:"relation_type"`
	// Algorithm selects how the sequence is correlated into edges (consts.Algo*). It is
	// optional and defaults to sequence correlation; it is the extension point for future
	// field-correlation / temporal-window algorithms.
	Algorithm string   `json:"algorithm,omitempty"`
	Sequence  []string `json:"sequence"`
	// Labels are OPTIONAL custom labels for the connections between consecutive sequence
	// steps: Labels[i] labels the edge sequence[i] → sequence[i+1] (e.g. "commits"). An
	// empty entry (or a missing tail) means an UNTAGGED connection, so a rule may label
	// only some connections. At most one label per connection (≤ len(Sequence)-1).
	Labels []string `json:"labels,omitempty"`
}

// LabelFor returns the custom label for the connection leaving sequence step i (the edge
// sequence[i] → sequence[i+1]), or "" when that connection is untagged.
func (s *Spec) LabelFor(i int) string {
	if i >= 0 && i < len(s.Labels) {
		return s.Labels[i]
	}
	return ""
}

// Rule is a Spec plus the runtime metadata the registry tracks: a stable id derived from
// the name, the source (builtin vs user), whether it is enabled, and the file it came
// from (empty for builtin rules).
type Rule struct {
	Spec
	ID      string `json:"id"`
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
	Path    string `json:"path,omitempty"`
	// File is the name of the file this rule was defined in — the embedded file for a
	// builtin, the on-disk file name for a user rule. It is what lets the inspector (P19)
	// read the rule back exactly as authored.
	File string `json:"file,omitempty"`
}

// LoadError describes a rule file that failed to load, so the UI can surface precise,
// per-file problems without aborting the rest of the load.
type LoadError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Parse decodes and validates one rule file's bytes into a normalized Spec.
func Parse(data []byte) (*Spec, error) {
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf(consts.MsgRuleParseFailed, err)
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	s.normalize()
	return &s, nil
}

// validate enforces the rule contract, returning the first actionable problem found.
func (s *Spec) validate() error {
	if s.FormatVersion == 0 {
		s.FormatVersion = consts.RuleFormatVersion // tolerate an omitted version as current
	}
	if s.FormatVersion > consts.RuleFormatVersion {
		return fmt.Errorf(consts.MsgRuleUnsupportedFormat, s.FormatVersion, consts.RuleFormatVersion)
	}
	if strings.TrimSpace(s.Name) == "" {
		return errors.New(consts.MsgRuleNameRequired)
	}
	if len(s.Sequence) < consts.RuleMinSequence {
		return fmt.Errorf(consts.MsgRuleShortSequence, consts.RuleMinSequence)
	}
	if len(s.Sequence) > consts.RuleMaxSequence {
		return fmt.Errorf(consts.MsgRuleLongSequence, consts.RuleMaxSequence)
	}
	for i, id := range s.Sequence {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf(consts.MsgRuleEmptyEventID, i)
		}
	}
	if len(s.Labels) > len(s.Sequence)-1 {
		return fmt.Errorf(consts.MsgRuleTooManyLabels, len(s.Labels), len(s.Sequence)-1)
	}
	if a := strings.TrimSpace(s.Algorithm); a != "" && a != consts.AlgoSequence {
		return fmt.Errorf(consts.MsgRuleUnknownAlgorithm, s.Algorithm)
	}
	return nil
}

// normalize trims user-entered strings and defaults the relation type so a rule always
// emits a valid, const-driven edge type.
func (s *Spec) normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Description = strings.TrimSpace(s.Description)
	s.RelationType = relationTypeOrDefault(s.RelationType)
	if s.Algorithm = strings.TrimSpace(s.Algorithm); s.Algorithm == "" {
		s.Algorithm = consts.DefaultAlgorithm
	}
	for i := range s.Sequence {
		s.Sequence[i] = strings.TrimSpace(s.Sequence[i])
	}
	for i := range s.Labels {
		s.Labels[i] = strings.TrimSpace(s.Labels[i])
	}
}

// relationTypeOrDefault maps an empty/unknown rule relation type to the correlation type
// (auto-graphing produces correlations by default).
func relationTypeOrDefault(t string) string {
	switch t {
	case consts.RelationTemporal, consts.RelationCorrelation, consts.RelationDefault:
		return t
	default:
		return consts.RelationCorrelation
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug derives a stable rule id from a name: lowercased, non-alphanumeric runs collapsed
// to single hyphens, trimmed. Two rules with the same name collide by design (the
// registry reports that as a duplicate).
func slug(name string) string {
	s := nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	return strings.Trim(s, "-")
}

// toRule wraps a validated spec with registry metadata.
func toRule(s *Spec, source, path, file string, enabled bool) *Rule {
	return &Rule{Spec: *s, ID: slug(s.Name), Source: source, Enabled: enabled, Path: path, File: file}
}
