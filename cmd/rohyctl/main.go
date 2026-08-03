// Command rohyctl works with rohy rule files from a terminal: validate them, format them, and
// explain what they do.
//
// 🔒 It never opens a case. The import list is `rules`, `consts` and `version` and nothing else —
// no graphene, no sidecars, no state. That is what lets it run in CI, in a pre-commit hook, and on
// a machine that has never seen the application, over rule files checked into somebody's own
// repository.
//
// It exists because a rule is a portable JSON file and portable files get edited by things that
// are not rohy. Three commands cover what that needs:
//
//	validate  would this load? — the SAME check the loader runs, never a second implementation
//	format    the canonical shape, with --check as a CI gate
//	explain   what does this rule actually establish?
//
// The first is the one that matters. A separate CLI validator would drift from the loader, and the
// drift would surface as a rule that passes in the terminal and is silently missing from the
// library on the next scan. So `validate` calls rules.ValidateSource — the same function the
// editor calls, and the same one the loader is built on.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rohy/backend/consts"
	"rohy/backend/rules"
	"rohy/backend/version"
)

const usage = `rohyctl — work with rohy correlation rules

Usage:
  rohyctl validate <path>...        check whether rule files would load
  rohyctl format [flags] <path>...  print or rewrite rule files in canonical form
  rohyctl explain [name|path]...    describe an algorithm, or what a rule establishes
  rohyctl version                   print the build identity

A path may be a file or a directory; directories are searched for .json files.
With no path, validate and format read from stdin.

Flags for format:
  -w, --write    rewrite files in place
  -c, --check    exit 1 if any file is not already formatted; write nothing

Exit codes:
  0  everything checked was fine
  1  a rule failed to validate, or --check found unformatted files
  2  the command could not run (bad usage, unreadable path)
`

// Exit codes. Two is reserved for "could not run at all", so a CI job can tell a rule problem
// apart from a broken invocation — the two want different responses.
const (
	exitOK      = 0
	exitProblem = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Stdin))
}

// run is main with its I/O injected, so every command is testable without a subprocess.
func run(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "validate":
		return cmdValidate(args[1:], stdout, stderr, stdin)
	case "format":
		return cmdFormat(args[1:], stdout, stderr, stdin)
	case "explain":
		return cmdExplain(args[1:], stdout, stderr)
	case "version":
		v := version.Current(consts.AppName)
		suffix := ""
		if v.Development {
			// An unstamped local build says so rather than presenting itself as a release — the
			// same thing the About dialog does, for the same reason.
			suffix = " (dev build)"
		}
		fmt.Fprintf(stdout, "%sctl %s %s %s%s\n", v.Name, v.Version, v.Commit, v.Date, suffix)
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

// --- validate ---

func cmdValidate(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	sources, code := gather(fs.Args(), stdin, stderr)
	if code != exitOK {
		return code
	}

	failed := 0
	for _, src := range sources {
		report := rules.ValidateSource(src.data)
		for _, e := range report.Errors {
			fmt.Fprintf(stdout, "%s\n", problemLine(src.name, "error", e))
		}
		// Warnings are printed but do not fail the run. They are the things that are legal and
		// worth saying — a field this build does not interpret, a rule that declares no channels —
		// and turning them into failures would make the CI gate refuse rules the app accepts.
		for _, e := range report.Warnings {
			fmt.Fprintf(stdout, "%s\n", problemLine(src.name, "warning", e))
		}
		if !report.Valid {
			failed++
		}
	}

	fmt.Fprintf(stdout, "%s\n", checkedSummary(len(sources), failed))
	if failed > 0 {
		return exitProblem
	}
	return exitOK
}

// problemLine renders one problem in the file:line:col shape editors and CI logs both parse.
func problemLine(name, kind string, e rules.ValidationError) string {
	loc := name
	if e.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", name, e.Line, e.Col)
	}
	field := ""
	if e.Field != "" {
		field = " (" + e.Field + ")"
	}
	return fmt.Sprintf("%s: %s: %s%s [%s]", loc, kind, e.Message, field, e.Code)
}

func checkedSummary(total, failed int) string {
	if failed == 0 {
		return fmt.Sprintf("checked %s, all valid", plural(total, "file"))
	}
	return fmt.Sprintf("checked %s, %d failed", plural(total, "file"), failed)
}

// --- format ---

