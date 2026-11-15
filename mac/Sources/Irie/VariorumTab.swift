import AppKit
import SwiftUI

// The right sidebar's Variorum tab, the Writing plugin's second part
// (docs/proposals/writers.md, "The Variorum tab"): the note the shown
// session works on, as Variorum holds it, read-only. Each paragraph reads as
// its shown take, with its other takes and its comments under it, and Show
// in Variorum opens its takes there, where the writer chooses. The app never
// talks to Variorum: the core's variorum op calls Variorum's door for it, as
// a client of its own. Variorum logs every call to its door, reads too, and
// keeps only the last 500 lines across its clients, so the tab reads only as
// it shows, as it shows another session, when asked, and 2 s after the
// session's agent finishes a call to the door: never on a timer, and never
// while the tab or the sidebar is hidden.

/// A session's last read: its note, or why there is none, and the takes and
/// comments that the read before it of the same note didn't have.
struct VariorumRead: Equatable {
    var note: VariorumNote?
    var error: String?
    var fresh: Set<String> = []
}

/// What a Show in Variorum said, for a few seconds.
struct VariorumFlash: Equatable {
    let id = UUID()
    var text: String
    var failed = false
}

/// What the tab draws for the session it shows.
enum VariorumShows: Equatable {
    /// Nothing heard yet.
    case waiting
    /// A setup is missing: the core's words, and Connect.
    case setup(VariorumResult)
    /// Variorum is missing, or a kept setup is broken.
    case needs(VariorumResult)
    /// The core turned the tab off, which then leaves the sidebar.
    case off(String)
    /// Ready, with no session of Irie's shown.
    case pick
    /// Ready, and the session's agent has named no note yet.
    case empty
    case note(VariorumNote, fresh: Set<String>)
    /// The read failed: a sentence to show as it is.
    case problem(String)
}

/// The Variorum tab's state. The store keeps it while the app runs, so the
/// tab draws its last read at once when it shows again, and marks what came
/// since; it reads only while the tab shows (watch(_:)).
@MainActor
@Observable
final class VariorumModel {
    /// Where things stand: the op's last answer to any action, without its
    /// note, list or error.
    private(set) var stand: VariorumResult? {
        didSet { if Self.wanted(stand) != Self.wanted(oldValue) { store?.keepSideTab() } }
    }
    /// Each session's last read, by session, and "" for where things stand
    /// with no session shown.
    private(set) var reads: [String: VariorumRead] = [:]
    /// The reads on their way, by the same keys.
    private(set) var reading: Set<String> = []
    private(set) var connecting = false
    /// Why the last Connect kept nothing, in the app's words or the core's.
    private(set) var connectProblem: String?
    /// What Show in Variorum said, by paragraph and "" for the note, and the
    /// shows on their way.
    private(set) var flashes: [String: VariorumFlash] = [:]
    private(set) var showing: Set<String> = []
    /// The notes shared with Irie, as the menu of notes last listed them,
    /// and why it couldn't.
    private(set) var notes: [VariorumNoteRef]?
    private(set) var notesProblem: String?
    /// The note picked for each session, which its reads ask for instead of
    /// the one its agent last named.
    private(set) var picks: [String: String] = [:]
    /// The paragraphs whose other takes are open.
    private(set) var opened: Set<String> = []

    @ObservationIgnored weak var store: Store?
    /// The ids of each note's takes and comments at its last read, by note.
    @ObservationIgnored private var seen: [String: Set<String>] = [:]
    /// The session the tab shows, while it shows.
    @ObservationIgnored private var shown: String?
    @ObservationIgnored private var visible = false
    /// Counts the watches, so that only the last one's news counts.
    @ObservationIgnored private var watches = 0
    /// The read that an ended call to the door asked for, 2 s on.
    @ObservationIgnored private var due: Task<Void, Never>?
    /// The reads asked for while one was on its way.
    @ObservationIgnored private var again: Set<String> = []
    /// The calls to the door seen ended in this watch, and whether the
    /// conversation's first update is in: the calls in it are old news.
    @ObservationIgnored private var ended: Set<String> = []
    @ObservationIgnored private var seeded = false
    @ObservationIgnored private var fixture = false

