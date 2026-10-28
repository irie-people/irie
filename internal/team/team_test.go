package team

import (
	"os"
	"testing"

	"github.com/irie-people/irie/internal/testutil"
)

func TestMain(m *testing.M) { testutil.Main(m) }

func TestParseListsEachSessionsTeammates(t *testing.T) {
	out := "s38bzqd9xtyvv\t%0\t0\t1\t0\thost.example.com\ttrue\n" +
		"s38bzqd9xtyvv-k2\t%3\t303\t0\t0\t⠂ Map the auth code\t\"cd /home/alice/repo && env CLAUDECODE=1 /home/alice/.local/bin/claude --agent-id third@session-1 --agent-name third --team-name session-1 --agent-color yellow --agent-type Explore\"\n" +
		"s38bzqd9xtyvv-k1\t%1\t101\t0\t0\t✳ scout\t\"cd /home/alice/repo && env CLAUDECODE=1 /home/alice/.local/bin/claude --agent-id probe@session-1 --agent-name 'the probe' --agent-color blue\"\n" +
		"s38bw0000000a-k1\t%5\t0\t1\t1\tfailed\tcat\n" +
		"s38bw11111111\t%6\t0\t1\t0\thost\ttrue\n" +
		"not a row\n"
	teams := Parse(out)
	if len(teams) != 3 {
		t.Fatalf("teams = %+v", teams)
	}
	got := teams["s38bzqd9xtyvv"]
	if len(got) != 2 {
		t.Fatalf("teammates = %+v", got)
	}
	if p := got[0]; p.Session != "s38bzqd9xtyvv-k1" || p.Pane != "%1" || p.PID != 101 || p.Name != "the probe" || p.Color != "blue" || p.Type != "" || p.Title != "scout" || p.Working || p.Dead {
		t.Errorf("first = %+v", p)
	}
	if p := got[1]; p.Name != "third" || p.Color != "yellow" || p.Type != "Explore" || p.Title != "Map the auth code" || !p.Working {
		t.Errorf("second = %+v", p)
	}
	if p := teams["s38bw0000000a"]; len(p) != 1 || !p[0].Dead || p[0].Exit != 1 || p[0].Name != "failed" {
		t.Errorf("a dead teammate = %+v", p)
	}
	if p, ok := teams["s38bw11111111"]; !ok || len(p) != 0 {
		t.Errorf("an anchor alone = %+v, %v", p, ok)
	}
}

func TestOwner(t *testing.T) {
	for name, want := range map[string]struct {
		id       string
		teammate bool
	}{
		"s38bzqd9xtyvv":    {"s38bzqd9xtyvv", false},
		"s38bzqd9xtyvv-k1": {"s38bzqd9xtyvv", true},
	} {
		if id, teammate := Owner(name); id != want.id || teammate != want.teammate {
			t.Errorf("Owner(%q) = %q, %v", name, id, teammate)
		}
	}
	if a, b := TeammateName("s1", 35), TeammateName("s1", 36); a != "s1-z" || b != "s1-10" {
		t.Errorf("names %q %q", a, b)
	}
}

func TestEnvFileRoundTrips(t *testing.T) {
	p := t.TempDir() + "/team.env"
	env := []string{"A=1", "B=two words", "C=line\nbreak"}
	if err := WriteEnv(p, env); err != nil {
		t.Fatal(err)
	}
	got, err := readEnv(p)
	if err != nil || len(got) != 3 || got[2] != "C=line\nbreak" {
		t.Errorf("read %q, %v", got, err)
	}
	if got, err := readEnv(p + ".missing"); err != nil || got != nil {
		t.Errorf("missing file: %q, %v", got, err)
	}
	os.Remove(p)
}
