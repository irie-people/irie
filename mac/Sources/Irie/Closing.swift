import AppKit
import Foundation

// Sessions close like tabs (DESIGN §4.5). ⌘W closes the selected session at
// once, without asking, because ⇧⌘T, reopen, is its undo; the next session
// in the list takes its place. ⇧⌘W still closes the window: File's Close
// Window, which takes the place of the window's own Close (MenuBar.swift).
//
// One thing asks first: closing a card whose commits have not reached its
// repo's land branch, such as live. Its branch keeps them, but nothing
// would bring them to mind again, so every close, of one card or several,
// asks the core what has not landed (unlanded), and when some has not, one
// short question names the commits, with Close Anyway. A clean card closes
// at once, as before.

/// A closed session that reopen can bring back. `auto` says it closed on
/// its own, by close_when_done; one you closed is done. `cold` is what its
/// next turn costs once reopened, now that its prompt cache has run out
/// (DESIGN §6.3).
struct ClosedSession: Decodable, Equatable, Identifiable {
    var id: String
    var name: String
    var closed: Date
    var auto: Bool?
    var cold: Cold?
}

/// One session to close, or several (sessions), or every finished one,
/// which close as one step of the history (Tabs.swift).
struct CloseArgs: Encodable {
    var session: String?
    var sessions: [String]?
    var finished: Bool?
    /// Finished keeps to this space ("" for the first) (Spaces.swift).
    var space: String?
}

struct ReopenArgs: Encodable {
    var session: String?
}

/// A card whose commits have not landed: those on its worktree's branch
/// whose work its repo's land branch (land) lacks, the first of them as
/// "abc1234 subject", newest first.
struct NotLive: Decodable, Equatable {
    var session: String
    var name: String
    var path: String
    var branch: String?
    var land: String
    var ahead: Int
    var commits: [String]
}

/// What of the work of the sessions a close would close has not landed:
/// those sessions, and the cards among them whose commits have not.
struct UnlandedResult: Decodable {
    var sessions: [String]
    var notLive: [NotLive]

    enum CodingKeys: String, CodingKey {
        case sessions
        case notLive = "not_live"
    }
}

/// The question a close asks before it closes cards whose commits have
/// not landed: which, and Close Anyway, which has Return, or Cancel, which
/// has Esc.
enum NotLiveConfirm {
    /// How many cards, and commits of each, it names.
    static let cards = 3
    static let commits = 3

    /// Close “api-fix”? 2 commits aren’t in live. / Close 5 sessions? 2 have
    /// commits that aren’t in live.
    static func title(_ list: [NotLive], closing count: Int) -> String {
        let land = Set(list.map(\.land)).count == 1 ? list[0].land : "their land branches"
        if count <= 1, let n = list.first {
            return "Close \(MenuText.quoted(n.name))? \(commitsWord(n.ahead)) \(n.ahead == 1 ? "isn’t" : "aren’t") in \(land)."
        }
        let some = list.count == count ? (count == 2 ? "Both" : "All \(count)") : "\(list.count)"
        return "Close \(count) sessions? \(some) \(list.count == 1 ? "has" : "have") commits that aren’t in \(land)."
    }

    /// The commits, by card when there are several, and that they stay.
    static func detail(_ list: [NotLive], closing count: Int) -> String {
        var parts: [String] = []
        for n in list.prefix(cards) {
            var lines = n.commits.prefix(commits).map { "• " + $0 }
            if n.ahead > min(n.commits.count, commits) { lines.append("• and \(n.ahead - min(n.commits.count, commits)) more") }
            let head = count <= 1 ? (n.branch.map { "On \($0):" } ?? "")
                : "\(MenuText.quoted(n.name)), \(commitsWord(n.ahead).lowercased())" + (n.branch.map { " on \($0)" } ?? "") + ":"
            parts.append(([head].filter { !$0.isEmpty } + lines).joined(separator: "\n"))
        }
        if list.count > cards { parts.append("And \(list.count - cards) more cards.") }
        let them = list.count == 1 ? "Its branch keeps them" : "Their branches keep them"
        parts.append("\(them), and \(Brand.slug) ctl dog lists \(list.count == 1 ? "it" : "them") for a week. Reopen brings \(count <= 1 ? "it" : "them") back.")
        return parts.joined(separator: "\n\n")
    }

    static func commitsWord(_ n: Int) -> String { n == 1 ? "1 commit" : "\(n) commits" }

