package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/genui"
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

// explainAbout is what a card that explains is about, for the library: its
// question, the project and the topic it was asked in, and what the person
// said to have it made from the one before ("" for a question's first).
type explainAbout struct {
	question, project, topic, asked string
}

// maxRefine bounds what the person says of a card, in bytes.
const maxRefine = 1 << 10

// explainCardArgs fills in an ask for a card that explains a.Explain: its
// id, from the question and the session it is asked in, its prompt, and,
// with a.Session, its context: what the session's project's docs say of
// it, then the session's conversation. With a.Refine it is a turn of the
// tile's chat: the newest card kept for the question made again, with what
// the person says of it done.
func (c *Core) explainCardArgs(a ipc.MaterializeArgs) (ipc.MaterializeArgs, explainAbout, error) {
	topic := explainText(a.Explain)
	if topic == "" {
		return a, explainAbout{}, ipc.Errorf(ipc.CodeBadArgs, "materialize: say what to explain")
	}
	ab := explainAbout{question: topic, topic: cleanText(a.Topic, 200)}
	a.Explain = topic
	a.ID = explainIDs + explainKey(topic)
	a.Prompt = "Explain this: " + topic
	a.Context, a.Inputs, a.Gap = "", "", explainGap
	var docs string
	var row ipc.Session
	var conv ipc.Conversation
	if a.Session != "" {
		var err error
		if conv, err = c.conversation(ipc.ConversationArgs{Session: a.Session, Limit: explainItems}); err != nil {
			return a, ab, err
		}
		c.mu.Lock()
		var dir, repo string
		x := c.sessions[a.Session]
		if x != nil {
			row, dir, repo = x.wire(), x.cwd, x.repo
		}
		c.mu.Unlock()
		if x == nil {
			return a, ab, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		a.ID = explainIDs + a.Session + "/" + explainKey(topic)
		ab.project = projectName(repo, dir)
		// What the project's own docs say of it first, then what was said
		// in the session, in the room the docs leave.
		if docs = docsFor(dir, topic, explainDocs); docs != "" {
			docs = docsHead + docs + "\n\n"
		}
		a.Inputs = explainInputs(conv)
	}
	var before string
	if a.Refine != "" {
		said := cutText(cleanText(a.Refine, maxRefine), maxRefine)
		base := c.newestExplained(ab, a.ID)
		if said == "" || base == nil {
			return a, ab, ipc.Errorf(ipc.CodeBadArgs, "materialize: there is no card for that question to change yet; ask it first")
		}
		// A card of its own for each thing said of each card, made once:
		// the chat's turns are versions, kept in the library in order.
		a.ID += "/r" + explainKey(said+"\x00"+string(base))
		a.Prompt = "Explain this: " + topic + "\n\nThe person is looking at the card made before, given last in the context, and says of it: " + said +
			"\nMake the whole card again with that done, and keep what they did not ask to change."
		before = "\n\nThe card made before:\n" + string(base)
		a.Inputs = ""
		ab.asked = said
	}
	if a.Session != "" {
		a.Context = docs + explainContext(row, conv, max(genui.MaxContext-len(docs)-len(before), 0))
	}
	a.Context += before
	return a, ab, nil
}

// projectName is a session's project as the library names it: its repo's
// name, else its folder's; "" for none.
func projectName(repo, dir string) string {
	switch {
	case repo != "":
		return filepath.Base(repo)
	case dir != "":
		return filepath.Base(dir)
	}
	return ""
}

// newestExplained is the newest card of ab's question: the newest version
// the library keeps, else the card the store keeps under id; nil for none.
func (c *Core) newestExplained(ab explainAbout, id string) json.RawMessage {
	if lib, ok := c.library(); ok {
		if e, err := lib.Load(ab.project, ab.question); err == nil && e != nil && len(e.Versions) > 0 {
			return compact(e.Latest().Card)
		}
	}
	return c.cardStore().Kept(id)
}

// docsHead heads what a project's docs say, in a card's context.
const docsHead = "What the project's own docs say, the surest guide to its words and to how it works:\n"

// explainContext is what a card that explains reads of its session: what
// the person and the agent said, oldest first, the newest kept when they
// run past what a model reads. Its steps are left out: a small model given
// them draws what the session did, not what was asked.
func explainContext(row ipc.Session, conv ipc.Conversation, budget int) string {
	said := conv
	said.Items, said.Needs = nil, nil
	for _, it := range conv.Items {
		if it.Kind == ipc.ItemUser || it.Kind == ipc.ItemText {
			said.Items = append(said.Items, it)
		}
	}
	row.Detail = ""
	return sessionContextIn(row, said, budget)
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
