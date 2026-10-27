import AppKit
import SwiftUI

// A session's helpers beside its conversation (docs/proposals/split-pane.md).
// A helper is one of the session's subagents. It has no terminal of its own:
// it runs inside the session's agent, so the pane draws the helper's own
// conversation, which the core reads from the helper's transcript
// (`subscribe` with agent sub:<id>). ui.helper_style picks the look: cards
// and plain words ("browser"), or a terminal ("terminal"). Either way the
// session's terminal keeps its size, so no agent is resized.

extension Notification.Name {
    /// Steps through the strip of helpers; the object is 1 or -1
    /// (Go > Next Helper, Previous Helper).
    static let irieHelperStep = Notification.Name("irieHelperStep")
}

/// How a helper's conversation names who speaks: the helper by its kind, and
/// its first message as the task from its session.
struct HelperVoice {
    let kind: String
    let parent: String
}

/// One helper as the strip lists it.
struct HelperRef: Identifiable, Equatable {
    var agent: String // sub:<id>
    var kind: String
    var task: String?
    var state: String // running, ok, error or stopped
    var started: Date?
    var ms: Int64?
    var tools: Int?
    var detail: String? // what it does now, while it runs
    var needsYou = false

    var id: String { agent }
    var running: Bool { state == "running" }

    /// The session's helpers in the order they started: those its
    /// conversation names, then any running one it doesn't name yet. A
    /// helper the session's row lists is running, whatever its call says:
    /// one started in the background has its call's result at once.
    static func list(_ items: [ConversationItem], _ session: Session) -> [HelperRef] {
        let live = Dictionary((session.subagents ?? []).map { ($0.agent, $0) }, uniquingKeysWith: { first, _ in first })
        var out: [HelperRef] = []
        var seen = Set<String>()
        for it in items where it.kind == "agent" {
            guard let agent = it.agent, seen.insert(agent).inserted else { continue }
            let now = live[agent]
            out.append(HelperRef(agent: agent, kind: it.agentType ?? now?.type ?? "Helper", task: it.text,
                                 state: now != nil ? "running" : it.state ?? "ok", started: it.when ?? now?.started,
                                 ms: it.ms, tools: it.tools, detail: now?.detail, needsYou: now?.needsYou == true))
        }
        for s in session.subagents ?? [] where seen.insert(s.agent).inserted {
            out.append(HelperRef(agent: s.agent, kind: s.type ?? "Helper", state: "running", started: s.started,
                                 detail: s.detail, needsYou: s.needsYou == true))
        }
        return out
    }

    /// How long it ran, or has run so far.
    func took(_ now: Date) -> String? {
        if !running, let ms, ms > 0 { return Took.short(ms) }
        guard let started else { return nil }
        return Took.short(Int64(max(0, now.timeIntervalSince(started)) * 1000))
    }

    /// Its state in a few words.
    var said: String {
        switch state {
        case "running": return needsYou ? "Needs you" : "Working"
        case "error": return "Failed"
        case "stopped": return "Stopped"
        default: return "Done"
        }
    }
}

/// The helper beside its session's conversation, or in its place when the
/// window is narrow.
struct HelperPane: View {
    @EnvironmentObject private var store: Store
    let session: Session
    let agent: String
    /// The session's own conversation, which names its helpers.
    @ObservedObject var parent: ConversationModel
    /// In the conversation's place, with a way back, rather than beside it.
    var narrow = false
    let style: String
    @StateObject private var model = ConversationModel()
    @State private var expanded: Set<String> = []
    /// Helpers dropped from the strip, as session/agent lines.
    @AppStorage("droppedHelpers") private var dropped = ""

    /// A pane for session's helper agent. The snapshots hand it a model
    /// that already holds the helper's conversation.
    init(session: Session, agent: String, parent: ConversationModel, narrow: Bool = false, style: String,
         model: ConversationModel? = nil) {
        self.session = session
        self.agent = agent
        _parent = ObservedObject(wrappedValue: parent)
        self.narrow = narrow
        self.style = style
        _model = StateObject(wrappedValue: model ?? ConversationModel())
    }

    /// The narrowest right side that has room for both.
    static let sideBySide: CGFloat = 840
    static let minWidth: CGFloat = 300

