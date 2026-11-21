import SwiftUI

// Your Zen (View › Your Zen…, Settings › Zen, and a step of zen's path):
// your zen's history as a graph, an arc diagram (Wattenberg, InfoVis 2002;
// docs/proposals/zen.md §5). Each zen you have had is a dot on a line, in
// the order you first had it, and each change an arc from the zen before to
// the one after: above the line going on, below it coming back, so a zen
// you return to gathers arcs. A dot's size is how long you kept that zen,
// and yours now is in the accent. A click, or ← and →, shows a zen's
// settings, with the way back to it, which is a new version: history is
// never rewritten. It is drawn here, with no web view and no network, and
// holds still.

/// Your zen's versions as a graph: the zens, and the changes between them.
struct ZenGraph: Equatable {
    struct Node: Equatable, Identifiable {
        /// The state: the name of its settings alone.
        let id: String
        let zen: ZenProfile
        let first: Date
        /// Why it first came, such as "Edge to Edge".
        let title: String
        /// How long it was your zen, all told.
        var kept: TimeInterval
        var visits = 1
        /// Its newest version, which going back starts from.
        var latest: ZenVersion
    }

    struct Arc: Equatable {
        let from: Int
        let to: Int
        let why: String
    }

    var nodes: [Node] = []
    var arcs: [Arc] = []
    /// Your zen now: the newest version's.
    var current: Int?

    /// The graph of versions newest first, as the core lists them, up to
    /// now.
    init(_ versions: [ZenVersion], now: Date = Date()) {
        let vs = Array(versions.reversed())
        var at: [String: Int] = [:]
        for (i, v) in vs.enumerated() {
            let until = i + 1 < vs.count ? vs[i + 1].created : now
            let spell = max(0, until.timeIntervalSince(v.created))
            let n: Int
            if let k = at[v.state] {
                n = k
                nodes[k].kept += spell
                nodes[k].visits += 1
                nodes[k].latest = v
            } else {
                n = nodes.count
                at[v.state] = n
                nodes.append(Node(id: v.state, zen: v.zen, first: v.created, title: v.why, kept: spell, latest: v))
            }
            if i > 0, let from = at[vs[i - 1].state] {
                arcs.append(Arc(from: from, to: n, why: v.why))
            }
        }
        current = vs.last.flatMap { at[$0.state] }
    }
}

/// The Your Zen sheet.
@MainActor
struct ZenHistory: View {
    @Environment(Store.self) private var store
    @Environment(\.dismiss) private var dismiss
    @State private var versions: [ZenVersion]
    @State private var loaded: Bool
    @State private var picked: Int?

    /// Seeded with versions to draw at once, as `--snapshot` does; else
    /// they come from the core.
    init(seed: [ZenVersion] = []) {
        _versions = State(initialValue: seed)
        _loaded = State(initialValue: !seed.isEmpty)
    }

    var body: some View {
        let graph = ZenGraph(versions)
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 5) {
                Text("Your Zen")
                    .font(Theme.font(17, .semibold))
                Text("Each zen you’ve had, in the order you first had it. An arc is a change: above the line going on, below it coming back. A larger dot is a zen you kept longer.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if graph.nodes.isEmpty {
                Text(loaded ? "Your zen hasn’t changed yet. Each change you keep will show here, and you can always go back." : "Reading your zen…")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, minHeight: 150)
            } else {
                ZenArcs(graph: graph, picked: Binding(get: { picked ?? graph.current }, set: { picked = $0 }))
                    .frame(height: ZenArcs.height)
                details(graph)
            }
            HStack(alignment: .firstTextBaseline) {
                Text(footnote(graph))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                Spacer(minLength: 12)
                Button("Done") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
        }
        .padding(22)
        .frame(width: 660)
        .background(Theme.ground)
        // Again whenever your zen gets a new version, as when going back.
        .task(id: store.zen.head) {
            versions = await store.zenVersions()
            loaded = true
            if let p = picked, p >= ZenGraph(versions).nodes.count { picked = nil }
        }
    }

    @ViewBuilder
    private func details(_ g: ZenGraph) -> some View {
        if let i = picked ?? g.current, g.nodes.indices.contains(i) {
            let n = g.nodes[i]
            let now = i == g.current
            VStack(alignment: .leading, spacing: 7) {
                HStack(spacing: 8) {
                    Text(n.title)
                        .font(Theme.font(13, .semibold))
                    if now {
                        Text("Your zen now")
                            .font(Theme.font(11, .medium))
                            .foregroundStyle(Theme.accentInk)
                    }
                }
                Text(Self.settings(n.zen))
                    .font(.callout)
                    .fixedSize(horizontal: false, vertical: true)
                Text(Self.when(n))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Button("Go Back to This Zen") { store.restoreZen(n.latest) }
                    .disabled(now)
                    .help(now ? "This is your zen now" : "Make this your zen again, as a new version you can come back from")
                    .padding(.top, 2)
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field))
            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        }
    }

    /// The versions, and the newest's name, which a tool can check.
    private func footnote(_ g: ZenGraph) -> String {
        guard let head = store.zen.head ?? versions.first?.name else { return "Kept on this Mac, and nowhere else." }
        let n = versions.count
        return "\(n == 1 ? "1 version" : "\(n) versions"), kept on this Mac. The newest is \(String(head.dropFirst(7).prefix(12)))…, its SHA-512."
    }

    /// A zen's settings, in a line.
    static func settings(_ p: ZenProfile) -> String {
        var s: [String] = []
        let layout = ZenLayout(rawValue: p.layout)?.title ?? p.layout
        s.append(p.layout == "centred" ? "\(layout), \(p.columns) columns" : layout)
        s.append(p.textSize > 0 ? "text at \(p.textSize) pt" : "the window’s text")
        s.append(p.fullScreen ? "full screen" : "in the window")
        s.append(p.gentle ? "a gentle agent" : "the agent as usual")
        s.append(p.breaks > 0 ? "a rest every \(p.breaks) minutes" : "no rests")
        s.append(p.night ? "a word at night" : "no word at night")
        return s.joined(separator: " · ")
    }

    /// When a zen first came, how long it was kept, and how often.
    static func when(_ n: ZenGraph.Node) -> String {
        let first = n.first.formatted(.dateTime.day().month(.abbreviated).hour().minute())
        let times = n.visits == 1 ? "" : " · your zen \(n.visits) times"
        return "First on \(first) · kept \(kept(n.kept))\(times)"
    }

    /// How long, in a word or two.
    static func kept(_ s: TimeInterval) -> String {
        switch s {
        case ..<90: return "a moment"
        case ..<5400: return "\(Int((s / 60).rounded())) minutes"
        case ..<(36 * 3600): return "\(Int((s / 3600).rounded())) hours"
        default: return "\(Int((s / 86400).rounded())) days"
        }
    }
}

