import AppKit
import SwiftUI

// The right sidebar's Chat tab (docs/proposals/chat.md): the person's
// Discord servers and Slack workspaces beside their sessions. From the top:
// the accounts, each in an agent's colour with its service's mark; the
// drafts agents wrote, waiting for Send; each account's channels, with what
// agents may do in each; and a channel opened in place of the list, its
// messages and a box to post as the bot. It is a pane of the app's own
// view, which shows while the chat plugin is on (PluginPanes.swift), drawn
// from Roots' parts: chips, cards, state marks, soft and icon buttons
// (components.md).

@MainActor
struct ChatTab: View {
    @Environment(Store.self) private var store

    var body: some View {
        let chat = store.chat
        Group {
            if !chat.loaded {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if chat.accounts.isEmpty {
                ChatWelcome()
            } else if let ch = chat.open {
                ChatChannelView(channel: ch)
            } else {
                ChatHome()
            }
        }
        .sheet(isPresented: Binding(get: { chat.adding }, set: { chat.adding = $0 })) {
            ChatAddAccount()
        }
        // Every few seconds while the tab shows: the core answers from
        // memory, and asks a service at most twice a minute.
        .task {
            while !Task.isCancelled {
                await chat.refresh()
                if chat.open != nil { await chat.read() }
                try? await Task.sleep(nanoseconds: 5_000_000_000)
            }
        }
    }

    /// When something happened, as a chat says it: 5 min ago, 2 h ago, or
    /// the day and time.
    static func when(_ d: Date, now: Date = Date()) -> String {
        let s = Int(now.timeIntervalSince(d))
        switch s {
        case ..<60: return "just now"
        case ..<3600: return "\(s / 60) min ago"
        case ..<(12 * 3600): return "\(s / 3600) h ago"
        default:
            let f = DateFormatter()
            f.dateFormat = "d MMM HH:mm"
            return f.string(from: d)
        }
    }

    /// What agents may do in a channel, as a mark and in words, so colour
    /// never says it alone.
    static func agents(_ mode: String) -> (symbol: String, words: String) {
        switch mode {
        case "post": return ("paperplane.fill", "Agents post here on their own")
        case "none": return ("nosign", "Agents do nothing here")
        default: return ("pencil", "Agents write drafts for you here")
        }
    }
}

/// The tab before any account: what Chat is, and Add Account.
@MainActor
private struct ChatWelcome: View {
    @Environment(Store.self) private var store

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: 26))
                .foregroundStyle(Theme.muted)
            Text("Chat")
                .font(Theme.font(14, .bold))
                .foregroundStyle(Theme.ink)
            Text("Your Discord servers and Slack workspaces beside your sessions, through a bot of your own. Agents write drafts here for you to send.")
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            Button("Add Account…") { store.chat.adding = true }
                .buttonStyle(SoftButton(prominent: true))
                .padding(.top, 4)
        }
        .frame(maxWidth: 280)
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// The list: the accounts, the drafts waiting, and each account's
/// channels.
@MainActor
private struct ChatHome: View {
    @Environment(Store.self) private var store

    var body: some View {
        let chat = store.chat
        VStack(spacing: 0) {
            ChatAccountStrip()
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    if let p = chat.problem {
                        ChatProblem(text: p)
                    }
                    if !chat.waiting.isEmpty {
                        ChatHead(title: "Waiting for you")
                        ForEach(chat.waiting) { ChatDraftCard(draft: $0) }
                    }
                    if !chat.mentions.isEmpty {
                        ChatHead(title: chat.unseen > 0 ? "Mentions · \(chat.unseen) new" : "Mentions",
                                 trailing: chat.unseen > 0 ? AnyView(Button("Mark Seen") { chat.markSeen() }
                                     .buttonStyle(.plain)
                                     .font(Theme.font(11.5, .semibold))
                                     .foregroundStyle(Theme.muted)) : nil)
                        VStack(spacing: 1) {
                            ForEach(chat.mentions.prefix(8)) { ChatMentionRow(mention: $0) }
                        }
                        .padding(4)
                        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.card))
                    }
                    ForEach(chat.accounts.filter { chat.only == nil || chat.only == $0.name }) { a in
                        ChatChannelList(account: a)
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

/// A section's head, quieter than its rows.
private struct ChatHead: View {
    let title: String
    var trailing: AnyView? = nil

    var body: some View {
        HStack {
            Text(title)
                .font(Theme.font(Theme.headSize, .heavy))
                .tracking(Theme.headTracking)
                .foregroundStyle(Theme.muted)
            Spacer()
            if let trailing { trailing }
        }
        .padding(.horizontal, 4)
        .padding(.top, Theme.headGap)
    }
}

/// Why the last call failed, in the core's words.
private struct ChatProblem: View {
    let text: String

    var body: some View {
        Text(text)
            .font(Theme.font(12))
            .foregroundStyle(Theme.needs)
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 4)
            .accessibilityLabel("Problem: \(text)")
    }
}

/// The accounts, a chip each: a click shows one alone, and its menu what
/// agents may do there, a fresh sign-in or its removal.
@MainActor
private struct ChatAccountStrip: View {
    @Environment(Store.self) private var store

