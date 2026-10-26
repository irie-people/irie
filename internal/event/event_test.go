package event

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// sample is the envelope example of DESIGN §5.1, with its data keys in the
// order encoding/json writes them.
const sample = `{"v":1,"seq":4182,"ts":"2026-10-26T14:02:11.408Z","session":"s0k3j9x2m4a7q","agent":"sub:a1b2c3","source":"hook","harness":"claude-code","kind":"tool.started","vendor":{"session_id":"6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f","tool_use_id":"toolu_01","agent_id":"a1b2c3","agent_type":"Explore","hook_event":"PreToolUse"},"data":{"summary":"go test ./...","tool":"Bash"}}`

func valid() Event {
	var e Event
	if err := json.Unmarshal([]byte(sample), &e); err != nil {
		panic(err)
	}
	return e
}

func TestTheDesignExampleValidates(t *testing.T) {
	e := valid()
	if err := Validate(e); err != nil {
		t.Fatal(err)
	}
	if e.Vendor == nil || e.Vendor.AgentType != "Explore" || e.Str("tool") != "Bash" {
		t.Errorf("decoded %+v", e)
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != sample {
		t.Errorf("round trip:\n%s\nwant:\n%s", b, sample)
	}
}

func TestValidateRejects(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*Event)
	}{
		{"version", func(e *Event) { e.V = 2 }},
		{"seq", func(e *Event) { e.Seq = 0 }},
		{"ts without milliseconds", func(e *Event) { e.TS = "2026-10-26T14:02:11Z" }},
		{"ts not UTC", func(e *Event) { e.TS = "2026-10-26T14:02:11.408+02:00" }},
		{"session", func(e *Event) { e.Session = "s0k3j9x2m4a7" }},
		{"session alphabet", func(e *Event) { e.Session = "s0k3j9x2m4a7u" }},
		{"agent", func(e *Event) { e.Agent = "child" }},
		{"empty sub agent", func(e *Event) { e.Agent = "sub:" }},
		{"source", func(e *Event) { e.Source = "screen" }},
		{"harness", func(e *Event) { e.Harness = "" }},
		{"kind", func(e *Event) { e.Kind = "tool.exploded" }},
		{"no data", func(e *Event) { e.Data = nil }},
		{"long data string", func(e *Event) { e.Data["summary"] = strings.Repeat("x", MaxString+1) }},
		{"long nested string", func(e *Event) { e.Data["list"] = []any{map[string]any{"s": strings.Repeat("x", MaxString+1)}} }},
		{"long vendor string", func(e *Event) { e.Vendor.ToolUseID = strings.Repeat("x", MaxString+1) }},
	} {
		e := valid()
		c.edit(&e)
		if err := Validate(e); err == nil {
			t.Errorf("%s: no error", c.name)
		}
	}
	e := valid()
	e.Agent, e.Vendor = "", nil
	if err := Validate(e); err != nil {
		t.Errorf("a session-wide event without vendor ids: %v", err)
	}
}

func TestEveryKindOfTheDesignIsKnown(t *testing.T) {
	for _, k := range []string{
		"spawn.requested", "spawn.failed", "session.started", "prompt.submitted",
		"tool.started", "tool.finished", "tool.failed", "input.needed", "turn.ended", "turn.failed",
		"subagent.started", "subagent.finished", "session.ended", "pane.exited",
		"gate.set", "gate.held", "gate.released", "freeze.on", "freeze.off", "priority.set",
		"note.queued", "note.delivered",
		"ui.renamed", "ui.parked", "ui.unparked", "ui.closed", "ui.reopened", "ui.undo", "ui.redo",
	} {
		if !Known(k) {
			t.Errorf("kind %s is unknown", k)
		}
	}
}

func TestStampIsUTCWithMilliseconds(t *testing.T) {
	at := time.Date(2026, 10, 26, 16, 2, 11, 408_900_000, time.FixedZone("x", 2*3600))
	if got := Stamp(at); got != "2026-10-26T14:02:11.408Z" {
		t.Errorf("Stamp = %q", got)
	}
	if got := Stamp(time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)); got != "2026-10-26T14:00:00.000Z" {
		t.Errorf("Stamp of a whole second = %q", got)
	}
	e := Event{TS: "2026-10-26T14:02:11.408Z"}
	if want := time.Date(2026, 10, 26, 14, 2, 11, 408_000_000, time.UTC); !e.Time().Equal(want) {
		t.Errorf("Time = %v", e.Time())
	}
}

func TestCapKeepsWholeRunes(t *testing.T) {
	s := strings.Repeat("é", MaxString) // two bytes each
	got := Cap(s)
	if len(got) > MaxString || !utf8.ValidString(got) {
		t.Errorf("Cap gave %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
	if len(got) < MaxString-1 {
		t.Errorf("Cap cut too much: %d bytes", len(got))
	}
	if Cap("short") != "short" {
		t.Error("a short string changed")
	}
}

func TestCapAllWalksMapsAndSlices(t *testing.T) {
	long := strings.Repeat("x", 3*MaxString)
	v := map[string]any{"a": long, "b": []any{long, 3.0, map[string]any{"c": long}}, "n": 7.0}
	CapAll(v)
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), strings.Repeat("x", MaxString+1)) {
		t.Errorf("a string over %d bytes survived: %d bytes in all", MaxString, len(b))
	}
	if v["n"] != 7.0 {
		t.Errorf("numbers changed: %v", v["n"])
	}
}

func TestAgentNames(t *testing.T) {
	if Agent("") != "main" || Agent("a1b2c3") != "sub:a1b2c3" {
		t.Errorf("Agent: %q %q", Agent(""), Agent("a1b2c3"))
	}
}

func TestDataAccessorsSurviveARoundTrip(t *testing.T) {
	e := Event{Data: map[string]any{"code": 3, "tool": "Bash", "ok": true}}
	b, _ := json.Marshal(e)
	var back Event
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	for _, x := range []Event{e, back} {
		if n, ok := x.Int("code"); !ok || n != 3 {
			t.Errorf("Int(code) = %d, %v", n, ok)
		}
		if x.Str("tool") != "Bash" || x.Str("missing") != "" {
			t.Errorf("Str: %q", x.Str("tool"))
		}
		if _, ok := x.Int("tool"); ok {
			t.Error("Int read a string")
		}
	}
}
