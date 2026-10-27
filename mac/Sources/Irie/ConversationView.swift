import SwiftUI

/// The right side by default: the shown session's conversation. You and
/// Claude take turns; each tool call is one line in plain words that opens
/// to its exact command or path, runs of reads or edits gather into one
/// line, and subagents are chips. A message box at the foot types to the
/// agent through the core; while the agent waits on you in its terminal, a
/// banner opens the terminal instead. View > Terminal (⌥⌘T) flips to the
/// live terminal and back. Colours, type and shapes come from Theme and the
/// system's text styles only, so the view follows the palette.
struct ConversationView: View {
    @EnvironmentObject private var store: Store
    @StateObject private var model = ConversationModel()
    /// The palette: a switch draws the view again in its colours.
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id
    /// How wide the helper pane is beside the conversation (HelperPane.swift).
    @AppStorage("helperPaneWidth") private var paneWidth: Double = 380

    private var selected: Session? { store.sessions.first { $0.id == store.selection } }

    var body: some View {
        VStack(spacing: 0) {
            if let s = selected {
                GeometryReader { geo in
                    split(s, width: geo.size.width)
                }
            } else {
                ConversationNote(title: "Pick a session", text: "Its conversation shows here.")
            }
        }
        .id(theme)
        .background(Theme.ground)
        .onAppear { model.follow(store.selection, client: store.client) }
        .onChange(of: store.selection) { model.follow(store.selection, client: store.client) }
        .onChange(of: newestHelper) { if let s = selected, let a = newestHelper { store.noteHelper(a, in: s.id) } }
    }

    /// The newest helper the shown session's conversation names, which
    /// View > Show Helpers opens when none runs.
    private var newestHelper: String? {
        guard let s = selected else { return nil }
        return HelperRef.list(model.items, s).last?.agent
    }

    /// The conversation, with its open helper beside it when there is room
    /// for both, or in its place when there is not. The conversation stays
    /// in place either way, so it keeps its scroll and its draft.
    @ViewBuilder private func split(_ s: Session, width: CGFloat) -> some View {
        let agent = store.openHelpers[s.id]
        let wide = width >= HelperPane.sideBySide
        let alone = agent != nil && !wide
        ZStack {
            HStack(spacing: 0) {
                VStack(spacing: 0) {
                    ConversationFeed(model: model, session: s)
                    Composer(model: model, session: s)
                }
                .opacity(alone ? 0 : 1)
                .allowsHitTesting(!alone)
                .accessibilityHidden(alone)
                if let agent, wide {
                    PaneDivider(width: $paneWidth, total: width)
                    HelperPane(session: s, agent: agent, parent: model, style: store.helperStyle)
                        .frame(width: HelperPane.width(paneWidth, in: width))
                }
            }
            if let agent, alone {
                HelperPane(session: s, agent: agent, parent: model, narrow: true, style: store.helperStyle)
            }
        }
    }
}

/// The conversation, scrolled to its newest turn. It keeps to the bottom
/// while you are there, and holds still while you read further up.
struct ConversationFeed: View {
    @EnvironmentObject private var store: Store
    @ObservedObject var model: ConversationModel
    let session: Session
    @State private var expanded: Set<String> = []
    @State private var atBottom = true

    var body: some View {
        if !model.loaded {
            if let p = model.problem {
                ConversationNote(title: "Can’t follow this conversation", text: "\(p) Its terminal still works.", terminal: true)
            } else {
                ConversationNote(title: "Loading the conversation…", text: nil)
            }
        } else if model.items.isEmpty && model.pending.isEmpty && model.needs == nil {
            empty
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    FeedContent(items: model.items, pending: model.pending, needs: model.needs, session: session,
                                more: model.more, expanded: $expanded)
                    Color.clear.frame(height: 1).id("end")
                        .onAppear { atBottom = true }
                        .onDisappear { atBottom = false }
                }
                .defaultScrollAnchor(.bottom)
                .onChange(of: model.items.count) { if atBottom { proxy.scrollTo("end", anchor: .bottom) } }
                .onChange(of: model.pending.count) { proxy.scrollTo("end", anchor: .bottom) }
                .onChange(of: model.needs?.text) { if atBottom { proxy.scrollTo("end", anchor: .bottom) } }
                .onChange(of: session.id) { proxy.scrollTo("end", anchor: .bottom) }
            }
        }
    }

    @ViewBuilder private var empty: some View {
        switch session.state {
        case "starting":
            ConversationNote(title: "Starting…", text: "Its conversation shows here as soon as it begins.")
        case "stalled":
            ConversationNote(title: "It hasn’t started", text: session.detail.map { "\($0)." } ?? "", terminal: true)
        default:
            if model.transcript == nil {
                ConversationNote(title: "This one lives in its terminal",
                                 text: "It started before Irie could follow conversations, so there’s nothing to show here. Its terminal has all of it.",
                                 terminal: true)
            } else {
                ConversationNote(title: "Nothing here yet", text: "Messages show here as they come.")
            }
        }
    }
}

