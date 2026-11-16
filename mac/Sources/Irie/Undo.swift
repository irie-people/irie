import AppKit
import Foundation

// Undo and redo (DESIGN §4.5). Edit > Undo and Redo, ⌘Z and ⇧⌘Z, step
// through the core's one history, which the terminal sidebar's u and U share:
// a park or an unpark, a close, a reopen, a start, a pause or a resume, of
// one session or all of them at once. The menu names the step each would
// take, and the sessions it takes. When a step is uncertain, such as a session that is busy
// or gone, the core changes nothing and asks; the app asks the same in an
// alert and sends the answer back. A key shows nothing of the step it would
// take, so ⌘Z and ⇧⌘Z ask first, as Undo Close “api-fix”?, unless Settings ›
// Tabs turns that off (UndoConfirm): the core asks it, tied to the step, so
// a step that comes in meanwhile is never the one taken. While a text field
// has the keyboard and has typing to take back, ⌘Z undoes the typing, as
// anywhere else.

struct UndoArgs: Encodable {
    var choiceID: String?
    var choice: String?
    /// Asks first even when the step is certain (UndoChoice.confirm).
    var confirm: Bool?

    enum CodingKeys: String, CodingKey {
        case choiceID = "choice_id"
        case choice
        case confirm
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
    /// The question UndoArgs.confirm asks of a step the core is sure of:
    /// its apply option takes the step, and cancel leaves it.
    var confirm: Bool?
}

struct UndoOption: Decodable {
    var key: String
    var label: String
    var detail: String
    var recommended: Bool?
}

enum UndoText {
    /// The app's words for the core's verbs: it calls freezing Pause and
    /// thawing Resume (Controls.swift), so its menu does too.
    private static let verbs = [("freeze", "pause"), ("thaw", "resume")]

    /// A step's label as a menu says it: the core's `park "api-fix"` reads
    /// Park “api-fix”, `rename "a" to "b"` reads Rename “a” to “b”, and
    /// `freeze "a" and "b"`, freezing them all, reads Pause “a” and “b”.
    /// Each name is quoted as Go's %q quotes it; `core.Title` reads labels
    /// the same way.
    static func title(_ label: String) -> String {
        var label = label
        for (core, app) in verbs where label == core || label.hasPrefix(core + " ") {
            label = app + label.dropFirst(core.count)
        }
        var out = ""
        var rest = Substring(label)
        while let open = rest.firstIndex(of: "\"") {
            out += rest[..<open]
            var name = ""
            var i = rest.index(after: open)
            var closed = false
            while i < rest.endIndex {
                let c = rest[i]
                if c == "\"" {
                    closed = true
                    break
                }
                if c == "\\", rest.index(after: i) < rest.endIndex {
                    i = rest.index(after: i)
                }
                name.append(rest[i])
                i = rest.index(after: i)
            }
            guard closed else {
                out += rest[open...]
                return out.capitalizedFirst
            }
            out += "“\(name)”"
            rest = rest[rest.index(after: i)...]
        }
        out += rest
        return out.capitalizedFirst
    }
}

private extension String {
    var capitalizedFirst: String { prefix(1).uppercased() + dropFirst() }
}

/// Settings › Tabs › Undo: ⌘Z and ⇧⌘Z ask before they take a step of the
/// history, on until turned off there or with the question's Don’t ask
/// again.
@MainActor
enum UndoConfirm {
    static let defaultsKey = "confirmUndo"
    static var on: Bool { UserDefaults.standard.object(forKey: defaultsKey) as? Bool ?? true }

    /// The menu's item was clicked, where it names the step, rather than
    /// its key pressed: a key, or anything else, asks.
    static var clicked: Bool {
        switch NSApp.currentEvent?.type {
        case .leftMouseDown?, .leftMouseUp?, .rightMouseDown?, .rightMouseUp?, .otherMouseDown?, .otherMouseUp?: return true
        default: return false
        }
    }

    /// What op would take, as its question heads it: Undo Close “api-fix”?
    static func title(_ op: String, _ label: String) -> String {
        "\(op == "redo" ? "Redo" : "Undo") \(UndoText.title(label))?"
    }

