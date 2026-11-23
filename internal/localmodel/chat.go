package localmodel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// What Irie's side tasks chat with: a local model that words an answer or
// makes a card, found on this computer's Ollama, so no tokens are bought.
// The Terms in short plugin (internal/terms) and materialized cards
// (internal/genui) share it, so neither needs the other's package.

// ChatServers are the Ollama servers a side task may chat on, the first
// that answers taking it: url (local.url) when set, else Irie's own on
// port, then Ollama's usual port.
func ChatServers(url string, port int) []string {
	if url != "" {
		return []string{url}
	}
	return []string{"http://127.0.0.1:" + strconv.Itoa(port), "http://127.0.0.1:11434"}
}

// ChatModel is a local model to chat with: one on this computer, from an
// Ollama server here.
type ChatModel struct {
	URL   string `json:"url"`  // its Ollama server
	Name  string `json:"name"` // such as qwen3.5:9b
	think bool   // it thinks before it answers
}

// Thinks reports whether the model thinks before it answers, which a
// caller that wants only the answer turns off (ChatOptions.Think).
func (m ChatModel) Thinks() bool { return m.think }

// ErrNoChatModel says no local model is here to chat with.
var ErrNoChatModel = errors.New("no local model here to word answers")

// ErrOffHere says no model may be loaded on this computer: its config's
// local.here is false (DESIGN §6.3), which leaves a side task no server
// to chat on unless local.url names another computer's.
var ErrOffHere = errors.New("local models are off on this computer (local.here = false)")

// maxChatModelGB is the most a model found by FindChatModel takes on disk:
// one bigger takes the memory the person's own work needs.
const maxChatModelGB = 10

// FindChatModel finds a local model to chat with: want, by its name, on
// the first of the servers at urls that has it, else the best model for
// chat that the first server to answer has. A cloud model is never one: it
// runs on someone else's computer, at a price. The model goes to a server
// that has it loaded already, if one has, so that it is never loaded twice,
// on Irie's own Ollama and on the user's, which share their models. No
// urls is ErrOffHere's case.
func FindChatModel(ctx context.Context, urls []string, want string) (ChatModel, error) {
	if len(urls) == 0 {
		return ChatModel{}, fmt.Errorf("%w: %w", ErrNoChatModel, ErrOffHere)
	}
	m, err := findChatModel(ctx, urls, want)
	if err != nil || len(urls) < 2 || loadedOn(ctx, m.URL, m.Name) {
		return m, err
	}
	for _, u := range urls {
		if u != m.URL && loadedOn(ctx, u, m.Name) {
			m.URL = u
			break
		}
	}
	return m, nil
}

// loadedOn says the Ollama server at url has model loaded now.
func loadedOn(ctx context.Context, url, model string) bool {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ls, err := Ollama{URL: url}.Running(c)
	return err == nil && slices.ContainsFunc(ls, func(l Loaded) bool { return SameModel(l.Name, model) })
}

func findChatModel(ctx context.Context, urls []string, want string) (ChatModel, error) {
	var tried []string
	for _, u := range urls {
		o := Ollama{URL: u}
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
			return ChatModel{}, err
		}
		models = slices.DeleteFunc(models, func(m Model) bool { return m.Remote })
		if want != "" {
			i := slices.IndexFunc(models, func(m Model) bool { return m.Name == want || strings.TrimSuffix(m.Name, ":latest") == want })
			if i < 0 {
				continue
			}
			return chatModelNamed(ctx, o, models[i].Name)
		}
		// The biggest that fits, by its parameters, of those that chat.
		slices.SortStableFunc(models, func(a, b Model) int {
			pa, pb := paramsOf(a.Params), paramsOf(b.Params)
			switch {
			case pa > pb:
				return -1
			case pa < pb:
				return 1
			}
			return int(b.Size/1e6 - a.Size/1e6)
		})
		for _, m := range models {
			if m.Size > maxChatModelGB<<30 || paramsOf(m.Params) > 16 || strings.Contains(m.Name, "embed") {
				continue
			}
			got, err := chatModelNamed(ctx, o, m.Name)
			if err == nil {
				return got, nil
			}
		}
		return ChatModel{}, fmt.Errorf("%w: the Ollama at %s has no model for chat that fits; `ollama pull qwen3:4b` gets a small one", ErrNoChatModel, u)
	}
	if want != "" {
		return ChatModel{}, fmt.Errorf("%w: no Ollama here has %s (tried %s)", ErrNoChatModel, want, strings.Join(urls, ", "))
	}
	return ChatModel{}, fmt.Errorf("%w: no Ollama answers at %s", ErrNoChatModel, strings.Join(tried, ", "))
}

// chatModelNamed checks a model can chat.
func chatModelNamed(ctx context.Context, o Ollama, name string) (ChatModel, error) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := o.Show(c, name)
	if err != nil {
		return ChatModel{}, err
	}
	if !slices.Contains(info.Capabilities, "completion") {
		return ChatModel{}, fmt.Errorf("%s doesn't write text", name)
	}
	return ChatModel{URL: o.URL, Name: name, think: slices.Contains(info.Capabilities, "thinking")}, nil
}

// paramsOf is a model's size in billions of parameters, from Ollama's
// "9.7B" or "270M"; 0 when it doesn't say.
func paramsOf(s string) float64 {
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
