package graphene

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"rohy/backend/consts"
)

// Torn tails: the unreadable end of an append-only file, kept beside it rather than lost.
//
// The engine reads each of its append-only files in whole records, up to the first thing that is not
// one, and a writable open cuts every file back to that point before it appends anything. That is the
// right repair for a crash, and the engine records each cut in the audit log as a digest, an offset
// and a length. It keeps nothing else, and a file damaged in the middle loses its unreadable
// remainder that way, which in a case file is evidence of the damage.
//
// 🔒 So rohy keeps the bytes. Before a writable open, every tail the open will cut is copied beside
// its file as <file>.torn-<UTC>. Where rohy can frame a file itself (the audit log and the
// checkpoint chain), it moves the tail aside before the open, and the engine then finds nothing to
// cut. Where it cannot, it saves a copy of the file, lets the engine decide, and copies aside
// exactly what the engine removed: everything past the file's length after the open.
//
// A read-only open cuts nothing, and none of this runs for one.

// KeptTail is one tail kept aside.
type KeptTail struct {
	File  string `json:"file"`  // the engine file it was cut from, by base name
	Bytes int64  `json:"bytes"` // how many bytes were kept
	Aside string `json:"aside"` // the copy's path
}

func (k KeptTail) String() string {
	return fmt.Sprintf(consts.MsgTornTailKept, k.File, k.Bytes, filepath.Base(k.Aside))
}

// tailGuard carries what a writable open needs from before it to after it.
type tailGuard struct {
	dir   string
	now   time.Time
	kept  []KeptTail
	tmp   string // holds the saved copies; removed by discard
	saved []savedFile
}

type savedFile struct {
	name string // base name in dir
	size int64  // its length before the open
	copy string // path of the saved copy
}

// saveBuffer is the copy buffer for a saved file: large, because the log can be tens of megabytes
// and the copy is syscall-bound at io.Copy's default.
const saveBuffer = 1 << 20

// guardTornTails prepares dir for a writable open: tails rohy can frame are kept aside now, and the
// files only the engine can frame are saved for settle to compare against.
func guardTornTails(dir string, now time.Time) (*tailGuard, error) {
	g := &tailGuard{dir: dir, now: now}
	aside, n, err := KeepAuditTailAside(dir, now)
	if err != nil {
		return nil, fmt.Errorf("keeping the audit log's torn tail aside: %w", err)
	}
	if n > 0 {
		g.kept = append(g.kept, KeptTail{File: consts.AuditFile, Bytes: n, Aside: aside})
	}
	aside, n, err = keepCheckpointTailAside(dir, now)
	if err != nil {
		return nil, fmt.Errorf("keeping the checkpoint chain's torn tail aside: %w", err)
	}
	if n > 0 {
		g.kept = append(g.kept, KeptTail{File: consts.CheckpointFile, Bytes: n, Aside: aside})
	}

	// The log is saved whole, by one sequential copy, rather than walked to find its tail first:
	// disk.InspectWAL reads record by record and costs about ten times the copy (PERFORMANCE.md §10).
	// A case closed cleanly was sealed, so its log is near empty and the copy costs nothing; a large
	// log at open is the crash that left a tail to keep.
	for _, name := range []string{consts.RedactionLedgerFile, consts.GrantLedgerFile, consts.WALFile} {
		if err := g.save(name); err != nil {
			g.discard()
			return nil, fmt.Errorf("saving %s before the open: %w", name, err)
		}
	}
	return g, nil
}

// save copies dir/name into the guard's temporary directory. A missing or empty file has no tail.
func (g *tailGuard) save(name string) error {
	src, err := os.Open(filepath.Join(g.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil || fi.Size() == 0 {
		return err
	}
	if g.tmp == "" {
		if g.tmp, err = os.MkdirTemp("", "rohy-open-"); err != nil {
			return err
		}
	}
	path := filepath.Join(g.tmp, name)
	dst, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.CopyBuffer(dst, src, make([]byte, saveBuffer)); err != nil {
		_ = dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	g.saved = append(g.saved, savedFile{name: name, size: fi.Size(), copy: path})
	return nil
}

// settle runs after the open: whatever the engine cut from a saved file is copied aside from the
// saved copy. It returns every tail kept for this open, before it and after it.
//
// On error the saved copies are left where they are and the error names them, so a failure to keep
// a tail never loses it.
func (g *tailGuard) settle() ([]KeptTail, error) {
	for _, f := range g.saved {
		path := filepath.Join(g.dir, f.name)
		fi, err := os.Stat(path)
		if err != nil {
			return g.kept, fmt.Errorf(consts.MsgTornTailUnsaved, f.name, g.tmp, err)
		}
		if fi.Size() >= f.size {
			continue
		}
		aside, err := copyTailAside(path, f.copy, fi.Size(), f.size, g.now)
		if err != nil {
			return g.kept, fmt.Errorf(consts.MsgTornTailUnsaved, f.name, g.tmp, err)
		}
		g.kept = append(g.kept, KeptTail{File: f.name, Bytes: f.size - fi.Size(), Aside: aside})
	}
	g.discard()
	return g.kept, nil
}

// discard removes the saved copies.
func (g *tailGuard) discard() {
	if g.tmp != "" {
		_ = os.RemoveAll(g.tmp)
		g.tmp = ""
	}
}

// copyTailAside copies bytes [from, to) of the saved copy beside the file at path, synced, under the
// name keepTailAside gives a tail. An existing copy is never replaced.
func copyTailAside(path, saved string, from, to int64, now time.Time) (string, error) {
	src, err := os.Open(saved)
	if err != nil {
		return "", err
	}
	defer src.Close()
	aside := tornTailName(path, now)
	dst, err := os.OpenFile(aside, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, io.NewSectionReader(src, from, to-from)); err != nil {
		_ = dst.Close()
		return "", err
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		return "", err
	}
	return aside, dst.Close()
}

// keepCheckpointTailAside moves a torn tail out of the checkpoint chain. Its records are a fixed
// size, so the tail is whatever does not make a whole one.
func keepCheckpointTailAside(dir string, now time.Time) (string, int64, error) {
	path := filepath.Join(dir, consts.CheckpointFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	end := len(data) - len(data)%checkpointRecordBytes
	if end == len(data) {
		return "", 0, nil
	}
	aside, err := keepTailAside(path, data, int64(end), now)
	return aside, int64(len(data) - end), err
}

// tornTailName is where the tail of the file at path is kept.
func tornTailName(path string, now time.Time) string {
	return path + ".torn-" + now.UTC().Format("20060102T150405.000000000Z")
}
