import AppKit
import SwiftUI

// The mail plugin's side of the app (docs/proposals/mail.md): the core
// reads every account, Gmail's and Proton Mail's alike, and the app only
// draws, in the right sidebar's Mail tab and in the Mail window. Each
// account wears one of the agents' six colours, which the core picks so
// accounts side by side stand apart, and its provider's mark beside its
// name, so colour never tells them apart alone.

struct MailArgs: Encodable {
    var `do`: String?
    var accounts: [String]?
    var folder: String?
    var query: String?
    var limit: Int?
    var refresh: Bool?
    var id: String?
    var seen: Bool?
    var flag: String?
    var to: String?
    var draft: MailDraft?
    var new: MailNew?
    var name: String?
}

struct MailNew: Encodable {
    var name: String?
    var kind: String
    var address: String
    var username: String?
    var imap: String?
    var smtp: String?
    var password: String
}

struct MailDraft: Encodable {
    var from: String
    var to: [String]
    var cc: [String]?
    var bcc: [String]?
    var subject: String
    var body: String
    var reply: String?
}

struct MailResult: Decodable {
    var accounts: [MailAccount]
    var messages: [MailMessage]?
    var message: MailBody?
    var text: String?
}

struct MailAccount: Decodable, Equatable, Identifiable {
    var name: String
    var kind: String
    var provider: String
    var address: String
    var colour: Int
    var unread: Int
    var state: String
    var problem: String?
    var web: String?

    var id: String { name }

    /// Its provider's mark, which tells a Gmail account from a Proton one
    /// without its colour.
    var symbol: String {
        switch kind {
        case "gmail": return "envelope.fill"
        case "proton": return "lock.shield.fill"
        default: return "tray.full.fill"
        }
    }

    var short: String {
        switch kind {
        case "gmail": return "Gmail"
        case "proton": return "Proton"
        default: return "Mail"
        }
    }

    var failing: Bool { state == "error" || state == "needs" }
}

struct MailMessage: Decodable, Equatable, Identifiable {
    var id: String
    var account: String
    var folder: String
    var from: String
    var fromAddr: String?
    var to: [String]?
    var subject: String
    var date: Date
    var unread: Bool?
    var starred: Bool?
    var answered: Bool?
    var size: Int?

    enum CodingKeys: String, CodingKey {
        case id, account, folder, from, to, subject, date, unread, starred, answered, size
        case fromAddr = "from_addr"
    }

    var isUnread: Bool { unread == true }
    var isStarred: Bool { starred == true }
}

struct MailBody: Decodable, Equatable {
    var id: String
    var account: String
    var folder: String
    var from: String
    var fromAddr: String?
    var to: [String]?
    var subject: String
    var date: Date
    var unread: Bool?
    var starred: Bool?
    var cc: [String]?
    var replyTo: String?
    var text: String
    var html: String?
    var attachments: [MailAttachment]?
    var messageId: String?
    var web: String?

    enum CodingKeys: String, CodingKey {
        case id, account, folder, from, to, subject, date, unread, starred, cc, text, html, attachments, web
        case fromAddr = "from_addr"
        case replyTo = "reply_to"
        case messageId = "message_id"
    }
}

struct MailAttachment: Decodable, Equatable, Hashable {
    var name: String
    var type: String
    var size: Int
}

/// The folders every account has, as the core names them.
enum MailFolder: String, CaseIterable, Identifiable {
    case inbox, starred, sent, drafts, archive, junk, trash

    var id: String { rawValue }

    var title: String {
        switch self {
        case .inbox: return "Inbox"
        case .starred: return "Starred"
        case .sent: return "Sent"
        case .drafts: return "Drafts"
        case .archive: return "Archive"
        case .junk: return "Junk"
        case .trash: return "Trash"
        }
    }

    var symbol: String {
        switch self {
        case .inbox: return "tray"
        case .starred: return "star"
        case .sent: return "paperplane"
        case .drafts: return "doc"
        case .archive: return "archivebox"
        case .junk: return "xmark.bin"
        case .trash: return "trash"
        }
    }
}

