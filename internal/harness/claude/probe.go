package claude

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/host"
)

// MinVersion is the first Claude Code with a hook's onFailure, which lets
// Irie hold a call without the call going through when the hook fails or
// times out (DESIGN §3.3).
const MinVersion = "2.1.295"

// optional are flags Irie passes only to a CLI whose --help lists them; one
// it lacks is left out with a warning.
var optional = []string{"--name"}

// quietly are optional flags whose lack needs no warning on every spawn, as
// they serve only some sessions: --append-system-prompt carries tutor mode's
// ask for diagrams (learn.visual).
var quietly = []string{"--append-system-prompt"}

// probeTimeout bounds each run of the CLI.
var probeTimeout = 5 * time.Second

var (
	probeMu sync.Mutex
	probes  = map[string]harness.Probe{} // by binary, its size and mtime, args and env
)

// Probe runs `<command> <args> --version`, and `--help` for the optional
// flags, once per binary: a new version is a new file. A command that is not
// found gives an empty probe, and the spawn then fails with the shell's own
// message.
func (Harness) Probe(p config.Profile, env []string) harness.Probe {
	command := p.Command
	if command == "" {
		command = "claude"
	}
	path, err := host.LookPath(command, env)
	if err != nil {
		return harness.Probe{}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return harness.Probe{}
	}
	key := fmt.Sprintf("%s\x00%d\x00%d\x00%q\x00%q", path, fi.Size(), fi.ModTime().UnixNano(), p.Args, profileEnv(p.Env))
	probeMu.Lock()
	cached, ok := probes[key]
	probeMu.Unlock()
	if ok {
		return clone(cached)
	}

	var pr harness.Probe
	out, err := probe(path, append(slices.Clone(p.Args), "--version"), env)
	pr.Version = ParseVersion(out)
	switch {
	case pr.Version == "":
		pr.Warnings = append(pr.Warnings, fmt.Sprintf("pause off: no version from `%s --version`", command))
	case !AtLeast(pr.Version, MinVersion):
		pr.Warnings = append(pr.Warnings, fmt.Sprintf("pause off: Claude Code %s is older than %s", pr.Version, MinVersion))
	}
	if err == nil {
		help, _ := probe(path, append(slices.Clone(p.Args), "--help"), env)
		for _, f := range optional {
			if listsFlag(help, f) {
				if pr.Flags == nil {
					pr.Flags = map[string]bool{}
				}
				pr.Flags[f] = true
			} else {
				pr.Warnings = append(pr.Warnings, fmt.Sprintf("%s left out: `%s --help` does not list it", f, command))
			}
		}
		for _, f := range quietly {
			if listsFlag(help, f) {
				if pr.Flags == nil {
					pr.Flags = map[string]bool{}
				}
				pr.Flags[f] = true
			}
		}
	}
	probeMu.Lock()
	probes[key] = pr
	probeMu.Unlock()
	return clone(pr)
}

func profileEnv(env map[string]string) []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(env)) {
		out = append(out, k+"="+env[k])
	}
	return out
}

func clone(p harness.Probe) harness.Probe {
	p.Flags = maps.Clone(p.Flags)
	p.Warnings = slices.Clone(p.Warnings)
	return p
}

// probe runs the CLI in its own process group, which is killed when it takes
// longer than probeTimeout, and returns what it printed.
func probe(path string, args, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	return string(out), err
}

var versionRE = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ParseVersion finds the first X.Y.Z in the output of --version, such as
// "2.1.310 (Claude Code)".
func ParseVersion(out string) string { return versionRE.FindString(out) }

// AtLeast reports whether version v is min or later. An empty or unreadable
// v is not.
func AtLeast(v, min string) bool {
	a, b := versionRE.FindStringSubmatch(v), versionRE.FindStringSubmatch(min)
	if a == nil || b == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x > y
		}
	}
	return true
}

// listsFlag reports whether help text lists flag as an option.
func listsFlag(help, flag string) bool {
	re := regexp.MustCompile(`(^|[\s,])` + regexp.QuoteMeta(flag) + `\b`)
	for _, line := range strings.Split(help, "\n") {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}
