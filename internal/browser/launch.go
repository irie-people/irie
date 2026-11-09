package browser

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrNotFound means no browser was found to drive.
var ErrNotFound = errors.New("Mullvad Browser is not installed")

// appPaths are where Mullvad Browser's installers put its executable: the
// app from its disk image or Homebrew's cask on macOS.
var appPaths = map[string][]string{
	"darwin": {
		"/Applications/Mullvad Browser.app/Contents/MacOS/mullvadbrowser",
		"~/Applications/Mullvad Browser.app/Contents/MacOS/mullvadbrowser",
	},
}

// pathNames are the names it goes by on PATH, as on Linux.
var pathNames = []string{"mullvad-browser", "mullvadbrowser"}

// lookPath and homeDir are swapped in tests.
var (
	lookPath = exec.LookPath
	homeDir  = os.UserHomeDir
)

// Find returns the browser to drive: path when it is set, else Mullvad
// Browser where its installers put it, then on PATH. Any browser that
// speaks WebDriver BiDi through Firefox's Remote Agent will do for path:
// Firefox, or another of its forks.
func Find(path string) (string, error) {
	if path != "" {
		if !executable(path) {
			return "", fmt.Errorf("%s is not an executable file", path)
		}
		return path, nil
	}
	home, _ := homeDir()
	for _, p := range appPaths[runtime.GOOS] {
		if rest, ok := strings.CutPrefix(p, "~/"); ok {
			if home == "" {
				continue
			}
			p = filepath.Join(home, rest)
		}
		if executable(p) {
			return p, nil
		}
	}
	for _, name := range pathNames {
		if p, err := lookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrNotFound
}

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// How long a browser may take to start. Mullvad Browser 16.0 took 6 s from
// a template and 11 s on an empty profile; macOS checks a newly installed
// app at its first launch, which took 29 s; and on a machine at a load of
// 25 with its memory under pressure, a first start took minutes.
const startTimeout = 5 * time.Minute

// headlessScreen is the screen a headless browser pretends to have. With
// resistFingerprinting on, a new window takes the largest of its standard
// sizes that fits, so this gives pages 1400×800 rather than the 1200×500 of
// Firefox's own 1366×768.
var headlessScreen = []string{"MOZ_HEADLESS_WIDTH=1920", "MOZ_HEADLESS_HEIGHT=1200"}

// Files in a profile that belong to one running browser, never copied from
// the template: the Remote Agent's address, the profile locks, and the
// marks Irie leaves.
var runFiles = map[string]bool{
	"WebDriverBiDiServer.json": true,
	".parentlock":              true,
	"parent.lock":              true,
	"lock":                     true,
	pidFile:                    true,
	templateMark:               true,
}

const (
	pidFile      = "irie-browser.pid"
	templateMark = "irie-template.json"
)

// running is one browser and its BiDi session.
type running struct {
	cmd     *exec.Cmd
	profile string
	exited  chan struct{}
	conn    *bidi
	name    string // the browser's name and version, from session.new
	log     *tail
}

// listening is the line Firefox's Remote Agent prints once it listens.
var listening = regexp.MustCompile(`WebDriver BiDi listening on ws://([^\s/]+)`)

// start runs the browser at exe on the profile folder dir, waits for its
// Remote Agent and opens a BiDi session with it.
func start(ctx context.Context, o Options, exe, dir string) (*running, error) {
	args := []string{"--no-remote", "--profile", dir, "--remote-debugging-port", "0"}
	if o.Headless {
		args = append(args, "--headless")
	}
	args = append(args, "about:blank")
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	if o.Headless {
		cmd.Env = append(cmd.Env, headlessScreen...)
	}
	r := &running{cmd: cmd, profile: dir, exited: make(chan struct{}), log: &tail{}}
	addr := make(chan string, 1)
	cmd.Stderr = &lineWriter{line: func(line string) {
		r.log.add(line)
		if m := listening.FindStringSubmatch(line); m != nil {
			select {
			case addr <- m[1]:
			default:
			}
		}
	}}
	// The browser's own children may hold stderr open after it exits.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	os.WriteFile(filepath.Join(dir, pidFile), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600)
	go func() {
		cmd.Wait()
		close(r.exited)
	}()

	fail := func(err error) (*running, error) {
		r.kill()
		return nil, err
	}
	// One deadline for the whole start: on a first run Firefox answers
	// session.new only once it has set the profile up, its bundled add-ons
	// too, which on a loaded machine takes longer than the agent's start.
	sctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	var where string
	for where == "" {
		select {
		case where = <-addr:
		case <-poll.C:
			// Firefox also writes the address into the profile.
			where = serverFile(dir)
		case <-r.exited:
			return nil, fmt.Errorf("the browser exited as it started: %s", r.log.last(5))
		case <-sctx.Done():
			if ctx.Err() != nil {
				return fail(ctx.Err())
			}
			return fail(fmt.Errorf("the browser did not open its Remote Agent within %s: %s", startTimeout, r.log.last(5)))
		}
	}
	// The handshake too waits on a browser still setting itself up.
	left := 10 * time.Second
	if d, ok := sctx.Deadline(); ok {
		left = max(time.Until(d), time.Second)
	}
	conn, err := connect(where, left)
	if err != nil {
		return fail(fmt.Errorf("connecting to the browser at %s: %w", where, err))
	}
	r.conn = conn
	var s struct {
		Capabilities struct {
			BrowserName    string `json:"browserName"`
			BrowserVersion string `json:"browserVersion"`
		} `json:"capabilities"`
	}
	if err := conn.call(sctx, "session.new", map[string]any{"capabilities": map[string]any{}}, &s); err != nil {
		if ctx.Err() == nil && sctx.Err() != nil {
			err = fmt.Errorf("the browser did not start a session within %s. On a busy machine a session that Irie holds back gets little of it, and a browser's first start needs a lot: try again later, or use WebFetch meanwhile", startTimeout)
		}
		return fail(err)
	}
	r.name = strings.TrimSpace(displayName(s.Capabilities.BrowserName) + " " + s.Capabilities.BrowserVersion)
	return r, nil
}

// displayName is how a browser calls itself in session.new, as people know
// it.
func displayName(name string) string {
	switch name {
	case "mullvadbrowser":
		return "Mullvad Browser"
	case "firefox":
		return "Firefox"
	}
	return name
}

// serverFile reads the Remote Agent's address from the profile, where
// Firefox writes it as {"ws_host": …, "ws_port": …}.
func serverFile(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "WebDriverBiDiServer.json"))
	if err != nil {
		return ""
	}
	var f struct {
		Host string `json:"ws_host"`
		Port int    `json:"ws_port"`
	}
	if json.Unmarshal(b, &f) != nil || f.Host == "" || f.Port == 0 {
		return ""
	}
	return net.JoinHostPort(f.Host, strconv.Itoa(f.Port))
}