    /// Whether the tab shows (Store.sideTabs): while the core hasn't turned
    /// it off, and Variorum is installed or one of its setups is kept, so
    /// that a Mac without Variorum keeps its four tabs. Nil until the core
    /// has said where things stand.
    var wanted: Bool? { Self.wanted(stand) }

    private static func wanted(_ s: VariorumResult?) -> Bool? {
        s.map { $0.state != "off" && ($0.installed == true || $0.sessions == true || $0.tab == true) }
    }

    /// What the tab draws for a session, or for none.
    func shows(_ session: String?) -> VariorumShows {
        let r = reads[session ?? ""]
        guard let s = stand else { return r?.error.map { VariorumShows.problem($0) } ?? .waiting }
        switch s.state {
        case "ready":
            guard session != nil else { return .pick }
            guard let r else { return .waiting }
            if let e = r.error { return .problem(e) }
            if let n = r.note { return .note(n, fresh: r.fresh) }
            return .empty
        case "setup": return .setup(s)
        case "needs": return .needs(s)
        default: return .off(s.text)
        }
    }

    /// Asks where things stand as the core connects and as the Writing
    /// plugin changes, so the tab shows only while the core can answer it.
    func check() {
        guard let store, store.canVariorum, !fixture else { return }
        Task {
            guard let r = try? await store.client.call("variorum", VariorumArgs(action: "status"), timeout: 5, as: VariorumResult.self) else { return }
            keep(r)
        }
    }

    /// Keeps where things stand from an answer.
    private func keep(_ r: VariorumResult) {
        let s = VariorumResult(state: r.state, text: r.text, sessions: r.sessions, tab: r.tab, installed: r.installed)
        if s != stand { stand = s }
    }

    // MARK: Reading

    /// The tab's work while it shows a session, or none, until the task ends
    /// as the tab hides or shows another: it reads, then follows the
    /// session's conversation for its agent's calls to Variorum's door.
    /// After a switch it waits a moment, so a walk through the cards reads
    /// only the one it stops on.
    func watch(_ id: String?) async {
        watches += 1
        let mine = watches
        let switched = visible && shown != id
        visible = true
        shown = id
        ended = []
        seeded = false
        due?.cancel()
        due = nil
        guard let store, store.canVariorum, !fixture else { return }
        if switched {
            try? await Task.sleep(nanoseconds: 350_000_000)
            guard !Task.isCancelled, mine == watches else { return }
        }
        await read()
        guard let id, !Task.isCancelled, mine == watches else { return }
        // The conversation's updates go in a batch at a time (Pace.swift).
        let batcher = Batcher<ConversationUpdate> { [weak self] updates in
            self?.took(updates, session: id, watch: mine)
        }
        while !Task.isCancelled, mine == watches {
            do {
                try await store.client.followConversation(id) { batcher.add($0) }
            } catch {
                if Task.isCancelled { return }
            }
            try? await Task.sleep(nanoseconds: 1_500_000_000)
        }
    }

    /// The tab or the sidebar is hidden: nothing is read until it shows.
    func hide() {
        visible = false
        shown = nil
        watches += 1
        due?.cancel()
        due = nil
        again = []
    }

    /// Notes the agent's calls to Variorum's door that ended, and reads 2 s
    /// after one does.
    private func took(_ updates: [ConversationUpdate], session id: String, watch: Int) {
        guard watch == watches, visible, shown == id else { return }
        var news = false
        for u in updates where u.session == id && u.agent == nil {
            for it in u.items ?? [] where Self.doorCall(it) {
                if ended.insert(it.id).inserted, seeded { news = true }
            }
            seeded = true
        }
        if news { soon() }
    }

    /// Whether a conversation's item is a call to Variorum's door that has
    /// ended: one of its tools as Claude Code names an MCP server's
    /// (mcp__SERVER__TOOL), from the door Irie gives a writing session or
    /// from a development build's.
    static func doorCall(_ it: ConversationItem) -> Bool {
        guard it.kind == "tool", let tool = it.tool, let state = it.state, state != "running" else { return false }
        return tool.hasPrefix("mcp__variorum__") || tool.hasPrefix("mcp__variorum-dev__")
    }

