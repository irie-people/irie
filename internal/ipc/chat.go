package ipc

import "time"

// OpChat is the chat plugin's accounts, channels, messages, drafts and log
// (docs/proposals/chat.md): Discord and Slack, each through a bot of the
// person's own. The core reads and posts, and clients draw. A call from
// inside an agent's pane is held to what the person opened to agents: a
// post becomes a draft the person sends, unless its channel lets agents
// post on their own, and a read is refused unless its account lets agents
// read. It answers a ChatResult.
const OpChat = "chat"

// What ChatArgs.Do asks for.
const (
	ChatList     = ""         // the accounts, the drafts and the newest mentions
	ChatChannels = "channels" // an account's servers or workspace, and their channels
	ChatRead     = "read"     // a channel's newest messages, or those before Before
	ChatPost     = "post"     // post Text to Channel; an agent's becomes a draft unless the channel lets agents post
	ChatEdit     = "edit"     // change the text of message ID in Channel
	ChatDelete   = "delete"   // remove message ID from Channel
	ChatSend     = "send"     // send draft ID, with Text when the person changed it: a person's word only
	ChatDiscard  = "discard"  // drop draft ID: the person's, or its own agent's while it waits
	ChatAgents   = "agents"   // set what agents may do in Channel (Agents), or on Account (Read): a person's word only
	ChatLog      = "log"      // what agents posted, edited and deleted, newest first
	ChatSeen     = "seen"     // the person has seen the mentions
	ChatAdd      = "add"      // add an account, once its token signs in
	ChatRemove   = "remove"   // remove account Account and its tokens
	ChatTest     = "test"     // sign in to account Account again
	ChatManifest = "manifest" // the Slack app's manifest, to make the app from
)

// The states of a chat account.
const (
	ChatOK      = "ok"      // its last look worked
	ChatNeeds   = "needs"   // it has no token kept
	ChatError   = "error"   // its last look failed: Problem says why
	ChatUnknown = "unknown" // not looked at since the core started
)

// The kinds of channel, across services.
const (
	ChannelText         = "text"         // a channel to post in
	ChannelAnnouncement = "announcement" // Discord's announcement channel: posts can be followed from other servers
	ChannelForum        = "forum"        // posts are threads with titles: Discord's forum and media channels
	ChannelVoice        = "voice"        // a voice or stage channel's text chat
	ChannelThread       = "thread"       // a thread under a channel
	ChannelDM           = "dm"           // a direct message with one person
	ChannelGroup        = "group"        // a direct message with several people
)

// The states of a draft.
const (
	DraftWaiting   = "waiting"   // for the person to send or discard
	DraftSent      = "sent"      // the person sent it: Link is where
	DraftDiscarded = "discarded" // the person, or its agent, dropped it
	DraftFailed    = "failed"    // the person sent it and the service refused: Error says why
)