    /// The question's alert: what op would take, what the core says of it,
    /// such as when it was taken, the button that goes ahead, which has
    /// Return, then Cancel, which has Esc, and Don’t ask again.
    static func alert(_ op: String, label: String, detail: String?, button: String) -> NSAlert {
        let alert = NSAlert()
        alert.messageText = title(op, label)
        alert.informativeText = detail ?? ""
        alert.addButton(withTitle: button)
        // Titled Cancel, it takes Esc.
        alert.addButton(withTitle: "Cancel")
        alert.showsSuppressionButton = true
        alert.suppressionButton?.title = "Don’t ask again"
        alert.alertStyle = .informational
        return alert
    }

    /// While alert is on show, the key that asked, pressed again, goes
    /// ahead as Return does: ⌘Z ⌘Z undoes, ⇧⌘Z ⇧⌘Z redoes. Remove the
    /// monitor it gives back once the alert is answered.
    static func again(_ alert: NSAlert, redo: Bool) -> Any? {
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak alert] event in
            MainActor.assumeIsolated {
                guard let alert, event.window === alert.window, isKey(event, redo: redo) else { return event }
                alert.buttons.first?.performClick(nil)
                return nil
            }
        }
    }

    /// Whether event is the key that asks: ⌘Z, or ⇧⌘Z to redo.
    nonisolated static func isKey(_ event: NSEvent, redo: Bool) -> Bool {
        event.charactersIgnoringModifiers?.lowercased() == "z"
            && event.modifierFlags.intersection([.command, .shift, .option, .control]) == (redo ? [.command, .shift] : .command)
    }
}

extension Store {
    /// Undoes the history's latest step, or redoes the one undone last.
    func undo(redo: Bool = false) {
        Task { await step(redo ? "redo" : "undo", UndoArgs()) }
    }

    /// ⌘Z and ⇧⌘Z: typing to take back in the text field that has the
    /// keyboard goes first, then the history, which asks first unless
    /// Settings › Tabs says not to. Edit › Undo, clicked, names its step, so
    /// it takes it at once.
    func undoKey(redo: Bool = false) {
        if let text = NSApp.keyWindow?.firstResponder as? NSTextView, let um = text.undoManager,
           redo ? um.canRedo : um.canUndo {
            NSApp.sendAction(Selector(redo ? "redo:" : "undo:"), to: nil, from: nil)
            return
        }
        // A code card's editor keeps ⌘Z to its file even with nothing left
        // to take back: the sessions' history is not what it shows.
        if NSApp.keyWindow?.firstResponder is CodeTextView {
            NSSound.beep()
            return
        }
        guard canUndo, let label = redo ? redoLabel : undoLabel else {
            NSSound.beep()
            return
        }
        guard UndoConfirm.on, !UndoConfirm.clicked else {
            undo(redo: redo)
            return
        }
        // One question at a time: a key pressed while one is on its way or
        // on show changes nothing.
        guard !undoAsking else { return }
        undoAsking = true
        Task { await askFirst(redo ? "redo" : "undo", label: label) }
    }

    /// Asks before op takes the step labelled label, then takes it if told
    /// to. The core asks, so the answer can only take the step it named;
    /// one from before undo-confirm is asked by the label the menu has.
    private func askFirst(_ op: String, label: String) async {
        guard canConfirmUndo else {
            let (go, off) = await confirm(op, label: label, detail: nil, button: op == "redo" ? "Redo" : "Undo")
            undoAsking = false
            if go { await step(op, UndoArgs(), also: off) } else { offNotice(off) }
            return
        }
        do {
            let r = try await client.call(op, UndoArgs(confirm: true), timeout: 60, as: UndoResult.self)
            guard r.did == "asked", let c = r.choice else {
                undoAsking = false
                report(r)
                return
            }
            // An uncertain step asks its own question instead, which asks
            // as much.
            guard c.confirm == true else {
                let key = ask(c, about: UndoConfirm.title(op, r.label))
                undoAsking = false
                if let key { await step(op, UndoArgs(choiceID: c.id, choice: key)) }
                return
            }
            let apply = c.options.first { $0.key == "apply" }
            let (go, off) = await confirm(op, label: r.label, detail: apply?.detail, button: apply?.label ?? (op == "redo" ? "Redo" : "Undo"))
            undoAsking = false
            if go { await step(op, UndoArgs(choiceID: c.id, choice: "apply"), also: off) } else { offNotice(off) }
        } catch {
            undoAsking = false
            say("Couldn’t \(op): \(error.localizedDescription)", error: true)
        }
    }

