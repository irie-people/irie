//go:build !nofounding

package skills

import "embed"

// foundingSkills are the founding plugin's skills (DESIGN §4.17): the
// charters, whose files are also what the Charters tab shows, and Common
// Schools', whose files the Common Schools page shows.
//
//go:embed bundled/founding-documents bundled/common-schools
var foundingSkills embed.FS

func init() { plugged = append(plugged, foundingSkills) }
