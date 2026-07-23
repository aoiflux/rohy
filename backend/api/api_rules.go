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
