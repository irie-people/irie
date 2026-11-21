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
        // In zen the session shows with its own panes alone, its card a
        // bubble over the terminal's corner, in zen's layout, with no right
        // sidebar or strip under it (Zen.swift).
        // The same views stay in place throughout, only their sizes change,
        // so the terminal is never made again.
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
                // Reversed, the parts run right to left: the right sidebar
                // and the panes at the window's left, the terminal beside
                // the sidebar (Store.reversed). Each reads left to right
                // inside, and is told which way its edges face.
                let flip = store.mirrored
                ZStack(alignment: .trailing) {
                    main.modifier(FileDrop(session: selected))
                        .background { PlaceProbe { r.mainProbe = $0 } }
                        .environment(\.layoutDirection, .leftToRight)
                        .padding(.trailing, side + r.kept(fit, behind: behind))
                        // Over the wheel while it has the keyboard, under the
                        // right sidebar still.
                        .zIndex(behind ? 0.5 : 0)
                    if r.open {
                        if r.folded {
                            RolodexStrip(flip: flip)
                                .environment(\.layoutDirection, .leftToRight)
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
                            RolodexView(lead: overlaps && !flip ? overlap : 0, trail: overlaps && flip ? overlap : 0)
                                .environment(\.layoutDirection, .leftToRight)
                                .padding(.leading, 4)
                                .padding(.trailing, 12)
                                .background { if overlaps, !behind { PanesGround(flip: flip) } }
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
                        RolodexEdge(room: room, from: seam, keep: keep, flip: flip)
                            .padding(.trailing, side + seam - 6)
                            .zIndex(1.5)
                            .transition(.opacity)
                    }
                    if let g = r.guide {
                        // Over the terminal, the part of it the panes will
                        // cover, from the guide to the terminal frame's edge
                        // at its narrowest.
                        if g.place == .over {
                            let widest = RolodexModel.widest(in: room, keep: keep)
                            OverBand(width: g.width - widest - 12, flip: flip)
                                .environment(\.layoutDirection, .leftToRight)
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
                                     most: Double(max(CGFloat(SideState.minWidth), geo.size.width - minTerminal)), flip: flip)
                            .environment(\.layoutDirection, .leftToRight)
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
                .environment(\.layoutDirection, flip ? .rightToLeft : .leftToRight)
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
        .background(Theme.ground.ignoresSafeArea())
        .toolbarBackground(Theme.ground, for: .windowToolbar)
        // The window's own title. The bar hides it (IrieApp.showsTitle),
        // or the name would show twice.
        .navigationTitle(selected?.name ?? "\(Brand.name)")
        .toolbar {
            // Reversed, the sidebar's name and counts take this side's part
            // of the bar, which lies over the sidebar then, and the session's
            // items go to the sidebar's part (SidebarView).
            if store.reversed {
                ToolbarItem(placement: .automatic) { TitleName() }
                ToolbarItem(placement: .automatic) { Counts() }
            } else {
                SessionBarItems.items(store: store, reversed: false)
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
            // zen the card's bubble lies over its corner, as over the
            // terminal's.
            if !store.showTerminal && (store.attach != nil || online) {
                zenGroup(ConversationView(), margins: EdgeInsets())
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
            VStack(alignment: .trailing, spacing: 10) {
                NoticeToast()
                // What zen offers, one thing at a time, under any notice
                // and over the terminal's oldest lines, never its prompt
                // (ZenCalm.swift).
                if zen != nil, let o = store.zen.offer {
                    ZenOfferBubble(offer: o)
                        .transition(.opacity)
                        .id("\(o.id) \(o.kind)")
                }
            }
            .padding(.top, 20)
            .padding(.trailing, 24)
            .padding(.leading, 48)
        }
        .animation(.easeOut(duration: 0.15), value: store.notice)
        .animation(.easeOut(duration: 0.2), value: store.zen.offer)
        // For a moment as zen starts, how to leave it (Zen.swift), over the
        // terminal's top.
        .overlay(alignment: .top) {
            if let note = zenNote, zen != nil {
                ZenHint(text: note)
                    .padding(.top, 36)
                    .padding(.leading, zen == .watch ? store.zen.watchColumn : 0)
                    .transition(.opacity)
                    .id(note)
            }
        }
        .animation(.easeOut(duration: 0.2), value: zenNote)
        .onChange(of: store.zen.started) {
            sayInZen(ZenHint.leave, for: 3)
            AccessibilityNotification.Announcement("Zen. \(ZenHint.leave).").post()
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
    private var zenShape: String { zen.map { "\($0.rawValue) \(store.zen.columns) \(store.zen.watchColumn)" } ?? "" }

    /// Centred's column: zen's columns of text at the text size in use, and
    /// the frame's padding round them.
    private var zenColumn: CGFloat { CGFloat(store.zen.columns) * store.fontSize * store.cellRatio + 36 + 1 }

    /// The session's part, the terminal's frame or the conversation, as zen
    /// lays it out: the card's bubble over its top-left corner, Keep Watch's
    /// list of the other sessions at the room's left edge, and in Centred at
    /// most the margins and the reading column wide, in the middle of the
    /// room. Outside zen it is only the margins round the frame, as ever: no
    /// bubble, no list, no limit.
    private func zenGroup(_ part: some View, margins: EdgeInsets) -> some View {
        let zen = self.zen
        let column = zen == .watch ? store.zen.watchColumn : 0
        return part
            .overlay(alignment: .topLeading) {
                if zen != nil {
                    VStack(alignment: .leading, spacing: 6) {
                        ZenBubble()
                        // Another session that would like you, softly
                        // (ZenCalm.swift); Keep Watch lists them.
                        if zen != .watch {
                            ZenOthersLine()
                        }
                    }
                    .padding(.top, 10)
                    .padding(.leading, 12)
                }
            }
            .padding(margins)
            .padding(.leading, column)
            .overlay(alignment: .leading) {
                if zen == .watch {
                    ZenWatchColumn()
                        .frame(width: column)
                }
            }
            .frame(maxWidth: zen == .centred ? margins.leading + margins.trailing + zenColumn : .infinity)
            .padding(.vertical, zen == .centred ? 12 : 0)
            .frame(maxWidth: .infinity)
    }

    /// What zen says for a moment, in a capsule: how to leave it, as it
    /// starts.
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
        let frame = RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous)
        let ground = Color(nsColor: Theme.terminalBackground)
        let shows = store.showTerminal && anchors
        // While the sidebar's edge or the window's is dragged, the terminal
        // keeps its size, and takes the new one as the drag ends.
        let inFrame = terminal
            .modifier(HeldSize())
            .padding(.vertical, padded ? 10 : 4)
            .padding(.horizontal, padded ? 18 : 6)
            .background {
                // A click on the frame types to the terminal too.
                frame.fill(ground)
                    .shadow(color: .black.opacity(0.25), radius: 10, y: 3)
                    .overlay(frame.strokeBorder(edge, lineWidth: TabLink.width))
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
        // 12 points round it, Edge to Edge's narrower, and zen's bubble over
        // its corner (zenGroup).
        let margins = zen == .edge ? EdgeInsets(top: 6, leading: 8, bottom: 6, trailing: 6)
                                   : EdgeInsets(top: 12, leading: 12, bottom: 12, trailing: 12)
        return zenGroup(inFrame, margins: margins)
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

/// The session side's items in the title bar: the session's name and
/// subtitle, then its helpers, the pop out, the panes and the right sidebar.
/// Normally the name centres in the terminal side's part of the bar and the
/// buttons sit at its end. Reversed (Store.reversed), they go in the
/// sidebar's part, which then lies over the terminal, one after another
/// beside the traffic lights.
/// A builder called from a view's body, on the main actor, as the store
/// is: ToolbarContent's own body is not.
@MainActor
enum SessionBarItems {
    @ToolbarContentBuilder
    static func items(store: Store, reversed: Bool) -> some ToolbarContent {
        let selected = store.selectedRow
        // The bar's only title: the name and subtitle centre on its
        // midline beside the button.
        ToolbarItem(placement: reversed ? .automatic : .principal) {
            VStack(alignment: .leading, spacing: 0) {
                Text(selected?.name ?? "\(Brand.name)")
                    .font(.system(size: 13, weight: .semibold))
                // Its state and the rest, in its agent's colour while it
                // works, as its card and the terminal's frame.
                Text(selected.map { TerminalArea.subtitle($0) } ?? "No session")
                    .font(.system(size: 11))
                    .foregroundStyle(selected.flatMap(Look.agentInk).map(AnyShapeStyle.init) ?? AnyShapeStyle(.secondary))
            }
            .lineLimit(1)
            // Beside the buttons rather than centred, it keeps to a width,
            // so a long name never pushes them into the overflow menu.
            .frame(idealWidth: reversed ? 300 : nil, maxWidth: reversed ? 360 : .infinity, maxHeight: .infinity, alignment: .leading)
            .padding(.leading, reversed ? 6 : 0)
        }
        ToolbarItem(placement: reversed ? .automatic : .primaryAction) {
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
        ToolbarItem(placement: reversed ? .automatic : .primaryAction) {
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
        ToolbarItem(placement: reversed ? .automatic : .primaryAction) {
            Button {
                store.togglePanes()
            } label: {
                Label(store.rolodex.open ? "Hide Panes" : "Show Panes", systemImage: "rectangle.stack")
            }
            .help(store.rolodex.open ? "Hide the panes (⌥⌘P)" : "Show your panes beside the terminal: a shell, a browser, a plugin’s page (⌥⌘P; ⌥Tab walks them)")
        }
        // At the toolbar's trailing end, where the HIG puts a button that
        // opens an inspector; reversed, the right sidebar comes out of the
        // left edge, and its mark says so.
        ToolbarItem(placement: reversed ? .automatic : .primaryAction) {
            Button {
                store.toggleSide()
            } label: {
                Label(store.side.open ? "Hide Right Sidebar" : "Show Right Sidebar", systemImage: reversed ? "sidebar.left" : "sidebar.right")
            }
            .help(store.side.open ? "Hide the right sidebar (⌥⌘B)"
                  : "Show the right sidebar: \(ListFormatter.localizedString(byJoining: store.sideTabs.map(\.title) + store.panes.map(\.title))) (⌥⌘B)")
        }
    }
}
