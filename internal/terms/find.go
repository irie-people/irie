package terms

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/irie-people/irie/internal/htmltext"
)

// Link is a link on a page: its words and address.
type Link struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// linksOf are a page's links as the terms keep them: their words made
// plain, as every text the terms show is.
func linksOf(ls []htmltext.Link) []Link {
	out := make([]Link, 0, len(ls))
	for _, l := range ls {
		out = append(out, Link{Text: squash(normalise(l.Text)), URL: l.URL})
	}
	return out
}

// Found are the legal pages a page links to.
type Found struct {
	Terms   string `json:"terms,omitempty"`
	Privacy string `json:"privacy,omitempty"`
	// Others are the links that may be terms, best first, Terms among
	// them: what to offer when the best isn't.
	Others []Link `json:"others,omitempty"`
}

var (
	termsText    = re(`^(?:the |our |site |website |service |user |customer |general |legal )?(?:terms(?: of (?:service|use|sale|business))?|terms (?:&|and) conditions|conditions(?: of (?:use|sale))?|user agreement|terms & privacy|legal(?: terms| notices?| information)?|tos|eula|end user licen[cs]e agreement|service agreement|subscriber agreement|website terms)(?: (?:&|and) (?:conditions|policies|privacy))?$`)
	termsSome    = re(`\b(?:terms|conditions|user agreement|eula|legal)\b`)
	termsPath    = re(`/(?:terms|tos|legal|conditions|user-?agreement|eula|terms-?of-?(?:service|use)|terms-?and-?conditions|policies)(?:[/._?#-]|$)`)
	privacyPath  = re(`/privacy|privacy-?policy|data-?policy`)
	notTermsText = re(`\b(?:privacy|cookie|cookies|accessibility|careers|jobs|press|contact|help|support|blog|login|log in|sign in|sign up|pricing|developers?|api)\b`)
)

