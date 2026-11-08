// Package browser is Irie's browser connector (DESIGN §4.9): `irie browser`,
// an MCP server that gives an agent a real web browser, Mullvad Browser by
// default, for research on pages that a plain fetch cannot read. It drives
// the browser over WebDriver BiDi through Firefox's Remote Agent, which
// Mullvad Browser keeps, so the browser runs unmodified, with its
// fingerprinting protections and private browsing on. The browser starts on
// an agent's first call, headless, on a profile of its own that is removed
// when it closes, and closes after a while without calls.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Options configure the connector.
type Options struct {
	Path     string        // the browser's executable; "" = Mullvad Browser where its installers put it
	Profile  string        // the folder of the browser's profile, made fresh at each start and removed after; "" = a temporary folder
	Template string        // a clean profile, made once and copied at each start, which starts faster; "" = none
	Headless bool          // draw no window
	Idle     time.Duration // close the browser after this long without a call; 0 = keep it open
	Proxy    string        // send the browser's traffic through this SOCKS5 proxy, socks5://host:port; "" = none
	Search   string        // the search tool's address, with %s for the query
	Version  string        // Irie's version, for the server's name
	Log      io.Writer     // diagnostics; nil = none
}

// DefaultSearch is DuckDuckGo's page for browsers without JavaScript,
// which answers with plain links.
const DefaultSearch = "https://html.duckduckgo.com/html/?q=%s"

// Limits on what one call returns, under the 50,000 characters past which
// Claude Code saves a tool's result to a file instead.
const (
	readLimit      = 10000
	readMax        = 40000
	maxTabs        = 6
	maxTyped       = 10000
	maxShotHeight  = 6000
	navTimeout     = 45 * time.Second
	settleMax      = 8 * time.Second
	afterClickWait = 1500 * time.Millisecond
)

// Browser is the server's one browser, started on the first call that needs
// it. Calls take turns on it: mu is held through each.
type Browser struct {
	o  Options
	mu sync.Mutex

	run    *running
	tabs   []*tab // in the order they were opened
	cur    *tab   // the tab a call that names none uses
	nextN  int
	last   time.Time // when the last call ended
	gone   string    // why the browser closed since the last call; "" = it did not
	tmpDir string    // the temporary profile folder, without Options.Profile
}

type tab struct {
	n    int
	ctx  string // the browsing context
	used time.Time
	next int // where the next read starts
}

// New returns a connector with nothing started.
func New(o Options) *Browser {
	if o.Search == "" {
		o.Search = DefaultSearch
	}
	return &Browser{o: o, last: time.Now()}
}

// Serve runs the MCP server on in and out until in ends or ctx is done,
// then closes the browser.
func Serve(ctx context.Context, in io.Reader, out io.Writer, o Options) error {
	b := New(o)
	defer b.Close()
	go b.closeWhenIdle(ctx)
	s := &server{name: "irie-browser", version: o.Version, instructions: b.instructions(), tools: b.tools(), out: out}
	return s.serve(ctx, in)
}

// Close closes the browser, if it runs, and removes its profile.
func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stop("")
	if b.tmpDir != "" {
		os.RemoveAll(b.tmpDir)
		b.tmpDir = ""
	}
}

func (b *Browser) stop(why string) {
	if b.run == nil {
		return
	}
	b.run.quit()
	os.RemoveAll(b.run.profile)
	b.run, b.tabs, b.cur = nil, nil, nil
	b.gone = why
}

// closeWhenIdle closes the browser once no call has come for o.Idle. The
// next call starts it again.
func (b *Browser) closeWhenIdle(ctx context.Context) {
	if b.o.Idle <= 0 {
		return
	}
	t := time.NewTicker(min(b.o.Idle/4, 30*time.Second))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.mu.Lock()
			closed := false
			if b.run != nil && time.Since(b.last) >= b.o.Idle {
				b.stop(fmt.Sprintf("it closed after %s without a call", roundDur(b.o.Idle)))
				closed = true
			}
			b.mu.Unlock()
			// With nobody waiting, make the template for the next start
			// when it is missing or a new build of the browser came.
			if closed && b.o.Template != "" {
				if exe, err := Find(b.o.Path); err == nil && !TemplateReady(b.o.Template, exe) {
					if err := ensureTemplate(ctx, b.o, exe); err != nil {
						logf(b.o, "making the template profile: %v", err)
					}
				}
			}
		}
	}
}

