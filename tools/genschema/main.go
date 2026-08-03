// Command genschema emits a JSON Schema for the rohy rule format, derived from
// rules.Describe() (P31).
//
// 🔒 It is GENERATED, never hand-written, and that is the whole point. A hand-maintained schema
// is a third statement of the rule format — after the Go validator and the guided editor — and
// three statements of one contract drift. What they drift into is the worst shape available: an
// editor that reports a file as valid, and a loader that then refuses it.
//
// So this reads the same descriptor the editor is built from, which is itself built from the
// consts the validator enforces. One edit to a bound moves the validator, the form, the
// completion list, and this file together.
//
// What a JSON Schema can and cannot say matters here. It expresses the SHAPE of a rule — types,
// enums, required keys, bounds — which is enough for an editor to underline a typo before the
// file is saved. It cannot express the algorithm-dependent rules ("a lineage rule must not carry
// a sequence", "match_fields is required when the algorithm is field"), because those are
// conditional on a value and JSON Schema's conditional forms would encode them a fourth time. The
// extension shells out to `rohyctl validate` for those, and the header of the emitted file says
// so — an editor that showed no error must not be read as an editor that found none.
//
// Usage:
//
//	go run ./tools/genschema                   # write the default output path
//	go run ./tools/genschema -o path.json      # write elsewhere
//	go run ./tools/genschema -stdout           # print, write nothing
//	go run ./tools/genschema -check            # exit 1 if the committed file is stale
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"rohy/backend/consts"
	"rohy/backend/rules"
)

// defaultOut is where the committed schema lives. The extension reads it from here, and the
// drift check compares against it.
const defaultOut = "tools/vscode-rohy/schemas/rohy-rule.schema.json"

// schemaID identifies the format. It is a URN rather than a URL because there is nothing to
// fetch: rohy makes no network calls, and a URL that 404s is worse than an identifier that never
// promised to resolve.
const schemaID = "urn:rohy:rule-format"

func main() {
	var out string
	var toStdout, check bool
	flag.StringVar(&out, "o", defaultOut, "output path")
	flag.BoolVar(&toStdout, "stdout", false, "print to stdout and write nothing")
	flag.BoolVar(&check, "check", false, "exit 1 if the committed file differs from what would be generated")
	flag.Parse()

	data, err := Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "genschema: %v\n", err)
		os.Exit(2)
	}

	if toStdout {
		os.Stdout.Write(data)
		return
	}
	if check {
		existing, err := os.ReadFile(out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "genschema: %s: %v\n(run `go run ./tools/genschema` to create it)\n", out, err)
			os.Exit(1)
		}
		if !bytes.Equal(existing, data) {
			fmt.Fprintf(os.Stderr, "genschema: %s is out of date — run `go run ./tools/genschema`\n", out)
			os.Exit(1)
		}
		fmt.Printf("%s is up to date\n", out)
		return
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "genschema: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "genschema: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("wrote %s\n", out)
}

// Generate renders the JSON Schema. Exported so the drift test calls exactly what the command
// does, rather than a copy that could agree with a stale file.
func Generate() ([]byte, error) {
	desc := rules.Describe()

	properties := map[string]any{}
	required := []string{}
	for _, f := range desc.Fields {
		properties[f.Name] = property(f)
		// 🔒 A field is listed as required here only when it is required for EVERY algorithm.
		//
		// `sequence` is required — but not for lineage, which matches no sequence at all. A schema
		// that listed it unconditionally would underline every valid lineage rule, and a red
		// squiggle on a correct file is the failure that makes somebody turn the extension off.
		// Under-reporting is recoverable: `rohyctl validate` catches a sequence rule with no
		// sequence. Over-reporting is not.
		if f.Required && appliesToAll(f) {
			required = append(required, f.Name)
		}
	}

	doc := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     schemaID,
		"title":   "rohy correlation rule",
		"description": "Generated from rules.Describe() by tools/genschema — do not edit by hand. " +
			"This describes the SHAPE of a rule. Rules that depend on the chosen algorithm " +
			"(a lineage rule carrying no sequence, match_fields being required for field " +
			"correlation) are not expressible here and are checked by `rohyctl validate`; an " +
			"editor showing no error is not an editor that found none.",
		"type": "object",
		// The format version and file cap this build accepts, so a schema file found on its own
		// says which rohy it came from. Prefixed, because they are not JSON Schema keywords and a
		// validator must ignore them rather than trip on them.
		"x-rohy-format-version": desc.FormatVersion,
		"x-rohy-max-file-bytes": desc.MaxFileBytes,
		// Unknown keys are ALLOWED, deliberately. The rule format preserves fields this build does
		// not interpret (RULES.md §3), so a schema that refused them would flag a forward-compatible
		// file as broken — and the loader would then accept the very thing the editor underlined.
		"additionalProperties": true,
		"properties":           properties,
		"required":             required,
	}
	// Indented and newline-terminated so the committed file diffs cleanly and a text editor does
	// not "fix" it on save, which would make the drift check fail for no reason.
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// appliesToAll reports whether every algorithm reads a field. An empty AppliesTo means "all", by
// the descriptor's own convention.
func appliesToAll(f rules.Field) bool {
	return len(f.AppliesTo) == 0 || len(f.AppliesTo) >= len(consts.AlgorithmNames())
}

// property renders one field. Everything it says comes from the descriptor; nothing is restated.
func property(f rules.Field) map[string]any {
	p := map[string]any{"description": describe(f)}

	switch f.Kind {
	case rules.KindInt:
		p["type"] = "integer"
	case rules.KindStringArray:
		p["type"] = "array"
		items := map[string]any{"type": "string"}
		if len(f.Enum) > 0 {
			items["enum"] = f.Enum
		}
		p["items"] = items
		if f.MinItems > 0 {
			p["minItems"] = f.MinItems
		}
		if f.MaxItems > 0 {
			p["maxItems"] = f.MaxItems
		}
	default:
		p["type"] = "string"
		if len(f.Enum) > 0 {
			p["enum"] = f.Enum
		}
	}

	if f.Default != nil {
		p["default"] = f.Default
	}
	if f.Example != nil {
		// An example the editor can offer as a completion. Wrapped in a list because that is the
		// keyword's shape, and a reader seeing one entry knows there could be more.
		p["examples"] = []any{f.Example}
	}
	return p
}

// describe assembles the hover text an editor shows: what the field is, how to choose a value,
// and which algorithms read it.
//
// The last part is why this is not just f.Description. A rule author's most common and most
// expensive mistake is filling in a field their algorithm ignores — the file saves, the rule
// loads, and it quietly does something other than what was written. Saying which algorithms read
// a field, at the point the field is being typed, is the cheapest place to prevent that.
func describe(f rules.Field) string {
	out := f.Description
	if f.Guidance != "" {
		out += "\n\n" + f.Guidance
	}
	// Only when the field is read by SOME algorithms. A field every algorithm reads has no "other
	// algorithms", and saying so anyway would train the reader to skip the sentence in the cases
	// where it carries the actual warning.
	if n := len(f.AppliesTo); n > 0 && n < len(consts.AlgorithmNames()) {
		out += "\n\nRead by: " + join(f.AppliesTo) + ". Other algorithms preserve it but ignore it."
	}
	if f.ReadOnly {
		out += "\n\nSet by rohy, not by the rule file."
	}
	return out
}

func join(xs []string) string {
	out := ""
	for i, x := range xs {
		switch {
		case i == 0:
		case i == len(xs)-1:
			out += " and "
		default:
			out += ", "
		}
		out += x
	}
	return out
}