// ChatArgs ask the chat op for something; Do says what.
type ChatArgs struct {
	Do string `json:"do,omitempty"`
	// Account is an account's name; "" = the only one, or every one for
	// the list.
	Account string `json:"account,omitempty"`
	// Channel is a channel or thread: its ID, #name, or SERVER/#name where
	// two servers share a name, each after ACCOUNT: when Account is "".
	Channel string `json:"channel,omitempty"`
	// ID is a message's ID for edit and delete, or a draft's for send and
	// discard.
	ID   string `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
	// Reply is the message a post answers.
	Reply string `json:"reply,omitempty"`
	// Thread, for a post, is the title of a thread it starts: from
	// Reply's message when there is one, in a forum as a new post, else in
	// the channel. For a read it is the thread to read: on Slack the ts of
	// its first message, on Discord the thread's ID.
	Thread string `json:"thread,omitempty"`
	// Before, for read, asks for the messages before this one.
	Before string `json:"before,omitempty"`
	// Limit is how many messages a read brings, or log entries; 0 = the
	// op's own.
	Limit int `json:"limit,omitempty"`
	// Agents is what agents may do in Channel: none, draft or post.
	Agents string `json:"agents,omitempty"`
	// Read says whether agents may read Account's messages.
	Read *bool `json:"read,omitempty"`
	// Refresh asks the service again now, rather than what the core read
	// in the last half minute.
	Refresh bool     `json:"refresh,omitempty"`
	New     *ChatNew `json:"new,omitempty"`
}

// ChatNew is an account to add.
type ChatNew struct {
	Kind string `json:"kind"`           // discord or slack
	Name string `json:"name,omitempty"` // "" = from the bot's own name
	// Token is the bot's token: Discord's from the application's Bot
	// page, Slack's xoxb- from OAuth & Permissions.
	Token string `json:"token"`
	// App is Slack's app-level token, xapp-, which hears mentions and DMs
	// as they come; "" = none yet.
	App string `json:"app,omitempty"`
	// User is the person's own user ID there; "" = none.
	User string `json:"user,omitempty"`
}

// ChatResult is the chat op's answer: always the accounts and the drafts,
// and what the call asked for.
type ChatResult struct {
	Accounts []ChatAccount `json:"accounts"`
	// Drafts are the agents' drafts: those waiting first, oldest first,
	// then those that ended in the last week, newest first. An agent sees
	// its own.
	Drafts []ChatDraft `json:"drafts"`
	// Mentions are the newest mentions and DMs heard, newest first, with
	// Unseen how many came since the person last looked. An agent sees
	// none.
	Mentions []ChatMention `json:"mentions,omitempty"`
	Unseen   int           `json:"unseen,omitempty"`
	// Spaces are an account's servers or workspace, with their channels.
	Spaces []ChatSpace `json:"spaces,omitempty"`
	// Channel and Messages are what a read brought: the messages oldest
	// first, as a chat shows them; More says older ones are there, before
	// the first.
	Channel  *ChatChannel  `json:"channel,omitempty"`
	Messages []ChatMessage `json:"messages,omitempty"`
	More     bool          `json:"more,omitempty"`
	// Note is what a reader should know of the messages, such as text the
	// service hides from the bot.
	Note string `json:"note,omitempty"`
	// Message is a post or an edit as it went out.
	Message *ChatMessage `json:"message,omitempty"`
	// Draft is the draft an agent's post became, or the one sent or
	// discarded.
	Draft *ChatDraft `json:"draft,omitempty"`
	// Log is what agents posted, edited and deleted, newest first.
	Log []ChatLogEntry `json:"log,omitempty"`
	// Manifest is the Slack app's manifest, as JSON.
	Manifest string `json:"manifest,omitempty"`
	// Text is the answer in words, as :chat and irie chat say it.
	Text string `json:"text"`
}

// ChatAccount is one account and how it stands.
type ChatAccount struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`    // discord or slack
	Service string `json:"service"` // Discord, Slack
	// Colour is which of the agents' six colours marks it, 0 to 5.
	Colour  int    `json:"colour"`
	State   string `json:"state"` // ok, needs, error or unknown
	Problem string `json:"problem,omitempty"`
	// Bot is the bot's name there, and BotID its user ID.
	Bot   string `json:"bot,omitempty"`
	BotID string `json:"bot_id,omitempty"`
	// Agents is what agents may do in its channels unless a channel says
	// otherwise, none or draft; AgentsRead whether they may read it.
	Agents     string `json:"agents"`
	AgentsRead bool   `json:"agents_read"`
	// Hidden says the service leaves message text out for this bot, as
	// Discord does until its Message Content intent is on; Fix says what
	// turns it on.
	Hidden bool    `json:"hidden,omitempty"`
	Fix    string  `json:"fix,omitempty"`
	Can    ChatCan `json:"can"`
}

// ChatCan is what a service has, so a client offers only that.
type ChatCan struct {
	Threads bool `json:"threads"` // replies in threads
	Titles  bool `json:"titles"`  // threads with titles of their own
	Forums  bool `json:"forums"`  // forum channels, whose posts are titled threads
	Edit    bool `json:"edit"`
	Delete  bool `json:"delete"`
	Listen  bool `json:"listen"` // mentions and DMs as they come
}

// ChatSpace is a Discord server or a Slack workspace, with its channels.
type ChatSpace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account string `json:"account"`
	// Problem says why its channels couldn't be read.
	Problem string `json:"problem,omitempty"`
	// Channels are in the service's own order, each thread after its
	// channel.
	Channels []ChatChannel `json:"channels"`
}

