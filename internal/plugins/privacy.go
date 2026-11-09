package plugins

import (
	"errors"
	"path/filepath"

	"github.com/irie-people/irie/internal/anon"
	"github.com/irie-people/irie/internal/browser"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/dms"
	"github.com/irie-people/irie/internal/vpn"
)

// The privacy plugin's parts, by name.
const (
	VPN     = "vpn"
	Anon    = "anon"
	Browser = "browser"
	Vault   = "vault"
	Switch  = "switch"
)

// switchSkill is the dead man's switch guide's skill.
const switchSkill = "dead-mans-switch"

// mullvadApp is where the Mullvad VPN app, which brings the mullvad
// command, comes from.
const mullvadApp = "https://mullvad.net/download"

// privacy is the privacy plugin: what keeps you and your agents private
// online, and your family looked after if something happens to you.
func privacy() Plugin {
	return Plugin{
		Name:    "privacy",
		Title:   "Privacy",
		Summary: "Mullvad VPN, anonymous research over Mullvad and Tor, Mullvad Browser for agents and a dead man's switch for your family, with Bitwarden for agents to come.",
		Setup:   ":setup privacy",
		key:     "plugins.privacy",
		on:      func(c config.Config) bool { return c.Plugins.Privacy },
		Parts: []Part{
			{
				Name:    VPN,
				Title:   "Mullvad VPN",
				Summary: "See the VPN in every client, and connect, disconnect or change its place from a task bar or the menu.",
				Switch:  "vpn.on", On: true, Off: false,
				Brings:  []string{":vpn", "File › VPN", "the header's mark"},
				Refused: "Mullvad VPN control is off: `irie plugins on privacy vpn`, or Settings › Plugins in the Mac app, turns it on.",
				own:     func(c config.Config) bool { return c.VPN.On },
				check:   checkVPN,
			},
			{
				Name:    Anon,
				Title:   "Anonymous research",
				Summary: "Tasks typed with +anon or +tor research in compartments, each leaving through an exit of its own on Mullvad or Tor, with search through Irie's own SearXNG.",
				Switch:  "anon.on", On: true, Off: false,
				Setup:   ":setup anon",
				Brings:  []string{"+anon", "+tor", "irie anon", "irie-anon's search and fetch"},
				Refused: "Anonymous research is off: `irie setup anon` sets it up.",
				own:     func(c config.Config) bool { return c.Anon.On },
				check:   checkAnon,
			},
			{
				Name:    Browser,
				Title:   "Mullvad Browser for agents",
				Summary: "Agents browse in a real Mullvad Browser without a window, for pages that need JavaScript, clicks or a form.",
				Switch:  "browser.connector", On: "on", Off: "off",
				Setup:   ":setup browser",
				Brings:  []string{"irie-browser's tools", "irie browser"},
				Refused: "Irie's browser is off: `irie setup browser` sets it up.",
				own:     func(c config.Config) bool { return c.Browser.Connector == "on" },
				check:   checkBrowser,
			},
			{
				Name:    Vault,
				Title:   "Bitwarden for agents",
				Summary: "An agent uses a login from your Bitwarden vault without ever seeing it, once you allow each request with Touch ID.",
				// Built on 2026-11-01 (irie/irie-extensions) and held back
				// on 2026-11-09: a review found that an agent could allow
				// its own requests and read the master password while bw
				// runs. docs/proposals/extensions.md §3.8 lists what must
				// change before it comes in.
				Held: "A security review found ways an agent could get past your Touch ID and read secrets, so it stays out until they are fixed.",
			},
			{
				Name:    Switch,
				Title:   "Dead man's switch",
				Summary: "A guided session sets up what your family needs if you stop checking in: a letter, emergency access to your passwords and an alarm that reaches them.",
				Setup:   ":setup switch",
				Brings:  []string{":setup switch", "/irie:dead-mans-switch", "irie dms"},
				Refused: "The dead man's switch guide is left out: skills.disabled names dead-mans-switch.",
				Skills:  []string{switchSkill},
				own: func(c config.Config) bool {
					return c.Skills.Enabled && !has(c.Skills.Disabled, switchSkill)
				},
				check: checkSwitch,
			},
		},
	}
}

func checkVPN(e Env, on bool) (bool, string) {
	if _, err := vpn.Find(expand(e.Config.VPN.Mullvad)); err != nil {
		return false, "Mullvad VPN isn't installed: its app, from " + mullvadApp + ", brings the mullvad command Irie uses."
	}
	if !on {
		return true, "Mullvad VPN is installed. Turn this on to see it and change it in Irie."
	}
	if v := e.Live.VPN; v != nil {
		if v.Problem != "" {
			return false, v.Problem
		}
		if v.Text != "" {
			return true, v.Text
		}
	}
	return true, "Irie shows Mullvad VPN in every client and takes the :vpn words."
}

func checkAnon(e Env, on bool) (bool, string) {
	d := anon.Dirs{Root: e.Paths.Anon()}
	_, torErr := anon.FindTor(expand(e.Config.Anon.Tor))
	searx := anon.SearxInstalled(d)
	_, vpnErr := vpn.Find(expand(e.Config.VPN.Mullvad))
	if !on {
		return true, "Off. :setup anon installs Tor and SearXNG, and turns it on."
	}
	if vpnErr != nil {
		return false, "+anon tasks start only while Mullvad VPN is connected, and it isn't installed: get it from " + mullvadApp + "."
	}
	switch {
	case torErr != nil && !searx:
		return true, "+anon tasks leave through Mullvad. Tor and SearXNG aren't installed: :setup anon offers them, for +tor and search."
	case torErr != nil:
		return true, "+anon tasks leave through Mullvad and search through SearXNG. Tor isn't installed: :setup anon offers it, for +tor."
	case !searx:
		return true, "+anon tasks leave through Mullvad, +tor ones through Tor. SearXNG isn't installed: :setup anon offers it, for search."
	}
	return true, "+anon tasks leave through Mullvad and +tor ones through Tor, and both search through Irie's SearXNG."
}

func checkBrowser(e Env, on bool) (bool, string) {
	exe, err := browser.Find(expand(e.Config.Browser.Path))
	switch {
	case errors.Is(err, browser.ErrNotFound):
		return false, "Mullvad Browser isn't installed: :setup browser installs it."
	case err != nil:
		return false, "browser.path: " + err.Error()
	case !on:
		return true, "Mullvad Browser is installed. :setup browser checks it on a page and turns this on."
	case !browser.TemplateReady(e.Paths.BrowserTemplate(), exe):
		return true, "Claude Code sessions get Mullvad Browser, which starts slowly until :setup browser makes its clean profile."
	}
	return true, "Claude Code sessions get Mullvad Browser through irie-browser."
}

func checkSwitch(e Env, on bool) (bool, string) {
	st, made, err := dms.Store{Dir: filepath.Join(e.Paths.State, "dms")}.Load()
	switch {
	case !on:
		return true, "Left out: skills.disabled names dead-mans-switch."
	case err != nil:
		return false, "The switch's state can't be read: " + err.Error()
	case made && !st.Made.IsZero():
		return true, "Set up on " + st.Made.Local().Format("2 January 2006") + ". `irie dms status` says how its checks stand."
	case made:
		return true, "Set up. `irie dms status` says how its checks stand."
	}
	return true, "The guide is ready: :setup switch, or File › Set Up a Dead Man's Switch… in the Mac app."
}
