package council

import (
	"encoding/json"
	"github.com/irie-people/irie/internal/brand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/arch"
	"github.com/irie-people/irie/internal/event"
)

var now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// journal is a fortnight of two stretches' events, as the journal holds
// them, oldest first.
func journal() []event.Event {
	var evs []event.Event
	ev := func(at time.Time, session, kind, id string, data map[string]any) {
		e := event.Event{TS: event.Stamp(at), Session: session, Agent: event.Main, Kind: kind, Data: data}
		if id != "" {
			e.Vendor = &event.Vendor{ToolUseID: id}
		}
		evs = append(evs, e)
	}
	call := func(at time.Time, session, id, tool, cmd string, ms float64, end string, more map[string]any) {
		ev(at, session, event.ToolStarted, id, map[string]any{"tool": tool, "summary": cmd})
		data := map[string]any{"tool": tool, "ms": ms}
		for k, v := range more {
			data[k] = v
		}
		ev(at.Add(time.Duration(ms)*time.Millisecond), session, end, id, data)
	}
	// Before the earlier stretch: not counted.
	ev(time.Date(2026, 11, 7, 11, 0, 0, 0, time.UTC), "s0", event.TurnFailed, "", map[string]any{"error": "rate_limit"})

	// The stretch before: one session in the repo.
	b := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	ev(b, "s3", event.SpawnRequested, "", map[string]any{"repo": "/home/alice/irie", "profile": "fake"})
	call(b.Add(time.Minute), "s3", "b1", "Bash", "grep -n x b.go", 1000, event.ToolFinished, nil)
	call(b.Add(2*time.Minute), "s3", "b2", "Bash", "cat b.go", 1000, event.ToolFinished, nil)
	ev(b.Add(3*time.Minute), "s3", event.CacheRebuilt, "", map[string]any{"usd": 1.0, "cause": "you"})
	ev(b.Add(4*time.Minute), "s3", event.TurnFailed, "", map[string]any{"error": "rate_limit"})
	ev(b.Add(5*time.Minute), "s3", event.TurnFailed, "", map[string]any{"error": "rate_limit"})

	// This stretch: a session in the repo, and a task it handed on.
	n := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ev(n, "s1", event.SpawnRequested, "", map[string]any{"repo": "/home/alice/irie", "profile": "fake"})
	call(n.Add(time.Minute), "s1", "t1", "Bash", "grep -n foo a.go", 1000, event.ToolFinished, nil)
	call(n.Add(2*time.Minute), "s1", "t2", "Bash", "sed -n 1,9p a.go", 1000, event.ToolFinished, nil)
	call(n.Add(3*time.Minute), "s1", "t3", "Bash", "sleep 30; irie ctl list", 30000, event.ToolFinished, nil)
	call(n.Add(4*time.Minute), "s1", "t4", "Bash", "go test ./internal/core", 60000, event.ToolFailed, map[string]any{"tests": "failed"})
	// An ask the user answers five minutes on, and one they leave.
	a := n.Add(time.Hour)
	ev(a, "s1", event.ToolStarted, "t5", map[string]any{"tool": "Bash", "summary": "rm -rf build"})
	ev(a.Add(time.Second), "s1", event.InputNeeded, "", map[string]any{"reason": "permission"})
	ev(a.Add(time.Second), "s1", event.InputNeeded, "", map[string]any{"reason": "permission"}) // the same ask, by a second hook
	ev(a.Add(5*time.Minute+time.Second), "s1", event.ToolFinished, "t5", map[string]any{"tool": "Bash", "ms": 300000.0})
	ev(a.Add(time.Hour), "s1", event.InputNeeded, "", map[string]any{"reason": "question"})
	ev(a.Add(2*time.Hour), "s1", event.CacheRebuilt, "", map[string]any{"usd": 0.5, "cause": "you"})
	ev(a.Add(2*time.Hour), "s1", event.TurnFailed, "", map[string]any{"error": "rate_limit"})
	ev(a.Add(2*time.Hour), "s1", event.DogNudged, "", map[string]any{"dirty": 2.0, "ahead": 0.0})
	ev(a.Add(2*time.Hour), "s1", event.RouteFailover, "", map[string]any{"to": "s2"})
	d := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	ev(d, "s2", event.SpawnRequested, "", map[string]any{"profile": "haiku", "parent": "s1"})
	call(d.Add(time.Minute), "s2", "t6", "Read", "", 20, event.ToolFinished, nil)

	// After now: not counted.
	ev(now.Add(time.Hour), "s1", event.TurnFailed, "", map[string]any{"error": "overloaded"})
	return evs
}

func tally() Tally {
	return Count(Input{
		Now: now, Days: 7, Events: journal(),
		Cost:  map[string]float64{"s1": 2.5, "s2": 0.25, "s3": 1},
		Names: map[string]string{"s1": "fix-parser", "s3": "old-task"},
		Home:  "/home/alice",
	})
}

