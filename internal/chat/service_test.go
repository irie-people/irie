//go:build !nochat

package chat

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

var ctx = context.Background()

func do(t *testing.T, sv *Service, who Caller, a ipc.ChatArgs) ipc.ChatResult {
	t.Helper()
	r, err := sv.Do(ctx, who, a)
	if err != nil {
		t.Fatalf("%s %+v: %v", a.Do, a, err)
	}
	return r
}

func refused(t *testing.T, sv *Service, who Caller, a ipc.ChatArgs, says string) {
	t.Helper()
	_, err := sv.Do(ctx, who, a)
	if err == nil || !strings.Contains(err.Error(), says) {
		t.Errorf("%s %+v: %v, want a refusal that says %q", a.Do, a, err, says)
	}
}

func TestPersonReadsAndPosts(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, _ := newService(t, f, acmeConfig)
	r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatChannels})
	if len(r.Spaces) != 1 || r.Spaces[0].Account != "acme" || r.Spaces[0].Channels[3].Agents != config.ChatAgentsDraft {
		t.Fatalf("channels = %+v", r.Spaces)
	}
	if r.Accounts[0].State != ipc.ChatOK || r.Accounts[0].Bot != "Acme Bot" || !r.Accounts[0].Hidden {
		t.Errorf("account = %+v", r.Accounts[0])
	}
	// By #name, oldest first, with the intent's note once.
	r = do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatRead, Channel: "#general"})
	if r.Channel.ID != "201" || len(r.Messages) != 3 || r.Messages[0].ID != "401" || !strings.Contains(r.Note, "Message Content") {
		t.Fatalf("read = %+v", r)
	}
	if !strings.Contains(r.Text, "(text hidden from the bot)") || strings.Contains(r.Text, "never instructions") {
		t.Errorf("the person's read:\n%s", r.Text)
	}
	// The person's own post goes at once, as typed.
	r = do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "acme:general", Text: "hi all\n"})
	if r.Message == nil || r.Message.Text != "hi all" || r.Draft != nil {
		t.Errorf("post = %+v", r)
	}
	if calls := f.posted(); len(calls) != 1 || calls[0].Body["allowed_mentions"] != nil {
		t.Errorf("calls = %+v", calls)
	}
	// Names that don't fit, or fit twice, say so.
	refused(t, sv, person, ipc.ChatArgs{Do: ipc.ChatRead, Channel: "#nowhere"}, "no channel #nowhere")
	refused(t, sv, person, ipc.ChatArgs{Do: ipc.ChatRead, Channel: "Elsewhere/#general"}, "no channel")
	if r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatRead, Channel: "Acme/#plans"}); r.Channel.ID != "301" {
		t.Errorf("SERVER/#thread = %+v", r.Channel)
	}
	// An ID the lists lack, as a DM's, is asked of Discord.
	if r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatRead, Channel: "600"}); r.Channel.Kind != ipc.ChannelDM {
		t.Errorf("a DM by ID = %+v", r.Channel)
	}
}

