// Package chain keeps versions of a document as a chain, as git keeps
// commits (docs/proposals/zen.md §4, local-chain.md). A version is
// canonical JSON, by the JSON Canonicalization Scheme (RFC 8785), named by
// the SHA-512 of its bytes (FIPS 180-4) as an OCI-style digest: "sha512:"
// and 128 hex digits. Each version names the one before it in its "parent",
// so the newest names the whole history. A Store keeps the versions in a
// folder, one file each, checked against its name whenever it is read, and
// HEAD, which names the newest; with a Vault, each sealed at rest
// (vault.go).
//
// Only public standards shape the format, so any tool with a JCS library
// and SHA-512 can check a version without Irie.
package chain

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/irie-people/irie/internal/filetx"
)

// Prefix starts every name: the hash it was made with, as an OCI digest
// says.
const Prefix = "sha512:"

// MaxInt is the largest integer a canonical document holds. RFC 8785 writes
// numbers as an IEEE double prints, and every double prints every integer
// up to 2^53 the same way; past it two writers could disagree.
const MaxInt = 1<<53 - 1

// ShortLen is how many hex digits a short name keeps, as git's short hashes
// do.
const ShortLen = 12

// Canonical is v as RFC 8785 canonical JSON: as encoding/json writes v, with
// every object's keys sorted by their UTF-16 code units, no whitespace,
// strings with only the escapes JSON requires, and numbers only as
// integers within ±MaxInt. A float or a larger number is refused, rather
// than written in a form another writer might not match.
func Canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var x any
	if err := d.Decode(&x); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := write(&b, x); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func write(b *bytes.Buffer, x any) error {
	switch x := x.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case json.Number:
		n, err := strconv.ParseInt(string(x), 10, 64)
		if err != nil || n > MaxInt || n < -MaxInt {
			return fmt.Errorf("chain: %s is not a whole number within ±2^53", x)
		}
		b.WriteString(strconv.FormatInt(n, 10))
	case string:
		quote(b, x)
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := write(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b string) int {
			return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
		})
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			quote(b, k)
			b.WriteByte(':')
			if err := write(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("chain: cannot write a %T", x)
	}
	return nil
}

