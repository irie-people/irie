import SwiftUI

/// The right side: the shown agent's live terminal. The core zooms the slot
/// while the app is open, so a tmux client of the main session shows just
/// that agent, and showing another session swaps it in place.
@MainActor
struct TerminalArea: View {
    @Environment(Store.self) private var store
    /// The palette's name: a change draws the ground and the frame again.
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id
    /// What draws the terminals (GhosttyTerminal.swift).
    @AppStorage(TerminalEngine.defaultsKey) private var engine = ""
    /// The right sidebar's width while a drag of its edge asks for another.
    @State private var sideGuide: Double?

    private var selected: Session? { store.selectedRow }

    private var online: Bool {
        if case .online = store.link { return true }
        return false
    }

    var body: some View {
        // The frame's colour for a finished session is Look's, which follows
        // ui.red_on_done; reading it here draws the frame again when it
        // changes.
        let _ = store.redOnDone
        VStack(spacing: 0) {
            // The right sidebar (RightSidebar.swift), inline after the
            // terminal or the conversation. The frame stays the first child,
            // so SwiftUI never makes the terminal again, which would restart
            // its tmux client. In a narrow window the sidebar gives up width
            // first, down to its narrowest, so the terminal keeps some room.
            GeometryReader { geo in
                let width = min(CGFloat(store.side.width), max(CGFloat(SideState.minWidth), geo.size.width - 12 - minTerminal))
                ZStack(alignment: .trailing) {
                    HStack(spacing: 0) {
                        main.modifier(FileDrop(session: selected))
                        if store.side.open {
                            RightSidebar(guide: $sideGuide)
                                .frame(width: width)
                                .padding([.top, .bottom, .trailing], 12)
                        }
                    }
                    if let g = sideGuide {
                        Rectangle()
                            .fill(Theme.accent)
                            .frame(width: 2)
                            .padding(.vertical, 12)
                            .padding(.trailing, CGFloat(g) + 12 + 4)
                            .allowsHitTesting(false)
                    }
                }
            }
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
                    // Its state and the rest, in its agent's colour while it
                    // works, as its card and the terminal's frame.
                    Text(selected.map { subtitle($0) } ?? "No session")
                        .font(.system(size: 11))
                        .foregroundStyle(selected.flatMap(Look.agentInk).map(AnyShapeStyle.init) ?? AnyShapeStyle(.secondary))
                }
                .lineLimit(1)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            }
            ToolbarItem(placement: .primaryAction) {
                // The session's helpers, in the right sidebar (RightSidebar.swift).
                Button {
                    store.toggleHelpers()
                } label: {
                    Label(store.helperShown ? "Hide Helpers" : "Show Helpers", systemImage: "rectangle.split.2x1")
                }
                .help(store.helperShown ? "Close the helpers (⇧⌘H)" : "Show the session’s helpers in the right sidebar (⇧⌘H)")
                .disabled(selected == nil)
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
            // At the toolbar's trailing end, where the HIG puts a button that
            // opens an inspector.
            ToolbarItem(placement: .primaryAction) {
                Button {
                    store.toggleSide()
                } label: {
                    Label(store.side.open ? "Hide Right Sidebar" : "Show Right Sidebar", systemImage: "sidebar.right")
                }
                .help(store.side.open ? "Hide the right sidebar (⌥⌘B)" : "Show the right sidebar: helpers and what is ready to review (⌥⌘B)")
            }
        }
    }

    /// What the terminal keeps beside the sidebar before the sidebar gives up
    /// width: about 80 columns and the frame round them.
    private var minTerminal: CGFloat { 80 * store.fontSize * 0.6 + 60 }

    private var main: some View {
        ZStack(alignment: .top) {
            // Once attached, the terminal stays: tmux outlives a core
            // restart, so a flap gets a banner, not a reattach.
            if let a = store.attach {
                // The terminal stays attached under the conversation, so
                // ⌥⌘T flips between them at once, and under the split.
                let shows = store.showTerminal && split == nil
                terminal(a, active: shows)
                    .opacity(shows ? 1 : 0)
                    .allowsHitTesting(shows)
                    .accessibilityHidden(!shows)
                if let s = split { framed(TeamSplit(session: s, team: s.team ?? []), padded: false) }
            } else if store.isFixture, store.showTerminal {
                // `--snapshot` attaches no terminal: the frame round an empty
                // one, or the split's panes.
                if let s = split {
                    framed(TeamSplit(session: s, team: s.team ?? []), padded: false)
                } else {
                    framed(Color.clear)
                }
            }
            // The conversation needs only the core, not the terminal.
            if !store.showTerminal && (store.attach != nil || online) {
                ConversationView()
            } else if store.attach == nil, !store.isFixture {
                if store.starting {
                    ProgressView("Starting Irie…").frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if case let .offline(why) = store.link {
                    Placeholder(title: why, text: "The sidebar says what to do. The app connects by itself once Irie is running.")
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
        .overlay(alignment: .bottom) {
            if let c = copied {
                Label(c.text, systemImage: "doc.on.doc")
                    .font(.system(size: 11.5, weight: .medium))
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .background(Capsule().fill(Theme.field).shadow(radius: 3, y: 1))
                    .overlay(Capsule().strokeBorder(Theme.edge, lineWidth: 1))
                    .padding(.bottom, 28)
                    .allowsHitTesting(false)
                    .transition(.opacity)
                    .id(c.id)
            }
        }
        .animation(.easeOut(duration: 0.15), value: copied)
        .onReceive(NotificationCenter.default.publisher(for: .irieTerminalCopied)) { note in
            guard let text = note.userInfo?["note"] as? String else { return }
            let c = CopiedNote(text: text)
            copied = c
            AccessibilityNotification.Announcement(text).post()
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.4) {
                if copied == c { copied = nil }
            }
        }
    }

    /// The note that a selection in a terminal went to the pasteboard, at
    /// the foot of the terminal for a moment (TerminalSelection.swift).
    @State private var copied: CopiedNote?

    private struct CopiedNote: Equatable {
        let id = UUID()
        let text: String
    }

    /// The picked session while it shows beside its teammates.
    private var split: Session? { store.splitSession }

    @ViewBuilder private func terminal(_ a: AttachInfo, active: Bool) -> some View {
        #if canImport(GhosttyKit)
        if TerminalEngine.named(engine) == .ghostty {
            framed(GhosttyTerminal(attach: .init(tmux: a.tmux, socket: a.socket, target: a.target), fontSize: store.fontSize,
                                   focusToken: store.focusTerminal, followsFocus: true, takesKeysOnStart: store.showTerminal,
                                   shows: active, onView: { store.terminalView = $0 }),
                   anchors: active)
        } else {
            framed(TerminalHost(info: a, focusToken: store.focusTerminal, fontSize: store.fontSize, store: store, active: active), anchors: active)
        }
        #elseif canImport(SwiftTerm)
        framed(TerminalHost(info: a, focusToken: store.focusTerminal, fontSize: store.fontSize, store: store, active: active), anchors: active)
        #else
        Placeholder(
            title: "The terminal opens in Ghostty for now",
            text: "This build has no terminal of its own yet. Pick a session on the left and the Ghostty window follows it.",
            action: ("Open the terminal", store.openInGhostty)
        )
        #endif
    }

    /// The terminal in its rounded frame, edged in the shown session's colour,
    /// where the lines from its card end while it shows (TabLink.swift).
    /// The split lays out its own panes inside the frame, so it is not
    /// padded; anchors is false for the terminal the split stands in for.
    private func framed(_ terminal: some View, padded: Bool = true, anchors: Bool = true) -> some View {
        let frame = RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
        let ground = Color(nsColor: Theme.terminalBackground)
        let shows = store.showTerminal && anchors
        return terminal
            .padding(.vertical, padded ? 10 : 4)
            .padding(.horizontal, padded ? 18 : 6)
            .background {
                // A click on the frame types to the terminal too.
                frame.fill(ground)
                    .shadow(color: .black.opacity(0.25), radius: 10, y: 3)
                    .overlay(frame.strokeBorder(edge, lineWidth: TabLink.width))
                    .onTapGesture { store.focusTerminal += 1 }
            }
            .transformAnchorPreference(key: TabLinkKey.self, value: .bounds) { ends, bounds in
                if shows { ends.frame = .init(bounds: bounds, ground: ground) }
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

    /// Its state, or what it needs from you or waits on, then the rest.
    private func subtitle(_ s: Session) -> String {
        let look = Look(s)
        var parts = [look.loud || look == .waiting ? s.detail ?? look.label : Look.label(s)]
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
