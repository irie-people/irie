package chain

// The vault (docs/proposals/local-chain.md, step 2): a chain's versions
// sealed at rest. Each record is AES-256-GCM (FIPS 197, NIST SP 800-38D)
// under the vault's data key, with a fresh random 96-bit nonce that the
// record carries (crypto/cipher's NewGCMWithRandomNonce), and the
// version's name as additional data, so a record opens only under its own
// name: one moved to another name, changed or cut short does not open.
//
// The data key is 32 random bytes, kept in the chain's key file, wrapped
// in turn by a key the system keeps: the login Keychain on macOS, the
// Secret Service on Linux. Where the system keeps none, the key file holds
// the data key itself, readable by the user alone, until a passphrase
// (Argon2id) can wrap it; and as soon as there is a store, the key goes
// into it, so no key sits plain beside the data while the system can keep
// one.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/irie-people/irie/internal/filetx"
)

// KeySize is a vault key's size, in bytes: AES-256's.
const KeySize = 32

// SealedExt ends a sealed record's file name, as .json ends a plain one's.
const SealedExt = ".sealed"

// recordForm starts every sealed record: the form of what follows, the
// nonce and the ciphertext with its tag.
const recordForm = 1

// KeyFile is the name of a chain's key file, beside HEAD.
const KeyFile = "key"

// Where a vault's data key is kept, as a key file says it.
const (
	KeptInKeychain = "keychain"       // wrapped by a key in the login Keychain
	KeptInSecrets  = "secret-service" // wrapped by a key in the Secret Service
	KeptInFile     = "file"           // the key file holds it, readable by you alone
)

// Errors a vault that does not open gives, each wrapped with what to do.
var (
	// ErrSealed: a record did not open under the vault's key.
	ErrSealed = errors.New("does not open under the vault's key: it was changed or cut short after it was written, or moved from another name, or the key is not the one it was sealed with")
	// ErrWrongKey: the key does not open the chain: it is not the one its
	// data key, or its versions, were sealed under.
	ErrWrongKey = errors.New("the key does not open the chain: it is not the key it was sealed with")
	// ErrKeyGone: the store keeps no key, but the data key was wrapped by one.
	ErrKeyGone = errors.New("the key that opens the chain's key is gone from where it was kept")
	// ErrNoStore: the store that keeps the key is off or out of reach now:
	// IRIE_KEYCHAIN=off, a locked Keychain where no one may be asked, no
	// login session, or a tool that fails. It is never a key lost.
	ErrNoStore = errors.New("the key store that keeps the chain's key is off or out of reach now")
	// ErrNoKey: a store keeps no key yet.
	ErrNoKey = errors.New("no key")
)

// Vault seals and opens a chain's records under its data key.
type Vault struct {
	aead cipher.AEAD
	// Kept says where its data key is kept: KeptInKeychain, KeptInSecrets
	// or KeptInFile.
	Kept string
}

// NewVault is a vault under key, KeySize bytes, kept as kept says.
func NewVault(key []byte, kept string) (*Vault, error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead, Kept: kept}, nil
}

