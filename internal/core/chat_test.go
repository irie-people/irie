//go:build !nochat

package core

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

// discordStandIn answers as Discord's API does for one bot in one server
// with one channel, and keeps what was posted.
type discordStandIn struct {
	srv   *httptest.Server
	mu    sync.Mutex
	posts []map[string]any
}

func newDiscordStandIn(t *testing.T) *discordStandIn {
	d := &discordStandIn{}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bot good.token" {
			w.WriteHeader(401)
			io.WriteString(w, `{"code":0,"message":"401: Unauthorized"}`)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /users/@me":
			io.WriteString(w, `{"id":"900","username":"acme-bot","global_name":"Acme Bot","bot":true}`)
		case "GET /applications/@me":
			io.WriteString(w, `{"id":"800","flags":524288}`)
		case "GET /users/@me/guilds":
			io.WriteString(w, `[{"id":"100","name":"Acme"}]`)
		case "GET /guilds/100":
			io.WriteString(w, `{"id":"100","name":"Acme","owner_id":"1","roles":[{"id":"100","name":"@everyone","permissions":"274877975552"}]}`)
		case "GET /guilds/100/members/900":
			io.WriteString(w, `{"roles":[]}`)
		case "GET /guilds/100/channels":
			io.WriteString(w, `[{"id":"201","type":0,"guild_id":"100","name":"general","position":0}]`)
		case "GET /guilds/100/threads/active":
			io.WriteString(w, `{"threads":[]}`)
		case "POST /channels/201/messages":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			d.mu.Lock()
			d.posts = append(d.posts, body)
			d.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"id": "501", "channel_id": "201", "type": 0, "content": body["content"], "timestamp": "2026-11-21T11:00:00Z", "author": map[string]any{"id": "900", "username": "acme-bot", "bot": true}})
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"code":0,"message":"404: Not Found"}`)
		}
	}))
	t.Cleanup(d.srv.Close)
	return d
}

// chatCore is a core whose chat asks the stand-in, with an agent's pane
// whose host is this test's own parent, so a call with this test's pid is
// that agent's.
func chatCore(t *testing.T, cfg config.Config) (*Core, *discordStandIn) {
	t.Helper()
	t.Setenv(brand.Env("KEYCHAIN"), "off")
	c := bareCore(t, cfg)
	d := newDiscordStandIn(t)
	c.chatService().Bases = map[string]string{config.ChatDiscord: d.srv.URL}
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid(), IrieSession: "s0000000000a1"}}}
	return c, d
}

// TestTheChatOp: an account comes in only once its token signs in, its
// token in the store and the rest in config.toml; :chat says how it
// stands; an agent's post is a draft that only the person sends; the
// account goes out whole (docs/proposals/chat.md).
func TestTheChatOp(t *testing.T) {
	c, d := chatCore(t, config.Default())
	r, err := c.chatOp(0, ipc.ChatArgs{})
	if err != nil || len(r.Accounts) != 0 || !strings.Contains(r.Text, "No chat account yet") {
		t.Fatalf("no accounts: %+v, %v", r, err)
	}
	if _, err := c.chatOp(0, ipc.ChatArgs{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "discord", Token: "stale"}}); err == nil || !strings.Contains(err.Error(), "Reset Token") {
		t.Fatalf("a stale token: %v", err)
	}
	if len(c.cfg().Chat.Accounts) != 0 {
		t.Fatal("an account whose token failed was kept")
	}
	r, err = c.chatOp(0, ipc.ChatArgs{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "discord", Name: "acme", Token: "good.token"}})
	if err != nil || r.Text != "Added acme (Discord, as Acme Bot)." {
		t.Fatalf("add: %+v, %v", r, err)
	}
	b, _ := os.ReadFile(c.paths.ConfigFile())
	if !strings.Contains(string(b), "[chat.accounts.acme]") || strings.Contains(string(b), "good.token") {
		t.Errorf("config.toml:\n%s", b)
	}
	if tok, err := os.ReadFile(filepath.Join(c.paths.State, "chat", "keys", "acme")); err != nil || strings.TrimSpace(string(tok)) != "good.token" {
		t.Errorf("kept token: %q, %v", tok, err)
	}
	in, err := c.input(ipc.InputArgs{Text: ":chat"}, 0)
	if err != nil || in.Did != ipc.InputAnswered || in.Op != ipc.OpChat || !strings.Contains(in.Text, "acme (Discord, as Acme Bot): signed in.") {
		t.Errorf(":chat = %+v, %v", in, err)
	}
	// The agent's post is a draft, from its session.
	r, err = c.chatOp(os.Getpid(), ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: "Hello from the agent"})
	if err != nil || r.Draft == nil || r.Draft.Session != "s0000000000a1" {
		t.Fatalf("an agent's post: %+v, %v", r, err)
	}
	if _, err := c.CallFrom(os.Getpid(), ipc.OpChat, []byte(`{"do":"send","id":"`+r.Draft.ID+`"}`)); err == nil {
		t.Error("an agent sent its own draft")
	}
	d.mu.Lock()
	n := len(d.posts)
	d.mu.Unlock()
	if n != 0 {
		t.Fatal("a draft went out before the person sent it")
	}
	if _, err := c.chatOp(0, ipc.ChatArgs{Do: ipc.ChatSend, ID: r.Draft.ID}); err != nil {
		t.Fatalf("the person's send: %v", err)
	}
	d.mu.Lock()
	if len(d.posts) != 1 || d.posts[0]["content"] != "Hello from the agent" {
		t.Errorf("posted: %+v", d.posts)
	}
	d.mu.Unlock()
	// The plugin's parts know the account.
	pr, _ := c.pluginsOp(ipc.PluginsArgs{})
	found := false
	for _, p := range pr.Plugins {
		for _, x := range p.Parts {
			if p.Name == "chat" && x.Name == "discord" {
				found = true
				if x.State != ipc.PartReady || !strings.Contains(x.Detail, "acme") || x.Pane == nil || x.Pane.Kind != "chat" {
					t.Errorf("discord part: %+v", x)
				}
			}
		}
	}
	if !found {
		t.Error("no chat plugin in the plugins op")
	}
	if _, err := c.chatOp(os.Getpid(), ipc.ChatArgs{Do: ipc.ChatRemove, Account: "acme"}); err == nil {
		t.Error("an agent removed an account")
	}
	r, err = c.chatOp(0, ipc.ChatArgs{Do: ipc.ChatRemove, Account: "acme"})
	if err != nil || len(r.Accounts) != 0 || len(c.cfg().Chat.Accounts) != 0 {
		t.Fatalf("remove: %+v, %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(c.paths.State, "chat", "keys", "acme")); !os.IsNotExist(err) {
		t.Errorf("the token stayed: %v", err)
	}
}

// TestChatClosedToAgents: chat.agents off refuses every call of an
// agent's, typed words too, and the person's still go; the plugin off
// refuses every call.
func TestChatClosedToAgents(t *testing.T) {
	cfg := config.Default()
	cfg.Chat.Agents = false
	c, _ := chatCore(t, cfg)
	if _, err := c.chatOp(os.Getpid(), ipc.ChatArgs{}); err == nil || !strings.Contains(err.Error(), "chat.agents is off") {
		t.Errorf("an agent's look: %v", err)
	}
	if _, err := c.input(ipc.InputArgs{Text: ":chat"}, os.Getpid()); err == nil {
		t.Error(":chat from an agent went through")
	}
	if _, err := c.chatOp(0, ipc.ChatArgs{}); err != nil {
		t.Errorf("the person's own call: %v", err)
	}
	cfg = config.Default()
	cfg.Plugins.Chat = false
	c, _ = chatCore(t, cfg)
	if _, err := c.chatOp(0, ipc.ChatArgs{}); err == nil || !strings.Contains(err.Error(), "plugins on chat") {
		t.Errorf("plugin off: %v", err)
	}
}
