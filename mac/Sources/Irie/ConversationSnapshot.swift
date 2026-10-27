import AppKit
import SwiftUI

/// `Irie --render-conversation DIR` draws the conversation view offscreen
/// with SwiftUI's ImageRenderer, from fixture items rather than a core, in
/// light and dark mode, and writes a PNG of each: the conversation, and its
/// foot while the agent waits on you. It is how the view is checked without
/// a screen (docs/MAC.md, "Build and run"). The window snapshot
/// (`--snapshot`) shows the same fixtures.
@MainActor
enum ConversationSnapshot {
    /// The views draw for ImageRenderer.
    static private(set) var rendering = false

    static func run(into dir: String) -> Never {
        rendering = true
        NSApplication.shared.setActivationPolicy(.accessory)
        let store = Store(socketPath: "/nonexistent/irie-snapshot.sock")
        store.loadFixture(Fixture.sessions, selection: Fixture.selected)
        do {
            try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
            // The conversation in every palette, light and dark, and the foot
            // of one that waits on you in the standard palette.
            var shots: [(String, String, String, Bool)] = []
            for p in Palette.all {
                for dark in [false, true] {
                    shots.append(("conversation", Fixture.selected, p.id, dark))
                }
            }
            for dark in [false, true] {
                shots.append(("waiting", "s01syncagent", Palette.standard.id, dark))
            }
            for (name, id, palette, dark) in shots {
                Theme.use(palette)
                let file = URL(fileURLWithPath: dir).appendingPathComponent("\(name)-\(palette)-\(dark ? "dark" : "light").png")
                try render(page(store, session: id), dark: dark, to: file)
                print(file.path)
            }
            // A helper beside the conversation, in each style (HelperPane.swift).
            for style in ["browser", "terminal"] {
                for palette in [Palette.standard.id, "evergreen"] {
                    for dark in [false, true] {
                        Theme.use(palette)
                        let file = URL(fileURLWithPath: dir).appendingPathComponent("split-\(style)-\(palette)-\(dark ? "dark" : "light").png")
                        try render(splitPage(store, style: style), dark: dark, to: file, width: 980)
                        print(file.path)
                    }
                }
            }
        } catch {
            FileHandle.standardError.write(Data("render-conversation: \(error.localizedDescription)\n".utf8))
            exit(1)
        }
        exit(0)
    }

    /// The view as the right side draws it, without the scroll view, which
    /// ImageRenderer leaves out.
    private static func page(_ store: Store, session id: String) -> some View {
        let model = ConversationModel()
        model.follow(id, client: store.client)
        let session = store.sessions.first { $0.id == id }!
        return VStack(spacing: 0) {
            FeedContent(items: model.items, pending: model.pending, needs: model.needs, session: session,
                        expanded: .constant(["t:fx-test", "t:fx-fail"]))
            Composer(model: model, session: session)
        }
        .frame(width: 760)
        .background(Theme.ground)
        .environmentObject(store)
    }

    /// The conversation with its running Explore helper beside it.
    private static func splitPage(_ store: Store, style: String) -> some View {
        let model = ConversationModel()
        model.follow(Fixture.selected, client: store.client)
        let helper = ConversationModel()
        helper.follow(Fixture.selected, agent: "sub:a1", client: store.client)
        let session = store.sessions.first { $0.id == Fixture.selected }!
        return HStack(alignment: .top, spacing: 0) {
            VStack(spacing: 0) {
                FeedContent(items: model.items, pending: model.pending, needs: model.needs, session: session, expanded: .constant([]))
                Composer(model: model, session: session)
            }
            .frame(width: 571)
            PaneDivider(width: .constant(400), total: 980)
            HelperPane(session: session, agent: "sub:a1", parent: model, style: style, model: helper)
                .frame(width: 400)
                .frame(maxHeight: .infinity)
        }
        .frame(width: 980)
        .background(Theme.ground)
        .environmentObject(store)
    }

