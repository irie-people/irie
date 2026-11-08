package claude

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestSDKProfileRunsInIriesOwnPane is a session of a profile with sdk
// (DESIGN §3.5): `irie sdk` runs the same command line, which takes no task,
// and sends the task file's text as the first message.
func TestSDKProfileRunsInIriesOwnPane(t *testing.T) {
	plain, _, plainFiles := launch(t, defaultProfile(), "describe this repo")
	p := defaultProfile()
	p.SDK = true
	argv, _, files := launch(t, p, "describe this repo")
	head := []string{irieBin, "sdk", "--first", filepath.Join(sessionDir, TaskFile), "--"}
	if !slices.Equal(argv[:len(head)], head) {
		t.Fatalf("argv = %q, want it to start with %q", argv, head)
	}
	if want := plain[:len(plain)-1]; !slices.Equal(argv[len(head):], want) {
		t.Errorf("the command line = %q, want %q: the same, without the task", argv[len(head):], want)
	}
	if string(files[TaskFile]) != "describe this repo" {
		t.Errorf("task file = %q", files[TaskFile])
	}
	if string(files[SettingsFile]) != string(plainFiles[SettingsFile]) {
		t.Error("a session on the Agent SDK gets other hooks than one in Claude Code's terminal app")
	}
}

func TestSDKResumeSendsOnlyTheNextPrompt(t *testing.T) {
	p := defaultProfile()
	p.SDK = true
	s := spawnOf("describe this repo")
	argv, _, files := resume(t, p, s)
	if slices.Contains(argv, "--first") {
		t.Errorf("argv = %q: a resumed session waits for input", argv)
	}
	if !slices.Equal(argv[:3], []string{irieBin, "sdk", "--"}) || argv[len(argv)-2] != "--resume" || argv[len(argv)-1] != vendorID {
		t.Errorf("argv = %q, want irie sdk -- … --resume %s", argv, vendorID)
	}
	if _, ok := files[TaskFile]; ok {
		t.Error("a resumed session got a task file")
	}

	s.Next = "and now the tests"
	argv, _, files = resume(t, p, s)
	if i := slices.Index(argv, "--first"); i != 2 || argv[3] != filepath.Join(sessionDir, TaskFile) {
		t.Errorf("argv = %q, want the next prompt by the task file", argv)
	}
	if argv[len(argv)-1] != vendorID {
		t.Errorf("argv = %q: the prompt goes as a message, not an argument", argv)
	}
	if string(files[TaskFile]) != s.Next {
		t.Errorf("task file = %q, want %q", files[TaskFile], s.Next)
	}
}
