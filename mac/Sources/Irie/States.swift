import SwiftUI

/// A row's state as DESIGN §5.3 draws it. The core derives the state, in
/// the table's order of precedence (internal/core/state.go); this gives it
/// the table's glyph and colour role, and says how loud or quiet it is.
/// Every state has a glyph of its own, so colour never carries the meaning
/// alone.
enum Look: Equatable {
    case failed, exited, exitedWithError, frozen, paused, needs, stalled, pausing, working, starting, done, idle

    init(_ s: Session) { self.init(state: s.state, exitCode: s.exitCode) }

    init(state: String, exitCode: Int? = nil) {
        switch state {
        case "failed": self = .failed
        case "exited": self = (exitCode ?? 0) == 0 ? .exited : .exitedWithError
        case "frozen": self = .frozen
        case "paused": self = .paused
        case "needs_you": self = .needs
        case "stalled": self = .stalled
        case "pausing": self = .pausing
        case "starting": self = .starting
        case "done": self = .done
        case "idle": self = .idle
        // The core takes a live session it has no events for to be working.
        default: self = .working
        }
    }

    /// Its name, as a person says it.
    var label: String {
        switch self {
        case .failed: return "Failed"
        case .exited: return "Exited"
        case .exitedWithError: return "Exited with an error"
        case .frozen: return "Frozen"
        case .paused: return "Paused"
        case .needs: return "Needs you"
        case .stalled: return "Stalled"
        case .pausing: return "Pausing"
        case .working: return "Working"
        case .starting: return "Starting"
        case .done: return "To verify"
        case .idle: return "Idle"
        }
    }

    /// Red: it needs you. Its card takes the red tint, and what it needs is
    /// written in red.
    var loud: Bool { self == .failed || self == .needs || self == .stalled }

    /// Quiet: its name is drawn muted.
    var quiet: Bool { self == .exited || self == .exitedWithError || self == .starting || self == .idle }

    /// ui.red_on_done, which the core sends: done is red instead of green.
    static var redOnDone = false

    /// Done's colour: green, or red with ui.red_on_done.
    static var doneColor: Color { redOnDone ? Theme.needs : Theme.done }

    /// The mark's colour, by DESIGN §5.3's colour role: red, cyan, yellow,
    /// green, the default (ink), or dim (muted).
    var mark: Color {
        switch self {
        case .failed, .needs, .stalled, .exitedWithError: return Theme.needs
        case .frozen: return Theme.frozen
        case .paused, .pausing: return Theme.paused
        case .working: return Theme.ink
        case .done: return Look.doneColor
        case .exited, .starting, .idle: return Theme.muted
        }
    }

    /// Whether the mark is its colour dimmed: pausing is dim yellow, and an
    /// exit with an error code dim red.
    var dimmed: Bool { self == .pausing || self == .exitedWithError }

    /// The colour of the edge round its card or the terminal: red while it
    /// needs you, green once it is done; none otherwise.
    var color: Color? {
        switch self {
        case .failed, .needs, .stalled: return Theme.needs
        case .done: return Look.doneColor
        default: return nil
        }
    }
}

extension Look {
    /// The agent's colour for this session, while it is working; none otherwise.
    static func agentTint(_ s: Session) -> Color? {
        Look(s) == .working ? Theme.agent(slot: Theme.agentSlot(s.id)) : nil
    }

    /// Its card's ground in the agent's colour, while it is working; none
    /// otherwise.
    static func agentWash(_ s: Session) -> Color? {
        Look(s) == .working ? Theme.agentWash(slot: Theme.agentSlot(s.id)) : nil
    }

    /// Its selected card's trailing end, deep toward the colour round its
    /// terminal, while it is working; none otherwise.
    static func agentDeep(_ s: Session) -> Color? {
        Look(s) == .working ? Theme.agentDeep(slot: Theme.agentSlot(s.id)) : nil
    }

    /// The colour that edges the selected card and the terminal's frame, and
    /// draws the line between them: red while it needs you, green once it is
    /// done, the agent's own colour while it works, and sun otherwise.
    static func edge(_ s: Session) -> Color {
        Look(s).color ?? agentTint(s) ?? Theme.sunEdge
    }

    /// The agent's colour as text, for its state, its details and its
    /// helpers away from its card, while it is working; none otherwise.
    static func agentInk(_ s: Session) -> Color? {
        Look(s) == .working ? Theme.agentInk(slot: Theme.agentSlot(s.id)) : nil
    }
}

/// A state's glyph from DESIGN §5.3, drawn: ✕ failed, · exited (✕ with an
/// error code), ◆ frozen, ‖ paused, ● needs you, ! stalled, ‖ pausing,
/// ◐ working, ◌ starting, ✓ done and ○ idle. The marks hold still: an
/// endless animation in a list makes the whole window lay out again on
/// every frame.
struct StateMark: View {
    let look: Look
    var size: CGFloat = 14
    /// The agent's own colour, which a working mark takes in place of ink.
    var tint: Color? = nil

    var body: some View {
        Canvas { ctx, box in
            let k = min(box.width, box.height) / 16
            ctx.scaleBy(x: k, y: k)
            let ink = GraphicsContext.Shading.color(look == .working ? tint ?? look.mark : look.mark)
            func line(_ points: [(CGFloat, CGFloat)]...) -> Path {
                var p = Path()
                for run in points {
                    p.move(to: CGPoint(x: run[0].0, y: run[0].1))
                    for q in run.dropFirst() { p.addLine(to: CGPoint(x: q.0, y: q.1)) }
                }
                return p
            }
            func circle(_ r: CGFloat, _ y: CGFloat = 8) -> Path {
                Path(ellipseIn: CGRect(x: 8 - r, y: y - r, width: 2 * r, height: 2 * r))
            }
            func stroke(_ p: Path, _ width: CGFloat = 1.7, dash: [CGFloat] = []) {
                ctx.stroke(p, with: ink, style: StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round, dash: dash))
            }
            switch look {
            case .failed, .exitedWithError:
                stroke(line([(4.5, 4.5), (11.5, 11.5)], [(11.5, 4.5), (4.5, 11.5)]), 2)
            case .exited:
                ctx.fill(circle(2.1), with: ink)
            case .frozen:
                ctx.fill(line([(8, 2.6), (13.4, 8), (8, 13.4), (2.6, 8)]), with: ink)
            case .paused, .pausing:
                stroke(line([(5.8, 3.5), (5.8, 12.5)], [(10.2, 3.5), (10.2, 12.5)]), 2.1)
            case .needs:
                ctx.fill(circle(7), with: .color(look.mark.opacity(0.22)))
                ctx.fill(circle(4.2), with: ink)
            case .stalled:
                stroke(line([(8, 3.2), (8, 9.2)]), 2.1)
                ctx.fill(circle(1.25, 12.6), with: ink)
            case .working:
                stroke(circle(5.6))
                var half = ctx
                half.clip(to: circle(5.6))
                half.fill(Path(CGRect(x: 0, y: 0, width: 8, height: 16)), with: ink)
            case .starting:
                stroke(circle(5.6), dash: [1.7, 2.3])
            case .done:
                stroke(line([(3.6, 8.4), (6.6, 11.3), (12.4, 4.9)]), 2.1)
            case .idle:
                stroke(circle(4.6))
            }
        }
        .frame(width: size, height: size)
        .opacity(look.dimmed ? 0.7 : 1)
        .accessibilityLabel(look.label)
    }
}
