package arch

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/irie-people/irie/internal/brand"
)

func TestMikaelLeadsTheArchAgents(t *testing.T) {
	agents := All()
	if len(agents) == 0 || agents[0].Name != "mikael" {
		t.Fatalf("arch agents = %v, want mikael first", Names())
	}
	m, ok := Get("mikael")
	if !ok {
		t.Fatal("no mikael")
	}
	if m.Title != "a helper against evil" || m.Color == "" {
		t.Errorf("mikael = %+v", m)
	}
	for _, want := range []string{"against evil", "good they want to see in the world", "Good", "Careful", "Evil"} {
		if !strings.Contains(m.Description+m.Charge, want) {
			t.Errorf("mikael's description and charge do not say %q", want)
		}
	}
	// He weighs and counsels: he may read and look things up, never change
	// a file or run a command.
	if want := []string{"Read", "Grep", "Glob", "WebFetch", "WebSearch"}; !slices.Equal(m.Tools, want) {
		t.Errorf("tools = %v, want %v", m.Tools, want)
	}
	if _, ok := Get("nobody"); ok {
		t.Error("an arch agent that is not there was found")
	}
}

func TestMikaelKeepsTheDoor(t *testing.T) {
	m, _ := Get("mikael")
	d := m.Door
	if d == nil {
		t.Fatal("mikael keeps no door")
	}
	if d.Timeout != 20 || d.Status == "" || !strings.Contains(d.Prompt, "$ARGUMENTS") || !strings.Contains(d.Prompt, `{"ok": true}`) {
		t.Errorf("door = %+v", d)
	}
	// What leaves the machine, what is lost for good, secrets, code from
	// elsewhere and a hold on the machine are weighed; ordinary reading,
	// building and testing are not.
	for _, want := range []string{"Bash(curl *)", "Bash(git push *)", "WebFetch", "Bash(rm *)", "Bash(security *)", "Read(~/.ssh/**)", "Bash(npx *)", "Bash(sudo *)", "Edit(~/.*)"} {
		if !slices.Contains(d.Rules, want) {
			t.Errorf("the door does not weigh %s", want)
		}
	}
	// So are Irie's own ops whose effect leaves Irie, as an agent runs them
	// through its program; those that stay inside, which undo takes back,
	// are not (DESIGN §4.5).
	for _, want := range []string{"Bash(* ctl spawn*)", "Bash(* ctl delegate*)", "Bash(* ctl fork*)", "Bash(* ctl bench*)",
		"Bash(* ctl input*)", "Bash(* ctl send*)", "Bash(* ctl continue*)", "Bash(* ctl move*)", "Bash(* remote pair*)", "Bash(* peer add*)",
		"Bash(* ctl council convene*)", "Bash(* ctl council yes*)", "Bash(* ctl ragu* convene*)", "Bash(* ctl ragu* yes*)"} {
		if !slices.Contains(d.Rules, want) {
			t.Errorf("the door does not weigh %s", want)
		}
	}
	for _, never := range []string{"Bash", "Read", "Edit", "Bash(go test *)", "Bash(git commit *)", "Bash(ls *)",
		"Bash(* ctl close *)", "Bash(* ctl park *)", "Bash(* ctl freeze *)", "Bash(* ctl rename *)", "Bash(* ctl list *)",
		"Bash(* ctl council*)", "Bash(* ctl council tally*)", "Bash(* ctl council propose*)"} {
		if slices.Contains(d.Rules, never) {
			t.Errorf("the door weighs %s, which is ordinary work", never)
		}
	}
	seen := map[string]bool{}
	for _, r := range d.Rules {
		if seen[r] {
			t.Errorf("rule %s is there twice", r)
		}
		seen[r] = true
	}
}

