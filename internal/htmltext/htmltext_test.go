package htmltext

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func mustURL(t testing.TB, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

type readCase struct {
	name  string
	html  string
	base  string // empty means a nil base
	title string
	text  string
	links []Link
}

var readCases = []readCase{
	{
		name:  "simple page",
		html:  "<html><head><title>Hello  World</title></head><body><p>First <b>bold</b> line.</p><p>Second.</p></body></html>",
		title: "Hello World",
		text:  "First bold line.\n\nSecond.",
	},
	{
		name: "script holding tags",
		html: `<div>before</div><script>var s = "</div><p>x";
if (a < b) { document.write("<p>y</p>"); }</script><div>after</div>`,
		text: "before\nafter",
	},
	{
		name: "script end tag in other case",
		html: "<p>a</p><SCRIPT>x = '<p>no</p>'</ScRiPt ><p>b</p>",
		text: "a\n\nb",
	},
	{
		name: "style",
		html: "<style>p > a { color: red } </style><p>text</p>",
		text: "text",
	},
	{
		name: "comments hold tags",
		html: "<p>a<!-- <p>hidden</p> --> b</p><!-- unterminated <p>gone</p>",
		text: "a b",
	},
	{
		name: "cdata doctype and processing instruction",
		html: "<!DOCTYPE html><?xml version=\"1.0\"?><p>a<![CDATA[ <b>x</b> ]]>b</p>",
		text: "ab",
	},
	{
		name: "entities",
		html: "<p>fish &amp; chips &lt;b&gt; it&#39;s it&#x27;s&nbsp;here &unknown; &copy;</p>",
		text: "fish & chips <b> it's it's here &unknown; ©",
	},
	{
		name: "nested lists",
		html: "<ul><li>one</li><li>two<ul><li>two a</li><li>two b</li></ul></li><li>three</li></ul>",
		text: "- one\n- two\n- two a\n- two b\n- three",
	},
	{
		name:  "headings",
		html:  "<h1>Big <i>title</i></h1><p>text</p><h3>Small</h3><h2></h2>tail",
		title: "Big title",
		text:  "# Big title\n\ntext\n\n### Small\n\ntail",
	},
	{
		name: "table",
		html: "<table><tr><th>Name</th><th>Price</th></tr><tr><td>Tea</td><td>3</td></tr><tr><td>Cake</td><td>5</td></tr></table>",
		text: "Name | Price\nTea | 3\nCake | 5",
	},
	{
		name: "pre keeps spacing",
		html: "<p>code:</p><pre>\nfunc main() {\n    x  :=  1\n}\n</pre><p>done</p>",
		text: "code:\n\nfunc main() {\n    x  :=  1\n}\n\ndone",
	},
	{
		name:  "images",
		html:  `<p>a <img src="x.png" alt="A  red   fox"> b <img src="y.png" alt=""><img src="z.png"></p>`,
		text:  "a [image: A red fox] b",
		title: "",
	},
	{
		name:  "relative links resolved and dropped",
		html:  `<a href="/a">A</a> <a href="b?x=1&amp;y=2#frag">B</a> <a href="javascript:alert(1)">J</a> <a href="mailto:alice@example.com">M</a> <a href="#top">T</a> <a href="https://other.example.com/p#q">  O   <b>th</b>er </a><a name="x">no href</a>`,
		base:  "https://example.com/dir/page",
		text:  "A B J M T O ther no href",
		links: []Link{{"A", "https://example.com/a"}, {"B", "https://example.com/dir/b?x=1&y=2"}, {"T", "https://example.com/dir/page"}, {"O ther", "https://other.example.com/p"}},
	},
	{
		name: "duplicate links merged",
		html: `<a href="/x"></a><a href="/y">Y</a><a href="/x#one">First text</a><a href="/x">Later</a>`,
		base: "https://example.com/",
		text: "YFirst textLater",
		links: []Link{
			{"First text", "https://example.com/x"},
			{"Y", "https://example.com/y"},
		},
	},
	{
		name:  "nil base keeps absolute links only",
		html:  `<a href="/rel">R</a><a href="http://example.com/a">A</a><a href="HTTPS://Example.com/b">B</a><a href="ftp://example.com/c">F</a>`,
		text:  "RABF",
		links: []Link{{"A", "http://example.com/a"}, {"B", "https://Example.com/b"}},
	},
	{
		name:  "main first",
		html:  "<nav>Menu</nav><main><h1>Story</h1><div><div>deep</div>still main</div></main><footer>Foot</footer></main>",
		text:  "# Story\n\ndeep\nstill main\n\n---\n\nMenu\nFoot",
		title: "Story",
	},
	{
		name: "main alone",
		html: "<main><p>only</p></main>",
		text: "only",
	},
	{
		name: "article fallback",
		html: "<div>top</div><article><p>one</p></article><article><p>two</p></article><div>bottom</div>",
		text: "one\n\n---\n\ntop\n\ntwo\n\nbottom",
	},
	{
		name: "main wins over article",
		html: "<article>a</article><main>m</main>",
		text: "m\n\n---\n\na",
	},
	{
		name: "unclosed main runs to the end",
		html: "<div>head</div><main><p>body<div>more",
		text: "body\nmore\n\n---\n\nhead",
	},
	{
		name: "stray end main ignored",
		html: "<p>a</p></main><p>b</p>",
		text: "a\n\nb",
	},
	{
		name: "unclosed tags",
		html: "<div><p>one<b>two<i>three<ul><li>four<li>five",
		text: "onetwothree\n- four\n- five",
	},
	{
		name:  "attribute value holding >",
		html:  `<p title="a > b" data-x='c > d'>shown</p><a href="/x?a>b" title="t>">link</a>`,
		base:  "https://example.com/",
		text:  "shown\n\nlink",
		links: []Link{{"link", "https://example.com/x?a>b"}},
	},
	{
		name: "unquoted attributes and self closing",
		html: `<p class=a id=b>x<br/>y<br>z<img alt=pic src=a.png/></p>`,
		text: "x\ny\nz[image: pic]",
	},
	{
		name:  "uppercase tags",
		html:  `<HTML><HEAD><TITLE>Up</TITLE></HEAD><BODY><H2>Head</H2><P CLASS="x">Para</P><A HREF="HTTP://example.com/u">Link</A></BODY></HTML>`,
		title: "Up",
		text:  "## Head\n\nPara\n\nLink",
		links: []Link{{"Link", "http://example.com/u"}},
	},
	{
		name: "less than is text",
		html: "<p>a < b and c <3 d, 1<2</p>",
		text: "a < b and c <3 d, 1<2",
	},
	{
		name: "invalid utf-8",
		html: "<p>ok \xff\xfe bad \xc3 end</p>",
		text: "ok � bad � end",
	},
	{
		name: "hidden parts skipped",
		html: "<p>a</p><svg><text>s</text></svg><template><p>t</p></template><select><option>o</option></select><noscript>n</noscript><iframe>i</iframe><video>v<source></video><p>b</p>",
		text: "a\n\nb",
	},
	{
		name: "textarea is raw text",
		html: "<textarea><b>not bold</b></textarea><p>x</p>",
		text: "<b>not bold</b>\n\nx",
	},
	{
		name: "blank lines limited",
		html: "<p>a</p><p></p><p></p><br><br><br><p>b</p>",
		text: "a\n\nb",
	},
	{
		name:  "title with entities, first h1 not used",
		html:  "<title> A &amp;\n B </title><h1>Other</h1>",
		title: "A & B",
		text:  "# Other",
	},
	{
		name:  "link text cut",
		html:  `<a href="http://example.com/">` + strings.Repeat("é", 150) + `</a>`,
		text:  strings.Repeat("é", 150),
		links: []Link{{strings.Repeat("é", 100), "http://example.com/"}},
	},
	{
		name: "empty page",
		html: "",
		text: "",
	},
}

func TestRead(t *testing.T) {
	for _, tc := range readCases {
		t.Run(tc.name, func(t *testing.T) {
			var base *url.URL
			if tc.base != "" {
				base = mustURL(t, tc.base)
			}
			got := Read([]byte(tc.html), base)
			if got.Title != tc.title {
				t.Errorf("title = %q, want %q", got.Title, tc.title)
			}
			if got.Text != tc.text {
				t.Errorf("text = %q, want %q", got.Text, tc.text)
			}
			if !reflect.DeepEqual(got.Links, tc.links) {
				t.Errorf("links = %q, want %q", got.Links, tc.links)
			}
		})
	}
}

func TestLinkLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		b.WriteString(`<a href="http://example.com/` + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `">l</a>`)
	}
	got := Read([]byte(b.String()), nil)
	if len(got.Links) != 300 {
		t.Fatalf("got %d links, want 300", len(got.Links))
	}
}

