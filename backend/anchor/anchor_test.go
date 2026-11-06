package anchor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
)

var (
	captured = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	now      = captured.Add(time.Hour)
	first    = Target{Seq: 1, Digest: strings.Repeat("ab", 32), CapturedAt: captured}
)

func TestAMissingFileIsNoReceiptsAndWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "roots")
	s := Open(dir)
	if s.Err() != nil || len(s.All()) != 0 {
		t.Fatalf("%v %v", s.Err(), s.All())
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("opening created the folder: a read-only session must leave the case as it found it")
	}
}

func TestAReceiptIsRecordedAndReadBack(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	r, err := s.Add(first, "  TSA serial 0x1F  ", new(captured.Add(time.Minute)), 4242, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ref != "TSA serial 0x1F" || r.Seq != 1 || r.Digest != first.Digest || r.RecordedBy != 4242 || !r.RecordedAt.Equal(now) {
		t.Fatalf("%+v", r)
	}
	again := Open(dir)
	if again.Err() != nil {
		t.Fatal(again.Err())
	}
	got := again.For(1)
	if len(got) != 1 || got[0].Ref != r.Ref || got[0].PublishedAt == nil || !got[0].PublishedAt.Equal(*r.PublishedAt) {
		t.Fatalf("%+v", got)
	}
	if len(again.For(2)) != 0 {
		t.Fatal("receipts for another checkpoint")
	}
}

// A checkpoint can be published to more than one place; the same place twice is a mistake.
func TestSeveralReceiptsPerCheckpointButNotTheSameOneTwice(t *testing.T) {
	s := Open(t.TempDir())
	if _, err := s.Add(first, "TSA serial 1", nil, 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(first, "letter to counsel, 2026-11-06", nil, 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(first, "TSA serial 1", nil, 1, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate: %v", err)
	}
	if len(s.For(1)) != 2 {
		t.Fatalf("%+v", s.All())
	}
}

func TestAReferenceMustBePlainText(t *testing.T) {
	s := Open(t.TempDir())
	for name, ref := range map[string]string{
		"empty":       "   ",
		"too long":    strings.Repeat("x", consts.ReceiptRefMax+1),
		"control":     "serial\n42",
		"bidi":        "serial ‮24",
		"zero width":  "serial​42",
		"not utf8":    "serial \xff",
		"line sep":    "serial  42",
		"only spaces": "\t ",
	} {
		if _, err := s.Add(first, ref, nil, 1, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.Add(first, strings.Repeat("é", consts.ReceiptRefMax), nil, 1, now); err != nil {
		t.Errorf("at the limit, in runes: %v", err)
	}
}

// Typo guards: a receipt timed after now, or well before its checkpoint existed, is refused.
func TestAReceiptsTimeIsGuardedAgainstTypos(t *testing.T) {
	s := Open(t.TempDir())
	slack := consts.ReceiptTimeSlack
	for name, when := range map[string]time.Time{
		"future": now.Add(slack + time.Minute),
		"before": captured.Add(-slack - time.Minute),
	} {
		if _, err := s.Add(first, name, new(when), 1, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, when := range map[string]time.Time{
		"a clock a little ahead":  now.Add(slack - time.Minute),
		"a clock a little behind": captured.Add(-slack + time.Minute),
	} {
		if _, err := s.Add(first, name, new(when), 1, now); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheTargetMustBeACheckpoint(t *testing.T) {
	s := Open(t.TempDir())
	for name, k := range map[string]Target{
		"no seq":       {Digest: first.Digest, CapturedAt: captured},
		"short digest": {Seq: 1, Digest: "ab", CapturedAt: captured},
		"upper hex":    {Seq: 1, Digest: strings.ToUpper(first.Digest), CapturedAt: captured},
	} {
		if _, err := s.Add(k, "ref", nil, 1, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheCaseRecordsAtMostSoManyReceipts(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	s.doc.Receipts = make([]Receipt, consts.ReceiptsMax)
	if _, err := s.Add(first, "one too many", nil, 1, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
}

// 🔒 A file rohy cannot use is left exactly as it is, and nothing is recorded over it.
func TestAnUnusableFileIsLeftInPlaceAndRefusesWrites(t *testing.T) {
	for name, body := range map[string]string{
		"corrupt":       `{"receipts_version":1,"receipts":[`,
		"newer":         `{"receipts_version":2,"receipts":[]}`,
		"unknown field": `{"receipts_version":1,"receipts":[],"extra":1}`,
		"bad digest":    `{"receipts_version":1,"receipts":[{"seq":1,"digest":"zz","ref":"r","recorded_at":"2026-11-06T10:00:00Z","recorded_by":1}]}`,
		"seq 0":         `{"receipts_version":1,"receipts":[{"seq":0,"digest":"` + first.Digest + `","ref":"r","recorded_at":"2026-11-06T10:00:00Z","recorded_by":1}]}`,
		"bad ref":       `{"receipts_version":1,"receipts":[{"seq":1,"digest":"` + first.Digest + `","ref":"a\nb","recorded_at":"2026-11-06T10:00:00Z","recorded_by":1}]}`,
		"oversize":      `{"receipts_version":1,"receipts":[]}` + strings.Repeat(" ", consts.ReceiptsMaxBytes),
		"trailing":      `{"receipts_version":1,"receipts":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, consts.ReceiptsFile)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			s := Open(dir)
			if s.Err() == nil {
				t.Fatal("accepted")
			}
			if name == "newer" && !strings.Contains(s.Err().Error(), "newer") {
				t.Errorf("%v", s.Err())
			}
			if _, err := s.Add(first, "ref", nil, 1, now); err == nil || errors.Is(err, ErrInvalid) {
				t.Fatalf("recorded into an unusable file: %v", err)
			}
			if b, _ := os.ReadFile(path); string(b) != body {
				t.Fatal("the file was changed")
			}
		})
	}
}

func TestAVersionOneDocumentIsRead(t *testing.T) {
	dir := t.TempDir()
	body := `{"receipts_version":1,"receipts":[{"seq":1,"digest":"` + first.Digest + `","ref":"r","recorded_at":"2026-11-06T10:00:00Z","recorded_by":7}]}`
	if err := os.WriteFile(filepath.Join(dir, consts.ReceiptsFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(dir)
	if s.Err() != nil || len(s.For(1)) != 1 || s.For(1)[0].RecordedBy != 7 {
		t.Fatalf("%v %+v", s.Err(), s.All())
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"receipts_version":1,"receipts":[{"seq":1,"digest":"` + first.Digest + `","ref":"r","recorded_at":"2026-11-06T10:00:00Z","recorded_by":7}]}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := decode(data)
		if err != nil {
			return
		}
		if len(d.Receipts) > consts.ReceiptsMax {
			t.Fatalf("%d receipts", len(d.Receipts))
		}
		for _, r := range d.Receipts {
			if r.Seq == 0 || !validDigest(r.Digest) {
				t.Fatalf("accepted %+v", r)
			}
			if _, err := validRef(r.Ref); err != nil {
				t.Fatalf("accepted ref %q", r.Ref)
			}
		}
	})
}
