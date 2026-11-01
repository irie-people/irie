package core

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

func TestAPeersFinishedWorkIsFetchedBackBesideYourBranches(t *testing.T) {
	testutil.IsolateGit(t)
	defer func(f func(string, string) string) { peerGitURL = f }(peerGitURL)
	peerGitURL = func(_, path string) string { return path } // the peer's checkout, on disk

	// The checkout on the other machine, where a task left a commit on its
	// branch; and the repo of the same name here, with a branch of its own
	// that happens to have that name.
	there := testutil.NewRepo(t, filepath.Join(t.TempDir(), "irie"), map[string]string{"a.txt": "one\n"})
	testutil.Git(t, there, "checkout", "-q", "-b", "irie/fix-x")
	testutil.WriteFiles(t, there, map[string]string{"a.txt": "two\n"})
	testutil.Git(t, there, "commit", "-q", "-am", "Fix x")
	work := testutil.Git(t, there, "rev-parse", "HEAD")
	testutil.Git(t, there, "checkout", "-q", "main")
	here := filepath.Join(t.TempDir(), "irie")
	testutil.Git(t, filepath.Dir(here), "clone", "-q", there, here)
	testutil.Git(t, here, "branch", "irie/fix-x", "main")
	mine := testutil.Git(t, here, "rev-parse", "irie/fix-x")

	c := newPeerCore(t)
	c.cfg().Repos.Aliases = map[string]string{"irie": here}
	p := &peerState{name: "Asahi", up: true, stop: func() {}, rows: map[string]ipc.Session{},
		fetchq: make(chan ipc.Session, 8), done: map[string]bool{}, fetched: map[string]fetched{}}
	p.conf.SSH = "asahi"
	c.peers = map[string]*peerState{"Asahi": p}
	stop := make(chan struct{})
	defer close(stop)
	go c.fetchLoop(p, stop)

	// The turn runs, then ends: its branch is fetched, and the row says so.
	row := ipc.Session{ID: "s1", Name: "fix x", State: ipc.StateWorking, Repo: there, Branch: "irie/fix-x"}
	c.peerFrame(p, ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{row}})
	row.State = ipc.StateDone
	c.peerFrame(p, ipc.Frame{T: ipc.FrameUpsert, Session: &row})
	testutil.Eventually(t, 20*time.Second, "the work was never fetched", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return p.fetched["s1"].head != ""
	})
	if got := testutil.Git(t, here, "rev-parse", "refs/remotes/Asahi/irie/fix-x"); got != work {
		t.Errorf("fetched %s, want %s", got, work)
	}
	if got := testutil.Git(t, here, "rev-parse", "irie/fix-x"); got != mine {
		t.Errorf("the branch of this machine's moved to %s", got)
	}
	c.mu.Lock()
	detail := c.list()[0].Detail
	c.mu.Unlock()
	if detail != "on Asahi · done · fetched as Asahi/irie/fix-x" {
		t.Errorf("detail = %q", detail)
	}

	// irie peer fetch does it at once, and says why it can't.
	got := c.fetchNow("Asahi/s1")
	if len(got) != 1 || got[0].Ref != "Asahi/irie/fix-x" || got[0].Head != work || got[0].Problem != "" {
		t.Errorf("fetch now = %+v", got)
	}
	c.mu.Lock()
	p.conf.SSH = ""
	c.mu.Unlock()
	if got := c.fetchNow("all"); len(got) != 1 || !strings.Contains(got[0].Problem, "--ssh") {
		t.Errorf("without an ssh name: %+v", got)
	}
	c.mu.Lock()
	p.conf.SSH = "asahi"
	p.rows["s2"] = ipc.Session{ID: "s2", State: ipc.StateDone, Repo: "/home/alice/src/website", Branch: "irie/copy"}
	c.mu.Unlock()
	if got := c.fetchNow("Asahi/s2"); len(got) != 1 || !strings.Contains(got[0].Problem, "no repo here called website") {
		t.Errorf("a repo this machine lacks: %+v", got)
	}
}
