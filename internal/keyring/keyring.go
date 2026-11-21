// Package keyring keeps the keys that lock Irie's vaults
// (docs/proposals/local-chain.md, "The vault, at rest"): in the macOS
// Keychain, through security(1), else in files of the user's alone. A key
// is Size random bytes, made the first time it is asked for and never
// replaced, since what it locks needs it as it was.
package keyring

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
)

// Size is a key's length in bytes: AES-256's.
const Size = 32

// Source keeps keys by name.
type Source interface {
	// Key is the key named, made at random the first time it is asked
	// for.
	Key(name string) ([]byte, error)
}

// Service is the Keychain service the keys are kept under, which Keychain
// Access lists them by.
var Service = brand.Name + " vault"

// Default is where this computer keeps the keys: the login keychain on
// macOS, else a file each in dir. IRIE_KEYCHAIN=off keeps them in dir on
// macOS too, as every test does.
func Default(dir string) Source {
	if runtime.GOOS == "darwin" && os.Getenv(brand.Env("KEYCHAIN")) != "off" {
		return Keychain{}
	}
	return Files{Dir: dir}
}

// checkName refuses a name that is more than a few lower-case letters,
// digits and dashes, as every key's is.
func checkName(name string) error {
	if name == "" || len(name) > 64 || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return fmt.Errorf("keyring: %q is not a key's name", name)
	}
	return nil
}

// fresh is a new key, from the system's random source.
func fresh() []byte {
	k := make([]byte, Size)
	rand.Read(k) // never fails: it stops the program instead (crypto/rand)
	return k
}

// parse reads a key kept as hex.
func parse(s, where string) ([]byte, error) {
	k, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(k) != Size {
		return nil, fmt.Errorf("keyring: %s holds no key of %d bytes", where, Size)
	}
	return k, nil
}

// errNotFound is a keychain's answer for a key it doesn't keep.
var errNotFound = errors.New("keyring: no such key")

// Keychain keeps each key as a generic password, service Service and
// account the key's name, written as hex. The key goes to security on its
// input, never on a command line, where other processes would see it.
// Since security itself makes the item, the item lets security read it
// again without asking.
type Keychain struct {
	Bin  string // security; "" = /usr/bin/security
	Path string // the keychain; "" = the user's default, the login keychain
}

// keychainWait is how long one run of security may take.
const keychainWait = 10 * time.Second

// security's exit statuses for an item the keychain lacks, and for one it
// has already.
const (
	notFound  = 44
	duplicate = 45
)

func (k Keychain) run(stdin string, args ...string) ([]byte, int, error) {
	bin := k.Bin
	if bin == "" {
		bin = "/usr/bin/security"
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.Bytes(), exit.ExitCode(), fmt.Errorf("security: %s", strings.TrimSpace(errb.String()))
	}
	if err != nil {
		return nil, -1, fmt.Errorf("security: %v", err)
	}
	return out.Bytes(), 0, nil
}

func (k Keychain) find(name string) ([]byte, error) {
	kc, err := k.keychain()
	if err != nil {
		return nil, err
	}
	out, code, err := k.run("", "find-generic-password", "-s", Service, "-a", name, "-w", kc)
	switch {
	case code == notFound:
		return nil, errNotFound
	case err != nil:
		// Locked where no one may be asked (36), not there (50), refused
		// (51): the store can't be asked now, which is never a key lost.
		return nil, fmt.Errorf("%w: %v", ErrAway, err)
	}
	return parse(string(out), "the Keychain's "+Service+": "+name)
}

// Key reads the key from the keychain, or makes it there. security's
// interactive mode reads the command from its input, with the key as hex.
// The key is added without -U, which would replace one there already, so a
// key that came meanwhile is kept, and the key comes back from a second
// read, as kept.
func (k Keychain) Key(name string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	key, err := k.find(name)
	if !errors.Is(err, errNotFound) {
		return key, err
	}
	kc, err := k.keychain()
	if err != nil {
		return nil, err
	}
	written := hex.EncodeToString([]byte(hex.EncodeToString(fresh())))
	line := fmt.Sprintf("add-generic-password -s %q -a %q -l %q -X %s %q", Service, name, Service+": "+name, written, kc)
	if _, code, err := k.run(line+"\n", "-i"); err != nil && code != duplicate {
		return nil, fmt.Errorf("%w: %v", ErrAway, err)
	}
	key, err = k.find(name)
	if errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("keyring: the Keychain didn't keep the key %q", name)
	}
	return key, err
}

// Files keeps each key in a file of its own in Dir, written as hex and
// readable by the user alone: for a system with no Keychain, and for
// tests.
type Files struct {
	Dir string
}

// Key reads the key's file, or makes it: whole, beside it, then linked
// into place, which refuses a key that came meanwhile, so the key read
// back is the one kept.
func (f Files) Key(name string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	path := filepath.Join(f.Dir, name+".key")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		err = filetx.Create(path, []byte(hex.EncodeToString(fresh())+"\n"), 0o600)
		if err == nil || errors.Is(err, fs.ErrExist) {
			b, err = os.ReadFile(path)
		}
	}
	if err != nil {
		return nil, err
	}
	return parse(string(b), path)
}
