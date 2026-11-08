package sdk

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

// csi matches the terminal sequences this program writes.
var csi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z~]`)

func clean(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = csi.ReplaceAllString(l, "")
	}
	return out
}

// parse reads one line the CLI could write.
func parse(t *testing.T, line string) message {
	t.Helper()
	var m message
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("%s: %v", line, err)
	}
	return m
}

func TestFeedWrapsAtSpacesToFitTheWidth(t *testing.T) {
	v := newView(20)
	got := v.feed("the quick brown fox jumps over the lazy dog\nand an unbreakablewordthatgoesonandon")
	got = append(got, v.flush()...)
	for _, l := range got {
		if w := lipgloss.Width(l); w >= 20 {
			t.Errorf("%q is %d columns wide; the terminal has 20", l, w)
		}
	}
	if joined := strings.Join(got, " "); !strings.Contains(joined, "the quick brown fox jumps over the lazy dog") {
		t.Errorf("lines = %q: words were lost or split", got)
	}
	if !slices.Contains(got, "the quick brown fox") {
		t.Errorf("lines = %q, want the first break at a space", got)
	}
}

// TestStreamedTextShowsOnce is a reply streamed in pieces, then the whole
// message: its text shows once, line by line, and its tool call after it.
func TestStreamedTextShowsOnce(t *testing.T) {
	v := newView(80)
	var got []string
	for _, l := range []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":3,"cache_read_input_tokens":1200,"cache_creation_input_tokens":40}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello\nWor"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"ld"}}}`,
	} {
		got = append(got, v.add(parse(t, l))...)
	}
	if !slices.Equal(got, []string{"Hello"}) || v.line != "World" {
		t.Errorf("lines = %q, line in progress %q; want Hello, then World in progress", got, v.line)
	}
	got = append(got, v.add(parse(t, `{"type":"stream_event","event":{"type":"content_block_stop"}}`))...)
	got = append(got, v.add(parse(t, `{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello\nWorld"},{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"go test ./...\necho done"}}]}}`))...)
	if want := []string{"Hello", "World", "  ▸ Bash  go test ./... …"}; !slices.Equal(clean(got), want) {
		t.Errorf("lines = %q, want %q", clean(got), want)
	}
	if v.context != 1243 {
		t.Errorf("context = %d, want 1243 tokens: input and both caches", v.context)
	}
}

