package variorum

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// runDoor runs `irie variorum door` as a writing session's Claude Code
// would, with data as Irie's data folder and home as HOME, says each of
// lines, and returns what came back, line by line, once the door ended.
func runDoor(t *testing.T, data, home string, lines ...string) []string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), "IRIE_TEST_DOOR="+data, "IRIE_TEST_FOLDERS="+strings.Join(Folders, string(os.PathListSeparator)),
		"HOME="+home, "IRIE_TEST_CANARY=not for the door")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var got []string
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			got = append(got, sc.Text())
		}
		close(done)
	}()
	for _, l := range lines {
		io.WriteString(in, l+"\n")
	}
	// The door answers before its input ends; then the session ends it.
	time.Sleep(500 * time.Millisecond)
	in.Close()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		t.Fatal("the door didn't end")
	}
	if err := cmd.Wait(); err != nil {
		t.Errorf("the door ended with %v", err)
	}
	return got
}

const initialize = `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude-code","version":"2.1.316"}}}`

// TestTheDoorBecomesVariorumsDoor: with Claude Code's setup kept, the door
// is Variorum's own program, with no arguments, the key and the minimal
// environment and nothing else of the session's, and every byte goes
// straight through.
func TestTheDoorBecomesVariorumsDoor(t *testing.T) {
	app := fakeApp(t, "io.lduarte.Variorum")
	setScript(t, app, script{Answers: map[string][]answer{"list_notes": {{Text: `{"notes":[]}`}}}})
	data, home := t.TempDir(), t.TempDir()
	if err := Save(data, Sessions, Setup{Command: doorOf(app), Key: sessionsKey}); err != nil {
		t.Fatal(err)
	}
	out := runDoor(t, data, home,
		`{"jsonrpc":"2.0","id":"probe","method":"server/discover"}`,
		initialize,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_notes","arguments":{}}}`)
	if len(out) == 0 || decode(t, []byte(out[0])).Result == nil {
		t.Fatalf("the door answered %q", out)
	}
	r := got(t, app)
	env := slices.Clone(r.Env)
	slices.Sort(env)
	if want := []string{"HOME=" + home, "PATH=/usr/bin:/bin", "VARIORUM_MCP_KEY=" + sessionsKey}; !slices.Equal(env, want) {
		t.Errorf("Variorum's door got the environment %q, want %q", env, want)
	}
	if len(r.Args) != 0 {
		t.Errorf("Variorum's door got the arguments %q", r.Args)
	}
	if len(r.Lines) != 4 || decode(t, r.Lines[0]).Method != "server/discover" || decode(t, r.Lines[3]).Method != "tools/call" {
		t.Errorf("Variorum's door got %d lines: %s", len(r.Lines), r.Lines)
	}
}

// TestTheDoorSaysWhatToDo: with no setup kept, the door answers the
// client's opening as Variorum's door does when it can't serve: a request
// before initialize gets "Method not found" on its own id, a ping an empty
// result, a notification nothing, and the initialize one error that says
// what to do.
func TestTheDoorSaysWhatToDo(t *testing.T) {
	fakeApp(t, "io.lduarte.Variorum")
	out := runDoor(t, t.TempDir(), t.TempDir(),
		`{"jsonrpc":"2.0","id":"probe","method":"server/discover"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled"}`,
		``,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
		initialize)
	if len(out) != 3 {
		t.Fatalf("the door answered %q", out)
	}
	if m := decode(t, []byte(out[0])); m.ID != "probe" || m.Error == nil || m.Error.Code != -32601 {
		t.Errorf("server/discover: %s", out[0])
	}
	if m := decode(t, []byte(out[1])); m.ID != float64(7) || m.Error != nil || m.Result == nil {
		t.Errorf("ping: %s", out[1])
	}
	m := decode(t, []byte(out[2]))
	if m.ID != float64(0) || m.Error == nil || m.Error.Code != -32000 || m.Error.Message != noSessionsSetup || !strings.Contains(m.Error.Message, "pbpaste | "+brand.Slug+" variorum connect") {
		t.Errorf("initialize: %s", out[2])
	}
}

// TestTheDoorRefusesAnotherProgram: a kept setup whose program fails its
// checks never runs, and the session hears why, on its initialize's id.
func TestTheDoorRefusesAnotherProgram(t *testing.T) {
	app := fakeApp(t, "io.lduarte.Variorum")
	data := t.TempDir()
	if err := Save(data, Sessions, Setup{Command: doorOf(app), Key: sessionsKey}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app+"/Contents/Info.plist", []byte(plist("io.example.Other")), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runDoor(t, data, t.TempDir(), initialize)
	if len(out) != 1 {
		t.Fatalf("the door answered %q", out)
	}
	m := decode(t, []byte(out[0]))
	if m.ID != float64(0) || m.Error == nil || m.Error.Code != -32000 || !strings.Contains(m.Error.Message, "isn't Variorum") || !strings.Contains(m.Error.Message, "Copy Claude Code's setup again") || strings.Contains(m.Error.Message, sessionsKey) {
		t.Errorf("initialize: %s", out[0])
	}
	if _, err := os.Stat(app + "/Contents/Resources/got.json"); err == nil {
		t.Error("the program ran")
	}
}

// TestRefuseAnswersAClientThatEndsOnNull: a client that ends before its
// initialize, or opens with something that isn't a request, hears the
// refusal on id null.
func TestRefuseAnswersAClientThatEndsOnNull(t *testing.T) {
	for name, in := range map[string]string{
		"nothing":     "",
		"not JSON":    "hello\n",
		"a null id":   `{"jsonrpc":"2.0","id":null,"method":"tools/list"}` + "\n",
		"an answer":   `{"jsonrpc":"2.0","id":3,"result":{}}` + "\n",
		"many probes": strings.Repeat(`{"jsonrpc":"2.0","method":"notifications/x"}`+"\n", 20),
	} {
		var out strings.Builder
		Refuse(strings.NewReader(in), &out, "Why.")
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		last := decode(t, []byte(lines[len(lines)-1]))
		wantID := any(nil)
		if name == "an answer" {
			wantID = float64(3)
		}
		if last.Error == nil || last.Error.Message != "Why." || last.ID != wantID {
			t.Errorf("%s: %q", name, out.String())
		}
	}
}