func cmdFormat(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	fs := flag.NewFlagSet("format", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var write, check bool
	fs.BoolVar(&write, "w", false, "rewrite files in place")
	fs.BoolVar(&write, "write", false, "rewrite files in place")
	fs.BoolVar(&check, "c", false, "exit 1 if any file is not already formatted")
	fs.BoolVar(&check, "check", false, "exit 1 if any file is not already formatted")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if write && check {
		// They contradict: one rewrites, the other promises to write nothing. Guessing which was
		// meant risks rewriting files in a job that was only supposed to inspect them.
		fmt.Fprintln(stderr, "format: --write and --check cannot be used together")
		return exitUsage
	}

	sources, code := gather(fs.Args(), stdin, stderr)
	if code != exitOK {
		return code
	}

	unformatted, failed := 0, 0
	for _, src := range sources {
		pretty, err := rules.Pretty(src.data)
		if err != nil {
			// Unparseable text has no canonical form. Reported and skipped rather than rewritten,
			// because "format" must never be the thing that damages a file somebody was mid-edit on.
			fmt.Fprintf(stderr, "%s: cannot format: %v\n", src.name, err)
			failed++
			continue
		}
		same := string(pretty) == string(src.data)
		switch {
		case check:
			if !same {
				fmt.Fprintf(stdout, "%s\n", src.name)
				unformatted++
			}
		case write:
			if same || src.path == "" {
				continue
			}
			if err := os.WriteFile(src.path, pretty, 0o644); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", src.name, err)
				failed++
				continue
			}
			fmt.Fprintf(stdout, "%s\n", src.name)
		default:
			stdout.Write(pretty)
		}
	}

	if check {
		if unformatted == 0 {
			fmt.Fprintf(stdout, "%s already formatted\n", plural(len(sources), "file"))
		} else {
			fmt.Fprintf(stdout, "%s not formatted\n", plural(unformatted, "file"))
		}
	}
	if failed > 0 || unformatted > 0 {
		return exitProblem
	}
	return exitOK
}

// --- explain ---

func cmdExplain(args []string, stdout, stderr io.Writer) int {
	schema := rules.Describe()
	if len(args) == 0 {
		// With nothing named, list what can be explained rather than printing everything: the
		// full schema is long, and a wall of text is not an answer to "what can I ask about".
		fmt.Fprintf(stdout, "Algorithms: %s\n", strings.Join(consts.AlgorithmNames(), ", "))
		fmt.Fprintln(stdout, "\nName one to describe it, or pass a rule file to describe what it establishes.")
		return exitOK
	}

	code := exitOK
	for _, arg := range args {
		if desc, ok := consts.AlgorithmByName(arg); ok {
			explainAlgorithm(stdout, schema, desc)
			continue
		}
		data, err := os.ReadFile(arg)
		if err != nil {
			fmt.Fprintf(stderr, "%s: not an algorithm name, and not a readable file: %v\n", arg, err)
			code = exitUsage
			continue
		}
		if !explainRule(stdout, stderr, arg, data) {
			code = exitProblem
		}
	}
	return code
}

func explainAlgorithm(w io.Writer, schema rules.Schema, desc consts.AlgorithmDescriptor) {
	fmt.Fprintf(w, "%s\n", desc.Name)
	fmt.Fprintf(w, "  %s\n", wrap(desc.Summary, 76, "  "))
	if desc.RequiresSequence {
		fmt.Fprintln(w, "  Matches an ordered event-ID sequence.")
	} else {
		fmt.Fprintln(w, "  Does not match a sequence; it reconstructs structure from the events themselves.")
	}
	if len(desc.Fields) == 0 {
		fmt.Fprintln(w, "  Reads no algorithm-specific fields.")
	} else {
		fmt.Fprintf(w, "  Reads: %s\n", strings.Join(desc.Fields, ", "))
		for _, name := range desc.Fields {
			if f, ok := schema.FieldByName(name); ok && f.Description != "" {
				fmt.Fprintf(w, "    %-20s %s\n", name, wrap(f.Description, 54, strings.Repeat(" ", 25)))
			}
		}
	}
	fmt.Fprintln(w)
}

