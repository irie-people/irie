import AppKit
import Foundation

// Undo and redo (DESIGN §4.5). Edit > Undo and Redo, ⌘Z and ⇧⌘Z, step
// through the core's one history, which the terminal sidebar's u and U share:
// a park or an unpark, a close, a reopen, a start. The menu names the step
// each would take. When a step is uncertain, such as a session that is busy
// or gone, the core changes nothing and asks; the app asks the same in an
// alert and sends the answer back. While a text field has the keyboard and
// has typing to take back, ⌘Z undoes the typing, as anywhere else.

struct UndoArgs: Encodable {
    var choiceID: String?
    var choice: String?

    enum CodingKeys: String, CodingKey {
        case choiceID = "choice_id"
        case choice
    }
}

/// What an undo or redo did to the step labelled label: undone, redone,
/// settled, skipped, cancelled, or asked, with the question in choice.
struct UndoResult: Decodable {
    var label: String
    var did: String
    var choice: UndoChoice?
}

/// A question the core asks before an uncertain step: two to four options,
/// the recommended one first.
struct UndoChoice: Decodable {
    var id: String
    var question: String
    var options: [UndoOption]
}

struct UndoOption: Decodable {
    var key: String
    var label: String
    var detail: String
    var recommended: Bool?
}

enum UndoText {
    /// A step's label as a menu says it: the core's `park "api-fix"` reads
    /// Park “api-fix”.
    static func title(_ label: String) -> String {
        guard let sp = label.firstIndex(of: " ") else { return label.capitalizedFirst }
        let verb = String(label[..<sp]).capitalizedFirst
        var name = String(label[label.index(after: sp)...])
        // The name is quoted as Go's %q quotes it.
        if name.count >= 2, name.hasPrefix("\""), name.hasSuffix("\"") {
            name = String(name.dropFirst().dropLast())
                .replacingOccurrences(of: "\\\"", with: "\"")
                .replacingOccurrences(of: "\\\\", with: "\\")
            return "\(verb) “\(name)”"
        }
        return "\(verb) \(name)"
    }
}

private extension String {
    var capitalizedFirst: String { prefix(1).uppercased() + dropFirst() }
}

extension Store {
    /// Undoes the history's latest step, or redoes the one undone last.
    func undo(redo: Bool = false) {
        Task { await step(redo ? "redo" : "undo", UndoArgs()) }
    }

    /// ⌘Z and ⇧⌘Z: typing to take back in the text field that has the
    /// keyboard goes first, then the history.
    func undoKey(redo: Bool = false) {
        if let text = NSApp.keyWindow?.firstResponder as? NSTextView, let um = text.undoManager,
           redo ? um.canRedo : um.canUndo {
            NSApp.sendAction(Selector(redo ? "redo:" : "undo:"), to: nil, from: nil)
            return
        }
        guard canUndo, (redo ? redoLabel : undoLabel) != nil else {
            NSSound.beep()
            return
        }
        undo(redo: redo)
    }

    private func step(_ op: String, _ args: UndoArgs) async {
        do {
            let r = try await client.call(op, args, timeout: 60, as: UndoResult.self)
            let what = UndoText.title(r.label)
            switch r.did {
            case "asked":
                guard let c = r.choice, let key = ask(c) else { return }
                await step(op, UndoArgs(choiceID: c.id, choice: key))
            case "undone": say("Undid \(what).", error: false)
            case "redone": say("Redid \(what).", error: false)
            case "settled": say("\(what): it was that way already, so the history moved on.", error: false)
            case "skipped": say("Took \(what) out of the history.", error: false)
            default: break
            }
        } catch {
            say("Couldn’t \(op): \(error.localizedDescription)", error: true)
        }
    }

    /// Asks the core's question in an alert: each option a button, the
    /// recommended one first, so Return takes it, and Cancel on Esc. It
    /// returns the key picked, or nil to change nothing.
    private func ask(_ c: UndoChoice) -> String? {
        let alert = NSAlert()
        alert.messageText = c.question
        alert.informativeText = c.options.map { "\($0.label): \($0.detail)" }.joined(separator: "\n\n")
        let options = Array(c.options.prefix(4))
        for (i, o) in options.enumerated() {
            let b = alert.addButton(withTitle: o.label)
            // The first button keeps Return.
            if o.key == "cancel", i > 0 { b.keyEquivalent = "\u{1b}" }
        }
        alert.alertStyle = .informational
        let picked = alert.runModal().rawValue - NSApplication.ModalResponse.alertFirstButtonReturn.rawValue
        guard options.indices.contains(picked), options[picked].key != "cancel" else { return nil }
        return options[picked].key
    }
}
