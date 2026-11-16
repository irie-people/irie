import AppKit
import SwiftUI

/// The lines that tie the selected card to the terminal: two, a little apart,
/// straight across from the middle of the card's trailing edge to the
/// terminal's frame. Each turns into both edges on a curve, the upper line up
/// and the lower one down, like a U over an upside-down U, and between them
/// both edges open, so the card, the lines and the frame read as one outline,
/// in the colour that edges them all (Look.edge). Between the lines the
/// card's ground runs on into the frame's, so the way through is one piece,
/// with no window or divider showing in it.
///
/// The card and the frame sit in different columns of the split view, each
/// in a hosting view of its own, and the list makes its cards as they scroll
/// into sight. The ends used to reach the lines as SwiftUI preferences,
/// across all that, and nothing promises when such a preference arrives: in
/// a test program the lines were first worked out from the card no longer
/// selected, and a card the list made after a scroll never reported where it
/// was. So each end is an AppKit view in the background of the selected card
/// and of the frame (TabLinkCard, TabLinkFrame). It reads where it is in the
/// window whenever it, a view round it or the list's scroll moves, and one
/// view over the window's content (TabLinkView) draws the lines there and
/// then, in the same pass. It draws them only while both ends name the same
/// session, so a join never runs from one session's card into another's
/// frame.
enum TabLink {
    /// The lines' width, which the card's edge and the frame's share.
    static let width: CGFloat = 1.5
    /// The clear space between the two lines.
    static let gap: CGFloat = 4
    /// The curve each line takes into an edge, at the line's middle.
    static let radius: CGFloat = 6
}

/// The selected card's end of the lines, in its background: the session it
/// shows, the colour and width of its edge, and the colours its ground ends
/// in at its trailing edge, bottom first, which the lines paint over its edge
/// where they open it.
struct TabLinkCard: NSViewRepresentable {
    let session: String
    let color: Color
    let width: CGFloat
    let ground: [Color]

    func makeNSView(context _: Context) -> TabLinkEnd { TabLinkEnd(card: true) }

    func updateNSView(_ end: TabLinkEnd, context _: Context) {
        end.set(session: session, color: NSColor(color), width: width, ground: ground.map { NSColor($0) })
    }
}

/// The terminal frame's end, in its background while the terminal shows: the
/// session whose colour it wears, and its fill.
struct TabLinkFrame: NSViewRepresentable {
    let session: String?
    let ground: NSColor

    func makeNSView(context _: Context) -> TabLinkEnd { TabLinkEnd(card: false) }

    func updateNSView(_ end: TabLinkEnd, context _: Context) {
        end.set(session: session, color: .clear, width: TabLink.width, ground: [ground])
    }
}

/// One end of the lines: an empty view the size of the card or the frame,
/// which tells its window's TabLinkView whenever where it is may have
/// changed. It takes no clicks and VoiceOver passes it by.
final class TabLinkEnd: NSView {
    let isCard: Bool
    private(set) var session: String?
    private(set) var color: NSColor = .clear
    private(set) var edgeWidth: CGFloat = TabLink.width
    private(set) var ground: [NSColor] = []
    /// The window it was in, which hears when it leaves.
    private weak var lastWindow: NSWindow?
    private var watching: [NSObjectProtocol] = []

    init(card: Bool) {
        isCard = card
        super.init(frame: .zero)
        setAccessibilityElement(false)
    }

    @available(*, unavailable)
    required init?(coder _: NSCoder) { nil }

    override var isFlipped: Bool { true }
    override func hitTest(_: NSPoint) -> NSView? { nil }

    func set(session: String?, color: NSColor, width: CGFloat, ground: [NSColor]) {
        self.session = session
        self.color = color
        edgeWidth = width
        self.ground = ground
        changed()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        watch()
        if let old = lastWindow, old !== window { TabLinkView.found(in: old)?.refresh() }
        lastWindow = window
        changed()
    }

