# Proposal: Chat, a plugin that hooks Irie into team and community chat

Date: 2026-11-21. Status: steps 2 and 3 of §8 built: the plugin with Discord and Slack, the drafts and the agents' rules; the Mac app's tab and live mentions follow. The owner asked for "a discord irie pluggin", then for it to be "like a generic plugin that hooks into things like discord and slack etc". There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote this file and the change it describes. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

The places a person's team or community talks, inside Irie beside the sessions: read a channel, post to it, hear when someone mentions you or writes to you, and let agents help, by writing drafts the person sends or, where the person says so, posting on their own. One plugin does it for every service, each service a connector.

**In short.**

1. **One plugin, a connector per service.** Discord and Slack first. Matrix, Mattermost, Telegram and Teams each come later as one more connector (§2), with no change to the tab, the words, the agents' rules or the notices.
2. **The core reads and posts; the Mac app only draws.** Tokens live in the login Keychain. Nothing anyone wrote is written to disk.
3. **Agents draft and a person sends.** Agents may post on their own only in a channel the person opens to them, and every such post is logged, rate-limited and never pings everyone. Agents read messages only on an account the person opens to them (§5).

## 1. What it is

Irie's tenth plugin, **Chat** (DESIGN §4.17), on by default: `[plugins] chat`, and `irie plugins off chat` turns it off. It shows nothing until an account is added. `go build -tags nochat` leaves it out. Its parts are kinds of account, as Mail's are, and one part for agents:

| Part | How Irie reaches it | Its check |
|---|---|---|
| Discord | A bot of the person's own: a Discord application's bot token, over the REST API v10; the Gateway for mentions and DMs as they come | ready once a Discord account is added and the core's last look at it didn't fail |
| Slack | A Slack app of the person's own, installed to their workspace: its bot token (`xoxb-`) for the Web API, and an app-level token (`xapp-`) for Socket Mode | the same, for Slack |
| Chat for agents | `irie chat` for agents and the skill `/irie:chat`: drafts by default (§5) | its own switch, `chat.agents`, on by default; ready once an account is added |

A **bot, never a person's own login.** Discord forbids automating a user account, and a Slack user token would speak as the person. A bot shows as a bot, which is honest to the people in the channel, and it sees only the servers and channels it was let into. Irie carries no bot or token of its own: Discord's developer terms forbid credentials in open-source code, and each person brings their own.

### Accounts

An account is a table in config.toml, by the short name every message of it shows:

```toml
[chat.accounts.acme]
kind = "discord"          # discord or slack
# agents = "draft"        # what agents may do in its channels: none, or draft (the default)
# agents_read = false     # whether agents may read its messages (§5)
# user = "123456789012345678"  # your own user ID there, so a mention of you counts too (§4)
# colour = 3              # 1 to 6; 0 or none = Irie picks

[chat.accounts.acme.channels.223456789012345678]
agents = "post"           # agents post here on their own, logged and rate-limited (§5)
name = "#announcements"   # for you to read; Irie writes the name the channel had then
```

`irie chat add discord [NAME]`, or Add Account in the Chat tab, adds one, and only once its token signs in: the core asks who the token is, and only then writes the table and keeps the token. Without a NAME it takes the bot's. The token comes on standard input, or is asked for without showing it on a terminal, never in an argument where `ps` shows it.

- **Discord:** a bot token from the person's application (Developer Portal › the application › Bot › Reset Token), its bot invited to their server with View Channels, Send Messages, Send Messages in Threads, Create Public Threads and Read Message History. Discord leaves message text empty for a bot without the **Message Content** intent, except in DMs to it, messages that mention it and its own (Bot › Privileged Gateway Intents). The core reads the application's flags at sign-in, and while the intent is off the tab and `irie chat read` say so once, above the messages, rather than showing them empty.
- **Slack:** the person makes a Slack app from the manifest Irie prints (`irie chat add slack` prints it first), installs it to their workspace, and pastes the bot token, then, for mentions and DMs as they come, an app-level token with `connections:write`. The scopes the manifest asks for are what the plugin uses and no more (§3).

**Tokens** never touch config.toml. On macOS they are generic passwords in the login Keychain, service "Irie chat", account the account's name (and `NAME.app` for Slack's app-level token), written through `security -i` with the token as hex on its input, as Mail's passwords are. Elsewhere, and in test Iries (`IRIE_KEYCHAIN=off`), they are files of the user's alone in `<state>/chat/keys`. No answer of the core ever holds one. Removing an account removes its table whole and its tokens.

