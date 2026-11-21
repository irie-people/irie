package main

import (
	"regexp"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// sgr matches a colour escape, which the plain screen op leaves out.
var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

// The size of an agent's terminal, as Irie's panes are on a laptop.
const cols, rows = 144, 49

// talk is a session's conversation: a little of everything the Browser view
// draws, and what the session waits on you for.
func talk(s ipc.Session) ipc.Conversation {
	t0 := s.Created
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	items := []ipc.Item{
		{ID: "u1", Kind: ipc.ItemUser, Time: at(0), Text: s.Name + ". Keep the change small, and run the tests before you say it works."},
		{ID: "t1", Kind: ipc.ItemText, Time: at(0), Text: "I'll start by reading how it works now, then make the change and run the tests."},
		{ID: "r1", Kind: ipc.ItemTool, Time: at(1), Tool: "Read", Phrase: "Read parse.go", Detail: "internal/parse/parse.go", State: ipc.ItemOK, Group: "read", Ms: 40},
		{ID: "r2", Kind: ipc.ItemTool, Time: at(1), Tool: "Read", Phrase: "Read parse_test.go", Detail: "internal/parse/parse_test.go", State: ipc.ItemOK, Group: "read", Ms: 35},
		{ID: "r3", Kind: ipc.ItemTool, Time: at(1), Tool: "Read", Phrase: "Read dates.go", Detail: "internal/parse/dates.go", State: ipc.ItemOK, Group: "read", Ms: 31},
		{ID: "a1", Kind: ipc.ItemAgent, Time: at(2), AgentType: "Explore", Text: "Find every test that reads the clock", Detail: "Search the tests for time.Now and report each place, with its file and line.", State: ipc.ItemOK, Tools: 14, Ms: 81000},
		{ID: "t2", Kind: ipc.ItemText, Time: at(4), Text: "Found it. `TestDates` reads `time.Now()` twice, so a run that crosses midnight fails:\n\n" +
			"### What changes\n\n" +
			"1. The test takes a fixed clock, `2026-11-15 09:00 UTC`.\n" +
			"2. `parse.Dates` takes the clock as an argument instead of reading it.\n" +
			"3. Every caller passes `time.Now`.\n\n" +
			"```go\nfunc Dates(s string, now func() time.Time) ([]time.Time, error) {\n\treturn dates(s, now().Location())\n}\n```\n\n" +
			"| File | Change |\n|---|---|\n| `parse.go` | takes the clock |\n| `parse_test.go` | a fixed clock |\n| `cmd/shop/main.go` | passes `time.Now` |\n"},
		{ID: "e1", Kind: ipc.ItemTool, Time: at(5), Tool: "Edit", Phrase: "Edited parse.go", Detail: "internal/parse/parse.go", State: ipc.ItemOK, Group: "edit", Added: 9, Removed: 4, Ms: 120},
		{ID: "e2", Kind: ipc.ItemTool, Time: at(5), Tool: "Edit", Phrase: "Edited parse_test.go", Detail: "internal/parse/parse_test.go", State: ipc.ItemOK, Group: "edit", Added: 12, Removed: 3, Ms: 98},
		{ID: "b1", Kind: ipc.ItemTool, Time: at(6), Tool: "Bash", Phrase: "Ran the tests", Detail: "go test ./internal/parse/ -count=20", State: ipc.ItemError, Ms: 4200,
			Output: "--- FAIL: TestDatesAcrossMidnight (0.00s)\n    parse_test.go:88: got 2026-11-16, want 2026-11-15\nFAIL\nexit status 1"},
		{ID: "t3", Kind: ipc.ItemText, Time: at(7), Text: "One more test read the clock on its own. I'll give it the same fixed clock."},
		{ID: "e3", Kind: ipc.ItemTool, Time: at(7), Tool: "Edit", Phrase: "Edited parse_test.go", Detail: "internal/parse/parse_test.go", State: ipc.ItemOK, Added: 2, Removed: 1, Ms: 60},
	}
	cv := ipc.Conversation{Session: s.ID, Transcript: "/home/alice/src/shop/" + s.ID + ".jsonl"}
	switch s.State {
	case "working":
		items = append(items,
			ipc.Item{ID: "u2", Kind: ipc.ItemUser, Time: at(8), Text: "Also check the other packages for the same mistake.", Queued: true},
			ipc.Item{ID: "b2", Kind: ipc.ItemTool, Time: at(9), Tool: "Bash", Phrase: "Running the tests", Detail: "go test ./... -count=20", State: ipc.ItemRunning})
	case "needs_you":
		need := ipc.Item{ID: "n1", Kind: ipc.ItemNeeds, Time: at(9), Tool: "Bash", Reason: "permission", Phrase: "May it run npm install?", Detail: "npm install --save-dev @playwright/test"}
		if s.Reason == "question" {
			need = ipc.Item{ID: "n1", Kind: ipc.ItemNeeds, Time: at(9), Tool: "AskUserQuestion", Reason: "question", Phrase: "Which opening should the proposal use?",
				Options: []string{"The short one, with the price first", "The story of the last project", "A question to the client"}}
		}
		cv.Needs = &need
	case "done", "idle":
		items = append(items,
			ipc.Item{ID: "b3", Kind: ipc.ItemTool, Time: at(10), Tool: "Bash", Phrase: "Ran the tests", Detail: "go test ./... -count=20", State: ipc.ItemOK, Ms: 38000},
			ipc.Item{ID: "t4", Kind: ipc.ItemText, Time: at(11), Text: "Done. Every test passes twenty times in a row, and none of them reads the clock any more.\n\n" +
				"- `parse.Dates` takes its clock.\n- Two tests take a fixed one.\n\nThe branch has one commit, not yet in main."})
	case "frozen":
		items = append(items, ipc.Item{ID: "o1", Kind: ipc.ItemNote, Time: at(10), Text: "Frozen"})
	case "stalled", "starting":
		items = nil
		cv.Transcript = ""
	}
	cv.Items = items
	return cv
}

// screen is what a session's terminal shows: Claude Code partway through
// its task, and its prompt, or the question it asks you.
func screen(s ipc.Session) ipc.Screen {
	const (
		dim    = "\x1b[2m"
		bold   = "\x1b[1m"
		reset  = "\x1b[0m"
		orange = "\x1b[38;5;174m"
		green  = "\x1b[38;5;114m"
		red    = "\x1b[38;5;203m"
		blue   = "\x1b[38;5;111m"
	)
	box := func(colour string, inner []string) []string {
		w := cols - 2
		out := []string{colour + "╭" + strings.Repeat("─", w) + "╮" + reset}
		for _, l := range inner {
			pad := w - 1 - len([]rune(sgr.ReplaceAllString(l, "")))
			out = append(out, colour+"│"+reset+" "+l+strings.Repeat(" ", max(0, pad))+colour+"│"+reset)
		}
		return append(out, colour+"╰"+strings.Repeat("─", w)+"╯"+reset)
	}
	var lines []string
	lines = append(lines, box(orange, []string{orange + "✻" + reset + " Welcome to " + bold + "Claude Code" + reset + "!", "", dim + "  /help for help, /status for your current setup" + reset, "", dim + "  cwd: /home/alice/src/shop" + reset})...)
	lines = append(lines, "",
		dim+"> "+reset+s.Name,
		"",
		"⏺ I'll start by reading how it works now, then make the change and run the tests.",
		"",
		green+"⏺"+reset+" "+bold+"Read"+reset+"(internal/parse/parse_test.go)",
		dim+"  ⎿  Read 214 lines (ctrl+r to expand)"+reset,
		"",
		green+"⏺"+reset+" "+bold+"Update"+reset+"(internal/parse/parse_test.go)",
		dim+"  ⎿  Updated internal/parse/parse_test.go with 12 additions and 3 removals"+reset,
		"       41      func TestDates(t *testing.T) {",
		red+"       42 -        now := time.Now()"+reset,
		green+"       42 +        now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)"+reset,
		"       43          got, err := parse.Dates(\"today and tomorrow\", func() time.Time { return now })",
		"",
		red+"⏺"+reset+" "+bold+"Bash"+reset+"(go test ./internal/parse/ -count=20)",
		dim+"  ⎿  "+reset+red+"--- FAIL: TestDatesAcrossMidnight (0.00s)"+reset,
		"         parse_test.go:88: got 2026-11-16, want 2026-11-15",
		"     FAIL",
		"",
		"⏺ One more test read the clock on its own. I'll give it the same fixed clock.",
		"",
	)
	var opts []ipc.ScreenOption
	var cursor *ipc.ScreenCursor
	switch s.State {
	case "needs_you":
		if s.Reason == "question" {
			opts = []ipc.ScreenOption{{Key: "1", Label: "The short one, with the price first", Selected: true}, {Key: "2", Label: "The story of the last project"}, {Key: "3", Label: "A question to the client"}, {Key: "4", Label: "Type something."}}
			lines = append(lines, box(blue, []string{bold + "Which opening should the proposal use?" + reset, "",
				blue + "❯ 1. The short one, with the price first" + reset, "  2. The story of the last project", "  3. A question to the client", "  4. Type something."})...)
		} else {
			opts = []ipc.ScreenOption{{Key: "1", Label: "Yes", Selected: true}, {Key: "2", Label: "Yes, and don’t ask again for npm install commands in /home/alice/src/shop"}, {Key: "3", Label: "No, and tell Claude what to do differently (esc)"}}
			lines = append(lines, box(orange, []string{bold + "Bash command" + reset, "", "  npm install --save-dev @playwright/test", dim + "  Install the browser tests' runner" + reset, "",
				"Do you want to proceed?", blue + "❯ 1. Yes" + reset, "  2. Yes, and don’t ask again for npm install commands in /home/alice/src/shop", "  3. No, and tell Claude what to do differently (esc)"})...)
		}
	default:
		if s.State == "working" {
			lines = append(lines, green+"⏺"+reset+" "+bold+"Bash"+reset+"(go test ./... -count=20)", dim+"  ⎿  Running…"+reset, "",
				orange+"✶ Running the tests… "+reset+dim+"(1m 12s · ↓ 2.4k tokens · esc to interrupt)"+reset, "")
		}
		lines = append(lines, box(dim, []string{"> "})...)
		lines = append(lines, dim+"  ⏵⏵ accept edits on (shift+tab to cycle)"+reset)
		cursor = &ipc.ScreenCursor{X: 4, Y: rows - 3}
	}
	for len(lines) < rows {
		lines = append([]string{""}, lines...)
	}
	lines = lines[len(lines)-rows:]
	return ipc.Screen{Session: s.ID, Cols: cols, Rows: rows, Cursor: cursor, Reset: true, Lines: lines, Options: opts}
}
