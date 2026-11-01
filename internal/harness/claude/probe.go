package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
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

// optional are flags Irie passes only to a CLI whose --help lists them.
// --plugin-dir also needs the CLI to load a plugin passed with it (plugins);
// --agents and --agent bring Irie's arch agents (DESIGN §4.11).
var optional = []string{"--name", "--plugin-dir", "--agents", "--agent"}

// probeTimeout bounds each run of the CLI, and pluginTimeout the run that
// lists its plugins, which reads every plugin's manifest.
var (
	probeTimeout  = 5 * time.Second
	pluginTimeout = 20 * time.Second
)

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
	}
	again := false
	if pr.Flags["--plugin-dir"] {
		if why, retry := plugins(path, p.Args, env); why != "" {
			delete(pr.Flags, "--plugin-dir")
			pr.Warnings = append(pr.Warnings, "Irie's skills left out: "+why)
			again = retry
		}
	}
	// A CLI too slow to list its plugins is asked again at the next launch.
	if !again {
		probeMu.Lock()
		probes[key] = pr
		probeMu.Unlock()
	}
	return clone(pr)
}

// plugins checks that the CLI loads a plugin passed with --plugin-dir, by
// listing its plugins with a throwaway one named irie: managed settings may
// refuse the flag (disableSideloadFlags), which would stop every launch, and
// Irie's plugin would stand in for one of the user's own named irie. It
// returns why Irie's skills are left out, "" when they can go in, and whether
// to look again at the next launch.
func plugins(path string, args, env []string) (why string, retry bool) {
	dir, err := os.MkdirTemp("", "irie-plugin-probe-")
	if err != nil {
		return "no folder for the plugin probe: " + err.Error(), true
	}
	defer os.RemoveAll(dir)
	manifest := filepath.Join(dir, ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		return "no folder for the plugin probe: " + err.Error(), true
	}
	if err := os.WriteFile(manifest, []byte(`{"name": "irie", "description": "Irie's probe", "version": "0"}`+"\n"), 0o600); err != nil {
		return "no folder for the plugin probe: " + err.Error(), true
	}
	out, err := probeFor(pluginTimeout, path, append(slices.Clone(args), "--plugin-dir", dir, "plugin", "list", "--json"), env)
	var exit *exec.ExitError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &exit) && exit.ProcessState != nil && !exit.Exited()):
		return "Claude Code took too long to list its plugins", true
	case errors.As(err, &exit):
		return "Claude Code refused --plugin-dir: " + firstLine(string(exit.Stderr)), false
	case err != nil:
		return "Claude Code could not list its plugins: " + err.Error(), true
	}
	return pluginList(out), false
}

// pluginList reads `claude plugin list --json`. Irie's throwaway plugin must
// be in it, loaded for the session as irie@inline, and no plugin of the
// user's own may be named irie. It returns why skills are left out, or "".
func pluginList(out string) string {
	var list []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return "Claude Code's list of plugins could not be read"
	}
	loaded := false
	for _, p := range list {
		name, from, _ := strings.Cut(p.ID, "@")
		switch {
		case name != "irie":
		case from == "inline":
			loaded = loaded || p.Enabled
		default:
			return "a plugin of your own is named irie (" + p.ID + ")"
		}
	}
	if !loaded {
		return "Claude Code did not load a plugin passed with --plugin-dir"
	}
	return ""
}

// firstLine is the first line of s that says something.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "Warning:") {
			return l
		}
	}
	return "no reason given"
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
	return probeFor(probeTimeout, path, args, env)
}

// probeFor is probe with a timeout of its own. A run killed at its timeout
// returns context.DeadlineExceeded.
func probeFor(timeout time.Duration, path string, args, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return string(out), ctx.Err()
	}
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