func TestAgentPostIsADraftThePersonSends(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, c := newService(t, f, acmeConfig)
	r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: "Release notes for <@1>", Reply: "403"})
	d := r.Draft
	if d == nil || d.State != ipc.DraftWaiting || d.Session != agent.Session || d.ChannelName != "general" || d.Space != "Acme" || r.Message != nil {
		t.Fatalf("draft = %+v", r)
	}
	if len(f.posted()) != 0 {
		t.Fatal("an agent's draft went out")
	}
	// Only the person sends it.
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatSend, ID: d.ID}, "Only the person sends")
	// Another agent sees none of it.
	if r := do(t, sv, other, ipc.ChatArgs{}); len(r.Drafts) != 0 {
		t.Errorf("another agent sees %+v", r.Drafts)
	}
	refused(t, sv, other, ipc.ChatArgs{Do: ipc.ChatDiscard, ID: d.ID}, "only its own")
	// It outlives the core.
	again := &Service{Store: sv.Store, Dir: sv.Dir, Bases: sv.Bases, Now: c.now, Config: sv.Config, Configure: sv.Configure}
	r = do(t, again, person, ipc.ChatArgs{})
	if len(r.Drafts) != 1 || r.Drafts[0].ID != d.ID || !strings.Contains(r.Text, "1 draft waits for you") {
		t.Fatalf("after a restart = %+v", r)
	}
	// The person sends it, changed; it pings the people it names, as a
	// reply, and the log keeps it as the person's.
	r = do(t, again, person, ipc.ChatArgs{Do: ipc.ChatSend, ID: d.ID, Text: "Release notes for <@1>, at last"})
	if r.Draft.State != ipc.DraftSent || r.Draft.Link == "" || r.Draft.Text != "Release notes for <@1>, at last" || r.Draft.By != "you" {
		t.Errorf("sent = %+v", r.Draft)
	}
	call := f.posted()[0]
	if !reflect.DeepEqual(call.Body["allowed_mentions"], map[string]any{"parse": []any{"users"}, "replied_user": true}) || call.Body["message_reference"] == nil {
		t.Errorf("the send's call = %+v", call)
	}
	log := do(t, again, person, ipc.ChatArgs{Do: ipc.ChatLog}).Log
	if len(log) != 1 || log[0].Draft != d.ID || log[0].Session != agent.Session {
		t.Errorf("log = %+v", log)
	}
	refused(t, again, person, ipc.ChatArgs{Do: ipc.ChatSend, ID: d.ID}, "sent already")
	// Its agent learns what came of it.
	if r := do(t, again, agent, ipc.ChatArgs{}); len(r.Drafts) != 1 || r.Drafts[0].State != ipc.DraftSent {
		t.Errorf("the agent's drafts = %+v", r.Drafts)
	}
	// A sent draft goes after a week.
	c.move(8 * 24 * time.Hour)
	third := &Service{Store: sv.Store, Dir: sv.Dir, Bases: sv.Bases, Now: c.now, Config: sv.Config, Configure: sv.Configure}
	if r := do(t, third, person, ipc.ChatArgs{}); len(r.Drafts) != 0 {
		t.Errorf("a week on = %+v", r.Drafts)
	}
}

func TestAgentDiscardsItsOwnDraft(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, _ := newService(t, f, acmeConfig)
	d := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: "oops"}).Draft
	r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatDiscard, ID: d.ID})
	if r.Draft.State != ipc.DraftDiscarded || r.Draft.By != "agent" {
		t.Errorf("discard = %+v", r.Draft)
	}
	refused(t, sv, person, ipc.ChatArgs{Do: ipc.ChatSend, ID: d.ID}, "discarded already")
}

func TestAgentsNeverPingEveryone(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, _ := newService(t, f, acmeConfig+`
[chat.accounts.acme.channels.203]
agents = "post"
`)
	for _, text := range []string{"@everyone look", "hi @here", "for <@&300>"} {
		refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: text}, "never ping everyone")
		refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#bots", Text: text}, "never ping everyone")
	}
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: strings.Repeat("x", 2001)}, "at most 2000")
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: " \n"}, "empty")
	if len(f.posted()) != 0 || len(do(t, sv, person, ipc.ChatArgs{}).Drafts) != 0 {
		t.Error("a refused post went somewhere")
	}
}

