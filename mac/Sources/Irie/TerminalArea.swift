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
        VStack(spacing: 0) {
            main
            PhraseBar()
        }
        .background(Theme.ground.ignoresSafeArea())
        .toolbarBackground(Theme.ground, for: .windowToolbar)
        // The window's own title. The bar hides it (IrieApp.showsTitle),
        // or the name would show twice.
        .navigationTitle(selected?.name ?? "Irie")
        .toolbar {
            // The bar's only title: the name and subtitle centre on its
            // midline beside the button.
            ToolbarItem(placement: .principal) {
                VStack(alignment: .leading, spacing: 0) {
                    Text(selected?.name ?? "Irie")
                        .font(.system(size: 13, weight: .semibold))
                    Text(selected.map(subtitle) ?? "No session")
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
                .lineLimit(1)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            }
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

    private var main: some View {
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
            } else if store.isFixture, store.showTerminal {
                // `--snapshot` attaches no terminal: the frame round an empty one.
                framed(Color.clear)
            }
            // The conversation needs only the core, not the terminal.
            if !store.showTerminal && (store.attach != nil || online) {
                ConversationView()
            } else if store.attach == nil, !store.isFixture {
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
    }

    @ViewBuilder private func terminal(_ a: AttachInfo) -> some View {
        #if canImport(SwiftTerm)
        framed(TerminalHost(info: a, focusToken: store.focusTerminal, fontSize: store.fontSize, store: store))
        #else
        Placeholder(
            title: "The terminal opens in Ghostty for now",
            text: "This build has no terminal of its own yet. Pick a session on the left and the Ghostty window follows it.",
            action: ("Open the terminal", store.openInGhostty)
        )
        #endif
    }

    /// The terminal in its rounded frame, edged in the shown session's colour,
    /// where the line from its card ends while it shows (TabLink.swift).
    private func framed(_ terminal: some View) -> some View {
        let frame = RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
        let shows = store.showTerminal
        return terminal
            .padding(.vertical, 10)
            .padding(.horizontal, 18)
            .background {
                // A click on the frame types to the terminal too.
                frame.fill(Color(nsColor: Theme.terminalBackground))
                    .shadow(color: .black.opacity(0.25), radius: 10, y: 3)
                    .overlay(frame.strokeBorder(edge, lineWidth: TabLink.width))
                    .onTapGesture { store.focusTerminal += 1 }
            }
            .transformAnchorPreference(key: TabLinkKey.self, value: .bounds) { ends, bounds in
                if shows { ends.frame = bounds }
            }
            .padding([.horizontal, .bottom], 12)
            .padding(.top, 12)
    }

    /// The frame's colour: the shown session's edge, as round its card
    /// (Look.edge), or sun with none shown.
    private var edge: Color { selected.map(Look.edge) ?? Theme.sunEdge }

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

/// A quiet page, laid out as ConversationNote is: the two take turns in the
/// same place.
private struct Placeholder: View {
    let title: String
    let text: String
    var action: (String, () -> Void)? = nil

    var body: some View {
        VStack(spacing: 12) {
            SunMark().frame(width: 48, height: 48)
            Text(title).font(.system(size: 17, weight: .semibold, design: .rounded)).multilineTextAlignment(.center)
            Text(text).font(.system(size: 13)).foregroundStyle(.secondary).multilineTextAlignment(.center).frame(maxWidth: 380)
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
