import AppKit
import SwiftUI
import WebKit

// What the mail plugin draws (Mail.swift has the model): the right
// sidebar's Mail tab, beside the sessions, where a message opens in place;
// and the Mail window, with the accounts and folders, the list and the
// message side by side. Every row starts with its account's colour and
// carries its account's name and provider's mark.

// MARK: The tab and the window

/// Window › Mail (⇧⌘E), while the mail plugin is on. The window's own item,
/// which SwiftUI would add whatever the plugin, is removed (IrieApp.swift).
struct MailCommands: Commands {
    let menu: MenuState
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        let on = MainActor.assumeIsolated { menu.facts.plugins.contains { $0.name == "mail" && $0.on } }
        CommandGroup(before: .windowList) {
            if on {
                Button("Mail") { openWindow(id: MailWindow.id) }
                    .keyboardShortcut("e", modifiers: [.command, .shift])
                Divider()
            }
        }
    }
}

/// The right sidebar's Mail tab (⌥⌘7): every account's newest mail in one
/// list, a strip of the accounts above it to show one alone, and the
/// message read in place of the list.
@MainActor
struct MailTab: View {
    @Environment(Store.self) private var store
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        let mail = store.mail
        VStack(spacing: 0) {
            if mail.opened != nil || mail.selected != nil {
                MailReaderPane(compact: true)
            } else {
                MailBar(compact: true, popOut: { openWindow(id: MailWindow.id) })
                if !mail.accounts.isEmpty { MailAccountStrip() }
                Rectangle().fill(Theme.edge).frame(height: 1)
                MailList()
            }
        }
        .onAppear { mail.appeared() }
        .onDisappear { mail.disappeared() }
        .mailSheets(mail)
    }
}

/// The Mail window (Window › Mail, ⇧⌘E): the accounts and folders, the
/// list and the message read, side by side.
@MainActor
struct MailWindow: View {
    static let id = "mail"
    @Environment(Store.self) private var store

    var body: some View {
        let mail = store.mail
        NavigationSplitView {
            MailSidebar()
                .navigationSplitViewColumnWidth(min: 180, ideal: 210, max: 280)
        } content: {
            VStack(spacing: 0) {
                MailBar(compact: false, popOut: nil)
                Rectangle().fill(Theme.edge).frame(height: 1)
                MailList()
            }
            .background(Theme.card)
            .navigationSplitViewColumnWidth(min: 300, ideal: 380, max: 520)
        } detail: {
            MailReaderPane(compact: false)
                .background(Theme.card)
        }
        .navigationTitle(mail.only.map { "Mail — \($0)" } ?? "Mail")
        .onAppear { mail.appeared() }
        .onDisappear { mail.disappeared() }
        .mailSheets(mail)
    }
}

private extension View {
    /// The composer and Add Account, over whichever Mail view asked.
    @MainActor
    func mailSheets(_ mail: MailModel) -> some View {
        self
            .sheet(item: Binding(get: { mail.composing }, set: { mail.composing = $0 })) { c in
                MailComposer(draft: c)
            }
            .sheet(isPresented: Binding(get: { mail.adding }, set: { mail.adding = $0 })) {
                AddMailAccount()
            }
    }
}

// MARK: The bar, the accounts and the list

/// The folder, the search field and the buttons above a list.
@MainActor
struct MailBar: View {
    @Environment(Store.self) private var store
    let compact: Bool
    let popOut: (() -> Void)?

