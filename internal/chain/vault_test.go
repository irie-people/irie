package chain

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/filetx"
)

// memStore is a key store in memory, as a test's Keychain.
type memStore struct {
	key  []byte
	kind string
}

func (m *memStore) Kind() string { return m.kind }
func (*memStore) Where() string  { return "a test's store" }
func (m *memStore) Get() ([]byte, error) {
	if m.key == nil {
		return nil, ErrNoKey
	}
	return m.key, nil
}
func (m *memStore) Set(k []byte) error { m.key = bytes.Clone(k); return nil }

func newKey() []byte {
	k := make([]byte, KeySize)
	rand.Read(k)
	return k
}

func vaultStore(t *testing.T, dir string, key []byte) Store {
	t.Helper()
	v, err := NewVault(key, KeptInFile)
	if err != nil {
		t.Fatal(err)
	}
	return Store{Dir: dir, Vault: v}
}

// What goes in comes out, and nothing on disk reads without the key: no
// record holds a word of what it seals, a store without the vault sees
// none of them, and one under another key opens none.
func TestSealedRecordsRoundTripAndNeedTheKey(t *testing.T) {
	dir := t.TempDir()
	key := newKey()
	s := vaultStore(t, dir, key)
	a := put(t, s, map[string]any{"group": "Steak", "card": "alpha"})
	b := put(t, s, map[string]any{"group": "Steak", "card": "beta", "parent": a})
	if err := s.SetHead(b); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{a, b} {
		got, err := s.Get(name)
		if err != nil || Name(got) != name {
			t.Fatalf("get %s: %s %v", Short(name), got, err)
		}
	}
	if log, err := s.Log(b, 0); err != nil || len(log) != 2 {
		t.Errorf("log %d %v", len(log), err)
	}
	if got, err := s.Find(Short(a)); err != nil || got != a {
		t.Errorf("find %q %v", got, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "objects", "*"))
	if len(files) != 2 {
		t.Errorf("objects %v: want two sealed records and no lock files", files)
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		if !strings.HasSuffix(f, SealedExt) || len(raw) < 29 || raw[0] != recordForm || bytes.Contains(raw, []byte("Steak")) || bytes.Contains(raw, []byte("alpha")) {
			t.Errorf("%s holds its record plain: %q", f, raw)
		}
		if info, _ := os.Stat(f); info.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v", f, info.Mode())
		}
	}
	// A record holds a nonce of its own: the same version sealed twice
	// differs.
	if bytes.Equal(s.Vault.Seal(a, []byte("x")), s.Vault.Seal(a, []byte("x"))) {
		t.Error("two seals of one record are the same")
	}
	plain := Store{Dir: dir}
	if _, err := plain.Get(a); err == nil {
		t.Error("a store without the vault read a sealed record")
	}
	other := vaultStore(t, dir, newKey())
	if _, err := other.Get(a); !errors.Is(err, ErrSealed) {
		t.Errorf("another key: %v", err)
	}
	if _, err := other.Log(b, 0); err == nil || !strings.Contains(err.Error(), "does not open") {
		t.Errorf("log under another key: %v", err)
	}
}

// A sealed record opens only as itself: moved to another name, changed
// in one bit, or cut short, it does not.
func TestASealedRecordOpensOnlyAsItself(t *testing.T) {
	s := vaultStore(t, t.TempDir(), newKey())
	a := put(t, s, map[string]any{"n": 1})
	b := put(t, s, map[string]any{"n": 2})
	ra, _ := os.ReadFile(s.object(a))
	rb, _ := os.ReadFile(s.object(b))
	flipped := bytes.Clone(ra)
	flipped[len(flipped)-1] ^= 1
	for what, tc := range map[string]struct {
		name string
		rec  []byte
	}{
		"moved":     {b, ra},
		"flipped":   {a, flipped},
		"cut short": {a, ra[:len(ra)/2]},
		"empty":     {a, nil},
	} {
		os.WriteFile(s.object(tc.name), tc.rec, 0o600)
		if _, err := s.Get(tc.name); !errors.Is(err, ErrSealed) {
			t.Errorf("%s: %v", what, err)
		}
		os.WriteFile(s.object(a), ra, 0o600)
		os.WriteFile(s.object(b), rb, 0o600)
	}
}

