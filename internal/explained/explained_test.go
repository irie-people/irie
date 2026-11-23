package explained

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)

const flow = `{"title":"Your groups","body":[{"type":"Flow","items":[{"title":"Pick","detail":"cards with ⌘"},{"title":"New Group","detail":"from the card's menu"}]}]}`

func TestNamesAreTheWordsAndADigest(t *testing.T) {
	if got := Slug("What is “Your groups” in this project, and how do I use it?"); !strings.HasPrefix(got, "what-is-your-groups-in-this-project-and-how-do-i-use-it-") || len(got) > most+7 {
		t.Errorf("slug %q", got)
	}
	if Slug("a b") == Slug("a  b?") {
		t.Error("two questions share a name")
	}
	// One name, never a path: no separator, and never . or .. alone.
	for in, want := range map[string]string{"irie": "irie", "my repo/../x": "my-repo-..-x", "": "general", "..": "general", "/": "general"} {
		if got := Folder(in); got != want {
			t.Errorf("Folder(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnExplanationKeepsEveryVersion(t *testing.T) {
	l := Library{Dir: t.TempDir()}
	q := "How do groups work?"
	if e, err := l.Load("irie", q); e != nil || err != nil {
		t.Fatalf("an explanation never kept loads as %+v, %v", e, err)
	}
	e, md, err := l.Add("irie", q, "Your groups", Version{Card: json.RawMessage(flow), Made: t0, By: "bonsai-2:9b"})
	if err != nil || len(e.Versions) != 1 || !strings.HasSuffix(md, ".md") {
		t.Fatalf("first: %+v, %s, %v", e, md, err)
	}
	e, _, err = l.Add("irie", q, "", Version{Card: json.RawMessage(flow), Made: t0.Add(time.Minute), By: "bonsai-2:9b", Asked: "add the keys"})
	if err != nil || len(e.Versions) != 2 || e.Latest().Asked != "add the keys" || e.Topic != "Your groups" {
		t.Fatalf("second: %+v, %v", e, err)
	}
	if e, err := l.Keep("irie", q, true); err != nil || !e.Kept {
		t.Fatalf("keep: %+v, %v", e, err)
	}
	if again, _ := l.Load("irie", q); again == nil || !again.Kept || len(again.Versions) != 2 {
		t.Errorf("read back: %+v", again)
	}
	text, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"question: \"How do groups work?\"\nproject: \"irie\"\ntopic: \"Your groups\"\nkept: true\nversions: 2\n",
		"# How do groups work?\n\n## Your groups\n\n1. **Pick** — cards with ⌘\n",
		"*Made by bonsai-2:9b on this computer, version 2 of 2.*",
		"## How it was made\n\n1. Asked: How do groups work?\n2. Then: add the keys\n",
		"Every version, as Irie made it, is in how-do-groups-work-",
	} {
		if !strings.Contains(string(text), want) {
			t.Errorf("the Markdown lacks %q:\n%s", want, text)
		}
	}
	l.Add("other", "What is a space?", "", Version{Card: json.RawMessage(flow), Made: t0.Add(time.Hour)})
	if all := l.All(""); len(all) != 2 || all[0].Project != "other" {
		t.Errorf("every project's: %d, newest %q", len(all), all[0].Project)
	}
	if mine := l.All("irie"); len(mine) != 1 || mine[0].Question != q {
		t.Errorf("irie's: %+v", mine)
	}
	if e, err := l.Keep("irie", "never asked", true); e != nil || err != nil {
		t.Errorf("keeping what was never asked: %+v, %v", e, err)
	}
}
