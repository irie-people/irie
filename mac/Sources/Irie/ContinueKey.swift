import AppKit

// A double ↓ on an agent's empty prompt sends it on: the core types
// "continue" and Enter into the prompt (op continue, DESIGN §2.4), the
// quick way on after an API error or a turn that stopped short. It works
// in the terminal, in the message box while it is empty, and on the note
// that stands for the box while a session waits on you (MAC.md §4).
//
// In the terminal the first ↓ waits a moment, but only while the screen
// shows the prompt empty: there Claude Code takes ↓ into its list of
// background tasks and agents, where Enter would open one, so neither ↓ of
// a pair may reach it. A lone ↓ goes on once the moment is over, or at
// once ahead of the next key, so keys keep their order. Any other ↓, on a
// prompt with text, in a menu or in another program, goes at once.

/// What the core's continue op takes.
struct ContinueArgs: Encodable {
    var session: String
}

/// A terminal whose screen the app can read (GhosttyTerminalView,
/// IrieTerminalView).
@MainActor
protocol TerminalScreen: AnyObject {
    /// The lines it shows now, top to bottom, one for each row.
    func screenLines() -> [String]?
    /// An input method is composing text in it, which ↓ may be choosing.
    var composing: Bool { get }
}

/// The agent's own prompt on a terminal's screen, read by the core's rule
/// (agentPrompt in internal/core/screen.go, whose tests hold the screens
/// it reads): a line that starts with ❯, or > before Claude Code 2.0,
/// right under a rule across the terminal, and the lines down to the next
/// rule; before 2.0 a box drew the rules and edged the lines. The prompt
/// nearest the bottom is the one.
enum AgentPrompt {
    static let marks = ["❯", ">"]

    /// What the prompt holds, its lines joined by newlines, without the
    /// mark; nil when no prompt shows, as while a menu stands in for it.
    static func typed(_ lines: [String]) -> String? {
        var i = lines.count - 1
        while i > 0 {
            if let first = promptLine(lines[i]), isRule(lines[i - 1]) {
                var text = [first]
                for l in lines[(i + 1)...] {
                    if isRule(l) { return text.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines) }
                    text.append(unboxed(l))
                }
                return nil
            }
            i -= 1
        }
        return nil
    }

    /// What follows the prompt's mark on a line that starts with one.
    static func promptLine(_ line: String) -> String? {
        let l = unboxed(line)
        for m in marks where l.hasPrefix(m) {
            let rest = l.dropFirst(m.count)
            if rest.first.map({ $0.isWhitespace }) ?? true {
                return rest.trimmingCharacters(in: .whitespaces)
            }
        }
        return nil
    }

    /// A line without the blanks round it and a box's edges.
    static func unboxed(_ line: String) -> String {
        var l = Substring(line.trimmingCharacters(in: .whitespaces))
        if l.hasPrefix("│") { l = l.dropFirst() }
        if l.hasSuffix("│") { l = l.dropLast() }
        return l.trimmingCharacters(in: .whitespaces)
    }

    /// A rule across the terminal: ─ from its start, or a box's corner,
    /// and a run of them, maybe with a title such as the session's name.
    static func isRule(_ line: String) -> Bool {
        let l = line.trimmingCharacters(in: .whitespaces)
        return (l.hasPrefix("─") || l.hasPrefix("╭") || l.hasPrefix("╰")) && l.contains(String(repeating: "─", count: 8))
    }
}

/// What ContinueKey needs of the app: the Store, and a stand-in in tests.
@MainActor
protocol ContinueTarget: AnyObject {
    /// The core has the continue op.
    var canContinue: Bool { get }
    /// The picked session, which the message box types to.
    var selection: String? { get }
    /// The session a terminal view types to, while it is the picked
    /// session's own; nil for any other.
    func continueSession(for view: NSView) -> String?
    /// The field editor edits the message box.
    func isMessageBox(_ editor: NSTextView) -> Bool
    /// Has the core send the session's agent on.
    func continueAgent(_ id: String)
}

/// Two presses of a key close together.
struct DoublePress {
    /// How soon the second press must follow the first: the Mac's
    /// double-click time, but no more than 0.4 s, since a lone ↓ on an
    /// empty prompt waits that long.
    static var interval: TimeInterval { min(NSEvent.doubleClickInterval, 0.4) }

    private var last: TimeInterval?

    /// Counts a press at t, an event's timestamp, and says whether it was
    /// the second of two.
    mutating func press(at t: TimeInterval) -> Bool {
        if let l = last, t - l <= Self.interval {
            last = nil
            return true
        }
        last = t
        return false
    }

    mutating func reset() { last = nil }
}

/// Watches the keys of Irie's window for a double ↓ on an empty prompt
/// (KeyRouter), and has the core send the agent on.
@MainActor
final class ContinueKey {
    /// A ↓ held back from a terminal while it may be the first of two.
    private struct Held {
        let event: NSEvent
        weak var view: NSView?
        let session: String
    }

    private var held: Held?
    /// Counts the holds, so a hold's moment ends only that hold.
    private var holds = 0
    private var box = DoublePress()

    /// Takes a key down in Irie's window before the window sends it on: nil
    /// when it kept the key.
    func route(_ event: NSEvent, store: ContinueTarget) -> NSEvent? {
        let down = Self.plainDown(event)
        if !down { box.reset() }
        if let h = held {
            if down, let v = h.view, v.window?.firstResponder === v, event.timestamp - h.event.timestamp <= DoublePress.interval {
                held = nil
                store.continueAgent(h.session)
                return nil
            }
            letGo()
        }
        guard down, store.canContinue, let responder = event.window?.firstResponder else { return event }
        if let view = responder as? NSView, let screen = view as? TerminalScreen {
            // Only the shown session's own terminal, with nothing typed in
            // its prompt and no input method at work.
            guard let session = store.continueSession(for: view), !screen.composing,
                  AgentPrompt.typed(screen.screenLines() ?? []) == "" else { return event }
            hold(event, view, session)
            return nil
        }
        if let editor = responder as? NSTextView, editor.isFieldEditor, store.isMessageBox(editor) {
            // ↓ does nothing in an empty box, so the first goes on.
            guard editor.string.isEmpty, let session = store.selection else {
                box.reset()
                return event
            }
            if box.press(at: event.timestamp) {
                store.continueAgent(session)
                return nil
            }
        }
        return event
    }

    /// A ↓ without Shift, Control, Option or Command, and not one that
    /// repeats while the key is held.
    static func plainDown(_ event: NSEvent) -> Bool {
        event.keyCode == 125 && !event.isARepeat
            && event.modifierFlags.intersection([.shift, .control, .option, .command]).isEmpty
    }

    private func hold(_ event: NSEvent, _ view: NSView, _ session: String) {
        held = Held(event: event, view: view, session: session)
        holds += 1
        let n = holds
        Task { @MainActor [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(DoublePress.interval * 1_000_000_000))
            guard let self, self.holds == n else { return }
            self.letGo()
        }
    }

    /// Hands a held ↓ on to its terminal: the lone ↓ it was.
    private func letGo() {
        guard let h = held else { return }
        held = nil
        if let v = h.view, v.window != nil { v.keyDown(with: h.event) }
    }
}
