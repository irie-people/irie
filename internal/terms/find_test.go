package terms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
)

// TestFindPicksTheTermsLink: the terms by their words, then their address,
// on the page's own site; the privacy policy apart; a link elsewhere
// counts less.
func TestFindPicksTheTermsLink(t *testing.T) {
	links := []Link{
		{Text: "Pricing", URL: "https://example.com/pricing"},
		{Text: "Their terms", URL: "https://other.example.org/terms"},
		{Text: "Privacy Policy", URL: "https://example.com/privacy"},
		{Text: "Terms of Service", URL: "https://example.com/legal/terms"},
		{Text: "Cookie settings", URL: "https://example.com/cookies"},
		{Text: "Legal", URL: "https://example.com/legal"},
	}
	f := Find("https://www.example.com/", links)
	if f.Terms != "https://example.com/legal/terms" || f.Privacy != "https://example.com/privacy" {
		t.Errorf("found %+v", f)
	}
	if len(f.Others) < 2 || f.Others[0].URL != f.Terms || f.Others[1].URL != "https://example.com/legal" {
		t.Errorf("others %+v", f.Others)
	}
	if f := Find("https://example.com/", []Link{{Text: "Blog", URL: "https://example.com/blog"}}); f.Terms != "" {
		t.Errorf("a page without terms found %q", f.Terms)
	}
	if f := Find("https://example.com/", []Link{{Text: "Help", URL: "https://example.com/terms-of-use"}}); f.Terms != "" || len(f.Others) != 0 {
		t.Errorf("a help link's address found %+v", f)
	}
	if f := Find("https://example.com/", []Link{{Text: "", URL: "https://example.com/terms-of-use"}}); f.Terms != "" || len(f.Others) != 1 {
		t.Errorf("a link of no words found %+v", f)
	}
}

// site serves a home page linking to its terms, the fixture's page at
// /legal/terms, and pages that aren't terms.
func site(t *testing.T) *httptest.Server {
	t.Helper()
	terms, err := os.ReadFile("testdata/terms.html")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<html><head><title>Example</title></head><body><h1>Welcome</h1><p>We make things.</p><footer><a href="/legal/terms">Terms of Service</a> <a href="/privacy">Privacy</a></footer></body></html>`)
	})
	mux.HandleFunc("/legal/terms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(terms)
	})
	mux.HandleFunc("/paper.pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.7"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestLocateFollowsThePagesLinks: an address of any page of a site reads
// its terms, through the page's link; the terms' own page reads as it is;
// a page with no terms says so with the links that may be them; a PDF is
// refused with what to do.
func TestLocateFollowsThePagesLinks(t *testing.T) {
	srv := site(t)
	f := Fetcher{Client: srv.Client()}
	ctx := context.Background()
	d, err := Locate(ctx, f, srv.URL+"/", nil, "")
	if err != nil || d.URL != srv.URL+"/legal/terms" || d.Kind != KindTerms || len(d.Points) < 10 {
		t.Fatalf("Locate home: %v, %+v", err, d)
	}
	if d.Privacy != srv.URL+"/privacy" {
		t.Errorf("privacy %q", d.Privacy)
	}
	if d, err := Locate(ctx, f, srv.URL+"/legal/terms", nil, ""); err != nil || d.URL != srv.URL+"/legal/terms" {
		t.Errorf("Locate the terms: %v, %+v", err, d)
	}
	// A site with none: its links that may be terms are offered.
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<html><body><p>Nothing here.</p><a href="/maybe">Legal stuff</a></body></html>`)
	}))
	defer bare.Close()
	_, err = Locate(ctx, Fetcher{Client: bare.Client()}, bare.URL+"/", nil, "")
	var nf *NotFoundError
	if !errors.As(err, &nf) || len(nf.Others) != 1 || nf.Others[0].Text != "Legal stuff" || !IsNotFound(err) {
		t.Errorf("no terms: %v, %+v", err, nf)
	}
	if _, err := f.Get(ctx, srv.URL+"/paper.pdf"); !errors.Is(err, ErrNotPage) || !strings.Contains(err.Error(), "PDF") {
		t.Errorf("a PDF: %v", err)
	}
	if _, err := f.Get(ctx, "ftp://example.com/terms"); err == nil {
		t.Error("fetched an ftp address")
	}
	// A page the browser read is read as it is, without a fetch.
	page := &Page{URL: "https://example.com/legal/terms", HTML: true, Body: mustRead(t, "testdata/terms.html")}
	if d, err := Locate(ctx, Fetcher{Client: &http.Client{Transport: refuseAll{}}}, page.URL, page, ""); err != nil || len(d.Points) < 10 {
		t.Errorf("a page given: %v", err)
	}
}

type refuseAll struct{}

