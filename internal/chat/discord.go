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
	"github.com/irie-people/irie/internal/version"
)

// Discord, through a bot of the person's own: the REST API v10, written
// from Discord's public documentation (PROVENANCE.md). A bot sees only the
// servers it was invited to, and in each only the channels it may view.

// discordAPI is where Discord's API answers, version 10 in the path: an
// address without one gets an old, deprecated version.
const discordAPI = "https://discord.com/api/v10"

// discordApp is Discord's own app, where links to a channel or a message go.
const discordApp = "https://discord.com/channels/"

// discordMax is the most characters a Discord message takes.
const discordMax = 2000

// The application flags that say the bot gets people's message text: the
// Message Content intent, for a bot in many servers or a few.
const (
	appMessageContent        = 1 << 18
	appMessageContentLimited = 1 << 19
)

// discordIntentFix is where a bot's owner turns the Message Content intent
// on.
const discordIntentFix = "Discord leaves people's message text out for this bot until its Message Content intent is on: Developer Portal › the application › Bot › Privileged Gateway Intents › Message Content Intent."

// The permissions the bot's channels are read for (Discord's Permissions
// page).
const (
	permAdministrator uint64 = 1 << 3
	permViewChannel   uint64 = 1 << 10
	permSendMessages  uint64 = 1 << 11
	permReadHistory   uint64 = 1 << 16
	permConnect       uint64 = 1 << 20
	permManageThreads uint64 = 1 << 34
	permSendInThreads uint64 = 1 << 38
	permAll           uint64 = ^uint64(0)
)

// A channel the bot may not view, as Discord's Gateway names it since
// channels were hidden from bots: Get Guild Channels leaves such channels
// out, and Irie skips any that come.
const (
	channelObfuscated = 1 << 17
	discordHidden     = "___hidden___"
)

// discordGuildsPerLook is the most servers one look lists: each takes four
// calls.
const discordGuildsPerLook = 50

func init() {
	kinds[config.ChatDiscord] = Kind{
		Service: "Discord",
		Can:     ipc.ChatCan{Threads: true, Titles: true, Forums: true, Edit: true, Delete: true, Listen: true},
		MaxText: discordMax,
		New: func(t Tokens, base string, c *http.Client) Connector {
			if base == "" {
				base = discordAPI
			}
			return &discord{api: strings.TrimRight(base, "/"), token: discordToken(t.Bot), http: c, lim: newLimiter()}
		},
		Tidy: func(t Tokens) (Tokens, error) {
			t.Bot = discordToken(t.Bot)
			return t, CheckToken(t.Bot)
		},
	}
}

// discordToken is a token as Discord takes it: a paste of the whole
// header's value, "Bot …", is the same token.
func discordToken(t string) string {
	t = strings.TrimSpace(t)
	if rest, ok := strings.CutPrefix(t, "Bot "); ok {
		return strings.TrimSpace(rest)
	}
	return t
}

type discord struct {
	api   string
	token string
	http  *http.Client
	lim   *limiter

	mu    sync.Mutex
	me    Me
	names map[string]string // the names of channels and roles, by ID, from the last look, for mentions in text
}

// discordFail is the body of a refusal: Discord's code and message.
type discordFail struct {
	Code       int     `json:"code"`
	Message    string  `json:"message"`
	RetryAfter float64 `json:"retry_after"`
	Global     bool    `json:"global"`
}

// route is a call's rate-limit route: its method and path, with the IDs of
// what isn't a channel, a server or a webhook made one, as Discord's
// buckets go by the top-level resource.
func route(method, path string) string {
	parts := strings.Split(strings.SplitN(path, "?", 2)[0], "/")
	for i, p := range parts {
		if i > 0 && p != "" && strings.Trim(p, "0123456789") == "" {
			switch parts[i-1] {
			case "channels", "guilds", "webhooks":
				continue
			}
			parts[i] = "{id}"
		}
	}
	return method + " " + strings.Join(parts, "/")
}