    /// Reads 2 s on, unless a read is due already: that one starts after the
    /// call ended, so it sees what the call did. The agent's calls make at
    /// most one read in any 2 s.
    private func soon() {
        guard due == nil else { return }
        due = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            guard let self, !Task.isCancelled else { return }
            self.due = nil
            await self.read()
        }
    }

    /// Reads the shown session's note now, the one picked for it if any, or
    /// with no session shown asks where things stand; never while the tab is
    /// hidden. A read asked for while one is on its way follows it.
    func read() async {
        due?.cancel()
        due = nil
        guard visible, let store, store.canVariorum, !fixture else { return }
        let id = shown
        let key = id ?? ""
        guard !reading.contains(key) else {
            again.insert(key)
            return
        }
        reading.insert(key)
        let args = id.map { VariorumArgs(action: "read", session: $0, note: picks[$0]) } ?? VariorumArgs(action: "status")
        do {
            // The core waits up to 10 s on Variorum.
            let r = try await store.client.call("variorum", args, timeout: id == nil ? 5 : 15, as: VariorumResult.self)
            keep(r)
            keepRead(r, key: key)
        } catch {
            let failed = VariorumRead(error: error.localizedDescription)
            if reads[key] != failed { reads[key] = failed }
        }
        reading.remove(key)
        if again.remove(key) != nil, visible, key == (shown ?? "") { await read() }
    }

    /// Keeps a read, marking each take and comment that the read before it
    /// of the same note didn't have; a note's first read marks none.
    private func keepRead(_ r: VariorumResult, key: String) {
        var next = VariorumRead(note: r.note, error: r.error)
        if let n = r.note {
            let ids = Set(n.blocks.flatMap { b in b.takes.map(\.id) + (b.comments ?? []).map(\.id) })
            if let before = seen[n.id] { next.fresh = ids.subtracting(before) }
            seen[n.id] = ids
        }
        if reads[key] != next { reads[key] = next }
    }

    // MARK: Acting

    /// Connect: keeps the setup copied in Variorum, from the clipboard. The
    /// app never shows the setup, logs it or keeps it: it goes to the core
    /// alone. Then the tab says where things stand, the core's words saying
    /// which setup to copy next, and once both are kept it reads, which is
    /// when Variorum asks the writer to let the tab in.
    func connect() {
        guard let store, !connecting, !fixture else { return }
        guard let setup = NSPasteboard.general.string(forType: .string),
              !setup.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            connectProblem = "The clipboard has no setup in it. Copy one in Variorum, then press Connect."
            return
        }
        connecting = true
        connectProblem = nil
        Task {
            do {
                let r = try await store.client.call("variorum", VariorumArgs(action: "connect", setup: setup), timeout: 15, as: VariorumResult.self)
                keep(r)
                connectProblem = r.error
                // Kept: the key leaves the clipboard, so it can't be
                // pasted into a chat by mistake.
                if r.error == nil, NSPasteboard.general.string(forType: .string) == setup {
                    NSPasteboard.general.clearContents()
                }
            } catch {
                connectProblem = error.localizedDescription
            }
            connecting = false
            if stand?.state == "ready" { await read() }
        }
    }

    /// Show in Variorum: the note, or one paragraph's takes, where the
    /// writer chooses. Variorum offers what it is asked to show, or shows it
    /// at once if the writer set it to; what it did shows for a few seconds,
    /// and a failure's sentence a little longer.
    func show(note: String, block: String?) {
        guard let store, !fixture else { return }
        let key = block ?? ""
        guard !showing.contains(key) else { return }
        showing.insert(key)
        Task {
            let f: VariorumFlash
            do {
                let args = VariorumArgs(action: "show", note: note, block: block, target: block == nil ? "note" : "takes")
                let r = try await store.client.call("variorum", args, timeout: 15, as: VariorumResult.self)
                keep(r)
                f = r.error.map { VariorumFlash(text: $0, failed: true) }
                    ?? VariorumFlash(text: r.shown == "offered" ? "Offered in Variorum" : "Shown in Variorum")
            } catch {
                f = VariorumFlash(text: error.localizedDescription, failed: true)
            }
            showing.remove(key)
            flashes[key] = f
            try? await Task.sleep(nanoseconds: f.failed ? 8_000_000_000 : 3_000_000_000)
            if flashes[key] == f { flashes[key] = nil }
        }
    }

    /// Lists the notes shared with Irie, as the menu of notes opens.
    func loadNotes() async {
        guard let store, !fixture else { return }
        notesProblem = nil
        do {
            let r = try await store.client.call("variorum", VariorumArgs(action: "notes"), timeout: 15, as: VariorumResult.self)
            keep(r)
            notes = r.notes ?? []
            notesProblem = r.error
        } catch {
            notesProblem = error.localizedDescription
        }
    }

    /// Shows another note for a session, nil for the one its agent last
    /// named, and reads it.
    func pick(_ note: String?, for session: String) {
        guard picks[session] != note else { return }
        picks[session] = note
        Task { await read() }
    }

    /// Opens or closes a paragraph's other takes.
    func toggle(_ block: String) {
        if opened.contains(block) { opened.remove(block) } else { opened.insert(block) }
    }

    /// Draws an answer with no core, for the snapshots.
    func useFixture(_ r: VariorumResult, session: String, fresh: Set<String> = [], opened: Set<String> = []) {
        fixture = true
        keep(r)
        reads[session] = VariorumRead(note: r.note, error: r.error, fresh: fresh)
        self.opened = opened
    }
}