    var body: some View {
        let chat = store.chat
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                if chat.accounts.count > 1 {
                    chip(nil, on: chat.only == nil)
                }
                ForEach(chat.accounts) { a in
                    chip(a, on: chat.only == a.name)
                        .contextMenu { ChatAccountMenu(account: a) }
                }
                Button { chat.adding = true } label: { Image(systemName: "plus") }
                    .buttonStyle(IconButtonStyle())
                    .help("Add a Discord bot or a Slack app of yours")
                    .accessibilityLabel("Add an account")
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
        }
    }

    private func chip(_ a: ChatAccount?, on: Bool) -> some View {
        let chat = store.chat
        return Button {
            chat.only = a.flatMap { chat.only == $0.name ? nil : $0.name }
        } label: {
            HStack(spacing: 4) {
                if let a {
                    Image(systemName: a.failing ? "exclamationmark.triangle.fill" : a.symbol)
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(a.failing ? Theme.needs : Theme.agent(slot: a.colour))
                }
                Text(a?.name ?? "All")
                    .font(Theme.font(11.5, on ? .bold : .medium))
                    .foregroundStyle(a.map { Theme.agentInk(slot: $0.colour) } ?? Theme.ink)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(Capsule().fill(a.map { Theme.agentWash(slot: $0.colour) } ?? Theme.field))
            .overlay(Capsule().strokeBorder(on ? (a.map { Theme.agent(slot: $0.colour) } ?? Theme.ink) : Theme.edge, lineWidth: on ? 1.5 : 1))
        }
        .buttonStyle(.plain)
        .help(a.map { "\($0.service)\($0.bot.map { ", as \($0)" } ?? "")" + ($0.problem.map { "\n\($0)" } ?? "") } ?? "Every account's channels")
    }
}

/// An account's menu: whether agents may read it, a fresh sign-in, its
/// removal.
@MainActor
private struct ChatAccountMenu: View {
    @Environment(Store.self) private var store
    let account: ChatAccount

    var body: some View {
        let chat = store.chat
        Toggle("Agents May Read Its Messages", isOn: Binding(get: { account.agentsRead }, set: { chat.setRead(account, $0) }))
        Button("Sign In Again") { chat.test(account) }
        Divider()
        Button("Remove \(account.name)…") { chat.remove(account) }
    }
}

/// A draft an agent wrote: where it goes, who wrote it and its words, with
/// Send, Edit and Discard. Nothing an agent drafts goes out without Send.
@MainActor
private struct ChatDraftCard: View {
    @Environment(Store.self) private var store
    let draft: ChatDraft

