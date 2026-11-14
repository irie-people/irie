//go:build !novpn

package plugins

import (
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/vpn"
)

// VPN is the VPN plugin's one part, by name.
const VPN = "vpn"

func init() { built["vpn"] = vpnPlugin }

// vpnPlugin is the VPN plugin (DESIGN §4.18): the VPN that vpn.provider
// names, in every client, with the :vpn words to change it. A company's
// copy offers only the VPNs its brand names, and keeps this plugin when it
// leaves the privacy plugin out.
func vpnPlugin() Plugin {
	return Plugin{
		Name:    "vpn",
		Title:   "VPN",
		Summary: vpnSummary(),
		// Its one part's Set Up… is the plugin's: no Set Up Each Part….
		key: "plugins.vpn",
		on:  func(c config.Config) bool { return c.Plugins.VPN },
		Parts: []Part{
			{
				Name:    VPN,
				Title:   "VPN",
				Summary: "See the VPN in every client, and connect or disconnect it from a task bar or the menu, or move it where the VPN has places.",
				Switch:  "vpn.on", On: true, Off: false,
				Setup:   ":setup vpn",
				Brings:  []string{":vpn", "Plugins › VPN", "the header's mark"},
				Refused: "VPN control is off: `" + brand.Slug + " setup vpn` sets it up, and `" + brand.Slug + " plugins on vpn` or Settings › Plugins in the Mac app turns it on.",
				own:     func(c config.Config) bool { return c.VPN.On },
				titled:  vpnTitle,
				check:   checkVPN,
			},
		},
	}
}

// vpnSummary says what the plugin is for, naming the VPNs this copy
// offers.
func vpnSummary() string {
	offered := config.VPNOffered()
	if len(offered) == 1 {
		if k, ok := vpn.KindOf(offered[0]); ok {
			return k.Title + " in every client, connected or disconnected from any task bar or the menu."
		}
	}
	var titles []string
	for _, name := range offered {
		if k, ok := vpn.KindOf(name); ok {
			titles = append(titles, kindWords(k))
		}
	}
	return "Your VPN in every client, connected, disconnected or moved from any task bar or the menu: " + joinAnd(titles) + "."
}

// kindWords is a kind's name in a list of VPNs.
func kindWords(k vpn.Kind) string {
	switch k.Name {
	case "system":
		return "the system's own VPNs"
	case "command":
		return "any other through commands of your own"
	}
	return k.Title
}

// joinAnd joins words as a sentence lists them: a, b and c.
func joinAnd(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// vpnTitle is the part's title: the VPN that vpn.provider names.
func vpnTitle(c config.Config) string {
	if k, ok := vpn.KindOf(c.VPN.Provider); ok {
		return k.Title
	}
	return "VPN"
}

// VPNSettings are the VPN's settings in cfg, with ~ in their paths made
// the home folder's: Mullvad's command is vpn.mullvad, which anonymous
// research shares, and every other VPN's vpn.cli.
func VPNSettings(cfg config.Config) vpn.Settings {
	v := cfg.VPN
	s := vpn.Settings{
		Provider:   v.Provider,
		CLI:        expand(v.CLI),
		Server:     v.Server,
		Status:     expandArgv(v.Status),
		Connect:    expandArgv(v.Connect),
		Disconnect: expandArgv(v.Disconnect),
		Connected:  v.Connected,
		Connecting: v.Connecting,
	}
	if v.Provider == "mullvad" {
		s.CLI = expand(v.Mullvad)
	}
	return s
}

// expandArgv is a command with ~ in its program made the home folder's.
func expandArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := append([]string{expand(argv[0])}, argv[1:]...)
	return out
}

// FindVPN finds the VPN that cfg names, and says what it lacks, and what
// fixes that, in a sentence; "" when its command is here.
func FindVPN(cfg config.Config) (vpn.Kind, string) {
	s := VPNSettings(cfg)
	k, ok := vpn.KindOf(s.Provider)
	switch {
	case !ok:
		return k, "vpn.provider = " + s.Provider + " is no VPN " + brand.Name + " knows: it knows " + strings.Join(vpn.Names(), ", ") + "."
	case !k.Runs():
		return k, k.NotHere() + "."
	case k.Needs && s.Server == "":
		return k, k.Title + " needs vpn.server, " + k.Server + ": `" + brand.Slug + " setup vpn` asks for it."
	}
	if _, err := k.Find(s); err != nil {
		return k, strings.TrimSuffix(err.Error(), ".") + "."
	}
	return k, ""
}

func checkVPN(e Env, on bool) (bool, string) {
	k, why := FindVPN(e.Config)
	if why != "" {
		return false, why
	}
	if !on {
		return true, k.Title + " is installed. Turn this on to see it and change it in " + brand.Name + "."
	}
	if v := e.Live.VPN; v != nil {
		if v.Problem != "" {
			return false, v.Problem
		}
		if v.Text != "" {
			return true, v.Text
		}
	}
	return true, brand.Name + " shows " + k.Title + " in every client and takes the :vpn words."
}