/// What the snapshots show in the Variorum tab, with no core: a short note
/// whose second paragraph has two takes from a writing session beside the
/// writer's own, one of them new, and a comment; and the tab half set up.
enum VariorumFixture {
    static var ready: VariorumResult {
        VariorumResult(state: "ready", text: "Variorum is open, and both setups are kept.", sessions: true, tab: true, installed: true,
                       note: VariorumNote(id: "n-steps", title: "The Keeper’s Steps", blocks: [
                           VariorumBlock(id: "b1", shown: "t1", takes: [
                               VariorumTake(id: "t1", text: "Every evening the keeper climbed the hundred and twelve steps, counting them aloud so the dark would know she was coming."),
                           ]),
                           VariorumBlock(id: "b2", shown: "t2a", takes: [
                               VariorumTake(id: "t2a", text: "By the time the lamp was lit, the boats had already turned for home, and the sea had gone the colour of slate.", chosen: true),
                               VariorumTake(id: "t2b", label: "claude-code · tighter", text: "By lamplight the boats had turned for home and the sea had gone to slate."),
                               VariorumTake(id: "t2c", label: "claude-code · present tense", text: "By the time the lamp is lit, the boats have already turned for home, and the sea has gone the colour of slate."),
                           ], comments: [
                               VariorumComment(id: "c1", text: "“Slate” comes back in the last paragraph. Keep one of them?"),
                           ]),
                           VariorumBlock(id: "b3", shown: "t3", takes: [
                               VariorumTake(id: "t3", text: "Nobody had asked her to count the steps. She counted anyway, and below her the slate water listened."),
                           ]),
                       ], read: Date()))
    }

    /// The take that came since the read before.
    static let fresh: Set<String> = ["t2c"]
    static let opened: Set<String> = ["b2"]

    /// As the core says it (internal/variorum, status.go).
    static let setup = VariorumResult(
        state: "setup",
        text: "\(Brand.name) keeps Claude Code's setup, for writing sessions, but not Any MCP client's, for the Variorum tab: in Variorum's Settings ▸ MCP ▸ Connect a client, choose Any MCP client, copy its setup and press Connect on the Variorum tab, or run `pbpaste | \(Brand.slug) variorum connect`.",
        sessions: true, tab: false, installed: true)
}

/// The Variorum tab: the note the shown session works on.
@MainActor
struct VariorumTab: View {
    @Environment(Store.self) private var store

    /// The session it shows: the selected one of Irie's, not one outside.
    private var shown: String? { store.outsideSelection == nil ? store.selection : nil }

