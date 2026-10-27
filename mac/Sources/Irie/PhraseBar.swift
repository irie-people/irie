import SwiftUI

/// The strip under the agent's terminal, the counterpart of the title bar:
/// lighter than the ground, rounded top corners, and one muted phrase that
/// gives way to the next every half minute.
struct PhraseBar: View {
    static let phrases = [
        "Small steps still arrive.",
        "Everything irie.",
        "One thing at a time.",
        "Slow is smooth, smooth is fast.",
        "Let the agents work; you steer.",
        "Done beats perfect.",
        "Breathe. The build will finish.",
        "Make it work, then make it right.",
        "A good day is a few finished things.",
        "No worries, just the next task.",
        "Ship the small thing.",
        "Rest is part of the work.",
        "Curiosity over certainty.",
        "Walk, don't run; still get there.",
        "Today's mess is tomorrow's refactor.",
        "Trust the process, check the diff.",
    ]

    @State private var index = Int.random(in: 0..<Self.phrases.count)
    private let tick = Timer.publish(every: 30, on: .main, in: .common).autoconnect()

    var body: some View {
        Text(Self.phrases[index])
            .id(index)
            .font(Theme.font(12))
            .foregroundStyle(Theme.muted)
            .lineLimit(1)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 7)
            .padding(.horizontal, 16)
            .background(UnevenRoundedRectangle(topLeadingRadius: Theme.frameRadius, topTrailingRadius: Theme.frameRadius, style: .continuous)
                .fill(Theme.card).shadow(color: Theme.shadow, radius: 3, y: -1))
            .transition(.opacity)
            .animation(.easeInOut(duration: 0.8), value: index)
            .onReceive(tick) { _ in index = (index + 1) % Self.phrases.count }
            .accessibilityHidden(true)
    }
}
