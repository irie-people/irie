// Package genui is Irie's catalog of card parts, the generative UI of
// docs/proposals/generative-ui.md §7.3: the JSON a model writes for a
// card, the check it must pass before Irie keeps or draws it, and a card
// drawn as plain text for a terminal. Irie owns the look: a model picks
// parts from the catalog and fills them with words and numbers, and never
// writes markup, styles or code. Even a Diagram is data, the links between
// things, which Irie lays out and draws itself.
package genui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Catalog is the catalog's version. It is part of the recipe of every
// card a model makes (internal/materialize), so a new version leaves every
// kept card cold, to be made again with what the new one holds.
const Catalog = 2

// Limits on a card, past which it is refused, never cut short.
const (
	MaxBytes = 32 << 10 // the card's JSON
	MaxParts = 200      // the parts of its body
	MaxRows  = 100      // the rows of a table, the items of a list or a flow, and a side's points
	MaxLinks = 24       // the links of a diagram
	MaxNodes = 16       // the things a diagram's links join
	MaxSides = 4        // the sides of a comparison
)

// The parts of the catalog, named after A2UI's basic catalog where the two
// overlap. Flow, Diagram and Compare are pictures, for a card that
// explains (KindExplain); Code is a short example.
const (
	PartText     = "Text"
	PartList     = "List"
	PartFacts    = "Facts"
	PartTable    = "Table"
	PartProgress = "Progress"
	PartDivider  = "Divider"
	PartFlow     = "Flow"    // steps in order, joined by arrows: Items
	PartDiagram  = "Diagram" // how things connect: Links
	PartCompare  = "Compare" // two to four things side by side: Sides
	PartCode     = "Code"    // a short example: Text, in Language
)

var parts = []string{PartText, PartList, PartFacts, PartTable, PartProgress, PartDivider, PartFlow, PartDiagram, PartCompare, PartCode}

// Card is a card: a title over the parts of its body, in order.
type Card struct {
	Title string `json:"title"`
	Body  []Part `json:"body"`
}

