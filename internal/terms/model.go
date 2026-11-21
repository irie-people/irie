package terms

import (
	"context"
	"errors"
	"fmt"
	"github.com/irie-people/irie/internal/brand"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/localmodel"
)

// PagePort is the port the core serves Terms in short's page on, for the
// Mac app's Terms tab: http://127.0.0.1:7393/.
const PagePort = 7393

// PageAddress is the page's address.
var PageAddress = "http://127.0.0.1:" + strconv.Itoa(PagePort) + "/"

// Servers are the Ollama servers a model may word answers on, the first
// that answers taking it: local.url, else Irie's own, then Ollama's usual
// port (localmodel.ChatServers).
func Servers(cfg config.Config) []string {
	return localmodel.ChatServers(cfg.Local.URL, cfg.Local.Port)
}

// Model is a local model that words answers: one on this computer, from
// an Ollama server here, so no tokens are bought (localmodel.ChatModel,
// which materialized cards share).
type Model = localmodel.ChatModel

// ErrNoModel says no local model is here to word answers.
var ErrNoModel = localmodel.ErrNoChatModel

// FindModel finds a local model to word answers: want, by its name, else
// the best for chat that fits (localmodel.FindChatModel). A cloud model is
// never one: it runs on someone else's computer, at a price.
func FindModel(ctx context.Context, urls []string, want string) (Model, error) {
	return localmodel.FindChatModel(ctx, urls, want)
}

// maxPassages is how many passages a model answers from.
const maxPassages = 6

const system = `You explain a website's terms of service to the person who is asked to agree to them.
Answer their question only from the numbered passages of the terms below. Never add anything the passages don't say.
Write in plain, friendly words, as you would to a friend: two to four short sentences, no lists, no headings.
Say what it means for them, and name the passage you rely on, such as [2].
If the passages don't answer the question, say that the terms don't seem to say, and what comes closest.
This is not legal advice, and you don't say it is.`

// AskModel answers a question with a local model's words: Irie's code
// finds the passages and the points on the question, and the model words
// the answer from them alone. part, when set, gets the words as they come.
// The answer quotes the passages, so its words can be checked.
func (d *Doc) AskModel(ctx context.Context, m Model, question string, part func(string)) (Answer, error) {
	a := d.Ask(question)
	ps := d.Passages(question, maxPassages)
	if len(ps) == 0 {
		return a, nil
	}
	var b strings.Builder
	name := d.Title
	if d.Site != "" {
		name += " (" + d.Site + ")"
	}
	fmt.Fprintf(&b, "Terms: %s\n\nPassages:\n", name)
	for i, p := range ps {
		sec := ""
		if p.Section != "" {
			sec = " (" + p.Section + ")"
		}
		fmt.Fprintf(&b, "[%d]%s %s\n\n", i+1, sec, p.Text)
	}
	if a.Found {
		fmt.Fprintf(&b, "%s's reading of these terms found: %s\n\n", brand.Name, a.Says)
	}
	fmt.Fprintf(&b, "Question: %s", a.Question)
	o := localmodel.Ollama{URL: m.URL}
	opt := localmodel.ChatOptions{Temperature: 0.2, Context: 8192, Keep: "10m"}
	if m.Thinks() {
		off := false
		opt.Think = &off
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// What a model writes is printed and shown as it comes: without
	// control characters, as a page's words are.
	clean := part
	if part != nil {
		clean = func(s string) {
			if s = Clean(s); s != "" {
				part(s)
			}
		}
	}
	text, err := o.Chat(ctx, m.Name, []localmodel.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: b.String()}}, opt, clean)
	text = strings.TrimSpace(Clean(StripThinking(text)))
	if err != nil || text == "" {
		if err == nil {
			err = errors.New(m.Name + " said nothing")
		}
		return a, err
	}
	a.Says = text
	a.By = m.Name
	a.Found = true
	a.Quotes = a.Quotes[:0]
	for _, p := range ps {
		if len(p.Text) > maxQuote {
			p.Text = clip(p.Text, maxQuote)
		}
		a.Quotes = append(a.Quotes, p)
	}
	return a, nil
}

// StripThinking takes out a model's thinking, which some write between
// <think> tags in the answer itself (localmodel.StripThinking).
func StripThinking(s string) string { return localmodel.StripThinking(s) }
