package rules

import (
	"rohy/backend/consts"
)

// This file is the rule format described as DATA rather than as code.
//
// The format is otherwise stated in four places that can drift apart: the Spec struct tags,
// validate(), the consts block, and RULES.md. That was tolerable while rules were only ever
// hand-written in a text editor. It is not tolerable once the application itself offers a
// form to author them, because a form has to know a field's prose, its allowed values, and
// its bounds — and a second, hand-maintained copy of those facts inside the UI would
// eventually disagree with the loader that enforces them.
//
// So the descriptor below is derived from the same consts the validator reads, and is
// served to the frontend over a binding. The guided form's controls, the raw editor's
// completion list, and the client-side pre-validation are all generated from it. A
// reflection test asserts every exported Spec field appears here, so a new field cannot be
// added to the format without being documented for the people who have to write it.

// FieldKind is a rule field's JSON type. It is what lets the editor pick a form control and
// colour a token from one descriptor instead of a switch on the field name.
type FieldKind string

const (
	KindString      FieldKind = "string"
	KindInt         FieldKind = "integer"
	KindStringArray FieldKind = "string[]"
)

// Field groups. The guided editor renders one section per group, in this order, which is
// what keeps metadata visibly separate from the matcher that actually decides what a rule
// does — the distinction a rule author most needs and most easily loses.
const (
	GroupIdentity = "identity"
	GroupMatcher  = "matcher"
	GroupMetadata = "metadata"
)

// FieldGroupOrder is the order the editor renders groups in.
var FieldGroupOrder = []string{GroupIdentity, GroupMatcher, GroupMetadata}

// Field describes one rule field completely enough that both editors can be generated from
// it: the raw editor takes Name/Kind/Enum for completion and colouring, the guided editor
// takes Description, Guidance and Example for its help text and Required/Enum/MinItems/
// MaxItems for its control.
//
// Description says what the field IS in one line. Guidance says how to choose a value, and
// is where the format's genuinely surprising rules live — that the id is a slug of the name,
// that labels[i] sits between two steps, that a higher format_version is refused outright.
type Field struct {
	Name        string    `json:"name"`
	Kind        FieldKind `json:"kind"`
	Required    bool      `json:"required"`
	Group       string    `json:"group"`
	ReadOnly    bool      `json:"read_only"`
	Default     any       `json:"default,omitempty"`
	Enum        []string  `json:"enum,omitempty"`
	MinItems    int       `json:"min_items,omitempty"`
	MaxItems    int       `json:"max_items,omitempty"`
	Description string    `json:"description"`
	Guidance    string    `json:"guidance"`
	Example     any       `json:"example"`
	// AppliesTo names the algorithms that READ this field. Empty means every algorithm.
	//
	// It is what lets the guided form show a rule's actual controls instead of every control
	// the format has: a lineage rule has no sequence and no window, and offering them would
	// invite an author to fill in fields that do nothing. It also drives the raw editor's
	// completion list, so a suggestion is never for a field the current algorithm ignores.
	//
	// The form HIDES an inapplicable field rather than disabling it — except when the file
	// already sets one, in which case it stays visible carrying the advisory that says it has
	// no effect. A field silently vanishing along with its value would be the one thing worse
	// than showing it.
	AppliesTo []string `json:"applies_to,omitempty"`
	// RequiresFormatVersion is the lowest rule format version that understands this field, so
	// the editor can say what setting it will cost in portability before it is set.
	RequiresFormatVersion int `json:"requires_format_version,omitempty"`
}

// Algorithm is one correlation algorithm as the editor needs to present it: the name a rule
// writes, the prose that explains what a match establishes, and the format version a rule must
// declare in order to select it.
type Algorithm struct {
	Name             string   `json:"name"`
	Summary          string   `json:"summary"`
	MinFormatVersion int      `json:"min_format_version"`
	RequiresSequence bool     `json:"requires_sequence"`
	Fields           []string `json:"fields"`
}

