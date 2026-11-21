package chain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/irie-people/irie/internal/filetx"
)

// A vault keeps a store's versions sealed on disk (docs/proposals/
// local-chain.md, "The vault, at rest"). Each version is sealed with
// AES-256-GCM under the store's data key, with a fresh random 96-bit
// nonce, and bound to its name, so a sealed file put in another's place
// doesn't open. The data key is sealed the same way under a key the system
// keeps (internal/keyring), in the store's file "key". So no key sits
// beside the data in plain, and nothing in the store reads without the
// system's key.
//
// A vault hides what each version says. It doesn't hide how many versions
// there are, or when their files were written.

// sealFormat is the first byte of every sealed record and of what each is
// bound to, so that a form to come can be told apart.
const sealFormat = 1

// The extensions of a version's file: sealed, or plain, as a store with no
// vault keeps it.
const (
	sealedExt = ".sealed"
	plainExt  = ".json"
)

// keyBound is what a sealed data key is bound to.
const keyBound = "key"

var (
	// ErrLocked says that a version is sealed and the store has no vault
	// open to read it, or that a store with a vault was asked to write
	// with none.
	ErrLocked = errors.New("chain: the vault is locked")
	// ErrWrongKey says that the key given doesn't open the vault's data
	// key, or that the data key is gone while sealed versions remain:
	// they can't be read with that key.
	ErrWrongKey = errors.New("chain: the key doesn't open the vault")
)

// Vault seals a store's versions and opens them again.
type Vault struct {
	aead cipher.AEAD
}

// gcm is AES-256-GCM under key, with a random nonce for each record, which
// goes before it. NIST SP 800-38D (§8.3) allows a key at most 2^32 records
// with random nonces; a store keeps a version for each change a person
// makes.
func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("chain: a vault's key is 32 bytes, not %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

// bound is what a record of what is bound to.
func bound(what string) []byte { return append([]byte{sealFormat}, what...) }

// seal is b sealed under a and bound to what: the format's byte, then the
// nonce, the ciphertext and its tag.
func seal(a cipher.AEAD, b []byte, what string) []byte {
	return a.Seal([]byte{sealFormat}, nil, b, bound(what))
}

// unseal opens a record seal made, and refuses one changed since, bound
// to anything but what, or sealed under another key.
func unseal(a cipher.AEAD, b []byte, what string) ([]byte, error) {
	if len(b) == 0 || b[0] != sealFormat {
		return nil, errors.New("chain: not a sealed record of a form this build reads")
	}
	return a.Open(nil, nil, b[1:], bound(what))
}

// OpenVault opens the vault of the store in dir with kek, the key the
// system keeps for it. A store with no vault yet gets one: a data key made
// at random and sealed under kek. A kek that doesn't open the data key, or
// a data key gone while sealed versions remain, is ErrWrongKey, and
// nothing is replaced. A data key that locks nothing yet, sealed under a
// key the system lost before anything was sealed, is made again.
func OpenVault(dir string, kek []byte) (*Vault, error) {
	wrap, err := gcm(kek)
	if err != nil {
		return nil, err
	}
	s := Store{Dir: dir}
	path := s.keyFile()
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	sealed, serr := s.sealed()
	if serr != nil {
		return nil, serr
	}
	switch {
	case err == nil:
		if dk, err := unseal(wrap, b, keyBound); err == nil {
			return vault(dk)
		}
		if sealed {
			return nil, ErrWrongKey
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	case sealed:
		return nil, fmt.Errorf("%w: its data key is missing", ErrWrongKey)
	}
	dk := make([]byte, 32)
	rand.Read(dk) // never fails: it stops the program instead (crypto/rand)
	if err := filetx.Create(path, seal(wrap, dk, keyBound), 0o600); err != nil {
		return nil, err
	}
	return vault(dk)
}

func vault(dk []byte) (*Vault, error) {
	a, err := gcm(dk)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: a}, nil
}

// keyFile is the store's data key, sealed under the system's key.
func (s Store) keyFile() string { return filepath.Join(s.Dir, "key") }

// HasVault says whether the store has a vault: its versions are, or are
// to be, sealed.
func (s Store) HasVault() bool {
	_, err := os.Stat(s.keyFile())
	return err == nil
}

// sealed says whether the store holds any sealed version.
func (s Store) sealed() (bool, error) {
	files, err := os.ReadDir(filepath.Join(s.Dir, "objects"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), sealedExt) {
			return true, nil
		}
	}
	return false, nil
}

// Seal moves the plain versions into the vault: each is sealed, and its
// plain file removed once its sealed one is in place. A plain version
// whose bytes don't match its name stays as it is, and the error names
// it. It says how many versions it moved.
func (s Store) Seal() (int, error) {
	if s.Vault == nil {
		return 0, ErrLocked
	}
	files, err := os.ReadDir(filepath.Join(s.Dir, "objects"))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, f := range files {
		h, ok := strings.CutSuffix(f.Name(), plainExt)
		name := Prefix + h
		if !ok || !Valid(name) {
			continue
		}
		path := s.object(name, plainExt)
		b, err := os.ReadFile(path)
		if err == nil && Name(b) != name {
			err = fmt.Errorf("chain: version %s does not match its name, so it stays as it is", Short(name))
		}
		if err == nil {
			_, err = s.Put(b)
		}
		if err == nil {
			err = os.Remove(path)
			// No writer takes its lock again: a store with a vault
			// writes no plain version.
			os.Remove(filetx.LockPath(path))
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}