## 2. The connector

`internal/chat` holds the plugin's service and one connector per kind of account. Each connector adds itself to the connectors this build has, as each plugin does in `internal/plugins`, behind one interface:

```go
type Connector interface {
	Me(ctx context.Context) (Me, error)                                // who the token is, and what it may do
	Spaces(ctx context.Context) ([]Space, error)                       // Discord's servers; Slack's workspace
	Channels(ctx context.Context, space string) ([]Channel, error)     // text, announcement and forum channels, threads, DMs
	Messages(ctx context.Context, channel string, p Page) ([]Message, error) // newest first; Page.Before for older
	Post(ctx context.Context, p Post) (Message, error)                 // to a channel or thread, a reply, a new thread or forum post
	Edit(ctx context.Context, channel, id, text string) (Message, error)
	Delete(ctx context.Context, channel, id string) error
	Listen(ctx context.Context, heard func(Event)) error               // mentions and DMs as they come, until ctx ends
}
```

A connector says what it can (`Can`: threads, titled threads, forums, edit, delete, listen), so the tab offers only what the service has. What each call is:

| Call | Discord (REST v10, `https://discord.com/api/v10`) | Slack (Web API, `https://slack.com/api`) |
|---|---|---|
| Me | `GET /users/@me`, `GET /applications/@me` (its flags say whether Message Content is on) | `auth.test` |
| Spaces | `GET /users/@me/guilds` | the workspace `auth.test` names |
| Channels | `GET /guilds/{id}/channels` and `GET /guilds/{id}/threads/active`, with what the bot may do in each from its roles and the channel's overwrites | `conversations.list` (public and private channels, group and direct messages), whether the bot is in each |
| Messages | `GET /channels/{id}/messages` | `conversations.history`; `conversations.replies` for a thread |
| Post | `POST /channels/{id}/messages`, with `message_reference` for a reply; a thread from a message or without one; a forum post (`POST /channels/{id}/threads` with its first message) | `chat.postMessage`, with `thread_ts` for a reply in a thread |
| Edit, Delete | `PATCH`, `DELETE /channels/{id}/messages/{id}` | `chat.update`, `chat.delete` |
| Listen | the Gateway, with the GUILDS, GUILD_MESSAGES and DIRECT_MESSAGES intents | Socket Mode: `app_mention`, `message.im` |