// The door weighs each way an agent writes to let code past Gatekeeper, as
// a README or an installer asks: the quarantine mark stripped, every
// attribute cleared, Gatekeeper switched off or told to let an app run, and
// each of them under sudo. What only reads passes: it changes nothing.
// Raphael runs such reads himself (spctl --status, spctl -a -vv -t exec),
// in the core, where no hook sees them (DESIGN §4.11).
func TestMikaelWeighsWhatGetsPastGatekeeper(t *testing.T) {
	m, _ := Get("mikael")
	for _, cmd := range []string{
		"xattr -d com.apple.quarantine /Applications/Foo.app",
		"xattr -r -d com.apple.quarantine Foo.app",
		"xattr -d -r com.apple.quarantine Foo.app",
		"xattr -dr com.apple.quarantine ~/.foo",
		"xattr -rd com.apple.quarantine Foo.app",
		`xattr -d "com.apple.quarantine" Foo.app`,
		"xattr -c Foo.app",
		"xattr -cr /Applications/Foo.app",
		"xattr -rc Foo.app",
		"xattr -r -c Foo.app",
		"xattr -c -r Foo.app",
		"spctl --master-disable",
		"spctl --global-disable",
		`spctl --disable --label "Mac App Store"`,
		"spctl --add /Applications/Foo.app",
		`spctl --add --label "My Stuff" /Applications/Foo.app`,
		"sudo xattr -rd com.apple.quarantine /Applications/Foo.app",
		"sudo xattr -cr /Applications/Foo.app",
		"sudo spctl --master-disable",
		"sudo spctl --global-disable",
		"sudo spctl --add /Applications/Foo.app",
	} {
		if !weighs(m.Door.Rules, cmd) {
			t.Errorf("the door does not weigh %s", cmd)
		}
	}
	for _, cmd := range []string{
		"xattr Foo.app",
		"xattr -l Foo.app",
		"xattr -lr /Users/alice/code/Foo.app",
		"xattr -p com.apple.quarantine Foo.app",
		"xattr -px com.apple.quarantine /Users/alice/Downloads/Foo.app",
		"spctl --status",
		"spctl -a /Applications/Mail.app",
		"spctl --assess --type execute /Applications/Mail.app",
		"spctl -a -vv -t exec /Applications/Mail.app",
		"spctl --global-enable",
	} {
		if weighs(m.Door.Rules, cmd) {
			t.Errorf("the door weighs %s, which only reads or keeps Gatekeeper on", cmd)
		}
	}
}

// weighs says whether one of rules matches a Bash command the way Claude
// Code matches a permission rule (permissions.md, "Wildcard patterns"): a *
// stands for any text, spaces too, and the rest is matched as written; a
// trailing " *" that is the rule's only * also matches the bare command.
// The commands here are single, so splitting compound commands and
// stripping wrappers such as timeout are left out.
func weighs(rules []string, command string) bool {
	for _, r := range rules {
		pattern, ok := strings.CutPrefix(r, "Bash(")
		if !ok {
			continue
		}
		pattern = strings.TrimSuffix(pattern, ")")
		if bare, ok := strings.CutSuffix(pattern, " *"); ok && !strings.Contains(bare, "*") && command == bare {
			return true
		}
		parts := strings.Split(pattern, "*")
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		if regexp.MustCompile(`(?s)^` + strings.Join(parts, ".*") + `$`).MatchString(command) {
			return true
		}
	}
	return false
}

func TestMikaelsWord(t *testing.T) {
	m, _ := Get("mikael")
	plain := m.Word(WordData{})
	if !strings.HasPrefix(plain, "["+brand.Name+"] "+brand.Mikael+", the first of "+brand.Name+"'s arch agents, is with you") || !strings.Contains(plain, "never orders") {
		t.Errorf("word:\n%s", plain)
	}
	if strings.Contains(plain, "mikael agent") || strings.Contains(plain, "never work around "+brand.Him) || strings.Contains(plain, "The good your user wants") {
		t.Errorf("a word with nothing on call, no door and no good says more:\n%s", plain)
	}
	full := m.Word(WordData{OnCall: true, Door: true, Good: []string{"Give credit where it is due.", "Be honest with clients."}})
	for _, want := range []string{
		"ask the mikael agent before you act",
		"never work around " + brand.Him,
		"The good your user wants to see in the world:\n- Give credit where it is due.\n- Be honest with clients.",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("word does not say %q:\n%s", want, full)
		}
	}
	if strings.Contains(full, "\n\n") || strings.HasSuffix(full, "\n") {
		t.Errorf("word has blank lines or a trailing newline:\n%q", full)
	}
}