// Check starts the browser, opens address and returns what an agent would
// read there, then closes it: `irie setup browser` runs it.
func Check(ctx context.Context, o Options, address string) (string, error) {
	b := New(o)
	defer b.Close()
	r := b.open(ctx, mustJSON(map[string]any{"url": address}))
	text := r.Content[0].Text
	if r.IsError {
		return "", errors.New(text)
	}
	return text, nil
}

// ensure starts the browser when it is not running. The note it returns
// tells the agent what happened, such as a restart that lost its tabs.
func (b *Browser) ensure(ctx context.Context) (string, error) {
	if b.run != nil && b.run.alive() {
		return "", nil
	}
	note := ""
	switch {
	case b.run != nil:
		b.run.kill()
		os.RemoveAll(b.run.profile)
		b.run, b.tabs, b.cur = nil, nil, nil
		note = "The browser had stopped, so it started again; its earlier tabs are gone. "
	case b.gone != "":
		note = "The browser started again (" + b.gone + "); its earlier tabs are gone. "
	}
	b.gone = ""
	exe, err := Find(b.o.Path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", errors.New("Mullvad Browser is not installed here. Ask the user to install it (`irie setup browser`, or `brew install --cask mullvad-browser` on a Mac), or to set browser.path in Irie's config to another Firefox-based browser")
		}
		return "", fmt.Errorf("the browser in browser.path: %w", err)
	}
	dir := b.o.Profile
	if dir == "" {
		if b.tmpDir == "" {
			if b.tmpDir, err = os.MkdirTemp("", "irie-browser-"); err != nil {
				return "", err
			}
		}
		dir = filepath.Join(b.tmpDir, "profile")
	}
	t0 := time.Now()
	r, err := launch(ctx, b.o, exe, dir)
	if err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("the browser did not start: %w", err)
	}
	b.run, b.tabs, b.cur, b.nextN = r, nil, nil, 0
	var tree struct {
		Contexts []struct {
			Context string `json:"context"`
		} `json:"contexts"`
	}
	if err := r.conn.call(ctx, "browsingContext.getTree", map[string]any{"maxDepth": 0}, &tree); err == nil && len(tree.Contexts) > 0 {
		b.addTab(tree.Contexts[0].Context)
	}
	return note + fmt.Sprintf("(Started %s in %s.) ", r.name, roundDur(time.Since(t0))), nil
}

// need returns an error when no browser runs, for the calls that work on a
// page already open.
func (b *Browser) need() error {
	if b.run != nil && b.run.alive() {
		return nil
	}
	if b.gone != "" {
		return fmt.Errorf("the browser is not open: %s, and its pages went with it. Open the page again", b.gone)
	}
	if b.run != nil {
		return errors.New("the browser stopped, and its pages went with it. Open the page again")
	}
	return errors.New("nothing is open yet: start with open or search")
}

func (b *Browser) addTab(ctx string) *tab {
	b.nextN++
	t := &tab{n: b.nextN, ctx: ctx, used: time.Now()}
	b.tabs = append(b.tabs, t)
	return t
}

// tabFor returns the tab a call works on: tab n when it is set, a new tab
// with newTab, else the tab used last.
func (b *Browser) tabFor(ctx context.Context, n int, newTab bool) (*tab, error) {
	var t *tab
	switch {
	case newTab || (n == 0 && b.cur == nil && len(b.tabs) == 0):
		var c struct {
			Context string `json:"context"`
		}
		if err := b.run.conn.call(ctx, "browsingContext.create", map[string]any{"type": "tab"}, &c); err != nil {
			return nil, err
		}
		t = b.addTab(c.Context)
		b.trimTabs(ctx, t)
	case n != 0:
		i := slices.IndexFunc(b.tabs, func(x *tab) bool { return x.n == n })
		if i < 0 {
			return nil, fmt.Errorf("there is no tab %d; tabs lists the open ones", n)
		}
		t = b.tabs[i]
	case b.cur != nil:
		t = b.cur
	default:
		t = b.tabs[len(b.tabs)-1]
	}
	t.used, b.cur = time.Now(), t
	return t, nil
}

// trimTabs closes the tabs used longest ago beyond maxTabs, keeping keep:
// each tab can hold a process of its own.
func (b *Browser) trimTabs(ctx context.Context, keep *tab) {
	for len(b.tabs) > maxTabs {
		old := slices.MinFunc(slices.DeleteFunc(slices.Clone(b.tabs), func(x *tab) bool { return x == keep }), func(x, y *tab) int { return x.used.Compare(y.used) })
		b.closeTab(ctx, old)
	}
}