    var body: some View {
        let mail = store.mail
        HStack(spacing: 6) {
            if compact {
                Menu {
                    ForEach(MailFolder.allCases) { f in
                        Button {
                            mail.show(folder: f)
                        } label: {
                            Label(f.title, systemImage: f.symbol)
                        }
                    }
                } label: {
                    Label(mail.folder.title, systemImage: mail.folder.symbol)
                        .font(Theme.font(12, .semibold))
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
                .help("Show another folder of every account")
            }
            HStack(spacing: 4) {
                Image(systemName: "magnifyingglass")
                    .font(.system(size: 10.5, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                TextField("Search", text: Binding(get: { mail.query }, set: { mail.query = $0 }))
                    .textFieldStyle(.plain)
                    .font(Theme.font(12))
                    .onSubmit { mail.search() }
                if !mail.query.isEmpty {
                    Button { mail.clearSearch() } label: {
                        Image(systemName: "xmark.circle.fill").foregroundStyle(Theme.muted)
                    }
                    .buttonStyle(.plain)
                    .help("Clear the search")
                }
            }
            .padding(.horizontal, 7)
            .padding(.vertical, 4)
            .background(RoundedRectangle(cornerRadius: 7).fill(Theme.field))
            .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(Theme.edge))
            barButton("arrow.clockwise", help: "Read every account again now", spinning: mail.loading) {
                Task { await mail.load(refresh: true) }
            }
            barButton("square.and.pencil", help: "Write a message") { mail.compose() }
                .disabled(mail.accounts.isEmpty)
            if let popOut {
                barButton("macwindow", help: "Open Mail in a window of its own (⇧⌘E)", action: popOut)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
    }

    private func barButton(_ symbol: String, help: String, spinning: Bool = false, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 11.5, weight: .semibold))
                .foregroundStyle(Theme.muted)
                .frame(width: 22, height: 22)
                .opacity(spinning ? 0.4 : 1)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(help)
    }
}

/// The accounts as chips across the tab, All first: a chip shows its
/// account's mail alone. Each wears its account's colour and provider's
/// mark, with its unread count.
@MainActor
struct MailAccountStrip: View {
    @Environment(Store.self) private var store

    var body: some View {
        let mail = store.mail
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 5) {
                chip(nil, title: "All", count: mail.unread, on: mail.only == nil)
                ForEach(mail.accounts) { a in
                    chip(a, title: a.name, count: a.unread, on: mail.only == a.name)
                        .contextMenu { MailAccountMenu(account: a) }
                }
                Button { mail.adding = true } label: {
                    Image(systemName: "plus")
                        .font(.system(size: 10, weight: .bold))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 22, height: 22)
                        .background(Circle().fill(Theme.field))
                }
                .buttonStyle(.plain)
                .help("Add a Gmail, Proton Mail or other account")
            }
            .padding(.horizontal, 10)
            .padding(.bottom, 7)
        }
    }

    private func chip(_ a: MailAccount?, title: String, count: Int, on: Bool) -> some View {
        let mail = store.mail
        return Button {
            mail.only = a?.name
            if let s = mail.selected, let m = mail.messages.first(where: { $0.id == s }), a != nil, m.account != a?.name { mail.close() }
        } label: {
            HStack(spacing: 4) {
                if let a {
                    Image(systemName: a.failing ? "exclamationmark.triangle.fill" : a.symbol)
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(a.failing ? Theme.needs : Theme.agent(slot: a.colour))
                }
                Text(title)
                    .font(Theme.font(11.5, on ? .bold : .medium))
                    .foregroundStyle(a.map { Theme.agentInk(slot: $0.colour) } ?? Theme.ink)
                if count > 0 {
                    Text("\(count)")
                        .font(Theme.font(10.5, .bold))
                        .foregroundStyle(Theme.muted)
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(Capsule().fill(a.map { Theme.agentWash(slot: $0.colour) } ?? Theme.field))
            .overlay(Capsule().strokeBorder(on ? (a.map { Theme.agent(slot: $0.colour) } ?? Theme.ink) : Theme.edge, lineWidth: on ? 1.5 : 1))
        }
        .buttonStyle(.plain)
        .help(a.map { "\($0.address) · \($0.provider)" + ($0.problem.map { "\n\($0)" } ?? "") } ?? "Every account's mail together")
    }
}

/// An account's right-click menu: check it, trust Bridge's new
/// certificate, its web mail, and remove it.
@MainActor
struct MailAccountMenu: View {
    @Environment(Store.self) private var store
    let account: MailAccount

    var body: some View {
        let mail = store.mail
        Button("Write from \(account.name)") { mail.compose(from: account.name) }
        Button("Sign In Again") { mail.check(account.name) }
        if account.kind == "proton" {
            Button("Trust Bridge’s New Certificate") { mail.check(account.name, trust: true) }
        }
        if let web = account.web, let url = URL(string: web) {
            Button("Open \(account.provider) on the Web") { NSWorkspace.shared.open(url) }
        }
        Divider()
        Button("New Password…") { mail.adding = true }
        Button("Remove \(account.name)…") { confirmRemove(account, mail) }
    }

    /// Asks before removing: the account's password leaves the Keychain.
    private func confirmRemove(_ a: MailAccount, _ mail: MailModel) {
        let alert = NSAlert()
        alert.messageText = "Remove \(a.name)?"
        alert.informativeText = "\(a.address) leaves \(Brand.name)’s Mail, and its password leaves the Keychain. Nothing changes in the account itself."
        alert.addButton(withTitle: "Remove")
        alert.addButton(withTitle: "Cancel")
        alert.buttons.first?.hasDestructiveAction = true
        if alert.runModal() == .alertFirstButtonReturn { mail.remove(a.name) }
    }
}

/// The Mail window's sidebar: the folders, then the accounts.
@MainActor
struct MailSidebar: View {
    @Environment(Store.self) private var store

    var body: some View {
        let mail = store.mail
        List {
            Section("Folders") {
                ForEach(MailFolder.allCases) { f in
                    row(f.title, symbol: f.symbol, tint: Theme.muted, on: mail.folder == f, count: f == .inbox ? mail.unread : 0) {
                        mail.show(folder: f)
                    }
                }
            }
            Section("Accounts") {
                row("All Accounts", symbol: "tray.2", tint: Theme.muted, on: mail.only == nil, count: 0) { mail.only = nil }
                ForEach(mail.accounts) { a in
                    accountRow(a, on: mail.only == a.name) { mail.only = a.name }
                        .contextMenu { MailAccountMenu(account: a) }
                }
                Button { mail.adding = true } label: {
                    Label("Add Account…", systemImage: "plus.circle")
                        .font(Theme.font(12))
                        .foregroundStyle(Theme.accentInk)
                }
                .buttonStyle(.plain)
            }
        }
        .listStyle(.sidebar)
    }

    private func row(_ title: String, symbol: String, tint: Color, on: Bool, count: Int, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 7) {
                Image(systemName: symbol).foregroundStyle(tint).frame(width: 16)
                Text(title).font(Theme.font(12.5, on ? .semibold : .regular)).foregroundStyle(Theme.ink)
                Spacer()
                if count > 0 { Text("\(count)").font(Theme.font(11, .semibold)).foregroundStyle(Theme.muted) }
            }
            .padding(.vertical, 2)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .listRowBackground(on ? RoundedRectangle(cornerRadius: 6).fill(Theme.selection) : nil)
    }

    private func accountRow(_ a: MailAccount, on: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 7) {
                RoundedRectangle(cornerRadius: 2).fill(Theme.agent(slot: a.colour)).frame(width: 4, height: 26)
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 4) {
                        Text(a.name).font(Theme.font(12.5, on ? .semibold : .medium)).foregroundStyle(Theme.agentInk(slot: a.colour))
                        Image(systemName: a.symbol).font(.system(size: 9)).foregroundStyle(Theme.agent(slot: a.colour))
                    }
                    Text(a.failing ? (a.problem ?? a.state) : a.address)
                        .font(Theme.font(10.5))
                        .foregroundStyle(a.failing ? Theme.needs : Theme.muted)
                        .lineLimit(1)
                }
                Spacer()
                if a.unread > 0 { Text("\(a.unread)").font(Theme.font(11, .semibold)).foregroundStyle(Theme.muted) }
            }
            .contentShape(Rectangle())
            .help("\(a.address) · \(a.provider)" + (a.problem.map { "\n\($0)" } ?? ""))
        }
        .buttonStyle(.plain)
        .listRowBackground(on ? RoundedRectangle(cornerRadius: 6).fill(Theme.selection) : nil)
    }
}

