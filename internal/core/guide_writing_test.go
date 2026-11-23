//go:build !nowriting

package core

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
)

// The writing guide is the writing plugin's, so its test is too.

// TestTheWriteGuideTakesATask: :write starts a writing session in Variorum,
// a Claude Code session on +claude in the guide's own folder that runs
// /irie:variorum, with what the writer wants done after it; that names the
// session, as a typed task would.
func TestTheWriteGuideTakesATask(t *testing.T) {
	c := prepare(t, agentConfig{})
	c.WriteConfig(t, agentConfig{extra: fmt.Sprintf("[profiles.claude]\ncommand = %q\nenv = { IRIE_FAKE_PLUGINS = \"1\" }\n", c.fake)}.toml(c.fake))
	// The plugin folders are read-only; the test's folders go when it ends.
	t.Cleanup(func() {
		filepath.WalkDir(c.paths.Skills(), func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o700)
			}
			return nil
		})
	})
	c.up()

	for _, tc := range []struct{ text, name, task string }{
		{":write", "Variorum", "/" + brand.Slug + ":variorum"},
		{":write notes on Chapter 3", "Notes on Chapter 3", "/" + brand.Slug + ":variorum notes on Chapter 3"},
	} {
		var r ipc.InputResult
		if err := c.call(ipc.OpInput, ipc.InputArgs{Text: tc.text}, &r); err != nil {
			t.Fatal(err)
		}
		if r.Did != ipc.InputRan || !r.Show || r.Session == nil || r.Session.Name != tc.name || !strings.Contains(r.Text, "trust the guide's folder") {
			t.Fatalf("%s: input = %+v", tc.text, r)
		}
		spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
		if err != nil {
			t.Fatal(err)
		}
		if spec.Profile != "claude" || !slices.Contains(spec.Argv, "--plugin-dir") || spec.Argv[len(spec.Argv)-1] != tc.task || spec.Cwd != filepath.Join(c.paths.Data, "guides", "variorum") {
			t.Fatalf("%s: spec = %+v", tc.text, spec)
		}
		// The fake agent, like Claude Code, refuses a skill it cannot find.
		c.waitState(r.Session.ID, ipc.StateWorking)
	}
}
