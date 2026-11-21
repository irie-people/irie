//go:build !nochat

package core

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chat"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// The chat plugin's part of the core (docs/proposals/chat.md): the chat
// op, which every client's Chat draws from. The rules of what agents may do
// are internal/chat's; the core says who asks. go build -tags nochat
// leaves this file out, and chat_off.go stands in.

const chatBuilt = true

// chatState is the chat service, made at the first call.
type chatState struct {
	once sync.Once
	sv   *chat.Service
}

// chatWait is how long one call may take: a look at an account's servers
// reads each of them, and a service's rate limit may make one wait.
const chatWait = 60 * time.Second

func (c *Core) chatService() *chat.Service {
	c.chat.once.Do(func() {
		dir := filepath.Join(c.paths.State, "chat")
		c.chat.sv = &chat.Service{
			Store:     chat.DefaultStore(filepath.Join(dir, "keys")),
			Dir:       dir,
			Config:    func() config.Chat { return c.cfg().Chat },
			Configure: func(sets []ipc.ConfigSet) error { return c.configure(c.paths.ConfigFile(), sets) },
		}
	})
	return c.chat.sv
}

// chatLive is how each account stands by the core's last look, for the
// plugin's parts: it asks no service.
func (c *Core) chatLive() []ipc.ChatAccount {
	return c.chatService().Accounts(chat.Accounts(c.cfg().Chat))
}

// chatOp answers the chat op. peer is the process that asks: a call from
// inside an agent's pane is the agent's, and held to what the person
// opened to agents, whatever the words in a message ask of it.
func (c *Core) chatOp(peer int, a ipc.ChatArgs) (ipc.ChatResult, error) {
	cfg := c.cfg()
	if !cfg.ChatOn() {
		return ipc.ChatResult{}, ipc.Errorf(ipc.CodeBadArgs, "Chat is off, as its plugin is: `%s plugins on chat` turns it on.", brand.Slug)
	}
	who := chat.Caller{}
	if id, ok := c.agentOf(peer); ok {
		who = chat.Caller{Agent: true, Session: id, Name: id}
		if id != "" {
			c.mu.Lock()
			who.Name = c.nameOf(id)
			c.mu.Unlock()
		} else {
			who.Session = "agent"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), chatWait)
	defer cancel()
	r, err := c.chatService().Do(ctx, who, a)
	if err != nil {
		var ie *ipc.Error
		if errors.As(err, &ie) {
			return ipc.ChatResult{}, err
		}
		return ipc.ChatResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", chat.Sentence(err))
	}
	return r, nil
}