func (b *Browser) closeTab(ctx context.Context, t *tab) {
	b.run.conn.call(ctx, "browsingContext.close", map[string]any{"context": t.ctx}, nil)
	b.tabs = slices.DeleteFunc(b.tabs, func(x *tab) bool { return x == t })
	if b.cur == t {
		b.cur = nil
	}
}

// sharedRef is a node the browser handed back, to pass to a later script.
type sharedRef string

// local turns a Go value into WebDriver BiDi's script.LocalValue.
func local(v any) any {
	switch x := v.(type) {
	case string:
		return map[string]any{"type": "string", "value": x}
	case int:
		return map[string]any{"type": "number", "value": x}
	case bool:
		return map[string]any{"type": "boolean", "value": x}
	case sharedRef:
		return map[string]any{"sharedId": string(x)}
	default:
		return map[string]any{"type": "null"}
	}
}

// fn calls a script function in the tab's page with args.
func (b *Browser) fn(ctx context.Context, t *tab, decl string, args ...any) (evalResult, error) {
	la := make([]any, len(args))
	for i, a := range args {
		la[i] = local(a)
	}
	var r evalResult
	err := b.run.conn.call(ctx, "script.callFunction", map[string]any{
		"functionDeclaration": decl,
		"target":              map[string]any{"context": t.ctx},
		"arguments":           la,
		"awaitPromise":        true,
		"resultOwnership":     "none",
	}, &r)
	return r, err
}

// jsonFn calls a script function that returns JSON text, and decodes it.
func (b *Browser) jsonFn(ctx context.Context, t *tab, decl string, out any, args ...any) error {
	r, err := b.fn(ctx, t, decl, args...)
	if err != nil {
		return err
	}
	if r.Type == "exception" {
		return errors.New(r.errorText())
	}
	var s string
	if r.Result.Type != "string" || json.Unmarshal(r.Result.Value, &s) != nil {
		return fmt.Errorf("the page script returned %s", r.Result.Type)
	}
	return json.Unmarshal([]byte(s), out)
}