/// The messages, newest first, with a line for each account that couldn't
/// be read, and what to do when there is nothing to show.
@MainActor
struct MailList: View {
    @Environment(Store.self) private var store

    var body: some View {
        let mail = store.mail
        let shown = mail.shown
        Group {
            if mail.loaded && mail.accounts.isEmpty {
                MailWelcome()
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 2) {
                        if let p = mail.problem { banner(p, symbol: "exclamationmark.triangle.fill") }
                        ForEach(mail.accounts.filter { $0.failing && (mail.only == nil || mail.only == $0.name) }) { a in
                            banner("\(a.name): \(a.problem ?? "it can’t be read.")", symbol: a.symbol, account: a)
                        }
                        if shown.isEmpty && mail.loaded {
                            Text(mail.query.isEmpty ? "Nothing in \(mail.folder.title)." : "No message holds “\(mail.query)”.")
                                .font(Theme.font(12))
                                .foregroundStyle(Theme.muted)
                                .frame(maxWidth: .infinity)
                                .padding(.top, 30)
                        } else if !mail.loaded {
                            ProgressView().controlSize(.small).frame(maxWidth: .infinity).padding(.top, 30)
                        }
                        ForEach(shown) { m in
                            MailRow(message: m, account: mail.account(m.account), selected: mail.selected == m.id)
                                .onTapGesture { mail.open(m) }
                                .contextMenu { rowMenu(m) }
                        }
                        if shown.count >= mail.limit {
                            Button("Show More") { mail.more() }
                                .buttonStyle(.borderless)
                                .font(Theme.font(12, .semibold))
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 8)
                        }
                    }
                    .padding(.horizontal, 6)
                    .padding(.vertical, 6)
                }
            }
        }
    }

    private func banner(_ text: String, symbol: String, account: MailAccount? = nil) -> some View {
        HStack(alignment: .top, spacing: 6) {
            Image(systemName: symbol).font(.system(size: 10)).foregroundStyle(Theme.needs)
            Text(text).font(Theme.font(11.5)).foregroundStyle(Theme.ink).fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
            if let account {
                Button("Sign In Again") { store.mail.check(account.name) }
                    .buttonStyle(.borderless)
                    .font(Theme.font(11, .semibold))
            }
        }
        .padding(8)
        .background(RoundedRectangle(cornerRadius: 8).fill(Theme.needsTint))
        .padding(.bottom, 4)
    }

    @ViewBuilder private func rowMenu(_ m: MailMessage) -> some View {
        let mail = store.mail
        Button(m.isUnread ? "Mark as Read" : "Mark as Unread") { mail.flag(m.id, m.isUnread ? "read" : "unread") }
        Button(m.isStarred ? "Unstar" : "Star") { mail.flag(m.id, m.isStarred ? "unstar" : "star") }
        Divider()
        if m.folder != MailFolder.archive.rawValue { Button("Archive") { mail.move(m.id, to: .archive) } }
        if m.folder != MailFolder.inbox.rawValue { Button("Move to Inbox") { mail.move(m.id, to: .inbox) } }
        if m.folder != MailFolder.junk.rawValue { Button("Junk") { mail.move(m.id, to: .junk) } }
        if m.folder != MailFolder.trash.rawValue { Button("Move to Trash") { mail.move(m.id, to: .trash) } }
    }
}

