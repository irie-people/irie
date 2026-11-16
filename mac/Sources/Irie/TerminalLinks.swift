import AppKit

/// A link clicked in one of the app's terminals, with either engine
/// (GhosttyTerminal.swift, TerminalHost.swift): an address the terminal
/// finds in the text, a file's path, or a link a program wrote (OSC 8).
///
/// Both engines open a link at a ⌘-click, as Ghostty and SwiftTerm do in
/// their own apps, while a plain click goes on to tmux and the program in
/// the terminal. Where it opens is ui.terminal_links, which the core keeps
/// and applies (the `link` op): by default in the session's own panes, a
/// page on a Browser card brought to the front, or the one that shows it
/// already, and a file of text on the Code card at the line it names;
/// else in the Mac's own apps, as before. ⇧⌘-click opens it in the other
/// place (Store.openTerminalLink).
enum TerminalLinks {
    /// Hands a clicked link to the store: other for a ⇧⌘-click.
    static func clicked(_ link: String, other: Bool) {
        NotificationCenter.default.post(name: .irieTerminalLink, object: nil, userInfo: ["link": link, "other": other])
    }

    /// Opens a link in the Mac's own app for it, as the terminals did
    /// before Irie took their links, when no core can say where it goes:
    /// an address in the app for its scheme, else a path, ~ for home, in
    /// the file's own app (as Ghostty's macOS app does).
    static func openHere(_ link: String) {
        let t = link.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return }
        if let u = URL(string: t), u.scheme != nil {
            open(u)
        } else {
            open(URL(fileURLWithPath: (t as NSString).standardizingPath))
        }
    }

    /// Opens what the core named in the Mac's own app for it, or says that
    /// nothing here opens it, rather than fail without a word.
    @discardableResult
    static func open(_ url: URL) -> Bool {
        if url.isFileURL || NSWorkspace.shared.urlForApplication(toOpen: url) != nil, NSWorkspace.shared.open(url) {
            return true
        }
        NSSound.beep()
        return false
    }

    /// Whether a browser card at `shown`, or first sent to `asked`, shows
    /// the page `link` names: the same address, give or take the case of
    /// its scheme and host, a default port, and the slash of a bare host.
    static func samePage(_ link: String, shown: String?, asked: String? = nil) -> Bool {
        guard let want = key(link) else { return false }
        return [shown, asked].contains { $0.flatMap(key) == want }
    }

    /// An address as samePage compares it.
    static func key(_ s: String) -> String? {
        guard var c = URLComponents(string: s.trimmingCharacters(in: .whitespacesAndNewlines)),
              let scheme = c.scheme?.lowercased(), let host = c.host?.lowercased(), !host.isEmpty else { return nil }
        c.scheme = scheme
        c.host = host
        if (scheme == "http" && c.port == 80) || (scheme == "https" && c.port == 443) { c.port = nil }
        if c.path.isEmpty { c.path = "/" }
        return c.string
    }
}

/// The link op's arguments and answer (internal/ipc/link.go).
struct LinkArgs: Encodable {
    /// The session whose panes show, whose terminal or card showed it.
    var session: String?
    var link: String
    var other: Bool?
}

struct LinkReply: Decodable, Equatable {
    /// "card", a card of the session's panes: kind browser showing url, or
    /// code with path open at line and col; or "system", the Mac's own app
    /// for url.
    var `in`: String
    var kind: String?
    var url: String?
    var path: String?
    var line: Int?
    var col: Int?
}

extension Notification.Name {
    /// A terminal's link was clicked; the user info's "link" is the link as
    /// the terminal gives it, and "other" whether the click asked for the
    /// other place than ui.terminal_links names.
    static let irieTerminalLink = Notification.Name("irieTerminalLink")
}
