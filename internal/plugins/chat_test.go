//go:build !nochat

package plugins

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

func chatStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "chat" {
			return p
		}
	}
	t.Fatal("no chat plugin")
	return ipc.Plugin{}
}

// TestChatParts: the chat plugin is on and shows its tab; each kind of
// account needs one until it is added, then stands by the core's last look;
// and agents' drafts are on with their own switch.
func TestChatParts(t *testing.T) {
	e := env(t)
	p := chatStatus(t, e)
	if !p.On || p.Title != "Chat" {
		t.Fatalf("chat = %+v", p)
	}
	d := part(t, p, ChatDiscord)
	if d.State != ipc.PartNeeds || !strings.Contains(d.Detail, "chat add discord") || d.Pane == nil || d.Pane.ID != "chat.tab" || d.Pane.Kind != "chat" {
		t.Errorf("discord = %+v", d)
	}
	if s := part(t, p, ChatSlack); s.State != ipc.PartNeeds || !strings.Contains(s.Detail, "chat add slack") {
		t.Errorf("slack = %+v", s)
	}
	a := part(t, p, ChatAgents)
	if a.State != ipc.PartReady || a.Switch != "chat.agents" || !a.On {
		t.Errorf("agents = %+v", a)
	}
	e.Config.Chat.Accounts = map[string]config.ChatAccount{
		"acme": {Kind: config.ChatDiscord, AgentsRead: true, Channels: map[string]config.ChatChannel{"1": {Agents: config.ChatAgentsPost, Name: "#bots"}}},
	}
	e.Live.Chat = []ipc.ChatAccount{{Name: "acme", Kind: config.ChatDiscord, State: ipc.ChatOK, Hidden: true}}
	p = chatStatus(t, e)
	if d := part(t, p, ChatDiscord); d.State != ipc.PartReady || !strings.Contains(d.Detail, "1 account (acme)") || !strings.Contains(d.Detail, "Message Content") {
		t.Errorf("discord with an account = %+v", d)
	}
	if a := part(t, p, ChatAgents); !strings.Contains(a.Detail, "post on their own in acme #bots") || !strings.Contains(a.Detail, "read acme") {
		t.Errorf("agents = %+v", a)
	}
	e.Live.Chat[0].State, e.Live.Chat[0].Problem = ipc.ChatError, "Discord doesn't know this bot's token."
	if d := part(t, chatStatus(t, e), ChatDiscord); d.State != ipc.PartNeeds || !strings.Contains(d.Detail, "acme: Discord doesn't know") {
		t.Errorf("discord with a stale token = %+v", d)
	}
}

// TestChatSkill: the skill goes to sessions only while the plugin is on,
// chat.agents is, and an account is there to write for.
func TestChatSkill(t *testing.T) {
	cfg := config.Default()
	if why := SkillsLeft(cfg)[chatSkill]; why != "no chat account yet" {
		t.Errorf("no account: %q", why)
	}
	cfg.Chat.Accounts = map[string]config.ChatAccount{"acme": {Kind: config.ChatDiscord}}
	if why, left := SkillsLeft(cfg)[chatSkill]; left {
		t.Errorf("with an account: %q", why)
	}
	cfg.Chat.Agents = false
	if why := SkillsLeft(cfg)[chatSkill]; why != "chat.agents is off" {
		t.Errorf("agents off: %q", why)
	}
	cfg.Plugins.Chat = false
	if why := SkillsLeft(cfg)[chatSkill]; !strings.Contains(why, "the chat plugin is off") {
		t.Errorf("plugin off: %q", why)
	}
	// The switch is the part's, and turns it off alone.
	edits, err := Edits("chat", ChatAgents, false)
	if err != nil || len(edits) != 1 || edits[0].Key != "chat.agents" || edits[0].Value != false {
		t.Errorf("edits = %+v, %v", edits, err)
	}
}
