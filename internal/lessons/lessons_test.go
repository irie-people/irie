package lessons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/irie-people/irie/internal/brand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/habits"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/localmodel"
)

var t0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

func stamp(sec int) string { return event.Stamp(t0.Add(time.Duration(sec) * time.Second)) }

// line is one transcript line, shaped as Claude Code writes them.
func line(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

func use(sec int, id, tool string, input map[string]any) string {
	return line(map[string]any{"type": "assistant", "timestamp": stamp(sec), "message": map[string]any{"role": "assistant",
		"content": []any{map[string]any{"type": "tool_use", "id": id, "name": tool, "input": input}}}})
}

func result(sec int, id string, failed bool, content any) string {
	return line(map[string]any{"type": "user", "timestamp": stamp(sec), "message": map[string]any{"role": "user",
		"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": failed, "content": content}}}})
}

func bash(cmd string) map[string]any { return map[string]any{"command": cmd} }

func TestByNamesWhoStoppedACall(t *testing.T) {
	for err, want := range map[string]string{
		"exit 1: FAIL": ByCommand,
		"File has not been read yet. Read it first before writing to it.":                                                                     ByTool,
		"PreToolUse:Bash hook error: [" + brand.Name + "] " + brand.Gabriel + " turned this call away: it would bring a window to the front.": ByHarness,
		"PreToolUse:Bash hook error: [You are " + brand.Mikael + ", the arch agent who stands with the user against evil. An agent":           ByHarness,
		`PreToolUse:Read hook error: [/home/alice/bin/guard "$CLAUDE_PROJECT_DIR/x"]: off-limits in this repo.`:                               ByHook,
		"PreToolUse:Edit hook error: Skills are not edited in place.":                                                                         ByHook,
		"Permission for this action was denied by the Claude Code auto mode classifier. Reason: [Undermining Oversight].":                     ByPermission,
		"The server-side auto mode classifier gave no verdict (the request timed out).":                                                       ByNoVerdict,
		"The user doesn't want to proceed with this tool use. The tool use was rejected.":                                                     ByUser,
	} {
		if got := By("Bash", err); got != want {
			t.Errorf("By(%q) = %s, want %s", err, got, want)
		}
	}
	if got := By("Read", "exit 1: no"); got != ByTool {
		t.Errorf("a tool other than Bash that says exit: %s", got)
	}
}

