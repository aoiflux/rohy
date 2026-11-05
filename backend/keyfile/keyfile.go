// Package keyfile keeps an analyst's Ed25519 signing key in a file, encrypted under their
// passphrase (plan §14.6, item 41).
//
// The key signs each seal of a case, so a recipient holding the public key can check who asserted
// an image. That holds only if the key is somewhere the case's holder cannot reach: 🔒 a key file
// inside the case's working folder is refused, at creation and at every unlock, because whoever can
// rewrite the case could re-sign it with the key sitting beside it.
//
// The passphrase is stretched with argon2id and the seed sealed with AES-256-GCM. Every public
// field — the public key, the label, the KDF parameters — is bound into the seal, so a file whose
// parameters were swapped does not open. A wrong passphrase and a damaged file give the same error:
// authenticated encryption cannot tell them apart, and guessing which would only help a guesser.
//
// What it cannot do: Go gives no reliable way to wipe the key from memory once unlocked. It lives
// in the process until the process ends.
package keyfile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/aoiflux/graphene/signing"
	"golang.org/x/crypto/argon2"

	"rohy/backend/consts"
	"rohy/backend/durable"
	"rohy/backend/utils"
)

// Errors a caller tells apart.
var (
	// ErrUnlock is a wrong passphrase or a damaged sealed key; they cannot be told apart.
	ErrUnlock = errors.New(consts.MsgKeyUnlockFailed)
	// ErrInsideCase refuses a key path inside the case's working folder.
	ErrInsideCase = errors.New("key path inside the case")
	// ErrMismatch is a file whose sealed key does not belong to the public key it names.
	ErrMismatch = errors.New("sealed key does not match its public key")
)

