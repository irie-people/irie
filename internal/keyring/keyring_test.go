package keyring

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
)

func TestFilesMakeAKeyOnceAndKeepIt(t *testing.T) {
	f := Files{Dir: filepath.Join(t.TempDir(), "keys")}
	a, err := f.Key("zen")
	if err != nil || len(a) != Size {
		t.Fatalf("key = %x, %v", a, err)
	}
	again, err := f.Key("zen")
	if err != nil || !bytes.Equal(a, again) {
		t.Fatalf("a second read gave %x, %v; want %x", again, err, a)
	}
	other, err := f.Key("chain")
	if err != nil || bytes.Equal(a, other) {
		t.Fatalf("another name's key = %x, %v", other, err)
	}
	fi, err := os.Stat(filepath.Join(f.Dir, "zen.key"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v, %v", fi.Mode(), err)
	}
	// A file that holds no key is an error, and is never replaced.
	bad := filepath.Join(f.Dir, "torn.key")
	os.WriteFile(bad, []byte("abc\n"), 0o600)
	if _, err := f.Key("torn"); err == nil {
		t.Error("a file of no key was taken")
	}
	if b, _ := os.ReadFile(bad); string(b) != "abc\n" {
		t.Errorf("the file was replaced: %q", b)
	}
	for _, name := range []string{"", "../zen", "Zen", "a b", "zen.key"} {
		if _, err := f.Key(name); err == nil {
			t.Errorf("name %q was taken", name)
		}
	}
}

func TestDefaultKeepsTestsOutOfTheKeychain(t *testing.T) {
	t.Setenv(brand.Env("KEYCHAIN"), "off")
	if _, ok := Default(t.TempDir()).(Files); !ok {
		t.Fatalf("%s=off gave %T", brand.Env("KEYCHAIN"), Default(""))
	}
}

// TestKeychain keeps keys in a keychain of its own in a temporary folder,
// never the login keychain.
func TestKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" || testing.Short() {
		t.Skip("macOS's Keychain")
	}
	path := filepath.Join(t.TempDir(), "test.keychain-db")
	if out, err := exec.Command("/usr/bin/security", "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Skipf("security create-keychain: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("/usr/bin/security", "delete-keychain", path).Run() })
	k := Keychain{Path: path}
	a, err := k.Key("zen")
	if err != nil || len(a) != Size {
		t.Fatalf("key = %x, %v", a, err)
	}
	again, err := k.Key("zen")
	if err != nil || !bytes.Equal(a, again) {
		t.Fatalf("a second read gave %x, %v; want %x", again, err, a)
	}
	if other, err := k.Key("chain"); err != nil || bytes.Equal(a, other) {
		t.Fatalf("another name's key = %x, %v", other, err)
	}
	// A key that came while this one was being made is kept, not replaced.
	if out, err := exec.Command("/usr/bin/security", "add-generic-password", "-s", Service, "-a", "race", "-w", strings.Repeat("cd", Size), path).CombinedOutput(); err != nil {
		t.Fatalf("security add: %v %s", err, out)
	}
	if _, code, err := k.run(fmt.Sprintf("add-generic-password -s %q -a race -X %s %q\n", Service, strings.Repeat("6565", Size), path), "-i"); err == nil || code != duplicate {
		t.Errorf("a second add: %d, %v", code, err)
	}
	if got, err := k.Key("race"); err != nil || !bytes.Equal(got, bytes.Repeat([]byte{0xcd}, Size)) {
		t.Errorf("the key kept = %x, %v", got, err)
	}
}