func TestDefinitionsPutMikaelOnCall(t *testing.T) {
	raw, err := Definitions([]string{"mikael"}, []string{"Pay contributors fairly."})
	if err != nil {
		t.Fatal(err)
	}
	var defs map[string]struct {
		Description string   `json:"description"`
		Prompt      string   `json:"prompt"`
		Tools       []string `json:"tools"`
		Color       string   `json:"color"`
	}
	if err := json.Unmarshal([]byte(raw), &defs); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	m := defs["mikael"]
	if len(defs) != 1 || m.Description == "" || len(m.Tools) == 0 || m.Color == "" {
		t.Fatalf("definitions = %+v", defs)
	}
	if !strings.HasPrefix(m.Prompt, "You are "+brand.Mikael) || !strings.HasSuffix(m.Prompt, "## The good your user wants to see in the world\n\n- Pay contributors fairly.") {
		t.Errorf("prompt:\n%s", m.Prompt)
	}
	// One line, so it goes as one argument as it is.
	if strings.Contains(raw, "\n") || strings.Contains(raw, `\u003c`) {
		t.Errorf("definitions are not one plain line: %s", raw)
	}
	if raw, _ := Definitions([]string{"mikael"}, nil); strings.Contains(raw, "The good your user") {
		t.Error("with no good written, the charge still names it")
	}
	if _, err := Definitions([]string{"nobody"}, nil); err == nil {
		t.Error("an arch agent that is not there was defined")
	}
}

func TestLoadRefusesBrokenFiles(t *testing.T) {
	const agent = "---\nname: uriel\ndescription: Helps.\n---\nYou are Uriel."
	for name, files := range map[string]fstest.MapFS{
		"no fields":      {"agents/uriel.md": {Data: []byte("You are Uriel.")}},
		"name not file":  {"agents/uriel.md": {Data: []byte("---\nname: gabriel\ndescription: x\n---\nYou are.")}},
		"no charge":      {"agents/uriel.md": {Data: []byte("---\nname: uriel\ndescription: x\n---\n")}},
		"bad rank":       {"agents/uriel.md": {Data: []byte("---\nname: uriel\nrank: first\ndescription: x\n---\nYou are.")}},
		"stray part":     {"agents/uriel.md": {Data: []byte(agent)}, "agents/uriel.song.md": {Data: []byte("la")}},
		"part of nobody": {"agents/uriel.md": {Data: []byte(agent)}, "agents/gabriel.word.md": {Data: []byte("hi")}},
		"door no rules":  {"agents/uriel.md": {Data: []byte(agent)}, "agents/uriel.door.md": {Data: []byte("---\nstatus: x\n---\nWeigh $ARGUMENTS")}},
		"door no call":   {"agents/uriel.md": {Data: []byte(agent)}, "agents/uriel.door.md": {Data: []byte("---\nrules:\n  - WebFetch\n---\nWeigh it.")}},
		"door bad rule":  {"agents/uriel.md": {Data: []byte(agent)}, "agents/uriel.door.md": {Data: []byte("---\nrules:\n  - curl anything\n---\nWeigh $ARGUMENTS")}},
		"bad word":       {"agents/uriel.md": {Data: []byte(agent)}, "agents/uriel.word.md": {Data: []byte("{{if}}")}},
	} {
		if _, err := load(files); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	got, err := load(fstest.MapFS{
		"agents/uriel.md":   {Data: []byte(agent)},
		"agents/gabriel.md": {Data: []byte("---\nname: gabriel\nrank: 2\ndescription: Helps too.\n# a comment\n---\nYou are " + brand.Gabriel + ".")},
	})
	if err != nil || len(got) != 2 || got[0].Name != "uriel" || got[1].Name != "gabriel" || got[0].Door != nil || got[0].Word(WordData{}) != "" {
		t.Errorf("load = %+v, %v; want uriel (rank 0) before gabriel (rank 2), with no door or word", got, err)
	}
}

func TestGabrielWorksInTheCore(t *testing.T) {
	g, ok := Get("gabriel")
	if !ok || g.Call || g.Rank != 2 || g.Door != nil {
		t.Fatalf("gabriel = %+v, %v; want rank 2, not called, no door", g, ok)
	}
	if w := g.Word(WordData{}); !strings.HasPrefix(w, "["+brand.Name+"] "+brand.Gabriel) || !strings.Contains(w, "ctl gabriel") {
		t.Errorf("word:\n%s", w)
	}
	// Nobody calls him: he is left off --agents, and alone gives none.
	if raw, err := Definitions([]string{"mikael", "gabriel"}, nil); err != nil || strings.Contains(raw, "gabriel") || !strings.Contains(raw, "mikael") {
		t.Errorf("definitions = %s, %v", raw, err)
	}
	if raw, err := Definitions([]string{"gabriel"}, nil); err != nil || raw != "" {
		t.Errorf(brand.Gabriel+" alone: %q, %v", raw, err)
	}
	// Raguel is callable so that a session may run as him (+raguel),
	// though no agent need call on him.
	if want := []string{"mikael", "raguel"}; !slices.Equal(Callable(), want) {
		t.Errorf("callable = %v, want %v", Callable(), want)
	}
}