func (refuseAll) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("no fetch expected")
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestAPagesLinksNeverReachThisComputer: the real fetcher refuses an
// address on this computer, as a hostile page's link would name, unless
// the person asked for it (Local).
func TestAPagesLinksNeverReachThisComputer(t *testing.T) {
	srv := site(t)
	ctx := context.Background()
	_, err := Fetcher{}.Get(ctx, srv.URL+"/legal/terms")
	var refused *refusedError
	if !errors.As(err, &refused) {
		t.Fatalf("fetched %s: %v", srv.URL, err)
	}
	if _, err := (Fetcher{Local: true}).Get(ctx, srv.URL+"/legal/terms"); err != nil {
		t.Errorf("Local: %v", err)
	}
	// This computer's own networks, as its interfaces have them: a global
	// IPv6 /64 that no fixed range covers.
	old := interfaceNets
	interfaceNets = func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("2a01:4f8:1:2::/64")} }
	t.Cleanup(func() { interfaceNets = old })
	for _, addr := range []string{
		"10.0.0.1:80", "192.168.1.1:443", "100.100.1.1:80", "[fd00::1]:80", "169.254.1.1:80", "[::1]:80", "0.0.0.0:80",
		"192.0.2.1:80", "198.51.100.7:80", "203.0.113.9:80", "[2001:db8::1]:80", "[fec0::1]:80", "[2002:c0a8:101::1]:80",
		"[64:ff9b::a00:1]:80", "[::ffff:192.168.1.1]:80", "[fe80::1%en0]:80", "[2a01:4f8:1:2::abcd]:8080",
	} {
		if public("tcp", addr, nil) == nil {
			t.Errorf("%s counts as public", addr)
		}
	}
	for _, addr := range []string{"93.184.215.14:443", "[2606:2800:21f:cb07:6820:80da:af6b:8b2c]:443", "[64:ff9b::5db8:d70e]:443"} {
		if err := public("tcp", addr, nil); err != nil {
			t.Errorf("a public address refused: %v", err)
		}
	}
}

// TestLocalIsOnlyTheHostAskedFor: an address typed on this computer is
// fetched, but its redirect to another local name isn't, nor a link of
// its page to another local host.
func TestLocalIsOnlyTheHostAskedFor(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			// Another name for this computer than the one asked for.
			http.Redirect(w, r, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)+"/terms", http.StatusFound)
		default:
			fmt.Fprint(w, `<html><body><p>Our terms.</p></body></html>`)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	f := Fetcher{Local: true}
	if _, err := f.Get(ctx, srv.URL+"/terms"); err != nil {
		t.Errorf("the address asked for: %v", err)
	}
	var refused *refusedError
	if _, err := f.Get(ctx, srv.URL+"/away"); !errors.As(err, &refused) {
		t.Errorf("a redirect to another local name: %v", err)
	}
	if g := linked(f, srv.URL+"/", "http://localhost:1/terms"); g.Local || g.Max != MaxLinked {
		t.Errorf("a link to another host: %+v", g)
	}
	if g := linked(f, srv.URL+"/", srv.URL+"/terms"); !g.Local {
		t.Errorf("a link on the host asked for: %+v", g)
	}
}

// TestAPrivacyRequestNeverGetsTheTermsAgain: a privacy policy is asked for
// from terms that link to none: the terms come back, not their terms link
// again and again.
func TestAPrivacyRequestNeverGetsTheTermsAgain(t *testing.T) {
	page := &Page{URL: "https://example.com/legal/terms", HTML: true, Body: mustRead(t, "testdata/terms.html")}
	page.Body = []byte(strings.Replace(string(page.Body), `<a href="https://example.com/privacy">Privacy Policy</a>`, "", 1))
	d, err := Locate(context.Background(), Fetcher{Client: &http.Client{Transport: refuseAll{}}}, page.URL, page, KindPrivacy)
	if err != nil || d.URL != page.URL || d.links != nil {
		t.Errorf("privacy from terms without a link to it: %v, %+v", err, d)
	}
}

// TestDecodeReadsLatin1: a page in Latin-1 or Windows-1252 reads as UTF-8;
// a UTF-8 page stays as it is.
func TestDecodeReadsLatin1(t *testing.T) {
	if got := string(decode([]byte("caf\xe9 \x93quoted\x94"), "windows-1252")); got != "café “quoted”" {
		t.Errorf("windows-1252: %q", got)
	}
	if got := string(decode([]byte("<meta charset=\"iso-8859-1\"><p>na\xefve</p>"), "")); !strings.Contains(got, "naïve") {
		t.Errorf("meta charset: %q", got)
	}
	if got := string(decode([]byte("already ü"), "latin1")); got != "already ü" {
		t.Errorf("UTF-8 kept: %q", got)
	}
}
