package genui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/irie-people/irie/internal/localmodel"
)

// MaxContext is the most of a card's context a model reads, in bytes:
// with the catalog and the card, it fits a context of 8,192 tokens.
const MaxContext = 20 << 10

// The kinds of card a model makes, each with its own instructions.
const (
	KindSummary = ""        // what its context says, at a glance, such as where a session stands
	KindExplain = "explain" // what the person asked to have explained, a picture first
)

// summary tells a model what a summary card is: the catalog, in short, and
// the rule that holds it to what it was given.
const summary = `You make one card: a short summary a person takes in at a glance, drawn by an app in its own look.
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

// explain tells a model what an explaining card is: pictures first, from
// the catalog's Flow, Diagram and Compare, with a few words beside them.
// It may explain from what it knows, but the work in its context it takes
// only as the context tells it.
const explain = `You explain one thing to a person who learns best from pictures. You answer with one card, which an app draws in its own look.
Answer with one JSON object and nothing else, in this shape:
{"title": "a few words", "body": [part, ...]}
Begin with one picture, the part that shows it best:
{"type": "Flow", "items": [{"title": "a step", "detail": "what happens in it"}]}   (steps in order, 2 to 6 of them)
{"type": "Diagram", "links": [{"from": "a thing", "to": "another", "label": "how they connect"}]}   (up to 12 links; the same name is the same thing)
{"type": "Compare", "sides": [{"title": "one", "points": ["...", "..."]}, {"title": "the other", "points": ["..."]}]}   (2 or 3 sides)
Then, where they help:
{"type": "Text", "text": "a sentence or two", "style": "body"}   (style: body or caption)
{"type": "Code", "language": "go", "text": "a few lines"}   (a short example, at most 12 lines)
{"type": "List", "items": [{"title": "...", "detail": "..."}]}
{"type": "Facts", "facts": [{"label": "...", "value": "..."}]}
Explain in plain words, as to a newcomer. The context, when there is one, is background: what the project's own docs say, and a conversation. Explain what is asked, not what the conversation did, unless that is what is asked. Take the project's words and how it works from its docs, over what you know, and never make up a name, a number or a fact about the work.
Keep it short: one picture and at most five parts in all, with no Markdown and no links.`

// schema is the JSON schema a kind of card's reply is held to, through
// Ollama's structured outputs: a title over a body of parts, each part an
// object with a type from those the kind's instructions offer. A small
// model then cannot write a part as a bare sentence, or leave its type
// out, as bonsai-2:9b did about one card in five with JSON mode alone.
func schema(kind string) json.RawMessage {
	str := map[string]any{"type": "string"}
	strs := map[string]any{"type": "array", "items": str}
	num := map[string]any{"type": "number"}
	obj := func(required []string, props map[string]any) map[string]any {
		return map[string]any{"type": "object", "required": required, "properties": props}
	}
	list := func(item map[string]any) map[string]any { return map[string]any{"type": "array", "items": item} }
	props := map[string]any{
		"text":  str,
		"style": str,
		"items": list(obj([]string{"title"}, map[string]any{"title": str, "detail": str, "status": str})),
		"facts": list(obj([]string{"label", "value"}, map[string]any{"label": str, "value": str})),
	}
	types := []string{PartText, PartList, PartFacts, PartTable, PartProgress, PartDivider}
	if kind == KindExplain {
		types = []string{PartFlow, PartDiagram, PartCompare, PartCode, PartText, PartList, PartFacts}
		props["language"] = str
		props["links"] = list(obj([]string{"from", "to"}, map[string]any{"from": str, "to": str, "label": str}))
		props["sides"] = list(obj([]string{"title", "points"}, map[string]any{"title": str, "points": strs}))
	} else {
		props["columns"], props["rows"] = strs, list(strs)
		props["label"], props["value"], props["total"] = str, num, num
	}
	props["type"] = map[string]any{"type": "string", "enum": types}
	b, _ := json.Marshal(obj([]string{"title", "body"}, map[string]any{"title": str, "body": list(obj([]string{"type"}, props))}))
	return b
}

// instructions are the instructions for a kind of card; summary for one
// it does not know.
func instructions(kind string) string {
	if kind == KindExplain {
		return explain
	}
	return summary
}

// Make has the local model m make the card prompt asks for, of the kind
// given, from text alone, the card's context, and gives the card's JSON
// as the catalog reads it (Check), compacted, once it passes. A card that does not is asked for once
// more, with what to fix, since a small model often gets it right the
// second time. m runs on this computer (localmodel.FindChatModel never
// picks a cloud model), so a card costs no tokens. The caller bounds how
// long it may take with ctx.
func Make(ctx context.Context, m localmodel.ChatModel, kind, prompt, text string) (json.RawMessage, error) {
	if len(text) > MaxContext {
		text = strings.ToValidUTF8(text[:MaxContext], "") + "\n[cut short]"
	}
	ask := "The card to make: " + prompt
	if strings.TrimSpace(text) != "" {
		ask += "\n\nThe context:\n" + text
		if kind == KindExplain {
			// What is asked goes last too, after the background, where a
			// small model attends to it.
			ask = "The context, for background:\n" + text + "\n\nThe card to make: " + prompt
		}
	}
	opt := localmodel.ChatOptions{Temperature: 0.2, Context: 8192, Keep: "10m", JSON: true, Schema: schema(kind)}
	if m.Thinks() {
		off := false
		opt.Think = &off
	}
	o := localmodel.Ollama{URL: m.URL}
	msgs := []localmodel.ChatMessage{{Role: "system", Content: instructions(kind)}, {Role: "user", Content: ask}}
	var err error
	for try := 0; try < 2; try++ {
		var reply string
		if reply, err = o.Chat(ctx, m.Name, msgs, opt, nil); err != nil {
			return nil, err
		}
		var spec json.RawMessage
		if spec, err = cardJSON(reply); err == nil {
			var c Card
			if c, err = Check(spec); err == nil {
				// Kept as the catalog reads it, every part with its type,
				// so a view that needs one draws it.
				return json.Marshal(c)
			}
		}
		msgs = append(msgs,
			localmodel.ChatMessage{Role: "assistant", Content: reply},
			localmodel.ChatMessage{Role: "user", Content: "That card does not draw: " + err.Error() + ". Write the whole card again, with that fixed."})
	}
	return nil, fmt.Errorf("%s: %w", m.Name, err)
}

// cardJSON is the JSON object in a model's reply, compacted. What a model
// writes around it, such as its thinking or a code fence, is left out:
// the object runs from the first brace to the last.
func cardJSON(reply string) (json.RawMessage, error) {
	reply = localmodel.StripThinking(reply)
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
