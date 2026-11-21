package core

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/testutil/ollamafake"
)

// sessionsFound are the sessions a find found, by id.
func sessionsFound(fs []ipc.Found) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.ID
	}
	return out
}

func findDocsOf(texts ...string) []findDoc {
	out := make([]findDoc, len(texts))
	for i, t := range texts {
		out[i] = findDoc{found: ipc.Found{ID: fmt.Sprintf("s%d", i)}, words: t}
	}
	return out
}

func TestRankFoundFusesWordsAndMeaning(t *testing.T) {
	docs := findDocsOf(
		"make the sidebar badges calmer. The badges use the calm palette now.",
		"add more vpn providers. Mullvad and Proton show in Settings.",
		"rename the parser package. Renamed parser to lexer.",
		"the reinstall must not grab focus. It waits until you look away.",
	)
	// Words alone: the session with every word is close, and the rest are
	// left out.
	got, close := rankFound("vpn providers", docs, nil, 5)
	if !close || !slices.Equal(sessionsFound(got), []string{"s1"}) || got[0].Words != 2 || got[0].Cosine != 0 {
		t.Errorf("by words = %+v, %v", got, close)
	}
	// Said another way: no word in common, but near by meaning.
	cos := []float64{0.60, 0.58, 0.55, 0.81}
	got, close = rankFound("stop the update stealing my window", docs, cos, 5)
	if !close || !slices.Equal(sessionsFound(got), []string{"s3"}) || got[0].Cosine != 0.81 || got[0].Words != 0 {
		t.Errorf("by meaning = %+v, %v", got, close)
	}
	// Nothing close: the nearest three, the nearest first.
	got, close = rankFound("zebra quartz", docs, []float64{0.61, 0.66, 0.52, 0.64}, 5)
	if close || !slices.Equal(sessionsFound(got), []string{"s1", "s3", "s0"}) || got[0].Close {
		t.Errorf("none close = %+v, %v", got, close)
	}
	// One the model has yet to read ranks by its words.
	got, close = rankFound("vpn providers", docs, []float64{0.61, unread, 0.52, 0.64}, 5)
	if !close || len(got) != 1 || got[0].ID != "s1" || got[0].Cosine != 0 {
		t.Errorf("unread = %+v, %v", got, close)
	}
	// Words and meaning together come before either alone.
	got, _ = rankFound("calm sidebar", docs, []float64{0.75, 0.74, 0.20, 0.79}, 5)
	if !slices.Equal(sessionsFound(got), []string{"s0", "s3", "s1"}) {
		t.Errorf("fused = %v, want the calm sidebar first, then the nearer by meaning", sessionsFound(got))
	}
	if got, _ := rankFound("calm sidebar", docs, []float64{0.75, 0.74, 0.20, 0.79}, 2); len(got) != 2 {
		t.Errorf("limit 2 gave %d", len(got))
	}
	// A tie keeps the newer first, as they come.
	twins := findDocsOf("fix the parser test", "fix the parser test")
	if got, _ := rankFound("parser test", twins, []float64{0.8, 0.8}, 5); !slices.Equal(sessionsFound(got), []string{"s0", "s1"}) {
		t.Errorf("tie = %v", sessionsFound(got))
	}
}

func TestSaidStartIsOnePlainLine(t *testing.T) {
	said := "> [!ASK]\n> 1. Open **Settings**\n\n## Done\nThe `vpn` list is live.\n"
	if got := saidStart(said); got != "1. Open Settings Done The vpn list is live." {
		t.Errorf("saidStart = %q", got)
	}
	if got := saidStart(strings.Repeat("word ", 100)); len([]rune(got)) != findSaid || !strings.HasSuffix(got, "…") {
		t.Errorf("a long answer is cut to %d runes: %q", len([]rune(got)), got)
	}
}

// saySession waits for a session's turn to end, then has it have said
// said, as its agent's last message.
func (c *coreEnv) saySession(id, said string) {
	c.t.Helper()
	file := filepath.Join(c.paths.Session(id), resultFile)
	testutil.Eventually(c.t, 15*time.Second, "the session's turn never ended", func() bool {
		_, err := os.Stat(file)
		return err == nil
	})
	if err := os.WriteFile(file, []byte(said), 0o600); err != nil {
		c.t.Fatal(err)
	}
}

