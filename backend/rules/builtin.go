package rules

import (
	"embed"
	"fmt"
	"path"
	"sort"

	"rohy/backend/consts"
)

// builtinFS carries the curated default correlation rules (P4), compiled into the binary
// so a fresh install has a working library with no files to ship alongside it. The path is
// a literal because //go:embed cannot reference a constant; it mirrors consts.RuleBuiltinDir.
//
//go:embed builtin/*.json
var builtinFS embed.FS

// Builtins parses the embedded default rules, returning them with the builtin source tag
// and (defensively) any that failed to parse. A broken builtin is an authoring bug rather
// than a user problem, but it is reported the same way so it can never silently vanish.
// The enabled flag is left false here; the registry stamps the persisted user preference.
func Builtins() ([]*Rule, []LoadError) {
	entries, err := builtinFS.ReadDir(consts.RuleBuiltinDir)
	if err != nil {
		return nil, []LoadError{{Path: consts.RuleBuiltinDir, Message: err.Error()}}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // deterministic order → deterministic duplicate resolution

	out := make([]*Rule, 0, len(names))
	var errs []LoadError
	seen := map[string]string{} // id → file that first claimed it

	for _, name := range names {
		p := path.Join(consts.RuleBuiltinDir, name)
		data, err := builtinFS.ReadFile(p)
		if err != nil {
			errs = append(errs, LoadError{Path: p, Message: err.Error()})
			continue
		}
		spec, err := Parse(data)
		if err != nil {
			errs = append(errs, LoadError{Path: p, Message: err.Error()})
			continue
		}
		id := slug(spec.Name)
		if first, dup := seen[id]; dup {
			errs = append(errs, LoadError{Path: p, Message: fmt.Sprintf(consts.MsgRuleDuplicateName, spec.Name, first)})
			continue
		}
		seen[id] = name
		// Path stays empty: a builtin has no on-disk file, which is also what marks it
		// undeletable in the rules UI (P5). File records which embedded file it came from,
		// so the inspector can read its source back (P19).
		out = append(out, toRule(spec, consts.RuleSourceBuiltin, "", name, false))
	}
	return out, errs
}
