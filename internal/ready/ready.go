// Package ready checks that a computer has what Irie needs, and says in
// plain words how to get each part it lacks (DESIGN §2.7). `irie setup` runs
// it and offers each fix; the first `irie` runs it when something is
// missing; the core answers the ready op with it, for the Mac app's welcome.
package ready

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/tmux"
)

// MinTmux is the first tmux with -N, which Irie gives every command that
// must not start a server (tmux's CHANGES, 3.2).
const MinTmux = "3.2"

// Status says how one need stands.
type Status string

const (
	OK      Status = "ok"      // there, and new enough
	Missing Status = "missing" // not there, or not usable
	Warn    Status = "warn"    // there, with a problem worth fixing
	Unknown Status = "unknown" // Irie could not tell
)

// The needs, by ID.
const (
	TmuxID   = "tmux"
	ClaudeID = "claude"
	SignInID = "signin"
	GitID    = "git"
	PathID   = "path"
)

// Item is one need and how it stands.
type Item struct {
	ID      string `json:"id"`
	Name    string `json:"name"` // what a person calls it
	Status  Status `json:"status"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	For     string `json:"for"`               // what it is for, in plain words
	Problem string `json:"problem,omitempty"` // what is wrong, in plain words
	// Blocks is set when agents cannot start until the item is fixed.
	Blocks bool `json:"blocks,omitempty"`
	Fix    *Fix `json:"fix,omitempty"`
}

// Fix is how to meet a need.
type Fix struct {
	Say string `json:"say"` // what the fix does, as a question's subject: "Install tmux with Homebrew"
	// Argv is what `irie setup` runs after a yes; nil when a person must
	// act, or when the fix is a line for a shell's startup file.
	Argv []string `json:"argv,omitempty"`
	Show string   `json:"show,omitempty"` // the command as a person types it
	Link string   `json:"link,omitempty"` // a page that explains
	// File and Line: the shell startup file to add Line to, for PATH.
	File string `json:"file,omitempty"`
	Line string `json:"line,omitempty"`
}

// Report is every need, in the order `irie setup` shows them.
type Report struct {
	Items []Item `json:"items"`
}

// Ready reports whether nothing blocks agents from starting.
func (r Report) Ready() bool { return len(r.Blocking()) == 0 }

// Blocking returns the items that keep agents from starting.
func (r Report) Blocking() []Item {
	var out []Item
	for _, it := range r.Items {
		if it.Blocks && it.Status == Missing {
			out = append(out, it)
		}
	}
	return out
}

// Item returns the item with the ID, and whether there is one.
func (r Report) Item(id string) (Item, bool) {
	for _, it := range r.Items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// Options are what a check looks at.
type Options struct {
	GOOS string // "" is runtime.GOOS
	// Env is the environment the agents run in: its PATH is searched.
	Env  []string
	Home string // "" is $HOME from Env
	// Tmux is tmux.path from the config; "" searches.
	Tmux string
	// Claude is the command of the claude profile; "" is claude.
	Claude string
	// Self is this irie, to check that a terminal finds it; "" skips that.
	Self string
	// SignIn also asks Claude Code whether it is signed in.
	SignIn bool
	// Dirs are searched after PATH for tmux, git and a package manager;
	// nil is the usual install folders. Tests pass an empty slice.
	Dirs []string
	// Timeout bounds each command a check runs; 0 is 10 s.
	Timeout time.Duration

	shellEnv []string // Env as given, before PathEnv
}

func (o *Options) fill() {
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.Home == "" {
		o.Home = envGet(o.Env, "HOME")
	}
	if o.Claude == "" {
		o.Claude = "claude"
	}
	if o.Dirs == nil {
		o.Dirs = tmux.Dirs
	}
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}
	o.shellEnv = o.Env
	o.Env = PathEnv(o.Env, o.Home)
}

// Check looks at each need, in the order a newcomer meets them.
func Check(o Options) Report {
	o.fill()
	var r Report
	r.Items = append(r.Items, checkTmux(o))
	c := checkClaude(o)
	r.Items = append(r.Items, c)
	if o.SignIn && c.Status != Missing {
		r.Items = append(r.Items, checkSignIn(o, c.Path))
	}
	r.Items = append(r.Items, checkGit(o))
	if o.Self != "" {
		r.Items = append(r.Items, checkPath(o))
	}
	return r
}

// LocalBin is where the installers of Irie and of Claude Code put the
// programs a person runs, under the home folder.
const LocalBin = ".local/bin"

// PathEnv returns env with ~/.local/bin at the end of its PATH when that
// folder exists and the PATH lacks it. Irie's installer and Claude Code's
// put their programs there, and a terminal finds them only once the
// shell's startup file says so: this way an agent finds Claude Code at
// once. The core's own server starts with it.
func PathEnv(env []string, home string) []string {
	if home == "" {
		return env
	}
	dir := filepath.Join(home, LocalBin)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return env
	}
	path, i := "", -1
	for j, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path, i = v, j
		}
	}
	if slices.Contains(filepath.SplitList(path), dir) {
		return env
	}
	out := slices.Clone(env)
	switch {
	case i < 0:
		out = append(out, "PATH=/usr/bin:/bin:/usr/sbin:/sbin:"+dir)
	case path == "":
		out[i] = "PATH=" + dir
	default:
		out[i] = "PATH=" + path + string(filepath.ListSeparator) + dir
	}
	return out
}

func checkTmux(o Options) Item {
	it := Item{ID: TmuxID, Name: "tmux", For: "keeps your agents running when no window is open", Blocks: true}
	path := ""
	if o.Tmux != "" {
		if isExec(o.Tmux) {
			path = o.Tmux
		} else {
			it.Status, it.Problem = Missing, "tmux.path in Irie's config, "+o.Tmux+", is not a program"
			it.Fix = &Fix{Say: "Fix tmux.path in Irie's config, or remove it", Show: "irie config path"}
			return it
		}
	} else {
		path = look("tmux", o.Env, o.Dirs)
	}
	if path == "" {
		it.Status, it.Problem = Missing, "not installed"
		it.Fix = installFix(o, "tmux", "https://github.com/tmux/tmux/wiki/Installing")
		return it
	}
	it.Path = path
	out, _ := run(o, path, "-V")
	it.Version = tmuxVersion(out)
	switch {
	case it.Version == "":
		it.Status = OK // "tmux master", a build from source: new enough
	case !versionAtLeast(it.Version, MinTmux):
		it.Status = Missing
		it.Problem = "tmux " + it.Version + " is too old: Irie needs " + MinTmux + " or later"
		it.Fix = upgradeFix(o, "tmux", "https://github.com/tmux/tmux/wiki/Installing")
	default:
		it.Status = OK
	}
	return it
}

// claudeDirs are where Claude Code's installers put claude, beyond PATH:
// the native installer's launcher, an older local install, and Homebrew's.
func claudeDirs(home string) []string {
	return []string{filepath.Join(home, LocalBin), filepath.Join(home, ".claude", "local"), "/opt/homebrew/bin", "/usr/local/bin"}
}

func checkClaude(o Options) Item {
	it := Item{ID: ClaudeID, Name: "Claude Code", For: "the coding agent Irie starts for you", Blocks: true}
	path, err := host.LookPath(o.Claude, o.Env)
	if err != nil {
		bare := !strings.Contains(o.Claude, "/")
		if p := look(o.Claude, nil, claudeDirs(o.Home)); bare && p != "" {
			it.Status, it.Path = Missing, p
			it.Problem = "it is in " + tilde(filepath.Dir(p), o.Home) + ", where your agents don't look"
			it.Fix = pathFix(o, filepath.Dir(p))
			return it
		}
		it.Status, it.Problem = Missing, "not installed"
		if !bare {
			it.Problem = o.Claude + " is not a program"
		}
		it.Fix = &Fix{
			Say:  "Install Claude Code with its official installer",
			Argv: []string{"/bin/sh", "-c", ClaudeInstaller},
			Show: ClaudeInstaller,
			Link: "https://code.claude.com/docs/en/setup",
		}
		return it
	}
	it.Path = path
	out, _ := run(o, path, "--version")
	it.Version = claude.ParseVersion(out)
	it.Status = OK
	if it.Version != "" && !claude.AtLeast(it.Version, claude.MinVersion) {
		it.Status = Warn
		it.Problem = "Claude Code " + it.Version + " is older than " + claude.MinVersion + ", so Irie can't pause it"
		it.Fix = &Fix{Say: "Update Claude Code", Argv: []string{path, "update"}, Show: "claude update"}
	}
	return it
}

// ClaudeInstaller is Claude Code's official install command for macOS and
// Linux (code.claude.com/docs/en/setup).
const ClaudeInstaller = "curl -fsSL https://claude.ai/install.sh | bash"

func checkSignIn(o Options, path string) Item {
	it := Item{ID: SignInID, Name: "Signed in", For: "lets Claude Code work on your account", Blocks: true}
	login := &Fix{Say: "Sign in to Claude Code in your browser", Argv: []string{path, "auth", "login"}, Show: "claude auth login"}
	out, _ := run(o, path, "auth", "status", "--json")
	// Only these fields are read: the rest names the person.
	var st struct {
		LoggedIn *bool  `json:"loggedIn"`
		Method   string `json:"authMethod"`
	}
	if i := strings.IndexByte(out, '{'); i < 0 || json.Unmarshal([]byte(out[i:]), &st) != nil || st.LoggedIn == nil {
		it.Status, it.Problem, it.Fix = Unknown, "Claude Code didn't say; if a first task waits at a sign-in, sign in there", login
		return it
	}
	if !*st.LoggedIn {
		it.Status, it.Problem, it.Fix = Missing, "Claude Code isn't signed in", login
		return it
	}
	it.Status = OK
	switch st.Method {
	case "api_key":
		it.For = "Claude Code works with an API key"
	case "claude.ai":
		it.For = "Claude Code works on your Claude account"
	}
	return it
}

func checkGit(o Options) Item {
	it := Item{ID: GitID, Name: "git", For: "gives each task on a project its own worktree"}
	path := look("git", o.Env, o.Dirs)
	if path == "" {
		it.Status, it.Problem = Missing, "not installed"
		it.Fix = installFix(o, "git", "https://git-scm.com/install")
		return it
	}
	it.Path = path
	// macOS's own /usr/bin/git only asks to install Apple's Command Line
	// Tools until they are there, so they are checked first.
	if o.GOOS == "darwin" && path == "/usr/bin/git" {
		if _, err := run(o, "/usr/bin/xcode-select", "-p"); err != nil {
			it.Status, it.Problem = Missing, "it comes with Apple's Command Line Tools, which aren't installed"
			it.Fix = &Fix{Say: "Install Apple's Command Line Tools", Argv: []string{"/usr/bin/xcode-select", "--install"}, Show: "xcode-select --install"}
			return it
		}
	}
	out, err := run(o, path, "--version")
	if err != nil {
		it.Status, it.Problem = Missing, "`git --version` failed"
		return it
	}
	it.Version = gitVersion(out)
	it.Status = OK
	return it
}

func checkPath(o Options) Item {
	it := Item{ID: PathID, Name: "irie on your PATH", For: "lets you type irie in any terminal"}
	self, err := os.Stat(o.Self)
	if err != nil {
		it.Status = Unknown
		return it
	}
	found, err := host.LookPath("irie", o.shellEnv)
	if err == nil {
		if fi, err := os.Stat(found); err == nil && os.SameFile(fi, self) {
			it.Status, it.Path = OK, found
			return it
		}
		it.Status, it.Path = Warn, found
		it.Problem = "`irie` in a terminal runs " + tilde(found, o.Home) + ", another build"
		return it
	}
	// The installer's link in ~/.local/bin is the folder to add, when it
	// is there.
	dir := filepath.Dir(o.Self)
	if fi, err := os.Stat(filepath.Join(o.Home, LocalBin, "irie")); err == nil && os.SameFile(fi, self) {
		dir = filepath.Join(o.Home, LocalBin)
	}
	it.Status = Warn
	it.Problem = "a terminal doesn't find irie in " + tilde(dir, o.Home)
	it.Fix = pathFix(o, dir)
	return it
}

// pathFix adds dir to PATH in the person's shell startup file.
func pathFix(o Options, dir string) *Fix {
	file, line := StartupLine(envGet(o.Env, "SHELL"), o.Home, envGet(o.Env, "ZDOTDIR"), o.GOOS, dir)
	return &Fix{
		Say:  "Add " + tilde(dir, o.Home) + " to your PATH in " + tilde(file, o.Home),
		File: file,
		Line: line,
		Show: "echo '" + line + "' >> " + tilde(file, o.Home),
	}
}

// StartupLine returns the startup file of the shell and the line that puts
// dir on its PATH. Dirs under home are written with $HOME.
func StartupLine(shell, home, zdotdir, goos, dir string) (file, line string) {
	d := dir
	if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
		d = "$HOME/" + rel
	}
	switch filepath.Base(shell) {
	case "zsh":
		base := home
		if filepath.IsAbs(zdotdir) {
			base = zdotdir
		}
		return filepath.Join(base, ".zshrc"), `export PATH="` + d + `:$PATH"`
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish"), "fish_add_path " + d
	case "bash":
		// Terminal on macOS opens login shells, which read .bash_profile.
		if goos == "darwin" {
			return filepath.Join(home, ".bash_profile"), `export PATH="` + d + `:$PATH"`
		}
		return filepath.Join(home, ".bashrc"), `export PATH="` + d + `:$PATH"`
	}
	return filepath.Join(home, ".profile"), `export PATH="` + d + `:$PATH"`
}

// AddLine appends line to file, unless the file has it already, making the
// file and its folder when they are missing.
func AddLine(file, line string) error {
	old, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, l := range strings.Split(string(old), "\n") {
		if strings.TrimSpace(l) == line {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	text := "\n# Added by irie setup: programs installed for you\n" + line + "\n"
	if len(old) > 0 && !strings.HasSuffix(string(old), "\n") {
		text = "\n" + text
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// installFix installs pkg with this computer's package manager, or says
// where to read how.
func installFix(o Options, pkg, link string) *Fix {
	pm := findPackageManager(o)
	if pm == nil {
		if o.GOOS == "darwin" {
			return homebrewFix(pkg)
		}
		return &Fix{Say: "Install " + pkg, Link: link}
	}
	return pm.fix("Install", pkg, link)
}

func upgradeFix(o Options, pkg, link string) *Fix {
	pm := findPackageManager(o)
	if pm == nil {
		return &Fix{Say: "Install a newer " + pkg, Link: link}
	}
	if pm.name == "brew" {
		return &Fix{Say: "Upgrade " + pkg + " with Homebrew", Argv: []string{pm.path, "upgrade", pkg}, Show: "brew upgrade " + pkg, Link: link}
	}
	return pm.fix("Install a newer", pkg, link)
}

// HomebrewInstaller is Homebrew's official install command (brew.sh).
const HomebrewInstaller = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

// homebrewFix installs Homebrew, the Mac's usual package manager, then pkg
// with it: Homebrew's installer asks for the person's password itself.
func homebrewFix(pkg string) *Fix {
	script := HomebrewInstaller + ` && b=/opt/homebrew/bin/brew && { [ -x "$b" ] || b=/usr/local/bin/brew; } && "$b" install ` + pkg
	return &Fix{
		Say:  "Install Homebrew, the Mac's package manager, then " + pkg + " with it",
		Argv: []string{"/bin/bash", "-c", script},
		Show: HomebrewInstaller + " && brew install " + pkg,
		Link: "https://brew.sh",
	}
}

type packageManager struct {
	name string // brew, port, apt-get, dnf, yum, pacman, zypper, apk
	path string
	sudo bool
}

func (pm packageManager) fix(verb, pkg, link string) *Fix {
	var cmd []string
	switch pm.name {
	case "brew":
		cmd = []string{"brew", "install", pkg}
	case "port":
		cmd = []string{"port", "install", pkg}
	case "apt-get":
		// A fresh system has no package lists yet.
		show := "apt-get update && apt-get install -y " + pkg
		if pm.sudo {
			show = "sudo apt-get update && sudo apt-get install -y " + pkg
		}
		return &Fix{Say: verb + " " + pkg + " with apt", Argv: []string{"/bin/sh", "-c", show}, Show: show, Link: link}
	case "dnf", "yum", "zypper":
		cmd = []string{pm.name, "install", "-y", pkg}
	case "pacman":
		cmd = []string{"pacman", "-S", "--noconfirm", pkg}
	case "apk":
		cmd = []string{"apk", "add", pkg}
	}
	argv := append([]string{pm.path}, cmd[1:]...)
	if pm.sudo {
		argv = append([]string{"sudo"}, argv...)
		cmd = append([]string{"sudo"}, cmd...)
	}
	say := map[string]string{"brew": "Homebrew", "port": "MacPorts"}[pm.name]
	if say == "" {
		say = pm.name
	}
	return &Fix{Say: verb + " " + pkg + " with " + say, Argv: argv, Show: strings.Join(cmd, " "), Link: link}
}

func findPackageManager(o Options) *packageManager {
	names := []string{"apt-get", "dnf", "yum", "pacman", "zypper", "apk"}
	dirs := o.Dirs
	if o.GOOS == "darwin" {
		names = []string{"brew", "port"}
		dirs = append(slices.Clone(dirs), "/opt/local/bin")
	}
	for _, n := range names {
		if p := look(n, o.Env, dirs); p != "" {
			// Homebrew refuses root; the others need it.
			return &packageManager{name: n, path: p, sudo: n != "brew" && os.Geteuid() != 0}
		}
	}
	return nil
}

// look finds name on env's PATH, else in dirs.
func look(name string, env []string, dirs []string) string {
	if env != nil {
		if p, err := host.LookPath(name, env); err == nil {
			return p
		}
	}
	for _, d := range dirs {
		if p := filepath.Join(d, name); isExec(p) {
			return p
		}
	}
	return ""
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// run runs a check's command in its own process group, killed after the
// timeout, and returns what it printed to stdout.
func run(o Options, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = o.Env
	cmd.Dir = o.Home
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	return string(out), err
}

var (
	tmuxRE = regexp.MustCompile(`(\d+)\.(\d+)([a-z]?)`)
	gitRE  = regexp.MustCompile(`(\d+\.\d+(\.\d+)?)`)
)

// tmuxVersion reads `tmux -V`: "tmux 3.7b", "tmux next-3.8", "tmux 3.2a".
// A build from source says "tmux master", which gives "".
func tmuxVersion(out string) string { return tmuxRE.FindString(out) }

// gitVersion reads `git --version`: "git version 2.39.5 (Apple Git-154)".
func gitVersion(out string) string { return gitRE.FindString(out) }

// versionAtLeast compares major.minor, and the letter of a tmux release.
func versionAtLeast(v, min string) bool {
	a, b := tmuxRE.FindStringSubmatch(v), tmuxRE.FindStringSubmatch(min)
	if a == nil || b == nil {
		return false
	}
	for i := 1; i <= 2; i++ {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x > y
		}
	}
	return a[3] >= b[3]
}

func envGet(env []string, key string) string { return lastValue(env, key) }

func lastValue(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if s, ok := strings.CutPrefix(kv, key+"="); ok {
			v = s
		}
	}
	return v
}

// tilde writes a path under home with ~.
func tilde(p, home string) string {
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + p[len(home):]
	}
	return p
}
