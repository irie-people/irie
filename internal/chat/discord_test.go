//go:build !nochat

package chat

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

func connect(t *testing.T, f *fakeDiscord, token string) Connector {
	t.Helper()
	k, _ := KindOf(config.ChatDiscord)
	return k.New(Tokens{Bot: token}, f.srv.URL, http.DefaultClient)
}

func TestDiscordSignsIn(t *testing.T) {
	f := newFakeDiscord(t)
	ctx := context.Background()
	me, err := connect(t, f, "Bot "+fakeToken).Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if me.ID != botID || me.Name != "Acme Bot" {
		t.Errorf("me = %+v", me)
	}
	// Without the Message Content intent people's text is hidden, and
	// the fix is said.
	if !me.Hidden || !strings.Contains(me.Fix, "Message Content Intent") {
		t.Errorf("hidden = %v, fix = %q", me.Hidden, me.Fix)
	}
	f.flags = appMessageContentLimited
	if me, _ := connect(t, f, fakeToken).Me(ctx); me.Hidden {
		t.Error("the limited intent still reads as hidden")
	}
	// A token Discord doesn't know, in words, as a refusal of the token.
	_, err = connect(t, f, "stale.token").Me(ctx)
	if !Auth(err) || !strings.Contains(err.Error(), "Reset Token") {
		t.Errorf("a stale token: %v", err)
	}
	// A person's token: Discord forbids automating a person's account.
	f.human = true
	if _, err := connect(t, f, fakeToken).Me(ctx); err == nil || !strings.Contains(err.Error(), "forbids automating") {
		t.Errorf("a person's token: %v", err)
	}
}

func TestDiscordChannels(t *testing.T) {
	f := newFakeDiscord(t)
	sp, err := connect(t, f, fakeToken).Spaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sp) != 1 || sp[0].Name != "Acme" || sp[0].ID != guildID {
		t.Fatalf("spaces = %+v", sp)
	}
	var got []string
	by := map[string]ipc.ChatChannel{}
	for _, c := range sp[0].Channels {
		got = append(got, c.Name)
		by[c.Name] = c
	}
	// No category first, then each category's text before its voice,
	// threads after their channel, the hidden one left out.
	want := []string{"readme", "ideas", "First idea", "general", "plans", "announcements", "bots", "quiet", "voice"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	g := by["general"]
	if g.Kind != ipc.ChannelText || g.Category != "Talk" || g.Topic != "Say hi" || !g.Read || !g.Post || g.Link != discordApp+guildID+"/201" {
		t.Errorf("general = %+v", g)
	}
	if a := by["announcements"]; a.Kind != ipc.ChannelAnnouncement || a.Post || !strings.Contains(a.Why, "Send Messages") {
		t.Errorf("announcements, closed to @everyone's posts = %+v", a)
	}
	if b := by["bots"]; !b.Post {
		t.Errorf("bots, open to the bot's role = %+v", b)
	}
	if q := by["quiet"]; q.Post || !q.Read {
		t.Errorf("quiet, closed to the bot's role = %+v", q)
	}
	if r := by["readme"]; r.Read || !strings.Contains(r.Why, "Read Message History") {
		t.Errorf("readme, closed to the bot itself = %+v", r)
	}
	if p := by["plans"]; p.Kind != ipc.ChannelThread || p.Parent != "201" || !p.Post {
		t.Errorf("plans = %+v", p)
	}
	if i := by["ideas"]; i.Kind != ipc.ChannelForum {
		t.Errorf("ideas = %+v", i)
	}
}