Every request says who is asking (Discord's `DiscordBot (URL, VERSION)` User-Agent) and keeps to the service's own limits: Discord's per-route buckets from its `X-RateLimit-*` headers, and on a 429 from either service the wait it names, up to a minute, after which the call fails and says why. A service's refusal comes back in words, with what fixes it: a token Discord doesn't know, a channel the bot can't see (and the permission it needs), a Slack scope the app lacks (and its name).

**A connector for another service** is a file in `internal/chat` with these calls, a kind in config, and a part in the plugin. What the next ones would use:

| Service | Its way in | What is different |
|---|---|---|
| Matrix | the Client-Server API with an access token of a bot user, `/sync` for events | rooms in spaces; end-to-end encrypted rooms need a crypto library, so the first connector reads unencrypted rooms only |
| Mattermost | REST API v4 with a bot account's token, its WebSocket for events | teams for spaces |
| Telegram | the Bot API, `getUpdates` for events | a bot can't list its chats: it learns them as messages come |
| Teams | Microsoft Graph with an app the organisation registers | posting into channels needs the organisation's admin to consent |

## 3. What the core keeps

The core owns the chat, as it owns every rule ([CORE.md](../CORE.md)); the clients only draw.

- **In memory:** each account's channels (fresh for 5 minutes), a channel's newest page of messages (fresh for 30 s, so a tab that looks every few seconds asks the service at most twice a minute), the mentions and DMs heard since the person last looked (the newest 50), and each account's state.
- **On disk**, in `<state>/chat`, the user's alone (0700, files 0600):
  - `drafts.json`, the agents' drafts: the agent's own words, where they go, and the session that wrote them. A draft must outlive the core, which restarts at every install, until the person sends or discards it. A sent or discarded draft stays a week, so its agent can learn what came of it, then goes.
  - `log.jsonl`, every post, edit and delete an agent made, on its own or through a draft the person sent: when, which session, where, the message's link and, for a post or an edit, its text. A post in a channel is outward-facing and goes out under the person's bot, so the person must be able to see all that agents said in their name, after an edit or a delete too. It keeps the newest 2,000 lines.
  - Nothing anyone else wrote. Messages read, mentions and DMs stay in memory and go with the core.

What a service sends is shown without control characters or the marks that reverse text, so a message can't rewrite a terminal's lines or hide where a link goes.

## 4. Where it shows

- **The right sidebar's Chat tab**, a plugin pane of the app's own view (kind `chat`), while the plugin is on, or with `:chat`. From the top:
  - **The accounts**, a strip of them, each in one of the agents' six colours with its service's mark, as Mail's accounts are; a click shows one alone.
  - **Waiting for you:** the agents' drafts, each with where it goes, the session that wrote it and its words, with **Send**, **Edit** and **Discard**. A draft never goes out without one of these.
  - **Mentions:** the newest mentions and DMs, each opening its channel at the message.
  - **Channels**, by server or workspace and then category, threads under their channel, each with a mark of what agents may do there: none, draft or post, by glyph as well as colour.
  - **A channel** opens in place of the list: its messages, oldest at the top, each with who wrote it, when, and its text; then a box to post as the bot, which sends at once. The channel's menu sets what agents may do there, and **Open in Discord** or **Open in Slack** opens it in the service's own app.
  - The tab's count is the drafts waiting and the mentions not yet seen, never red: they are news, not alarms.
- **Words and commands:** `:chat` in any task bar says how the accounts stand, the drafts waiting and the newest mentions; the Mac app opens the Chat tab instead. `irie chat` does the rest from a terminal (`irie chat help`):

  ```
  irie chat                         the accounts, the drafts waiting, the newest mentions
  irie chat channels [ACCOUNT]      each server's channels, and what agents may do in each
  irie chat read CHANNEL            a channel's newest messages (--before ID for older, --thread ID for a thread's)
  irie chat post CHANNEL [TEXT|-]   post (--reply ID, --thread TITLE); an agent's becomes a draft
  irie chat edit|delete CHANNEL ID  change or remove a message
  irie chat drafts                  the drafts waiting for you
  irie chat send|discard DRAFT      send an agent's draft, as it is or as you change it, or drop it
  irie chat agents CHANNEL none|draft|post   what agents may do in a channel
  irie chat agents ACCOUNT read on|off       whether agents may read an account's messages
  irie chat log                     what agents posted, edited and deleted
  irie chat add discord|slack [NAME], remove NAME, test NAME
  ```

  A CHANNEL is `#name`, a channel's ID, or either after `ACCOUNT:` when there are several accounts, with `SERVER/` before a name two servers share.
- **Plugins:** Settings › Plugins, the Plugins menu, `irie plugins` and `:plugins` show each part's state and what it lacks.

**Mentions and DMs.** While an account can listen (Discord always; Slack once its app-level token is kept), the core keeps a connection open to the service and hears each message that mentions the bot, and each DM to it. With the account's `user` set to the person's own ID there, a mention of the person counts too: Discord sends whom a message mentions even without the Message Content intent, so the bot can tell, and Slack's events carry it in the text. The core keeps them in memory and counts them until the person looks. Once Israfil (DESIGN §4.11) is in, each becomes a notice in the Mac app, at most one a channel a minute, and none while the person looks at the Chat tab. He never texts for chat: a mention is news, not a session that needs the person.

## 5. What agents may do

A post to a channel is outward-facing: it goes out at once, under the person's bot, to people who can't take it back. And chat is where prompt injection arrives: words in a message can try to turn an agent against its person. So the rules are the core's, checked on every call, and the core knows a call from inside an agent's pane by its process (`fromAgent`, as Mail's refusals do).

| | A person | An agent |
|---|---|---|
| Add, test or remove an account, see a token | yes | never |
| See the accounts and channels by name | yes | yes, unless `chat.agents` is off or the account says `agents = "none"` |
| Read messages | yes | only on an account with `agents_read = true`, each message marked as people's words, never orders |
| Post, reply, start a thread or a forum post | yes, at once | a draft the person sends or discards; on its own only in a channel set to `post` |
| Edit or delete a message | any the bot may | the drafts it wrote, and its own posts in a channel set to `post` |
| Send a draft, or change what agents may do | yes | never |

**Drafts are the default** for every account and every channel, for every person. A draft waits in the tab and in `irie chat drafts` until the person sends it, changed or not, or discards it. Its agent sees what came of it (`irie chat drafts` lists an agent's own), and may withdraw it while it waits. A draft to a DM, or that replies in one, is a draft wherever it is: agents never write to a person on their own.

**Posting on their own is the person's to open, one channel at a time**: the channel's menu in the tab, `irie chat agents CHANNEL post`, or the channel's table in config.toml. There is no way to open a whole account, and the person closes a channel again the same way, at once. In such a channel an agent's post:

- **never pings everyone:** the core refuses an agent's post, and draft, that names everyone, here or a role (Discord's `@everyone`, `@here` and `<@&ROLE>`; Slack's `<!everyone>`, `<!channel>`, `<!here>` and `<!subteam^…>`), and sends an agent's own post with no pings at all (Discord's `allowed_mentions` with nothing to parse; Slack pings only what the text spells out, which the refusal has already ruled out). A draft the person sends pings the people it names, never more;
- **is rate-limited**, far inside each service's own limits: at most one a minute in a channel, and `chat.agent_posts` an hour (10 unless set, at most 60), counted from the log, so a restart of the core starts no new count. An edit counts as a post; a delete never does;
- **is logged** in `log.jsonl` (§3) before the core answers the agent, and the tab and `irie chat log` show it.

**Reads are off until the person opens an account to agents** (`agents_read`), since every message read is words from people the person may not know. With it on, an agent reads a channel's messages through `irie chat read`, each marked as what people wrote, which the skill tells it never to follow as instructions. With it off, an agent sees the channels by name, enough to address a draft, and nothing anyone wrote, the channels' topics included.

**`chat.agents` off** leaves agents nothing: every call from an agent is refused, the skill leaves every session, and drafts waiting stay for the person.

**What this can't stop.** An agent runs as the person's own user, so one that sets out to could edit config.toml, or read the token from the Keychain with `security`, as with Mail's passwords. The rules bind an agent that goes through Irie, which is every agent that follows the skill and every agent that a message tries to steer through Irie's tools; Claude Code's own permission prompts stand between an agent and the rest. Turning the guard on those two paths is listed under Not yet.

**Agents use `irie chat`, not an MCP server.** The core is the one gate either way. The commands cost a session nothing until its agent uses them, where a server's tools would sit in every session's context, and a person reads what an agent ran. The skill says how: find the channel, write the draft with the text on standard input, check what came of it later.

## 6. A build without it

`go build -tags nochat` leaves the plugin out: `internal/chat`, `internal/core/chat.go` (`chat_off.go` stands in), `cmd/irie/chat.go` (`chat_off.go`), `internal/config/chat_on.go` (`chat_off.go`), the plugin's entry and its skill (`internal/skills/bundled_chat.go`). `hello` leaves out the `chat` op, so the Mac app shows no Chat tab. The build test and `tools/check.sh` build and vet every package with the tag among the others. A company's copy keeps the plugin by default, since its people may talk in Slack or Teams; `nochat` in its tags leaves it out.

## 7. Tests

- `internal/chat`: each connector against a fake service, an `httptest` server of the test's own that answers as the service's documentation says, never the service itself: signing in, and refusing a bad token in words; servers and channels, with threads, forums, categories and what the bot may do in each; a channel's messages and an older page; a post, a reply, a thread from a message, a forum post, an edit and a delete, with the pings each sends; empty text under an intent that's off, said once; a 429 waited out, one too long failing, and Discord's buckets; a refusal in words for a missing permission or scope. The service: drafts that outlive a restart, the log, the agents' rate counted from it, the pings refused, the token stores, the Keychain in a temporary keychain of its own.
- `internal/core`: the op, `:chat`, the plugin off; an agent's post a draft, sent by a person, refused when the agent tries to send it; an agent's reads refused, then allowed per account; an agent's own post only in a `post` channel, logged and limited; `chat.agents` off.
- `internal/plugins`, `internal/config`, `cmd/irie`: the parts' states, the settings checked, `irie chat` against a stand-in core.

## 8. Order of the work

1. This proposal.
2. The plugin with Discord: accounts, channels, reading, posting, threads, forums, drafts, the agents' rules, `irie chat`, `:chat`, the skill and the build tag.
3. Slack.
4. The Mac app's Chat tab.
5. Mentions and DMs as they come: the Gateway, Socket Mode, and their notices.

## 9. Not yet

- **Files and pictures**, reactions, embeds and Slack's blocks, slash commands, and voice.
- **Notices through Israfil**, which waits for him to land.
- **The guard** refusing an agent's own edit of `chat.*` in config.toml and its reading of a chat token through `security`.
- **Matrix, Mattermost, Telegram and Teams**, a connector each (§2).
- The terminal sidebar and the phone show `:chat`'s words, not the tab.
