//go:build !nochat

package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// Mentions and DMs as they come (docs/proposals/chat.md §4): Discord's
// Gateway, as its documentation says. The bot connects with the intents
// for servers, their messages and DMs, none of them privileged, and hears
// each message that mentions it, or the person, and each DM to it. Without
// the Message Content intent Discord still gives a bot the text of its DMs
// and of the messages that mention it.

// Event is a message heard as it came: a mention of the bot, or of the
// person, or a DM to the bot.
type Event struct {
	Message     ipc.ChatMessage
	ChannelName string
	DM          bool
	You         bool // it mentions the person, not the bot
}

// Listener is a connector that hears mentions and DMs as they come, until
// ctx ends or the service refuses for good; user is the person's own ID
// there, or "".
type Listener interface {
	Listen(ctx context.Context, user string, heard func(Event)) error
}

// The Gateway's intents the bot asks for (Discord's Gateway page).
const (
	intentGuilds         = 1 << 0
	intentGuildMessages  = 1 << 9
	intentDirectMessages = 1 << 12
)

// The Gateway's opcodes.
const (
	gwDispatch       = 0
	gwHeartbeat      = 1
	gwIdentify       = 2
	gwResume         = 6
	gwReconnect      = 7
	gwInvalidSession = 9
	gwHello          = 10
	gwHeartbeatACK   = 11
)

type gwPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

// gwState is what carries a session over from one connection to the
// next, so it resumes rather than identifies again.
type gwState struct {
	session string
	resume  string
	seq     atomic.Int64
}

// errAgain asks for a new connection at once: Discord's Reconnect, or a
// session it no longer knows.
var errAgain = errors.New("again")

// gwBackoff is the longest wait between two tries, and listenWait the
// first, which doubles after each try that fails soon.
const gwBackoff = 2 * time.Minute

var listenWait = time.Second

func (d *discord) Listen(ctx context.Context, user string, heard func(Event)) error {
	me, err := d.self(ctx)
	if err != nil {
		return err
	}
	var gw struct {
		URL string `json:"url"`
	}
	if err := d.do(ctx, http.MethodGet, "/gateway/bot", nil, &gw); err != nil {
		return err
	}
	st := &gwState{}
	wait := listenWait
	for {
		u := gw.URL
		if st.session != "" && st.resume != "" {
			u = st.resume
		}
		began := time.Now()
		err := d.connect(ctx, u, st, me, user, heard)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var c *Closed
		if errors.As(err, &c) {
			switch c.Code {
			case 4004:
				return &Refused{Status: http.StatusUnauthorized, Msg: "Discord's Gateway doesn't take this bot's token: make a new one (Developer Portal › the application › Bot › Reset Token), then `" + brand.Slug + " chat add discord` takes it."}
			case 4010, 4011, 4012, 4013, 4014:
				return &Refused{Msg: fmt.Sprintf("Discord's Gateway closed for good (%d): %s", c.Code, c.Reason)}
			case 4007, 4009:
				// A sequence or a session Discord no longer takes: a new one.
				st.session, st.resume = "", ""
			}
		}
		if errors.Is(err, errAgain) || time.Since(began) > time.Minute {
			wait = listenWait
		}
		if err := sleepCtx(ctx, wait/2+rand.N(wait/2+1)); err != nil {
			return err
		}
		wait = min(wait*2, gwBackoff)
	}
}

