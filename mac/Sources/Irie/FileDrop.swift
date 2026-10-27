import Foundation
import SwiftUI
#if canImport(SwiftTerm)
import SwiftTerm
#endif

/// Lets a file be dropped anywhere on the right pane, on the conversation or
/// the terminal. Its path goes into the shown agent's prompt, escaped for a
/// shell, the way Terminal, iTerm2 and Ghostty type a dropped file, and
/// Claude Code takes the path of an image as the image. Over the conversation
/// the path waits in the prompt and goes out with the next message.
struct FileDrop: ViewModifier {
    @EnvironmentObject private var store: Store
    let session: Session?

    func body(content: Content) -> some View {
        content.dropDestination(for: URL.self) { urls, _ in drop(urls) }
    }

    /// Why the shown agent can't take a file now, by the states the message
    /// box looks at (Composer).
    private var refusal: String? {
        guard let session else { return "Select a session first, then drop the file." }
        switch session.state {
        case "needs_you": return "\(session.name) is waiting for you in its terminal, so it can’t take a file now."
        case "exited", "failed": return "\(session.name) has ended, so it can’t take a file."
        case "starting", "stalled": return "\(session.name) isn’t ready yet, so it can’t take a file."
        default: return nil
        }
    }

    @MainActor private func drop(_ urls: [URL]) -> Bool {
        // A file reference names its file by number until it is resolved.
        let files = urls.filter(\.isFileURL).map { ($0 as NSURL).filePathURL ?? $0 }
        guard !files.isEmpty else {
            store.say("Only files can be dropped here.", error: true)
            return false
        }
        if let why = refusal {
            store.say(why, error: true)
            return false
        }
        // A terminal ends a dropped path with a space, so the next word starts apart from it.
        let text = files.map { Self.shellEscaped($0.path(percentEncoded: false)) }.joined(separator: " ") + " "
        guard paste(text) else {
            store.say("There’s no terminal in the window yet, so it can’t take a file.", error: true)
            return false
        }
        if !store.showTerminal, let session {
            store.say("Dropped into \(session.name)’s prompt. It goes with your next message.", error: false)
        }
        return true
    }

    /// Types the text into the terminal as a paste; false when there is no
    /// terminal, as in a build without SwiftTerm.
    @MainActor private func paste(_ text: String) -> Bool {
        #if canImport(SwiftTerm)
        guard let view = store.terminalView as? IrieTerminalView else { return false }
        view.pasteText(text)
        return true
        #else
        return false
        #endif
    }

    /// A path as one word for a shell: a backslash goes before each ASCII
    /// character the shell would act on. Everything else stays as it is, as
    /// in a terminal, which includes the narrow no-break space a screenshot's
    /// name has before AM or PM.
    static func shellEscaped(_ path: String) -> String {
        let special = Set(" \t\n\\'\"`()[]{}<>!#$&;|*?~".unicodeScalars)
        var out = String.UnicodeScalarView()
        for c in path.unicodeScalars {
            if special.contains(c) { out.append("\\") }
            out.append(c)
        }
        return String(out)
    }
}

#if canImport(SwiftTerm)
extension IrieTerminalView {
    /// Types text as a paste does, between the marks of a bracketed paste
    /// when the program asked for them. SwiftTerm's paste(_:) takes its text
    /// from the general pasteboard, so using it would overwrite what the user
    /// copied.
    func pasteText(_ text: String) {
        let bracketed = terminal.bracketedPasteMode
        if bracketed { send(data: EscapeSequences.bracketedPasteStart[0...]) }
        send(txt: text)
        if bracketed { send(data: EscapeSequences.bracketedPasteEnd[0...]) }
    }
}
#endif
