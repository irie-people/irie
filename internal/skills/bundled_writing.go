//go:build !nowriting

package skills

import "embed"

// writingSkills are the writing plugin's skills (DESIGN §4.17): writing in
// Variorum.
//
//go:embed bundled/variorum
var writingSkills embed.FS

func init() { plugged = append(plugged, writingSkills) }
