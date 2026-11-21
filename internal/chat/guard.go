//go:build !nochat

package chat

import (
	"regexp"
	"strings"

	"github.com/irie-people/irie/internal/config"
)

// What agents may never post: words that ping everyone in a channel, or a
// whole role or group of people. The core refuses an agent's post, and its
// draft, that holds one, whatever the channel lets agents do, and sends an
// agent's own post with no pings at all besides (Pings).

// discordEveryone are Discord's marks that ping many people: everyone,
// here, and a role.
var discordEveryone = regexp.MustCompile(`(?i)@(everyone|here)\b|<@&\d+>`)

// slackEveryone are Slack's: everyone, the channel, here, and a user group;
// the words alone too, which Slack pings only with link_names, which Irie
// never sends.
var slackEveryone = regexp.MustCompile(`(?i)<!(everyone|channel|here)(\|[^>]*)?>|<!subteam\^[^>]*>|@(everyone|channel|here)\b`)

// Everyone is the first word in text that would ping everyone, or a whole
// role or group, in kind's service, or "" when there is none.
func Everyone(kind, text string) string {
	re := discordEveryone
	if kind == config.ChatSlack {
		re = slackEveryone
	}
	return strings.TrimSpace(re.FindString(text))
}
