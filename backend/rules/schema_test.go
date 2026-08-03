package rules

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"rohy/backend/consts"
)

// specFieldNames returns the JSON name of every field in Spec, by reflection.
func specFieldNames(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(Spec{})
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("Spec.%s has no json tag; the rule format is a wire contract", typ.Field(i).Name)
		}
		out = append(out, strings.Split(tag, ",")[0])
	}
	return out
}

// TestSchemaDescribesEverySpecField is the guard that keeps the format honest with the
// people who have to write it.
//
// Adding a field to Spec is a one-line change that silently does three things: the loader
// starts reading it, the editor's completion list stops being complete, and the guided form
// omits a control for a field that now matters. Reflection catches the omission at the
// moment the field is added, rather than when an author eventually asks why the form cannot
// set it.
func TestSchemaDescribesEverySpecField(t *testing.T) {
	schema := Describe()
	for _, name := range specFieldNames(t) {
		if _, ok := schema.FieldByName(name); !ok {
			t.Errorf("Spec field %q has no descriptor in Describe(); add one so both editors can offer it", name)
		}
	}
	// And the other way: a descriptor for a field the loader does not read would put a
	// control in the form that quietly does nothing.
	known := map[string]bool{}
	for _, name := range specFieldNames(t) {
		known[name] = true
	}
	for _, f := range schema.Fields {
		if !known[f.Name] {
			t.Errorf("descriptor %q has no matching Spec field; the form would offer a field the loader ignores", f.Name)
		}
	}
}

func TestSchemaFieldsAreFullyDocumented(t *testing.T) {
	schema := Describe()
	groups := map[string]bool{}
	for _, g := range schema.GroupOrder {
		groups[g] = true
	}

	for _, f := range schema.Fields {
		if f.Description == "" {
			t.Errorf("%s: no description", f.Name)
		}
		if f.Guidance == "" {
			t.Errorf("%s: no guidance — the form would show a control with no advice on choosing a value", f.Name)
		}
		if f.Example == nil {
			t.Errorf("%s: no example", f.Name)
		}
		if !groups[f.Group] {
			t.Errorf("%s: group %q is not in GroupOrder, so the form would not render it", f.Name, f.Group)
		}
		if f.Required && f.Default != nil {
			t.Errorf("%s: a required field cannot have a default", f.Name)
		}
	}
}

// TestSchemaBoundsMatchTheValidator pins the descriptor to the consts the validator reads.
// A form that advertises a limit the loader does not enforce (or vice versa) is worse than
// no form: it teaches the author a rule that is not true.
func TestSchemaBoundsMatchTheValidator(t *testing.T) {
	schema := Describe()
	if schema.FormatVersion != consts.RuleFormatVersion {
		t.Errorf("schema format version = %d, want %d", schema.FormatVersion, consts.RuleFormatVersion)
	}
	if schema.MaxFileBytes != consts.RuleMaxFileBytes {
		t.Errorf("schema max file bytes = %d, want %d", schema.MaxFileBytes, int64(consts.RuleMaxFileBytes))
	}

	seq, _ := schema.FieldByName("sequence")
	if seq.MinItems != consts.RuleMinSequence || seq.MaxItems != consts.RuleMaxSequence {
		t.Errorf("sequence bounds = %d..%d, want %d..%d",
			seq.MinItems, seq.MaxItems, consts.RuleMinSequence, consts.RuleMaxSequence)
	}
	// A sequence of n steps has n−1 connections, so labels can never exceed that.
	labels, _ := schema.FieldByName("labels")
	if labels.MaxItems != consts.RuleMaxSequence-1 {
		t.Errorf("labels max = %d, want %d", labels.MaxItems, consts.RuleMaxSequence-1)
	}
}