// connect is one connection to the Gateway, until it ends.
func (d *discord) connect(ctx context.Context, u string, st *gwState, me Me, user string, heard func(Event)) error {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ws, err := dialWS(cctx, strings.TrimRight(u, "/")+"/?v=10&encoding=json")
	if err != nil {
		return err
	}
	defer ws.c.Close()
	read := func() (gwPayload, error) {
		b, err := ws.ReadMessage()
		if err != nil {
			return gwPayload{}, err
		}
		var p gwPayload
		if err := json.Unmarshal(b, &p); err != nil {
			return gwPayload{}, fmt.Errorf("the Gateway's message didn't read: %v", err)
		}
		if p.S != nil {
			st.seq.Store(*p.S)
		}
		return p, nil
	}
	send := func(op int, d any) error {
		b, err := json.Marshal(map[string]any{"op": op, "d": d})
		if err != nil {
			return err
		}
		return ws.WriteText(b)
	}
	hello, err := read()
	if err != nil {
		return err
	}
	var h struct {
		Interval int `json:"heartbeat_interval"`
	}
	if hello.Op != gwHello || json.Unmarshal(hello.D, &h) != nil || h.Interval <= 0 {
		return errors.New("the Gateway said no hello")
	}
	if st.session != "" {
		err = send(gwResume, map[string]any{"token": d.token, "session_id": st.session, "seq": st.seq.Load()})
	} else {
		err = send(gwIdentify, map[string]any{
			"token":      d.token,
			"intents":    intentGuilds | intentGuildMessages | intentDirectMessages,
			"properties": map[string]string{"os": runtime.GOOS, "browser": brand.Slug, "device": brand.Slug},
		})
	}
	if err != nil {
		return err
	}
	// The heartbeat: the first after a random part of the interval, then
	// each interval; one not acknowledged before the next means the
	// connection is a zombie, closed with 4000 so the session resumes.
	var acked atomic.Bool
	acked.Store(true)
	beat := func() error {
		seq := st.seq.Load()
		if seq == 0 {
			return send(gwHeartbeat, nil)
		}
		return send(gwHeartbeat, seq)
	}
	go func() {
		every := time.Duration(h.Interval) * time.Millisecond
		if sleepCtx(cctx, time.Duration(rand.Float64()*float64(every))) != nil {
			return
		}
		for {
			if !acked.Load() {
				ws.CloseWith(4000)
				return
			}
			acked.Store(false)
			if beat() != nil {
				return
			}
			if sleepCtx(cctx, every) != nil {
				return
			}
		}
	}()
	for {
		p, err := read()
		if err != nil {
			return err
		}
		switch p.Op {
		case gwHeartbeat:
			if err := beat(); err != nil {
				return err
			}
		case gwHeartbeatACK:
			acked.Store(true)
		case gwReconnect:
			ws.CloseWith(4000)
			return errAgain
		case gwInvalidSession:
			var resumable bool
			json.Unmarshal(p.D, &resumable)
			if !resumable {
				st.session, st.resume = "", ""
			}
			sleepCtx(ctx, time.Second+rand.N(4*time.Second))
			return errAgain
		case gwDispatch:
			d.dispatch(p, st, me, user, heard)
		}
	}
}

// dispatch takes one event: a session's start, a server's channels for
// their names, and a message to hear.
func (d *discord) dispatch(p gwPayload, st *gwState, me Me, user string, heard func(Event)) {
	switch p.T {
	case "READY":
		var r struct {
			Session string `json:"session_id"`
			Resume  string `json:"resume_gateway_url"`
		}
		if json.Unmarshal(p.D, &r) == nil {
			st.session, st.resume = r.Session, r.Resume
		}
	case "GUILD_CREATE":
		var g struct {
			Channels []discordChannel `json:"channels"`
			Threads  []discordChannel `json:"threads"`
		}
		if json.Unmarshal(p.D, &g) != nil {
			return
		}
		d.mu.Lock()
		if d.names == nil {
			d.names = map[string]string{}
		}
		for _, c := range append(g.Channels, g.Threads...) {
			if c.Name != "" && c.Name != discordHidden {
				d.names["#"+c.ID] = Line(c.Name, 100)
			}
		}
		d.mu.Unlock()
	case "MESSAGE_CREATE":
		var m struct {
			discordMessage
			GuildID string `json:"guild_id"`
		}
		if json.Unmarshal(p.D, &m) != nil || m.Author.ID == me.ID {
			return
		}
		dm := m.GuildID == ""
		of := func(id string) bool {
			return id != "" && slices.ContainsFunc(m.Mentions, func(u discordUser) bool { return u.ID == id })
		}
		bot, you := of(me.ID), of(user)
		if !dm && !bot && !you {
			return
		}
		ch := ipc.ChatChannel{ID: m.ChannelID, Space: m.GuildID, Kind: ipc.ChannelText}
		d.mu.Lock()
		name := d.names["#"+m.ChannelID]
		d.mu.Unlock()
		switch {
		case dm:
			ch.Kind, name = ipc.ChannelDM, "DM with "+m.Author.name()
		case name == "":
			name = "a channel"
		}
		ch.Name = name
		heard(Event{Message: d.message(m.discordMessage, ch, me), ChannelName: name, DM: dm, You: you && !bot})
	}
}
