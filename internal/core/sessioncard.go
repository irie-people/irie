package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/genui"
	"github.com/irie-people/irie/internal/ipc"
)

// A session's own card, where it stands, in its details
// (docs/proposals/materialized-cards.md §4): a materialized card the core
// makes from the session's row and the newest items of its conversation.

const (
	sessionCardPrompt = "Where does this session stand? Say what it is doing or did, what it changed, and what waits on the person, if anything."
	sessionCardItems  = 80              // the newest items of its conversation it reads
	sessionCardGap    = 2 * time.Minute // the least time between two of its cards
)

// sessionCardArgs fills in an ask for session a.Session's own card: its id,
// its prompt, and its context, from the session's row and conversation.
// Its inputs are the session's state and its newest message, so the card
// is made again when a turn ends, a message comes or the session waits on
// you, never for each tool call, and at most every sessionCardGap.
func (c *Core) sessionCardArgs(a ipc.MaterializeArgs) (ipc.MaterializeArgs, error) {
	if !c.cfg().Materialize.Sessions {
		return a, ipc.Errorf(ipc.CodeBadArgs, "materialize: session cards are off; materialize.sessions turns them on")
	}
	conv, err := c.conversation(ipc.ConversationArgs{Session: a.Session, Limit: sessionCardItems})
	if err != nil {
		return a, err
	}
	c.mu.Lock()
	var row ipc.Session
	x := c.sessions[a.Session]
	if x != nil {
		row = x.wire()
	}
	c.mu.Unlock()
	if x == nil {
		return a, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	newest := ""
	for _, it := range conv.Items {
		if it.Kind == ipc.ItemUser || it.Kind == ipc.ItemText {
			newest = it.ID
		}
	}
	if conv.Needs != nil {
		newest += "|needs"
	}
	a.ID = "session/" + a.Session + "/status"
	a.Prompt = sessionCardPrompt
	a.Context = sessionContext(row, conv)
	a.Inputs = row.State + "|" + newest
	a.Gap = sessionCardGap
	return a, nil
}

// sessionContext is what a session's card is made from: its name, its
// state and what it is doing, then its messages and steps, oldest first.
// When they run past what a model reads, the newest are kept.
func sessionContext(row ipc.Session, conv ipc.Conversation) string {
	return sessionContextIn(row, conv, genui.MaxContext)
}

// sessionContextIn is sessionContext in at most budget bytes, the newest
// kept.
func sessionContextIn(row ipc.Session, conv ipc.Conversation, budget int) string {
	var head strings.Builder
	fmt.Fprintf(&head, "Session: %s\nState: %s", row.Name, row.State)
	if row.Detail != "" {
		fmt.Fprintf(&head, ", %s", cleanText(row.Detail, 200))
	}
	if row.Branch != "" {
		fmt.Fprintf(&head, "\nBranch: %s", row.Branch)
	}
	head.WriteString("\n\nWhat happened, oldest first:\n")
	var lines []string
	for _, it := range conv.Items {
		if l := contextLine(it); l != "" {
			lines = append(lines, l)
		}
	}
	if conv.Needs != nil {
		lines = append(lines, "Waits on the person: "+cleanText(conv.Needs.Text, 400))
	}
	budget -= head.Len() + len(leftOut)
	start := len(lines)
	for start > 0 && budget >= len(lines[start-1])+1 {
		start--
		budget -= len(lines[start]) + 1
	}
	if start > 0 {
		head.WriteString(leftOut)
	}
	return head.String() + strings.Join(lines[start:], "\n")
}

// leftOut stands for the steps a session's context had no room for.
const leftOut = "[older steps left out]\n"

// contextLine is one item of a conversation as a line of a card's
// context, "" for an item a card has no use for.
func contextLine(it ipc.Item) string {
	switch it.Kind {
	case ipc.ItemUser:
		return "Person: " + cleanText(it.Text, 600)
	case ipc.ItemText:
		return "Agent: " + cleanText(it.Text, 900)
	case ipc.ItemTool:
		l := "Step: " + cleanText(it.Phrase, 120)
		if it.Detail != "" {
			l += " — " + cleanText(it.Detail, 160)
		}
		if it.Added+it.Removed > 0 {
			l += fmt.Sprintf(" (+%d −%d)", it.Added, it.Removed)
		}
		if it.State == ipc.ItemError {
			l += " (failed)"
		}
		return l
	case ipc.ItemAgent:
		return "Helper: " + cleanText(it.Text, 200)
	case ipc.ItemNote:
		return "Note: " + cleanText(it.Text, 200)
	}
	return ""
}
