import AppKit
import SwiftUI

// A session beside its agent-team teammates, as cmux shows them: the
// session's own terminal on the left, and its teammates' terminals in a
// column on the right, one under another at equal heights, each live and
// typed to with a click. A teammate is a Claude Code session of its own,
// which Claude Code starts in a tmux pane. Irie runs those panes on its
// team server, one tmux session each (internal/team), and each pane here is
// a tmux client of one. The session's own terminal is a client of the view
// session, which shows only its window, so it takes this pane's size while
// every other agent keeps the slot's (docs/DESIGN.md §2.3).

struct TeamSplit: View {
    let session: Session
    let team: [Teammate]
    @EnvironmentObject private var store: Store
    /// The lead's share of the width, where the divider was left.
    @AppStorage("teamSplitLead") private var leadShare = 0.5
    @State private var dragStart: Double?
    /// The pane a Ghostty split key zoomed to fill the split, by id.
    @State private var zoomed: String?
    @StateObject private var panes = PaneViews()

    /// Below this width the column goes under the lead.
    static let stackBelow: CGFloat = 640
    /// The lead's id among the panes; a teammate's is its tmux session.
    static let leadID = "lead"

    /// The panes in order: the lead, then the teammates down the column.
    private var order: [String] { [Self.leadID] + team.map(\.session) }

    var body: some View {
        GeometryReader { geo in
            let l = layout(geo.size)
            // Every pane keeps its place in the view tree, and a zoomed one
            // only grows over the others, so no terminal ends for a zoom.
            ZStack(alignment: .topLeading) {
                place(lead, l.panes[Self.leadID], id: Self.leadID)
                ForEach(team) { mate in place(teammate(mate), l.panes[mate.session], id: mate.session) }
                if zoomed == nil {
                    ForEach(Array(l.rules.enumerated()), id: \.offset) { _, r in
                        Rectangle().fill(TeamInk.rule).frame(width: r.width, height: r.height).offset(x: r.minX, y: r.minY)
                    }
                    divider(l.divider, wide: l.wide, length: l.wide ? geo.size.width : geo.size.height)
                }
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .irieSplitMove)) { handle($0) }
        .onChange(of: team.map(\.session)) { _, ids in
            if let z = zoomed, z != Self.leadID, !ids.contains(z) { zoomed = nil }
        }
    }

    /// A pane at its frame; under a zoomed one it neither shows nor takes
    /// clicks.
    @ViewBuilder private func place(_ pane: some View, _ rect: CGRect?, id: String) -> some View {
        let r = rect ?? .zero
        let hidden = zoomed != nil && zoomed != id
        pane.frame(width: r.width, height: r.height)
            .offset(x: r.minX, y: r.minY)
            .opacity(hidden ? 0 : 1)
            .allowsHitTesting(!hidden)
            .zIndex(zoomed == id ? 1 : 0)
    }

    /// Each pane's frame by id, the bar between the lead and the column,
    /// and the rules between teammates, for a size. A zoomed pane fills it.
    private func layout(_ size: CGSize) -> (panes: [String: CGRect], divider: CGRect, rules: [CGRect], wide: Bool) {
        let wide = size.width >= Self.stackBelow
        let share = min(max(leadShare, 0.3), 0.75)
        let bar: CGFloat = 7 // the divider's line and the room to grab it
        let lead, column, divider: CGRect
        if wide {
            let w = ((size.width - bar) * share).rounded()
            lead = CGRect(x: 0, y: 0, width: w, height: size.height)
            divider = CGRect(x: w, y: 0, width: bar, height: size.height)
            column = CGRect(x: w + bar, y: 0, width: size.width - w - bar, height: size.height)
        } else {
            let h = ((size.height - bar) * share).rounded()
            lead = CGRect(x: 0, y: 0, width: size.width, height: h)
            divider = CGRect(x: 0, y: h, width: size.width, height: bar)
            column = CGRect(x: 0, y: h + bar, width: size.width, height: size.height - h - bar)
        }
        var panes = [Self.leadID: lead]
        var rules: [CGRect] = []
        let n = CGFloat(max(team.count, 1))
        let each = ((column.height - (n - 1)) / n).rounded(.down)
        for (i, mate) in team.enumerated() {
            let y = column.minY + CGFloat(i) * (each + 1)
            panes[mate.session] = CGRect(x: column.minX, y: y, width: column.width, height: i == team.count - 1 ? column.maxY - y : each)
            if i > 0 { rules.append(CGRect(x: column.minX, y: y - 1, width: column.width, height: 1)) }
        }
        if let z = zoomed, panes[z] != nil { panes[z] = CGRect(origin: .zero, size: size) }
        return (panes, divider, rules, wide)
    }

