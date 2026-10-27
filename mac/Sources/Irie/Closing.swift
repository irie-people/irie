import AppKit
import Foundation

// Sessions close like tabs (DESIGN §4.5). ⌘W closes the selected session at
// once, without asking, because ⇧⌘T, reopen, is its undo; the next session
// in the list takes its place. ⇧⌘W still closes the window.

/// A closed session that reopen can bring back.
struct ClosedSession: Decodable, Equatable, Identifiable {
    var id: String
    var name: String
    var closed: Date
}

struct CloseArgs: Encodable {
    var session: String
}

struct ReopenArgs: Encodable {
    var session: String?
}

struct ReopenResult: Decodable {
    var session: Session
}

extension Store {
    /// The session ⌘W closes: the selected card, unless an outside row is
    /// selected.
    var closable: Session? {
        guard outsideSelection == nil, let id = selection else { return nil }
        return sessions.first { $0.id == id }
    }

    /// Closes a session at once. When it is the selected one, the next
    /// session in the list is shown in its place, or the one before it at
    /// the end, as when a tab closes.
    func close(_ s: Session) {
        let order = drawnOrder
        if selection == s.id, let i = order.firstIndex(where: { $0.id == s.id }) {
            let rest = order.filter { $0.id != s.id }
            if rest.isEmpty {
                selection = nil
            } else {
                show(rest[min(i, rest.count - 1)].id)
            }
        }
        Task {
            do {
                _ = try await client.call("close", CloseArgs(session: s.id), as: Empty.self)
                say("Closed \(s.name). Reopen it with ⇧⌘T.", error: false)
            } catch {
                say("Couldn’t close \(s.name): \(error.localizedDescription)", error: true)
            }
        }
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

/// The window's own Close item, which SwiftUI adds with ⌘W, says Close
/// Window and ⇧⌘W, since ⌘W closes a session. KeyRouter acts on both keys
/// first, so this only keeps the menu honest; it runs again whenever a menu
/// opens, in case SwiftUI rebuilt the item.
@MainActor
enum WindowCloseItem {
    private static var observer: Any?

    static func install() {
        rebind()
        guard observer == nil else { return }
        observer = NotificationCenter.default.addObserver(forName: NSMenu.didBeginTrackingNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { rebind() }
        }
    }

    private static func rebind() {
        for top in NSApp.mainMenu?.items ?? [] {
            for item in top.submenu?.items ?? [] where item.action == #selector(NSWindow.performClose(_:)) {
                item.title = "Close Window"
                item.keyEquivalent = "w"
                item.keyEquivalentModifierMask = [.command, .shift]
            }
        }
    }
}
