//go:build !noyoink

package plugins

import (
	"os"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/yoink"
)

// Yoink is the yoink plugin's one part, by name.
const Yoink = "yoink"

// PaneYoink is the kind of the Yoink tab's pane: a view of the Mac app's
// own (docs/proposals/plugin-panes.md).
const PaneYoink = "yoink"

func init() { built["yoink"] = yoinkPlugin }

// yoinkPlugin is the yoink plugin: a video, or its sound, downloaded from
// a link into the person's Downloads folder, by yt-dlp. Off until asked, so
// a new Irie downloads from no site of its own accord.
func yoinkPlugin() Plugin {
	return Plugin{
		Name:    "yoink",
		Title:   "Yoink",
		Summary: "Download a video, or its sound as an mp3, from a link to YouTube, Vimeo, Wikimedia Commons and the other sites yt-dlp knows, into your Downloads folder. Sites you sign in to come later.",
		key:     "plugins.yoink",
		on:      func(c config.Config) bool { return c.Plugins.Yoink },
		Parts: []Part{
			{
				Name:    Yoink,
				Title:   "Video downloads",
				Summary: "Paste a link in the Yoink tab, or give it to :yoink in any task bar or " + brand.Slug + " yoink in a terminal, and pick a size or the sound alone: yt-dlp, pinned, downloads it, and ffmpeg merges it.",
				Brings:  []string{"the Yoink tab in the right sidebar", ":yoink [audio|720p] LINK", brand.Slug + " yoink LINK"},
				Pane:    &ipc.PluginPane{Title: "Yoink", Icon: "arrow.down.circle", Kind: PaneYoink},
				own:     always,
				check:   checkYoink,
			},
		},
	}
}

// checkYoink says whether a download can run: uvx for the pinned yt-dlp,
// or the yt-dlp yoink.ytdlp names, and ffmpeg, found on the PATH or where
// their installers put them. It reads only the file system and the
// config: it runs nothing.
func checkYoink(e Env, _ bool) (bool, string) {
	h, _ := os.UserHomeDir()
	s := yoink.Look(e.Config.Yoink, os.Getenv("PATH"), h)
	if lacks := s.Lacks(); lacks != "" {
		return false, lacks
	}
	engine := "yt-dlp " + s.Version + " through uvx"
	if s.Named != "" {
		engine = repo.Tilde(s.YtDlp, h)
	}
	detail := "Ready: " + engine + ", with ffmpeg; downloads land in " + repo.Tilde(s.Folder, h) + ". The Yoink tab, :yoink LINK or `" + brand.Slug + " yoink LINK` takes a link."
	if s.Deno == "" {
		detail += " For YouTube, yt-dlp also wants Deno, without which some of its formats may be missing: `brew install deno`."
	}
	return true, detail
}
