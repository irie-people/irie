// Package wsl is the Windows wrapper's logic (docs/WINDOWS.md). Irie runs
// inside WSL 2, Windows' own Linux, as it runs on any Linux; the wrapper,
// irie.exe, sets WSL up, installs Irie in it and starts it there from a
// Windows terminal, the Start menu or sign-in. Nothing here needs Windows
// to build or to test: the wrapper's main runs the commands it makes
// through a Machine, which tests stand in for.
package wsl

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/irie-people/irie/internal/brand"
)

// DefaultDistro is the distribution the wrapper installs when it finds none
// to run in: WSL's own default.
const DefaultDistro = "Ubuntu"

// MinBuild is the first Windows build `wsl --install` works on: Windows 10
// version 2004. Every Windows 11 is later.
const MinBuild = 19041

// Distro is one Linux distribution WSL has.
type Distro struct {
	Name    string
	State   string // as wsl.exe says it, such as Running or Stopped
	Version int    // 1 or 2
	Default bool   // what wsl.exe runs when it is given no -d
}

// Decode turns wsl.exe's output into text. wsl.exe writes UTF-16LE into a
// pipe unless WSL_UTF8=1 is set, which older versions ignore, so it reads
// both.
func Decode(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xff && b[1] == 0xfe || b[0] != 0 && b[1] == 0) {
		if b[0] == 0xff && b[1] == 0xfe {
			b = b[2:]
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}

// ParseList reads `wsl.exe --list --verbose`: a header, then a line for
// each distribution, the default one marked with *, which ends in its WSL
// version. Any other line, such as the header in any language or the note
// of a WSL with none, is left out.
func ParseList(out string) []Distro {
	var ds []Distro
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		def := strings.HasPrefix(line, "*")
		f := strings.Fields(strings.TrimPrefix(line, "*"))
		if len(f) < 3 {
			continue
		}
		v, err := strconv.Atoi(f[len(f)-1])
		if err != nil || v != 1 && v != 2 {
			continue
		}
		ds = append(ds, Distro{Name: f[0], State: strings.Join(f[1:len(f)-1], " "), Version: v, Default: def})
	}
	return ds
}

// Choose picks the distribution to run in: the one named, else Ubuntu, else
// the default one when it runs on WSL 2 and is not a container tool's own.
// ok is false when there is none yet, and Distro then names the one to
// install.
func Choose(ds []Distro, named string) (d Distro, ok bool) {
	want := named
	if want == "" {
		want = DefaultDistro
	}
	for _, d := range ds {
		if strings.EqualFold(d.Name, want) {
			return d, true
		}
	}
	if named == "" {
		for _, d := range ds {
			if d.Default && d.Version == 2 && !tools(d.Name) {
				return d, true
			}
		}
	}
	return Distro{Name: want}, false
}

// tools says whether a distribution is one an app keeps for itself, such
// as Docker Desktop's, which no one works in.
func tools(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "docker-desktop") || strings.HasPrefix(n, "rancher-desktop") || strings.HasPrefix(n, "podman")
}

var buildRE = regexp.MustCompile(`\d+\.\d+\.(\d+)`)

// Build reads Windows' build number from `cmd /c ver`, such as "Microsoft
// Windows [Version 10.0.19045.3803]".
func Build(ver string) (int, bool) {
	m := buildRE.FindStringSubmatch(ver)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

var drivePath = regexp.MustCompile(`^(@?)([A-Za-z]):[\\/](.*)$`)

// LinuxPath is where a Windows path is inside WSL, under /mnt/, where WSL
// puts each drive unless wsl.conf moves them; an @path, as a task names a
// folder, stays one. ok is false for anything that is not such a path.
func LinuxPath(p string) (string, bool) {
	m := drivePath.FindStringSubmatch(p)
	if m == nil {
		return p, false
	}
	return m[1] + "/mnt/" + strings.ToLower(m[2]) + "/" + strings.ReplaceAll(m[3], `\`, "/"), true
}

// dataDir is the shell that sets d to where install.sh puts Irie: its
// XDG_DATA_HOME folder when that is a full path, as the XDG spec has it,
// else ~/.local/share.
const dataDir = `d=${XDG_DATA_HOME:-}; case $d in /*) ;; *) d=$HOME/.local/share ;; esac; `

// binary is Irie's own program inside WSL, in the shell's words.
const binary = `"$d/` + brand.Slug + `/bin/` + brand.Slug + `"`

// Command is wsl.exe's arguments to run Irie with args inside distro, from
// the home folder, through a login shell, so that its PATH is the one a
// terminal there has. A Windows path among args becomes its WSL path.
func Command(distro string, args []string) []string {
	out := []string{"-d", distro, "--cd", "~", "-e", "sh", "-lc", dataDir + "exec " + binary + ` "$@"`, brand.Slug}
	for _, a := range args {
		a, _ = LinuxPath(a)
		out = append(out, a)
	}
	return out
}

// Installed is wsl.exe's arguments to ask whether Irie is installed inside
// distro: it exits 0 when it is.
func Installed(distro string) []string {
	return []string{"-d", distro, "-e", "sh", "-c", dataDir + "test -x " + binary}
}

// Install is wsl.exe's arguments to run Irie's installer inside distro,
// which then runs `irie setup` there: tmux, Claude Code and its sign-in,
// each after a yes.
func Install(distro string) []string {
	return []string{"-d", distro, "--cd", "~", "-e", "sh", "-lc", installScript}
}

// InstallWindow is Install for a console window of its own, as the
// Windows app opens one: it waits for Enter before the window closes, so
// its last words can be read, and exits as the installer did.
func InstallWindow(distro string) []string {
	return []string{"-d", distro, "--cd", "~", "-e", "sh", "-lc", "(" + installScript + "); s=$?; echo; printf 'Press Enter to close this window. '; read _; exit $s"}
}

const installScript = "command -v curl >/dev/null 2>&1 || { echo 'curl is missing: run sudo apt install curl, then this again' >&2; exit 1; }; curl -fsSL " + brand.Site + "install.sh | sh"
