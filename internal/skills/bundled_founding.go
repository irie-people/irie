//go:build !nofounding

package skills

import "embed"

// foundingSkills are the founding plugin's skills (DESIGN §4.17): the
// founding documents, whose files are also what the Founding tab shows.
//
//go:embed bundled/founding-documents
var foundingSkills embed.FS

func init() { plugged = append(plugged, foundingSkills) }