func (c *coreEnv) find(a ipc.FindArgs) ipc.FindResult {
	c.t.Helper()
	var r ipc.FindResult
	if err := c.call(ipc.OpFind, a, &r); err != nil {
		c.t.Fatalf("find %+v: %v", a, err)
	}
	return r
}

func TestFindLooksSessionsUpByWhatTheyDid(t *testing.T) {
	f := ollamafake.New(0, []ollamafake.Model{{Name: "embed-fake", Size: 300 << 20, Embeds: true}})
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	c := setupWith(t, agentConfig{extra: fmt.Sprintf("\n[route.embed]\nmodel = \"embed-fake\"\nurl = %q\n", srv.URL)})

	badges := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop\nmake the sidebar badges calmer"})
	c.saySession(badges.ID, "The badges use the calm palette now, live as abc1234.")
	vpn := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop\nadd more vpn providers"})
	c.saySession(vpn.ID, "**Mullvad and Proton** now show in Settings.")
	c.closeSession(vpn.ID)
	// A session past what reopen reaches keeps only its folder.
	old := filepath.Join(c.paths.Sessions(), "s0old0000000a")
	testutil.WriteFiles(t, old, map[string]string{"task.md": "rename the parser package\n", "result.md": "Renamed parser to lexer.\n"})

	if err := c.call(ipc.OpFind, ipc.FindArgs{Query: "  "}, nil); err == nil {
		t.Error("found sessions for no words")
	}
	r := c.find(ipc.FindArgs{Query: "vpn providers"})
	if !r.Close || len(r.Found) != 1 {
		t.Fatalf("vpn providers = %+v", r)
	}
	got := r.Found[0]
	if got.ID != vpn.ID || got.State != foundClosed || !got.Reopen || got.Closed.IsZero() || got.Said != "Mullvad and Proton now show in Settings." || !strings.HasSuffix(got.File, filepath.Join(vpn.ID, resultFile)) {
		t.Errorf("the closed session = %+v", got)
	}
	r = c.find(ipc.FindArgs{Query: "parser lexer"})
	if len(r.Found) != 1 || r.Found[0].ID != "s0old0000000a" || r.Found[0].State != foundOld || r.Found[0].Reopen || r.Found[0].Name != "rename the parser package" {
		t.Errorf("the old session = %+v", r.Found)
	}
	// The session that asks is left out: its own task has the words.
	if r := c.find(ipc.FindArgs{Query: "calmer sidebar badges", From: badges.ID}); slices.Contains(sessionsFound(r.Found), badges.ID) {
		t.Errorf("the session that asked was found: %+v", r.Found)
	}

	// The model reads every session in the background; then meaning ranks
	// them too.
	testutil.Eventually(t, 15*time.Second, "the model never read the sessions", func() bool {
		r := c.find(ipc.FindArgs{Query: "calmer sidebar badges"})
		return r.Meaning && r.Reading == 0 && len(r.Found) > 0 && r.Found[0].ID == badges.ID && r.Found[0].Cosine > 0 && r.Found[0].State == foundOpen
	})
	if _, err := os.Stat(c.paths.FindVectors()); err != nil {
		t.Errorf("the vectors were not kept: %v", err)
	}
	// A restarted core reads them back: only the words looked up are read.
	before := f.Embeds
	c.restartCore()
	if r := c.find(ipc.FindArgs{Query: "calmer sidebar badges"}); !r.Meaning || r.Reading != 0 || r.Found[0].ID != badges.ID {
		t.Errorf("after a restart = %+v", r)
	}
	if n := f.Embeds - before; n != 1 {
		t.Errorf("after a restart the server read %d texts, want just the words looked up", n)
	}

	// With the server gone, words alone rank them, at once.
	srv.Close()
	start := time.Now()
	r = c.find(ipc.FindArgs{Query: "vpn providers"})
	if r.Meaning || !strings.Contains(r.Note, "did not answer") || !r.Close || r.Found[0].ID != vpn.ID {
		t.Errorf("with no server = %+v", r)
	}
	if d := time.Since(start); d > embedWait+2*time.Second {
		t.Errorf("with no server the find took %v", d)
	}
}