/// The turns of a conversation, the newest last, in a readable column.
struct FeedContent: View {
    let items: [ConversationItem]
    let pending: [ConversationItem]
    let needs: ConversationItem?
    let session: Session
    var more = false
    @Binding var expanded: Set<String>
    /// A helper's conversation: its replies under its kind, and its first
    /// message as the task from its session (HelperPane.swift).
    var helper: HelperVoice? = nil

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 18) {
            if more {
                Text("Earlier messages are in its terminal.")
                    .font(.caption).foregroundStyle(Theme.muted)
                    .frame(maxWidth: .infinity)
            }
            ForEach(Turn.make(items, pending: pending)) { turn in
                switch turn.kind {
                case .you(let it):
                    YouTurn(item: it, from: helper.map { it.from == "parent" ? "From \($0.parent)" : "Message" })
                case .claude(let parts):
                    ClaudeTurn(parts: parts, session: session, expanded: $expanded, name: helper?.kind ?? "Claude")
                case .note(let it):
                    NoteLine(text: it.text ?? "")
                }
            }
            if let needs {
                NeedsCard(item: needs, name: session.name)
            }
        }
        .padding(.horizontal, 24)
        .padding(.top, 30)
        .padding(.bottom, 18)
        .frame(maxWidth: 780)
        .frame(maxWidth: .infinity)
    }
}

/// A turn: a message from you, Claude's run of replies and tool calls, or a
/// note between them.
struct Turn: Identifiable {
    enum Kind {
        case you(ConversationItem)
        case claude([Part])
        case note(ConversationItem)
    }

    let id: String
    let kind: Kind

    /// One piece of Claude's turn.
    enum Part: Identifiable {
        case text(ConversationItem)
        case tool(ConversationItem)
        case group(String, [ConversationItem]) // read, edit or search, two or more in a row
        case agent(ConversationItem)

        var id: String {
            switch self {
            case let .text(it), let .tool(it), let .agent(it): return it.id
            case let .group(_, items): return "g:" + (items.first?.id ?? "")
            }
        }
    }

    static func make(_ items: [ConversationItem], pending: [ConversationItem]) -> [Turn] {
        var turns: [Turn] = []
        var parts: [Part] = []
        func flush() {
            if let first = parts.first {
                turns.append(Turn(id: "c:" + first.id, kind: .claude(gather(parts))))
            }
            parts = []
        }
        for it in items {
            switch it.kind {
            case "user":
                flush()
                turns.append(Turn(id: it.id, kind: .you(it)))
            case "note":
                flush()
                turns.append(Turn(id: it.id, kind: .note(it)))
            case "text":
                parts.append(.text(it))
            case "agent":
                parts.append(.agent(it))
            default:
                parts.append(.tool(it))
            }
        }
        flush()
        for it in pending {
            turns.append(Turn(id: it.id, kind: .you(it)))
        }
        return turns
    }

    /// Gathers runs of two or more finished calls of one group into one.
    private static func gather(_ parts: [Part]) -> [Part] {
        var out: [Part] = []
        var run: [ConversationItem] = []
        func end() {
            if run.count >= 2, let g = run.first?.group {
                out.append(.group(g, run))
            } else {
                out += run.map { .tool($0) }
            }
            run = []
        }
        for p in parts {
            if case let .tool(it) = p, let g = it.group, !g.isEmpty, it.state == "ok", run.isEmpty || run.first?.group == g {
                run.append(it)
                continue
            }
            end()
            if case let .tool(it) = p, let g = it.group, !g.isEmpty, it.state == "ok" {
                run.append(it)
            } else {
                out.append(p)
            }
        }
        end()
        return out
    }
}

