//go:build windows

package durable

import "golang.org/x/sys/windows"

// replaceDurably renames tmp over file and returns only once the rename is on disk.
//
// os.Rename on Windows is MoveFileEx with MOVEFILE_REPLACE_EXISTING alone, which may return before
// the new directory entry is written. MOVEFILE_WRITE_THROUGH waits for it; Windows offers no
// directory sync to do the same afterwards.
func replaceDurably(_, tmp, file string) error {
	from, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// syncDir is a no-op: Windows offers no directory sync. NTFS journals the new directory entry, and
// the file's own data is flushed before this is reached.
func syncDir(string) error { return nil }
