//go:build !nochat

package plugins

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// The chat plugin's parts, by name.
const (
	ChatDiscord = "discord"
	ChatSlack   = "slack"
	ChatAgents  = "agents"
)

// chatSkill is the skill that tells agents how to work with the chat.
const chatSkill = "chat"

// discordPortal is where a person makes the bot Irie reads Discord through.
const discordPortal = "https://discord.com/developers/applications"

func init() { built["chat"] = chatPlugin }

// chatPlugin is the chat plugin: team and community chat beside the
// sessions, through bots of the person's own, and agents' drafts the
// person sends (docs/proposals/chat.md).
func chatPlugin() Plugin {
	brings := []string{":chat", "Chat in the right sidebar", brand.Slug + " chat"}
	return Plugin{
		Name:    "chat",
		Title:   "Chat",
		Summary: "Your Discord servers and Slack workspaces beside your sessions, through bots of your own: read and post, and let agents write drafts that you send. Tokens stay in the Keychain, and what it reads stays in memory.",
		key:     "plugins.chat",
		on:      func(c config.Config) bool { return c.Plugins.Chat },
		Parts: []Part{
			{
				Name:    ChatDiscord,
				Title:   "Discord",
				Summary: "Your Discord servers, through a bot of yours: its channels, threads and forums, read and posted to as the bot.",
				Setup:   ":chat",
				Brings:  brings,
				own:     func(config.Config) bool { return true },
				check:   func(e Env, on bool) (bool, string) { return checkChat(e, config.ChatDiscord) },
				// The tab is the plugin's, and shows with its first part,
				// which is on while the plugin is.
				Pane: &ipc.PluginPane{ID: "chat.tab", Title: "Chat", Icon: "bubble.left.and.bubble.right", Kind: "chat"},
			},
			{
				Name:    ChatSlack,
				Title:   "Slack",
				Summary: "Your Slack workspace, through a Slack app of yours: its channels and threads, read and posted to as the app's bot.",
				Setup:   ":chat",
				Brings:  brings,
				own:     func(config.Config) bool { return true },
				check:   func(e Env, on bool) (bool, string) { return checkChat(e, config.ChatSlack) },
			},
			{
				Name:    ChatAgents,
				Title:   "Chat for agents",
				Summary: "Agents write drafts that wait for you to send; they read an account only where you let them, and post on their own only in a channel you open to them, logged, rate-limited and never pinging everyone.",
				Switch:  "chat.agents", On: true, Off: false,
				Brings:  []string{"/" + brand.Slug + ":" + chatSkill, brand.Slug + " chat for agents, as drafts"},
				Refused: "Chat is closed to agents: chat.agents is off. `" + brand.Slug + " plugins on chat agents` opens it.",
				Skills:  []string{chatSkill},
				own:     func(c config.Config) bool { return c.Chat.Agents },
				check:   checkChatAgents,
				skillsOff: func(c config.Config) string {
					switch {
					case !c.Chat.Agents:
						return "chat.agents is off"
					case len(c.Chat.Accounts) == 0:
						return "no chat account yet"
					}
					return ""
				},
			},
		},
	}
}

// checkChat says how a kind's accounts stand: from config.toml and the
// core's last look at each, which it keeps in memory. It reads no token and
// asks no service.
func checkChat(e Env, kind string) (bool, string) {
	var names []string
	for name, a := range e.Config.Chat.Accounts {
		if a.Kind == kind {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		if kind == config.ChatDiscord {
			return false, "No Discord account yet: make a bot at " + discordPortal + ", invite it to your server, then `" + brand.Slug + " chat add discord` takes its token."
		}
		return false, "No Slack account yet: `" + brand.Slug + " chat add slack` says how to make a Slack app of yours, then takes its tokens."
	}
	var bad, notes []string
	for _, a := range e.Live.Chat {
		if a.Kind != kind {
			continue
		}
		switch a.State {
		case ipc.ChatNeeds, ipc.ChatError:
			bad = append(bad, a.Name+": "+strings.TrimSuffix(a.Problem, "."))
		}
		if a.Hidden {
			notes = append(notes, a.Name+"'s bot gets no message text from people until its Message Content intent is on (Developer Portal › Bot)")
		}
	}
	if len(bad) > 0 {
		return false, strings.Join(bad, "; ") + "."
	}
	what := "account"
	if len(names) > 1 {
		what = "accounts"
	}
	text := fmt.Sprintf("Ready: %d %s (%s). :chat, or Chat in the right sidebar, reads them.", len(names), what, strings.Join(names, ", "))
	if len(notes) > 0 {
		text += " " + strings.Join(notes, "; ") + "."
	}
	return true, text
}

// checkChatAgents says what agents may do, from config.toml alone.
func checkChatAgents(e Env, on bool) (bool, string) {
	c := e.Config.Chat
	if !c.Agents {
		return true, "Off: agents can't use the chat at all, and drafts already waiting stay for you."
	}
	if len(c.Accounts) == 0 {
		return true, "Ready: once an account is added, agents write drafts that wait for you to send."
	}
	var reads, posts []string
	for _, name := range slices.Sorted(maps.Keys(c.Accounts)) {
		a := c.Accounts[name]
		if a.AgentsRead {
			reads = append(reads, name)
		}
		for id, ch := range a.Channels {
			if ch.Agents == config.ChatAgentsPost {
				label := ch.Name
				if label == "" {
					label = id
				}
				posts = append(posts, name+" "+label)
			}
		}
	}
	slices.Sort(posts)
	text := "Ready: agents write drafts that wait for you to send."
	if len(posts) > 0 {
		text += " They post on their own in " + strings.Join(posts, ", ") + "."
	}
	if len(reads) > 0 {
		text += " They may read " + strings.Join(reads, ", ") + "."
	}
	return true, text
}
