// Package durable replaces a file so that a crash or a power loss leaves the old contents or the
// new, never neither and never a torn mix.
//
// Most of rohy's sidecars only need the second half of that — temp file, then rename — and get it
// from os.Rename. The redaction journal and the copies a redaction scrubs need the first half too:
// a step recorded as done must not be undone by a power cut that the record survived.
package durable

import (
	"os"
	"path/filepath"
)

// TempSuffix marks a write in progress. A file named with it after a crash is a partial or stale
// copy of the file it was replacing, and holds whatever that file held.
const TempSuffix = ".tmp"

// WriteFile replaces path with data, durably: the data is synced before the rename, and the
// rename is synced before WriteFile returns. On failure the temporary file is removed and path is
// unchanged.
func WriteFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+TempSuffix+"*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return replaceDurably(dir, tmp.Name(), path)
}
