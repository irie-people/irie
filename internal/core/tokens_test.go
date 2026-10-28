package core

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

func reply(id string, in, out, cacheRead, cacheWrite int) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"role":"assistant","usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`+"\n",
		id, in, out, cacheRead, cacheWrite)
}

func TestTokenTallyCountsEachReplyOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	appendTo := func(s string) {
		t.Helper()
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	var tally tokenTally
	if _, ok := tally.update([]string{path}); ok {
		t.Fatal("usage before the transcript exists")
	}

	appendTo(`{"type":"user","message":{"role":"user","content":"hi"}}` + "\n")
	appendTo(reply("m1", 10, 5, 100, 20))
	appendTo(reply("m1", 10, 5, 100, 20)) // the same reply's next content block
	half := reply("m2", 3, 7, 0, 0)
	appendTo(half[:30])
	got, ok := tally.update([]string{path})
	if want := (ipc.Tokens{Input: 10, Output: 5, CacheRead: 100, CacheWrite: 20}); !ok || got != want {
		t.Fatalf("tokens %+v (%v), want %+v", got, ok, want)
	}

	// The rest of the half-written line arrives.
	appendTo(half[30:])
	got, _ = tally.update([]string{path})
	if want := (ipc.Tokens{Input: 13, Output: 12, CacheRead: 100, CacheWrite: 20}); got != want {
		t.Fatalf("after the rest of the line: %+v, want %+v", got, want)
	}

	// A finished subagent's transcript adds to the session's.
	sub := filepath.Join(dir, "agent-a1b2c3.jsonl")
	if err := os.WriteFile(sub, []byte(reply("a1", 1, 1, 1, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ = tally.update([]string{path, sub})
	if want := (ipc.Tokens{Input: 14, Output: 13, CacheRead: 101, CacheWrite: 21}); got != want {
		t.Fatalf("with the subagent: %+v, want %+v", got, want)
	}

	// A transcript replaced by a shorter one is counted again from the start.
	if err := os.WriteFile(path, []byte(reply("m9", 2, 2, 0, 0)), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ = tally.update([]string{path, sub})
	if want := (ipc.Tokens{Input: 3, Output: 3, CacheRead: 1, CacheWrite: 1}); got != want {
		t.Fatalf("after the transcript was replaced: %+v, want %+v", got, want)
	}
}