func TestCountTalliesAStretchBesideTheOneBefore(t *testing.T) {
	tl := tally()
	a, b := tl.Now, tl.Before
	if tl.Days != 7 || !a.From.Equal(now.AddDate(0, 0, -7)) || !b.From.Equal(now.AddDate(0, 0, -14)) || !b.To.Equal(a.From) {
		t.Fatalf("stretches %v to %v and %v to %v", a.From, a.To, b.From, b.To)
	}
	if a.Sessions != 2 || a.Calls != 6 || b.Sessions != 1 || b.Calls != 2 {
		t.Errorf("now %d sessions, %d calls; before %d, %d", a.Sessions, a.Calls, b.Sessions, b.Calls)
	}
	// Cost: by the stretch of each session's last event.
	if a.USD != 2.75 || b.USD != 1 {
		t.Errorf("cost now %v, before %v", a.USD, b.USD)
	}
	if want := []Row{{Name: "~/irie", N: 1, USD: 2.5}, {Name: "(no repository)", N: 1, USD: 0.25}}; !same(a.ByRepo, want) {
		t.Errorf("by repo %+v", a.ByRepo)
	}
	if len(a.ByProfile) != 2 || a.ByProfile[0].Name != "+fake" || a.ByProfile[1].Name != "+haiku" {
		t.Errorf("by profile %+v", a.ByProfile)
	}
	if len(a.Costliest) != 2 || a.Costliest[0].Name != "fix-parser" || a.Costliest[0].Note != "~/irie · +fake" || a.Costliest[1].Name != "s2" {
		t.Errorf("costliest %+v", a.Costliest)
	}
	if a.Delegated.N != 1 || a.Delegated.USD != 0.25 {
		t.Errorf("delegated %+v", a.Delegated)
	}
	// Calls, by kind and tool.
	if len(a.Finds) != 1 || a.Finds[0].Name != "grep" || a.Finds[0].N != 1 || a.Finds[0].Of != 1 {
		t.Errorf("finds %+v", a.Finds)
	}
	if a.Waits.N != 1 || a.Waits.Minutes != 0.5 || len(a.Waits.Examples) != 1 {
		t.Errorf("waits %+v", a.Waits)
	}
	if a.Longest[0].Name != "rm" || a.Longest[0].Minutes != 5 || a.Longest[1].Name != "go test" {
		t.Errorf("longest %+v", a.Longest)
	}
	if a.Tools[0].Name != "Bash" || a.Tools[0].N != 5 || a.Tools[1].Name != "Read" {
		t.Errorf("tools %+v", a.Tools)
	}
	// What breaks.
	if a.Tests.N != 1 || a.Tests.Of != 1 || len(a.TestsBy) != 1 || a.TestsBy[0].Name != "go test" {
		t.Errorf("tests %+v %+v", a.Tests, a.TestsBy)
	}
	if len(a.ToolFailures) != 1 || a.ToolFailures[0].Name != "go test" || a.ToolFailures[0].N != 1 || a.ToolFailures[0].Of != 1 {
		t.Errorf("tool failures %+v", a.ToolFailures)
	}
	if want := []Row{{Name: "rate_limit", N: 1}}; !same(a.TurnFailures, want) || !same(b.TurnFailures, []Row{{Name: "rate_limit", N: 2}}) {
		t.Errorf("turn failures now %+v, before %+v", a.TurnFailures, b.TurnFailures)
	}
	if want := []Row{{Name: "files not committed", N: 1}}; !same(a.Nudges, want) {
		t.Errorf("nudges %+v", a.Nudges)
	}
	if a.Failovers != 1 || a.Fallbacks != 0 {
		t.Errorf("failovers %d, fallbacks %d", a.Failovers, a.Fallbacks)
	}
	if a.Rebuilds.N != 1 || a.Rebuilds.USD != 0.5 || b.Rebuilds.USD != 1 || !same(a.RebuiltBy, []Row{{Name: "you", N: 1, USD: 0.5}}) {
		t.Errorf("rebuilds %+v by %+v; before %+v", a.Rebuilds, a.RebuiltBy, b.Rebuilds)
	}
	// The user's asks: the permission answered after five minutes, asked
	// once though two hooks said so; the question left.
	if want := []Row{{Name: "permission", N: 1, Of: 1, Minutes: 5, Most: 5}, {Name: "question", N: 0, Of: 1}}; !same(a.Asks, want) {
		t.Errorf("asks %+v", a.Asks)
	}
}

