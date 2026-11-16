package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/terms"
)

// TestTheExtensionsNames: its ID in Chrome comes from its key as Chrome
// makes one, and the host's name is one the browsers take.
func TestTheExtensionsNames(t *testing.T) {
	if got := ChromeID(); got != "llbofblbfopkofjigpkgobkpchccbamd" {
		t.Errorf("ChromeID = %q", got)
	}
	if !regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`).MatchString(HostName()) {
		t.Errorf("HostName = %q: only lower-case letters, digits, underscores and single dots", HostName())
	}
	if !strings.HasPrefix(GeckoID(), "extension@") {
		t.Errorf("GeckoID = %q", GeckoID())
	}
}

// TestTheFilesItWrites: the manifest takes this build's names, its ID's
// key and Firefox's; every file the manifest and the panel name is there;
// and nothing loads from elsewhere.
func TestTheFilesItWrites(t *testing.T) {
	fs := files()
	var m struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		VersionTag string `json:"version_name"`
		Key        string `json:"key"`
		Background struct {
			Worker  string   `json:"service_worker"`
			Scripts []string `json:"scripts"`
		} `json:"background"`
		Action struct {
			Icons map[string]string `json:"default_icon"`
		} `json:"action"`
		Panel struct {
			Path string `json:"default_path"`
		} `json:"side_panel"`
		Sidebar struct {
			Panel string `json:"default_panel"`
		} `json:"sidebar_action"`
		Settings struct {
			Gecko struct {
				ID string `json:"id"`
			} `json:"gecko"`
		} `json:"browser_specific_settings"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(fs["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != brand.Name || m.Key != Key || m.Settings.Gecko.ID != GeckoID() || !strings.HasPrefix(m.VersionTag, m.Version+" ") || m.VersionTag != Version() {
		t.Errorf("manifest: %+v", m)
	}
	if !regexp.MustCompile(`^\d+(\.\d+){0,3}$`).MatchString(m.Version) {
		t.Errorf("version %q isn't Chrome's dotted numbers", m.Version)
	}
	// Only what it needs: no host permissions, no tabs, no history.
	if want := []string{"activeTab", "nativeMessaging", "scripting", "sidePanel", "storage"}; !slices.Equal(slices.Sorted(slices.Values(m.Permissions)), want) {
		t.Errorf("permissions %q, want %q", m.Permissions, want)
	}
	need := []string{m.Background.Worker, m.Panel.Path, m.Sidebar.Panel, "brand.js", "panel.js", "panel.css"}
	need = append(need, m.Background.Scripts...)
	for _, icon := range m.Action.Icons {
		need = append(need, icon)
	}
	for _, n := range need {
		if _, ok := fs[n]; !ok || n == "" {
			t.Errorf("the manifest or panel names %q, which isn't written", n)
		}
	}
	if !bytes.Contains(fs["brand.js"], []byte(`"host":"`+HostName()+`"`)) {
		t.Errorf("brand.js: %s", fs["brand.js"])
	}
	away := regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["']?(?:https?:)?//|url\(\s*["']?(?:https?:)?//|@import`)
	for name, b := range fs {
		if strings.HasSuffix(name, ".png") {
			continue
		}
		if away.Match(b) {
			t.Errorf("%s loads something from elsewhere: %s", name, away.Find(b))
		}
	}
}

// TestWriteKeepsTheFolderCurrent: the files go into the folder once,
// writing again changes nothing, and a file an older build left goes.
func TestWriteKeepsTheFolderCurrent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "extension")
	if changed, err := Write(dir); err != nil || !changed {
		t.Fatalf("Write: %v, %v", changed, err)
	}
	if changed, err := Write(dir); err != nil || changed {
		t.Errorf("again: %v, %v", changed, err)
	}
	old := filepath.Join(dir, "old.js")
	os.WriteFile(old, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644)
	if changed, err := Write(dir); err != nil || !changed {
		t.Errorf("with an old file: %v, %v", changed, err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("an older build's file stayed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".DS_Store")); err != nil {
		t.Error("a hidden file of the system's went")
	}
}

func testPaths(t *testing.T) (paths.Paths, string) {
	t.Helper()
	dir := t.TempDir()
	return paths.Paths{Config: filepath.Join(dir, "config"), State: filepath.Join(dir, "state"), Data: filepath.Join(dir, "data"), Runtime: filepath.Join(dir, "run")}, filepath.Join(dir, "home")
}

// TestSetupLooksAndRemoves: setup writes the folder, the host's program,
// which runs irie by its full path, and the host's file for each browser
// that has run here and no other; Look reads them; Remove takes them out.
func TestSetupLooksAndRemoves(t *testing.T) {
	p, home := testPaths(t)
	if _, err := Setup(p, home, "/opt/irie/bin/irie"); err == nil {
		t.Error("set up with no browser here")
	}
	var chrome, firefox Browser
	for _, b := range Browsers(home) {
		switch b.Name {
		case "Chrome":
			chrome = b
		case "Firefox":
			firefox = b
		}
	}
	for _, b := range []Browser{chrome, firefox} {
		if err := os.MkdirAll(b.Home, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Setup(p, home, "irie"); err == nil {
		t.Error("set up with a bare command")
	}
	done, err := Setup(p, home, "/opt/it's here/irie")
	if err != nil || !slices.Equal(done, []string{"Chrome", "Firefox"}) {
		t.Fatalf("Setup: %q, %v", done, err)
	}
	script, err := os.ReadFile(HostFile(p))
	if err != nil || !strings.Contains(string(script), `exec '/opt/it'\''s here/irie' extension host "$@"`) {
		t.Errorf("host program: %s, %v", script, err)
	}
	if st, _ := os.Stat(HostFile(p)); st.Mode()&0o111 == 0 {
		t.Error("the host's program can't run")
	}
	var cm, fm map[string]any
	readJSON(t, filepath.Join(chrome.Dir, HostName()+".json"), &cm)
	readJSON(t, filepath.Join(firefox.Dir, HostName()+".json"), &fm)
	if cm["name"] != HostName() || cm["path"] != HostFile(p) || cm["type"] != "stdio" || !slices.Equal(toStrings(cm["allowed_origins"]), []string{"chrome-extension://" + ChromeID() + "/"}) {
		t.Errorf("Chrome's host file: %v", cm)
	}
	if !slices.Equal(toStrings(fm["allowed_extensions"]), []string{GeckoID()}) || fm["allowed_origins"] != nil {
		t.Errorf("Firefox's host file: %v", fm)
	}
	s := Look(p, home)
	if s.Written != Version() || !s.Current || !slices.Equal(s.Browsers, []string{"Chrome", "Firefox"}) || len(s.Seen) != 0 {
		t.Errorf("Look: %+v", s)
	}
	if err := Remove(p, home); err != nil {
		t.Fatal(err)
	}
	if s := Look(p, home); s.Written != "" || len(s.Browsers) != 0 {
		t.Errorf("after Remove: %+v", s)
	}
	if _, err := os.Stat(HostFile(p)); err == nil {
		t.Error("the host's program stayed")
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func toStrings(v any) []string {
	var out []string
	xs, _ := v.([]any)
	for _, x := range xs {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// TestMessagesAreFramed: a message's length goes first, in this
// computer's byte order, and one too big for the host is refused.
func TestMessagesAreFramed(t *testing.T) {
	var b bytes.Buffer
	if err := WriteMessage(&b, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMessage(&b)
	if err != nil || string(got) != `{"a":1}` {
		t.Errorf("ReadMessage: %q, %v", got, err)
	}
	if err := WriteMessage(io.Discard, make([]byte, maxReply+1)); err == nil {
		t.Error("wrote a reply bigger than a browser takes")
	}
	b.Reset()
	b.Write([]byte{0xff, 0xff, 0xff, 0x7f})
	if _, err := ReadMessage(&b); err == nil {
		t.Error("read a message of 2 GB")
	}
}

const page = `<html><head><title>Terms of Service</title></head><body><main>
<h1>Terms of Service</h1>
<p>These Terms are an agreement between you and Example, Inc. ("Example", "we"). By using the Service you agree to them.</p>
<h2>Payments</h2><p>All fees are non-refundable. Your subscription will automatically renew until you cancel.</p>
<h2>Disputes</h2><p>Any dispute will be resolved by binding arbitration administered by JAMS, and not in court.</p>
<p>` + "These words are here so the page reads as terms of some length, as real terms do. " + `</p>
</main></body></html>`

// host runs Serve on pipes, as a browser would.
type testHost struct {
	in   *io.PipeWriter
	out  *io.PipeReader
	done chan error
}

func startHost(t *testing.T, o Options) *testHost {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &testHost{in: inW, out: outR, done: make(chan error, 1)}
	go func() {
		err := Serve(context.Background(), inR, outW, o)
		outW.Close()
		h.done <- err
	}()
	t.Cleanup(func() { inW.Close() })
	return h
}

func (h *testHost) call(t *testing.T, r Request) []Reply {
	t.Helper()
	b, _ := json.Marshal(r)
	if err := WriteMessage(h.in, b); err != nil {
		t.Fatal(err)
	}
	var out []Reply
	for {
		msg, err := ReadMessage(h.out)
		if err != nil {
			t.Fatalf("reading the reply to %s: %v", r.Op, err)
		}
		var rep Reply
		if err := json.Unmarshal(msg, &rep); err != nil {
			t.Fatal(err)
		}
		out = append(out, rep)
		if rep.Done {
			return out
		}
	}
}

// TestTheHostAnswersTheExtension: hello says who Irie is and writes this
// build's extension over an older one; a page read gives its points and
// questions to ask; a question gets an answer, by Irie's code when no
// local model is here; a document it doesn't hold, an op it doesn't know
// and a request that isn't JSON say so; with the plugin off only hello
// answers; and the browser closing the port ends it.
func TestTheHostAnswersTheExtension(t *testing.T) {
	p, _ := testPaths(t)
	on := true
	o := Options{
		Paths: p,
		Load:  func() (config.Config, error) { return config.Default(), nil },
		Why: func(cfg config.Config, part string) string {
			if !on {
				return "Terms in short is off."
			}
			return ""
		},
		Servers: func(config.Config) []string { return []string{"http://127.0.0.1:1"} },
		Fetcher: terms.Fetcher{Timeout: time.Second},
	}
	// An older build's extension in its folder.
	os.MkdirAll(Folder(p), 0o755)
	os.WriteFile(filepath.Join(Folder(p), "manifest.json"), []byte(`{"version_name":"0.1 old"}`), 0o644)
	h := startHost(t, o)

	hello := h.call(t, Request{ID: 1, Op: "hello", Browser: "Chrome", Version: "0.1 old"})[0]
	if hello.Hello == nil || !hello.Hello.On || hello.Hello.Name != brand.Name || !slices.Equal(hello.Hello.Tools, []string{"terms"}) || !hello.Hello.Updated {
		t.Fatalf("hello: %+v", hello)
	}
	if s := Look(p, ""); s.Written != Version() || s.Seen["Chrome"].IsZero() {
		t.Errorf("after hello: %+v", s)
	}
	read := h.call(t, Request{ID: 2, Op: "terms.read", URL: "https://example.com/terms", Title: "Terms of Service", HTML: page})[0]
	if read.Doc == nil || read.Doc.Kind != terms.KindTerms || len(read.Doc.Points) < 3 || len(read.Ask) == 0 {
		t.Fatalf("read: %+v", read)
	}
	ask := h.call(t, Request{ID: 3, Op: "terms.ask", Doc: read.Doc.ID, Question: "Can I get a refund?"})[0]
	if ask.Answer == nil || !strings.Contains(ask.Answer.Says, "refunds") || ask.Answer.By != terms.Code {
		t.Errorf("ask: %+v", ask)
	}
	model := h.call(t, Request{ID: 4, Op: "terms.ask", Doc: read.Doc.ID, Question: "Can I get a refund?", Model: true})[0]
	if model.Answer == nil || model.Code != "model" || model.Answer.By != terms.Code {
		t.Errorf("ask a model that isn't here: %+v", model)
	}
	if r := h.call(t, Request{ID: 5, Op: "terms.ask", Doc: "nope", Question: "?"})[0]; r.Code != "gone" {
		t.Errorf("a document it doesn't hold: %+v", r)
	}
	if r := h.call(t, Request{ID: 6, Op: "dance"})[0]; r.Code != "bad" {
		t.Errorf("an op it doesn't know: %+v", r)
	}
	if r := h.call(t, Request{ID: 7, Op: "terms.read"})[0]; r.Code != "bad" {
		t.Errorf("nothing to read: %+v", r)
	}
	WriteMessage(h.in, []byte("not json"))
	if msg, err := ReadMessage(h.out); err != nil || !strings.Contains(string(msg), `"code":"bad"`) {
		t.Errorf("not JSON: %s, %v", msg, err)
	}
	on = false
	if r := h.call(t, Request{ID: 8, Op: "terms.read", URL: "https://example.com/terms", HTML: page})[0]; r.Code != "off" || r.Error != "Terms in short is off." {
		t.Errorf("off: %+v", r)
	}
	if r := h.call(t, Request{ID: 9, Op: "hello"})[0]; r.Hello == nil || r.Hello.On || r.Hello.Why == "" {
		t.Errorf("hello while off: %+v", r)
	}
	h.in.Close()
	select {
	case err := <-h.done:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Serve went on after the port closed")
	}
}

// TestSeenSaysWhenEachBrowserTalked: the newest first, in words.
func TestSeenSaysWhenEachBrowserTalked(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	s := Status{Seen: map[string]time.Time{"Firefox": now.Add(-3 * time.Hour), "Chrome": now.Add(-2 * time.Minute)}}
	if got := Seen(s, now); got != "Chrome 2 minutes ago, Firefox 3 hours ago" {
		t.Errorf("Seen = %q", got)
	}
}
