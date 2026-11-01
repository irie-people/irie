// Package focus reads what the person at the Mac is doing, for Gabriel
// (DESIGN §4.11): the app in front, the processes that use the most CPU,
// and the processes macOS is asked to keep awake for, such as one that
// plays sound or video or holds a call. Each read runs one small macOS
// tool (lsappinfo, ps or pmset) and parses what it prints; on another
// system a read returns errors.ErrUnsupported.
package focus

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// App is the app in front.
type App struct {
	Name       string // such as Safari
	BundleID   string // such as com.apple.Safari
	BundlePath string // such as /Applications/Safari.app
	PID        int
}

// Proc is one process, as ps sees it.
type Proc struct {
	PID, PPID, UID int
	CPU            float64 // percent of one CPU, as ps counts it
	Path           string  // the executable, by its full path when ps knows it
}

// timeout bounds each tool's run.
const timeout = 3 * time.Second

func run(name string, args ...string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

// Front returns the app in front, by `lsappinfo list`; ok is false when no
// app is.
func Front() (App, bool, error) {
	out, err := run("/usr/bin/lsappinfo", "list")
	if err != nil {
		return App{}, false, err
	}
	a, ok := ParseFront(out)
	return a, ok, nil
}

var (
	frontName = regexp.MustCompile(`^\s*\d+\) "(.*)" ASN:\S+ \(in front\)`)
	appLine   = regexp.MustCompile(`^\s*\d+\) "`)
	quoted    = regexp.MustCompile(`^\s*(bundleID|bundle path)="(.*)"`)
	pidField  = regexp.MustCompile(`^\s*pid = (\d+)\b`)
)

// ParseFront finds the app marked "(in front)" in the output of `lsappinfo
// list`: its name on that line, then its bundle id, bundle path and pid on
// the lines that follow, up to the next app.
func ParseFront(out string) (App, bool) {
	var a App
	in := false
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if m := frontName.FindStringSubmatch(line); m != nil {
			a, in = App{Name: m[1]}, true
			continue
		}
		if !in {
			continue
		}
		if appLine.MatchString(line) {
			break // the next app
		}
		if m := quoted.FindStringSubmatch(line); m != nil {
			if m[1] == "bundleID" {
				a.BundleID = m[2]
			} else {
				a.BundlePath = m[2]
			}
		}
		if m := pidField.FindStringSubmatch(line); m != nil {
			a.PID, _ = strconv.Atoi(m[1])
			break // the pid comes after the bundle's fields
		}
	}
	return a, in && a.PID > 0
}

// Procs returns every process, by `ps -Ao pid=,ppid=,uid=,pcpu=,comm=`.
func Procs() ([]Proc, error) {
	out, err := run("/bin/ps", "-Ao", "pid=,ppid=,uid=,pcpu=,comm=")
	if err != nil {
		return nil, err
	}
	return ParseProcs(out), nil
}

// ParseProcs reads the output of Procs's ps: four numbers and then the
// executable, which may hold spaces.
func ParseProcs(out string) []Proc {
	var ps []Proc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		uid, e3 := strconv.Atoi(f[2])
		cpu, e4 := strconv.ParseFloat(f[3], 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		// The executable is the rest of the line, spaces and all.
		rest := line
		for i := 0; i < 4; i++ {
			rest = strings.TrimLeft(rest, " \t")
			rest = rest[strings.IndexAny(rest, " \t"):]
		}
		ps = append(ps, Proc{PID: pid, PPID: ppid, UID: uid, CPU: cpu, Path: strings.TrimSpace(rest)})
	}
	return ps
}

// LowPower reports whether macOS's Low Power Mode is on, which slows the
// whole machine to save power (`pmset -g`, whose lowpowermode is 1 then).
func LowPower() bool {
	out, err := run("/usr/bin/pmset", "-g")
	return err == nil && ParseLowPower(out)
}

// ParseLowPower reads the output of `pmset -g`.
func ParseLowPower(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "lowpowermode" {
			return f[1] == "1"
		}
	}
	return false
}

// Awake returns the processes macOS is asked to keep the Mac or its
// display awake for, by `pmset -g assertions`: each assertion's owner, and
// the process it was made for, as coreaudiod makes one for an app that
// plays sound.
func Awake() (map[int]bool, error) {
	out, err := run("/usr/bin/pmset", "-g", "assertions")
	if err != nil {
		return nil, err
	}
	return ParseAwake(out), nil
}

var (
	owner      = regexp.MustCompile(`^\s*pid (\d+)\([^)]*\): \[[^\]]*\] \S+ (\S+)`)
	createdFor = regexp.MustCompile(`Created for PID: (\d+)`)
)

// awakeKinds are the assertions that say a process is doing something the
// person wants kept going: sound, video, a call, a download.
var awakeKinds = map[string]bool{
	"PreventUserIdleDisplaySleep": true,
	"PreventUserIdleSystemSleep":  true,
	"NoDisplaySleepAssertion":     true,
	"NoIdleSleepAssertion":        true,
	"PreventSystemSleep":          true,
}

// ParseAwake reads the output of `pmset -g assertions`.
func ParseAwake(out string) map[int]bool {
	pids := map[int]bool{}
	held := false
	for _, line := range strings.Split(out, "\n") {
		if m := owner.FindStringSubmatch(line); m != nil {
			held = awakeKinds[m[2]]
			if held {
				pid, _ := strconv.Atoi(m[1])
				pids[pid] = true
			}
			continue
		}
		if m := createdFor.FindStringSubmatch(line); m != nil && held {
			pid, _ := strconv.Atoi(m[1])
			pids[pid] = true
		}
	}
	return pids
}