    var body: some View {
        let chat = store.chat
        let a = chat.account(draft.account)
        let editing = chat.editing == draft.id
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 7) {
                StateMark(look: .waiting, size: 10, tint: a.map { Theme.agent(slot: $0.colour) })
                    .padding(.top, 3)
                VStack(alignment: .leading, spacing: 1) {
                    Text(draft.whereTo)
                        .font(Theme.font(12.5, .semibold))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(2)
                    Text("From \(draft.from) · \(ChatTab.when(draft.made))")
                        .font(Theme.font(11))
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 4)
                if let a {
                    Image(systemName: a.symbol)
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(Theme.agent(slot: a.colour))
                        .help("\(a.name), \(a.service)")
                }
            }
            if editing {
                TextEditor(text: Bindable(chat).edited)
                    .font(Theme.font(12.5))
                    .frame(minHeight: 70, maxHeight: 180)
                    .scrollContentBackground(.hidden)
                    .padding(4)
                    .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field))
                    .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(Theme.accent, lineWidth: 1.5))
            } else {
                Text(draft.text)
                    .font(Theme.font(12.5))
                    .foregroundStyle(Theme.ink)
                    .textSelection(.enabled)
                    .lineLimit(10)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let e = draft.error {
                Text("It didn’t go: \(e)")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.needs)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 6) {
                if editing {
                    Button("Send Changed") { chat.send(draft, text: chat.edited) }
                        .buttonStyle(SoftButton(prominent: true))
                        .disabled(chat.sending || chat.edited.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    Button("Cancel") { chat.editing = nil }
                        .buttonStyle(SoftButton())
                } else {
                    Button("Send") { chat.send(draft) }
                        .buttonStyle(SoftButton(prominent: true))
                        .disabled(chat.sending)
                        .help("Post it, as the bot, to \(draft.whereTo)")
                    Button("Edit") { chat.edit(draft) }
                        .buttonStyle(SoftButton())
                }
                Spacer()
                Button { chat.discard(draft) } label: { Image(systemName: "trash") }
                    .buttonStyle(IconButtonStyle())
                    .help("Discard the draft: its agent sees that you did")
                    .accessibilityLabel("Discard the draft")
            }
            .padding(.top, 1)
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.card))
        .overlay(alignment: .leading) {
            if let a {
                RoundedRectangle(cornerRadius: 2).fill(Theme.agent(slot: a.colour)).frame(width: 3).padding(.vertical, 8)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("A draft to \(draft.whereTo), from \(draft.from)")
    }
}

/// A mention or DM heard as it came: who, where, when and its words; a click
/// opens its channel.
@MainActor
private struct ChatMentionRow: View {
    @Environment(Store.self) private var store
    let mention: ChatMention
    @State private var hovering = false

    var body: some View {
        let chat = store.chat
        let a = chat.account(mention.account)
        Button { chat.showMention(mention) } label: {
            HStack(alignment: .top, spacing: 7) {
                Image(systemName: mention.dm == true ? "envelope" : "at")
                    .font(.system(size: 10.5, weight: .semibold))
                    .foregroundStyle(a.map { Theme.agent(slot: $0.colour) } ?? Theme.muted)
                    .frame(width: 14)
                    .padding(.top, 2)
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 4) {
                        Text(mention.author)
                            .font(Theme.font(12, .semibold))
                            .foregroundStyle(Theme.ink)
                            .lineLimit(1)
                        Text(mention.dm == true ? "· " + mention.channelName : "in #" + mention.channelName)
                            .font(Theme.font(11))
                            .foregroundStyle(Theme.muted)
                            .lineLimit(1)
                        Spacer(minLength: 2)
                        Text(ChatTab.when(mention.time))
                            .font(Theme.font(10.5))
                            .foregroundStyle(Theme.muted)
                    }
                    Text(mention.hidden == true ? "Hidden from the bot" : mention.text)
                        .font(mention.hidden == true ? Theme.font(12).italic() : Theme.font(12))
                        .foregroundStyle(mention.hidden == true ? Theme.muted : Theme.ink)
                        .lineLimit(2)
                    if mention.you == true {
                        Text("Mentions you")
                            .font(Theme.font(10, .bold))
                            .foregroundStyle(Theme.accentInk)
                    }
                }
            }
            .padding(6)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(hovering ? Theme.hover : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help("Open it")
        .accessibilityLabel("\(mention.author)\(mention.dm == true ? ", a DM" : ", in #\(mention.channelName)"): \(mention.hidden == true ? "hidden from the bot" : mention.text)")
    }
}

/// An account's servers and their channels, read once the account shows,
/// each channel with its kind and what agents may do there.
@MainActor
private struct ChatChannelList: View {
    @Environment(Store.self) private var store
    let account: ChatAccount

