//go:build !nochat

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// The chat plugin's command (docs/proposals/chat.md): irie chat, which asks
// the core, where the accounts' connections and the agents' rules are. An
// agent runs it too: the core knows its calls by their process, and holds
// them to what the person opened to agents. go build -tags nochat leaves
// this file out.

// chatUsage is the chat plugin's lines in the usage.
const chatUsage = `  ` + brand.Slug + ` chat [<command>]   Discord and Slack through bots of yours: channels,
                          reading, posting, and the drafts agents write
                          for you to send (` + brand.Slug + ` chat help)
`

const chatHelp = `usage:
  ` + brand.Slug + ` chat                    the accounts, and the drafts that wait for you
  ` + brand.Slug + ` chat channels [ACCOUNT] [--refresh]
                        each server's channels, and what agents may do in each
  ` + brand.Slug + ` chat read CHANNEL [--thread ID] [--before ID] [--limit N] [--refresh]
                        a channel's newest messages, oldest first, or a
                        thread's: on Slack the ID of its first message
  ` + brand.Slug + ` chat post CHANNEL [TEXT|-] [--reply ID] [--thread TITLE]
                        post TEXT, or standard input with -: a reply, or a
                        thread with TITLE (a forum's posts need one). An
                        agent's post is a draft, unless its channel lets
                        agents post on their own
  ` + brand.Slug + ` chat edit CHANNEL ID [TEXT|-]   change a message's text
  ` + brand.Slug + ` chat delete CHANNEL ID          remove a message
  ` + brand.Slug + ` chat drafts             the drafts agents wrote, and what came of them
  ` + brand.Slug + ` chat send DRAFT [TEXT|-]   send a draft, as it is or as you change it
  ` + brand.Slug + ` chat discard DRAFT      drop a draft
  ` + brand.Slug + ` chat agents CHANNEL none|draft|post
                        what agents may do in a channel: nothing, write
                        drafts for you (the default), or post on their own,
                        at most once a minute and chat.agent_posts an hour,
                        never pinging everyone, each in the log
  ` + brand.Slug + ` chat agents ACCOUNT read on|off
                        whether agents may read an account's messages
  ` + brand.Slug + ` chat log [ACCOUNT] [--limit N]
                        what agents posted, edited and deleted, newest first
  ` + brand.Slug + ` chat add discord|slack [NAME] [--user ID]
                        add a Discord bot of yours, or a Slack app of yours,
                        once its token signs in: the token comes on standard
                        input, or is asked for without showing; it goes to
                        the Keychain, never config.toml. Slack's says first
                        how to make the app, and takes its app-level token
                        after the bot's (a line of its own; none to skip).
                        --user is your own user ID there, so a mention of
                        you counts too
  ` + brand.Slug + ` chat remove NAME        remove an account, its tokens and its drafts
  ` + brand.Slug + ` chat test NAME          sign in to an account again

A CHANNEL is #name or a channel's ID, with ACCOUNT: in front when there are
several accounts, and SERVER/ before a name two servers share.
`

func init() {
	pluginCmds["chat"] = pluginCmd{run: func(args []string) error { return chatCmd(args, os.Stdin, os.Stdout) }}
}

func chatCall(a ipc.ChatArgs, wait time.Duration) (ipc.ChatResult, error) {
	_, p, err := load()
	if err != nil {
		return ipc.ChatResult{}, err
	}
	var r ipc.ChatResult
	if err := ipc.Call(p.Socket(), wait, ipc.OpChat, a, &r); err != nil {
		return ipc.ChatResult{}, err
	}
	return r, nil
}

