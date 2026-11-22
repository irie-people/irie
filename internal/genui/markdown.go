package genui

import (
	"fmt"
	"strings"
)

// Markdown draws a card as Markdown, for a file a person reads anywhere,
// as Obsidian, GitHub or an editor show it: each part as Markdown has it,
// and a Diagram as a Mermaid flowchart, which most of them draw. Irie
// writes the flowchart from the diagram's links; the model never writes
// markup. Every string is written as text, its marks escaped, so a card
// cannot become markup in the file.
func Markdown(c Card) string {
	var b strings.Builder
	b.WriteString("## " + inline(c.Title) + "\n\n")
	for _, p := range c.Body {
		switch p.Type {
		case PartText:
			t := inline(p.Text)
			switch p.Style {
			case "title":
				t = "**" + t + "**"
			case "caption":
				t = "*" + t + "*"
			case "mono":
				t = codeSpan(p.Text)
			}
			b.WriteString(t + "\n\n")
		case PartList:
			for _, it := range p.Items {
				b.WriteString("- " + mark(it.Status) + " **" + inline(it.Title) + "**")
				if it.Detail != "" {
					b.WriteString(" — " + inline(it.Detail))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		case PartFlow:
			for i, it := range p.Items {
				fmt.Fprintf(&b, "%d. **%s**", i+1, inline(it.Title))
				if it.Detail != "" {
					b.WriteString(" — " + inline(it.Detail))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		case PartFacts:
			rows := make([][]Cell, len(p.Facts))
			for i, f := range p.Facts {
				rows[i] = []Cell{Cell(f.Label), f.Value}
			}
			b.WriteString(mdTable([]Cell{"", ""}, rows))
		case PartTable:
			b.WriteString(mdTable(p.Columns, p.Rows))
		case PartCompare:
			cols := make([]Cell, len(p.Sides))
			n := 0
			for i, sd := range p.Sides {
				cols[i] = Cell(sd.Title)
				n = max(n, len(sd.Points))
			}
			rows := make([][]Cell, n)
			for r := range rows {
				rows[r] = make([]Cell, len(p.Sides))
				for i, sd := range p.Sides {
					if r < len(sd.Points) {
						rows[r][i] = sd.Points[r]
					}
				}
			}
			b.WriteString(mdTable(cols, rows))
		case PartProgress:
			label := inline(p.Label)
			if label == "" {
				label = "Progress"
			}
			fmt.Fprintf(&b, "%s: %s of %s\n\n", label, num(p.Value), num(p.Total))
		case PartDivider:
			b.WriteString("---\n\n")
		case PartCode:
			fence := "```"
			for strings.Contains(p.Text, fence) {
				fence += "`"
			}
			lang := strings.Map(func(r rune) rune {
				if r == '`' || r == '\n' || r == ' ' {
					return -1
				}
				return r
			}, p.Language)
			b.WriteString(fence + lang + "\n" + strings.TrimRight(p.Text, "\n") + "\n" + fence + "\n\n")
		case PartDiagram:
			b.WriteString(Mermaid(p.Links) + "\n")
		}
	}
	return b.String()
}

// Mermaid writes a diagram's links as a Mermaid flowchart in a fenced
// block, top to bottom: each thing a node by its place among them, its
// name a quoted label with the marks Mermaid reads written as its own
// codes, and each link's words a quoted label on its arrow.
func Mermaid(links []Link) string {
	var b strings.Builder
	b.WriteString("```mermaid\nflowchart TD\n")
	ids := map[string]string{}
	node := func(name string) string {
		name = strings.TrimSpace(name)
		if id, ok := ids[name]; ok {
			return id
		}
		id := fmt.Sprintf("n%d", len(ids))
		ids[name] = id
		return id + `["` + mermaidText(name) + `"]`
	}
	for _, l := range links {
		from, to := node(l.From), node(l.To)
		arrow := " --> "
		if strings.TrimSpace(l.Label) != "" {
			arrow = ` -->|"` + mermaidText(l.Label) + `"| `
		}
		b.WriteString("  " + from + arrow + to + "\n")
	}
	b.WriteString("```\n")
	return b.String()
}

// mermaidText is s for a quoted Mermaid label: on one line, with the marks
// Mermaid or HTML would read written as Mermaid's numbered codes.
func mermaidText(s string) string {
	var b strings.Builder
	for _, r := range strings.Join(strings.Fields(plain(s)), " ") {
		switch r {
		case '"', '<', '>', '&', '#', '`', '|':
			fmt.Fprintf(&b, "#%d;", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// inline is s as Markdown text on one line, its marks escaped.
func inline(s string) string {
	var b strings.Builder
	for _, r := range strings.Join(strings.Fields(plain(s)), " ") {
		if strings.ContainsRune("\\`*_[]<>#|!~", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// codeSpan is s as an inline code span, its fence longer than any run of
// backticks in it.
func codeSpan(s string) string {
	s = strings.Join(strings.Fields(plain(s)), " ")
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	return fence + " " + s + " " + fence
}

// mdTable is a Markdown table: columns as its head (blank for facts), then
// its rows, each cell as text on one line.
func mdTable(columns []Cell, rows [][]Cell) string {
	if len(columns) == 0 && len(rows) > 0 {
		columns = make([]Cell, len(rows[0]))
	}
	if len(columns) == 0 {
		return ""
	}
	var b strings.Builder
	line := func(cells []Cell) {
		b.WriteString("|")
		for i := range columns {
			c := Cell("")
			if i < len(cells) {
				c = cells[i]
			}
			b.WriteString(" " + inline(string(c)) + " |")
		}
		b.WriteString("\n")
	}
	line(columns)
	b.WriteString("|" + strings.Repeat(" --- |", len(columns)) + "\n")
	for _, r := range rows {
		line(r)
	}
	return b.String() + "\n"
}
