package jot

import "testing"

func TestFollows(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		// It goes on from the jot before.
		{"also add a test for it", true},
		{"Also, make the button blue", true},
		{"oh and make sure it works offline", true},
		{"Oh, and the error only shows on the second click", true},
		{"and push it when you're done", true},
		{"btw the repo is the web one", true},
		{"P.S. don't touch the tests", true},
		{"ps: it should be behind a flag", true},
		{"forgot to say: use the staging database", true},
		{"I forgot the screenshot is on the desktop", true},
		{"I meant the Mac app, not the sidebar", true},
		{"actually use sqlite instead", true},
		{"make sure it pushes the tags too", true},
		{"don’t merge it to main", true},
		{"Do not push", true},
		{"one more thing: keep the old flag working", true},
		{"same for the sidebar", true},
		{"then run tools/live.sh", true},
		{"More context: the bug started after the last install", true},
		{"update: it’s only on the second monitor", true},
		{"context: we ship on Friday", true},
		{"can you also check the TUI", true},
		{"please also update the docs", true},
		{"- also the footer", true},
		// It points back at the jot before.
		{"it’s in auth_test.go, the retry one", true},
		{"its the one with the red border", true},
		{"that should also cover the empty state", true},
		{"these are the phrases:\n- one\n- two", true},
		{"Here's the log", true},
		{"for that one, use the green palette", true},
		{"^ in the web repo", true},
		{"↑ the second one", true},
		// A link, a path or an error message alone.
		{"https://github.com/example/repo/issues/12", true},
		{"~/Desktop/screenshot.png", true},
		{"/home/alice/notes.txt", true},
		{"TypeError: cannot read properties of undefined (reading 'id')", true},
		{"error[E0308]: mismatched types", true},
		{"panic: runtime error: index out of range [3] with length 3", true},
		{"fatal: not a git repository", true},
		{"Traceback (most recent call last):\n  File \"x.py\", line 1", true},

		// A task of its own.
		{"deep research lovable", false},
		{"deep research open source crm", false},
		{"can we fix the colors on the tabs", false},
		{"research a faster git alternative", false},
		{"i want an orchestrator to show its subagents", false},
		{"lets write a post introducing the app", false},
		{"the unpark doesn't work, can you look", false},
		{"update the README with the new flags", false},
		{"edit the config so the sidebar is wider", false},
		{"note taking for sessions", false},
		{"android build is broken", false},
		{"orchestrate the release", false},
		{"thematic colors for the site", false},
		{"error handling in the api needs work", false},
		{"make an app icon", false},
		{"wait where did my operator go?", false},
		{"is the build green", false},
		{"what is the status of the release", false},
		{"", false},
		{"   \n  ", false},
	} {
		why, got := Follows(c.text)
		if got != c.want {
			t.Errorf("Follows(%q) = %v (%s), want %v", c.text, got, why, c.want)
		}
		if got && why == "" {
			t.Errorf("Follows(%q) gives no reason", c.text)
		}
	}
}
