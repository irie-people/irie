package anon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SearchQuery is one search of Irie's SearXNG.
type SearchQuery struct {
	Q          string
	Page       int    // from 1; 0 = 1
	TimeRange  string // day | week | month | year; "" = any time
	Categories string // comma-separated, such as news; "" = general
	Language   string // such as en; "" = all
}

// Hit is one result.
type Hit struct {
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Content   string   `json:"content"`
	Engines   []string `json:"engines"`
	Published string   `json:"publishedDate"`
}

// Results are what SearXNG's JSON format gives back (its search API docs):
// results, and answers, suggestions and corrections besides, with the
// engines that did not answer.
type Results struct {
	Hits         []Hit
	Answers      []string
	Suggestions  []string
	Corrections  []string
	Infobox      string
	Unresponsive []string
}

// maxHits is the most results a search gives the agent.
const maxHits = 15

// Search asks the SearXNG at base, such as http://127.0.0.1:8888.
func Search(ctx context.Context, base string, q SearchQuery) (Results, error) {
	v := url.Values{"q": {q.Q}, "format": {"json"}}
	if q.Page > 1 {
		v.Set("pageno", strconv.Itoa(q.Page))
	}
	if q.TimeRange != "" {
		v.Set("time_range", q.TimeRange)
	}
	if q.Categories != "" {
		v.Set("categories", q.Categories)
	}
	if q.Language != "" {
		v.Set("language", q.Language)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/search?"+v.Encode(), nil)
	if err != nil {
		return Results{}, err
	}
	// SearXNG is on this machine: no proxy between.
	c := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 60 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return Results{}, fmt.Errorf("Irie's SearXNG at %s did not answer (it may still be starting: try again in a minute): %v", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Results{}, fmt.Errorf("Irie's SearXNG answered %s", resp.Status)
	}
	var raw struct {
		Results      []Hit             `json:"results"`
		Answers      []json.RawMessage `json:"answers"`
		Suggestions  []string          `json:"suggestions"`
		Corrections  []string          `json:"corrections"`
		Infoboxes    []json.RawMessage `json:"infoboxes"`
		Unresponsive [][]any           `json:"unresponsive_engines"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 16<<20)).Decode(&raw); err != nil {
		return Results{}, fmt.Errorf("Irie's SearXNG gave no JSON: %v", err)
	}
	r := Results{Hits: raw.Results, Suggestions: raw.Suggestions, Corrections: raw.Corrections}
	if len(r.Hits) > maxHits {
		r.Hits = r.Hits[:maxHits]
	}
	// An answer is a string in older SearXNG, an object with one now.
	for _, a := range raw.Answers {
		var s string
		var o struct {
			Answer string `json:"answer"`
		}
		switch {
		case json.Unmarshal(a, &s) == nil && s != "":
			r.Answers = append(r.Answers, s)
		case json.Unmarshal(a, &o) == nil && o.Answer != "":
			r.Answers = append(r.Answers, o.Answer)
		}
	}
	if len(raw.Infoboxes) > 0 {
		var ib struct {
			Infobox string `json:"infobox"`
			Content string `json:"content"`
		}
		if json.Unmarshal(raw.Infoboxes[0], &ib) == nil && ib.Infobox != "" {
			r.Infobox = strings.TrimSpace(ib.Infobox + ": " + ib.Content)
		}
	}
	for _, u := range raw.Unresponsive {
		if len(u) > 0 {
			name, _ := u[0].(string)
			why := ""
			if len(u) > 1 {
				why, _ = u[1].(string)
			}
			r.Unresponsive = append(r.Unresponsive, strings.TrimSpace(name+" ("+why+")"))
		}
	}
	return r, nil
}

// Words are the results as the agent reads them.
func (r Results) Words(query string) string {
	var b strings.Builder
	if len(r.Hits) == 0 {
		fmt.Fprintf(&b, "No results for %q.", query)
	} else {
		fmt.Fprintf(&b, "Results for %q:\n", query)
	}
	for i, h := range r.Hits {
		fmt.Fprintf(&b, "\n%d. %s\n   %s\n", i+1, oneLine(h.Title, 200), h.URL)
		if c := oneLine(h.Content, 400); c != "" {
			fmt.Fprintf(&b, "   %s\n", c)
		}
		var about []string
		if h.Published != "" {
			about = append(about, h.Published)
		}
		if len(h.Engines) > 0 {
			about = append(about, strings.Join(h.Engines, ", "))
		}
		if len(about) > 0 {
			fmt.Fprintf(&b, "   (%s)\n", strings.Join(about, " · "))
		}
	}
	if r.Infobox != "" {
		fmt.Fprintf(&b, "\nInfobox: %s\n", oneLine(r.Infobox, 600))
	}
	for _, a := range r.Answers {
		fmt.Fprintf(&b, "\nAnswer: %s\n", oneLine(a, 600))
	}
	if len(r.Corrections) > 0 {
		fmt.Fprintf(&b, "\nDid you mean: %s\n", strings.Join(r.Corrections, "; "))
	}
	if len(r.Suggestions) > 0 {
		fmt.Fprintf(&b, "\nRelated searches: %s\n", strings.Join(r.Suggestions[:min(len(r.Suggestions), 8)], "; "))
	}
	if len(r.Unresponsive) > 0 {
		fmt.Fprintf(&b, "\nEngines that did not answer: %s\n", strings.Join(r.Unresponsive, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// oneLine is s with its whitespace made single spaces, cut to n runes.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
