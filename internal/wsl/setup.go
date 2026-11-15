package wsl

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/irie-people/irie/internal/brand"
)

// Machine runs Windows' programs for the wrapper; tests stand in for it.
type Machine interface {
	// Output runs a program, waits, and gives what it wrote, decoded, and
	// an error when it could not run or exited with anything but 0.
	Output(name string, args ...string) (string, error)
	// Run runs a program in this console, with the user's input and
	// output, and gives its exit code.
	Run(name string, args ...string) (int, error)
}

// Env is where things are on this Windows computer, from its environment.
type Env struct {
	Exe          string // irie.exe itself
	Profile      string // %USERPROFILE%, where .wslconfig lives
	AppData      string // %APPDATA%, which holds the Start menu
	LocalAppData string // %LOCALAPPDATA%, which holds Windows Terminal's fragments and the wrapper's own folder
	ProgramFiles string // %ProgramW6432%, where the Store's WSL lives
	SystemRoot   string // %SystemRoot%, where Windows' own WSL lives
	Distro       string // the distribution asked for, by IRIE_WSL_DISTRO or setup --distro; "" = Choose's pick
}

// EnvFromOS reads Env from the environment, and the distribution saved by
// setup --distro.
func EnvFromOS() Env {
	exe, _ := os.Executable()
	e := Env{Exe: exe, Profile: os.Getenv("USERPROFILE"), AppData: os.Getenv("APPDATA"), LocalAppData: os.Getenv("LOCALAPPDATA"),
		ProgramFiles: os.Getenv("ProgramW6432"), SystemRoot: os.Getenv("SystemRoot")}
	if e.ProgramFiles == "" {
		e.ProgramFiles = os.Getenv("ProgramFiles")
	}
	e.Distro = strings.TrimSpace(os.Getenv(brand.Env("WSL_DISTRO")))
	if e.Distro == "" {
		if b, err := os.ReadFile(e.distroFile()); err == nil {
			e.Distro = strings.TrimSpace(string(b))
		}
	}
	return e
}

func (e Env) distroFile() string { return filepath.Join(e.LocalAppData, brand.Slug, "distro") }
func (e Env) wslconfig() string  { return filepath.Join(e.Profile, ".wslconfig") }
func (e Env) programs() string {
	return filepath.Join(e.AppData, "Microsoft", "Windows", "Start Menu", "Programs")
}

// HasWSL says whether WSL is installed: the Store's, which `wsl --install`
// puts in, or Windows' own older one. It looks at their files rather than
// asking wsl.exe, which on a Windows without WSL may offer to install it
// and wait a minute for a key.
func (e Env) HasWSL() bool {
	return e.ProgramFiles != "" && exists(filepath.Join(e.ProgramFiles, "WSL", "wsl.exe")) ||
		e.SystemRoot != "" && exists(filepath.Join(e.SystemRoot, "System32", "lxss", "LxssManager.dll"))
}

// Shortcut is the Start menu's shortcut to the wrapper.
func (e Env) Shortcut() string { return filepath.Join(e.programs(), brand.Name+".lnk") }

// Startup is the shortcut that starts Irie as the user signs in.
func (e Env) Startup() string { return filepath.Join(e.programs(), "Startup", brand.Name+".lnk") }

// Fragment is the file that adds Irie to Windows Terminal's profiles.
func (e Env) Fragment() string {
	return filepath.Join(e.LocalAppData, "Microsoft", "Windows Terminal", "Fragments", brand.Name, brand.Slug+".json")
}

// State is how far this computer is set up.
type State struct {
	Build       int      // Windows' build number; 0 = unknown
	WSL         bool     // WSL is installed and answers
	Distros     []Distro // what WSL lists
	Distro      Distro   // the distribution Irie runs in, or the one to install
	HasDistro   bool     // Distro is installed
	Installed   bool     // Irie is installed in Distro
	KeepRunning bool     // .wslconfig keeps a distribution running once its last window closes
	Shortcut    bool     // the Start menu has Irie
	Startup     bool     // Irie starts at sign-in
}

