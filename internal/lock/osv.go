package lock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// OSV is the address of OSV.dev's API (google.github.io/osv.dev/api), which
// a check asks: POST /v1/querybatch for the advisories of each package
// version, then GET /v1/vulns/{id} for each advisory's summary and the
// versions that fix it.
const OSV = "https://api.osv.dev"

// The most a check sends and reads: queries in one batch, pages of one
// query's answer, advisories read at once, and the size of one answer.
const (
	batchMost   = 1000
	pagesMost   = 10
	readersMost = 4
	answerMost  = 32 << 20
)

// Checker asks OSV.dev about package versions. Only names and versions go
// out, and the ids of the advisories they bring back: nothing of the
// sessions, the machine or the user.
type Checker struct {
	Base   string       // OSV's address; "" = OSV
	Client *http.Client // nil = one with a timeout of 30 s
	Agent  string       // the User-Agent, such as irie/1.5.0
}

// Check asks about each package and returns what OSV.dev says affects it,
// in order, as of now. An advisory known from before, in prev by the same
// package version, whose modified time OSV gives again, is not read again.
func (k Checker) Check(ctx context.Context, pkgs []ipc.LockPackage, prev map[string]ipc.LockFound, now time.Time) ([]ipc.LockFound, error) {
	ids, mods, err := k.query(ctx, pkgs)
	if err != nil {
		return nil, err
	}
	// Each advisory once, however many versions it affects, unless every
	// version it affects knew it already as it is.
	known := map[string]ipc.LockAdvisory{}
	var want []string
	for i, p := range pkgs {
		old := map[string]ipc.LockAdvisory{}
		for _, a := range prev[Key(p)].Advisories {
			old[a.ID] = a
		}
		for _, id := range ids[i] {
			if a, ok := old[id]; ok && !a.Modified.IsZero() && a.Modified.Equal(mods[id]) {
				known[Key(p)+"\x00"+id] = a
				continue
			}
			if !slices.Contains(want, id) {
				want = append(want, id)
			}
		}
	}
	vulns, err := k.vulns(ctx, want)
	if err != nil {
		return nil, err
	}
	out := make([]ipc.LockFound, len(pkgs))
	for i, p := range pkgs {
		f := ipc.LockFound{Ecosystem: p.Ecosystem, Name: p.Name, Version: p.Version, Part: p.Part, Checked: now}
		for _, id := range ids[i] {
			if a, ok := known[Key(p)+"\x00"+id]; ok {
				f.Advisories = append(f.Advisories, a)
				continue
			}
			v := vulns[id]
			f.Advisories = append(f.Advisories, ipc.LockAdvisory{
				ID: id, Summary: v.summary(), Fixed: v.fixedFor(p), Modified: v.Modified,
			})
		}
		out[i] = f
	}
	return out, nil
}

// query is POST /v1/querybatch: the ids of the advisories that affect each
// package, by its place in pkgs, and when each advisory last changed. A
// query whose answer comes in pages is asked again for the next.
func (k Checker) query(ctx context.Context, pkgs []ipc.LockPackage) ([][]string, map[string]time.Time, error) {
	type pkgQ struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	}
	type query struct {
		Package   pkgQ   `json:"package"`
		Version   string `json:"version"`
		PageToken string `json:"page_token,omitempty"`
	}
	ids := make([][]string, len(pkgs))
	mods := map[string]time.Time{}
	tokens := make([]string, len(pkgs))
	todo := make([]int, len(pkgs))
	for i := range pkgs {
		todo[i] = i
	}
	for page := 0; len(todo) > 0; page++ {
		if page == pagesMost {
			return nil, nil, fmt.Errorf("OSV.dev's answer had more than %d pages", pagesMost)
		}
		var next []int
		for from := 0; from < len(todo); from += batchMost {
			chunk := todo[from:min(from+batchMost, len(todo))]
			var body struct {
				Queries []query `json:"queries"`
			}
			for _, i := range chunk {
				p := pkgs[i]
				body.Queries = append(body.Queries, query{Package: pkgQ{Name: p.Name, Ecosystem: p.Ecosystem}, Version: p.Version, PageToken: tokens[i]})
			}
			var res struct {
				Results []struct {
					Vulns []struct {
						ID       string    `json:"id"`
						Modified time.Time `json:"modified"`
					} `json:"vulns"`
					NextPageToken string `json:"next_page_token"`
				} `json:"results"`
			}
			if err := k.do(ctx, http.MethodPost, "/v1/querybatch", body, &res); err != nil {
				return nil, nil, err
			}
			if len(res.Results) != len(chunk) {
				return nil, nil, fmt.Errorf("OSV.dev answered %d queries of %d", len(res.Results), len(chunk))
			}
			for j, r := range res.Results {
				i := chunk[j]
				for _, v := range r.Vulns {
					if v.ID == "" || slices.Contains(ids[i], v.ID) {
						continue
					}
					ids[i] = append(ids[i], v.ID)
					mods[v.ID] = v.Modified
				}
				if tokens[i] = r.NextPageToken; tokens[i] != "" {
					next = append(next, i)
				}
			}
		}
		todo = next
	}
	for i := range ids {
		slices.Sort(ids[i])
	}
	return ids, mods, nil
}

