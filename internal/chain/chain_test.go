package chain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RFC 8785 §3.2.3's example: keys sort by UTF-16 code units, so the emoji,
// a surrogate pair from 0xD83D, comes before U+FB33, though its code point
// is larger; and its strings keep every character but the controls as it
// is.
func TestCanonicalSortsAsRFC8785(t *testing.T) {
	in := "{\"\\u20ac\":\"Euro Sign\",\"\\r\":\"Carriage Return\",\"\\ufb33\":\"Hebrew Letter Dalet With Dagesh\",\"1\":\"One\"," +
		"\"\\ud83d\\ude00\":\"Emoji: Grinning Face\",\"\\u0080\":\"Control\",\"\\u00f6\":\"Latin Small Letter O With Diaeresis\"}"
	var v map[string]any
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"\\r\":\"Carriage Return\",\"1\":\"One\",\"\u0080\":\"Control\",\"ö\":\"Latin Small Letter O With Diaeresis\"," +
		"\"€\":\"Euro Sign\",\"😀\":\"Emoji: Grinning Face\",\"\ufb33\":\"Hebrew Letter Dalet With Dagesh\"}"
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestCanonicalEscapesOnlyWhatJSONMust(t *testing.T) {
	got, err := Canonical(map[string]any{"s": "a\"b\\c\n\t\x01\x1f</>&\u2028é", "n": -0, "z": []any{true, nil, 12}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"n":0,"s":"a\"b\\c\n\t\u0001\u001f</>&` + "\u2028" + `é","z":[true,null,12]}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A struct's fields come out sorted, whatever order Go declares them in.
func TestCanonicalSortsAStructsFields(t *testing.T) {
	type doc struct {
		Zed   int    `json:"zed"`
		Alpha string `json:"alpha"`
		Mid   bool   `json:"mid,omitempty"`
	}
	got, err := Canonical(doc{Zed: 3, Alpha: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"alpha":"a","zed":3}` {
		t.Errorf("got %s", got)
	}
}

func TestCanonicalRefusesWhatWritersMightNotMatch(t *testing.T) {
	for _, v := range []any{1.5, map[string]any{"big": int64(MaxInt + 1)}, []any{-int64(MaxInt) - 1}, 1e300} {
		if b, err := Canonical(v); err == nil {
			t.Errorf("%v: got %s, want an error", v, b)
		}
	}
	if b, err := Canonical(int64(MaxInt)); err != nil || string(b) != "9007199254740991" {
		t.Errorf("MaxInt: %s %v", b, err)
	}
}

// FIPS 180-2's example for SHA-512, "abc".
func TestNameIsSHA512(t *testing.T) {
	want := Prefix + "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a" +
		"2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"
	if got := Name([]byte("abc")); got != want {
		t.Errorf("got %s", got)
	}
	if !Valid(want) || Valid(strings.ToUpper(want)) || Valid(want[:20]) || Valid("sha256:"+want[7:]) {
		t.Error("Valid")
	}
	if Short(want) != "ddaf35a19361" {
		t.Errorf("Short: %s", Short(want))
	}
}

func put(t *testing.T, s Store, doc map[string]any) string {
	t.Helper()
	b, err := Canonical(doc)
	if err != nil {
		t.Fatal(err)
	}
	name, err := s.Put(b)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestStoreKeepsAChain(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "zen")}
	if h, err := s.Head(); err != nil || h != "" {
		t.Fatalf("empty head: %q %v", h, err)
	}
	a := put(t, s, map[string]any{"n": 1})
	b := put(t, s, map[string]any{"n": 2, "parent": a})
	c := put(t, s, map[string]any{"n": 3, "parent": b})
	if again := put(t, s, map[string]any{"parent": b, "n": 3}); again != c {
		t.Error("the same document had another name")
	}
	if err := s.SetHead(c); err != nil {
		t.Fatal(err)
	}
	head, err := s.Head()
	if err != nil || head != c {
		t.Fatalf("head %q %v", head, err)
	}
	log, err := s.Log(head, 0)
	if err != nil || len(log) != 3 || log[0].Name != c || log[2].Name != a {
		t.Fatalf("log %v %v", log, err)
	}
	if two, _ := s.Log(head, 2); len(two) != 2 {
		t.Errorf("log of 2: %d", len(two))
	}
	if got, err := s.Find(Short(b)); err != nil || got != b {
		t.Errorf("find %q %v", got, err)
	}
	if got, err := s.Find(b); err != nil || got != b {
		t.Errorf("find whole %q %v", got, err)
	}
	if _, err := s.Find("abc"); err == nil {
		t.Error("a short prefix was taken")
	}
	if _, err := s.Find("000000"); !errors.Is(err, ErrNone) {
		t.Errorf("a missing one: %v", err)
	}
	info, err := os.Stat(s.object(a, plainExt))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v %v", info, err)
	}
}

// A version changed on disk after it was written is refused, and so is the
// walk through it.
func TestStoreRefusesAChangedVersion(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a := put(t, s, map[string]any{"n": 1})
	b := put(t, s, map[string]any{"n": 2, "parent": a})
	if err := os.WriteFile(s.object(a, plainExt), []byte(`{"n":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a); err == nil || !strings.Contains(err.Error(), "changed after it was written") {
		t.Errorf("get: %v", err)
	}
	log, err := s.Log(b, 0)
	if err == nil || len(log) != 1 {
		t.Errorf("log: %d %v", len(log), err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "HEAD"), []byte("nonsense\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Head(); err == nil {
		t.Error("a bad HEAD was taken")
	}
}
