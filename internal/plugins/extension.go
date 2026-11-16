//go:build !noextension

package plugins

import (
	"os"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/extension"
	"github.com/irie-people/irie/internal/repo"
)

// The browser extension plugin's parts, by name.
const (
	Extension = "extension"
	Terms     = "terms"
)

func init() { built["extension"] = extensionPlugin }

// extensionPlugin is the browser extension plugin: Irie in your own
// browser, a side panel of tools that run on this computer
// (docs/proposals/browser-extension.md).
func extensionPlugin() Plugin {
	return Plugin{
		Name:    "extension",
		Title:   "Browser extension",
		Summary: brand.Name + " in your own browser: a side panel of tools that run on this computer, so what you read is never sent anywhere.",
		Setup:   ":setup extension",
		key:     "plugins.extension",
		on:      func(c config.Config) bool { return c.Plugins.Extension },
		Parts: []Part{
			{
				Name:    Extension,
				Title:   brand.Name + " in your browser",
				Summary: "The extension for Chrome, Brave, Edge, Arc and Firefox, and its one way into " + brand.Name + " on this computer: its button opens a side panel beside the page you're on.",
				Setup:   ":setup extension",
				Brings:  []string{"the browser extension", brand.Slug + " setup extension", brand.Slug + " extension"},
				own:     func(config.Config) bool { return true },
				check:   checkExtension,
			},
			{
				Name:    Terms,
				Title:   "Terms in short",
				Summary: "A site's terms of service or privacy policy in plain words: the points that matter, each with the clause behind it, and answers to your questions about them. " + brand.Name + "'s own code reads them, with no tokens spent; a local model words answers only when you ask.",
				Brings:  []string{"the extension's Terms tool", brand.Slug + " terms ADDRESS", brand.Slug + " terms ask ADDRESS QUESTION"},
				own:     func(config.Config) bool { return true },
				check: func(e Env, on bool) (bool, string) {
					return true, "Ready: press " + brand.Name + "'s button on a site in your browser, or run `" + brand.Slug + " terms example.com` in a terminal."
				},
			},
		},
	}
}

// checkExtension says how the extension stands, from its files: its
// folder, the browsers the host is set up for, and when each last talked
// to Irie.
func checkExtension(e Env, _ bool) (bool, string) {
	h, _ := os.UserHomeDir()
	s := extension.Look(e.Paths, h)
	folder := repo.Tilde(s.Folder, h)
	switch {
	case s.Written == "" || len(s.Browsers) == 0:
		return false, "Not set up yet: `" + brand.Slug + " setup extension` writes it to " + folder + " and lets your browsers reach " + brand.Name + "; then load that folder in your browser once."
	case len(s.Seen) == 0:
		return false, "Set up for " + andList(s.Browsers) + ", and not loaded in a browser yet: in Chrome, open chrome://extensions, turn on Developer mode, choose Load unpacked and pick " + folder + " (⌘⇧G takes the path)."
	}
	detail := "Ready in " + andList(s.Browsers) + "; last used in " + extension.Seen(s, time.Now()) + "."
	if !s.Current {
		detail += " Its folder holds an older build, which the extension takes when it next connects."
	}
	return true, strings.TrimSpace(detail)
}

// andList joins names in a sentence: a, b and c.
func andList(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}
