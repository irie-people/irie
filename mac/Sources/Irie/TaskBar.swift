import SwiftUI

/// The task bar at the foot of the sidebar. It is always three lines tall,
/// and longer text scrolls inside it. Nothing appears or vanishes around it,
/// so the list above never shifts, and nothing is ever drawn over it: the
/// hint is the placeholder, and notices show at the top of the working area
/// (Notices.swift). Beside the text, a menu picks the repo a task runs
/// in; an @repo word in the text wins over it, and +profile still picks the
/// agent. ⌘N opens the full new-task sheet. It empties as a task starts,
/// so the next can be typed at once; one that reads as more for the task
/// just sent goes to that task's agent instead (DESIGN §4.2). The pin beside
/// the start button is Send to Top: armed, the next task goes first in
/// Pinned, boosted (DESIGN §4.1), and ⌘Return sends one there without it.
@MainActor
struct TaskBar: View {
    @Environment(Store.self) private var store
    /// Kept in the defaults as it is typed, so an unsent task survives the
    /// app being stopped and relaunched, as every install does.
    @AppStorage("taskDraft") private var text = ""
    @FocusState private var focused: Bool
    /// Tasks started from the bar, newest last, kept across launches. ↑ walks
    /// back through them and ↓ forward again, ending on what was being typed.
    /// A note sent as a jot goes in too (TaskHistory).
    @State private var history = TaskHistory.load()
    @State private var recall: Int?
    @State private var draft = ""
    /// The text of a task that did not go, put back to fix: its coming back
    /// must not clear the notice that says why, as typing does.
    @State private var returned: String?

    var body: some View {
        HStack(alignment: .top, spacing: 6) {
            // It keeps the keyboard while a task starts and after, since the
            // task starts behind the session on show; submit() refuses a
            // second start until the first is done.
            TextField(placeholder, text: $text, axis: .vertical)
                .textFieldStyle(.plain)
                .font(Theme.font(13.5))
                .foregroundStyle(Theme.ink)
                .lineLimit(3, reservesSpace: true)
                .focused($focused)
                .onSubmit { submit() }
                // ⌘Return sends this one to the top, armed or not.
                .onKeyPress(keys: [.return], phases: .down) { press in
                    guard store.canSendToTop, press.modifiers.contains(.command) else { return .ignored }
                    submit(top: true)
                    return .handled
                }
                .onKeyPress(.upArrow) { step(-1) }
                .onKeyPress(.downArrow) { step(1) }
                .onChange(of: text) {
                    if text == returned { return }
                    returned = nil
                    if store.notice?.isError == true { store.dismissNotice() }
                }
                .onChange(of: store.focusTaskBar) { focused = true }
                .accessibilityLabel("New task")
                .accessibilityHint("Start with @repo or +profile to pick where and who. Return starts it"
                    + (store.canSendToTop ? ", and Command-Return sends it to the top." : "."))
            VStack(alignment: .trailing, spacing: 0) {
                RepoMenu(typed: TaskParser.target(text))
                Spacer(minLength: 4)
                HStack(spacing: 2) {
                    if store.canSendToTop { topButton }
                    startButton
                }
            }
        }
        .padding(.leading, Theme.inset)
        // The menu and the start button sit in the corner, as the message
        // box's send button does.
        .padding(.trailing, 6)
        .padding(.vertical, 6)
        .fixedSize(horizontal: false, vertical: true)
        .background(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous).fill(Theme.field))
        .overlay(
            RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
                .strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: focused ? 1.5 : 1)
        )
        .padding(10)
        .onReceive(NotificationCenter.default.publisher(for: .irieQuickTask)) { _ in focused = true }
        .onReceive(NotificationCenter.default.publisher(for: .irieTaskHistory)) { _ in
            if recall == nil { history = TaskHistory.load() }
        }
    }

    private var blank: Bool { text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    /// Send to Top is armed, and the core takes it.
    private var toTop: Bool { store.canSendToTop && store.sendToTop }

    /// While a task starts, the hint says how to add to it; one sent to the
    /// top never adds, so with Send to Top armed it says where it goes.
    private var placeholder: String {
        if toTop { return focused ? "@repo +profile, then the task for the top" : "What goes to the top?" }
        if store.spawning { return "Starting… “also …” adds to it" }
        return focused ? "@repo +profile, then the task" : "What should we work on?"
    }

    /// Send to Top, beside the start button: armed, the next task sent goes
    /// first in Pinned, over every card, and boosted, and the pin empties.
    /// Its pin, filled, is the one mark that it is armed.
    private var topButton: some View {
        let on = toTop
        return Button { store.setSendToTop(!on) } label: {
            Image(systemName: on ? "pin.fill" : "pin")
                .font(.system(size: 11.5, weight: .semibold))
                .frame(width: 24, height: 24)
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .foregroundStyle(on ? Theme.accentInk : Theme.muted)
        .help(on ? "Send to Top is on: the next task you send goes first in Pinned, boosted. Click to turn it off"
                 : "Send to Top: the next task you send goes first in Pinned, boosted. ⌘Return sends one there")
        .accessibilityLabel(on ? "Stop sending to the top" : "Send to Top")
    }

    private var startButton: some View {
            Button { submit() } label: {
                Group {
                    // A task still starting shows until the next is typed.
                    if store.spawning && blank {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: "arrow.up").font(.system(size: 12, weight: .bold))
                    }
                }
                .frame(width: 24, height: 24)
                .background(Circle().fill(Theme.accentFill))
                .foregroundStyle(Theme.onAccent)
            }
            .buttonStyle(.plain)
            .disabled(blank)
            .help("Start (Return)")
            .accessibilityLabel("Start the task")
    }

    /// ↑/↓ recall only from an empty bar or while already walking the history,
    /// so they still move the caret in text being typed.
    private func step(_ dir: Int) -> KeyPress.Result {
        guard !history.isEmpty, recall != nil || text.isEmpty else { return .ignored }
        let at = (recall ?? history.count) + dir
        if recall == nil { draft = text }
        if at < 0 { return .handled }
        if at >= history.count {
            recall = nil
            text = draft
        } else {
            recall = at
            text = history[at]
        }
        return .handled
    }

    private func remember(_ input: String) {
        recall = nil
        history = TaskHistory.remember(input)
    }

    /// Sends what was typed; top sends it to the top, as ⌘Return does.
    private func submit(top: Bool = false) {
        let input = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !input.isEmpty else { return }
        store.dismissNotice()
        // The bar empties at once, so the next can be typed while this one
        // starts; one that reads as more for it goes to its agent (DESIGN
        // §4.2). ↑ brings it back.
        remember(input)
        text = ""
        Task {
            let shown = store.selection
            if await store.jot(input, top: top) {
                // A task starts behind the session on show and the bar keeps
                // the keyboard for the next one; when nothing was on show, the
                // store gives the new agent's terminal the keyboard.
                if store.selection != shown, blank { focused = false }
            } else if blank {
                // It did not go: its text comes back, to fix, under the
                // notice that says why.
                returned = input
                text = input
            }
        }
    }
}

