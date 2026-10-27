import SwiftUI

/// The task bar at the foot of the sidebar. It is always three lines tall,
/// and longer text scrolls inside it. Nothing appears or vanishes around it,
/// so the list above never shifts: the hint is the placeholder, and notices
/// float above the bar. Beside the text, a menu picks the repo a task runs
/// in; an @repo word in the text wins over it, and +profile still picks the
/// agent. ⌘N opens the full new-task sheet.
struct TaskBar: View {
    @EnvironmentObject private var store: Store
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        HStack(alignment: .top, spacing: 6) {
            // It keeps the keyboard while a task starts, then hands it to the
            // new agent's terminal; submit() refuses a second start until the
            // first is done.
            TextField(focused ? "@repo +profile, then the task" : "What should we work on?", text: $text, axis: .vertical)
                .textFieldStyle(.plain)
                .font(Theme.font(13.5))
                .foregroundStyle(Theme.ink)
                .lineLimit(3, reservesSpace: true)
                .focused($focused)
                .onSubmit(submit)
                .onChange(of: text) {
                    if store.notice?.isError == true { store.dismissNotice() }
                }
                .accessibilityLabel("New task")
                .accessibilityHint("Start with @repo or +profile to pick where and who. Return starts it.")
            VStack(alignment: .trailing, spacing: 0) {
                RepoMenu(typed: TaskParser.target(text))
                Spacer(minLength: 4)
                startButton
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, 5)
        .padding(.vertical, 5)
        .fixedSize(horizontal: false, vertical: true)
        .background(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous).fill(Theme.field)
            .shadow(color: Theme.shadow, radius: 3, y: 1.5))
        .overlay(
            RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
                .strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: focused ? 1.5 : 1)
        )
        .padding(10)
        .overlay(alignment: .top) { notice }
        .animation(.easeOut(duration: 0.15), value: store.notice)
        .onReceive(NotificationCenter.default.publisher(for: .irieQuickTask)) { _ in focused = true }
    }

    private var startButton: some View {
            Button(action: submit) {
                Group {
                    if store.spawning {
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
            .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || store.spawning)
            .help("Start (Return)")
            .accessibilityLabel("Start the task")
    }

    @ViewBuilder private var notice: some View {
            if let n = store.notice {
                Text(n.text)
                    .font(.system(size: 11.5, weight: .medium))
                    .foregroundStyle(n.isError ? Theme.needs : Theme.ink)
                    .lineLimit(4)
                    .textSelection(.enabled)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(
                        RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                            .fill(Theme.field)
                            .shadow(radius: 3, y: 1)
                            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                                .strokeBorder(n.isError ? Theme.needs.opacity(0.6) : Theme.edge, lineWidth: 1))
                    )
                    .padding(.horizontal, 10)
                    // 4 points above the field, over the bar's padding and the list
                    .alignmentGuide(.top) { d in d[.bottom] - 6 }
                    .transition(.opacity)
                    .accessibilityAddTraits(.updatesFrequently)
            }
    }

    private func submit() {
        let input = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !input.isEmpty, !store.spawning else { return }
        store.dismissNotice()
        if let q = Hows.parse(input) {
            if let s = Hows.find(store.sessions, q) {
                store.say(Hows.line(s), error: false, seconds: 8)
            } else {
                store.say("No agent matches “\(q)”.", error: true)
            }
            text = ""
            return
        }
        Task {
            if await store.spawn(input) {
                text = ""
                focused = false // the store gives the new agent's terminal the keyboard
            }
        }
    }
}

/// Where a task runs, beside the task bar's text: a repo the @ words know,
/// a folder, or no repo at all. An @repo typed in the text wins, and the
/// menu shows it in the accent.
struct RepoMenu: View {
    @EnvironmentObject private var store: Store
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