/// Who speaks, with when.
private struct Speaker: View {
    let name: String
    let symbol: String
    let tint: Color
    var time: Date?
    var note: String?

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: symbol)
                .font(.callout)
                .foregroundStyle(tint)
                .accessibilityHidden(true)
            Text(name)
                .font(.system(.subheadline, design: .rounded).weight(.semibold))
            if let note {
                Text(note).font(.caption).foregroundStyle(Theme.muted)
            }
            Spacer(minLength: 8)
            if let time {
                Text(time, style: .time).font(.caption).foregroundStyle(Theme.muted)
            }
        }
    }
}

/// A message from you, on a soft card. A long one, such as a task, folds
/// after a dozen lines.
private struct YouTurn: View {
    let item: ConversationItem
    /// Who it is from, in a helper's conversation; you otherwise.
    var from: String? = nil
    @State private var whole = false

    private var text: String { item.text ?? "" }
    private var long: Bool { text.split(separator: "\n", omittingEmptySubsequences: false).count > 12 || text.count > 1400 }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Speaker(name: from ?? "You", symbol: from == nil ? "person.crop.circle.fill" : "arrow.turn.down.right",
                    tint: Theme.muted, time: item.when, note: note)
            VStack(alignment: .leading, spacing: 6) {
                Text(text)
                    .font(.body)
                    .lineLimit(long && !whole ? 12 : nil)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if long {
                    Button(whole ? "Show less" : "Show all") { whole.toggle() }
                        .buttonStyle(.plain)
                        .font(.caption.weight(.medium))
                        .foregroundStyle(Theme.sunInk)
                }
            }
            .padding(.horizontal, Theme.inset)
            .padding(.vertical, 9)
            .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.card))
            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                .strokeBorder(item.pending == true ? Theme.sunEdge.opacity(0.6) : Theme.edge, style: StrokeStyle(lineWidth: 1, dash: item.pending == true ? [4, 3] : [])))
            .opacity(item.pending == true ? 0.8 : 1)
        }
        .accessibilityElement(children: .combine)
    }

    private var note: String? {
        if item.pending == true {
            return item.queued == true ? "· it reads this at its next step" : "· on its way"
        }
        return item.queued == true ? "· sent while it worked" : nil
    }
}

/// Claude's replies and calls between two of your messages.
private struct ClaudeTurn: View {
    @EnvironmentObject private var store: Store
    let parts: [Turn.Part]
    let session: Session
    @Binding var expanded: Set<String>
    /// Who speaks: Claude, or a helper by its kind.
    var name = "Claude"

    private var time: Date? {
        for p in parts {
            switch p {
            case let .text(it), let .tool(it), let .agent(it):
                if let t = it.when { return t }
            case let .group(_, items):
                if let t = items.first?.when { return t }
            }
        }
        return nil
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Speaker(name: name, symbol: "sparkle", tint: Theme.sunInk, time: time)
            VStack(alignment: .leading, spacing: 6) {
                ForEach(parts) { part in
                    switch part {
                    case let .text(it):
                        MarkdownText(source: it.text ?? "")
                            .padding(.vertical, 2)
                    case let .tool(it):
                        CallRow(item: it, expanded: $expanded)
                    case let .group(kind, items):
                        GroupRow(kind: kind, items: items, expanded: $expanded)
                    case let .agent(it):
                        HelperChip(item: it, live: live(for: it), running: running(it), expanded: $expanded,
                                   isOpen: it.agent != nil && store.openHelpers[session.id] == it.agent,
                                   open: it.agent.map { a in { store.openHelper(a, in: session.id) } })
                    }
                }
            }
        }
    }

    /// What a running subagent does now, from the session's row: its own
    /// helper's, or else the one of its kind when only one runs.
    private func live(for it: ConversationItem) -> String? {
        if let a = it.agent, let h = session.subagents?.first(where: { $0.agent == a }) { return h.detail }
        guard it.state == "running", let type = it.agentType else { return nil }
        let same = (session.subagents ?? []).filter { $0.type == type }
        return same.count == 1 ? same[0].detail : nil
    }

    /// Whether its helper still runs: one started in the background has its
    /// call's result at once, while the session's row lists it running.
    private func running(_ it: ConversationItem) -> Bool {
        it.state == "running" || it.agent.map { a in session.subagents?.contains { $0.agent == a } == true } == true
    }
}

