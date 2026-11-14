package mail

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
)

// Pins are the certificates of the mail servers on this computer, such as
// Proton Mail Bridge's, which signs its own: Irie trusts the first one a
// server shows and, after that, only that one, so another program that
// takes the server's port can't read a password. A server anywhere else
// must show a certificate the system trusts.
type Pins struct {
	Path string // a JSON file: server's host:port → its certificate's SHA-256

	mu sync.Mutex
}

// ErrPinChanged is the answer for a server on this computer that shows a
// certificate other than the one Irie trusts.
var ErrPinChanged = errors.New("the certificate changed")

func (p *Pins) read() (map[string]string, error) {
	b, err := os.ReadFile(p.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %v", p.Path, err)
	}
	return m, nil
}

func (p *Pins) write(m map[string]string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(p.Path, append(b, '\n'), 0o600)
}

// Forget stops trusting the certificate of the server at addr, so the next
// one it shows is trusted: after Proton Mail Bridge was installed again,
// say.
func (p *Pins) Forget(addr string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	m, err := p.read()
	if err != nil {
		return err
	}
	if _, ok := m[addr]; !ok {
		return nil
	}
	delete(m, addr)
	return p.write(m)
}

// fingerprint is a certificate's SHA-256, in hex.
func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// TLS is the TLS config for a connection to s: the system's checks for a
// server elsewhere, and for one on this computer, its pinned certificate.
func (p *Pins) TLS(s Server) *tls.Config {
	c := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	if !s.Local() {
		return c
	}
	addr := s.Addr()
	// Go's own checks would refuse a certificate the server signed
	// itself, so this one checks instead: the leaf must be the one
	// pinned, or, with none pinned yet, it becomes the one.
	c.InsecureSkipVerify = true
	c.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
		if len(raw) == 0 {
			return errors.New("the server showed no certificate")
		}
		if _, err := x509.ParseCertificate(raw[0]); err != nil {
			return fmt.Errorf("the server's certificate: %v", err)
		}
		got := fingerprint(raw[0])
		p.mu.Lock()
		defer p.mu.Unlock()
		m, err := p.read()
		if err != nil {
			return err
		}
		switch want, ok := m[addr]; {
		case !ok:
			m[addr] = got
			return p.write(m)
		case want != got:
			return fmt.Errorf("%w: the mail server at %s shows another certificate than the one %s trusts. If you installed Proton Mail Bridge again, or it made a new one, `%s mail trust` trusts the new one", ErrPinChanged, addr, brand.Name, brand.Slug)
		}
		return nil
	}
	return c
}
