//go:build !noyoink

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/yoink"
)

func yoinkStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "yoink" {
			return p
		}
	}
	t.Fatal("no yoink plugin")
	return ipc.Plugin{}
}

// TestYoinkIsOffByDefault: a new Irie downloads from no site until asked,
// and the words of its part say which switch turns it on.
func TestYoinkIsOffByDefault(t *testing.T) {
	e := env(t)
	p := yoinkStatus(t, e)
	if p.On || p.Title != "Yoink" || len(p.Parts) != 1 {
		t.Fatalf("yoink = %+v", p)
	}
	x := part(t, p, Yoink)
	if x.State != ipc.PartPluginOff || x.Switch != "" || x.Title != "Video downloads" {
		t.Errorf("part = %+v", x)
	}
	if x.Pane == nil || x.Pane.ID != "yoink.yoink" || x.Pane.Kind != PaneYoink || x.Pane.Title != "Yoink" || x.Pane.Store != "" {
		t.Errorf("pane = %+v", x.Pane)
	}
	if why := Why(e.Config, Yoink); !strings.Contains(why, "irie plugins on yoink") {
		t.Errorf("Why = %q", why)
	}
	if ed, err := Edits("yoink", "", true); err != nil || len(ed) != 1 || ed[0].Key != "plugins.yoink" || ed[0].Value != true {
		t.Errorf("Edits = %+v, %v", ed, err)
	}
	if _, err := Edits("yoink", Yoink, true); err == nil || !strings.Contains(err.Error(), "no switch of its own") {
		t.Errorf("Edits of the part = %v", err)
	}
}

// TestVideoDownloadsNeedUvxAndFfmpeg: on, the part says which of uvx and
// ffmpeg it lacks and how to get each, is ready once both are found, and
// asks for Deno only for YouTube's sake; a yt-dlp of yoink.ytdlp's stands
// in for uvx.
func TestVideoDownloadsNeedUvxAndFfmpeg(t *testing.T) {
	e := env(t)
	e.Config.Plugins.Yoink = true
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	old := yoink.Places
	yoink.Places = func(string) []string { return nil }
	t.Cleanup(func() { yoink.Places = old })
	state := func() ipc.PluginPart { return part(t, yoinkStatus(t, e), Yoink) }

	x := state()
	if x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "needs uvx") || !strings.Contains(x.Detail, "brew install uv") || !strings.Contains(x.Detail, "needs ffmpeg") || !strings.Contains(x.Detail, "brew install ffmpeg") {
		t.Errorf("nothing installed: %+v", x)
	}
	if why := Why(e.Config, Yoink); why != "" {
		t.Errorf("Why with the plugin on = %q", why)
	}
	exe(t, bin, "uvx")
	if x := state(); x.State != ipc.PartNeeds || strings.Contains(x.Detail, "uvx") || !strings.Contains(x.Detail, "needs ffmpeg") {
		t.Errorf("uvx alone: %+v", x)
	}
	exe(t, bin, "ffmpeg")
	if x := state(); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "needs ffprobe") {
		t.Errorf("ffmpeg without ffprobe: %+v", x)
	}
	exe(t, bin, "ffprobe")
	x = state()
	if x.State != ipc.PartReady || !strings.Contains(x.Detail, "yt-dlp 2026.10.26 through uvx") || !strings.Contains(x.Detail, "Downloads") || !strings.Contains(x.Detail, "brew install deno") {
		t.Errorf("ready: %+v", x)
	}
	exe(t, bin, "deno")
	if x := state(); x.State != ipc.PartReady || strings.Contains(x.Detail, "deno") {
		t.Errorf("with Deno: %+v", x)
	}
	e.Config.Yoink.YtDlp = filepath.Join(t.TempDir(), "yt-dlp")
	if x := state(); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "yoink.ytdlp names") {
		t.Errorf("a yt-dlp that isn't there: %+v", x)
	}
	exe(t, filepath.Dir(e.Config.Yoink.YtDlp), "yt-dlp")
	if x := state(); x.State != ipc.PartReady || !strings.Contains(x.Detail, "yt-dlp,") {
		t.Errorf("yoink.ytdlp's yt-dlp: %+v", x)
	}
	// Off, the plugin keeps its settings for when it is on again.
	e.Config.Plugins.Yoink = false
	if x := state(); x.State != ipc.PartPluginOff || e.Config.Yoink.YtDlp == "" {
		t.Errorf("plugin off: %+v", x)
	}
}

// exe makes a program named name in dir.
func exe(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
