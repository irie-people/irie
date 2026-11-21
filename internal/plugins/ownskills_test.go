package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

// Skills and tools of your own: [skills.<id>] and [tools.<id>] tables in a
// manifest (ownskills.go, owntools.go).

// skillFolder makes a skill's folder under ~ named name, holding a SKILL.md.
func skillFolder(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, "apps", "grants", "skills", name)
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + name + "\ndescription: Find and track grants with the grants app on this computer.\n---\n\n# Grants\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "stages.md"), []byte("# Stages\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestAManifestBringsASkillAndTools: a manifest whose skill and tools go to
// agents' sessions is off until you turn it on; then both are ready, and
// sessions get them.
func TestAManifestBringsASkillAndTools(t *testing.T) {
	e := env(t)
	h := home(t)
	dir := skillFolder(t, h, "grants")
	server := program(t, "grants-app")
	path := manifest(t, e, "grants-app.toml", `
title = "Grants"

[skills.grants]
title = "Grant work"
path = "~/apps/grants/skills/grants"

[tools.grants]
title = "Grants app"
run = ["`+server+`", "mcp", "--db", "~/grants.db"]
`)
	p := mine(t, e, "grants-app")
	if p.On || !strings.Contains(p.Summary, "It gives agents' sessions a skill and tools of yours, so it is off until you turn it on.") {
		t.Fatalf("a manifest that speaks to agents is on before you say so: %+v", p)
	}
	for _, name := range []string{"skills.grants", "tools.grants"} {
		if x := part(t, p, name); x.State != ipc.PartPluginOff {
			t.Errorf("off: %s = %+v", name, x)
		}
	}
	if ts := Tools(e); len(ts) != 1 || ts[0].On {
		t.Errorf("off, Tools = %+v", ts)
	}
	all, st := SessionSkills(e, true)
	if c := choice(t, all, st, "grants"); c.On || c.Why != "the grants-app plugin is off (plugins.manifests.grants-app)" {
		t.Errorf("off, the skill = %+v", c)
	}

	e.Config.Plugins.Manifests["grants-app"] = true
	p = mine(t, e, "grants-app")
	if x := part(t, p, "skills.grants"); !p.On || x.Title != "Grant work" || x.State != ipc.PartReady ||
		x.Detail != "Sessions that start now get /irie:grants." || !reflect.DeepEqual(x.Brings, []string{"a skill for agents' sessions, /irie:grants"}) {
		t.Errorf("on, the skill = %+v", x)
	}
	if x := part(t, p, "tools.grants"); x.Title != "Grants app" || x.State != ipc.PartReady ||
		x.Detail != "Sessions that start now get its tools, mcp__grants__…, from grants-app." {
		t.Errorf("on, the tools = %+v", x)
	}
	want := OwnTools{Name: "grants", Plugin: "grants-app", Title: "Grants app",
		Run: []string{server, "mcp", "--db", filepath.Join(h, "grants.db")}, Manifest: path, On: true}
	if ts := Tools(e); len(ts) != 1 || !reflect.DeepEqual(ts[0], want) {
		t.Errorf("Tools = %+v\nwant %+v", ts, want)
	}
	all, st = SessionSkills(e, true)
	c := choice(t, all, st, "grants")
	if !c.On || !c.Mine || c.Source != "your plugin grants-app, from "+dir || len(c.Files) != 2 || c.Description == "" {
		t.Errorf("on, the skill = %+v", c)
	}
	// Irie's own come first, and stay.
	if all[0].Mine || !choice(t, all, st, "present").On {
		t.Errorf("Irie's own skills: %+v", all)
	}
	// A profile that takes no skills gets none of yours either.
	all, st = SessionSkills(e, false)
	if c := choice(t, all, st, "grants"); c.On {
		t.Errorf("a profile with no skills: %+v", c)
	}
	// skills.disabled leaves yours out by name, as Irie's own.
	e.Config.Skills.Disabled = []string{"grants"}
	all, st = SessionSkills(e, true)
	if c := choice(t, all, st, "grants"); c.On || c.Why != "turned off (skills.disabled)" {
		t.Errorf("disabled: %+v", c)
	}
}

// choice is how skills.Choose decides on the skill named name.
func choice(t *testing.T, all []skills.Skill, st skills.Settings, name string) skills.Choice {
	t.Helper()
	for _, c := range skills.Choose(all, st) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no skill %q among %d", name, len(all))
	return skills.Choice{}
}

// TestASkillThatDoesntReadSaysWhy: a table or a folder that can't be a
// skill says what is wrong, in its part, and sessions go without it.
func TestASkillThatDoesntReadSaysWhy(t *testing.T) {
	for _, c := range []struct{ table, want string }{
		{`title = "x"`, `[skills.grants] has no path: path = "~/skills/grants" names the folder that holds its SKILL.md.`},
		{`path = "skills/grants"`, "isn't a full path to a folder"},
		{`path = "~/nowhere"`, "Sessions get it once its folder holds a skill: " + "/nowhere isn't there."},
	} {
		e := env(t)
		h := home(t)
		manifest(t, e, "grants-app.toml", "[skills.grants]\n"+c.table+"\n")
		e.Config.Plugins.Manifests["grants-app"] = true
		x := part(t, mine(t, e, "grants-app"), "skills.grants")
		if want := strings.ReplaceAll(c.want, "/nowhere", filepath.Join(h, "nowhere")); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, want) {
			t.Errorf("%s: part = %+v, want it to need %q", c.table, x, want)
		}
		all, st := SessionSkills(e, true)
		if ch := choice(t, all, st, "grants"); ch.On || ch.Why == "" {
			t.Errorf("%s: the skill went to sessions: %+v", c.table, ch)
		}
	}
	// One of Irie's own names stays Irie's.
	e := env(t)
	h := home(t)
	skillFolder(t, h, "present")
	manifest(t, e, "grants-app.toml", "[skills.present]\npath = \"~/apps/grants/skills/present\"\n")
	e.Config.Plugins.Manifests["grants-app"] = true
	if x := part(t, mine(t, e, "grants-app"), "skills.present"); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "One of Irie's own skills is named present") {
		t.Errorf("Irie's name: %+v", x)
	}
	all, st := SessionSkills(e, true)
	if c := choice(t, all, st, "present"); !c.On || c.Mine {
		t.Errorf("Irie's own present: %+v", c)
	}
}