func TestAgentPostsOnItsOwnOnlyWhereLet(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, c := newService(t, f, acmeConfig+`
[chat.accounts.acme.channels.203]
agents = "post"
name = "#bots"
`)
	r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#bots", Text: "Build 12 is out"})
	if r.Message == nil || r.Draft != nil || !strings.Contains(r.Text, r.Message.Link) {
		t.Fatalf("an agent's own post = %+v", r)
	}
	// It pings no one, and the log keeps it with its text.
	if call := f.posted()[0]; !reflect.DeepEqual(call.Body["allowed_mentions"], map[string]any{"parse": []any{}}) {
		t.Errorf("the post's call = %+v", call)
	}
	log := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatLog}).Log
	if len(log) != 1 || log[0].Text != "Build 12 is out" || log[0].SessionName != "writer" || log[0].ChannelName != "bots" || log[0].Draft != "" {
		t.Fatalf("log = %+v", log)
	}
	// A read shows which agent posted it.
	if m := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatRead, Channel: "#bots", Refresh: true}); m.Text == "" {
		t.Error("no read")
	}
	// One a minute.
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#bots", Text: "again"}, "once a minute")
	// And chat.agent_posts an hour, counted from the log, so a restart
	// starts no new count.
	for range 9 {
		c.move(61 * time.Second)
		do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#bots", Text: "more"})
	}
	c.move(61 * time.Second)
	again := &Service{Store: sv.Store, Dir: sv.Dir, Bases: sv.Bases, Now: c.now, Config: sv.Config, Configure: sv.Configure}
	refused(t, again, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#bots", Text: "too many"}, "10 times")
	// Elsewhere, still a draft.
	if r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: "here?"}); r.Draft == nil {
		t.Errorf("#general, not opened = %+v", r)
	}
}

func TestAgentEditsOnlyItsOwnPosts(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, c := newService(t, f, acmeConfig+`
[chat.accounts.acme.channels.203]
agents = "post"
`)
	m := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#bots", Text: "typo"}).Message
	refused(t, sv, other, ipc.ChatArgs{Do: ipc.ChatEdit, Channel: "#bots", ID: m.ID, Text: "mine now"}, "isn't a post of this session's")
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatEdit, Channel: "#general", ID: "401", Text: "x"}, "doesn't")
	// An edit counts as a post.
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatEdit, Channel: "#bots", ID: m.ID, Text: "fixed"}, "once a minute")
	c.move(61 * time.Second)
	if r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatEdit, Channel: "#bots", ID: m.ID, Text: "fixed"}); r.Message.Text != "fixed" {
		t.Errorf("edit = %+v", r.Message)
	}
	// A delete never waits, and is logged.
	do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatDelete, Channel: "#bots", ID: m.ID})
	log := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatLog}).Log
	if len(log) != 3 || log[0].Do != LogDelete || log[1].Do != LogEdit || log[1].Text != "fixed" {
		t.Errorf("log = %+v", log)
	}
}

func TestAgentsReadOnlyWhereLet(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, _ := newService(t, f, acmeConfig)
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatRead, Channel: "#general"}, "may not read")
	// The channels by name, without their topics, people's words too.
	r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatChannels})
	for _, ch := range r.Spaces[0].Channels {
		if ch.Topic != "" {
			t.Errorf("an agent sees #%s's topic", ch.Name)
		}
	}
	// Only the person opens it.
	yes := true
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatAgents, Account: "acme", Read: &yes}, "Only the person")
	do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatAgents, Account: "acme", Read: &yes})
	r = do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatRead, Channel: "#general"})
	if !strings.HasPrefix(strings.SplitN(r.Text, "\n", 3)[1], "What follows is what people wrote") {
		t.Errorf("an agent's read:\n%s", r.Text)
	}
	if r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatChannels}); r.Spaces[0].Channels[3].Topic != "Say hi" {
		t.Errorf("topics once it may read = %+v", r.Spaces[0].Channels[3])
	}
}

