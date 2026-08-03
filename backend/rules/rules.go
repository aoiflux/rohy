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
	"time"

	"rohy/backend/consts"
)

// Spec is the on-disk shape of a rule file. FormatVersion guards forward-compatibility;
// Sequence is the ordered list of event IDs to correlate. Fields beyond these are
// reserved for future matchers (provider, channel, user, time-window).
type Spec struct {
	FormatVersion int    `json:"format_version"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	RelationType  string `json:"relation_type"`
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

	// --- Algorithm-specific matchers (format version 2) ---
	//
	// These are flat, prefixed top-level fields rather than nested objects, and that is a
	// considered choice rather than a shortcut. Three things in this package address a rule
	// by a FLAT name: scanPositions, which locates a problem at "field" or "field[i]";
	// ValidationError.Field/Index, which is how the guided form finds the control to
	// highlight; and the descriptor the raw editor's completion list is generated from.
	// Nesting would mean changing all three at once and would buy no expressiveness.
	//
	// A field belonging to a different algorithm is IGNORED, exactly as any field this build
	// does not interpret is (RULES.md §3) — reported as an advisory, never as a rejection.

	// MatchFields names correlation fields (consts.CorrelationSlots) that every event in a
	// match must share. This is what turns a match from "these happened in this order on this
	// host" into "…and they concern the same logon session / account / process".
	MatchFields []string `json:"match_fields,omitempty"`
	// MatchScope partitions events before matching (consts.CorrelationScopes). Defaults to
	// the originating computer, so a chain is never assembled across unrelated hosts.
	MatchScope string `json:"match_scope,omitempty"`
	// WindowWithin bounds the time between consecutive matched steps, as a Go duration
	// ("90s", "5m", "2h"). WindowTotal optionally bounds first step to last.
	WindowWithin string `json:"window_within,omitempty"`
	WindowTotal  string `json:"window_total,omitempty"`
	// LineageCreateIDs are the event IDs that record a process being created (4688 by
	// default; Sysmon's is 1). LineageDepth adds transitive ancestor edges above the direct
	// parent link — 0, the default, emits direct edges only, because transitive links are
	// derivable by traversing them and emitting them multiplies edge count without adding
	// information.
	LineageCreateIDs []string `json:"lineage_create_ids,omitempty"`
	LineageDepth     int      `json:"lineage_depth,omitempty"`

	// Channels declares the Windows log channels this rule needs. It is metadata — no
	// algorithm reads it — and it exists so rohy can tell an analyst "this rule cannot fire
	// on this case, because the log it depends on was never ingested" instead of reporting
	// zero matches and leaving them to work out why.
	//
	// It is an optional field with a safe default, so per RULES.md §5 it does NOT require the
	// format version bump: it would have been legal in v1.
	Channels []string `json:"channels,omitempty"`
}

// LabelFor returns the custom label for the connection leaving sequence step i (the edge
// sequence[i] → sequence[i+1]), or "" when that connection is untagged.
func (s *Spec) LabelFor(i int) string {
	if i >= 0 && i < len(s.Labels) {
		return s.Labels[i]
	}
	return ""
}

// AlgorithmOrDefault resolves the algorithm a rule selects, defaulting to sequence
// correlation. Every reader goes through this rather than reading the field, so an omitted
// value means the same thing everywhere.
func (s *Spec) AlgorithmOrDefault() string {
	if a := trimmed(s.Algorithm); a != "" {
		return a
	}
	return consts.DefaultAlgorithm
}

// ScopeOrDefault resolves the correlation scope, defaulting to the originating computer.
func (s *Spec) ScopeOrDefault() string {
	if sc := trimmed(s.MatchScope); sc != "" {
		return sc
	}
	return consts.DefaultScope
}

// LineageCreateIDsOrDefault resolves the process-creation event IDs, defaulting to 4688 so
// the common case is a rule with a name and an algorithm and nothing else.
func (s *Spec) LineageCreateIDsOrDefault() []string {
	out := make([]string, 0, len(s.LineageCreateIDs))
	for _, id := range s.LineageCreateIDs {
		if id = trimmed(id); id != "" {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return []string{consts.LineageDefaultCreateID}
	}
	return out
}

// Window returns the between-steps and total time bounds. A zero duration means unbounded.
// Both are re-parsed rather than cached because a Spec is a decoded file, not a compiled
// object — validation has already established that they parse.
func (s *Spec) Window() (within, total time.Duration) {
	within, _ = parseDuration(s.WindowWithin)
	total, _ = parseDuration(s.WindowTotal)
	return within, total
}

// MatchSlots resolves MatchFields to correlation slot indices. ok is false when any name is
// not part of the vocabulary — which validation already refuses, so a false here means the
// caller is holding a spec that never passed the loader.
func (s *Spec) MatchSlots() (slots []int, ok bool) {
	slots = make([]int, 0, len(s.MatchFields))
	for _, name := range s.MatchFields {
		i, found := consts.CorrelationSlotIndex(trimmed(name))
		if !found {
			return nil, false
		}
		slots = append(slots, i)
	}
	return slots, true
}

// parseDuration reads one of the rule format's duration fields. An empty value is zero and no
// error: the field is optional, and "absent" is not "malformed".
func parseDuration(v string) (time.Duration, error) {
	v = trimmed(v)
	if v == "" {
		return 0, nil
	}
	return time.ParseDuration(v)
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
//
// The contract itself lives in Spec.problems (validate.go), which reports every violation
// with a stable code and a location. This wrapper is the loader's view of it: default the
// omitted version, then surface the first problem as an error. Having one implementation is
// the point — the editor validates a buffer with exactly the checks that decide, moments
// later, whether the file it writes will load.
func (s *Spec) validate() error {
	if s.FormatVersion == 0 {
		s.FormatVersion = consts.RuleFormatVersion // tolerate an omitted version as current
	}
	if problems := s.problems(); len(problems) > 0 {
		return errors.New(problems[0].Message)
	}
	return nil
}

// trimmed is strings.TrimSpace under a shorter name, used by the contract checks where the
// trimming is incidental to the condition being read.
func trimmed(s string) string { return strings.TrimSpace(s) }

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
	// The v2 matcher fields. Trimming here rather than at each reader is what makes
	// "  logon_id  " and "logon_id" the same rule.
	for i := range s.MatchFields {
		s.MatchFields[i] = strings.TrimSpace(s.MatchFields[i])
	}
	for i := range s.LineageCreateIDs {
		s.LineageCreateIDs[i] = strings.TrimSpace(s.LineageCreateIDs[i])
	}
	for i := range s.Channels {
		s.Channels[i] = strings.TrimSpace(s.Channels[i])
	}
	s.MatchScope = scopeOrDefault(s.MatchScope)
	s.WindowWithin = strings.TrimSpace(s.WindowWithin)
	s.WindowTotal = strings.TrimSpace(s.WindowTotal)
}

// scopeOrDefault maps an empty scope to the default, leaving an unrecognized one alone so
// validation can name it. This differs from relationTypeOrDefault deliberately: an unknown
// relation type is cosmetic (it drives edge colouring) and is silently corrected, whereas an
// unknown scope would change WHICH events can match each other, so it is refused rather than
// quietly replaced with something the author did not ask for.
func scopeOrDefault(scope string) string {
	if s := strings.TrimSpace(scope); s != "" {
		return s
	}
	return consts.DefaultScope
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
