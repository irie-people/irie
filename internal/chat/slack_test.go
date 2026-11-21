//go:build !nochat

package chat

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// A stand-in for Slack's Web API that answers as Slack's documentation
// says, for one workspace, Acme, and one app's bot, never Slack itself.

const (
	slackBot = "xoxb-good-1"
	slackApp = "xapp-good-1"
)

type fakeSlack struct {
	srv *httptest.Server
	mu  sync.Mutex
	// calls are the methods that changed something, with their bodies.
	calls []call
	// asked are every method's query, by method.
	asked map[string][]url.Values
	// limited answers a method with a 429 this many times first.
	limited map[string]int
	// fail answers a method with this error.
	fail  map[string]string
	noBot bool
	// socket is the stand-in Socket Mode's address, ws://.
	socket string
}

func newFakeSlack(t *testing.T) *fakeSlack {
	f := &fakeSlack{asked: map[string][]url.Values{}, limited: map[string]int{}, fail: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSlack) answer(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (f *fakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	method := strings.TrimPrefix(r.URL.Path, "/")
	b, _ := io.ReadAll(r.Body)
	// From 2027-02-17 Slack answers a GET with a body with a 400.
	if r.Method == http.MethodGet && len(b) > 0 {
		w.WriteHeader(400)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	want := slackBot
	if method == "apps.connections.open" {
		want = slackApp
	}
	if token != want {
		f.answer(w, map[string]any{"ok": false, "error": "invalid_auth"})
		return
	}
	if f.limited[method] > 0 {
		f.limited[method]--
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(429)
		return
	}
	if e := f.fail[method]; e != "" {
		a := map[string]any{"ok": false, "error": e}
		if e == "missing_scope" {
			a["needed"] = "channels:history"
		}
		f.answer(w, a)
		return
	}
	var body map[string]any
	if r.Method == http.MethodPost {
		if r.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			f.answer(w, map[string]any{"ok": false, "error": "invalid_arguments"})
			return
		}
		json.Unmarshal(b, &body)
		if method != "auth.test" && method != "apps.connections.open" {
			f.calls = append(f.calls, call{Method: method, Body: body})
		}
	}
	q := r.URL.Query()
	f.asked[method] = append(f.asked[method], q)
	switch method {
	case "auth.test":
		a := map[string]any{"ok": true, "url": "https://acme.slack.com/", "team": "Acme", "user": "acme-bot", "team_id": "T1", "user_id": "U900", "bot_id": "B900"}
		if f.noBot {
			delete(a, "bot_id")
		}
		f.answer(w, a)
	case "apps.connections.open":
		u := f.socket
		if u == "" {
			u = "wss://example.com/link/?ticket=1"
		}
		f.answer(w, map[string]any{"ok": true, "url": u})
	case "conversations.list":
		if q.Get("cursor") == "" {
			f.answer(w, map[string]any{"ok": true, "channels": []any{
				map[string]any{"id": "C2", "name": "random", "is_channel": true, "is_member": false},
				map[string]any{"id": "C1", "name": "general", "is_channel": true, "is_member": true, "topic": map[string]any{"value": "Say hi"}},
			}, "response_metadata": map[string]any{"next_cursor": "p2"}})
			return
		}
		f.answer(w, map[string]any{"ok": true, "channels": []any{
			map[string]any{"id": "D1", "is_im": true, "user": "U1"},
			map[string]any{"id": "G1", "name": "mpdm-alice--bob-1", "is_mpim": true, "is_member": true},
		}, "response_metadata": map[string]any{"next_cursor": ""}})
	case "conversations.info":
		f.answer(w, map[string]any{"ok": true, "channel": map[string]any{"id": q.Get("channel"), "is_im": true, "user": "U1"}})
	case "users.info":
		if q.Get("user") == "U1" {
			f.answer(w, map[string]any{"ok": true, "user": map[string]any{"name": "alice", "profile": map[string]any{"display_name": "Alice"}}})
			return
		}
		f.answer(w, map[string]any{"ok": false, "error": "user_not_found"})
	case "conversations.history":
		if q.Get("channel") == "C2" {
			f.answer(w, map[string]any{"ok": false, "error": "not_in_channel"})
			return
		}
		msgs := []any{
			map[string]any{"type": "message", "user": "U1", "text": "Hi <@U900>, see <#C2|random>, <https://example.com|the site> &amp; <!here>", "ts": "1700000003.000300", "thread_ts": "1700000003.000300", "reply_count": 2},
			map[string]any{"type": "message", "subtype": "channel_join", "user": "U2", "text": "<@U2> has joined", "ts": "1700000002.000200"},
			map[string]any{"type": "message", "user": "U900", "bot_id": "B900", "text": "Welcome", "ts": "1700000001.000100", "edited": map[string]any{"ts": "1700000001.500000"}},
		}
		if q.Get("latest") == "1700000003.000300" {
			msgs = msgs[2:]
		}
		f.answer(w, map[string]any{"ok": true, "messages": msgs})
	case "conversations.replies":
		f.answer(w, map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "user": "U1", "text": "the start", "ts": "1700000003.000300", "thread_ts": "1700000003.000300", "reply_count": 1},
			map[string]any{"type": "message", "user": "U1", "text": "a reply", "ts": "1700000004.000400", "thread_ts": "1700000003.000300"},
		}})
	case "chat.postMessage":
		f.answer(w, map[string]any{"ok": true, "channel": body["channel"], "ts": "1700000009.000900", "message": map[string]any{"type": "message", "user": "U900", "bot_id": "B900", "text": body["text"], "ts": "1700000009.000900"}})
	case "chat.update":
		f.answer(w, map[string]any{"ok": true, "channel": body["channel"], "ts": body["ts"], "text": body["text"], "message": map[string]any{"user": "U900", "text": body["text"], "edited": map[string]any{"ts": "1700000010.000000"}}})
	case "chat.delete":
		f.answer(w, map[string]any{"ok": true})
	default:
		f.answer(w, map[string]any{"ok": false, "error": "unknown_method"})
	}
}