// Find picks a page's links to its terms and its privacy policy: by their
// words, then their address, on the page's own site.
func Find(page string, links []Link) Found {
	type cand struct {
		l     Link
		score int
	}
	var terms, privacy []cand
	for _, l := range links {
		words := squash(strings.Trim(normalise(l.Text), " |·•›»"))
		low := strings.ToLower(words)
		u, err := url.Parse(l.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		path := strings.ToLower(u.Path)
		same := page == "" || sameSite(l.URL, page)
		bonus := 0
		if same {
			bonus = 3
		}
		if privacyLink.MatchString(low) || (privacyPath.MatchString(path) && strings.Contains(low, "privacy")) {
			privacy = append(privacy, cand{l, 10 + bonus})
			continue
		}
		if low != "" && notTermsText.MatchString(low) && !termsSome.MatchString(low) {
			// "Help" or "Careers", whatever its address.
			continue
		}
		s := 0
		switch {
		case termsText.MatchString(low):
			s = 10
		case termsSome.MatchString(low) && !notTermsText.MatchString(low) && len(low) <= 60:
			s = 6
		}
		if termsPath.MatchString(path) && !privacyPath.MatchString(path) {
			// An address alone says less than words: a link whose words
			// say nothing of terms isn't taken for them, only offered.
			if s == 0 {
				s = 3
			} else {
				s += 4
			}
		}
		if s == 0 {
			continue
		}
		if !same {
			s -= 3
		}
		terms = append(terms, cand{l, s + bonus})
	}
	best := func(cs []cand) []cand {
		slices.SortStableFunc(cs, func(a, b cand) int { return b.score - a.score })
		return cs
	}
	var f Found
	t := best(terms)
	if len(t) > 0 && t[0].score >= 7 {
		f.Terms = t[0].l.URL
	}
	for _, c := range t {
		if len(f.Others) < 6 && !slices.ContainsFunc(f.Others, func(l Link) bool { return l.URL == c.l.URL }) {
			f.Others = append(f.Others, c.l)
		}
	}
	if p := best(privacy); len(p) > 0 {
		f.Privacy = p[0].l.URL
	}
	return f
}

// wellKnown are the paths where sites keep their terms, tried when a page
// links to none.
var wellKnown = []string{"/terms", "/tos", "/terms-of-service", "/terms-of-use", "/legal/terms", "/terms-and-conditions", "/legal", "/policies/terms", "/user-agreement"}

// Locate reads the terms for a page: the page itself when it is terms or a
// privacy policy, else the terms its links point to, else those at the
// places sites usually keep them. page may be nil: it is fetched. Want
// picks the privacy policy over the terms, when the site has both.
func Locate(ctx context.Context, f Fetcher, address string, page *Page, want string) (*Doc, error) {
	if page == nil {
		p, err := f.Get(ctx, address)
		if err != nil {
			return nil, err
		}
		page = &p
	}
	if page.URL == "" {
		page.URL = address
	}
	here := docOf(*page)
	legal := here.Kind != KindOther && len(here.Points) > 0
	if legal && (want == "" || here.Kind == want) {
		return done(here), nil
	}
	found := Find(page.URL, here.links)
	next := found.Terms
	switch {
	case want == KindPrivacy:
		// Only a privacy policy answers for one: never the terms again.
		next = found.Privacy
	case here.Kind == KindTerms && next == "":
		next = found.Privacy
	}
	if next != "" && next != page.URL {
		if p, err := linked(f, address, next).Get(ctx, next); err == nil {
			d := docOf(p)
			if d.Kind != KindOther {
				if d.Privacy == "" {
					d.Privacy = found.Privacy
				}
				return done(d), nil
			}
		}
	}
	if here.Kind != KindOther {
		return done(here), nil
	}
	if want != KindPrivacy {
		if d := guess(ctx, linked(f, address, page.URL), page.URL); d != nil {
			return done(d), nil
		}
	}
	return nil, &NotFoundError{Page: page.URL, Title: here.Title, Others: found.Others}
}

// done lets a document go of its page's links, which only finding the
// terms needs.
func done(d *Doc) *Doc {
	d.links = nil
	return d
}

// linked is the fetcher for a link of the page at address: smaller pages,
// and this computer or its network only on the host the person asked for,
// never at a page's word.
func linked(f Fetcher, address, link string) Fetcher {
	f.Max = MaxLinked
	a, err1 := url.Parse(address)
	b, err2 := url.Parse(link)
	f.Local = f.Local && err1 == nil && err2 == nil && strings.EqualFold(a.Hostname(), b.Hostname())
	return f
}

// NotFoundError says a page has no terms that Irie could find, with the
// links that may be them.
type NotFoundError struct {
	Page, Title string
	Others      []Link
}

func (e *NotFoundError) Error() string {
	return "no terms of service found on " + e.Page + ": open the site's terms and read that page, or give their address"
}

// IsNotFound says whether err is a NotFoundError.
func IsNotFound(err error) bool {
	var e *NotFoundError
	return errors.As(err, &e)
}

// guess tries the places sites usually keep their terms, three at a time
// in wellKnown's order, and takes the first that reads as terms.
func guess(ctx context.Context, f Fetcher, page string) *Doc {
	u, err := url.Parse(page)
	if err != nil {
		return nil
	}
	for i := 0; i < len(wellKnown); i += 3 {
		batch := wellKnown[i:min(i+3, len(wellKnown))]
		docs := make([]*Doc, len(batch))
		var wg sync.WaitGroup
		for j, p := range batch {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// A page that breaks the reading is no terms, and
				// never takes the rest down with it.
				defer func() { _ = recover() }()
				x := url.URL{Scheme: u.Scheme, Host: u.Host, Path: p}
				pg, err := f.Get(ctx, x.String())
				if err != nil {
					return
				}
				if d := docOf(pg); d.Kind == KindTerms && len(d.Points) > 0 {
					docs[j] = d
				}
			}()
		}
		wg.Wait()
		for _, d := range docs {
			if d != nil {
				return d
			}
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	return nil
}

func docOf(p Page) *Doc {
	if p.HTML {
		return FromHTML(p.URL, p.Body)
	}
	return FromText(p.URL, p.Title, string(p.Body))
}
