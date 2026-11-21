import AppKit
import SwiftUI

// The chat plugin's side of the app (docs/proposals/chat.md): the core
// reads Discord and Slack through bots of the person's own, posts, and
// keeps the drafts agents write; the app only draws, in the right sidebar's
// Chat tab, a pane of the app's own view (ChatViews.swift). A draft goes
// out only at the person's Send, here or in a terminal.

// MARK: Wire

struct ChatArgs: Encodable {
    var `do`: String?
    var account: String?
    var channel: String?
    var id: String?
    var text: String?
    var reply: String?
    var thread: String?
    var before: String?
    var limit: Int?
    var agents: String?
    var read: Bool?
    var refresh: Bool?
    var new: ChatNew?
}

struct ChatNew: Encodable {
    var kind: String
    var name: String?
    var token: String
    var app: String?
    var user: String?
}

struct ChatResult: Decodable {
    var accounts: [ChatAccount]?
    var drafts: [ChatDraft]?
    var mentions: [ChatMention]?
    var unseen: Int?
    var spaces: [ChatSpace]?
    var channel: ChatChannel?
    var messages: [ChatMessage]?
    var more: Bool?
    var note: String?
    var message: ChatMessage?
    var draft: ChatDraft?
    var manifest: String?
    var text: String?
}

/// One account and how it stands, as the core's last look found it.
struct ChatAccount: Decodable, Equatable, Identifiable {
    var name: String
    var kind: String
    var service: String
    /// Which of the agents' six colours marks it.
    var colour: Int
    /// ok, needs, error or unknown.
    var state: String
    var problem: String?
    var bot: String?
    /// What agents may do in its channels unless a channel says otherwise.
    var agents: String
    var agentsRead: Bool
    /// The service leaves people's message text out for this bot; fix says
    /// what turns it on.
    var hidden: Bool?
    var fix: String?
    /// The core hears its mentions and DMs as they come; deaf says why not.
    var listening: Bool?
    var deaf: String?

    enum CodingKeys: String, CodingKey {
        case name, kind, service, colour, state, problem, bot, agents, hidden, fix, listening, deaf
        case agentsRead = "agents_read"
    }

    var id: String { name }

    var failing: Bool { state == "error" || state == "needs" }

    /// Its service's mark, which tells a Discord account from a Slack one
    /// without its colour.
    var symbol: String { kind == "slack" ? "number.square.fill" : "bubble.left.and.bubble.right.fill" }
}

/// A Discord server or a Slack workspace, with its channels.
struct ChatSpace: Decodable, Equatable, Identifiable {
    var id: String
    var name: String
    var account: String
    var problem: String?
    var channels: [ChatChannel]
}

/// One channel or thread.
struct ChatChannel: Decodable, Equatable, Identifiable, Hashable {
    var id: String
    var name: String
    /// text, announcement, forum, voice, thread, dm or group.
    var kind: String
    var account: String
    var space: String?
    var parent: String?
    var category: String?
    var topic: String?
    /// What agents may do here: none, draft or post.
    var agents: String
    var read: Bool
    var post: Bool
    var why: String?
    var link: String?

    /// Its kind's mark.
    var symbol: String {
        switch kind {
        case "announcement": return "megaphone"
        case "forum": return "text.bubble"
        case "voice": return "speaker.wave.2"
        case "thread": return "arrow.turn.down.right"
        case "dm": return "person"
        case "group": return "person.2"
        default: return "number"
        }
    }

    /// Its name as a chat writes it: #general, or a DM's as it is.
    var shown: String { kind == "dm" || kind == "group" || kind == "thread" ? name : "#" + name }
}

/// One message.
struct ChatMessage: Decodable, Equatable, Identifiable {
    var id: String
    var channel: String
    var author: String
    var bot: Bool?
    var mine: Bool?
    var time: Date
    var edited: Date?
    var text: String
    var hidden: Bool?
    var reply: String?
    var thread: String?
    var replies: Int?
    var files: Int?
    var link: String?
    /// The session that posted it, when an agent did through Irie.
    var agent: String?
}

/// A message that mentions the bot or the person, or a DM to the bot,
/// heard as it came.
struct ChatMention: Decodable, Equatable, Identifiable {
    var account: String
    var channel: String
    var channelName: String
    var message: String
    var author: String
    var text: String
    var hidden: Bool?
    var dm: Bool?
    /// It mentions the person, not the bot.
    var you: Bool?
    var time: Date
    var link: String?

    enum CodingKeys: String, CodingKey {
        case account, channel, message, author, text, hidden, dm, you, time, link
        case channelName = "channel_name"
    }

    var id: String { account + "/" + message }
}