    var body: some View {
        let chat = store.chat
        VStack(alignment: .leading, spacing: 2) {
            ChatHead(title: account.name + " · " + account.service)
            if account.failing, let p = account.problem {
                ChatProblem(text: p)
            }
            if let spaces = chat.spaces[account.name] {
                ForEach(spaces) { s in
                    if spaces.count > 1 || s.name != account.name {
                        Text(s.name)
                            .font(Theme.font(12, .semibold))
                            .foregroundStyle(Theme.ink)
                            .padding(.horizontal, 4)
                            .padding(.top, 4)
                    }
                    if let p = s.problem {
                        ChatProblem(text: p)
                    }
                    ForEach(Array(s.channels.enumerated()), id: \.element.id) { i, ch in
                        if let cat = ch.category, ch.kind != "thread", i == 0 || s.channels[i - 1].category != cat {
                            Text(cat.uppercased())
                                .font(Theme.font(10, .bold))
                                .tracking(0.4)
                                .foregroundStyle(Theme.muted)
                                .padding(.horizontal, 6)
                                .padding(.top, 6)
                        }
                        ChatChannelRow(channel: ch)
                    }
                }
            } else if !account.failing {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.mini)
                    Text("Reading its channels…").font(Theme.font(11.5)).foregroundStyle(Theme.muted)
                }
                .padding(.horizontal, 6)
            }
        }
        .task(id: account.name) { await chat.channels(account.name) }
    }
}

/// One channel: its mark and name, and what agents may do there; a click
/// opens it, and its menu says what agents may do.
@MainActor
private struct ChatChannelRow: View {
    @Environment(Store.self) private var store
    let channel: ChatChannel
    @State private var hovering = false

    var body: some View {
        let chat = store.chat
        let agents = ChatTab.agents(channel.agents)
        Button { chat.show(channel) } label: {
            HStack(spacing: 7) {
                Image(systemName: channel.symbol)
                    .font(.system(size: 10.5))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 14)
                Text(channel.name)
                    .font(Theme.font(12.5))
                    .foregroundStyle(channel.read ? Theme.ink : Theme.muted)
                    .lineLimit(1)
                Spacer(minLength: 4)
                Image(systemName: agents.symbol)
                    .font(.system(size: 9.5))
                    .foregroundStyle(channel.agents == "post" ? Theme.accentInk : Theme.muted)
                    .opacity(channel.agents == "draft" && !hovering ? 0.45 : 1)
                    .help(agents.words)
                    .accessibilityLabel(agents.words)
            }
            .padding(.leading, channel.kind == "thread" ? 22 : 6)
            .padding(.trailing, 6)
            .padding(.vertical, 4)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(hovering ? Theme.hover : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(channel.why ?? channel.topic ?? channel.shown)
        .contextMenu { ChatAgentsMenu(channel: channel) }
        .accessibilityLabel("\(channel.shown), \(agents.words)")
    }
}

/// What agents may do in a channel, the person's to say: nothing, drafts
/// for them, or posts of their own.
@MainActor
private struct ChatAgentsMenu: View {
    @Environment(Store.self) private var store
    let channel: ChatChannel

    var body: some View {
        let chat = store.chat
        Picker("Agents", selection: Binding(get: { channel.agents }, set: { chat.setAgents(channel, $0) })) {
            Text("Write Drafts for You").tag("draft")
            Text("Post on Their Own").tag("post")
            Text("Nothing").tag("none")
        }
        if let link = channel.link {
            Divider()
            Button("Open in \(chat.account(channel.account)?.service ?? "Its App")") { chat.visit(link) }
        }
    }
}

/// A channel opened in place of the list: its messages, oldest at the top,
/// and a box that posts as the bot.
@MainActor
private struct ChatChannelView: View {
    @Environment(Store.self) private var store
    let channel: ChatChannel

    var body: some View {
        let chat = store.chat
        let a = chat.account(channel.account)
        VStack(spacing: 0) {
            HStack(spacing: 6) {
                Button { chat.close() } label: { Image(systemName: "chevron.left") }
                    .buttonStyle(IconButtonStyle())
                    .help("Back to the channels")
                    .accessibilityLabel("Back")
                Image(systemName: channel.symbol)
                    .font(.system(size: 11))
                    .foregroundStyle(a.map { Theme.agent(slot: $0.colour) } ?? Theme.muted)
                Text(channel.name)
                    .font(Theme.font(13, .semibold))
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                Spacer(minLength: 4)
                Menu {
                    ChatAgentsMenu(channel: channel)
                    Divider()
                    Button("Read Again") { Task { await chat.read(again: true) } }
                } label: {
                    Image(systemName: ChatTab.agents(channel.agents).symbol)
                        .font(.system(size: 11))
                }
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                .help(ChatTab.agents(channel.agents).words + ": click to change")
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            if let note = chat.note {
                HStack(alignment: .top, spacing: 6) {
                    StateMark(look: .needs, size: 10).padding(.top, 2)
                    Text(note)
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.ink)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(9)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.needsTint))
                .padding(.horizontal, 10)
                .padding(.bottom, 4)
            }
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 10) {
                        if chat.more {
                            Button("Older Messages") { chat.older() }
                                .buttonStyle(SoftButton())
                                .frame(maxWidth: .infinity)
                        }
                        if let p = chat.problem {
                            ChatProblem(text: p)
                        }
                        ForEach(chat.messages) { ChatMessageRow(message: $0, account: a) }
                        Color.clear.frame(height: 1).id("end")
                    }
                    .padding(10)
                }
                .onChange(of: chat.messages.last?.id) { proxy.scrollTo("end", anchor: .bottom) }
                .onAppear { proxy.scrollTo("end", anchor: .bottom) }
            }
            ChatComposer(channel: channel, bot: a?.bot)
        }
    }
}

