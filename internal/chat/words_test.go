//go:build !nochat

package chat

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// TestMentionLine: a mention on one line, who first, a DM and a mention of
// the person said so, hidden text said in words, long words cut.
func TestMentionLine(t *testing.T) {
	for _, c := range []struct {
		m    ipc.ChatMention
		want string
	}{
		{ipc.ChatMention{Author: "Alice", ChannelName: "general", Text: "Hi @Acme Helper"}, "Alice in #general: “Hi @Acme Helper”"},
		{ipc.ChatMention{Author: "Alice", ChannelName: "DM with Alice", Text: "psst", DM: true}, "Alice, a DM: “psst”"},
		{ipc.ChatMention{Author: "bob", ChannelName: "general", You: true, Hidden: true}, "bob mentioned you in #general (its text is hidden from the bot)"},
		{ipc.ChatMention{ChannelName: "general", Text: "line one\nline two"}, "Someone in #general: “line one line two”"},
		{ipc.ChatMention{Author: "Alice", ChannelName: "general", Text: strings.Repeat("word ", 20)}, "Alice in #general: “" + strings.Repeat("word ", 9) + "word…”"},
	} {
		if got := MentionLine(c.m); got != c.want {
			t.Errorf("MentionLine(%+v)\n = %q\nwant %q", c.m, got, c.want)
		}
	}
	// :chat lists the newest new ones, a line each.
	r := ipc.ChatResult{Accounts: []ipc.ChatAccount{{Name: "acme", Service: "Discord", State: ipc.ChatOK}}, Unseen: 1,
		Mentions: []ipc.ChatMention{{Author: "Alice", ChannelName: "general", Text: "new"}, {Author: "Bob", ChannelName: "general", Text: "seen"}}}
	if s := Summary(r); !strings.Contains(s, "1 new mention:\n  Alice in #general: “new”") || strings.Contains(s, "Bob") {
		t.Errorf("Summary:\n%s", s)
	}
}
