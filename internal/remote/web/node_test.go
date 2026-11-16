package web

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runInNode runs a check of the app's modules in Node: the script, beside
// copies of js/, as ES modules in a folder of their own. The script prints
// "ok", or what failed and exits non-zero. Without Node the test is
// skipped.
func runInNode(t *testing.T, script string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node to run the app's scripts")
	}
	dir := t.TempDir()
	files := map[string]string{"package.json": `{"type":"module"}`, "check.js": script}
	mods, err := fs.Glob(FS, "js/*.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range mods {
		b, err := fs.ReadFile(FS, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(b)
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "check.js")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("node check.js: %v\n%s", err, out)
	}
}