/// A post an agent wrote, for the person to send or discard.
struct ChatDraft: Decodable, Equatable, Identifiable {
    var id: String
    var account: String
    var channel: String
    var channelName: String
    var space: String?
    var reply: String?
    var thread: String?
    var text: String
    var session: String
    var sessionName: String?
    var made: Date
    /// waiting, sent or discarded.
    var state: String
    var link: String?
    var error: String?
    var ended: Date?
    var by: String?

    enum CodingKeys: String, CodingKey {
        case id, account, channel, space, reply, thread, text, session, made, state, link, error, ended, by
        case channelName = "channel_name"
        case sessionName = "session_name"
    }

    var waiting: Bool { state == "waiting" }

    /// Where it goes, in a line: #general in Acme, a thread “Week 1”.
    var whereTo: String {
        var s = "#" + channelName
        if let sp = space, !sp.isEmpty { s += " in " + sp }
        if let t = thread, !t.isEmpty { s += ", a thread “\(t)”" } else if reply != nil { s += ", a reply" }
        return s
    }

    var from: String { sessionName?.isEmpty == false ? sessionName! : session }
}

// MARK: The model

/// The Chat tab's state: the accounts and the drafts as the core last said,
/// each account's channels once looked at, and the channel open with its
/// messages. While the tab shows it asks the core every few seconds, which
/// answers from memory: the services see a look at most twice a minute.
@MainActor
@Observable
final class ChatModel {
    let client: CoreClient

    init(client: CoreClient) {
        self.client = client
    }

    private(set) var accounts: [ChatAccount] = []
    private(set) var drafts: [ChatDraft] = []
    /// The mentions and DMs heard as they came, newest first, and how many
    /// came since the person last looked.
    private(set) var mentions: [ChatMention] = []
    private(set) var unseen = 0
    /// Each account's servers and channels, by its name.
    private(set) var spaces: [String: [ChatSpace]] = [:]
    /// The one account shown, or nil for every one.
    var only: String?
    /// The channel open in place of the list, its messages oldest first.
    private(set) var open: ChatChannel?
    private(set) var messages: [ChatMessage] = []
    private(set) var note: String?
    private(set) var more = false
    /// What the box under an open channel holds.
    var composing = ""
    private(set) var sending = false
    /// A draft being edited before it is sent, and its words.
    var editing: String?
    var edited = ""
    var problem: String?
    var adding = false
    private(set) var loaded = false

    @ObservationIgnored private var isFixture = false

    var waiting: [ChatDraft] { drafts.filter(\.waiting) }

    func account(_ name: String) -> ChatAccount? { accounts.first { $0.name == name } }

    private func call(_ args: ChatArgs, timeout: TimeInterval = 70) async throws -> ChatResult {
        do {
            return try await client.call("chat", args, timeout: timeout, as: ChatResult.self)
        } catch let e as WireError {
            throw e
        } catch {
            throw WireError(code: "offline", msg: "\(Brand.name)’s core isn’t answering now. Drafts wait for it.")
        }
    }

    private func take(_ r: ChatResult) {
        if let a = r.accounts, a != accounts { accounts = a }
        if let d = r.drafts, d != drafts { drafts = d }
        // Every answer to the person carries the mentions, an empty list
        // left out.
        if r.accounts != nil {
            let m = r.mentions ?? []
            if m != mentions { mentions = m }
            if (r.unseen ?? 0) != unseen { unseen = r.unseen ?? 0 }
        }
        if let o = only, !accounts.contains(where: { $0.name == o }) { only = nil }
        loaded = true
    }

    /// Reads the accounts and the drafts again: the core answers from
    /// memory.
    func refresh() async {
        guard !isFixture else { return }
        guard let r = try? await call(ChatArgs(), timeout: 10) else { return }
        take(r)
    }

    /// Reads an account's servers and channels, from the core's memory
    /// unless again is set.
    func channels(_ account: String, again: Bool = false) async {
        guard !isFixture else { return }
        do {
            let r = try await call(ChatArgs(do: "channels", account: account, refresh: again))
            take(r)
            spaces[account] = r.spaces ?? []
        } catch {
            problem = error.localizedDescription
        }
    }

    /// Opens a channel in place of the list.
    func show(_ ch: ChatChannel) {
        open = ch
        messages = []
        note = nil
        more = false
        composing = ""
        problem = nil
        Task { await read() }
    }

    /// Goes back from a channel to the list.
    func close() {
        open = nil
        messages = []
        note = nil
        composing = ""
    }

