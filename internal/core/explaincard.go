package core

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/ipc"
)

// Cards that explain, the Teach tile's (docs/proposals/materialized-cards.md
// §4): what the person asks to have explained, made into a picture first
// by a local model, and kept, so a question asked again draws at once. A
// question about a session's work is answered from its conversation.

const (
	explainItems = 40              // the newest items of a session's conversation an explanation reads
	explainGap   = 2 * time.Minute // the least time between two cards for one question
	explainKept  = 60              // the most cards that explain the store keeps, the newest
	explainIDs   = "explain/"      // the start of every such card's id
)

// explainCardArgs fills in an ask for a card that explains a.Explain: its
// id, from the question and the session it is asked in, its prompt, and,
// with a.Session, its context, that session's conversation.
func (c *Core) explainCardArgs(a ipc.MaterializeArgs) (ipc.MaterializeArgs, error) {
	topic := explainText(a.Explain)
	if topic == "" {
		return a, ipc.Errorf(ipc.CodeBadArgs, "materialize: say what to explain")
	}
	a.Explain = topic
	a.ID = explainIDs + explainKey(topic)
	a.Prompt = "Explain this: " + topic
	a.Context, a.Inputs, a.Gap = "", "", explainGap
	if a.Session == "" {
		return a, nil
	}
	conv, err := c.conversation(ipc.ConversationArgs{Session: a.Session, Limit: explainItems})
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
	a.ID = explainIDs + a.Session + "/" + explainKey(topic)
	a.Context = sessionContext(row, conv)
	a.Inputs = explainInputs(conv)
	return a, nil
}

// explainInputs is the digest of what a card that explains reads from its
// session, the card's inputs (materialize.Recipe): the session's newest
// message, the person's or the agent's. When another comes, the card kept
// for the question is behind, and the next ask makes it again, no sooner
// than explainGap after the last, while the old one stays on show. Its
// steps alone never do: a tool call changes what the agent did, not what
// it said of it. A card is made again only when it is asked for, so this
// prices a question asked twice, never the session's work.
func explainInputs(conv ipc.Conversation) string {
	newest := ""
	for _, it := range conv.Items {
		if it.Kind == ipc.ItemUser || it.Kind == ipc.ItemText {
			newest = it.ID
		}
	}
	return newest
}

// explainText is what the person asked to have explained, as the card's
// prompt takes it: its lines kept, since it may be code, each without
// control characters and trailing blanks, and at most ipc.MaxExplain bytes.
func explainText(s string) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		l = strings.Map(func(r rune) rune {
			switch {
			case r == '\t':
				return ' '
			case unicode.IsControl(r):
				return -1
			}
			return r
		}, l)
		lines = append(lines, strings.TrimRightFunc(l, unicode.IsSpace))
	}
	t := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(t) > ipc.MaxExplain {
		n := ipc.MaxExplain
		for n > 0 && !utf8.RuneStart(t[n]) {
			n--
		}
		t = t[:n]
	}
	return t
}

// explainKey names a question's card by its words as asked. The words are
// the card's prompt too, so a key that took other words for the same
// question would have two recipes take turns at one card.
func explainKey(topic string) string {
	sum := sha256.Sum256([]byte(topic))
	return hex.EncodeToString(sum[:8])
}
