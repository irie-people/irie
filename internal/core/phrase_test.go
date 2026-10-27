package core

import (
	"encoding/json"
	"testing"
)

func bash(command, description string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": command, "description": description})
	return b
}

func input(kv ...string) json.RawMessage {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	b, _ := json.Marshal(m)
	return b
}

func TestPhrase(t *testing.T) {
	for _, c := range []struct {
		tool        string
		input       json.RawMessage
		doing, done string
	}{
		{"Bash", bash("go test ./...", ""), "Running the tests", "Ran the tests"},
		{"Bash", bash("cd internal/core && go test -run TestPhrase ./...", ""), "Running the tests", "Ran the tests"},
		{"Bash", bash("go vet ./... && go test ./... 2>&1 | tail -30", ""), "Running the tests", "Ran the tests"},
		{"Bash", bash("pnpm test", ""), "Running the tests", "Ran the tests"},
		{"Bash", bash("npx vitest run", ""), "Running the tests", "Ran the tests"},
		{"Bash", bash("python3 -m pytest -q", ""), "Running the tests", "Ran the tests"},
		{"Bash", bash(`bash -c "go test ./..."`, ""), "Running the tests", "Ran the tests"},
		{"Bash", bash("make test", ""), "Running the tests", "Ran the tests"},
		{"Bash", bash("make", ""), "Building the project", "Built the project"},
		{"Bash", bash("mac/build.sh", ""), "Building the project", "Built the project"},
		{"Bash", bash("swiftc -O -o build/Irie Sources/Irie/*.swift", ""), "Building the project", "Built the project"},
		{"Bash", bash("npm run build", ""), "Building the project", "Built the project"},
		{"Bash", bash("npm install", ""), "Installing packages", "Installed packages"},
		{"Bash", bash("brew install tmux", ""), "Installing packages", "Installed packages"},
		{"Bash", bash("gofmt -l .", ""), "Formatting the code", "Formatted the code"},
		{"Bash", bash("git status", ""), "Checking what changed", "Checked what changed"},
		{"Bash", bash("git diff --stat", ""), "Looking at the changes", "Looked at the changes"},
		{"Bash", bash("git log --oneline | head -20", ""), "Reading the history", "Read the history"},
		{"Bash", bash(`git add -A && TZ=UTC git commit -s -m "fix: a && b"`, ""), "Committing the changes", "Committed the changes"},
		{"Bash", bash("git push -u origin main", ""), "Pushing the changes", "Pushed the changes"},
		{"Bash", bash("git checkout -b irie/phrases", ""), "Making the branch irie/phrases", "Made the branch irie/phrases"},
		{"Bash", bash("git merge live", ""), "Merging live", "Merged live"},
		{"Bash", bash("git stash", ""), "Setting the changes aside", "Set the changes aside"},
		{"Bash", bash("gh pr create --fill", ""), "Opening a pull request", "Opened a pull request"},
		{"Bash", bash(`rg -n "Theme" mac/Sources`, ""), "Searching for “Theme”", "Searched for “Theme”"},
		{"Bash", bash(`grep -rn --include=*.go -e 'func Phrase' .`, ""), "Searching for “func Phrase”", "Searched for “func Phrase”"},
		{"Bash", bash(`find . -name "*.swift"`, ""), "Looking for .swift files", "Looked for .swift files"},
		{"Bash", bash("ls -la mac/Sources", ""), "Looking in Sources", "Looked in Sources"},
		{"Bash", bash("ls", ""), "Looking at the files", "Looked at the files"},
		{"Bash", bash("sed -n '1,80p' internal/core/state.go", ""), "Reading state.go", "Read state.go"},
		{"Bash", bash("cat README.md docs/MAC.md", ""), "Reading README.md and MAC.md", "Read README.md and MAC.md"},
		{"Bash", bash("sed -i '' 's/old/new/' a.go b.go c.go", ""), "Editing a.go and 2 more", "Edited a.go and 2 more"},
		{"Bash", bash("cat > notes.md <<'EOF'\n# Notes\ngo test ./...\nEOF", ""), "Writing notes.md", "Wrote notes.md"},
		{"Bash", bash(`echo "go test" > script.txt`, ""), "Writing script.txt", "Wrote script.txt"},
		{"Bash", bash("rm -rf build", ""), "Deleting build", "Deleted build"},
		{"Bash", bash("mkdir -p mac/build", ""), "Making the build folder", "Made the build folder"},
		{"Bash", bash("chmod +x tools/live.sh", ""), "Making live.sh runnable", "Made live.sh runnable"},
		{"Bash", bash("curl -fsSL https://www.example.com/a.json | jq .name", ""), "Fetching example.com", "Fetched example.com"},
		{"Bash", bash("sleep 5", ""), "Waiting 5 seconds", "Waited 5 seconds"},
		{"Bash", bash("pkill -x Irie", ""), "Stopping Irie", "Stopped Irie"},
		{"Bash", bash("for f in *.go; do gofmt -l $f; done", ""), "Formatting the code", "Formatted the code"},
		{"Bash", bash("frob --all", ""), "Running frob", "Ran frob"},
		{"Bash", bash("frob --all", "Fetch the latest prices."), "Fetching the latest prices", "Fetched the latest prices"},
		{"Bash", bash("./tools/live.sh", "Install the build into the running Irie"), "Installing the build into the running Irie", "Installed the build into the running Irie"},
		{"Bash", bash("frob --all", "Prices for today"), "Running frob", "Ran frob"},
		{"Bash", bash("", ""), "Running a command", "Ran a command"},
		{"Read", input("file_path", "/home/alice/irie/mac/Sources/Irie/Store.swift"), "Reading Store.swift", "Read Store.swift"},
		{"Edit", input("file_path", "/home/alice/src/main.rs", "old_string", "a", "new_string", "b"), "Editing main.rs", "Edited main.rs"},
		{"Write", input("file_path", "notes.md", "content", "hi"), "Writing notes.md", "Wrote notes.md"},
		{"Grep", input("pattern", "Theme"), "Searching for “Theme”", "Searched for “Theme”"},
		{"Glob", input("pattern", "**/*.swift"), "Looking for .swift files", "Looked for .swift files"},
		{"Glob", input("pattern", "src/**/*.test.ts"), "Looking for “src/**/*.test.ts”", "Looked for “src/**/*.test.ts”"},
		{"WebFetch", input("url", "https://www.example.com/docs", "prompt", "x"), "Reading example.com", "Read example.com"},
		{"WebSearch", input("query", "swift ImageRenderer"), "Searching the web for “swift ImageRenderer”", "Searched the web for “swift ImageRenderer”"},
		{"Agent", input("description", "Find the config loader", "subagent_type", "Explore"), "Asking a helper to find the config loader", "Asked a helper to find the config loader"},
		{"AskUserQuestion", json.RawMessage(`{"questions":[{"question":"Which colour should the button be?","header":"Colour","options":[],"multiSelect":false}]}`),
			"Asks: which colour should the button be?", "Asked: which colour should the button be?"},
		{"AskUserQuestion", nil, "Asking you a question", "Asked you a question"},
		{"ExitPlanMode", input("plan", "1. Do it"), "Asks: may it go ahead with its plan?", "Shared its plan"},
		{"TodoWrite", nil, "Updating its to-do list", "Updated its to-do list"},
		{"Skill", input("skill", "deploy"), "Using the deploy skill", "Used the deploy skill"},
		{"mcp__github__create_issue", nil, "Using github (create issue)", "Used github (create issue)"},
		{"Frobnicate", nil, "Using Frobnicate", "Used Frobnicate"},
		{"Edit", nil, "Editing a file", "Edited a file"},
		{"Edit", json.RawMessage(`[1,2]`), "Editing a file", "Edited a file"},
		{"Read", json.RawMessage(`{"file_path":7}`), "Reading a file", "Read a file"},
	} {
		if got := Phrase(c.tool, c.input); got != c.doing {
			t.Errorf("Phrase(%s, %s) = %q, want %q", c.tool, c.input, got, c.doing)
		}
		if got := PhraseDone(c.tool, c.input); got != c.done {
			t.Errorf("PhraseDone(%s, %s) = %q, want %q", c.tool, c.input, got, c.done)
		}
	}
}

