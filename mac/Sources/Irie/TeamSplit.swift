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

    /// Below this width the column goes under the lead.
    static let stackBelow: CGFloat = 640

    var body: some View {
        GeometryReader { geo in
            let wide = geo.size.width >= Self.stackBelow
            let share = min(max(leadShare, 0.3), 0.75)
            if wide {
                HStack(spacing: 0) {
                    lead.frame(width: geo.size.width * share)
                    divider(vertical: true, length: geo.size.width)
                    column
                }
            } else {
                VStack(spacing: 0) {
                    lead.frame(height: geo.size.height * share)
                    divider(vertical: false, length: geo.size.height)
                    column
                }
            }
        }
    }

    private var lead: some View {
        Pane(title: session.name, detail: "Lead", ink: Look.agentInk(session) ?? TeamInk.lead, working: Look(session) == .working) {
            if let a = store.attach, let view = a.view {
                // It takes the keyboard from the terminal it stands in for.
                SplitTerminalView(tmux: a.tmux, socket: a.socket, target: "=" + view, fontSize: store.fontSize,
                                  focusToken: store.focusTerminal, followsFocus: true,
                                  takesKeysOnStart: store.terminalView.map { $0.window?.firstResponder === $0 } ?? true)
            } else {
                FixtureScreen(lines: ["❯ spawn three teammates to review the auth module", "", "⏺ Spawned probe, second and third.",
                                      "  They report back on their own."])
            }
        }
    }

    private var column: some View {
        VStack(spacing: 0) {
            ForEach(Array(team.enumerated()), id: \.element.id) { i, mate in
                VStack(spacing: 0) {
                    if i > 0 { Rectangle().fill(TeamInk.rule).frame(height: 1) }
                    Pane(title: mate.name, detail: detail(mate), ink: TeamInk.color(mate.color), working: mate.working == true) {
                        if let a = store.attach, let socket = a.teams {
                            SplitTerminalView(tmux: a.tmux, socket: socket, target: "=" + mate.session, fontSize: store.fontSize)
                        } else {
                            FixtureScreen(lines: ["⏺ Reading internal/auth/session.go", "  ⎿  Read 214 lines", "", "✻ Thinking…"])
                        }
                    }
                }
            }
        }
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
    private func divider(vertical: Bool, length: CGFloat) -> some View {
        Rectangle()
            .fill(TeamInk.rule)
            .frame(width: vertical ? 1 : nil, height: vertical ? nil : 1)
            .padding(vertical ? .horizontal : .vertical, 3)
            .contentShape(Rectangle())
            .onHover { inside in
                if inside { (vertical ? NSCursor.resizeLeftRight : NSCursor.resizeUpDown).push() } else { NSCursor.pop() }
            }
            .gesture(DragGesture(minimumDistance: 1).onChanged { g in
                let start = dragStart ?? leadShare
                if dragStart == nil { dragStart = start }
                let moved = (vertical ? g.translation.width : g.translation.height) / max(length, 1)
                leadShare = min(max(start + moved, 0.3), 0.75)
            }.onEnded { _ in dragStart = nil })
            .accessibilityLabel("Divider between the session and its teammates")
    }
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

    var body: some View {
        #if canImport(SwiftTerm)
        SplitTerminal(tmux: tmux, socket: socket, target: target, fontSize: fontSize, focusToken: focusToken,
                      followsFocus: followsFocus, takesKeysOnStart: takesKeysOnStart)
        #else
        Color.clear
        #endif
    }
}