// gcm is AES-256-GCM under key, each seal with a fresh random 96-bit
// nonce at the front of what it gives.
func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("chain: a vault key is %d bytes, not %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

// recordAD is what a record is sealed with besides its version: its form
// and the name of the version it holds.
func recordAD(name string) []byte { return []byte("chain record 1\x00" + name) }

// Seal is version b, named name, sealed: the form, then the nonce, the
// ciphertext and the tag.
func (v *Vault) Seal(name string, b []byte) []byte {
	return v.aead.Seal([]byte{recordForm}, nil, b, recordAD(name))
}

// Open is the version sealed in rec, which must be the one named name.
func (v *Vault) Open(name string, rec []byte) ([]byte, error) {
	if len(rec) < 1+v.aead.Overhead() || rec[0] != recordForm {
		return nil, ErrSealed
	}
	b, err := v.aead.Open(nil, nil, rec[1:], recordAD(name))
	if err != nil {
		return nil, ErrSealed
	}
	return b, nil
}

// keyFile is a chain's key file: its form, where the data key is kept,
// the data key, wrapped (sealed under the store's key) or, kept in the
// file, as it is, and a check, nothing sealed under the data key, which
// opens only under that key; base64 each.
type keyFile struct {
	Form  int    `json:"form"`
	Kept  string `json:"kept"`
	Key   string `json:"key"`
	Check string `json:"check"`
}

// keyAD is what a data key is wrapped with besides itself, and checkAD
// what its check is sealed with.
var (
	keyAD   = []byte("chain key 1")
	checkAD = []byte("chain key check 1")
)

// OpenVault opens the vault of the chain kept in dir, under the key store
// the system keeps (nil for none). On the first open it makes the data
// key: wrapped by the store's key, made too should the store have none,
// or with no store, in the key file. A key file that holds the data key
// itself gives it to the store as soon as there is one. A store that does
// not open the data key, or that has lost its key or is not there, a key
// that is not the one its check was sealed under, and a key file gone
// from beside sealed records are each an error that says which
// (ErrWrongKey, ErrKeyGone, ErrNoStore), and leave everything as it is.
func OpenVault(dir string, store KeyStore) (*Vault, error) {
	path := filepath.Join(dir, KeyFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// A new key would open none of the records sealed already.
		if sealed, _ := filepath.Glob(filepath.Join(dir, "objects", "*"+SealedExt)); len(sealed) > 0 {
			return nil, fmt.Errorf("%w: the key file %s is gone, and %d versions were sealed under its key", ErrKeyGone, path, len(sealed))
		}
		key := make([]byte, KeySize)
		rand.Read(key)
		kf, err := wrap(key, store)
		if err != nil {
			return nil, err
		}
		if err := writeKeyFile(path, kf); err != nil {
			return nil, err
		}
		return NewVault(key, kf.Kept)
	}
	if err != nil {
		return nil, err
	}
	v, err := openKeyFile(path, raw, store)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// openKeyFile is the vault of key file raw, at path.
func openKeyFile(path string, raw []byte, store KeyStore) (*Vault, error) {
	var kf keyFile
	if err := json.Unmarshal(raw, &kf); err != nil || kf.Form != 1 {
		return nil, fmt.Errorf("the chain's key file %s does not read (%v), so it was left as it is", path, err)
	}
	sealed, err := base64.StdEncoding.DecodeString(kf.Key)
	if err != nil {
		return nil, fmt.Errorf("the chain's key file %s does not read (%v), so it was left as it is", path, err)
	}
	if kf.Kept == KeptInFile {
		if err := checkKey(sealed, kf.Check); err != nil {
			return nil, fmt.Errorf("%w (the key file %s)", err, path)
		}
		if store != nil {
			// Never a key plain beside the data while the system keeps keys.
			next, err := wrap(sealed, store)
			if err != nil {
				return nil, fmt.Errorf("the chain's key could not go into %s, so it stays in %s: %w", store.Where(), path, err)
			}
			if err := writeKeyFile(path, next); err != nil {
				return nil, err
			}
			kf = next
		}
		return NewVault(sealed, kf.Kept)
	}
	if store == nil || store.Kind() != kf.Kept {
		return nil, fmt.Errorf("%w (%s)", ErrNoStore, keptWords(kf.Kept))
	}
	kek, err := store.Get()
	switch {
	case errors.Is(err, ErrNoKey):
		return nil, fmt.Errorf("%w: %s", ErrKeyGone, store.Where())
	case err != nil && !errors.Is(err, ErrNoStore):
		return nil, fmt.Errorf("%w: %s: %v", ErrNoStore, store.Where(), err)
	case err != nil:
		return nil, err
	}
	aead, err := gcm(kek)
	if err != nil {
		return nil, err
	}
	key, err := aead.Open(nil, nil, sealed, keyAD)
	if err != nil {
		return nil, fmt.Errorf("%w (%s)", ErrWrongKey, store.Where())
	}
	if err := checkKey(key, kf.Check); err != nil {
		return nil, fmt.Errorf("%w (%s)", err, store.Where())
	}
	return NewVault(key, kf.Kept)
}

// checkKey says whether key is the one check was sealed under.
func checkKey(key []byte, check string) error {
	aead, err := gcm(key)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWrongKey, err)
	}
	c, err := base64.StdEncoding.DecodeString(check)
	if err != nil {
		return ErrWrongKey
	}
	if _, err := aead.Open(nil, nil, c, checkAD); err != nil {
		return ErrWrongKey
	}
	return nil
}