    /// What the tab's work follows: the session, and whether the core
    /// answers the tab yet.
    private struct Watch: Equatable {
        var session: String?
        var can: Bool
    }

    var body: some View {
        VariorumPage(model: store.variorum, session: shown)
            .task(id: Watch(session: shown, can: store.canVariorum)) { await watch() }
            .onDisappear { store.variorum.hide() }
    }

    private func watch() async {
        await store.variorum.watch(shown)
    }
}

/// The tab's page for a session, drawn from the model alone.
@MainActor
struct VariorumPage: View {
    let model: VariorumModel
    let session: String?

    var body: some View {
        switch model.shows(session) {
        case .waiting:
            ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
        case let .setup(s):
            VariorumSetup(model: model, stand: s)
        case let .needs(s):
            VariorumNeeds(model: model, stand: s)
        case let .off(text):
            SideNote(title: "Variorum is off", text: text)
        case .pick:
            SideNote(title: "Pick a session", text: "The note its agent works on in Variorum shows here, to read beside it.")
        case .empty:
            VariorumEmpty(model: model, session: session ?? "")
        case let .note(n, fresh):
            ScrollView {
                VariorumNoteView(model: model, session: session, note: n, fresh: fresh)
            }
        case let .problem(text):
            VariorumProblem(model: model, session: session, text: text)
        }
    }
}

/// A note as Variorum holds it: its title, which opens the menu of notes,
/// then each paragraph.
@MainActor
struct VariorumNoteView: View {
    let model: VariorumModel
    let session: String?
    let note: VariorumNote
    let fresh: Set<String>

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            header
            ForEach(note.blocks) { b in
                VariorumBlockView(model: model, note: note.id, block: b, fresh: fresh)
            }
            if note.more == true {
                Text("The note goes on in Variorum.")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 4)
            }
        }
        .padding(10)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            if let s = session {
                VariorumNotesMenu(model: model, session: s, title: note.title)
            } else {
                Text(note.title)
                    .font(Theme.font(14, .bold))
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
            // In a narrow sidebar the time goes without its words, so
            // Show in Variorum keeps its own.
            let time = note.read.formatted(date: .omitted, time: .shortened)
            ViewThatFits(in: .horizontal) {
                controls("Read at " + time)
                controls(time)
            }
        }
        .padding(.horizontal, 4)
    }

    private func controls(_ time: String) -> some View {
        HStack(spacing: 6) {
            if let f = model.flashes[""] {
                VariorumFlashText(flash: f)
            } else {
                Text(time)
                    .font(Theme.font(11))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                    .help("When \(Brand.name) read the note")
            }
            Spacer(minLength: 4)
            VariorumRefresh(model: model, session: session)
            if model.showing.contains("") {
                ProgressView().controlSize(.mini).frame(width: 20, height: 20)
            }
            Button { model.show(note: note.id, block: nil) } label: {
                Label("Show in Variorum", systemImage: "arrow.up.forward.app")
                    .font(Theme.font(11.5, .semibold))
                    .foregroundStyle(Theme.accentInk)
                    .lineLimit(1)
                    .fixedSize()
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(model.showing.contains(""))
            .help("Open the note in Variorum")
        }
    }
}

/// A paragraph: its shown take, the others under a disclosure, then its
/// comments, smaller. A dot marks a take or comment that the read before
/// didn't have.
@MainActor
private struct VariorumBlockView: View {
    let model: VariorumModel
    let note: String
    let block: VariorumBlock
    let fresh: Set<String>

    /// The take the note shows: its own pick, else the chosen one.
    private var top: VariorumTake? {
        block.takes.first { $0.id == block.shown } ?? block.takes.first { $0.chosen == true } ?? block.takes.first
    }

