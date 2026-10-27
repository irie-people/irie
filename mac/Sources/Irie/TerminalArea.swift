import SwiftUI

/// The right side: the shown agent's live terminal. The core zooms the slot
/// while the app is open, so a tmux client of the main session shows just
/// that agent, and showing another session swaps it in place.
struct TerminalArea: View {
    @EnvironmentObject private var store: Store

    private var selected: Session? { store.sessions.first { $0.id == store.selection } }

    var body: some View {
        Group {
            switch store.link {
            case .connecting:
                ProgressView("Connecting to Irie…").frame(maxWidth: .infinity, maxHeight: .infinity)
            case let .offline(why):
                Placeholder(title: why, text: "Start Irie with `irie up`. The app connects by itself once it’s running.")
            case .online:
                if let a = store.attach {
                    terminal(a)
                } else {
                    ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
        }
        .navigationTitle(selected?.name ?? "Irie")
        .navigationSubtitle(selected.map(subtitle) ?? "")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    store.openInGhostty()
                } label: {
                    Label("Open in Ghostty", systemImage: "macwindow.on.rectangle")
                }
                .help("Open the terminal in a Ghostty window too")
                .disabled(store.attach == nil)
            }
        }
    }

    @ViewBuilder private func terminal(_ a: AttachInfo) -> some View {
        #if canImport(SwiftTerm)
        TerminalHost(info: a, focusToken: store.focusTerminal, fontSize: store.fontSize, store: store)
            .background(Color(nsColor: Theme.terminalBackground))
        #else
        Placeholder(
            title: "The terminal opens in Ghostty for now",
            text: "This build has no terminal of its own yet. Pick a session on the left and the Ghostty window follows it.",
            action: ("Open the terminal", store.openInGhostty)
        )
        #endif
    }

    private func subtitle(_ s: Session) -> String {
        var parts: [String] = []
        switch Look(s) {
        case .needs: parts.append(s.detail ?? "Needs you")
        case .working: parts.append("Working")
        case .starting: parts.append("Starting")
        case .done: parts.append("Done")
        case .idle: parts.append("Idle")
        case .ended: parts.append(s.state == "exited" ? "Exited" : s.state.capitalized)
        }
        if let p = s.profile, !p.isEmpty { parts.append(p) }
        if let b = s.branch, !b.isEmpty { parts.append(b) }
        if let t = s.tokens { parts.append("\(Tokens.short(t.total)) tokens") }
        return parts.joined(separator: " · ")
    }
}

private struct Placeholder: View {
    let title: String
    let text: String
    var action: (String, () -> Void)? = nil

    var body: some View {
        VStack(spacing: 14) {
            SunMark().frame(width: 64, height: 64)
            Text(title).font(.system(size: 17, weight: .semibold, design: .rounded)).multilineTextAlignment(.center)
            Text(text).font(.system(size: 13)).foregroundStyle(.secondary).multilineTextAlignment(.center).frame(maxWidth: 360)
            if let action {
                Button(action.0, action: action.1)
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.deepGreen)
                    .controlSize(.large)
            }
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
