import SwiftUI

/// The right side: the shown agent's live terminal. The core zooms the slot
/// while the app is open, so a tmux client of the main session shows just
/// that agent, and showing another session swaps it in place.
struct TerminalArea: View {
    @EnvironmentObject private var store: Store
    /// The palette's name: a change draws the ground and the frame again.
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id

    private var selected: Session? { store.sessions.first { $0.id == store.selection } }

    private var online: Bool {
        if case .online = store.link { return true }
        return false
    }

    var body: some View {
        ZStack(alignment: .top) {
            // Once attached, the terminal stays: tmux outlives a core
            // restart, so a flap gets a banner, not a reattach.
            if let a = store.attach {
                // The terminal stays attached under the conversation, so
                // ⌥⌘T flips between them at once.
                terminal(a)
                    .opacity(store.showTerminal ? 1 : 0)
                    .allowsHitTesting(store.showTerminal)
                    .accessibilityHidden(!store.showTerminal)
            }
            // The conversation needs only the core, not the terminal.
            if !store.showTerminal && (store.attach != nil || online) {
                ConversationView()
            } else if store.attach == nil {
                if case let .offline(why) = store.link {
                    Placeholder(title: why, text: "Start Irie with `irie up`. The app connects by itself once it’s running.")
                } else {
                    ProgressView("Connecting to Irie…").frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            if store.attach != nil, let note = linkNote {
                Text(note)
                    .font(.system(size: 11.5, weight: .medium))
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .background(Capsule().fill(Theme.field).shadow(radius: 3, y: 1))
                    .overlay(Capsule().strokeBorder(Theme.edge, lineWidth: 1))
                    .padding(.top, 8)
                    .accessibilityAddTraits(.isStaticText)
            }
        }
        .background(Theme.ground.ignoresSafeArea())
        .toolbarBackground(Theme.ground, for: .windowToolbar)
        .navigationTitle(selected?.name ?? "Irie")
        // Never empty: the title moves when a subtitle appears or goes.
        .navigationSubtitle(selected.map(subtitle) ?? "No session")
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
        let frame = RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
        TerminalHost(info: a, focusToken: store.focusTerminal, fontSize: store.fontSize, store: store)
            .padding(10)
            .background {
                // A click on the frame types to the terminal too.
                frame.fill(Color(nsColor: Theme.terminalBackground))
                    .shadow(color: .black.opacity(0.25), radius: 10, y: 3)
                    .overlay(frame.strokeBorder(edge.opacity(store.terminalHasKeys ? 0.6 : 0.35),
                                                lineWidth: 1))
                    .onTapGesture { store.focusTerminal += 1 }
            }
            .padding(12)
        #else
        Placeholder(
            title: "The terminal opens in Ghostty for now",
            text: "This build has no terminal of its own yet. Pick a session on the left and the Ghostty window follows it.",
            action: ("Open the terminal", store.openInGhostty)
        )
        #endif
    }

    /// The frame's colour: the shown session's (its own while it works), or sun while it is quiet. It
    /// deepens a little while the terminal has the keyboard.
    private var edge: Color { selected.flatMap { Look($0).color ?? Look.agentTint($0) } ?? Theme.sunEdge }

    /// What the banner says while the core is away.
    private var linkNote: String? {
        switch store.link {
        case .online: return nil
        case .connecting: return "Connecting to Irie…"
        case let .offline(why): return "\(why) Reconnecting…"
        }
    }

    private func subtitle(_ s: Session) -> String {
        let look = Look(s)
        var parts = [look.loud ? s.detail ?? look.label : look.label]
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
