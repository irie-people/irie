// Package peer is a core's side of the cores it is paired with
// (docs/proposals/multi-host.md §4.1): their gateways, `irie remote serve`
// on each other machine, reached over HTTPS on the local network with a
// token that machine issued, as a phone reaches them (docs/PHONE.md). A
// client pins the other machine's Irie authority by its fingerprint, so it
// trusts no other certificate. The tokens are kept in peers.json, mode
// 0600, beside the gateway's own files.
package peer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// Link is what a pairing link (`irie remote pair`) carries: the gateway,
// a one-time code, the SHA-256 of the machine's Irie authority and the
// machine's name.
type Link struct {
	URL, Code, Fingerprint, Name string
}

// ParseLink reads a pairing link, irie://pair?u=…&c=…&f=…&n=….
func ParseLink(s string) (Link, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "irie" || u.Host != "pair" {
		return Link{}, fmt.Errorf("%q is not a pairing link: `irie remote pair` on the other machine prints one, irie://pair?…", s)
	}
	q := u.Query()
	l := Link{URL: q.Get("u"), Code: q.Get("c"), Fingerprint: strings.ToUpper(q.Get("f")), Name: q.Get("n")}
	if err := checkURL(l.URL); err != nil {
		return Link{}, err
	}
	if l.Code == "" {
		return Link{}, errors.New("the pairing link has no code")
	}
	if b, err := hex.DecodeString(l.Fingerprint); err != nil || len(b) != sha256.Size {
		return Link{}, errors.New("the pairing link's fingerprint is not a SHA-256")
	}
	return l, nil
}

func checkURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" && u.Path != "/" {
		return fmt.Errorf("%q is not a gateway's address, https://host:port", s)
	}
	return nil
}

// Info is what a gateway says about its machine without a token.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	API     int    `json:"api"`
	OS      string `json:"os"`
}

// Client talks to one other machine's gateway.
type Client struct {
	URL   string // https://host:port
	Token string // "" until paired
	http  *http.Client
}

// New makes a client of the gateway at base that trusts only the Irie
// authority whose SHA-256 is fingerprint (hex).
func New(base, token, fingerprint string) (*Client, error) {
	if err := checkURL(base); err != nil {
		return nil, err
	}
	want, err := hex.DecodeString(fingerprint)
	if err != nil || len(want) != sha256.Size {
		return nil, errors.New("a peer's fingerprint must be a SHA-256, in hex")
	}
	u, _ := url.Parse(base)
	host := u.Hostname()
	tr := &http.Transport{
		Proxy:               nil, // the local network only, never through a proxy
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// The chain is checked below, against the pinned authority
			// alone, rather than against the system's roots.
			InsecureSkipVerify:    true,
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error { return verifyPinned(raw, want, host) },
		},
	}
	return &Client{URL: strings.TrimSuffix(base, "/"), Token: token, http: &http.Client{Transport: tr}}, nil
}

// verifyPinned checks that the server's certificate is valid for host
// under the authority whose SHA-256 is want, which the server must send
// with its chain, as Irie's gateway does.
func verifyPinned(raw [][]byte, want []byte, host string) error {
	if len(raw) == 0 {
		return errors.New("the peer sent no certificate")
	}
	leaf, err := x509.ParseCertificate(raw[0])
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	found := false
	for _, r := range raw[1:] {
		sum := sha256.Sum256(r)
		if !bytes.Equal(sum[:], want) {
			continue
		}
		ca, err := x509.ParseCertificate(r)
		if err != nil {
			return err
		}
		roots.AddCert(ca)
		found = true
	}
	if !found {
		return errors.New("the peer's certificate is not from the Irie authority it was paired with")
	}
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err
}

// Info asks the gateway who it is.
func (c *Client) Info(ctx context.Context) (Info, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/info", nil)
	if err != nil {
		return Info{}, err
	}
	var in Info
	return in, c.do(req, &in)
}

// Pair turns a one-time code into a token, as a phone pairs. device is
// how the other machine lists this one.
func (c *Client) Pair(ctx context.Context, code, device string) (string, error) {
	var out struct {
		Token string     `json:"token"`
		Error *ipc.Error `json:"error"`
	}
	if err := c.post(ctx, "/api/pair", map[string]string{"code": code, "device": device}, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", out.Error
	}
	if out.Token == "" {
		return "", errors.New("the peer gave no token")
	}
	c.Token = out.Token
	return out.Token, nil
}

// Forget unpairs this machine on the other one.
func (c *Client) Forget(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.URL+"/api/device", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// Call sends one op to the other machine's core and decodes its result,
// as the core there wrote it. The core's own errors come back as
// *ipc.Error.
func (c *Client) Call(ctx context.Context, op string, args, result any) error {
	var out struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *ipc.Error      `json:"error"`
	}
	if args == nil {
		args = struct{}{}
	}
	if err := c.post(ctx, "/api/call", map[string]any{"op": op, "args": args}, &out); err != nil {
		return err
	}
	if !out.OK {
		if out.Error == nil {
			return errors.New("the peer refused the call")
		}
		return out.Error
	}
	if result != nil && len(out.Result) > 0 {
		return json.Unmarshal(out.Result, result)
	}
	return nil
}

// Subscribe follows the other machine's core: a snapshot, then a frame for
// each change, as the core there writes them, until ctx ends or the stream
// does. The gateway pings every 15 s while nothing changes, which comes as
// a frame of its own, so a reader can tell a quiet peer from a gone one.
func (c *Client) Subscribe(ctx context.Context, args ipc.SubscribeArgs) (<-chan ipc.Frame, <-chan error, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/api/subscribe", bytes.NewReader(b))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, nil, statusError(resp)
	}
	rd := bufio.NewReaderSize(resp.Body, 64<<10)
	first, err := readLine(rd)
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	var rep struct {
		OK    bool       `json:"ok"`
		Error *ipc.Error `json:"error"`
	}
	if err := json.Unmarshal(first, &rep); err != nil || !rep.OK {
		resp.Body.Close()
		if rep.Error != nil {
			return nil, nil, rep.Error
		}
		return nil, nil, fmt.Errorf("the peer's subscription: %s", first)
	}
	frames, errs := make(chan ipc.Frame, 16), make(chan error, 1)
	go func() {
		defer resp.Body.Close()
		defer close(frames)
		for {
			line, err := readLine(rd)
			if err != nil {
				errs <- err
				return
			}
			var f ipc.Frame
			if err := json.Unmarshal(line, &f); err != nil {
				errs <- fmt.Errorf("the peer's frame: %w", err)
				return
			}
			select {
			case frames <- f:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
	}()
	return frames, errs, nil
}

// readLine reads one line of NDJSON, of up to 32 MiB.
func readLine(rd *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, more, err := rd.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > 32<<20 {
			return nil, errors.New("a frame of over 32 MiB")
		}
		if !more {
			return buf, nil
		}
	}
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

// do sends req with the token and decodes the JSON reply into out. A reply
// the gateway refused with an error of its own decodes as that error.
func (c *Client) do(req *http.Request, out any) error {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNoContent:
		return nil
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests:
		return statusError(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

// statusError is a refused request, with the gateway's own error when it
// sent one.
func statusError(resp *http.Response) error {
	var body struct {
		Error *ipc.Error `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body) == nil && body.Error != nil {
		return body.Error
	}
	return fmt.Errorf("the peer answered %s", resp.Status)
}