/// One message in a list: its account's colour down its edge, who sent
/// it, when, its subject, and its account's name and provider.
@MainActor
struct MailRow: View {
    let message: MailMessage
    let account: MailAccount?
    let selected: Bool
    @State private var hovered = false

    var body: some View {
        let m = message
        let slot = account?.colour ?? 0
        HStack(alignment: .top, spacing: 8) {
            RoundedRectangle(cornerRadius: 1.5)
                .fill(Theme.agent(slot: slot))
                .frame(width: 3)
                .padding(.vertical, 2)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 5) {
                    if m.isUnread {
                        Circle().fill(Theme.agent(slot: slot)).frame(width: 7, height: 7)
                            .accessibilityLabel("Unread")
                    }
                    Text(m.from.isEmpty ? "(no sender)" : m.from)
                        .font(Theme.font(12.5, m.isUnread ? .bold : .medium))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if m.isStarred {
                        Image(systemName: "star.fill").font(.system(size: 9)).foregroundStyle(Theme.sun)
                            .accessibilityLabel("Starred")
                    }
                    if m.answered == true {
                        Image(systemName: "arrowshape.turn.up.left.fill").font(.system(size: 8.5)).foregroundStyle(Theme.muted)
                            .accessibilityLabel("Answered")
                    }
                    Text(MailRow.when(m.date))
                        .font(Theme.font(11))
                        .foregroundStyle(Theme.muted)
                }
                Text(m.subject.isEmpty ? "(no subject)" : m.subject)
                    .font(Theme.font(12, m.isUnread ? .semibold : .regular))
                    .foregroundStyle(m.isUnread ? Theme.ink : Theme.muted)
                    .lineLimit(2)
                if let account { MailAccountBadge(account: account) }
            }
        }
        .padding(.vertical, 6)
        .padding(.horizontal, 8)
        .background(RoundedRectangle(cornerRadius: 8).fill(selected ? Theme.selection : (hovered ? Theme.hover : Color.clear)))
        .contentShape(Rectangle())
        .onHover { hovered = $0 }
        .accessibilityElement(children: .combine)
    }

    /// A message's time as a list shows it: the time today, the day this
    /// week, else the date, as irie mail prints it.
    static func when(_ d: Date, now: Date = Date()) -> String {
        let cal = Calendar.current
        if cal.isDate(d, inSameDayAs: now) { return d.formatted(date: .omitted, time: .shortened) }
        if d < now, now.timeIntervalSince(d) < 6 * 86400 { return d.formatted(.dateTime.weekday(.abbreviated)) }
        if cal.component(.year, from: d) == cal.component(.year, from: now) { return d.formatted(.dateTime.day().month(.abbreviated)) }
        return d.formatted(.dateTime.day().month(.abbreviated).year())
    }
}

