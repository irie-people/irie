package ctxgit

import (
	"strconv"
	"strings"
	"time"
)

// A turn's commit message reads as the turn: the prompt as the subject, the
// whole prompt when it is longer, the last reply quoted, then git trailers
// that say whose turn it was, so `git log --format=%(trailers)` and Irie
// read the same facts:
//
//	fix the flaky test in core
//
//	> Fixed: the test waited on a timer; it waits on the event now.
//
//	Irie-Session: s0k3j9x2m4a7q
//	Irie-Turn: 3
//	Irie-Leaf: 6f1c…
//	Irie-Code: 1b2c…
const (
	trSession      = "Irie-Session"
	trName         = "Irie-Name"
	trConversation = "Irie-Conversation"
	trTurn         = "Irie-Turn"
	trLeaf         = "Irie-Leaf"
	trModel        = "Irie-Model"
	trCode         = "Irie-Code"
	trDirty        = "Irie-Dirty"
	trPartial      = "Irie-Partial"
	trCut          = "Irie-Cut"
)

const (
	subjectMax = 100
	promptMax  = 2000
	saidMax    = 800
)

// message is the commit message of a turn.
func message(t Turn) string {
	subject := strings.TrimSpace(t.Prompt)
	switch {
	case subject != "":
	case t.Turn == 0:
		subject = "(before the first prompt)"
	default:
		subject = "(no prompt)"
	}
	var b strings.Builder
	first := oneLine(subject, subjectMax)
	b.WriteString(first + "\n\n")
	if whole := strings.TrimSpace(t.Prompt); whole != "" && oneLine(whole, len(whole)+1) != first {
		b.WriteString(cutText(whole, promptMax) + "\n\n")
	}
	if said := strings.TrimSpace(t.Said); said != "" {
		for _, l := range strings.Split(cutText(said, saidMax), "\n") {
			b.WriteString(strings.TrimRight("> "+l, " ") + "\n")
		}
		b.WriteString("\n")
	}
	tr := func(k, v string) {
		if v = strings.Join(strings.Fields(v), " "); v != "" {
			b.WriteString(k + ": " + v + "\n")
		}
	}
	tr(trSession, t.Session)
	tr(trName, t.Name)
	tr(trConversation, t.Conversation)
	tr(trTurn, strconv.Itoa(t.Turn))
	tr(trLeaf, t.Leaf)
	tr(trModel, t.Model)
	tr(trCode, t.Code)
	if t.Dirty > 0 {
		tr(trDirty, strconv.Itoa(t.Dirty))
	}
	if t.Partial {
		tr(trPartial, "yes")
	}
	tr(trCut, t.Cut)
	return b.String()
}

// parseMessage reads a turn back from its commit message; Commit and Time
// are left for the caller.
func parseMessage(msg string) Turn {
	var t Turn
	paras := strings.Split(strings.TrimSpace(msg), "\n\n")
	if len(paras) == 0 {
		return t
	}
	t.Prompt = strings.TrimSpace(strings.SplitN(paras[0], "\n", 2)[0])
	for _, p := range paras[1:] {
		if strings.HasPrefix(p, ">") {
			var said []string
			for _, l := range strings.Split(p, "\n") {
				said = append(said, strings.TrimPrefix(strings.TrimPrefix(l, ">"), " "))
			}
			t.Said = strings.Join(said, "\n")
		}
	}
	for _, l := range strings.Split(paras[len(paras)-1], "\n") {
		k, v, ok := strings.Cut(l, ": ")
		if !ok {
			continue
		}
		switch k {
		case trSession:
			t.Session = v
		case trName:
			t.Name = v
		case trConversation:
			t.Conversation = v
		case trTurn:
			t.Turn, _ = strconv.Atoi(v)
		case trLeaf:
			t.Leaf = v
		case trModel:
			t.Model = v
		case trCode:
			t.Code = v
		case trDirty:
			t.Dirty, _ = strconv.Atoi(v)
		case trPartial:
			t.Partial = v == "yes"
		case trCut:
			t.Cut = v
		}
	}
	return t
}

// cutText is s cut at n bytes on a rune's edge, with an ellipsis.
func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

// commitTime reads git's %ct.
func commitTime(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0).UTC()
}
