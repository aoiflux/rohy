package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"rohy/backend/consts"
)

// This file turns rule validation from "the first problem, as a sentence" into "every
// problem, located".
//
// The loader only ever needed the sentence: a rule file either loads or is reported as
// broken. An editor needs more. It has to underline a token in the raw text, highlight a
// control in the guided form, and let the author fix several things in one pass — none of
// which is possible from a string. So validation grows a typed representation with a stable
// code, the field and element it concerns, and a line and column in the source.
//
// What has NOT changed is who decides. Spec.validate() still gates every load, still returns
// the first problem, and still produces the identical message text; it simply reads the
// first entry of the list this file builds. There is exactly one validator, and the editor
// asks it rather than approximating it.

// ValidationError is one problem with a rule file, located precisely enough for either
// editor to point at it.
//
// Field and Index address the guided form: Field names the rule field, and Index is the
// 0-based element within it, or -1 when the problem concerns the field as a whole. Line and
// Col address the raw text and are 1-based, with Col counted in runes so a non-ASCII
// description does not shift the caret. Both are 0 when the problem could not be located —
// a field that is absent from the file has no position in it.
type ValidationError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Index   int    `json:"index"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

// ValidationReport is the answer to "would this text load?".
//
// Valid is true only when Errors is empty. Warnings never affect Valid — they are the
// things that are legal but worth saying, such as a field this build does not interpret.
// Normalized is what the loader would actually store once strings are trimmed and defaults
// applied, so the editor can show the author the real result before they commit to it; it
// is nil when the text does not load.
type ValidationReport struct {
	Valid      bool              `json:"valid"`
	Errors     []ValidationError `json:"errors"`
	Warnings   []ValidationError `json:"warnings"`
	Normalized *Spec             `json:"normalized,omitempty"`
	// UnknownFields lists top-level keys this build does not interpret. They are preserved
	// verbatim on save (RULES.md §3), and the guided editor shows them read-only rather than
	// hiding fields it would otherwise appear to have silently dropped.
	UnknownFields []string `json:"unknown_fields,omitempty"`
}

// ValidateSource runs the real loader over candidate bytes and reports every problem it
// finds, located in the text.
//
// It exists so that the editor's "this will load" claim is made by the code that does the
// loading. A separate editor-only validator would drift from Spec.validate() sooner or
// later, and the drift would surface as the worst possible failure: a rule that saves
// cleanly and is then missing from the library on the next scan.
func ValidateSource(data []byte) ValidationReport {
	// The size cap is checked here as well as at import, because the editor can produce a
	// buffer that was never a file — refusing it before it is written is the same guarantee
	// readRuleFile gives for one that was.
	if int64(len(data)) > consts.RuleMaxFileBytes {
		return ValidationReport{Errors: []ValidationError{{
			Code:    consts.RuleErrFileTooLarge,
			Index:   -1,
			Message: fmt.Sprintf(consts.MsgRuleFileTooLarge, len(data), int64(consts.RuleMaxFileBytes)),
		}}}
	}

	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return ValidationReport{Errors: []ValidationError{syntaxError(data, err)}}
	}

	positions, keys := scanPositions(data)
	report := ValidationReport{UnknownFields: unknownFields(keys)}

	problems := spec.problems()
	for i := range problems {
		locate(&problems[i], data, positions)
	}
	report.Errors = problems

	for _, w := range spec.advisories(report.UnknownFields) {
		locate(&w, data, positions)
		report.Warnings = append(report.Warnings, w)
	}

	if len(report.Errors) > 0 {
		return report
	}

	// Valid: hand back what the loader would actually keep. The defaulting mirrors
	// validate()/normalize() rather than repeating their rules, so the preview cannot claim
	// a normalization the loader does not perform.
	normalized := spec
	if normalized.FormatVersion == 0 {
		normalized.FormatVersion = normalized.RequiredFormatVersion()
	}
	normalized.normalize()
	report.Valid = true
	report.Normalized = &normalized
	return report
}

// problems returns every way this spec violates the rule contract, in the order the
// contract is stated. It does not mutate the spec: validate() owns the defaulting, and a
// pure check is what lets the editor validate a buffer without changing it.
func (s *Spec) problems() []ValidationError {
	var out []ValidationError

	version := s.FormatVersion
	if version == 0 {
		version = s.RequiredFormatVersion() // an omitted version means "what this rule needs" (RULES.md §5)
	}
	if version > consts.RuleFormatVersion {
		// Nothing else is reported for a file from the future. The remaining checks would be
		// this build's rules applied to a format it has just said it cannot read, so every
		// further complaint would be unreliable and some would be actively wrong.
		return []ValidationError{{
			Code:    consts.RuleErrUnsupportedFormat,
			Field:   "format_version",
			Index:   -1,
			Message: fmt.Sprintf(consts.MsgRuleUnsupportedFormat, version, consts.RuleFormatVersion),
		}}
	}

	// The algorithm is resolved BEFORE anything else is checked, and an unknown one stops the
	// pass, for the same reason a from-the-future format version does: almost every remaining
	// check depends on which algorithm this is. Whether a sequence is required, whether
	// match_fields must be present, whether a window is mandatory — all of it changes. Running
	// those checks against a guessed algorithm would put confident, wrong complaints in front
	// of the author alongside the real one.
	algoName := s.AlgorithmOrDefault()
	algo, known := consts.AlgorithmByName(algoName)
	if !known {
		return []ValidationError{{
			Code: consts.RuleErrUnknownAlgorithm, Field: consts.FieldAlgorithm, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleUnknownAlgorithm, s.Algorithm),
		}}
	}
	// A rule may not use a matcher its declared format version does not contain. This is the
	// author-facing half of the forward-compatibility guard: the file must announce the
	// version it needs, so an older build refuses it rather than matching it wrongly.
	if version < algo.MinFormatVersion {
		out = append(out, ValidationError{
			Code: consts.RuleErrAlgorithmNeedsFormat, Field: consts.FieldAlgorithm, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleAlgorithmNeedsFormat, algoName, algo.MinFormatVersion, version),
		})
	}

	if trimmed(s.Name) == "" {
		out = append(out, ValidationError{
			Code: consts.RuleErrNameRequired, Field: "name", Index: -1,
			Message: consts.MsgRuleNameRequired,
		})
	}

	out = append(out, s.sequenceProblems(algo)...)
	out = append(out, s.scopeProblems()...)
	out = append(out, s.matchFieldProblems(algo)...)
	out = append(out, s.windowProblems(algo)...)
	out = append(out, s.lineageProblems(algo)...)
	out = append(out, s.channelProblems()...)
	return out
}

// sequenceProblems checks the event-ID sequence, for the algorithms that have one.
func (s *Spec) sequenceProblems(algo consts.AlgorithmDescriptor) []ValidationError {
	var out []ValidationError
	if !algo.RequiresSequence {
		// Lineage reconstructs ancestry from creation records; it has no sequence to check.
		// A sequence that is present anyway is preserved and reported as an advisory, never
		// rejected — the format ignores what an algorithm does not read.
		return nil
	}
	if len(s.Sequence) < consts.RuleMinSequence {
		out = append(out, ValidationError{
			Code: consts.RuleErrSequenceShort, Field: "sequence", Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleShortSequence, consts.RuleMinSequence),
		})
	}
	if len(s.Sequence) > consts.RuleMaxSequence {
		out = append(out, ValidationError{
			Code: consts.RuleErrSequenceLong, Field: "sequence", Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleLongSequence, consts.RuleMaxSequence),
		})
	}
	// Every blank id is reported, not just the first: fixing them one round-trip at a time
	// is exactly the tedium an editor is supposed to remove.
	for i, id := range s.Sequence {
		if trimmed(id) == "" {
			out = append(out, ValidationError{
				Code: consts.RuleErrSequenceEmptyID, Field: "sequence", Index: i,
				Message: fmt.Sprintf(consts.MsgRuleEmptyEventID, i),
			})
		}
	}
	// Only meaningful once the sequence is long enough to have connections at all. Without
	// this guard an absent sequence reports "more connection labels (0) than connections
	// (-1)" on top of the real problem — the loader never showed it because it stops at the
	// first error, but an editor that lists every problem would put a nonsense one in front
	// of the author.
	if len(s.Sequence) >= consts.RuleMinSequence && len(s.Labels) > len(s.Sequence)-1 {
		out = append(out, ValidationError{
			Code: consts.RuleErrLabelsTooMany, Field: "labels", Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleTooManyLabels, len(s.Labels), len(s.Sequence)-1),
		})
	}
	return out
}

// scopeProblems refuses an unrecognized correlation scope.
//
// Unlike an unknown relation_type — which is cosmetic and is silently corrected — an unknown
// scope would change which events are allowed to match each other. Substituting a default for
// it would produce a graph the author did not ask for and has no way to notice.
func (s *Spec) scopeProblems() []ValidationError {
	scope := trimmed(s.MatchScope)
	if scope == "" {
		return nil
	}
	for _, valid := range consts.CorrelationScopes {
		if scope == valid {
			return nil
		}
	}
	return []ValidationError{{
		Code: consts.RuleErrUnknownScope, Field: consts.FieldMatchScope, Index: -1,
		Message: fmt.Sprintf(consts.MsgRuleUnknownScope, s.MatchScope, strings.Join(consts.CorrelationScopes, ", ")),
	}}
}

// matchFieldProblems checks the correlation fields a match must share.
func (s *Spec) matchFieldProblems(algo consts.AlgorithmDescriptor) []ValidationError {
	var out []ValidationError
	reads := algorithmReads(algo, consts.FieldMatchFields)

	// Field correlation without fields to correlate on is just sequence correlation wearing a
	// different name, so it is refused rather than quietly downgraded.
	if algo.Name == consts.AlgoField && len(s.MatchFields) == 0 {
		out = append(out, ValidationError{
			Code: consts.RuleErrMatchFieldsRequired, Field: consts.FieldMatchFields, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleMatchFieldsRequired, algo.Name),
		})
	}
	if !reads {
		return out
	}

	seen := map[string]bool{}
	for i, name := range s.MatchFields {
		name = trimmed(name)
		if _, ok := consts.CorrelationSlotIndex(name); !ok {
			out = append(out, ValidationError{
				Code: consts.RuleErrUnknownMatchField, Field: consts.FieldMatchFields, Index: i,
				Message: fmt.Sprintf(consts.MsgRuleUnknownMatchField, s.MatchFields[i],
					strings.Join(consts.CorrelationSlots, ", ")),
			})
			continue
		}
		// A repeated field narrows nothing and is almost always a paste error.
		if seen[name] {
			out = append(out, ValidationError{
				Code: consts.RuleErrDuplicateMatchField, Field: consts.FieldMatchFields, Index: i,
				Message: fmt.Sprintf(consts.MsgRuleDuplicateMatchField, name),
			})
		}
		seen[name] = true
	}
	return out
}

// windowProblems checks the temporal bounds.
func (s *Spec) windowProblems(algo consts.AlgorithmDescriptor) []ValidationError {
	var out []ValidationError
	if !algorithmReads(algo, consts.FieldWindowWithin) {
		return nil
	}

	within, withinErr := parseDuration(s.WindowWithin)
	total, totalErr := parseDuration(s.WindowTotal)

	// An unbounded temporal rule is a slower spelling of a sequence rule, so the window is
	// required rather than defaulted to infinity.
	if trimmed(s.WindowWithin) == "" {
		out = append(out, ValidationError{
			Code: consts.RuleErrWindowRequired, Field: consts.FieldWindowWithin, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleWindowRequired, algo.Name),
		})
	}
	out = append(out, durationProblems(consts.FieldWindowWithin, s.WindowWithin, within, withinErr)...)
	out = append(out, durationProblems(consts.FieldWindowTotal, s.WindowTotal, total, totalErr)...)

	// A total shorter than the per-step bound can never be satisfied, so the rule would
	// silently never fire. Saying so is better than letting it look like "no matches".
	if withinErr == nil && totalErr == nil && total > 0 && within > 0 && total < within {
		out = append(out, ValidationError{
			Code: consts.RuleErrWindowTotalTooSmall, Field: consts.FieldWindowTotal, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleWindowTotalTooSmall, total, within),
		})
	}
	return out
}

// durationProblems reports one duration field's syntax and bounds.
func durationProblems(field, raw string, d time.Duration, err error) []ValidationError {
	if trimmed(raw) == "" {
		return nil // absent is not malformed; whether it is REQUIRED is decided by the caller
	}
	if err != nil {
		return []ValidationError{{
			Code: consts.RuleErrBadDuration, Field: field, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleBadDuration, field, err),
		}}
	}
	if d <= 0 {
		return []ValidationError{{
			Code: consts.RuleErrBadDuration, Field: field, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleWindowNotPositive, field),
		}}
	}
	// A window measured in weeks is almost always a units slip — "30" read as nanoseconds, or
	// "30d" where "30m" was meant — and it would make the rule pair events that have nothing
	// to do with each other.
	if d > consts.TemporalMaxWindow {
		return []ValidationError{{
			Code: consts.RuleErrWindowTooLarge, Field: field, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleWindowTooLarge, field, d, consts.TemporalMaxWindow),
		}}
	}
	return nil
}

// lineageProblems checks the process-ancestry settings.
func (s *Spec) lineageProblems(algo consts.AlgorithmDescriptor) []ValidationError {
	var out []ValidationError
	if !algorithmReads(algo, consts.FieldLineageCreateIDs) {
		return nil
	}
	// An empty list is fine — it defaults to 4688 — but a list containing a blank entry is a
	// half-finished edit, and reporting each one lets the author fix them in a single pass.
	for i, id := range s.LineageCreateIDs {
		if trimmed(id) == "" {
			out = append(out, ValidationError{
				Code: consts.RuleErrLineageIDsEmpty, Field: consts.FieldLineageCreateIDs, Index: i,
				Message: fmt.Sprintf(consts.MsgRuleLineageIDsEmpty, i),
			})
		}
	}
	if s.LineageDepth < 0 || s.LineageDepth > consts.LineageMaxDepth {
		out = append(out, ValidationError{
			Code: consts.RuleErrLineageDepthRange, Field: consts.FieldLineageDepth, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleLineageDepthRange, consts.LineageMaxDepth),
		})
	}
	return out
}

// channelProblems checks the declared channel list. Channels are metadata for every
// algorithm, so this does not depend on which one is selected.
func (s *Spec) channelProblems() []ValidationError {
	var out []ValidationError
	for i, ch := range s.Channels {
		if trimmed(ch) == "" {
			out = append(out, ValidationError{
				Code: consts.RuleErrChannelEmpty, Field: consts.FieldChannels, Index: i,
				Message: fmt.Sprintf(consts.MsgRuleChannelEmpty, i),
			})
		}
	}
	return out
}

// algorithmReads reports whether an algorithm reads a given rule field.
func algorithmReads(algo consts.AlgorithmDescriptor, field string) bool {
	for _, f := range algo.Fields {
		if f == field {
			return true
		}
	}
	return false
}

// advisories returns the problems that do not stop a rule loading but are worth saying to
// whoever is writing it.
func (s *Spec) advisories(unknown []string) []ValidationError {
	var out []ValidationError
	if trimmed(s.Description) == "" {
		out = append(out, ValidationError{
			Code: consts.RuleWarnNoDescription, Field: "description", Index: -1,
			Message: consts.MsgRuleNoDescription,
		})
	}
	// A rule that does not say which logs it needs cannot be checked against a case, so the
	// integrity report has to stay silent about it — and silence there would read as "fine".
	if len(s.Channels) == 0 {
		out = append(out, ValidationError{
			Code: consts.RuleWarnNoChannels, Field: consts.FieldChannels, Index: -1,
			Message: consts.MsgRuleNoChannels,
		})
	}
	out = append(out, s.inertFieldAdvisories()...)
	for _, key := range unknown {
		out = append(out, ValidationError{
			Code: consts.RuleWarnUnknownField, Field: key, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleUnknownField, key),
		})
	}
	return out
}

// inertFieldAdvisories reports fields the selected algorithm does not read.
//
// These are warnings and never errors, and that is the same rule the format already applies to
// a field it does not recognize at all (RULES.md §3): it is preserved on save and ignored on
// load. Rejecting a `window_within` on a sequence rule would make the editor stricter than the
// loader, which is the one direction it must never be — a save the editor refuses but the
// loader would have accepted teaches the author a rule that is not true.
//
// Saying nothing would be worse than either, though. An author who sets match_fields and
// leaves the algorithm at its default gets a rule that looks precise and correlates on nothing
// but ordering, and no part of the result would tell them.
func (s *Spec) inertFieldAdvisories() []ValidationError {
	algo, ok := consts.AlgorithmByName(s.AlgorithmOrDefault())
	if !ok {
		return nil // an unknown algorithm is already a hard error; do not pile on
	}

	var out []ValidationError
	warn := func(field string) {
		out = append(out, ValidationError{
			Code: consts.RuleWarnFieldNotForAlgorithm, Field: field, Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleFieldNotForAlgorithm, field, algo.Name),
		})
	}
	if len(s.MatchFields) > 0 && !algorithmReads(algo, consts.FieldMatchFields) {
		warn(consts.FieldMatchFields)
	}
	if trimmed(s.WindowWithin) != "" && !algorithmReads(algo, consts.FieldWindowWithin) {
		warn(consts.FieldWindowWithin)
	}
	if trimmed(s.WindowTotal) != "" && !algorithmReads(algo, consts.FieldWindowTotal) {
		warn(consts.FieldWindowTotal)
	}
	if len(s.LineageCreateIDs) > 0 && !algorithmReads(algo, consts.FieldLineageCreateIDs) {
		warn(consts.FieldLineageCreateIDs)
	}
	if s.LineageDepth != 0 && !algorithmReads(algo, consts.FieldLineageDepth) {
		warn(consts.FieldLineageDepth)
	}
	// A sequence on an algorithm that does not match one is the same class of problem, but it
	// gets its own code because it is the likeliest of these to be a genuine misunderstanding
	// about what the algorithm does.
	if len(s.Sequence) > 0 && !algo.RequiresSequence {
		out = append(out, ValidationError{
			Code: consts.RuleWarnSequenceIgnored, Field: "sequence", Index: -1,
			Message: fmt.Sprintf(consts.MsgRuleSequenceIgnored, algo.Name),
		})
	}
	return out
}

// unknownFields returns the top-level keys the format does not define, in file order.
func unknownFields(keys []string) []string {
	known := knownFields()
	var out []string
	for _, k := range keys {
		if !known[k] {
			out = append(out, k)
		}
	}
	return out
}

// locate fills in a problem's Line and Col from the positions scanned out of the source.
// A problem about a field the file does not contain stays unlocated (0,0) rather than being
// pinned to an arbitrary line — pointing at the wrong token is worse than pointing at none.
func locate(e *ValidationError, data []byte, positions map[string]int) {
	if e.Field == "" {
		return
	}
	key := e.Field
	if e.Index >= 0 {
		key = fmt.Sprintf("%s[%d]", e.Field, e.Index)
	}
	off, ok := positions[key]
	if !ok {
		return
	}
	e.Line, e.Col = offsetToLineCol(data, off)
}

// syntaxError converts a json decode failure into a located problem. The message keeps the
// MsgRuleParseFailed wrapper the loader already uses, so a file that fails here reads the
// same in the editor as it does in the rules view's load-errors panel.
func syntaxError(data []byte, err error) ValidationError {
	out := ValidationError{
		Code:    consts.RuleErrSyntax,
		Index:   -1,
		Message: fmt.Sprintf(consts.MsgRuleParseFailed, err),
	}
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		out.Line, out.Col = offsetToLineCol(data, int(syn.Offset))
	case errors.As(err, &typ):
		// A type mismatch names the field it happened on, which is enough for the guided
		// editor to highlight the right control even though the value is unusable.
		out.Field = typ.Field
		out.Line, out.Col = offsetToLineCol(data, int(typ.Offset))
	case errors.Is(err, io.ErrUnexpectedEOF):
		// Truncated input: the problem is wherever the text ran out.
		out.Line, out.Col = offsetToLineCol(data, len(data))
	}
	return out
}

// offsetToLineCol converts a byte offset into a 1-based line and column. Columns count
// runes rather than bytes, so a caret drawn from this lands where the author sees the
// character and not several columns to its right.
func offsetToLineCol(data []byte, off int) (line, col int) {
	if off < 0 {
		off = 0
	}
	if off > len(data) {
		off = len(data)
	}
	line, col = 1, 1
	for i := 0; i < off; {
		r, size := utf8.DecodeRune(data[i:])
		if size == 0 {
			break
		}
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i += size
	}
	return line, col
}

// scanPositions records where each addressable part of a rule file begins, and the
// top-level keys in file order.
//
// Positions are keyed by field name for a whole value ("sequence") and by name and index
// for an array element ("sequence[2]"), which is the same addressing ValidationError uses,
// so locating a problem is a map lookup rather than a second parse.
//
// It re-reads the source with a token decoder instead of reusing the Unmarshal above
// because Unmarshal discards positions entirely — by the time there is a Spec, every byte
// offset is gone. A malformed tail simply ends the scan: whatever was located before it is
// still correct, and this runs only after Unmarshal has already succeeded.
func scanPositions(data []byte) (map[string]int, []string) {
	positions := map[string]int{}
	var keys []string

	dec := json.NewDecoder(bytes.NewReader(data))
	prevEnd := 0
	// advance returns the offset of the next token's first byte. InputOffset gives the END
	// of the last token read, so the start of the next one is found by stepping over the
	// whitespace and structural punctuation between them.
	advance := func() int { return skipFiller(data, prevEnd) }
	read := func() (json.Token, int, bool) {
		start := advance()
		tok, err := dec.Token()
		if err != nil {
			return nil, 0, false
		}
		prevEnd = int(dec.InputOffset())
		return tok, start, true
	}

	tok, _, ok := read()
	if !ok {
		return positions, keys
	}
	if d, isDelim := tok.(json.Delim); !isDelim || d != '{' {
		return positions, keys // not an object; nothing addressable
	}

	for dec.More() {
		keyTok, keyStart, ok := read()
		if !ok {
			return positions, keys
		}
		name, isString := keyTok.(string)
		if !isString {
			return positions, keys
		}
		keys = append(keys, name)
		positions["#"+name] = keyStart // the key token, for errors about the field's presence

		valTok, valStart, ok := read()
		if !ok {
			return positions, keys
		}
		positions[name] = valStart

		delim, isDelim := valTok.(json.Delim)
		if !isDelim {
			continue
		}
		if delim == '[' {
			for i := 0; dec.More(); i++ {
				elemTok, elemStart, ok := read()
				if !ok {
					return positions, keys
				}
				positions[fmt.Sprintf("%s[%d]", name, i)] = elemStart
				if d, nested := elemTok.(json.Delim); nested {
					if !skipRest(dec, d) {
						return positions, keys
					}
					prevEnd = int(dec.InputOffset())
				}
			}
			if _, _, ok := read(); !ok { // the closing ']'
				return positions, keys
			}
			continue
		}
		// An object value: step over it wholesale, since nothing inside is addressable.
		if !skipRest(dec, delim) {
			return positions, keys
		}
		prevEnd = int(dec.InputOffset())
	}
	return positions, keys
}

// skipRest consumes tokens until the container just opened by open is closed.
func skipRest(dec *json.Decoder, open json.Delim) bool {
	if open != '[' && open != '{' {
		return true
	}
	for depth := 1; depth > 0; {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '[', '{':
				depth++
			case ']', '}':
				depth--
			}
		}
	}
	return true
}

// skipFiller advances past the whitespace and structural punctuation that separates two
// tokens, so a recorded position is the first byte of the value itself rather than of the
// comma or colon in front of it.
func skipFiller(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n', ',', ':':
			i++
		default:
			return i
		}
	}
	return i
}