func (f *fakeSlack) posted() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func slackConnect(f *fakeSlack, bot, app string) Connector {
	k, _ := KindOf(config.ChatSlack)
	return k.New(Tokens{Bot: bot, App: app}, f.srv.URL, http.DefaultClient)
}

func TestSlackTokens(t *testing.T) {
	k, _ := KindOf(config.ChatSlack)
	for _, c := range []struct {
		t    Tokens
		says string
	}{
		{Tokens{Bot: "xapp-1"}, "app-level token"},
		{Tokens{Bot: "xoxp-1"}, "speaks as you"},
		{Tokens{Bot: "abc"}, "starts xoxb-"},
		{Tokens{Bot: "xoxb-1", App: "xoxb-2"}, "starts xapp-"},
	} {
		if _, err := k.Tidy(c.t); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("Tidy(%+v) = %v, want %q", c.t, err, c.says)
		}
	}
	if got, err := k.Tidy(Tokens{Bot: " xoxb-1 ", App: "xapp-1\n"}); err != nil || got.Bot != "xoxb-1" || got.App != "xapp-1" {
		t.Errorf("Tidy = %+v, %v", got, err)
	}
}

func TestSlackSignsIn(t *testing.T) {
	f := newFakeSlack(t)
	me, err := slackConnect(f, slackBot, "").Me(ctx)
	if err != nil || me.ID != "U900" || me.Name != "acme-bot" || me.Hidden {
		t.Fatalf("me = %+v, %v", me, err)
	}
	if _, err := slackConnect(f, "xoxb-stale", "").Me(ctx); !Auth(err) || !strings.Contains(err.Error(), "install the app") {
		t.Errorf("a stale token: %v", err)
	}
	f.noBot = true
	if _, err := slackConnect(f, slackBot, "").Me(ctx); err == nil || !strings.Contains(err.Error(), "a person's") {
		t.Errorf("no bot: %v", err)
	}
	f.noBot = false
	c := slackConnect(f, slackBot, slackApp).(*slack)
	if err := c.CheckApp(ctx); err != nil {
		t.Errorf("app token: %v", err)
	}
	if err := slackConnect(f, slackBot, "xapp-stale").(*slack).CheckApp(ctx); err == nil || !strings.Contains(err.Error(), "App-Level Tokens") {
		t.Errorf("a stale app token: %v", err)
	}
}

func TestSlackChannels(t *testing.T) {
	f := newFakeSlack(t)
	sp, err := slackConnect(f, slackBot, "").Spaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sp) != 1 || sp[0].ID != "T1" || sp[0].Name != "Acme" {
		t.Fatalf("spaces = %+v", sp)
	}
	var got []string
	for _, c := range sp[0].Channels {
		got = append(got, c.Name)
	}
	// Both pages, the bot's channels first, then the rest, the DMs last.
	if want := []string{"general", "random", "DM with Alice", "mpdm-alice--bob-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("channels = %v, want %v", got, want)
	}
	g, r, d := sp[0].Channels[0], sp[0].Channels[1], sp[0].Channels[2]
	if !g.Read || !g.Post || g.Topic != "Say hi" || g.Link != "https://acme.slack.com/archives/C1" {
		t.Errorf("general = %+v", g)
	}
	if r.Read || !strings.Contains(r.Why, "/invite") {
		t.Errorf("random = %+v", r)
	}
	if d.Kind != ipc.ChannelDM || !d.Post {
		t.Errorf("DM = %+v", d)
	}
	if q := f.asked["conversations.list"][0]; q.Get("types") != "public_channel,private_channel,mpim,im" || q.Get("exclude_archived") != "true" {
		t.Errorf("list asked %v", q)
	}
}

