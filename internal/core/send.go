package core

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-hq/irie/internal/ipc"
)

// enterDelay is how long send waits between pasting a message and pressing
// Enter, so that the agent has taken the whole paste when Enter submits it.
const enterDelay = 150 * time.Millisecond

// send types a message to a session's agent the way a person would: a
// bracketed paste of the text into the agent's pane, which only the core
// writes to, then Enter. An agent at work takes it at its next tool
// boundary, since Claude Code queues what is typed while it works. While
// the agent waits on you in its terminal, Enter would answer that prompt,
// so send refuses, and the views offer the terminal instead (DESIGN §4.7).
// The message shows in the conversation as pending until the transcript
// has it.
func (c *Core) send(a ipc.SendArgs) error {
	text := strings.TrimRight(strings.TrimLeft(a.Text, "\r\n"), " \t\r\n")
	if strings.TrimSpace(text) == "" {
		return ipc.Errorf(ipc.CodeBadArgs, "there is nothing to send")
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	if err := sendable(x, text); err != nil {
		c.mu.Unlock()
		return err
	}
	pane, working := x.pane, x.wire().State == ipc.StateWorking
	// tmux pastes from a buffer; the message goes in by a file, as a task
	// does, so no shell or tmux parser ever reads it. paste-buffer shows
	// control characters other than tabs and newlines as ^X, so a message
	// cannot steer the terminal.
	file := filepath.Join(c.paths.Session(x.id), "message.txt")
	err := os.WriteFile(file, []byte(text), 0o600)
	if err == nil {
		buf := "irie-message-" + x.id
		_, err = c.tmux.RunList([][]string{
			{"load-buffer", "-b", buf, file},
			{"paste-buffer", "-p", "-d", "-b", buf, "-t", pane},
		})
		os.Remove(file)
	}
	if err != nil {
		c.mu.Unlock()
		return err
	}
	x.quiet++
	cv, _ := c.conversationOf(x)
	cv.sent(text, time.Now(), working)
	c.tellConversation(x)
	c.mu.Unlock()

	time.Sleep(enterDelay)
	c.mu.Lock()
	defer c.mu.Unlock()
	if x := c.sessions[a.Session]; x == nil || x.pane != pane || x.dead {
		return ipc.Errorf(ipc.CodeBusy, "the session ended before its message was sent")
	}
	_, err = c.tmux.Run("send-keys", "-t", pane, "Enter")
	return err
}

// sendable says why session x cannot take a message now, if it cannot.
// The caller holds c.mu.
func sendable(x *session, text string) error {
	switch x.wire().State {
	case ipc.StateNeedsYou:
		return ipc.Errorf(ipc.CodeBusy, "%s is waiting for you in its terminal: answer it there", x.name)
	case ipc.StateStarting, ipc.StateStalled:
		return ipc.Errorf(ipc.CodeBusy, "%s hasn’t started yet", x.name)
	case ipc.StateExited, ipc.StateFailed:
		return ipc.Errorf(ipc.CodeBusy, "%s has ended", x.name)
	}
	if x.dead {
		return ipc.Errorf(ipc.CodeBusy, "%s has ended", x.name)
	}
	// Without bracketed paste every newline would be Enter, and each line
	// would go as a message of its own.
	if strings.ContainsAny(text, "\r\n") && !x.bracket {
		return ipc.Errorf(ipc.CodeBusy, "%s can’t take a message of several lines right now: send one line, or type it in its terminal", x.name)
	}
	return nil
}

// sent adds a message sent to the agent, pending until the transcript has
// it.
func (cv *conversation) sent(text string, at time.Time, queued bool) {
	cv.pending = append(cv.pending, ipc.Item{ID: "s:" + at.UTC().Format(time.RFC3339Nano), Kind: ipc.ItemUser, Time: at,
		Text: capText(text, maxItemText), Queued: queued, Pending: true})
	cv.bumpMeta()
}

// taken drops the oldest pending message once the transcript has a message
// from you sent no earlier than it: the agent took it.
func (cv *conversation) taken(at time.Time) {
	if len(cv.pending) > 0 && !at.IsZero() && !at.Before(cv.pending[0].Time.Add(-2*time.Second)) {
		cv.pending = cv.pending[1:]
		cv.bumpMeta()
	}
}

// pendingFor is how long a sent message may wait to show in the transcript
// before the conversation stops showing it as pending.
const pendingFor = 15 * time.Minute

// expire drops pending messages older than pendingFor.
func (cv *conversation) expire(now time.Time) {
	n := 0
	for n < len(cv.pending) && now.Sub(cv.pending[n].Time) > pendingFor {
		n++
	}
	if n > 0 {
		cv.pending = cv.pending[n:]
		cv.bumpMeta()
	}
}

// tellConversation wakes the clients that follow session x's conversation.
// The caller holds c.mu.
func (c *Core) tellConversation(x *session) {
	for s := range c.subs {
		if s.conversation && s.session == x.id {
			s.noteConversation()
		}
	}
}