/// An account's name in its colour, with its provider's mark and name, so
/// a Gmail account and a Proton one differ at a glance and without colour.
struct MailAccountBadge: View {
    let account: MailAccount

    var body: some View {
        HStack(spacing: 3) {
            Image(systemName: account.symbol).font(.system(size: 8, weight: .semibold))
            Text("\(account.name) · \(account.short)").font(Theme.font(10, .semibold))
        }
        .foregroundStyle(Theme.agentInk(slot: account.colour))
        .padding(.horizontal, 6)
        .padding(.vertical, 1.5)
        .background(Capsule().fill(Theme.agentWash(slot: account.colour)))
        .accessibilityLabel("\(account.name), \(account.provider)")
    }
}

// MARK: Reading

/// The message read, or what shows before one is.
@MainActor
struct MailReaderPane: View {
    @Environment(Store.self) private var store
    let compact: Bool

    var body: some View {
        let mail = store.mail
        if let o = mail.opened {
            MailReader(message: o, account: mail.account(o.account), compact: compact)
        } else if mail.selected != nil {
            VStack(spacing: 0) {
                if compact { backBar }
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        } else {
            SideNote(title: "No message open", text: "Pick a message in the list. Each account’s mail wears its colour, with its name and provider below the subject.")
        }
    }

    private var backBar: some View {
        HStack {
            Button { store.mail.close() } label: { Label("Mail", systemImage: "chevron.left").font(Theme.font(12, .semibold)) }
                .buttonStyle(.borderless)
            Spacer()
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
    }
}

/// One message: who, to whom and when, what it carries, and its words,
/// as plain text or, when it has HTML, as the sender laid it out, with
/// nothing loaded from elsewhere and no scripts.
@MainActor
struct MailReader: View {
    @Environment(Store.self) private var store
    let message: MailBody
    let account: MailAccount?
    let compact: Bool
    @AppStorage("mailFormatted") private var formatted = false

    var body: some View {
        let o = message
        let mail = store.mail
        return VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 2) {
                if compact {
                    Button { mail.close() } label: { Image(systemName: "chevron.left").font(.system(size: 12, weight: .semibold)) }
                        .buttonStyle(.borderless)
                        .help("Back to the list")
                        .padding(.trailing, 4)
                }
                tool("arrowshape.turn.up.left", "Reply") { mail.reply() }
                tool("arrowshape.turn.up.left.2", "Reply All") { mail.reply(all: true) }
                tool("arrowshape.turn.up.right", "Forward") { mail.forward() }
                Spacer(minLength: 6)
                tool(o.starred == true ? "star.fill" : "star", o.starred == true ? "Unstar" : "Star") {
                    mail.flag(o.id, o.starred == true ? "unstar" : "star")
                }
                tool("envelope.badge", "Mark as Unread") { mail.flag(o.id, "unread"); mail.close() }
                if o.folder != MailFolder.archive.rawValue { tool("archivebox", "Archive") { mail.move(o.id, to: .archive) } }
                if o.folder != MailFolder.inbox.rawValue { tool("tray.and.arrow.down", "Move to Inbox") { mail.move(o.id, to: .inbox) } }
                if o.folder != MailFolder.trash.rawValue { tool("trash", "Move to Trash") { mail.move(o.id, to: .trash) } }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            Rectangle().fill(Theme.edge).frame(height: 1)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    header(o)
                    if let files = o.attachments, !files.isEmpty { attachments(files, web: o.web) }
                    if o.html != nil {
                        Picker("", selection: $formatted) {
                            Text("Text").tag(false)
                            Text("As Sent").tag(true)
                        }
                        .pickerStyle(.segmented)
                        .labelsHidden()
                        .fixedSize()
                        .help("As sent shows the sender's layout, with no pictures or scripts loaded from elsewhere")
                    }
                    if formatted, let html = o.html {
                        MailHTMLView(html: html)
                            .frame(minHeight: 420)
                            .clipShape(RoundedRectangle(cornerRadius: 8))
                    } else {
                        Text(o.text.isEmpty ? "(This message has no text.)" : o.text)
                            .font(.system(size: 13))
                            .foregroundStyle(Theme.ink)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                .padding(compact ? 12 : 20)
            }
        }
    }

    private func header(_ o: MailBody) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(o.subject.isEmpty ? "(no subject)" : o.subject)
                .font(Theme.font(compact ? 15 : 18, .bold))
                .foregroundStyle(Theme.ink)
                .textSelection(.enabled)
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let account {
                    RoundedRectangle(cornerRadius: 1.5).fill(Theme.agent(slot: account.colour)).frame(width: 3, height: 12)
                }
                Text(o.from).font(Theme.font(12.5, .semibold)).foregroundStyle(Theme.ink)
                if let a = o.fromAddr, a != o.from {
                    Text(a).font(Theme.font(11.5)).foregroundStyle(Theme.muted).textSelection(.enabled).lineLimit(1)
                }
                if !compact {
                    Spacer(minLength: 4)
                    Text(o.date.formatted(date: .abbreviated, time: .shortened)).font(Theme.font(11)).foregroundStyle(Theme.muted)
                }
            }
            if compact {
                // A narrow tab has no room beside the sender for the date.
                Text(o.date.formatted(date: .abbreviated, time: .shortened)).font(Theme.font(11)).foregroundStyle(Theme.muted)
            }
            if let to = o.to, !to.isEmpty { line("To", to.joined(separator: ", ")) }
            if let cc = o.cc, !cc.isEmpty { line("Cc", cc.joined(separator: ", ")) }
            if let r = o.replyTo { line("Reply to", r) }
            HStack(spacing: 6) {
                if let account { MailAccountBadge(account: account) }
                if let web = o.web, let url = URL(string: web) {
                    Button("Open in \(account?.provider ?? "the Web")") { NSWorkspace.shared.open(url) }
                        .buttonStyle(.borderless)
                        .font(Theme.font(11, .semibold))
                }
            }
        }
    }

    private func line(_ label: String, _ value: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 5) {
            Text(label).font(Theme.font(11, .semibold)).foregroundStyle(Theme.muted).frame(width: 52, alignment: .leading)
            Text(value).font(Theme.font(11.5)).foregroundStyle(Theme.ink).textSelection(.enabled).lineLimit(3)
        }
    }

    private func attachments(_ files: [MailAttachment], web: String?) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(files, id: \.self) { f in
                HStack(spacing: 5) {
                    Image(systemName: "paperclip").font(.system(size: 10)).foregroundStyle(Theme.muted)
                    Text(f.name).font(Theme.font(11.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                    Text(ByteCountFormatter.string(fromByteCount: Int64(f.size * 3 / 4), countStyle: .file))
                        .font(Theme.font(10.5)).foregroundStyle(Theme.muted)
                }
            }
            Text(web != nil ? "Files open in the provider’s web mail for now." : "Files open in the provider’s own app for now.")
                .font(Theme.font(10.5)).foregroundStyle(Theme.muted)
        }
        .padding(8)
        .background(RoundedRectangle(cornerRadius: 8).fill(Theme.field))
    }

    private func tool(_ symbol: String, _ help: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(Theme.ink)
                .frame(width: 26, height: 24)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(help)
    }
}