// ChatChannel is one channel or thread.
type ChatChannel struct {
	ID      string `json:"id"`
	Name    string `json:"name"` // without #
	Kind    string `json:"kind"` // text, announcement, forum, voice, thread, dm or group
	Account string `json:"account"`
	Space   string `json:"space,omitempty"` // its server's ID
	// Parent is a thread's channel, or a channel's category; Category is
	// that category's name.
	Parent   string `json:"parent,omitempty"`
	Category string `json:"category,omitempty"`
	Topic    string `json:"topic,omitempty"` // left out for an agent that may not read
	// Agents is what agents may do here: none, draft or post.
	Agents string `json:"agents"`
	// Read and Post say whether the bot may read and post here; Why says
	// what it lacks.
	Read bool   `json:"read"`
	Post bool   `json:"post"`
	Why  string `json:"why,omitempty"`
	Link string `json:"link,omitempty"` // the channel in the service's own app
}

// ChatMessage is one message.
type ChatMessage struct {
	ID       string    `json:"id"`
	Channel  string    `json:"channel"`
	Author   string    `json:"author"` // the name it shows
	AuthorID string    `json:"author_id,omitempty"`
	Bot      bool      `json:"bot,omitempty"`  // a bot wrote it
	Mine     bool      `json:"mine,omitempty"` // this account's own bot wrote it
	Time     time.Time `json:"time"`
	Edited   time.Time `json:"edited,omitzero"`
	Text     string    `json:"text"`
	// Hidden says the service left the text out for this bot.
	Hidden bool `json:"hidden,omitempty"`
	// Reply is the message it answers; Thread the thread it starts or is
	// in, and Replies how many that holds.
	Reply   string `json:"reply,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Replies int    `json:"replies,omitempty"`
	// Files is how many files and pictures it carries, which Irie doesn't
	// show yet.
	Files int    `json:"files,omitempty"`
	Link  string `json:"link,omitempty"`
	// Agent is the session that posted it, when an agent did through
	// Irie.
	Agent string `json:"agent,omitempty"`
}

// ChatDraft is a post an agent wrote, for the person to send or discard.
type ChatDraft struct {
	ID          string `json:"id"`
	Account     string `json:"account"`
	Channel     string `json:"channel"`
	ChannelName string `json:"channel_name"`
	Space       string `json:"space,omitempty"` // its server's name
	Reply       string `json:"reply,omitempty"`
	Thread      string `json:"thread,omitempty"` // the title of a thread it starts
	Text        string `json:"text"`
	// Session is the session that wrote it, and SessionName its name
	// then.
	Session     string    `json:"session"`
	SessionName string    `json:"session_name,omitempty"`
	Made        time.Time `json:"made"`
	State       string    `json:"state"` // waiting, sent, discarded or failed
	Link        string    `json:"link,omitempty"`
	Error       string    `json:"error,omitempty"`
	Ended       time.Time `json:"ended,omitzero"`
	// By is who ended it: you or agent.
	By string `json:"by,omitempty"`
}

// ChatLogEntry is one post, edit or delete an agent made.
type ChatLogEntry struct {
	Time        time.Time `json:"time"`
	Session     string    `json:"session"`
	SessionName string    `json:"session_name,omitempty"`
	Account     string    `json:"account"`
	Channel     string    `json:"channel"`
	ChannelName string    `json:"channel_name"`
	Do          string    `json:"do"` // post, edit or delete
	Message     string    `json:"message"`
	Link        string    `json:"link,omitempty"`
	Text        string    `json:"text,omitempty"`
	// Draft is the draft it was, when the person sent it.
	Draft string `json:"draft,omitempty"`
}

// ChatMention is a message that mentions the bot, or the person, or a DM
// to the bot.
type ChatMention struct {
	Account     string    `json:"account"`
	Channel     string    `json:"channel"`
	ChannelName string    `json:"channel_name"`
	Message     string    `json:"message"`
	Author      string    `json:"author"`
	Text        string    `json:"text"`
	Hidden      bool      `json:"hidden,omitempty"`
	DM          bool      `json:"dm,omitempty"`
	You         bool      `json:"you,omitempty"` // it mentions the person, not only the bot
	Time        time.Time `json:"time"`
	Link        string    `json:"link,omitempty"`
}