    private var lead: some View {
        Pane(title: session.name, detail: "Lead", ink: Look.agentInk(session) ?? TeamInk.lead, working: Look(session) == .working) {
            if let a = store.attach, let view = a.view {
                // It takes the keyboard from the terminal it stands in for.
                SplitTerminalView(tmux: a.tmux, socket: a.socket, target: "=" + view, fontSize: store.fontSize,
                                  focusToken: store.focusTerminal, followsFocus: true,
                                  takesKeysOnStart: store.terminalView.map { $0.window?.firstResponder === $0 } ?? true,
                                  shows: zoomed == nil || zoomed == Self.leadID,
                                  onView: { panes.set(Self.leadID, $0) })
            } else {
                FixtureScreen(lines: ["❯ spawn three teammates to review the auth module", "", "⏺ Spawned probe, second and third.",
                                      "  They report back on their own."])
            }
        }
    }

    private func teammate(_ mate: Teammate) -> some View {
        Pane(title: mate.name, detail: detail(mate), ink: TeamInk.color(mate.color), working: mate.working == true) {
            if let a = store.attach, let socket = a.teams {
                SplitTerminalView(tmux: a.tmux, socket: socket, target: "=" + mate.session, fontSize: store.fontSize,
                                  shows: zoomed == nil || zoomed == mate.session,
                                  onView: { panes.set(mate.session, $0) })
            } else {
                FixtureScreen(lines: ["⏺ Reading internal/auth/session.go", "  ⎿  Read 214 lines", "", "✻ Thinking…"])
            }
        }
    }

    /// A Ghostty split key from one of the panes: move the keyboard to
    /// another pane, move the bar, put it back in the middle, or zoom.
    private func handle(_ note: Notification) {
        guard let view = note.object as? NSView, let from = panes.id(of: view),
              let move = note.userInfo?["move"] as? SplitMove else { return }
        let ids = order
        guard let i = ids.firstIndex(of: from) else { return }
        let lead = panes.view(Self.leadID).map { $0.convert($0.bounds, to: nil) }
        let mate = team.first.flatMap { panes.view($0.session) }.map { $0.convert($0.bounds, to: nil) }
        let wide = lead.flatMap { l in mate.map { $0.minX >= l.maxX - 1 } } ?? true
        switch move {
        case let .focus(next):
            focus(ids[(i + (next ? 1 : ids.count - 1)) % ids.count])
        case let .focusToward(edge):
            if let target = neighbour(of: view, toward: edge), let id = panes.id(of: target) { focus(id) }
        case let .resize(edge):
            let grow: Double
            switch edge {
            case .maxX: grow = wide ? 0.05 : 0
            case .minX: grow = wide ? -0.05 : 0
            case .minY: grow = wide ? 0 : 0.05
            default: grow = wide ? 0 : -0.05
            }
            leadShare = min(max(leadShare + grow, 0.3), 0.75)
        case .equalize:
            leadShare = 0.5
        case .zoom:
            zoomed = zoomed == nil ? from : nil
            focus(from)
        }
    }

    /// Gives a pane the keyboard. Under a zoom, the split unzooms first, so
    /// the keyboard never goes to a terminal the zoomed pane covers.
    private func focus(_ id: String) {
        guard let v = panes.view(id) else { return }
        if let z = zoomed, z != id {
            zoomed = nil
            #if canImport(GhosttyKit)
            // It shows from the next update; it takes the keyboard now.
            (v as? GhosttyTerminalView)?.shows = true
            #endif
        }
        v.window?.makeFirstResponder(v)
    }

    /// The nearest other pane in the direction of edge, by where the panes
    /// stand in the window (whose y goes up).
    private func neighbour(of from: NSView, toward edge: NSRectEdge) -> NSView? {
        let a = from.convert(from.bounds, to: nil)
        let candidates = order.compactMap { panes.view($0) }.filter { $0 !== from && $0.window != nil }.compactMap { v -> (NSView, CGRect)? in
            let b = v.convert(v.bounds, to: nil)
            switch edge {
            case .minX: return b.midX < a.minX ? (v, b) : nil
            case .maxX: return b.midX > a.maxX ? (v, b) : nil
            case .minY: return b.midY < a.minY ? (v, b) : nil
            default: return b.midY > a.maxY ? (v, b) : nil
            }
        }
        return candidates.min { hypot($0.1.midX - a.midX, $0.1.midY - a.midY) < hypot($1.1.midX - a.midX, $1.1.midY - a.midY) }?.0
    }

    /// What a teammate's header says after its name.
    private func detail(_ m: Teammate) -> String {
        var parts: [String] = []
        if let t = m.type, !t.isEmpty, t != m.name { parts.append(t) }
        if m.dead == true {
            parts.append(m.exitCode.map { $0 == 0 ? "Done" : "Failed (\($0))" } ?? "Done")
        } else if m.working == true {
            parts.append("Working")
        } else {
            parts.append("Waiting")
        }
        return parts.joined(separator: " · ")
    }

