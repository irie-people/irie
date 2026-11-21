package genui

import (
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// barWidth is how many cells wide Text draws a Progress bar.
const barWidth = 20

// Text draws a card as plain text, at most width columns wide, as the
// terminal slot will (docs/proposals/generative-ui.md §7.3): its title over
// a rule, then each part. Every string is drawn as words, with control
// characters and the marks that turn text around shown as ^X or ^?, so a
// card cannot hide or rewrite what is around it.
func Text(c Card, width int) string {
	width = max(width, 24)
	var b strings.Builder
	title := plain(c.Title)
	b.WriteString(cut(title, width) + "\n")
	b.WriteString(strings.Repeat("─", min(width, utf8.RuneCountInString(title))) + "\n")
	for _, p := range c.Body {
		switch p.Type {
		case PartText:
			indent := ""
			if p.Style == "caption" {
				indent = "  "
			}
			for _, para := range strings.Split(p.Text, "\n") {
				for _, l := range wrap(plain(para), width-len(indent)) {
					b.WriteString(indent + l + "\n")
				}
			}
		case PartList:
			for _, it := range p.Items {
				line := mark(it.Status) + " " + plain(it.Title)
				if it.Detail != "" {
					line += " — " + plain(it.Detail)
				}
				for i, l := range wrap(line, width-2) {
					if i > 0 {
						l = "  " + l
					}
					b.WriteString(l + "\n")
				}
			}
		case PartFacts:
			rows := make([][]Cell, len(p.Facts))
			for i, f := range p.Facts {
				rows[i] = []Cell{Cell(f.Label), f.Value}
			}
			b.WriteString(table(nil, rows, width))
		case PartTable:
			b.WriteString(table(p.Columns, p.Rows, width))
		case PartProgress:
			line := bar(float64(p.Value), float64(p.Total), barWidth) + " " + num(p.Value) + " of " + num(p.Total)
			if p.Label != "" {
				line = plain(p.Label) + "  " + line
			}
			b.WriteString(cut(line, width) + "\n")
		case PartDivider:
			b.WriteString(strings.Repeat("─", width) + "\n")
		}
	}
	return b.String()
}

// bar draws value of total as a bar width cells wide: █ for what is done
// and ░ for the rest. A total of 0 or less draws it empty, and a value past
// the total draws it full.
func bar(value, total float64, width int) string {
	if total <= 0 || math.IsNaN(value) || math.IsNaN(total) {
		return strings.Repeat("░", width)
	}
	n := int(math.Round(min(max(value/total, 0), 1) * float64(width)))
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

// table lines up columns (none for a list of facts) over rows, two spaces
// apart, each line cut to width.
func table(columns []Cell, rows [][]Cell, width int) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	line := func(cells []Cell) {
		for i, c := range cells {
			if i > 0 {
				tw.Write([]byte("\t"))
			}
			tw.Write([]byte(plain(string(c))))
		}
		tw.Write([]byte("\n"))
	}
	if len(columns) > 0 {
		line(columns)
	}
	for _, r := range rows {
		line(r)
	}
	tw.Flush()
	var out strings.Builder
	for _, l := range strings.SplitAfter(b.String(), "\n") {
		if l = strings.TrimRight(l, " \n"); l != "" {
			out.WriteString(cut(l, width) + "\n")
		}
	}
	return out.String()
}

// mark is a list item's status mark.
func mark(status string) string {
	switch status {
	case "ok":
		return "✓"
	case "warn":
		return "!"
	case "note":
		return "i"
	}
	return "•"
}

// num writes a figure as briefly as it reads: 3, 2.5.
func num(n Number) string { return strconv.FormatFloat(float64(n), 'f', -1, 64) }

// plain is s as words for one line: a line break or a tab as a space, any
// other control character as ^X, and DEL, the C1 controls and the marks
// that turn text around (bidi) as ^?.
func plain(s string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "�") {
		switch {
		case r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20:
			b.WriteByte('^')
			b.WriteRune(r + 0x40)
		case r == 0x7f, r >= 0x80 && r < 0xa0, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			b.WriteString("^?")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// wrap breaks s into lines of at most width runes, between words where it
// can, and within a word longer than a line.
func wrap(s string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		for utf8.RuneCountInString(w) > width {
			if line != "" {
				lines, line = append(lines, line), ""
			}
			r := []rune(w)
			lines, w = append(lines, string(r[:width])), string(r[width:])
		}
		switch {
		case line == "":
			line = w
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) <= width:
			line += " " + w
		default:
			lines, line = append(lines, line), w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// cut is s at most width runes long, with … where it was cut.
func cut(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	return string([]rune(s)[:width-1]) + "…"
}
