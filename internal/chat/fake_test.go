//go:build !nochat

package chat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// A stand-in for Discord's API that answers as Discord's documentation
// says, for one server, Acme, and one bot, never Discord itself.

const (
	fakeToken = "good.discord.token"
	botID     = "900"
	guildID   = "100"
	botRole   = "300"
)

// Permission sets the fake server's roles and overwrites use.
const (
	everyoneCan = permViewChannel | permSendMessages | permReadHistory | permSendInThreads
)

type call struct {
	Method, Path string
	Body         map[string]any
}

type fakeDiscord struct {
	t     *testing.T
	srv   *httptest.Server
	mu    sync.Mutex
	flags int64
	human bool // the token is a person's
	// messages are each channel's, newest first.
	messages map[string][]map[string]any
	calls    []call
	// limited answers a route, "METHOD /path", with a 429 this many times
	// first, asking to wait retry.
	limited map[string]int
	retry   float64
	next    int
	// gateway is the stand-in Gateway's address, ws://.
	gateway string
}

func newFakeDiscord(t *testing.T) *fakeDiscord {
	f := &fakeDiscord{t: t, limited: map[string]int{}, retry: 0.01, next: 500}
	f.messages = map[string][]map[string]any{
		"201": {
			{"id": "403", "channel_id": "201", "type": 0, "author": user("1", "alice", "Alice"), "content": "Hi <@900>, see <#202> and ask <@&300> <:wave:77> at <t:0:R>", "timestamp": "2026-11-21T10:03:00Z", "mentions": []any{user(botID, "acme-bot", "Acme Bot")}},
			{"id": "402", "channel_id": "201", "type": 0, "author": user("2", "bob", ""), "content": "", "timestamp": "2026-11-21T10:02:00Z", "mentions": []any{}},
			{"id": "401", "channel_id": "201", "type": 0, "author": user(botID, "acme-bot", "Acme Bot"), "content": "Welcome", "timestamp": "2026-11-21T10:01:00Z", "mentions": []any{}},
		},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func user(id, name, global string) map[string]any {
	u := map[string]any{"id": id, "username": name, "global_name": nil}
	if global != "" {
		u["global_name"] = global
	}
	if id == botID {
		u["bot"] = true
	}
	return u
}

func ow(id string, typ int, allow, deny uint64) map[string]any {
	return map[string]any{"id": id, "type": typ, "allow": fmt.Sprint(allow), "deny": fmt.Sprint(deny)}
}

// channels are Acme's, as Get Guild Channels answers: out of order, with a
// hidden one Discord would leave out, and overwrites that close some to the
// bot.
func (f *fakeDiscord) channels() []map[string]any {
	return []map[string]any{
		{"id": "205", "type": dVoice, "guild_id": guildID, "name": "voice", "position": 3, "parent_id": "200"},
		{"id": "202", "type": dAnnouncement, "guild_id": guildID, "name": "announcements", "position": 1, "parent_id": "200",
			"permission_overwrites": []any{ow(guildID, 0, 0, permSendMessages)}},
		{"id": "200", "type": dCategory, "guild_id": guildID, "name": "Talk", "position": 0},
		{"id": "201", "type": dText, "guild_id": guildID, "name": "general", "position": 0, "parent_id": "200", "topic": "Say hi"},
		{"id": "203", "type": dText, "guild_id": guildID, "name": "bots", "position": 2, "parent_id": "200",
			"permission_overwrites": []any{ow(guildID, 0, 0, permSendMessages), ow(botRole, 0, permSendMessages, 0)}},
		{"id": "208", "type": dText, "guild_id": guildID, "name": "quiet", "position": 4, "parent_id": "200",
			"permission_overwrites": []any{ow(botRole, 0, 0, permSendMessages)}},
		{"id": "204", "type": dForum, "guild_id": guildID, "name": "ideas", "position": 5},
		{"id": "206", "type": dText, "guild_id": guildID, "name": discordHidden, "position": 4, "flags": channelObfuscated},
		{"id": "207", "type": dText, "guild_id": guildID, "name": "readme", "position": 0,
			"permission_overwrites": []any{ow(botID, 1, 0, permReadHistory)}},
	}
}

func (f *fakeDiscord) threads() []map[string]any {
	return []map[string]any{
		{"id": "301", "type": dThread, "guild_id": guildID, "name": "plans", "parent_id": "201", "message_count": 4},
		{"id": "302", "type": dThread, "guild_id": guildID, "name": "First idea", "parent_id": "204"},
	}
}

func (f *fakeDiscord) answer(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func (f *fakeDiscord) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("User-Agent"), "DiscordBot (") {
		f.answer(w, 400, map[string]any{"code": 0, "message": "no User-Agent"})
		return
	}
	if r.Header.Get("Authorization") != "Bot "+fakeToken {
		f.answer(w, 401, map[string]any{"code": 0, "message": "401: Unauthorized"})
		return
	}
	var body map[string]any
	if b, _ := io.ReadAll(r.Body); len(b) > 0 {
		if r.Header.Get("Content-Type") != "application/json" {
			f.answer(w, 400, map[string]any{"code": 50035, "message": "Invalid Form Body"})
			return
		}
		json.Unmarshal(b, &body)
	}
	path := r.URL.Path
	route := r.Method + " " + path
	if f.limited[route] > 0 {
		f.limited[route]--
		w.Header().Set("Retry-After", fmt.Sprint(f.retry))
		f.answer(w, 429, map[string]any{"message": "You are being rate limited.", "retry_after": f.retry, "global": false})
		return
	}
	if r.Method != http.MethodGet {
		f.calls = append(f.calls, call{Method: r.Method, Path: path, Body: body})
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case route == "GET /users/@me":
		u := user(botID, "acme-bot", "Acme Bot")
		if f.human {
			delete(u, "bot")
		}
		f.answer(w, 200, u)
	case route == "GET /gateway/bot":
		f.answer(w, 200, map[string]any{"url": f.gateway, "shards": 1})
	case route == "GET /applications/@me":
		f.answer(w, 200, map[string]any{"id": "800", "flags": f.flags})
	case route == "GET /users/@me/guilds":
		f.answer(w, 200, []any{map[string]any{"id": guildID, "name": "Acme"}})
	case route == "GET /guilds/"+guildID:
		f.answer(w, 200, map[string]any{"id": guildID, "name": "Acme", "owner_id": "1", "roles": []any{
			map[string]any{"id": guildID, "name": "@everyone", "permissions": fmt.Sprint(everyoneCan)},
			map[string]any{"id": botRole, "name": "Helpers", "permissions": "0"},
		}})
	case route == "GET /guilds/"+guildID+"/members/"+botID:
		f.answer(w, 200, map[string]any{"roles": []any{botRole}})
	case route == "GET /guilds/"+guildID+"/channels":
		f.answer(w, 200, f.channels())
	case route == "GET /guilds/"+guildID+"/threads/active":
		f.answer(w, 200, map[string]any{"threads": f.threads(), "members": []any{}})
	case len(parts) == 2 && parts[0] == "channels" && r.Method == http.MethodGet:
		for _, c := range append(f.channels(), f.threads()...) {
			if c["id"] == parts[1] {
				f.answer(w, 200, c)
				return
			}
		}
		if parts[1] == "600" {
			f.answer(w, 200, map[string]any{"id": "600", "type": dDM, "recipients": []any{user("1", "alice", "Alice")}})
			return
		}
		f.answer(w, 404, map[string]any{"code": 10003, "message": "Unknown Channel"})
	case len(parts) == 3 && parts[0] == "channels" && parts[2] == "messages" && r.Method == http.MethodGet:
		list := f.messages[parts[1]]
		if before := r.URL.Query().Get("before"); before != "" {
			for i, m := range list {
				if m["id"] == before {
					list = list[i+1:]
					break
				}
			}
		}
		if n := r.URL.Query().Get("limit"); n != "" {
			var k int
			fmt.Sscan(n, &k)
			if k < len(list) {
				list = list[:k]
			}
		}
		if list == nil {
			list = []map[string]any{}
		}
		f.answer(w, 200, list)
	case len(parts) == 3 && parts[0] == "channels" && parts[2] == "messages" && r.Method == http.MethodPost:
		f.answer(w, 200, f.made(parts[1], body))
	case len(parts) == 4 && parts[0] == "channels" && parts[2] == "messages" && r.Method == http.MethodPatch:
		m := f.made(parts[1], body)
		m["id"] = parts[3]
		m["edited_timestamp"] = "2026-11-21T11:00:00Z"
		f.answer(w, 200, m)
	case len(parts) == 4 && parts[0] == "channels" && parts[2] == "messages" && r.Method == http.MethodDelete:
		f.answer(w, 204, nil)
	case len(parts) == 3 && parts[0] == "channels" && parts[2] == "threads" && r.Method == http.MethodPost:
		f.next++
		id := fmt.Sprint(f.next)
		t := map[string]any{"id": id, "type": dThread, "guild_id": guildID, "name": body["name"], "parent_id": parts[1]}
		if parts[1] == "204" {
			msg, _ := body["message"].(map[string]any)
			t["message"] = f.made(id, msg)
		}
		f.answer(w, 201, t)
	case len(parts) == 5 && parts[0] == "channels" && parts[2] == "messages" && parts[4] == "threads":
		f.answer(w, 201, map[string]any{"id": parts[3], "type": dThread, "guild_id": guildID, "name": body["name"], "parent_id": parts[1]})
	default:
		f.answer(w, 404, map[string]any{"code": 0, "message": "404: Not Found " + route})
	}
}

// made is a message the bot posted in channel, as Discord answers it.
func (f *fakeDiscord) made(channel string, body map[string]any) map[string]any {
	f.next++
	return map[string]any{
		"id": fmt.Sprint(f.next), "channel_id": channel, "type": 0, "author": user(botID, "acme-bot", "Acme Bot"),
		"content": body["content"], "timestamp": "2026-11-21T11:00:00Z", "mentions": []any{},
	}
}

// posted are the calls that changed something, in order.
func (f *fakeDiscord) posted() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// box holds a test's config.toml, which Configure changes as the core
// does: each key's value read as the config op reads it, applied to the
// file, and the file read again.
type box struct {
	mu   sync.Mutex
	data []byte
	cfg  config.Config
}

func newBox(t *testing.T, toml string) *box {
	b := &box{data: []byte(toml)}
	cfg, err := config.Parse(b.data, "config.toml")
	if err != nil {
		t.Fatal(err)
	}
	b.cfg = cfg
	return b
}

func (b *box) chat() config.Chat {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.Chat
}

func (b *box) configure(sets []ipc.ConfigSet) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var edits []config.Edit
	for _, s := range sets {
		v, err := config.ParseValue(s.Key, s.Value)
		if err != nil {
			return err
		}
		edits = append(edits, config.Edit{Key: s.Key, Value: v})
	}
	out, err := config.Apply(b.data, edits)
	if err != nil {
		return err
	}
	cfg, err := config.Parse(out, "config.toml")
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	b.data, b.cfg = out, cfg
	return nil
}

// clock is a test's time, moved by hand.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) move(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

// acmeConfig is a config.toml with one Discord account, acme.
const acmeConfig = `
[chat.accounts.acme]
kind = "discord"
`

// newService is a service for the fake Discord, its token kept, its
// config from toml.
func newService(t *testing.T, f *fakeDiscord, toml string) (*Service, *box, *clock) {
	t.Helper()
	b := newBox(t, toml)
	c := &clock{at: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	sv := &Service{
		Store: Files{Dir: dir + "/keys"}, Dir: dir, Bases: map[string]string{config.ChatDiscord: f.srv.URL},
		Now: c.now, Config: b.chat, Configure: b.configure,
	}
	if err := sv.Store.Set("acme", fakeToken); err != nil {
		t.Fatal(err)
	}
	return sv, b, c
}

var (
	person = Caller{}
	agent  = Caller{Agent: true, Session: "s0000000000a1", Name: "writer"}
	other  = Caller{Agent: true, Session: "s0000000000b2", Name: "other"}
)