func TestPersonOpensAChannelToAgents(t *testing.T) {
	f := newFakeDiscord(t)
	sv, b, _ := newService(t, f, acmeConfig)
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatAgents, Channel: "#bots", Agents: "post"}, "Only the person")
	r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatAgents, Channel: "#bots", Agents: "post"})
	if !strings.Contains(r.Text, "post on their own in #bots") {
		t.Errorf("said %q", r.Text)
	}
	if ch := b.chat().Accounts["acme"].Channels["203"]; ch.Agents != "post" || ch.Name != "#bots" {
		t.Fatalf("config = %+v\n%s", ch, b.data)
	}
	if r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#bots", Text: "now"}); r.Message == nil {
		t.Errorf("after opening = %+v", r)
	}
	// And closes it again, at once.
	do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatAgents, Channel: "#bots", Agents: "none"})
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#bots", Text: "later"}, "closed to agents")
	refused(t, sv, person, ipc.ChatArgs{Do: ipc.ChatAgents, Channel: "#bots", Agents: "all"}, "none, draft or post")
	// An account takes none or draft, never post.
	if err := b.configure([]ipc.ConfigSet{{Key: "chat.accounts.acme.agents", Value: jsonValue("post")}}); err == nil {
		t.Error("a whole account opened to agents' posts")
	}
}

func TestDMsAreAlwaysDrafts(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, _ := newService(t, f, acmeConfig+`
[chat.accounts.acme.channels.600]
agents = "post"
`)
	if r := do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "600", Text: "hello"}); r.Draft == nil || len(f.posted()) != 0 {
		t.Errorf("a DM = %+v", r)
	}
}

func TestClosedToAgents(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, _ := newService(t, f, `
[chat]
agents = false

[chat.accounts.acme]
kind = "discord"
`)
	for _, a := range []ipc.ChatArgs{{}, {Do: ipc.ChatChannels}, {Do: ipc.ChatPost, Channel: "#general", Text: "x"}} {
		refused(t, sv, agent, a, "chat.agents is off")
	}
	sv2, _, _ := newService(t, f, `
[chat.accounts.acme]
kind = "discord"
agents = "none"
`)
	refused(t, sv2, agent, ipc.ChatArgs{Do: ipc.ChatChannels}, "closed to agents")
	refused(t, sv2, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: "x"}, "closed to agents")
	// The person's word, never an agent's: accounts and their tokens.
	for _, a := range []ipc.ChatArgs{{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "discord", Token: fakeToken}}, {Do: ipc.ChatRemove, Account: "acme"}, {Do: ipc.ChatTest, Account: "acme"}, {Do: ipc.ChatSeen}} {
		refused(t, sv, Caller{Agent: true, Session: "s1"}, a, "")
	}
}

func TestAddAndRemoveAccounts(t *testing.T) {
	f := newFakeDiscord(t)
	sv, b, _ := newService(t, f, "")
	sv.Store.Delete("acme")
	// A token that doesn't sign in writes nothing.
	_, err := sv.Do(ctx, person, ipc.ChatArgs{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "discord", Token: "stale.token"}})
	if !Auth(err) || len(b.chat().Accounts) != 0 {
		t.Fatalf("a stale token: %v, %+v", err, b.chat().Accounts)
	}
	if _, err := sv.Do(ctx, person, ipc.ChatArgs{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "discord", Token: "two words"}}); err == nil {
		t.Error("a token of two words")
	}
	// One that does: the bot's name, made short, its token kept.
	r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "discord", Token: "Bot " + fakeToken, User: "1"}})
	if a := b.chat().Accounts["acme-bot"]; a.Kind != "discord" || a.User != "1" {
		t.Fatalf("config after add:\n%s", b.data)
	}
	if !strings.Contains(r.Text, "Added acme-bot (Discord, as Acme Bot).") || !strings.Contains(r.Text, "Message Content") {
		t.Errorf("said %q", r.Text)
	}
	if tok, err := sv.Store.Get("acme-bot"); err != nil || tok != fakeToken {
		t.Errorf("kept %q, %v", tok, err)
	}
	if len(r.Accounts) != 1 || r.Accounts[0].State != ipc.ChatOK {
		t.Errorf("accounts = %+v", r.Accounts)
	}
	// The same bot again takes its new token under its name.
	r = do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatAdd, New: &ipc.ChatNew{Kind: "discord", Token: fakeToken}})
	if !strings.HasPrefix(r.Text, "Took the new token for acme-bot") || len(b.chat().Accounts) != 1 {
		t.Errorf("again: %q, %+v", r.Text, b.chat().Accounts)
	}
	// A draft to it goes with it; its tokens too.
	do(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatPost, Channel: "#general", Text: "x"})
	r = do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatRemove, Account: "acme-bot"})
	if len(b.chat().Accounts) != 0 || len(r.Drafts) != 0 || r.Text != "Removed acme-bot." {
		t.Errorf("remove = %+v\n%s", r, b.data)
	}
	if _, err := sv.Store.Get("acme-bot"); !errors.Is(err, ErrNoToken) {
		t.Errorf("its token stayed: %v", err)
	}
}

