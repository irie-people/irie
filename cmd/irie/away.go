package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

const awayUsage = `usage: ` + brand.Slug + ` away [--for DURATION] [--app NAME]... -- COMMAND [ARG...]
       ` + brand.Slug + ` away [--for DURATION] --app NAME...

Runs COMMAND so that no window it opens lands on a desktop the user can
see: each window of a process it starts, of an app it opens with open(1),
or of an app named with --app (its name, bundle id, path or pid) goes to a
desktop they are not looking at, while COMMAND runs and for --for after it
ends (default 10s). An app it started that cannot move is hidden instead.
Exits with COMMAND's status, or 3 when a window stayed in sight. On macOS
only, with ` + brand.Name + `.app installed.
`

// away runs a command whose windows must stay off the desktops the user can
// see (DESIGN §4.11, "Out of sight"). The Mac app's own executable, run with
// --away-helper, runs the command and moves its windows
// (mac/Sources/Irie/Away.swift); it takes this process's place, so signals
// and the exit status are the command's.
func away(args []string) error {
	fs := flag.NewFlagSet("away", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, aligned(awayUsage)) }
	wait := 10 * time.Second
	fs.Func("for", "how long to watch for windows after COMMAND ends, such as 30s or 30 (default 10s)", func(s string) error {
		d, err := time.ParseDuration(s)
		if n, nerr := strconv.ParseFloat(s, 64); nerr == nil {
			d, err = time.Duration(n*float64(time.Second)), nil
		}
		if err != nil || d < 0 {
			return errors.New("want a duration, such as 30s")
		}
		wait = d
		return nil
	})
	var apps []string
	fs.Func("app", "an app whose new windows go out of sight too", func(s string) error {
		apps = append(apps, s)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return exitCode{2, err}
	}
	command := fs.Args()
	if len(command) == 0 && len(apps) == 0 {
		return exitCode{2, errors.New("away: name a command after --, or an app with --app (" + brand.Slug + " away --help)")}
	}
	if runtime.GOOS != "darwin" {
		return exitCode{2, errors.New("away keeps windows off the user's desktop on macOS only; here, run the command without a window, such as a browser headless")}
	}
	helper, err := awayHelper()
	if err != nil {
		return exitCode{2, err}
	}
	argv := []string{helper, "--away-helper", "--for", strconv.FormatFloat(wait.Seconds(), 'f', -1, 64)}
	for _, a := range apps {
		argv = append(argv, "--app", a)
	}
	if len(command) > 0 {
		argv = append(append(argv, "--"), command...)
	}
	return syscall.Exec(helper, argv, os.Environ())
}

// awayHelper is the executable of the Mac app that carries the away helper:
// the one IRIE_APP names, such as a preview build, else the installed one
// in ~/Applications or /Applications. An app built before the helper, whose
// Info.plist lacks AwayHelperVersion, would open its window instead of
// keeping one away, so it does not count.
func awayHelper() (string, error) {
	var apps []string
	if a := os.Getenv(brand.Env("APP")); a != "" {
		apps = append(apps, a)
	}
	if home, err := os.UserHomeDir(); err == nil {
		apps = append(apps, filepath.Join(home, "Applications", brand.Name+".app"))
	}
	apps = append(apps, filepath.Join("/Applications", brand.Name+".app"))
	old := ""
	for _, app := range apps {
		plist, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
		if err != nil {
			continue
		}
		if !bytes.Contains(plist, []byte("AwayHelperVersion")) {
			old = app
			continue
		}
		for _, exe := range []string{brand.Exe, brand.Exe + "Preview"} {
			p := filepath.Join(app, "Contents", "MacOS", exe)
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				return p, nil
			}
		}
	}
	if old != "" {
		return "", fmt.Errorf("away: %s is older than the helper that keeps windows away; install a newer %s.app, or run the command without a window, such as a browser headless", old, brand.Name)
	}
	return "", fmt.Errorf("away needs %s.app, which carries the helper that keeps windows away; install it, or run the command without a window, such as a browser headless", brand.Name)
}