    override func viewDidMoveToSuperview() {
        super.viewDidMoveToSuperview()
        watch()
        changed()
    }

    override func setFrameOrigin(_ p: NSPoint) {
        super.setFrameOrigin(p)
        changed()
    }

    override func setFrameSize(_ s: NSSize) {
        super.setFrameSize(s)
        changed()
    }

    override func viewDidHide() {
        super.viewDidHide()
        changed()
    }

    override func viewDidUnhide() {
        super.viewDidUnhide()
        changed()
    }

    /// What moves it in the window while it keeps its place in its
    /// superview: a view round it that moves or takes a new size, as the
    /// split view's divider, a column and the window's edges do, and the
    /// list's scroll.
    private func watch() {
        let center = NotificationCenter.default
        for o in watching { center.removeObserver(o) }
        watching = []
        guard window != nil else { return }
        var v = superview
        while let s = v {
            watching.append(center.addObserver(forName: NSView.frameDidChangeNotification, object: s, queue: nil) { [weak self] _ in
                MainActor.assumeIsolated { self?.changed() }
            })
            if let clip = s as? NSClipView {
                clip.postsBoundsChangedNotifications = true
                watching.append(center.addObserver(forName: NSView.boundsDidChangeNotification, object: clip, queue: nil) { [weak self] _ in
                    MainActor.assumeIsolated { self?.changed() }
                })
            }
            if let split = s as? NSSplitView {
                watching.append(center.addObserver(forName: NSSplitView.didResizeSubviewsNotification, object: split, queue: nil) { [weak self] _ in
                    MainActor.assumeIsolated { self?.changed() }
                })
            }
            v = s.superview
        }
    }

    private func changed() {
        guard let window else { return }
        TabLinkView.of(window).take(self)
    }
}

/// The lines between the two ends that name the same session, drawn over
/// the window's content, above both columns and the divider between them.
/// It is only as big as the lines and the opening between them, takes no
/// clicks, and VoiceOver passes it by.
final class TabLinkView: NSView {
    private let ends = NSHashTable<TabLinkEnd>.weakObjects()
    private var shape: Shape?

    /// The one over the window's content, put there when an end first asks.
    static func of(_ window: NSWindow) -> TabLinkView {
        if let v = found(in: window) { return v }
        let v = TabLinkView(frame: .zero)
        window.contentView?.addSubview(v, positioned: .above, relativeTo: nil)
        return v
    }

    static func found(in window: NSWindow) -> TabLinkView? {
        window.contentView?.subviews.lazy.compactMap { $0 as? TabLinkView }.first
    }

    override init(frame: NSRect) {
        super.init(frame: frame)
        isHidden = true
        setAccessibilityElement(false)
    }

    @available(*, unavailable)
    required init?(coder _: NSCoder) { nil }

    override var isFlipped: Bool { true }
    override var isOpaque: Bool { false }
    override func hitTest(_: NSPoint) -> NSView? { nil }

    /// Takes an end's news and draws again.
    func take(_ end: TabLinkEnd) {
        ends.add(end)
        refresh()
    }