// chatText is a message's text: the words given, or with - or none,
// standard input whole, when it isn't a terminal.
func chatText(words []string, stdin io.Reader) (string, error) {
	if len(words) > 0 && !(len(words) == 1 && words[0] == "-") {
		return strings.Join(words, " "), nil
	}
	if isTerminal(stdin) {
		return "", errors.New("what text? Give it after the channel, or - and the text on standard input")
	}
	b, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// flags parses fs from args, letting flags come after the words too: it
// gives the words.
func flags(fs *flag.FlagSet, args []string) ([]string, error) {
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return words, nil
		}
		words = append(words, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func chatCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	say := func(a ipc.ChatArgs, wait time.Duration) error {
		r, err := chatCall(a, wait)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, r.Text)
		return nil
	}
	const wait = 90 * time.Second
	switch sub {
	case "", "list", "drafts":
		if len(args) > 0 {
			return fmt.Errorf("usage: %s chat %s", brand.Slug, sub)
		}
		r, err := chatCall(ipc.ChatArgs{}, wait)
		if err != nil {
			return err
		}
		if sub == "drafts" {
			fmt.Fprintln(stdout, draftsText(r.Drafts))
			return nil
		}
		fmt.Fprintln(stdout, r.Text)
		return nil
	case "channels":
		fs := flag.NewFlagSet("chat channels", flag.ContinueOnError)
		refresh := fs.Bool("refresh", false, "ask the service again now")
		words, err := flags(fs, args)
		if err != nil {
			return err
		}
		if len(words) > 1 {
			return errors.New("usage: " + brand.Slug + " chat channels [ACCOUNT]")
		}
		a := ipc.ChatArgs{Do: ipc.ChatChannels, Refresh: *refresh}
		if len(words) == 1 {
			a.Account = words[0]
		}
		return say(a, wait)
	case "read":
		fs := flag.NewFlagSet("chat read", flag.ContinueOnError)
		before := fs.String("before", "", "the messages before this one")
		thread := fs.String("thread", "", "the thread to read")
		limit := fs.Int("limit", 0, "how many")
		refresh := fs.Bool("refresh", false, "ask the service again now")
		words, err := flags(fs, args)
		if err != nil {
			return err
		}
		if len(words) != 1 {
			return errors.New("usage: " + brand.Slug + " chat read CHANNEL [--thread ID] [--before ID] [--limit N]")
		}
		return say(ipc.ChatArgs{Do: ipc.ChatRead, Channel: words[0], Thread: *thread, Before: *before, Limit: *limit, Refresh: *refresh}, wait)
	case "post":
		fs := flag.NewFlagSet("chat post", flag.ContinueOnError)
		reply := fs.String("reply", "", "the message it answers")
		thread := fs.String("thread", "", "the title of a thread it starts")
		words, err := flags(fs, args)
		if err != nil {
			return err
		}
		if len(words) < 1 {
			return errors.New("usage: " + brand.Slug + " chat post CHANNEL [TEXT|-] [--reply ID] [--thread TITLE]")
		}
		text, err := chatText(words[1:], stdin)
		if err != nil {
			return err
		}
		return say(ipc.ChatArgs{Do: ipc.ChatPost, Channel: words[0], Text: text, Reply: *reply, Thread: *thread}, wait)
	case "edit", "delete":
		if len(args) < 2 || (sub == "delete" && len(args) != 2) {
			return fmt.Errorf("usage: %s chat %s CHANNEL ID%s", brand.Slug, sub, map[bool]string{true: " [TEXT|-]"}[sub == "edit"])
		}
		a := ipc.ChatArgs{Do: ipc.ChatEdit, Channel: args[0], ID: args[1]}
		if sub == "delete" {
			a.Do = ipc.ChatDelete
		} else {
			text, err := chatText(args[2:], stdin)
			if err != nil {
				return err
			}
			a.Text = text
		}
		return say(a, wait)
	case "send":
		if len(args) < 1 {
			return errors.New("usage: " + brand.Slug + " chat send DRAFT [TEXT|-]")
		}
		a := ipc.ChatArgs{Do: ipc.ChatSend, ID: args[0]}
		if len(args) > 1 {
			text, err := chatText(args[1:], stdin)
			if err != nil {
				return err
			}
			a.Text = text
		}
		return say(a, wait)
	case "discard":
		if len(args) != 1 {
			return errors.New("usage: " + brand.Slug + " chat discard DRAFT")
		}
		return say(ipc.ChatArgs{Do: ipc.ChatDiscard, ID: args[0]}, wait)
	case "agents":
		switch {
		case len(args) == 3 && args[1] == "read" && (args[2] == "on" || args[2] == "off"):
			on := args[2] == "on"
			return say(ipc.ChatArgs{Do: ipc.ChatAgents, Account: args[0], Read: &on}, wait)
		case len(args) == 2:
			return say(ipc.ChatArgs{Do: ipc.ChatAgents, Channel: args[0], Agents: args[1]}, wait)
		}
		return errors.New("usage: " + brand.Slug + " chat agents CHANNEL none|draft|post, or " + brand.Slug + " chat agents ACCOUNT read on|off")
	case "log":
		fs := flag.NewFlagSet("chat log", flag.ContinueOnError)
		limit := fs.Int("limit", 0, "how many")
		words, err := flags(fs, args)
		if err != nil {
			return err
		}
		a := ipc.ChatArgs{Do: ipc.ChatLog, Limit: *limit}
		if len(words) == 1 {
			a.Account = words[0]
		}
		return say(a, wait)
	case "add":
		return chatAdd(args, stdin, stdout)
	case "remove", "test":
		if len(args) != 1 {
			return fmt.Errorf("usage: %s chat %s NAME", brand.Slug, sub)
		}
		do := map[string]string{"remove": ipc.ChatRemove, "test": ipc.ChatTest}[sub]
		return say(ipc.ChatArgs{Do: do, Account: args[0]}, wait)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, chatHelp)
		return nil
	}
	return fmt.Errorf("unknown chat command %q\n\n%s", sub, chatHelp)
}

