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
        // A theme of yours that changed, or one being made, draws the
        // ground and the frame again too (Looks.swift).
        let _ = LookBook.shared.revision
        // In zen the session shows alone, in zen's layout, with no panes,
        // right sidebar or strip under it (Zen.swift). The same views stay
        // in place throughout, only their sizes change, so the terminal is
        // never made again.
        let zen = self.zen
        VStack(spacing: 0) {
            // The right sidebar (RightSidebar.swift), a drawer that slides
            // in from the window's edge, over the terminal's or the
            // conversation's, and then sits after it: the terminal makes
            // room once it is in, in one step, and takes the room back as a
            // close starts. The frame stays the first child, so SwiftUI never
            // makes the terminal again, which would restart its tmux client.
            // In a narrow window the sidebar gives up width first, down to
            // its narrowest, so the terminal keeps some room.
            GeometryReader { geo in
                let width = min(CGFloat(store.side.width), max(CGFloat(SideState.minWidth), geo.size.width - minTerminal))
                // The panes, a rolodex of cards between the terminal and the
                // right sidebar (Rolodex.swift), slide in the same way: the
                // terminal makes room once they are in. Folded, they are a
                // strip of their tabs, and fold and unfold the same way.
                let side = store.sideDocked && zen == nil ? width : 0
                let room = geo.size.width - side
                let r = store.rolodex
                let keep = keepBesidePanes
                // Beside the terminal the wheel leaves it at least keep. Let
                // go of the divider past that point, and the wheel goes over
                // the terminal instead, which keeps the room it had beside the
                // wheel at its widest; where they overlap, the one with the
                // keyboard lies on top, and nothing changes size as they swap.
                let fit = RolodexModel.width(share: r.share, in: room, keep: keep)
                let deck = r.over ? RolodexModel.overWidth(share: r.share, in: room, fit: fit) : fit
                let overlap = r.noteOverlap(deck - fit, room: room, keep: keep, fit: fit)
                let overlaps = overlap > 0 && !r.folded
                // A fold or an unfold slides over the terminal's edge, as
                // before; the one with the keyboard comes up as it ends.
                let behind = overlaps && !r.raised && !r.folding
                let strip = RolodexModel.stripWidth
                ZStack(alignment: .trailing) {
                    main.modifier(FileDrop(session: selected))
                        .background { PlaceProbe { r.mainProbe = $0 } }
                        .padding(.trailing, zen == nil ? side + r.kept(fit, behind: behind) : 0)
                        // Over the wheel while it has the keyboard, under the
                        // right sidebar still.
                        .zIndex(behind ? 0.5 : 0)
                    if r.open, zen == nil {
                        if r.folded {
                            RolodexStrip()
                                .padding(.leading, 4)
                                .padding(.trailing, 10)
                                .padding(.vertical, 12)
                                .frame(width: strip)
                                .background { PlaceProbe { r.wheelProbe = $0 } }
                                .padding(.trailing, side)
                                .transition(.move(edge: .trailing))
                        } else {
                            // Over the terminal its tabs start where the
                            // terminal ends, so they show whichever is on
                            // top, and on top it stands on a window's ground.
                            RolodexView(lead: overlaps ? overlap : 0)
                                .padding(.leading, 4)
                                .padding(.trailing, 12)
                                .background { if overlaps, !behind { PanesGround() } }
                                .padding(.vertical, 12)
                                .frame(width: deck)
                                .background { PlaceProbe { r.wheelProbe = $0 } }
                                .padding(.trailing, side)
                                .transition(.move(edge: .trailing))
                        }
                        // The divider, at the edge that shows between them:
                        // the wheel's, or the terminal frame's while the
                        // terminal lies over the wheel.
                        let seam = r.folded ? strip : (behind ? fit + 11 : deck)
                        RolodexEdge(room: room, from: seam, keep: keep)
                            .padding(.trailing, side + seam - 6)
                            .zIndex(1.5)
                            .transition(.opacity)
                    }
                    if let g = r.guide, zen == nil {
                        // Over the terminal, the part of it the panes will
                        // cover, from the guide to the terminal frame's edge
                        // at its narrowest.
                        if g.place == .over {
                            let widest = RolodexModel.widest(in: room, keep: keep)
                            OverBand(width: g.width - widest - 12)
                                .padding(.vertical, 12)
                                .padding(.trailing, side + widest + 12)
                                .allowsHitTesting(false)
                                .zIndex(2)
                        }
                        Rectangle()
                            .fill(Theme.accent)
                            .frame(width: 2)
                            .padding(.vertical, 12)
                            .padding(.trailing, side + g.width - 1)
                            .allowsHitTesting(false)
                            .zIndex(2)
                    }
                    if store.side.open, zen == nil {
                        RightSidebar(guide: $sideGuide, drawn: Double(width),
                                     most: Double(max(CGFloat(SideState.minWidth), geo.size.width - minTerminal)))
                            .frame(width: width)
                            .padding(.vertical, 12)
                            .transition(.move(edge: .trailing))
                            .zIndex(1)
                    }
                    if let g = sideGuide, zen == nil {
                        Rectangle()
                            .fill(Theme.accent)
                            .frame(width: 2)
                            .padding(.vertical, 12)
                            .padding(.trailing, CGFloat(g) + 4)
                            .allowsHitTesting(false)
                            .zIndex(2)
                    }
                }
                // Each session's terminal size, beside its panes or not, for
                // the core to keep its agent's window at (PaneSizes.swift).
                .onChange(of: [room, fit, geo.size.height, r.open ? 1 : 0, r.folded ? 1 : 0, store.fontSize], initial: true) {
                    store.sizesSoon()
                }
                .onChange(of: r.shows) { store.sizesSoon() }
                .onChange(of: store.poppedOut == nil) { store.sizesSoon() }
                // Zen, its layout and Centred's width change the terminal's
                // size, whatever the room.
                .onChange(of: zenShape) { store.sizesSoon() }
            }
            if zen == nil { PhraseBar() }
        }
        // Edge to Edge is the terminal's ground from edge to edge.
        .background((zen == .edge && store.showTerminal ? Color(nsColor: Theme.terminalBackground) : Theme.ground).ignoresSafeArea())
        .toolbarBackground(Theme.ground, for: .windowToolbar)
        // The window's own title. The bar hides it (IrieApp.showsTitle),
        // or the name would show twice.
        .navigationTitle(selected?.name ?? "\(Brand.name)")
        .toolbar {
            // The bar's only title: the name and subtitle centre on its
            // midline beside the button.
            ToolbarItem(placement: .principal) {
                VStack(alignment: .leading, spacing: 0) {
                    Text(selected?.name ?? "\(Brand.name)")
                        .font(.system(size: 13, weight: .semibold))
                    // Its state and the rest, in its agent's colour while it
                    // works, as its card and the terminal's frame.
                    Text(selected.map { Self.subtitle($0) } ?? "No session")
                        .font(.system(size: 11))
                        .foregroundStyle(selected.flatMap(Look.agentInk).map(AnyShapeStyle.init) ?? AnyShapeStyle(.secondary))
                }
                .lineLimit(1)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            }
            ToolbarItem(placement: .primaryAction) {
                // The session's helpers, in its Helpers card beside the
                // terminal (Rolodex.swift), with that card's icon: the
                // helpers never split the terminal.
                Button {
                    store.toggleHelpers()
                } label: {
                    Label(store.helperShown ? "Hide Helpers" : "Show Helpers", systemImage: PaneKind.helpers.symbol)
                }
                .help(store.helperShown ? "Hide the panes (⇧⌘H)" : "Show the session’s helpers in the panes beside its terminal (⇧⌘H)")
                .disabled(selected == nil)
            }
            ToolbarItem(placement: .primaryAction) {
                // The terminal out in a Ghostty window of its own, and back
                // (PopOut.swift).
                let out = store.poppedOut != nil
                Button {
                    store.togglePopOut()
                } label: {
                    Label(out ? "Pop Back In" : "Pop Out", systemImage: out ? "pip.exit" : "macwindow.on.rectangle")
                }
                .help(out ? "Bring the terminal back into this window (⌥⌘O)" : "Pop the terminal out into a Ghostty window of its own (⌥⌘O)")
                .disabled(store.attach == nil)
            }
            // The panes beside the terminal, a rolodex of cards (Rolodex.swift).
            ToolbarItem(placement: .primaryAction) {
                Button {
                    store.togglePanes()
                } label: {
                    Label(store.rolodex.open ? "Hide Panes" : "Show Panes", systemImage: "rectangle.stack")
                }
                .help(store.rolodex.open ? "Hide the panes (⌥⌘P)" : "Show your panes beside the terminal: a shell, a browser, a plugin’s page (⌥⌘P; ⌥Tab walks them)")
            }
            // At the toolbar's trailing end, where the HIG puts a button that
            // opens an inspector.
            ToolbarItem(placement: .primaryAction) {
                Button {
                    store.toggleSide()
                } label: {
                    Label(store.side.open ? "Hide Right Sidebar" : "Show Right Sidebar", systemImage: "sidebar.right")
                }
                .help(store.side.open ? "Hide the right sidebar (⌥⌘B)"
                      : "Show the right sidebar: \(ListFormatter.localizedString(byJoining: store.sideTabs.map(\.title) + store.panes.map(\.title))) (⌥⌘B)")
            }
        }
    }

    /// What the terminal keeps beside the sidebar before the sidebar gives up
    /// width: about 80 columns and the frame round them.
    private var minTerminal: CGFloat { 80 * store.fontSize * 0.6 + 60 }

    /// What it keeps beside the panes, which give up width first: about 60
    /// columns, since every agent takes the slot's size.
    private var keepBesidePanes: CGFloat { 60 * store.fontSize * 0.6 + 60 }

    private var main: some View {
        ZStack(alignment: .top) {
            // Once attached, the terminal stays: tmux outlives a core
            // restart, so a flap gets a banner, not a reattach. Only a pop
            // out ends it: the Ghostty window is then the one client that
            // sizes the slot, and a note stands in the frame (PopOut.swift).
            if let a = store.attach {
                if store.poppedOut != nil {
                    if store.showTerminal { framed(PoppedOutNote()) }
                } else {
                    // The terminal stays attached under the conversation, so
                    // ⌥⌘T flips between them at once, and under the split.
                    let shows = store.showTerminal && split == nil
                    terminal(a, active: shows)
                        .opacity(shows ? 1 : 0)
                        .allowsHitTesting(shows)
                        .accessibilityHidden(!shows)
                    if let s = split { framed(TeamSplit(session: s, team: s.team ?? []), padded: false) }
                }
            } else if store.isFixture, store.showTerminal {
                // `--snapshot` attaches no terminal: the frame round an empty
                // one, the split's panes, or the note of a pop out.
                if store.poppedOut != nil {
                    framed(PoppedOutNote())
                } else if let s = split {
                    framed(TeamSplit(session: s, team: s.team ?? []), padded: false)
                } else {
                    framed(Color.clear)
                }
            }
            // The conversation needs only the core, not the terminal. In
            // zen it keeps clear of the title and the rail.
            if !store.showTerminal && (store.attach != nil || online) {
                ConversationView()
                    .padding(.top, zen == .centred || zen == .watch ? ZenModel.titleRoom - 12 : 0)
                    .padding(.leading, zen == .watch ? ZenModel.railWidth + 12 : 0)
            } else if store.attach == nil, !store.isFixture {
                if store.starting {
                    ProgressView("Starting \(Brand.name)…").frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if case let .offline(why) = store.link {
                    Placeholder(title: why, text: "The sidebar says what to do. The app connects by itself once \(Brand.name) is running.")
                } else {
                    ProgressView("Connecting to \(Brand.name)…").frame(maxWidth: .infinity, maxHeight: .infinity)
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
        // The notice on show (Notices.swift): here, at the top of the
        // working area, and never over the task bar.
        .overlay(alignment: .topTrailing) {
            NoticeToast()
                .padding(.top, 20)
                .padding(.trailing, 24)
                .padding(.leading, 48)
        }
        .animation(.easeOut(duration: 0.15), value: store.notice)
        // Zen (Zen.swift): Keep Watch's rail where the sidebar was, the
        // session's name over Centred's column and Keep Watch's frame, and
        // for a moment as zen starts, how to leave it.
        .overlay(alignment: .leading) {
            if zen == .watch {
                ZenRail()
                    .padding(.vertical, 12)
                    .padding(.leading, 12)
            }
        }
        .overlay(alignment: zen == .watch ? .topLeading : .top) {
            if zen == .centred || zen == .watch {
                ZenTitle(session: selected)
                    .padding(.top, 13)
                    .padding(.leading, zen == .watch ? 12 + ZenModel.railWidth + 16 : 24)
                    .padding(.trailing, 24)
            }
        }
        .overlay(alignment: .top) {
            if let note = zenNote, zen != nil {
                ZenHint(text: note)
                    .padding(.top, zen == .edge ? 14 : ZenModel.titleRoom + 14)
                    .transition(.opacity)
                    .id(note)
            }
        }
        .animation(.easeOut(duration: 0.2), value: zenNote)
        .onChange(of: store.zen.started) {
            sayInZen(ZenHint.leave, for: 3)
            AccessibilityNotification.Announcement("Zen. \(ZenHint.leave).").post()
        }
        // Edge to Edge has no title: a session shown there says its name
        // for a moment.
        .onChange(of: selected?.id) {
            if zen == .edge, let s = selected { sayInZen(s.name, for: 1.5) }
        }
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

    /// Zen's layout while zen is on; nil outside zen (Zen.swift).
    private var zen: ZenLayout? { store.zen.on ? store.zen.layout : nil }

    /// What of zen sizes the terminal, which every agent's window takes.
    private var zenShape: String { zen.map { "\($0.rawValue) \(store.zen.columns)" } ?? "" }

    /// Centred's column: zen's columns of text at the text size in use, and
    /// the frame's padding round them.
    private var zenColumn: CGFloat { CGFloat(store.zen.columns) * store.fontSize * store.cellRatio + 36 + 1 }

    /// Round the terminal's frame: 12 points, or zen's room for its title
    /// and rail; none at all edge to edge.
    private func frameInsets(_ zen: ZenLayout?) -> EdgeInsets {
        switch zen {
        case nil: return EdgeInsets(top: 12, leading: 12, bottom: 12, trailing: 12)
        case .centred?: return EdgeInsets(top: ZenModel.titleRoom, leading: 24, bottom: 24, trailing: 24)
        case .edge?: return EdgeInsets()
        case .watch?: return EdgeInsets(top: ZenModel.titleRoom, leading: 12 + ZenModel.railWidth + 10, bottom: 12, trailing: 12)
        }
    }

    /// What zen says for a moment: how to leave it, as it starts, or in
    /// Edge to Edge the name of a session as it shows.
    @State private var zenNote: String?
    @State private var zenNotes = 0

    private func sayInZen(_ text: String, for seconds: Double) {
        zenNotes += 1
        let n = zenNotes
        zenNote = text
        DispatchQueue.main.asyncAfter(deadline: .now() + seconds) {
            if zenNotes == n { zenNote = nil }
        }
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
            action: ("Open the terminal", store.popOut)
        )
        #endif
    }

    /// The terminal in its rounded frame, edged in the shown session's colour,
    /// where the lines from its card end while it shows (TabLink.swift).
    /// The split lays out its own panes inside the frame, so it is not
    /// padded; anchors is false for the terminal the split stands in for.
    private func framed(_ terminal: some View, padded: Bool = true, anchors: Bool = true) -> some View {
        // Edge to Edge has no frame to draw: the terminal's ground runs to
        // the screen's edges (Zen.swift).
        let zen = self.zen
        let bare = zen == .edge
        let frame = RoundedRectangle(cornerRadius: bare ? 0 : Theme.frameRadius, style: .continuous)
        let ground = Color(nsColor: Theme.terminalBackground)
        // In zen no card shows, so no lines end here.
        let shows = store.showTerminal && anchors && zen == nil
        // While the sidebar's edge or the window's is dragged, the terminal
        // keeps its size, and takes the new one as the drag ends.
        return terminal
            .modifier(HeldSize())
            .padding(.vertical, padded ? 10 : 4)
            .padding(.horizontal, padded ? 18 : 6)
            .background {
                // A click on the frame types to the terminal too.
                frame.fill(ground)
                    .shadow(color: .black.opacity(bare ? 0 : 0.25), radius: 10, y: 3)
                    .overlay(frame.strokeBorder(bare ? Color.clear : edge, lineWidth: TabLink.width))
                    .onTapGesture { store.focusTerminal += 1 }
            }
            // Where the lines from the selected card end (TabLink.swift).
            .background {
                if shows {
                    TabLinkFrame(session: selected?.id, ground: Theme.terminalBackground)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
            }
            // Centred's column, in the middle of the room.
            .frame(maxWidth: zen == .centred ? zenColumn : .infinity)
            .padding(frameInsets(zen))
            .frame(maxWidth: .infinity)
    }

    /// The frame's colour: the shown session's edge, as round its card
    /// (Look.edge), or sun with none shown.
    private var edge: Color { selected.map(Look.edge) ?? Theme.sunEdge }

    /// What the banner says while the core is away.
    private var linkNote: String? {
        switch store.link {
        case .online: return nil
        case .connecting: return "Connecting to \(Brand.name)…"
        case let .offline(why): return "\(why) Reconnecting…"
        }
    }

    /// Its state, or what it needs from you or waits on, then the rest: the
    /// title bar's, and zen's title (Zen.swift).
    static func subtitle(_ s: Session) -> String {
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
