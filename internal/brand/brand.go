// Package brand is the product's name, and the names made from it, in one
// place, so that a copy of Irie under another name changes them here and
// nowhere else (DESIGN §2.10).
//
// Name is what a person reads: windows, menus, messages, the help. Slug is
// what a computer looks up: the command, the folders in each XDG directory,
// the runtime folder and its socket, the tmux server and its sessions, the
// default branch prefix, the skills' namespace and, in capitals, the
// environment variables (Env). The Mac app takes the same values: mac/
// build.sh writes them into its Info.plist, and tools/brand.sh --swift
// writes mac/Sources/Irie/Brand.swift from this file.
//
// They are constants, so a message made from them is still a constant, and
// go vet still checks its format. To rename a copy, change them below, then
// run
//
//	tools/brand.sh --swift >mac/Sources/Irie/Brand.swift
//
// and build as usual. The package's test fails while Brand.swift is behind.
package brand

import "strings"

const (
	// Name is the product's name as a person reads it: at most MaxName
	// letters, digits, spaces, hyphens and full stops, starting with a
	// letter or digit, with one space at most between words. Every place
	// that shows it fits a name that long.
	Name = "Irie"
	// Slug is its name for computers: lower-case letters, digits and
	// hyphens, starting with a letter.
	Slug = "irie"
	// EnvPrefix starts the name of each environment variable it sets or
	// reads: Slug in capitals, with underscores for hyphens.
	EnvPrefix = "IRIE"
	// BundleID is the Mac app's bundle identifier. The core uses it to find
	// the app, and the lid's launchd job is named after it.
	BundleID = "io.github.irie-hq.irie"
	// Site is the guide's address, ending in a slash.
	Site = "https://irie-people.github.io/irie/"
	// Repo is the source's address, for credits and links.
	Repo = "https://github.com/irie-people/irie"
	// Tagline is the line under the name on the Mac app's welcome.
	Tagline = "Everything irie."
	// Bar is what the strip under the terminal shows unless ui.bar says
	// otherwise: "cheer", Irie's phrases of encouragement, or "tips",
	// tips for those new to it.
	Bar = "cheer"
	// Tags are the go build tags the scripts build it with (tools/live.sh,
	// tools/release.sh), comma-separated: novpn, noprivacy, nowriting,
	// nomarkets, nomail, nobitwarden, noterms, noextension, noyoink, nochat,
	// noobsidian, nofounding and nojammin leave those plugins' code out
	// (DESIGN §4.17), as a company's copy may want.
	// A build by hand passes them itself: go build -tags "$(tools/brand.sh
	// --tags)" ./cmd/irie.
	Tags = ""
	// VPNs are the VPNs it drives (DESIGN §4.18), comma-separated, the
	// first the default for vpn.provider: mullvad, tailscale, netbird,
	// warp, nordvpn, cisco, globalprotect, forticlient, system and
	// command. "" offers them
	// all, Mullvad first. A company's copy names the VPN it approves, such
	// as "cisco", so its people see and drive that VPN and no other; a
	// copy that names any shows its VPN from the start (vpn.on).
	VPNs = ""
	// VPNServer is vpn.server's default: the host, portal or VPN by name
	// that a company's VPN connects to, so its people set nothing. "" =
	// none.
	VPNServer = ""
)

// MaxName is the most characters Name may have.
const MaxName = 30

// App is the Mac app's folder name, such as Irie.app.
const App = Name + ".app"

// Env is the environment variable called name, such as Env("SESSION") for
// IRIE_SESSION.
func Env(name string) string { return EnvPrefix + "_" + name }

// Exe is the Mac app's executable and so its process's name: Name's ASCII
// letters and digits, 15 at most, or Slug's when Name has none. macOS keeps
// only a process name's first bytes, which is all pgrep -x matches.
var Exe = exe(Name, Slug)

func exe(name, slug string) string {
	keep := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if b.Len() < 15 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	if e := keep(name); e != "" {
		return e
	}
	return keep(slug)
}

// Text is one of Irie's own texts, such as a skill, an arch agent's words or
// a page of the phone app, with the names in it made this product's: Irie;
// irie as a word, which is the command (`irie up`, irie ctl dog, a line of a
// code block) or a folder (~/.config/irie/, the irie/ branch prefix); IRIE_
// variables; and the guide's address. irie joined to more by a full stop, a
// hyphen or an underscore stays, as in an id (irie.park), a key
// (irie-phone) or a tmux option (@irie_role), and so does a link other than
// the guide's, such as the source's on github.com. The texts keep Irie's
// names in the source, so they read as they are.
func Text(s string) string { return text(s, Name, Slug, EnvPrefix, Site) }

// guide is the guide's address as Irie's own texts have it.
const guide = "https://irie-people.github.io/irie/"

// text is Text for a product of these names, in one pass, so that no name
// it writes is read again.
func text(s, name, slug, env, site string) string {
	if name == "Irie" && slug == "irie" && env == "IRIE" && site == guide {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, guide):
			b.WriteString(site)
			i += len(guide)
		case strings.HasPrefix(rest, "Irie") && !strings.HasSuffix(s[:i], "Sources/"):
			// The Mac app's source folder, mac/Sources/Irie, keeps its name
			// in every copy.
			b.WriteString(name)
			i += len("Irie")
		case strings.HasPrefix(rest, "IRIE_"):
			b.WriteString(env + "_")
			i += len("IRIE_")
		case strings.HasPrefix(rest, "irie") && commandWord(s, i):
			b.WriteString(slug)
			i += len("irie")
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// commandWord says whether the irie at s[i:] is the command or a folder: a
// word of its own, not joined to more by a letter, digit, full stop, hyphen,
// underscore or @, and not in a link's address or the command's source
// folder (./cmd/irie), which keeps its name in every copy.
func commandWord(s string, i int) bool {
	part := func(c byte) bool {
		return c == '_' || c == '.' || c == '-' || c == '@' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	if i > 0 && part(s[i-1]) || i+4 < len(s) && part(s[i+4]) || strings.HasSuffix(s[:i], "cmd/") {
		return false
	}
	// The address it sits in, if any: back to the last space, quote,
	// backquote or bracket. A link has a scheme (https://) or starts with a
	// host (github.com/…); a folder does neither (~/.config/…,
	// $XDG_STATE_HOME/…).
	start := strings.LastIndexAny(s[:i], " \t\r\n'\"`()<>[]{}") + 1
	addr := s[start:i]
	if strings.Contains(addr, "://") {
		return false
	}
	host, _, found := strings.Cut(addr, "/")
	if found {
		if dot := strings.Index(host, "."); dot > 0 && dot < len(host)-1 && part(host[dot-1]) && part(host[dot+1]) {
			return false
		}
	}
	return true
}