// quit closes the browser and waits for it to exit: browser.close first,
// then SIGTERM, then SIGKILL.
func (r *running) quit() {
	if r.conn != nil && r.conn.alive() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		r.conn.call(ctx, "browser.close", nil, nil)
		cancel()
		r.conn.close()
	}
	select {
	case <-r.exited:
	case <-time.After(10 * time.Second):
		r.kill()
	}
}

// kill stops the browser now, if it still runs.
func (r *running) kill() {
	if r.conn != nil {
		r.conn.close()
	}
	select {
	case <-r.exited:
		return
	default:
	}
	r.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-r.exited:
	case <-time.After(5 * time.Second):
		r.cmd.Process.Kill()
		<-r.exited
	}
}

// alive reports whether the browser and its connection are still up.
func (r *running) alive() bool {
	select {
	case <-r.exited:
		return false
	default:
	}
	return r.conn != nil && r.conn.alive()
}

// launch makes a fresh profile at dir, from the template when there is one,
// and starts the browser on it.
func launch(ctx context.Context, o Options, exe, dir string) (*running, error) {
	killStale(dir)
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// The template only when it is ready: an agent never waits for one to
	// be made. It is made by `irie setup browser`, and again when the
	// browser closes after a while without calls (Browser.closeWhenIdle).
	if o.Template != "" && TemplateReady(o.Template, exe) {
		if err := copyProfile(o.Template, dir); err != nil {
			logf(o, "copying the template profile: %v", err)
			os.RemoveAll(dir)
			os.MkdirAll(dir, 0o700)
		}
	}
	// A proxy named by its host name is looked up here, at each start, so
	// that the browser can do without DNS of its own: Mullvad's relays'
	// proxies have names that resolve only inside its tunnel.
	if o.Proxy != "" {
		p, err := resolveProxy(ctx, o.Proxy)
		if err != nil {
			return nil, err
		}
		o.Proxy = p
	}
	if prefs := userPrefs(o); prefs != "" {
		if err := os.WriteFile(filepath.Join(dir, "user.js"), []byte(prefs), 0o600); err != nil {
			return nil, err
		}
	}
	return start(ctx, o, exe, dir)
}