/// A message's HTML as the sender laid it out, in a web view that runs no
/// script, keeps no data and loads nothing from elsewhere: a content
/// security policy refuses every load but the page's own styles and
/// inline pictures, so no tracking picture reports that it was read. A
/// link opens in the browser.
struct MailHTMLView: NSViewRepresentable {
    let html: String

    static let policy = "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:\">"

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.setValue(false, forKey: "drawsBackground")
        return web
    }

    func updateNSView(_ web: WKWebView, context: Context) {
        guard context.coordinator.shown != html else { return }
        context.coordinator.shown = html
        let page = Self.policy + "<style>html{background:#fff;color:#111;font:14px -apple-system,sans-serif}body{margin:12px}img{max-width:100%;height:auto}</style>" + html
        web.loadHTMLString(page, baseURL: nil)
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var shown: String?

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            // The page itself loads; a link goes to the browser, where the
            // person sees its real address.
            if action.navigationType == .other, action.request.url?.scheme == "about" || action.request.url == nil {
                decisionHandler(.allow)
                return
            }
            if action.navigationType == .linkActivated, let url = action.request.url, ["http", "https", "mailto"].contains(url.scheme?.lowercased() ?? "") {
                NSWorkspace.shared.open(url)
            }
            decisionHandler(.cancel)
        }
    }
}