func TestToolResultsInAFewWords(t *testing.T) {
	v := newView(80)
	v.add(parse(t, `{"type":"assistant","message":{"id":"m","content":[{"type":"tool_use","id":"r1","name":"Read","input":{"file_path":"/home/alice/a.go"}},{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"ls"}}]}}`))
	got := clean(v.add(parse(t, `{"type":"user","message":{"role":"user","content":[`+
		`{"type":"tool_result","tool_use_id":"r1","content":"1\tpackage a\n2\n3\tfunc A() {}"},`+
		`{"type":"tool_result","tool_use_id":"b1","content":[{"type":"text","text":"a.go\nb.go\nc.go"}]},`+
		`{"type":"tool_result","tool_use_id":"b1","content":"exit status 1","is_error":true},`+
		`{"type":"tool_result","tool_use_id":"b1","content":""}]}}`)))
	want := []string{"    3 lines", "    a.go (+2 lines)", "    ✗ exit status 1", "    no output"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestASubagentsWorkIsIndentedAndItsTextLeftOut(t *testing.T) {
	v := newView(80)
	got := clean(v.add(parse(t, `{"type":"user","parent_tool_use_id":"agent_1","message":{"role":"user","content":"Find the tests"}}`)))
	got = append(got, clean(v.add(parse(t, `{"type":"assistant","parent_tool_use_id":"agent_1","message":{"id":"s1","content":[{"type":"text","text":"Looking"},{"type":"tool_use","id":"g1","name":"Grep","input":{"pattern":"func Test","path":"internal"}}]}}`)))...)
	if want := []string{"    ▸ Grep  func Test in internal"}; !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestResultLines(t *testing.T) {
	v := newView(80)
	v.add(parse(t, `{"type":"system","subtype":"init","model":"claude-opus-5-5","permissionMode":"auto"}`))
	got := clean(v.result(parse(t, `{"type":"result","subtype":"success","is_error":false,"total_cost_usd":0.4271,"modelUsage":{"claude-opus-5-5":{"contextWindow":200000}}}`), 12400*time.Millisecond))
	if want := []string{"  · done in 12s · $0.43 so far"}; !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
	if got := dollars(0.0042); got != "$0.004" {
		t.Errorf("dollars(0.0042) = %q, want $0.004", got)
	}
	if v.window != 200000 {
		t.Errorf("window = %d", v.window)
	}
	for line, want := range map[string]string{
		`{"type":"result","subtype":"success","is_error":true,"result":"Credit balance is too low"}`:         "  ! Credit balance is too low",
		`{"type":"result","subtype":"error_max_turns","is_error":true,"errors":["Reached the most turns"]}`:  "  ! Reached the most turns",
		`{"type":"result","subtype":"error_during_execution","is_error":true,"errors":[{"message":"boom"}]}`: "  ! boom",
		`{"type":"result","subtype":"error_during_execution","is_error":true}`:                               "  ! error_during_execution",
	} {
		if got := clean(v.result(parse(t, line), time.Second)); !slices.Equal(got, []string{want}) {
			t.Errorf("%s: lines = %q, want %q", line, got, want)
		}
	}
}

func TestSystemLines(t *testing.T) {
	v := newView(80)
	for line, want := range map[string]string{
		`{"type":"system","subtype":"init","model":"claude-haiku-5-5","permissionMode":"auto"}`:                        "  · claude-haiku-5-5 · auto mode",
		`{"type":"system","subtype":"api_retry","attempt":2,"max_retries":10,"error":"overloaded","error_status":529}`: "  · overloaded; trying again (2 of 10)",
		`{"type":"system","subtype":"status","status":"compacting"}`:                                                   "  · compacting the conversation…",
		`{"type":"system","subtype":"hook_started"}`:                                                                   "",
	} {
		got := strings.Join(clean(v.add(parse(t, line))), "\n")
		if got != want {
			t.Errorf("%s: %q, want %q", line, got, want)
		}
	}
	// Each turn starts with an init; only a new model shows again.
	if got := v.add(parse(t, `{"type":"system","subtype":"init","model":"claude-haiku-5-5","permissionMode":"auto"}`)); got != nil {
		t.Errorf("a second init showed %q", got)
	}
}

func TestSummaryAndDetails(t *testing.T) {
	for _, c := range []struct{ tool, input, want string }{
		{"Bash", `{"command":"rm -rf build"}`, "rm -rf build"},
		{"Edit", `{"file_path":"/home/alice/a.go","old_string":"a","new_string":"b"}`, "/home/alice/a.go"},
		{"WebFetch", `{"url":"https://example.com"}`, "https://example.com"},
		{"mcp__x__y", `{"a": 1}`, `{"a":1}`},
	} {
		if got := summary(c.tool, json.RawMessage(c.input)); got != c.want {
			t.Errorf("summary(%s, %s) = %q, want %q", c.tool, c.input, got, c.want)
		}
	}
	got := details("Edit", json.RawMessage(`{"file_path":"/home/alice/a.go","old_string":"x := 1","new_string":"x := 2\ny := 3"}`))
	if want := []string{"    /home/alice/a.go", "    - x := 1", "    + x := 2", "    + y := 3"}; !slices.Equal(got, want) {
		t.Errorf("details = %q, want %q", got, want)
	}
	long := strings.Repeat("echo hi\n", 30)
	got = details("Bash", json.RawMessage(`{"command":`+mustJSON(long)+`}`))
	if len(got) != 21 || got[20] != "    $ … 10 more lines" {
		t.Errorf("details of a long command = %d lines, last %q", len(got), got[len(got)-1])
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestStatusPayloadIsWhatTheStatusLineReads(t *testing.T) {
	v := newView(80)
	if got := string(statusPayload(v)); got != "{}" {
		t.Errorf("an empty view's payload = %s", got)
	}
	v.model, v.cost, v.costed, v.context, v.window = "claude-opus-5-5", 1.25, true, 50000, 200000
	var p struct {
		Model struct {
			ID string `json:"id"`
		} `json:"model"`
		Cost struct {
			Total float64 `json:"total_cost_usd"`
		} `json:"cost"`
		Context struct {
			Used float64 `json:"used_percentage"`
			Size int     `json:"context_window_size"`
		} `json:"context_window"`
	}
	if err := json.Unmarshal(statusPayload(v), &p); err != nil {
		t.Fatal(err)
	}
	if p.Model.ID != "claude-opus-5-5" || p.Cost.Total != 1.25 || p.Context.Used != 25 || p.Context.Size != 200000 {
		t.Errorf("payload = %+v", p)
	}
}

func TestWrapKeepsEveryWord(t *testing.T) {
	text := "one two three four five six seven eight nine ten\n\nafter a blank line"
	got := wrap(text, 20)
	if strings.Join(strings.Fields(strings.Join(got, " ")), " ") != strings.Join(strings.Fields(text), " ") {
		t.Errorf("wrap = %q: words were lost", got)
	}
	if !slices.Contains(got, "") {
		t.Errorf("wrap = %q: the blank line was lost", got)
	}
	for _, l := range got {
		if lipgloss.Width(l) >= 20 {
			t.Errorf("%q is wider than 19 columns", l)
		}
	}
}

// TestNothingShownCanSteerTheTerminal is text from a tool, the model or a
// paste that holds escape sequences: a colour, a title, a write to the
// clipboard, a C1 control, a byte that is not UTF-8. None of it reaches
// the terminal; the words do.
func TestNothingShownCanSteerTheTerminal(t *testing.T) {
	evil := "ok \x1b[31mred\x1b[0m \x1b]0;title\x07 \x1b]52;c;aGk=\x1b\\ \x1bPdcs\x1b\\ \u009b6n \x9b6n bell\x07 tab\tend"
	got := safe(evil)
	if strings.ContainsAny(got, "\x1b\x07\u009b") || strings.Contains(got, "\x9b") || strings.Contains(got, "aGk=") || strings.Contains(got, "title") {
		t.Errorf("safe(%q) = %q: a control got through", evil, got)
	}
	for _, want := range []string{"ok ", "red", "bell", "tab    end"} {
		if !strings.Contains(got, want) {
			t.Errorf("safe(%q) = %q, want it to keep %q", evil, got, want)
		}
	}
	if got := safe("plain text, with ünïcode\nand a new line"); got != "plain text, with ünïcode\nand a new line" {
		t.Errorf("safe changed plain text: %q", got)
	}

	v := newView(40)
	v.add(parse(t, `{"type":"assistant","message":{"id":"m","content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"cat notes.txt"}}]}}`))
	lines := v.add(parse(t, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1","content":"116:\tcase \"help\": \u001b]52;c;aGk=\u0007done"}]}}`))
	lines = append(lines, v.feed("the model says \u001b[2Jhi\n")...)
	for _, l := range lines {
		plain := strings.TrimSuffix(strings.TrimPrefix(l, dim), reset)
		if strings.ContainsAny(plain, "\x1b\x07\t") {
			t.Errorf("%q reached the terminal", l)
		}
		if w := lipgloss.Width(plain); w >= 40 {
			t.Errorf("%q is %d columns wide; the terminal has 40", plain, w)
		}
	}
}
