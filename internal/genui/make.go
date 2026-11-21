package genui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/terms"
)

// MaxContext is the most of a card's context a model reads, in bytes:
// with the catalog and the card, it fits a context of 8,192 tokens.
const MaxContext = 20 << 10

// system tells a model what a card is: the catalog, in short, and the
// rule that holds it to what it was given.
const system = `You make one card: a short summary a person takes in at a glance, drawn by an app in its own look.
Answer with one JSON object and nothing else, in this shape:
{"title": "a few words", "body": [part, ...]}
Each part is one of these:
{"type": "Text", "text": "a sentence or two", "style": "body"}   (style: title, body, caption or mono)
{"type": "List", "items": [{"title": "...", "detail": "...", "status": "ok"}]}   (status: ok, warn, note or none)
{"type": "Facts", "facts": [{"label": "...", "value": "..."}]}
{"type": "Table", "columns": ["...", "..."], "rows": [["...", "..."]]}
{"type": "Progress", "label": "...", "value": 3, "total": 10}
{"type": "Divider"}
Use only what the context says, and never make up a name, a number or a fact.
Keep it short: at most eight parts, in plain words, with no Markdown and no links.`

// Make has the local model m make the card prompt asks for, from text
// alone, the card's context, and gives the card's JSON, compacted, once it
// passes Check. m runs on this computer (terms.FindModel never picks a
// cloud model), so a card costs no tokens. The caller bounds how long it
// may take with ctx.
func Make(ctx context.Context, m terms.Model, prompt, text string) (json.RawMessage, error) {
	if len(text) > MaxContext {
		text = strings.ToValidUTF8(text[:MaxContext], "") + "\n[cut short]"
	}
	ask := "The card to make: " + prompt
	if strings.TrimSpace(text) != "" {
		ask += "\n\nThe context:\n" + text
	}
	opt := localmodel.ChatOptions{Temperature: 0.2, Context: 8192, Keep: "10m", JSON: true}
	if m.Thinks() {
		off := false
		opt.Think = &off
	}
	o := localmodel.Ollama{URL: m.URL}
	reply, err := o.Chat(ctx, m.Name, []localmodel.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: ask}}, opt, nil)
	if err != nil {
		return nil, err
	}
	spec, err := cardJSON(reply)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.Name, err)
	}
	if _, err := Check(spec); err != nil {
		return nil, fmt.Errorf("%s: %w", m.Name, err)
	}
	return spec, nil
}

// cardJSON is the JSON object in a model's reply, compacted. What a model
// writes around it, such as its thinking or a code fence, is left out:
// the object runs from the first brace to the last.
func cardJSON(reply string) (json.RawMessage, error) {
	reply = terms.StripThinking(reply)
	i, j := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if i < 0 || j < i {
		return nil, errors.New("it wrote no JSON object")
	}
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(reply[i:j+1])); err != nil {
		return nil, fmt.Errorf("its card is not JSON: %w", err)
	}
	return b.Bytes(), nil
}
