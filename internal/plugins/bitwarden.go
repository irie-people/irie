//go:build !nobitwarden

package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
)

// The Bitwarden plugin's parts, by name.
const (
	Autofill = "autofill"
	Vault    = "vault"
)

// bwPlaces are where the Mac app looks for the Bitwarden CLI, so the check
// looks there too: Homebrew's two prefixes. Never a path from config.toml,
// which an agent could point at a program of its own that records the
// master password. Tests point them elsewhere.
var bwPlaces = []string{"/opt/homebrew/bin/bw", "/usr/local/bin/bw"}

// bwData is the Bitwarden CLI's data file, which holds its sign-in:
// BITWARDENCLI_APPDATA_DIR's, else its folder in Application Support on a
// Mac and in ~/.config elsewhere. Tests point it elsewhere.
var bwData = func() string {
	if dir := os.Getenv("BITWARDENCLI_APPDATA_DIR"); dir != "" {
		return filepath.Join(dir, "data.json")
	}
	if runtime.GOOS == "darwin" {
		return expand("~/Library/Application Support/Bitwarden CLI/data.json")
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "Bitwarden CLI", "data.json")
	}
	return expand("~/.config/Bitwarden CLI/data.json")
}

func init() { built["bitwarden"] = bitwardenPlugin }

// bitwardenPlugin is the Bitwarden plugin: the person's own logins, filled
// into the sign-in pages of their panes. The Mac app does the filling, and
// the core only says how it stands: a login never crosses the core's
// socket, which agents can reach.
func bitwardenPlugin() Plugin {
	return Plugin{
		Name:    "bitwarden",
		Title:   "Bitwarden",
		Summary: "Your Bitwarden logins fill the sign-in pages of your panes in the right sidebar. The Mac app unlocks Bitwarden with your master password and keeps only those logins, in its own memory, until your Mac locks or sleeps. No agent can ask for one.",
		key:     "plugins.bitwarden",
		on:      func(c config.Config) bool { return c.Plugins.Bitwarden },
		Parts: []Part{
			{
				Name:    Autofill,
				Title:   "Fill sign-ins in panes",
				Summary: "A pane's sign-in page fills itself with the login your vault has for its address, and the key in the pane's header fills another or locks Bitwarden again.",
				Brings:  []string{"the key in each web pane's header", "Plugins › Bitwarden: Unlock Bitwarden…"},
				own:     func(config.Config) bool { return true },
				check:   checkBitwarden,
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
		},
	}
}

// checkBitwarden says whether the Mac app has a bw to unlock: whether the
// program is in one of its places, and whether it is signed in, from the
// one entry of its data file that says which account is. It reads nothing
// of the vault and runs nothing.
func checkBitwarden(Env, bool) (bool, string) {
	found := false
	for _, p := range bwPlaces {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			found = true
			break
		}
	}
	if !found {
		return false, "The Bitwarden CLI isn't installed: `brew install bitwarden-cli`, then `bw login` once in your own terminal."
	}
	if !bwSignedIn(bwData()) {
		return false, "The Bitwarden CLI isn't signed in: run `bw login` once in your own terminal, where your email, master password and two-step code are yours to type."
	}
	return true, "Ready: unlock Bitwarden with a pane's key, or in the Plugins menu, and each pane's sign-in fills itself. " + brand.Name + " locks it again when your Mac locks or sleeps."
}

// bwSignedIn reads whether the Bitwarden CLI's data file names an active
// account. A file it can't read counts as signed in, since the data file's
// shape is bw's own to change: unlocking then says what is wrong.
func bwSignedIn(path string) bool {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false
	}
	var data map[string]json.RawMessage
	if err != nil || json.Unmarshal(b, &data) != nil {
		return true
	}
	raw, set := data["global_account_activeAccountId"]
	if !set {
		return true
	}
	// null once bw logout has run.
	var id string
	return json.Unmarshal(raw, &id) == nil && id != ""
}