// The scripts the tools run in a page. Each returns JSON text, or for the
// finders a node with JSON text beside it.
const (
	jsState = `() => JSON.stringify({url: location.href, ready: document.readyState, len: document.body ? document.body.innerText.length : 0})`

	// jsText returns the page's text: its main content when the page marks
	// one that holds most of its words, else all of it.
	jsText = `(whole) => {
  const d = document, b = d.body;
  const out = {title: d.title || "", url: location.href, text: "", part: "", links: d.links ? d.links.length : 0};
  if (!b) return JSON.stringify(out);
  const all = b.innerText || "";
  let text = all;
  if (!whole) {
    for (const sel of ["main", "article", "[role=main]"]) {
      const el = d.querySelector(sel);
      const t = el ? el.innerText || "" : "";
      if (t.trim().length >= 200 && t.length >= all.length * 0.3 && t.length < all.length) { text = t; out.part = sel; break; }
    }
  }
  out.text = text.replace(/[ \t\u00a0]+\n/g, "\n").replace(/\n{3,}/g, "\n\n").trim();
  return JSON.stringify(out);
}`

	// jsLinks numbers the page's visible links, for click, and lists them.
	// A DuckDuckGo result points through its redirect; the list shows
	// where it goes.
	jsLinks = `(filter) => {
  const f = (filter || "").toLowerCase();
  for (const el of document.querySelectorAll("[data-irie-n]")) el.removeAttribute("data-irie-n");
  const seen = new Map(), out = [];
  let n = 0;
  for (const a of document.querySelectorAll("a[href]")) {
    let href = a.href;
    if (!/^https?:/i.test(href) || !a.getClientRects().length) continue;
    try {
      const u = new URL(href);
      if (/(^|\.)duckduckgo\.com$/.test(u.hostname) && u.pathname === "/l/" && u.searchParams.get("uddg")) href = u.searchParams.get("uddg");
    } catch (e) {}
    const text = (a.innerText || a.getAttribute("aria-label") || a.title || "").replace(/\s+/g, " ").trim().slice(0, 120);
    if (f && !text.toLowerCase().includes(f) && !href.toLowerCase().includes(f)) continue;
    // One line per address: a result's title, icon and snippet often all
    // link to it. The first words found name it.
    if (seen.has(href)) {
      const row = out[seen.get(href)];
      if (!row[1] && text) row[1] = text;
      continue;
    }
    seen.set(href, out.length);
    a.setAttribute("data-irie-n", String(++n));
    out.push([n, text, href]);
    if (n >= 300) break;
  }
  return JSON.stringify({total: document.links.length, links: out});
}`

	// jsFind finds what click names: a link number, a CSS selector or the
	// words on a link or button, best match first.
	jsFind = `(n, text, selector) => {
  let el = null;
  if (n) el = document.querySelector('[data-irie-n="' + n + '"]');
  else if (selector) el = document.querySelector(selector);
  else if (text) {
    const norm = (s) => (s || "").replace(/\s+/g, " ").trim().toLowerCase();
    const want = norm(text);
    let score = 0;
    for (const c of document.querySelectorAll("a, button, input[type=submit], input[type=button], [role=button], [role=link], [role=tab], [role=menuitem], [role=option], summary, label, [onclick]")) {
      if (!c.getClientRects().length) continue;
      const t = norm(c.innerText || c.value || c.getAttribute("aria-label") || c.title);
      if (!t) continue;
      const s = t === want ? 3 : t.startsWith(want) ? 2 : t.includes(want) ? 1 : 0;
      if (s > score) { el = c; score = s; if (s === 3) break; }
    }
  }
  if (!el) return [null, "{}"];
  el.scrollIntoView({block: "center", inline: "center"});
  const a = el.closest("a[href]");
  const what = (el.innerText || el.value || el.getAttribute("aria-label") || el.title || "").replace(/\s+/g, " ").trim().slice(0, 80);
  return [el, JSON.stringify({tag: el.tagName.toLowerCase(), text: what, href: a ? a.href : "", target: a ? a.target : "", url: location.href})];
}`

	// jsField finds the field type names, focuses it and selects what it
	// holds, so typing replaces it.
	jsField = `(field) => {
  const shown = (e) => e && e.getClientRects().length > 0;
  let el = null;
  if (field) {
    try { el = document.querySelector(field); } catch (e) {}
    if (!el) {
      const want = field.toLowerCase();
      for (const c of document.querySelectorAll("input, textarea, select, [contenteditable]")) {
        if (!shown(c) || c.type === "hidden") continue;
        const words = [c.placeholder, c.getAttribute("aria-label"), c.name, c.id, c.title, ...(c.labels ? Array.from(c.labels, (l) => l.innerText) : [])].filter(Boolean).join(" ").toLowerCase();
        if (words.includes(want)) { el = c; break; }
      }
    }
  } else {
    const f = document.activeElement;
    if (f && f !== document.body && (f.isContentEditable || /^(input|textarea)$/i.test(f.tagName))) el = f;
    if (!el) for (const c of document.querySelectorAll("input[type=search], input[type=text], input:not([type]), textarea, [contenteditable=true]")) { if (shown(c)) { el = c; break; } }
  }
  if (!el) return [null, "{}"];
  el.scrollIntoView({block: "center"});
  el.focus();
  if (typeof el.select === "function") { try { el.select(); } catch (e) {} }
  const label = el.getAttribute("aria-label") || el.placeholder || el.name || el.id || el.tagName.toLowerCase();
  return [el, JSON.stringify({label: label, url: location.href})];
}`

	jsSize = `() => { const e = document.documentElement, b = document.body; return JSON.stringify({w: Math.max(e.scrollWidth, b ? b.scrollWidth : 0), h: Math.max(e.scrollHeight, b ? b.scrollHeight : 0)}); }`

	jsTitle = `() => JSON.stringify({title: document.title || "", url: location.href})`
)

type pageText struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Text  string `json:"text"`
	Part  string `json:"part"`
	Links int    `json:"links"`
}

// navigate loads address in the tab. A page that takes longer than
// navTimeout is read as far as it got.
func (b *Browser) navigate(ctx context.Context, t *tab, address string) (string, error) {
	nctx, cancel := context.WithTimeout(ctx, navTimeout)
	defer cancel()
	t.next = 0
	err := b.run.conn.call(nctx, "browsingContext.navigate", map[string]any{"context": t.ctx, "url": address, "wait": "interactive"}, nil)
	switch {
	case err == nil:
		return "", nil
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
		return fmt.Sprintf("\n(The page was still loading after %s; this is what it showed by then.)", roundDur(navTimeout)), nil
	default:
		var be *bidiError
		if errors.As(err, &be) {
			return "", fmt.Errorf("could not load %s: %s", address, be.Message)
		}
		return "", err
	}
}

