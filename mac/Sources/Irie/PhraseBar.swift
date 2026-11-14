import SwiftUI

/// The strip under the agent's terminal, the counterpart of the title bar:
/// lighter than the ground, rounded top corners, and one muted phrase that
/// gives way to the next every half minute. With ui.bar = tips, as a
/// company's copy may set (DESIGN §2.10), the phrases are the core's tips
/// for those new to it instead of words of encouragement.
@MainActor
struct PhraseBar: View {
    static let phrases = [
        "Be the change you want to see in the world. 🌍",
        "Have no fear, \(Brand.name) is here! 🌴",
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
    /// The core's tips, while ui.bar is tips; empty for the phrases.
    @State private var tips: [String] = []
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
            .onReceive(tick) { _ in
                guard Self.fixed == nil else { return }
                index += 1
                // ui.bar may have changed in Settings: a look every five minutes.
                if index % 10 == 0 { Task { await readBar() } }
            }
            .task(id: store.link) { await readBar() }
            .accessibilityHidden(true)
    }

    /// The phrase; in a preview, how its next build goes (Preview.swift).
    @ViewBuilder private var line: some View {
        if Preview.isOn {
            PreviewBadge()
        } else {
            Text(tips.isEmpty ? Self.phrases[index % Self.phrases.count] : "Tip: " + tips[index % tips.count])
                .id(index)
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .truncationMode(.tail)
        }
    }

    /// Reads ui.bar and the tips from the core once it answers, and again
    /// after it comes back, as after an install or a change of settings.
    private func readBar() async {
        guard !Preview.isOn, Self.fixed == nil, case .online = store.link, let r = await store.learn() else { return }
        let next = r.bar == "tips" ? (r.tips ?? []) : []
        if next != tips {
            tips = next
            index = next.isEmpty ? Int.random(in: 0..<Self.phrases.count) : Int.random(in: 0..<next.count)
        }
    }
}
