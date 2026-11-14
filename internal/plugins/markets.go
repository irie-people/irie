//go:build !nomarkets

package plugins

import (
	"os"
	"path/filepath"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/repo"
)

// Map is the markets plugin's one part, by name.
const Map = "map"

// MapPage is the map's page in its folder.
const MapPage = "index.html"

func init() { built["markets"] = markets }

// markets is the markets plugin: a map of your own market research, kept
// in a folder of yours, in a window of its own, with sessions that work on
// it there.
func markets() Plugin {
	return Plugin{
		Name:    "markets",
		Title:   "Markets",
		Summary: "Your market research as a map: a page you keep in a folder of your own, in a window beside your sessions, with sessions that keep it current.",
		key:     "plugins.markets",
		on:      func(c config.Config) bool { return c.Plugins.Markets },
		Parts: []Part{
			{
				Name:    Map,
				Title:   "Market map",
				Summary: "Window › Markets in the Mac app, or :markets, shows the index.html in markets.folder, again each time it is rebuilt; :markets TASK starts a session that works in that folder.",
				Brings:  []string{":markets [TASK]", "Window › Markets", "the Markets window"},
				own:     func(config.Config) bool { return true },
				check:   checkMap,
			},
		},
	}
}

// Where is where the market map is: markets.folder, ~ expanded, and its
// page, with when the page last changed while it is there.
type Where struct {
	Folder, Page string
	Changed      time.Time
}

// FindMap finds the market map that cfg names. What it lacks, and what
// fixes that, is in a sentence, or "" when the page is there. A folder that
// is there without its page can still take a session that makes one.
func FindMap(cfg config.Config) (Where, string) {
	if cfg.Markets.Folder == "" {
		return Where{}, "No folder for the map yet: choose one in the Markets window (Window › Markets in the Mac app), or set markets.folder in config.toml to the folder that holds your map's " + MapPage + "."
	}
	h, _ := os.UserHomeDir()
	w := Where{Folder: expand(cfg.Markets.Folder)}
	w.Page = filepath.Join(w.Folder, MapPage)
	if st, err := os.Stat(w.Folder); err != nil || !st.IsDir() {
		return w, "markets.folder names " + repo.Tilde(w.Folder, h) + ", which isn't a folder here: choose another in the Markets window, or change markets.folder in config.toml."
	}
	st, err := os.Stat(w.Page)
	if err != nil || st.IsDir() {
		return w, "No map in " + repo.Tilde(w.Folder, h) + " yet: the window shows its " + MapPage + ". `:markets TASK` starts a session there that can make one."
	}
	w.Changed = st.ModTime()
	return w, ""
}

// HasFolder says whether w's folder is there, for a session to work in.
func (w Where) HasFolder() bool {
	st, err := os.Stat(w.Folder)
	return w.Folder != "" && err == nil && st.IsDir()
}

func checkMap(e Env, _ bool) (bool, string) {
	w, why := FindMap(e.Config)
	if why != "" {
		return false, why
	}
	h, _ := os.UserHomeDir()
	return true, "Ready: your map is " + repo.Tilde(w.Page, h) + ", changed " + w.Changed.Local().Format("2 Jan 15:04") + ". Window › Markets in the Mac app, or :markets, shows it; :markets TASK starts a session in its folder."
}
