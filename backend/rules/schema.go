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
}

// Schema is the whole rule-format descriptor handed to the frontend, including the limits
// that are not per-field (the format version this build accepts, the file size cap).
type Schema struct {
	FormatVersion int      `json:"format_version"`
	MaxFileBytes  int64    `json:"max_file_bytes"`
	GroupOrder    []string `json:"group_order"`
	Fields        []Field  `json:"fields"`
}

// Describe returns the descriptor for the rule format this build understands.
//
// Every bound here is the consts value the validator enforces, never a second copy of the
// number: raising RuleMaxSequence changes the form's limit, the completion list and the
// validator in one edit, which is the only way the three stay honest with each other.
func Describe() Schema {
	return Schema{
		FormatVersion: consts.RuleFormatVersion,
		MaxFileBytes:  consts.RuleMaxFileBytes,
		GroupOrder:    FieldGroupOrder,
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
				Description: "Ordered event IDs to match in chronological order on one computer.",
				Guidance: "Repeat an id to require it more than once. Matching is greedy and " +
					"non-overlapping, and steps need not be adjacent. A match establishes a temporally " +
					"ordered pairing on one host — it does NOT establish that the events share a user, " +
					"account, or logon session.",
				Example: []string{"4625", "4625", "4625", "4624"},
			},
			{
				Name:     "labels",
				Kind:     KindStringArray,
				Group:    GroupMatcher,
				MaxItems: consts.RuleMaxSequence - 1,
				Description: "Optional label for each connection: labels[i] labels the edge from " +
					"sequence[i] to sequence[i+1].",
				Guidance: "A sequence of n steps has n−1 connections, so there are at most n−1 labels. " +
					"An empty string (or a missing trailing entry) leaves that connection untagged, which " +
					"lets you label only the hop that carries the meaning.",
				Example: []string{"", "", "then succeeds"},
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
				Enum:        []string{consts.AlgoSequence},
				Description: "How the sequence is correlated into edges.",
				Guidance: "'" + consts.AlgoSequence + "' is the only value this build accepts. Field-correlation " +
					"and temporal-window matchers are reserved but not implemented, and any other value is " +
					"refused at load so a rule can never half-run on a matcher that does not exist.",
				Example: consts.AlgoSequence,
			},
			{
				Name:        "format_version",
				Kind:        KindInt,
				Group:       GroupMetadata,
				ReadOnly:    true,
				Default:     consts.RuleFormatVersion,
				Description: "The rule-format version this file targets.",
				Guidance: "Omit it and the file is treated as current. A file declaring a version HIGHER " +
					"than the build reading it is refused with an explanation rather than partially " +
					"matched, because a newer rule may rely on a matcher this build does not have.",
				Example: consts.RuleFormatVersion,
			},
		},
	}
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