// MARK: Writing and adding

/// The composer: from which account, to whom, and the words. Send sends at
/// once; the account's provider keeps the copy in Sent.
@MainActor
struct MailComposer: View {
    @Environment(Store.self) private var store
    @Environment(\.dismiss) private var dismiss
    @State var draft: MailCompose
    @State private var sending = false
    @State private var problem: String?

    var body: some View {
        let mail = store.mail
        VStack(alignment: .leading, spacing: 10) {
            Text(draft.reply == nil ? "New Message" : "Reply").font(Theme.font(15, .bold))
            Picker("From", selection: $draft.from) {
                ForEach(mail.accounts) { a in
                    Text("\(a.name) — \(a.address) (\(a.short))").tag(a.name)
                }
            }
            if let a = mail.account(draft.from) {
                HStack(spacing: 5) {
                    RoundedRectangle(cornerRadius: 1.5).fill(Theme.agent(slot: a.colour)).frame(width: 3, height: 12)
                    Text("Sends as \(a.address), through \(a.provider)").font(Theme.font(11)).foregroundStyle(Theme.muted)
                }
            }
            field("To", text: $draft.to, prompt: "name@example.com, …")
            field("Cc", text: $draft.cc, prompt: "")
            field("Subject", text: $draft.subject, prompt: "")
            TextEditor(text: $draft.body)
                .font(.system(size: 13))
                .frame(minHeight: 220)
                .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(Theme.edge))
            if let problem {
                Text(problem).font(Theme.font(11.5)).foregroundStyle(Theme.needs).fixedSize(horizontal: false, vertical: true)
            }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button(sending ? "Sending…" : "Send") {
                    sending = true
                    problem = nil
                    Task {
                        let err = await mail.send(draft)
                        sending = false
                        if let err { problem = err } else { dismiss() }
                    }
                }
                .keyboardShortcut(.return, modifiers: .command)
                .buttonStyle(.borderedProminent)
                .tint(Theme.accent)
                .disabled(sending || draft.to.trimmingCharacters(in: .whitespaces).isEmpty || draft.from.isEmpty)
                .help("Send it now (⌘↩)")
            }
        }
        .padding(18)
        .frame(width: 560)
    }

    private func field(_ label: String, text: Binding<String>, prompt: String) -> some View {
        HStack(spacing: 8) {
            Text(label).font(Theme.font(12, .semibold)).foregroundStyle(Theme.muted).frame(width: 56, alignment: .trailing)
            TextField(label, text: text, prompt: Text(prompt))
                .textFieldStyle(.roundedBorder)
                .labelsHidden()
        }
    }
}