    static func alert(_ list: [NotLive], closing count: Int) -> NSAlert {
        let alert = NSAlert()
        alert.messageText = title(list, closing: count)
        alert.informativeText = detail(list, closing: count)
        alert.addButton(withTitle: "Close Anyway")
        // Titled Cancel, it takes Esc.
        alert.addButton(withTitle: "Cancel")
        alert.alertStyle = .warning
        return alert
    }
}

struct ReopenResult: Decodable {
    var session: Session
}

extension Store {
    /// The session ⌘W closes: the selected card, unless an outside row is
    /// selected.
    var closable: Session? {
        outsideSelection == nil ? selectedRow : nil
    }

    /// Closes a session, at once unless commits of its work have not landed
    /// (mayClose). When it is the selected one, the next session in the list
    /// is shown in its place at once, or the one before it at the end, as
    /// when a tab closes, so ⌘W ⌘W closes the next card too; Cancel shows
    /// it again. A card already on its way out is let be.
    func close(_ s: Session) {
        guard !closingNow.contains(s.id) else { return }
        closingNow.insert(s.id)
        let shown = selection == s.id
        let order = drawnOrder
        if shown, let i = order.firstIndex(where: { $0.id == s.id }) {
            let rest = order.filter { $0.id != s.id }
            if rest.isEmpty {
                selection = nil
            } else {
                show(rest[min(i, rest.count - 1)].id)
            }
        }
        let next = selection
        Task {
            defer { closingNow.remove(s.id) }
            let (go, asked) = await mayClose(CloseArgs(session: s.id))
            guard go else {
                // It stays: it is shown again, unless you went elsewhere.
                if shown, selection == next, sessions.contains(where: { $0.id == s.id }) { show(s.id) }
                return
            }
            do {
                // The core reads what of its work has not landed first.
                let r = try await client.call("close", CloseArgs(session: s.id), timeout: 30, as: CloseResult.self)
                if let said = Self.notLiveNotice(r.notLive ?? [], asked: asked) {
                    say("Closed \(s.name). \(said) Reopen it with ⇧⌘T.", error: true, seconds: 10)
                } else {
                    say("Closed \(s.name). Reopen it with ⇧⌘T.", error: false)
                }
            } catch {
                say("Couldn’t close \(s.name): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Whether a close of args may go ahead: the core says what of the work
    /// of the sessions it would close has not landed, and when some has
    /// not, one question names the commits, Close Anyway or Cancel (DESIGN
    /// §4.5). It gives back the cards it asked about. A core that can't
    /// say, or not within 3 s, lets the close go ahead; the close's reply
    /// says it then (notLiveNotice).
    func mayClose(_ args: CloseArgs) async -> (go: Bool, asked: Set<String>) {
        guard canSayUnlanded,
              let r = try? await client.call("unlanded", args, timeout: 3, as: UnlandedResult.self),
              !r.notLive.isEmpty else { return (true, []) }
        let alert = NotLiveConfirm.alert(r.notLive, closing: r.sessions.count)
        let answer: NSApplication.ModalResponse
        if let w = WindowPanes.window, w.isKeyWindow, w.attachedSheet == nil {
            answer = await withCheckedContinuation { done in
                alert.beginSheetModal(for: w) { done.resume(returning: $0) }
            }
        } else {
            answer = alert.runModal()
        }
        return (answer == .alertFirstButtonReturn, Set(r.notLive.map(\.session)))
    }

    /// What a notice says of the cards a close closed whose commits had not
    /// landed, of those its question did not name, as when their agents
    /// committed meanwhile; nil when there are none.
    static func notLiveNotice(_ list: [NotLive], asked: Set<String>) -> String? {
        let new = list.filter { !asked.contains($0.session) }
        guard let n = new.first else { return nil }
        let more = new.count > 1 ? " and \(new.count - 1) more" : ""
        return "\(MenuText.quoted(n.name))\(more) had \(NotLiveConfirm.commitsWord(n.ahead).lowercased()) not in \(n.land); \(Brand.slug) ctl dog lists \(new.count == 1 ? "it" : "them")."
    }

    /// Brings back the session closed last in its place in the list, shows
    /// it and gives its terminal the keyboard.
    func reopenLast() {
        guard let last = closed.first else {
            NSSound.beep()
            return
        }
        Task {
            do {
                // It waits for the closed agent to end, and starts like a spawn.
                let r = try await client.call("reopen", ReopenArgs(session: last.id), timeout: 600, as: ReopenResult.self)
                say("Reopened \(r.session.name).", error: false)
                show(r.session.id, focus: true)
            } catch {
                say("Couldn’t reopen \(last.name): \(error.localizedDescription)", error: true)
            }
        }
    }
}
