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

// TestSchemaEnumValuesAreAccepted runs every advertised value through the real loader. An
// enum the form offers but the validator refuses would make the guided editor produce a
// rule that cannot be saved — the single most confusing failure a form can have.
func TestSchemaEnumValuesAreAccepted(t *testing.T) {
	for _, f := range Describe().Fields {
		for _, value := range f.Enum {
			body := fmt.Sprintf(`{"name":"Enum Probe","description":"4624 then 1102.",
				"sequence":["4624","1102"],%q:%q}`, f.Name, value)
			if _, err := Parse([]byte(body)); err != nil {
				t.Errorf("%s = %q is offered by the schema but rejected by the loader: %v", f.Name, value, err)
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
