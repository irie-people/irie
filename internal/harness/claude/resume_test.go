package claude

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
)

func resume(t *testing.T, p config.Profile, s harness.Spawn) ([]string, map[string]string, map[string][]byte) {
	t.Helper()
	argv, env, files, err := Harness{}.Resume(p, s)
	if err != nil {
		t.Fatal(err)
	}
	return argv, env, files
}

// TestResumeArgvGolden is the argv of a conversation moved into Irie
// (DESIGN §4.6): the profile's flags and Irie's settings, then --resume
// with the conversation's own id. A resumed session keeps its id, so there
// is no --session-id, and it waits for input, so there is no task.
func TestResumeArgvGolden(t *testing.T) {
	s := spawnOf("")
	argv, env, files := resume(t, defaultProfile(), s)
	got, err := json.MarshalIndent(argv, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "argv-resume.golden", append(got, '\n'))
	if i := slices.Index(argv, "--resume"); i < 0 || argv[i+1] != vendorID || i+2 != len(argv) {
		t.Errorf("argv = %q, want it to end with --resume %s", argv, vendorID)
	}
	for _, flag := range []string{"--session-id", "--name", "--fork-session"} {
		if slices.Contains(argv, flag) {
			t.Errorf("argv = %q: %s does not belong to a resumed session", argv, flag)
		}
	}
	if len(env) != 0 {
		t.Errorf("env = %v, want none", env)
	}
	if want := []string{SettingsFile}; !slices.Equal(slices.Sorted(maps.Keys(files)), want) {
		t.Errorf("files = %v, want only %v: there is no task", slices.Sorted(maps.Keys(files)), want)
	}
	launched, _, launchFiles := launch(t, defaultProfile(), "x")
	if string(files[SettingsFile]) != string(launchFiles[SettingsFile]) {
		t.Error("a resumed session gets other hooks than a new one")
	}
	if i := slices.Index(launched, "--settings"); !reflect.DeepEqual(argv[:i+2], launched[:i+2]) {
		t.Errorf("resume argv %q and launch argv %q differ before --settings", argv, launched)
	}
}

func TestResumeIgnoresTheTaskAndTheName(t *testing.T) {
	s := spawnOf("describe this repo")
	argv, _, files := resume(t, defaultProfile(), s)
	if slices.Contains(argv, "describe this repo") || slices.Contains(argv, s.Name) {
		t.Errorf("argv = %q", argv)
	}
	if _, ok := files[TaskFile]; ok {
		t.Error("a resumed session got a task file")
	}
}

func TestResumeNeedsAConversation(t *testing.T) {
	for _, id := range []string{"", "--fork-session"} {
		s := spawnOf("")
		s.VendorID = id
		if _, _, _, err := (Harness{}).Resume(defaultProfile(), s); err == nil {
			t.Errorf("vendor id %q: no error", id)
		}
	}
	var _ harness.Resumer = Harness{}
}
