//go:build !nochat

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
)

// chatFake is a core whose chat op says what it was asked.
type chatFake struct {
	mu    sync.Mutex
	asked []ipc.ChatArgs
}

func (f *chatFake) Call(op string, raw json.RawMessage) (any, error) {
	if op != ipc.OpChat {
		return nil, ipc.Errorf(ipc.CodeBadArgs, "no %s here", op)
	}
	var a ipc.ChatArgs
	json.Unmarshal(raw, &a)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, a)
	r := ipc.ChatResult{Accounts: []ipc.ChatAccount{}, Drafts: []ipc.ChatDraft{{ID: "dabc", Account: "acme", ChannelName: "general", Text: "line one\nline two", State: ipc.DraftWaiting, SessionName: "writer"}}, Text: "said " + a.Do}
	return r, nil
}

func (f *chatFake) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	return nil, nil, errors.New("no")
}

func (f *chatFake) last() ipc.ChatArgs {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[len(f.asked)-1]
}

// TestIrieChat: irie chat sends each command to the core's chat op as it
// was typed, a post's text from standard input, a token from standard
// input and never an argument, and prints the core's words.
func TestIrieChat(t *testing.T) {
	bare(t)
	run, err := os.MkdirTemp("/tmp", "ct")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	t.Setenv("XDG_RUNTIME_DIR", run)
	p, err := paths.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(p.Config, 0o700)
	os.MkdirAll(p.Runtime, 0o700)
	l, err := ipc.Listen(p.Socket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	fake := &chatFake{}
	go ipc.Serve(l, fake)
	chat := func(stdin string, args ...string) (string, error) {
		var out bytes.Buffer
		err := chatCmd(args, strings.NewReader(stdin), &out)
		return out.String(), err
	}
	if out, err := chat("", "channels", "acme", "--refresh"); err != nil || out != "said channels\n" {
		t.Errorf("channels: %q, %v", out, err)
	}
	if a := fake.last(); a.Do != ipc.ChatChannels || a.Account != "acme" || !a.Refresh {
		t.Errorf("channels asked %+v", a)
	}
	// A post's text from standard input, whole, with its flags after.
	if _, err := chat("Release \"2\"\nis out\n", "post", "acme:#news", "-", "--thread", "v2"); err != nil {
		t.Fatal(err)
	}
	if a := fake.last(); a.Do != ipc.ChatPost || a.Channel != "acme:#news" || a.Text != "Release \"2\"\nis out\n" || a.Thread != "v2" {
		t.Errorf("post asked %+v", a)
	}
	if _, err := chat("", "post", "#news", "hello", "there", "--reply", "403"); err != nil {
		t.Fatal(err)
	}
	if a := fake.last(); a.Text != "hello there" || a.Reply != "403" {
		t.Errorf("post asked %+v", a)
	}
	if _, err := chat("", "agents", "#bots", "post"); err != nil {
		t.Fatal(err)
	}
	if a := fake.last(); a.Do != ipc.ChatAgents || a.Channel != "#bots" || a.Agents != "post" {
		t.Errorf("agents asked %+v", a)
	}
	if _, err := chat("", "agents", "acme", "read", "on"); err != nil {
		t.Fatal(err)
	}
	if a := fake.last(); a.Read == nil || !*a.Read || a.Account != "acme" {
		t.Errorf("agents read asked %+v", a)
	}
	// The token on standard input goes to the op, and no further.
	out, err := chat("good.token\n", "add", "discord", "acme", "--user", "1")
	if err != nil || out != "said add\n" {
		t.Errorf("add: %q, %v", out, err)
	}
	if a := fake.last(); a.Do != ipc.ChatAdd || a.New == nil || a.New.Token != "good.token" || a.New.Name != "acme" || a.New.User != "1" {
		t.Errorf("add asked %+v", a.New)
	}
	if _, err := chat("", "add", "matrix"); err == nil {
		t.Error("a kind there is none of")
	}
	// Slack's two tokens, a line each, both reach the op.
	if _, err := chat("xoxb-1\nxapp-1\n", "add", "slack"); err != nil {
		t.Fatal(err)
	}
	if a := fake.last(); a.New.Kind != "slack" || a.New.Token != "xoxb-1" || a.New.App != "xapp-1" {
		t.Errorf("add slack asked %+v", a.New)
	}
	if _, err := chat("xoxb-2\n", "add", "slack", "team"); err != nil {
		t.Fatal(err)
	}
	if a := fake.last(); a.New.Token != "xoxb-2" || a.New.App != "" || a.New.Name != "team" {
		t.Errorf("add slack without an app token asked %+v", a.New)
	}
	// A token from a Keychain item the person keeps already, never an
	// argument; Slack's app token still asked for after it.
	keychainToken = func(service string) (string, error) {
		if service != "acme-bot" {
			return "", errors.New("no such item")
		}
		return "kept.token", nil
	}
	if _, err := chat("", "add", "discord", "--keychain", "acme-bot"); err != nil || fake.last().New.Token != "kept.token" {
		t.Errorf("add --keychain: %v %+v", err, fake.last().New)
	}
	if _, err := chat("xapp-9\n", "add", "slack", "--keychain", "acme-bot"); err != nil || fake.last().New.Token != "kept.token" || fake.last().New.App != "xapp-9" {
		t.Errorf("add slack --keychain: %v %+v", err, fake.last().New)
	}
	if _, err := chat("", "add", "discord", "--keychain", "nothing"); err == nil {
		t.Error("a Keychain item there is none of")
	}
	if _, err := chat("", "read", "#general", "--thread", "1700000003.000300"); err != nil || fake.last().Thread != "1700000003.000300" {
		t.Errorf("read a thread: %v %+v", err, fake.last())
	}
	out, err = chat("", "drafts")
	if err != nil || !strings.HasPrefix(out, "dabc  waiting  to acme:#general, from writer") || !strings.Contains(out, "    line two") {
		t.Errorf("drafts: %v\n%s", err, out)
	}
	if _, err := chat("", "send", "dabc"); err != nil || fake.last().ID != "dabc" {
		t.Errorf("send: %v %+v", err, fake.last())
	}
	if _, err := chat("", "bogus"); err == nil || !strings.Contains(err.Error(), "unknown chat command") {
		t.Errorf("bogus: %v", err)
	}
	if out, err := chat("", "help"); err != nil || !strings.Contains(out, "chat agents CHANNEL none|draft|post") {
		t.Errorf("help: %v\n%s", err, out)
	}
}
