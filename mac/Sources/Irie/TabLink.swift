import SwiftUI

/// The lines that tie the selected card to the terminal: two, a little apart,
/// straight across from the middle of the card's trailing edge to the
/// terminal's frame. Each turns into both edges on a curve, the upper line up
/// and the lower one down, like a U over an upside-down U, and between them
/// both edges open, so the card, the lines and the frame read as one outline,
/// in the colour that edges them all (Look.edge). Between the lines the
/// card's ground runs on into the frame's, so the way through is one piece,
/// with no window or divider showing in it. The card and the frame sit in
/// different columns, so all of it is drawn over the whole window, across
/// the divider between them.
struct TabLink: View {
    let ends: TabLinkKey.Ends
    @Environment(\.displayScale) private var scale

    /// The lines' width, which the card's edge and the frame's share.
    static let width: CGFloat = 1.5
    /// The clear space between the two lines.
    static let gap: CGFloat = 4
    /// The curve each line takes into an edge, at the line's middle.
    static let radius: CGFloat = 6

    var body: some View {
        GeometryReader { g in
            if let card = ends.card, let list = ends.list, let frame = ends.frame,
               let join = Join(card: g[card.bounds], cardEdge: card.width, list: g[list], frame: g[frame.bounds], scale: scale) {
                let box = join.through.boundingRect
                // The card's ground fades out from its edge to the frame's.
                let start = UnitPoint(x: (join.fadeStart - box.minX) / box.width, y: 0)
                let end = UnitPoint(x: (join.fadeEnd - box.minX) / box.width, y: 0)
                ZStack {
                    // Each edge opens between its curves into it, and the
                    // way through from one to the other takes the frame's
                    // ground with the card's over it, fading out on the way
                    // across. It is cut out once, as one, so no seam shows
                    // where its colours meet.
                    ZStack {
                        frame.ground
                        ForEach(card.ground.indices, id: \.self) { i in
                            LinearGradient(colors: [card.ground[i], card.ground[i].opacity(0)], startPoint: start, endPoint: end)
                        }
                    }
                    .frame(width: box.width, height: box.height)
                    .compositingGroup()
                    .clipShape(join.through.offsetBy(dx: -box.minX, dy: -box.minY))
                    .position(x: box.midX, y: box.midY)
                    join.lines.fill(card.color)
                }
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

/// Where the lines run, in the window: between the middles of the card's edge
/// and the frame's, level with the middle of the card.
private struct Join {
    /// The two lines, as shapes to fill.
    let lines: Path
    /// The way through from the card to the frame: where each edge opens,
    /// between the curves into it, and the space between the lines' middles.
    let through: Path
    /// Across, where the card's ground starts to fade, at the outer side
    /// of its edge, and where it has gone, at the outer side of the frame's.
    let fadeStart: CGFloat
    let fadeEnd: CGFloat

    /// None unless the lines, curves and all, are in sight in the list, meet
    /// the straight part of the frame's edge, and have room for their curves.
    /// A hidden sidebar's list has no height, so nothing draws then.
    init?(card: CGRect, cardEdge: CGFloat, list: CGRect, frame: CGRect, scale: CGFloat) {
        let w = TabLink.width, r = TabLink.radius
        let a = card.maxX - cardEdge / 2, b = frame.minX + w / 2
        // The upper line's top on a whole pixel, so both draw crisp.
        let top = ((card.midY - TabLink.gap / 2 - w) * scale).rounded() / scale
        let upper = top + w / 2, lower = upper + TabLink.gap + w
        let from = upper - r, to = lower + r
        guard b - a >= 2 * r,
              from - Self.lead >= list.minY, to + Self.lead <= list.maxY,
              from > frame.minY + Theme.frameRadius, to < frame.maxY - Theme.frameRadius
        else { return nil }
        let upperSpine = Self.spine(at: upper, turn: -1, from: a, to: b, startWidth: cardEdge)
        let lowerSpine = Self.spine(at: lower, turn: 1, from: a, to: b, startWidth: cardEdge)
        var lines = Self.outline(upperSpine)
        lines.addPath(Self.outline(lowerSpine))
        self.lines = lines
        // Down the inner side of the card's edge where it opens, across
        // under the upper line's middle, down the inner side of the frame's
        // edge, and back over the lower line's middle. Each spine's ends run
        // on along the edges, past the openings, so they are left out.
        let inCard = card.maxX - cardEdge, inFrame = frame.minX + w
        var round: [CGPoint] = [CGPoint(x: inCard, y: from)]
        round += upperSpine.dropFirst().dropLast().map(\.at)
        round += [CGPoint(x: inFrame, y: from), CGPoint(x: inFrame, y: to)]
        round += lowerSpine.dropFirst().dropLast().reversed().map(\.at)
        round.append(CGPoint(x: inCard, y: to))
        var through = Path()
        through.addLines(round)
        through.closeSubpath()
        self.through = through
        fadeStart = card.maxX
        fadeEnd = frame.minX
    }

    /// How far each line runs on along the edges that stay, so the two meet
    /// without a seam.
    private static let lead: CGFloat = 1

    /// A point down the middle of a line, which way is outward from the
    /// curve's centre there, and half the line's width there.
    private typealias Point = (at: CGPoint, out: CGVector, half: CGFloat)

    /// Down the middle of one line: along the card's edge, round into the
    /// line, across, and round into the frame's edge. `turn` is -1 for the
    /// upper line, whose ends turn up into the edges, and 1 for the lower one.
    /// The first curve narrows from the card's edge, bolder while the list has
    /// the keyboard, to the line's width.
    private static func spine(at y: CGFloat, turn: CGFloat, from a: CGFloat, to b: CGFloat, startWidth: CGFloat) -> [Point] {
        let r = TabLink.radius, w = TabLink.width, steps = 16
        var spine: [Point] = [(CGPoint(x: a, y: y + turn * (r + lead)), CGVector(dx: -1, dy: 0), startWidth / 2)]
        for i in 0...steps {
            let t = CGFloat(i) / CGFloat(steps), angle = CGFloat.pi + turn * t * .pi / 2
            let out = CGVector(dx: cos(angle), dy: sin(angle))
            spine.append((CGPoint(x: a + r + r * out.dx, y: y + turn * r + r * out.dy), out, (startWidth + (w - startWidth) * t) / 2))
        }
        for i in 0...steps {
            let t = CGFloat(i) / CGFloat(steps), angle = -turn * (1 - t) * .pi / 2
            let out = CGVector(dx: cos(angle), dy: sin(angle))
            spine.append((CGPoint(x: b - r + r * out.dx, y: y + turn * r + r * out.dy), out, w / 2))
        }
        spine.append((CGPoint(x: b, y: y + turn * (r + lead)), CGVector(dx: 1, dy: 0), w / 2))
        return spine
    }

    /// A line as an outline to fill: out along one side of its middle and
    /// back along the other.
    private static func outline(_ spine: [Point]) -> Path {
        let side = spine.map { CGPoint(x: $0.at.x + $0.out.dx * $0.half, y: $0.at.y + $0.out.dy * $0.half) }
            + spine.reversed().map { CGPoint(x: $0.at.x - $0.out.dx * $0.half, y: $0.at.y - $0.out.dy * $0.half) }
        var path = Path()
        path.addLines(side)
        path.closeSubpath()
        return path
    }
}

/// Where the lines run: the selected card, the list it scrolls in, and the
/// terminal's frame while the terminal shows.
struct TabLinkKey: PreferenceKey {
    /// The selected card, the colour and width of its edge, and the colours
    /// its ground ends in at its trailing edge, bottom first.
    struct Card {
        var bounds: Anchor<CGRect>
        var color: Color
        var width: CGFloat
        var ground: [Color]
    }

    /// The terminal's frame and its fill.
    struct Frame {
        var bounds: Anchor<CGRect>
        var ground: Color
    }

    struct Ends {
        var card: Card?
        var list: Anchor<CGRect>?
        var frame: Frame?
    }

    static var defaultValue = Ends()

    static func reduce(value: inout Ends, nextValue: () -> Ends) {
        let next = nextValue()
        value.card = value.card ?? next.card
        value.list = value.list ?? next.list
        value.frame = value.frame ?? next.frame
    }
}
