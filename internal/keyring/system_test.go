package keyring

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
)

// A Keychain that can't be asked now is away, never a key lost: locked
// where no one may be asked (36), not there (50), refused (51) or
// cancelled (128), or a session with no login keychain; and Key makes no
// key then. Only "not found" (44) is no key. Each command names the
// keychain it means, the login keychain by default.
func TestAKeychainThatCantBeAskedIsAway(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "security")
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
case "$1" in
login-keychain)
	[ -n "$NO_LOGIN" ] && { echo "security: SecKeychainCopyLogin: The specified keychain could not be found." >&2; exit 50; }
	echo '    "/home/alice/Library/Keychains/login.keychain-db"' ;;
find-generic-password)
	[ "$FAKE_EXIT" = 44 ] || echo "security: SecKeychainSearchCopyNext: $FAKE_WORDS" >&2
	exit "$FAKE_EXIT" ;;
-i) cat >> "` + log + `"; exit 1 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	k := Keychain{Bin: bin}
	for code, words := range map[string]string{
		"36": "User interaction is not allowed.", "50": "The specified keychain could not be found.",
		"51": "The user name or passphrase you entered is not correct.", "128": "User canceled the operation.",
	} {
		t.Setenv("FAKE_EXIT", code)
		t.Setenv("FAKE_WORDS", words)
		for what, get := range map[string]func(string) ([]byte, error){"find": k.Find, "key": k.Key} {
			if _, err := get("sidebar"); !errors.Is(err, ErrAway) || errors.Is(err, ErrNoKey) || !strings.Contains(err.Error(), words) {
				t.Errorf("exit %s, %s: %v", code, what, err)
			}
		}
	}
	b, _ := os.ReadFile(log)
	if bytes.Contains(b, []byte("add-generic-password")) {
		t.Errorf("a key was made while the Keychain was away:\n%s", b)
	}
	if !bytes.Contains(b, []byte("find-generic-password -s "+brand.Name+" vault -a sidebar -w /home/alice/Library/Keychains/login.keychain-db")) {
		t.Errorf("the login keychain was not named:\n%s", b)
	}
	t.Setenv("FAKE_EXIT", "44")
	if _, err := k.Find("sidebar"); !errors.Is(err, ErrNoKey) {
		t.Errorf("not found: %v", err)
	}
	t.Setenv("NO_LOGIN", "1")
	if _, err := k.Find("sidebar"); !errors.Is(err, ErrAway) || errors.Is(err, ErrNoKey) {
		t.Errorf("no login keychain: %v", err)
	}
}

// The Secret Service through secret-tool: a stand-in that keeps what it is
// given in a file, says nothing when it has none, and fails aloud when its
// service is down.
func TestSecretServiceKeepsTheKey(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "secret-tool")
	script := `#!/bin/sh
case "$1" in
store) shift; shift; cat > "$STORE.$*" ;;
lookup) shift; [ -n "$DOWN" ] && { echo "Cannot autolaunch D-Bus without X11 \$DISPLAY" >&2; exit 1; }; [ -f "$STORE.$*" ] || exit 1; cat "$STORE.$*" ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STORE", filepath.Join(dir, "kept"))
	s := SecretService{Bin: bin}
	if _, err := s.Find("sidebar"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("before: %v", err)
	}
	key, err := s.Key("sidebar")
	if err != nil || len(key) != Size {
		t.Fatalf("key %x %v", key, err)
	}
	if got, err := s.Find("sidebar"); err != nil || !bytes.Equal(got, key) {
		t.Errorf("find %x %v", got, err)
	}
	if again, err := s.Key("sidebar"); err != nil || !bytes.Equal(again, key) {
		t.Errorf("a second key %x %v", again, err)
	}
	kept, _ := filepath.Glob(filepath.Join(dir, "kept.*"))
	if len(kept) != 1 || !strings.HasSuffix(kept[0], "service "+Service+" account sidebar") {
		t.Errorf("kept %v", kept)
	}
	if b, _ := os.ReadFile(kept[0]); len(b) != 2*Size {
		t.Errorf("stored %q: the hex, and no newline", b)
	}
	t.Setenv("DOWN", "1")
	if _, err := s.Key("sidebar"); !errors.Is(err, ErrAway) || errors.Is(err, ErrNoKey) || !strings.Contains(err.Error(), "D-Bus") {
		t.Errorf("down: %v", err)
	}
}

// Find reads a key and never makes one; Kind says where a source keeps
// its keys; with IRIE_KEYCHAIN=off, System keeps them in files, as Default.
func TestFindMakesNoKey(t *testing.T) {
	f := Files{Dir: filepath.Join(t.TempDir(), "keys")}
	if _, err := f.Find("sidebar"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("before: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "sidebar.key")); !os.IsNotExist(err) {
		t.Error("Find made a key")
	}
	key, _ := f.Key("sidebar")
	if got, err := f.Find("sidebar"); err != nil || !bytes.Equal(got, key) {
		t.Errorf("find %x %v", got, err)
	}
	t.Setenv(brand.Env("KEYCHAIN"), "off")
	if src := System(f.Dir); Kind(src) != KindFile {
		t.Errorf("System with %s=off: %T", brand.Env("KEYCHAIN"), src)
	}
	if Kind(Keychain{}) != KindKeychain || Kind(SecretService{}) != KindSecrets {
		t.Error("Kind")
	}
}

// Put keeps the key it is given, in place of any, as a key moves from its
// file into a store: through secret-tool, and in a keychain of the test's
// own.
func TestPutKeepsTheKeyGiven(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "secret-tool")
	os.WriteFile(bin, []byte(`#!/bin/sh
case "$1" in
store) shift; shift; cat > "$STORE.$*" ;;
lookup) shift; [ -f "$STORE.$*" ] || exit 1; cat "$STORE.$*" ;;
esac
`), 0o700)
	t.Setenv("STORE", filepath.Join(dir, "kept"))
	key := bytes.Repeat([]byte{0x5a}, Size)
	s := SecretService{Bin: bin}
	s.Key("sidebar")
	if err := s.Put("sidebar", key); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Find("sidebar"); err != nil || !bytes.Equal(got, key) {
		t.Errorf("secret-tool: %x %v", got, err)
	}
	if runtime.GOOS != "darwin" || testing.Short() {
		return
	}
	path := filepath.Join(t.TempDir(), "test.keychain-db")
	if out, err := exec.Command("/usr/bin/security", "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Skipf("security create-keychain: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("/usr/bin/security", "delete-keychain", path).Run() })
	k := Keychain{Path: path}
	k.Key("sidebar")
	if err := k.Put("sidebar", key); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Find("sidebar"); err != nil || !bytes.Equal(got, key) {
		t.Errorf("keychain: %x %v", got, err)
	}
}
