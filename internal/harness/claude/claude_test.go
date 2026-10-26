package claude

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/harness"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	sessionDir = "/home/alice/.local/state/irie/sessions/s0k3j9x2m4a7q"
	vendorID   = "6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f"
)

func spawnOf(task string) harness.Spawn {
	return harness.Spawn{
		Session:  "s0k3j9x2m4a7q",
		Name:     "describe-this-repo",
		Task:     task,
		Cwd:      "/home/alice/code/website",
		Dir:      sessionDir,
		VendorID: vendorID,
	}
}

func defaultProfile() config.Profile { return config.Default().Profiles["claude"] }

func launch(t *testing.T, p config.Profile, task string) ([]string, map[string]string, map[string][]byte) {
	t.Helper()
	argv, env, files, err := Harness{}.Launch(p, spawnOf(task))
	if err != nil {
		t.Fatal(err)
	}
	return argv, env, files
}

func TestArgvGoldenForDefaultProfile(t *testing.T) {
	const task = "describe this repo in one paragraph"
	argv, env, files := launch(t, defaultProfile(), task)
	got, err := json.MarshalIndent(argv, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("testdata", "argv-default.golden")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("argv:\n%s\nwant:\n%s", got, want)
	}
	if len(env) != 0 {
		t.Errorf("env = %v, want none", env)
	}
	if string(files["task.md"]) != task {
		t.Errorf("task.md = %q", files["task.md"])
	}
}

func TestLargeTaskGoesThroughTaskFile(t *testing.T) {
	task := strings.Repeat("line of a very long task\n", (2<<20)/25+1)
	if len(task) < 2<<20 {
		t.Fatalf("test setup: task is %d bytes", len(task))
	}
	argv, _, files := launch(t, defaultProfile(), task)
	want := "Your full task is in " + sessionDir + "/task.md. Read all of it before doing anything else."
	if got := argv[len(argv)-1]; got != want {
		t.Errorf("task argument = %q, want %q", got, want)
	}
	if !bytes.Equal(files["task.md"], []byte(task)) {
		t.Error("task.md is not byte-identical to the task")
	}
	for _, a := range argv {
		if len(a) > InlineMax {
			t.Errorf("an argument is %d bytes", len(a))
		}
	}
}

func TestInlineLimitIs100KiB(t *testing.T) {
	atLimit := strings.Repeat("a", 100<<10)
	argv, _, _ := launch(t, defaultProfile(), atLimit)
	if argv[len(argv)-1] != atLimit {
		t.Error("a 100 KiB task was not passed inline")
	}
	argv, _, _ = launch(t, defaultProfile(), atLimit+"b")
	if !strings.HasPrefix(argv[len(argv)-1], "Your full task is in ") {
		t.Error("a task over 100 KiB was passed inline")
	}
}

func TestNewlinesSurvive(t *testing.T) {
	const task = "line one\nline two\n\n    indented\r\n\ttabbed\nlast"
	argv, _, files := launch(t, defaultProfile(), task)
	if got := argv[len(argv)-1]; got != task {
		t.Errorf("task argument = %q, want %q", got, task)
	}
	if string(files["task.md"]) != task {
		t.Errorf("task.md = %q, want %q", files["task.md"], task)
	}
}

func TestLeadingDashGetsASpace(t *testing.T) {
	const task = "-v makes the build fail; find out why"
	argv, _, files := launch(t, defaultProfile(), task)
	if got := argv[len(argv)-1]; got != " "+task {
		t.Errorf("task argument = %q", got)
	}
	if string(files["task.md"]) != task {
		t.Errorf("task.md = %q", files["task.md"])
	}
}

func TestTaskWithNULGoesThroughTaskFile(t *testing.T) {
	argv, _, files := launch(t, defaultProfile(), "a\x00b")
	if !strings.HasPrefix(argv[len(argv)-1], "Your full task is in ") {
		t.Errorf("task argument = %q", argv[len(argv)-1])
	}
	if string(files["task.md"]) != "a\x00b" {
		t.Errorf("task.md = %q", files["task.md"])
	}
}

func TestProfileArgsFollowTheCommand(t *testing.T) {
	p := defaultProfile()
	p.Command = "/bin/sh"
	p.Args = []string{"-c", "echo ready; sleep 600"}
	argv, _, _ := launch(t, p, "x")
	if want := []string{"/bin/sh", "-c", "echo ready; sleep 600", "--model"}; !reflect.DeepEqual(argv[:4], want) {
		t.Errorf("argv = %q", argv)
	}
}

func TestEmptyValuesAreLeftOut(t *testing.T) {
	p := config.Profile{Harness: "claude-code"}
	argv, _, _ := launch(t, p, "x")
	if want := []string{"claude", "--session-id", vendorID, "x"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

func TestProfileEnvIsPassed(t *testing.T) {
	p := defaultProfile()
	p.Env = map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:11434"}
	_, env, _ := launch(t, p, "x")
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:11434" {
		t.Errorf("env = %v", env)
	}
	env["ANTHROPIC_BASE_URL"] = "changed"
	if p.Env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:11434" {
		t.Error("Launch returned the profile's own env map")
	}
}

func TestVendorIDIsRequired(t *testing.T) {
	s := spawnOf("x")
	s.VendorID = ""
	if _, _, _, err := (Harness{}).Launch(defaultProfile(), s); err == nil {
		t.Error("no error without a vendor id")
	}
}

func TestSessionIDFlagCarriesTheVendorID(t *testing.T) {
	argv, _, _ := launch(t, defaultProfile(), "x")
	i := slices.Index(argv, "--session-id")
	if i < 0 || argv[i+1] != vendorID {
		t.Errorf("argv = %q", argv)
	}
}