func same(a, b []Row) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestMarkdownGivesEachSeatItsSection(t *testing.T) {
	tl := tally()
	tl.Earlier = Earliers(tl, []Council{{Date: "2026-11-14", Plans: []Plan{
		{N: 1, Seat: "gabriel", Title: "Fewer sleeps", Watch: brand.Gabriel + ": hand-written waits", Said: Yes, Jot: "s9", Then: "Hand-written waits: 9 calls (4), 12 min (3)"},
		{N: 2, Seat: "raguel", Title: "A cheaper model", Watch: "cost", Said: No, Why: "not now"},
	}}}, func(jot string) string { return "done" }, 5)
	md := Markdown(tl)
	for _, want := range []string{
		"# " + brand.Raguel + "'s tally, 2026-11-14 to 2026-11-21\n",
		"\n## " + brand.Ragu + " 🤝: cost\n",
		"\n## " + brand.Gabriel + " 🪽: time\n",
		"\n## " + brand.Raffi + " ❤️\u200d🩹: what breaks\n",
		"\n## " + brand.Mikael + " ⚔️🛡️: what must not be traded\n",
		"- What the sessions cost: $2.75 ($1.00)\n",
		"- rate_limit: 1 turn (2)\n",
		"- grep: 1 of 1, 100% (1 of 1, 100%)\n",
		"- permission: 1 asked (0), 1 answered (0), 5 min waited in all (0), the longest 5 min (0)\n",
		"- Hand-written waits: 1 call (0), 0.5 min (0). E.g. `sleep 30; irie ctl list`\n",
		"- fix-parser: $2.50, ~/irie · +fake\n",
		"- 2026-11-14, plan 1, " + brand.Gabriel + " 🪽: Fewer sleeps. Yes; its jot s9 is done. Watch: " + brand.Gabriel + ": hand-written waits. Then: Hand-written waits: 9 calls (4), 12 min (3). Now: Hand-written waits: 1 call (0), 0.5 min (0). E.g. `sleep 30; irie ctl list`.\n",
		"- 2026-11-14, plan 2, " + brand.Ragu + " 🤝: A cheaper model. No: not now. Watch: cost.\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("no %q in\n%s", want, md)
		}
	}
	// The seats come in their order, the earlier plans last.
	order := []string{brand.Ragu + " 🤝: cost", brand.Gabriel + " 🪽: time", brand.Raffi + " ❤️\u200d🩹: what breaks", brand.Mikael + " ⚔️🛡️", "Earlier councils' plans"}
	for i := 1; i < len(order); i++ {
		if strings.Index(md, order[i-1]) > strings.Index(md, order[i]) {
			t.Errorf("%q comes after %q", order[i-1], order[i])
		}
	}
}

func TestLineFindsTheLineAWatchNames(t *testing.T) {
	tl := tally()
	for watch, want := range map[string]string{
		brand.Gabriel + "'s hand-written waits, in calls":  "Hand-written waits: 1 call (0), 0.5 min (0). E.g. `sleep 30; irie ctl list`",
		"grep in Searches, then a read of what they found": "grep: 1 of 1, 100% (1 of 1, 100%)",
		"go test in Calls that failed, by kind":            "go test: 1 of 1, 100% (0 of 0, 0%)",
		"something the tally lacks":                        "",
		"grepping":                                         "",
	} {
		if got := Line(tl, watch); got != want {
			t.Errorf("Line(%q) = %q, want %q", watch, got, want)
		}
	}
}

func plan() Plan {
	return Plan{Seat: "raguel", Title: "One call for a grep and its read", Evidence: "59% of greps then a read", Change: "tools/code.sh grep --show",
		Saves: "3,700 calls a week", Effort: "small", Watch: "grep in Searches, then a read", Where: "/home/alice/irie", Brief: "Build it."}
}

func TestCheckNamesTheFieldItRefuses(t *testing.T) {
	if err := Check(plan()); err != nil {
		t.Fatalf("a whole plan: %v", err)
	}
	for want, bad := range map[string]func(*Plan){
		`seat "uriel"`:       func(p *Plan) { p.Seat = "uriel" },
		"title: is empty":    func(p *Plan) { p.Title = "  " },
		"title: 121":         func(p *Plan) { p.Title = strings.Repeat("t", 121) },
		"title: must be one": func(p *Plan) { p.Title = "two\nlines" },
		`effort "huge"`:      func(p *Plan) { p.Effort = "huge" },
		"evidence: is empty": func(p *Plan) { p.Evidence = "" },
		"change: is empty":   func(p *Plan) { p.Change = "" },
		"saves: is empty":    func(p *Plan) { p.Saves = "" },
		"watch: is empty":    func(p *Plan) { p.Watch = "\n" },
		"where: is empty":    func(p *Plan) { p.Where = "" },
		"brief: is empty":    func(p *Plan) { p.Brief = "" },
	} {
		p := plan()
		bad(&p)
		if err := Check(p); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", want, err)
		}
	}
	p := plan()
	p.Title, p.Risk = strings.Repeat("é", MaxTitle), ""
	if err := Check(p); err != nil {
		t.Errorf("a title of %d characters and no risk: %v", MaxTitle, err)
	}
}

