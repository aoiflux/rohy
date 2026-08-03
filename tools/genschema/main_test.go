package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/rules"
)

func generated(t *testing.T) map[string]any {
	t.Helper()
	data, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the generated schema is not valid JSON: %v", err)
	}
	return doc
}

func props(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	p, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatal("no properties in the generated schema")
	}
	return p
}

func TestCommittedSchemaIsNotStale(t *testing.T) {
	// 🔒 The drift gate. The schema is generated from rules.Describe(), so a change to the format
	// that did not regenerate it would leave an editor validating against the OLD contract — which
	// is the exact failure the generator exists to prevent, arriving through the back door.
	want, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", defaultOut)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run `go run ./tools/genschema`)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s is out of date — run `go run ./tools/genschema`", defaultOut)
	}
}

func TestEveryDescribedFieldAppears(t *testing.T) {
	// Generated means generated: nothing is hand-picked, so a field added to the descriptor must
	// arrive here without anybody editing this file.
	p := props(t, generated(t))
	for _, f := range rules.Describe().Fields {
		if _, ok := p[f.Name]; !ok {
			t.Errorf("field %q is described but missing from the schema", f.Name)
		}
	}
	if len(p) != len(rules.Describe().Fields) {
		t.Errorf("schema has %d properties, the descriptor has %d fields", len(p), len(rules.Describe().Fields))
	}
}

func TestOnlyUniversallyRequiredFieldsAreRequired(t *testing.T) {
	// 🔒 `sequence` is required — but not for lineage, which matches no sequence at all. Listing it
	// unconditionally would underline every valid lineage rule, and a red squiggle on a correct
	// file is the failure that makes somebody turn the extension off.
	doc := generated(t)
	req, _ := doc["required"].([]any)
	for _, r := range req {
		name, _ := r.(string)
		var field rules.Field
		for _, f := range rules.Describe().Fields {
			if f.Name == name {
				field = f
			}
		}
		if !appliesToAll(field) {
			t.Errorf("%q is marked required but is not read by every algorithm", name)
		}
	}
	// And `name` — which every algorithm needs — IS required, or the check above passes vacuously.
	found := false
	for _, r := range req {
		if r == "name" {
			found = true
		}
	}
	if !found {
		t.Error("`name` is not required, so the required list says nothing at all")
	}
}

func TestUnknownKeysAreAllowed(t *testing.T) {
	// 🔒 The rule format preserves fields this build does not interpret (RULES.md §3). A schema
	// that refused them would flag a forward-compatible file as broken — and the loader would then
	// accept the very thing the editor underlined.
	doc := generated(t)
	if doc["additionalProperties"] != true {
		t.Errorf("additionalProperties = %v, want true", doc["additionalProperties"])
	}
}

func TestTypesAndEnumsComeFromTheDescriptor(t *testing.T) {
	p := props(t, generated(t))

	algo, _ := p["algorithm"].(map[string]any)
	if algo["type"] != "string" {
		t.Errorf("algorithm type = %v", algo["type"])
	}
	enum, _ := algo["enum"].([]any)
	if len(enum) != len(consts.AlgorithmNames()) {
		t.Errorf("algorithm enum has %d entries, the engine registers %d", len(enum), len(consts.AlgorithmNames()))
	}

	seq, _ := p["sequence"].(map[string]any)
	if seq["type"] != "array" {
		t.Errorf("sequence type = %v", seq["type"])
	}
	items, _ := seq["items"].(map[string]any)
	if items["type"] != "string" {
		t.Errorf("sequence items = %v", items)
	}
	if seq["minItems"] == nil || seq["maxItems"] == nil {
		t.Errorf("the sequence bounds the validator enforces are missing: %v", seq)
	}

	depth, _ := p["lineage_depth"].(map[string]any)
	if depth["type"] != "integer" {
		t.Errorf("lineage_depth type = %v", depth["type"])
	}
}

func TestMatchFieldsOffersTheCorrelationVocabulary(t *testing.T) {
	// A hand-maintained copy in the schema would eventually offer a field the extractor does not
	// populate — a completion that produces a rule matching on nothing.
	p := props(t, generated(t))
	mf, _ := p["match_fields"].(map[string]any)
	items, _ := mf["items"].(map[string]any)
	enum, _ := items["enum"].([]any)
	if len(enum) != len(consts.CorrelationSlots) {
		t.Fatalf("match_fields offers %d values, the vocabulary has %d", len(enum), len(consts.CorrelationSlots))
	}
	if enum[0] != consts.CorrelationSlots[0] {
		t.Errorf("first offered field = %v, want %q", enum[0], consts.CorrelationSlots[0])
	}
}