func TestOpenVaultKeepsItsKeyWhereTheSystemKeepsKeys(t *testing.T) {
	dir := t.TempDir()
	store := &memStore{kind: KeptInKeychain}
	v, err := OpenVault(dir, store)
	if err != nil || v.Kept != KeptInKeychain || store.key == nil {
		t.Fatalf("first open: %+v %v; store %x", v, err, store.key)
	}
	s := Store{Dir: dir, Vault: v}
	a := put(t, s, map[string]any{"n": 1})
	kf, _ := os.ReadFile(filepath.Join(dir, KeyFile))
	if info, _ := os.Stat(filepath.Join(dir, KeyFile)); info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v", info.Mode())
	}
	// Opened again, the same key opens what it sealed.
	again, err := OpenVault(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Dir: dir, Vault: again}).Get(a); err != nil {
		t.Errorf("reopened: %v", err)
	}
	// A wrong key, a key gone and a store not there fail loudly, and
	// change nothing.
	for what, tc := range map[string]struct {
		store KeyStore
		want  error
	}{
		"wrong key": {&memStore{kind: KeptInKeychain, key: newKey()}, ErrWrongKey},
		"key gone":  {&memStore{kind: KeptInKeychain}, ErrKeyGone},
		"no store":  {nil, ErrNoStore},
		"another":   {&memStore{kind: KeptInSecrets, key: store.key}, ErrNoStore},
	} {
		if _, err := OpenVault(dir, tc.store); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", what, err, tc.want)
		}
		if now, _ := os.ReadFile(filepath.Join(dir, KeyFile)); !bytes.Equal(now, kf) {
			t.Errorf("%s changed the key file", what)
		}
	}
}

// With no store the data key is in the key file, readable by you alone;
// once there is a store it goes into it, and the file keeps it wrapped.
func TestAKeyKeptInTheFileGoesToAStoreOnceThereIsOne(t *testing.T) {
	dir := t.TempDir()
	v, err := OpenVault(dir, nil)
	if err != nil || v.Kept != KeptInFile {
		t.Fatalf("%+v %v", v, err)
	}
	a := put(t, Store{Dir: dir, Vault: v}, map[string]any{"n": 1})
	plainKey, _ := os.ReadFile(filepath.Join(dir, KeyFile))
	store := &memStore{kind: KeptInSecrets}
	w, err := OpenVault(dir, store)
	if err != nil || w.Kept != KeptInSecrets || store.key == nil {
		t.Fatalf("%+v %v", w, err)
	}
	wrapped, _ := os.ReadFile(filepath.Join(dir, KeyFile))
	if bytes.Equal(wrapped, plainKey) || !bytes.Contains(wrapped, []byte(KeptInSecrets)) {
		t.Errorf("key file %s", wrapped)
	}
	if _, err := (Store{Dir: dir, Vault: w}).Get(a); err != nil {
		t.Errorf("after the key moved: %v", err)
	}
}

// A chain kept before the vault moves into it: each plain version that
// matches its name is sealed, its file and lock gone; one that does not
// stays as it was, and is listed.
func TestMoveInSealsAPlainChain(t *testing.T) {
	dir := t.TempDir()
	plain := Store{Dir: dir}
	a := put(t, plain, map[string]any{"n": 1})
	b := put(t, plain, map[string]any{"n": 2, "parent": a})
	c := put(t, plain, map[string]any{"n": 3, "parent": b})
	os.WriteFile(plain.object(c), []byte(`{"n":9}`), 0o600)
	s := vaultStore(t, dir, newKey())
	moved, stayed, err := s.MoveIn()
	if err != nil || moved != 2 || len(stayed) != 1 || stayed[0] != c {
		t.Fatalf("moved %d, stayed %v, %v", moved, stayed, err)
	}
	for _, name := range []string{a, b} {
		if _, err := os.Stat(plain.object(name)); !os.IsNotExist(err) {
			t.Errorf("%s: the plain record stayed", Short(name))
		}
		if _, err := os.Stat(filetx.LockPath(plain.object(name))); !os.IsNotExist(err) {
			t.Errorf("%s: its lock file stayed", Short(name))
		}
	}
	if log, err := s.Log(b, 0); err != nil || len(log) != 2 {
		t.Errorf("log %d %v", len(log), err)
	}
	if _, err := s.Get(c); err == nil || !strings.Contains(err.Error(), "not sealed") {
		t.Errorf("the plain one that stayed: %v", err)
	}
	if again, _, err := s.MoveIn(); err != nil || again != 0 {
		t.Errorf("a second move: %d %v", again, err)
	}
}

// The login Keychain keeps the key, in a keychain of the test's own in a
// temporary folder, never the user's.
func TestKeychainKeepsTheKey(t *testing.T) {
	if runtime.GOOS != "darwin" || testing.Short() {
		t.Skip("macOS's Keychain")
	}
	path := filepath.Join(t.TempDir(), "test.keychain-db")
	if out, err := exec.Command("/usr/bin/security", "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Skipf("security create-keychain: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("/usr/bin/security", "delete-keychain", path).Run() })
	k := Keychain{Service: "Irie test vault", Account: "sidebar", Path: path}
	if _, err := k.Get(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("before: %v", err)
	}
	key := newKey()
	if err := k.Set(key); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get(); err != nil || !bytes.Equal(got, key) {
		t.Errorf("get %x %v", got, err)
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
store) shift; label=$1; shift; cat > "$STORE.$*" ;;
lookup) shift; [ -n "$DOWN" ] && { echo "Cannot autolaunch D-Bus" >&2; exit 1; }; [ -f "$STORE.$*" ] || exit 1; cat "$STORE.$*" ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STORE", filepath.Join(dir, "kept"))
	s := SecretService{Bin: bin, Service: "Irie test vault", Account: "sidebar"}
	if _, err := s.Get(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("before: %v", err)
	}
	key := newKey()
	if err := s.Set(key); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(); err != nil || !bytes.Equal(got, key) {
		t.Errorf("get %x %v", got, err)
	}
	kept, _ := filepath.Glob(filepath.Join(dir, "kept.*"))
	if len(kept) != 1 || !strings.Contains(kept[0], "service Irie test vault account sidebar") {
		t.Errorf("kept %v", kept)
	}
	t.Setenv("DOWN", "1")
	if _, err := s.Get(); err == nil || errors.Is(err, ErrNoKey) || !strings.Contains(err.Error(), "D-Bus") {
		t.Errorf("down: %v", err)
	}
}

