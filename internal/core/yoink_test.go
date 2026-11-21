//go:build !noyoink

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/yoink"
)

// yoinkCore is a core with the yoink plugin on, whose yt-dlp is the yoink
// package's stand-in, which answers by the link's words with no network,
// and whose ffmpeg and ffprobe are programs that do nothing, found on the
// PATH.
func yoinkCore(t *testing.T) (*Core, string) {
	t.Helper()
	ytdlp, err := filepath.Abs(filepath.Join("..", "yoink", "testdata", "yt-dlp"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := yoink.Places
	yoink.Places = func(string) []string { return nil }
	t.Cleanup(func() { yoink.Places = old })
	folder := t.TempDir()
	cfg := config.Default()
	cfg.Plugins.Yoink, cfg.Yoink.YtDlp, cfg.Yoink.Folder = true, ytdlp, folder
	return bareCore(t, cfg), folder
}

// yoinkDone waits for the download id to finish, and gives it.
func yoinkDone(t *testing.T, c *Core, id string) ipc.YoinkDownload {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		r, err := c.yoinkOp(0, ipc.YoinkArgs{})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range r.Downloads {
			if d.ID == id && (d.State == ipc.YoinkDone || d.State == ipc.YoinkFailed || d.State == ipc.YoinkCanceled) {
				return d
			}
		}
	}
	t.Fatalf("download %s never finished", id)
	return ipc.YoinkDownload{}
}

// TestTheYoinkOp: a link's choices, a download into the folder, :yoink in a
// task bar starting one and saying how they stand, a cancel and a clear.
func TestTheYoinkOp(t *testing.T) {
	c, folder := yoinkCore(t)
	r, err := c.yoinkOp(0, ipc.YoinkArgs{})
	if err != nil || len(r.Downloads) != 0 || r.Folder != folder || !strings.Contains(r.Text, "No downloads yet") {
		t.Fatalf("none yet: %+v, %v", r, err)
	}
	r, err = c.yoinkOp(0, ipc.YoinkArgs{Do: ipc.YoinkFormats, URL: "https://www.example.com/watch?v=abc123"})
	if err != nil || r.Info == nil || r.Info.Title != "A clip, with sound" || len(r.Info.Choices) != 5 || !strings.Contains(r.Text, "Best (1080p), 1080p, 720p, 360p, Audio only (mp3)") {
		t.Fatalf("formats: %+v, %v", r, err)
	}
	r, err = c.yoinkOp(0, ipc.YoinkArgs{Do: ipc.YoinkStart, URL: "https://www.example.com/watch?v=abc123", Get: "720p"})
	if err != nil || r.Download == nil || !strings.HasPrefix(r.Text, "Yoinking “A clip, with sound” (720p) into ") {
		t.Fatalf("start: %+v, %v", r, err)
	}
	if d := yoinkDone(t, c, r.Download.ID); d.State != ipc.YoinkDone || d.File != filepath.Join(folder, "Clip [abc123].mp4") {
		t.Errorf("done: %+v", d)
	}
	// Typed: a start runs, and :yoink alone answers how they stand.
	in, err := c.input(ipc.InputArgs{Text: ":yoink audio www.example.com/watch?v=ok"}, 0)
	if err != nil || in.Did != ipc.InputRan || in.Op != ipc.OpYoink || !strings.HasPrefix(in.Text, "Yoinking “A clip, with sound” (mp3)") {
		t.Fatalf(":yoink audio: %+v, %v", in, err)
	}
	r, _ = c.yoinkOp(0, ipc.YoinkArgs{})
	yoinkDone(t, c, r.Downloads[0].ID)
	in, err = c.input(ipc.InputArgs{Text: ":yoink"}, 0)
	if err != nil || in.Did != ipc.InputAnswered || !strings.Contains(in.Text, "“A clip, with sound” (mp3) is in") || !strings.Contains(in.Text, "Clip [abc123].mp4") {
		t.Errorf(":yoink: %+v, %v", in, err)
	}
	// A download that stops in the middle, canceled.
	r, err = c.yoinkOp(0, ipc.YoinkArgs{Do: ipc.YoinkStart, URL: "https://www.example.com/slow"})
	if err != nil {
		t.Fatal(err)
	}
	if r, err = c.yoinkOp(0, ipc.YoinkArgs{Do: ipc.YoinkCancel, ID: r.Download.ID}); err != nil || r.Download.State != ipc.YoinkCanceled || !strings.Contains(r.Text, "Canceled") {
		t.Errorf("cancel: %+v, %v", r, err)
	}
	if r, err = c.yoinkOp(0, ipc.YoinkArgs{Do: ipc.YoinkClear}); err != nil || len(r.Downloads) != 0 || !strings.Contains(r.Text, "Cleared 3") {
		t.Errorf("clear: %+v, %v", r, err)
	}
	// What the op refuses, in sentences.
	for a, says := range map[ipc.YoinkArgs]string{
		{Do: ipc.YoinkStart, URL: "file:///etc/passwd"}:                 "Yoink takes http and https links only",
		{Do: ipc.YoinkStart, URL: "https://example.com/v", Get: "flac"}: "isn't a choice Yoink has",
		{Do: ipc.YoinkFormats, URL: "https://example.com/missing"}:      "Unable to download webpage: HTTP Error 404: Not Found.",
		{Do: ipc.YoinkFormats, URL: "https://example.com/playlist"}:     "That link is a playlist of 12 videos",
		{Do: ipc.YoinkCancel, ID: "00000000"}:                           "There is no download",
		{Do: "explode"}:                                                 "The yoink op takes formats, start, cancel or clear",
	} {
		if _, err := c.yoinkOp(0, a); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%+v: %v; want %q", a, err, says)
		}
	}
}

