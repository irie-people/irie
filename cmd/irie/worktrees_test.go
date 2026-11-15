package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

func TestPrintWorktreesSaysWhatCouldGoAndWhyTheRestStays(t *testing.T) {
	r := ipc.WorktreesResult{
		Trees: []ipc.ClosedTree{
			{Session: "s38aaaaaaaaaa", Path: "/home/alice/.local/share/irie/worktrees/web/fix", Branch: "irie/fix", Bytes: 9 << 20},
			{Session: "s38bbbbbbbbbb", Path: "/home/alice/.local/share/irie/worktrees/web/wip", Branch: "irie/wip", Bytes: 3 << 19, Keep: "1 file is not committed: notes.md"},
		},
		Can: 1, Bytes: 9 << 20,
	}
	var b bytes.Buffer
	if err := printWorktrees(&b, r, false, false, "/home/alice"); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"SESSION        MB   WORKTREE",
		"s38aaaaaaaaaa  9.0  ~/.local/share/irie/worktrees/web/fix  irie/fix",
		"1 worktree of closed sessions could go, 9.0 MB; their branches stay, and reopen adds each back. `" + brand.Slug + " ctl worktrees --drop` removes them. 1 worktree stays (--all says why).",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "notes.md") {
		t.Errorf("without --all it says why one stays:\n%s", out)
	}

	b.Reset()
	printWorktrees(&b, r, false, true, "/home/alice")
	if out := b.String(); !strings.Contains(out, "s38bbbbbbbbbb  1.5  ~/.local/share/irie/worktrees/web/wip  1 file is not committed: notes.md") {
		t.Errorf("--all:\n%s", out)
	}

	b.Reset()
	r.Trees[0].Dropped, r.Dropped = true, 1
	printWorktrees(&b, r, true, false, "/home/alice")
	if out := b.String(); !strings.Contains(out, "Dropped 1 worktree of closed sessions, 9.0 MB; their branches stay, and reopen adds each back.") {
		t.Errorf("--drop:\n%s", out)
	}

	b.Reset()
	printWorktrees(&b, ipc.WorktreesResult{}, false, false, "/home/alice")
	if out := b.String(); out != "No closed session's worktree can go now.\n" {
		t.Errorf("none: %q", out)
	}
}