func TestHostileInputIsFast(t *testing.T) {
	const size = 2 << 20
	inputs := map[string]string{
		"open tags without end":  strings.Repeat("<a ", size/3),
		"less than signs":        strings.Repeat("<", size),
		"nested divs":            strings.Repeat("<div>", 100000),
		"nested pre and main":    strings.Repeat("<main><pre><article>", 50000),
		"unterminated comments":  strings.Repeat("<!--", size/4),
		"script starts":          strings.Repeat("<script>", size/8),
		"end tags in script":     "<script>" + strings.Repeat("</", size/2),
		"unterminated quotes":    strings.Repeat(`<a href="`, size/9),
		"links":                  strings.Repeat(`<a href="/x">y</a>`, size/18),
		"ampersands":             strings.Repeat("&#x", size/3),
		"nested skipped":         strings.Repeat("<svg>", 100000),
		"many small elements":    strings.Repeat("<td>a</td><li>b<br>", size/19),
		"raw text end lookalike": "<style>" + strings.Repeat("</styl ", size/7),
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			got := Read([]byte(in), mustURL(t, "https://example.com/"))
			if d := time.Since(start); d > 2*time.Second {
				t.Errorf("took %v", d)
			}
			if !utf8.ValidString(got.Text) {
				t.Error("text is not valid UTF-8")
			}
		})
	}
}

func FuzzRead(f *testing.F) {
	for _, tc := range readCases {
		f.Add(tc.html)
	}
	f.Add("<a href='")
	f.Add("<!-")
	f.Add("</")
	f.Add("<script")
	f.Add("<p \x00 a=\"\xff\">")
	base, _ := url.Parse("https://example.com/dir/")
	f.Fuzz(func(t *testing.T, in string) {
		got := Read([]byte(in), base)
		if !utf8.ValidString(got.Title) || !utf8.ValidString(got.Text) {
			t.Fatalf("invalid UTF-8 in %q", in)
		}
		if len(got.Links) > 300 {
			t.Fatalf("%d links", len(got.Links))
		}
		for _, l := range got.Links {
			if !utf8.ValidString(l.Text) || !utf8.ValidString(l.URL) || utf8.RuneCountInString(l.Text) > 100 {
				t.Fatalf("bad link %q", l)
			}
		}
		if got.Text != strings.Trim(got.Text, "\n") {
			t.Fatalf("text has outer blank lines: %q", got.Text)
		}
	})
}
