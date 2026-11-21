//go:build !nochat

package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// Slack, through a Slack app of the person's own installed to their
// workspace: its bot token (xoxb-) for the Web API, written from Slack's
// public documentation (PROVENANCE.md). The app sees only the channels its
// bot is in, and the DMs people start with it.

// slackAPI is where Slack's Web API answers, a method's name after it.
const slackAPI = "https://slack.com/api/"

// The most characters a message takes: Slack cuts text after 40,000, and
// reads best under 4,000, the most an agent writes.
const (
	slackMax      = 40000
	slackAgentMax = 4000
)

// slackScopes are the bot scopes the plugin uses, and asks for in the
// app's manifest: no more.
var slackScopes = []string{
	"channels:read", "groups:read", "im:read", "mpim:read",
	"channels:history", "groups:history", "im:history", "mpim:history",
	"chat:write", "users:read", "app_mentions:read",
}

func init() {
	kinds[config.ChatSlack] = Kind{
		Service: "Slack",
		Can:     ipc.ChatCan{Threads: true, Edit: true, Delete: true, Listen: true},
		MaxText: slackAgentMax,
		New: func(t Tokens, base string, c *http.Client) Connector {
			if base == "" {
				base = slackAPI
			}
			return &slack{api: strings.TrimRight(base, "/") + "/", bot: t.Bot, app: t.App, http: c, lim: newLimiter(), names: map[string]string{}}
		},
		Tidy: tidySlack,
	}
}

// tidySlack checks that each token is the kind it is given as: the bot's
// xoxb-, and the app-level xapp- for events. A person's own token, xoxp-,
// would speak as them.
func tidySlack(t Tokens) (Tokens, error) {
	t.Bot, t.App = strings.TrimSpace(t.Bot), strings.TrimSpace(t.App)
	if err := CheckToken(t.Bot); err != nil {
		return t, err
	}
	switch {
	case strings.HasPrefix(t.Bot, "xapp-"):
		return t, fmt.Errorf("that is the app-level token: the bot's comes first, from OAuth & Permissions, and starts xoxb-")
	case strings.HasPrefix(t.Bot, "xoxp-"):
		return t, fmt.Errorf("that is a user token, which speaks as you: give the bot's, from OAuth & Permissions, which starts xoxb-")
	case !strings.HasPrefix(t.Bot, "xoxb-"):
		return t, fmt.Errorf("a Slack bot's token starts xoxb-: copy the Bot User OAuth Token from OAuth & Permissions")
	}
	if t.App != "" {
		if err := CheckToken(t.App); err != nil {
			return t, err
		}
		if !strings.HasPrefix(t.App, "xapp-") {
			return t, fmt.Errorf("an app-level token starts xapp-: make one under Basic Information › App-Level Tokens, with connections:write")
		}
	}
	return t, nil
}

type slack struct {
	api  string
	bot  string
	app  string
	http *http.Client
	lim  *limiter

	mu    sync.Mutex
	me    Me
	team  string            // the workspace's ID
	url   string            // the workspace's address, ending in /
	names map[string]string // people's and channels' names, by ID, as read
}

// slackAnswer is what every method answers besides its own fields.
type slackAnswer struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
	Needed string `json:"needed"`
	Meta   struct {
		Next string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// call calls method: a read as GET with its query, a change as POST with
// JSON, token in the Authorization header either way. It waits out a 429's
// Retry-After, up to maxWait, and says Slack's refusals in words.
func (s *slack) call(ctx context.Context, method string, query url.Values, body map[string]any, token string, out any) error {
	route := method
	for try := 0; ; try++ {
		if long, err := s.lim.wait(ctx, route); err != nil {
			return err
		} else if long > 0 {
			return &Refused{Status: http.StatusTooManyRequests, Msg: "Slack asks the app to wait " + said(long) + " before it calls " + method + " again: try then."}
		}
		var req *http.Request
		var err error
		if body == nil {
			u := s.api + method
			if len(query) > 0 {
				u += "?" + query.Encode()
			}
			req, err = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		} else {
			b, merr := json.Marshal(body)
			if merr != nil {
				return merr
			}
			req, err = http.NewRequestWithContext(ctx, http.MethodPost, s.api+method, bytes.NewReader(b))
			if req != nil {
				req.Header.Set("Content-Type", "application/json; charset=utf-8")
			}
		}
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := s.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("Slack didn't answer: %v", unwrapURL(err))
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("Slack's answer broke off: %v", err)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait, ok := seconds(resp.Header.Get("Retry-After"))
			if !ok {
				wait = time.Second
			}
			s.lim.hold(route, wait, false)
			if try < 3 {
				continue
			}
			return &Refused{Status: resp.StatusCode, Msg: "Slack asks the app to wait " + said(wait) + " before it calls " + method + " again: try then."}
		}
		if resp.StatusCode >= 400 {
			return &Refused{Status: resp.StatusCode, Msg: fmt.Sprintf("Slack refused %s, with %d %s.", method, resp.StatusCode, http.StatusText(resp.StatusCode))}
		}
		var a slackAnswer
		if err := json.Unmarshal(data, &a); err != nil {
			return fmt.Errorf("Slack's answer didn't read: %v", err)
		}
		if !a.OK {
			return slackRefused(method, a)
		}
		if out != nil {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("Slack's answer didn't read: %v", err)
			}
		}
		return nil
	}
}

