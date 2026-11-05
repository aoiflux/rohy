package keyfile

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aoiflux/graphene/signing"

	"rohy/backend/consts"
)

var update = flag.Bool("update", false, "rewrite testdata/v1.json")

// fast keeps tests quick: the lowest parameters a file may carry. Production keys use
// defaultParams, exercised once in TestTheDefaultParametersRoundTrip.
var fast = params{time: 1, memoryKiB: consts.KeyArgonMinMemoryKiB, threads: 1}

const pass = "correct horse battery staple"

var when = time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)

type cases struct{ caseDir, outside string }

func folders(t *testing.T) cases {
	t.Helper()
	root := t.TempDir()
	c := cases{caseDir: filepath.Join(root, "work"), outside: filepath.Join(root, "keys")}
	for _, d := range []string{filepath.Join(c.caseDir, "rohy-data"), c.outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func newKey(t *testing.T, c cases, name string) (string, Info) {
	t.Helper()
	path := filepath.Join(c.outside, name)
	info, err := create(rand.Reader, path, c.caseDir, pass, "Ada", when, fast)
	if err != nil {
		t.Fatal(err)
	}
	return path, info
}

func TestAKeyRoundTripsAndSigns(t *testing.T) {
	c := folders(t)
	path, info := newKey(t, c, "ada.key")
	key, got, err := Unlock(path, c.caseDir, pass)
	if err != nil {
		t.Fatal(err)
	}
	if got != info || key.KeyID() != info.KeyID {
		t.Fatalf("unlocked %+v (key %d), created %+v", got, key.KeyID(), info)
	}
	if want := Fingerprint(key.Public()); got.Fingerprint != want {
		t.Fatalf("fingerprint %s, want %s", got.Fingerprint, want)
	}
	ring := signing.NewKeyring()
	if err := ring.Add(info.KeyID, key.Public()); err != nil {
		t.Fatal(err)
	}
	sig, _ := key.Sign([]byte("seal"))
	if err := ring.Verify(info.KeyID, []byte("seal"), sig); err != nil {
		t.Fatalf("a signature by the unlocked key does not verify under its public key: %v", err)
	}
	pub, _ := Inspect(path)
	if pub != info {
		t.Fatalf("inspect %+v, want %+v", pub, info)
	}
}

func TestTheDefaultParametersRoundTrip(t *testing.T) {
	c := folders(t)
	path := filepath.Join(c.outside, "default.key")
	start := time.Now()
	if _, err := Create(path, c.caseDir, pass, "Ada", when); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Unlock(path, c.caseDir, pass); err != nil {
		t.Fatal(err)
	}
	t.Logf("create + unlock at the default parameters: %v", time.Since(start))
	var d document
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.KDF.Time != consts.KeyArgonTime || d.KDF.MemoryKiB != consts.KeyArgonMemoryKiB || d.KDF.Threads != consts.KeyArgonThreads {
		t.Fatalf("written with %+v", d.KDF)
	}
}

func TestAWrongPassphraseIsRefusedAsUnlockFailed(t *testing.T) {
	c := folders(t)
	path, _ := newKey(t, c, "ada.key")
	for _, p := range []string{"correct horse battery stapl", "short", pass + " "} {
		if _, _, err := Unlock(path, c.caseDir, p); !errors.Is(err, ErrUnlock) {
			t.Errorf("passphrase %q: %v, want ErrUnlock", p, err)
		}
	}
}

// Every public field is bound into the seal: change one and the file no longer opens.
func TestASwappedFieldDoesNotOpen(t *testing.T) {
	c := folders(t)
	path, _ := newKey(t, c, "ada.key")
	orig, _ := os.ReadFile(path)
	for name, edit := range map[string]func(*document){
		"label":   func(d *document) { d.Label = "Mallory" },
		"created": func(d *document) { d.Created = d.Created.Add(time.Hour) },
		"time":    func(d *document) { d.KDF.Time = 2 },
		"memory":  func(d *document) { d.KDF.MemoryKiB = consts.KeyArgonMinMemoryKiB * 2 },
		"threads": func(d *document) { d.KDF.Threads = 2 },
		"salt":    func(d *document) { d.KDF.Salt = strings.Repeat("ab", consts.KeySaltLen) },
		"nonce":   func(d *document) { d.Cipher.Nonce = strings.Repeat("cd", consts.KeyNonceLen) },
	} {
		t.Run(name, func(t *testing.T) {
			var d document
			if err := json.Unmarshal(orig, &d); err != nil {
				t.Fatal(err)
			}
			edit(&d)
			b, _ := json.Marshal(d)
			p := filepath.Join(c.outside, name+".key")
			if err := os.WriteFile(p, b, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Unlock(p, c.caseDir, pass); !errors.Is(err, ErrUnlock) {
				t.Fatalf("edited %s: %v, want ErrUnlock", name, err)
			}
		})
	}
}

// A different public key is caught before any decryption: its key id no longer matches.
func TestASwappedPublicKeyIsNotAKey(t *testing.T) {
	c := folders(t)
	path, _ := newKey(t, c, "ada.key")
	_, other := newKey(t, c, "eve.key")
	var d document
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &d)
	d.PublicKey = other.PublicKey
	b, _ = json.Marshal(d)
	_ = os.WriteFile(path, b, 0o600)
	if _, _, err := Unlock(path, c.caseDir, pass); !errors.Is(err, ErrNotKey) {
		t.Fatalf("%v, want ErrNotKey", err)
	}
	// And with the id moved too, the seal still refuses it.
	d.KeyID = other.KeyID
	b, _ = json.Marshal(d)
	_ = os.WriteFile(path, b, 0o600)
	if _, _, err := Unlock(path, c.caseDir, pass); !errors.Is(err, ErrUnlock) {
		t.Fatalf("%v, want ErrUnlock", err)
	}
}

func TestAKeyFileIsNeverReplaced(t *testing.T) {
	c := folders(t)
	path, first := newKey(t, c, "ada.key")
	before, _ := os.ReadFile(path)
	_, err := create(rand.Reader, path, c.caseDir, pass, "Ada", when, fast)
	if err == nil || !strings.Contains(err.Error(), "never replaces") {
		t.Fatalf("second create: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("the existing key file changed")
	}
	if got, _ := Inspect(path); got != first {
		t.Fatalf("now %+v", got)
	}
}

func TestPassphraseAndLabelBounds(t *testing.T) {
	c := folders(t)
	for _, tc := range []struct {
		pass, label string
	}{
		{strings.Repeat("a", consts.KeyMinPassphrase-1), "Ada"},
		{strings.Repeat("a", consts.KeyMaxPassphrase+1), "Ada"},
		{"not\xffvalid passphrase", "Ada"},
		{pass, ""},
		{pass, "line\nbreak"},
		{pass, strings.Repeat("a", consts.KeyMaxLabel+1)},
	} {
		path := filepath.Join(c.outside, "bounds.key")
		if _, err := create(rand.Reader, path, c.caseDir, tc.pass, tc.label, when, fast); err == nil {
			t.Errorf("accepted passphrase of %d bytes, label %q", len(tc.pass), tc.label)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("a refused create left a file: %v", err)
		}
	}
	if err := ValidPassphrase(strings.Repeat("a", consts.KeyMinPassphrase)); err != nil {
		t.Errorf("minimum passphrase refused: %v", err)
	}
}

// 🔒 Parameters read from a file are untrusted: out of bounds is "not a key", before any work.
func TestUntrustedParametersAreBounded(t *testing.T) {
	c := folders(t)
	path, _ := newKey(t, c, "ada.key")
	orig, _ := os.ReadFile(path)
	for name, edit := range map[string]func(*document){
		"memory huge":   func(d *document) { d.KDF.MemoryKiB = 1 << 30 },
		"memory small":  func(d *document) { d.KDF.MemoryKiB = 1 },
		"time zero":     func(d *document) { d.KDF.Time = 0 },
		"time huge":     func(d *document) { d.KDF.Time = 1 << 20 },
		"threads zero":  func(d *document) { d.KDF.Threads = 0 },
		"threads many":  func(d *document) { d.KDF.Threads = 200 },
		"salt short":    func(d *document) { d.KDF.Salt = "abcd" },
		"salt not hex":  func(d *document) { d.KDF.Salt = strings.Repeat("zz", 16) },
		"nonce long":    func(d *document) { d.Cipher.Nonce = strings.Repeat("ab", 16) },
		"cipher short":  func(d *document) { d.Cipher.Ciphertext = "abcd" },
		"kdf name":      func(d *document) { d.KDF.Name = "pbkdf2" },
		"cipher name":   func(d *document) { d.Cipher.Name = "chacha20" },
		"format":        func(d *document) { d.Format = "ssh-key" },
		"version old":   func(d *document) { d.Version = 0 },
		"no created":    func(d *document) { d.Created = time.Time{} },
		"invisible lbl": func(d *document) { d.Label = "Ada" + string(rune(0x200B)) },
		"key id":        func(d *document) { d.KeyID++ },
	} {
		var d document
		_ = json.Unmarshal(orig, &d)
		edit(&d)
		b, _ := json.Marshal(d)
		if _, err := decode(b); !errors.Is(err, ErrNotKey) {
			t.Errorf("%s: %v, want ErrNotKey", name, err)
		}
	}
	for name, b := range map[string][]byte{
		"unknown field": bytes.Replace(orig, []byte(`"label"`), []byte(`"extra": 1, "label"`), 1),
		"trailing":      append(append([]byte{}, orig...), []byte(`{}`)...),
		"too large":     append(orig, bytes.Repeat([]byte(" "), consts.KeyFileMaxSize)...),
		"empty":         nil,
		"not json":      []byte("-----BEGIN OPENSSH PRIVATE KEY-----"),
	} {
		if _, err := decode(b); !errors.Is(err, ErrNotKey) {
			t.Errorf("%s: %v, want ErrNotKey", name, err)
		}
	}
	var d document
	_ = json.Unmarshal(orig, &d)
	d.Version = consts.KeyFileVersion + 1
	b, _ := json.Marshal(d)
	_ = os.WriteFile(path, b, 0o600)
	if _, err := Inspect(path); err == nil || !strings.Contains(err.Error(), "newer rohy") {
		t.Errorf("newer version: %v", err)
	}
}

// 🔒 A key inside the case's working folder is refused, however the path names it.
func TestAKeyInsideTheCaseIsRefused(t *testing.T) {
	c := folders(t)
	paths := map[string]string{
		"in the working folder": filepath.Join(c.caseDir, "ada.key"),
		"in rohy-data":          filepath.Join(c.caseDir, "rohy-data", "ada.key"),
		"via dot-dot":           filepath.Join(c.outside, "..", "work", "ada.key"),
		"the folder itself":     c.caseDir,
	}
	if runtime.GOOS == "windows" {
		paths["in another letter case"] = filepath.Join(strings.ToUpper(c.caseDir), "ADA.KEY")
		link := filepath.Join(c.outside, "junction")
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, c.caseDir).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J: %v: %s", err, out)
		}
		paths["through a junction"] = filepath.Join(link, "ada.key")
	} else {
		link := filepath.Join(c.outside, "link")
		if err := os.Symlink(c.caseDir, link); err != nil {
			t.Fatal(err)
		}
		paths["through a symlink"] = filepath.Join(link, "ada.key")
	}
	for name, p := range paths {
		_, err := create(rand.Reader, p, c.caseDir, pass, "Ada", when, fast)
		if !errors.Is(err, ErrInsideCase) {
			t.Errorf("%s (%s): %v, want ErrInsideCase", name, p, err)
		}
	}

	// A key created outside and moved in is refused at unlock.
	path, _ := newKey(t, c, "ada.key")
	moved := filepath.Join(c.caseDir, "ada.key")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Unlock(moved, c.caseDir, pass); !errors.Is(err, ErrInsideCase) {
		t.Fatalf("unlock of a key moved inside: %v", err)
	}
	if inside, err := InsideFolder(filepath.Join(c.outside, "ok.key"), c.caseDir); err != nil || inside {
		t.Fatalf("a sibling folder reads inside=%v err=%v", inside, err)
	}
}

// fixedStream is a deterministic randomness source for the golden vector.
type fixedStream struct{ n byte }

func (f *fixedStream) Read(p []byte) (int, error) {
	for i := range p {
		f.n++
		p[i] = f.n
	}
	return len(p), nil
}

// TestTheV1FormatStillOpens pins the format: a key written by this build must open in every later
// one. The file in testdata was written by create over a fixed byte stream; any change to the AAD,
// the KDF or the cipher fails here before it strands an analyst's key.
func TestTheV1FormatStillOpens(t *testing.T) {
	golden := filepath.Join("testdata", "v1.json")
	c := folders(t)
	if *update {
		p := filepath.Join(c.outside, "golden.key")
		if _, err := create(&fixedStream{}, p, c.caseDir, pass, "Golden", when, fast); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run once with -update to write it)", err)
	}
	p := filepath.Join(c.outside, "again.key")
	if _, err := create(&fixedStream{}, p, c.caseDir, pass, "Golden", when, fast); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))) {
		t.Fatalf("this build writes a different v1 file:\n%s\nwant:\n%s", got, want)
	}
	copyTo := filepath.Join(c.outside, "golden.key")
	_ = os.WriteFile(copyTo, want, 0o600)
	key, info, err := Unlock(copyTo, c.caseDir, pass)
	if err != nil {
		t.Fatalf("the v1 golden key does not open: %v", err)
	}
	seed := make([]byte, ed25519.SeedSize)
	_, _ = (&fixedStream{}).Read(seed)
	if !key.Public().Equal(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)) || info.Label != "Golden" {
		t.Fatalf("opened %+v", info)
	}
}

