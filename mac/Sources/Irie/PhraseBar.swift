import SwiftUI

/// The strip under the agent's terminal, the counterpart of the title bar:
/// lighter than the ground, rounded top corners, and one muted phrase that
/// gives way to the next every half minute.
@MainActor
struct PhraseBar: View {
    static let phrases = [
        "Be the change you want to see in the world. 🌍",
        "Have no fear, Irie is here! 🌴",
        "Power to the people! ✊",
        "If you contribute, you earn stake. 🥩",
        "No worries. 😌",
        "Good vibes. ✌️",
        "You can do this! 💪",
        "Go get it! 🚀",
        "We are only limited by imagination. 🌈",
        "Express your creativity. 🎨",
        "Take back your time. ⏳",
        "Live your life. 🌺",
        "Be happy. 😊",
        "Pay it forward. 🤝",
        "Live by actions. 🛠️",
        "Be kind. 💛",
        "Ain't nothin' to it but to do it. 🔥",
        "Have faith. 🙏",
    ]

    /// The phrase a recording shows throughout (Recording.swift), so a film
    /// is the same on every run.
    static let filmed = "Take back your time. ⏳"
    /// Set, the bar keeps to this phrase rather than one picked at random.
    static var fixed: Int?

    @Environment(Store.self) private var store
    @State private var index = Self.fixed ?? Int.random(in: 0..<Self.phrases.count)
    private let tick = Timer.publish(every: 30, on: .main, in: .common).autoconnect()

    var body: some View {
        line
            .lineLimit(1)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 7)
            .padding(.horizontal, 16)
            .background(UnevenRoundedRectangle(topLeadingRadius: Theme.frameRadius, topTrailingRadius: Theme.frameRadius, style: .continuous)
                .fill(Theme.card).shadow(color: Theme.shadow, radius: 3, y: -1))
            .overlay(alignment: .trailing) {
                // The core's version, muted, in the bottom right corner;
                // a preview names its checkout there instead.
                if !Preview.isOn, case let .online(version) = store.link {
                    Text(version)
                        .font(Theme.font(10.5))
                        .foregroundStyle(Theme.muted.opacity(0.7))
                        .lineLimit(1)
                        .padding(.trailing, 16)
                        .allowsHitTesting(false)
                }
            }
            .transition(.opacity)
            .animation(.easeInOut(duration: 0.8), value: index)
            .onReceive(tick) { _ in if Self.fixed == nil { index = (index + 1) % Self.phrases.count } }
            .accessibilityHidden(true)
    }

    /// The phrase; in a preview, how its next build goes (Preview.swift).
    @ViewBuilder private var line: some View {
        if Preview.isOn {
            PreviewBadge()
        } else {
            Text(Self.phrases[index])
                .id(index)
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
        }
    }
}
