package generic

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
)

const sessionDir = "/home/alice/.local/state/irie/sessions/s0k3j9x2m4a7q"

func spawnOf(task string) harness.Spawn {
	return harness.Spawn{
		Session: "s0k3j9x2m4a7q", Name: "fix-it", Task: task, Cwd: "/home/alice/code/website", Dir: sessionDir,
		VendorID: "6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f", Bin: "/home/alice/.local/bin/irie", HoldMax: 24 * time.Hour,
	}
}

func aider(task string) config.Profile {
	return config.Profile{
		Harness: "generic", Command: "aider", Args: []string{"--model", "ollama_chat/gpt-oss:20b"},
		Env: map[string]string{"OLLAMA_API_BASE": "http://127.0.0.1:11434"}, Task: task,
	}
}

func launch(t *testing.T, p config.Profile, task string) ([]string, map[string]string, map[string][]byte) {
	t.Helper()
	argv, env, files, err := Harness{}.Launch(p, spawnOf(task))
	if err != nil {
		t.Fatal(err)
	}
	return argv, env, files
}

func TestArgPutsTheTaskLast(t *testing.T) {
	for _, mode := range []string{"arg", ""} {
		argv, env, files := launch(t, aider(mode), "fix the flaky test")
		if want := []string{"aider", "--model", "ollama_chat/gpt-oss:20b", "fix the flaky test"}; !reflect.DeepEqual(argv, want) {
			t.Errorf("task %q: argv = %q, want %q", mode, argv, want)
		}
		if env["OLLAMA_API_BASE"] != "http://127.0.0.1:11434" || len(env) != 1 {
			t.Errorf("env = %v", env)
		}
		if string(files[harness.TaskFile]) != "fix the flaky test" || len(files) != 1 {
			t.Errorf("files = %v", files)
		}
	}
}

func TestPasteLeavesTheTaskOutOfArgv(t *testing.T) {
	argv, _, files := launch(t, aider("paste"), "fix the flaky test\nand say why")
	if want := []string{"aider", "--model", "ollama_chat/gpt-oss:20b"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	if string(files[harness.TaskFile]) != "fix the flaky test\nand say why" {
		t.Errorf("task.md = %q", files[harness.TaskFile])
	}
}

func TestALargeTaskArgPointsAtTheFile(t *testing.T) {
	task := strings.Repeat("x", harness.InlineMax+1)
	argv, _, files := launch(t, aider("arg"), task)
	if got, want := argv[len(argv)-1], "Your full task is in "+sessionDir+"/task.md. Read all of it before doing anything else."; got != want {
		t.Errorf("task argument = %q", got)
	}
	if !bytes.Equal(files[harness.TaskFile], []byte(task)) {
		t.Error("task.md is not the task")
	}
	argv, _, _ = launch(t, aider("arg"), "-v shows it")
	if argv[len(argv)-1] != " -v shows it" {
		t.Errorf("task argument = %q", argv[len(argv)-1])
	}
}

func TestTheEnvIsACopy(t *testing.T) {
	p := aider("arg")
	_, env, _ := launch(t, p, "x")
	env["OLLAMA_API_BASE"] = "changed"
	if p.Env["OLLAMA_API_BASE"] != "http://127.0.0.1:11434" {
		t.Error("Launch returned the profile's own env map")
	}
}

func TestACommandIsRequired(t *testing.T) {
	p := aider("arg")
	p.Command = ""
	if _, _, _, err := (Harness{}).Launch(p, spawnOf("x")); err == nil {
		t.Error("no error without a command")
	}
}

func TestGenericIsLaunchOnly(t *testing.T) {
	h := Harness{}
	if h.ID() != "generic" {
		t.Errorf("id %q", h.ID())
	}
	if l := h.Levels(); l != harness.Generic || l.Has(harness.Observed) || l.Has(harness.Held) {
		t.Errorf("levels %v, want G only", l)
	}
	if evs, err := h.Normalize([]byte(`{"hook_event_name":"Stop"}`)); err == nil || len(evs) != 0 {
		t.Errorf("Normalize = %v, %v; want an error: a generic harness has no hooks", evs, err)
	}
	if out := h.Respond(event.Event{}, harness.HookReply{Context: "x"}); out != nil {
		t.Errorf("Respond = %q", out)
	}
}