// draftsText is the drafts agents wrote, those waiting first.
func draftsText(drafts []ipc.ChatDraft) string {
	if len(drafts) == 0 {
		return "No drafts."
	}
	var b strings.Builder
	for _, d := range drafts {
		fmt.Fprintf(&b, "%s  %s  to %s:#%s", d.ID, d.State, d.Account, d.ChannelName)
		if d.Thread != "" {
			fmt.Fprintf(&b, ", a thread %q", d.Thread)
		} else if d.Reply != "" {
			fmt.Fprintf(&b, ", replying to %s", d.Reply)
		}
		by := d.SessionName
		if by == "" {
			by = d.Session
		}
		fmt.Fprintf(&b, ", from %s, %s\n", by, d.Made.Local().Format("2 Jan 15:04"))
		if d.Link != "" {
			fmt.Fprintf(&b, "  %s\n", d.Link)
		}
		if d.Error != "" {
			fmt.Fprintf(&b, "  last try: %s\n", d.Error)
		}
		for _, line := range strings.Split(d.Text, "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func chatAdd(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) < 1 {
		return errors.New("usage: " + brand.Slug + " chat add discord|slack [NAME] [--user ID]")
	}
	kind := args[0]
	fs := flag.NewFlagSet("chat add", flag.ContinueOnError)
	user := fs.String("user", "", "your own user ID there")
	words, err := flags(fs, args[1:])
	if err != nil {
		return err
	}
	if len(words) > 1 {
		return errors.New("usage: " + brand.Slug + " chat add discord|slack [NAME] [--user ID]")
	}
	n := &ipc.ChatNew{Kind: kind, User: *user}
	if len(words) == 1 {
		n.Name = words[0]
	}
	prompts := []string{"The bot's token"}
	switch kind {
	case config.ChatDiscord:
		if isTerminal(stdin) {
			fmt.Fprintln(stdout, "Discord takes a bot's token: Developer Portal (https://discord.com/developers/applications) › your application › Bot › Reset Token. Invite the bot to your server with View Channels, Send Messages, Send Messages in Threads, Create Public Threads and Read Message History.")
		}
	case config.ChatSlack:
		if isTerminal(stdin) {
			r, err := chatCall(ipc.ChatArgs{Do: ipc.ChatManifest}, 30*time.Second)
			if err != nil {
				return err
			}
			fmt.Fprintln(stdout, r.Text)
		}
		prompts = []string{"The bot's token (xoxb-)", "The app-level token (xapp-), or Return for none yet"}
	default:
		return fmt.Errorf("%q is no kind of chat account here: discord or slack", kind)
	}
	tokens, err := readHiddenLines(stdin, stdout, prompts...)
	if err != nil {
		return err
	}
	n.Token = tokens[0]
	if len(tokens) > 1 {
		n.App = tokens[1]
	}
	r, err := chatCall(ipc.ChatArgs{Do: ipc.ChatAdd, New: n}, 90*time.Second)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, r.Text)
	return nil
}
