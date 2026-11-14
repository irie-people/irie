package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
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
	return c.typeTo(a.Session, text, sendable, nil)
}

// typeTo types text to session id's agent as send does, once may, called
// with c.mu held just before the paste, says the session can take it.
// With shows, Enter waits until shows says the agent shows the text where
// Enter sends it, and is not pressed when it says otherwise, so Enter
// never answers something that came up meanwhile; the message is then no
// longer pending. The trod bot types its messages this way too (trod.go).
func (c *Core) typeTo(id, text string, may func(*session, string) error, shows func(name, pane, text string) error) error {
	c.sendMu.LockAt(c.levelOf(id))
	defer c.sendMu.Unlock()
	c.mu.Lock()
	x := c.sessions[id]
	if x == nil {
		c.mu.Unlock()
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", id)
	}
	if err := may(x, text); err != nil {
		c.mu.Unlock()
		return err
	}
	pane, name, working := x.pane, x.name, x.wire().State == ipc.StateWorking
	// tmux pastes from a buffer; the message goes in by a file, as a task
	// does, so no shell or tmux parser ever reads it. paste-buffer shows
	// control characters other than tabs and newlines as ^X, so a message
	// cannot steer the terminal.
	file := filepath.Join(c.paths.Session(x.id), "message.txt")
	err := os.WriteFile(file, []byte(text), 0o600)
	if err == nil {
		buf := brand.Slug + "-message-" + x.id
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
	pokeScreen(x)
	cv, _ := c.conversationOf(x)
	at := time.Now()
	cv.sent(text, at, working)
	c.tellConversation(x)
	c.mu.Unlock()

	time.Sleep(enterDelay)
	if shows != nil {
		if err := shows(name, pane, text); err != nil {
			c.mu.Lock()
			if x := c.sessions[id]; x != nil {
				if cv, _ := c.conversationOf(x); cv.unsent(at) {
					c.tellConversation(x)
				}
			}
			c.mu.Unlock()
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if x := c.sessions[id]; x == nil || x.pane != pane || x.dead {
		return ipc.Errorf(ipc.CodeBusy, "the session ended before its message was sent")
	}
	_, err = c.tmux.Run("send-keys", "-t", pane, "Enter")
	return err
}

// continueWord is what continue types: the word that sends an agent on
// from where it stopped.
const continueWord = "continue"

// promptWait is how long continue waits after the paste for the agent's
// prompt to show the word before it gives up on Enter, and promptPoll how
// often it looks meanwhile: an agent on a busy machine draws late.
const (
	promptWait = 2 * time.Second
	promptPoll = 50 * time.Millisecond
)

// continueAgent sends a session's agent on (ipc.OpContinue): it types
// "continue" and Enter as send does, but only into the agent's own prompt
// with nothing typed in it, so the word never joins a message you are
// writing in its terminal. A prompt that shows is where Claude Code waits
// for a message; a permission's, a question's or a plan's menu stands in
// for it. So a turn that failed, on an API error, takes the word too,
// though send refuses while a session needs you. Enter waits until the
// prompt shows the word.
func (c *Core) continueAgent(a ipc.ContinueArgs) error {
	return c.typeTo(a.Session, continueWord, c.continuable, c.promptHolds)
}

// continuable says why session x's agent cannot take the continue word
// now, if it cannot: as sendable says, except that a turn that failed
// waits at the agent's prompt, and the prompt must show with nothing typed
// in it. The caller holds c.mu.
func (c *Core) continuable(x *session, text string) error {
	if !failedTurn(x) {
		if err := sendable(x, text); err != nil {
			return err
		}
	}
	typed, shows, err := c.promptIn(x.pane)
	switch {
	case errors.Is(err, errScrolled):
		return ipc.Errorf(ipc.CodeBusy, "%s's terminal is scrolled back: scroll to its end first", x.name)
	case err != nil:
		return err
	case !shows:
		return ipc.Errorf(ipc.CodeBusy, "%s shows no prompt to type to: look at its terminal", x.name)
	case typed != "":
		return ipc.Errorf(ipc.CodeBusy, "%s's prompt has something typed in it: send that or clear it first", x.name)
	}
	return nil
}

// failedTurn says whether all session x waits on you for is a word after
// its turn failed, as on an API error: its agent then waits at its own
// prompt, not at a question. The caller holds c.mu.
func failedTurn(x *session) bool {
	n, ok := x.track.needs[event.Main]
	return ok && len(x.track.needs) == 1 && n.Kind == event.TurnFailed && x.wire().State == ipc.StateNeedsYou
}

// promptHolds waits, up to promptWait, for the agent's prompt in pane to
// hold text, as it does once the agent has drawn the paste, and says why
// Enter must not go when it does not: the prompt went, or holds something
// else, as when a permission came up meanwhile.
func (c *Core) promptHolds(name, pane, text string) error {
	until := time.Now().Add(promptWait)
	for {
		typed, shows, err := c.promptIn(pane)
		if err == nil && shows && typed == text {
			return nil
		}
		// Still drawing the paste, nothing yet or the text's start, or a
		// tmux too busy to answer: look again.
		again := err == nil && shows && strings.HasPrefix(text, typed) || err != nil && !errors.Is(err, errScrolled)
		if !again || time.Now().After(until) {
			return ipc.Errorf(ipc.CodeBusy, "%s's terminal changed before Enter, so “%s” was not sent: look at its terminal", name, text)
		}
		time.Sleep(promptPoll)
	}
}

// errScrolled is promptIn's answer for a terminal scrolled back in tmux's
// copy mode, where a key goes to the mode rather than to the agent.
var errScrolled = errors.New("the terminal is scrolled back")

// promptIn reads what the agent's prompt in pane holds now, and whether it
// shows (agentPrompt).
func (c *Core) promptIn(pane string) (typed string, shows bool, err error) {
	out, err := c.tmux.RunList([][]string{
		{"display-message", "-p", "-t", pane, "#{pane_in_mode}"},
		{"capture-pane", "-p", "-t", pane},
	})
	if err != nil {
		return "", false, err
	}
	mode, screen, _ := strings.Cut(out, "\n")
	if strings.TrimSpace(mode) != "0" {
		return "", false, errScrolled
	}
	typed, shows = agentPrompt(strings.Split(screen, "\n"))
	return typed, shows, nil
}

// sendable says why session x cannot take a message now, if it cannot.
// The caller holds c.mu.
func sendable(x *session, text string) error {
	switch w := x.wire(); w.State {
	case ipc.StateNeedsYou:
		// Your turn in teach mode waits at the agent's prompt, for your
		// word that the part is written.
		if w.Reason == event.ReasonTurn {
			break
		}
		return ipc.Errorf(ipc.CodeBusy, "%s is waiting for you in its terminal: answer it there", x.name)
	case ipc.StateStarting, ipc.StateStalled:
		return ipc.Errorf(ipc.CodeBusy, "%s hasn’t started yet", x.name)
	case ipc.StateExited, ipc.StateFailed:
		return ipc.Errorf(ipc.CodeBusy, "%s has ended", x.name)
	case ipc.StateFrozen:
		return ipc.Errorf(ipc.CodeBusy, "%s is paused: resume it first", x.name)
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
	cv.pending = append(cv.pending, ipc.Item{ID: sentID(at), Kind: ipc.ItemUser, Time: at,
		Text: capText(text, maxItemText), Queued: queued, Pending: true, From: trodFrom(text)})
	cv.bumpMeta()
}

// unsent drops the message sent at at, which Enter never sent, and says
// whether it was pending.
func (cv *conversation) unsent(at time.Time) bool {
	n := len(cv.pending)
	cv.pending = slices.DeleteFunc(cv.pending, func(it ipc.Item) bool { return it.ID == sentID(at) })
	if len(cv.pending) == n {
		return false
	}
	cv.bumpMeta()
	return true
}

// sentID is the id of the message sent at at.
func sentID(at time.Time) string { return "s:" + at.UTC().Format(time.RFC3339Nano) }

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
		if s.conversation && s.session == x.id && !isHelper(s.agent) {
			s.noteConversation()
		}
	}
}