// Look finds how far this computer is set up. It changes nothing.
func Look(m Machine, e Env) State {
	var st State
	if out, err := m.Output("cmd.exe", "/c", "ver"); err == nil {
		st.Build, _ = Build(out)
	}
	// With no distribution, wsl.exe lists none and exits non-zero.
	if st.WSL = e.HasWSL(); st.WSL {
		out, _ := m.Output("wsl.exe", "--list", "--verbose")
		st.Distros = ParseList(out)
	}
	st.Distro, st.HasDistro = Choose(st.Distros, e.Distro)
	if st.HasDistro && st.Distro.Version == 2 {
		_, err := m.Output("wsl.exe", Installed(st.Distro.Name)...)
		st.Installed = err == nil
	}
	if b, err := os.ReadFile(e.wslconfig()); err == nil {
		st.KeepRunning = KeepsRunning(string(b))
	}
	st.Shortcut = exists(e.Shortcut())
	st.Startup = exists(e.Startup())
	return st
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Step is one thing setup offers to do.
type Step struct {
	ID      string // windows, wsl, distro, version, install, keep, shortcut, startup
	Need    string // what is missing, in plain words
	Ask     string // the question asked before the fix runs; "" = none can run
	Blocks  bool   // Irie can't run until it is done
	Restart bool   // Windows must restart once it is done
}

// Plan is what setup offers for st, in the order it must happen.
func Plan(st State) []Step {
	var s []Step
	switch {
	case st.Build > 0 && st.Build < MinBuild:
		return []Step{{ID: "windows", Blocks: true,
			Need: fmt.Sprintf("Windows build %d is too old for WSL 2: %s needs Windows 10 version 2004 (build %d) or later, or Windows 11. Update Windows, then run %s setup again.", st.Build, brand.Name, MinBuild, brand.Slug)}}
	case !st.WSL:
		return []Step{{ID: "wsl", Blocks: true, Restart: true,
			Need: brand.Name + " runs inside WSL, Windows' own Linux, which is not installed.",
			Ask:  "Install WSL? Windows asks for an administrator's OK, and then needs a restart."}}
	case !st.HasDistro:
		s = append(s, Step{ID: "distro", Blocks: true,
			Need: "WSL has no " + st.Distro.Name + " to run " + brand.Name + " in.",
			Ask:  "Install " + st.Distro.Name + "? It asks you to choose a user name and password for Linux; then type exit to come back here."})
	case st.Distro.Version != 2:
		s = append(s, Step{ID: "version", Blocks: true,
			Need: st.Distro.Name + " runs on WSL 1, and " + brand.Name + " needs WSL 2.",
			Ask:  "Move " + st.Distro.Name + " to WSL 2? It keeps its files, and takes a few minutes."})
	}
	if !st.Installed {
		s = append(s, Step{ID: "install", Blocks: true,
			Need: brand.Name + " is not installed in " + st.Distro.Name + ".",
			Ask:  "Install " + brand.Name + " in " + st.Distro.Name + "? Its own setup then offers tmux, Claude Code and the sign-in, each after a yes."})
	}
	if !st.KeepRunning {
		s = append(s, Step{ID: "keep",
			Need: "WSL stops Linux, and every agent in it, seconds after the last window closes.",
			Ask:  "Keep Linux running while Windows is on, so agents keep working with no window open? (.wslconfig: instanceIdleTimeout=-1)"})
	}
	if !st.Shortcut {
		s = append(s, Step{ID: "shortcut",
			Need: "The Start menu has no " + brand.Name + ".",
			Ask:  "Add " + brand.Name + " to the Start menu, and to Windows Terminal's profiles?"})
	}
	if !st.Startup {
		s = append(s, Step{ID: "startup",
			Need: brand.Name + " does not start when you sign in to Windows.",
			Ask:  "Start " + brand.Name + " in the background when you sign in, so it is ready before you open it?"})
	}
	return s
}

// Ready says whether Irie can run: nothing blocks.
func Ready(st State) bool {
	for _, s := range Plan(st) {
		if s.Blocks {
			return false
		}
	}
	return true
}

// Fix runs step's fix.
func Fix(m Machine, e Env, st State, step Step) error {
	switch step.ID {
	case "wsl":
		// Installing WSL needs an administrator; Start-Process asks Windows
		// for one, and waits for the install.
		return powershell(m, "Start-Process -FilePath wsl.exe -ArgumentList '--install','--no-distribution' -Verb RunAs -Wait")
	case "distro":
		return run(m, "wsl.exe", "--install", "-d", st.Distro.Name)
	case "version":
		return run(m, "wsl.exe", "--set-version", st.Distro.Name, "2")
	case "install":
		return run(m, "wsl.exe", Install(st.Distro.Name)...)
	case "keep":
		var old string
		if b, err := os.ReadFile(e.wslconfig()); err == nil {
			old = string(b)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.WriteFile(e.wslconfig(), []byte(KeepRunning(old)), 0o644)
	case "shortcut":
		if err := shortcut(m, e.Shortcut(), e.Exe, "", "Your agents, in "+brand.Name, false); err != nil {
			return err
		}
		// Windows Terminal reads every fragment in its folder as it starts;
		// with no Windows Terminal, the file waits for one.
		f, err := Fragment(e.Exe)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(e.Fragment()), 0o755)
		}
		if err == nil {
			err = os.WriteFile(e.Fragment(), f, 0o644)
		}
		return err
	case "startup":
		return shortcut(m, e.Startup(), e.Exe, "up", "Starts "+brand.Name+" in WSL", true)
	}
	return fmt.Errorf("no fix for %s", step.ID)
}