    /// The pane's width beside a conversation that keeps 520 points.
    static func width(_ wanted: Double, in total: CGFloat) -> CGFloat {
        min(max(CGFloat(wanted), minWidth), max(minWidth, min(520, total - 529)))
    }

    private var terminal: Bool { style == "terminal" }

    private var helpers: [HelperRef] {
        let gone = Set(dropped.split(separator: "\n").map(String.init))
        return HelperRef.list(parent.items, session).filter { $0.agent == agent || $0.running || !gone.contains(key($0.agent)) }
    }

    private var current: HelperRef {
        helpers.first { $0.agent == agent } ?? HelperRef(agent: agent, kind: "Helper", state: "ok")
    }

    private func key(_ a: String) -> String { session.id + "/" + a }

    var body: some View {
        let edge = Look.edge(session)
        VStack(spacing: 0) {
            if narrow {
                Button { store.closeHelper(in: session.id) } label: {
                    Label(session.name, systemImage: "chevron.left")
                        .font(terminal ? .system(.callout, design: .monospaced).weight(.semibold) : Theme.font(13, .bold))
                        .lineLimit(1)
                }
                .buttonStyle(.plain)
                .foregroundStyle(terminal ? Term.ink : Theme.accentInk)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 14)
                .padding(.top, 10)
                .help("Back to the session’s conversation")
            }
            HelperStrip(helpers: helpers, open: agent, terminal: terminal) { store.openHelper($0, in: session.id) }
            Rectangle().fill(terminal ? Term.rule : Theme.edge).frame(height: 1)
            header
            feed
            Rectangle().fill(terminal ? Term.rule : Theme.edge).frame(height: 1)
            foot
        }
        .background(terminal ? Term.ground : Theme.sidebar)
        .clipShape(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous).strokeBorder(edge, lineWidth: 1.5))
        .padding(narrow ? 12 : 10)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Helper \(current.kind)")
        .onAppear { model.follow(session.id, agent: agent, client: store.client) }
        .onChange(of: agent) { model.follow(session.id, agent: agent, client: store.client) }
        .onChange(of: session.id) { model.follow(session.id, agent: agent, client: store.client) }
        .onReceive(NotificationCenter.default.publisher(for: .irieHelperStep)) { n in
            step(n.object as? Int ?? 1)
        }
    }

    // MARK: Parts

    private var header: some View {
        let h = current
        return VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(h.kind)
                    .font(terminal ? mono(.semibold) : Theme.font(14, .bold))
                    .foregroundStyle(terminal ? Term.ink : Theme.ink)
                if let t = h.task, !t.isEmpty {
                    Text("· " + t)
                        .font(terminal ? mono() : Theme.font(14))
                        .foregroundStyle(terminal ? Term.faint : Theme.muted)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
                Spacer(minLength: 4)
                if !narrow {
                    Button { store.closeHelper(in: session.id) } label: {
                        Image(systemName: "xmark").font(.caption.weight(.bold))
                            .frame(width: 22, height: 22)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(terminal ? Term.faint : Theme.muted)
                    .help("Close the helper pane (⇧⌘H)")
                    .accessibilityLabel("Close the helper pane")
                }
            }
            TimelineView(.periodic(from: .now, by: 30)) { ctx in
                Text(meta(h, now: ctx.date))
                    .font(terminal ? mono(size: -1) : Theme.font(12, .semibold))
                    .foregroundStyle(stateInk(h))
                    .monospacedDigit()
            }
            if h.running, let d = h.detail, !d.isEmpty {
                Text("Now: " + d)
                    .font(terminal ? mono(size: -1) : Theme.font(12))
                    .foregroundStyle(terminal ? Term.faint : Theme.muted)
                    .lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
    }

    @ViewBuilder private var feed: some View {
        let h = current
        if !model.loaded {
            quiet(model.problem.map { "Can’t follow this helper: \($0)" } ?? "Loading the helper…")
        } else if model.items.isEmpty {
            quiet(model.transcript == nil
                ? "Its transcript isn’t there yet. What it does shows here once Claude Code writes it."
                : "Nothing here yet.")
        } else if terminal {
            Scrolled(bottom: true) {
                HelperTerminalFeed(items: model.items, fontSize: store.fontSize)
            }
        } else {
            Scrolled(bottom: true) {
                FeedContent(items: model.items, pending: [], needs: nil, session: session, more: model.more,
                            expanded: $expanded, helper: HelperVoice(kind: h.kind, parent: session.name))
            }
        }
    }

    private var foot: some View {
        let h = current
        return HStack(spacing: 10) {
            if !h.running {
                Text(h.state == "error" ? "It failed. Its result went back to the session." : "It finished. Its report went back to the session.")
                    .font(terminal ? mono(size: -2) : Theme.font(12))
                    .foregroundStyle(terminal ? Term.faint : Theme.muted)
                Spacer(minLength: 6)
                Button("Drop") { drop(h) }
                    .buttonStyle(FootButton(terminal: terminal))
                    .help("Take it off the strip")
            } else {
                Text("A helper can’t take messages from Irie yet. In the terminal, Claude Code’s own panel can type to it.")
                    .font(terminal ? mono(size: -2) : Theme.font(12))
                    .foregroundStyle(terminal ? Term.faint : Theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 6)
                Button("Terminal") { store.showTerminal = true }
                    .buttonStyle(FootButton(terminal: terminal))
                    .help("Show the session’s terminal (⌥⌘T)")
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
    }

    private func quiet(_ text: String) -> some View {
        Text(text)
            .font(terminal ? mono() : Theme.font(13))
            .foregroundStyle(terminal ? Term.faint : Theme.muted)
            .multilineTextAlignment(.center)
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func meta(_ h: HelperRef, now: Date) -> String {
        var parts = [h.said]
        if let t = h.took(now) { parts.append(h.running ? t : "in " + t) }
        if let n = h.tools, n > 0 { parts.append(n == 1 ? "1 tool call" : "\(n) tool calls") }
        return parts.joined(separator: " · ")
    }

    private func stateInk(_ h: HelperRef) -> Color {
        if h.needsYou { return terminal ? Term.red : Theme.needs }
        switch h.state {
        case "running": return terminal ? Term.yellow : Look.agentInk(session) ?? Theme.ink
        case "error": return terminal ? Term.red : Theme.needs
        case "stopped": return terminal ? Term.faint : Theme.muted
        default: return terminal ? Term.green : Look.doneColor
        }
    }

    private func mono(_ weight: Font.Weight = .regular, size: CGFloat = 0) -> Font {
        .system(size: max(9, store.fontSize - 1 + size), weight: weight, design: .monospaced)
    }

    // MARK: Actions

    private func step(_ by: Int) {
        let list = helpers
        guard let i = list.firstIndex(where: { $0.agent == agent }), !list.isEmpty else { return }
        let next = list[(i + by + list.count) % list.count]
        store.openHelper(next.agent, in: session.id)
    }

    private func drop(_ h: HelperRef) {
        let list = helpers
        var lines = dropped.split(separator: "\n").map(String.init)
        lines.append(key(h.agent))
        dropped = lines.suffix(500).joined(separator: "\n")
        if let i = list.firstIndex(where: { $0.agent == h.agent }) {
            let rest = list.enumerated().filter { $0.offset != i }.map(\.element)
            if let next = rest.last(where: { $0.running }) ?? rest.last {
                store.openHelper(next.agent, in: session.id)
                return
            }
        }
        store.closeHelper(in: session.id)
    }
}

/// The pills of a session's helpers, in the order they started; the open
/// one stands out, one that needs you is red.
private struct HelperStrip: View {
    let helpers: [HelperRef]
    let open: String
    let terminal: Bool
    let pick: (String) -> Void

    var body: some View {
        Scrolled(axis: .horizontal) {
            HStack(spacing: 6) {
                ForEach(helpers) { h in
                    Button { pick(h.agent) } label: {
                        HelperPill(helper: h, selected: h.agent == open, terminal: terminal)
                    }
                    .buttonStyle(.plain)
                    .help([h.kind, h.task, h.said].compactMap { $0 }.joined(separator: " · "))
                    .accessibilityLabel("\(h.kind), \(h.task ?? ""), \(h.said)")
                    .accessibilityAddTraits(h.agent == open ? [.isSelected] : [])
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
        }
    }
}

private struct HelperPill: View {
    @EnvironmentObject private var store: Store
    let helper: HelperRef
    let selected: Bool
    let terminal: Bool

    var body: some View {
        TimelineView(.periodic(from: .now, by: 30)) { ctx in
            if terminal {
                Text("\(glyph) \(helper.kind)\(helper.took(ctx.date).map { " " + $0 } ?? "")")
                    .font(.system(size: max(9, store.fontSize - 2), weight: selected ? .bold : .regular, design: .monospaced))
                    .lineLimit(1)
                    .fixedSize()
                    .foregroundStyle(helper.needsYou ? Term.red : selected ? Term.ground : Term.ink)
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(RoundedRectangle(cornerRadius: 3).fill(selected ? Term.ink : Term.rule.opacity(0.6)))
            } else {
                HStack(spacing: 5) {
                    mark
                    Text(helper.kind).font(Theme.font(12, .bold))
                    if let t = helper.took(ctx.date) {
                        Text(t).font(Theme.font(12)).foregroundStyle(Theme.muted).monospacedDigit()
                    }
                }
                .lineLimit(1)
                .fixedSize()
                .foregroundStyle(helper.needsYou ? Theme.needs : Theme.ink)
                .padding(.horizontal, 9)
                .padding(.vertical, 4)
                .background(Capsule().fill(selected ? Theme.selection : Theme.card))
                .overlay(Capsule().strokeBorder(helper.needsYou ? Theme.needs : selected ? Theme.accent : Theme.edge,
                                                lineWidth: selected ? 1.5 : 1))
            }
        }
    }

    private var glyph: String {
        switch helper.state {
        case "running": return helper.needsYou ? "●" : "◐"
        case "error": return "✕"
        case "stopped": return "–"
        default: return "✓"
        }
    }

    @ViewBuilder private var mark: some View {
        if helper.running {
            CallMark(state: "running")
        } else {
            CallMark(state: helper.state)
        }
    }
}

/// A helper's conversation drawn as a terminal: what its session asked,
/// what it says, and a line per tool call with how it went.
struct HelperTerminalFeed: View {
    let items: [ConversationItem]
    let fontSize: CGFloat

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(items) { it in
                line(it)
            }
        }
        .font(.system(size: max(9, fontSize - 1), design: .monospaced))
        .foregroundStyle(Term.ink)
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
    }

    @ViewBuilder private func line(_ it: ConversationItem) -> some View {
        switch it.kind {
        case "user": userLine(it)
        case "text": Text("⏺ " + (it.text ?? ""))
        case "note": noteLine(it)
        case "agent": agentLine(it)
        default: toolLine(it)
        }
    }

    /// A message to the helper: its task from its session first.
    private func userLine(_ it: ConversationItem) -> some View {
        let task = it.from == "parent"
        let text: String = "› " + (it.text ?? "")
        return Text(text).foregroundStyle(task ? Term.cyan : Term.ink).lineLimit(task ? 6 : nil)
    }

    private func noteLine(_ it: ConversationItem) -> some View {
        let text: String = "── " + (it.text ?? "") + " ──"
        return Text(text).foregroundStyle(Term.faint)
    }

    /// A helper this helper started.
    private func agentLine(_ it: ConversationItem) -> some View {
        let text: String = "↳ " + (it.agentType ?? "Helper") + "  " + (it.text ?? "")
        return HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(text).lineLimit(1)
            Spacer(minLength: 4)
            end(it)
        }
    }

    /// A tool call: the tool, what it was given, and how it went.
    private func toolLine(_ it: ConversationItem) -> some View {
        let what: String = it.detail ?? it.phrase ?? ""
        let arrow: Text = Text("→ ").foregroundColor(Term.faint)
        let tool: Text = Text(it.tool ?? "").bold()
        let given: Text = Text("  " + what).foregroundColor(Term.faint)
        let failed: String? = it.state == "error" ? it.output.flatMap { $0.isEmpty ? nil : "  " + $0 } : nil
        return VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                (arrow + tool + given).lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 4)
                end(it)
            }
            if let failed {
                Text(failed).foregroundStyle(Term.red).lineLimit(3)
            }
        }
    }

    private func end(_ it: ConversationItem) -> some View {
        let (text, colour) = ending(it)
        return Text(text).foregroundColor(colour).monospacedDigit().layoutPriority(1)
    }

    /// How a call went, as a terminal says it: … while it runs, then ✓ with
    /// the lines it changed and how long it took, ✕ or –.
    private func ending(_ it: ConversationItem) -> (String, Color) {
        let took: String = it.ms.flatMap { $0 > 0 ? " " + Took.short($0) : nil } ?? ""
        switch it.state {
        case "running": return ("…", Term.yellow)
        case "error": return ("✕" + took, Term.red)
        case "stopped": return ("–", Term.faint)
        default:
            let a = it.added ?? 0
            let r = it.removed ?? 0
            let changes: String = a + r > 0 ? " +\(a) −\(r)" : ""
            return ("✓" + changes + took, Term.green)
        }
    }
}

