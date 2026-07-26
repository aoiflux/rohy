package api

import (
	"context"
	"errors"
	"strings"
	"sync"

	"rohy/backend/consts"
	"rohy/backend/rules"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// RulesAPI is the Wails binding over the correlation-rule registry (P2). It exposes the
// loaded rules and their per-file errors, and lets the frontend toggle a rule on/off or
// rescan the user rules directory. Rule parsing/validation and enabled-state persistence
// live entirely in the rules package; this struct only adapts them to the binding layer.
// Import/delete of user rules arrives at P5; the embedded default library at P4.
type RulesAPI struct {
	registry *rules.Registry
	mu       sync.Mutex
	appCtx   context.Context
}

// NewRulesAPI constructs the binding over an open registry.
func NewRulesAPI(registry *rules.Registry) *RulesAPI {
	return &RulesAPI{registry: registry}
}

// Startup captures the application context so the import bindings can open native
// dialogs. Wails injects this context and does not expose it to JS, so it stays out of
// the frontend's callable surface.
func (a *RulesAPI) Startup(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.appCtx = ctx
}

// ctx returns the app context captured at Startup, or an error if a binding is called
// before the app has started.
func (a *RulesAPI) ctx() (context.Context, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.appCtx == nil {
		return nil, AsError(consts.ErrCodeInternal, errors.New("application not started"))
	}
	return a.appCtx, nil
}

// RulesResult is the DTO returned to the frontend: the valid rules plus the per-file load
// errors, so the UI can list working rules and surface precise problems for broken ones in
// the same pass.
type RulesResult struct {
	Rules  []*rules.Rule     `json:"rules"`
	Errors []rules.LoadError `json:"errors"`
}

// ListRules returns the currently loaded rules and load errors.
func (a *RulesAPI) ListRules() RulesResult {
	return RulesResult{Rules: a.registry.List(), Errors: a.registry.Invalids()}
}

// SetRuleEnabled toggles a rule by id and persists the choice. An unknown id is a
// validation error surfaced to the frontend.
func (a *RulesAPI) SetRuleEnabled(id string, enabled bool) error {
	if err := a.registry.SetEnabled(id, enabled); err != nil {
		return AsError(consts.ErrCodeRule, err)
	}
	return nil
}

// ReloadRules rescans the user rules directory and returns the fresh result, so the user
// can drop a new rule file in and refresh without restarting.
func (a *RulesAPI) ReloadRules() (RulesResult, error) {
	if err := a.registry.Reload(); err != nil {
		return RulesResult{}, AsError(consts.ErrCodeRule, err)
	}
	return RulesResult{Rules: a.registry.List(), Errors: a.registry.Invalids()}, nil
}

// ImportRuleFiles opens a multi-select dialog filtered to rule files and imports the
// chosen ones. Valid rules are copied into the rules directory and become live; invalid
// ones are reported per file and left untouched. A cancelled dialog imports nothing.
func (a *RulesAPI) ImportRuleFiles() (rules.ImportResult, error) {
	ctx, err := a.ctx()
	if err != nil {
		return rules.ImportResult{}, err
	}
	selected, err := runtime.OpenMultipleFilesDialog(ctx, runtime.OpenDialogOptions{
		Title: consts.DialogRuleFilesTitle,
		Filters: []runtime.FileFilter{
			{DisplayName: consts.DialogRuleFilterName, Pattern: consts.DialogRuleFilterGlob},
		},
	})
	if err != nil {
		return rules.ImportResult{}, AsError(consts.ErrCodeIO, err)
	}
	if len(selected) == 0 {
		return rules.ImportResult{}, nil // cancelled
	}
	res, err := a.registry.Import(selected)
	if err != nil {
		return res, AsError(consts.ErrCodeRule, err)
	}
	return res, nil
}

// ImportRuleFolder opens a folder dialog and imports every rule file beneath it
// (recursively), with the same per-file validation as ImportRuleFiles.
func (a *RulesAPI) ImportRuleFolder() (rules.ImportResult, error) {
	ctx, err := a.ctx()
	if err != nil {
		return rules.ImportResult{}, err
	}
	dir, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: consts.DialogRuleFolderTitle})
	if err != nil {
		return rules.ImportResult{}, AsError(consts.ErrCodeIO, err)
	}
	if strings.TrimSpace(dir) == "" {
		return rules.ImportResult{}, nil // cancelled
	}
	res, err := a.registry.ImportFolder(dir)
	if err != nil {
		return res, AsError(consts.ErrCodeRule, err)
	}
	return res, nil
}