// quote writes s as RFC 8785 §3.2.2.2 does: the quote and the backslash
// escaped, the controls with a short escape where JSON has one and \u00xx
// in lower case where it hasn't, and every other character as itself.
func quote(b *bytes.Buffer, s string) {
	const hexDigits = "0123456789abcdef"
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[r>>4])
				b.WriteByte(hexDigits[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// Name names canonical bytes: "sha512:" and the hex of their SHA-512.
func Name(b []byte) string {
	sum := sha512.Sum512(b)
	return Prefix + hex.EncodeToString(sum[:])
}

// Valid says whether s is a name: the prefix and 128 lower-case hex digits.
func Valid(s string) bool {
	h, ok := strings.CutPrefix(s, Prefix)
	if !ok || len(h) != 2*sha512.Size {
		return false
	}
	for _, c := range h {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

// Short is a name's first ShortLen hex digits, for people to read; a short
// name finds its version as long as no other starts the same (Find).
func Short(name string) string {
	h := strings.TrimPrefix(name, Prefix)
	if len(h) > ShortLen {
		h = h[:ShortLen]
	}
	return h
}

// Parent is the name a version's document gives as its "parent", or "" for
// the first version.
func Parent(b []byte) (string, error) {
	var v struct {
		Parent string `json:"parent"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	if v.Parent != "" && !Valid(v.Parent) {
		return "", fmt.Errorf("chain: the parent %q is not a version's name", v.Parent)
	}
	return v.Parent, nil
}

// ErrNone says that no version has the name asked for.
var ErrNone = errors.New("chain: no such version")

// Store keeps versions in its folder: objects/<hex>.json, each a version's
// canonical bytes, and HEAD, the newest's name. One process writes it, the
// core; each file is put in place whole, so a reader never sees half of
// one. With a Vault, each version is sealed instead, as
// objects/<hex>.sealed (vault.go), and only sealed ones count: MoveIn
// seals the plain ones a store kept before.
type Store struct {
	Dir   string
	Vault *Vault
}

// ext ends the store's records' file names.
func (s Store) ext() string {
	if s.Vault != nil {
		return SealedExt
	}
	return ".json"
}

func (s Store) object(name string) string {
	return filepath.Join(s.Dir, "objects", strings.TrimPrefix(name, Prefix)+s.ext())
}

// plain is where the plain record of the version named is, or was.
func (s Store) plain(name string) string {
	return filepath.Join(s.Dir, "objects", strings.TrimPrefix(name, Prefix)+".json")
}

// Put keeps canonical bytes under their name and returns it. A version
// that is there already is left as it is. A sealed record is written once
// under a name its contents decide, so it takes no lock.
func (s Store) Put(b []byte) (string, error) {
	name := Name(b)
	path := s.object(name)
	if _, err := os.Stat(path); err == nil {
		return name, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if s.Vault != nil {
		return name, filetx.WriteOnce(path, s.Vault.Seal(name, b), 0o600)
	}
	return name, filetx.WriteFile(path, b, 0o600)
}

// Get reads the version named, and refuses bytes that its name does not
// match, as when a file was changed by hand; with a vault, a record that
// does not open under its key as that name.
func (s Store) Get(name string) ([]byte, error) {
	if !Valid(name) {
		return nil, fmt.Errorf("chain: %q is not a version's name", name)
	}
	b, err := os.ReadFile(s.object(name))
	if errors.Is(err, os.ErrNotExist) {
		if _, perr := os.Stat(s.plain(name)); s.Vault != nil && perr == nil {
			return nil, fmt.Errorf("chain: version %s is not sealed: its plain record stayed out of the vault, as it did not match its name", Short(name))
		}
		return nil, fmt.Errorf("%w: %s", ErrNone, Short(name))
	}
	if err != nil {
		return nil, err
	}
	if s.Vault != nil {
		if b, err = s.Vault.Open(name, b); err != nil {
			return nil, fmt.Errorf("chain: version %s %w", Short(name), err)
		}
	}
	if Name(b) != name {
		return nil, fmt.Errorf("chain: version %s does not match its name: it was changed after it was written", Short(name))
	}
	return b, nil
}

// Head names the newest version, or is "" before the first.
func (s Store) Head() (string, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, "HEAD"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(b))
	if !Valid(name) {
		return "", fmt.Errorf("chain: HEAD holds %q, not a version's name", name)
	}
	return name, nil
}

// SetHead makes the version named the newest.
func (s Store) SetHead(name string) error {
	if !Valid(name) {
		return fmt.Errorf("chain: %q is not a version's name", name)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(filepath.Join(s.Dir, "HEAD"), []byte(name+"\n"), 0o600)
}

// Entry is a version as Log reads it: its name and its bytes.
type Entry struct {
	Name  string
	Bytes []byte
}

// Log walks back from the version named, newest first, through at most n
// versions, or all of them when n is 0 or less. A version whose parent is
// missing ends the walk with an error, after the versions before it.
func (s Store) Log(name string, n int) ([]Entry, error) {
	var out []Entry
	seen := map[string]bool{}
	for name != "" && (n <= 0 || len(out) < n) {
		if seen[name] {
			return out, fmt.Errorf("chain: version %s comes round again", Short(name))
		}
		seen[name] = true
		b, err := s.Get(name)
		if err != nil {
			return out, err
		}
		out = append(out, Entry{Name: name, Bytes: b})
		if name, err = Parent(b); err != nil {
			return out, err
		}
	}
	return out, nil
}

// Find names the one version whose hex starts with prefix, with or without
// "sha512:", as git takes the start of a hash. It needs at least six
// digits, and refuses a prefix two versions share.
func (s Store) Find(prefix string) (string, error) {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(prefix), Prefix))
	if len(h) < 6 || len(h) > 2*sha512.Size || strings.Trim(h, "0123456789abcdef") != "" {
		return "", fmt.Errorf("chain: %q: give at least six hex digits of a version's name", prefix)
	}
	if len(h) == 2*sha512.Size {
		if _, err := os.Stat(s.object(Prefix + h)); err != nil {
			return "", fmt.Errorf("%w: %s", ErrNone, Short(h))
		}
		return Prefix + h, nil
	}
	files, err := os.ReadDir(filepath.Join(s.Dir, "objects"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var found []string
	for _, f := range files {
		name, ok := strings.CutSuffix(f.Name(), s.ext())
		if ok && strings.HasPrefix(name, h) && Valid(Prefix+name) {
			found = append(found, Prefix+name)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("%w: %s", ErrNone, h)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("chain: %s starts %d versions' names; give more of it", h, len(found))
}