    var body: some View {
        let top = top
        let others = block.takes.filter { $0.id != top?.id }
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top, spacing: 6) {
                VStack(alignment: .leading, spacing: 2) {
                    if let name = block.name, !name.isEmpty {
                        Text(name)
                            .font(Theme.font(10.5, .semibold))
                            .foregroundStyle(Theme.muted)
                    }
                    Text(top?.text ?? "")
                        .font(.system(size: 12.5))
                        .foregroundStyle(Theme.ink)
                        .lineSpacing(2)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                        .modifier(NewMark(on: top.map { fresh.contains($0.id) } ?? false))
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                showButton
            }
            if !others.isEmpty { takes(others) }
            if let cs = block.comments, !cs.isEmpty { comments(cs) }
            if let f = model.flashes[block.id] { VariorumFlashText(flash: f) }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.card))
        .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        .accessibilityElement(children: .contain)
    }

    /// Show in Variorum for this paragraph: its takes, where the writer
    /// chooses.
    @ViewBuilder private var showButton: some View {
        if model.showing.contains(block.id) {
            ProgressView().controlSize(.mini).frame(width: 20, height: 20)
        } else {
            Button { model.show(note: note, block: block.id) } label: {
                Image(systemName: "arrow.up.forward.app")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 20, height: 20)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Show in Variorum: this paragraph’s takes, where you choose")
            .accessibilityLabel("Show in Variorum")
        }
    }

    /// The other takes, under a disclosure, each with its label.
    @ViewBuilder private func takes(_ others: [VariorumTake]) -> some View {
        let open = model.opened.contains(block.id)
        let n = others.count
        Button { model.toggle(block.id) } label: {
            HStack(spacing: 5) {
                Image(systemName: "chevron.right")
                    .font(.system(size: 8.5, weight: .bold))
                    .rotationEffect(.degrees(open ? 90 : 0))
                    .frame(width: 10)
                Text(n == 1 ? "1 other take" : "\(n) other takes")
                    .font(Theme.font(11, .semibold))
                Spacer(minLength: 0)
            }
            .foregroundStyle(Theme.muted)
            .contentShape(Rectangle())
            // A new take behind a closed disclosure marks it.
            .modifier(NewMark(on: !open && others.contains { fresh.contains($0.id) }))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(n == 1 ? "1 other take" : "\(n) other takes")
        .accessibilityValue(open ? "shown" : "hidden")
        if open {
            VStack(alignment: .leading, spacing: 9) {
                ForEach(others) { t in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(label(t))
                            .font(Theme.font(10.5, .medium))
                            .foregroundStyle(Theme.muted)
                        Text(t.text)
                            .font(.system(size: 12))
                            .foregroundStyle(Theme.ink)
                            .lineSpacing(1.5)
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .modifier(NewMark(on: fresh.contains(t.id)))
                }
            }
            .padding(.leading, 15)
        }
    }

    /// A take's label, or its place among the paragraph's takes.
    private func label(_ t: VariorumTake) -> String {
        let name = t.label.flatMap { $0.isEmpty ? nil : $0 } ?? "Take \((block.takes.firstIndex(of: t) ?? 0) + 1)"
        return t.chosen == true ? name + " · chosen" : name
    }

    private func comments(_ cs: [VariorumComment]) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            ForEach(cs) { c in
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Image(systemName: "text.bubble")
                        .font(.system(size: 9.5, weight: .medium))
                        .foregroundStyle(Theme.muted)
                    Text(c.text)
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .modifier(NewMark(on: fresh.contains(c.id)))
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Comment: " + c.text)
            }
        }
    }
}

/// The dot on a take or comment that came since the read before, in the
/// margin before its first line.
private struct NewMark: ViewModifier {
    let on: Bool

    func body(content: Content) -> some View {
        if on {
            content
                .overlay(alignment: .topLeading) {
                    Circle()
                        .fill(Theme.accent)
                        .frame(width: 6, height: 6)
                        .offset(x: -9.5, y: 5)
                        .accessibilityHidden(true)
                }
                .accessibilityValue("new")
        } else {
            content
        }
    }
}

/// The note's title, which opens the menu of notes shared with Irie; with
/// no title, a button that does. A pick shows that note for the session
/// until another pick.
@MainActor
private struct VariorumNotesMenu: View {
    let model: VariorumModel
    let session: String
    let title: String?
    @State private var open = false