// TestTheFirstPluginKeepsAName: two plugins of yours with a skill, or
// tools, of the same name: the first by its file's name keeps it, and the
// later one's part says whose it is.
func TestTheFirstPluginKeepsAName(t *testing.T) {
	e := env(t)
	h := home(t)
	skillFolder(t, h, "grants")
	server := program(t, "grants-app")
	table := "[skills.grants]\npath = \"~/apps/grants/skills/grants\"\n\n[tools.grants]\nrun = [\"" + server + "\"]\n"
	manifest(t, e, "a-grants.toml", table)
	manifest(t, e, "b-grants.toml", table)
	e.Config.Plugins.Manifests["a-grants"] = true
	e.Config.Plugins.Manifests["b-grants"] = true
	a, b := mine(t, e, "a-grants"), mine(t, e, "b-grants")
	if part(t, a, "skills.grants").State != ipc.PartReady || part(t, a, "tools.grants").State != ipc.PartReady {
		t.Errorf("the first: %+v", a.Parts)
	}
	if x := part(t, b, "skills.grants"); x.State != ipc.PartNeeds || x.Detail != "Your plugin a-grants has a skill named grants first: give [skills.grants] another name." {
		t.Errorf("the later skill: %+v", x)
	}
	if x := part(t, b, "tools.grants"); x.State != ipc.PartNeeds || x.Detail != "Your plugin a-grants has tools named grants first: give [tools.grants] another name." {
		t.Errorf("the later tools: %+v", x)
	}
	if ts := Tools(e); len(ts) != 2 || ts[0].Problem != "" || ts[1].Problem == "" {
		t.Errorf("Tools = %+v", ts)
	}
	all, st := SessionSkills(e, true)
	n := 0
	for _, c := range skills.Choose(all, st) {
		if c.Name == "grants" {
			n++
			if !c.On || !strings.Contains(c.Source, "a-grants") {
				t.Errorf("the skill sessions get: %+v", c)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d skills named grants", n)
	}
}

// TestToolsThatDontReadSayWhy: a server's table that can't run says what
// is wrong, in its part.
func TestToolsThatDontReadSayWhy(t *testing.T) {
	for _, c := range []struct{ id, table, want string }{
		{"grants", `title = "x"`, `[tools.grants] has no run: run = ["~/bin/grants", "mcp"] names the program that serves its tools.`},
		{"grants", `run = [" "]`, `run starts with an empty word: its first is the program, such as run = ["~/bin/grants", "mcp"].`},
		{"grants", `run = ["bin/grants"]`, "isn't a name on your PATH or a full path"},
		{"grants", `run = ["no-such-program-for-irie-tests"]`, "isn't a program on this computer, so sessions can't start its tools"},
		{"irie-browser", `run = ["sh"]`, "Irie's own servers have names that start irie: give [tools.irie-browser] another name."},
		{"Grants", `run = ["sh"]`, "isn't a server's name"},
	} {
		e := env(t)
		home(t)
		manifest(t, e, "grants-app.toml", "[tools."+c.id+"]\n"+c.table+"\n")
		e.Config.Plugins.Manifests["grants-app"] = true
		x := part(t, mine(t, e, "grants-app"), "tools."+c.id)
		if x.State != ipc.PartNeeds || !strings.Contains(x.Detail, c.want) {
			t.Errorf("%s: part = %+v, want it to need %q", c.table, x, c.want)
		}
	}
}

// TestAManifestWithEverythingSaysWhatWaits: a pane, a helper, a skill and
// tools: its summary names what waits for your switch in one sentence.
func TestAManifestWithEverythingSaysWhatWaits(t *testing.T) {
	e := env(t)
	h := home(t)
	skillFolder(t, h, "grants")
	manifest(t, e, "grants-app.toml", `
[panes.grants]
address = "http://127.0.0.1:8585/"

[helpers.serve]
run = ["sh", "-c", "sleep 1"]

[skills.grants]
path = "~/apps/grants/skills/grants"

[tools.grants]
run = ["sh"]
`)
	p := mine(t, e, "grants-app")
	if want := " It runs a program of yours and gives agents' sessions a skill and tools of yours, so it is off until you turn it on."; p.On || !strings.HasSuffix(p.Summary, want) {
		t.Errorf("summary = %q, want it to end %q", p.Summary, want)
	}
	if len(p.Parts) != 4 {
		t.Errorf("parts = %+v", p.Parts)
	}
}

// TestABundleHoldsNoSkillsOrTools: a bundle names plugins, and has no
// parts of its own.
func TestABundleHoldsNoSkillsOrTools(t *testing.T) {
	e := env(t)
	manifest(t, e, "business.toml", "plugins = [\"grants-app\"]\n\n[tools.grants]\nrun = [\"sh\"]\n")
	_, bs := Mine(e.Paths.Plugins())
	if len(bs) != 1 || !strings.Contains(bs[0].Problem, "put its panes, helpers, skills, tools or schedules in a manifest of their own") {
		t.Errorf("bundles = %+v", bs)
	}
}
