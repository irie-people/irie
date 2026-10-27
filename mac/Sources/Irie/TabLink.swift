import SwiftUI

/// The line that ties the selected card to the terminal: straight across from
/// the middle of the card's trailing edge to the terminal's frame, in the
/// colour that edges them both (Look.edge). The card and the frame sit in
/// different columns, so the line is drawn over the whole window, where it
/// also crosses the divider between them.
struct TabLink: View {
    let ends: TabLinkKey.Ends

    /// The line's width, which the card's edge and the frame's share.
    static let width: CGFloat = 1.5

    var body: some View {
        GeometryReader { g in
            let card = ends.card.map { g[$0] }
            let list = ends.list.map { g[$0] }
            let frame = ends.frame.map { g[$0] }
            // While the card's middle is in sight in the list, and level with
            // the straight part of the frame's edge. A hidden sidebar's list
            // has no height, so nothing draws then.
            if let card, let list, let frame, let color = ends.color,
               card.maxX < frame.minX,
               card.midY >= list.minY, card.midY <= list.maxY,
               card.midY > frame.minY + Theme.frameRadius, card.midY < frame.maxY - Theme.frameRadius {
                Path { p in
                    // From inside the card's edge into the frame's, so the
                    // three join.
                    p.move(to: CGPoint(x: card.maxX - Self.width, y: card.midY))
                    p.addLine(to: CGPoint(x: frame.minX + Self.width, y: card.midY))
                }
                .stroke(color, lineWidth: Self.width)
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

/// Where the line runs: the selected card and the colour of its edge, the
/// list it scrolls in, and the terminal's frame while the terminal shows.
struct TabLinkKey: PreferenceKey {
    struct Ends {
        var card: Anchor<CGRect>?
        var color: Color?
        var list: Anchor<CGRect>?
        var frame: Anchor<CGRect>?
    }

    static var defaultValue = Ends()

    static func reduce(value: inout Ends, nextValue: () -> Ends) {
        let next = nextValue()
        value.card = value.card ?? next.card
        value.color = value.color ?? next.color
        value.list = value.list ?? next.list
        value.frame = value.frame ?? next.frame
    }
}