// do calls Discord: path under the API, body as JSON when there is one,
// the answer into out. It waits out the route's rate limit and a 429's,
// up to maxWait, and says Discord's refusals in words.
func (d *discord) do(ctx context.Context, method, path string, body, out any) error {
	r := route(method, path)
	for try := 0; ; try++ {
		if long, err := d.lim.wait(ctx, r); err != nil {
			return err
		} else if long > 0 {
			return &Refused{Status: http.StatusTooManyRequests, Msg: "Discord asks the bot to wait " + said(long) + " before it calls again: try then."}
		}
		var rd io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, d.api+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bot "+d.token)
		req.Header.Set("User-Agent", "DiscordBot ("+brand.Repo+", "+version.Base+")")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := d.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("Discord didn't answer: %v", unwrapURL(err))
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("Discord's answer broke off: %v", err)
		}
		d.lim.heard(r, resp.Header)
		if resp.StatusCode == http.StatusTooManyRequests {
			var f discordFail
			json.Unmarshal(data, &f)
			wait, ok := seconds(resp.Header.Get("Retry-After"))
			if f.RetryAfter > 0 {
				wait, ok = time.Duration(f.RetryAfter*float64(time.Second)), true
			}
			if !ok {
				wait = time.Second
			}
			global := f.Global || resp.Header.Get("X-RateLimit-Global") == "true"
			d.lim.hold(r, wait, global)
			if try < 3 {
				continue
			}
			return &Refused{Status: resp.StatusCode, Msg: "Discord asks the bot to wait " + said(wait) + " before it calls again: try then."}
		}
		if resp.StatusCode >= 400 {
			return discordRefused(resp.StatusCode, data)
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("Discord's answer didn't read: %v", err)
			}
		}
		return nil
	}
}

// discordRefused is Discord's refusal of a call in words, with what fixes
// it.
func discordRefused(status int, data []byte) error {
	var f discordFail
	json.Unmarshal(data, &f)
	r := &Refused{Status: status, Code: strconv.Itoa(f.Code)}
	msg := Line(f.Message, 200)
	switch {
	case status == http.StatusUnauthorized:
		r.Msg = "Discord doesn't know this bot's token: make a new one (Developer Portal › the application › Bot › Reset Token), then `" + brand.Slug + " chat add discord` takes it."
	case f.Code == 50001:
		r.Msg = "The bot can't see that: give it View Channel there, in the server's settings."
	case f.Code == 50013:
		r.Msg = "The bot lacks a permission for that in this channel: give it Send Messages, Send Messages in Threads, Create Public Threads and Read Message History."
	case f.Code == 10003:
		r.Msg = "Discord has no such channel: it may have been deleted, or the bot taken out of its server."
	case f.Code == 10008:
		r.Msg = "Discord has no such message: it may have been deleted."
	case f.Code == 10004:
		r.Msg = "Discord has no such server for this bot: it may have been taken out of it."
	case f.Code == 50035:
		r.Msg = "Discord refused the message as it was written"
		if msg != "" {
			r.Msg += ": " + strings.TrimSuffix(msg, ".")
		}
		r.Msg += "."
	case msg != "":
		r.Msg = "Discord refused: " + strings.TrimSuffix(msg, ".") + "."
	default:
		r.Msg = fmt.Sprintf("Discord refused, with %d %s.", status, http.StatusText(status))
	}
	return r
}

// unwrapURL is a transport error without the method and address net/http
// puts before it, which say nothing a person needs.
func unwrapURL(err error) error {
	var u *url.Error
	if errors.As(err, &u) {
		return u.Err
	}
	return err
}

type discordUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Bot        bool   `json:"bot"`
}

func (u discordUser) name() string {
	if n := Line(u.GlobalName, 80); n != "" {
		return n
	}
	return Line(u.Username, 80)
}

func (d *discord) Me(ctx context.Context) (Me, error) {
	var u discordUser
	if err := d.do(ctx, http.MethodGet, "/users/@me", nil, &u); err != nil {
		return Me{}, err
	}
	if !u.Bot {
		return Me{}, &Refused{Msg: "This token is a person's, not a bot's, and Discord forbids automating a person's account: make a bot (Developer Portal › the application › Bot) and give " + brand.Name + " its token."}
	}
	var app struct {
		Flags int64 `json:"flags"`
	}
	if err := d.do(ctx, http.MethodGet, "/applications/@me", nil, &app); err != nil {
		return Me{}, err
	}
	me := Me{ID: u.ID, Name: u.name()}
	if app.Flags&(appMessageContent|appMessageContentLimited) == 0 {
		me.Hidden, me.Fix = true, discordIntentFix
	}
	d.mu.Lock()
	d.me = me
	d.mu.Unlock()
	return me, nil
}