// FuzzDecode: no input may panic the decoder, and anything it accepts is internally consistent.
func FuzzDecode(f *testing.F) {
	if b, err := os.ReadFile(filepath.Join("testdata", "v1.json")); err == nil {
		f.Add(b)
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"format":"rohy-signing-key","version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := decode(data)
		if err != nil {
			return
		}
		id, err := KeyIDOf(p.pub)
		if err != nil || id != p.doc.KeyID {
			t.Fatalf("accepted key id %d for a key whose id is %d (%v)", p.doc.KeyID, id, err)
		}
		k := p.doc.KDF
		if k.MemoryKiB > consts.KeyArgonMaxMemoryKiB || k.Time > consts.KeyArgonMaxTime ||
			k.Threads > consts.KeyArgonMaxThreads || len(p.ciphertext) != sealedSeedLen ||
			len(p.nonce) != consts.KeyNonceLen {
			t.Fatalf("accepted out-of-bounds parameters %+v", k)
		}
	})
}

// BenchmarkUnlockDefault is the wait an analyst sees when unlocking a key (PERFORMANCE.md).
func BenchmarkUnlockDefault(b *testing.B) {
	dir := b.TempDir()
	caseDir := filepath.Join(dir, "work")
	_ = os.MkdirAll(caseDir, 0o755)
	path := filepath.Join(dir, "k.key")
	if _, err := Create(path, caseDir, pass, "Bench", when); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := Unlock(path, caseDir, pass); err != nil {
			b.Fatal(err)
		}
	}
}
