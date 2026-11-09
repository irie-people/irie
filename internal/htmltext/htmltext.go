// Package htmltext reads an HTML page as text an agent can read.
//
// The page is untrusted and may be hostile. Read never panics, never fails
// and takes time linear in the size of the page. It uses a small tokenizer
// of its own, not a browser: scripts, styles and hidden parts are dropped,
// block elements become lines, headings, list items and table cells keep a
// light mark, and links are collected with their words.
package htmltext

import (
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Link is one link of a page.
type Link struct {
	Text string // the link's words, whitespace collapsed, cut to at most 100 characters
	URL  string // an absolute http or https address, without its fragment
}

// Page is what Read finds in a page.
type Page struct {
	Title string
	Text  string // the readable text, its main part first
	Links []Link // in page order, one per URL, at most 300
}

const (
	maxLinks    = 300
	maxLinkText = 100  // characters
	linkBufMax  = 400  // bytes kept while a link's words are gathered
	h1BufMax    = 1000 // bytes kept while a first heading is gathered
)

// Read reads page, which is assumed to be UTF-8, resolving links against
// base. When base is nil, only absolute http and https links are kept.
func Read(page []byte, base *url.URL) Page {
	s := strings.ToValidUTF8(string(page), "�")
	r := &reader{base: base, seen: map[string]int{}}
	r.updateActive()
	r.run(s)
	r.endLink()
	r.finishH1()

	p := Page{Title: r.title, Links: r.links}
	if p.Title == "" {
		p.Title = r.h1Title
	}
	switch {
	case r.mainState != 0:
		p.Text = join(r.inMain.text(), r.outMain.text())
	case r.artState != 0:
		p.Text = join(r.inArt.text(), r.outArt.text())
	default:
		p.Text = r.all.text()
	}
	return p
}

// join puts the main part first, then a rule, then the rest.
func join(in, rest string) string {
	switch {
	case in == "":
		return rest
	case rest == "":
		return in
	}
	return in + "\n\n---\n\n" + rest
}

var blockBreak = map[string]int{
	"p": 2, "h1": 2, "h2": 2, "h3": 2, "h4": 2, "h5": 2, "h6": 2,
	"pre": 2, "blockquote": 2, "table": 2,
	"div": 1, "section": 1, "article": 1, "main": 1, "header": 1,
	"footer": 1, "nav": 1, "aside": 1, "ul": 1, "ol": 1, "li": 1,
	"thead": 1, "tbody": 1, "tfoot": 1, "tr": 1, "hr": 1, "dl": 1,
	"dt": 1, "dd": 1, "figure": 1, "figcaption": 1, "form": 1,
	"fieldset": 1, "address": 1, "details": 1, "summary": 1, "caption": 1,
}

var skipped = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"svg": true, "math": true, "iframe": true, "object": true,
	"embed": true, "head": true, "select": true, "canvas": true,
	"audio": true, "video": true,
}

var rawText = map[string]bool{
	"script": true, "style": true, "textarea": true, "title": true,
	"xmp": true, "noscript": true, "iframe": true, "noembed": true,
	"noframes": true,
}

// writer gathers lines of text.
type writer struct {
	buf    []byte
	nl     int    // newlines owed before the next word
	sp     bool   // a space is owed before the next word
	prefix string // written before the next word
}

// flush writes the owed newlines and prefix. It says whether a prefix went out.
func (w *writer) flush() bool {
	if len(w.buf) > 0 {
		for ; w.nl > 0; w.nl-- {
			w.buf = append(w.buf, '\n')
		}
	}
	w.nl = 0
	if w.prefix != "" {
		w.buf = append(w.buf, w.prefix...)
		w.prefix = ""
		return true
	}
	return false
}

func (w *writer) word(s string) {
	prefixed := w.flush()
	if w.sp && !prefixed && len(w.buf) > 0 && w.buf[len(w.buf)-1] != '\n' {
		w.buf = append(w.buf, ' ')
	}
	w.sp = false
	w.buf = append(w.buf, s...)
}

func (w *writer) block(n int) {
	if n > w.nl {
		w.nl = n
	}
}

// raw writes s as it is, for pre. Newlines at its end are owed, not written.
func (w *writer) raw(s string) {
	t := 0
	for t < len(s) && s[len(s)-1-t] == '\n' {
		t++
	}
	if body := s[:len(s)-t]; body != "" {
		w.flush()
		w.sp = false
		w.buf = append(w.buf, body...)
	}
	if t > w.nl {
		w.nl = t
	}
}