/// How a call went: an open ring while it runs, a check, a cross, or a dash
/// once its turn ended without it. The marks hold still.
struct CallMark: View {
    let state: String?

    var body: some View {
        Group {
            switch state {
            case "ok":
                Image(systemName: "checkmark.circle.fill").foregroundStyle(Theme.done)
            case "error":
                Image(systemName: "xmark.circle.fill").foregroundStyle(Theme.needs)
            case "stopped":
                Image(systemName: "minus.circle").foregroundStyle(Theme.muted)
            default:
                Circle().trim(from: 0, to: 0.72)
                    .stroke(Theme.work, style: StrokeStyle(lineWidth: 1.6, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .frame(width: 10, height: 10)
                    .frame(width: 13, height: 13)
            }
        }
        .font(.callout)
        .accessibilityLabel(CallMark.label(state))
    }

    static func label(_ state: String?) -> String {
        switch state {
        case "ok": return "done"
        case "error": return "failed"
        case "stopped": return "stopped"
        default: return "running"
        }
    }
}

/// The time a call took: 0.4 s, 40 s, 3 min, 1 h 5 min.
enum Took {
    static func short(_ ms: Int64) -> String {
        let s = Double(ms) / 1000
        switch s {
        case ..<10: return String(format: "%.1f s", s)
        case ..<60: return "\(Int(s.rounded())) s"
        case ..<3600: return "\(Int(s / 60)) min"
        default:
            let m = Int(s / 60)
            return m % 60 == 0 ? "\(m / 60) h" : "\(m / 60) h \(m % 60) min"
        }
    }
}

/// Lines an edit added and removed.
private struct Changes: View {
    let added: Int
    let removed: Int

    var body: some View {
        if added > 0 || removed > 0 {
            HStack(spacing: 4) {
                if added > 0 { Text("+\(added)").foregroundStyle(Theme.done) }
                if removed > 0 { Text("−\(removed)").foregroundStyle(Theme.needs) }
            }
            .font(.callout.monospacedDigit())
            .accessibilityLabel("\(added) lines added, \(removed) removed")
        }
    }
}

/// The card round a call, a run of calls or the detail they open to.
private struct CallCard<Content: View>: View {
    var failed = false
    @ViewBuilder var content: Content

    var body: some View {
        content
            .padding(.horizontal, Theme.inset)
            .padding(.vertical, 7)
            .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.card))
            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                .strokeBorder(failed ? Theme.needs.opacity(0.5) : Theme.edge, lineWidth: 1))
    }
}

/// The one line of a call or a run of calls: open it to see more.
private struct CallLine: View {
    let open: Bool
    let state: String?
    let phrase: String
    var added = 0
    var removed = 0
    var ms: Int64?
    let toggle: () -> Void

