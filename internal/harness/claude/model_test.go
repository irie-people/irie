package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The settings as Claude Code writes them, with the user's default model.
const savedSettings = `{
  "env": {
    "model": "not this one"
  },
  "model": "fable[1m]",
  "theme": "dark"
}
`

func settingsDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readSettings(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSavedModel(t *testing.T) {
	for body, want := range map[string]string{
		savedSettings:                         `"fable[1m]"`,
		`{"theme":"dark"}`:                    ``,
		`{"model":"opus","model":"sonnet"}`:   `"sonnet"`,
		`{ "model" :  "opus" , "x": [1, 2] }`: `"opus"`,
	} {
		got, err := SavedModel(settingsDir(t, body))
		if err != nil || string(got) != want {
			t.Errorf("%s: %s, %v; want %s", body, got, err, want)
		}
	}
	if got, err := SavedModel(t.TempDir()); err != nil || got != nil {
		t.Errorf("with no settings: %s, %v", got, err)
	}
	if _, err := SavedModel(settingsDir(t, `["model"]`)); err == nil {
		t.Error("settings that are no object read")
	}
}

// /model saves its model as the default; the switch puts the user's own
// back as it was, the rest of the file byte for byte, and its mode.
func TestRestoreModelPutsTheUsersDefaultBack(t *testing.T) {
	dir := settingsDir(t, savedSettings)
	was, _ := SavedModel(dir)
	names := []string{"claude-opus-5-5"}

	// Not saved yet: it waits.
	if put, settled, err := RestoreModel(dir, was, names); put || settled || err != nil {
		t.Errorf("before the save: put %v, settled %v, %v", put, settled, err)
	}
	saved := `{
  "env": {
    "model": "not this one"
  },
  "model": "claude-opus-5-5",
  "theme": "dark"
}
`
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(saved), 0o640)
	if put, settled, err := RestoreModel(dir, was, names); !put || !settled || err != nil {
		t.Errorf("after the save: put %v, settled %v, %v", put, settled, err)
	}
	if got := readSettings(t, dir); got != savedSettings {
		t.Errorf("after the restore:\n%s\nwant\n%s", got, savedSettings)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "settings.json")); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	// Someone else's default stays.
	other := `{"model": "claude-sonnet-5-5"}`
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(other), 0o640)
	if put, settled, err := RestoreModel(dir, was, names); put || !settled || err != nil {
		t.Errorf("someone else's default: put %v, settled %v, %v", put, settled, err)
	}
	if got := readSettings(t, dir); got != other {
		t.Errorf("someone else's default became %s", got)
	}
}

// A user with no default has none again, with the commas right wherever
// the key was.
func TestRestoreModelTakesOutADefaultTheUserNeverHad(t *testing.T) {
	for saved, want := range map[string]string{
		"{\n  \"model\": \"claude-opus-5-5\"\n}\n":                                "{\n}\n",
		"{\n  \"model\": \"claude-opus-5-5\",\n  \"theme\": \"dark\"\n}\n":        "{\n  \"theme\": \"dark\"\n}\n",
		"{\n  \"theme\": \"dark\",\n  \"model\": \"claude-opus-5-5\"\n}\n":        "{\n  \"theme\": \"dark\"\n}\n",
		"{\n  \"a\": 1,\n  \"model\": \"claude-opus-5-5\",\n  \"b\": [2, 3]\n}\n": "{\n  \"a\": 1,\n  \"b\": [2, 3]\n}\n",
	} {
		dir := settingsDir(t, saved)
		if put, _, err := RestoreModel(dir, nil, []string{"claude-opus-5-5"}); !put || err != nil {
			t.Errorf("%q: put %v, %v", saved, put, err)
		}
		got := readSettings(t, dir)
		if got != want {
			t.Errorf("%q became %q, want %q", saved, got, want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("%q is not JSON", got)
		}
	}
	// With no file, nothing was saved and nothing waits.
	if put, settled, err := RestoreModel(t.TempDir(), nil, []string{"opus"}); put || !settled || err != nil {
		t.Errorf("no file: put %v, settled %v, %v", put, settled, err)
	}
}

// The settings are often a link into the user's dotfiles: the link stays.
func TestRestoreModelKeepsALink(t *testing.T) {
	real := settingsDir(t, `{"model": "claude-opus-5-5"}`)
	dir := t.TempDir()
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(filepath.Join(real, "settings.json"), link); err != nil {
		t.Fatal(err)
	}
	if put, _, err := RestoreModel(dir, json.RawMessage(`"fable[1m]"`), []string{"claude-opus-5-5"}); !put || err != nil {
		t.Fatalf("put %v, %v", put, err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link is gone: %v, %v", fi, err)
	}
	if got := readSettings(t, real); got != `{"model": "fable[1m]"}` {
		t.Errorf("the linked file: %s", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left beside the link: %v", entries)
	}
}

func TestSeesSwitches(t *testing.T) {
	now, err := Settings("/home/alice/.local/bin/irie", 0, true, Extra{})
	if err != nil {
		t.Fatal(err)
	}
	old, err := Settings("/home/alice/.local/bin/irie", 0, false, Extra{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, c := range map[string]struct {
		body []byte
		want bool
	}{
		"now": {now, true}, "older": {old, false}, "broken": {[]byte("{"), false},
	} {
		path := filepath.Join(dir, name+".json")
		os.WriteFile(path, c.body, 0o600)
		if got := SeesSwitches(path); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
	if SeesSwitches(filepath.Join(dir, "none.json")) {
		t.Error("a session with no settings sees switches")
	}
}
