import AppKit
import SwiftUI

// A drag scrolls the list (DESIGN §4.1). While a card, or a group of yours by
// its header, is dragged, holding it near the list's top or bottom edge
// scrolls the list that way, so it can go past the cards out of sight. Each
// edge has a band a fifth of the list deep, about two cards: the deeper into
// it the pointer, the faster the list goes, from a crawl at the band's inner
// edge, for aiming, to its fastest at the list's edge, for going far. Above
// the list, over the find field, where nothing takes a drop, it goes at its
// fastest too; below the list it stops, since the space chips there take
// drops. The list eases into a new speed rather than jumping to it, and
// eases out a little quicker, so it never lurches as the pointer moves. A
// band the drag began in waits until the pointer moves toward its edge, or
// leaves it, so taking up a card near an edge doesn't set the list off.
//
// AppKit scrolls a little by itself in a thin strip right at the edge, in
// steps; nothing public turns that off, so the band reaches well past it.
// A drop tells only the card under the pointer where the pointer is, so
// from the moment a drag takes hold of something (the cards' and headers'
// onDrag) until the button comes up, a view inside the list
// (DragScrollView) reads where the pointer is at each frame of the display,
// and scrolls the list's clip view itself, as its scroller would.

extension Notification.Name {
    /// A drag took hold of a card or a group of yours: the list follows the
    /// pointer until the button comes up.
    static let irieDragBegan = Notification.Name("irieDragBegan")
}

/// One drag's scrolling: how fast the list goes, for where the pointer is.
struct DragScroll {
    /// Each edge's band: a fifth of a list h tall, from 64 to 120 points
    /// deep, and never more than a third of it.
    static func band(_ h: CGFloat) -> CGFloat { min(max(h / 5, 64), 120, h / 3) }
    /// Points a second at a band's inner edge, and at the list's edge.
    static let slowest: CGFloat = 60
    static let fastest: CGFloat = 900
    /// Seconds the list takes to come most of the way to a faster speed, and
    /// to a slower one or a stop.
    static let rise: CGFloat = 0.15
    static let fall: CGFloat = 0.05
    /// How far toward its edge the pointer moves to wake the band the drag
    /// began in.
    static let nudge: CGFloat = 8

    /// Where the drag began, in points down from the list's top edge.
    let start: CGFloat
    /// Whether each band scrolls yet.
    private(set) var up = false
    private(set) var down = false
    /// How fast the list goes now, in points a second, down above 0.
    private(set) var velocity: CGFloat = 0

    init(start: CGFloat) { self.start = start }

    /// How far the list goes in the dt seconds to the next frame, with the
    /// pointer y points down from the top edge of a list h tall, or nil
    /// while it is off the list's side of the window: its speed eases
    /// toward the speed for where the pointer is.
    mutating func step(at y: CGFloat?, height h: CGFloat, dt: CGFloat) -> CGFloat {
        let goal = speed(at: y, height: h)
        let faster = goal * velocity >= 0 && abs(goal) > abs(velocity)
        velocity += (goal - velocity) * CGFloat(1 - exp(-Double(dt / (faster ? Self.rise : Self.fall))))
        if goal == 0, abs(velocity) < Self.slowest / 4 { velocity = 0 }
        return velocity * dt
    }

    /// The list came to its end: it starts from still when it can go on.
    mutating func stopped() { velocity = 0 }

    /// The speed the list heads for, in points a second, up below 0 and
    /// down above 0, with the pointer y points down from the top edge of a
    /// list h tall, or nil while it is off the list's side of the window.
    mutating func speed(at y: CGFloat?, height h: CGFloat) -> CGFloat {
        guard let y else {
            // Coming back into a band from elsewhere is meant.
            up = true
            down = true
            return 0
        }
        let b = Self.band(h)
        guard b > 0 else { return 0 }
        if y >= b || y <= start - Self.nudge { up = true }
        if y <= h - b || y >= start + Self.nudge { down = true }
        if y < b {
            return up ? -Self.pace(y <= 0 ? 1 : (b - y) / b) : 0
        }
        if y > h - b, y <= h {
            return down ? Self.pace((y - (h - b)) / b) : 0
        }
        return 0
    }

    /// From the slowest at depth 0 to the fastest at 1, gently at first.
    private static func pace(_ depth: CGFloat) -> CGFloat {
        slowest + (fastest - slowest) * depth * depth
    }
}

