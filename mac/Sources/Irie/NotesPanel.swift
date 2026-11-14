import AppKit
import SwiftUI

/// Which line of the notepad has the keyboard: a note, or the last line,
/// where a new note is typed.
enum NoteFocus: Hashable {
    case note(String)
    case draft
}

/// The notepad (DESIGN §4.18), between the selected session's details and
/// the task bar: notes kept for later, each a line of a checklist, as in
/// Notes. Whatever is typed in the last line becomes a note: Return makes
/// it and the next one, Return in a note makes an empty one under it,
/// ⇧Return breaks a line inside one, and Backspace in an empty note takes
/// it away. The circle ticks a note off, or on again (⇧⌘U), and ⌃⌘↑ and
/// ⌃⌘↓ move one, Notes' own keys. The arrow beside a note, or ⌘Return,
/// sends it to the task bar as if it were typed there and sent: the core
/// ticks it off then, and the paper plane says when it went. A click on the
/// title folds the notepad to one line. The core keeps the notes
/// (Notes.swift): every change is saved as it is made, and a deleted note
/// waits in Recently Deleted.
@MainActor
struct NotesPanel: View {
    @Environment(Store.self) private var store
    @AppStorage(NotesPanel.openKey) private var open = true
    /// What is typed in the last line before it is a note, kept in the
    /// defaults as it is typed, as the task bar's draft is, so an install
    /// that stops the app loses none of it.
    @AppStorage(NotesPanel.draftKey) private var draft = ""
    @FocusState private var focus: NoteFocus?
    @State private var hovered: String?
    @State private var listHeight: CGFloat = 0

    static let openKey = "notesOpen"
    static let draftKey = "notesDraft"
    /// The most the list takes before it scrolls: about seven notes.
    static let tallest: CGFloat = 170

    var body: some View {
        let pad = store.notes.pad
        let shape = RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
        VStack(alignment: .leading, spacing: 0) {
            header(pad)
            if open {
                if let p = pad.problem, !p.isEmpty {
                    Text(p)
                        .font(Theme.font(11))
                        .foregroundStyle(Theme.sunInk)
                        .lineLimit(3)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                        .padding(.top, 4)
                }
                list(pad)
            }
        }
        .padding(.leading, Theme.inset - 2)
        .padding(.trailing, 8)
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(shape.fill(Theme.panel))
        .overlay(shape.strokeBorder(Theme.edge, lineWidth: 1))
        .clipShape(shape)
        .background(shape.fill(Theme.panel).shadow(color: Theme.shadow, radius: 3, y: 1.5))
        .padding(.horizontal, 10)
        .padding(.top, 6)
        .onChange(of: focus) { old, _ in left(old) }
        .onReceive(NotificationCenter.default.publisher(for: .irieNewNote)) { _ in newNote() }
    }

    // MARK: The title