    var body: some View {
        Button(action: toggle) {
            HStack(spacing: 8) {
                Image(systemName: "chevron.right")
                    .font(.caption2.weight(.bold))
                    .foregroundStyle(Theme.muted)
                    .rotationEffect(.degrees(open ? 90 : 0))
                    // At the front of its slot, so its tip lines up with the
                    // text of the cards around it.
                    .frame(width: 10, alignment: .leading)
                CallMark(state: state)
                Text(phrase)
                    .font(.callout)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Spacer(minLength: 8)
                Changes(added: added, removed: removed)
                if let ms, ms > 0 {
                    Text(Took.short(ms)).font(.callout.monospacedDigit()).foregroundStyle(Theme.muted)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(open ? "Hide the details" : "Show the details")
        .accessibilityLabel("\(phrase), \(CallMark.label(state))")
        .accessibilityHint(open ? "Hides the details" : "Shows the details")
    }
}

/// One tool call in plain words, which opens to its exact command or path.
private struct CallRow: View {
    let item: ConversationItem
    @Binding var expanded: Set<String>

    var body: some View {
        let open = expanded.contains(item.id)
        CallCard(failed: item.state == "error") {
            VStack(alignment: .leading, spacing: 8) {
                CallLine(open: open, state: item.state, phrase: item.phrase ?? item.tool ?? "A tool call",
                         added: item.added ?? 0, removed: item.removed ?? 0, ms: item.ms) {
                    expanded.formSymmetricDifference([item.id])
                }
                if open {
                    if let d = item.detail, !d.isEmpty {
                        Mono(text: d)
                    }
                    if let o = item.output, !o.isEmpty {
                        Mono(text: o, tint: Theme.needs)
                    }
                    if (item.detail ?? "").isEmpty && (item.output ?? "").isEmpty {
                        Text(item.tool ?? "No details").font(.caption).foregroundStyle(Theme.muted)
                    }
                }
            }
        }
    }
}

/// A run of reads, edits or searches as one line: "Edited 3 files".
private struct GroupRow: View {
    let kind: String
    let items: [ConversationItem]
    @Binding var expanded: Set<String>

    private var id: String { "g:" + (items.first?.id ?? "") }

    var body: some View {
        let open = expanded.contains(id)
        CallCard {
            VStack(alignment: .leading, spacing: 6) {
                CallLine(open: open, state: "ok", phrase: phrase,
                         added: items.reduce(0) { $0 + ($1.added ?? 0) }, removed: items.reduce(0) { $0 + ($1.removed ?? 0) },
                         ms: items.reduce(0) { $0 + ($1.ms ?? 0) }) {
                    expanded.formSymmetricDifference([id])
                }
                if open {
                    ForEach(items) { it in
                        CallRow(item: it, expanded: $expanded)
                    }
                    .padding(.leading, 18)
                }
            }
        }
    }

    private var phrase: String {
        let files = Set(items.compactMap { $0.detail.map(Self.base) })
        let one = files.count == 1 ? files.first : nil
        switch kind {
        case "edit":
            return one.map { "Edited \($0) \(items.count) times" } ?? "Edited \(files.count) files"
        case "read":
            return one.map { "Read \($0) \(items.count) times" } ?? "Read \(files.count) files"
        default:
            return "Searched \(items.count) times"
        }
    }

    static func base(_ path: String) -> String {
        (path.split(separator: "\n").first.map(String.init) ?? path).split(separator: "/").last.map(String.init) ?? path
    }
}

/// A subagent Claude started: its kind and task, how it went, and what it
/// does now while it runs. One whose helper Irie knows opens that helper
/// beside the conversation (HelperPane.swift); any other unfolds in place.
private struct HelperChip: View {
    let item: ConversationItem
    let live: String?
    /// Its helper still runs, though its call may have its result already.
    var running = false
    @Binding var expanded: Set<String>
    /// Its helper shows beside the conversation.
    var isOpen = false
    /// Shows its helper beside the conversation; nil while Irie doesn't know
    /// which helper it is.
    var open: (() -> Void)? = nil

    var body: some View {
        let unfolded = open == nil && expanded.contains(item.id)
        VStack(alignment: .leading, spacing: 6) {
            Button {
                if let open { open() } else { expanded.formSymmetricDifference([item.id]) }
            } label: {
                HStack(spacing: 7) {
                    Image(systemName: "arrow.turn.down.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(Theme.muted)
                    Text(item.agentType ?? "Helper")
                        .font(.callout.weight(.semibold))
                    Text(item.text ?? item.phrase ?? "")
                        .font(.callout)
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                        .truncationMode(.tail)
                    Spacer(minLength: 6)
                    CallMark(state: running ? "running" : item.state)
                    if let ms = item.ms, ms > 0, !running {
                        Text(Took.short(ms)).font(.callout.monospacedDigit()).foregroundStyle(Theme.muted)
                    }
                    if open != nil {
                        Image(systemName: "chevron.right")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(Theme.muted)
                            .accessibilityHidden(true)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(open != nil ? "Show this helper beside the conversation" : "")
            .accessibilityLabel("Helper \(item.agentType ?? ""): \(item.phrase ?? ""), \(CallMark.label(running ? "running" : item.state))")
            .accessibilityHint(open != nil ? "Shows it beside the conversation" : "")
            if let live, running {
                Text(live)
                    .font(.caption)
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                    .padding(.leading, 22)
            }
            if unfolded {
                if let tools = item.tools, tools > 0 {
                    Text(tools == 1 ? "1 tool call" : "\(tools) tool calls")
                        .font(.caption).foregroundStyle(Theme.muted)
                }
                if let d = item.detail, !d.isEmpty {
                    Mono(text: d)
                }
            }
        }
        .padding(.horizontal, Theme.inset)
        .padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous).fill(Theme.panel))
        .overlay(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
            .strokeBorder(isOpen ? Theme.accent : Theme.edge, lineWidth: isOpen ? 1.5 : 1))
    }
}

/// Exact text, such as a command, in a fixed-width face.
private struct Mono: View {
    let text: String
    var tint: Color = .primary

    var body: some View {
        Text(text)
            .font(.system(.footnote, design: .monospaced))
            .foregroundStyle(tint)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(8)
            .background(RoundedRectangle(cornerRadius: Theme.radius - 4, style: .continuous).fill(Theme.panel))
    }
}

/// A note between turns: an interruption, a compaction, a new conversation.
private struct NoteLine: View {
    let text: String

    var body: some View {
        HStack(spacing: 10) {
            Rectangle().fill(Theme.edge).frame(height: 1)
            Text(text).font(.caption).foregroundStyle(Theme.muted).fixedSize()
            Rectangle().fill(Theme.edge).frame(height: 1)
        }
        .accessibilityElement(children: .combine)
    }
}

/// What the session waits on you for, at the end of the conversation.
struct NeedsCard: View {
    @EnvironmentObject private var store: Store
    let item: ConversationItem
    let name: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Circle().fill(Theme.needs).frame(width: 8, height: 8).accessibilityHidden(true)
                Text(item.text ?? "Needs you")
                    .font(.body.weight(.semibold))
                    .foregroundStyle(Theme.needs)
                    .textSelection(.enabled)
            }
            if let options = item.options, !options.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(options, id: \.self) { o in
                        HStack(spacing: 6) {
                            Circle().strokeBorder(Theme.needs.opacity(0.6), lineWidth: 1).frame(width: 9, height: 9)
                            Text(o).font(.callout)
                        }
                    }
                }
                .padding(.leading, 16)
            }
            if let plan = item.detail, !plan.isEmpty {
                MarkdownText(source: plan)
                    .padding(10)
                    .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.card))
            }
            Button {
                store.showTerminal = true
            } label: {
                Label("Answer in its terminal", systemImage: "terminal")
            }
            .buttonStyle(SoftButton(prominent: true))
            .help("Show \(name)’s terminal (⌥⌘T)")
        }
        .padding(Theme.inset)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.needsTint))
        .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(Theme.needs.opacity(0.45), lineWidth: 1))
        .accessibilityElement(children: .contain)
    }
}