    /// Takes op with args, answering the core's questions as they come.
    /// also, when set, ends the notice that says what it did.
    private func step(_ op: String, _ args: UndoArgs, also: String? = nil) async {
        do {
            let r = try await client.call(op, args, timeout: 60, as: UndoResult.self)
            if r.did == "asked" {
                guard let c = r.choice, let key = ask(c, about: UndoConfirm.title(op, r.label)) else { return offNotice(also) }
                await step(op, UndoArgs(choiceID: c.id, choice: key), also: also)
                return
            }
            report(r, also: also)
        } catch let e as WireError where e.code == "stale" {
            // A step came in while the question was on show, and the answer
            // was about the one it named. The core's own words name the
            // terminal sidebar's key.
            let redo = op == "redo"
            let stale = "Nothing was \(redo ? "redone" : "undone"): the history changed while you were asked. \(redo ? "⇧⌘Z" : "⌘Z") asks about the step it would take now."
            say([stale, also].compactMap { $0 }.joined(separator: " "), error: true)
        } catch {
            say("Couldn’t \(op): \(error.localizedDescription)", error: true)
        }
    }

    /// Says what an undo or redo did.
    private func report(_ r: UndoResult, also: String? = nil) {
        let what = UndoText.title(r.label)
        let text: String
        switch r.did {
        case "undone": text = "Undid \(what)."
        case "redone": text = "Redid \(what)."
        case "settled": text = "\(what): it was that way already, so the history moved on."
        case "skipped": text = "Took \(what) out of the history."
        default: return offNotice(also)
        }
        say([text, also].compactMap { $0 }.joined(separator: " "), error: false, seconds: also == nil ? 3 : 6)
    }

    /// Says that ⌘Z no longer asks, when the question was turned off.
    private func offNotice(_ off: String?) {
        if let off { say(off, error: false, seconds: 6) }
    }

    /// The question a key asks before op takes the step labelled label:
    /// Undo Close “api-fix”?, with what the core says of it, such as when
    /// it was taken, in a sheet on Irie's window while that has the
    /// keyboard, else on its own. Return or the same key again takes it,
    /// and Esc changes nothing. It gives back whether to go ahead, and the
    /// words of a notice when Don’t ask again turned the question off.
    private func confirm(_ op: String, label: String, detail: String?, button: String) async -> (Bool, String?) {
        let alert = UndoConfirm.alert(op, label: label, detail: detail, button: button)
        let again = UndoConfirm.again(alert, redo: op == "redo")
        defer { if let again { NSEvent.removeMonitor(again) } }
        let r: NSApplication.ModalResponse
        if let w = WindowPanes.window, w.isKeyWindow, w.attachedSheet == nil {
            r = await withCheckedContinuation { done in
                alert.beginSheetModal(for: w) { done.resume(returning: $0) }
            }
        } else {
            r = alert.runModal()
        }
        var off: String?
        if alert.suppressionButton?.state == .on {
            UserDefaults.standard.set(false, forKey: UndoConfirm.defaultsKey)
            off = "⌘Z and ⇧⌘Z won’t ask again: Settings › Tabs › Undo brings the question back."
        }
        return (r == .alertFirstButtonReturn, off)
    }

    /// Answers a question the core asked about a step, as a typed :undo or
    /// :redo can bring one: in an alert, then back through op.
    func answer(_ op: String, _ c: UndoChoice) async {
        guard let key = ask(c) else { return }
        await step(op, UndoArgs(choiceID: c.id, choice: key))
    }

    /// Asks the core's question in an alert: each option a button, the
    /// recommended one first, so Return takes it, and Cancel on Esc. Headed
    /// by what it is about, as Undo Close “api-fix”?, when that is known. It
    /// returns the key picked, or nil to change nothing.
    private func ask(_ c: UndoChoice, about: String? = nil) -> String? {
        let alert = NSAlert()
        alert.messageText = about ?? c.question
        alert.informativeText = ((about == nil ? [] : [c.question]) + c.options.map { "\($0.label): \($0.detail)" }).joined(separator: "\n\n")
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