// probeRule builds a rule that is valid for the given algorithm, so a probe exercises the one
// field it is about rather than tripping over that algorithm's other requirements.
//
// It exists because the format stopped being uniform: selecting `field` makes match_fields
// mandatory, `temporal` makes window_within mandatory, and `lineage` removes the sequence
// entirely. A single fixed probe body could only ever have tested one of the four.
func probeRule(algorithm string) map[string]any {
	spec := map[string]any{
		"name":        "Probe",
		"description": "4624 then 1102.",
		"channels":    []string{consts.ChannelSecurity},
	}
	algo, ok := consts.AlgorithmByName(algorithm)
	if !ok {
		return spec
	}
	spec["algorithm"] = algorithm
	if algo.RequiresSequence {
		spec["sequence"] = []string{"4624", "1102"}
	}
	if algorithm == consts.AlgoField {
		spec["match_fields"] = []string{consts.CorrelationSlots[0]}
	}
	if algorithm == consts.AlgoTemporal {
		spec["window_within"] = "5m"
	}
	return spec
}

// parseProbe encodes and loads a probe rule.
func parseProbe(t *testing.T, spec map[string]any) error {
	t.Helper()
	body, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal probe: %v", err)
	}
	_, err = Parse(body)
	return err
}

// fieldAlgorithms returns the algorithms a field applies to, defaulting to every algorithm
// when the descriptor does not narrow it.
func fieldAlgorithms(f Field) []string {
	if len(f.AppliesTo) > 0 {
		return f.AppliesTo
	}
	return consts.AlgorithmNames()
}

// TestSchemaEnumValuesAreAccepted runs every advertised value through the real loader. An
// enum the form offers but the validator refuses would make the guided editor produce a
// rule that cannot be saved — the single most confusing failure a form can have.
//
// Each value is probed under an algorithm that actually reads the field, because a value can
// now be legal for one matcher and inert for another.
func TestSchemaEnumValuesAreAccepted(t *testing.T) {
	for _, f := range Describe().Fields {
		if len(f.Enum) == 0 {
			continue
		}
		for _, algorithm := range fieldAlgorithms(f) {
			for _, value := range f.Enum {
				spec := probeRule(algorithm)
				// The algorithm field's own enum names the algorithm to probe under, rather
				// than being a value set on some other algorithm's rule.
				if f.Name == consts.FieldAlgorithm {
					spec = probeRule(value)
				} else if f.Kind == KindStringArray {
					spec[f.Name] = []string{value}
				} else {
					spec[f.Name] = value
				}
				if err := parseProbe(t, spec); err != nil {
					t.Errorf("%s = %q (under algorithm %q) is offered by the schema but rejected by the loader: %v",
						f.Name, value, algorithm, err)
				}
			}
		}
	}
}

// TestEveryAlgorithmHasAWorkingMinimalRule pins the claim the selector makes: choosing any
// algorithm and filling in what it asks for produces a rule that loads. An algorithm that
// cannot be satisfied would be an option in the form that leads nowhere.
func TestEveryAlgorithmHasAWorkingMinimalRule(t *testing.T) {
	for _, algo := range consts.Algorithms {
		if err := parseProbe(t, probeRule(algo.Name)); err != nil {
			t.Errorf("the minimal %q rule does not load: %v", algo.Name, err)
		}
	}
}

// TestSchemaAlgorithmsMatchTheVocabulary keeps the descriptor's algorithm list and the
// validator's accepted set the same list. They are derived from one source; this asserts the
// derivation was not replaced by a hand-written copy.
func TestSchemaAlgorithmsMatchTheVocabulary(t *testing.T) {
	schema := Describe()
	if len(schema.Algorithms) != len(consts.Algorithms) {
		t.Fatalf("descriptor lists %d algorithms, consts has %d", len(schema.Algorithms), len(consts.Algorithms))
	}
	for i, a := range schema.Algorithms {
		want := consts.Algorithms[i]
		if a.Name != want.Name ||
			a.RequiresSequence != want.RequiresSequence || a.Summary != want.Summary {
			t.Errorf("algorithm %d: descriptor %+v disagrees with consts %+v", i, a, want)
		}
	}
	// The algorithm field's enum is what the selector renders; it must be the same set.
	field, ok := schema.FieldByName(consts.FieldAlgorithm)
	if !ok {
		t.Fatal("no algorithm field in the descriptor")
	}
	if strings.Join(field.Enum, ",") != strings.Join(consts.AlgorithmNames(), ",") {
		t.Errorf("algorithm enum = %v, want %v", field.Enum, consts.AlgorithmNames())
	}
	// match_fields offers the correlation vocabulary; an option the extractor never populates
	// would be a field the author can select and that silently matches nothing.
	mf, ok := schema.FieldByName(consts.FieldMatchFields)
	if !ok {
		t.Fatal("no match_fields in the descriptor")
	}
	if strings.Join(mf.Enum, ",") != strings.Join(consts.CorrelationSlots, ",") {
		t.Errorf("match_fields enum = %v, want the correlation slots %v", mf.Enum, consts.CorrelationSlots)
	}
}