// Schema is the whole rule-format descriptor handed to the frontend, including the limits
// that are not per-field (the format version this build accepts, the file size cap).
type Schema struct {
	FormatVersion int      `json:"format_version"`
	MaxFileBytes  int64    `json:"max_file_bytes"`
	GroupOrder    []string `json:"group_order"`
	Fields        []Field  `json:"fields"`
	// Algorithms describes every matcher this build implements, so the guided form's algorithm
	// selector and the help beside it are generated rather than hand-listed in the UI.
	Algorithms []Algorithm `json:"algorithms"`
	// CorrelationFields is the vocabulary match_fields draws from, served for the same reason
	// as everything else here: a hand-maintained copy in the frontend would eventually offer a
	// field the extractor does not populate.
	CorrelationFields []string `json:"correlation_fields"`
}

// Describe returns the descriptor for the rule format this build understands.
//
// Every bound here is the consts value the validator enforces, never a second copy of the
// number: raising RuleMaxSequence changes the form's limit, the completion list and the
// validator in one edit, which is the only way the three stay honest with each other.
func Describe() Schema {
	return Schema{
		FormatVersion:     consts.RuleFormatVersion,
		MaxFileBytes:      consts.RuleMaxFileBytes,
		GroupOrder:        FieldGroupOrder,
		Algorithms:        describeAlgorithms(),
		CorrelationFields: consts.CorrelationSlots,
		Fields: []Field{
			{
				Name:        "name",
				Kind:        KindString,
				Required:    true,
				Group:       GroupIdentity,
				Description: "Human-readable name for the rule, and also its identity.",
				Guidance: "The rule's id is a slug of this name, so two rules with the same name collide " +
					"and renaming a rule replaces its identity. Name the pattern you are looking for, " +
					"not the event IDs that express it.",
				Example: "Failed Logons Then Successful Logon",
			},
			{
				Name:        "sequence",
				Kind:        KindStringArray,
				Required:    true,
				Group:       GroupMatcher,
				MinItems:    consts.RuleMinSequence,
				MaxItems:    consts.RuleMaxSequence,
				AppliesTo:   algorithmsRequiringSequence(),
				Description: "Ordered event IDs to match in chronological order within the scope.",
				Guidance: "Repeat an id to require it more than once. Matching is greedy and " +
					"non-overlapping, and steps need not be adjacent. On its own a match establishes a " +
					"temporally ordered pairing on one host — it does NOT establish that the events share " +
					"a user, account, or logon session. Add match_fields (with the 'field' algorithm) if " +
					"you need that.",
				Example: []string{"4625", "4625", "4625", "4624"},
			},
			{
				Name:      "labels",
				Kind:      KindStringArray,
				Group:     GroupMatcher,
				MaxItems:  consts.RuleMaxSequence - 1,
				AppliesTo: algorithmsRequiringSequence(),
				Description: "Optional label for each connection: labels[i] labels the edge from " +
					"sequence[i] to sequence[i+1].",
				Guidance: "A sequence of n steps has n−1 connections, so there are at most n−1 labels. " +
					"An empty string (or a missing trailing entry) leaves that connection untagged, which " +
					"lets you label only the hop that carries the meaning.",
				Example: []string{"", "", "then succeeds"},
			},
			{
				Name:                  consts.FieldMatchFields,
				Kind:                  KindStringArray,
				Group:                 GroupMatcher,
				Enum:                  consts.CorrelationSlots,
				MaxItems:              consts.CorrelationSlotCount,
				AppliesTo:             []string{consts.AlgoField, consts.AlgoTemporal},
				RequiresFormatVersion: 2,
				Description:           "Correlation fields every event in a match must share.",
				Guidance: "This is what turns a match from 'these happened in this order on this host' " +
					"into 'and they concern the same logon session'. An event that carries no value for a " +
					"listed field is EXCLUDED from matching rather than grouped with the others, and the " +
					"run reports how many were left out. Listing more fields narrows further; listing one " +
					"good one usually beats listing three.",
				Example: []string{"logon_id"},
			},
			{
				Name:                  consts.FieldMatchScope,
				Kind:                  KindString,
				Group:                 GroupMatcher,
				Default:               consts.DefaultScope,
				Enum:                  consts.CorrelationScopes,
				AppliesTo:             consts.AlgorithmNames(),
				RequiresFormatVersion: 2,
				Description:           "How events are partitioned before matching.",
				Guidance: "'" + consts.ScopeComputer + "' matches within one host, which is almost always " +
					"what you want — a chain assembled across unrelated machines is not evidence of " +
					"anything. '" + consts.ScopeGlobal + "' drops that partition and should be paired with " +
					"match_fields, or it will correlate across your whole case.",
				Example: consts.ScopeComputer,
			},
			{
				Name:                  consts.FieldWindowWithin,
				Kind:                  KindString,
				Group:                 GroupMatcher,
				AppliesTo:             []string{consts.AlgoTemporal},
				RequiresFormatVersion: 2,
				Description:           "Maximum time between consecutive matched steps, as a duration.",
				Guidance: "Written like \"90s\", \"5m\" or \"2h\". Required by the temporal algorithm: an " +
					"unbounded window would make it a slower spelling of the sequence algorithm. Choose it " +
					"from the behaviour you are describing — a password-guessing burst is minutes, a " +
					"persistence step after a logon can be hours.",
				Example: "5m",
			},
			{
				Name:                  consts.FieldWindowTotal,
				Kind:                  KindString,
				Group:                 GroupMatcher,
				AppliesTo:             []string{consts.AlgoTemporal},
				RequiresFormatVersion: 2,
				Description:           "Optional maximum time from the first matched step to the last.",
				Guidance: "Bounds the whole chain, not each hop. Useful for a long sequence where every " +
					"individual gap is plausible but the total span is not. Must be at least as long as " +
					"window_within, or no match could ever complete.",
				Example: "30m",
			},
			{
				Name:                  consts.FieldLineageCreateIDs,
				Kind:                  KindStringArray,
				Group:                 GroupMatcher,
				AppliesTo:             []string{consts.AlgoLineage},
				RequiresFormatVersion: 2,
				Description:           "Event IDs that record a process being created.",
				Guidance: "Defaults to " + consts.LineageDefaultCreateID + " (Windows process creation), so " +
					"most lineage rules do not set it. Override it to read a different provider — Sysmon's " +
					"process-creation event is 1.",
				Example: []string{consts.LineageDefaultCreateID},
			},
			{
				Name:                  consts.FieldLineageDepth,
				Kind:                  KindInt,
				Group:                 GroupMatcher,
				Default:               0,
				MaxItems:              consts.LineageMaxDepth,
				AppliesTo:             []string{consts.AlgoLineage},
				RequiresFormatVersion: 2,
				Description:           "How many ancestor levels above the direct parent to link.",
				Guidance: "0 — the default — emits only direct parent→child edges. Transitive links are " +
					"derivable by walking those, so raising this multiplies the edge count without adding " +
					"information; it is worth doing only when you want ancestry visible without traversal. " +
					"Edges beyond the direct parent are stamped with a lower confidence, because they are " +
					"derived rather than read from a record.",
				Example: 0,
			},
			{
				Name:        "description",
				Kind:        KindString,
				Group:       GroupMetadata,
				Default:     "",
				Description: "Free text shown in the rules list and the rule inspector.",
				Guidance: "State what a match does and does not establish, and name the channel the rule " +
					"depends on if it reaches outside Security and System. If the rule anchors on a " +
					"high-volume event such as 4672, say that the pairing is broad.",
				Example: "Three or more failed logon attempts (4625) followed by a successful logon (4624) " +
					"on the same computer.",
			},
			{
				Name:        "relation_type",
				Kind:        KindString,
				Group:       GroupMetadata,
				Default:     consts.RelationCorrelation,
				Enum:        []string{consts.RelationCorrelation, consts.RelationTemporal, consts.RelationDefault},
				Description: "Edge type stamped on every relation this rule produces.",
				Guidance: "Drives edge colouring on the graph canvas. An empty or unrecognized value " +
					"becomes " + consts.RelationCorrelation + " rather than being rejected.",
				Example: consts.RelationCorrelation,
			},
			{
				Name:        "algorithm",
				Kind:        KindString,
				Group:       GroupMetadata,
				Default:     consts.DefaultAlgorithm,
				Enum:        consts.AlgorithmNames(),
				Description: "How this rule's events are correlated into edges.",
				Guidance: "This is the single most consequential field, because it decides what a match " +
					"ESTABLISHES. 'sequence' proves ordering on one host and nothing more; 'field' adds " +
					"shared entity values; 'temporal' adds a bounded time window; 'lineage' reconstructs " +
					"process ancestry instead of matching a sequence at all. Anything else is refused at " +
					"load, so a rule can never half-run on a matcher that does not exist.",
				Example: consts.AlgoSequence,
			},
			{
				Name:        consts.FieldChannels,
				Kind:        KindStringArray,
				Group:       GroupMetadata,
				AppliesTo:   consts.AlgorithmNames(),
				Description: "The Windows log channels this rule needs in order to fire.",
				Guidance: "No algorithm reads this — it exists so rohy can tell you 'this rule cannot " +
					"fire, the log it depends on was never ingested' instead of reporting zero matches and " +
					"leaving you to work out why. Declare every channel the rule's event IDs come from. " +
					"A rule that omits it is simply not checked.",
				Example: []string{consts.ChannelSecurity},
			},
			{
				Name:        "format_version",
				Kind:        KindInt,
				Group:       GroupMetadata,
				ReadOnly:    true,
				Default:     1,
				Description: "The rule-format version this file targets.",
				Guidance: "Declare the LOWEST version your rule needs, not the newest that exists: a rule " +
					"using only sequence matching should say 1, so older builds still load it. Omit it and " +
					"rohy fills in the minimum the rule's algorithm requires. A file declaring a version " +
					"HIGHER than the build reading it is refused with an explanation rather than partially " +
					"matched, because a newer rule may rely on a matcher that build does not have.",
				Example: 1,
			},
		},
	}
}

