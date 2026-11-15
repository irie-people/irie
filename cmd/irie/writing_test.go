//go:build !nowriting

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/variorum"
)

// TestIrieVariorumKeepsAndForgetsTheSetups: with no core, irie variorum
// says where things stand, connect keeps a setup read from stdin and says
// which, a refused one leaves nothing kept and never echoes its key, and
// forget deletes both.
func TestIrieVariorumKeepsAndForgetsTheSetups(t *testing.T) {
	home, _ := bare(t)
	folder := filepath.Join(home, "Applications")
	old := variorum.Folders
	variorum.Folders = []string{folder}
	t.Cleanup(func() { variorum.Folders = old })
	contents := filepath.Join(folder, "Variorum.app", "Contents")
	if err := os.MkdirAll(filepath.Join(contents, "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>io.lduarte.Variorum</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	door := filepath.Join(contents, "MacOS", variorum.Program)
	if err := os.WriteFile(door, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	key := "vmk2_claude-code_" + strings.Repeat("k7", 21) + "x"

	run := func(stdin string, args ...string) (string, error) {
		var out bytes.Buffer
		err := variorumCmd(args, strings.NewReader(stdin), &out)
		return out.String(), err
	}
	if out, err := run("", "status"); err != nil || !strings.Contains(out, "none of Variorum's setups") {
		t.Errorf("status: %q, %v", out, err)
	}
	setup := fmt.Sprintf("claude mcp remove -s user variorum >/dev/null 2>&1; claude mcp add -s user variorum -e VARIORUM_MCP_KEY=%s -- %q\n", key, door)
	if out, err := run(setup, "connect"); err != nil || !strings.HasPrefix(out, "Kept Claude Code's setup, for writing sessions.") || strings.Contains(out, key) {
		t.Errorf("connect: %q, %v", out, err)
	}
	if out, err := run("my key is "+key, "connect"); err == nil || strings.Contains(err.Error(), key) || out != "" {
		t.Errorf("connect what isn't a setup: %q, %v", out, err)
	}
	p, err := paths.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if st, err := variorum.Load(p.Data); err != nil || st.Sessions == nil || st.Sessions.Key != key || st.Tab != nil {
		t.Errorf("kept: %v, %v", st, err)
	}
	if out, err := run(""); err != nil || !strings.Contains(out, "not Any MCP client's") {
		t.Errorf("irie variorum: %q, %v", out, err)
	}
	if out, err := run("", "forget"); err != nil || !strings.HasPrefix(out, "Forgot both of Variorum's setups.") {
		t.Errorf("forget: %q, %v", out, err)
	}
	if _, err := os.Stat(variorum.File(p.Data)); !os.IsNotExist(err) {
		t.Errorf("door.json after forget: %v", err)
	}
	for _, args := range [][]string{{"read"}, {"status", "more"}} {
		if _, err := run("", args...); err == nil {
			t.Errorf("irie variorum %s went through", strings.Join(args, " "))
		}
	}
}

// TestIrieVariorumDoorSaysWhatToDo: with no setup kept, the door answers
// the session's initialize with one error, and nothing else on stdout.
func TestIrieVariorumDoorSaysWhatToDo(t *testing.T) {
	bare(t)
	var out bytes.Buffer
	in := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"clientInfo":{"name":"claude-code"}}}` + "\n"
	if code := variorumDoor(nil, strings.NewReader(in), &out); code != 0 {
		t.Errorf("exit %d", code)
	}
	var m struct {
		ID    int `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &m); err != nil || m.Error.Code != -32000 || !strings.Contains(m.Error.Message, "pbpaste | irie variorum connect") {
		t.Errorf("the door answered %q (%v)", out.String(), err)
	}
}

// TestIrieVariorumWithTheTabOff: the setups serve :write sessions too, so
// irie variorum keeps them with the tab's switch off, and names that
// switch only once writing in Variorum is off as well.
func TestIrieVariorumWithTheTabOff(t *testing.T) {
	bare(t)
	cfg, p, err := load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Variorum.Tab = false
	if r, err := variorumAction(cfg, p, ipc.VariorumStatus, strings.NewReader("")); err != nil || r.State == ipc.VariorumOff {
		t.Errorf("the tab off: %+v, %v", r, err)
	}
	cfg.Skills.Disabled = []string{"variorum"}
	if r, err := variorumAction(cfg, p, ipc.VariorumConnect, strings.NewReader("claude mcp add")); err != nil || r.State != ipc.VariorumOff || !strings.Contains(r.Error, "irie plugins on writing tab") {
		t.Errorf("both parts off: %+v, %v", r, err)
	}
}