    override func viewDidChangeBackingProperties() {
        super.viewDidChangeBackingProperties()
        refresh()
    }

    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        refresh()
    }

    /// Works out the lines again from where the ends are now, and draws
    /// them, or hides them while the ends don't make a join.
    func refresh() {
        guard let window, let host = superview else { return }
        // Over the window's content, which may have put views of its own
        // above this one since: back on top once this pass of its layout
        // is over, which changing its views in the middle of would upset.
        if host.subviews.last !== self {
            DispatchQueue.main.async { [weak self, weak host] in
                guard let self, let host, superview === host, host.subviews.last !== self else { return }
                host.addSubview(self, positioned: .above, relativeTo: nil)
            }
        }
        let live = ends.allObjects.filter { $0.window === window && !$0.isHiddenOrHasHiddenAncestor && $0.bounds.width > 0 && $0.bounds.height > 0 }
        // The frame's session, and the card of that session whose lines
        // fit: an old card can linger a moment after the selection moves.
        guard let frame = live.first(where: { !$0.isCard && $0.session != nil }) else { return hide() }
        let frameBox = box(frame, in: host)
        let scale = window.backingScaleFactor
        for card in live where card.isCard && card.session == frame.session {
            let list = card.enclosingScrollView.map { box($0.contentView, in: host) } ?? box(host, in: host)
            let cardBox = box(card, in: host)
            // A reversed layout has the card right of the frame (Store.
            // reversed): the join is worked out in a mirror, where the card
            // is on the left, and turned back. The card's ground at its
            // left edge is its fill alone: its wash and deep end start
            // clear there.
            let width = host.bounds.width
            let flip = cardBox.midX > frameBox.midX
            func turn(_ r: CGRect) -> CGRect { flip ? CGRect(x: width - r.maxX, y: r.minY, width: r.width, height: r.height) : r }
            guard let join = Join(card: turn(cardBox), cardEdge: card.edgeWidth, list: turn(list), frame: turn(frameBox), scale: scale) else { continue }
            return show(flip ? join.mirrored(across: width) : join, color: card.color, ground: flip ? Array(card.ground.prefix(1)) : card.ground,
                        frameGround: frame.ground.first ?? .clear, in: host, scale: scale)
        }
        hide()
    }

    /// A view's bounds in the host's coordinates, measured from its top.
    private func box(_ v: NSView, in host: NSView) -> CGRect {
        let r = host.convert(v.bounds, from: v)
        return host.isFlipped ? r : CGRect(x: r.minX, y: host.bounds.height - r.maxY, width: r.width, height: r.height)
    }

    private func hide() {
        guard !isHidden else { return }
        isHidden = true
        shape = nil
    }

    private func show(_ join: Join, color: NSColor, ground: [NSColor], frameGround: NSColor, in host: NSView, scale: CGFloat) {
        // Just big enough, on whole pixels, so the lines stay crisp.
        let all = join.lines.boundingRect.union(join.through.boundingRect).insetBy(dx: -1, dy: -1)
        let x0 = (all.minX * scale).rounded(.down) / scale, y0 = (all.minY * scale).rounded(.down) / scale
        let x1 = (all.maxX * scale).rounded(.up) / scale, y1 = (all.maxY * scale).rounded(.up) / scale
        let at = CGRect(x: x0, y: y0, width: x1 - x0, height: y1 - y0)
        var move = CGAffineTransform(translationX: -x0, y: -y0)
        // The colours as they are drawn now, in the window's appearance.
        var colors: (CGColor, [CGColor], CGColor) = (color.cgColor, [], frameGround.cgColor)
        effectiveAppearance.performAsCurrentDrawingAppearance {
            colors = (color.cgColor, ground.map(\.cgColor), frameGround.cgColor)
        }
        let next = Shape(lines: join.lines.cgPath.copy(using: &move) ?? join.lines.cgPath,
                         through: join.through.cgPath.copy(using: &move) ?? join.through.cgPath,
                         fadeStart: join.fadeStart - x0, fadeEnd: join.fadeEnd - x0,
                         color: colors.0, ground: colors.1, frameGround: colors.2)
        let place = host.isFlipped ? at : CGRect(x: at.minX, y: host.bounds.height - at.maxY, width: at.width, height: at.height)
        if frame != place { frame = place }
        if isHidden { isHidden = false }
        if next != shape {
            shape = next
            needsDisplay = true
        }
    }

    override func draw(_: NSRect) {
        guard let s = shape, let ctx = NSGraphicsContext.current?.cgContext else { return }
        // The way through takes the frame's ground with the card's over it,
        // fading out on the way across. It is cut out once, as one image,
        // so no seam shows where its colours meet.
        if let fill = s.fill(size: bounds.size, scale: window?.backingScaleFactor ?? 2) {
            ctx.saveGState()
            ctx.addPath(s.through)
            ctx.clip()
            ctx.draw(fill, in: bounds)
            ctx.restoreGState()
        }
        ctx.addPath(s.lines)
        ctx.setFillColor(s.color)
        ctx.fillPath()
    }

    /// What it draws, in its own coordinates.
    private struct Shape: Equatable {
        var lines: CGPath
        var through: CGPath
        var fadeStart: CGFloat
        var fadeEnd: CGFloat
        var color: CGColor
        var ground: [CGColor]
        var frameGround: CGColor

        /// The way through's fill, which only changes across: the frame's
        /// ground, then each of the card's colours fading out from its edge
        /// to the frame's.
        func fill(size: CGSize, scale: CGFloat) -> CGImage? {
            let w = Int((size.width * scale).rounded(.up)), h = Int((size.height * scale).rounded(.up))
            guard w > 0, h > 0, let space = CGColorSpace(name: CGColorSpace.sRGB),
                  let ctx = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0, space: space,
                                      bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
            else { return nil }
            ctx.scaleBy(x: scale, y: scale)
            ctx.setFillColor(frameGround)
            ctx.fill(CGRect(origin: .zero, size: size))
            for c in ground {
                guard let clear = c.copy(alpha: 0),
                      let g = CGGradient(colorsSpace: space, colors: [c, clear] as CFArray, locations: [0, 1])
                else { continue }
                ctx.drawLinearGradient(g, start: CGPoint(x: fadeStart, y: 0), end: CGPoint(x: fadeEnd, y: 0),
                                       options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
            }
            return ctx.makeImage()
        }
    }
}