    private func header(_ pad: Notepad) -> some View {
        let left = pad.notes.filter { !$0.isDone }.count
        return HStack(spacing: 4) {
            Button { withAnimation(.easeOut(duration: 0.15)) { open.toggle() } } label: {
                HStack(spacing: 5) {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 8.5, weight: .heavy))
                        .rotationEffect(.degrees(open ? 90 : 0))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 10)
                    Text("Notes")
                        .font(Theme.font(12, .heavy))
                        .foregroundStyle(Theme.ink)
                    // Folded, the next note to do shows after the title.
                    if !open, let next = pad.notes.first(where: { !$0.isDone && !$0.text.isBlank }) {
                        Text(next.text.firstLine)
                            .font(Theme.font(11.5))
                            .foregroundStyle(Theme.muted)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                    Spacer(minLength: 4)
                    if left > 0 {
                        Text("\(left)")
                            .font(Theme.font(11.5, .bold))
                            .monospacedDigit()
                            .foregroundStyle(Theme.muted)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(open ? "Fold the notes" : "Show the notes")
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Notes, \(left) to do")
            .accessibilityHint(open ? "Folds the notes" : "Shows the notes")
            .accessibilityAddTraits([.isHeader, .isButton])
            Menu {
                NotesMenu(pad: pad, newNote: newNote)
            } label: {
                Image(systemName: "ellipsis")
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .frame(width: 18)
            .font(.system(size: 11, weight: .semibold))
            .foregroundStyle(Theme.muted)
            .help("More")
            .accessibilityLabel("More about the notes")
        }
    }

    // MARK: The list

    private func list(_ pad: Notepad) -> some View {
        ScrollViewReader { proxy in
            ScrollView(.vertical) {
                VStack(alignment: .leading, spacing: 1) {
                    ForEach(pad.notes) { n in
                        row(n, in: pad.notes)
                            .id(NoteFocus.note(n.id))
                    }
                    if store.notes.canChange {
                        draftRow(first: pad.notes.isEmpty)
                            .id(NoteFocus.draft)
                    }
                }
                .padding(.top, 4)
                .background(GeometryReader { g in
                    Color.clear
                        .onAppear { listHeight = g.size.height }
                        .onChange(of: g.size.height) { listHeight = g.size.height }
                })
            }
            .scrollIndicators(.automatic)
            .frame(height: min(max(listHeight, 24), Self.tallest))
            .onChange(of: focus) { _, f in
                if let f { proxy.scrollTo(f) }
            }
        }
    }

    private func row(_ n: Note, in notes: [Note]) -> some View {
        let model = store.notes
        let sending = model.sending.contains(n.id)
        let showSend = hovered == n.id || focus == .note(n.id) || sending
        return HStack(alignment: .firstTextBaseline, spacing: 6) {
            CheckCircle(done: n.isDone) { model.check(n.id, !n.isDone) }
                .disabled(!model.canChange)
            TextField("", text: Binding(get: { model.note(n.id)?.text ?? n.text }, set: { model.edit(n.id, $0) }), axis: .vertical)
                .textFieldStyle(.plain)
                .font(Theme.font(12.5))
                .foregroundStyle(n.isDone ? Theme.muted : Theme.ink)
                .lineLimit(1...6)
                .disabled(!model.canChange)
                .focused($focus, equals: .note(n.id))
                .onSubmit { returned(n.id, in: notes) }
                .onKeyPress(keys: [.return], phases: .down) { press in
                    if press.modifiers.contains(.command) {
                        model.send(n.id)
                        return .handled
                    }
                    return press.modifiers.contains(.shift) ? softReturn() : .ignored
                }
                .onKeyPress(keys: ["u", "U"], phases: .down) { press in
                    guard press.modifiers.contains(.command), press.modifiers.contains(.shift) else { return .ignored }
                    model.check(n.id, !(model.note(n.id)?.isDone ?? false))
                    return .handled
                }
                .onKeyPress(.delete) { backspace(n.id, in: notes) }
                .onKeyPress(keys: [.upArrow, .downArrow], phases: [.down, .repeat]) { press in
                    let by = press.key == .upArrow ? -1 : 1
                    if press.modifiers.contains(.control), press.modifiers.contains(.command) {
                        return move(n.id, by: by, in: notes)
                    }
                    return press.modifiers.isEmpty ? step(from: n.id, by: by, in: notes) : .ignored
                }
                .onKeyPress(.escape) {
                    focus = nil
                    return .handled
                }
                .accessibilityLabel(n.isDone ? "Note, done" : "Note")
                .accessibilityAction(named: "Send as a jot") { model.send(n.id) }
            if let sent = n.sent {
                SentMark(note: n, sent: sent)
            }
            Button { model.send(n.id) } label: {
                Group {
                    if sending {
                        ProgressView().controlSize(.mini)
                    } else {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.system(size: 13))
                            .foregroundStyle(Theme.accentFill)
                    }
                }
                .frame(width: 16, height: 16)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(n.text.isBlank || sending)
            .opacity(showSend ? 1 : 0)
            .allowsHitTesting(showSend)
            .help("Send as a jot (⌘↩): as if typed in the task bar and sent")
            .accessibilityHidden(true)
        }
        .padding(.vertical, 1.5)
        .contentShape(Rectangle())
        .onHover { inside in
            if inside { hovered = n.id } else if hovered == n.id { hovered = nil }
        }
        .contextMenu { rowMenu(n, in: notes) }
    }

    /// The last line, where a new note is typed: Return makes it a note, and
    /// the line stays for the next. A paste of several lines makes a note of
    /// each.
    private func draftRow(first: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: "circle")
                .font(.system(size: 13))
                .foregroundStyle(Theme.muted.opacity(0.45))
                .frame(width: 16)
                .accessibilityHidden(true)
            TextField(first ? "A note for later" : "New note", text: $draft, axis: .vertical)
                .textFieldStyle(.plain)
                .font(Theme.font(12.5))
                .foregroundStyle(Theme.ink)
                .lineLimit(1...6)
                .focused($focus, equals: .draft)
                .onSubmit { commitDraft(keepFocus: true) }
                .onKeyPress(keys: [.return], phases: .down) { press in
                    press.modifiers.contains(.shift) ? softReturn() : .ignored
                }
                .onKeyPress(.delete) {
                    guard draft.isEmpty, let last = store.notes.pad.notes.last else { return .ignored }
                    focusEnd(.note(last.id))
                    return .handled
                }
                .onKeyPress(.upArrow) {
                    guard !draft.contains("\n"), let last = store.notes.pad.notes.last else { return .ignored }
                    focusEnd(.note(last.id))
                    return .handled
                }
                .onKeyPress(.escape) {
                    focus = nil
                    return .handled
                }
                .onChange(of: draft) { old, new in
                    // A paste of several lines is a note a line; a line
                    // broken with ⇧Return adds one character, and stays.
                    let breaks = { (t: String) in t.filter(\.isNewline).count }
                    if new.count > old.count + 1, breaks(new) > breaks(old) { commitDraft(split: true, keepFocus: true) }
                }
                .accessibilityLabel("New note")
                .accessibilityHint("Return keeps it as a note for later.")
            Spacer(minLength: 0).frame(width: 16)
        }
        .padding(.vertical, 1.5)
    }

