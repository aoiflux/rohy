package keyfile

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"rohy/backend/consts"
	"rohy/backend/utils"
)

// document is the key file as stored. Everything but the ciphertext is public: a key file can be
// inspected, matched to a case and shown without its passphrase.
type document struct {
	Format    string    `json:"format"`
	Version   int       `json:"version"`
	KeyID     uint64    `json:"key_id"`
	PublicKey string    `json:"public_key"`
	Created   time.Time `json:"created"`
	Label     string    `json:"label"`
	KDF       kdfBlock  `json:"kdf"`
	Cipher    aeadBlock `json:"cipher"`
}

type kdfBlock struct {
	Name      string `json:"name"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
	Salt      string `json:"salt"`
}

type aeadBlock struct {
	Name       string `json:"name"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// parsed is a document whose every field has been checked and decoded.
type parsed struct {
	doc        document
	pub        ed25519.PublicKey
	salt       []byte
	nonce      []byte
	ciphertext []byte
}

// ErrNotKey matches every refusal of a file that is not a key file this build can read, so callers
// can tell it from an IO error or a wrong passphrase without matching text.
var ErrNotKey = errors.New("not a rohy signing key")

// NotKeyError says why a file is not a key file. Path is set once the file is named.
type NotKeyError struct {
	Path   string
	Detail string
}

func (e *NotKeyError) Error() string {
	if e.Path == "" {
		return ErrNotKey.Error() + ": " + e.Detail
	}
	return fmt.Sprintf(consts.MsgKeyFileNotKey, e.Path, e.Detail)
}

func (e *NotKeyError) Is(target error) bool { return target == ErrNotKey }

func notKey(format string, args ...any) error {
	return &NotKeyError{Detail: fmt.Sprintf(format, args...)}
}

// decode parses and checks a key file without touching the ciphertext. Every field is untrusted:
// the KDF parameters are bounded so a crafted file cannot make an unlock allocate or run without
// limit, and the key id must be the one the public key implies.
func decode(data []byte) (parsed, error) {
	var p parsed
	if len(data) > consts.KeyFileMaxSize {
		return p, notKey("larger than %d bytes", consts.KeyFileMaxSize)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p.doc); err != nil {
		return p, notKey("%v", err)
	}
	if dec.More() {
		return p, notKey("trailing data after the key")
	}
	d := p.doc
	if d.Format != consts.KeyFileFormat {
		return p, notKey("format %q", d.Format)
	}
	if d.Version > consts.KeyFileVersion {
		return p, &NewerError{Version: d.Version}
	}
	if d.Version != consts.KeyFileVersion {
		return p, notKey("version %d", d.Version)
	}

	pub, err := hex.DecodeString(d.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return p, notKey("the public key is not %d bytes of hex", ed25519.PublicKeySize)
	}
	p.pub = ed25519.PublicKey(pub)
	if id, err := KeyIDOf(p.pub); err != nil || id != d.KeyID {
		return p, notKey("key id %d does not belong to its public key", d.KeyID)
	}
	if d.Created.IsZero() {
		return p, notKey("no creation time")
	}
	if label, ok := utils.DisplayName(d.Label, consts.KeyMaxLabel); !ok || label != d.Label {
		return p, notKey("the label is not a display name")
	}

	k := d.KDF
	if k.Name != consts.KeyKDFArgon2id {
		return p, notKey("key derivation %q", k.Name)
	}
	if k.Time < consts.KeyArgonMinTime || k.Time > consts.KeyArgonMaxTime ||
		k.MemoryKiB < consts.KeyArgonMinMemoryKiB || k.MemoryKiB > consts.KeyArgonMaxMemoryKiB ||
		k.Threads < 1 || k.Threads > consts.KeyArgonMaxThreads {
		return p, notKey("key derivation parameters out of bounds (time %d, memory %d KiB, threads %d)",
			k.Time, k.MemoryKiB, k.Threads)
	}
	if p.salt, err = hex.DecodeString(k.Salt); err != nil ||
		len(p.salt) < consts.KeyMinSaltLen || len(p.salt) > consts.KeyMaxSaltLen {
		return p, notKey("the salt is not %d to %d bytes of hex", consts.KeyMinSaltLen, consts.KeyMaxSaltLen)
	}

	c := d.Cipher
	if c.Name != consts.KeyCipherAEAD {
		return p, notKey("cipher %q", c.Name)
	}
	if p.nonce, err = hex.DecodeString(c.Nonce); err != nil || len(p.nonce) != consts.KeyNonceLen {
		return p, notKey("the nonce is not %d bytes of hex", consts.KeyNonceLen)
	}
	if p.ciphertext, err = hex.DecodeString(c.Ciphertext); err != nil || len(p.ciphertext) != sealedSeedLen {
		return p, notKey("the sealed key is not %d bytes of hex", sealedSeedLen)
	}
	return p, nil
}

// sealedSeedLen is the seed plus the GCM tag.
const sealedSeedLen = ed25519.SeedSize + 16

// aadDomain separates this binding from anything else rohy authenticates.
const aadDomain = "rohy-signing-key/aad\x00"

// aad binds every public field to the ciphertext, so a file whose parameters, public key or label
// were swapped does not decrypt.
//
// 🔒 A fixed binary encoding, never re-marshalled JSON: a later build that orders or formats JSON
// differently must still open every key written before it. TestTheV1FormatStillOpens pins it.
func aad(d document, salt, nonce []byte) []byte {
	var b bytes.Buffer
	b.WriteString(aadDomain)
	put := func(v any) { _ = binary.Write(&b, binary.BigEndian, v) }
	str := func(s string) {
		put(uint32(len(s)))
		b.WriteString(s)
	}
	str(d.Format)
	put(uint32(d.Version))
	put(d.KeyID)
	str(d.PublicKey)
	put(d.Created.UnixNano())
	str(d.Label)
	str(d.KDF.Name)
	put(d.KDF.Time)
	put(d.KDF.MemoryKiB)
	put(d.KDF.Threads)
	put(uint32(len(salt)))
	b.Write(salt)
	str(d.Cipher.Name)
	put(uint32(len(nonce)))
	b.Write(nonce)
	return b.Bytes()
}

// NewerError reports a key file written by a newer rohy.
type NewerError struct{ Version int }

func (e *NewerError) Error() string {
	return fmt.Sprintf("key format %d, this build reads %d", e.Version, consts.KeyFileVersion)
}