/// The terminal look's colours, the same in both appearances, as the
/// terminal's own are (Theme.terminalBackground and terminalForeground).
enum Term {
    static let ground = Color(nsColor: Theme.terminalBackground)
    static let ink = Color(nsColor: Theme.terminalForeground)
    static let faint = Color(nsColor: Theme.terminalForeground).opacity(0.58)
    static let rule = Color(nsColor: Theme.terminalForeground).opacity(0.16)
    static let green = Color(red: 0x57 / 255, green: 0xD0 / 255, blue: 0x8C / 255)
    static let red = Color(red: 0xFF / 255, green: 0x6E / 255, blue: 0x64 / 255)
    static let yellow = Color(red: 0xE8 / 255, green: 0xC0 / 255, blue: 0x4F / 255)
    static let cyan = Color(red: 0x4C / 255, green: 0xC9 / 255, blue: 0xDB / 255)
}

/// Content in a scroll view, or laid out whole while ImageRenderer draws
/// the snapshots, which leaves scroll views out (ConversationSnapshot).
private struct Scrolled<Content: View>: View {
    var axis: Axis.Set = .vertical
    var bottom = false
    @ViewBuilder let content: Content

    var body: some View {
        if ConversationSnapshot.rendering {
            if axis == .horizontal {
                content.fixedSize()
            } else {
                content.frame(maxHeight: .infinity, alignment: .top)
            }
        } else if axis == .horizontal {
            // As tall as the pills: a scroll view takes all the room it is given.
            ScrollView(.horizontal, showsIndicators: false) { content }
                .fixedSize(horizontal: false, vertical: true)
        } else if bottom {
            ScrollView(axis, showsIndicators: false) { content }
                .defaultScrollAnchor(.bottom)
        } else {
            ScrollView(axis, showsIndicators: false) { content }
        }
    }
}