// TestAppliesToNamesRealAlgorithms stops a descriptor from restricting a field to an
// algorithm that does not exist, which would hide the control in every form forever.
func TestAppliesToNamesRealAlgorithms(t *testing.T) {
	for _, f := range Describe().Fields {
		for _, name := range f.AppliesTo {
			if _, ok := consts.AlgorithmByName(name); !ok {
				t.Errorf("field %q applies_to unknown algorithm %q", f.Name, name)
			}
		}
	}
	// And the other way: every field an algorithm claims to read must exist in the descriptor,
	// or the algorithm advertises a setting the editor cannot offer.
	for _, algo := range consts.Algorithms {
		for _, field := range algo.Fields {
			if _, ok := Describe().FieldByName(field); !ok {
				t.Errorf("algorithm %q reads field %q, which has no descriptor", algo.Name, field)
			}
		}
	}
}

// TestSchemaDefaultsAreAccepted does the same for the defaults, including the one the form
// seeds a brand-new rule with.
func TestSchemaDefaultsAreAccepted(t *testing.T) {
	for _, f := range Describe().Fields {
		if f.Default == nil {
			continue
		}
		encoded, err := json.Marshal(f.Default)
		if err != nil {
			t.Fatalf("%s: default is not serializable: %v", f.Name, err)
		}
		body := fmt.Sprintf(`{"name":"Default Probe","description":"4624 then 1102.",
			"sequence":["4624","1102"],%q:%s}`, f.Name, encoded)
		if _, err := Parse([]byte(body)); err != nil {
			t.Errorf("%s default %s is rejected by the loader: %v", f.Name, encoded, err)
		}
	}
}

// TestSchemaExamplesAreAccepted checks the examples too. They are copy-paste starting points
// in the guided editor's help text, so an example that does not load is a trap.
func TestSchemaExamplesAreAccepted(t *testing.T) {
	for _, f := range Describe().Fields {
		encoded, err := json.Marshal(f.Example)
		if err != nil {
			t.Fatalf("%s: example is not serializable: %v", f.Name, err)
		}
		// Build a rule whose other fields are valid, with this field set to its example.
		spec := map[string]any{
			"name":        "Example Probe",
			"description": "4624 then 1102.",
			"sequence":    []string{"4624", "1102"},
		}
		var value any
		if err := json.Unmarshal(encoded, &value); err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		spec[f.Name] = value
		// labels is bounded by the sequence length, so give the labels example enough steps
		// to be legal — the example is about the shape, not about this probe's sequence.
		if f.Name == "labels" {
			spec["sequence"] = []string{"4625", "4625", "4625", "4624"}
		}
		body, _ := json.Marshal(spec)
		if _, err := Parse(body); err != nil {
			t.Errorf("%s example %s is rejected by the loader: %v", f.Name, encoded, err)
		}
	}
}

func TestKnownFieldsMatchesTheDescriptor(t *testing.T) {
	known := knownFields()
	if len(known) != len(Describe().Fields) {
		t.Errorf("knownFields has %d entries, descriptor has %d", len(known), len(Describe().Fields))
	}
	// unknownFields is what tells an author which of their keys this build ignores; if it
	// disagreed with the descriptor it would either hide a real field or invent a fake one.
	if got := unknownFields([]string{"name", "sequence", "max_gap_seconds"}); len(got) != 1 || got[0] != "max_gap_seconds" {
		t.Errorf("unknownFields = %v, want [max_gap_seconds]", got)
	}
}
