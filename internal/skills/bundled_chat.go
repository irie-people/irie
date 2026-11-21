//go:build !nochat

package skills

import "embed"

// chatSkills are the chat plugin's skills (DESIGN §4.17): how agents write
// the drafts their person sends.
//
//go:embed bundled/chat
var chatSkills embed.FS

func init() { plugged = append(plugged, chatSkills) }