// DeleteRule removes an imported user rule. Built-in rules are protected — they can only
// be disabled — and an unknown id is reported as such.
func (a *RulesAPI) DeleteRule(id string) error {
	if err := a.registry.Delete(id); err != nil {
		return AsError(consts.ErrCodeRule, err)
	}
	return nil
}

// RulesDir returns the directory user rules live in, so the UI can tell the user where to
// drop files for a manual import.
func (a *RulesAPI) RulesDir() string {
	return a.registry.Dir()
}

// RuleSource returns a rule's file exactly as authored, for the rule inspector (P19). It is
// the raw file rather than a re-serialization of the parsed rule, so what the user sees is
// what is actually on disk — including any field this build does not interpret.
func (a *RulesAPI) RuleSource(id string) (rules.RuleSource, error) {
	src, err := a.registry.Source(id)
	if err != nil {
		return src, AsError(consts.ErrCodeRule, err)
	}
	return src, nil
}

// --- Rule editor (P26) ---
//
// The four bindings below are what let the application author a rule rather than only
// import one. They share a principle: the editor asks the backend the same questions the
// loader answers, instead of re-implementing the format in JavaScript. A frontend copy of
// the schema or the validator would drift, and the drift would surface as a rule that saves
// cleanly and is then missing from the library.

// RuleSchema returns the rule-format descriptor that drives both editor modes: field prose,
// allowed values, and bounds. It is served rather than duplicated in the frontend so the
// guided form's controls, the raw editor's completion list, and the client-side validator
// all move together when the format changes.
func (a *RulesAPI) RuleSchema() rules.Schema {
	return rules.Describe()
}

// ValidateRule reports whether candidate text would load, with each problem carrying a
// stable code and a position in the source so the editor can underline the offending token
// or highlight the offending control. editingID excludes a rule from its own name-collision
// check; pass "" for a new rule.
//
// It returns no error: an unparseable buffer is a normal state while typing, not a failed
// call, and surfacing it as a rejected promise would make the editor flicker error dialogs
// at someone mid-keystroke.
func (a *RulesAPI) ValidateRule(source string, editingID string) rules.ValidationReport {
	return a.registry.Validate(source, editingID)
}

// FormatRule pretty-prints or minifies rule text. It works on the text rather than on a
// parsed rule, so a field this build does not interpret survives the round trip.
func (a *RulesAPI) FormatRule(source string, minify bool) (string, error) {
	var (
		out []byte
		err error
	)
	if minify {
		out, err = rules.Minify([]byte(source))
	} else {
		out, err = rules.Pretty([]byte(source))
	}
	if err != nil {
		return "", AsError(consts.ErrCodeRule, err)
	}
	return string(out), nil
}

// ReadRuleFile returns the contents of a file in the rules directory by path, for the one
// case RuleSource cannot serve: a file that failed to load has no rule and therefore no id.
// It is what lets the load-errors panel offer to repair a broken file instead of only naming
// it. Paths outside the rules directory are refused.
func (a *RulesAPI) ReadRuleFile(path string) (string, error) {
	src, err := a.registry.ReadFile(path)
	if err != nil {
		return "", AsError(consts.ErrCodeIO, err)
	}
	return src, nil
}

// SaveRule creates or updates a user rule from the editor. Built-in rules are refused —
// they live in the binary — so the editor's path for varying one is to duplicate it under a
// new name, which arrives here as a create.
func (a *RulesAPI) SaveRule(req rules.SaveRequest) (rules.SaveResult, error) {
	res, err := a.registry.Save(req)
	if err != nil {
		return res, AsError(consts.ErrCodeRule, err)
	}
	return res, nil
}