    /// Reads the open channel's newest messages again.
    func read(again: Bool = false) async {
        guard let ch = open, !isFixture else { return }
        do {
            let r = try await call(ChatArgs(do: "read", account: ch.account, channel: ch.id, refresh: again))
            guard open?.id == ch.id else { return }
            take(r)
            if let m = r.messages, m != messages { messages = m }
            note = r.note
            more = r.more ?? false
        } catch {
            guard open?.id == ch.id else { return }
            problem = error.localizedDescription
        }
    }

    /// Reads the messages before the oldest shown.
    func older() {
        guard let ch = open, let first = messages.first, !isFixture else { return }
        Task {
            do {
                let r = try await call(ChatArgs(do: "read", account: ch.account, channel: ch.id, before: first.id))
                guard open?.id == ch.id else { return }
                messages = (r.messages ?? []) + messages
                more = r.more ?? false
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Posts what the box holds to the open channel, as the bot: the
    /// person's own post goes at once.
    func post() {
        guard let ch = open, !isFixture else { return }
        let text = composing.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        sending = true
        problem = nil
        Task {
            defer { sending = false }
            do {
                take(try await call(ChatArgs(do: "post", account: ch.account, channel: ch.id, text: text)))
                composing = ""
                await read(again: true)
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Sends a draft, as the agent wrote it or as the person changed it.
    func send(_ d: ChatDraft, text: String? = nil) {
        guard !isFixture else { return }
        sending = true
        problem = nil
        Task {
            defer { sending = false }
            do {
                take(try await call(ChatArgs(do: "send", id: d.id, text: text)))
                if editing == d.id { editing = nil }
            } catch {
                problem = error.localizedDescription
                await refresh()
            }
        }
    }

    func discard(_ d: ChatDraft) {
        guard !isFixture else { return }
        Task {
            do {
                take(try await call(ChatArgs(do: "discard", id: d.id)))
                if editing == d.id { editing = nil }
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Marks the mentions seen.
    func markSeen() {
        guard !isFixture, unseen > 0 else { return }
        Task {
            if let r = try? await call(ChatArgs(do: "seen"), timeout: 10) { take(r) }
        }
    }

    /// Opens a mention's channel, when the list has it, else the message in
    /// its service's own app.
    func showMention(_ m: ChatMention) {
        if let ch = spaces[m.account]?.flatMap(\.channels).first(where: { $0.id == m.channel }) {
            show(ch)
        } else {
            visit(m.link)
        }
        markSeen()
    }

    /// Starts changing a draft's words before it is sent.
    func edit(_ d: ChatDraft) {
        editing = d.id
        edited = d.text
    }

    /// Says what agents may do in a channel: none, draft or post.
    func setAgents(_ ch: ChatChannel, _ mode: String) {
        guard !isFixture else { return }
        Task {
            do {
                take(try await call(ChatArgs(do: "agents", account: ch.account, channel: ch.id, agents: mode)))
                await channels(ch.account)
                if open?.id == ch.id, let fresh = spaces[ch.account]?.flatMap(\.channels).first(where: { $0.id == ch.id }) { open = fresh }
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Says whether agents may read an account's messages.
    func setRead(_ a: ChatAccount, _ on: Bool) {
        guard !isFixture else { return }
        Task {
            do {
                take(try await call(ChatArgs(do: "agents", account: a.name, read: on)))
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Adds an account once its token signs in: the core keeps the token in
    /// the Keychain. It answers why not, or nil.
    func add(_ n: ChatNew) async -> String? {
        do {
            let r = try await call(ChatArgs(do: "add", new: n), timeout: 90)
            take(r)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    func remove(_ a: ChatAccount) {
        Task {
            do {
                take(try await call(ChatArgs(do: "remove", account: a.name)))
                spaces[a.name] = nil
                if open?.account == a.name { close() }
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    func test(_ a: ChatAccount) {
        Task {
            do {
                let r = try await call(ChatArgs(do: "test", account: a.name))
                take(r)
                problem = r.text
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// The steps and the link that make a Slack app from Irie's manifest.
    func slackSteps() async -> String? {
        try? await call(ChatArgs(do: "manifest"), timeout: 10).text
    }

    /// Opens an address the person clicked in the browser.
    func visit(_ link: String?) {
        guard let link, let url = URL(string: link), url.scheme == "https" else { return }
        NSWorkspace.shared.open(url)
    }
}

extension ChatModel {
    /// The snapshots' chat (Snapshot.swift): a Discord server and a Slack
    /// workspace, two drafts waiting and a channel open when reading.
    func loadFixture(reading: Bool = false, empty: Bool = false) {
        isFixture = true
        loaded = true
        problem = nil
        editing = nil
        if empty {
            accounts = []
            drafts = []
            mentions = []
            unseen = 0
            spaces = [:]
            close()
            return
        }
        let now = Date()
        accounts = [
            ChatAccount(name: "acme", kind: "discord", service: "Discord", colour: 0, state: "ok", bot: "Acme Helper", agents: "draft", agentsRead: false, hidden: true,
                        fix: "Discord leaves people's message text out for this bot until its Message Content intent is on: Developer Portal › the application › Bot › Privileged Gateway Intents › Message Content Intent."),
            ChatAccount(name: "team", kind: "slack", service: "Slack", colour: 3, state: "ok", bot: "acme-bot", agents: "draft", agentsRead: true),
        ]
        drafts = [
            ChatDraft(id: "dk3x9mfa2b", account: "acme", channel: "202", channelName: "announcements", space: "Acme",
                      text: "Version 2 is out: a faster start, a darker theme and the new Chat tab.\nThanks to everyone who tried the preview!",
                      session: "s0000000000a1", sessionName: "Release notes", made: now.addingTimeInterval(-300), state: "waiting"),
            ChatDraft(id: "dq7wn4e8hc", account: "team", channel: "C1", channelName: "general", space: "Acme", reply: "1700000003.000300",
                      text: "The build is green again; the flaky test was a clock in the fake server.",
                      session: "s0000000000b2", sessionName: "Fix the build", made: now.addingTimeInterval(-60), state: "waiting"),
        ]
        func ch(_ id: String, _ name: String, _ kind: String, _ account: String, agents: String = "draft", category: String? = nil, parent: String? = nil, post: Bool = true, why: String? = nil) -> ChatChannel {
            ChatChannel(id: id, name: name, kind: kind, account: account, space: account == "acme" ? "100" : "T1", parent: parent, category: category,
                        agents: agents, read: true, post: post, why: why, link: "https://example.com/\(id)")
        }
        spaces = [
            "acme": [ChatSpace(id: "100", name: "Acme", account: "acme", channels: [
                ch("207", "readme", "text", "acme", post: false, why: "The bot lacks Send Messages here."),
                ch("204", "ideas", "forum", "acme"),
                ch("201", "general", "text", "acme", category: "Talk"),
                ch("301", "plans", "thread", "acme", category: "Talk", parent: "201"),
                ch("202", "announcements", "announcement", "acme", category: "Talk"),
                ch("203", "bots", "text", "acme", agents: "post", category: "Talk"),
                ch("205", "voice", "voice", "acme", agents: "none", category: "Talk"),
            ])],
            "team": [ChatSpace(id: "T1", name: "Acme", account: "team", channels: [
                ch("C1", "general", "text", "team"),
                ch("C3", "standup", "text", "team", agents: "post"),
                ch("D1", "DM with Alice", "dm", "team"),
            ])],
        ]
        mentions = [
            ChatMention(account: "team", channel: "D1", channelName: "DM with Alice", message: "1700000006.000600", author: "Alice",
                        text: "Can you look at the release checklist before lunch?", dm: true, time: now.addingTimeInterval(-240), link: "https://example.com/D1"),
            ChatMention(account: "acme", channel: "201", channelName: "general", message: "403", author: "Alice",
                        text: "Hi @Acme Helper, when does version 2 come out?", time: now.addingTimeInterval(-1800), link: "https://example.com/403"),
        ]
        unseen = 1
        if reading {
            open = spaces["acme"]![0].channels[2]
            note = accounts[0].fix
            messages = [
                ChatMessage(id: "401", channel: "201", author: "Acme Helper", bot: true, mine: true, time: now.addingTimeInterval(-7200), text: "Welcome to Acme! Say hi, and ask anything in #help."),
                ChatMessage(id: "402", channel: "201", author: "bob", time: now.addingTimeInterval(-3600), text: "", hidden: true),
                ChatMessage(id: "403", channel: "201", author: "Alice", time: now.addingTimeInterval(-1800), text: "Hi @Acme Helper, when does version 2 come out?", thread: "403", replies: 4),
                ChatMessage(id: "404", channel: "201", author: "Acme Helper", bot: true, mine: true, time: now.addingTimeInterval(-600), text: "This week: the notes are in #announcements.", agent: "Release notes"),
            ]
            more = true
        } else {
            close()
        }
    }
}

extension Store {
    /// The Chat tab's pane, while its part is on (PluginPanes.swift).
    var chatPane: PluginPaneInfo? { panes.first { $0.kind == "chat" } }

    /// Shows the Chat tab, as :chat asks.
    func showChat() {
        guard let p = chatPane else { return }
        if !(side.open && side.tab == .plugin && side.pane == p.id) { openPane(p.id) }
        Task { await chat.refresh() }
    }
}
