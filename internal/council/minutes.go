package council

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/roots"
)

// Minutes are a council's minutes, written once it is done: page, an HTML
// page to read on a phone, and md, Markdown for an agent. Each holds the
// council's summary, then its plans, then the tally it read, then what it
// cost. The page is whole in itself, with nothing to fetch, and follows
// the light or dark appearance of whatever shows it, in the design
// library's colours.
func Minutes(c Council, t Tally) (page, md []byte, err error) {
	title := "Raguel's council, " + c.Date
	var m strings.Builder
	fmt.Fprintf(&m, "# %s\n\n%s\n\n## The plans\n\n", title, strings.TrimSpace(c.Summary))
	if len(c.Plans) == 0 {
		m.WriteString("The council brought no plans this time.\n\n")
	}
	for _, p := range c.Plans {
		fmt.Fprintf(&m, "### %d. %s\n\n%s · Saves: %s · Effort: %s · %s\n\n", p.N, p.Title, Seat(p.Seat), oneLine(p.Saves, 400), p.Effort, saidOf(p))
		for _, f := range []struct{ name, v string }{{"Evidence", p.Evidence}, {"Change", p.Change}, {"Risk", p.Risk}, {"Watch", p.Watch}, {"Where", p.Where}} {
			if v := strings.TrimSpace(f.v); v != "" {
				fmt.Fprintf(&m, "**%s.** %s\n\n", f.name, v)
			}
		}
		m.WriteString("**Brief.**\n\n")
		for _, l := range strings.Split(strings.TrimSpace(p.Brief), "\n") {
			m.WriteString(strings.TrimRight("> "+l, " ") + "\n")
		}
		m.WriteString("\n")
	}
	if len(c.Plans) > 0 {
		m.WriteString(answer + "\n\n")
	}
	m.WriteString(render(t, 2).b.String())
	fmt.Fprintf(&m, "\n## What the council cost\n\n%s\n", costOf(c))

	type plan struct {
		N                                           int
		Seat, Title, Saves, Effort, Said            string
		Evidence, Change, Risk, Watch, Where, Brief string
	}
	data := struct {
		Title, When, Summary, Answer, Tally, Cost string
		Style                                     template.CSS
		Plans                                     []plan
	}{Title: title, Summary: strings.TrimSpace(c.Summary), Answer: answer, Tally: Markdown(t), Cost: costOf(c), Style: style()}
	data.When = fmt.Sprintf("%s to %s, beside the %s before", day(t.Now.From), day(t.Now.To), counted(t.Days, "day"))
	for _, p := range c.Plans {
		data.Plans = append(data.Plans, plan{N: p.N, Seat: Seat(p.Seat), Title: p.Title, Saves: p.Saves, Effort: p.Effort, Said: saidOf(p),
			Evidence: p.Evidence, Change: p.Change, Risk: p.Risk, Watch: p.Watch, Where: p.Where, Brief: p.Brief})
	}
	var b bytes.Buffer
	if err := minutesPage.Execute(&b, data); err != nil {
		return nil, nil, err
	}
	return b.Bytes(), []byte(m.String()), nil
}

// answer says how the user answers the plans.
const answer = "Answer each plan in the Arch Agents tab, with `" + brand.Slug + " ctl council yes N` or `" + brand.Slug + " ctl council no N --why …` in a terminal of your own, or by telling Ragu in the council's card. A yes starts the plan's brief as a jot."

// saidOf is what the user said to a plan, in a few words.
func saidOf(p Plan) string {
	switch p.Said {
	case Yes:
		return "Yes: started as a jot."
	case No:
		return "No."
	case Lapsed:
		return "Lapsed."
	}
	return "Waiting for your word."
}

// costOf is what the council cost, in a sentence.
func costOf(c Council) string {
	if c.CostUSD == nil {
		return "Not known: its session's status line said nothing of its cost."
	}
	return usd(*c.CostUSD) + ", by its session's status line, at list price."
}

// style is the page's colours, from the design library's default palette,
// light and dark.
func style() template.CSS {
	p, ok := roots.PaletteByID(roots.Default())
	if !ok {
		p = roots.Palettes()[0]
	}
	vars := func(s roots.Set) string {
		return fmt.Sprintf("--ground:%s;--card:%s;--ink:%s;--muted:%s;--accent:%s;--edge:%s", s["ground"], s["card"], s["ink"], s["muted"], s["accent_ink"], s["edge"])
	}
	return template.CSS(":root{color-scheme:light dark;" + vars(p.Mode(false)) + "}@media (prefers-color-scheme:dark){:root{" + vars(p.Mode(true)) + "}}" + pageCSS)
}

// pageCSS lays the page out for a phone first.
const pageCSS = `
body{margin:0;background:var(--ground);color:var(--ink);font:17px/1.5 -apple-system,BlinkMacSystemFont,"SF Pro Text",system-ui,sans-serif;-webkit-text-size-adjust:100%}
main{max-width:44rem;margin:0 auto;padding:20px 16px 48px}
h1{font-size:1.5rem;line-height:1.25;margin:8px 0 4px}
.when,.seat,.cost,.answer{color:var(--muted)}
.when{margin:0 0 20px;font-size:.9rem}
.summary{white-space:pre-wrap}
article{background:var(--card);border:1px solid var(--edge);border-radius:16px;padding:16px;margin:16px 0}
article h2{font-size:1.15rem;line-height:1.3;margin:4px 0 8px}
.seat{font-size:.9rem}
.lead{font-weight:600;margin:0 0 8px}
dl{margin:0}dt{font-weight:600;margin-top:10px;color:var(--accent)}dd{margin:2px 0 0;white-space:pre-wrap;overflow-wrap:anywhere}
details{margin-top:10px}summary{cursor:pointer;color:var(--accent);font-weight:600}
pre{white-space:pre-wrap;overflow-wrap:anywhere;font:13px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace;background:var(--card);border:1px solid var(--edge);border-radius:12px;padding:12px}
.answer{font-size:.95rem}
`

var minutesPage = template.Must(template.New("minutes").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p class="when">{{.When}}</p>
<p class="summary">{{.Summary}}</p>
{{range .Plans}}<article>
<div class="seat">{{.Seat}} · {{.Said}}</div>
<h2>{{.N}}. {{.Title}}</h2>
<p class="lead">Saves {{.Saves}} · Effort: {{.Effort}}</p>
<dl>
<dt>Evidence</dt><dd>{{.Evidence}}</dd>
<dt>Change</dt><dd>{{.Change}}</dd>
{{if .Risk}}<dt>Risk</dt><dd>{{.Risk}}</dd>
{{end}}<dt>Watch</dt><dd>{{.Watch}}</dd>
<dt>Where</dt><dd>{{.Where}}</dd>
</dl>
<details><summary>The brief</summary><pre>{{.Brief}}</pre></details>
</article>
{{else}}<p>The council brought no plans this time.</p>
{{end}}{{if .Plans}}<p class="answer">{{.Answer}}</p>
{{end}}<details><summary>The tally</summary><pre>{{.Tally}}</pre></details>
<p class="cost">What the council cost: {{.Cost}}</p>
</main>
</body>
</html>
`))