/// One message: who wrote it, when, and its words, or that the service
/// hides them from the bot.
private struct ChatMessageRow: View {
    let message: ChatMessage
    let account: ChatAccount?

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 5) {
                Text(message.author)
                    .font(Theme.font(12, .bold))
                    .foregroundStyle(message.mine == true ? (account.map { Theme.agentInk(slot: $0.colour) } ?? Theme.ink) : Theme.ink)
                    .lineLimit(1)
                if message.bot == true {
                    Text("BOT")
                        .font(Theme.font(8.5, .heavy))
                        .foregroundStyle(Theme.muted)
                        .padding(.horizontal, 3)
                        .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(Theme.edge))
                }
                Text(ChatTab.when(message.time))
                    .font(Theme.font(10.5))
                    .foregroundStyle(Theme.muted)
                if message.edited != nil {
                    Text("edited").font(Theme.font(10.5)).foregroundStyle(Theme.muted)
                }
            }
            if message.hidden == true {
                Text("Hidden from the bot")
                    .font(Theme.font(12).italic())
                    .foregroundStyle(Theme.muted)
            } else if !message.text.isEmpty {
                Text(message.text)
                    .font(Theme.font(12.5))
                    .foregroundStyle(Theme.ink)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            } else if let n = message.files, n > 0 {
                Text(n == 1 ? "A file" : "\(n) files")
                    .font(Theme.font(12).italic())
                    .foregroundStyle(Theme.muted)
            }
            HStack(spacing: 8) {
                if let n = message.replies, n > 0 {
                    Label(n == 1 ? "1 reply" : "\(n) replies", systemImage: "arrowshape.turn.up.left")
                        .font(Theme.font(10.5))
                        .foregroundStyle(Theme.muted)
                }
                if let agent = message.agent {
                    Label("Posted by \(agent)", systemImage: "sparkles")
                        .font(Theme.font(10.5))
                        .foregroundStyle(Theme.muted)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// The box under a channel, which posts at once as the bot: the person's
/// own post, unlike an agent's draft.
@MainActor
private struct ChatComposer: View {
    @Environment(Store.self) private var store
    let channel: ChatChannel
    let bot: String?
    @FocusState private var focused: Bool

    var body: some View {
        let chat = store.chat
        let empty = chat.composing.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        VStack(alignment: .leading, spacing: 4) {
            if !channel.post {
                Text(channel.why ?? "The bot can’t post here.")
                    .font(Theme.font(11))
                    .foregroundStyle(Theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                HStack(alignment: .bottom, spacing: 6) {
                    TextField("Message \(channel.shown)\(bot.map { " as \($0)" } ?? "")", text: Bindable(chat).composing, axis: .vertical)
                        .textFieldStyle(.plain)
                        .font(Theme.font(12.5))
                        .lineLimit(1...6)
                        .focused($focused)
                        .onSubmit { chat.post() }
                        .accessibilityLabel("A message to \(channel.shown)")
                    Button { chat.post() } label: {
                        Image(systemName: "arrow.up")
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(Theme.onAccent)
                            .frame(width: 20, height: 20)
                            .background(Circle().fill(Theme.accentFill))
                    }
                    .buttonStyle(.plain)
                    .disabled(empty || chat.sending)
                    .opacity(empty ? 0.4 : 1)
                    .help("Post it now, as the bot")
                    .accessibilityLabel("Post")
                }
                .padding(.leading, Theme.inset)
                .padding(.trailing, 6)
                .padding(.vertical, 6)
                .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field))
                .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: focused ? 1.5 : 1))
            }
        }
        .padding(10)
    }
}