func run(m Machine, name string, args ...string) error {
	code, err := m.Run(name, args...)
	if err == nil && code != 0 {
		err = fmt.Errorf("%s exited with %d", name, code)
	}
	return err
}

// powershell runs a script in Windows PowerShell, which every Windows has,
// given as -EncodedCommand, so no quote in it is read twice.
func powershell(m Machine, script string) error {
	return run(m, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", Encode(script))
}

// Encode is a script as -EncodedCommand takes it: UTF-16LE in base64.
func Encode(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// quote is s as a PowerShell string that expands nothing.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// ShortcutScript is the PowerShell that makes a .lnk shortcut to target,
// minimized when it only starts something.
func ShortcutScript(lnk, target, args, desc string, minimized bool) string {
	style := 1
	if minimized {
		style = 7
	}
	return fmt.Sprintf("$l = %s; New-Item -ItemType Directory -Force -Path (Split-Path -Parent $l) | Out-Null; $s = (New-Object -ComObject WScript.Shell).CreateShortcut($l); $s.TargetPath = %s; $s.Arguments = %s; $s.WorkingDirectory = $env:USERPROFILE; $s.Description = %s; $s.WindowStyle = %d; $s.Save()",
		quote(lnk), quote(target), quote(args), quote(desc), style)
}

func shortcut(m Machine, lnk, target, args, desc string, minimized bool) error {
	return powershell(m, ShortcutScript(lnk, target, args, desc, minimized))
}

// Fragment is the Windows Terminal fragment that adds a profile for Irie,
// which runs the wrapper.
func Fragment(exe string) ([]byte, error) {
	b, err := json.MarshalIndent(map[string]any{"profiles": []any{map[string]any{
		"name":              brand.Name,
		"commandline":       `"` + exe + `"`,
		"startingDirectory": "%USERPROFILE%",
	}}}, "", "  ")
	return append(b, '\n'), err
}

// KeepsRunning says whether .wslconfig keeps distributions running once
// their last window closes: instanceIdleTimeout=-1 in [general].
func KeepsRunning(conf string) bool {
	sec := ""
	for _, line := range strings.Split(conf, "\n") {
		k, v, isKey, s := iniLine(line)
		switch {
		case s != "":
			sec = s
		case isKey && sec == "general" && strings.EqualFold(k, "instanceIdleTimeout"):
			return v == "-1"
		}
	}
	return false
}

// KeepRunning is .wslconfig with instanceIdleTimeout=-1 in [general], and
// everything else in it as it was, its line endings too.
func KeepRunning(conf string) string {
	cr := ""
	if strings.Contains(conf, "\r\n") {
		cr = "\r"
	}
	key := "instanceIdleTimeout=-1" + cr
	lines := strings.Split(conf, "\n")
	sec, at := "", -1
	for i, line := range lines {
		k, _, isKey, s := iniLine(line)
		switch {
		case s != "":
			sec = s
			if s == "general" && at < 0 {
				at = i
			}
		case isKey && sec == "general" && strings.EqualFold(k, "instanceIdleTimeout"):
			lines[i] = key
			return strings.Join(lines, "\n")
		}
	}
	if at >= 0 {
		lines = append(lines[:at+1], append([]string{key}, lines[at+1:]...)...)
		return strings.Join(lines, "\n")
	}
	if conf != "" && !strings.HasSuffix(conf, "\n") {
		conf += cr + "\n"
	}
	if conf != "" {
		conf += cr + "\n"
	}
	return conf + "[general]" + cr + "\n" + key + "\n"
}

// iniLine reads one line of an INI file: a key and its value, or a
// section's name, in lower case.
func iniLine(line string) (key, value string, isKey bool, section string) {
	t := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	switch {
	case t == "" || t[0] == '#' || t[0] == ';':
		return
	case t[0] == '[' && strings.HasSuffix(t, "]"):
		return "", "", false, strings.ToLower(strings.TrimSpace(t[1 : len(t)-1]))
	}
	k, v, ok := strings.Cut(t, "=")
	if !ok {
		return
	}
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "#;"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return strings.TrimSpace(k), v, true, ""
}

// Setup is `irie setup` on Windows: it looks at how far the computer is
// set up and offers each step, in order, running each fix after a yes and
// looking again after it. With check it changes nothing; with yes it
// takes every offer. It gives the exit code: 1 while something blocks.
func Setup(m Machine, e Env, in io.Reader, out io.Writer, check, yes bool) int {
	r := bufio.NewReader(in)
	asked, fixed := map[string]bool{}, map[string]bool{}
	for {
		st := Look(m, e)
		steps := Plan(st)
		if check {
			report(out, st, steps)
			if Ready(st) {
				return 0
			}
			return 1
		}
		// A fix that ran without error but left its step blocking, as when
		// a new distribution's first start was closed before its user was
		// made, ends here rather than in the next step's failure.
		for _, s := range steps {
			if s.Blocks && fixed[s.ID] {
				fmt.Fprintln(out, "Not done yet: "+s.Need+" Run "+brand.Slug+" setup again to try once more.")
				return 1
			}
		}
		var next *Step
		for i := range steps {
			if !asked[steps[i].ID] {
				next = &steps[i]
				break
			}
		}
		if next == nil {
			if !Ready(st) {
				fmt.Fprintln(out, "Not set up yet: "+firstBlock(steps)+" See "+brand.Repo+"/blob/main/docs/WINDOWS.md")
				return 1
			}
			fmt.Fprintln(out, brand.Name+" is set up in "+st.Distro.Name+". Start it from the Start menu, or type "+brand.Slug+".")
			return 0
		}
		asked[next.ID] = true
		fmt.Fprintln(out, next.Need)
		if next.Ask == "" {
			return 1
		}
		if !yes && !confirm(r, out, next.Ask) {
			if next.Blocks {
				fmt.Fprintln(out, "Left as it is; "+brand.Name+" can't run until it is done.")
				return 1
			}
			continue
		}
		if err := Fix(m, e, st, *next); err != nil {
			fmt.Fprintln(out, "That did not work: "+err.Error())
			if next.Blocks {
				return 1
			}
			continue
		}
		fixed[next.ID] = true
		if next.Restart {
			fmt.Fprintln(out, "Restart Windows, then run "+brand.Slug+" setup again to go on.")
			return 1
		}
	}
}

func firstBlock(steps []Step) string {
	for _, s := range steps {
		if s.Blocks {
			return s.Need
		}
	}
	return ""
}

func report(out io.Writer, st State, steps []Step) {
	build := "unknown"
	if st.Build > 0 {
		build = fmt.Sprint(st.Build)
	}
	fmt.Fprintf(out, "Windows build %s\n", build)
	fmt.Fprintf(out, "WSL            %s\n", yesNo(st.WSL))
	fmt.Fprintf(out, "Distribution   %s\n", distroLine(st))
	fmt.Fprintf(out, "%-14s %s\n", brand.Name, yesNo(st.Installed))
	fmt.Fprintf(out, "Keeps running  %s\n", yesNo(st.KeepRunning))
	fmt.Fprintf(out, "Start menu     %s\n", yesNo(st.Shortcut))
	fmt.Fprintf(out, "At sign-in     %s\n", yesNo(st.Startup))
	for _, s := range steps {
		fmt.Fprintln(out, "- "+s.Need)
	}
}

func distroLine(st State) string {
	if !st.HasDistro {
		return "none (" + st.Distro.Name + " to install)"
	}
	return fmt.Sprintf("%s, WSL %d", st.Distro.Name, st.Distro.Version)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// confirm asks a yes-or-no question; Enter alone is yes.
func confirm(r *bufio.Reader, out io.Writer, q string) bool {
	fmt.Fprint(out, q+" [Y/n] ")
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(out)
		return false
	}
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "" || a == "y" || a == "yes"
}

// SaveDistro keeps the distribution setup --distro chose, for every later
// run.
func SaveDistro(e Env, name string) error {
	if err := os.MkdirAll(filepath.Dir(e.distroFile()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(e.distroFile(), []byte(name+"\n"), 0o644)
}
