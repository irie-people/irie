package terms

import (
	"context"
	"errors"
	"fmt"
	"github.com/irie-people/irie/internal/brand"
	"slices"
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
// port.
func Servers(cfg config.Config) []string {
	if cfg.Local.URL != "" {
		return []string{cfg.Local.URL}
	}
	return []string{"http://127.0.0.1:" + strconv.Itoa(cfg.Local.Port), "http://127.0.0.1:11434"}
}

// Model is a local model that words answers: one on this computer, from
// an Ollama server here, so no tokens are bought.
type Model struct {
	URL   string `json:"url"`  // its Ollama server
	Name  string `json:"name"` // such as qwen3.5:9b
	think bool   // it thinks before it answers, which answers turn off
}

// Thinks reports whether the model thinks before it answers, which a
// caller that wants only the answer turns off (localmodel.ChatOptions.Think).
func (m Model) Thinks() bool { return m.think }

// ErrNoModel says no local model is here to word answers.
var ErrNoModel = errors.New("no local model here to word answers")

// maxModelGB is the most a model may take on disk to word answers: one
// bigger takes the memory the person's own work needs.
const maxModelGB = 10

// FindModel finds a local model to word answers: want, by its name, on the
// first of the servers at urls that has it, else the best model for chat
// that the first server to answer has. A cloud model is never one: it
// runs on someone else's computer, at a price.
func FindModel(ctx context.Context, urls []string, want string) (Model, error) {
	var tried []string
	for _, u := range urls {
		o := localmodel.Ollama{URL: u}
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := o.Version(c)
		cancel()
		if err != nil {
			tried = append(tried, u)
			continue
		}
		c, cancel = context.WithTimeout(ctx, 5*time.Second)
		models, err := o.Models(c)
		cancel()
		if err != nil {
			return Model{}, err
		}
		models = slices.DeleteFunc(models, func(m localmodel.Model) bool { return m.Remote })
		if want != "" {
			i := slices.IndexFunc(models, func(m localmodel.Model) bool { return m.Name == want || strings.TrimSuffix(m.Name, ":latest") == want })
			if i < 0 {
				continue
			}
			return model(ctx, o, models[i].Name)
		}
		// The biggest that fits, by its parameters, of those that chat.
		slices.SortStableFunc(models, func(a, b localmodel.Model) int {
			pa, pb := params(a.Params), params(b.Params)
			switch {
			case pa > pb:
				return -1
			case pa < pb:
				return 1
			}
			return int(b.Size/1e6 - a.Size/1e6)
		})
		for _, m := range models {
			if m.Size > maxModelGB<<30 || params(m.Params) > 16 || strings.Contains(m.Name, "embed") {
				continue
			}
			got, err := model(ctx, o, m.Name)
			if err == nil {
				return got, nil
			}
		}
		return Model{}, fmt.Errorf("%w: the Ollama at %s has no model for chat that fits; `ollama pull qwen3:4b` gets a small one", ErrNoModel, u)
	}
	if want != "" {
		return Model{}, fmt.Errorf("%w: no Ollama here has %s (tried %s)", ErrNoModel, want, strings.Join(urls, ", "))
	}
	return Model{}, fmt.Errorf("%w: no Ollama answers at %s", ErrNoModel, strings.Join(tried, ", "))
}

// model checks a model can chat.
func model(ctx context.Context, o localmodel.Ollama, name string) (Model, error) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := o.Show(c, name)
	if err != nil {
		return Model{}, err
	}
	if !slices.Contains(info.Capabilities, "completion") {
		return Model{}, fmt.Errorf("%s doesn't write text", name)
	}
	return Model{URL: o.URL, Name: name, think: slices.Contains(info.Capabilities, "thinking")}, nil
}

// params is a model's size in billions of parameters, from Ollama's "9.7B"
// or "270M"; 0 when it doesn't say.
func params(s string) float64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	case strings.HasSuffix(s, "M"):
		s, mult = strings.TrimSuffix(s, "M"), 0.001
	default:
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f * mult
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
	if m.think {
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
// <think> tags in the answer itself.
func StripThinking(s string) string {
	for {
		i := strings.Index(s, "<think>")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "</think>")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+len("</think>"):]
	}
}