func (w *writer) text() string {
	return strings.Trim(string(w.buf), "\n")
}

type reader struct {
	base *url.URL

	all, inMain, outMain, inArt, outArt writer
	active                              [3]*writer

	mainState, mainDepth int // state: 0 not seen, 1 inside, 2 closed
	artState, artDepth   int

	skipName  string
	skipDepth int

	preDepth int
	preLead  bool
	rowCells int

	title    string
	titleSet bool

	h1On    bool
	h1Done  bool
	h1Buf   []byte
	h1Title string

	collSp   bool // a space is owed before the next collected word
	linkOpen bool
	linkURL  string
	linkBuf  []byte
	links    []Link
	seen     map[string]int
}

func (r *reader) updateActive() {
	r.active[0] = &r.all
	r.active[1] = &r.outMain
	if r.mainState == 1 {
		r.active[1] = &r.inMain
	}
	r.active[2] = &r.outArt
	if r.artState == 1 {
		r.active[2] = &r.inArt
	}
}

func (r *reader) block(n int) {
	for _, w := range r.active {
		w.block(n)
	}
}

func (r *reader) setPrefix(p string) {
	for _, w := range r.active {
		w.prefix = p
	}
}

func (r *reader) setSpace() {
	for _, w := range r.active {
		w.sp = true
	}
	r.collSp = true
}

func (r *reader) word(s string) {
	for _, w := range r.active {
		w.word(s)
	}
	r.collect(s)
}

// collect adds a word to the link and heading being gathered.
func (r *reader) collect(s string) {
	if r.linkOpen {
		r.linkBuf = appendLimited(r.linkBuf, s, linkBufMax, r.collSp)
	}
	if r.h1On {
		r.h1Buf = appendLimited(r.h1Buf, s, h1BufMax, r.collSp)
	}
	r.collSp = false
}

func appendLimited(b []byte, s string, max int, space bool) []byte {
	sep := 0
	if len(b) > 0 && space {
		sep = 1
	}
	room := max - len(b) - sep
	if room <= 0 {
		return b
	}
	if sep == 1 {
		b = append(b, ' ')
	}
	return append(b, clip(s, room)...)
}

// clip cuts s to at most n bytes, on a character boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func clipRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// text adds a run of page text, with whitespace collapsed.
func (r *reader) text(s string) {
	if r.skipName != "" || s == "" {
		return
	}
	if r.preDepth > 0 {
		r.preText(s)
		return
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		r.setSpace()
		return
	}
	if c, _ := utf8.DecodeRuneInString(s); unicode.IsSpace(c) {
		r.setSpace()
	}
	for k, x := range f {
		if k > 0 {
			r.setSpace()
		}
		r.word(x)
	}
	if c, _ := utf8.DecodeLastRuneInString(s); unicode.IsSpace(c) {
		r.setSpace()
	}
}

func (r *reader) preText(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if r.preLead {
		r.preLead = false
		s = strings.TrimPrefix(s, "\n")
	}
	for _, w := range r.active {
		w.raw(s)
	}
	if r.linkOpen || r.h1On {
		for k, x := range strings.Fields(s) {
			if k > 0 {
				r.collSp = true
			}
			r.collect(x)
		}
		if c, _ := utf8.DecodeLastRuneInString(s); unicode.IsSpace(c) {
			r.collSp = true
		}
	}
}

// run reads the whole page.
func (r *reader) run(s string) {
	n := len(s)
	i := 0
	for i < n {
		j := nextMarkup(s, i)
		if j > i {
			r.text(html.UnescapeString(s[i:j]))
		}
		if j >= n {
			return
		}
		i = r.markup(s, j)
	}
}

func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// nextMarkup finds the next "<" at or after i that starts a tag, comment or
// the like. A "<" before anything else is text.
func nextMarkup(s string, i int) int {
	for {
		k := strings.IndexByte(s[i:], '<')
		if k < 0 {
			return len(s)
		}
		i += k
		if i+1 < len(s) {
			if c := s[i+1]; isLetter(c) || c == '/' || c == '!' || c == '?' {
				return i
			}
		}
		i++
	}
}

// skipPast gives the index after the first end in s from j, or len(s).
func skipPast(s string, j int, end string, from int) int {
	k := strings.Index(s[from:], end)
	if k < 0 {
		return len(s)
	}
	return from + k + len(end)
}