    private static func render(_ view: some View, dark: Bool, to url: URL, width: CGFloat = 760) throws {
        let appearance = NSAppearance(named: dark ? .darkAqua : .aqua)!
        var data: Data?
        appearance.performAsCurrentDrawingAppearance {
            let r = ImageRenderer(content: view.environment(\.colorScheme, dark ? .dark : .light))
            r.scale = 2
            r.proposedSize = ProposedViewSize(width: width, height: nil)
            if let cg = r.cgImage {
                data = NSBitmapImageRep(cgImage: cg).representation(using: .png, properties: [:])
            }
        }
        guard let data else { throw CocoaError(.fileWriteUnknown) }
        try data.write(to: url)
    }
}

/// Conversations for the snapshots, for the fixture sessions, in
/// /home/alice. Only a run with --snapshot or --render-conversation gets
/// them.
enum ConversationFixture {
    static let active = CommandLine.arguments.contains("--snapshot") || CommandLine.arguments.contains("--render-conversation")

    static func update(for id: String, agent: String? = nil) -> ConversationUpdate? {
        guard active else { return nil }
        let now = Date()
        func at(_ minutesAgo: Double) -> Date { now.addingTimeInterval(-minutesAgo * 60) }
        func item(_ id: String, _ kind: String, _ ago: Double, text: String? = nil, phrase: String? = nil, detail: String? = nil,
                  tool: String? = nil, state: String? = nil, ms: Int64? = nil, added: Int? = nil, removed: Int? = nil,
                  group: String? = nil, output: String? = nil, agentType: String? = nil, tools: Int? = nil,
                  options: [String]? = nil, reason: String? = nil, queued: Bool? = nil, pending: Bool? = nil,
                  agent: String? = nil, from: String? = nil) -> ConversationItem {
            ConversationItem(id: id, kind: kind, time: at(ago), text: text, phrase: phrase, detail: detail, tool: tool, state: state,
                             ms: ms, added: added, removed: removed, group: group, output: output, agentType: agentType,
                             tools: tools, options: options, reason: reason, queued: queued, pending: pending,
                             agent: agent, from: from)
        }
        let site = "/home/alice/code/irie-docs-guide/docs"
        // The helpers' own conversations, as the core reads them from their
        // transcripts (HelperPane.swift).
        switch (id, agent) {
        case (Fixture.selected, "sub:a1"?):
            return ConversationUpdate(session: id, agent: agent, transcript: "/home/alice/fixture/subagents/agent-a1.jsonl", reset: true, items: [
                item("u:h1", "user", 3, text: "Read docs/DESIGN.md and say which of its sections a phrasebook page should link to.", from: "parent"),
                item("a:h1", "text", 2.9, text: "I’ll read the design notes, then the guide’s contents."),
                item("t:h-r1", "tool", 2.8, phrase: "Read DESIGN.md", detail: site + "/DESIGN.md", tool: "Read", state: "ok", ms: 140, group: "read"),
                item("t:h-s1", "tool", 2.5, phrase: "Searched for “glossary”", detail: "glossary", tool: "Grep", state: "ok", ms: 60, group: "search"),
                item("a:h2", "text", 1.5, text: "Sections 2 and 4.7 use the phrases. The glossary is in `docs/DESIGN.md`, near the top."),
                item("t:h-r2", "tool", 0.3, phrase: "Reading guide.md", detail: site + "/guide.md", tool: "Read", state: "running"),
            ])
        case (Fixture.selected, "sub:fx-glossary"?):
            return ConversationUpdate(session: id, agent: agent, transcript: "/home/alice/fixture/subagents/agent-fx-glossary.jsonl", reset: true, items: [
                item("u:g1", "user", 24.5, text: "Find where the glossary is kept and how pages link to it.", from: "parent"),
                item("t:g-s1", "tool", 24.4, phrase: "Searched for “glossary”", detail: "glossary", tool: "Grep", state: "ok", ms: 80, group: "search"),
                item("t:g-r1", "tool", 24.2, phrase: "Read DESIGN.md", detail: site + "/DESIGN.md", tool: "Read", state: "ok", ms: 120, group: "read"),
                item("a:g1", "text", 23.6, text: "The glossary is the list at the top of `docs/DESIGN.md`. Pages link to its terms by anchor, as `DESIGN.md#glossary`."),
            ])
        case (_, _?):
            return ConversationUpdate(session: id, agent: agent, reset: true, items: [])
        default:
            break
        }
        switch id {
        case Fixture.selected:
            return ConversationUpdate(session: id, transcript: "/home/alice/fixture.jsonl", reset: true, items: [
                item("u:1", "user", 25, text: "add the phrasebook page"),
                item("a:1", "text", 24.8, text: "I’ll add a **Phrasebook** page under `docs/` and link it from the guide."),
                item("t:fx-r1", "tool", 24.7, phrase: "Read guide.md", detail: site + "/guide.md", tool: "Read", state: "ok", ms: 120, group: "read"),
                item("t:fx-r2", "tool", 24.6, phrase: "Read DESIGN.md", detail: site + "/DESIGN.md", tool: "Read", state: "ok", ms: 90, group: "read"),
                item("t:fx-h", "agent", 24.5, text: "Find the glossary", phrase: "Asked a helper to find the glossary",
                     detail: "Find where the glossary is kept and how pages link to it.", tool: "Agent", state: "ok", ms: 61000,
                     agentType: "Explore", tools: 7, agent: "sub:fx-glossary"),
                item("t:fx-e1", "tool", 23, phrase: "Wrote phrasebook.md", detail: site + "/phrasebook.md", tool: "Write", state: "ok", ms: 40, added: 30, group: "edit"),
                item("t:fx-e2", "tool", 22.9, phrase: "Edited guide.md", detail: site + "/guide.md", tool: "Edit", state: "ok", ms: 35, added: 8, removed: 5, group: "edit"),
                item("t:fx-e3", "tool", 22.8, phrase: "Edited index.md", detail: site + "/index.md", tool: "Edit", state: "ok", ms: 30, added: 4, removed: 2, group: "edit"),
                item("t:fx-test", "tool", 22.5, phrase: "Ran the tests", detail: "go test ./...", tool: "Bash", state: "ok", ms: 40200),
                item("a:2", "text", 21.8, text: """
                    Added a Phrasebook page and linked it.

                    - New page: `docs/phrasebook.md`, with *greetings* and **thanks**
                    - Linked from the guide’s contents

                    ```sh
                    go test ./...
                    ```
                    """),
                item("u:2", "user", 6, text: "also link it from the home page", queued: true),
                item("t:fx-fail", "tool", 5.5, phrase: "Couldn’t build the site", detail: "make site", tool: "Bash", state: "error", ms: 2300,
                     output: "make: *** No rule to make target 'site'.  Stop."),
                item("t:fx-design", "agent", 3, text: "Read the design notes", phrase: "Asked a helper to read the design notes (in the background)",
                     detail: "Read docs/DESIGN.md and say which of its sections a phrasebook page should link to.", tool: "Agent", state: "ok",
                     ms: 90, agentType: "Explore", agent: "sub:a1"),
                item("t:fx-plan", "agent", 2, text: "Plan the home page’s links", phrase: "Asking a helper to plan the home page’s links",
                     detail: "Plan where the home page links to the phrasebook.", tool: "Agent", state: "running", agentType: "Plan", agent: "sub:a2"),
                item("t:fx-run", "tool", 0.2, phrase: "Editing home.md", detail: site + "/home.md", tool: "Edit", state: "running"),
            ], pending: [item("s:1", "user", 0.1, text: "and add a table of contents", queued: true, pending: true)])
        case "s01syncagent":
            return ConversationUpdate(session: id, transcript: "/home/alice/fixture2.jsonl", reset: true, items: [
                item("u:1", "user", 30, text: "tidy the build folder before the release"),
                item("a:1", "text", 29.5, text: "The old build output is in `build/`. I’ll clear it."),
            ], needs: item("needs", "needs", 1, text: "Asks: may it run rm -rf build?", reason: "permission"))
        default:
            return ConversationUpdate(session: id, reset: true, items: [])
        }
    }
}