/// The message box, or why there is none now, and the way to the terminal.
struct Composer: View {
    @EnvironmentObject private var store: Store
    @ObservedObject var model: ConversationModel
    let session: Session
    @State private var text = ""
    @State private var problem: String?
    @FocusState private var focused: Bool

    private var waits: Bool { session.state == "needs_you" }
    private var ended: Bool { session.state == "exited" || session.state == "failed" }
    private var early: Bool { session.state == "starting" || session.state == "stalled" }

    var body: some View {
        VStack(spacing: 7) {
            if waits {
                banner("\(session.name) is waiting for you in its terminal.", button: "Open the terminal")
            } else if ended {
                banner("\(session.name) has ended.", button: "Show its terminal")
            } else {
                field
            }
            if let problem {
                Text(problem)
                    .font(.caption)
                    .foregroundStyle(Theme.needs)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .textSelection(.enabled)
            }
            Button { store.showTerminal = true } label: {
                HStack(spacing: 5) {
                    Image(systemName: "terminal")
                    Text("Terminal")
                    Text("⌥⌘T").foregroundStyle(Theme.muted)
                }
                .font(.caption)
            }
            .buttonStyle(.plain)
            .foregroundStyle(Theme.muted)
            .help("Show the live terminal (View > Terminal)")
        }
        .padding(.horizontal, 24)
        .padding(.top, 10)
        .padding(.bottom, 12)
        .frame(maxWidth: 780)
        .frame(maxWidth: .infinity)
        .background(Theme.ground)
        .onChange(of: store.focusMessage) { focused = true }
        .onChange(of: focused) { store.messageHasKeys = focused }
        .onChange(of: session.id) {
            text = ""
            problem = nil
        }
    }

