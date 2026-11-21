package config

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
)

// The chat plugin's settings (docs/proposals/chat.md) live here, so the
// config needs none of the plugin's packages: a build without the plugin
// (go build -tags nochat) leaves them out of the binary, and still reads a
// config.toml that sets them.

// Chat is the chat plugin's accounts and what agents may do with them:
// Discord and Slack, each through a bot of the person's own. Each bot's
// token lives in the macOS Keychain, never in this file.
type Chat struct {
	// Accounts are the chat accounts, by the short name each shows, such
	// as acme.
	Accounts map[string]ChatAccount `toml:"accounts"`
	// Agents says whether agents may use the chat at all: write drafts,
	// and do what each account and channel opens to them. Off, the core
	// refuses every call from an agent.
	Agents bool `toml:"agents"`
	// AgentPosts is how many posts agents may make on their own in one
	// channel in an hour, where a channel lets them: at most one a minute,
	// whatever it says.
	AgentPosts int `toml:"agent_posts"`
}

// ChatAccount is one chat account: a bot of the person's own in one
// service.
type ChatAccount struct {
	Kind string `toml:"kind"` // discord or slack
	// Agents is what agents may do in its channels unless a channel's own
	// table says otherwise: none, or draft (the default). Posting on their
	// own is opened one channel at a time, never a whole account.
	Agents string `toml:"agents,omitempty"`
	// AgentsRead says whether agents may read its messages: off until the
	// person turns it on, since every message is words from people they
	// may not know.
	AgentsRead bool `toml:"agents_read,omitempty"`
	// User is the person's own user ID in the service, so a message that
	// mentions them counts as a mention too; "" = only the bot's.
	User string `toml:"user,omitempty"`
	// Colour is which of Irie's six colours marks it, 1 to 6; 0 = Irie
	// picks, so accounts side by side differ.
	Colour int `toml:"colour,omitempty"`
	// Channels say what agents may do in each channel that differs from
	// the account, by the channel's ID.
	Channels map[string]ChatChannel `toml:"channels,omitempty"`
}

// ChatChannel is what agents may do in one channel.
type ChatChannel struct {
	Agents string `toml:"agents"` // none, draft or post
	// Name is the channel's name when the person set it, for them to read
	// the file by; Irie never goes by it.
	Name string `toml:"name,omitempty"`
}

// The kinds of chat account.
const (
	ChatDiscord = "discord"
	ChatSlack   = "slack"
)

// ChatKinds are the kinds, in the order Irie lists accounts by.
var ChatKinds = []string{ChatDiscord, ChatSlack}

// What agents may do in a channel.
const (
	ChatAgentsNone  = "none"  // nothing: no drafts either
	ChatAgentsDraft = "draft" // write drafts the person sends or discards
	ChatAgentsPost  = "post"  // post on their own, logged and rate-limited
)

// ChatColours is how many colours an account can take.
const ChatColours = 6

// ChatAgentPosts is the most posts agents may make in a channel in an
// hour, where the person lets them: far inside Discord's and Slack's own
// limits, which allow about one a second.
const ChatAgentPosts = 60

// ChatName is the form of an account's name: short, lower case, as it shows
// on every message.
var ChatName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)

// ChatID is the form of a channel's or a user's ID in a service: Discord's
// numbers, Slack's C0123ABCD.
var ChatID = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

// AgentsIn is what agents may do in the channel whose ID is channel: its
// own table's word, else the account's, else draft.
func (a ChatAccount) AgentsIn(channel string) string {
	if c, ok := a.Channels[channel]; ok && c.Agents != "" {
		return c.Agents
	}
	if a.Agents != "" {
		return a.Agents
	}
	return ChatAgentsDraft
}

func defaultChat() Chat {
	return Chat{Accounts: map[string]ChatAccount{}, Agents: true, AgentPosts: 10}
}

// checkChat adds what is wrong with the chat settings.
func checkChat(c Chat, bad func(string, ...any)) {
	if c.AgentPosts < 1 || c.AgentPosts > ChatAgentPosts {
		bad("chat.agent_posts = %d: must be from 1 to %d", c.AgentPosts, ChatAgentPosts)
	}
	for _, name := range sortedKeys(c.Accounts) {
		a := c.Accounts[name]
		key := "chat.accounts." + name
		if !ChatName.MatchString(name) {
			bad("%s: an account's name is lower case letters, digits and -, at most 24, such as acme", key)
		}
		if !slices.Contains(ChatKinds, a.Kind) {
			bad("%s.kind = %q: must be %s", key, a.Kind, strings.Join(ChatKinds, " or "))
		}
		switch a.Agents {
		case "", ChatAgentsNone, ChatAgentsDraft:
		case ChatAgentsPost:
			bad("%s.agents = \"post\": agents post on their own one channel at a time, in [%s.channels.ID]; an account takes none or draft", key, key)
		default:
			bad("%s.agents = %q: must be none or draft", key, a.Agents)
		}
		if a.User != "" && !ChatID.MatchString(a.User) {
			bad("%s.user = %q: must be your user ID there, letters and digits", key, a.User)
		}
		if a.Colour < 0 || a.Colour > ChatColours {
			bad("%s.colour = %d: must be from 1 to %d, or 0 for "+brand.Name+" to pick", key, a.Colour, ChatColours)
		}
		for _, id := range sortedKeys(a.Channels) {
			ch := a.Channels[id]
			ckey := key + ".channels." + id
			if !ChatID.MatchString(id) {
				bad("%s: a channel goes by its ID, letters and digits", ckey)
			}
			switch ch.Agents {
			case ChatAgentsNone, ChatAgentsDraft, ChatAgentsPost:
			default:
				bad("%s.agents = %q: must be none, draft or post", ckey, ch.Agents)
			}
			if utf8.RuneCountInString(ch.Name) > 100 || strings.ContainsAny(ch.Name, "\r\n\x00") {
				bad("%s.name: one line, at most 100 characters", ckey)
			}
		}
	}
}