// keyCheck is the check of data key key: nothing, sealed under it.
func keyCheck(key []byte) string {
	aead, _ := gcm(key)
	return base64.StdEncoding.EncodeToString(aead.Seal(nil, nil, nil, checkAD))
}

// wrap is the key file of data key key: wrapped by the store's key, which
// it makes and gives the store should the store have none; with no store,
// kept in the file.
func wrap(key []byte, store KeyStore) (keyFile, error) {
	if store == nil {
		return keyFile{Form: 1, Kept: KeptInFile, Key: base64.StdEncoding.EncodeToString(key), Check: keyCheck(key)}, nil
	}
	// Only a store that plainly has no key gets a new one; one out of reach
	// gets nothing, and no key is kept anywhere else meanwhile.
	kek, err := store.Get()
	if errors.Is(err, ErrNoKey) {
		kek = make([]byte, KeySize)
		rand.Read(kek)
		err = store.Set(kek)
	}
	if err != nil && !errors.Is(err, ErrNoStore) {
		err = fmt.Errorf("%w: %s: %v", ErrNoStore, store.Where(), err)
	}
	if err != nil {
		return keyFile{}, err
	}
	aead, err := gcm(kek)
	if err != nil {
		return keyFile{}, err
	}
	return keyFile{Form: 1, Kept: store.Kind(), Key: base64.StdEncoding.EncodeToString(aead.Seal(nil, nil, key, keyAD)), Check: keyCheck(key)}, nil
}

func writeKeyFile(path string, kf keyFile) error {
	b, err := json.Marshal(kf)
	if err != nil {
		return err
	}
	return filetx.WriteFile(path, append(b, '\n'), 0o600)
}

// keptWords says where a key file's kept names, for a person.
func keptWords(kept string) string {
	switch kept {
	case KeptInKeychain:
		return "the login Keychain"
	case KeptInSecrets:
		return "the Secret Service"
	case KeptInFile:
		return "the key file"
	}
	return fmt.Sprintf("%q", kept)
}

// MoveIn seals each plain version the store kept before it had a vault,
// each checked against its name first: one that does not match stays as
// it is, plain, out of the vault's reach, and is listed in stayed, never
// sealed as though it were good. A version sealed already loses its plain
// copy once the seal opens as the same bytes, so a move cut short goes on
// where it stopped. moved counts the versions sealed.
func (s Store) MoveIn() (moved int, stayed []string, err error) {
	if s.Vault == nil {
		return 0, nil, errors.New("chain: no vault to move the versions into")
	}
	files, err := os.ReadDir(filepath.Join(s.Dir, "objects"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	for _, f := range files {
		h, ok := strings.CutSuffix(f.Name(), ".json")
		if !ok || !Valid(Prefix+h) {
			continue
		}
		name := Prefix + h
		b, err := os.ReadFile(s.plain(name))
		if err != nil {
			return moved, stayed, err
		}
		if Name(b) != name {
			stayed = append(stayed, name)
			continue
		}
		if _, err := os.Stat(s.object(name)); err == nil {
			if got, err := s.Get(name); err != nil || !bytes.Equal(got, b) {
				stayed = append(stayed, name)
				continue
			}
		} else if err := filetx.WriteOnce(s.object(name), s.Vault.Seal(name, b), 0o600); err != nil {
			return moved, stayed, err
		}
		if err := os.Remove(s.plain(name)); err != nil {
			return moved, stayed, err
		}
		os.Remove(filetx.LockPath(s.plain(name)))
		moved++
	}
	return moved, stayed, nil
}

// AnyOpens reports whether any of the store's sealed records, up to max of
// them, opens under its vault's key: none that does, with records there,
// says the key is not the chain's, rather than that one was changed.
func (s Store) AnyOpens(max int) (opens, any bool) {
	files, _ := os.ReadDir(filepath.Join(s.Dir, "objects"))
	for _, f := range files {
		h, ok := strings.CutSuffix(f.Name(), SealedExt)
		if !ok || !Valid(Prefix+h) || s.Vault == nil {
			continue
		}
		if max--; max < 0 {
			break
		}
		any = true
		if _, err := s.Get(Prefix + h); err == nil {
			return true, true
		}
	}
	return false, any
}
