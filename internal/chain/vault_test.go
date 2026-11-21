package chain

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

// sealedStore is a store in a new folder with its vault open under kek.
func sealedStore(t *testing.T, dir string, kek []byte) Store {
	t.Helper()
	v, err := OpenVault(dir, kek)
	if err != nil {
		t.Fatal(err)
	}
	return Store{Dir: dir, Vault: v}
}

// everything is every file's bytes under dir.
func everything(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(path)
			out[path] = b
		}
		return nil
	})
	return out
}

// Nothing on disk reads without the system's key: no version's bytes, no
// word of one, and neither key, while the vault gives every version back.
func TestNothingInAVaultReadsWithoutItsKey(t *testing.T) {
	dir := t.TempDir()
	kek := key(7)
	s := sealedStore(t, dir, kek)
	a := put(t, s, map[string]any{"why": "Edge to Edge after a long night", "n": 1})
	b := put(t, s, map[string]any{"why": "Text at 18 pt", "n": 2, "parent": a})
	if err := s.SetHead(b); err != nil {
		t.Fatal(err)
	}
	da, _ := Canonical(map[string]any{"why": "Edge to Edge after a long night", "n": 1})
	for path, data := range everything(t, dir) {
		for _, secret := range [][]byte{da, []byte("long night"), []byte("18 pt"), []byte(`"why"`), kek} {
			if bytes.Contains(data, secret) {
				t.Errorf("%s holds %q", path, secret)
			}
		}
	}
	if _, err := os.Stat(s.object(a, plainExt)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a plain copy: %v", err)
	}
	log, err := s.Log(b, 0)
	if err != nil || len(log) != 2 || log[1].Name != a || !bytes.Equal(log[1].Bytes, da) {
		t.Fatalf("log = %v, %v", log, err)
	}
	if got, err := s.Find(Short(a)); err != nil || got != a {
		t.Errorf("find = %q, %v", got, err)
	}
	if got, err := s.Find(a); err != nil || got != a {
		t.Errorf("find whole = %q, %v", got, err)
	}

	// The same vault, opened again with the same key, reads it all.
	again := sealedStore(t, dir, kek)
	if got, err := again.Get(a); err != nil || !bytes.Equal(got, da) {
		t.Errorf("reopened: %q, %v", got, err)
	}

	// With no vault open, nothing reads, and nothing is written plain.
	bare := Store{Dir: dir}
	if _, err := bare.Get(a); !errors.Is(err, ErrLocked) {
		t.Errorf("a locked get: %v", err)
	}
	if _, err := bare.Put([]byte(`{"n":3}`)); !errors.Is(err, ErrLocked) {
		t.Errorf("a plain put beside the vault: %v", err)
	}

	// Another key opens nothing, and replaces nothing.
	if _, err := OpenVault(dir, key(8)); !errors.Is(err, ErrWrongKey) {
		t.Errorf("another key: %v", err)
	}
	if got, err := sealedStore(t, dir, kek).Get(b); err != nil || len(got) == 0 {
		t.Errorf("after another key was tried: %v", err)
	}
	// Nor does a vault whose data key is gone.
	os.Rename(filepath.Join(dir, "key"), filepath.Join(dir, "key.gone"))
	if _, err := OpenVault(dir, kek); !errors.Is(err, ErrWrongKey) {
		t.Errorf("no data key: %v", err)
	}
}

// A sealed version changed on disk, or put in another's place, doesn't
// open.
func TestASealedVersionIsBoundToItsName(t *testing.T) {
	dir := t.TempDir()
	s := sealedStore(t, dir, key(1))
	a := put(t, s, map[string]any{"n": 1})
	b := put(t, s, map[string]any{"n": 2})
	ab, _ := os.ReadFile(s.object(a, sealedExt))
	if err := os.WriteFile(s.object(b, sealedExt), ab, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(b); err == nil || !strings.Contains(err.Error(), "doesn't open") {
		t.Errorf("a's record in b's place: %v", err)
	}
	ab[len(ab)-1] ^= 1
	os.WriteFile(s.object(a, sealedExt), ab, 0o600)
	if _, err := s.Get(a); err == nil {
		t.Error("a changed record opened")
	}
	ab[0] = 9
	if _, err := unseal(s.Vault.aead, ab, a); err == nil {
		t.Error("a record of another form opened")
	}
}

// A store kept before its vault moves in whole: each version sealed, its
// plain file gone, its head as it was. One changed by hand stays out.
func TestPlainVersionsMoveIntoTheVault(t *testing.T) {
	dir := t.TempDir()
	plain := Store{Dir: dir}
	a := put(t, plain, map[string]any{"n": 1})
	b := put(t, plain, map[string]any{"n": 2, "parent": a})
	c := put(t, plain, map[string]any{"n": 3, "parent": b})
	plain.SetHead(c)
	os.WriteFile(plain.object(c, plainExt), []byte(`{"n":9}`), 0o600)

	s := sealedStore(t, dir, key(2))
	n, err := s.Seal()
	if n != 2 || err == nil || !strings.Contains(err.Error(), Short(c)) {
		t.Fatalf("seal = %d, %v; want 2 and an error naming %s", n, err, Short(c))
	}
	for _, name := range []string{a, b} {
		if _, err := os.Stat(s.object(name, plainExt)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is still plain: %v", Short(name), err)
		}
		if _, err := os.Stat(filepath.Join(dir, "objects", "."+strings.TrimPrefix(name, Prefix)+plainExt+".lock")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s's lock stayed: %v", Short(name), err)
		}
	}
	if head, _ := s.Head(); head != c {
		t.Errorf("head = %s", head)
	}
	if log, err := s.Log(b, 0); err != nil || len(log) != 2 {
		t.Errorf("log = %v, %v", log, err)
	}
	if n, err := s.Seal(); n != 0 || err == nil {
		t.Errorf("sealing again = %d, %v", n, err)
	}
	if _, err := (Store{Dir: dir}).Seal(); !errors.Is(err, ErrLocked) {
		t.Errorf("sealing with no vault: %v", err)
	}
}

// A data key that locks nothing yet, under a key the system lost, is made
// again rather than leaving the store locked for good.
func TestAVaultThatLocksNothingStartsAgain(t *testing.T) {
	dir := t.TempDir()
	sealedStore(t, dir, key(3))
	s := sealedStore(t, dir, key(4))
	a := put(t, s, map[string]any{"n": 1})
	if _, err := sealedStore(t, dir, key(4)).Get(a); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVault(dir, key(3)); !errors.Is(err, ErrWrongKey) {
		t.Errorf("the lost key, once something is sealed: %v", err)
	}
	if _, err := OpenVault(t.TempDir(), []byte("short")); err == nil {
		t.Error("a short key was taken")
	}
}
