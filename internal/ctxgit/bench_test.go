package ctxgit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// longSession writes a synthetic session of turns turns, each a prompt, a
// dozen tool calls with 1 KiB results and a reply: about 26 lines and
// 20 KiB a turn.
func longSession(b *testing.B, dir string, turns int) *tx {
	b.Helper()
	t := &testing.T{}
	x := newTx(t, dir, "sess-a")
	result := strings.Repeat("x", 1024)
	for i := 0; i < turns; i++ {
		x.prompt(fmt.Sprintf("step %d: change the parser", i))
		for j := 0; j < 12; j++ {
			x.add(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
				map[string]any{"type": "tool_use", "id": fmt.Sprint("toolu_", i, j), "name": "Bash", "input": map[string]any{"command": "go test ./..."}}}}})
			x.add(map[string]any{"type": "user", "toolUseResult": map[string]any{"stdout": result}, "message": map[string]any{"content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": fmt.Sprint("toolu_", i, j), "content": result}}}})
		}
		x.reply(fmt.Sprintf("Done with step %d.", i))
	}
	return x
}

func size(path string) string {
	fi, _ := os.Stat(path)
	return fmt.Sprintf("%.1f MB", float64(fi.Size())/1e6)
}

// BenchmarkSyncFirst stores a 300-turn session that the store has never
// read: what a session's first turn end after an install costs.
func BenchmarkSyncFirst(b *testing.B) {
	x := longSession(b, b.TempDir(), 300)
	b.Logf("transcript %s, %d lines", size(x.path), x.n)
	for b.Loop() {
		st, err := Open(filepath.Join(b.TempDir(), "context.git"))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := st.Sync(Session{ID: "s0bench", Transcripts: []string{x.path}}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSyncTurn stores one more turn of a 300-turn session the store
// has read: what every turn end costs.
func BenchmarkSyncTurn(b *testing.B) {
	x := longSession(b, b.TempDir(), 300)
	st, err := Open(filepath.Join(b.TempDir(), "context.git"))
	if err != nil {
		b.Fatal(err)
	}
	s := Session{ID: "s0bench", Transcripts: []string{x.path}}
	if _, err := st.Sync(s); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		b.StopTimer()
		x.prompt("one more step")
		x.tool("Bash", "go test ./...")
		x.reply("Done.")
		b.StartTimer()
		if _, err := st.Sync(s); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTranscript reads a 300-turn context back as a fork's
// transcript: what a fork costs before its agent starts.
func BenchmarkTranscript(b *testing.B) {
	x := longSession(b, b.TempDir(), 300)
	st, err := Open(filepath.Join(b.TempDir(), "context.git"))
	if err != nil {
		b.Fatal(err)
	}
	head, err := st.Sync(Session{ID: "s0bench", Transcripts: []string{x.path}})
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		lines, err := st.Transcript(head, "")
		if err != nil {
			b.Fatal(err)
		}
		ForkLines(lines, "fork-id", "/home/alice/app", "/home/alice/forks/app")
	}
}
