package mail

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
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
)

// ErrNoPassword is a store's answer for an account it keeps no password
// for.
var ErrNoPassword = errors.New("no password")

// Store keeps the accounts' passwords out of config.toml, by address: in
// the macOS Keychain, else in files of the user's alone.
type Store interface {
	Get(address string) (string, error) // ErrNoPassword when it has none
	Has(address string) (bool, error)
	Set(address, password string) error
	Delete(address string) error // none to delete is no error
}

// KeychainService is the Keychain service the passwords are kept under, which
// Keychain Access lists them by.
var KeychainService = brand.Name + " mail"

// DefaultStore is where this computer keeps the passwords: the login
// keychain on macOS, else a file each in dir. IRIE_KEYCHAIN=off keeps
// them in dir on macOS too, as a test's Irie does.
func DefaultStore(dir string) Store {
	if runtime.GOOS == "darwin" && os.Getenv(brand.Env("KEYCHAIN")) != "off" {
		return Keychain{}
	}
	return Files{Dir: dir}
}

// CheckPassword says what is wrong with a password: Irie takes one line,
// as every server's are.
func CheckPassword(p string) error {
	switch {
	case p == "":
		return errors.New("the password is empty")
	case len(p) > 1024:
		return errors.New("the password is longer than any server takes")
	case strings.ContainsAny(p, "\r\n\x00"):
		return errors.New("a password is one line: check that it was copied whole")
	case !utf8.ValidString(p):
		return errors.New("the password isn't text: check that it was copied whole")
	}
	return nil
}

// Keychain keeps each password as a generic password, service KeychainService
// and account the address, through security(1). The password goes to
// security on its input, as hex, never on a command line, where other
// processes would see it.
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

func (k Keychain) Get(address string) (string, error) {
	if !config.MailAddress(address) {
		return "", fmt.Errorf("%q is not an email address", address)
	}
	out, code, err := k.run("", k.in("find-generic-password", "-s", KeychainService, "-a", address, "-w")...)
	switch {
	case code == notFound:
		return "", ErrNoPassword
	case err != nil:
		return "", err
	}
	return unhex(strings.TrimRight(string(out), "\r\n")), nil
}

// unhex undoes security's printing of a password beyond plain ASCII, such
// as one with an accent, as hex: hex that reads as such a password is one.
// A plain password of hex digits, such as deadbeef, comes back as it is,
// since its bytes as hex are no UTF-8.
func unhex(s string) string {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) == 0 || !utf8.Valid(b) {
		return s
	}
	for _, c := range b {
		if c >= utf8.RuneSelf {
			return string(b)
		}
	}
	return s
}

func (k Keychain) Has(address string) (bool, error) {
	if !config.MailAddress(address) {
		return false, fmt.Errorf("%q is not an email address", address)
	}
	_, code, err := k.run("", k.in("find-generic-password", "-s", KeychainService, "-a", address)...)
	switch {
	case code == notFound:
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// Set adds the password, or replaces the account's: security's interactive
// mode reads the command from its input, and the password goes as hex, so
// no character of it needs quoting.
func (k Keychain) Set(address, password string) error {
	if err := CheckPassword(password); err != nil {
		return err
	}
	if !config.MailAddress(address) {
		return fmt.Errorf("%q is not an email address", address)
	}
	line := fmt.Sprintf("add-generic-password -U -s %q -a %q -l %q -X %s", KeychainService, address, KeychainService+": "+address, hex.EncodeToString([]byte(password)))
	if k.Path != "" {
		line += fmt.Sprintf(" %q", k.Path)
	}
	if _, _, err := k.run(line+"\n", "-i"); err != nil {
		return err
	}
	// security -i says nothing of a command that failed, so look.
	got, err := k.Get(address)
	if err != nil {
		return fmt.Errorf("the Keychain didn't keep the password: %v", err)
	}
	if got != password {
		return errors.New("the Keychain kept another password than the one given")
	}
	return nil
}

func (k Keychain) Delete(address string) error {
	if !config.MailAddress(address) {
		return fmt.Errorf("%q is not an email address", address)
	}
	_, code, err := k.run("", k.in("delete-generic-password", "-s", KeychainService, "-a", address)...)
	if code == notFound {
		return nil
	}
	return err
}

// Files keeps each password in a file of its own in Dir, readable by the
// user alone, for a system with no Keychain.
type Files struct {
	Dir string
}

func (f Files) path(address string) (string, error) {
	if !config.MailAddress(address) {
		return "", fmt.Errorf("%q is not an email address", address)
	}
	return filepath.Join(f.Dir, strings.ToLower(address)), nil
}

func (f Files) Get(address string) (string, error) {
	p, err := f.path(address)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoPassword
	}
	return strings.TrimRight(string(b), "\n"), err
}

func (f Files) Has(address string) (bool, error) {
	_, err := f.Get(address)
	if errors.Is(err, ErrNoPassword) {
		return false, nil
	}
	return err == nil, err
}

func (f Files) Set(address, password string) error {
	if err := CheckPassword(password); err != nil {
		return err
	}
	p, err := f.path(address)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(f.Dir, 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(p, []byte(password+"\n"), 0o600)
}

func (f Files) Delete(address string) error {
	p, err := f.path(address)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