// Part is one part of a card. Type says which, and which of the rest it
// uses; a view skips a field it does not know, so a newer catalog's cards
// still draw in an older view.
type Part struct {
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`     // Text, Code
	Style    string   `json:"style,omitempty"`    // Text: title, body, caption or mono; "" is body
	Items    []Item   `json:"items,omitempty"`    // List, Flow
	Facts    []Fact   `json:"facts,omitempty"`    // Facts
	Columns  []Cell   `json:"columns,omitempty"`  // Table
	Rows     [][]Cell `json:"rows,omitempty"`     // Table
	Label    string   `json:"label,omitempty"`    // Progress
	Value    Number   `json:"value,omitempty"`    // Progress
	Total    Number   `json:"total,omitempty"`    // Progress
	Links    []Link   `json:"links,omitempty"`    // Diagram
	Sides    []Side   `json:"sides,omitempty"`    // Compare
	Language string   `json:"language,omitempty"` // Code: such as go or swift; "" when it is not code of one
}

// UnmarshalJSON reads a part as a model writes it: an object of the
// catalog; or, as a small model will, a sentence alone, which is a Text
// part, or an object that left its type out, which takes the type its
// fields name (inferred). What a model meant is plain in both, as a
// number written "3" is (Number), so the card is taken, not refused.
func (p *Part) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*p = Part{Type: PartText, Text: s}
		return nil
	}
	type plain Part
	var q plain
	if err := json.Unmarshal(b, &q); err != nil {
		return err
	}
	*p = Part(q)
	if p.Type == "" {
		p.Type = p.inferred()
	}
	return nil
}

// inferred is the type a part's fields name, for a part that left its
// type out: "" when they name none. Items are a List's, not a Flow's,
// which would draw an order the model never said.
func (p Part) inferred() string {
	switch {
	case len(p.Links) > 0:
		return PartDiagram
	case len(p.Sides) > 0:
		return PartCompare
	case len(p.Facts) > 0:
		return PartFacts
	case len(p.Rows) > 0 || len(p.Columns) > 0:
		return PartTable
	case len(p.Items) > 0:
		return PartList
	case p.Language != "":
		return PartCode
	case p.Label != "" && p.Total != 0:
		return PartProgress
	case p.Text != "":
		return PartText
	}
	return ""
}

// Item is one line of a List, or one step of a Flow: a title, a detail
// beside it, and a status mark: ok, warn, note or none.
type Item struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Status string `json:"status,omitempty"`
}

// Fact is a label and its value.
type Fact struct {
	Label string `json:"label"`
	Value Cell   `json:"value"`
}

// Link is one line of a Diagram: from one thing to another, by their
// names, with a few words on how. Things of the same name are one thing.
type Link struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// Side is one side of a Compare: what it is, and its points.
type Side struct {
	Title  string `json:"title"`
	Points []Cell `json:"points,omitempty"`
}

// Cell is a table's cell or a fact's value: words, though a model may
// write a number or true for one, which is taken as it is written.
type Cell string

// UnmarshalJSON takes a string as it is, and any other value as written.
func (c *Cell) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*c = Cell(s)
		return nil
	}
	if string(b) == "null" {
		*c = ""
		return nil
	}
	if b = bytes.TrimSpace(b); len(b) > 0 && (b[0] == '{' || b[0] == '[') {
		return fmt.Errorf("a cell holds words or a number, not %s", b)
	}
	*c = Cell(b)
	return nil
}

// Number is a progress figure, which a model may write as "3" as well as 3.
type Number float64

// UnmarshalJSON takes a number, or a string that holds one.
func (n *Number) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = Number(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("not a number: %s", b)
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return fmt.Errorf("not a number: %q", s)
	}
	*n = Number(f)
	return nil
}

// Check reads spec as a card of the catalog, or says what to fix: a card
// past a limit, one with no title, or a part the catalog does not have.
// The words say how to put it right, so that a model told them can.
func Check(spec []byte) (Card, error) {
	if len(spec) > MaxBytes {
		return Card{}, fmt.Errorf("the card is %d bytes, and at most %d fit", len(spec), MaxBytes)
	}
	var c Card
	if err := json.Unmarshal(spec, &c); err != nil {
		return Card{}, fmt.Errorf("the card is not one JSON object of the catalog: %w", err)
	}
	if strings.TrimSpace(c.Title) == "" {
		return Card{}, errors.New("the card has no title")
	}
	if len(c.Body) > MaxParts {
		return Card{}, fmt.Errorf("the card has %d parts, and at most %d fit", len(c.Body), MaxParts)
	}
	for i, p := range c.Body {
		switch {
		case p.Type == "":
			return Card{}, fmt.Errorf("part %d has no type and nothing to show: give each part a type, one of %s", i+1, strings.Join(parts, ", "))
		case !slices.Contains(parts, p.Type):
			return Card{}, fmt.Errorf("part %d is a %q, which the catalog does not have: use %s", i+1, p.Type, strings.Join(parts, ", "))
		case len(p.Rows) > MaxRows || len(p.Items) > MaxRows:
			return Card{}, fmt.Errorf("part %d has more than %d rows", i+1, MaxRows)
		case len(p.Sides) > MaxSides:
			return Card{}, fmt.Errorf("part %d compares %d sides, and at most %d fit side by side", i+1, len(p.Sides), MaxSides)
		}
		for _, sd := range p.Sides {
			if len(sd.Points) > MaxRows {
				return Card{}, fmt.Errorf("part %d has a side of more than %d points", i+1, MaxRows)
			}
		}
		if err := checkLinks(p.Links); err != nil {
			return Card{}, fmt.Errorf("part %d: %w", i+1, err)
		}
	}
	return c, nil
}

// checkLinks says what to fix in a diagram's links: too many of them, too
// many things joined, or a link with an end left out, which would draw as
// an arrow from nothing.
func checkLinks(links []Link) error {
	if len(links) > MaxLinks {
		return fmt.Errorf("the diagram has %d links, and at most %d fit", len(links), MaxLinks)
	}
	nodes := map[string]bool{}
	for j, l := range links {
		from, to := strings.TrimSpace(l.From), strings.TrimSpace(l.To)
		if from == "" || to == "" {
			return fmt.Errorf("link %d of the diagram needs both a from and a to", j+1)
		}
		nodes[from], nodes[to] = true, true
	}
	if len(nodes) > MaxNodes {
		return fmt.Errorf("the diagram joins %d things, and at most %d fit", len(nodes), MaxNodes)
	}
	return nil
}