// slackRefused is Slack's refusal of a method in words, with what fixes
// it.
func slackRefused(method string, a slackAnswer) error {
	r := &Refused{Code: a.Error}
	switch a.Error {
	case "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive":
		r.Status = http.StatusUnauthorized
		r.Msg = "Slack doesn't take this app's token any more: install the app to your workspace again (OAuth & Permissions), then `" + brand.Slug + " chat add slack` takes its new token."
	case "missing_scope":
		need := Line(a.Needed, 80)
		if need == "" {
			need = "one it needs"
		}
		r.Msg = "The Slack app lacks the scope " + need + ": add it under OAuth & Permissions, install the app again, and give " + brand.Name + " the new token."
	case "not_in_channel":
		r.Msg = "The app's bot isn't in that channel: invite it there, with /invite and its name."
	case "channel_not_found":
		r.Msg = "Slack has no such channel for this app: it may be private, with the bot not in it."
	case "message_not_found", "thread_not_found":
		r.Msg = "Slack has no such message: it may have been deleted."
	case "cant_update_message", "cant_delete_message", "edit_window_closed":
		r.Msg = "Slack lets a bot change only its own messages, while they can still be changed."
	case "msg_too_long":
		r.Msg = "Slack refused the message as too long."
	case "is_archived":
		r.Msg = "That channel is archived."
	case "restricted_action", "restricted_action_read_only_channel", "restricted_action_thread_only_channel", "restricted_action_non_threadable_channel":
		r.Msg = "The workspace doesn't let this app do that there."
	case "no_text":
		r.Msg = "Post what? The text is empty."
	case "":
		r.Msg = "Slack refused " + method + "."
	default:
		r.Msg = "Slack refused " + method + ": " + Line(a.Error, 80) + "."
	}
	return r
}

func (s *slack) Me(ctx context.Context) (Me, error) {
	var a struct {
		URL    string `json:"url"`
		Team   string `json:"team"`
		User   string `json:"user"`
		TeamID string `json:"team_id"`
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
	}
	if err := s.call(ctx, "auth.test", nil, map[string]any{}, s.bot, &a); err != nil {
		return Me{}, err
	}
	if a.BotID == "" {
		return Me{}, &Refused{Msg: "This token is a person's, not a bot's: give " + brand.Name + " the app's Bot User OAuth Token, which starts xoxb-."}
	}
	me := Me{ID: a.UserID, Name: Line(a.User, 80)}
	s.mu.Lock()
	s.me, s.team, s.url = me, a.TeamID, a.URL
	if s.url != "" && !strings.HasSuffix(s.url, "/") {
		s.url += "/"
	}
	s.names["@"+a.UserID] = me.Name
	s.names["team"] = Line(a.Team, 100)
	s.mu.Unlock()
	return me, nil
}

