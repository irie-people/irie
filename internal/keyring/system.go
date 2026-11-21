package keyring

// Where the keys of the sidebar's chain are kept (DESIGN §4.23): the
// store the system offers, said plainly. On macOS the login Keychain; on
// Linux the Secret Service, through secret-tool (libsecret), when it
// answers; else a file each, of the user's alone. A store answers in one
// of three ways: the key; ErrNoKey, when it plainly has none, which alone
// may lead to a new key; or ErrAway, when it can't be asked now, as when
// the Keychain is locked, there is no login session, or its tool fails,
// which is never a key lost.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

var (
	// ErrNoKey says that a store plainly keeps no key of the name.
	ErrNoKey = errNotFound
	// ErrAway says that a store can't be asked now: off, locked where no
	// one may be asked, with no login session, or with a tool that fails.
	ErrAway = errors.New("keyring: the key store is off or out of reach now")
)

// Where a source keeps its keys, as Kind names it.
const (
	KindKeychain = "keychain"
	KindSecrets  = "secret-service"
	KindFile     = "file"
)

// Finder reads a key and never makes one: a key lost must read as lost,
// not be made again under the same name.
type Finder interface {
	Find(name string) ([]byte, error)
}

// System is where this computer keeps the keys of Irie's vaults, as
// Default, but on Linux the Secret Service when secret-tool is installed
// and the service answers: there, a file only with neither.
// IRIE_KEYCHAIN=off keeps them in dir, as every test does.
func System(dir string) Source {
	if runtime.GOOS == "linux" && os.Getenv(brand.Env("KEYCHAIN")) != "off" {
		if bin, err := exec.LookPath("secret-tool"); err == nil {
			s := SecretService{Bin: bin}
			if _, err := s.Find("probe"); err == nil || errors.Is(err, ErrNoKey) {
				return s
			}
		}
	}
	return Default(dir)
}

// Kind names where src keeps its keys: KindKeychain, KindSecrets or
// KindFile.
func Kind(src Source) string {
	if k, ok := src.(interface{ Kind() string }); ok {
		return k.Kind()
	}
	switch src.(type) {
	case Keychain:
		return KindKeychain
	case SecretService:
		return KindSecrets
	}
	return KindFile
}

// keychain is the keychain k means: Path, or the login keychain as
// security login-keychain names it, so that a session whose search list
// lacks it gets an error rather than "not found".
func (k Keychain) keychain() (string, error) {
	if k.Path != "" {
		return k.Path, nil
	}
	out, _, err := k.run("", "login-keychain")
	path := strings.Trim(strings.TrimSpace(string(out)), `"`)
	if err != nil || path == "" {
		if err == nil {
			err = errors.New("this session has no login keychain")
		}
		return "", fmt.Errorf("%w: %v", ErrAway, err)
	}
	return path, nil
}

// Find is the key named, or ErrNoKey; it makes none.
func (k Keychain) Find(name string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	return k.find(name)
}

// Find is the key named, or ErrNoKey; it makes none.
func (f Files) Find(name string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	path := filepath.Join(f.Dir, name+".key")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, err
	}
	return parse(string(b), path)
}

// SecretService keeps each key in the Secret Service, the desktop's
// keyring (GNOME Keyring, KWallet), through secret-tool, under the
// attributes service Service and account the key's name, written as hex.
// The key goes to secret-tool on its input, which it reads to the end, so
// no newline goes after it.
type SecretService struct {
	Bin string // secret-tool
}

func (s SecretService) run(stdin string, args ...string) (out []byte, errOut string, code int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), keychainWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err = cmd.Run()
	errOut = strings.TrimSpace(e.String())
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return o.Bytes(), errOut, exit.ExitCode(), fmt.Errorf("secret-tool: %s", errOut)
	}
	if err != nil {
		return nil, errOut, -1, fmt.Errorf("secret-tool: %v", err)
	}
	return o.Bytes(), errOut, 0, nil
}

func (SecretService) attrs(name string) []string {
	return []string{"service", Service, "account", name}
}

// Find looks the key up. secret-tool says nothing and fails when no item
// matches, and says why when the service does not answer.
func (s SecretService) Find(name string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	out, errOut, code, err := s.run("", append([]string{"lookup"}, s.attrs(name)...)...)
	switch {
	case code > 0 && errOut == "" && len(out) == 0:
		return nil, ErrNoKey
	case err != nil:
		return nil, fmt.Errorf("%w: %v", ErrAway, err)
	}
	return parse(string(out), "the Secret Service's "+Service+": "+name)
}

// Key reads the key, or makes it and stores it, then reads it back as
// kept.
func (s SecretService) Key(name string) ([]byte, error) {
	key, err := s.Find(name)
	if !errors.Is(err, ErrNoKey) {
		return key, err
	}
	args := append([]string{"store", "--label=" + Service + ": " + name}, s.attrs(name)...)
	if _, _, _, err := s.run(hex.EncodeToString(fresh()), args...); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAway, err)
	}
	key, err = s.Find(name)
	if errors.Is(err, ErrNoKey) {
		return nil, fmt.Errorf("%w: the Secret Service didn't keep the key %q", ErrAway, name)
	}
	return key, err
}

// Putter keeps a key it is given under a name, in place of any it kept: to
// move a key kept in a file into a store, once there is one.
type Putter interface {
	Put(name string, key []byte) error
}

// Put keeps key in the keychain under name, through security's interactive
// mode, and reads it back as kept.
func (k Keychain) Put(name string, key []byte) error {
	if err := checkName(name); err != nil {
		return err
	}
	kc, err := k.keychain()
	if err != nil {
		return err
	}
	written := hex.EncodeToString([]byte(hex.EncodeToString(key)))
	line := fmt.Sprintf("add-generic-password -U -s %q -a %q -l %q -X %s %q", Service, name, Service+": "+name, written, kc)
	if _, _, err := k.run(line+"\n", "-i"); err != nil {
		return fmt.Errorf("%w: %v", ErrAway, err)
	}
	return readBack(k, name, key)
}

// Put keeps key in the Secret Service under name, and reads it back.
func (s SecretService) Put(name string, key []byte) error {
	if err := checkName(name); err != nil {
		return err
	}
	args := append([]string{"store", "--label=" + Service + ": " + name}, s.attrs(name)...)
	if _, _, _, err := s.run(hex.EncodeToString(key), args...); err != nil {
		return fmt.Errorf("%w: %v", ErrAway, err)
	}
	return readBack(s, name, key)
}

// readBack checks that f keeps key under name.
func readBack(f Finder, name string, key []byte) error {
	got, err := f.Find(name)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, key) {
		return fmt.Errorf("%w: it kept another key than the one given", ErrAway)
	}
	return nil
}