/// A message being written, in the composer.
struct MailCompose: Identifiable, Equatable {
    let id = UUID()
    var from: String
    var to = ""
    var cc = ""
    var subject = ""
    var body = ""
    var reply: String?
}

/// Every Mail view's state: the accounts and the list as the core last
/// gave them, the folder and the account shown, the message read and the
/// one being written. While any Mail view shows, it reads the list again
/// each minute; the core keeps what it read for as long, so that costs the
/// servers nothing more.
@MainActor
@Observable
final class MailModel {
    let client: CoreClient

    init(client: CoreClient) {
        self.client = client
    }

    var accounts: [MailAccount] = []
    var messages: [MailMessage] = []
    var folder = MailFolder.inbox
    /// The account shown alone; nil shows every account's mail together.
    var only: String?
    /// The words typed in the search field, and the ones the list holds.
    var query = ""
    private(set) var searched = ""
    private(set) var loading = false
    private(set) var loaded = false
    /// Why the last call failed, for the list's top.
    var problem: String?
    /// The message picked, and the one read, once the core gave it.
    var selected: String?
    private(set) var opened: MailBody?
    private(set) var reading = false
    var composing: MailCompose?
    var adding = false
    /// How many of each account's newest the list holds.
    private(set) var limit = 50

    @ObservationIgnored private var watchers = 0
    @ObservationIgnored private var timer: Task<Void, Never>?
    @ObservationIgnored private var generation = 0
    /// The snapshots' mail, which no core reads.
    @ObservationIgnored private var isFixture = false

    var unread: Int { accounts.reduce(0) { $0 + $1.unread } }

    func account(_ name: String) -> MailAccount? { accounts.first { $0.name == name } }

    /// The messages shown: every account's, or the one shown alone's.
    var shown: [MailMessage] {
        guard let only else { return messages }
        return messages.filter { $0.account == only }
    }

    private func call(_ args: MailArgs, timeout: TimeInterval = 50) async throws -> MailResult {
        do {
            return try await client.call("mail", args, timeout: timeout, as: MailResult.self)
        } catch let e as WireError {
            throw e
        } catch {
            // Anything but the core's own answer: it isn't there to ask.
            throw WireError(code: "offline", msg: "\(Brand.name)’s core isn’t answering, so Mail can’t read your accounts now.")
        }
    }

    // MARK: Looking