// markup handles the markup at s[j] and gives the index to go on from.
func (r *reader) markup(s string, j int) int {
	switch s[j+1] {
	case '!':
		switch {
		case strings.HasPrefix(s[j:], "<!--"):
			return skipPast(s, j, "-->", j+4)
		case strings.HasPrefix(s[j:], "<![CDATA["):
			return skipPast(s, j, "]]>", j+9)
		}
		return skipPast(s, j, ">", j)
	case '?':
		return skipPast(s, j, ">", j)
	case '/':
		if j+2 < len(s) && isLetter(s[j+2]) {
			t, ok := scanTag(s, j)
			if !ok {
				return len(s)
			}
			r.closeTag(t.name)
			return t.end
		}
		return skipPast(s, j, ">", j)
	}
	t, ok := scanTag(s, j)
	if !ok {
		return len(s)
	}
	r.openTag(t)
	if !rawText[t.name] {
		return t.end
	}
	end := findClose(s, t.end, t.name)
	if end < 0 {
		end = len(s)
	}
	r.rawContent(t.name, s[t.end:end])
	return end
}

func (r *reader) rawContent(name, content string) {
	if r.skipName != "" && name != "title" {
		return
	}
	if name == "title" {
		if !r.titleSet && (r.skipName == "" || r.skipName == "head") {
			r.titleSet = true
			r.title = strings.Join(strings.Fields(html.UnescapeString(content)), " ")
		}
		return
	}
	r.text(html.UnescapeString(content))
}

// findClose finds the end tag of a raw-text element, from from.
func findClose(s string, from int, name string) int {
	for i := from; ; {
		k := strings.Index(s[i:], "</")
		if k < 0 {
			return -1
		}
		p := i + k + 2
		if len(s)-p >= len(name) && eqFold(s[p:p+len(name)], name) {
			q := p + len(name)
			if q == len(s) || isSpaceByte(s[q]) || s[q] == '/' || s[q] == '>' {
				return p - 2
			}
		}
		i = p - 1
	}
}

// eqFold says whether a equals b, which is lower case, ignoring ASCII case.
func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		c := a[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != b[i] {
			return false
		}
	}
	return true
}

type tag struct {
	name      string
	selfClose bool
	href, alt string
	hasHref   bool
	end       int
}

// scanTag reads the tag at s[i], which is "<". It says false when the tag
// is not closed before the end of s.
func scanTag(s string, i int) (tag, bool) {
	var t tag
	n := len(s)
	j := i + 1
	closing := false
	if s[j] == '/' {
		closing = true
		j++
	}
	ns := j
	for j < n && !isSpaceByte(s[j]) && s[j] != '/' && s[j] != '>' {
		j++
	}
	t.name = strings.ToLower(s[ns:j])
	want := !closing && (t.name == "a" || t.name == "img")
	for {
		for j < n && isSpaceByte(s[j]) {
			j++
		}
		if j >= n {
			return t, false
		}
		switch s[j] {
		case '>':
			t.end = j + 1
			return t, true
		case '/':
			if j+1 < n && s[j+1] == '>' {
				t.selfClose = true
				t.end = j + 2
				return t, true
			}
			j++
			continue
		}
		as := j
		j++
		for j < n && !isSpaceByte(s[j]) && s[j] != '/' && s[j] != '>' && s[j] != '=' {
			j++
		}
		an := s[as:j]
		for j < n && isSpaceByte(s[j]) {
			j++
		}
		val := ""
		if j < n && s[j] == '=' {
			j++
			for j < n && isSpaceByte(s[j]) {
				j++
			}
			if j >= n {
				return t, false
			}
			if q := s[j]; q == '"' || q == '\'' {
				k := strings.IndexByte(s[j+1:], q)
				if k < 0 {
					return t, false
				}
				val = s[j+1 : j+1+k]
				j += k + 2
			} else {
				vs := j
				for j < n && !isSpaceByte(s[j]) && s[j] != '>' {
					j++
				}
				val = s[vs:j]
			}
		}
		if want {
			switch {
			case eqFold(an, "href") && !t.hasHref && t.name == "a":
				t.href, t.hasHref = val, true
			case eqFold(an, "alt") && t.alt == "" && t.name == "img":
				t.alt = val
			}
		}
	}
}