    // MARK: Keys and actions

    private func newNote() {
        open = true
        DispatchQueue.main.async { focusEnd(.draft) }
    }

    /// Gives a line the keyboard with the caret at its end, as Notes does.
    /// A text field given the keyboard otherwise has all its text selected,
    /// and the next key would type over the whole note.
    private func focusEnd(_ f: NoteFocus) {
        focus = f
        // Once the field has the keyboard: two turns of the run loop, as
        // SwiftUI hands it over in the first.
        DispatchQueue.main.async {
            DispatchQueue.main.async {
                guard focus == f, let editor = NSApp.keyWindow?.firstResponder as? NSTextView else { return }
                let end = (editor.string as NSString).length
                editor.setSelectedRange(NSRange(location: end, length: 0))
            }
        }
    }

    /// Makes what is typed in the last line a note, or a note of each line
    /// when it was pasted; lines broken with ⇧Return stay in one note.
    private func commitDraft(split: Bool = false, keepFocus: Bool = false) {
        guard store.notes.canChange else { return }
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        let notes = split
            ? text.split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
            : [text].filter { !$0.isEmpty }
        if !draft.isEmpty { draft = "" }
        for n in notes { store.notes.add(n) }
        if keepFocus { focusEnd(.draft) }
    }

    /// Return in a note: an empty note under it, as in Notes, or the last
    /// line under the last note. An empty note goes instead.
    private func returned(_ id: String, in notes: [Note]) {
        let model = store.notes
        guard let note = model.note(id) else { return }
        if note.text.isBlank {
            model.delete([id])
            focusEnd(.draft)
            return
        }
        guard notes.last?.id != id else {
            focusEnd(.draft)
            return
        }
        let new = model.add("", after: id)
        DispatchQueue.main.async { focusEnd(.note(new)) }
    }

    /// Backspace in an empty note takes it away and goes to the note above.
    private func backspace(_ id: String, in notes: [Note]) -> KeyPress.Result {
        let model = store.notes
        guard let note = model.note(id), note.text.isEmpty else { return .ignored }
        let i = notes.firstIndex { $0.id == id } ?? 0
        model.delete([id])
        focusEnd(i > 0 ? .note(notes[i - 1].id) : (notes.count > 1 ? .note(notes[1].id) : .draft))
        return .handled
    }

    /// ⌃⌘↑ and ⌃⌘↓ move a note up or down the list, as in Notes.
    private func move(_ id: String, by d: Int, in notes: [Note]) -> KeyPress.Result {
        guard let i = notes.firstIndex(where: { $0.id == id }) else { return .ignored }
        if d < 0, i > 0 {
            store.notes.move(id, before: notes[i - 1].id)
        } else if d > 0, i < notes.count - 1 {
            store.notes.move(id, before: i + 2 < notes.count ? notes[i + 2].id : nil)
        }
        return .handled
    }

    /// ⇧Return breaks the line inside a note, as in Notes: the text view
    /// being typed in puts the break where the caret is.
    private func softReturn() -> KeyPress.Result {
        guard let editor = NSApp.keyWindow?.firstResponder as? NSTextView else { return .ignored }
        editor.insertNewlineIgnoringFieldEditor(nil)
        return .handled
    }

    /// ↑ and ↓ go from note to note, in a note of one line.
    private func step(from id: String, by d: Int, in notes: [Note]) -> KeyPress.Result {
        guard let i = notes.firstIndex(where: { $0.id == id }), !(store.notes.note(id)?.text.contains("\n") ?? false) else { return .ignored }
        let j = i + d
        if j < 0 { return .handled }
        focusEnd(j < notes.count ? .note(notes[j].id) : .draft)
        return .handled
    }

    /// When a line loses the keyboard: what was typed in the last line is a
    /// note, and a note left empty goes.
    private func left(_ old: NoteFocus?) {
        switch old {
        case .draft:
            commitDraft()
        case let .note(id):
            if let n = store.notes.note(id), n.text.isBlank { store.notes.delete([id]) }
        case nil:
            break
        }
    }