// describeAlgorithms projects the engine's vocabulary into the editor's shape. It is derived
// from consts.Algorithms rather than listed again here, so an algorithm cannot appear in the
// selector without the validator accepting it.
func describeAlgorithms() []Algorithm {
	out := make([]Algorithm, 0, len(consts.Algorithms))
	for _, a := range consts.Algorithms {
		out = append(out, Algorithm{
			Name:             a.Name,
			Summary:          a.Summary,
			MinFormatVersion: a.MinFormatVersion,
			RequiresSequence: a.RequiresSequence,
			Fields:           a.Fields,
		})
	}
	return out
}

// algorithmsRequiringSequence lists the algorithms that match an event-ID sequence, so the
// sequence and labels fields advertise their applicability from the same source the validator
// enforces it from.
func algorithmsRequiringSequence() []string {
	var out []string
	for _, a := range consts.Algorithms {
		if a.RequiresSequence {
			out = append(out, a.Name)
		}
	}
	return out
}

// FieldByName returns a descriptor by field name.
func (s Schema) FieldByName(name string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// knownFields is the set of field names this build interprets. It backs the "fields this
// build does not interpret" report (§3 of RULES.md: unknown fields are ignored, not
// rejected) so the editor can show an author exactly what it is carrying but not reading,
// instead of leaving them to guess whether a field took effect.
func knownFields() map[string]bool {
	out := map[string]bool{}
	for _, f := range Describe().Fields {
		out[f.Name] = true
	}
	return out
}