func TestPhraseAsk(t *testing.T) {
	for _, c := range []struct {
		tool  string
		input json.RawMessage
		want  string
	}{
		{"Bash", bash("rm -rf build", "Remove the build directory"), "Asks: may it run rm -rf build?"},
		{"Bash", bash("go test ./...\ngo vet ./...", ""), "Asks: may it run go test ./... …?"},
		{"Bash", nil, "Asks: may it run a command?"},
		{"Edit", input("file_path", "/home/alice/src/main.rs"), "Asks: may it edit main.rs?"},
		{"WebFetch", input("url", "https://example.com/a"), "Asks: may it read example.com?"},
		{"AskUserQuestion", json.RawMessage(`{"questions":[{"question":"May it run rm -rf?"}]}`), "Asks: may it run rm -rf?"},
		{"ExitPlanMode", nil, "Asks: may it go ahead with its plan?"},
	} {
		if got := PhraseAsk(c.tool, c.input); got != c.want {
			t.Errorf("PhraseAsk(%s, %s) = %q, want %q", c.tool, c.input, got, c.want)
		}
	}
}

func TestVerbForms(t *testing.T) {
	for _, c := range []struct{ verb, ing, past string }{
		{"run", "running", "ran"}, {"make", "making", "made"}, {"copy", "copying", "copied"},
		{"stop", "stopping", "stopped"}, {"commit", "committing", "committed"}, {"open", "opening", "opened"},
		{"see", "seeing", "saw"}, {"tie", "tying", "tied"}, {"tidy", "tidying", "tidied"},
		{"deploy", "deploying", "deployed"}, {"free", "freeing", "freed"}, {"format", "formatting", "formatted"},
		{"sync", "syncing", "synced"}, {"fix", "fixing", "fixed"}, {"look", "looking", "looked"},
		{"edit", "editing", "edited"}, {"quit", "quitting", "quit"}, {"grep", "grepping", "grepped"},
	} {
		if got := ing(c.verb); got != c.ing {
			t.Errorf("ing(%s) = %s, want %s", c.verb, got, c.ing)
		}
		if got := past(c.verb); got != c.past {
			t.Errorf("past(%s) = %s, want %s", c.verb, got, c.past)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	got := splitCommand("A=1 go test ./... 2>&1 | tee log.txt; echo 'a;b' > out.txt # done\ncat <<EOF\nrm -rf /\nEOF\nls")
	want := []simple{
		{args: []string{"A=1", "go", "test", "./..."}},
		{args: []string{"tee", "log.txt"}},
		{args: []string{"echo", "a;b"}, out: "out.txt"},
		{args: []string{"cat"}},
		{args: []string{"ls"}},
	}
	if len(got) != len(want) {
		t.Fatalf("splitCommand = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].out != want[i].out || len(got[i].args) != len(want[i].args) {
			t.Errorf("command %d = %+v, want %+v", i, got[i], want[i])
			continue
		}
		for j := range want[i].args {
			if got[i].args[j] != want[i].args[j] {
				t.Errorf("command %d = %+v, want %+v", i, got[i], want[i])
				break
			}
		}
	}
}