    /// The bar between the lead and the column, which drags.
    private func divider(_ rect: CGRect, wide: Bool, length: CGFloat) -> some View {
        Rectangle()
            .fill(TeamInk.rule)
            .frame(width: wide ? 1 : nil, height: wide ? nil : 1)
            .frame(width: rect.width, height: rect.height)
            .contentShape(Rectangle())
            .onHover { inside in
                if inside { (wide ? NSCursor.resizeLeftRight : NSCursor.resizeUpDown).push() } else { NSCursor.pop() }
            }
            .gesture(DragGesture(minimumDistance: 1).onChanged { g in
                let start = dragStart ?? leadShare
                if dragStart == nil { dragStart = start }
                let moved = (wide ? g.translation.width : g.translation.height) / max(length, 1)
                leadShare = min(max(start + moved, 0.3), 0.75)
            }.onEnded { _ in dragStart = nil })
            .offset(x: rect.minX, y: rect.minY)
            .accessibilityLabel("Divider between the session and its teammates")
    }
}

/// The split's terminal views by pane, which the Ghostty split keys move
/// the keyboard between.
final class PaneViews: ObservableObject {
    private struct Weak {
        weak var view: NSView?
    }

    private var views: [String: Weak] = [:]

    func set(_ id: String, _ view: NSView) { views[id] = Weak(view: view) }
    func view(_ id: String) -> NSView? { views[id]?.view }
    func id(of view: NSView) -> String? { views.first { $0.value.view === view }?.key }
}

/// One terminal of the split under its header: a dot and the name in its
/// colour, and what it does.
private struct Pane<Content: View>: View {
    let title: String
    let detail: String
    let ink: Color
    let working: Bool
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Circle().fill(ink).frame(width: 7, height: 7).opacity(working ? 1 : 0.55)
                Text(title).font(.system(size: 11.5, weight: .semibold)).foregroundStyle(ink)
                Text(detail).font(.system(size: 11)).foregroundStyle(TeamInk.muted)
                Spacer(minLength: 0)
            }
            .lineLimit(1)
            .padding(.horizontal, 12)
            .frame(height: 24)
            content.padding(.horizontal, 10).padding(.bottom, 6)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(title), \(detail)")
    }
}

/// Colours on the terminal's dark ground.
enum TeamInk {
    static let rule = Color.white.opacity(0.12)
    static let muted = Color(white: 0.62)
    static let lead = Color(red: 0.93, green: 0.90, blue: 0.84)

    /// Claude Code's name for a teammate's colour, made to read on the
    /// terminal's ground.
    static func color(_ name: String?) -> Color {
        switch name {
        case "red": return Color(red: 1.0, green: 0.42, blue: 0.42)
        case "blue": return Color(red: 0.42, green: 0.71, blue: 1.0)
        case "green": return Color(red: 0.48, green: 0.85, blue: 0.56)
        case "yellow": return Color(red: 0.96, green: 0.84, blue: 0.43)
        case "purple": return Color(red: 0.76, green: 0.65, blue: 1.0)
        case "orange": return Color(red: 1.0, green: 0.65, blue: 0.34)
        case "pink": return Color(red: 1.0, green: 0.56, blue: 0.78)
        case "cyan": return Color(red: 0.39, green: 0.90, blue: 0.89)
        default: return lead
        }
    }
}

/// A terminal's screen in a snapshot, which attaches no terminal.
private struct FixtureScreen: View {
    let lines: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                Text(line.isEmpty ? " " : line).font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Color(nsColor: Theme.terminalForeground))
            }
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .padding(.top, 4)
    }
}

/// One terminal of the split: a tmux client of a session, its own process.
/// It attaches again while it shows, as the main terminal does, and its
/// client ends when it goes.
struct SplitTerminalView: View {
    let tmux: String
    let socket: String
    let target: String
    let fontSize: CGFloat
    /// Bumped to give it the keyboard, when it follows the app's focus.
    var focusToken = 0
    var followsFocus = false
    var takesKeysOnStart = false
    /// It shows, not under a zoomed pane.
    var shows = true
    /// Hands the view over, for the split keys.
    var onView: ((NSView) -> Void)? = nil
    @AppStorage(TerminalEngine.defaultsKey) private var engine = ""

    var body: some View {
        #if canImport(GhosttyKit)
        if TerminalEngine.named(engine) == .ghostty {
            GhosttyTerminal(attach: .init(tmux: tmux, socket: socket, target: target), fontSize: fontSize, focusToken: focusToken,
                            followsFocus: followsFocus, takesKeysOnStart: takesKeysOnStart, shows: shows, onView: onView)
        } else {
            swiftTerm
        }
        #else
        swiftTerm
        #endif
    }

    @ViewBuilder private var swiftTerm: some View {
        #if canImport(SwiftTerm)
        SplitTerminal(tmux: tmux, socket: socket, target: target, fontSize: fontSize, focusToken: focusToken,
                      followsFocus: followsFocus, takesKeysOnStart: takesKeysOnStart)
        #else
        Color.clear
        #endif
    }
}