    var body: some View {
        Group {
            if let title {
                Button { open = true } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 5) {
                        Text(title)
                            .font(Theme.font(14, .bold))
                            .foregroundStyle(Theme.ink)
                            .multilineTextAlignment(.leading)
                            .fixedSize(horizontal: false, vertical: true)
                        Image(systemName: "chevron.down")
                            .font(.system(size: 9, weight: .bold))
                            .foregroundStyle(Theme.muted)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(title), pick another note")
            } else {
                Button("Pick a Note…") { open = true }
            }
        }
        .help("Pick another note shared with \(Brand.name)")
        .popover(isPresented: $open, arrowEdge: .bottom) {
            VariorumNotesList(model: model, session: session) { open = false }
        }
    }
}

/// The menu of notes: the one the session's agent works on, then each note
/// shared with Irie, as Variorum lists them as the menu opens.
@MainActor
private struct VariorumNotesList: View {
    let model: VariorumModel
    let session: String
    let done: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 1) {
                row("The note its agent works on", detail: nil, on: model.picks[session] == nil) { model.pick(nil, for: session) }
                Divider().padding(.vertical, 4)
                if let notes = model.notes {
                    if notes.isEmpty, model.notesProblem == nil {
                        Text("No notes are shared with \(Brand.name) yet. In Variorum, share one with \(Brand.slug) at Read.")
                            .font(Theme.font(11.5))
                            .foregroundStyle(Theme.muted)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(6)
                    }
                    ForEach(notes) { n in
                        row(n.title, detail: edited(n), on: model.picks[session] == n.id) { model.pick(n.id, for: session) }
                    }
                } else if model.notesProblem == nil {
                    ProgressView().controlSize(.small).frame(maxWidth: .infinity).padding(8)
                }
                if let p = model.notesProblem {
                    Text(p)
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.needs)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(6)
                }
            }
            .padding(8)
        }
        .frame(width: 270)
        .frame(maxHeight: 360)
        .task { await model.loadNotes() }
    }

    private func row(_ title: String, detail: String?, on: Bool, action: @escaping () -> Void) -> some View {
        Button {
            action()
            done()
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Image(systemName: "checkmark")
                    .font(.system(size: 10, weight: .bold))
                    .foregroundStyle(Theme.accentInk)
                    .opacity(on ? 1 : 0)
                VStack(alignment: .leading, spacing: 1) {
                    Text(title)
                        .font(Theme.font(12.5, on ? .semibold : .regular))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(2)
                    if let detail {
                        Text(detail)
                            .font(Theme.font(10.5))
                            .foregroundStyle(Theme.muted)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 6)
            .padding(.vertical, 4)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? [.isSelected] : [])
    }

    /// When a note last changed, in words.
    private func edited(_ n: VariorumNoteRef) -> String? {
        guard let d = n.edited.flatMap(Wire.parseTime), d.timeIntervalSince1970 > 0 else { return nil }
        return "Changed " + d.formatted(.relative(presentation: .named))
    }
}

/// The read again button, or a spinner while a read is on its way.
@MainActor
private struct VariorumRefresh: View {
    let model: VariorumModel
    let session: String?

    var body: some View {
        if model.reading.contains(session ?? "") {
            ProgressView().controlSize(.mini).frame(width: 20, height: 20)
        } else {
            Button { Task { await model.read() } } label: {
                Image(systemName: "arrow.clockwise")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 20, height: 20)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Read the note again")
            .accessibilityLabel("Read the note again")
        }
    }
}

/// What a Show in Variorum said.
private struct VariorumFlashText: View {
    let flash: VariorumFlash