func TestATranscriptGivesEachCallAndWhatItsErrorSaid(t *testing.T) {
	transcript := use(-60, "t0", "Bash", bash("ls")) + result(-59, "t0", true, "Exit code 2\nls: x: No such file or directory") +
		use(10, "t1", "Bash", bash("go test ./...")) + result(12, "t1", true, "Exit code 1\n--- FAIL: TestX\nFAIL") +
		use(20, "t2", "Edit", map[string]any{"file_path": "/home/alice/a.go"}) +
		result(21, "t2", true, "<tool_use_error>File has not been read yet. Read it first before writing to it.</tool_use_error>") +
		use(30, "t3", "Read", map[string]any{"file_path": "/home/alice/a.go"}) + result(31, "t3", false, []any{map[string]any{"type": "text", "text": "package a"}}) +
		use(40, "t4", "Bash", bash("open -a Safari")) +
		result(41, "t4", true, []any{map[string]any{"type": "text", "text": "PreToolUse:Bash hook error: [" + brand.Name + "] " + brand.Gabriel + " turned this call away."}}) +
		use(50, "t5", "Bash", bash("sleep 100")) + result(51, "t5", true, "[Request interrupted by user for tool use]") +
		line(map[string]any{"type": "assistant", "isSidechain": true, "timestamp": stamp(60), "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "t6", "name": "Grep", "input": map[string]any{}}}}}) +
		"not json\n" + use(70, "t7", "Bash", bash("make"))
	calls, err := Transcript(strings.NewReader(transcript), "s1", event.Main, t0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range calls {
		got = append(got, fmt.Sprintf("%s|%s|%v|%s", c.Family, c.Cmd, c.Failed, c.Error))
		if c.Session != "s1" || c.Agent != event.Main {
			t.Errorf("call %+v: not s1's main agent's", c)
		}
	}
	want := []string{
		"go test|go test ./...|true|exit 1: FAIL",
		"Edit||true|<tool_use_error>File has not been read yet. Read it first before writing to it.</tool_use_error>",
		"Read||false|",
		"open|open -a Safari|true|PreToolUse:Bash hook error: [" + brand.Name + "] " + brand.Gabriel + " turned this call away.",
		"sleep|sleep 100|true|",
		"make|make|false|",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !calls[0].At.Equal(t0.Add(10 * time.Second)) {
		t.Errorf("the first call started at %v", calls[0].At)
	}
	// A subagent's own transcript keeps its sidechain lines.
	sub, _ := Transcript(strings.NewReader(line(map[string]any{"type": "assistant", "isSidechain": true, "timestamp": stamp(60), "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "t6", "name": "Grep", "input": map[string]any{}}}}})), "s1", "sub:a1", t0)
	if len(sub) != 1 || sub[0].Agent != "sub:a1" {
		t.Errorf("a subagent's calls: %+v", sub)
	}
}

// failed is a call that failed with err, by session s's main agent.
func failed(s, tool, cmd, err string) habits.Call {
	fam := tool
	if cmd != "" {
		fam = habits.Family(cmd)
	}
	return habits.Call{Session: s, Agent: event.Main, Tool: tool, Cmd: cmd, Family: fam, Failed: true, Error: err, At: t0}
}

func worked(s, tool, cmd string) habits.Call {
	c := failed(s, tool, cmd, "")
	c.Failed = false
	return c
}

const notRead = "<tool_use_error>File has not been read yet. Read it first before writing to it.</tool_use_error>"

func TestEvidenceRanksWhatSessionsShareWithWhatWorkedNext(t *testing.T) {
	var calls []habits.Call
	for _, s := range []string{"s1", "s2", "s3"} {
		calls = append(calls, failed(s, "Edit", "", notRead), worked(s, "Read", ""), worked(s, "Edit", ""))
	}
	calls = append(calls,
		// One session's own trouble is no pattern.
		failed("s1", "Bash", "make deploy", "exit 2: make: *** No rule to make target 'deploy'."), worked("s1", "Bash", "make release"),
		// A guard of the user's own: its share, never a lesson.
		failed("s1", "Read", "", `PreToolUse:Read hook error: [/home/alice/guard "$CLAUDE_PROJECT_DIR/x"]: "secret-project" is off-limits.`),
		failed("s2", "Read", "", `PreToolUse:Read hook error: [/home/alice/guard "$CLAUDE_PROJECT_DIR/x"]: "other-project" is off-limits.`),
		// The user's no costs them their time even once.
		failed("s2", "Bash", "git push --force", "The user doesn't want to proceed with this tool use."),
		// A hiccup of the auto mode's service is no one's.
		failed("s3", "Bash", "ls", "The server-side auto mode classifier gave no verdict (error)."),
		// Two sessions that ran a window into Gabriel, and then went round it.
		failed("s1", "Bash", "open -a Safari https://example.com", "PreToolUse:Bash hook error: ["+brand.Name+"] "+brand.Gabriel+" turned this call away: use irie away."),
		worked("s1", "Bash", brand.Slug+" away -- open -a Safari https://example.com"),
		failed("s3", "Bash", "open -a Preview a.pdf", "PreToolUse:Bash hook error: ["+brand.Name+"] "+brand.Gabriel+" turned this call away: use irie away."),
		worked("s3", "Bash", brand.Slug+" away -- open -a Preview a.pdf"),
		// Cut short by the user: failed, with no error to learn from.
		habits.Call{Session: "s3", Agent: event.Main, Tool: "Bash", Family: "sleep", Failed: true},
	)
	ev := Build(calls)
	if ev.Calls != len(calls) || ev.Sessions != 3 || ev.Failed != 11 || ev.Unanswered != 1 {
		t.Errorf("calls %d, sessions %d, failed %d, unanswered %d", ev.Calls, ev.Sessions, ev.Failed, ev.Unanswered)
	}
	var got []string
	for _, k := range ev.Kinds {
		got = append(got, fmt.Sprintf("%d %s %d/%d %s then %s", k.N, k.By, k.Calls, k.Sessions, k.Kind, k.Then))
	}
	want := []string{
		"1 tool 3/3 Edit: File has not been read yet. Read it first before writing to it. then Read",
		"2 harness 2/2 open: PreToolUse:Bash hook error: [" + brand.Name + "] " + brand.Gabriel + " turned this call away: use irie away. then " + brand.Slug + " away",
		"3 user 1/1 git push: The user doesn't want to proceed with this tool use. then ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("kinds:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if ev.YoursCalls != 2 || len(ev.Yours) != 1 || strings.Contains(ev.Yours[0].Kind, "secret-project") || ev.Yours[0].Sessions != 2 {
		t.Errorf("yours: %d, %+v; want both refusals as one kind, its quoted words left out", ev.YoursCalls, ev.Yours)
	}
	// What a call said goes to the model alone: it can name any repository's files.
	if ev.Kinds[1].example == "" || ev.Kinds[1].thenEx != brand.Slug+" away -- open -a Preview a.pdf" {
		t.Errorf("examples: %q, then %q", ev.Kinds[1].example, ev.Kinds[1].thenEx)
	}
	if b, _ := json.Marshal(ev); strings.Contains(string(b), "Safari") || strings.Contains(string(b), "secret-project") {
		t.Errorf("the evidence as kept names a call's own words: %s", b)
	}
	if n := ev.Since([]string{ev.Kinds[0].Kind}, t0); n != 3 {
		t.Errorf("since t0, %d calls of the first kind, want 3", n)
	}
	if n := ev.Since([]string{ev.Kinds[0].Kind}, t0.Add(time.Second)); n != 0 {
		t.Errorf("since after them, %d calls, want 0", n)
	}
}

// appendEvents writes evs into a journal in dir.
func appendEvents(t *testing.T, dir string, evs ...event.Event) {
	t.Helper()
	j, err := journal.Open(dir, journal.Options{KeepDays: 90})
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for _, e := range evs {
		if _, err := j.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

func toolEvent(session, kind, id string, data map[string]any) event.Event {
	return event.Event{Session: session, Agent: event.Main, Source: event.SourceHook, Harness: "claude-code", Kind: kind,
		Vendor: &event.Vendor{ToolUseID: id}, Data: data}
}

func TestGatherReadsTheTranscriptsOfIriesSessionsElseTheJournal(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	now := time.Now()
	recent := func(sec int) string { return event.Stamp(now.Add(time.Duration(sec-600) * time.Second)) }
	tr := filepath.Join(home, ".claude", "projects", "-home-alice-repo", "abc.jsonl")
	sub := filepath.Join(strings.TrimSuffix(tr, ".jsonl"), "subagents", "agent-a1.jsonl")
	for path, text := range map[string]string{
		// A hook's refusal, which the journal never hears of, and the
		// read that worked next.
		tr: line(map[string]any{"type": "assistant", "timestamp": recent(1), "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": bash("open -a Safari")}}}}) +
			line(map[string]any{"type": "user", "timestamp": recent(2), "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "is_error": true, "content": "PreToolUse:Bash hook error: [" + brand.Name + "] " + brand.Gabriel + " turned this call away."}}}}),
		sub: line(map[string]any{"type": "assistant", "isSidechain": true, "timestamp": recent(3), "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "t2", "name": "Grep", "input": map[string]any{}}}}}) +
			line(map[string]any{"type": "user", "isSidechain": true, "timestamp": recent(4), "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t2", "is_error": true, "content": "Error: No such tool available: Grep."}}}}),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	appendEvents(t, dir,
		event.Event{Session: "s0000000000a1", Agent: event.Main, Source: event.SourceHook, Harness: "claude-code", Kind: event.SessionStarted, Data: map[string]any{"transcript": tr}},
		// The journal's own copy of a call in s1's transcript counts once.
		toolEvent("s0000000000a1", event.ToolStarted, "t1", map[string]any{"tool": "Bash", "summary": "open -a Safari"}),
		// s2's transcript is not there: its calls come from the journal.
		event.Event{Session: "s0000000000a2", Agent: event.Main, Source: event.SourceHook, Harness: "claude-code", Kind: event.SessionStarted, Data: map[string]any{"transcript": filepath.Join(home, "gone.jsonl")}},
		toolEvent("s0000000000a2", event.ToolStarted, "u1", map[string]any{"tool": "Bash", "summary": "go vet ./..."}),
		toolEvent("s0000000000a2", event.ToolFailed, "u1", map[string]any{"tool": "Bash", "ms": 900.0, "error": "exit 1: vet: x.go:3: unreachable code"}),
		toolEvent("s0000000000a3", event.ToolStarted, "v1", map[string]any{"tool": "Read"}),
		toolEvent("s0000000000a3", event.ToolFinished, "v1", map[string]any{"tool": "Read", "ms": 3.0}),
	)
	ev, err := Gather(dir, now.Add(-24*time.Hour), now, true)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Calls != 4 || ev.Failed != 3 || ev.Sessions != 3 || ev.Transcripts != 2 {
		t.Errorf("calls %d, failed %d, sessions %d, transcripts %d; want 4, 3, 3, 2", ev.Calls, ev.Failed, ev.Sessions, ev.Transcripts)
	}
	var kinds []string
	for _, c := range ev.failed {
		kinds = append(kinds, c.Session+" "+c.Agent+" "+habits.FailureKind(c))
	}
	for _, want := range []string{"s0000000000a1 main open: PreToolUse", "s0000000000a1 sub:a1 Grep: Error: No such tool available: Grep.", "s0000000000a2 main go vet: exit N: vet"} {
		if !strings.Contains(strings.Join(kinds, "\n"), want) {
			t.Errorf("no %q among the failures:\n%s", want, strings.Join(kinds, "\n"))
		}
	}
	// The journal alone hears nothing of what hooks refused.
	ev, err = Gather(dir, now.Add(-24*time.Hour), now, false)
	if err != nil || ev.Calls != 3 || ev.Failed != 1 || ev.Transcripts != 0 {
		t.Errorf("the journal alone: calls %d, failed %d, transcripts %d, %v", ev.Calls, ev.Failed, ev.Transcripts, err)
	}
}

// fakeOllama answers each chat with the next of its replies, and keeps
// what it was asked.
type fakeOllama struct {
	mu      sync.Mutex
	replies []string
	asked   []struct {
		Model    string
		Messages []struct{ Role, Content string }
		Format   json.RawMessage
		Think    *bool
	}
}

func (f *fakeOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/chat" {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var a struct {
		Model    string
		Messages []struct{ Role, Content string }
		Format   json.RawMessage
		Think    *bool
	}
	json.NewDecoder(r.Body).Decode(&a)
	f.asked = append(f.asked, a)
	reply := f.replies[min(len(f.asked), len(f.replies))-1]
	b, _ := json.Marshal(map[string]any{"message": map[string]string{"role": "assistant", "content": reply}, "done": true})
	fmt.Fprintf(w, "%s\n", b)
}

// someEvidence has three kinds, as Build gives them.
func someEvidence() Evidence {
	var calls []habits.Call
	for _, s := range []string{"s1", "s2"} {
		calls = append(calls, failed(s, "Edit", "", notRead), worked(s, "Read", ""),
			failed(s, "Bash", "sleep 30 && tail -5 log", "Blocked: sleep 30 followed by: tail -5 log"), worked(s, "Monitor", ""),
			failed(s, "Bash", "curl -s https://example.com/x", "exit 6: curl: (6) Could not resolve host: example.com"), worked(s, "Bash", "curl -s https://example.com/x"))
	}
	ev := Build(calls)
	ev.From, ev.To = t0.Add(-14*24*time.Hour), t0
	return ev
}

func TestADraftHoldsTheModelToTheEvidence(t *testing.T) {
	ev := someEvidence()
	answer := `<think>Edits first.</think>{"lessons":[
		{"lesson":"- Read a file with Read before you Edit or Write it.","kinds":[1]},
		{"lesson":"\"Wait on a log with Monitor, never with sleep and tail.\"","kinds":[2,2,9]},
		{"lesson":"Never make up a kind.","kinds":[9]},
		{"lesson":"Ask the user before you push.","kinds":[]},
		{"lesson":"Check the network first.","kinds":[3]},
		{"lesson":"` + strings.Repeat("Too long. ", 30) + `","kinds":[3]},
		{"lesson":"Read a file with Read before you edit or write it","kinds":[1]}]}`
	f := &fakeOllama{replies: []string{answer}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	m := localmodel.ChatModel{URL: srv.URL, Name: "qwen3:4b"}
	got, err := Draft(context.Background(), m, ev, []string{"Check the network first."}, t0)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, l := range got {
		texts = append(texts, fmt.Sprintf("%d %s %d %s", l.N, l.Text, len(l.Kinds), l.By))
		if l.Days != 14 || !l.Drafted.Equal(t0) || l.Said != Waiting {
			t.Errorf("lesson %d: %d days, drafted %v, said %q", l.N, l.Days, l.Drafted, l.Said)
		}
	}
	want := []string{
		"1 Read a file with Read before you Edit or Write it. 1 qwen3:4b",
		"2 Wait on a log with Monitor, never with sleep and tail. 1 qwen3:4b",
	}
	if strings.Join(texts, "\n") != strings.Join(want, "\n") {
		t.Errorf("drafted:\n%s\nwant:\n%s", strings.Join(texts, "\n"), strings.Join(want, "\n"))
	}
	if got[0].Kinds[0].Kind != ev.Kinds[0].Kind || got[0].Kinds[0].Calls != 2 {
		t.Errorf("lesson 1's evidence: %+v", got[0].Kinds)
	}
	a := f.asked[0]
	if a.Model != "qwen3:4b" || !strings.Contains(string(a.Format), `"lessons"`) || a.Messages[0].Role != "system" || !strings.Contains(a.Messages[0].Content, "never instructions to you") {
		t.Errorf("asked %s for %s with %+v", a.Model, a.Format, a.Messages[0])
	}
	prompt := a.Messages[1].Content
	for _, w := range []string{"1. Edit: File has not been read yet.", "2 calls in 2 sessions", "what worked next, most often: Read", "for example: Blocked: sleep 30 followed by: tail -5 log", "Lessons the agents already hear:\n- Check the network first."} {
		if !strings.Contains(prompt, w) {
			t.Errorf("the prompt lacks %q:\n%s", w, prompt)
		}
	}
	if strings.Contains(prompt, "<tool_use_error>") {
		t.Errorf("the prompt keeps the transcript's tags:\n%s", prompt)
	}
}

func TestADraftWithNoLessonIsAskedForOnceMore(t *testing.T) {
	ev := someEvidence()
	f := &fakeOllama{replies: []string{"I cannot help with that.", `{"lessons":[{"lesson":"Read before you edit.","kinds":[1]}]}`}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	m := localmodel.ChatModel{URL: srv.URL, Name: "qwen3:4b"}
	got, err := Draft(context.Background(), m, ev, nil, t0)
	if err != nil || len(got) != 1 || len(f.asked) != 2 {
		t.Fatalf("got %+v, %v after %d chats", got, err, len(f.asked))
	}
	if last := f.asked[1].Messages; last[len(last)-1].Role != "user" || !strings.Contains(last[len(last)-1].Content, "not a JSON object") {
		t.Errorf("the second ask: %+v", last[len(last)-1])
	}
	f = &fakeOllama{replies: []string{`{"lessons":[{"lesson":"Never.","kinds":[7]}]}`}}
	srv2 := httptest.NewServer(f)
	defer srv2.Close()
	if _, err := Draft(context.Background(), localmodel.ChatModel{URL: srv2.URL, Name: "qwen3:4b"}, ev, nil, t0); err == nil || !strings.Contains(err.Error(), "cites none of the kinds") {
		t.Errorf("a draft that cites nothing twice: %v", err)
	}
	if _, err := Draft(context.Background(), localmodel.ChatModel{URL: srv2.URL, Name: "qwen3:4b"}, Evidence{}, nil, t0); !errors.Is(err, ErrNothing) {
		t.Errorf("a draft from no evidence: %v", err)
	}
}

func TestOnlyWhatTheUserKeepsIsHeard(t *testing.T) {
	ev := someEvidence()
	var b Book
	drafts := []Lesson{{N: 1, Text: "Read before you edit.", Kinds: ev.Kinds[:1], Days: 14}, {N: 2, Text: "Wait with Monitor.", Kinds: ev.Kinds[1:2], Days: 14}, {N: 3, Text: "Retry a lookup once.", Kinds: ev.Kinds[2:], Days: 14}}
	b.Take(drafts, ev, "qwen3:4b", "s1", t0)
	if Note(b.Kept) != "" || len(b.Waiting()) != 3 {
		t.Fatalf("before any word: note %q, %d waiting", Note(b.Kept), len(b.Waiting()))
	}
	if err := b.Keep([]int{1}, t0.Add(time.Hour), t0.Add(time.Hour)); !errors.Is(err, ErrStale) {
		t.Errorf("a keep of an older draft: %v", err)
	}
	if err := b.Keep([]int{1, 1}, t0, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := b.Keep([]int{1}, time.Time{}, t0); err == nil || !strings.Contains(err.Error(), "kept already") {
		t.Errorf("keep twice: %v", err)
	}
	if err := b.No([]int{2}, time.Time{}, t0); err != nil {
		t.Fatal(err)
	}
	if err := b.Keep([]int{2}, time.Time{}, t0); err == nil || !strings.Contains(err.Error(), "turned down") {
		t.Errorf("keep what was turned down: %v", err)
	}
	if err := b.Keep([]int{4}, time.Time{}, t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("keep a lesson not drafted: %v", err)
	}
	if err := b.Keep([]int{3}, time.Time{}, t0); err != nil {
		t.Fatal(err)
	}
	want := "[" + brand.Name + "] Lessons from what agents got wrong most often, which your user picked; follow them:\n- Read before you edit.\n- Retry a lookup once."
	if got := Note(b.Kept); got != want {
		t.Errorf("note:\n%s\nwant:\n%s", got, want)
	}
	if b.Kept[0].N != 1 || b.Kept[1].N != 2 || len(b.Waiting()) != 0 {
		t.Errorf("kept %+v, waiting %d", b.Kept, len(b.Waiting()))
	}
	// A kept lesson keeps its number when one before it is dropped.
	if err := b.Drop([]int{1}); err != nil || len(b.Kept) != 1 || b.Kept[0].N != 2 {
		t.Errorf("drop 1: %v, kept %+v", err, b.Kept)
	}
	if err := b.Drop([]int{1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("drop it again: %v", err)
	}
	for i := 0; len(b.Kept) < MostKept; i++ {
		b.Kept = append(b.Kept, Lesson{N: 10 + i, Text: fmt.Sprint("Lesson ", i)})
	}
	b.Take([]Lesson{{N: 1, Text: "One too many.", Kinds: ev.Kinds[:1]}}, ev, "qwen3:4b", "", t0)
	if err := b.Keep([]int{1}, time.Time{}, t0); err == nil || !strings.Contains(err.Error(), "drop one first") {
		t.Errorf("keep past the most: %v", err)
	}
}

func TestWhatCameOfAKeptLessonIsCountedAgain(t *testing.T) {
	ev := someEvidence()
	b := Book{Kept: []Lesson{{N: 1, Text: "Read before you edit.", Kinds: ev.Kinds[:1], Days: 14, At: t0.Add(-24 * time.Hour)}}}
	if !strings.Contains(CameOf(b.Kept[0]), "shows once lessons are drafted again") {
		t.Errorf("before a new draft: %s", CameOf(b.Kept[0]))
	}
	b.Watch(ev)
	if b.Kept[0].Since == nil || *b.Kept[0].Since != 2 || b.Kept[0].SinceDays != 1 {
		t.Fatalf("since: %v over %v days", b.Kept[0].Since, b.Kept[0].SinceDays)
	}
	if got := CameOf(b.Kept[0]); !strings.Contains(got, "Before: 0.1 failed calls of its kinds a day. Since: 2.0 a day, over 1 day.") {
		t.Errorf("came of it: %s", got)
	}
	if b.Evidence == nil || b.Evidence.failed != nil {
		t.Errorf("the book keeps the evidence without its calls: %+v", b.Evidence)
	}
}
