package variorum

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// TestCheckRunsOnlyVariorumsDoor: the program must be variorum-mcp,
// directly in the Contents/MacOS of an app in an Applications folder whose
// Info.plist names Variorum's bundle, the release's or the debug build's.
func TestCheckRunsOnlyVariorumsDoor(t *testing.T) {
	app := fakeApp(t, "io.lduarte.Variorum")
	if err := Check(doorOf(app)); err != nil {
		t.Fatalf("Variorum's door: %v", err)
	}
	folder := Folders[0]
	debug := makeApp(t, folder, "Variorum Debug.app", "io.lduarte.Variorum.debug")
	if err := Check(doorOf(debug)); err != nil {
		t.Errorf("the debug build's door: %v", err)
	}

	other := makeApp(t, folder, "Other.app", "io.example.Other")
	elsewhere := makeApp(t, t.TempDir(), "Variorum.app", "io.lduarte.Variorum")
	renamed := filepath.Join(app, "Contents", "MacOS", "variorum-mcp2")
	copyFile(t, doorOf(app), renamed)
	deeper := filepath.Join(app, "Contents", "Resources", Program)
	copyFile(t, doorOf(app), deeper)
	noApp := filepath.Join(folder, "Variorum", "Contents", "MacOS", Program)
	gone := doorOf(filepath.Join(folder, "Gone.app"))
	for name, tc := range map[string]struct{ command, want string }{
		"a relative path":         {"Variorum.app/Contents/MacOS/variorum-mcp", "doesn't name Variorum's door"},
		"a path not clean":        {filepath.Join(app, "Contents", "MacOS") + "/../MacOS/" + Program, "doesn't name Variorum's door"},
		"another name":            {renamed, "doesn't name Variorum's door"},
		"not in Contents/MacOS":   {deeper, "doesn't name Variorum's door"},
		"not in an app":           {noApp, "doesn't name Variorum's door"},
		"outside the folders":     {doorOf(elsewhere), "doesn't name Variorum's door"},
		"another app":             {doorOf(other), `isn't Variorum: its bundle is "io.example.Other"`},
		"an app no longer there":  {gone, "isn't at"},
		"another program by link": {doorOf(makeLinked(t, folder)), "isn't a program"},
	} {
		err := Check(tc.command)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want one that says %q", name, err, tc.want)
		}
	}

	// The door is a link to the test binary, which must keep its mode.
	if err := os.Remove(doorOf(app)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doorOf(app), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(doorOf(app)); err == nil || !strings.Contains(err.Error(), "isn't a program") {
		t.Errorf("a door that can't run: %v", err)
	}
	if err := os.Remove(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		t.Fatal(err)
	}
	if err := Check(doorOf(app)); err == nil || !strings.Contains(err.Error(), "has no Info.plist") {
		t.Errorf("no Info.plist: %v", err)
	}
}

// makeLinked makes an app of Variorum's bundle whose door is a link to
// another program.
func makeLinked(t *testing.T, folder string) string {
	t.Helper()
	app := makeApp(t, folder, "Linked.app", "io.lduarte.Variorum")
	if err := os.Remove(doorOf(app)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/sh", doorOf(app)); err != nil {
		t.Fatal(err)
	}
	return app
}

// TestCheckReadsABinaryInfoPlist: Xcode may write Info.plist in binary,
// which macOS's plutil reads.
func TestCheckReadsABinaryInfoPlist(t *testing.T) {
	if _, err := os.Stat("/usr/bin/plutil"); err != nil {
		t.Skip("no plutil: not macOS")
	}
	app := fakeApp(t, "io.lduarte.Variorum")
	p := filepath.Join(app, "Contents", "Info.plist")
	if out, err := exec.Command("/usr/bin/plutil", "-convert", "binary1", p).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(p); !strings.HasPrefix(string(b), "bplist") {
		t.Fatal("the plist isn't binary")
	}
	if err := Check(doorOf(app)); err != nil {
		t.Errorf("a binary Info.plist: %v", err)
	}
}

