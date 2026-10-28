package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/ipc"
)

// A session's conversation, read from its Claude Code transcripts as they
// grow: your messages, the agent's text, its tool calls in plain words
// (Phrase), its subagents, and what it waits on you for. The views that
// show a conversation instead of the agent's raw terminal read it through
// the conversation op and subscription (docs/MAC.md).
//
// A transcript's lines are Claude Code's own: observed, not published,
// and they change between versions. A line Irie does not understand is
// skipped.

// conversation is one session's conversation. Every change bumps ver and
// stamps what it changed, so each client gets only what is new to it.
type conversation struct {
	files []*transcriptFeed // the session's transcripts, oldest first
	items []ipc.Item
	index map[string]int   // item id → position in items
	vers  []uint64         // per item, the version that last changed it
	calls map[string]*call // tool calls by tool_use id
	ver   uint64           // the newest version
	reset uint64           // the version at which it last started over
	needs *ipc.Item        // what the session waits on you for
	meta  uint64           // the version that last changed needs, pending or the transcript
	path  string           // the newest transcript
	// pending are messages sent to the agent that the transcript does not
	// have yet (send.go).
	pending []ipc.Item
	// sidechain marks a helper's conversation, read from the helper's own
	// transcript, where every line is a sidechain line (helpers.go).
	sidechain bool
	tasked    bool // its first message, the helper's task, is in
}

type transcriptFeed struct {
	follower
	path string
}

// call is what a tool call needs once its result arrives, and what a need
// shows while it waits.
type call struct {
	done, failed   string   // the phrase once it has run, or failed
	added, removed int      // the lines an edit changes, from its input
	options        []string // a question's choices
	plan           string   // a plan to approve
}

// maxItemText caps a message or a reply, which a view shows in full.
const maxItemText = 20000

// resetLimit is how many of the newest items a subscription's first frame
// carries.
const resetLimit = 1000

func newConversation() *conversation {
	return &conversation{index: map[string]int{}, calls: map[string]*call{}}
}

// newHelperConversation is a helper's conversation (helpers.go).
func newHelperConversation() *conversation {
	cv := newConversation()
	cv.sidechain = true
	return cv
}

// update reads what the transcripts at paths, oldest first, gained.
// active says whether the agent may still be running a tool: once it is
// not, a call without a result has stopped. It reports whether anything
// changed.
func (cv *conversation) update(paths []string, active bool) bool {
	from := cv.ver
	same := len(paths) >= len(cv.files)
	for i, f := range cv.files {
		same = same && f.path == paths[i]
	}
	if !same {
		cv.clear()
	}
	for pass := 0; pass < 2; pass++ {
		replaced := false
		for i, p := range paths {
			if i == len(cv.files) {
				if i > 0 {
					cv.add(ipc.Item{ID: fmt.Sprintf("n:new:%d", i), Kind: ipc.ItemNote, Text: "A new conversation"})
				}
				cv.files = append(cv.files, &transcriptFeed{path: p})
			}
			cv.files[i].read(p, func() { replaced = true }, func(b []byte) {
				if !replaced {
					cv.line(b)
				}
			})
		}
		if !replaced {
			break
		}
		cv.clear() // a transcript was replaced: read them all again
	}
	cv.expire(time.Now())
	if !active {
		for i, it := range cv.items {
			if it.State == ipc.ItemRunning {
				it.State = ipc.ItemStopped
				cv.set(i, it)
			}
		}
	}
	if p := last(paths); p != cv.path {
		cv.path = p
		cv.bumpMeta()
	}
	return cv.ver != from
}

// clear starts over, with every transcript to be read from its start.
func (cv *conversation) clear() {
	cv.files, cv.items, cv.vers = nil, nil, nil
	cv.index, cv.calls = map[string]int{}, map[string]*call{}
	cv.tasked = false
	cv.ver++
	cv.reset = cv.ver
}

func (cv *conversation) add(it ipc.Item) {
	if i, ok := cv.index[it.ID]; ok {
		cv.set(i, it)
		return
	}
	cv.ver++
	cv.index[it.ID] = len(cv.items)
	cv.items = append(cv.items, it)
	cv.vers = append(cv.vers, cv.ver)
}