// explainRule says what one rule does and — the part worth having — what a match of it actually
// establishes. Returns false when the file would not load.
func explainRule(stdout, stderr io.Writer, name string, data []byte) bool {
	report := rules.ValidateSource(data)
	if !report.Valid || report.Normalized == nil {
		fmt.Fprintf(stderr, "%s: does not load\n", name)
		for _, e := range report.Errors {
			fmt.Fprintf(stderr, "  %s\n", problemLine(name, "error", e))
		}
		return false
	}
	spec := report.Normalized
	algo := spec.AlgorithmOrDefault()

	fmt.Fprintf(stdout, "%s\n", spec.Name)
	if spec.Description != "" {
		fmt.Fprintf(stdout, "  %s\n", wrap(spec.Description, 76, "  "))
	}
	fmt.Fprintf(stdout, "  algorithm: %s\n", algo)
	if len(spec.Sequence) > 0 {
		fmt.Fprintf(stdout, "  sequence:  %s\n", strings.Join(spec.Sequence, " → "))
	}
	if len(spec.Channels) > 0 {
		fmt.Fprintf(stdout, "  needs:     %s\n", strings.Join(spec.Channels, ", "))
	} else {
		// Silence about channels means "not declared", never "needs nothing" — the same
		// distinction the integrity checker refuses to blur.
		fmt.Fprintln(stdout, "  needs:     not declared — rohy cannot warn when this rule's log is missing")
	}

	fmt.Fprintf(stdout, "  a match establishes:\n")
	for _, line := range establishes(spec, algo) {
		fmt.Fprintf(stdout, "    · %s\n", line)
	}
	for _, e := range report.Warnings {
		fmt.Fprintf(stdout, "  note: %s\n", e.Message)
	}
	fmt.Fprintln(stdout)
	return true
}

// establishes is the whole point of `explain`: what a match of this rule PROVES, in plain words.
//
// It is assembled from the rule's own fields rather than from a per-algorithm sentence, because
// what a match establishes depends on how the rule was written — a sequence rule with match_fields
// establishes strictly more than one without, and saying otherwise would overstate the weaker one
// or understate the stronger.
func establishes(spec *rules.Spec, algo string) []string {
	out := make([]string, 0, 4)

	switch algo {
	case consts.AlgoLineage:
		out = append(out, "one process created another, resolved through the PID's lifetime rather than by matching the number")
	default:
		out = append(out, fmt.Sprintf("these %d event IDs occurred in this order", len(spec.Sequence)))
	}

	scope := spec.ScopeOrDefault()
	if scope == consts.ScopeComputer {
		out = append(out, "on one computer")
	} else {
		out = append(out, "anywhere in the case — the match is NOT scoped to a single computer")
	}

	if len(spec.MatchFields) > 0 {
		out = append(out, "sharing the same "+strings.Join(spec.MatchFields, " and ")+
			" — so the steps concern the same entity, not merely the same host")
	} else if algo != consts.AlgoLineage {
		out = append(out, "and NOTHING MORE — the steps need not involve the same account, session or process")
	}

	if within, total := spec.Window(); within > 0 || total > 0 {
		parts := make([]string, 0, 2)
		if within > 0 {
			parts = append(parts, "consecutive steps within "+within.String())
		}
		if total > 0 {
			parts = append(parts, "first to last within "+total.String())
		}
		out = append(out, strings.Join(parts, ", "))
	}
	return out
}

// --- shared ---

// source is one thing to check: its bytes, a name to report it under, and the path to write back
// to (empty for stdin, which cannot be rewritten in place).
type source struct {
	name string
	path string
	data []byte
}

// gather resolves paths into sources, walking directories for .json files. With no paths it reads
// stdin, so the tool composes with a pipe.
func gather(paths []string, stdin io.Reader, stderr io.Writer) ([]source, int) {
	if len(paths) == 0 {
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "reading stdin: %v\n", err)
			return nil, exitUsage
		}
		return []source{{name: "<stdin>", data: data}}, exitOK
	}

	var out []source
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", p, err)
			return nil, exitUsage
		}
		if !info.IsDir() {
			data, err := os.ReadFile(p)
			if err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", p, err)
				return nil, exitUsage
			}
			out = append(out, source{name: p, path: p, data: data})
			continue
		}

		var found []string
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".json") {
				return nil
			}
			found = append(found, path)
			return nil
		})
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", p, err)
			return nil, exitUsage
		}
		// Sorted, so a run over a directory reports in the same order every time — which is what
		// makes a CI log diffable between runs.
		sort.Strings(found)
		for _, path := range found {
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return nil, exitUsage
			}
			out = append(out, source{name: path, path: path, data: data})
		}
	}
	if len(out) == 0 {
		fmt.Fprintln(stderr, "no .json files found in the given paths")
		return nil, exitUsage
	}
	return out, exitOK
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// wrap breaks text at word boundaries so a long description reads as a paragraph in a terminal
// rather than as one line that scrolls off.
func wrap(text string, width int, indent string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	line := 0
	for i, w := range words {
		if i > 0 {
			if line+1+len(w) > width {
				b.WriteString("\n" + indent)
				line = 0
			} else {
				b.WriteString(" ")
				line++
			}
		}
		b.WriteString(w)
		line += len(w)
	}
	return b.String()
}
