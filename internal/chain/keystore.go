package chain

// The key stores a vault's data key is wrapped by: the login Keychain on
// macOS, through /usr/bin/security, and the Secret Service on Linux,
// through secret-tool (libsecret). Each keeps one key, 32 random bytes, as
// 64 hex digits, which go to the tool on its input, never on a command
// line, where other processes would see them.
//
// A store answers in one of three ways: the key, ErrNoKey when it plainly
// has none, or ErrNoStore when it can't be asked now, as when the Keychain
// is locked, there is no login session, or the tool fails. Only the
// second may make a new key; the third must never read as a key lost.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// KeyStore is where the system keeps the key that wraps a vault's data
// key.
type KeyStore interface {
	// Kind names it in a key file: KeptInKeychain or KeptInSecrets.
	Kind() string
	// Where says where it is, for a person: "the login Keychain".
	Where() string
	// Get is the key it keeps, or ErrNoKey.
	Get() ([]byte, error)
	// Set keeps key in place of any it kept.
	Set(key []byte) error
}

// storeWait is how long one run of a store's tool may take.
const storeWait = 10 * time.Second

// SystemKeyStore is the store this computer keeps keys in, for account
// (such as "sidebar") under the service brand.Name+" vault": the login
// Keychain on macOS; on Linux the Secret Service, when secret-tool is
// installed and the service answers; else nil, for none. IRIE_KEYCHAIN=off
// makes it none, as a test's Irie wants.
func SystemKeyStore(account string) KeyStore {
	if os.Getenv(brand.Env("KEYCHAIN")) == "off" {
		return nil
	}
	service := brand.Name + " vault"
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat("/usr/bin/security"); err == nil {
			return Keychain{Service: service, Account: account}
		}
	case "linux":
		bin, err := exec.LookPath("secret-tool")
		if err != nil {
			return nil
		}
		s := SecretService{Bin: bin, Service: service, Account: account}
		if _, err := s.Get(); err == nil || errors.Is(err, ErrNoKey) {
			return s
		}
	}
	return nil
}

// run runs bin with args and stdin, and gives its output, its exit status
// and an error that says what it wrote to stderr.
func run(bin, stdin string, args ...string) (out []byte, errOut string, code int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err = cmd.Run()
	errOut = strings.TrimSpace(e.String())
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return o.Bytes(), errOut, exit.ExitCode(), fmt.Errorf("%s: %s", bin, strings.Join(strings.Fields(cmp.Or(errOut, exit.String())), " "))
	}
	if err != nil {
		return nil, errOut, -1, fmt.Errorf("%s: %v", bin, err)
	}
	return o.Bytes(), errOut, 0, nil
}

// away is err as a store that can't be asked now gives it: ErrNoStore,
// with the tool's own words.
func away(where string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrNoStore, where, err)
}

// readKey is a key as a store gives it back: 64 hex digits.
func readKey(out []byte) ([]byte, error) {
	k, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil || len(k) != KeySize {
		return nil, errors.New("the key it keeps is not one a vault made")
	}
	return k, nil
}

// Keychain keeps the key as a generic password of the login Keychain, or
// of the keychain at Path, through security(1). Each command names the
// keychain it means, so a session whose search list lacks the login
// keychain gets an error, not "not found".
type Keychain struct {
	Service, Account string
	Path             string // "" for the login keychain, as security login-keychain names it
	Bin              string // security; "" for /usr/bin/security
}

// notFound is security's exit status for an item the keychain lacks: the
// low byte of errSecItemNotFound. Any other failure, such as a locked
// keychain where no one may be asked (errSecInteractionNotAllowed, 36),
// one that is not there (50), or a refusal (51), is the store out of
// reach.
const notFound = 44

func (Keychain) Kind() string { return KeptInKeychain }

func (k Keychain) Where() string {
	if k.Path != "" {
		return "the keychain " + k.Path
	}
	return "the login Keychain"
}

func (k Keychain) bin() string { return cmp.Or(k.Bin, "/usr/bin/security") }

// keychain is the keychain k means: Path, or the login keychain.
func (k Keychain) keychain() (string, error) {
	if k.Path != "" {
		return k.Path, nil
	}
	out, _, _, err := run(k.bin(), "", "login-keychain")
	path := strings.Trim(strings.TrimSpace(string(out)), `"`)
	if err != nil || path == "" {
		return "", away(k.Where(), cmp.Or(err, errors.New("this session has no login keychain")))
	}
	return path, nil
}

func (k Keychain) Get() ([]byte, error) {
	kc, err := k.keychain()
	if err != nil {
		return nil, err
	}
	out, _, code, err := run(k.bin(), "", "find-generic-password", "-s", k.Service, "-a", k.Account, "-w", kc)
	switch {
	case code == notFound:
		return nil, ErrNoKey
	case err != nil:
		return nil, away(k.Where(), err)
	}
	return readKey(out)
}

// Set keeps the key through security's interactive mode, which reads the
// command from its input; -X takes the password as hex, so nothing in it
// needs quoting. security -i says nothing of a command that failed, so Set
// reads the key back.
func (k Keychain) Set(key []byte) error {
	kc, err := k.keychain()
	if err != nil {
		return err
	}
	pw := hex.EncodeToString(key)
	line := fmt.Sprintf("add-generic-password -U -s %q -a %q -l %q -j %q -X %s %q", k.Service, k.Account,
		k.Service+": "+k.Account, "The key that wraps the "+k.Account+" chain's key. Without it the chain's versions do not open.",
		hex.EncodeToString([]byte(pw)), kc)
	if _, _, _, err := run(k.bin(), line+"\n", "-i"); err != nil {
		return away(k.Where(), err)
	}
	got, err := k.Get()
	if err != nil {
		return fmt.Errorf("the Keychain did not keep the key: %w", err)
	}
	if !bytes.Equal(got, key) {
		return away(k.Where(), errors.New("it kept another key than the one given"))
	}
	return nil
}

// SecretService keeps the key in the Secret Service (the desktop's
// keyring, such as GNOME Keyring or KWallet), through secret-tool, under
// the attributes service and account.
type SecretService struct {
	Bin              string // secret-tool
	Service, Account string
}

func (SecretService) Kind() string  { return KeptInSecrets }
func (SecretService) Where() string { return "the Secret Service" }

func (s SecretService) attrs() []string { return []string{"service", s.Service, "account", s.Account} }

// Get looks the key up. secret-tool says nothing and fails when no item
// matches, and says why when the service does not answer.
func (s SecretService) Get() ([]byte, error) {
	out, errOut, code, err := run(s.Bin, "", append([]string{"lookup"}, s.attrs()...)...)
	switch {
	case code > 0 && errOut == "" && len(out) == 0:
		return nil, ErrNoKey
	case err != nil:
		return nil, away(s.Where(), err)
	}
	return readKey(out)
}

// Set stores the key, which secret-tool reads from its input, all of it
// up to the end; so no newline goes after it.
func (s SecretService) Set(key []byte) error {
	args := append([]string{"store", "--label=" + s.Service + ": " + s.Account}, s.attrs()...)
	if _, _, _, err := run(s.Bin, hex.EncodeToString(key), args...); err != nil {
		return away(s.Where(), err)
	}
	got, err := s.Get()
	if err != nil {
		return fmt.Errorf("the Secret Service did not keep the key: %w", err)
	}
	if !bytes.Equal(got, key) {
		return away(s.Where(), errors.New("it kept another key than the one given"))
	}
	return nil
}