    var body: some View {
        Label(flash.text, systemImage: flash.failed ? "exclamationmark.circle" : "checkmark")
            .font(Theme.font(11, .semibold))
            .foregroundStyle(flash.failed ? Theme.needs : Theme.done)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// A setup is missing: the one next step, what is kept so far, and
/// Connect. After Connect, only an error shows in the core's words.
@MainActor
struct VariorumSetup: View {
    let model: VariorumModel
    let stand: VariorumResult

    /// Which of Variorum's two setups to copy next, from what is kept.
    private var step: String {
        switch (stand.sessions == true, stand.tab == true) {
        case (false, false): return "In Variorum, open Settings ▸ MCP ▸ Connect a client, choose Claude Code, copy, then press Connect."
        case (true, false): return "In Variorum's Settings ▸ MCP ▸ Connect a client, now choose Any MCP client, copy, then press Connect."
        case (false, true): return "In Variorum's Settings ▸ MCP ▸ Connect a client, now choose Claude Code, copy, then press Connect."
        case (true, true): return stand.text
        }
    }

    var body: some View {
        VStack(spacing: 12) {
            Text("Connect Variorum")
                .font(Theme.font(14, .bold))
                .foregroundStyle(Theme.ink)
            Text(step)
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            VStack(alignment: .leading, spacing: 5) {
                VariorumKept(title: "Writing sessions", on: stand.sessions == true)
                VariorumKept(title: "This tab", on: stand.tab == true)
            }
            VariorumConnect(model: model, prominent: true)
        }
        .frame(maxWidth: 280)
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// Variorum is missing, or a kept setup is broken: the core's words. With a
/// setup kept, Connect takes a fresh copy.
@MainActor
private struct VariorumNeeds: View {
    let model: VariorumModel
    let stand: VariorumResult

    var body: some View {
        VStack(spacing: 12) {
            Text("Variorum isn’t ready")
                .font(Theme.font(14, .bold))
                .foregroundStyle(Theme.ink)
            Text(stand.text)
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            Button("Check Again") { Task { await model.read() } }
                .controlSize(.small)
                .disabled(!model.reading.isEmpty)
            if stand.sessions == true || stand.tab == true {
                VariorumConnect(model: model, prominent: false)
            }
        }
        .frame(maxWidth: 280)
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// One of the two setups, and whether it is kept.
private struct VariorumKept: View {
    let title: String
    let on: Bool

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: on ? "checkmark.circle.fill" : "circle")
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(on ? Theme.done : Theme.muted)
            Text(title)
                .font(Theme.font(12, on ? .medium : .regular))
                .foregroundStyle(on ? Theme.ink : Theme.muted)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(title): \(on ? "connected" : "not connected yet")")
    }
}

/// Connect, and what it said when it kept nothing.
@MainActor
private struct VariorumConnect: View {
    let model: VariorumModel
    let prominent: Bool

    var body: some View {
        VStack(spacing: 8) {
            if prominent {
                button.buttonStyle(.borderedProminent).tint(Theme.accent)
            } else {
                button
            }
            if let p = model.connectProblem {
                Text(p)
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.needs)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Text("Connect reads the setup from the clipboard. \(Brand.name) keeps it, and never shows it or puts it in a chat.")
                .font(Theme.font(11))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var button: some View {
        Button(model.connecting ? "Connecting…" : "Connect") { model.connect() }
            .controlSize(.small)
            .disabled(model.connecting)
            .help("Keep the setup you copied in Variorum")
    }
}

/// Ready, and the session's agent has named no note yet.
@MainActor
private struct VariorumEmpty: View {
    let model: VariorumModel
    let session: String

    var body: some View {
        VStack(spacing: 10) {
            Text("No note yet")
                .font(Theme.font(14, .bold))
                .foregroundStyle(Theme.ink)
            Text("When this session’s agent opens a note in Variorum, the note shows here. Or pick one shared with \(Brand.name).")
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            VariorumNotesMenu(model: model, session: session, title: nil)
                .controlSize(.small)
        }
        .frame(maxWidth: 280)
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// A read that failed, in its own sentence, in place of the note.
@MainActor
private struct VariorumProblem: View {
    let model: VariorumModel
    let session: String?
    let text: String

    var body: some View {
        VStack(spacing: 12) {
            Text(text)
                .font(Theme.font(12.5))
                .foregroundStyle(Theme.ink)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            HStack(spacing: 8) {
                Button { Task { await model.read() } } label: { Label("Read Again", systemImage: "arrow.clockwise") }
                    .disabled(model.reading.contains(session ?? ""))
                // A note picked and no longer shared isn't a dead end.
                if let s = session { VariorumNotesMenu(model: model, session: s, title: nil) }
            }
            .controlSize(.small)
        }
        .frame(maxWidth: 280)
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