// TestAnAgentCannotYoink: a link is the person's own word, so every call
// from inside an agent is refused, a look as much as a start, typed words
// too; and the plugin off, or a computer without ffmpeg, refuses with what
// fixes it.
func TestAnAgentCannotYoink(t *testing.T) {
	c, folder := yoinkCore(t)
	// This test's own parent stands for an agent's host.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	for _, a := range []ipc.YoinkArgs{{}, {Do: ipc.YoinkFormats, URL: "https://www.example.com/watch?v=ok"}, {Do: ipc.YoinkStart, URL: "https://www.example.com/watch?v=ok"}} {
		if _, err := c.yoinkOp(os.Getpid(), a); err == nil || !strings.Contains(err.Error(), "an agent can't look at, start or stop a download") {
			t.Errorf("%+v from an agent: %v", a, err)
		}
	}
	if _, err := c.CallFrom(os.Getpid(), ipc.OpYoink, []byte(`{"do":"start","url":"https://www.example.com/watch?v=ok"}`)); err == nil {
		t.Error("the op from an agent went through")
	}
	if _, err := c.input(ipc.InputArgs{Text: ":yoink https://www.example.com/watch?v=ok"}, os.Getpid()); err == nil {
		t.Error(":yoink from an agent went through")
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 0 {
		t.Errorf("the folder has %v", entries)
	}
	if _, err := c.yoinkOp(0, ipc.YoinkArgs{}); err != nil {
		t.Errorf("the person's own call: %v", err)
	}

	off := config.Default()
	c = bareCore(t, off)
	if _, err := c.yoinkOp(0, ipc.YoinkArgs{}); err == nil || !strings.Contains(err.Error(), "plugins on yoink") {
		t.Errorf("plugin off: %v", err)
	}
	if _, err := c.input(ipc.InputArgs{Text: ":yoink https://example.com/v"}, 0); err == nil || !strings.Contains(err.Error(), "plugins on yoink") {
		t.Errorf(":yoink with the plugin off: %v", err)
	}

	on := config.Default()
	on.Plugins.Yoink, on.Yoink.YtDlp = true, "/nowhere/yt-dlp"
	t.Setenv("PATH", t.TempDir())
	c = bareCore(t, on)
	if _, err := c.yoinkOp(0, ipc.YoinkArgs{Do: ipc.YoinkStart, URL: "https://example.com/v"}); err == nil || !strings.Contains(err.Error(), "yoink.ytdlp names /nowhere/yt-dlp") || !strings.Contains(err.Error(), "brew install ffmpeg") {
		t.Errorf("nothing installed: %v", err)
	}
}