/// The arc diagram: dots on a line, arcs between them.
@MainActor
private struct ZenArcs: View {
    let graph: ZenGraph
    @Binding var picked: Int?
    static let height: CGFloat = 200
    static let gap: CGFloat = 96
    static let side: CGFloat = 48

    /// How wide the row of zens is, and how far in it starts, so a short
    /// history sits in the middle.
    private var used: CGFloat { Self.side * 2 + CGFloat(max(0, graph.nodes.count - 1)) * Self.gap }
    private var width: CGFloat { max(Self.least, used) }
    private func x(_ i: Int) -> CGFloat { (width - used) / 2 + Self.side + CGFloat(i) * Self.gap }
    static let least: CGFloat = 612

    var body: some View {
        let g = graph
        let width = self.width
        let longest = max(g.nodes.map(\.kept).max() ?? 1, 1)
        let base = Self.height / 2
        ScrollView(.horizontal, showsIndicators: width > Self.least) {
            ZStack(alignment: .topLeading) {
                Canvas { ctx, _ in
                    var line = Path()
                    line.move(to: CGPoint(x: x(0) - 18, y: base))
                    line.addLine(to: CGPoint(x: x(g.nodes.count - 1) + 18, y: base))
                    ctx.stroke(line, with: .color(Theme.edge), lineWidth: 1)
                    for (k, a) in g.arcs.enumerated() {
                        let x1 = x(a.from), x2 = x(a.to)
                        // Going on, above the line; coming back, below.
                        let lift = min(abs(x2 - x1) * 2 / 3, base - 12) * (a.to > a.from ? -1 : 1)
                        var p = Path()
                        p.move(to: CGPoint(x: x1, y: base))
                        p.addCurve(to: CGPoint(x: x2, y: base), control1: CGPoint(x: x1, y: base + lift), control2: CGPoint(x: x2, y: base + lift))
                        let newest = k == g.arcs.count - 1
                        ctx.stroke(p, with: .color(newest ? Theme.accent : Theme.muted.opacity(0.5)), lineWidth: newest ? 1.6 : 1.1)
                    }
                }
                .frame(width: width, height: Self.height)
                .accessibilityHidden(true)
                ForEach(g.nodes.indices, id: \.self) { i in
                    let n = g.nodes[i]
                    let r = 5 + 9 * CGFloat((n.kept / longest).squareRoot())
                    Button { picked = i } label: { dot(i, r) }
                        .buttonStyle(.plain)
                        .help(n.title)
                        .accessibilityLabel("\(n.title), kept \(ZenHistory.kept(n.kept))\(i == g.current ? ", your zen now" : "")")
                        .accessibilityAddTraits(i == picked ? .isSelected : [])
                        .position(x: x(i), y: base)
                }
            }
            .frame(width: width, height: Self.height)
        }
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(.leftArrow) { step(-1) }
        .onKeyPress(.rightArrow) { step(1) }
    }

    private func dot(_ i: Int, _ r: CGFloat) -> some View {
        let now = i == graph.current, on = i == picked
        return Circle()
            .fill(now ? Theme.accent : Theme.muted.opacity(0.55))
            .frame(width: 2 * r, height: 2 * r)
            .overlay(Circle().strokeBorder(on ? Theme.ink : .clear, lineWidth: 1.5).padding(-5))
            .frame(width: 2 * r + 14, height: 2 * r + 14)
            .contentShape(Circle())
    }

    private func step(_ by: Int) -> KeyPress.Result {
        guard !graph.nodes.isEmpty else { return .ignored }
        let at = picked ?? graph.current ?? 0
        picked = min(max(at + by, 0), graph.nodes.count - 1)
        return .handled
    }
}
