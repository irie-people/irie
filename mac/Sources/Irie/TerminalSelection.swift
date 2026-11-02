import AppKit

/// How the app's terminals select, with either engine (GhosttyTerminal.swift,
/// TerminalHost.swift).
///
/// tmux takes the mouse in every terminal, for its wheel and its clicks, and
/// hands it on to a program that asks for it, as Claude Code's fullscreen
/// view does. Left to that, a drag selects inside the program, with a round
/// trip through tmux for every move, or in tmux's copy mode, which drops the
/// selection when the button comes up. So the terminal keeps a drag for
/// itself: while the button is down it reports nothing to tmux and selects
/// as any Mac text view does, drawn at once and kept until a click, a key or
/// the wheel. A press waits until it is known to be a click, which goes on
/// to tmux when the button comes up, or a drag. Double- and triple-clicks
/// select a word and a line, Shift extends the selection, and the wheel and
/// the pointer's moves still go to tmux. Letting go copies the selection,
/// unless Settings turns that off.
enum TerminalSelection {
    /// Where the app keeps whether a selection is copied as it is made
    /// (Settings > Appearance). It is on until turned off.
    static let copyKey = "terminalCopyOnSelect"

    static var copies: Bool {
        UserDefaults.standard.object(forKey: copyKey) as? Bool ?? true
    }

    /// How far, in points, the pointer goes with the button down before a
    /// press is a drag that selects, not a click for the program inside.
    static let dragDistance: CGFloat = 4

    static func isDrag(from down: NSEvent, to now: NSEvent) -> Bool {
        let a = down.locationInWindow, b = now.locationInWindow
        return hypot(b.x - a.x, b.y - a.y) >= dragDistance
    }

    /// Whether a selection holds anything to copy: blanks alone would only
    /// take the place of what the pasteboard holds.
    static func worthCopying(_ text: String) -> Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// What the note says once a selection is on the pasteboard.
    static func note(_ text: String) -> String {
        let lines = text.trimmingCharacters(in: .newlines)
            .split(separator: "\n", omittingEmptySubsequences: false).count
        return lines > 1 ? "Copied \(lines) lines" : "Copied"
    }

    /// Says that a selection went to the pasteboard, for the note over the
    /// terminal (TerminalArea.swift).
    static func copied(_ text: String) {
        NotificationCenter.default.post(name: .irieTerminalCopied, object: nil, userInfo: ["note": note(text)])
    }

    /// The terminal's menu on a right-click or a Control-click: Copy, Paste
    /// and Select All, as in any Mac text view. Neither click reaches tmux,
    /// whose own menu holds Kill and whose Control-click swaps panes.
    static func menu(for event: NSEvent, target: NSView, canCopy: Bool) -> NSMenu? {
        switch event.type {
        case .rightMouseDown: break
        case .leftMouseDown where event.modifierFlags.contains(.control): break
        default: return nil
        }
        let menu = NSMenu()
        menu.autoenablesItems = false
        menu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "").isEnabled = canCopy
        menu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "")
        menu.addItem(.separator())
        menu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "")
        for item in menu.items where !item.isSeparatorItem { item.target = target }
        return menu
    }
}

extension Notification.Name {
    /// A terminal put its selection on the pasteboard; the user info's
    /// "note" says so in words.
    static let irieTerminalCopied = Notification.Name("irieTerminalCopied")
}