// Info is the public half of a key file: enough to show it and match it to a case.
type Info struct {
	Path        string    `json:"path"`
	KeyID       uint64    `json:"key_id"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	Label       string    `json:"label"`
	Created     time.Time `json:"created"`
}

// KeyIDOf derives the key id from the public key: the first eight bytes of its SHA-256, masked to
// 53 bits so it survives the frontend's doubles. Zero means "no key" to the engine and is refused;
// Create draws another key rather than return one.
func KeyIDOf(pub ed25519.PublicKey) (uint64, error) {
	if len(pub) != ed25519.PublicKeySize {
		return 0, fmt.Errorf("public key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	sum := sha256.Sum256(pub)
	id := binary.BigEndian.Uint64(sum[:8]) & (1<<53 - 1)
	if id == 0 {
		return 0, errors.New("this public key maps to key id 0")
	}
	return id, nil
}

// Fingerprint is what an analyst keeps outside the case and a recipient compares: the SHA-256 of
// the public key, in hex.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

// ValidPassphrase checks a passphrase's length and encoding. It imposes no composition rules:
// length is what resists an offline guess at a stolen file.
func ValidPassphrase(p string) error {
	switch {
	case !utf8.ValidString(p):
		return errors.New(consts.MsgKeyPassphraseUTF8)
	case len(p) < consts.KeyMinPassphrase:
		return fmt.Errorf(consts.MsgKeyPassphraseShort, consts.KeyMinPassphrase)
	case len(p) > consts.KeyMaxPassphrase:
		return fmt.Errorf(consts.MsgKeyPassphraseLong, len(p), consts.KeyMaxPassphrase)
	}
	return nil
}

// Create writes a new key file at path, refusing a path inside caseFolder and never replacing an
// existing file.
func Create(path, caseFolder, passphrase, label string, now time.Time) (Info, error) {
	return create(rand.Reader, path, caseFolder, passphrase, label, now, defaultParams())
}

type params struct {
	time, memoryKiB uint32
	threads         uint8
}

func defaultParams() params {
	return params{consts.KeyArgonTime, consts.KeyArgonMemoryKiB, consts.KeyArgonThreads}
}

func create(random io.Reader, path, caseFolder, passphrase, label string, now time.Time, kp params) (Info, error) {
	if err := ValidPassphrase(passphrase); err != nil {
		return Info{}, err
	}
	label, ok := utils.DisplayName(label, consts.KeyMaxLabel)
	if !ok {
		return Info{}, fmt.Errorf(consts.MsgKeyLabelInvalid, consts.KeyMaxLabel)
	}
	if err := refuseInside(path, caseFolder); err != nil {
		return Info{}, err
	}

	var seed []byte
	var pub ed25519.PublicKey
	var id uint64
	for {
		seed = make([]byte, ed25519.SeedSize)
		if _, err := io.ReadFull(random, seed); err != nil {
			return Info{}, err
		}
		pub = ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		var err error
		if id, err = KeyIDOf(pub); err == nil {
			break
		}
	}
	salt := make([]byte, consts.KeySaltLen)
	nonce := make([]byte, consts.KeyNonceLen)
	if _, err := io.ReadFull(random, salt); err != nil {
		return Info{}, err
	}
	if _, err := io.ReadFull(random, nonce); err != nil {
		return Info{}, err
	}

	d := document{
		Format:    consts.KeyFileFormat,
		Version:   consts.KeyFileVersion,
		KeyID:     id,
		PublicKey: hex.EncodeToString(pub),
		Created:   now.UTC().Truncate(time.Second),
		Label:     label,
		KDF: kdfBlock{
			Name: consts.KeyKDFArgon2id, Time: kp.time, MemoryKiB: kp.memoryKiB, Threads: kp.threads,
			Salt: hex.EncodeToString(salt),
		},
		Cipher: aeadBlock{Name: consts.KeyCipherAEAD, Nonce: hex.EncodeToString(nonce)},
	}
	gcm, err := sealer(passphrase, salt, kp)
	if err != nil {
		return Info{}, err
	}
	d.Cipher.Ciphertext = hex.EncodeToString(gcm.Seal(nil, nonce, seed, aad(d, salt, nonce)))

	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return Info{}, err
	}
	if err := durable.CreateNew(path, append(data, '\n'), 0o600); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Info{}, fmt.Errorf(consts.MsgKeyFileExists, path)
		}
		return Info{}, fmt.Errorf(consts.MsgKeyWriteFailed, err)
	}
	return info(path, d, pub), nil
}

// Inspect reads a key file's public half, without the passphrase.
func Inspect(path string) (Info, error) {
	p, err := read(path)
	if err != nil {
		return Info{}, err
	}
	return info(path, p.doc, p.pub), nil
}

// Unlock opens a key file. The path is checked against caseFolder first: a key moved into the case
// after it was created is refused like one created there.
func Unlock(path, caseFolder, passphrase string) (*signing.Key, Info, error) {
	if err := refuseInside(path, caseFolder); err != nil {
		return nil, Info{}, err
	}
	p, err := read(path)
	if err != nil {
		return nil, Info{}, err
	}
	key, err := open(p, passphrase)
	if errors.Is(err, ErrMismatch) {
		return nil, Info{}, fmt.Errorf("%w: "+consts.MsgKeyFileMismatch, ErrMismatch, path)
	}
	if err != nil {
		return nil, Info{}, err
	}
	return key, info(path, p.doc, p.pub), nil
}

func open(p parsed, passphrase string) (*signing.Key, error) {
	if err := ValidPassphrase(passphrase); err != nil {
		return nil, ErrUnlock
	}
	kp := params{p.doc.KDF.Time, p.doc.KDF.MemoryKiB, p.doc.KDF.Threads}
	gcm, err := sealer(passphrase, p.salt, kp)
	if err != nil {
		return nil, err
	}
	seed, err := gcm.Open(nil, p.nonce, p.ciphertext, aad(p.doc, p.salt, p.nonce))
	if err != nil {
		return nil, ErrUnlock
	}
	priv := ed25519.NewKeyFromSeed(seed)
	if !priv.Public().(ed25519.PublicKey).Equal(p.pub) {
		// Authenticated, so not a passphrase problem: the file was written wrong.
		return nil, ErrMismatch
	}
	return signing.NewKey(p.doc.KeyID, priv)
}

func sealer(passphrase string, salt []byte, kp params) (cipher.AEAD, error) {
	k := argon2.IDKey([]byte(passphrase), salt, kp.time, kp.memoryKiB, kp.threads, 32)
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func read(path string) (parsed, error) {
	f, err := os.Open(path)
	if err != nil {
		return parsed{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, consts.KeyFileMaxSize+1))
	if err != nil {
		return parsed{}, err
	}
	p, err := decode(data)
	var newer *NewerError
	var bad *NotKeyError
	switch {
	case errors.As(err, &newer):
		return p, fmt.Errorf(consts.MsgKeyFileNewer, path, newer.Version, consts.KeyFileVersion)
	case errors.As(err, &bad):
		return p, &NotKeyError{Path: path, Detail: bad.Detail}
	case err != nil:
		return p, err
	}
	return p, nil
}

func info(path string, d document, pub ed25519.PublicKey) Info {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return Info{
		Path: abs, KeyID: d.KeyID, PublicKey: d.PublicKey, Fingerprint: Fingerprint(pub),
		Label: d.Label, Created: d.Created,
	}
}

func refuseInside(path, caseFolder string) error {
	inside, err := InsideFolder(path, caseFolder)
	if err != nil {
		return err
	}
	if inside {
		return fmt.Errorf("%w: "+consts.MsgKeyInsideCase, ErrInsideCase, path, caseFolder)
	}
	return nil
}