// lookupHost finds a host's addresses; tests replace it.
var lookupHost = net.DefaultResolver.LookupHost

// resolveProxy is the SOCKS5 proxy address proxy with its host looked up,
// socks5://ip:port, its IPv4 address first.
func resolveProxy(ctx context.Context, proxy string) (string, error) {
	host, port, err := ParseProxy(proxy)
	if err != nil {
		return "", err
	}
	if net.ParseIP(host) != nil {
		return proxy, nil
	}
	lctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	addrs, err := lookupHost(lctx, host)
	if err != nil || len(addrs) == 0 {
		return "", fmt.Errorf("the proxy %s doesn't resolve (is Mullvad VPN connected?): %v", host, err)
	}
	slices.SortStableFunc(addrs, func(a, b string) int {
		return boolInt(strings.Contains(a, ":")) - boolInt(strings.Contains(b, ":"))
	})
	return "socks5://" + net.JoinHostPort(addrs[0], strconv.Itoa(port)), nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func logf(o Options, format string, args ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, "irie browser: "+format+"\n", args...)
	}
}

// killStale stops a browser that an earlier server left running on dir,
// such as one whose server was killed. The pid file names it, and only a
// process whose command line names dir is stopped, since a pid is reused.
func killStale(dir string) {
	b, err := os.ReadFile(filepath.Join(dir, pidFile))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !strings.Contains(string(out), dir) {
		return
	}
	syscall.Kill(pid, syscall.SIGTERM)
	for range 50 {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
}

// templateInfo says which browser made a template: a new build gets a new
// template.
type templateInfo struct {
	Exe     string `json:"exe"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
}

func infoOf(exe string) (templateInfo, error) {
	fi, err := os.Stat(exe)
	if err != nil {
		return templateInfo{}, err
	}
	return templateInfo{Exe: exe, Size: fi.Size(), ModTime: fi.ModTime().UnixNano()}, nil
}

var templateMu sync.Mutex

// TemplateReady reports whether the template profile at dir was made by
// the browser at exe as it is now.
func TemplateReady(dir, exe string) bool {
	want, err := infoOf(exe)
	return err == nil && templateOK(dir, want)
}

// MakeTemplate makes the template profile at o.Template for the browser at
// exe, unless it is ready already.
func MakeTemplate(ctx context.Context, o Options, exe string) error {
	if o.Template == "" {
		return errors.New("no template folder")
	}
	return ensureTemplate(ctx, o, exe)
}

// ensureTemplate makes the template profile when it is missing or was made
// by another build: the browser starts once on an empty profile, which it
// fills in, and closes again. A fresh profile copied from it starts in
// about 6 s where an empty one takes 11 (Mullvad Browser 16.0 on an M3).
// Two servers may race to make it; each makes its own and renames it into
// place, so the template is always whole.
func ensureTemplate(ctx context.Context, o Options, exe string) error {
	templateMu.Lock()
	defer templateMu.Unlock()
	want, err := infoOf(exe)
	if err != nil {
		return err
	}
	if templateOK(o.Template, want) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(o.Template), 0o700); err != nil {
		return err
	}
	tmp := o.Template + ".new-" + randHex()
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	plain := o
	plain.Proxy = "" // the template never goes on the network
	if err := os.WriteFile(filepath.Join(tmp, "user.js"), []byte(userPrefs(plain)), 0o600); err != nil {
		return err
	}
	r, err := start(ctx, plain, exe, tmp)
	if err != nil {
		return err
	}
	r.quit()
	for name := range runFiles {
		os.Remove(filepath.Join(tmp, name))
	}
	mark, _ := json.Marshal(want)
	if err := os.WriteFile(filepath.Join(tmp, templateMark), mark, 0o600); err != nil {
		return err
	}
	if templateOK(o.Template, want) {
		return nil // another server made it meanwhile
	}
	old := o.Template + ".old-" + randHex()
	os.Rename(o.Template, old)
	defer os.RemoveAll(old)
	if err := os.Rename(tmp, o.Template); err != nil {
		if templateOK(o.Template, want) {
			return nil
		}
		return err
	}
	return nil
}

func templateOK(dir string, want templateInfo) bool {
	b, err := os.ReadFile(filepath.Join(dir, templateMark))
	if err != nil {
		return false
	}
	var have templateInfo
	return json.Unmarshal(b, &have) == nil && have == want
}

func randHex() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// copyProfile copies the template's files into dst, keeping their times:
// Firefox checks its extensions again, which is slow, when their files'
// times change. Links and the files of a running browser stay behind.
func copyProfile(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return err
		}
		if runFiles[rel] {
			return nil
		}
		to := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if err := os.MkdirAll(to, info.Mode().Perm()|0o700); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(p, to, info.Mode().Perm()); err != nil {
				return err
			}
			if err := os.Chtimes(to, info.ModTime(), info.ModTime()); err != nil {
				return err
			}
		}
		return nil
	})
}

func copyFile(from, to string, perm fs.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ParseProxy checks a SOCKS5 proxy URL, socks5://host:port (socks5h://
// too), and returns its host and port.
func ParseProxy(s string) (string, int, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", 0, err
	}
	if u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return "", 0, errors.New("must be socks5://host:port")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return "", 0, errors.New("must be socks5://host:port, with nothing more")
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("must be socks5://host:port")
	}
	if !hostRE.MatchString(host) {
		return "", 0, fmt.Errorf("%q is not a host name or address", host)
	}
	return host, port, nil
}

var hostRE = regexp.MustCompile(`^[A-Za-z0-9.:-]+$`)

// userPrefs is the profile's user.js. It turns off the testing prefs that
// Firefox's Remote Agent would otherwise apply at start (about 120 of them,
// tracking protection and the popup blocker among them), so the browser
// keeps its own settings, and adds the proxy when there is one, with DNS
// lookups made at the proxy too. Behind a proxy it also shuts the ways
// round it (DESIGN §4.16): WebRTC, whose connections don't take the proxy
// and would tell a page the Mac's own exit; a direct connection when the
// proxy fails; DNS lookups on the Mac; and pages reaching the Mac's own
// services, as localhost goes to the proxy too. Nothing else changes.
func userPrefs(o Options) string {
	prefs := "user_pref(\"remote.prefs.recommended\", false);\n"
	if o.Proxy == "" {
		return prefs
	}
	host, port, err := ParseProxy(o.Proxy)
	if err != nil {
		return prefs
	}
	// Firefox takes an IPv6 proxy's address without its brackets.
	host = strings.Trim(host, "[]")
	return prefs + fmt.Sprintf(`user_pref("network.proxy.type", 1);
user_pref("network.proxy.socks", %q);
user_pref("network.proxy.socks_port", %d);
user_pref("network.proxy.socks_version", 5);
user_pref("network.proxy.socks_remote_dns", true);
user_pref("network.proxy.failover_direct", false);
user_pref("network.proxy.allow_hijacking_localhost", true);
user_pref("network.dns.disabled", true);
user_pref("media.peerconnection.enabled", false);
`, host, port)
}

// lineWriter hands each whole line written to it to line.
type lineWriter struct {
	line func(string)
	buf  []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.line(strings.TrimRight(string(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 64<<10 { // a line that never ends
		w.line(string(w.buf))
		w.buf = nil
	}
	return len(p), nil
}

// tail keeps the browser's last lines of stderr, for the reason a start
// failed.
type tail struct {
	mu    sync.Mutex
	lines []string
}

func (t *tail) add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, line)
	if len(t.lines) > 50 {
		t.lines = t.lines[len(t.lines)-50:]
	}
}

func (t *tail) last(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	l := t.lines[max(0, len(t.lines)-n):]
	if len(l) == 0 {
		return "it printed nothing"
	}
	return strings.Join(l, " / ")
}
