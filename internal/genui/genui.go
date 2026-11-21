// Package genui is Irie's catalog of card parts, the generative UI of
// docs/proposals/generative-ui.md §7.3: the JSON a model writes for a
// card, the check it must pass before Irie keeps or draws it, and a card
// drawn as plain text for a terminal. Irie owns the look: a model picks
// parts from the catalog and fills them with words and numbers, and never
// writes markup, styles or code.
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
const Catalog = 1

// Limits on a card, past which it is refused, never cut short.
const (
	MaxBytes = 32 << 10 // the card's JSON
	MaxParts = 200      // the parts of its body
	MaxRows  = 100      // the rows of a table, and the items of a list
)

// The parts of the catalog, named after A2UI's basic catalog where the two
// overlap.
const (
	PartText     = "Text"
	PartList     = "List"
	PartFacts    = "Facts"
	PartTable    = "Table"
	PartProgress = "Progress"
	PartDivider  = "Divider"
)

var parts = []string{PartText, PartList, PartFacts, PartTable, PartProgress, PartDivider}

// Card is a card: a title over the parts of its body, in order.
type Card struct {
	Title string `json:"title"`
	Body  []Part `json:"body"`
}

// Part is one part of a card. Type says which, and which of the rest it
// uses; a view skips a field it does not know, so a newer catalog's cards
// still draw in an older view.
type Part struct {
	Type    string   `json:"type"`
	Text    string   `json:"text,omitempty"`    // Text
	Style   string   `json:"style,omitempty"`   // Text: title, body, caption or mono; "" is body
	Items   []Item   `json:"items,omitempty"`   // List
	Facts   []Fact   `json:"facts,omitempty"`   // Facts
	Columns []Cell   `json:"columns,omitempty"` // Table
	Rows    [][]Cell `json:"rows,omitempty"`    // Table
	Label   string   `json:"label,omitempty"`   // Progress
	Value   Number   `json:"value,omitempty"`   // Progress
	Total   Number   `json:"total,omitempty"`   // Progress
}

// Item is one line of a List: a title, a detail beside it, and a status
// mark: ok, warn, note or none.
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
		case !slices.Contains(parts, p.Type):
			return Card{}, fmt.Errorf("part %d is a %q, which the catalog does not have: use %s", i+1, p.Type, strings.Join(parts, ", "))
		case len(p.Rows) > MaxRows || len(p.Items) > MaxRows:
			return Card{}, fmt.Errorf("part %d has more than %d rows", i+1, MaxRows)
		}
	}
	return c, nil
}