func TestSlackMessages(t *testing.T) {
	f := newFakeSlack(t)
	c := slackConnect(f, slackBot, "")
	general := ipc.ChatChannel{ID: "C1", Name: "general", Kind: ipc.ChannelText}
	if _, err := c.Spaces(ctx); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.Messages(ctx, general, Page{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %+v", msgs)
	}
	hi := msgs[0]
	if hi.Text != "Hi @acme-bot, see #random, the site (https://example.com) & @here" || hi.Author != "Alice" || hi.Thread != hi.ID || hi.Replies != 2 {
		t.Errorf("hi = %+v", hi)
	}
	if hi.Link != "https://acme.slack.com/archives/C1/p1700000003000300" || !hi.Time.Equal(time.Unix(1700000003, 300000).UTC()) {
		t.Errorf("hi's link and time = %q %v", hi.Link, hi.Time)
	}
	if w := msgs[1]; !w.Mine || !w.Bot || w.Edited.IsZero() {
		t.Errorf("the bot's own = %+v", w)
	}
	older, err := c.Messages(ctx, general, Page{Before: "1700000003.000300", Limit: 50})
	if err != nil || len(older) != 1 {
		t.Errorf("older = %+v, %v", older, err)
	}
	if q := f.asked["conversations.history"][1]; q.Get("latest") != "1700000003.000300" || q.Get("inclusive") != "false" {
		t.Errorf("history asked %v", q)
	}
	// A thread's messages, newest first as a channel's are.
	thread, err := c.Messages(ctx, general, Page{Thread: "1700000003.000300", Limit: 50})
	if err != nil || len(thread) != 2 || thread[0].Text != "a reply" || thread[0].Reply != "1700000003.000300" {
		t.Errorf("thread = %+v, %v", thread, err)
	}
	if _, err := c.Messages(ctx, ipc.ChatChannel{ID: "C2", Name: "random"}, Page{}); err == nil || !strings.Contains(err.Error(), "/invite") {
		t.Errorf("not in it: %v", err)
	}
}

func TestSlackPosts(t *testing.T) {
	f := newFakeSlack(t)
	c := slackConnect(f, slackBot, "")
	general := ipc.ChatChannel{ID: "C1", Name: "general", Kind: ipc.ChannelText}
	for _, p := range []Post{
		{Channel: general, Text: "a < b & <@U1> <!here> <https://example.com|site>"},
		{Channel: general, Text: "for <@U1> <!here> <!subteam^S1|@team>", Pings: PingPeople, Reply: "1700000003.000300"},
		{Channel: general, Text: "thanks <@U1>", Pings: PingNone},
	} {
		m, err := c.Post(ctx, p)
		if err != nil || !m.Mine || m.ID != "1700000009.000900" || m.Link != "https://acme.slack.com/archives/C1/p1700000009000900" {
			t.Fatalf("post %q = %+v, %v", p.Text, m, err)
		}
	}
	calls := f.posted()
	// The person's own: their marks kept, the rest of <, > and & escaped.
	if got := calls[0].Body["text"]; got != "a &lt; b &amp; <@U1> <!here> <https://example.com|site>" {
		t.Errorf("as typed = %q", got)
	}
	// A draft the person sent: its people, never everyone or a group.
	if got := calls[1].Body["text"]; got != "for <@U1> &lt;!here&gt; &lt;!subteam^S1|@team&gt;" || calls[1].Body["thread_ts"] != "1700000003.000300" {
		t.Errorf("a draft sent = %+v", calls[1].Body)
	}
	// An agent's own: people by name, pinging no one.
	if got := calls[2].Body["text"]; got != "thanks @Alice" {
		t.Errorf("an agent's = %q", got)
	}
	for _, c := range calls {
		if _, ok := c.Body["link_names"]; ok {
			t.Errorf("link_names sent: %+v", c.Body)
		}
	}
	if _, err := c.Post(ctx, Post{Channel: general, Text: "titled", Thread: "A title"}); err == nil || !strings.Contains(err.Error(), "no titles") {
		t.Errorf("a title: %v", err)
	}
	if m, err := c.Edit(ctx, general, "1700000001.000100", "Welcome!", PingNone); err != nil || m.Text != "Welcome!" || m.Edited.IsZero() {
		t.Errorf("edit = %+v, %v", m, err)
	}
	if err := c.Delete(ctx, general, "1700000001.000100"); err != nil {
		t.Error(err)
	}
	calls = f.posted()
	if e, d := calls[3], calls[4]; e.Method != "chat.update" || e.Body["ts"] != "1700000001.000100" || d.Method != "chat.delete" || d.Body["channel"] != "C1" {
		t.Errorf("edit and delete = %+v %+v", e, d)
	}
}

func TestSlackRefusals(t *testing.T) {
	f := newFakeSlack(t)
	c := slackConnect(f, slackBot, "")
	general := ipc.ChatChannel{ID: "C1", Name: "general", Kind: ipc.ChannelText}
	f.fail["conversations.history"] = "missing_scope"
	_, err := c.Messages(ctx, general, Page{})
	if err == nil || !strings.Contains(err.Error(), "lacks the scope channels:history") {
		t.Errorf("missing scope: %v", err)
	}
	delete(f.fail, "conversations.history")
	// A 429 is waited out, as Retry-After asks.
	f.limited["chat.postMessage"] = 2
	if _, err := c.Post(ctx, Post{Channel: general, Text: "patient"}); err != nil {
		t.Errorf("after two 429s: %v", err)
	}
	f.fail["chat.delete"] = "cant_delete_message"
	var r *Refused
	if err := c.Delete(ctx, general, "1"); !errors.As(err, &r) || r.Code != "cant_delete_message" || !strings.Contains(err.Error(), "only its own") {
		t.Errorf("someone else's: %v", err)
	}
}

func TestSlackManifest(t *testing.T) {
	var m struct {
		OAuth struct {
			Scopes struct {
				Bot []string `json:"bot"`
			} `json:"scopes"`
		} `json:"oauth_config"`
		Settings struct {
			Socket bool `json:"socket_mode_enabled"`
			Events struct {
				Bot []string `json:"bot_events"`
			} `json:"event_subscriptions"`
		} `json:"settings"`
	}
	if err := json.Unmarshal([]byte(SlackManifest()), &m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.OAuth.Scopes.Bot, slackScopes) || !m.Settings.Socket || !reflect.DeepEqual(m.Settings.Events.Bot, []string{"app_mention", "message.im"}) {
		t.Errorf("manifest = %+v", m)
	}
	u, err := url.Parse(SlackCreate())
	if err != nil || u.Host != "api.slack.com" || u.Query().Get("new_app") != "1" || u.Query().Get("manifest_json") != SlackManifest() {
		t.Errorf("create = %v, %v", u, err)
	}
	if !strings.Contains(SlackSteps(), "xoxb-") || !strings.Contains(SlackSteps(), "connections:write") {
		t.Errorf("steps:\n%s", SlackSteps())
	}
}

// TestSlackAgents: the agents' rules hold on Slack too: a draft the person
// sends, and an agent's own post that pings no one.
func TestSlackAgents(t *testing.T) {
	f := newFakeSlack(t)
	b := newBox(t, `
[chat.accounts.team]
kind = "slack"

[chat.accounts.team.channels.C1]
agents = "post"
`)
	c := &clock{at: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	sv := &Service{Store: Files{Dir: dir + "/keys"}, Dir: dir, Bases: map[string]string{config.ChatSlack: f.srv.URL}, Now: c.now, Config: b.chat, Configure: b.configure}
	sv.Store.Set("team", slackBot)
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: "<!channel> standup"}, "never ping everyone")
	r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: "Standup notes for <@U1>"})
	if r.Message == nil || f.posted()[0].Body["text"] != "Standup notes for @Alice" {
		t.Fatalf("an agent's own post = %+v, %+v", r, f.posted())
	}
	// Elsewhere a draft, which the person sends with its people.
	d := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "D1", Text: "Hi <@U1>"}).Draft
	if d == nil {
		t.Fatal("a DM went out on its own")
	}
	do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatSend, ID: d.ID})
	if got := f.posted()[1].Body["text"]; got != "Hi <@U1>" {
		t.Errorf("the sent draft = %q", got)
	}
	// Adding takes both tokens, and checks the app's.
	sv.Store.Delete("team")
	if _, err := sv.Do(ctx, person, ipc.ChatArgs{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "slack", Token: slackBot, App: "xapp-stale"}}); err == nil {
		t.Error("a stale app token was taken")
	}
	r = do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "slack", Name: "team", Token: slackBot, App: slackApp}})
	if !strings.Contains(r.Text, "team (Slack, as acme-bot)") {
		t.Errorf("add = %q", r.Text)
	}
	if tok, _ := sv.Store.Get(AppKey("team")); tok != slackApp {
		t.Errorf("the app token kept = %q", tok)
	}
	if r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatManifest}); !strings.Contains(r.Text, "api.slack.com/apps?new_app=1") || r.Manifest == "" {
		t.Errorf("manifest = %+v", r)
	}
}