/// The foot's buttons: soft capsules as the conversation's, or a
/// terminal's plain keys.
private struct FootButton: ButtonStyle {
    let terminal: Bool

    @ViewBuilder func makeBody(configuration: Configuration) -> some View {
        if terminal {
            configuration.label
                .font(.system(.caption, design: .monospaced).weight(.semibold))
                .padding(.horizontal, 8)
                .padding(.vertical, 3)
                .foregroundStyle(Term.ink)
                .background(RoundedRectangle(cornerRadius: 3).fill(Term.rule))
                .opacity(configuration.isPressed ? 0.7 : 1)
                .contentShape(Rectangle())
        } else {
            SoftButton().makeBody(configuration: configuration)
        }
    }
}

/// The bar between the conversation and the helper pane. Dragging it gives
/// either more room, and the app keeps where it was left.
struct PaneDivider: View {
    @Binding var width: Double
    let total: CGFloat
    @State private var start: Double?

    var body: some View {
        Color.clear
            .frame(width: 9)
            .overlay(Capsule().fill(Theme.edge).frame(width: 3, height: 34))
            .contentShape(Rectangle())
            .onHover { inside in
                if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() }
            }
            .gesture(DragGesture(minimumDistance: 1)
                .onChanged { g in
                    let base = start ?? width
                    if start == nil { start = width }
                    width = Double(HelperPane.width(base - Double(g.translation.width), in: total))
                }
                .onEnded { _ in start = nil })
            .accessibilityHidden(true)
    }
}