func TestDescriptionsSayWhichAlgorithmsReadAField(t *testing.T) {
	// A rule author's most expensive mistake is filling in a field their algorithm ignores: the
	// file saves, the rule loads, and it quietly does something other than what was written.
	p := props(t, generated(t))
	mf, _ := p["match_fields"].(map[string]any)
	desc, _ := mf["description"].(string)
	if !strings.Contains(desc, "Read by:") {
		t.Errorf("match_fields does not say which algorithms read it: %q", desc)
	}

	// And a field EVERY algorithm reads does not carry the sentence, or the reader learns to skip
	// it in the cases where it matters.
	name, _ := p["name"].(map[string]any)
	if nd, _ := name["description"].(string); strings.Contains(nd, "Read by:") {
		t.Errorf("a universally-read field claims to be algorithm-specific: %q", nd)
	}
}

func TestNoShippedRuleWouldBeFlaggedByTheSchema(t *testing.T) {
	// 🔒 The false-positive gate, checked against the library the app actually ships. The schema
	// under-reports on purpose (algorithm-dependent rules go to `rohyctl validate`); what it must
	// never do is mark a correct rule wrong.
	doc := generated(t)
	p := props(t, doc)
	req, _ := doc["required"].([]any)

	dir := filepath.Join("..", "..", "backend", "rules", "builtin")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var rule map[string]any
		if err := json.Unmarshal(data, &rule); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		checked++

		for _, r := range req {
			key, _ := r.(string)
			if _, ok := rule[key]; !ok {
				t.Errorf("%s: the schema requires %q, which this shipped rule does not have", e.Name(), key)
			}
		}
		for key, value := range rule {
			spec, ok := p[key].(map[string]any)
			if !ok {
				continue // an unknown key is allowed by design
			}
			if got, want := jsonType(value), spec["type"]; got != want {
				t.Errorf("%s: %q is %s, the schema says %v", e.Name(), key, got, want)
			}
			checkEnum(t, e.Name(), key, spec, value)
		}
	}
	if checked == 0 {
		t.Fatal("no built-in rules were checked — the path is wrong")
	}
}

// checkEnum verifies a value against the schema's enum, for both scalar and array fields.
func checkEnum(t *testing.T, file, key string, spec map[string]any, value any) {
	t.Helper()
	allowed := func(enum []any, v any) bool {
		for _, e := range enum {
			if e == v {
				return true
			}
		}
		return false
	}
	if enum, ok := spec["enum"].([]any); ok {
		if !allowed(enum, value) {
			t.Errorf("%s: %q = %v, which the schema's enum does not allow", file, key, value)
		}
		return
	}
	items, ok := spec["items"].(map[string]any)
	if !ok {
		return
	}
	enum, ok := items["enum"].([]any)
	if !ok {
		return
	}
	list, ok := value.([]any)
	if !ok {
		return
	}
	for _, v := range list {
		if !allowed(enum, v) {
			t.Errorf("%s: %q contains %v, which the schema's enum does not allow", file, key, v)
		}
	}
}

// jsonType names a decoded JSON value's type in the schema's vocabulary. Numbers decode as
// float64, and every numeric field in this format is an integer, so it reports "integer" —
// distinguishing them would flag a valid `"lineage_depth": 2` as the wrong type.
func jsonType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "integer"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "null"
	}
}

func TestGeneratedFileEndsInANewline(t *testing.T) {
	// So the committed file diffs cleanly and an editor does not "fix" it on save — which would
	// fail the drift check for no reason at all.
	data, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("the generated schema does not end in a newline")
	}
}

func TestSchemaRecordsTheBuildItCameFrom(t *testing.T) {
	doc := generated(t)
	if doc["x-rohy-format-version"] != float64(rules.Describe().FormatVersion) {
		t.Errorf("format version = %v, want %d", doc["x-rohy-format-version"], rules.Describe().FormatVersion)
	}
}

func TestJoinReadsAsASentence(t *testing.T) {
	if got := join([]string{"a", "b", "c"}); got != "a, b and c" {
		t.Errorf("join = %q", got)
	}
	if got := join([]string{"a", "b"}); got != "a and b" {
		t.Errorf("join = %q", got)
	}
	if got := join([]string{"a"}); got != "a" {
		t.Errorf("join = %q", got)
	}
	if got := join(nil); got != "" {
		t.Errorf("join = %q", got)
	}
}
