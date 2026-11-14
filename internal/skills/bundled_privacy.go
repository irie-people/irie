//go:build !noprivacy

package skills

import "embed"

// privacySkills are the privacy plugin's skills (DESIGN §4.17): the dead
// man's switch guide.
//
//go:embed bundled/dead-mans-switch
var privacySkills embed.FS

func init() { plugged = append(plugged, privacySkills) }