// A key file gone from beside sealed records is not made again, which
// would open none of them; a key in the file that fails its check is
// refused.
func TestAVaultNeverMakesANewKeyOverSealedRecords(t *testing.T) {
	dir := t.TempDir()
	v, err := OpenVault(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, Store{Dir: dir, Vault: v}, map[string]any{"n": 1})
	keyPath := filepath.Join(dir, KeyFile)
	kf, _ := os.ReadFile(keyPath)
	os.Remove(keyPath)
	if _, err := OpenVault(dir, nil); !errors.Is(err, ErrKeyGone) {
		t.Errorf("key file gone: %v", err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Error("a new key file was made")
	}
	// Another key in the file, its check left as it was.
	var file keyFile
	if err := json.Unmarshal(kf, &file); err != nil {
		t.Fatal(err)
	}
	file.Key = base64.StdEncoding.EncodeToString(newKey())
	bad, _ := json.Marshal(file)
	os.WriteFile(keyPath, bad, 0o600)
	if _, err := OpenVault(dir, nil); !errors.Is(err, ErrWrongKey) {
		t.Errorf("a key that fails its check: %v", err)
	}
}

// A Keychain that can't be asked now is out of reach, never a key lost:
// locked where no one may be asked (36), not there (50), refused (51) or
// cancelled (128), or a session with no login keychain. Only "not found"
// (44) is no key, and a chain sealed under it then reads as its key gone.
// A first open while it is out of reach makes no key, there or anywhere.
func TestAKeychainOutOfReachIsNeverAKeyLost(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "security")
	script := `#!/bin/sh
case "$1" in
login-keychain)
	[ -n "$NO_LOGIN" ] && { echo "security: SecKeychainCopyLogin: The specified keychain could not be found." >&2; exit 50; }
	echo '    "/home/alice/Library/Keychains/login.keychain-db"' ;;
find-generic-password)
	[ "$FAKE_EXIT" = 0 ] && { echo 00; exit 0; }
	[ "$FAKE_EXIT" = 44 ] || echo "security: SecKeychainSearchCopyNext: $FAKE_WORDS" >&2
	exit "$FAKE_EXIT" ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	k := Keychain{Service: "Irie test vault", Account: "sidebar", Bin: bin}
	sealed := filepath.Join(dir, "chain")
	if _, err := OpenVault(sealed, &memStore{kind: KeptInKeychain}); err != nil {
		t.Fatal(err)
	}
	kf, _ := os.ReadFile(filepath.Join(sealed, KeyFile))
	for code, words := range map[string]string{
		"36": "User interaction is not allowed.", "50": "The specified keychain could not be found.",
		"51": "The user name or passphrase you entered is not correct.", "128": "User canceled the operation.",
	} {
		t.Setenv("FAKE_EXIT", code)
		t.Setenv("FAKE_WORDS", words)
		if _, err := k.Get(); !errors.Is(err, ErrNoStore) || !strings.Contains(err.Error(), words) {
			t.Errorf("exit %s: %v", code, err)
		}
		_, err := OpenVault(sealed, k)
		if !errors.Is(err, ErrNoStore) || errors.Is(err, ErrKeyGone) || errors.Is(err, ErrWrongKey) {
			t.Errorf("exit %s, a chain sealed under it: %v", code, err)
		}
		fresh := filepath.Join(t.TempDir(), "chain")
		if _, err := OpenVault(fresh, k); !errors.Is(err, ErrNoStore) {
			t.Errorf("exit %s, a first open: %v", code, err)
		}
		if _, err := os.Stat(filepath.Join(fresh, KeyFile)); !os.IsNotExist(err) {
			t.Errorf("exit %s: a key file was made while the store was out of reach", code)
		}
	}
	if now, _ := os.ReadFile(filepath.Join(sealed, KeyFile)); !bytes.Equal(now, kf) {
		t.Error("the key file changed")
	}
	t.Setenv("FAKE_EXIT", "44")
	if _, err := k.Get(); !errors.Is(err, ErrNoKey) {
		t.Errorf("not found: %v", err)
	}
	if _, err := OpenVault(sealed, k); !errors.Is(err, ErrKeyGone) {
		t.Errorf("not found, a chain sealed under it: %v", err)
	}
	t.Setenv("NO_LOGIN", "1")
	if _, err := k.Get(); !errors.Is(err, ErrNoStore) || errors.Is(err, ErrNoKey) {
		t.Errorf("no login keychain: %v", err)
	}
}