    /// A Mail view came into sight: the list is read at once, and each
    /// minute while one shows.
    func appeared() {
        watchers += 1
        guard watchers == 1 else { return }
        Task { await load() }
        timer = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 60_000_000_000)
                guard let self, !Task.isCancelled else { return }
                await self.load()
            }
        }
    }

    func disappeared() {
        watchers = max(0, watchers - 1)
        guard watchers == 0 else { return }
        timer?.cancel()
        timer = nil
    }

    /// Reads the folder's list from the core, every account at once; with
    /// refresh, the core reads the servers whatever it has.
    func load(refresh: Bool = false) async {
        guard !isFixture else { return }
        generation += 1
        let mine = generation
        loading = true
        defer { if mine == generation { loading = false } }
        let words = query.trimmingCharacters(in: .whitespaces)
        var args = MailArgs(folder: folder.rawValue, limit: limit, refresh: refresh ? true : nil)
        if !words.isEmpty { args.query = words }
        do {
            let r = try await call(args)
            guard mine == generation else { return }
            accounts = r.accounts
            messages = r.messages ?? []
            searched = words
            problem = nil
            loaded = true
            if let only, account(only) == nil { self.only = nil }
            if let s = selected, !messages.contains(where: { $0.id == s }), opened?.id != s {
                selected = nil
            }
        } catch {
            guard mine == generation else { return }
            problem = error.localizedDescription
            loaded = true
        }
    }

    func show(folder f: MailFolder) {
        guard f != folder else { return }
        folder = f
        selected = nil
        opened = nil
        messages = []
        Task { await load() }
    }

    func search() {
        selected = nil
        opened = nil
        Task { await load() }
    }

    func clearSearch() {
        query = ""
        if !searched.isEmpty { search() }
    }

    func more() {
        limit = min(limit + 50, 500)
        Task { await load() }
    }

    /// Opens the message: the core reads it whole and marks it read.
    func open(_ m: MailMessage) {
        selected = m.id
        if opened?.id == m.id { return }
        opened = nil
        reading = true
        Task {
            defer { if selected == m.id { reading = false } }
            do {
                let r = try await call(MailArgs(do: "read", id: m.id, seen: true))
                guard selected == m.id, let body = r.message else { return }
                opened = body
                accounts = r.accounts
                if let i = messages.firstIndex(where: { $0.id == m.id }) { messages[i].unread = false }
                problem = nil
            } catch {
                if selected == m.id { problem = error.localizedDescription }
            }
        }
    }

    func close() {
        selected = nil
        opened = nil
    }

    // MARK: Changing

    func flag(_ id: String, _ flag: String) {
        if let i = messages.firstIndex(where: { $0.id == id }) {
            switch flag {
            case "read": messages[i].unread = false
            case "unread": messages[i].unread = true
            case "star": messages[i].starred = true
            case "unstar": messages[i].starred = false
            default: break
            }
        }
        if opened?.id == id {
            if flag == "star" { opened?.starred = true }
            if flag == "unstar" { opened?.starred = false }
        }
        Task {
            do {
                let r = try await call(MailArgs(do: "flag", id: id, flag: flag))
                accounts = r.accounts
            } catch {
                problem = error.localizedDescription
                await load()
            }
        }
    }

    /// Moves the message, and opens the next one in the list, as a mail
    /// app does after Archive or Delete.
    func move(_ id: String, to folder: MailFolder) {
        let list = shown
        let next = list.firstIndex(where: { $0.id == id }).flatMap { i in
            i + 1 < list.count ? list[i + 1] : (i > 0 ? list[i - 1] : nil)
        }
        messages.removeAll { $0.id == id }
        let wasOpen = opened?.id == id || selected == id
        Task {
            do {
                let r = try await call(MailArgs(do: "move", id: id, to: folder.rawValue))
                accounts = r.accounts
            } catch {
                problem = error.localizedDescription
                await load()
            }
        }
        if wasOpen {
            if let next { open(next) } else { close() }
        }
    }

    /// Sends the message; nil when it went, else why not.
    func send(_ c: MailCompose) async -> String? {
        let split: (String) -> [String] = { s in
            s.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        }
        let draft = MailDraft(from: c.from, to: split(c.to), cc: split(c.cc).nilIfEmpty, subject: c.subject, body: c.body, reply: c.reply)
        do {
            let r = try await call(MailArgs(do: "send", draft: draft), timeout: 120)
            accounts = r.accounts
            if folder == .sent { await load(refresh: true) }
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    /// Adds the account; nil when it went in, else why not.
    func add(_ n: MailNew) async -> String? {
        do {
            let r = try await call(MailArgs(do: "add", new: n), timeout: 90)
            accounts = r.accounts
            await load()
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    func remove(_ name: String) {
        Task {
            do {
                let r = try await call(MailArgs(do: "remove", name: name))
                accounts = r.accounts
                if only == name { only = nil }
                messages.removeAll { $0.account == name }
                if let o = opened, o.account == name { close() }
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Signs in to the account again, after trusting its server's new
    /// certificate with trust.
    func check(_ name: String, trust: Bool = false) {
        Task {
            do {
                let r = try await call(MailArgs(do: trust ? "trust" : "test", name: name))
                accounts = r.accounts
                await load()
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    // MARK: Writing

    func compose(from account: String? = nil) {
        let from = account ?? only ?? accounts.first?.name ?? ""
        composing = MailCompose(from: from)
    }

    /// An answer to the message read: to its sender, or with all, to every
    /// address on it but the account's own.
    func reply(all: Bool = false) {
        guard let o = opened else { return }
        let me = account(o.account)?.address.lowercased() ?? ""
        var c = MailCompose(from: o.account)
        c.to = o.replyTo ?? o.fromAddr ?? o.from
        if all {
            let rest = ((o.to ?? []) + (o.cc ?? [])).filter { !$0.lowercased().contains(me) || me.isEmpty }
            c.cc = rest.joined(separator: ", ")
        }
        c.subject = o.subject.lowercased().hasPrefix("re:") ? o.subject : "Re: " + o.subject
        c.body = "\n\n" + Self.quoted(o)
        c.reply = o.id
        composing = c
    }

    func forward() {
        guard let o = opened else { return }
        var c = MailCompose(from: o.account)
        c.subject = o.subject.lowercased().hasPrefix("fwd:") ? o.subject : "Fwd: " + o.subject
        let head = "---------- Forwarded message ----------\nFrom: \(o.from)\(o.fromAddr.map { " <\($0)>" } ?? "")\nDate: \(o.date.formatted(date: .abbreviated, time: .shortened))\nSubject: \(o.subject)\n"
        c.body = "\n\n" + head + "\n" + o.text
        composing = c
    }

    static func quoted(_ o: MailBody) -> String {
        let when = o.date.formatted(date: .abbreviated, time: .shortened)
        let lines = o.text.split(separator: "\n", omittingEmptySubsequences: false).map { "> " + $0 }
        return "On \(when), \(o.from) wrote:\n" + lines.joined(separator: "\n")
    }
}

extension MailModel {
    /// The snapshots' mail (Snapshot.swift): two Gmail accounts and a
    /// Proton one, a list across them, and no core.
    func loadFixture(opening: Bool = false) {
        isFixture = true
        accounts = [
            MailAccount(name: "personal", kind: "gmail", provider: "Gmail", address: "alice@example.com", colour: 0, unread: 2, state: "ready", web: "https://mail.google.com/"),
            MailAccount(name: "work", kind: "gmail", provider: "Gmail", address: "alice@work.example", colour: 3, unread: 1, state: "ready"),
            MailAccount(name: "proton", kind: "proton", provider: "Proton Mail", address: "alice@proton.example", colour: 1, unread: 1, state: "ready"),
        ]
        let now = Date()
        func m(_ i: Int, _ account: String, _ from: String, _ subject: String, _ ago: TimeInterval, unread: Bool = false, starred: Bool = false) -> MailMessage {
            MailMessage(id: "\(account)|1|\(i)|INBOX", account: account, folder: "inbox", from: from, fromAddr: "\(from.lowercased().replacingOccurrences(of: " ", with: "."))@example.com",
                        subject: subject, date: now.addingTimeInterval(-ago), unread: unread, starred: starred)
        }
        messages = [
            m(1, "proton", "Bob Example", "Lunch on Friday? The usual place at noon", 600, unread: true),
            m(2, "personal", "Carol", "Photos from the weekend", 3600, unread: true),
            m(3, "work", "Dana Lee", "Re: the release notes for 1.2", 7200, unread: true, starred: true),
            m(4, "personal", "Bank of Example", "Your statement is ready", 26 * 3600, unread: true),
            m(5, "proton", "Erin", "Re: Tickets", 3 * 86400),
            m(6, "work", "Frank", "Planning for next week", 4 * 86400),
            m(7, "personal", "News Weekly", "This week: what’s new", 9 * 86400),
        ]
        loaded = true
        if opening {
            selected = messages[0].id
            opened = MailBody(id: messages[0].id, account: "proton", folder: "inbox", from: "Bob Example", fromAddr: "bob@example.com",
                              to: ["Alice <alice@proton.example>"], subject: "Lunch on Friday? The usual place at noon", date: now.addingTimeInterval(-600),
                              unread: false, starred: false, text: "Hi Alice,\n\nShall we have lunch on Friday? The usual place at noon, unless you’d rather try the new one by the river.\n\nBob",
                              attachments: [MailAttachment(name: "menu.pdf", type: "application/pdf", size: 120_000)])
        } else {
            selected = nil
            opened = nil
        }
    }
}

extension Store {
    /// Whether Mail shows: the core's build has the mail plugin, and the
    /// plugin is on.
    var mailShown: Bool {
        canMail && (plugins.first { $0.name == "mail" }?.on ?? true)
    }
}

private extension Array {
    var nilIfEmpty: [Element]? { isEmpty ? nil : self }
}
