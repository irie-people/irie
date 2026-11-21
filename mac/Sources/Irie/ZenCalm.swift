import SwiftUI

// What zen offers, one thing at a time (docs/proposals/zen.md §6, §7): a
// step of its path, a rest with a breath to follow, or a word late at
// night, in a bubble at the foot of the terminal that stays until it is
// answered (Roots §7.3). The core decides what and when; the bubble takes
// no key, so the terminal keeps every keystroke. And the one soft line
// that says another session would like you.

/// The one thing zen offers, at the foot of the terminal.
@MainActor
struct ZenOfferBubble: View {
    @Environment(Store.self) private var store
    let offer: ZenOffer

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.radius + 4, style: .continuous)
        HStack(alignment: .center, spacing: 14) {
            if offer.kind == "rest" {
                ZenBreath()
            }
            VStack(alignment: .leading, spacing: 9) {
                Text(offer.text)
                    .font(Theme.font(12.5))
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                HStack(spacing: 16) {
                    Button(offer.yes) { store.answerZen(offer, yes: true) }
                        .buttonStyle(.borderless)
                        .font(.system(size: 11.5, weight: .semibold))
                        .foregroundStyle(Theme.accentInk)
                    if let no = offer.no {
                        Button(no) { store.answerZen(offer, yes: false) }
                            .buttonStyle(.borderless)
                            .font(.system(size: 11.5, weight: .medium))
                            .foregroundStyle(Theme.muted)
                    }
                    if let link = offer.link, let url = URL(string: link), url.scheme == "https" {
                        Link(destination: url) {
                            Label(url.host ?? link, systemImage: "arrow.up.right")
                                .labelStyle(.titleAndIcon)
                        }
                        .font(.system(size: 11.5, weight: .medium))
                        .foregroundStyle(Theme.accentInk)
                        .help(link)
                    }
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .frame(maxWidth: 470, alignment: .leading)
        .background(shape.fill(Theme.field).shadow(radius: 3, y: 1))
        .overlay(shape.strokeBorder(Theme.edge, lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Zen")
    }
}

/// A breath to follow: a dot that rises for 4 seconds as you breathe in and
/// falls for 6 as you breathe out, six breaths a minute. It moves only in
/// position and opacity (Roots §5.1), only while it shows, and holds still
/// under Reduce Motion (§5.3).
struct ZenBreath: View {
    @Environment(\.accessibilityReduceMotion) private var still
    static let inhale: TimeInterval = 4
    static let exhale: TimeInterval = 6

    var body: some View {
        let track: CGFloat = 42, dot: CGFloat = 10
        TimelineView(.animation(minimumInterval: 1.0 / 30, paused: still)) { tl in
            let up = still ? 0.5 : Self.height(at: tl.date.timeIntervalSinceReferenceDate)
            ZStack(alignment: .bottom) {
                Capsule()
                    .fill(Theme.edge)
                    .frame(width: 3, height: track)
                    .frame(width: dot)
                Circle()
                    .fill(Theme.accent)
                    .frame(width: dot, height: dot)
                    .opacity(0.45 + 0.55 * up)
                    .offset(y: -(track - dot) * up)
            }
            .frame(width: dot, height: track)
        }
        .accessibilityHidden(true)
    }

    /// How far up the dot is, from 0 to 1, t seconds into the breaths,
    /// eased in and out as a breath is.
    static func height(at t: TimeInterval) -> CGFloat {
        let p = t.truncatingRemainder(dividingBy: inhale + exhale)
        let x = p < inhale ? p / inhale : 1 - (p - inhale) / exhale
        return CGFloat(0.5 - 0.5 * cos(Double.pi * x))
    }
}

/// One soft line while another session would like you: its red mark, as
/// red means one thing (Roots §1.5), and plain words. A click shows the
/// first, still in zen. Keep Watch's list says the same in its own way.
@MainActor
struct ZenOthersLine: View {
    @Environment(Store.self) private var store

    var body: some View {
        let shown = store.selection
        let needing = store.drawnOrder.filter { $0.id != shown && Look($0).loud }
        if let first = needing.first {
            let words = needing.count == 1 ? "Another session would like you" : "\(needing.count) other sessions would like you"
            Button { store.show(first.id, focus: true) } label: {
                HStack(spacing: 6) {
                    StateMark(look: .needs, size: 8)
                    Text(words)
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(Capsule().fill(Theme.field))
                .overlay(Capsule().strokeBorder(Theme.edge, lineWidth: 1))
                .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .help("Show it when you’re ready: zen stays on")
            .accessibilityLabel(words)
            .transition(.opacity)
        }
    }
}
