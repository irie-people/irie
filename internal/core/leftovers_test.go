package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/testutil"
)

const leftoverUs = "/run/alice/irie/core.sock"

func TestReadServerTellsAnIrieFromOtherServers(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	ours := []string{"IRIE_SOCKET=" + leftoverUs, "IRIE_SESSION=s1", "TMUX_TMPDIR=/t"}
	for _, c := range []struct {
		name string
		argv []string
		env  []string
		ok   bool
		want tmuxServer
	}{
		{"an Irie an agent started", []string{"tmux", "-u", "-L", "irie-upcheck-1", "-f", "/s/irie/tmux.conf", "new-session", "-d", "-s", "irie-agents", "-n", "core", "-x", "80", "-y", "24", "--", "/x/irie", "core", "--supervise"}, ours,
			true, tmuxServer{path: "/t/tmux-" + uid + "/irie-upcheck-1", socket: "irie-upcheck-1", irie: true, ours: true, session: "s1"}},
		{"its team server", []string{"tmux", "-u", "-L", "irie-upcheck-1-teams", "-f", "/s/irie/tmux-teams.conf", "new-session", "-d", "-s", "s38x", "--", "sh"}, []string{"IRIE_SOCKET=" + leftoverUs, "TMUX_TMPDIR=/t"},
			true, tmuxServer{path: "/t/tmux-" + uid + "/irie-upcheck-1-teams", socket: "irie-upcheck-1-teams", side: "-teams", ours: true}},
		{"a server an agent started for something else", []string{"tmux", "-L", "devserver", "new-session", "-d", "-s", "web", "while :; do sleep 1; done"}, ours,
			true, tmuxServer{path: "/t/tmux-" + uid + "/devserver", socket: "devserver", ours: true, session: "s1"}},
		{"another core's Irie, at a path", []string{"tmux", "-S", "/tmp/x/irie-demo", "new", "-d", "-s", "irie-agents"}, []string{"IRIE_SOCKET=/elsewhere.sock", "IRIE_SESSION=s1"},
			true, tmuxServer{path: "/tmp/x/irie-demo", socket: "irie-demo", irie: true}},
		{"a -teams name without the team conf", []string{"tmux", "-L", "my-teams", "new-session", "-d"}, nil,
			true, tmuxServer{path: "/tmp/tmux-" + uid + "/my-teams", socket: "my-teams"}},
		{"a client", []string{"tmux", "-S", "/private/tmp/tmux-501/irie", "attach-session", "-t", "irie"}, nil, false, tmuxServer{}},
		{"the user's own tmux", []string{"tmux"}, nil, false, tmuxServer{}},
		{"a flag without its value", []string{"tmux", "-L"}, nil, false, tmuxServer{}},
	} {
		got, ok := readServer(c.argv, c.env, leftoverUs)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%s: readServer = %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestLeftoverWhyStopsOnlyIriesWhoseRunIsOver(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	hour := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	test := tmuxServer{socket: "irie-test-0b18d93c", irie: true, started: hour(18)}
	agent := tmuxServer{socket: "irie-upcheck-73820", irie: true, ours: true, session: "s1", started: hour(3)}
	open := map[string]bool{"s1": true}
	for _, c := range []struct {
		name  string
		look  leftoverLook
		s     tmuxServer
		open  map[string]bool
		stops bool
	}{
		{"a test run's, with no test binary running", leftoverLook{}, test, open, true},
		{"a test run's, its binary may still run", leftoverLook{tests: []time.Time{hour(19)}}, test, open, false},
		{"a test run's, binaries that started after it", leftoverLook{tests: []time.Time{hour(1), now}}, test, open, true},
		{"a test run's, a binary's start unread", leftoverLook{blind: true}, test, open, false},
		{"a test run's, a day old", leftoverLook{tests: []time.Time{hour(30)}}, tmuxServer{socket: "irie-test-1", started: hour(25)}, open, true},
		{"a test run's, its start unread", leftoverLook{}, tmuxServer{socket: "irie-test-1"}, open, false},
		{"an agent's, its session open", leftoverLook{}, agent, open, false},
		{"an agent's, its session closed", leftoverLook{}, agent, map[string]bool{}, true},
		{"an agent's, a day old", leftoverLook{}, tmuxServer{socket: "irie-demo", irie: true, ours: true, session: "s1", started: hour(25)}, open, true},
		{"another core's agent's", leftoverLook{}, tmuxServer{socket: "irie-demo", irie: true, session: "s1", started: hour(48)}, map[string]bool{}, false},
		{"an agent's server that is no Irie", leftoverLook{}, tmuxServer{socket: "devserver", ours: true, session: "s1", started: hour(48)}, map[string]bool{}, false},
		{"the user's own Irie", leftoverLook{}, tmuxServer{socket: "irie-mine", irie: true, started: hour(200)}, map[string]bool{}, false},
	} {
		why := c.look.leftoverWhy(c.s, c.open, now)
		if (why != "") != c.stops {
			t.Errorf("%s: leftoverWhy = %q, want a stop: %v", c.name, why, c.stops)
		}
	}
}

func TestLeftoversTakeSideServersAlong(t *testing.T) {
	now := time.Now()
	srv := func(path, side string, irie, ours bool, session string) tmuxServer {
		return tmuxServer{path: path, socket: filepath.Base(path), side: side, irie: irie, ours: ours, session: session, started: now.Add(-time.Hour)}
	}
	l := leftoverLook{servers: []tmuxServer{
		srv("/t/a", "", true, true, "closed"),
		srv("/t/a-teams", "-teams", false, true, ""),
		srv("/t/a-dock", "-dock", false, true, ""),
		srv("/t/b", "", true, true, "open"),
		srv("/t/b-teams", "-teams", false, true, ""),
		srv("/t/gone-teams", "-teams", false, true, ""),
		srv("/t/theirs-teams", "-teams", false, false, ""),
	}}
	got := l.leftovers(map[string]bool{"open": true}, now)
	var stopped []string
	for path := range got {
		stopped = append(stopped, path)
	}
	slices.Sort(stopped)
	if want := []string{"/t/a", "/t/a-dock", "/t/a-teams", "/t/gone-teams"}; !slices.Equal(stopped, want) {
		t.Errorf("stopped %v, want %v (%v)", stopped, want, got)
	}
	if !strings.HasPrefix(got["/t/a-teams"], "its Irie stops") || got["/t/gone-teams"] != "its Irie is gone" {
		t.Errorf("reasons: %v", got)
	}
}

func TestALeftoverTestServerStopsWithWhatItRuns(t *testing.T) {
	bin := testutil.NeedTmux(t)
	dir := testutil.ShortDir(t)
	name := fmt.Sprintf("irie-test-left%d", os.Getpid())
	cmd := exec.Command(bin, "-L", name, "-f", "/dev/null", "new-session", "-d", "-s", "irie-agents", "sleep 300")
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+dir, "IRIE_SOCKET="+leftoverUs, "IRIE_SESSION=s1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux: %v: %s", err, out)
	}
	tb, err := proc.Read()
	if err != nil {
		t.Skip(err)
	}
	find := func(l leftoverLook) (tmuxServer, bool) {
		i := slices.IndexFunc(l.servers, func(s tmuxServer) bool { return s.socket == name })
		if i < 0 {
			return tmuxServer{}, false
		}
		return l.servers[i], true
	}
	l := lookForLeftovers(tb, []string{"irie", "irie-teams"}, leftoverUs, os.Getpid())
	s, ok := find(l)
	if !ok {
		t.Fatalf("the look did not find %s", name)
	}
	if !s.irie || !s.ours || s.session != "s1" || len(tb.Tree(s.PID)) == 0 {
		t.Errorf("found %+v with %d processes under it", s, len(tb.Tree(s.PID)))
	}
	if !slices.ContainsFunc(l.tests, func(at time.Time) bool { return !at.After(s.started) }) {
		t.Errorf("this test binary, which started before the server, is not among the test runs: %v", l.tests)
	}
	if why := l.leftoverWhy(s, nil, time.Now()); why != "" {
		t.Errorf("a server whose test binary still runs is stopped: %s", why)
	}
	if _, ok := find(lookForLeftovers(tb, []string{name}, leftoverUs, os.Getpid())); ok {
		t.Error("the core's own server, by name, is among the leftovers")
	}
	tree := append([]proc.Proc{s.Proc}, tb.Tree(s.PID)...)
	if left := stopServers(tb, []proc.Proc{s.Proc}); len(left) > 0 {
		t.Errorf("still running: %v", pidsOf(left))
	}
	if left := stillRunning(tree); len(left) > 0 {
		t.Errorf("the server's tree still runs: %v", pidsOf(left))
	}
}

func TestIsTestBinarySeesThisOne(t *testing.T) {
	p, err := proc.One(os.Getpid())
	if err != nil {
		t.Skip(err)
	}
	if !isTestBinary(p) {
		t.Errorf("this test binary, %q, is not one", p.Name)
	}
	if isTestBinary(proc.Proc{PID: os.Getpid(), Name: "tmux"}) {
		t.Error("tmux is a test binary")
	}
}
