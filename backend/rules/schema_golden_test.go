package rules

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// Schema regression golden.
//
// The descriptor is a WIRE CONTRACT. Three consumers generate themselves from it — the guided
// form's controls, the raw editor's completion list, and the client-side pre-validator — and a
// fourth is planned (a JSON Schema for the editor extension). A field quietly gaining an enum
// value, losing a bound, or changing which algorithms it applies to changes what all of them
// offer, and nothing else in the suite would notice.
//
// WHAT IS PINNED, and what deliberately is not:
//
// The golden holds the STRUCTURAL surface only — names, kinds, groups, enums, bounds,
// applicability, format requirements — and not Description, Guidance or Example.
//
// Pinning the prose was the obvious version and is the wrong one. Every wording improvement
// would fail this test, the update would become reflexive, and a golden that is updated
// without being read stops being a tripwire and becomes a chore. The prose is already
// guarded: TestSchemaFieldsAreFullyDocumented requires all three to be present, and
// TestSchemaExamplesAreAccepted runs every example through the real loader.
//
// A failure here means the format changed. That is a change that also belongs in RULES.md,
// which is the whole reason this test makes it impossible to do quietly.

var updateGolden = flag.Bool("update-schema-golden", false,
	"rewrite testdata/schema-golden.json from the current descriptor")

// goldenField is the structural projection of one field descriptor.
type goldenField struct {
	Name      string    `json:"name"`
	Kind      FieldKind `json:"kind"`
	Required  bool      `json:"required"`
	Group     string    `json:"group"`
	ReadOnly  bool      `json:"read_only"`
	Default   any       `json:"default,omitempty"`
	Enum      []string  `json:"enum,omitempty"`
	MinItems  int       `json:"min_items,omitempty"`
	MaxItems  int       `json:"max_items,omitempty"`
	AppliesTo []string  `json:"applies_to,omitempty"`
}

// goldenSchema is the structural projection of the whole descriptor.
type goldenSchema struct {
	FormatVersion     int           `json:"format_version"`
	MaxFileBytes      int64         `json:"max_file_bytes"`
	GroupOrder        []string      `json:"group_order"`
	CorrelationFields []string      `json:"correlation_fields"`
	Algorithms        []Algorithm   `json:"algorithms"`
	Fields            []goldenField `json:"fields"`
}

func structuralSchema() goldenSchema {
	s := Describe()
	out := goldenSchema{
		FormatVersion:     s.FormatVersion,
		MaxFileBytes:      s.MaxFileBytes,
		GroupOrder:        s.GroupOrder,
		CorrelationFields: s.CorrelationFields,
		Algorithms:        s.Algorithms,
	}
	for _, f := range s.Fields {
		out.Fields = append(out.Fields, goldenField{
			Name:      f.Name,
			Kind:      f.Kind,
			Required:  f.Required,
			Group:     f.Group,
			ReadOnly:  f.ReadOnly,
			Default:   f.Default,
			Enum:      f.Enum,
			MinItems:  f.MinItems,
			MaxItems:  f.MaxItems,
			AppliesTo: f.AppliesTo,
		})
	}
	return out
}

func TestSchemaGolden(t *testing.T) {
	path := filepath.Join("testdata", "schema-golden.json")

	current, err := json.MarshalIndent(structuralSchema(), "", "  ")
	if err != nil {
		t.Fatalf("encode descriptor: %v", err)
	}
	current = append(current, '\n')

	if *updateGolden {
		if err := os.WriteFile(path, current, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("rewrote %s — check the diff, and update RULES.md in the same change", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update-schema-golden to create it): %v", err)
	}
	if string(current) != string(want) {
		t.Errorf("the rule format descriptor changed.\n\n"+
			"This is a wire contract: the guided form, the completion list and the client-side "+
			"validator all generate themselves from it.\n\n"+
			"If the change is intended, run:\n"+
			"    go test ./backend/rules/ -run TestSchemaGolden -update-schema-golden\n"+
			"and update RULES.md in the same change, so the document that authors read still "+
			"describes the format the loader enforces.\n\ngot:\n%s\nwant:\n%s", current, want)
	}
}
