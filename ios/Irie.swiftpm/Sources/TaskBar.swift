import SwiftUI

// The task bar: a task for the machine you pick, in the repo and with the
// agent you pick, in every word of the task bar's language (`input`,
// CORE.md §3). The @ and + words typed in the first line win over the
// chips; the chips show them.

@MainActor struct TaskBar: View {
    @Environment(AppModel.self) private var app
    @FocusState private var focused: Bool
    @State private var picking: Picking?

    enum Picking: String, Identifiable {
        case machine, repo, profile
        var id: String { rawValue }
    }

    var body: some View {
        @Bindable var app = app
        let text = app.draft.trimmingCharacters(in: .whitespacesAndNewlines)
        VStack(spacing: 8) {
            HStack(alignment: .bottom, spacing: 8) {
                TextField("New task…", text: $app.draft, axis: .vertical)
                    .lineLimit(1...6)
                    .font(.system(size: 16))
                    .focused($focused)
                    .padding(.vertical, 6)
                    .accessibilityLabel("New task")
                SendButton(enabled: !text.isEmpty && !app.taskBusy, busy: app.taskBusy, label: "Start the task") {
                    Task { await app.submitTask() }
                }
            }
            .padding(.leading, 15).padding(.trailing, 5).padding(.vertical, 5)
            .background(Theme.field, in: RoundedRectangle(cornerRadius: 23, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 23, style: .continuous).strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: 1))
            if focused || !text.isEmpty || picking != nil {
                chips
            }
        }
        .padding(.horizontal, 10).padding(.top, 8).padding(.bottom, 10)
        .background(Theme.sidebar)
        .overlay(alignment: .top) { Rectangle().fill(Theme.edge).frame(height: 0.5) }
        .onChange(of: app.draft) { app.saveDraft() }
        .sheet(item: $picking) { p in
            PickerSheet(kind: p)
                .presentationDetents([.medium, .large])
                .presentationDragIndicator(.visible)
        }
    }

    private var chips: some View {
        let c = app.taskConn
        let typed = Self.typed(app.draft)
        let prefs = c.map { app.prefs($0) } ?? TaskPrefs()
        let repo = typed.repo ?? prefs.repo
        let profile = typed.profile ?? prefs.profile
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 7) {
                if app.conns.count > 1, let c {
                    chip("", c.m.name, set: true, dot: true, label: "Machine: \(c.m.name)") { picking = .machine }
                }
                chip("@", typed.repo ?? (repo.isEmpty ? "No repo" : Fmt.base(repo)), set: !repo.isEmpty,
                     label: "Where it runs: \(repo.isEmpty ? "not in a repo" : repo)") { picking = .repo }
                chip("+", profile.isEmpty ? "Default agent" : profile, set: !profile.isEmpty,
                     label: "Agent: \(profile.isEmpty ? "the default" : profile)") { picking = .profile }
            }
            .padding(.horizontal, 2)
        }
    }

    private func chip(_ sym: String, _ text: String, set: Bool, dot: Bool = false, label: String, run: @escaping () -> Void) -> some View {
        Button(action: run) {
            HStack(spacing: 2) {
                if dot { Circle().fill(Theme.done).frame(width: 7, height: 7).padding(.trailing, 5) }
                if !sym.isEmpty { Text(sym).fontWeight(.heavy).padding(.trailing, 1) }
                Text(text).lineLimit(1)
                Image(systemName: "chevron.down").font(.system(size: 10, weight: .bold)).opacity(0.7).padding(.leading, 3)
            }
            .font(Theme.rounded(13.5, .semibold))
            .foregroundStyle(set ? Theme.accentInk : Theme.muted)
            .padding(.leading, 10).padding(.trailing, 8)
            .frame(minHeight: 34)
            .background(set ? Theme.accentFill.opacity(0.22) : Theme.panel, in: Capsule())
            .overlay(Capsule().strokeBorder(set ? Theme.accent.opacity(0.35) : Theme.edge, lineWidth: 1))
        }
        .buttonStyle(PressStyle(scale: 0.96))
        .accessibilityLabel(label)
    }

    /// The @ and + words typed in the first line.
    static func typed(_ text: String) -> (repo: String?, profile: String?) {
        let first = String(text.split(separator: "\n", omittingEmptySubsequences: false).first ?? "")
        func word(_ sigil: Character) -> String? {
            for w in first.split(separator: " ") where w.first == sigil && w.count > 1 { return String(w.dropFirst()) }
            return nil
        }
        return (word("@"), word("+"))
    }
}

