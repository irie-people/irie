package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// A file as people write them: comments above and beside keys, blank lines,
// an inline table and a profile of their own.
const commented = `# @irie finds the repo by name.
[repos.aliases]
irie = "~/code/irie"

# Build sessions, without the skill listing.
[profiles.clean]
harness = "claude-code"
model = "claude-opus-5-5"   # the strongest
args = ["--disable-slash-commands"]

# Every task runs on +claude.
[spawn]
default_profile = "claude"
close_when_done = false

# Orchestrators stay on Opus.
[route]
failover = false

[dog]
land = { irie = "live" }
`

func apply(t *testing.T, data string, edits ...Edit) string {
	t.Helper()
	out, err := Apply([]byte(data), edits)
	if err != nil {
		t.Fatalf("Apply(%v): %v", edits, err)
	}
	if _, err := Parse(out, "config.toml"); err != nil {
		t.Fatalf("the result does not load: %v\n%s", err, out)
	}
	return string(out)
}

func TestSetChangesOnlyTheValue(t *testing.T) {
	got := apply(t, commented, Edit{"spawn.default_profile", "auto"}, Edit{"profiles.clean.model", "claude-sonnet-5-5"})
	want := strings.Replace(commented, `default_profile = "claude"`, `default_profile = "auto"`, 1)
	want = strings.Replace(want, `model = "claude-opus-5-5"   # the strongest`, `model = "claude-sonnet-5-5"   # the strongest`, 1)
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestANewKeyJoinsItsTable(t *testing.T) {
	got := apply(t, commented, Edit{"spawn.close_delay", time.Minute}, Edit{"route.min_accuracy", 0.9})
	want := strings.Replace(commented, "close_when_done = false\n", "close_when_done = false\nclose_delay = \"1m\"\n", 1)
	want = strings.Replace(want, "failover = false\n", "failover = false\nmin_accuracy = 0.9\n", 1)
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestANewTableGoesAtTheEnd(t *testing.T) {
	got := apply(t, commented, Edit{"ui.red_on_done", true}, Edit{"profiles.claude.effort", "high"})
	want := commented + "\n[ui]\nred_on_done = true\n\n[profiles.claude]\neffort = \"high\"\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	c, err := Parse([]byte(got), "config.toml")
	if err != nil {
		t.Fatal(err)
	}
	// A new profile table keeps the built-in profile's other keys.
	if p := c.Profiles["claude"]; p.Effort != "high" || p.Model != "claude-opus-5-5" {
		t.Errorf("profiles.claude = %+v", p)
	}
}

func TestAnEmptyFileGetsItsFirstTable(t *testing.T) {
	if got, want := apply(t, "", Edit{"spawn.close_when_done", true}), "[spawn]\nclose_when_done = true\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A last line without its newline gets one before the new key.
	if got, want := apply(t, "[spawn]\nclose_when_done = true", Edit{"spawn.close_delay", 10 * time.Second}), "[spawn]\nclose_when_done = true\nclose_delay = \"10s\"\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnInlineTableIsWrittenAgain(t *testing.T) {
	got := apply(t, commented, Edit{"dog.land.web", "main"})
	if want := strings.Replace(commented, `land = { irie = "live" }`, `land = { irie = "live", web = "main" }`, 1); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	got = apply(t, commented, Edit{"dog.land.irie", nil})
	if want := strings.Replace(commented, `land = { irie = "live" }`, `land = {}`, 1); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUnsetTakesTheLineOut(t *testing.T) {
	got := apply(t, commented, Edit{"spawn.close_when_done", nil}, Edit{"ui.theme", nil})
	if want := strings.Replace(commented, "close_when_done = false\n", "", 1); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestDottedKeysStayDotted(t *testing.T) {
	const file = "spawn.default_profile = \"claude\" # mine\n\n[ui]\nred_on_done = true\n"
	got := apply(t, file, Edit{"spawn.default_profile", "sonnet"}, Edit{"spawn.close_when_done", true})
	if want := "spawn.default_profile = \"sonnet\" # mine\nspawn.close_when_done = true\n\n[ui]\nred_on_done = true\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestValuesAreWrittenAsTOML(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value any
		want  string
	}{
		{"route.min_accuracy", 1.0, "min_accuracy = 1.0"},
		{"route.min_accuracy", 0.85, "min_accuracy = 0.85"},
		{"local.memory_gib", 12, "memory_gib = 12"},
		{"hold.max", 24 * time.Hour, `max = "24h"`},
		{"spawn.close_delay", 90 * time.Second, `close_delay = "1m30s"`},
		{"repos.roots", []string{"~/code", "/srv/git"}, `roots = ["~/code", "/srv/git"]`},
		{"repos.roots", []string{}, `roots = []`},
		{"local.env", map[string]string{"OLLAMA_MODELS": "/data/models", "B": "x"}, `env = { B = "x", OLLAMA_MODELS = "/data/models" }`},
		{"dog.land", map[string]string{"~/code/odd name": "main"}, `land = { "~/code/odd name" = "main" }`},
		{"profiles.clean.cost", "say \"hi\" \\ \t", `cost = "say \"hi\" \\ \t"`},
	} {
		got := apply(t, "", Edit{tc.key, tc.value})
		if !strings.Contains(got, "\n"+tc.want+"\n") {
			t.Errorf("%s = %#v: got\n%s\nwant a line %s", tc.key, tc.value, got, tc.want)
		}
	}
}

func TestLongValuesDoNotConfuseIt(t *testing.T) {
	const file = `[profiles.aider]
harness = "generic"
command = "aider"
args = [
  "--model", # which one
  "ollama_chat/gpt-oss:20b",
]

[profiles.aider.env]
NOTE = """
[spawn]
default_profile = "trap"
"""

[spawn]
close_when_done = true
`
	got := apply(t, file, Edit{"spawn.default_profile", "aider"}, Edit{"profiles.aider.task", "paste"})
	want := strings.Replace(file, "close_when_done = true\n", "close_when_done = true\ndefault_profile = \"aider\"\n", 1)
	want = strings.Replace(want, "  \"ollama_chat/gpt-oss:20b\",\n]\n", "  \"ollama_chat/gpt-oss:20b\",\n]\ntask = \"paste\"\n", 1)
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestWhatItCannotEditIsRefused(t *testing.T) {
	// A table made of an array of tables is beyond it.
	if _, err := Apply([]byte("[[spawn]]\nclose_when_done = true\n"), []Edit{{"spawn.close_delay", time.Second}}); err == nil {
		t.Error("an array of tables was edited")
	}
	if _, err := Apply([]byte("[spawn\n"), []Edit{{"spawn.close_when_done", true}}); err == nil {
		t.Error("a file that does not parse was edited")
	}
	if _, err := Apply([]byte(commented), []Edit{{"spawn.default_profile.x", "y"}}); err == nil {
		t.Error("a key under a string was set")
	}
	if _, err := Apply([]byte(commented), []Edit{{"no such", "y"}}); err == nil {
		t.Error("a key with a space was set")
	}
}

func TestParseValueTakesTheKeysType(t *testing.T) {
	for _, tc := range []struct {
		key, json string
		want      any
	}{
		{"spawn.default_profile", `"claude"`, "claude"},
		{"spawn.close_when_done", `true`, true},
		{"spawn.close_delay", `"45s"`, 45 * time.Second},
		{"route.min_accuracy", `1`, 1.0},
		{"local.port", `11436`, 11436},
		{"repos.roots", `["~/code"]`, []string{"~/code"}},
		{"dog.land", `{"irie": "live"}`, map[string]string{"irie": "live"}},
		{"profiles.new.model", `"claude-haiku-5-5"`, "claude-haiku-5-5"},
		{"profiles.local.local.min_context", `32768`, 32768},
		{"spawn.close_delay", `null`, nil},
		{"spawn.close_delay", ``, nil},
	} {
		got, err := ParseValue(tc.key, []byte(tc.json))
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseValue(%s, %s) = %#v, %v; want %#v", tc.key, tc.json, got, err, tc.want)
		}
	}
	for _, tc := range []struct{ key, json, want string }{
		{"spawn.nope", `1`, `unknown key "spawn.nope"`},
		{"spawn.close_when_done", `"yes"`, "spawn.close_when_done takes true or false"},
		{"spawn.close_delay", `30`, "takes a duration"},
		{"route.accuracy", `{}`, "set in the file by hand"},
		{"spawn.default_profile.x", `"y"`, "spawn.default_profile is not a table"},
	} {
		if _, err := ParseValue(tc.key, []byte(tc.json)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseValue(%s, %s): %v, want %q", tc.key, tc.json, err, tc.want)
		}
	}
}

func TestTreeNamesKeysAsTheFileDoes(t *testing.T) {
	tr := Tree(Default())
	spawn := tr["spawn"].(map[string]any)
	if spawn["default_profile"] != "auto" || spawn["close_delay"] != "30s" || spawn["close_when_done"] != false {
		t.Errorf("spawn = %v", spawn)
	}
	if got := tr["hold"].(map[string]any)["max"]; got != "24h" {
		t.Errorf("hold.max = %v", got)
	}
	profiles := tr["profiles"].(map[string]any)
	if _, ok := profiles["claude"].(map[string]any)["local"]; ok {
		t.Error("profiles.claude has a local table")
	}
	if got := profiles["local"].(map[string]any)["local"].(map[string]any)["min_context"]; got != DefaultMinContext {
		t.Errorf("profiles.local.local.min_context = %v", got)
	}
}