/// Where a task runs, beside the task bar's text: a repo the @ words know,
/// a folder, or no repo at all. An @repo typed in the text wins, and the
/// menu shows it in the accent.
@MainActor
struct RepoMenu: View {
    @Environment(Store.self) private var store
    /// The @ word typed in the text, if any.
    let typed: String?

    var body: some View {
        Menu {
            Picker("Repo", selection: Binding(get: { store.taskRepo ?? "" }, set: { store.setTaskRepo($0.isEmpty ? nil : $0) })) {
                ForEach(choices, id: \.self) { r in Text(r.repoLabel).tag(r) }
                Text("Not in a repo").tag("")
            }
            .pickerStyle(.inline)
            .labelsHidden()
            Divider()
            Button("Choose a Folder…") { store.chooseTaskFolder() }
        } label: {
            Text(label)
        }
        .menuStyle(.borderlessButton)
        .font(Theme.font(11.5, .bold))
        .fixedSize()
        .padding(.leading, 8)
        .padding(.trailing, 4)
        .padding(.vertical, 2)
        .background(Capsule().fill(Theme.panel))
        .overlay(Capsule().strokeBorder(Theme.edge, lineWidth: 1))
        .help(typed.map { "This task runs in @\($0), which the text names" } ?? "Where the task runs: a repo gets its own worktree")
        .accessibilityLabel("Repo: " + label)
    }

    /// The repos the core knows, and the folder chosen, if it is not one.
    private var choices: [String] {
        var out = store.repoNames
        if let r = store.taskRepo, !out.contains(r) { out.insert(r, at: 0) }
        return out
    }

    /// What the menu says, cut to fit beside the text.
    private var label: String {
        let full = typed.map { "@" + $0 } ?? store.taskRepo?.repoLabel ?? "No repo"
        return full.count > 12 ? full.prefix(11) + "…" : full
    }
}
