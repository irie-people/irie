package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/irie-people/irie/internal/brand"
)

// TestAwayHelperNeedsTheHelper: an app built before the away helper would
// open its own window if run with --away-helper, the very thing irie away
// is for, so only an app whose Info.plist names the helper is taken.
func TestAwayHelperNeedsTheHelper(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := filepath.Join(t.TempDir(), brand.Name+" Preview.app")
	exe := filepath.Join(app, "Contents", "MacOS", brand.Exe+"Preview")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(brand.Env("APP"), app)
	plist := filepath.Join(app, "Contents", "Info.plist")

	if err := os.WriteFile(plist, []byte("<plist><dict><key>CFBundleExecutable</key><string>x</string></dict></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := awayHelper(); err == nil && got == exe {
		t.Errorf("took an app without the helper: %s", got)
	}

	if err := os.WriteFile(plist, []byte("<plist><dict><key>AwayHelperVersion</key><string>1</string></dict></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := awayHelper(); err != nil || got != exe {
		t.Errorf("awayHelper() = %q, %v; want %q", got, err, exe)
	}
}

// TestAwayNeedsSomethingToKeepAway: no command and no app, or a negative
// wait, is a usage error, before anything runs.
func TestAwayNeedsSomethingToKeepAway(t *testing.T) {
	for _, args := range [][]string{nil, {"--for", "30s"}, {"--for", "-1s", "--", "true"}} {
		var ec exitCode
		if err := away(args); !errors.As(err, &ec) || ec.code != 2 {
			t.Errorf("away(%q) = %v, want exit code 2", args, err)
		}
	}
}