/// Where the lines run, in the host's coordinates measured from its top:
/// between the middles of the card's edge and the frame's, level with the
/// middle of the card.
struct Join {
    /// The two lines, as shapes to fill.
    let lines: Path
    /// The way through from the card to the frame: where each edge opens,
    /// between the curves into it, and the space between the lines' middles.
    let through: Path
    /// Across, where the card's ground starts to fade, at the outer side
    /// of its edge, and where it has gone, at the outer side of the frame's.
    let fadeStart: CGFloat
    let fadeEnd: CGFloat

    /// None unless the lines, curves and all, are in sight in the list, the
    /// card's edge is too, the lines meet the straight part of the frame's
    /// edge, and they have room for their curves. A hidden sidebar's list
    /// has no height, so nothing draws then.
    init?(card: CGRect, cardEdge: CGFloat, list: CGRect, frame: CGRect, scale: CGFloat) {
        let w = TabLink.width, r = TabLink.radius
        let a = card.maxX - cardEdge / 2, b = frame.minX + w / 2
        // The upper line's top on a whole pixel, so both draw crisp.
        let top = ((card.midY - TabLink.gap / 2 - w) * scale).rounded() / scale
        let upper = top + w / 2, lower = upper + TabLink.gap + w
        let from = upper - r, to = lower + r
        guard b - a >= 2 * r,
              from - Self.lead >= list.minY, to + Self.lead <= list.maxY, card.maxX <= list.maxX + 1,
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

    private init(lines: Path, through: Path, fadeStart: CGFloat, fadeEnd: CGFloat) {
        self.lines = lines
        self.through = through
        self.fadeStart = fadeStart
        self.fadeEnd = fadeEnd
    }

    /// The join as a mirror shows it, across a width: for a reversed
    /// layout, whose card is right of the frame.
    func mirrored(across width: CGFloat) -> Join {
        let t = CGAffineTransform(a: -1, b: 0, c: 0, d: 1, tx: width, ty: 0)
        return Join(lines: lines.applying(t), through: through.applying(t), fadeStart: width - fadeStart, fadeEnd: width - fadeEnd)
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