/// Add Account: Gmail, Proton Mail or another IMAP account. The core adds
/// it only once its password signs in, and keeps the password in the
/// Keychain.
@MainActor
struct AddMailAccount: View {
    @Environment(Store.self) private var store
    @Environment(\.dismiss) private var dismiss
    @State private var kind = "gmail"
    @State private var address = ""
    @State private var name = ""
    @State private var password = ""
    @State private var imap = ""
    @State private var smtp = ""
    @State private var username = ""
    @State private var working = false
    @State private var problem: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Add a Mail Account").font(Theme.font(15, .bold))
            Picker("", selection: $kind) {
                Text("Gmail").tag("gmail")
                Text("Proton Mail").tag("proton")
                Text("Other").tag("imap")
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            help
            Grid(alignment: .leading, horizontalSpacing: 8, verticalSpacing: 8) {
                row("Address") { TextField("", text: $address, prompt: Text(kind == "gmail" ? "you@gmail.com" : "you@proton.me")).textFieldStyle(.roundedBorder) }
                row(kind == "gmail" ? "App password" : kind == "proton" ? "Bridge password" : "Password") {
                    SecureField("", text: $password).textFieldStyle(.roundedBorder)
                }
                row("Name") { TextField("", text: $name, prompt: Text("shown on each message, such as personal")).textFieldStyle(.roundedBorder) }
                if kind == "imap" {
                    row("IMAP") { TextField("", text: $imap, prompt: Text("imap.example.com:993")).textFieldStyle(.roundedBorder) }
                    row("SMTP") { TextField("", text: $smtp, prompt: Text("smtp.example.com:465")).textFieldStyle(.roundedBorder) }
                    row("Login") { TextField("", text: $username, prompt: Text("if not the address")).textFieldStyle(.roundedBorder) }
                }
            }
            if let problem {
                Text(problem).font(Theme.font(11.5)).foregroundStyle(Theme.needs).fixedSize(horizontal: false, vertical: true)
            }
            HStack {
                Text("The password goes to the Keychain, never to a file.").font(Theme.font(10.5)).foregroundStyle(Theme.muted)
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button(working ? "Signing In…" : "Add") { add() }
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.accent)
                    .disabled(working || address.isEmpty || password.isEmpty || (kind == "imap" && (imap.isEmpty || smtp.isEmpty)))
            }
        }
        .padding(18)
        .frame(width: 480)
    }

    @ViewBuilder private var help: some View {
        switch kind {
        case "gmail":
            VStack(alignment: .leading, spacing: 4) {
                Text("Gmail takes an app password, not your Google password. Make one for “\(Brand.name) Mail” with 2-Step Verification on, then paste it here.")
                    .font(Theme.font(11.5)).foregroundStyle(Theme.muted).fixedSize(horizontal: false, vertical: true)
                Button("Make an App Password") { open("https://myaccount.google.com/apppasswords") }
                    .buttonStyle(.link).font(Theme.font(11.5, .semibold))
            }
        case "proton":
            VStack(alignment: .leading, spacing: 4) {
                Text("Proton Mail reads through Proton Mail Bridge on this Mac, which needs a paid plan. Sign in to Bridge, pick the account, and paste the password from its mailbox details.")
                    .font(Theme.font(11.5)).foregroundStyle(Theme.muted).fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 12) {
                    Button("Open Bridge") { openBridge() }.buttonStyle(.link).font(Theme.font(11.5, .semibold))
                    Button("Get Bridge") { open("https://proton.me/mail/bridge") }.buttonStyle(.link).font(Theme.font(11.5, .semibold))
                }
            }
        default:
            Text("Any account with IMAP and SMTP over TLS. Port 993 or 465 means TLS at once, any other STARTTLS.")
                .font(Theme.font(11.5)).foregroundStyle(Theme.muted).fixedSize(horizontal: false, vertical: true)
        }
    }

    private func row<F: View>(_ label: String, @ViewBuilder _ field: () -> F) -> some View {
        GridRow {
            Text(label).font(Theme.font(12, .semibold)).foregroundStyle(Theme.muted).gridColumnAlignment(.trailing)
            field()
        }
    }

    private func open(_ s: String) {
        if let url = URL(string: s) { NSWorkspace.shared.open(url) }
    }

    private func openBridge() {
        for path in ["/Applications/Proton Mail Bridge.app", NSHomeDirectory() + "/Applications/Proton Mail Bridge.app"] where FileManager.default.fileExists(atPath: path) {
            NSWorkspace.shared.openApplication(at: URL(fileURLWithPath: path), configuration: NSWorkspace.OpenConfiguration())
            return
        }
        problem = "Proton Mail Bridge isn’t in Applications: Get Bridge installs it."
    }

    private func add() {
        working = true
        problem = nil
        let n = MailNew(name: name.isEmpty ? nil : name.lowercased(), kind: kind, address: address.trimmingCharacters(in: .whitespaces),
                        username: username.isEmpty ? nil : username, imap: imap.isEmpty ? nil : imap, smtp: smtp.isEmpty ? nil : smtp,
                        password: password)
        Task {
            let err = await store.mail.add(n)
            working = false
            if let err { problem = err } else { dismiss() }
        }
    }
}

/// What the tab and the window show before any account: what Mail is, and
/// Add Account.
@MainActor
struct MailWelcome: View {
    @Environment(Store.self) private var store

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: "envelope.badge.shield.half.filled")
                .font(.system(size: 26))
                .foregroundStyle(Theme.muted)
            Text("Your mail, beside your sessions").font(Theme.font(14, .bold)).foregroundStyle(Theme.ink)
            Text("Add your Gmail and Proton Mail accounts: their newest mail shows here in one list, each account in a colour of its own. Agents can’t read it, and nothing of it is kept on disk.")
                .font(Theme.font(12)).foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            Button("Add Account…") { store.mail.adding = true }
                .controlSize(.large)
        }
        .frame(maxWidth: 300)
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
