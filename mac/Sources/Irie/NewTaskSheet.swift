import SwiftUI

/// The new-task sheet (⌘N): where the task runs, whether it gets its own
/// worktree, which agent runs it and on which model, and the task itself.
/// The @repo and +agent words in the text still work, and win over the
/// menus, as in the task bar (DESIGN §4.2). ⌘Return starts it; the repo
/// chosen here is the task bar's from then on.
struct NewTaskSheet: View {
    @EnvironmentObject private var store: Store
    @Environment(\.dismiss) private var dismiss
    @State private var text = ""
    /// A repo's name or a folder's path; empty for a new scratch folder.
    @State private var repo = ""
    @State private var worktree = true
    /// An agent's name; empty for the default profile.
    @State private var agent = ""
    /// A model alias or name; empty for the agent's own.
    @State private var model = ""
    @FocusState private var textFocused: Bool

    /// The aliases Claude Code's `--model` takes for its latest models.
    private static let models = [("Fable", "fable"), ("Opus", "opus"), ("Sonnet", "sonnet")]

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack(spacing: 8) {
                SunMark().frame(width: 22, height: 22)
                Text("New task").font(Theme.font(18, .heavy)).foregroundStyle(Theme.ink)
            }
            Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 12) {
                GridRow {
                    label("Repo")
                    HStack(spacing: 10) {
                        Picker("Repo", selection: $repo) {
                            Text("Not in a repo").tag("")
                            ForEach(repoChoices, id: \.self) { r in Text(r.repoLabel).tag(r) }
                        }
                        .labelsHidden()
                        .frame(maxWidth: 200)
                        Button("Folder…", action: chooseFolder)
                        Toggle("Its own worktree", isOn: $worktree)
                            .disabled(repo.isEmpty)
                            .help("Off runs it in the repo's own checkout, as @repo! does")
                    }
                }
                GridRow {
                    label("Agent")
                    Picker("Agent", selection: $agent) {
                        Text("The default agent").tag("")
                        ForEach(store.agents) { a in
                            Text(a.note.isEmpty ? a.name : "\(a.name) · \(a.note)").tag(a.name)
                        }
                    }
                    .labelsHidden()
                    .frame(maxWidth: 360)
                }
                GridRow {
                    label("Model")
                    Picker("Model", selection: $model) {
                        Text("The agent's own").tag("")
                        ForEach(Self.models, id: \.1) { m in Text(m.0).tag(m.1) }
                    }
                    .labelsHidden()
                    .frame(maxWidth: 200)
                    .disabled(!modelApplies)
                    .help(modelApplies ? "Runs this task on the latest model of that name" : "This agent picks its own model")
                }
            }
            .font(Theme.font(13))
            VStack(alignment: .leading, spacing: 6) {
                TextEditor(text: $text)
                    .font(Theme.font(14))
                    .foregroundStyle(Theme.ink)
                    .scrollContentBackground(.hidden)
                    .padding(8)
                    .frame(minHeight: 130)
                    .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field))
                    .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                        .strokeBorder(textFocused ? Theme.accent : Theme.edge, lineWidth: textFocused ? 1.5 : 1))
                    .focused($textFocused)
                    .accessibilityLabel("The task")
                Text("What should we work on? @repo and +agent words in the text win over the menus.")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
            }
            HStack {
                if let n = store.notice, n.isError {
                    Text(n.text).font(Theme.font(12, .semibold)).foregroundStyle(Theme.needs).lineLimit(2)
                }
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button(store.spawning ? "Starting…" : "Start") { start() }
                    .keyboardShortcut(.return, modifiers: .command)
                    .buttonStyle(.borderedProminent)
                    .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || store.spawning)
                    .help("Start (⌘Return)")
            }
        }
        .padding(22)
        .frame(width: 560)
        .background(Theme.sidebar)
        .tint(Theme.accent)
        .onAppear {
            repo = store.taskRepo ?? ""
            textFocused = true
            store.loadChoices()
        }
    }

    private func label(_ s: String) -> some View {
        Text(s).font(Theme.font(13, .bold)).foregroundStyle(Theme.ink).gridColumnAlignment(.trailing)
    }

    /// The repos the core knows, and the folder chosen, if it is not one.
    private var repoChoices: [String] {
        var out = store.repoNames
        if !repo.isEmpty, !out.contains(repo) { out.insert(repo, at: 0) }
        return out
    }

    /// A model applies to a Claude Code profile, and only when the core takes
    /// one for a single task.
    private var modelApplies: Bool {
        store.canPickModel && (agent.isEmpty || store.agents.first { $0.name == agent }?.takesModel != false)
    }

    private func chooseFolder() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.prompt = "Use This Folder"
        panel.message = "The task runs in this folder: a git repo gets its own worktree."
        if panel.runModal() == .OK, let url = panel.url { repo = url.path }
    }

    private func start() {
        let input = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !input.isEmpty, !store.spawning else { return }
        store.dismissNotice()
        Task {
            let ok = await store.input(input, repo: repo.isEmpty ? nil : repo, worktree: worktree,
                                       agent: agent.isEmpty ? nil : agent, model: modelApplies && !model.isEmpty ? model : nil)
            if ok {
                store.setTaskRepo(repo.isEmpty ? nil : repo)
                dismiss()
            }
        }
    }
}
