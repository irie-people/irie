package dms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultAPI is the Management API of the hosted Healthchecks.io. A user who
// runs their own Healthchecks points --api at theirs.
const DefaultAPI = "https://healthchecks.io/api/v3/"

// The API's limits on a check (https://healthchecks.io/docs/api/).
const (
	MinSeconds = 60       // one minute, for timeout and grace
	MaxSeconds = 31536000 // 365 days
	MaxName    = 100      // characters
	MaxDesc    = 10000    // characters
)

// Client talks to a Healthchecks Management API with a project's API key.
// The key never appears in an error or a log.
type Client struct {
	API  string // ends in /
	Key  string
	HTTP *http.Client
}

// Check is a check as the API returns it. A read-only key gets no uuid,
// ping_url or channels, and a unique_key instead.
type Check struct {
	Name      string     `json:"name"`
	Desc      string     `json:"desc"`
	Timeout   int        `json:"timeout"` // seconds
	Grace     int        `json:"grace"`   // seconds
	Status    string     `json:"status"`  // new, up, grace, down or paused
	Pings     int        `json:"n_pings"`
	LastPing  *time.Time `json:"last_ping"`
	NextPing  *time.Time `json:"next_ping"`
	UUID      string     `json:"uuid"`
	PingURL   string     `json:"ping_url"`
	Channels  string     `json:"channels"` // the integrations it notifies, by id, separated by commas
	UniqueKey string     `json:"unique_key"`
}

// Channel is an integration of the project, such as one email address.
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // email, sms, webhook, …
}

// NewCheck is what Upsert sends. It never sends channels: who is emailed is
// a standing rule that writes to other people, which the user sets in the
// web page (docs/proposals/extensions.md §1.3).
type NewCheck struct {
	Name    string   `json:"name"`
	Desc    string   `json:"desc"`
	Timeout int      `json:"timeout"`
	Grace   int      `json:"grace"`
	Unique  []string `json:"unique"`
}

// Upsert makes the check, or brings the check of that name up to date, and
// says whether it was new.
func (c Client) Upsert(n NewCheck) (Check, bool, error) {
	n.Unique = []string{"name"}
	var out Check
	code, err := c.do(http.MethodPost, "checks/", n, &out)
	if err != nil {
		return Check{}, false, err
	}
	return out, code == http.StatusCreated, nil
}

// Checks lists the project's checks.
func (c Client) Checks() ([]Check, error) {
	var out struct{ Checks []Check }
	_, err := c.do(http.MethodGet, "checks/", nil, &out)
	return out.Checks, err
}

// Channels lists the project's integrations.
func (c Client) Channels() ([]Channel, error) {
	var out struct{ Channels []Channel }
	_, err := c.do(http.MethodGet, "channels/", nil, &out)
	return out.Channels, err
}

// Pause stops the check from going down until its next ping, so nobody is
// emailed meanwhile. Resume starts it again.
func (c Client) Pause(uuid string) error  { return c.post(uuid, "pause") }
func (c Client) Resume(uuid string) error { return c.post(uuid, "resume") }

func (c Client) post(uuid, what string) error {
	if uuid == "" || strings.ContainsAny(uuid, "/?#") {
		return fmt.Errorf("no such check %q", uuid)
	}
	_, err := c.do(http.MethodPost, "checks/"+url.PathEscape(uuid)+"/"+what, struct{}{}, nil)
	return err
}

// do makes one request and reads its JSON reply into out.
func (c Client) do(method, path string, in, out any) (int, error) {
	if c.Key == "" {
		return 0, errors.New("no Healthchecks API key: copy the project's API key, then run `irie dms key --paste`")
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.API, "/")+"/"+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Api-Key", c.Key)
	req.Header.Set("User-Agent", "irie-dms")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Healthchecks: %v", redact(err.Error(), c.Key))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
	case http.StatusUnauthorized:
		return resp.StatusCode, errors.New("Healthchecks refused the API key. Make a new one in the project's settings, copy it, and run `irie dms key --paste` again")
	case http.StatusForbidden:
		// The API's 403 is a plan's limit: "For free accounts, the limit is
		// 20 checks per account".
		return resp.StatusCode, errors.New("Healthchecks refused to make another check: the account has as many as its plan allows (20 on the free plan). Delete one you don't use, and run this again")
	case http.StatusNotFound:
		return resp.StatusCode, errors.New("Healthchecks has no such check in this key's project")
	default:
		msg := strings.TrimSpace(redact(string(data), c.Key))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return resp.StatusCode, fmt.Errorf("Healthchecks answered %s: %s", resp.Status, msg)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return resp.StatusCode, fmt.Errorf("Healthchecks: an answer Irie cannot read: %v", err)
	}
	return resp.StatusCode, nil
}

// redact takes a key out of text, in case a server or a proxy echoes it.
func redact(text, key string) string {
	if key == "" {
		return text
	}
	return strings.ReplaceAll(text, key, "[key]")
}

// Ping checks in, as the phone's tap does: one request to a check's ping
// URL.
func Ping(hc *http.Client, pingURL string) error {
	u, err := url.Parse(pingURL)
	if err != nil || u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return fmt.Errorf("%q is not a ping URL", pingURL)
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Post(pingURL, "text/plain", strings.NewReader("I'm OK, from irie dms ping"))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the ping answered %s", resp.Status)
	}
	// The hosted service answers 200 for a check it doesn't have, and says
	// so in the body (https://healthchecks.io/docs/http_api/).
	switch answer := strings.TrimSpace(string(body)); {
	case strings.Contains(answer, "not found"):
		return errors.New("Healthchecks has no check at that ping URL any more; make the checks again with `irie dms checks`")
	case strings.Contains(answer, "rate limited"):
		return errors.New("Healthchecks took too many pings in a minute; try again in a minute")
	}
	return nil
}

func isLoopback(host string) bool { return host == "127.0.0.1" || host == "::1" || host == "localhost" }
