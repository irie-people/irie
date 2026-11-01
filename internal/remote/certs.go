package remote

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/filetx"
)

// The phone trusts Irie's own certificate authority, which it installs
// once as a profile (docs/PHONE.md). The authority may only vouch for
// names and addresses of a local network, so its key, should it ever leak,
// can't stand in for any site on the internet: its certificate carries
// name constraints (RFC 5280 §4.2.1.10), which iOS checks.
var (
	// localDomains are the DNS names it may vouch for: Bonjour's .local
	// (RFC 6762), the home network's home.arpa (RFC 8375) and localhost.
	localDomains = []string{"local", "home.arpa", "localhost"}
	// localNets are the addresses it may vouch for: the private ranges
	// (RFC 1918, RFC 4193), shared address space, which mesh VPNs use
	// (RFC 6598), link-local and loopback.
	localNets = mustNets("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "169.254.0.0/16", "127.0.0.0/8",
		"fc00::/7", "fe80::/10", "::1/128")
)

func mustNets(cidrs ...string) []*net.IPNet {
	var out []*net.IPNet
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

// localIP says whether the authority may vouch for ip.
func localIP(ip net.IP) bool {
	return slices.ContainsFunc(localNets, func(n *net.IPNet) bool { return n.Contains(ip) })
}

// localName says whether the authority may vouch for the DNS name.
func localName(name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	return slices.ContainsFunc(localDomains, func(d string) bool { return name == d || strings.HasSuffix(name, "."+d) })
}

const (
	caFile    = "ca.pem"
	caKeyFile = "ca-key.pem"
	// caLife is the authority's life. iOS bounds a server certificate's
	// life (825 days), not an authority's.
	caLife = 10 * 365 * 24 * time.Hour
	// leafLife is a server certificate's: under the 398 days that browsers
	// allow, and renewed long before it ends.
	leafLife  = 397 * 24 * time.Hour
	leafRenew = 30 * 24 * time.Hour
)

// Authority is Irie's certificate authority for the phone app. Several
// machines may share one, so a phone installs it once for them all.
type Authority struct {
	Cert *x509.Certificate
	Key  crypto.Signer
	PEM  []byte // the certificate, PEM-encoded
}

// LoadAuthority reads the authority from dir, making a new one when there
// is none.
func LoadAuthority(dir string) (*Authority, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := filetx.Lock(filepath.Join(dir, caFile), 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	a, err := readAuthority(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if a, err = newAuthority(time.Now()); err == nil {
			err = a.write(dir)
		}
	}
	return a, err
}

func readAuthority(dir string) (*Authority, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, caFile))
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, caKeyFile))
	if err != nil {
		return nil, err
	}
	return ParseAuthority(append(certPEM, keyPEM...))
}

// ParseAuthority reads an authority from PEM: its certificate and its
// private key, as `irie remote ca export` writes them.
func ParseAuthority(data []byte) (*Authority, error) {
	a := &Authority{}
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			break
		}
		switch b.Type {
		case "CERTIFICATE":
			c, err := x509.ParseCertificate(b.Bytes)
			if err != nil {
				return nil, fmt.Errorf("the authority's certificate: %w", err)
			}
			a.Cert, a.PEM = c, pem.EncodeToMemory(b)
		case "PRIVATE KEY":
			k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
			if err != nil {
				return nil, fmt.Errorf("the authority's key: %w", err)
			}
			s, ok := k.(crypto.Signer)
			if !ok {
				return nil, errors.New("the authority's key can't sign")
			}
			a.Key = s
		}
	}
	switch {
	case a.Cert == nil || a.Key == nil:
		return nil, errors.New("an authority needs its certificate and its private key, both PEM")
	case !a.Cert.IsCA:
		return nil, errors.New("that certificate is not an authority's")
	case !publicKeysEqual(a.Cert.PublicKey, a.Key.Public()):
		return nil, errors.New("the key is not the certificate's")
	}
	return a, nil
}

func publicKeysEqual(a, b crypto.PublicKey) bool {
	x, ok := a.(interface{ Equal(crypto.PublicKey) bool })
	return ok && x.Equal(b)
}

