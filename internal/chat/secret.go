//go:build !nochat

package chat

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
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
)

// Store keeps the accounts' tokens out of config.toml, by key: an account's
// name for its bot's token, and NAME.app for Slack's app-level token. They
// live in the macOS Keychain, else in files of the user's alone.
type Store interface {
	Get(key string) (string, error) // ErrNoToken when it has none
	Set(key, token string) error
	Delete(key string) error // none to delete is no error
}

// AppKey is the key of account's app-level token.
func AppKey(account string) string { return account + ".app" }

// tokenKey is the form of a key: an account's name, perhaps with .app.
var tokenKey = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}(\.app)?$`)

func checkKey(key string) error {
	if !tokenKey.MatchString(key) {
		return fmt.Errorf("%q names no chat account", key)
	}
	return nil
}

// KeychainService is the Keychain service the tokens are kept under, which
// Keychain Access lists them by.
var KeychainService = brand.Name + " chat"

// DefaultStore is where this computer keeps the tokens: the login keychain
// on macOS, else a file each in dir. IRIE_KEYCHAIN=off keeps them in dir
// on macOS too, as a test's Irie does.
func DefaultStore(dir string) Store {
	if runtime.GOOS == "darwin" && os.Getenv(brand.Env("KEYCHAIN")) != "off" {
		return Keychain{}
	}
	return Files{Dir: dir}
}

// CheckToken says what is wrong with a token: one line of text, as every
// service's are.
func CheckToken(t string) error {
	switch {
	case t == "":
		return errors.New("the token is empty")
	case len(t) > 512:
		return errors.New("the token is longer than any service's: check that only the token was copied")
	case strings.ContainsAny(t, " \t\r\n\x00"):
		return errors.New("a token is one word: check that it was copied whole, and nothing with it")
	case !utf8.ValidString(t):
		return errors.New("the token isn't text: check that it was copied whole")
	}
	return nil
}

// Keychain keeps each token as a generic password, service KeychainService
// and account the key, through security(1). The token goes to security on
// its input, as hex, never on a command line, where other processes would
// see it.
type Keychain struct {
	Bin  string // security; "" = /usr/bin/security
	Path string // the keychain; "" = the user's default, the login keychain
}

// keychainWait is how long one run of security may take.
const keychainWait = 10 * time.Second

// notFound is security's exit status for an item the keychain lacks.
const notFound = 44

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

// in is the keychain to search, after a command's own arguments: none for
// the user's default.
func (k Keychain) in(args ...string) []string {
	if k.Path != "" {
		args = append(args, k.Path)
	}
	return args
}

func (k Keychain) Get(key string) (string, error) {
	if err := checkKey(key); err != nil {
		return "", err
	}
	out, code, err := k.run("", k.in("find-generic-password", "-s", KeychainService, "-a", key, "-w")...)
	switch {
	case code == notFound:
		return "", ErrNoToken
	case err != nil:
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

// Set adds the token, or replaces the key's: security's interactive mode
// reads the command from its input, and the token goes as hex, so no
// character of it needs quoting.
func (k Keychain) Set(key, token string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := CheckToken(token); err != nil {
		return err
	}
	line := fmt.Sprintf("add-generic-password -U -s %q -a %q -l %q -X %s", KeychainService, key, KeychainService+": "+key, hex.EncodeToString([]byte(token)))
	if k.Path != "" {
		line += fmt.Sprintf(" %q", k.Path)
	}
	if _, _, err := k.run(line+"\n", "-i"); err != nil {
		return err
	}
	// security -i says nothing of a command that failed, so look.
	got, err := k.Get(key)
	if err != nil {
		return fmt.Errorf("the Keychain didn't keep the token: %v", err)
	}
	if got != token {
		return errors.New("the Keychain kept another token than the one given")
	}
	return nil
}

func (k Keychain) Delete(key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	_, code, err := k.run("", k.in("delete-generic-password", "-s", KeychainService, "-a", key)...)
	if code == notFound {
		return nil
	}
	return err
}

// Files keeps each token in a file of its own in Dir, readable by the user
// alone, for a system with no Keychain.
type Files struct {
	Dir string
}

func (f Files) path(key string) (string, error) {
	if err := checkKey(key); err != nil {
		return "", err
	}
	return filepath.Join(f.Dir, key), nil
}

func (f Files) Get(key string) (string, error) {
	p, err := f.path(key)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoToken
	}
	return strings.TrimRight(string(b), "\n"), err
}

func (f Files) Set(key, token string) error {
	if err := CheckToken(token); err != nil {
		return err
	}
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(f.Dir, 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(p, []byte(token+"\n"), 0o600)
}

func (f Files) Delete(key string) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