func (cv *conversation) set(i int, it ipc.Item) {
	cv.ver++
	cv.items[i], cv.vers[i] = it, cv.ver
}

func (cv *conversation) bumpMeta() {
	cv.ver++
	cv.meta = cv.ver
}

// setNeeds records what the session waits on you for, or nil.
func (cv *conversation) setNeeds(n *ipc.Item) bool {
	if n == nil && cv.needs == nil || n != nil && cv.needs != nil && sameItem(*n, *cv.needs) {
		return false
	}
	cv.needs = n
	cv.bumpMeta()
	return true
}

func sameItem(a, b ipc.Item) bool {
	return a.ID == b.ID && a.Text == b.Text && a.Detail == b.Detail && a.Reason == b.Reason &&
		slices.Equal(a.Options, b.Options) && a.Time.Equal(b.Time)
}

// all is the whole conversation, or its newest limit items.
func (cv *conversation) all(limit int) ipc.Conversation {
	out := ipc.Conversation{Transcript: cv.path, Reset: true, Needs: cv.needs, Items: cv.items, Pending: slices.Clone(cv.pending)}
	if limit > 0 && len(out.Items) > limit {
		out.Items, out.More = out.Items[len(out.Items)-limit:], true
	}
	out.Items = append([]ipc.Item{}, out.Items...)
	return out
}

// since is what changed after version seen, and whether anything did. A
// client that saw a version from before the last start over gets it all.
func (cv *conversation) since(seen uint64) (ipc.Conversation, bool) {
	if seen < cv.reset {
		return cv.all(resetLimit), true
	}
	out := ipc.Conversation{Transcript: cv.path, Needs: cv.needs, Pending: slices.Clone(cv.pending)}
	for i, v := range cv.vers {
		if v > seen {
			out.Items = append(out.Items, cv.items[i])
		}
	}
	return out, len(out.Items) > 0 || cv.meta > seen
}

// open is the newest call of tool that has not ended.
func (cv *conversation) open(tool string) *call {
	for i := len(cv.items) - 1; i >= 0; i-- {
		if it := cv.items[i]; it.Tool == tool && it.State == ipc.ItemRunning {
			return cv.calls[strings.TrimPrefix(it.ID, "t:")]
		}
	}
	return nil
}

