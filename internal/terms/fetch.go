package terms

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/version"
)

// MaxPage is the most of a page a fetch reads.
const MaxPage = 8 << 20

// MaxLinked is the most of a page a fetch reads when the page is only a
// link's, or a guess at where a site keeps its terms: terms are rarely a
// tenth of it.
const MaxLinked = 2 << 20

// Fetcher gets pages of terms over HTTP: the page a person asked for, and
// the links on it to the terms.
type Fetcher struct {
	// Local lets it fetch from this computer and the local network the
	// host of the address it is asked for, as a person typing the address
	// of an app of their own may want. Every other host, a redirect's
	// included, must be on the public internet, so a hostile page can't
	// make it knock on a router or a local app.
	Local bool
	// Timeout bounds each fetch; 0 = 20 s.
	Timeout time.Duration
	// Max is the most of a page it reads; 0 = MaxPage.
	Max int64
	// Client, when set, does the fetching instead, as tests do.
	Client *http.Client
}

// Page is a page fetched.
type Page struct {
	URL   string // where it ended up, after redirects
	Body  []byte
	HTML  bool // HTML, rather than plain text
	Title string
}

// ErrNotPage is a fetch of something that isn't a page of text, such as a
// PDF.
var ErrNotPage = errors.New("not a page of text")

// Get fetches a page.
func (f Fetcher) Get(ctx context.Context, address string) (Page, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Page{}, fmt.Errorf("%q is not a web address: give one that starts with https://", address)
	}
	timeout := f.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", Agent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "en;q=1, *;q=0.5")
	local := ""
	if f.Local {
		local = u.Hostname()
	}
	resp, err := f.client(local).Do(req)
	if err != nil {
		var refused *refusedError
		if errors.As(err, &refused) {
			return Page{}, refused
		}
		return Page{}, fmt.Errorf("fetching %s: %w", u.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Page{}, fmt.Errorf("%s answered %s", u.Host, resp.Status)
	}
	ct, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	isHTML := ct == "" || ct == "text/html" || ct == "application/xhtml+xml"
	if !isHTML && ct != "text/plain" && ct != "text/markdown" {
		if ct == "application/pdf" {
			return Page{}, fmt.Errorf("%w: %s is a PDF; copy its text into `%s terms -` instead", ErrNotPage, resp.Request.URL, brand.Slug)
		}
		return Page{}, fmt.Errorf("%w: %s is %s", ErrNotPage, resp.Request.URL, ct)
	}
	max := f.Max
	if max <= 0 {
		max = MaxPage
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return Page{}, fmt.Errorf("reading %s: %w", u.Host, err)
	}
	return Page{URL: resp.Request.URL.String(), Body: decode(body, params["charset"]), HTML: isHTML}, nil
}

// Agent is the User-Agent fetches send: who is asking, honestly.
func Agent() string {
	return brand.Slug + "-terms/" + version.String() + " (reads terms of service for a person; " + brand.Site + ")"
}

// client is the HTTP client of a fetch that may reach the host named
// local on this computer or its network; "" = none may.
func (f Fetcher) client(local string) *http.Client {
	if f.Client != nil {
		return f.Client
	}
	open := &net.Dialer{Timeout: 10 * time.Second}
	guarded := &net.Dialer{Timeout: 10 * time.Second, Control: public}
	t := &http.Transport{
		Proxy: nil,
		// The dial knows the host a connection is for, before it is
		// looked up: only the one asked for may be local.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if host, _, err := net.SplitHostPort(addr); err == nil && local != "" && strings.EqualFold(host, local) {
				return open.DialContext(ctx, network, addr)
			}
			return guarded.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: t,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 6 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("a redirect to %s, which isn't a web address", req.URL.Scheme)
			}
			return nil
		},
	}
}

// refusedError is a fetch of an address on this computer or its network.
type refusedError struct{ addr string }

func (e *refusedError) Error() string {
	return "refused to fetch " + e.addr + ": it is on this computer or its local network, which a page's links may not reach"
}

// nonPublic are address ranges no page's link may reach, besides what
// netip knows as private: shared address space (carrier-grade NAT, and
// Tailscale's), the IETF's, benchmarking, the documentation ranges, the
// reserved, IPv6's old site-local, 6to4 and Teredo, which carry an IPv4
// address inside, and the local-use NAT64 prefix.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fec0::/10"),
}

// nat64 is the well-known NAT64 prefix: its addresses carry a public IPv4
// address in their last 32 bits, which is checked as itself.
var nat64 = netip.MustParsePrefix("64:ff9b::/96")

// interfaceNets are this computer's own addresses and the networks they
// are on: global IPv6 addresses and their /64s too, which no fixed range
// covers. A test may stand in for it.
var interfaceNets = func() []netip.Prefix {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(n.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		ones, _ := n.Mask.Size()
		if ip.Is4() && ones > 32 {
			ones -= 96
		}
		if p, err := ip.Prefix(ones); err == nil {
			out = append(out, p)
		}
		out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
	}
	return out
}

// public refuses a connection to an address that isn't on the public
// internet, or is this computer's or its network's. It runs as each
// connection is made, redirects' too, so a name that resolves to a local
// address is refused as well. One way round it stays: a public address of
// this network's own router, which it may send back in (hairpin NAT).
func public(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return &refusedError{address}
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return &refusedError{address}
	}
	if !publicAddr(ip.WithZone("").Unmap()) {
		return &refusedError{address}
	}
	return nil
}

func publicAddr(ip netip.Addr) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if nat64.Contains(ip) {
		b := ip.As16()
		return publicAddr(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
	}
	for _, p := range nonPublic {
		if p.Contains(ip) {
			return false
		}
	}
	for _, p := range interfaceNets() {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// cp1252 are Windows-1252's characters from 0x80 to 0x9f, where it
// differs from Latin-1; 0 where it has none.
var cp1252 = [32]rune{
	0x20ac, 0, 0x201a, 0x0192, 0x201e, 0x2026, 0x2020, 0x2021, 0x02c6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017d, 0,
	0, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014, 0x02dc, 0x2122, 0x0161, 0x203a, 0x0153, 0, 0x017e, 0x0178,
}

// decode makes a page's bytes UTF-8, from the charset its header or its
// own meta tag names: Latin-1 and Windows-1252 are read as such, the rest
// as UTF-8.
func decode(b []byte, charset string) []byte {
	cs := strings.ToLower(charset)
	if cs == "" {
		head := strings.ToLower(string(b[:min(len(b), 2048)]))
		if i := strings.Index(head, "charset="); i >= 0 {
			cs = strings.Trim(head[i+8:min(len(head), i+30)], `"' `)
			if j := strings.IndexAny(cs, `"'; />`); j >= 0 {
				cs = cs[:j]
			}
		}
	}
	switch cs {
	case "iso-8859-1", "latin1", "latin-1", "windows-1252", "cp1252", "us-ascii", "ascii":
		if utf8.Valid(b) {
			return b
		}
		out := make([]byte, 0, len(b)+len(b)/8)
		for _, c := range b {
			r := rune(c)
			if c >= 0x80 && c <= 0x9f && cp1252[c-0x80] != 0 {
				r = cp1252[c-0x80]
			}
			out = utf8.AppendRune(out, r)
		}
		return out
	}
	return b
}