// TestTheSetupsAreTheUsersAlone: door.json holds both setups, the file
// 0600 in a folder 0700, even over a file another made; Forget deletes
// both.
func TestTheSetupsAreTheUsersAlone(t *testing.T) {
	data := t.TempDir()
	if st, err := Load(data); err != nil || st.Sessions != nil || st.Tab != nil {
		t.Fatalf("nothing kept: %+v, %v", st, err)
	}
	if err := os.MkdirAll(filepath.Dir(File(data)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(File(data), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(data, Sessions, Setup{Command: door, Key: sessionsKey}); err != nil {
		t.Fatal(err)
	}
	if err := Save(data, Tab, Setup{Command: door, Key: tabKey}); err != nil {
		t.Fatal(err)
	}
	st, err := Load(data)
	if err != nil || st.Sessions == nil || st.Sessions.Key != sessionsKey || st.Tab == nil || st.Tab.Key != tabKey || st.Tab.Command != door {
		t.Fatalf("kept: %+v, %v", st, err)
	}
	b, _ := os.ReadFile(File(data))
	for _, want := range []string{`"sessions": {`, `"tab": {`, `"command": "` + door + `"`, `"key": "` + tabKey + `"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("door.json lacks %s:\n%s", want, b)
		}
	}
	if fi, _ := os.Stat(File(data)); fi.Mode().Perm() != 0o600 {
		t.Errorf("door.json's mode: %v", fi.Mode())
	}
	if fi, _ := os.Stat(filepath.Dir(File(data))); fi.Mode().Perm() != 0o700 {
		t.Errorf("its folder's mode: %v", fi.Mode())
	}
	if err := Save(data, "other", Setup{}); err == nil {
		t.Error("kept a setup of no kind")
	}
	if err := Forget(data); err != nil {
		t.Fatal(err)
	}
	if st, err := Load(data); err != nil || st.Sessions != nil || st.Tab != nil {
		t.Errorf("after forget: %+v, %v", st, err)
	}
	if err := Forget(data); err != nil {
		t.Errorf("forget with nothing kept: %v", err)
	}
}

// TestWhereThingsStand: setup until both setups are kept, then ready;
// needs while Variorum isn't installed, or a kept setup's program fails its
// checks; and no text ever holds a key.
func TestWhereThingsStand(t *testing.T) {
	data := t.TempDir()
	old := Folders
	Folders = []string{t.TempDir()}
	t.Cleanup(func() { Folders = old })
	if r := Status(data); r.State != ipc.VariorumNeeds || !strings.Contains(r.Text, Site) || r.Installed {
		t.Errorf("no Variorum: %+v", r)
	}

	app := fakeApp(t, "io.lduarte.Variorum")
	check := func(what, state, says string, sessions, tab bool) {
		t.Helper()
		r := Status(data)
		installed := !strings.Contains(says, "isn't installed")
		if r.State != state || !strings.Contains(r.Text, says) || r.Sessions != sessions || r.Tab != tab || r.Installed != installed {
			t.Errorf("%s: %+v, want %s saying %q", what, r, state, says)
		}
		if strings.Contains(r.Text, sessionsKey) || strings.Contains(r.Text, tabKey) {
			t.Errorf("%s: the text holds a key: %s", what, r.Text)
		}
	}
	check("nothing kept", ipc.VariorumSetup, "none of Variorum's setups", false, false)

	r, kind := Connect(data, claudeSetup("variorum", sessionsKey, doorOf(app)))
	if kind != Sessions || r.Error != "" || !strings.HasPrefix(r.Text, "Kept Claude Code's setup, for writing sessions. ") || !strings.Contains(r.Text, "not Any MCP client's") {
		t.Errorf("connect Claude Code's: %q, %+v", kind, r)
	}
	check("Claude Code's kept", ipc.VariorumSetup, "choose Any MCP client", true, false)

	r, kind = Connect(data, anySetup(tabKey, doorOf(app)))
	if kind != Tab || r.Error != "" || r.State != ipc.VariorumReady || !strings.HasPrefix(r.Text, "Kept Any MCP client's setup, for the Variorum tab. Ready: ") {
		t.Errorf("connect Any MCP client's: %q, %+v", kind, r)
	}
	check("both kept", ipc.VariorumReady, "Ready", true, true)

	// A setup that names another program is refused, and nothing changes.
	other := makeApp(t, Folders[0], "Other.app", "io.example.Other")
	if r, kind := Connect(data, anySetup(tabKey+"2", doorOf(other))); kind != "" || !strings.Contains(r.Error, "isn't Variorum") || strings.Contains(r.Error, tabKey) {
		t.Errorf("another program: %q, %+v", kind, r)
	}
	if st, _ := Load(data); st.Tab == nil || st.Tab.Key != tabKey {
		t.Error("a refused setup was kept")
	}
	if r, _ := Connect(data, "my key is "+tabKey); r.Error == "" || strings.Contains(r.Error, tabKey) || r.State != ipc.VariorumReady {
		t.Errorf("not a setup: %+v", r)
	}

	// Variorum gone from where the setups say.
	if err := os.Rename(app, app+".old"); err != nil {
		t.Fatal(err)
	}
	check("Variorum gone", ipc.VariorumNeeds, "Variorum isn't installed", true, true)
	makeApp(t, Folders[0], "Variorum.app", "io.lduarte.Variorum.debug")
	check("a Variorum.app again", ipc.VariorumReady, "Ready", true, true)
	if err := os.Remove(doorOf(app)); err != nil {
		t.Fatal(err)
	}
	check("a door gone", ipc.VariorumNeeds, "has no variorum-mcp", true, true)

	r = ForgetAll(data)
	if r.Error != "" || !strings.HasPrefix(r.Text, "Forgot both of Variorum's setups. ") || r.State != ipc.VariorumSetup || r.Sessions || r.Tab || !r.Installed {
		t.Errorf("forget: %+v", r)
	}

	if err := os.WriteFile(File(data), []byte("not json "+tabKey), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := Status(data); r.State != ipc.VariorumNeeds || !strings.Contains(r.Text, "variorum forget") || strings.Contains(r.Text, tabKey) {
		t.Errorf("a broken door.json: %+v", r)
	}
}