// CheckApp says whether the app-level token opens Socket Mode: Slack hands
// out a connection's address, which goes unused.
func (s *slack) CheckApp(ctx context.Context) error {
	if s.app == "" {
		return nil
	}
	var a struct {
		URL string `json:"url"`
	}
	err := s.call(ctx, "apps.connections.open", nil, map[string]any{}, s.app, &a)
	var r *Refused
	switch {
	case errors.As(err, &r) && r.Status == http.StatusUnauthorized:
		return &Refused{Status: r.Status, Msg: "Slack doesn't take the app-level token: make one under Basic Information › App-Level Tokens, with connections:write."}
	case errors.As(err, &r) && r.Code == "missing_scope":
		return &Refused{Msg: "The app-level token lacks connections:write: make one with it under Basic Information › App-Level Tokens."}
	}
	return err
}

// self is who the bot is, and its workspace, by the last Me.
func (s *slack) self(ctx context.Context) (Me, string, string, error) {
	s.mu.Lock()
	me, team, u := s.me, s.team, s.url
	s.mu.Unlock()
	if me.ID != "" {
		return me, team, u, nil
	}
	me, err := s.Me(ctx)
	if err != nil {
		return Me{}, "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return me, s.team, s.url, nil
}

type slackChannel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsChannel bool   `json:"is_channel"`
	IsGroup   bool   `json:"is_group"`
	IsIM      bool   `json:"is_im"`
	IsMPIM    bool   `json:"is_mpim"`
	IsPrivate bool   `json:"is_private"`
	IsMember  bool   `json:"is_member"`
	Archived  bool   `json:"is_archived"`
	User      string `json:"user"`
	Topic     struct {
		Value string `json:"value"`
	} `json:"topic"`
}

// slackMaxChannels is the most channels one look lists.
const slackMaxChannels = 2000

