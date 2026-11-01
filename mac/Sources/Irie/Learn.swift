import AppKit
import SwiftUI

/// The pages of the Learn window: free learning resources, and the glossary
/// of the words Irie and its agents use (docs/proposals/learning.md §7).
enum LearnTab: String, Hashable {
    case resources, glossary
}

/// The Help menu (docs/proposals/learning.md §7.1): Irie's guide pages on its
/// site, opened in the browser, and the Learn window's two pages.
struct LearnCommands: Commands {
    let store: Store
    @Environment(\.openWindow) private var openWindow

    static let site = URL(string: "https://irie-people.github.io/irie/")!
    static let pages = [
        ("Irie Guide", "docs.html"),
        ("Your First Session", "first-session.html"),
        ("Tasks, Repos and Worktrees", "tasks.html"),
        ("Keys and Commands", "keys.html"),
        ("Troubleshooting", "troubleshooting.html"),
    ]

    var body: some Commands {
        CommandGroup(replacing: .help) {
            ForEach(Self.pages, id: \.1) { page in
                Button(page.0) { NSWorkspace.shared.open(Self.site.appendingPathComponent(page.1)) }
            }
            Divider()
            Button("Learning Resources…") {
                MainActor.assumeIsolated { store.learnTab = .resources }
                openWindow(id: LearnWindow.id)
            }
            Button("Glossary…") {
                MainActor.assumeIsolated { store.learnTab = .glossary }
                openWindow(id: LearnWindow.id)
            }
        }
    }
}

/// The Learn window: the resources, the user's own links and the tutorial of
/// the shown session's language first, and the glossary. The core serves
/// both (the learn op), so the terminal sidebar and the site can say the
/// same.
struct LearnWindow: View {
    static let id = "learn"

    @EnvironmentObject private var store: Store
    @State private var result: LearnResult?
    @State private var loaded = false
    @State private var find = ""

    var body: some View {
        VStack(spacing: 0) {
            Picker("Page", selection: $store.learnTab) {
                Text("Resources").tag(LearnTab.resources)
                Text("Glossary").tag(LearnTab.glossary)
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(maxWidth: 260)
            .padding(.vertical, 12)
            if let result {
                switch store.learnTab {
                case .resources: ResourceList(resources: result.resources)
                case .glossary: GlossaryList(terms: result.glossary, find: $find)
                }
            } else if loaded {
                Text("These come from Irie’s core, which isn’t answering. Start it in a terminal with `irie up`.")
                    .font(Theme.font(13))
                    .foregroundStyle(Theme.muted)
                    .multilineTextAlignment(.center)
                    .padding(24)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(minWidth: 460, minHeight: 420)
        .background(Theme.sidebar)
        .tint(Theme.accent)
        .task(id: store.selection) {
            result = await store.learn()
            loaded = true
        }
    }
}

/// The resources by group, in the core's order: your links, the language at
/// hand, then the rest. A click opens one in the browser.
private struct ResourceList: View {
    let resources: [LearnResource]

    private var groups: [(name: String, items: [LearnResource])] {
        var out: [(name: String, items: [LearnResource])] = []
        for r in resources {
            let name = r.here == true ? "For this project" : r.group
            if let i = out.firstIndex(where: { $0.name == name }) {
                out[i].items.append(r)
            } else {
                out.append((name, [r]))
            }
        }
        return out
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 18) {
                ForEach(groups, id: \.name) { g in
                    VStack(alignment: .leading, spacing: 8) {
                        Text(g.name).font(Theme.font(12, .bold)).foregroundStyle(Theme.muted).textCase(.uppercase)
                        ForEach(g.items) { r in ResourceRow(resource: r) }
                    }
                }
                Text("Irie links to these and copies nothing of them. Each opens in your browser.")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
            }
            .padding(.horizontal, 22)
            .padding(.bottom, 22)
        }
    }
}

private struct ResourceRow: View {
    let resource: LearnResource
    @State private var hover = false

    var body: some View {
        Button {
            if let url = URL(string: resource.url) { NSWorkspace.shared.open(url) }
        } label: {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(resource.title).font(Theme.font(14, .bold)).foregroundStyle(Theme.ink)
                    Image(systemName: "arrow.up.right").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.muted)
                }
                if !resource.about.isEmpty {
                    Text(resource.about).font(Theme.font(12.5)).foregroundStyle(Theme.ink.opacity(0.85))
                }
                if let note = resource.note, !note.isEmpty {
                    Text(note).font(Theme.font(11.5)).foregroundStyle(Theme.muted)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 12)
            .padding(.vertical, 9)
            .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(hover ? Theme.card : Theme.card.opacity(0.6)))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
        .help(resource.url)
        .accessibilityElement(children: .combine)
        .accessibilityHint("Opens in your browser")
    }
}

/// The glossary in plain words, A to Z, with a field that finds a word in
/// either the word or what it means.
private struct GlossaryList: View {
    let terms: [LearnTerm]
    @Binding var find: String

    private var shown: [LearnTerm] {
        let q = find.trimmingCharacters(in: .whitespaces)
        return terms
            .filter { q.isEmpty || $0.term.localizedCaseInsensitiveContains(q) || $0.means.localizedCaseInsensitiveContains(q) }
            .sorted { $0.term.localizedCaseInsensitiveCompare($1.term) == .orderedAscending }
    }

    var body: some View {
        VStack(spacing: 10) {
            TextField("Find a word", text: $find)
                .textFieldStyle(.roundedBorder)
                .padding(.horizontal, 22)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    ForEach(shown) { t in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(t.term).font(Theme.font(14, .bold)).foregroundStyle(Theme.ink)
                            Text(t.means).font(Theme.font(12.5)).foregroundStyle(Theme.ink.opacity(0.85))
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        .accessibilityElement(children: .combine)
                    }
                    if shown.isEmpty {
                        Text("No word matches “\(find)”.").font(Theme.font(12.5)).foregroundStyle(Theme.muted)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 22)
                .padding(.bottom, 22)
            }
        }
    }
}