    private var field: some View {
        HStack(alignment: .bottom, spacing: 8) {
            entry
            Button(action: send) {
                Group {
                    if model.sending {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: "arrow.up").font(.body.weight(.bold))
                    }
                }
                .frame(width: 26, height: 26)
                .background(Circle().fill(Theme.accentFill))
                .foregroundStyle(Theme.onAccent)
            }
            .buttonStyle(.plain)
            .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.sending || early)
            .help("Send (Return)")
            .accessibilityLabel("Send")
        }
        .padding(.leading, Theme.inset)
        .padding(.trailing, 6)
        .padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous).fill(Theme.field))
        .overlay(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
            .strokeBorder(focused ? Theme.sunEdge : Theme.edge, lineWidth: focused ? 1.5 : 1))
    }

    /// The text field. ImageRenderer cannot draw AppKit's field, so a
    /// rendering for --render-conversation shows its placeholder instead.
    @ViewBuilder private var entry: some View {
        let prompt = early ? "\(session.name) is starting…" : "Message \(session.name)…"
        if ConversationSnapshot.rendering {
            Text(prompt).font(.body).foregroundStyle(Theme.muted).padding(.vertical, 3)
                .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            TextField(prompt, text: $text, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.body)
                .lineLimit(1...8)
                .focused($focused)
                .disabled(early)
                .onSubmit(send)
                .onChange(of: text) { if problem != nil { problem = nil } }
                .padding(.vertical, 3)
                .accessibilityLabel("Message \(session.name)")
                .accessibilityHint("Return sends it. It reaches the agent at its next step.")
        }
    }

    private func banner(_ text: String, button: String) -> some View {
        HStack(spacing: 10) {
            Text(text)
                .font(.callout.weight(.medium))
                .foregroundStyle(waits ? Theme.needs : Color.primary)
            Spacer(minLength: 8)
            Button(button) { store.showTerminal = true }
                .buttonStyle(SoftButton(prominent: waits))
                .keyboardShortcut(.return, modifiers: [])
        }
        .padding(.horizontal, Theme.inset)
        .padding(.vertical, 10)
        .background(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous).fill(waits ? Theme.needsTint : Theme.panel))
        .overlay(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
            .strokeBorder(waits ? Theme.needs.opacity(0.45) : Theme.edge, lineWidth: 1))
    }

    private func send() {
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty, !model.sending, !early else { return }
        problem = nil
        Task {
            if let why = await model.send(message) {
                problem = why
            } else if text.trimmingCharacters(in: .whitespacesAndNewlines) == message {
                text = ""
            }
        }
    }
}

/// A quiet page for when there is no conversation to show, with the way to
/// the terminal when that is where it is.
struct ConversationNote: View {
    @EnvironmentObject private var store: Store
    let title: String
    let text: String?
    var terminal = false

    var body: some View {
        VStack(spacing: 12) {
            SunMark().frame(width: 48, height: 48)
            Text(title)
                .font(.system(.title3, design: .rounded).weight(.semibold))
                .multilineTextAlignment(.center)
            if let text, !text.isEmpty {
                Text(text)
                    .font(.callout)
                    .foregroundStyle(Theme.muted)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 380)
            }
            if terminal {
                Button("Open the terminal") { store.showTerminal = true }
                    .buttonStyle(SoftButton(prominent: true))
            }
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// A soft, rounded button in the palette: filled with its accent when it is
/// the thing to do, a quiet card otherwise.
struct SoftButton: ButtonStyle {
    var prominent = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.callout.weight(.medium))
            .padding(.horizontal, 14)
            .padding(.vertical, 6)
            .foregroundStyle(prominent ? Theme.onAccent : Theme.ink)
            .background(Capsule().fill(prominent ? Theme.accentFill : Theme.card))
            .overlay(Capsule().strokeBorder(prominent ? Color.clear : Theme.edge, lineWidth: 1))
            .opacity(configuration.isPressed ? 0.75 : 1)
            .contentShape(Capsule())
    }
}