func TestDiscordMessages(t *testing.T) {
	f := newFakeDiscord(t)
	c := connect(t, f, fakeToken)
	ctx := context.Background()
	sp, err := c.Spaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	general := sp[0].Channels[3]
	msgs, err := c.Messages(ctx, general, Page{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %+v", msgs)
	}
	// People, channels and roles by name; custom emoji by theirs; times
	// as dates.
	hi := msgs[0]
	want := "Hi @Acme Bot, see #announcements and ask @Helpers :wave: at " + time.Unix(0, 0).Local().Format("2 Jan 2006 15:04")
	if hi.Text != want || hi.Author != "Alice" || hi.Hidden || hi.Link != discordApp+guildID+"/201/403" {
		t.Errorf("hi = %+v\nwant text %q", hi, want)
	}
	// Empty text, from a person, while the intent is off: hidden, not
	// empty.
	if !msgs[1].Hidden || msgs[1].Author != "bob" {
		t.Errorf("hidden = %+v", msgs[1])
	}
	if !msgs[2].Mine || msgs[2].Hidden {
		t.Errorf("the bot's own = %+v", msgs[2])
	}
	older, err := c.Messages(ctx, general, Page{Before: "402", Limit: 50})
	if err != nil || len(older) != 1 || older[0].ID != "401" {
		t.Errorf("before 402 = %+v, %v", older, err)
	}
	if _, err := c.Messages(ctx, ipc.ChatChannel{ID: "204", Kind: ipc.ChannelForum}, Page{}); err == nil {
		t.Error("a forum read as a channel")
	}
}

func TestDiscordPosts(t *testing.T) {
	f := newFakeDiscord(t)
	c := connect(t, f, fakeToken)
	ctx := context.Background()
	general := ipc.ChatChannel{ID: "201", Name: "general", Kind: ipc.ChannelText, Space: guildID}
	ideas := ipc.ChatChannel{ID: "204", Name: "ideas", Kind: ipc.ChannelForum, Space: guildID}
	news := ipc.ChatChannel{ID: "202", Name: "announcements", Kind: ipc.ChannelAnnouncement, Space: guildID}
	for _, p := range []Post{
		{Channel: general, Text: "as typed"},
		{Channel: general, Text: "a draft sent", Reply: "403", Pings: PingPeople},
		{Channel: general, Text: "an agent's", Pings: PingNone},
	} {
		m, err := c.Post(ctx, p)
		if err != nil || m.Text != p.Text || !m.Mine {
			t.Fatalf("post %q = %+v, %v", p.Text, m, err)
		}
	}
	calls := f.posted()
	if _, ok := calls[0].Body["allowed_mentions"]; ok {
		t.Errorf("the person's own post set allowed_mentions: %v", calls[0].Body)
	}
	if !reflect.DeepEqual(calls[1].Body["allowed_mentions"], map[string]any{"parse": []any{"users"}, "replied_user": true}) ||
		!reflect.DeepEqual(calls[1].Body["message_reference"], map[string]any{"message_id": "403"}) {
		t.Errorf("a draft sent as a reply = %v", calls[1].Body)
	}
	if !reflect.DeepEqual(calls[2].Body["allowed_mentions"], map[string]any{"parse": []any{}}) {
		t.Errorf("an agent's own post = %v", calls[2].Body)
	}
	// A forum's post is a thread with a title, its message the post.
	m, err := c.Post(ctx, Post{Channel: ideas, Text: "Let's", Thread: "Big idea", Pings: PingNone})
	if err != nil || m.Thread == "" || m.Text != "Let's" {
		t.Fatalf("forum post = %+v, %v", m, err)
	}
	last := f.posted()[3]
	if last.Path != "/channels/204/threads" || last.Body["name"] != "Big idea" || last.Body["message"].(map[string]any)["content"] != "Let's" {
		t.Errorf("forum post's call = %+v", last)
	}
	if _, err := c.Post(ctx, Post{Channel: ideas, Text: "untitled"}); err == nil {
		t.Error("a forum post without a title went")
	}
	// A thread from a message, and one in an announcement channel.
	if _, err := c.Post(ctx, Post{Channel: general, Text: "about that", Reply: "403", Thread: "That"}); err != nil {
		t.Fatal(err)
	}
	calls = f.posted()
	if calls[4].Path != "/channels/201/messages/403/threads" || calls[5].Path != "/channels/403/messages" {
		t.Errorf("a thread from a message = %+v", calls[4:])
	}
	if _, err := c.Post(ctx, Post{Channel: news, Text: "news", Thread: "Week 1"}); err != nil {
		t.Fatal(err)
	}
	calls = f.posted()
	if calls[6].Path != "/channels/202/threads" || calls[6].Body["type"] != float64(dNewsThread) {
		t.Errorf("an announcement thread = %+v", calls[6])
	}
	// Too long, said with the numbers.
	if _, err := c.Post(ctx, Post{Channel: general, Text: strings.Repeat("a", discordMax+1)}); err == nil || !strings.Contains(err.Error(), "2000") {
		t.Errorf("too long: %v", err)
	}
	// An edit pings no more than it may; a delete goes.
	if m, err := c.Edit(ctx, general, "401", "Welcome!", PingNone); err != nil || m.ID != "401" || m.Edited.IsZero() {
		t.Errorf("edit = %+v, %v", m, err)
	}
	if err := c.Delete(ctx, general, "401"); err != nil {
		t.Error(err)
	}
	calls = f.posted()
	if e := calls[len(calls)-2]; e.Method != http.MethodPatch || !reflect.DeepEqual(e.Body["allowed_mentions"], map[string]any{"parse": []any{}}) {
		t.Errorf("edit's call = %+v", e)
	}
}

func TestDiscordRateLimits(t *testing.T) {
	f := newFakeDiscord(t)
	c := connect(t, f, fakeToken)
	ctx := context.Background()
	general := ipc.ChatChannel{ID: "201", Name: "general", Kind: ipc.ChannelText, Space: guildID}
	// A 429 is waited out, as it asks.
	f.limited["POST /channels/201/messages"] = 2
	start := time.Now()
	if _, err := c.Post(ctx, Post{Channel: general, Text: "patient"}); err != nil {
		t.Fatalf("after two 429s: %v", err)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Error("the 429s weren't waited out")
	}
	// One that asks for too long gives up, and says how long.
	f.limited["POST /channels/201/messages"] = 1
	f.retry = 120
	_, err := c.Post(ctx, Post{Channel: general, Text: "impatient"})
	var r *Refused
	if !errors.As(err, &r) || r.Status != http.StatusTooManyRequests || !strings.Contains(err.Error(), "2 minutes") {
		t.Errorf("a long wait: %v", err)
	}
	// And the route waits after, without asking Discord again.
	before := len(f.posted())
	if _, err := c.Post(ctx, Post{Channel: general, Text: "still too soon"}); err == nil || len(f.posted()) != before {
		t.Errorf("the held route was called: %v", err)
	}
}

func TestDiscordBuckets(t *testing.T) {
	l := newLimiter()
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	var slept time.Duration
	l.sleep = func(_ context.Context, d time.Duration) error { slept += d; return nil }
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", "0")
	h.Set("X-RateLimit-Reset-After", "1.5")
	l.heard("POST /channels/201/messages", h)
	if _, err := l.wait(context.Background(), "POST /channels/201/messages"); err != nil || slept != 1500*time.Millisecond {
		t.Errorf("waited %v, %v", slept, err)
	}
	// Another channel's bucket is its own.
	slept = 0
	if l.wait(context.Background(), "POST /channels/202/messages"); slept != 0 {
		t.Errorf("another channel waited %v", slept)
	}
	if got := route("PATCH", "/channels/201/messages/403"); got != "PATCH /channels/201/messages/{id}" {
		t.Errorf("route = %q", got)
	}
	if got := route("GET", "/guilds/100/members/900?x=1"); got != "GET /guilds/100/members/{id}" {
		t.Errorf("route = %q", got)
	}
}

func TestEveryone(t *testing.T) {
	for _, c := range []struct {
		kind, text, want string
	}{
		{config.ChatDiscord, "hello all", ""},
		{config.ChatDiscord, "hey @everyone look", "@everyone"},
		{config.ChatDiscord, "@HERE now", "@HERE"},
		{config.ChatDiscord, "ping <@&123>", "<@&123>"},
		{config.ChatDiscord, "thanks <@123>", ""},
		{config.ChatDiscord, "mail me at alice@example.com", ""},
		{config.ChatSlack, "<!here> standup", "<!here>"},
		{config.ChatSlack, "<!channel|channel> hi", "<!channel|channel>"},
		{config.ChatSlack, "<!everyone>", "<!everyone>"},
		{config.ChatSlack, "<!subteam^S123|@team> look", "<!subteam^S123|@team>"},
		{config.ChatSlack, "@channel please", "@channel"},
		{config.ChatSlack, "thanks <@U123>", ""},
	} {
		if got := Everyone(c.kind, c.text); got != c.want {
			t.Errorf("Everyone(%s, %q) = %q, want %q", c.kind, c.text, got, c.want)
		}
	}
}

func TestClean(t *testing.T) {
	in := "a\x1b[2Jb‮evil‬\r\nline two\x00\tend "
	if got := Clean(in); got != "a[2Jbevil\nline two\tend" {
		t.Errorf("Clean = %q", got)
	}
	if got := Line("one\ntwo   three", 9); got != "one two…" {
		t.Errorf("Line = %q", got)
	}
}
