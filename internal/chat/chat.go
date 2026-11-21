//go:build !nochat

// Package chat is the chat plugin's service (docs/proposals/chat.md): team
// and community chat beside Irie's sessions, Discord and Slack first, each
// through a bot of the person's own. Each service is a connector behind one
// interface, so another service is one more connector, with no change to
// the tab, the words, the agents' rules or the notices.
//
// The core reads and posts, and clients draw. It keeps what it reads in
// memory only; on disk it keeps the agents' drafts and the log of what
// agents posted, which are the agents' own words (drafts.go, log.go).
package chat

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// Connector is one service's way in, for one account.
type Connector interface {
	// Me is who the token is, and what the service gives it.
	Me(ctx context.Context) (Me, error)
	// Spaces are where its channels live, each with its channels: Discord's
	// servers, Slack's workspace.
	Spaces(ctx context.Context) ([]ipc.ChatSpace, error)
	// Messages are a channel's or a thread's messages, newest first: the
	// newest Page.Limit, or those before Page.Before.
	Messages(ctx context.Context, ch ipc.ChatChannel, p Page) ([]ipc.ChatMessage, error)
	// Post posts a message, as Post says.
	Post(ctx context.Context, p Post) (ipc.ChatMessage, error)
	// Edit changes a message's text, pinging no more than pings lets, and
	// Delete removes it.
	Edit(ctx context.Context, ch ipc.ChatChannel, id, text string, pings Pings) (ipc.ChatMessage, error)
	Delete(ctx context.Context, ch ipc.ChatChannel, id string) error
}

// Me is who an account's token is.
type Me struct {
	ID   string // the bot's user ID
	Name string // its name, as people see it
	// Hidden says the service leaves people's message text out for this
	// bot, and Fix says what turns it on.
	Hidden bool
	Fix    string
}

// Page asks for some of a channel's messages.
type Page struct {
	Before string // the messages before this one; "" = the newest
	Limit  int
}

// Post is a message to post.
type Post struct {
	Channel ipc.ChatChannel
	Text    string
	Reply   string // the message it answers
	// Thread is the title of a thread it starts: from Reply's message, or
	// in a forum as a new post, or in the channel.
	Thread string
	// Pings is whom it may ping.
	Pings Pings
}

// Pings is whom a post may ping.
type Pings int

const (
	// PingAsTyped pings whom the text names, as the service's own app
	// would: a person's own post.
	PingAsTyped Pings = iota
	// PingPeople pings the people the text names and no one else: a draft
	// the person sent.
	PingPeople
	// PingNone pings no one: an agent's own post.
	PingNone
)

// Kind is a kind of account: its service and its connector.
type Kind struct {
	Service string // Discord
	Can     ipc.ChatCan
	// MaxText is the most characters a message takes.
	MaxText int
	// New makes the connector for an account, with its tokens, the
	// service's address (a test's fake service, else "") and the client
	// it asks with.
	New func(t Tokens, base string, c *http.Client) Connector
	// Tidy is the tokens as the service takes them, such as a Discord
	// token pasted with its "Bot " in front, or what is wrong with them
	// before the service is asked, such as Slack's app token given as its
	// bot token.
	Tidy func(t Tokens) (Tokens, error)
}

// Tokens are an account's tokens.
type Tokens struct {
	Bot string // the bot's token
	App string // Slack's app-level token, for events; "" = none
}

// kinds are the connectors this build has, by config's kind: each
// connector's file adds itself.
var kinds = map[string]Kind{}

// KindOf is the kind named kind.
func KindOf(kind string) (Kind, bool) {
	k, ok := kinds[kind]
	return k, ok
}

// ServiceName is the name of kind's service, such as Discord.
func ServiceName(kind string) string {
	if k, ok := kinds[kind]; ok {
		return k.Service
	}
	return kind
}

// Account is one account as config.toml has it.
type Account struct {
	Name string
	config.ChatAccount
	Colour int // 0 to 5: its own colour when it sets one, else one of spread
}

// spread is the order colours go to accounts that leave the pick to Irie:
// each near the opposite side of the wheel from the one before, as Mail's
// accounts and the cards side by side are.
var spread = []int{0, 3, 1, 4, 2, 5}

// Accounts are the accounts in c, Discord's first, then Slack's, each kind
// by name, with their colours worked out.
func Accounts(c config.Chat) []Account {
	var out []Account
	for name, a := range c.Accounts {
		out = append(out, Account{Name: name, ChatAccount: a})
	}
	slices.SortFunc(out, func(a, b Account) int {
		if d := slices.Index(config.ChatKinds, a.Kind) - slices.Index(config.ChatKinds, b.Kind); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	// An account's own colour first, then the rest from spread, skipping
	// any taken while some are free.
	used := map[int]bool{}
	for i, a := range out {
		if a.ChatAccount.Colour > 0 {
			out[i].Colour = a.ChatAccount.Colour - 1
			used[out[i].Colour] = true
		}
	}
	next := 0
	for i, a := range out {
		if a.ChatAccount.Colour > 0 {
			continue
		}
		pick := spread[next%len(spread)]
		for tries := 0; used[pick] && tries < len(spread) && len(used) < len(spread); tries++ {
			next++
			pick = spread[next%len(spread)]
		}
		next++
		out[i].Colour = pick
		used[pick] = true
	}
	return out
}

// Find is the account named name, or the only one when name is "".
func Find(all []Account, name string) (Account, error) {
	if name == "" {
		switch len(all) {
		case 0:
			return Account{}, ipc.Errorf(ipc.CodeNotFound, "No chat account yet: `%s` adds one.", addWords)
		case 1:
			return all[0], nil
		}
		var names []string
		for _, a := range all {
			names = append(names, a.Name)
		}
		return Account{}, ipc.Errorf(ipc.CodeBadArgs, "Which account? There are %s.", strings.Join(names, ", "))
	}
	for _, a := range all {
		if a.Name == name {
			return a, nil
		}
	}
	return Account{}, ipc.Errorf(ipc.CodeNotFound, "No chat account is called %s.", name)
}

// addWords are the words that add an account.
var addWords = brand.Slug + " chat add discord|slack"

// Wait is how long one call to a service may take, its waits on the
// service's rate limits included.
const Wait = 30 * time.Second

// maxWait is the longest a call waits out a service's rate limit before it
// gives up and says so.
const maxWait = time.Minute

// ErrNoToken is a store's answer for an account it keeps no token for.
var ErrNoToken = errors.New("no token kept")

// Refused is a service's refusal of a call, in words a person reads, with
// what fixes it.
type Refused struct {
	Status int    // the HTTP status, or 0
	Code   string // the service's own code, such as 50001 or not_in_channel
	Msg    string
}

func (e *Refused) Error() string { return e.Msg }

// Auth says whether err is the service refusing the token itself.
func Auth(err error) bool {
	var r *Refused
	return errors.As(err, &r) && r.Status == http.StatusUnauthorized
}