/// A list to pick from, with a filter when it is long.
@MainActor private struct PickerSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    let kind: TaskBar.Picking
    @State private var options: [Option]?
    @State private var problem = ""
    @State private var query = ""

    struct Option: Identifiable {
        var value: String
        var label: String
        var note: String
        var id: String { value }
    }

    var body: some View {
        NavigationStack {
            Group {
                if let options {
                    let q = query.trimmingCharacters(in: .whitespaces).lowercased()
                    let shown = options.filter { q.isEmpty || ($0.label + " " + $0.note).lowercased().contains(q) }
                    List(shown) { o in
                        Button {
                            choose(o.value)
                        } label: {
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(o.label).font(Theme.rounded(16, .semibold)).foregroundStyle(Theme.ink)
                                    if !o.note.isEmpty { Text(o.note).font(.system(size: 13)).foregroundStyle(Theme.muted) }
                                }
                                Spacer()
                                if o.value == current {
                                    Image(systemName: "checkmark").font(.system(size: 15, weight: .bold)).foregroundStyle(Theme.accentInk)
                                }
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .listRowBackground(Theme.card)
                        .accessibilityAddTraits(o.value == current ? .isSelected : [])
                    }
                    .overlay {
                        if shown.isEmpty { Text("Nothing matches.").foregroundStyle(Theme.muted) }
                    }
                    .modifier(Filter(on: options.count >= 9, query: $query))
                } else if !problem.isEmpty {
                    Text(problem).font(.system(size: 15, weight: .medium)).foregroundStyle(Theme.needs).padding(24)
                        .frame(maxHeight: .infinity, alignment: .top)
                } else {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Loading…").foregroundStyle(Theme.muted)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.sidebar)
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
        .task { await load() }
    }

    private var title: String {
        switch kind {
        case .machine: return "Run it on"
        case .repo: return "Where it runs"
        case .profile: return "Agent"
        }
    }

    private var current: String {
        guard let c = app.taskConn else { return "" }
        switch kind {
        case .machine: return c.mid
        case .repo: return app.prefs(c).repo
        case .profile: return app.prefs(c).profile
        }
    }

    private func load() async {
        guard let c = app.taskConn else {
            problem = "No machine to run it on."
            return
        }
        do {
            switch kind {
            case .machine:
                options = app.conns.filter { $0.status != .unpaired }.map { Option(value: $0.mid, label: $0.m.name, note: $0.status.words) }
            case .repo:
                let r = try await c.call("complete", ["text": "@"], timeout: 8, as: CompleteResult.self)
                let names = (r.items ?? []).map { $0.hasPrefix("@") ? String($0.dropFirst()) : $0 }
                options = [Option(value: "", label: "Not in a repo", note: "A folder of its own")]
                    + names.map { Option(value: $0, label: "@" + $0, note: "Its own worktree") }
            case .profile:
                let r = try await c.call("complete", ["text": "+"], timeout: 8, as: CompleteResult.self)
                let notes = r.notes ?? []
                options = [Option(value: "", label: "The default", note: "As the machine’s config says")]
                    + (r.items ?? []).enumerated().map { k, i in
                        Option(value: i.hasPrefix("+") ? String(i.dropFirst()) : i, label: i, note: k < notes.count ? notes[k] : "")
                    }
            }
        } catch {
            let e = error as? APIError ?? APIError(code: "error", message: error.localizedDescription)
            if e.unpaired { app.unpaired(c, e.message) }
            problem = e.message
        }
    }

    private func choose(_ v: String) {
        Haptic.pick()
        if kind == .machine {
            app.pickMachine(v)
        } else if let c = app.taskConn {
            var p = app.prefs(c)
            if kind == .repo { p.repo = v } else { p.profile = v }
            app.setPrefs(c, p)
        }
        dismiss()
    }

    @MainActor private struct Filter: ViewModifier {
        var on: Bool
        @Binding var query: String

        func body(content: Content) -> some View {
            if on {
                content.searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Filter")
            } else {
                content
            }
        }
    }
}