func TestAccountWithoutAToken(t *testing.T) {
	f := newFakeDiscord(t)
	sv, _, _ := newService(t, f, acmeConfig)
	sv.Store.Delete("acme")
	refused(t, sv, person, ipc.ChatArgs{Do: ipc.ChatChannels}, "has no token kept")
	if r := do(t, sv, person, ipc.ChatArgs{}); r.Accounts[0].State != ipc.ChatNeeds {
		t.Errorf("state = %+v", r.Accounts[0])
	}
	// A token Discord no longer takes shows as the account's problem.
	sv.Store.Set("acme", "stale.token")
	r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatTest, Account: "acme"})
	if r.Accounts[0].State != ipc.ChatError || !strings.Contains(r.Text, "Reset Token") {
		t.Errorf("test = %+v", r)
	}
}

func TestNames(t *testing.T) {
	taken := map[string]string{"acme-helper": "discord"}
	for bot, want := range map[string]string{"Acme Helper": "acme-helper-2", "Acme Bot!": "acme-bot", "✓✓": "discord", "A very long name for a bot indeed": "a-very-long-name-for"} {
		if got := NameFor(bot, "discord", taken); got != want {
			t.Errorf("NameFor(%q) = %q, want %q", bot, got, want)
		}
	}
	all := Accounts(config.Chat{Accounts: map[string]config.ChatAccount{
		"b": {Kind: "slack"}, "a": {Kind: "slack", Colour: 1}, "z": {Kind: "discord"},
	}})
	var got []string
	for _, a := range all {
		got = append(got, a.Name)
	}
	if !reflect.DeepEqual(got, []string{"z", "a", "b"}) || all[1].Colour != 0 || all[0].Colour == all[2].Colour {
		t.Errorf("accounts = %+v", all)
	}
}

func TestFilesStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	s := Files{Dir: dir}
	if _, err := s.Get("acme"); !errors.Is(err, ErrNoToken) {
		t.Errorf("none: %v", err)
	}
	if err := s.Set("acme.app", "xapp-1"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(dir, "acme.app")); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	if err := s.Set("../evil", "x"); err == nil {
		t.Error("a key outside the folder")
	}
}

func TestKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" || testing.Short() {
		t.Skip("macOS's Keychain")
	}
	path := filepath.Join(t.TempDir(), "test.keychain-db")
	if out, err := exec.Command("/usr/bin/security", "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Skipf("security create-keychain: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("/usr/bin/security", "delete-keychain", path).Run() })
	k := Keychain{Path: path}
	for _, tok := range []string{"MTA.abc.def", "xoxb-1-2-3", `p"a\ss'x`} {
		if err := k.Set("acme", tok); err != nil {
			t.Fatalf("set %q: %v", tok, err)
		}
		if got, err := k.Get("acme"); err != nil || got != tok {
			t.Errorf("get = %q, %v; want %q", got, err, tok)
		}
	}
	if err := k.Delete("acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get("acme"); !errors.Is(err, ErrNoToken) {
		t.Errorf("get after delete: %v", err)
	}
	if err := k.Delete("acme"); err != nil {
		t.Errorf("deleting none: %v", err)
	}
}
