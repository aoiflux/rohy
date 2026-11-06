// Package anchor keeps what the analyst recorded about where each checkpoint's digest was published
// (plan §14.7, item 42).
//
// rohy publishes nothing itself. It hands the analyst a checkpoint's digest; the analyst takes it
// somewhere the case's rewriter cannot reach — a timestamping authority, opposing counsel, an
// organisational log, a printed sheet in a safe — and records here what that place gave back.
//
// 🔒 A receipt is the case's own record. Whoever can rewrite the case can write one, so it is never a
// witness: it is the pointer a reader follows to the place that is. The check that does not rest on
// the case is a digest the reader holds (custody.AnchorReport).
package anchor

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"rohy/backend/consts"
	"rohy/backend/durable"
	"rohy/backend/utils"
)

// ErrInvalid marks a receipt refused for what it says, as opposed to a file that could not be
// written.
var ErrInvalid = errors.New("invalid receipt")

// Receipt is one record of a checkpoint's digest reaching somewhere outside the case.
type Receipt struct {
	Seq    uint64 `json:"seq"`
	Digest string `json:"digest"`
	// Ref is what the anchor gave back: a token serial, a URL, a page number. Opaque to rohy.
	Ref string `json:"ref"`
	// PublishedAt is the anchor's time as the analyst read it, when they gave one.
	PublishedAt *time.Time `json:"published_at,omitempty"`
	RecordedAt  time.Time  `json:"recorded_at"`
	RecordedBy  uint64     `json:"recorded_by"`
}

// Target is the checkpoint a receipt is for. The caller has found it in a chain that checks.
type Target struct {
	Seq        uint64
	Digest     string
	CapturedAt time.Time
}

type document struct {
	Version  int       `json:"receipts_version"`
	Receipts []Receipt `json:"receipts"`
}

// Store is the receipts sidecar.
type Store struct {
	path string
	mu   sync.Mutex
	doc  document
	// err is why the file could not be used. While it is set the file is left as it is and every
	// write is refused: a receipt cannot be reconstructed, so nothing is written over one.
	err error
}

// Open reads the receipts in dir. It never writes: a folder or file that does not exist is no
// receipts, and is created at the first one.
func Open(dir string) *Store {
	s := &Store{path: filepath.Join(dir, consts.ReceiptsFile), doc: document{Version: consts.ReceiptsDocVersion}}
	data, err := readBounded(s.path, consts.ReceiptsMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return s
	}
	if err == nil {
		s.doc, err = decode(data)
	}
	if err != nil {
		s.doc = document{Version: consts.ReceiptsDocVersion}
		s.err = fmt.Errorf(consts.MsgReceiptsUnusable, s.path, err)
	}
	return s
}

// Err is why the receipts could not be read, or nil.
func (s *Store) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// All returns every receipt, in the order recorded.
func (s *Store) All() []Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.doc.Receipts)
}

// For returns the receipts for checkpoint seq.
func (s *Store) For(seq uint64) []Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Receipt
	for _, r := range s.doc.Receipts {
		if r.Seq == seq {
			out = append(out, r)
		}
	}
	return out
}

// Add records a receipt for checkpoint k, durably. by is the analyst recording it.
func (s *Store) Add(k Target, ref string, publishedAt *time.Time, by uint64, now time.Time) (Receipt, error) {
	if k.Seq == 0 || !validDigest(k.Digest) {
		return Receipt{}, fmt.Errorf("%w: not a checkpoint", ErrInvalid)
	}
	ref, err := validRef(ref)
	if err != nil {
		return Receipt{}, err
	}
	r := Receipt{Seq: k.Seq, Digest: k.Digest, Ref: ref, RecordedAt: now.UTC(), RecordedBy: by}
	if publishedAt != nil {
		p := publishedAt.UTC()
		switch {
		case p.After(now.Add(consts.ReceiptTimeSlack)):
			return Receipt{}, fmt.Errorf("%w: %s", ErrInvalid, consts.MsgReceiptTimeFuture)
		case p.Before(k.CapturedAt.Add(-consts.ReceiptTimeSlack)):
			return Receipt{}, fmt.Errorf("%w: "+consts.MsgReceiptTimeBefore, ErrInvalid, k.Seq)
		}
		r.PublishedAt = &p
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return Receipt{}, s.err
	}
	if len(s.doc.Receipts) >= consts.ReceiptsMax {
		return Receipt{}, fmt.Errorf("%w: "+consts.MsgReceiptsFull, ErrInvalid, consts.ReceiptsMax)
	}
	for _, x := range s.doc.Receipts {
		if x.Seq == r.Seq && x.Ref == r.Ref {
			return Receipt{}, fmt.Errorf("%w: "+consts.MsgReceiptDuplicate, ErrInvalid, r.Seq)
		}
	}
	next := document{Version: consts.ReceiptsDocVersion, Receipts: append(slices.Clone(s.doc.Receipts), r)}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return Receipt{}, err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return Receipt{}, err
	}
	if err := durable.WriteFile(s.path, data); err != nil {
		return Receipt{}, err
	}
	s.doc = next
	return r, nil
}

func decode(data []byte) (document, error) {
	var d document
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return document{}, err
	}
	if dec.More() {
		return document{}, errors.New("trailing data")
	}
	if d.Version > consts.ReceiptsDocVersion {
		return document{}, fmt.Errorf(consts.MsgReceiptsNewer, d.Version, consts.ReceiptsDocVersion)
	}
	if d.Version != consts.ReceiptsDocVersion {
		return document{}, fmt.Errorf("format %d", d.Version)
	}
	if len(d.Receipts) > consts.ReceiptsMax {
		return document{}, fmt.Errorf(consts.MsgReceiptsFull, len(d.Receipts))
	}
	for i, r := range d.Receipts {
		if r.Seq == 0 || !validDigest(r.Digest) || r.RecordedAt.IsZero() {
			return document{}, fmt.Errorf("receipt %d does not name a checkpoint", i+1)
		}
		if ref, err := validRef(r.Ref); err != nil || ref != r.Ref {
			return document{}, fmt.Errorf("receipt %d: the reference is not plain text", i+1)
		}
	}
	return d, nil
}

// validRef trims ref and checks it is display text of a bounded length.
func validRef(ref string) (string, error) {
	out, ok := utils.DisplayName(ref, consts.ReceiptRefMax)
	if !ok {
		return "", fmt.Errorf("%w: "+consts.MsgReceiptRef, ErrInvalid, consts.ReceiptRefMax)
	}
	return out, nil
}

// validDigest reports whether s is one checkpoint digest: 32 bytes in lowercase hex.
func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && s == strings.ToLower(s)
}

func readBounded(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("larger than %d bytes", max)
	}
	return data, nil
}
