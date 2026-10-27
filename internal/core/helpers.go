package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/irie-hq/irie/internal/ipc"
)

// A session's helpers are its subagents (DESIGN §4.4). A helper has no
// terminal of its own: it runs inside its session's agent, and all there is
// to see of it is its hook events and its own transcript. A view that
// shows a helper beside its session follows the helper's conversation
// (conversation and subscribe with agent "sub:<id>"), read from that
// transcript by the same reader as the session's, its sidechain lines
// kept (docs/proposals/split-pane.md).

// helperAgent is a helper's agent as the wire names it. The id becomes part
// of a file name, so it takes no path separators.
var helperAgent = regexp.MustCompile(`^sub:[A-Za-z0-9_-]{1,128}$`)

// isHelper reports whether agent names a helper rather than the session's
// own agent.
func isHelper(agent string) bool { return strings.HasPrefix(agent, "sub:") }

// helperFeed is one helper's conversation, read while clients follow it.
type helperFeed struct {
	conv *conversation
	subs int // the subscriptions that follow it
}

// helperTranscript is where session x's helper agent keeps its transcript:
// the path its SubagentStop gave, or else agent-<id>.jsonl in the
// subagents folder beside one of the session's transcripts, the newest
// first, or beside the newest while none exists yet. The session's
// transcripts are those its SessionStart hooks named, or else the one
// Claude Code keeps for it. "" while it has none. The caller holds c.mu.
func (c *Core) helperTranscript(x *session, agent string) string {
	name := "agent-" + strings.TrimPrefix(agent, "sub:") + ".jsonl"
	var mains []string
	for _, p := range x.track.transcripts {
		switch {
		case filepath.Base(p) == name && filepath.Base(filepath.Dir(p)) == "subagents":
			return p
		case filepath.Base(filepath.Dir(p)) != "subagents":
			mains = append(mains, p)
		}
	}
	if len(mains) == 0 {
		mains = c.transcriptsOf(x)
	}
	for i := len(mains) - 1; i >= 0; i-- {
		p := filepath.Join(strings.TrimSuffix(mains[i], ".jsonl"), "subagents", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(mains) > 0 {
		return filepath.Join(strings.TrimSuffix(mains[len(mains)-1], ".jsonl"), "subagents", name)
	}
	return ""
}

// readHelper catches cv up with the transcript of session x's helper agent.
// Once the helper has stopped, a call it left without a result has
// stopped too. It reports whether cv changed. The caller holds c.mu.
func (c *Core) readHelper(x *session, agent string, cv *conversation) bool {
	var paths []string
	if p := c.helperTranscript(x, agent); p != "" {
		if _, err := os.Stat(p); err == nil {
			paths = []string{p}
		}
	}
	_, running := x.track.subs[agent]
	return cv.update(paths, running)
}

// followHelper starts or joins following session x's helper agent. The
// caller holds c.mu.
func (c *Core) followHelper(x *session, agent string) *helperFeed {
	h := x.helpers[agent]
	if h == nil {
		h = &helperFeed{conv: newHelperConversation()}
		if x.helpers == nil {
			x.helpers = map[string]*helperFeed{}
		}
		x.helpers[agent] = h
		c.readHelper(x, agent, h.conv)
	}
	h.subs++
	return h
}

// unfollowHelper leaves following session x's helper agent, and forgets
// its conversation once nobody follows it. The caller holds c.mu.
func (c *Core) unfollowHelper(x *session, agent string) {
	if h := x.helpers[agent]; h != nil {
		if h.subs--; h.subs <= 0 {
			delete(x.helpers, agent)
		}
	}
}

// followHelpers catches up the helper conversations that clients follow in
// session x, and tells those clients. The caller holds c.mu.
func (c *Core) followHelpers(x *session) {
	for agent, h := range x.helpers {
		if c.readHelper(x, agent, h.conv) {
			for s := range c.subs {
				if s.conversation && s.session == x.id && s.agent == agent {
					s.noteConversation()
				}
			}
		}
	}
}

// helperConversation answers the conversation op for a helper. The caller
// holds c.mu.
func (c *Core) helperConversation(x *session, agent string, limit int) ipc.Conversation {
	cv := newHelperConversation()
	if h := x.helpers[agent]; h != nil {
		cv = h.conv
	}
	c.readHelper(x, agent, cv)
	out := cv.all(limit)
	out.Session, out.Agent = x.id, agent
	return out
}

// linkAgents ties each running subagent item that names no helper yet to a
// running helper of its kind that no other item names, the oldest first.
// Such an item is a subagent in the foreground, whose result names its
// helper only once it ends; one in the background names it at once. It
// reports whether it tied any.
func (cv *conversation) linkAgents(running []ipc.Subagent) bool {
	if len(running) == 0 {
		return false
	}
	taken := map[string]bool{}
	for _, it := range cv.items {
		if it.Kind == ipc.ItemAgent && it.Agent != "" {
			taken[it.Agent] = true
		}
	}
	changed := false
	for i, it := range cv.items {
		if it.Kind != ipc.ItemAgent || it.Agent != "" || it.State != ipc.ItemRunning {
			continue
		}
		for _, sa := range running {
			if !taken[sa.Agent] && sa.Type == it.AgentType {
				it.Agent, taken[sa.Agent] = sa.Agent, true
				cv.set(i, it)
				changed = true
				break
			}
		}
	}
	return changed
}