/// Puts a DragScrollView in the list's content, where it finds the list.
struct DragScrolling: NSViewRepresentable {
    func makeNSView(context _: Context) -> DragScrollView { DragScrollView() }
    func updateNSView(_: DragScrollView, context _: Context) {}
}

/// An empty view in the list's content that scrolls the list while a drag
/// holds near one of its edges. It takes no clicks or drops, and VoiceOver
/// passes it by.
final class DragScrollView: NSView {
    private var link: CADisplayLink?
    private var drag: DragScroll?
    /// When the last frame was, and what it moved short of a pixel.
    private var last: CFTimeInterval?
    private var carry: CGFloat = 0
    private var listening: NSObjectProtocol?

    override init(frame: NSRect) {
        super.init(frame: frame)
        setAccessibilityElement(false)
    }

    @available(*, unavailable)
    required init?(coder _: NSCoder) { nil }

    override func hitTest(_: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        stop()
        let center = NotificationCenter.default
        if let listening { center.removeObserver(listening) }
        listening = nil
        guard window != nil else { return }
        listening = center.addObserver(forName: .irieDragBegan, object: nil, queue: nil) { [weak self] _ in
            MainActor.assumeIsolated { self?.begin() }
        }
    }

    /// Follows the pointer from now until the button comes up.
    func begin() {
        drag = nil
        last = nil
        carry = 0
        guard link == nil, window != nil else { return }
        let l = displayLink(target: self, selector: #selector(tick(_:)))
        // Every frame a ProMotion screen draws, while the drag lasts.
        l.preferredFrameRateRange = CAFrameRateRange(minimum: 60, maximum: 120, preferred: 120)
        // A drag runs the main run loop in its tracking mode.
        l.add(to: .main, forMode: .common)
        link = l
    }

    private func stop() {
        link?.invalidate()
        link = nil
        drag = nil
        last = nil
        carry = 0
    }

    @objc private func tick(_ link: CADisplayLink) {
        guard let window else { return stop() }
        // Where the list is when this frame shows, for an even pace.
        step(at: link.targetTimestamp, pointer: window.mouseLocationOutsideOfEventStream,
             held: NSEvent.pressedMouseButtons & 1 != 0)
    }

    /// One frame of the drag, shown at time now, with the pointer at p in
    /// the window's coordinates, the button held or not: the list goes as far
    /// as its speed takes it since the last frame.
    func step(at now: CFTimeInterval, pointer p: NSPoint, held: Bool) {
        guard held, let window, let scroll = enclosingScrollView else { return stop() }
        let clip = scroll.contentView
        let list = shown(clip)
        let over = NSRect(origin: .zero, size: window.frame.size).contains(p) && p.x >= list.minX && p.x < list.maxX
        let y = list.maxY - p.y
        var d = drag ?? DragScroll(start: y)
        // A frame late under load moves the list no further than a
        // twentieth of a second would.
        let dt = last.map { CGFloat(min(max(now - $0, 0), 0.05)) } ?? 0
        let dy = d.step(at: over ? y : nil, height: list.height, dt: dt)
        last = now
        if dy == 0 {
            carry = 0
        } else if !move(dy, clip, in: scroll) {
            d.stopped()
        }
        drag = d
    }

    /// The part of the list that shows, in the window's coordinates.
    private func shown(_ clip: NSClipView) -> NSRect {
        let i = clip.contentInsets
        var r = clip.bounds
        r.origin.x += i.left
        r.origin.y += clip.isFlipped ? i.top : i.bottom
        r.size.width -= i.left + i.right
        r.size.height -= i.top + i.bottom
        return clip.convert(r, to: nil)
    }

    /// Scrolls the list dy points, down above 0, as far as it goes: a whole
    /// pixel at a time, keeping the rest for the next frame, so a crawl
    /// still moves. It reports false once the list can go no further.
    private func move(_ dy: CGFloat, _ clip: NSClipView, in scroll: NSScrollView) -> Bool {
        let scale = window?.backingScaleFactor ?? 2
        carry += dy
        let px = (carry * scale).rounded(.towardZero) / scale
        guard px != 0 else { return true }
        carry -= px
        var b = clip.bounds
        b.origin.y += clip.isFlipped ? px : -px
        let to = clip.constrainBoundsRect(b).origin
        guard to != clip.bounds.origin else {
            carry = 0
            return false
        }
        clip.scroll(to: to)
        scroll.reflectScrolledClipView(clip)
        return true
    }
}