    private func delete(_ n: Note) {
        store.notes.delete([n.id])
        if !n.text.isBlank {
            store.say("Deleted “\(n.text.firstLine.cut(40))”. It waits in Recently Deleted for 30 days.", error: false, seconds: 6, restore: [n.id])
        }
    }

    @ViewBuilder private func rowMenu(_ n: Note, in notes: [Note]) -> some View {
        let model = store.notes
        let i = notes.firstIndex { $0.id == n.id } ?? 0
        Button("Send as a Jot (⌘↩)") { model.send(n.id) }
            .disabled(n.text.isBlank)
        Button(n.isDone ? "Mark as Not Done (⇧⌘U)" : "Mark as Done (⇧⌘U)") { model.check(n.id, !n.isDone) }
        if let s = n.session {
            Button("Show Its Session") { store.showNoteSession(s) }
        }
        Divider()
        Button("Move Up (⌃⌘↑)") { _ = move(n.id, by: -1, in: notes) }
            .disabled(i == 0)
        Button("Move Down (⌃⌘↓)") { _ = move(n.id, by: 1, in: notes) }
            .disabled(i >= notes.count - 1)
        Divider()
        Button("Delete") { delete(n) }
    }
}

/// The circle before a note: empty while it is to do, filled and ticked once
/// it is done, as in Notes.
private struct CheckCircle: View {
    let done: Bool
    let toggle: () -> Void

    var body: some View {
        Button(action: toggle) {
            Image(systemName: done ? "checkmark.circle.fill" : "circle")
                .font(.system(size: 13))
                .foregroundStyle(done ? Theme.accentFill : Theme.muted)
                .frame(width: 16)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(done ? "Done: click to make it to do again (⇧⌘U)" : "Tick it off (⇧⌘U)")
        .accessibilityLabel(done ? "Done" : "To do")
        .accessibilityAddTraits(done ? [.isButton, .isSelected] : .isButton)
    }
}

/// The paper plane after a note sent to the task bar: when it went, and a
/// click shows the session it started. It reads no rows, so the cards'
/// frames never draw the notepad again.
private struct SentMark: View {
    @Environment(Store.self) private var store
    let note: Note
    let sent: Date

    var body: some View {
        Button {
            if let s = note.session { store.showNoteSession(s) }
        } label: {
            Image(systemName: "paperplane.fill")
                .font(.system(size: 9.5))
                .foregroundStyle(Theme.muted)
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(help)
    }

    private var help: String {
        let when = Calendar.current.isDateInToday(sent)
            ? sent.formatted(date: .omitted, time: .shortened)
            : sent.formatted(.dateTime.month(.abbreviated).day().hour().minute())
        return note.session == nil ? "Sent as a jot at \(when)" : "Sent as a jot at \(when): click to show its session"
    }
}

/// The notepad's ⋯ menu.
@MainActor
private struct NotesMenu: View {
    @Environment(Store.self) private var store
    let pad: Notepad
    let newNote: @MainActor () -> Void

    var body: some View {
        let done = pad.notes.filter(\.isDone)
        Button("New Note", action: newNote)
        Button(done.isEmpty ? "Delete Done Notes" : "Delete \(done.count) Done \(done.count == 1 ? "Note" : "Notes")") {
            let ids = done.map(\.id)
            store.notes.delete(ids)
            store.say(done.count == 1 ? "Deleted 1 done note." : "Deleted \(done.count) done notes.", error: false, seconds: 6, restore: ids)
        }
        .disabled(done.isEmpty)
        Menu("Recently Deleted") {
            if pad.deleted.isEmpty {
                Text("Nothing deleted lately")
            } else {
                ForEach(pad.deleted.prefix(20)) { n in
                    Button(n.text.firstLine.cut(48)) { store.notes.restore([n.id]) }
                }
                if pad.deleted.count > 20 {
                    Text("and \(pad.deleted.count - 20) more")
                }
            }
        }
        if let path = pad.path {
            Divider()
            Button("Show the Notes File in Finder") { store.reveal(path) }
        }
    }
}

extension Notification.Name {
    /// File > New Note: the notepad opens with the keyboard in its last line.
    static let irieNewNote = Notification.Name("irieNewNote")
}

extension String {
    /// The first line, for a note shown on one line.
    var firstLine: String { split(whereSeparator: \.isNewline).first.map(String.init) ?? self }

    /// At most n characters, with an ellipsis when cut.
    func cut(_ n: Int) -> String { count > n ? prefix(n - 1) + "…" : self }
}