func headingLevel(name string) int {
	if len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6' {
		return int(name[1] - '0')
	}
	return 0
}

func (r *reader) openTag(t tag) {
	name := t.name
	if r.skipName != "" {
		switch {
		case name == r.skipName && !t.selfClose:
			r.skipDepth++
		case name == "body" && r.skipName == "head":
			r.skipName, r.skipDepth = "", 0
		}
		return
	}
	if skipped[name] {
		if name == "embed" || ((name == "svg" || name == "math") && t.selfClose) {
			return
		}
		r.endLink()
		r.skipName, r.skipDepth = name, 1
		return
	}
	if b := blockBreak[name]; b > 0 {
		r.block(b)
	}
	if lvl := headingLevel(name); lvl > 0 {
		r.finishH1()
		r.setPrefix(strings.Repeat("#", lvl) + " ")
		if lvl == 1 && !r.h1Done {
			r.h1On = true
		}
		return
	}
	switch name {
	case "main":
		r.enter(&r.mainState, &r.mainDepth)
	case "article":
		r.enter(&r.artState, &r.artDepth)
	case "li":
		r.setPrefix("- ")
	case "pre":
		r.preDepth++
		r.preLead = true
	case "tr":
		r.rowCells = 0
	case "td", "th":
		if r.rowCells > 0 {
			for _, w := range r.active {
				w.nl = 0
				w.sp = true
				w.word("|")
				w.sp = true
			}
			r.collSp = true
			r.collect("|")
		}
		r.rowCells++
	case "br":
		for _, w := range r.active {
			if w.nl < 2 {
				w.nl++
			}
		}
	case "img":
		if a := strings.Join(strings.Fields(html.UnescapeString(t.alt)), " "); a != "" {
			r.word("[image: " + a + "]")
		}
	case "a":
		r.endLink()
		r.startLink(t)
	}
}

func (r *reader) closeTag(name string) {
	if r.skipName != "" {
		if name == r.skipName {
			r.skipDepth--
			if r.skipDepth <= 0 {
				r.skipName, r.skipDepth = "", 0
			}
		}
		return
	}
	if b := blockBreak[name]; b > 0 {
		r.block(b)
	}
	if headingLevel(name) > 0 {
		r.setPrefix("")
		r.finishH1()
		return
	}
	switch name {
	case "main":
		r.leave(&r.mainState, &r.mainDepth)
	case "article":
		r.leave(&r.artState, &r.artDepth)
	case "li":
		r.setPrefix("")
	case "pre":
		if r.preDepth > 0 {
			r.preDepth--
		}
	case "a":
		r.endLink()
	}
}

// enter opens the first main or article, or counts one nested in it.
func (r *reader) enter(state, depth *int) {
	switch *state {
	case 0:
		*state, *depth = 1, 1
		r.updateActive()
		r.block(1)
	case 1:
		*depth++
	}
}

func (r *reader) leave(state, depth *int) {
	if *state != 1 {
		return
	}
	if *depth--; *depth == 0 {
		*state = 2
		r.updateActive()
		r.block(1)
	}
}

func (r *reader) finishH1() {
	if !r.h1On {
		return
	}
	r.h1On = false
	if t := strings.TrimSpace(string(r.h1Buf)); t != "" {
		r.h1Title, r.h1Done = t, true
	}
	r.h1Buf = r.h1Buf[:0]
}

func (r *reader) startLink(t tag) {
	if !t.hasHref {
		return
	}
	href := strings.TrimSpace(html.UnescapeString(t.href))
	if href == "" {
		return
	}
	u, err := url.Parse(href)
	if err != nil {
		return
	}
	if r.base != nil {
		u = r.base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return
	}
	u.Fragment, u.RawFragment = "", ""
	r.linkOpen = true
	r.linkURL = u.String()
	r.linkBuf = r.linkBuf[:0]
}

func (r *reader) endLink() {
	if !r.linkOpen {
		return
	}
	r.linkOpen = false
	text := strings.TrimSpace(clipRunes(string(r.linkBuf), maxLinkText))
	if i, ok := r.seen[r.linkURL]; ok {
		if r.links[i].Text == "" {
			r.links[i].Text = text
		}
		return
	}
	if len(r.links) < maxLinks {
		r.seen[r.linkURL] = len(r.links)
		r.links = append(r.links, Link{Text: text, URL: r.linkURL})
	}
}
