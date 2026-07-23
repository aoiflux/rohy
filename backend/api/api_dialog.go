package api

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rohy/backend/consts"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// File/folder picker bindings. They open native dialogs (which need the app context)
// and return only .evtx paths, so the frontend never has to know how to filter — the
// backend is "smart enough" to ingest event logs only. The pure filtering/walking
// helpers below are unit-tested independently of the Wails runtime.

// PickEVTXFiles opens a multi-select file dialog filtered to .evtx and returns the
// chosen files. An empty result means the user cancelled.
func (a *EventsAPI) PickEVTXFiles() ([]string, error) {
	ctx, err := a.ctx()
	if err != nil {
		return nil, err
	}
	selected, err := runtime.OpenMultipleFilesDialog(ctx, runtime.OpenDialogOptions{
		Title: consts.DialogFilesTitle,
		Filters: []runtime.FileFilter{
			{DisplayName: consts.DialogEVTXFilterName, Pattern: consts.DialogEVTXFilterGlob},
		},
	})
	if err != nil {
		return nil, AsError(consts.ErrCodeIO, err)
	}
	return onlyIngestible(selected), nil
}

// PickEVTXFolder opens a folder dialog and returns every .evtx file found beneath the
// chosen directory (recursively). Call it repeatedly to add multiple folders. An empty
// result means the user cancelled or the folder held no logs.
func (a *EventsAPI) PickEVTXFolder() ([]string, error) {
	ctx, err := a.ctx()
	if err != nil {
		return nil, err
	}
	dir, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: consts.DialogFolderTitle})
	if err != nil {
		return nil, AsError(consts.ErrCodeIO, err)
	}
	if strings.TrimSpace(dir) == "" {
		return nil, nil // cancelled
	}
	files, err := collectIngestibleFiles(dir)
	if err != nil {
		return nil, AsError(consts.ErrCodeIO, err)
	}
	return files, nil
}

// ctx returns the app context captured at Startup, or an error if the app has not
// started yet (e.g. a binding called before OnStartup).
func (a *EventsAPI) ctx() (context.Context, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.appCtx == nil {
		return nil, AsError(consts.ErrCodeInternal, errors.New("application not started"))
	}
	return a.appCtx, nil
}

// TotalSize returns the combined byte size of the given files, so the UI can warn
// before ingesting an extremely large dataset (P2-L.5). Unreadable paths contribute 0.
func (a *EventsAPI) TotalSize(paths []string) int64 {
	return sumFileSizes(paths)
}

func sumFileSizes(paths []string) int64 {
	var total int64
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			total += fi.Size()
		}
	}
	return total
}

// isIngestible reports whether name is a source rohy can ingest: an .evtx binary log or a
// .db SQLite database carrying EVTX data (P17). Matching is case-insensitive. A .db is
// accepted here on extension alone — whether it actually holds a recognized EVTX schema is
// decided when it is opened, so the user gets a precise reason rather than a silent filter.
func isIngestible(name string) bool {
	ext := filepath.Ext(name)
	return strings.EqualFold(ext, consts.EVTXExt) || strings.EqualFold(ext, consts.DBExt)
}

// onlyIngestible keeps just the ingestible paths from a selection, sorted and de-duplicated.
func onlyIngestible(paths []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range paths {
		if isIngestible(p) {
			if _, dup := seen[p]; !dup {
				seen[p] = struct{}{}
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// collectIngestibleFiles walks root recursively and returns every ingestible file, sorted.
// Unreadable sub-entries are skipped rather than aborting the whole walk.
func collectIngestibleFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !d.IsDir() && isIngestible(d.Name()) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