// line reads one transcript line.
func (cv *conversation) line(b []byte) {
	var rec struct {
		Type             string `json:"type"`
		UUID             string `json:"uuid"`
		Timestamp        string `json:"timestamp"`
		IsMeta           bool   `json:"isMeta"`
		IsSidechain      bool   `json:"isSidechain"`
		IsCompactSummary bool   `json:"isCompactSummary"`
		Subtype          string `json:"subtype"`
		Message          struct {
			ID      string          `json:"id"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		ToolUseResult json.RawMessage `json:"toolUseResult"`
		Attachment    struct {
			Type   string          `json:"type"`
			Prompt json.RawMessage `json:"prompt"`
			Origin struct {
				Kind string `json:"kind"`
			} `json:"origin"`
		} `json:"attachment"`
	}
	if json.Unmarshal(b, &rec) != nil || rec.IsSidechain && !cv.sidechain {
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, rec.Timestamp)
	id := or(rec.UUID, rec.Message.ID, rec.Timestamp)
	switch rec.Type {
	case "user":
		if !rec.IsMeta && !rec.IsCompactSummary {
			cv.user(id, at, rec.Message.Content, rec.ToolUseResult)
		}
	case "assistant":
		cv.assistant(id, at, rec.Message.Content)
	case "attachment":
		// A message typed while the agent worked reaches it at a tool
		// boundary as a queued command. One from another agent is not
		// yours.
		if rec.Attachment.Type == "queued_command" && rec.Attachment.Origin.Kind != "peer" {
			if text := contentText(rec.Attachment.Prompt); text != "" {
				cv.said(id, at, text, true)
			}
		}
	case "system":
		if rec.Subtype == "compact_boundary" {
			cv.add(ipc.Item{ID: "n:" + id, Kind: ipc.ItemNote, Time: at, Text: "The conversation was compacted"})
		}
	}
}

// block is one content block of a message.
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

func (cv *conversation) user(id string, at time.Time, content, result json.RawMessage) {
	var text string
	if json.Unmarshal(content, &text) == nil {
		cv.said(id, at, text, false)
		return
	}
	var blocks []block
	if json.Unmarshal(content, &blocks) != nil {
		return
	}
	var texts []string
	for _, bl := range blocks {
		switch bl.Type {
		case "text":
			texts = append(texts, bl.Text)
		case "tool_result":
			cv.result(bl.ToolUseID, at, bl.IsError, bl.Content, result)
		}
	}
	if len(texts) > 0 {
		cv.said(id, at, strings.Join(texts, "\n\n"), false)
	}
}

var systemReminder = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// tags are the tags Claude Code wraps your commands in.
var tags = map[string]*regexp.Regexp{}

func init() {
	for _, t := range []string{"command-name", "command-args", "bash-input", "user-memory-input"} {
		tags[t] = regexp.MustCompile(`(?s)<` + t + `>(.*?)</` + t + `>`)
	}
}

// tagText is the text inside the first <tag>…</tag> of s.
func tagText(s, tag string) string {
	if m := tags[tag].FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// said adds what you said, as the transcript holds it. Claude Code wraps
// slash commands, shell commands and its own notes in tags, which say what
// to show; reminders it adds for the model are left out.
func (cv *conversation) said(id string, at time.Time, text string, queued bool) {
	text = strings.TrimSpace(systemReminder.ReplaceAllString(text, ""))
	switch {
	case text == "":
		return
	case strings.HasPrefix(text, "[Request interrupted by user"):
		cv.add(ipc.Item{ID: "n:" + id, Kind: ipc.ItemNote, Time: at, Text: "Interrupted"})
		return
	case strings.Contains(text, "<command-name>"):
		text = strings.TrimSpace(tagText(text, "command-name") + " " + tagText(text, "command-args"))
	case strings.Contains(text, "<bash-input>"):
		text = "! " + tagText(text, "bash-input")
	case strings.Contains(text, "<user-memory-input>"):
		text = "# " + tagText(text, "user-memory-input")
	case strings.HasPrefix(text, "<local-command-"), strings.HasPrefix(text, "<bash-stdout>"),
		strings.HasPrefix(text, "<bash-stderr>"), strings.HasPrefix(text, "<task-notification>"),
		strings.HasPrefix(text, "Caveat: The messages below were generated by the user"):
		return
	}
	if text != "" {
		it := ipc.Item{ID: "u:" + id, Kind: ipc.ItemUser, Time: at, Text: capText(text, maxItemText), Queued: queued}
		if cv.sidechain && !cv.tasked {
			// A helper's first message is the task its session gave it.
			it.From, cv.tasked = "parent", true
		}
		cv.add(it)
		cv.taken(at)
	}
}

func (cv *conversation) assistant(id string, at time.Time, content json.RawMessage) {
	var blocks []block
	if json.Unmarshal(content, &blocks) != nil {
		return
	}
	for i, bl := range blocks {
		switch bl.Type {
		case "text":
			if t := strings.TrimSpace(bl.Text); t != "" && t != "(no content)" {
				cv.add(ipc.Item{ID: fmt.Sprintf("a:%s:%d", id, i), Kind: ipc.ItemText, Time: at, Text: capText(t, maxItemText)})
			}
		case "tool_use":
			cv.toolUse(bl.ID, bl.Name, bl.Input, at)
		}
	}
}

func (cv *conversation) toolUse(id, name string, input json.RawMessage, at time.Time) {
	if id == "" {
		return
	}
	var in toolInput
	json.Unmarshal(input, &in)
	w := wordsFor(name, input)
	it := ipc.Item{ID: "t:" + id, Kind: ipc.ItemTool, Time: at, Tool: name, State: ipc.ItemRunning,
		Phrase: w.doing(), Detail: toolDetail(name, in, input), Group: toolGroup(name, w)}
	c := &call{done: w.done(), failed: w.failed()}
	switch name {
	case "Agent", "Task":
		it.Kind, it.AgentType, it.Text = ipc.ItemAgent, or(in.str("subagent_type"), "general-purpose"), in.str("description")
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		c.added, c.removed = editLines(name, in)
	case "AskUserQuestion":
		c.options = questionOptions(in)
	case "ExitPlanMode":
		c.plan = capText(in.str("plan"), maxItemText)
	}
	cv.calls[id] = c
	cv.add(it)
}

// result ends a tool call: ok or failed, how long it took, and what the
// structured result adds, such as the lines an edit changed.
func (cv *conversation) result(id string, at time.Time, failed bool, content, structured json.RawMessage) {
	i, ok := cv.index["t:"+id]
	if !ok {
		return
	}
	it := cv.items[i]
	it.State = ipc.ItemOK
	if failed {
		it.State, it.Output = ipc.ItemError, capText(contentText(content), 600)
	}
	if !it.Time.IsZero() && at.After(it.Time) {
		it.Ms = at.Sub(it.Time).Milliseconds()
	}
	if c := cv.calls[id]; c != nil {
		it.Phrase, it.Added, it.Removed = c.done, c.added, c.removed
		if failed {
			it.Phrase, it.Added, it.Removed = c.failed, 0, 0
		}
	}
	var out struct {
		StructuredPatch []struct {
			Lines []string `json:"lines"`
		} `json:"structuredPatch"`
		TotalDurationMs   int64  `json:"totalDurationMs"`
		TotalToolUseCount int    `json:"totalToolUseCount"`
		Status            string `json:"status"`
		AgentID           string `json:"agentId"`
	}
	if json.Unmarshal(structured, &out) == nil {
		if len(out.StructuredPatch) > 0 {
			it.Added, it.Removed = 0, 0
			for _, h := range out.StructuredPatch {
				for _, l := range h.Lines {
					switch {
					case strings.HasPrefix(l, "+"):
						it.Added++
					case strings.HasPrefix(l, "-"):
						it.Removed++
					}
				}
			}
		}
		if out.TotalDurationMs > 0 {
			it.Ms = out.TotalDurationMs
		}
		it.Tools = out.TotalToolUseCount
		if out.Status == "async_launched" {
			it.Phrase += " (in the background)"
		}
		// A subagent's result names its helper, at once for one that runs
		// in the background.
		if it.Kind == ipc.ItemAgent && out.AgentID != "" && helperAgent.MatchString("sub:"+out.AgentID) {
			it.Agent = "sub:" + out.AgentID
		}
	}
	cv.set(i, it)
}

// contentText is the text of message content: a string, or the text of
// its blocks.
func contentText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []block
	json.Unmarshal(content, &blocks)
	var texts []string
	for _, bl := range blocks {
		if bl.Type == "text" && strings.TrimSpace(bl.Text) != "" {
			texts = append(texts, strings.TrimSpace(bl.Text))
		}
	}
	return strings.Join(texts, "\n")
}

// toolDetail is what a tool call works on, exactly: the command, path,
// pattern or URL, a subagent's task, or a plan.
func toolDetail(name string, in toolInput, raw json.RawMessage) string {
	switch name {
	case "Bash", "PowerShell":
		return capText(in.str("command"), 4000)
	case "Read", "Write", "Edit", "MultiEdit":
		return in.str("file_path")
	case "NotebookEdit", "NotebookRead":
		return in.str("notebook_path")
	case "Grep", "Glob":
		if where := or(in.str("path"), in.str("glob")); where != "" {
			return in.str("pattern") + "  in " + where
		}
		return in.str("pattern")
	case "WebFetch":
		return in.str("url")
	case "WebSearch":
		return in.str("query")
	case "Skill":
		return strings.TrimSpace(in.str("skill") + " " + in.str("args"))
	case "Agent", "Task":
		return capText(in.str("prompt"), 4000)
	case "ExitPlanMode":
		return capText(in.str("plan"), 4000)
	case "AskUserQuestion":
		var q []struct {
			Question string `json:"question"`
		}
		json.Unmarshal(in["questions"], &q)
		var qs []string
		for _, x := range q {
			qs = append(qs, x.Question)
		}
		return strings.Join(qs, "\n")
	case "TodoWrite":
		var todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		}
		json.Unmarshal(in["todos"], &todos)
		var lines []string
		for _, t := range todos {
			mark := "☐"
			switch t.Status {
			case "completed":
				mark = "☑"
			case "in_progress":
				mark = "▸"
			}
			lines = append(lines, mark+" "+t.Content)
		}
		return strings.Join(lines, "\n")
	}
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return ""
	}
	return capText(string(raw), 1000)
}

// toolGroup gathers reads, edits and searches, which a view may show as
// one line ("Read 4 files").
func toolGroup(name string, w words) string {
	switch name {
	case "Read", "NotebookRead":
		return "read"
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return "edit"
	case "Grep", "Glob":
		return "search"
	case "Bash":
		switch w.verb {
		case "read":
			return "read"
		case "edit":
			return "edit"
		case "search for", "search", "look for":
			return "search"
		}
	}
	return ""
}

// editLines counts the lines an edit adds and removes, from its input.
func editLines(name string, in toolInput) (added, removed int) {
	switch name {
	case "Edit":
		return lineDiff(in.str("old_string"), in.str("new_string"))
	case "MultiEdit":
		var edits []struct {
			Old string `json:"old_string"`
			New string `json:"new_string"`
		}
		json.Unmarshal(in["edits"], &edits)
		for _, e := range edits {
			a, r := lineDiff(e.Old, e.New)
			added, removed = added+a, removed+r
		}
		return added, removed
	case "Write":
		return countLines(in.str("content")), 0
	case "NotebookEdit":
		return countLines(in.str("new_source")), 0
	}
	return 0, 0
}

// lineDiff counts the lines that turn a into b: those outside their
// longest common run of lines.
func lineDiff(a, b string) (added, removed int) {
	x, y := splitLines(a), splitLines(b)
	for len(x) > 0 && len(y) > 0 && x[0] == y[0] {
		x, y = x[1:], y[1:]
	}
	for len(x) > 0 && len(y) > 0 && x[len(x)-1] == y[len(y)-1] {
		x, y = x[:len(x)-1], y[:len(y)-1]
	}
	common := 0
	if len(x)*len(y) <= 250000 {
		// The longest common subsequence, a row at a time.
		row := make([]int, len(y)+1)
		for i := range x {
			prev := 0
			for j := range y {
				cur := row[j+1]
				if x[i] == y[j] {
					row[j+1] = prev + 1
				} else if row[j] > row[j+1] {
					row[j+1] = row[j]
				}
				prev = cur
			}
		}
		common = row[len(y)]
	}
	return len(y) - common, len(x) - common
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func countLines(s string) int { return len(splitLines(s)) }

// questionOptions are the choices of an AskUserQuestion's first question.
func questionOptions(in toolInput) []string {
	var q []struct {
		Options []struct {
			Label string `json:"label"`
		} `json:"options"`
	}
	if json.Unmarshal(in["questions"], &q) != nil || len(q) == 0 {
		return nil
	}
	var out []string
	for _, o := range q[0].Options {
		if o.Label != "" {
			out = append(out, o.Label)
		}
	}
	return out
}

// capText cuts s to n characters at most.
func capText(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// Following sessions' conversations.

// conversationOf is session x's conversation, caught up with its
// transcripts and its state. It reports whether it changed. The caller
// holds c.mu.
func (c *Core) conversationOf(x *session) (*conversation, bool) {
	if x.conv == nil {
		x.conv = newConversation()
	}
	row := x.wire()
	active := row.State == ipc.StateWorking || row.State == ipc.StateNeedsYou || row.State == ipc.StateStalled || row.State == ipc.StateStarting
	changed := x.conv.update(c.transcriptsOf(x), active)
	if x.conv.linkAgents(x.track.subagents()) {
		changed = true
	}
	if x.conv.setNeeds(c.needsOf(x, row)) {
		changed = true
	}
	return x.conv, changed
}

// followConversations catches up the conversations that clients follow
// and tells those clients. The caller holds c.mu.
func (c *Core) followConversations() {
	for _, x := range c.sessions {
		if x.conv != nil && x.convSubs > 0 || len(x.helpers) > 0 {
			c.followConversation(x)
		}
	}
}

// followConversation catches up one session's conversation for the clients
// that follow it. The caller holds c.mu.
func (c *Core) followConversation(x *session) {
	if x.convSubs > 0 {
		if _, changed := c.conversationOf(x); changed {
			c.tellConversation(x)
		}
	}
	c.followHelpers(x)
}

// transcriptsOf are session x's own transcripts that exist, oldest first:
// those its SessionStart hooks named, or, when there are none, the
// transcript Claude Code keeps for its session id, as for a session started
// before Irie followed hooks. The caller holds c.mu.
func (c *Core) transcriptsOf(x *session) []string {
	var out []string
	sub := string(filepath.Separator) + "subagents" + string(filepath.Separator)
	for _, p := range x.track.transcripts {
		if _, err := os.Stat(p); err == nil && !strings.Contains(p, sub) {
			out = append(out, p)
		}
	}
	if len(out) == 0 && x.vendorID != "" && x.cwd != "" {
		config := c.cfg().Profiles[x.profile].Env["CLAUDE_CONFIG_DIR"]
		if config == "" {
			config = os.Getenv("CLAUDE_CONFIG_DIR")
		}
		if config == "" {
			home, _ := os.UserHomeDir()
			config = filepath.Join(home, ".claude")
		}
		if p := claudeTranscript(config, x.cwd, x.vendorID); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var notAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9]`)

// claudeTranscript is where Claude Code keeps a session's transcript, if it
// is there: <config>/projects/<the working directory, every character but
// letters and digits made "-">/<session id>.jsonl. A name over 200
// characters gets a hash Irie cannot know.
func claudeTranscript(config, cwd, sessionID string) string {
	dirs := []string{cwd}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		dirs = append(dirs, real)
	}
	for _, d := range dirs {
		name := notAlphanumeric.ReplaceAllString(d, "-")
		if len(name) > 200 {
			continue
		}
		p := filepath.Join(config, "projects", name, sessionID+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// needsOf is what session x waits on you for, as an item: what its newest
// need asks in plain words, with a question's choices or the plan to
// approve. The caller holds c.mu.
func (c *Core) needsOf(x *session, row ipc.Session) *ipc.Item {
	if row.State != ipc.StateNeedsYou && row.State != ipc.StateStalled {
		return nil
	}
	it := &ipc.Item{ID: "needs", Kind: ipc.ItemNeeds, Reason: row.Reason, Text: "Needs you"}
	if row.Detail != "" {
		it.Text = sentence(row.Detail, "")
	}
	n := x.track.latestNeed()
	if p := n.Str("phrase"); p != "" && row.State == ipc.StateNeedsYou {
		it.Text = p
	}
	it.Time = n.Time()
	switch row.Reason {
	case "question":
		if cl := x.conv.open("AskUserQuestion"); cl != nil {
			it.Options = cl.options
		}
	case "plan":
		if cl := x.conv.open("ExitPlanMode"); cl != nil {
			it.Detail = cl.plan
		}
	}
	return it
}

// conversation answers the conversation op.
func (c *Core) conversation(a ipc.ConversationArgs) (ipc.Conversation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	x := c.sessions[a.Session]
	if x == nil {
		return ipc.Conversation{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	if isHelper(a.Agent) {
		if !helperAgent.MatchString(a.Agent) {
			return ipc.Conversation{}, ipc.Errorf(ipc.CodeBadArgs, "no helper %q", a.Agent)
		}
		return c.helperConversation(x, a.Agent, a.Limit), nil
	}
	cv, _ := c.conversationOf(x)
	out := cv.all(a.Limit)
	out.Session = x.id
	return out, nil
}