// settle waits, at most max, for the page to finish loading and its text to
// stop changing, as a page that builds itself with scripts does after load.
func (b *Browser) settle(ctx context.Context, t *tab, max time.Duration) {
	deadline := time.Now().Add(max)
	last, stable := -1, 0
	for time.Now().Before(deadline) {
		var st struct {
			Ready string `json:"ready"`
			Len   int    `json:"len"`
		}
		if err := b.jsonFn(ctx, t, jsState, &st); err == nil && st.Ready == "complete" {
			if st.Len == last && st.Len > 0 {
				if stable++; stable >= 2 {
					return
				}
			} else {
				stable = 0
			}
			last = st.Len
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// afterAction waits for what a click or Enter starts: a new page, or a page
// that changes in place.
func (b *Browser) afterAction(ctx context.Context, t *tab, before string) {
	deadline := time.Now().Add(afterClickWait)
	for time.Now().Before(deadline) {
		var st struct {
			URL   string `json:"url"`
			Ready string `json:"ready"`
		}
		// An error here is a page being replaced.
		if err := b.jsonFn(ctx, t, jsState, &st); err != nil || st.URL != before || st.Ready != "complete" {
			t.next = 0
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	b.settle(ctx, t, settleMax)
}

// read returns the tab's page as the agent reads it: its tab, title and
// address, then its text from offset, at most limit characters.
func (b *Browser) read(ctx context.Context, t *tab, offset, limit int, whole bool) (string, error) {
	var p pageText
	if err := b.jsonFn(ctx, t, jsText, &p, whole); err != nil {
		return "", err
	}
	text := []rune(p.Text)
	offset = min(max(offset, 0), len(text))
	end := min(offset+limit, len(text))
	t.next = end
	var s strings.Builder
	fmt.Fprintf(&s, "Tab %d · %s\n%s\n\n", t.n, oneLine(p.Title), p.URL)
	switch {
	case len(text) == 0:
		s.WriteString("(The page shows no text. It may still be loading, or be all pictures: screenshot shows it.)\n")
	case offset == len(text):
		s.WriteString("(That is the end of the page.)\n")
	default:
		s.WriteString(string(text[offset:end]))
		s.WriteString("\n")
	}
	var foot []string
	if len(text) > 0 {
		f := fmt.Sprintf("characters %s–%s of %s", num(offset), num(end), num(len(text)))
		if end < len(text) {
			f += ": read goes on from " + num(end)
		}
		foot = append(foot, f)
	}
	if p.Part != "" {
		foot = append(foot, "the page's main part only; read with whole for all of it")
	}
	switch {
	case p.Links == 1:
		foot = append(foot, "1 link: links lists it for click")
	case p.Links > 1:
		foot = append(foot, fmt.Sprintf("%s links: links lists them for click", num(p.Links)))
	}
	if len(foot) > 0 {
		s.WriteString("\n[" + strings.Join(foot, " · ") + "]")
	}
	return s.String(), nil
}

// normURL checks an address and adds https:// when it has no scheme. Only
// web pages load: a file or another scheme is refused.
func normURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("no address given")
	}
	if s == "about:blank" {
		return s, nil
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("%q is not an address: %v", s, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https pages open in the browser, not %s:", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%q has no host", s)
	}
	return u.String(), nil
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "(no title)"
	}
	return s
}

// num writes n with thousands separated, as 12,345.
func num(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func roundDur(d time.Duration) string {
	switch {
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	case d >= 10*time.Second:
		return fmt.Sprintf("%d s", int(d.Round(time.Second)/time.Second))
	default:
		return fmt.Sprintf("%.1f s", d.Seconds())
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// call runs one tool call: it holds the browser, decodes the arguments and
// marks the time, so the idle closer waits for the call to end.
func call[A any](b *Browser, args json.RawMessage, body func(a A) toolResult) toolResult {
	var a A
	if err := json.Unmarshal(args, &a); err != nil {
		return errorResult("bad arguments: %v", err)
	}
	b.mu.Lock()
	defer func() {
		b.last = time.Now()
		b.mu.Unlock()
	}()
	return body(a)
}
