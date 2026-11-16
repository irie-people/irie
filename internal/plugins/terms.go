//go:build !noterms

package plugins

import (
	"net"
	"strconv"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/terms"
)

// Terms is the terms plugin's one part, by name.
const Terms = "terms"

func init() { built["terms"] = termsPlugin }

// termsPlugin is Terms in short (docs/proposals/browser-extension.md): what
// a site's terms of service mean for you, read by Irie's own code, in the
// Mac app's right sidebar, in a terminal and in the browser extension's
// panel.
func termsPlugin() Plugin {
	return Plugin{
		Name:    "terms",
		Title:   "Terms in short",
		Summary: "What a site's terms of service mean for you, in plain words, with answers to your questions about them: " + brand.Name + "'s own code reads them on this computer, with no tokens spent.",
		key:     "plugins.terms",
		on:      func(c config.Config) bool { return c.Plugins.Terms },
		Parts: []Part{
			{
				Name:    Terms,
				Title:   "Terms in short",
				Summary: "Give a site, and its terms of service or privacy policy read in plain words: the points that matter, each with the clause behind it, and answers to your questions. A local model words answers only when you ask.",
				Brings:  []string{"the Terms tab in the right sidebar", "the browser extension's Terms tool", brand.Slug + " terms ADDRESS", brand.Slug + " terms ask ADDRESS QUESTION"},
				Pane: &ipc.PluginPane{
					Title:   "Terms",
					Icon:    "doc.text.magnifyingglass",
					Kind:    ipc.PaneWeb,
					Address: terms.PageAddress,
					Origin:  "http://127.0.0.1:" + strconv.Itoa(terms.PagePort),
					Start:   "Restart " + brand.Name + ": its core serves this page while the plugin is on.",
				},
				own:   always,
				check: checkTerms,
			},
		},
	}
}

// termsServed says whether something takes a TCP connection at the Terms
// tab's address, on this computer, within a moment. Nothing is sent. A test
// may stand in for it.
var termsServed = func(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// checkTerms says whether the core serves the Terms tab's page: it knocks
// once on its address, on this computer, and sends nothing.
func checkTerms(e Env, on bool) (bool, string) {
	if !on {
		return true, "Off: the Terms tab, `" + brand.Slug + " terms` and the browser extension's Terms tool go with it."
	}
	if termsServed(net.JoinHostPort("127.0.0.1", strconv.Itoa(terms.PagePort))) {
		return true, "Ready: the Terms tab in the right sidebar, `" + brand.Slug + " terms example.com` in a terminal, and the browser extension's panel."
	}
	return false, "Nothing answers at 127.0.0.1:" + strconv.Itoa(terms.PagePort) + " yet, where " + brand.Name + "'s core serves the Terms tab while the plugin is on: another program may hold the port. `" + brand.Slug + " terms example.com` still works in a terminal."
}
