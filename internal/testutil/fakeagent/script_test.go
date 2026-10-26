package main

import (
	"reflect"
	"testing"
	"time"
)

func TestParseScript(t *testing.T) {
	got, err := parseScript("prompt, tool:Bash,ask,permission,sub:a1b2c3:tool:Read+ask+sleep:10ms,stopfail:rate_limit,sleep:1s,stop,exit:3")
	if err != nil {
		t.Fatal(err)
	}
	want := []step{
		{kind: "prompt"},
		{kind: "tool", arg: "Bash"},
		{kind: "tool", arg: "AskUserQuestion"},
		{kind: "permission"},
		{kind: "sub", arg: "a1b2c3", sub: []step{{kind: "tool", arg: "Read"}, {kind: "tool", arg: "AskUserQuestion"}, {kind: "sleep", d: 10 * time.Millisecond}}},
		{kind: "stopfail", arg: "rate_limit"},
		{kind: "sleep", d: time.Second},
		{kind: "stop"},
		{kind: "exit", n: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseScriptRefusesWhatIsNotAScript(t *testing.T) {
	for _, s := range []string{
		"", "describe this repo in one paragraph", "say hello", "keep running", "prompt,", "tool:", "tool:a b",
		"sleep:soon", "exit:x", "exit:300", "stopfail:", "sub:a1", "sub:a1:prompt", "sub:a1:sub:a2:tool:Bash",
		"sub:a1:stop", "sub:a1:exit:1", "prompt:now",
	} {
		if steps, err := parseScript(s); err == nil {
			t.Errorf("%q parsed as %+v", s, steps)
		}
	}
}

func TestMatchers(t *testing.T) {
	for _, c := range []struct {
		matcher, value string
		want           bool
	}{
		{"", "Bash", true}, {"*", "Bash", true},
		{"Bash", "Bash", true}, {"Bash", "BashOutput", false},
		{"Edit|Write", "Write", true}, {"Edit, Write", "Edit", true}, {"Edit|Write", "Read", false},
		{"^Notebook", "NotebookEdit", true}, {"mcp__.*", "mcp__tracker__create", true}, {"mcp__.*", "Bash", false},
	} {
		if got := matches(c.matcher, c.value); got != c.want {
			t.Errorf("matches(%q, %q) = %v", c.matcher, c.value, got)
		}
	}
}