/// Adds a Discord bot or a Slack app of the person's own: the core signs in
/// with its token, and only then keeps it, in the Keychain.
@MainActor
private struct ChatAddAccount: View {
    @Environment(Store.self) private var store
    @Environment(\.dismiss) private var dismiss
    @State private var kind = "discord"
    @State private var name = ""
    @State private var token = ""
    @State private var app = ""
    @State private var user = ""
    @State private var working = false
    @State private var problem: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Add a Chat Account").font(Theme.font(15, .bold))
            Picker("", selection: $kind) {
                Text("Discord").tag("discord")
                Text("Slack").tag("slack")
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            help
            Grid(alignment: .leading, horizontalSpacing: 8, verticalSpacing: 8) {
                row(kind == "slack" ? "Bot token" : "Token") {
                    SecureField("", text: $token, prompt: Text(kind == "slack" ? "xoxb-…" : "the bot's token")).textFieldStyle(.roundedBorder)
                }
                if kind == "slack" {
                    row("App token") { SecureField("", text: $app, prompt: Text("xapp-…, for mentions as they come")).textFieldStyle(.roundedBorder) }
                }
                row("Name") { TextField("", text: $name, prompt: Text("the bot's own when left empty")).textFieldStyle(.roundedBorder) }
                row("Your ID") { TextField("", text: $user, prompt: Text("optional: your own user ID there")).textFieldStyle(.roundedBorder) }
            }
            if let problem {
                Text(problem).font(Theme.font(11.5)).foregroundStyle(Theme.needs).fixedSize(horizontal: false, vertical: true)
            }
            HStack {
                Text("The token goes to the Keychain, never to a file.").font(Theme.font(10.5)).foregroundStyle(Theme.muted)
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button(working ? "Signing In…" : "Add") { add() }
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.accent)
                    .disabled(working || token.isEmpty)
            }
        }
        .padding(18)
        .frame(width: 480)
    }

    @ViewBuilder private var help: some View {
        switch kind {
        case "slack":
            VStack(alignment: .leading, spacing: 4) {
                Text("Slack takes an app of your own. Make it from \(Brand.name)’s manifest, which asks for only what \(Brand.name) uses, install it to your workspace, and paste the Bot User OAuth Token from OAuth & Permissions. For mentions as they come, add an app-level token with connections:write.")
                    .font(Theme.font(11.5)).foregroundStyle(Theme.muted).fixedSize(horizontal: false, vertical: true)
                Button("Make the Slack App") { makeSlackApp() }
                    .buttonStyle(.link).font(Theme.font(11.5, .semibold))
            }
        default:
            VStack(alignment: .leading, spacing: 4) {
                Text("Discord takes a bot of your own: make an application, then on its Bot page Reset Token and paste it here. Invite the bot to your server with View Channels, Send Messages, Send Messages in Threads, Create Public Threads and Read Message History.")
                    .font(Theme.font(11.5)).foregroundStyle(Theme.muted).fixedSize(horizontal: false, vertical: true)
                Button("Open the Developer Portal") { store.chat.visit("https://discord.com/developers/applications") }
                    .buttonStyle(.link).font(Theme.font(11.5, .semibold))
            }
        }
    }

    private func row<F: View>(_ label: String, @ViewBuilder _ field: () -> F) -> some View {
        GridRow {
            Text(label).font(Theme.font(12, .semibold)).foregroundStyle(Theme.muted).gridColumnAlignment(.trailing)
            field()
        }
    }

    /// Opens Slack's Create an App with Irie's manifest filled in, as the
    /// core gives it.
    private func makeSlackApp() {
        Task {
            guard let steps = await store.chat.slackSteps(),
                  let link = steps.split(whereSeparator: \.isWhitespace).first(where: { $0.hasPrefix("https://api.slack.com/apps?") })
            else {
                problem = "\(Brand.name)’s core isn’t answering now: try again in a moment."
                return
            }
            store.chat.visit(String(link))
        }
    }

    private func add() {
        working = true
        problem = nil
        let n = ChatNew(kind: kind, name: name.isEmpty ? nil : name.lowercased(), token: token.trimmingCharacters(in: .whitespacesAndNewlines),
                        app: kind == "slack" && !app.isEmpty ? app.trimmingCharacters(in: .whitespacesAndNewlines) : nil,
                        user: user.isEmpty ? nil : user.trimmingCharacters(in: .whitespaces))
        Task {
            let err = await store.chat.add(n)
            working = false
            if let err { problem = err } else { dismiss() }
        }
    }
}