// self is who the bot is, by the last Me.
func (d *discord) self(ctx context.Context) (Me, error) {
	d.mu.Lock()
	me := d.me
	d.mu.Unlock()
	if me.ID != "" {
		return me, nil
	}
	return d.Me(ctx)
}

type discordGuild struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OwnerID string `json:"owner_id"`
	Roles   []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Permissions string `json:"permissions"`
	} `json:"roles"`
}

type discordOverwrite struct {
	ID    string `json:"id"`
	Type  int    `json:"type"` // 0 a role, 1 a member
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

type discordChannel struct {
	ID         string             `json:"id"`
	Type       int                `json:"type"`
	GuildID    string             `json:"guild_id"`
	Name       string             `json:"name"`
	Topic      string             `json:"topic"`
	Position   int                `json:"position"`
	ParentID   string             `json:"parent_id"`
	Flags      int                `json:"flags"`
	Overwrites []discordOverwrite `json:"permission_overwrites"`
	Thread     *struct {
		Archived bool `json:"archived"`
		Locked   bool `json:"locked"`
	} `json:"thread_metadata"`
	MessageCount int `json:"message_count"`
}

// Discord's channel types (Discord's Channel page).
const (
	dText         = 0
	dDM           = 1
	dVoice        = 2
	dGroup        = 3
	dCategory     = 4
	dAnnouncement = 5
	dNewsThread   = 10
	dThread       = 11
	dPrivate      = 12
	dStage        = 13
	dForum        = 15
	dMedia        = 16
)

// kind is a Discord channel type as Irie names it; "" = none it lists.
func (c discordChannel) kind() string {
	switch c.Type {
	case dText:
		return ipc.ChannelText
	case dAnnouncement:
		return ipc.ChannelAnnouncement
	case dForum, dMedia:
		return ipc.ChannelForum
	case dVoice, dStage:
		return ipc.ChannelVoice
	case dNewsThread, dThread, dPrivate:
		return ipc.ChannelThread
	case dDM:
		return ipc.ChannelDM
	case dGroup:
		return ipc.ChannelGroup
	}
	return ""
}

func (d *discord) Spaces(ctx context.Context) ([]ipc.ChatSpace, error) {
	me, err := d.self(ctx)
	if err != nil {
		return nil, err
	}
	var guilds []discordGuild
	if err := d.do(ctx, http.MethodGet, "/users/@me/guilds?limit=200", nil, &guilds); err != nil {
		return nil, err
	}
	if len(guilds) > discordGuildsPerLook {
		guilds = guilds[:discordGuildsPerLook]
	}
	names := map[string]string{}
	var out []ipc.ChatSpace
	for _, g := range guilds {
		s, err := d.space(ctx, me, g.ID, names)
		if err != nil {
			out = append(out, ipc.ChatSpace{ID: g.ID, Name: Line(g.Name, 100), Problem: err.Error(), Channels: []ipc.ChatChannel{}})
			continue
		}
		out = append(out, s)
	}
	d.mu.Lock()
	d.names = names
	d.mu.Unlock()
	return out, nil
}

// space is one server and its channels, each with what the bot may do in
// it, worked out from its roles and the channel's overwrites as Discord's
// Permissions page says.
func (d *discord) space(ctx context.Context, me Me, id string, names map[string]string) (ipc.ChatSpace, error) {
	var g discordGuild
	if err := d.do(ctx, http.MethodGet, "/guilds/"+id, nil, &g); err != nil {
		return ipc.ChatSpace{}, err
	}
	var member struct {
		Roles []string `json:"roles"`
	}
	if err := d.do(ctx, http.MethodGet, "/guilds/"+id+"/members/"+me.ID, nil, &member); err != nil {
		return ipc.ChatSpace{}, err
	}
	var channels []discordChannel
	if err := d.do(ctx, http.MethodGet, "/guilds/"+id+"/channels", nil, &channels); err != nil {
		return ipc.ChatSpace{}, err
	}
	var active struct {
		Threads []discordChannel `json:"threads"`
	}
	if err := d.do(ctx, http.MethodGet, "/guilds/"+id+"/threads/active", nil, &active); err != nil {
		return ipc.ChatSpace{}, err
	}
	for _, r := range g.Roles {
		names["@&"+r.ID] = Line(r.Name, 80)
	}
	base := discordBase(g, me.ID, member.Roles)
	byID := map[string]discordChannel{}
	for _, c := range channels {
		byID[c.ID] = c
		if c.Name != discordHidden {
			names["#"+c.ID] = Line(c.Name, 100)
		}
	}
	threads := map[string][]discordChannel{}
	for _, t := range active.Threads {
		threads[t.ParentID] = append(threads[t.ParentID], t)
		names["#"+t.ID] = Line(t.Name, 100)
	}
	for p := range threads {
		slices.SortFunc(threads[p], func(a, b discordChannel) int { return snowflakeCmp(b.ID, a.ID) })
	}
	s := ipc.ChatSpace{ID: g.ID, Name: Line(g.Name, 100), Channels: []ipc.ChatChannel{}}
	for _, c := range discordOrder(channels) {
		if c.kind() == "" || c.Name == discordHidden || c.Flags&channelObfuscated != 0 {
			continue
		}
		perms := discordOverwrites(base, g.ID, me.ID, member.Roles, c)
		ch := d.channel(c, g.ID, perms, byID)
		s.Channels = append(s.Channels, ch)
		for _, t := range threads[c.ID] {
			tc := d.channel(t, g.ID, perms, byID)
			tc.Parent = c.ID
			s.Channels = append(s.Channels, tc)
		}
	}
	return s, nil
}

// channel is a Discord channel as Irie shows it, perms what the bot may do
// in it, or in a thread's channel.
func (d *discord) channel(c discordChannel, guild string, perms uint64, byID map[string]discordChannel) ipc.ChatChannel {
	ch := ipc.ChatChannel{ID: c.ID, Name: Line(c.Name, 100), Kind: c.kind(), Space: guild, Parent: c.ParentID, Topic: Line(c.Topic, 300), Link: discordApp + guild + "/" + c.ID}
	if p, ok := byID[c.ParentID]; ok && p.Type == dCategory {
		ch.Category = Line(p.Name, 100)
	}
	view := perms&permViewChannel != 0
	ch.Read = view && perms&permReadHistory != 0
	if ch.Kind == ipc.ChannelVoice {
		ch.Read = ch.Read && perms&permConnect != 0
	}
	switch ch.Kind {
	case ipc.ChannelThread:
		ch.Post = view && perms&permSendInThreads != 0
		if c.Thread != nil && (c.Thread.Locked || c.Thread.Archived) && perms&permManageThreads == 0 {
			ch.Post, ch.Why = false, "The thread is locked or archived."
		}
	default:
		ch.Post = view && perms&permSendMessages != 0
	}
	var lacks []string
	if !ch.Read {
		lacks = append(lacks, "Read Message History")
	}
	if !ch.Post && ch.Why == "" {
		if ch.Kind == ipc.ChannelThread {
			lacks = append(lacks, "Send Messages in Threads")
		} else {
			lacks = append(lacks, "Send Messages")
		}
	}
	if len(lacks) > 0 && ch.Why == "" {
		ch.Why = "The bot lacks " + strings.Join(lacks, " and ") + " here."
	}
	return ch
}

// discordBase is the bot's permissions across the server: @everyone's and
// its roles', or all of them as the owner or an administrator.
func discordBase(g discordGuild, me string, roles []string) uint64 {
	if g.OwnerID == me {
		return permAll
	}
	var p uint64
	for _, r := range g.Roles {
		if r.ID == g.ID || slices.Contains(roles, r.ID) {
			p |= bits(r.Permissions)
		}
	}
	if p&permAdministrator != 0 {
		return permAll
	}
	return p
}

// discordOverwrites applies a channel's overwrites to the bot's base
// permissions: @everyone's, then its roles' together, then its own.
func discordOverwrites(base uint64, guild, me string, roles []string, c discordChannel) uint64 {
	if base == permAll {
		return permAll
	}
	p := base
	for _, o := range c.Overwrites {
		if o.ID == guild {
			p &^= bits(o.Deny)
			p |= bits(o.Allow)
		}
	}
	var allow, deny uint64
	for _, o := range c.Overwrites {
		if o.Type == 0 && o.ID != guild && slices.Contains(roles, o.ID) {
			allow |= bits(o.Allow)
			deny |= bits(o.Deny)
		}
	}
	p &^= deny
	p |= allow
	for _, o := range c.Overwrites {
		if o.Type == 1 && o.ID == me {
			p &^= bits(o.Deny)
			p |= bits(o.Allow)
		}
	}
	return p
}

// bits reads a permission set, which Discord writes as a string.
func bits(s string) uint64 {
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

// discordOrder is a server's channels in the order Discord's own app shows
// them: those in no category first, then each category with its own, text
// before voice, each by its position.
func discordOrder(all []discordChannel) []discordChannel {
	voice := func(c discordChannel) int {
		if c.Type == dVoice || c.Type == dStage {
			return 1
		}
		return 0
	}
	byPos := func(a, b discordChannel) int {
		if d := voice(a) - voice(b); d != 0 {
			return d
		}
		if a.Position != b.Position {
			return a.Position - b.Position
		}
		return snowflakeCmp(a.ID, b.ID)
	}
	var cats, loose []discordChannel
	under := map[string][]discordChannel{}
	for _, c := range all {
		switch {
		case c.Type == dCategory:
			cats = append(cats, c)
		case c.ParentID == "":
			loose = append(loose, c)
		default:
			under[c.ParentID] = append(under[c.ParentID], c)
		}
	}
	slices.SortFunc(loose, byPos)
	slices.SortFunc(cats, func(a, b discordChannel) int {
		if a.Position != b.Position {
			return a.Position - b.Position
		}
		return snowflakeCmp(a.ID, b.ID)
	})
	out := loose
	for _, c := range cats {
		kids := under[c.ID]
		slices.SortFunc(kids, byPos)
		out = append(out, kids...)
		delete(under, c.ID)
	}
	// A channel whose category this list lacks still shows.
	for _, kids := range under {
		slices.SortFunc(kids, byPos)
		out = append(out, kids...)
	}
	return out
}

// snowflakeCmp orders two of Discord's IDs, which are numbers too long for
// some JSON readers and so come as strings.
func snowflakeCmp(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// Channel is one channel by its ID, for a DM or an archived thread the
// server's lists leave out. What the bot may do in it shows when it tries.
func (d *discord) Channel(ctx context.Context, id string) (ipc.ChatChannel, error) {
	var c discordChannel
	if err := d.do(ctx, http.MethodGet, "/channels/"+id, nil, &c); err != nil {
		return ipc.ChatChannel{}, err
	}
	if c.kind() == "" || c.Name == discordHidden {
		return ipc.ChatChannel{}, &Refused{Msg: "That channel holds no messages the bot can read."}
	}
	ch := ipc.ChatChannel{ID: c.ID, Name: Line(c.Name, 100), Kind: c.kind(), Space: c.GuildID, Parent: c.ParentID, Read: true, Post: true}
	switch {
	case c.GuildID != "":
		ch.Link = discordApp + c.GuildID + "/" + c.ID
	default:
		ch.Link = discordApp + "@me/" + c.ID
		if ch.Name == "" {
			ch.Name = "a DM"
		}
	}
	return ch, nil
}

type discordMessage struct {
	ID        string        `json:"id"`
	ChannelID string        `json:"channel_id"`
	Author    discordUser   `json:"author"`
	Content   string        `json:"content"`
	Timestamp time.Time     `json:"timestamp"`
	Edited    *time.Time    `json:"edited_timestamp"`
	Type      int           `json:"type"`
	Mentions  []discordUser `json:"mentions"`
	Attach    []struct{}    `json:"attachments"`
	Embeds    []struct{}    `json:"embeds"`
	Stickers  []struct{}    `json:"sticker_items"`
	Ref       *struct {
		ID string `json:"message_id"`
	} `json:"message_reference"`
	Thread *discordChannel `json:"thread"`
}

// discordReply is the message type of a reply.
const discordReply = 19

func (d *discord) Messages(ctx context.Context, ch ipc.ChatChannel, p Page) ([]ipc.ChatMessage, error) {
	if ch.Kind == ipc.ChannelForum {
		return nil, &Refused{Msg: "A forum's posts are its threads: read one of them."}
	}
	me, err := d.self(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(min(max(p.Limit, 1), 100)))
	if p.Before != "" {
		q.Set("before", p.Before)
	}
	if p.Thread != "" {
		// A thread is a channel of its own.
		ch.ID, ch.Kind, ch.Parent = p.Thread, ipc.ChannelThread, ch.ID
	}
	var list []discordMessage
	if err := d.do(ctx, http.MethodGet, "/channels/"+ch.ID+"/messages?"+q.Encode(), nil, &list); err != nil {
		return nil, err
	}
	out := make([]ipc.ChatMessage, 0, len(list))
	for _, m := range list {
		out = append(out, d.message(m, ch, me))
	}
	return out, nil
}

// message is a Discord message as Irie shows it.
func (d *discord) message(m discordMessage, ch ipc.ChatChannel, me Me) ipc.ChatMessage {
	out := ipc.ChatMessage{
		ID: m.ID, Channel: m.ChannelID, Author: m.Author.name(), AuthorID: m.Author.ID, Bot: m.Author.Bot,
		Mine: m.Author.ID == me.ID, Time: m.Timestamp, Text: d.text(m), Files: len(m.Attach),
	}
	if out.Channel == "" {
		out.Channel = ch.ID
	}
	if m.Edited != nil {
		out.Edited = *m.Edited
	}
	if m.Type == discordReply && m.Ref != nil {
		out.Reply = m.Ref.ID
	}
	if m.Thread != nil {
		out.Thread, out.Replies = m.Thread.ID, m.Thread.MessageCount
	}
	mentioned := slices.ContainsFunc(m.Mentions, func(u discordUser) bool { return u.ID == me.ID })
	if me.Hidden && m.Content == "" && len(m.Attach) == 0 && len(m.Embeds) == 0 && len(m.Stickers) == 0 &&
		!out.Mine && !mentioned && ch.Kind != ipc.ChannelDM && (m.Type == 0 || m.Type == discordReply) {
		out.Hidden = true
	}
	if ch.Space != "" {
		out.Link = discordApp + ch.Space + "/" + out.Channel + "/" + m.ID
	} else {
		out.Link = discordApp + "@me/" + out.Channel + "/" + m.ID
	}
	return out
}

// discordMarks are the marks Discord writes into a message's text for
// people, roles, channels, custom emoji and times.
var discordMarks = regexp.MustCompile(`<(@!?|@&|#)(\d+)>|<a?:(\w+):\d+>|<t:(-?\d+)(?::[tTdDfFR])?>`)

// text is a message's text as a person reads it: people, roles and
// channels by name, custom emoji by theirs, times as dates.
func (d *discord) text(m discordMessage) string {
	d.mu.Lock()
	names := d.names
	d.mu.Unlock()
	people := map[string]string{}
	for _, u := range m.Mentions {
		people[u.ID] = u.name()
	}
	s := discordMarks.ReplaceAllStringFunc(m.Content, func(mark string) string {
		g := discordMarks.FindStringSubmatch(mark)
		switch {
		case g[1] == "@" || g[1] == "@!":
			if n := people[g[2]]; n != "" {
				return "@" + n
			}
			return "@someone"
		case g[1] == "@&":
			if n := names["@&"+g[2]]; n != "" {
				return "@" + n
			}
			return "@a role"
		case g[1] == "#":
			if n := names["#"+g[2]]; n != "" {
				return "#" + n
			}
			return "#a channel"
		case g[3] != "":
			return ":" + g[3] + ":"
		case g[4] != "":
			if sec, err := strconv.ParseInt(g[4], 10, 64); err == nil {
				return time.Unix(sec, 0).Local().Format("2 Jan 2006 15:04")
			}
		}
		return mark
	})
	return Clean(s)
}

// discordMentions is the allowed_mentions object that pings no more than
// pings lets: nothing for an agent's own post, the people it names for a
// draft the person sent, and Discord's own way for the person's post.
func discordMentions(pings Pings, reply bool) any {
	switch pings {
	case PingNone:
		return map[string]any{"parse": []string{}}
	case PingPeople:
		return map[string]any{"parse": []string{"users"}, "replied_user": reply}
	}
	return nil
}

func (d *discord) Post(ctx context.Context, p Post) (ipc.ChatMessage, error) {
	if count(p.Text) > discordMax {
		return ipc.ChatMessage{}, &Refused{Msg: fmt.Sprintf("Discord takes at most %d characters a message, and this has %d.", discordMax, count(p.Text))}
	}
	if strings.TrimSpace(p.Text) == "" {
		return ipc.ChatMessage{}, &Refused{Msg: "Post what? The text is empty."}
	}
	me, err := d.self(ctx)
	if err != nil {
		return ipc.ChatMessage{}, err
	}
	ch := p.Channel
	msg := map[string]any{"content": p.Text}
	if m := discordMentions(p.Pings, p.Reply != ""); m != nil {
		msg["allowed_mentions"] = m
	}
	title := Line(p.Thread, 100)
	switch {
	case ch.Kind == ipc.ChannelForum:
		// A forum's post is a thread, its first message the post.
		if title == "" {
			return ipc.ChatMessage{}, &Refused{Msg: "A post in a forum needs a title: give it one (the thread's)."}
		}
		if p.Reply != "" {
			return ipc.ChatMessage{}, &Refused{Msg: "A forum's posts answer nothing: reply in the post's thread instead."}
		}
		var t struct {
			discordChannel
			Message discordMessage `json:"message"`
		}
		if err := d.do(ctx, http.MethodPost, "/channels/"+ch.ID+"/threads", map[string]any{"name": title, "message": msg}, &t); err != nil {
			return ipc.ChatMessage{}, err
		}
		thread := ch
		thread.ID, thread.Kind, thread.Parent = t.ID, ipc.ChannelThread, ch.ID
		out := d.message(t.Message, thread, me)
		out.Thread = t.ID
		return out, nil
	case title != "":
		// A thread, from the message it answers or in the channel, then the
		// post as its first message.
		var t discordChannel
		path, body := "/channels/"+ch.ID+"/threads", map[string]any{"name": title, "type": dThread}
		if ch.Kind == ipc.ChannelAnnouncement {
			body["type"] = dNewsThread
		}
		if p.Reply != "" {
			path, body = "/channels/"+ch.ID+"/messages/"+p.Reply+"/threads", map[string]any{"name": title}
		}
		if err := d.do(ctx, http.MethodPost, path, body, &t); err != nil {
			return ipc.ChatMessage{}, err
		}
		var m discordMessage
		if err := d.do(ctx, http.MethodPost, "/channels/"+t.ID+"/messages", msg, &m); err != nil {
			return ipc.ChatMessage{}, err
		}
		thread := ch
		thread.ID, thread.Kind, thread.Parent = t.ID, ipc.ChannelThread, ch.ID
		out := d.message(m, thread, me)
		out.Thread = t.ID
		return out, nil
	}
	if p.Reply != "" {
		msg["message_reference"] = map[string]any{"message_id": p.Reply}
	}
	var m discordMessage
	if err := d.do(ctx, http.MethodPost, "/channels/"+ch.ID+"/messages", msg, &m); err != nil {
		return ipc.ChatMessage{}, err
	}
	return d.message(m, ch, me), nil
}

func (d *discord) Edit(ctx context.Context, ch ipc.ChatChannel, id, text string, pings Pings) (ipc.ChatMessage, error) {
	if count(text) > discordMax {
		return ipc.ChatMessage{}, &Refused{Msg: fmt.Sprintf("Discord takes at most %d characters a message, and this has %d.", discordMax, count(text))}
	}
	if strings.TrimSpace(text) == "" {
		return ipc.ChatMessage{}, &Refused{Msg: "Change it to what? The text is empty: delete the message instead."}
	}
	me, err := d.self(ctx)
	if err != nil {
		return ipc.ChatMessage{}, err
	}
	body := map[string]any{"content": text}
	if m := discordMentions(pings, false); m != nil {
		body["allowed_mentions"] = m
	}
	var m discordMessage
	if err := d.do(ctx, http.MethodPatch, "/channels/"+ch.ID+"/messages/"+id, body, &m); err != nil {
		return ipc.ChatMessage{}, err
	}
	return d.message(m, ch, me), nil
}

func (d *discord) Delete(ctx context.Context, ch ipc.ChatChannel, id string) error {
	return d.do(ctx, http.MethodDelete, "/channels/"+ch.ID+"/messages/"+id, nil, nil)
}