// vuln is what a check reads of an advisory: GET /v1/vulns/{id}, in the
// OSV schema.
type vuln struct {
	ID       string    `json:"id"`
	Summary  string    `json:"summary"`
	Details  string    `json:"details"`
	Modified time.Time `json:"modified"`
	Affected []struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
		} `json:"package"`
		Ranges []struct {
			Type   string `json:"type"`
			Events []struct {
				Introduced   string `json:"introduced"`
				Fixed        string `json:"fixed"`
				LastAffected string `json:"last_affected"`
			} `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
}

// vulns reads each advisory, a few at a time.
func (k Checker) vulns(ctx context.Context, ids []string) (map[string]vuln, error) {
	out := map[string]vuln{}
	var mu sync.Mutex
	var first error
	sem := make(chan struct{}, readersMost)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var v vuln
			err := k.do(ctx, http.MethodGet, "/v1/vulns/"+url.PathEscape(id), nil, &v)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			out[id] = v
		}()
	}
	wg.Wait()
	return out, first
}

// summary is what the advisory is, in a line: its summary, else the first
// line of its details.
func (v vuln) summary() string {
	s := strings.TrimSpace(v.Summary)
	if s == "" {
		s, _, _ = strings.Cut(strings.TrimSpace(v.Details), "\n")
	}
	return oneLine(s, 200)
}

// fixedFor is the first version after p's that fixes the advisory for p's
// package: the lowest version a fixed event of its ranges names that is
// above p's, which is where the range p's version is in ends; "" when none
// does yet.
func (v vuln) fixedFor(p ipc.LockPackage) string {
	best := ""
	for _, a := range v.Affected {
		if a.Package.Ecosystem != p.Ecosystem || !sameName(p.Ecosystem, a.Package.Name, p.Name) {
			continue
		}
		for _, r := range a.Ranges {
			if r.Type != "SEMVER" && r.Type != "ECOSYSTEM" {
				continue
			}
			for _, e := range r.Events {
				if e.Fixed != "" && Compare(e.Fixed, p.Version) > 0 && (best == "" || Compare(e.Fixed, best) < 0) {
					best = e.Fixed
				}
			}
		}
	}
	return best
}

// sameName compares two names as the ecosystem does: PyPI's normalized
// as PEP 503 says, npm's exactly.
func sameName(ecosystem, a, b string) bool {
	if ecosystem == PyPI {
		norm := func(s string) string {
			s = strings.ToLower(s)
			return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == '.' }), "-")
		}
		return norm(a) == norm(b)
	}
	return a == b
}

// do sends one request to OSV.dev and reads its JSON answer into out.
func (k Checker) do(ctx context.Context, method, path string, in, out any) error {
	base := strings.TrimRight(k.Base, "/")
	if base == "" {
		base = OSV
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if k.Agent != "" {
		req.Header.Set("User-Agent", k.Agent)
	}
	cl := k.Client
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("OSV.dev didn't answer: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, answerMost+1))
	if err != nil {
		return fmt.Errorf("OSV.dev's answer didn't read: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OSV.dev answered %s: %s", resp.Status, oneLine(string(b), 200))
	}
	if len(b) > answerMost {
		return fmt.Errorf("OSV.dev's answer was over %d MiB", answerMost>>20)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("OSV.dev's answer didn't read: %v", err)
	}
	return nil
}

// oneLine is s on one line, cut to n runes.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
