package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"rohy/backend/consts"
)

// ErrRuleNotFound is returned when enabling/disabling an unknown rule id.
var ErrRuleNotFound = fmt.Errorf("rule not found")

// Registry loads correlation rules from the user rules directory (P2; the embedded default
// library merges in at P4), validates them, tracks per-rule enabled state (persisted
// beside the rules), and exposes the valid rules plus the per-file errors for any that
// failed to load. It is safe for concurrent use.
type Registry struct {
	dir     string
	mu      sync.Mutex
	valid   []*Rule
	invalid []LoadError
	enabled map[string]bool // id → enabled override (absent = enabled by default)
}

// Open creates the rules directory if needed, loads the persisted enabled-state, and
// performs an initial scan.
func Open(dir string) (*Registry, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Registry{dir: dir, enabled: map[string]bool{}}
	r.loadState()
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Dir returns the user rules directory.
func (r *Registry) Dir() string { return r.dir }

// stateFile is the path of the persisted enabled-state sidecar.
func (r *Registry) stateFile() string {
	return filepath.Join(r.dir, consts.RuleStateFile)
}

// loadState reads the persisted enabled overrides (best-effort; a missing/corrupt file
// means "all defaults").
func (r *Registry) loadState() {
	data, err := os.ReadFile(r.stateFile())
	if err != nil {
		return
	}
	m := map[string]bool{}
	if json.Unmarshal(data, &m) == nil {
		r.enabled = m
	}
}

// persistState atomically writes the enabled overrides.
func (r *Registry) persistState() error {
	data, err := json.MarshalIndent(r.enabled, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.stateFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.stateFile())
}

// enabledFor reports whether a rule id is enabled (default true when no override).
func (r *Registry) enabledFor(id string) bool {
	if v, ok := r.enabled[id]; ok {
		return v
	}
	return true
}

// Reload rebuilds the rule set: the embedded default library loads first, then every
// *.json in the user rules directory (except the state sidecar) is parsed and validated.
// Valid rules populate the registry with their persisted enabled state; files that fail —
// including duplicate-name collisions — are recorded as per-file errors instead of aborting
// the load. A user rule whose name matches a builtin OVERRIDES it (one rule per name, user
// wins); two user files claiming the same name is an error.
func (r *Registry) Reload() error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || e.Name() == consts.RuleStateFile {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), consts.RuleFileExt) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // deterministic order → deterministic duplicate resolution

	// The embedded defaults form the base layer; user files are merged over them.
	byID := map[string]*Rule{}
	builtins, invalid := Builtins()
	for _, rule := range builtins {
		rule.Enabled = r.enabledFor(rule.ID)
		byID[rule.ID] = rule
	}

	seen := map[string]string{} // id → user filename that first claimed it

	for _, name := range names {
		path := filepath.Join(r.dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			invalid = append(invalid, LoadError{Path: path, Message: err.Error()})
			continue
		}
		spec, err := Parse(data)
		if err != nil {
			invalid = append(invalid, LoadError{Path: path, Message: err.Error()})
			continue
		}
		id := slug(spec.Name)
		if first, dup := seen[id]; dup {
			invalid = append(invalid, LoadError{Path: path, Message: fmt.Sprintf(consts.MsgRuleDuplicateName, spec.Name, first)})
			continue
		}
		seen[id] = name
		// Overwrites a same-named builtin by design (user rules win); collisions between
		// two user files are caught above.
		byID[id] = toRule(spec, consts.RuleSourceUser, path, name, r.enabledFor(id))
	}

	valid := make([]*Rule, 0, len(byID))
	for _, rule := range byID {
		valid = append(valid, rule)
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Name < valid[j].Name })

	r.mu.Lock()
	r.valid = valid
	r.invalid = invalid
	r.mu.Unlock()
	return nil
}

// List returns the valid rules (copies), sorted by name.
func (r *Registry) List() []*Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Rule, len(r.valid))
	for i, rule := range r.valid {
		c := *rule
		out[i] = &c
	}
	return out
}

// Enabled returns only the enabled valid rules (copies) — the set the auto-graphing
// workflow (P6) actually runs.
func (r *Registry) Enabled() []*Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Rule
	for _, rule := range r.valid {
		if rule.Enabled {
			c := *rule
			out = append(out, &c)
		}
	}
	return out
}

// Invalids returns the per-file load errors (copies).
func (r *Registry) Invalids() []LoadError {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LoadError, len(r.invalid))
	copy(out, r.invalid)
	return out
}

// SetEnabled toggles a rule by id and persists the choice. Unknown ids error.
func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found *Rule
	for _, rule := range r.valid {
		if rule.ID == id {
			found = rule
			break
		}
	}
	if found == nil {
		return ErrRuleNotFound
	}
	found.Enabled = enabled
	r.enabled[id] = enabled
	return r.persistState()
}