func (s *slack) Spaces(ctx context.Context) ([]ipc.ChatSpace, error) {
	_, team, home, err := s.self(ctx)
	if err != nil {
		return nil, err
	}
	var all []slackChannel
	cursor := ""
	for len(all) < slackMaxChannels {
		q := url.Values{"types": {"public_channel,private_channel,mpim,im"}, "exclude_archived": {"true"}, "limit": {"200"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var page struct {
			slackAnswer
			Channels []slackChannel `json:"channels"`
		}
		if err := s.call(ctx, "conversations.list", q, nil, s.bot, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Channels...)
		cursor = page.Meta.Next
		if cursor == "" {
			break
		}
	}
	s.mu.Lock()
	name := s.names["team"]
	s.mu.Unlock()
	sp := ipc.ChatSpace{ID: team, Name: name, Channels: []ipc.ChatChannel{}}
	var named, direct []ipc.ChatChannel
	for _, c := range all {
		ch := s.channel(ctx, c, team, home)
		if ch.Kind == ipc.ChannelDM || ch.Kind == ipc.ChannelGroup {
			direct = append(direct, ch)
		} else {
			named = append(named, ch)
		}
	}
	// The channels the bot is in first, then the rest, each by name; the
	// DMs last.
	byName := func(a, b ipc.ChatChannel) int {
		if a.Read != b.Read {
			if a.Read {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	}
	slices.SortStableFunc(named, byName)
	slices.SortStableFunc(direct, byName)
	sp.Channels = append(append(sp.Channels, named...), direct...)
	return []ipc.ChatSpace{sp}, nil
}

// channel is a Slack conversation as Irie shows it.
func (s *slack) channel(ctx context.Context, c slackChannel, team, home string) ipc.ChatChannel {
	ch := ipc.ChatChannel{ID: c.ID, Name: Line(c.Name, 100), Kind: ipc.ChannelText, Space: team, Topic: Line(c.Topic.Value, 300)}
	switch {
	case c.IsIM:
		ch.Kind, ch.Name = ipc.ChannelDM, "DM with "+s.person(ctx, c.User)
		ch.Read, ch.Post = true, true
	case c.IsMPIM:
		ch.Kind = ipc.ChannelGroup
		ch.Read, ch.Post = c.IsMember, c.IsMember
	default:
		ch.Read, ch.Post = c.IsMember, c.IsMember
	}
	if !ch.Read {
		ch.Why = "The app's bot isn't in this channel: invite it there, with /invite and its name."
	}
	if home != "" {
		ch.Link = home + "archives/" + c.ID
	}
	s.mu.Lock()
	s.names["#"+c.ID] = ch.Name
	s.mu.Unlock()
	return ch
}

// Channel is one conversation by its ID, as for a DM someone started.
func (s *slack) Channel(ctx context.Context, id string) (ipc.ChatChannel, error) {
	_, team, home, err := s.self(ctx)
	if err != nil {
		return ipc.ChatChannel{}, err
	}
	var a struct {
		Channel slackChannel `json:"channel"`
	}
	if err := s.call(ctx, "conversations.info", url.Values{"channel": {id}}, nil, s.bot, &a); err != nil {
		return ipc.ChatChannel{}, err
	}
	return s.channel(ctx, a.Channel, team, home), nil
}

// person is the name of the person whose ID is id: from what was read,
// else users.info, else the ID.
func (s *slack) person(ctx context.Context, id string) string {
	if id == "" {
		return "someone"
	}
	s.mu.Lock()
	n, ok := s.names["@"+id]
	s.mu.Unlock()
	if ok {
		return n
	}
	var a struct {
		User struct {
			Name    string `json:"name"`
			Profile struct {
				Display string `json:"display_name"`
				Real    string `json:"real_name"`
			} `json:"profile"`
		} `json:"user"`
	}
	n = id
	if err := s.call(ctx, "users.info", url.Values{"user": {id}}, nil, s.bot, &a); err == nil {
		for _, v := range []string{a.User.Profile.Display, a.User.Profile.Real, a.User.Name} {
			if v = Line(v, 80); v != "" {
				n = v
				break
			}
		}
	}
	s.mu.Lock()
	s.names["@"+id] = n
	s.mu.Unlock()
	return n
}

type slackMessage struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	User     string `json:"user"`
	BotID    string `json:"bot_id"`
	Username string `json:"username"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
	Replies  int    `json:"reply_count"`
	Edited   *struct {
		TS string `json:"ts"`
	} `json:"edited"`
	Files []struct{} `json:"files"`
}

// slackPeople is the most names one page looks up.
const slackPeople = 20

func (s *slack) Messages(ctx context.Context, ch ipc.ChatChannel, p Page) ([]ipc.ChatMessage, error) {
	me, _, home, err := s.self(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{"channel": {ch.ID}, "limit": {strconv.Itoa(min(max(p.Limit, 1), 100))}}
	method := "conversations.history"
	if p.Thread != "" {
		method = "conversations.replies"
		q.Set("ts", p.Thread)
	}
	if p.Before != "" {
		q.Set("latest", p.Before)
		q.Set("inclusive", "false")
	}
	var page struct {
		Messages []slackMessage `json:"messages"`
	}
	if err := s.call(ctx, method, q, nil, s.bot, &page); err != nil {
		return nil, err
	}
	list := page.Messages
	if p.Thread != "" {
		// Replies come oldest first, the thread's own message among them.
		for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
			list[i], list[j] = list[j], list[i]
		}
	}
	looked := 0
	out := make([]ipc.ChatMessage, 0, len(list))
	for _, m := range list {
		if m.Subtype == "channel_join" || m.Subtype == "channel_leave" {
			continue
		}
		out = append(out, s.message(ctx, m, ch, me, home, &looked))
	}
	return out, nil
}

// message is a Slack message as Irie shows it; looked counts the names
// looked up for its page.
func (s *slack) message(ctx context.Context, m slackMessage, ch ipc.ChatChannel, me Me, home string, looked *int) ipc.ChatMessage {
	out := ipc.ChatMessage{ID: m.TS, Channel: ch.ID, AuthorID: m.User, Bot: m.BotID != "", Mine: m.User == me.ID, Time: slackTime(m.TS), Files: len(m.Files)}
	switch {
	case m.User != "" && *looked < slackPeople:
		s.mu.Lock()
		_, known := s.names["@"+m.User]
		s.mu.Unlock()
		if !known {
			*looked++
		}
		out.Author = s.person(ctx, m.User)
	case m.User != "":
		out.Author = m.User
	case m.Username != "":
		out.Author = Line(m.Username, 80)
	default:
		out.Author = "a bot"
	}
	out.Text = s.text(m.Text)
	if m.Edited != nil {
		out.Edited = slackTime(m.Edited.TS)
	}
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		out.Reply = m.ThreadTS
	}
	if m.Replies > 0 {
		out.Thread, out.Replies = m.TS, m.Replies
	}
	out.Link = slackLink(home, ch.ID, m.TS)
	return out
}

// slackTime is a message's ts as a time: seconds and microseconds.
func slackTime(ts string) time.Time {
	sec, frac, _ := strings.Cut(ts, ".")
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}
	}
	us, _ := strconv.ParseInt((frac + "000000")[:6], 10, 64)
	return time.Unix(n, us*1000).UTC()
}

// slackLink is a message's permalink, in the form chat.getPermalink
// answers: the workspace, archives, the channel, and p with the ts's
// digits.
func slackLink(home, channel, ts string) string {
	if home == "" || ts == "" {
		return ""
	}
	return home + "archives/" + channel + "/p" + strings.ReplaceAll(ts, ".", "")
}

// slackMarks are Slack's marks in a message's text: people, channels,
// everyone and its kin, groups, and links.
var slackMarks = regexp.MustCompile(`<([@#!])([^>|]*)(?:\|([^>]*))?>|<((?:https?|mailto):[^>|]+)(?:\|([^>]*))?>`)

// text is a message's text as a person reads it: people and channels by
// name, links with their words, Slack's escapes undone.
func (s *slack) text(t string) string {
	out := slackMarks.ReplaceAllStringFunc(t, func(mark string) string {
		g := slackMarks.FindStringSubmatch(mark)
		switch {
		case g[1] == "@":
			if g[3] != "" {
				return "@" + g[3]
			}
			s.mu.Lock()
			n, ok := s.names["@"+g[2]]
			s.mu.Unlock()
			if ok {
				return "@" + n
			}
			return "@someone"
		case g[1] == "#":
			if g[3] != "" {
				return "#" + g[3]
			}
			s.mu.Lock()
			n, ok := s.names["#"+g[2]]
			s.mu.Unlock()
			if ok {
				return "#" + n
			}
			return "#a channel"
		case g[1] == "!":
			if g[3] != "" {
				return strings.TrimPrefix(g[3], "@")
			}
			if strings.HasPrefix(g[2], "subteam^") {
				return "@a group"
			}
			return "@" + g[2]
		case g[4] != "":
			if g[5] != "" && g[5] != g[4] {
				return g[5] + " (" + g[4] + ")"
			}
			return g[4]
		}
		return mark
	})
	out = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(out)
	return Clean(out)
}

// slackSpecials are the marks that ping everyone or a group.
var slackSpecials = regexp.MustCompile(`<!(?:everyone|channel|here|subteam\^)[^>]*>`)

// slackUsers are the marks that ping one person.
var slackUsers = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|[^>]*)?>`)

// slackOut is text as Slack takes it, pinging no more than pings lets.
// Slack reads <, > and & as marks, so a person's own text keeps the marks
// it spells out and has the rest escaped; a draft the person sent keeps
// its people and loses everyone, here and groups; an agent's own post
// names its people without pinging them.
func (s *slack) slackOut(ctx context.Context, text string, pings Pings) string {
	marks := regexp.MustCompile(`<(?:[@#!][^>\s]*|(?:https?|mailto):[^>\s]+)>`)
	var b strings.Builder
	last := 0
	for _, loc := range marks.FindAllStringIndex(text, -1) {
		b.WriteString(escape(text[last:loc[0]]))
		mark := text[loc[0]:loc[1]]
		switch {
		case pings != PingAsTyped && slackSpecials.MatchString(mark):
			b.WriteString(escape(mark))
		case pings == PingNone && slackUsers.MatchString(mark):
			b.WriteString("@" + s.person(ctx, slackUsers.FindStringSubmatch(mark)[1]))
		default:
			b.WriteString(mark)
		}
		last = loc[1]
	}
	b.WriteString(escape(text[last:]))
	return b.String()
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func (s *slack) Post(ctx context.Context, p Post) (ipc.ChatMessage, error) {
	if strings.TrimSpace(p.Text) == "" {
		return ipc.ChatMessage{}, &Refused{Msg: "Post what? The text is empty."}
	}
	if count(p.Text) > slackMax {
		return ipc.ChatMessage{}, &Refused{Msg: fmt.Sprintf("Slack takes at most %d characters a message, and this has %d.", slackMax, count(p.Text))}
	}
	if p.Thread != "" {
		return ipc.ChatMessage{}, &Refused{Msg: "Slack's threads have no titles: post the message, then reply to it."}
	}
	me, _, home, err := s.self(ctx)
	if err != nil {
		return ipc.ChatMessage{}, err
	}
	body := map[string]any{"channel": p.Channel.ID, "text": s.slackOut(ctx, p.Text, p.Pings)}
	if p.Reply != "" {
		body["thread_ts"] = p.Reply
	}
	var a struct {
		Channel string       `json:"channel"`
		TS      string       `json:"ts"`
		Message slackMessage `json:"message"`
	}
	if err := s.call(ctx, "chat.postMessage", nil, body, s.bot, &a); err != nil {
		return ipc.ChatMessage{}, err
	}
	m := a.Message
	if m.TS == "" {
		m.TS = a.TS
	}
	if m.User == "" {
		m.User = me.ID
	}
	looked := slackPeople
	out := s.message(ctx, m, p.Channel, me, home, &looked)
	out.Author, out.Mine = me.Name, true
	return out, nil
}

func (s *slack) Edit(ctx context.Context, ch ipc.ChatChannel, id, text string, pings Pings) (ipc.ChatMessage, error) {
	if strings.TrimSpace(text) == "" {
		return ipc.ChatMessage{}, &Refused{Msg: "Change it to what? The text is empty: delete the message instead."}
	}
	if count(text) > slackMax {
		return ipc.ChatMessage{}, &Refused{Msg: fmt.Sprintf("Slack takes at most %d characters a message, and this has %d.", slackMax, count(text))}
	}
	me, _, home, err := s.self(ctx)
	if err != nil {
		return ipc.ChatMessage{}, err
	}
	var a struct {
		TS      string       `json:"ts"`
		Message slackMessage `json:"message"`
	}
	if err := s.call(ctx, "chat.update", nil, map[string]any{"channel": ch.ID, "ts": id, "text": s.slackOut(ctx, text, pings)}, s.bot, &a); err != nil {
		return ipc.ChatMessage{}, err
	}
	m := a.Message
	if m.TS == "" {
		m.TS = id
	}
	if m.User == "" {
		m.User = me.ID
	}
	looked := slackPeople
	out := s.message(ctx, m, ch, me, home, &looked)
	out.Author, out.Mine = me.Name, true
	if out.Edited.IsZero() {
		out.Edited = time.Now().UTC()
	}
	return out, nil
}

func (s *slack) Delete(ctx context.Context, ch ipc.ChatChannel, id string) error {
	return s.call(ctx, "chat.delete", nil, map[string]any{"channel": ch.ID, "ts": id}, s.bot, nil)
}

// SlackManifest is the Slack app's manifest, as JSON: a bot with the
// scopes the plugin uses, Socket Mode for mentions and DMs as they come.
func SlackManifest() string {
	m := map[string]any{
		"_metadata":           map[string]any{"major_version": 1},
		"display_information": map[string]any{"name": brand.Name, "description": brand.Name + "'s chat: your workspace beside your sessions, through this app's bot."},
		"features":            map[string]any{"bot_user": map[string]any{"display_name": brand.Name, "always_online": false}},
		"oauth_config":        map[string]any{"scopes": map[string]any{"bot": slackScopes}},
		"settings": map[string]any{
			"socket_mode_enabled":    true,
			"org_deploy_enabled":     false,
			"token_rotation_enabled": false,
			"event_subscriptions":    map[string]any{"bot_events": []string{"app_mention", "message.im"}},
		},
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

// SlackCreate is the address that opens Slack's Create an App with the
// manifest filled in.
func SlackCreate() string {
	return "https://api.slack.com/apps?new_app=1&manifest_json=" + url.QueryEscape(SlackManifest())
}

// SlackSteps are the steps that make the Slack app and give Irie its
// tokens, in words.
func SlackSteps() string {
	return `Slack takes an app of your own, installed to your workspace:
1. Open this link, pick your workspace, and create the app (its manifest asks for only what ` + brand.Name + ` uses):
   ` + SlackCreate() + `
2. Install to Workspace, then copy the Bot User OAuth Token (xoxb-) from OAuth & Permissions.
3. For mentions and DMs as they come: Basic Information › App-Level Tokens › Generate, with connections:write (xapp-).
4. Invite the bot to each channel it should read: /invite and its name.`
}
