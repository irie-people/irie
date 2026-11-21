package config

import (
	"strings"
	"testing"
)

// TestChatSettings: the chat plugin's settings, checked: an account's
// kind, what agents may do in it and in each channel, never post for a
// whole account, and the agents' hourly posts.
func TestChatSettings(t *testing.T) {
	c := Default()
	if !c.Chat.Agents || c.Chat.AgentPosts != 10 || !c.Plugins.Chat || c.ChatOn() != ChatBuilt {
		t.Errorf("defaults: %+v, plugin %v", c.Chat, c.Plugins.Chat)
	}
	cfg, err := Parse([]byte(`
[chat]
agent_posts = 5

[chat.accounts.acme]
kind = "discord"
agents_read = true
user = "123456789012345678"

[chat.accounts.acme.channels.223456789012345678]
agents = "post"
name = "#announcements"

[chat.accounts.team]
kind = "slack"
agents = "none"
`), "config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	a := cfg.Chat.Accounts["acme"]
	if a.AgentsIn("223456789012345678") != ChatAgentsPost || a.AgentsIn("1") != ChatAgentsDraft || !a.AgentsRead {
		t.Errorf("acme: %+v", a)
	}
	if cfg.Chat.Accounts["team"].AgentsIn("C1") != ChatAgentsNone || cfg.Chat.AgentPosts != 5 {
		t.Errorf("team: %+v", cfg.Chat)
	}
	for toml, says := range map[string]string{
		"[chat.accounts.acme]\nkind = \"irc\"\n":                                                        "must be discord or slack",
		"[chat.accounts.acme]\nkind = \"discord\"\nagents = \"post\"\n":                                 "one channel at a time",
		"[chat.accounts.Acme]\nkind = \"discord\"\n":                                                    "lower case",
		"[chat.accounts.acme]\nkind = \"discord\"\n[chat.accounts.acme.channels.1]\nagents = \"all\"\n": "none, draft or post",
		"[chat.accounts.acme]\nkind = \"discord\"\nuser = \"a b\"\n":                                    "your user ID",
		"[chat]\nagent_posts = 61\n":                                                                    "from 1 to 60",
	} {
		cfg, err := Parse([]byte(toml), "config.toml")
		if err == nil {
			err = cfg.Validate()
		}
		if err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%q: %v, want %q", toml, err, says)
		}
	}
}