func newAuthority(now time.Time) (*Authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	var permitted []*net.IPNet
	for _, n := range localNets {
		// The constraint's mask must be as long as its address.
		ip := n.IP.To4()
		if ip == nil {
			ip = n.IP.To16()
		}
		mask := n.Mask
		if len(mask) != len(ip) {
			ones, _ := mask.Size()
			mask = net.CIDRMask(ones, len(ip)*8)
		}
		permitted = append(permitted, &net.IPNet{IP: ip, Mask: mask})
	}
	tmpl := &x509.Certificate{
		SerialNumber:                serial(),
		Subject:                     pkix.Name{CommonName: "Irie local CA " + now.UTC().Format("2006-01-02"), Organization: []string{"Irie"}},
		NotBefore:                   now.Add(-time.Hour),
		NotAfter:                    now.Add(caLife),
		IsCA:                        true,
		BasicConstraintsValid:       true,
		MaxPathLenZero:              true,
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:                 []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         localDomains,
		PermittedIPRanges:           permitted,
		// No e-mail address or URI can be under .invalid (RFC 6761), so
		// it vouches for none: only for TLS servers.
		PermittedEmailAddresses: []string{".invalid"},
		PermittedURIDomains:     []string{".invalid"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &Authority{Cert: cert, Key: key, PEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}, nil
}

// write saves the authority in dir: its key readable by its owner only.
func (a *Authority) write(dir string) error {
	key, err := x509.MarshalPKCS8PrivateKey(a.Key)
	if err != nil {
		return err
	}
	if err := filetx.WriteFile(filepath.Join(dir, caKeyFile), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0o600); err != nil {
		return err
	}
	return filetx.WriteFile(filepath.Join(dir, caFile), a.PEM, 0o600)
}

// Export is the authority as PEM, its key included, for another machine of
// the same person to import, so one profile on the phone serves both.
func (a *Authority) Export() ([]byte, error) {
	key, err := x509.MarshalPKCS8PrivateKey(a.Key)
	if err != nil {
		return nil, err
	}
	return append(append([]byte{}, a.PEM...), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})...), nil
}

