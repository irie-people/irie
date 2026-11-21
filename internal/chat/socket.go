//go:build !nochat

package chat

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// Mentions and DMs as they come on Slack: Socket Mode, as Slack's
// documentation says. The app-level token opens a connection
// (apps.connections.open), each envelope that comes is acknowledged by its
// ID, and Slack sends a disconnect before it ends one, after which another
// opens. The manifest subscribes the app to app_mention and message.im,
// the mentions of its bot and the DMs to it.

// ErrNoListen says an account can't hear as messages come: a Slack account
// without an app-level token.
var ErrNoListen = errors.New("this account can't hear mentions as they come")

type slackEnvelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"envelope_id"`
	Reason  string          `json:"reason"`
	Payload json.RawMessage `json:"payload"`
}

type slackEvent struct {
	Event struct {
		Type        string `json:"type"`
		Subtype     string `json:"subtype"`
		User        string `json:"user"`
		BotID       string `json:"bot_id"`
		Text        string `json:"text"`
		TS          string `json:"ts"`
		ThreadTS    string `json:"thread_ts"`
		Channel     string `json:"channel"`
		ChannelType string `json:"channel_type"`
	} `json:"event"`
}

func (s *slack) Listen(ctx context.Context, user string, heard func(Event)) error {
	if s.app == "" {
		return ErrNoListen
	}
	me, _, home, err := s.self(ctx)
	if err != nil {
		return err
	}
	wait := listenWait
	for {
		var a struct {
			URL string `json:"url"`
		}
		err := s.call(ctx, "apps.connections.open", nil, map[string]any{}, s.app, &a)
		if Auth(err) {
			return &Refused{Status: 401, Msg: "Slack doesn't take the app-level token: make one under Basic Information › App-Level Tokens, with connections:write, then `" + brand.Slug + " chat add slack` takes it."}
		}
		began := time.Now()
		if err == nil {
			err = s.socket(ctx, a.URL, me, home, heard)
		}
		if ctx.Err() != nil {
			return ctx.Err()
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

// socket is one Socket Mode connection, until Slack ends it.
func (s *slack) socket(ctx context.Context, u string, me Me, home string, heard func(Event)) error {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ws, err := dialWS(cctx, u)
	if err != nil {
		return err
	}
	defer ws.c.Close()
	for {
		b, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		var env slackEnvelope
		if json.Unmarshal(b, &env) != nil {
			continue
		}
		if env.ID != "" {
			// Acknowledged at once, as Slack asks, so it sends no copy.
			ack, _ := json.Marshal(map[string]string{"envelope_id": env.ID})
			if err := ws.WriteText(ack); err != nil {
				return err
			}
		}
		switch env.Type {
		case "disconnect":
			return errAgain
		case "events_api":
			var p slackEvent
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			e := p.Event
			if e.User == me.ID || e.BotID != "" || e.Subtype != "" {
				continue
			}
			dm := e.Type == "message" && e.ChannelType == "im"
			if e.Type != "app_mention" && !dm {
				continue
			}
			ch := ipc.ChatChannel{ID: e.Channel, Kind: ipc.ChannelText}
			name := ""
			if dm {
				ch.Kind = ipc.ChannelDM
				name = "DM with " + s.person(ctx, e.User)
			} else {
				s.mu.Lock()
				name = s.names["#"+e.Channel]
				s.mu.Unlock()
				if name == "" {
					name = "a channel"
				}
			}
			ch.Name = name
			looked := 0
			m := s.message(ctx, slackMessage{Type: "message", User: e.User, Text: e.Text, TS: e.TS, ThreadTS: e.ThreadTS}, ch, me, home, &looked)
			heard(Event{Message: m, ChannelName: name, DM: dm})
		}
	}
}