func TestMinutesEscapeHTMLAndListEveryPlan(t *testing.T) {
	a, b := plan(), plan()
	a.N, b.N = 1, 2
	a.Title = `Stop <script>alert("x")</script> sleeps`
	b.Title, b.Seat, b.Said = "Fewer failed test runs", "raphael", Yes
	cost := 3.126
	c := Council{Date: "2026-11-21", Summary: "A calm week & cheaper.", Plans: []Plan{a, b}, CostUSD: &cost}
	page, md, err := Minutes(c, tally())
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"<!doctype html>", `name="viewport"`, "prefers-color-scheme:dark",
		"A calm week &amp; cheaper.", "1. Stop &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; sleeps", "2. Fewer failed test runs",
		brand.Ragu + " 🤝", brand.Raffi + " ❤️\u200d🩹", "Saves 3,700 calls a week · Effort: small", "<details><summary>The tally</summary>", "$3.13",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("no %q in the page", want)
		}
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "http") || strings.Contains(html, "<link") {
		t.Errorf("the page runs or fetches something:\n%s", html)
	}
	// The summary, then the plans, then the tally, then the cost.
	at := func(s string) int { return strings.Index(html, s) }
	if !(at("A calm week") < at("1. Stop") && at("1. Stop") < at("2. Fewer") && at("2. Fewer") < at("The tally") && at("The tally") < at("What the council cost")) {
		t.Errorf("the page is out of order")
	}
	m := string(md)
	for _, want := range []string{"# " + brand.Raguel + "'s council, 2026-11-21\n", "### 1. Stop <script>", "### 2. Fewer failed test runs\n", "> Build it.\n", "## " + brand.Raguel + "'s tally, ", "### " + brand.Ragu + " 🤝: cost", "## What the council cost\n\n$3.13"} {
		if !strings.Contains(m, want) {
			t.Errorf("no %q in the Markdown:\n%s", want, m)
		}
	}
}

// TestTheChargeNamesThePlansFields: Raguel's charge tells his session the
// fields of a plan, which the op decodes by their JSON names, and the
// commands it runs.
func TestTheChargeNamesThePlansFields(t *testing.T) {
	a, ok := arch.Get("raguel")
	if !ok || a.Nickname != brand.Ragu || a.Icon != "🤝" || a.Rank != 5 || !a.Call || a.Door != nil {
		t.Fatalf("raguel = %+v, %v", a, ok)
	}
	for _, s := range append(Seats, Efforts...) {
		if !strings.Contains(a.Charge, "`"+s+"`") {
			t.Errorf("the charge does not name %s", s)
		}
	}
	for _, cmd := range []string{"ctl council propose", "ctl council done", "ctl council tally --days N", "ctl council yes N", `{"summary": "..."}`} {
		if !strings.Contains(a.Charge, cmd) {
			t.Errorf("the charge does not say %q", cmd)
		}
	}
	pt := reflect.TypeOf(Plan{})
	for i := range pt.NumField() {
		name, _, _ := strings.Cut(pt.Field(i).Tag.Get("json"), ",")
		switch pt.Field(i).Name {
		case "N":
			continue // the core numbers the plans
		case "Said":
			return // the rest is the core's to keep
		}
		if !strings.Contains(a.Charge, "`"+name+"`") {
			t.Errorf("the charge does not name the plan's %s", name)
		}
	}
}

// TestARowKeepsItsEarlierNumberPastTheEarlierTop: a row of this stretch
// is shown beside its number before, though that ranked below the
// earlier list's top.
func TestARowKeepsItsEarlierNumberPastTheEarlierTop(t *testing.T) {
	now := []Row{{Name: "a", N: 9, Examples: []string{"a x"}}, {Name: "b", N: 8}, {Name: "c", N: 1}}
	before := []Row{{Name: "x", N: 7, Examples: []string{"x y"}}, {Name: "y", N: 6}, {Name: "z", N: 5}, {Name: "b", N: 1}}
	a, b := cut(now, before, 2)
	if !same(a, now[:2]) {
		t.Errorf("now = %+v", a)
	}
	if want := []Row{{Name: "x", N: 7}, {Name: "y", N: 6}, {Name: "b", N: 1}}; !same(b, want) {
		t.Errorf("before = %+v, want %+v", b, want)
	}
	var d doc
	d.list(a, b, 2, cols{n: "calls"})
	if got := d.b.String(); got != "- a: 9 calls (0). E.g. `a x`\n- b: 8 calls (1)\n" {
		t.Errorf("list:\n%s", got)
	}
}
