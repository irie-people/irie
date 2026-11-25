package schools

import (
	"context"
	"encoding/json"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// headless finds a headless Chromium to run the page in: IRIE_TEST_CHROME,
// or Playwright's headless_shell where Playwright keeps it; "" = none.
func headless() string {
	if c := os.Getenv("IRIE_TEST_CHROME"); c != "" {
		return c
	}
	home, _ := os.UserHomeDir()
	for _, pat := range []string{
		"Library/Caches/ms-playwright/chromium_headless_shell-*/chrome-mac*/headless_shell",
		".cache/ms-playwright/chromium_headless_shell-*/chrome-linux*/headless_shell",
	} {
		if m, _ := filepath.Glob(filepath.Join(home, pat)); len(m) > 0 {
			slices.Sort(m)
			return m[len(m)-1]
		}
	}
	return ""
}

// TestThePageLoadsNothing: the page is one file whose policy lets it load
// nothing, and nothing in it asks to; its links out are to the sources,
// which open only when clicked.
func TestThePageLoadsNothing(t *testing.T) {
	b, err := Page()
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	if !strings.Contains(page, `content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:"`) {
		t.Error("the page's Content Security Policy isn't there")
	}
	for _, bad := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)<(script|img|iframe|link|source|audio|video)[^>]+(src|href)=["']?https?:`),
		regexp.MustCompile(`(?i)url\(\s*["']?https?:`),
		regexp.MustCompile(`(?i)@import`),
		regexp.MustCompile(`(?i)\bfetch\(|XMLHttpRequest|WebSocket|sendBeacon`),
	} {
		if m := bad.FindString(page); m != "" {
			t.Errorf("the page loads from elsewhere: %q", m)
		}
	}
	if strings.Count(page, "</script>") != 2 {
		t.Errorf("%d script elements end; want the data's and the script's", strings.Count(page, "</script>"))
	}
}

// TestThePageRuns opens the page in a headless Chromium, with no window,
// at a desktop's size and a phone's, and drives it from inside as a person
// would (testdata/selftest.js): the first pie and its table, a row and a
// slice opening into pies, a state's lands, Esc, a slice with no parts,
// Back and Forward, a text with its passage lit, the keys and the rows,
// with no error thrown.
func TestThePageRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("opens a browser")
	}
	bin := headless()
	if bin == "" {
		t.Skip("no headless Chromium here: set IRIE_TEST_CHROME to one")
	}
	b, err := Page()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile("testdata/selftest-before.js")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile("testdata/selftest.js")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(string(b), `<script type="application/json"`, "<script>"+string(before)+"</script>\n"+`<script type="application/json"`, 1)
	page = strings.Replace(page, "</body>", "<script>"+string(after)+"</script>\n</body>", 1)
	dir := t.TempDir()
	file := filepath.Join(dir, "pies.html")
	if err := os.WriteFile(file, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, size := range []string{"1280,800", "390,844"} {
		t.Run(size, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, "--headless", "--no-first-run", "--disable-gpu", "--hide-scrollbars",
				"--user-data-dir="+filepath.Join(dir, "profile-"+size), "--window-size="+size,
				"--virtual-time-budget=20000", "--dump-dom", "file://"+file)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s: %v", bin, err)
			}
			dom := string(out)
			i := strings.LastIndex(dom, `<pre id="selftest">`)
			if i < 0 {
				t.Fatalf("the self-test wrote nothing; the page ends %q", dom[max(0, len(dom)-400):])
			}
			raw := dom[i+len(`<pre id="selftest">`):]
			raw = html.UnescapeString(raw[:strings.Index(raw, "</pre>")])
			var res struct {
				Checks []struct {
					Name string
					OK   bool
					Got  string
				}
				Errors []string
			}
			if err := json.Unmarshal([]byte(raw), &res); err != nil {
				t.Fatalf("%v: %s", err, raw)
			}
			for _, e := range res.Errors {
				t.Errorf("the page threw: %s", e)
			}
			if len(res.Checks) < 20 {
				t.Errorf("only %d checks ran", len(res.Checks))
			}
			for _, c := range res.Checks {
				if !c.OK {
					t.Errorf("%s: got %s", c.Name, c.Got)
				}
			}
		})
	}
}

// TestTheSiteHasThePage: the site's copy of the page is this build's, so
// a change to a text, a link, a figure or the page shows on the site with
// it. go run ./tools/schools writes it.
func TestTheSiteHasThePage(t *testing.T) {
	path := filepath.Join("..", "..", "site", "schools", "index.html")
	got, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if _, err := os.Stat(filepath.Join("..", "..", "site")); os.IsNotExist(err) {
			t.Skip("no site in this checkout")
		}
	}
	if err != nil {
		t.Fatalf("%v: go run ./tools/schools writes it", err)
	}
	want, err := WebPage(SiteHome)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is stale: go run ./tools/schools writes it again", path)
	}
}