// Import replaces the authority in dir with the one in data. Phones that
// trusted the old one must install the new one's profile.
func Import(dir string, data []byte) (*Authority, error) {
	a, err := ParseAuthority(data)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := filetx.Lock(filepath.Join(dir, caFile), 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	return a, a.write(dir)
}

// Fingerprint is the SHA-256 of the authority's certificate, as iOS shows
// it under the profile's details, in pairs of hex digits.
func (a *Authority) Fingerprint() string {
	sum := sha256.Sum256(a.Cert.Raw)
	var b strings.Builder
	for i, c := range sum {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%02X", c)
	}
	return b.String()
}

// FingerprintHex is the fingerprint as 64 hex digits, with no spaces, as
// a pairing link carries it.
func (a *Authority) FingerprintHex() string {
	sum := sha256.Sum256(a.Cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// FingerprintStart is the fingerprint's first eight bytes, enough to tell
// the authority from one an attacker made: matching them takes about 2^64
// tries. A person compares them with what iOS shows before trusting it.
func (a *Authority) FingerprintStart() string {
	return a.Fingerprint()[:8*3-1]
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		panic(err)
	}
	return n.Add(n, big.NewInt(1))
}

// Leaves issues the server's certificate: for the machine's own local
// names and addresses, those a phone dials it by, signed by the authority.
// A handshake on an address of the machine's that the certificate lacks,
// as after the network gave the machine a new one, gets a new certificate
// that has it. The name a client asks for (SNI) never goes in: a client
// chooses it, and a certificate with a name the machine doesn't have would
// be no use, or fail to parse for strict verifiers.
type Leaves struct {
	CA *Authority
	// Names are the DNS names besides localhost, such as studio.local.
	Names func() []string
	// Addrs are the machine's addresses.
	Addrs func() []net.IP
	Now   func() time.Time

	mu   sync.Mutex
	cert *tls.Certificate
	leaf *x509.Certificate
}

// GetCertificate is for tls.Config.
func (l *Leaves) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	var dialed net.IP
	if hello.Conn != nil {
		if a, ok := hello.Conn.LocalAddr().(*net.TCPAddr); ok {
			dialed = a.IP
		}
	}
	return l.get(dialed)
}

func (l *Leaves) get(dialed net.IP) (*tls.Certificate, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.cert != nil && now.Before(l.leaf.NotAfter.Add(-leafRenew)) && l.covers(dialed) {
		return l.cert, nil
	}
	cert, leaf, err := l.issue(now, dialed)
	if err != nil {
		return nil, err
	}
	l.cert, l.leaf = cert, leaf
	return cert, nil
}

func (l *Leaves) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// covers says whether the current certificate has the address a client
// dialed, when it is one a certificate can have. The caller holds l.mu.
func (l *Leaves) covers(dialed net.IP) bool {
	return dialed == nil || !vouchable(dialed) || slices.ContainsFunc(l.leaf.IPAddresses, dialed.Equal)
}

// vouchable says whether a server certificate may name ip: a local address
// with no zone, which link-local ones need.
func vouchable(ip net.IP) bool {
	return localIP(ip) && !ip.IsLinkLocalUnicast()
}

// hostLabel is one label of a host name (RFC 1123).
var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// hostName says whether n is a host name made of such labels.
func hostName(n string) bool {
	if n == "" || len(n) > 253 {
		return false
	}
	for _, label := range strings.Split(n, ".") {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func (l *Leaves) issue(now time.Time, dialed net.IP) (*tls.Certificate, *x509.Certificate, error) {
	names := []string{"localhost"}
	if l.Names != nil {
		names = append(names, l.Names()...)
	}
	var ips []net.IP
	if l.Addrs != nil {
		ips = l.Addrs()
	}
	if dialed != nil {
		ips = append(ips, dialed)
	}
	// Only what the authority may vouch for: anything else would make the
	// whole certificate fail its constraints.
	var dns []string
	for _, n := range names {
		n = strings.ToLower(strings.TrimSuffix(n, "."))
		if hostName(n) && localName(n) && !slices.Contains(dns, n) {
			dns = append(dns, n)
		}
	}
	var addrs []net.IP
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
		}
		if vouchable(ip) && !slices.ContainsFunc(addrs, ip.Equal) {
			addrs = append(addrs, ip)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: dns[len(dns)-1], Organization: []string{"Irie"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafLife),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  addrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, l.CA.Cert, key.Public(), l.CA.Key)
	if err != nil {
		return nil, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der, l.CA.Cert.Raw}, PrivateKey: key, Leaf: leaf}, leaf, nil
}

// Profile is the authority as an iOS configuration profile, a root
// certificate payload (Apple's Device Management reference,
// com.apple.security.root). Its identifiers come from the certificate, so
// installing it again replaces it rather than adding a second.
func (a *Authority) Profile() []byte {
	sum := sha256.Sum256(a.Cert.Raw)
	uuid := func(salt byte) string {
		h := sha256.Sum256(append([]byte{salt}, sum[:]...))
		h[6] = h[6]&0x0f | 0x50 // a name-based UUID's version and variant bits
		h[8] = h[8]&0x3f | 0x80
		return fmt.Sprintf("%X-%X-%X-%X-%X", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
	}
	outer, inner := uuid(1), uuid(2)
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadCertificateFileName</key>
			<string>irie-ca.cer</string>
			<key>PayloadContent</key>
			<data>`)
	b.WriteString(base64.StdEncoding.EncodeToString(a.Cert.Raw))
	fmt.Fprintf(&b, `</data>
			<key>PayloadDescription</key>
			<string>Lets this phone check that it talks to Irie on your own machines.</string>
			<key>PayloadDisplayName</key>
			<string>%[1]s</string>
			<key>PayloadIdentifier</key>
			<string>io.github.irie-people.irie.ca.%[2]s</string>
			<key>PayloadType</key>
			<string>com.apple.security.root</string>
			<key>PayloadUUID</key>
			<string>%[2]s</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
		</dict>
	</array>
	<key>PayloadDescription</key>
	<string>Irie's own certificate authority, which may vouch only for machines on a local network. With it, the Irie app on this phone talks to Irie on your machines privately.</string>
	<key>PayloadDisplayName</key>
	<string>Irie</string>
	<key>PayloadIdentifier</key>
	<string>io.github.irie-people.irie.%[3]s</string>
	<key>PayloadRemovalDisallowed</key>
	<false/>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>%[3]s</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>
`, xmlEscape(a.Cert.Subject.CommonName), inner, outer)
	return b.Bytes()
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
