import AppKit
import SwiftUI

/// The sidebar: a card per session, each with its helpers under it. The List
/// view is one list; the Repo view is a tree, a group per repo. Either way
/// the cards keep the list's one order, which only the user changes, and the
/// parked ones wait in the Parked group at the end, so a state change only
/// changes a card's mark and colours, never where it is (DESIGN §4.1). A
/// click shows that agent's terminal and gives it the keyboard; the arrow
/// keys walk the cards and keep the keyboard here, and z parks or unparks
/// the selected card. The switch between the views, and the selected
/// session's details, sit above the task bar, on one panel with it.
struct SidebarView: View {
    @EnvironmentObject private var store: Store
    @FocusState private var listFocused: Bool

    /// The least room the cards keep, however tall the details are dragged:
    /// a group's header and two cards.
    static let cardsLeast: CGFloat = 132

    var body: some View {
        // One row of its own above the cards: Irie's mark, its name and the
        // counts sit in the title bar, beside the traffic lights.
        VStack(spacing: 0) {
            // The header sits lighter than the list, with rounded bottom
            // corners, like the footer.
            FindField()
                .background(UnevenRoundedRectangle(bottomLeadingRadius: Theme.frameRadius, bottomTrailingRadius: Theme.frameRadius, style: .continuous)
                    .fill(Theme.card).shadow(color: Theme.shadow, radius: 3, y: 1))
                .zIndex(1)
            content
                .frame(minHeight: Self.cardsLeast)
            ReviewDock()
            // The footer sits lighter than the list, with rounded top
            // corners: the switch between the views, the details and the
            // task bar, on one panel.
            VStack(spacing: 0) {
                if !store.sessions.isEmpty {
                    ModeSwitch()
                }
                // The details, once there is a session to pick: small until
                // their edge is dragged, and the cards have the rest.
                if !store.sessions.isEmpty || !store.outside.isEmpty {
                    DetailsPanel()
                }
                TaskBar()
            }
            .background(UnevenRoundedRectangle(topLeadingRadius: Theme.frameRadius, topTrailingRadius: Theme.frameRadius, style: .continuous)
                .fill(Theme.card).shadow(color: Theme.shadow, radius: 3, y: -1))
            // It takes its height before the cards, which keep their least.
            .layoutPriority(1)
        }
        .background(Theme.sidebar.ignoresSafeArea())
        // A card parked from under the pointer takes its place once the
        // pointer leaves it, or the app the front (Controls.swift).
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didResignActiveNotification)) { _ in store.settle() }
        .toolbar {
            // Both over the sidebar, beside the traffic lights and above the
            // find field: macOS sets automatic items in their own column's
            // part of the bar, leading to trailing. A .navigation item goes
            // to the detail column's bar, ahead of its title, and a
            // .primaryAction one to the bar's leading edge. Short of room,
            // the bar moves its trailing item into the overflow menu first,
            // so the counts go before the name does.
            ToolbarItem(placement: .automatic) { TitleName() }
            ToolbarItem(placement: .automatic) { Counts() }
        }
        // The toggle would crowd them out of the title bar; View >
        // Hide Sidebar (SidebarCommands) still hides it.
        .toolbar(removing: .sidebarToggle)
        .modifier(OutsideMoveConfirmation())
    }

    @ViewBuilder private var content: some View {
        let groups = store.drawnGroups
        if case let .offline(why) = store.link, store.sessions.isEmpty {
            Offline(why: why, socket: store.socketPath)
        } else if store.sessions.isEmpty && store.outside.isEmpty {
            if store.link == .connecting {
                Hint(text: "Connecting to Irie…")
            } else {
                Welcome()
            }
        } else if groups.isEmpty && store.drawnOutside.isEmpty {
            Hint(text: "No sessions match “\(store.filter)”.")
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    // One clock for every card's age.
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        LazyVStack(alignment: .leading, spacing: 6) {
                            ForEach(groups) { g in
                                // The List view's one list needs no header.
                                if g.id != SessionGroup.list { GroupHeader(group: g) }
                                if store.isOpen(g) {
                                    ForEach(g.sessions) { s in
                                        SessionCard(session: s, selected: s.id == store.selection && store.outsideSelection == nil, listFocused: listFocused, now: ctx.date)
                                            .id(s.id)
                                        HelperRows(session: s)
                                    }
                                }
                            }
                            OutsideSection(now: ctx.date, listFocused: listFocused)
                        }
                        .padding(.horizontal, 10)
                        .padding(.top, 3)
                        .padding(.bottom, 8)
                    }
                }
                // The lines from the selected card show while the stretch
                // of its edge they open is in sight here (TabLink.swift).
                .transformAnchorPreference(key: TabLinkKey.self, value: .bounds) { $0.list = $1 }
                .onChange(of: store.selection) {
                    if let id = store.selection { proxy.scrollTo(id) }
                }
                .onChange(of: store.outsideSelection) {
                    if let k = store.outsideSelection { proxy.scrollTo("outside:" + k) }
                }
            }
            .focusable()
            .focusEffectDisabled()
            .focused($listFocused)
            .onChange(of: store.focusSidebar) { listFocused = true }
            // The arrows walk on into the outside rows, which they only select.
            // A name open to edit keeps its keys.
            .onKeyPress(.upArrow) {
                guard store.renaming == nil else { return .ignored }
                store.walk(-1)
                return .handled
            }
            .onKeyPress(.downArrow) {
                guard store.renaming == nil else { return .ignored }
                store.walk(1)
                return .handled
            }
            .onKeyPress(.return) {
                guard store.renaming == nil else { return .ignored }
                if let o = store.selectedOutside {
                    store.explainOutside(o)
                    return .handled
                }
                guard let id = store.selection else { return .ignored }
                store.show(id, focus: true)
                return .handled
            }
            // z parks or unparks the selected card, and Z folds Parked.
            .onKeyPress(characters: CharacterSet(charactersIn: "zZ"), phases: .down) { press in
                guard store.renaming == nil, store.canPark, press.modifiers.subtracting([.shift, .capsLock]).isEmpty else { return .ignored }
                if press.modifiers.contains(.shift) {
                    store.foldParked()
                } else if store.closable != nil {
                    store.toggleParked()
                } else {
                    return .ignored
                }
                return .handled
            }
            .accessibilityLabel("Sessions")
        }
    }
}

/// One session, always two lines: its name, then what it needs from you (in
/// red), what it is doing, or how it ended, in plain words. The trailing
/// slot has a fixed width, so the name never re-wraps when the age ticks or
/// the pointer brings up the actions. The card is soft and rounded, and lies
/// flat in the list, with no edge or shadow, like a window's other tabs:
/// washed in its agent's colour while it works, tinted and edged in red
/// while it needs you. The selected card stands up from the others, on a
/// gentle shadow, filled with the selection and edged in its session's
/// colour, which two lines carry on to the terminal's frame (TabLink.swift).
/// A selected working card is a gradient, from the selection by its mark to
/// the colour round its terminal at the trailing end.
private struct SessionCard: View {
    @EnvironmentObject private var store: Store
    let session: Session
    let selected: Bool
    let listFocused: Bool
    let now: Date
    @State private var hovering = false

    /// The trailing slot's width: the age and tokens, or the actions.
    private static let slot: CGFloat = 66

    private var look: Look { Look(session) }
    /// Its name drawn muted: idle and the like, and a parked finished turn,
    /// which is out of the way.
    private var quiet: Bool { look.quiet || look == .done && session.isParked }
    private var renaming: Bool { store.renaming == session.id }
    /// Its own colour while it works, so a glance tells the agents apart.
    private var tint: Color? { Look.agentTint(session) }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            StateMark(look: look, tint: tint).padding(.top, 2)
            VStack(alignment: .leading, spacing: 2) {
                if renaming {
                    RenameField(session: session)
                } else {
                    HStack(spacing: 4) {
                        Text(session.name)
                            .font(Theme.font(13.5, quiet ? .medium : .semibold))
                            .foregroundStyle(quiet ? Theme.muted : Theme.ink)
                            .lineLimit(1)
                            .truncationMode(.tail)
                        if session.isPromoted { PromotedBadge() }
                    }
                }
                Text(status)
                    .font(Theme.font(12, look.loud ? .semibold : .regular))
                    .foregroundStyle(look.loud ? Theme.needs : session.loose != nil ? Theme.accentInk : Theme.muted)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            ZStack(alignment: .topTrailing) {
                VStack(alignment: .trailing, spacing: 2) {
                    Text(Age.short(session.created, now: now))
                    Text(session.tokens.map { Tokens.short($0.total) } ?? " ")
                }
                .font(Theme.font(11)).monospacedDigit()
                // In ink over a selected working card's deep end.
                .foregroundStyle(deep == nil ? Theme.muted : Theme.ink)
                .opacity(hovering ? 0 : 1)
                QuickActions(session: session, ink: deep != nil)
                    .opacity(hovering ? 1 : 0)
                    .allowsHitTesting(hovering)
            }
            .frame(width: Self.slot, alignment: .trailing)
        }
        .padding(.vertical, 8)
        .padding(.horizontal, Theme.inset)
        .background(ground.shadow(color: selected ? Theme.shadow : .clear, radius: 3, y: 1.5))
        .overlay(shape.strokeBorder(edge, lineWidth: edgeWidth))
        .transformAnchorPreference(key: TabLinkKey.self, value: .bounds) { ends, bounds in
            if selected {
                ends.card = .init(bounds: bounds, color: edge, width: edgeWidth, ground: trailingGround)
            }
        }
        .contentShape(shape)
        .onHover {
            hovering = $0
            store.hover(session.id, $0)
        }
        .onTapGesture { if !renaming { store.show(session.id, focus: true) } }
        // A double click opens the name to edit; its first click showed it.
        .simultaneousGesture(TapGesture(count: 2).onEnded { store.beginRename(session.id) })
        .modifier(Reorderable(session: session))
        .contextMenu { Actions(session: session) }
        .help([session.name, session.detail, session.command].compactMap { $0 }.joined(separator: "\n"))
        // While the name is open to edit, VoiceOver reaches the field.
        .accessibilityElement(children: renaming ? AccessibilityChildBehavior.contain : .ignore)
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction { store.show(session.id, focus: true) }
        .accessibilityActions {
            Button(session.isFrozen ? "Resume" : "Pause") { store.setFrozen(session, !session.isFrozen) }
            Button(session.isPromoted ? "Stop promoting" : "Promote to the top") { store.setPromoted(session, !session.isPromoted) }
            if store.canPark { Button(session.isParked ? "Unpark" : "Park") { store.setParked(session, !session.isParked) } }
            if store.canMove(session, by: -1) { Button("Move up") { store.move(session, by: -1) } }
            if store.canMove(session, by: 1) { Button("Move down") { store.move(session, by: 1) } }
            Button("Rename") { store.beginRename(session.id) }
            if let cwd = session.cwd {
                Button("Show the folder in Finder") { store.reveal(cwd) }
                Button("Copy the folder path") { store.copy(cwd, what: "folder path") }
            }
            Button("Open in Ghostty") {
                store.show(session.id)
                store.openInGhostty()
            }
            if let branch = session.branch, !branch.isEmpty {
                Button("Copy the branch name") { store.copy(branch, what: "branch name") }
            }
            Button("Copy the session ID") { store.copy(session.id, what: "session ID") }
            Button("Close the session") { store.close(session) }
        }
    }

    /// The second line. A finished jot whose work has not landed, or that
    /// waits on work of its own in the background, says so (DESIGN §4.8).
    private var status: String {
        switch look {
        case .starting: return "Starting…"
        case .frozen where session.freezing == true: return "Freezing…"
        case .done, .idle: return session.detail.flatMap { $0.isEmpty ? nil : $0 } ?? Look.label(session)
        case .frozen, .paused, .pausing: return look.label
        default: return session.detail ?? look.label
        }
    }

    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: Theme.radius, style: .continuous) }

    /// The fill, its wash, and a selected working card's deep end across the
    /// trailing slot.
    private var ground: some View {
        shape.fill(fill)
            .overlay { if let wash { shape.fill(wash) } }
            .overlay(alignment: .trailing) {
                if let deep {
                    LinearGradient(colors: [deep.opacity(0), deep], startPoint: .leading, endPoint: .trailing)
                        .frame(width: Self.slot + Theme.inset)
                }
            }
            .clipShape(shape)
    }

    /// The selection's fill is full even while the terminal has the keyboard,
    /// so the shown session is plain to see; the edge tells where the
    /// keyboard is.
    private var fill: Color {
        if selected { return Theme.selection }
        if look.loud { return Theme.needsTint }
        return hovering ? Theme.hover : Theme.card
    }

    /// Its own colour across the card while it works, fullest by the mark and
    /// fading into the fill toward the trailing slot. A selected card turns
    /// it round: the selection by the mark, turning to its own colour, which
    /// deepens across the trailing slot into the colour round its terminal.
    private var wash: LinearGradient? {
        guard let c = Look.agentWash(session) else { return nil }
        let colors = selected ? [c.opacity(0), c] : [c, c.opacity(0.5)]
        return LinearGradient(colors: colors, startPoint: .leading, endPoint: .trailing)
    }

    private var deep: Color? { selected ? Look.agentDeep(session) : nil }

    /// A selected card's ground at its trailing edge, bottom first: the fill,
    /// then the full ends of its wash and of its deep end. The lines from it
    /// paint these over its edge where they open it (TabLink.swift).
    private var trailingGround: [Color] { [fill] + [Look.agentWash(session), deep].compactMap { $0 } }

    /// Round the selected card, the colour of the terminal's frame and the
    /// lines between them; a faint accent round the one under the pointer;
    /// red round one that needs you. The rest have none: a working card's
    /// agent shows in its mark and its wash.
    private var edge: Color {
        if selected { return Look.edge(session) }
        if hovering { return Theme.accent.opacity(0.45) }
        return look.loud ? Theme.needs.opacity(0.5) : .clear
    }

    /// The selected card's edge is as wide as the lines from it, and bolder
    /// while the list has the keyboard, so its colour always matches the
    /// lines' and the frame's while its width tells where the keyboard is.
    /// The lines narrow from it as they curve away.
    private var edgeWidth: CGFloat { selected ? (listFocused ? 2.5 : TabLink.width) : 1 }

    private var spoken: String {
        var parts = [session.name, Look.label(session)]
        if let d = session.detail, !d.isEmpty { parts.append(d) }
        parts.append("started " + Age.spoken(session.created, now: now))
        if let t = session.tokens { parts.append("\(t.total) tokens") }
        return parts.joined(separator: ", ")
    }
}

/// A session's name, open to edit in its card at the name's size, so the
/// card keeps its height. Return renames the session, Esc keeps the name,
/// and clicking elsewhere renames it, as in Finder. What is typed lives in
/// the store, which renames it when another session is shown.
private struct RenameField: View {
    @EnvironmentObject private var store: Store
    let session: Session
    @FocusState private var focused: Bool

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 4, style: .continuous)
        // A frame like the find field's, just outside the text, so the name
        // stays where it was drawn.
        TextField("Name", text: $store.renameText)
            .textFieldStyle(.plain)
            .font(Theme.font(13.5, .semibold))
            .foregroundStyle(Theme.ink)
            .focused($focused)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 3)
            .background(shape.fill(Theme.field))
            .overlay(shape.strokeBorder(Theme.sunEdge, lineWidth: 1))
            .padding(.horizontal, -3)
            // After the card has drawn it, so the field can take the keyboard.
            .task { focused = true }
            .onSubmit {
                store.endRename(commit: true)
                store.focusSidebar += 1
            }
            .onExitCommand {
                store.endRename(commit: false)
                store.focusSidebar += 1
            }
            .onChange(of: focused) {
                if !focused { store.endRename(commit: true) }
            }
            .accessibilityLabel("New name for \(session.name)")
    }
}

/// A group's header: its name and count, and a chevron that folds it. A
/// folded group says how many of its sessions need you, in red, and the
/// Parked header turns red while any parked session needs you.
private struct GroupHeader: View {
    @EnvironmentObject private var store: Store
    let group: SessionGroup

    var body: some View {
        let open = store.isOpen(group)
        let needs = group.sessions.filter { Look($0).loud }.count
        let note = !open && needs > 0 ? "\(needs) \(needs == 1 ? "needs" : "need") you" : nil
        Button { store.fold(group) } label: {
            HStack(spacing: 5) {
                Image(systemName: "chevron.right")
                    .font(.system(size: 8.5, weight: .heavy))
                    .rotationEffect(.degrees(open ? 90 : 0))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 10)
                Text(group.title)
                    .font(Theme.font(12, .heavy))
                    .foregroundStyle(group.id == SessionGroup.parked && needs > 0 ? Theme.needs : Theme.ink)
                    .lineLimit(1)
                if let note {
                    Text("· " + note).font(Theme.font(11.5, .bold)).foregroundStyle(Theme.needs).lineLimit(1)
                }
                Spacer(minLength: 4)
                Text("\(group.sessions.count)").font(Theme.font(11.5, .bold)).monospacedDigit().foregroundStyle(Theme.muted)
            }
            .padding(.horizontal, 4)
            .padding(.top, 3)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(group.id == SessionGroup.parked
              ? "Out of the way, and still running. Unpark one with its P button, z, or a drag out of Parked; ⌘Z takes back the last park."
              : group.path.map(Age.tilde) ?? group.title)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([group.title, "\(group.sessions.count) sessions", note].compactMap { $0 }.joined(separator: ", "))
        .accessibilityHint(open ? "Folds the group" : "Opens the group")
        .accessibilityAddTraits([.isHeader, .isButton])
        .accessibilityAction { store.fold(group) }
    }
}

/// A session's helpers, its running subagents, under its card: each one's
/// kind and what it is doing, in red when it needs you. Three at most, then
/// how many more.
private struct HelperRows: View {
    @EnvironmentObject private var store: Store
    let session: Session

    var body: some View {
        let helpers = session.subagents ?? []
        let team = session.team ?? []
        // In their orchestrator's colour while it works, so they read as its.
        let own = Look.agentInk(session)
        if !helpers.isEmpty || !team.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(helpers.prefix(3)) { h in
                    let loud = h.needsYou == true
                    let shown = store.helperShown && store.selection == session.id && store.openHelpers[session.id] == h.agent
                    // A click shows the helper beside its session's conversation.
                    Button { store.openHelper(h.agent, in: session.id) } label: {
                        HStack(spacing: 5) {
                            Text("↳").font(Theme.font(12)).foregroundStyle(own ?? Theme.muted)
                            Text(h.type ?? "Helper").font(Theme.font(12, .bold)).foregroundStyle(loud ? Theme.needs : own ?? Theme.ink)
                            if let d = h.detail, !d.isEmpty {
                                Text(d).font(Theme.font(12, loud ? .semibold : .regular)).foregroundStyle(loud ? Theme.needs : own ?? Theme.muted)
                            }
                        }
                        .lineLimit(1)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(shown ? Theme.selection : .clear))
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help([h.type, h.detail, h.command].compactMap { $0 }.joined(separator: "\n"))
                    .accessibilityHint("Shows the helper beside the conversation")
                }
                if helpers.count > 3 {
                    Text("and \(helpers.count - 3) more helpers").font(Theme.font(12)).foregroundStyle(own ?? Theme.muted)
                }
                // Its teammates, whose terminals stand beside its own.
                ForEach(team.prefix(3)) { m in
                    Button { store.showTeam(session.id) } label: {
                        HStack(spacing: 5) {
                            Text("⊞").font(Theme.font(12)).foregroundStyle(own ?? Theme.muted)
                            Text(m.name).font(Theme.font(12, .bold)).foregroundStyle(own ?? Theme.ink)
                            Text(m.dead == true ? (m.exitCode ?? 0) == 0 ? "Done" : "Failed" : m.working == true ? m.title ?? "Working" : "Waiting")
                                .font(Theme.font(12)).foregroundStyle(own ?? Theme.muted)
                        }
                        .lineLimit(1)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help([m.name, m.type, m.title].compactMap { $0 }.joined(separator: "\n"))
                    .accessibilityHint("Shows the session's terminal beside its teammates'")
                }
                if team.count > 3 {
                    Text("and \(team.count - 3) more teammates").font(Theme.font(12)).foregroundStyle(own ?? Theme.muted)
                }
            }
            // Under the card's name: its inset, its mark and the gap, less
            // each line's own inset.
            .padding(.leading, 28)
            .padding(.trailing, Theme.inset)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .contain)
        }
    }
}

/// The switch between the List and Repo views, above the details.
private struct ModeSwitch: View {
    @EnvironmentObject private var store: Store

    var body: some View {
        HStack(spacing: 2) {
            ForEach(SidebarMode.allCases, id: \.self) { m in
                let on = store.mode == m
                Button { store.choose(m) } label: {
                    Text(m.title)
                        .font(Theme.font(12, .bold))
                        .foregroundStyle(on ? Theme.ink : Theme.muted)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 3)
                        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(on ? Theme.card : Color.clear)
                            .shadow(color: on ? Theme.shadow : Color.clear, radius: 1.5, y: 0.5))
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help(m == .list ? "One list, in your order: only you move a card" : "Group the sessions by repo, with their helpers")
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .padding(2)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.panel))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        .frame(maxWidth: 190)
        // As far from the footer's top as the task bar is from its foot.
        .padding(.top, 10)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Group the sessions")
    }
}

/// What a hovered card offers. They sit in the card's fixed trailing slot,
/// three icons 16 points wide and the menu 18, so the name keeps its room;
/// VoiceOver gets them, and more, as the card's actions.
private struct QuickActions: View {
    @EnvironmentObject private var store: Store
    let session: Session
    /// All in ink, over a selected working card's deep end.
    let ink: Bool

    var body: some View {
        HStack(spacing: 0) {
            // A click on the card types to it; the folder is in the menu.
            PauseButton(session: session)
            PromoteButton(session: session, ink: ink)
            if store.canPark { ParkButton(session: session, onCard: true) }
            Menu {
                Actions(session: session)
            } label: {
                Image(systemName: "ellipsis")
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .frame(width: 18)
            .help("More")
        }
        .buttonStyle(.borderless)
        .font(.system(size: 11, weight: .semibold))
        .foregroundStyle(ink ? Theme.ink : Theme.muted)
        .accessibilityHidden(true)
    }
}

/// The selected session's details, on a card above the task bar, edged in
/// its state's colour, or its agent's while it works: long text is cut off,
/// and the full text is on hover.
/// It opens small, the state and one line, and shows as many whole lines as
/// fit. Its top edge drags it taller, as far as leaves the
/// cards their room, or shorter. The chevron, a double click on the edge or
/// a drag to the bottom minimizes it to one line of what the agent is
/// doing. The app keeps its height and whether it is minimized.
struct DetailsPanel: View {
    @EnvironmentObject private var store: Store
    /// Its height while open, in points.
    @AppStorage(DetailsPanel.heightKey) private var height = DetailsPanel.compact
    @AppStorage(DetailsPanel.minimizedKey) private var minimized = false
    /// Its height as drawn: less than `height` while the cards need the room.
    @State private var drawn: CGFloat = 0
    /// While the edge is dragged: the height drawn, and the height kept, as
    /// the drag began.
    @State private var dragFrom: (drawn: CGFloat, kept: Double)?
    @State private var overEdge = false

    static let heightKey = "detailsHeight"
    static let minimizedKey = "detailsMinimized"
    /// Its height until the edge is dragged: the state and one line, about a
    /// sixth of the half of the sidebar it took before it could be dragged.
    static let compact: Double = 60
    /// The least it opens to, the state and one line. A drag below `fold`
    /// minimizes it.
    private static let least: CGFloat = 58
    private static let fold: CGFloat = 44

    private var selected: Session? { store.sessions.first { $0.id == store.selection } }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
        let outside = store.selectedOutside
        // Its state's colour, or its agent's while it works, as the
        // terminal's frame.
        let stateColor = outside == nil ? selected.flatMap { Look($0).color ?? Look.agentTint($0) } : nil
        Group {
            if minimized {
                summary(outside)
            } else if let o = outside {
                OutsideDetails(outside: o)
            } else if let s = selected {
                details(s)
            } else {
                HStack(alignment: .top, spacing: 6) {
                    Text("Pick a session to see its details.")
                        .font(Theme.font(12))
                        .foregroundStyle(Theme.muted)
                    Spacer(minLength: 4)
                    DetailsFold()
                }
                .controlSize(.small)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            }
        }
        .padding(.horizontal, Theme.inset)
        .padding(.vertical, minimized ? 6 : 9)
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: minimized ? nil : CGFloat(height), alignment: .top)
        .background(GeometryReader { g in
            Color.clear
                .onAppear { drawn = g.size.height }
                .onChange(of: g.size.height) { drawn = g.size.height }
        })
        .background(shape.fill(Theme.panel))
        .overlay(shape.strokeBorder(stateColor?.opacity(0.45) ?? Theme.edge, lineWidth: 1))
        .clipShape(shape)
        .background(shape.fill(Theme.panel).shadow(color: Theme.shadow, radius: 3, y: 1.5))
        .overlay(alignment: .top) { edge }
        .padding(.horizontal, 10)
        .padding(.top, 6)
    }

    /// The top edge, a strip across the border: a drag makes the panel
    /// taller or shorter, and a double click minimizes it or brings it
    /// back. Its grip shows under the pointer.
    private var edge: some View {
        Capsule()
            .fill(Theme.muted.opacity(overEdge || dragFrom != nil ? 0.55 : 0))
            .frame(width: 32, height: 3)
            .padding(.bottom, 1)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottom)
            .contentShape(Rectangle())
            .frame(height: 10)
            .offset(y: -4)
            .onHover { inside in
                overEdge = inside
                if inside { NSCursor.resizeUpDown.push() } else { NSCursor.pop() }
            }
            .onDisappear { if overEdge { NSCursor.pop() } }
            .onTapGesture(count: 2) { withAnimation(.easeOut(duration: 0.15)) { minimized.toggle() } }
            // In the window's space, since the edge moves with the drag.
            .gesture(DragGesture(minimumDistance: 1, coordinateSpace: .global)
                .onChanged { drag(by: $0.translation.height) }
                .onEnded { _ in endDrag() })
            .accessibilityElement()
            .accessibilityLabel("Details height")
            .accessibilityValue(minimized ? "Minimized" : "\(Int(drawn)) points")
            .accessibilityAdjustableAction { direction in
                switch direction {
                case .increment: step(40)
                case .decrement: step(-40)
                @unknown default: break
                }
            }
    }

    private func drag(by dy: CGFloat) {
        let from = dragFrom ?? (drawn: drawn, kept: height)
        dragFrom = from
        let want = from.drawn - dy
        if want < Self.fold {
            minimized = true
        } else {
            minimized = false
            height = Double(max(want, Self.least))
        }
    }

    /// Keeps no more than the height drawn, which the cards' room may have
    /// cut short, or the one it had when a drag minimized it.
    private func endDrag() {
        guard let from = dragFrom else { return }
        height = minimized ? from.kept : min(height, Double(drawn))
        dragFrom = nil
    }

    /// VoiceOver's steps: taller or shorter, minimized below the least.
    private func step(_ by: CGFloat) {
        if minimized {
            if by > 0 { minimized = false }
        } else if drawn + by < Self.least {
            minimized = true
        } else {
            height = Double(drawn + by)
        }
    }

    /// The minimized panel's line: what the session needs from you, or the
    /// call its agent is making, or what it is doing.
    private func summary(_ outside: OutsideSession?) -> some View {
        HStack(spacing: 6) {
            if let o = outside {
                StateMark(look: Look(outside: o))
                line(o.doing, color: Theme.ink)
            } else if let s = selected {
                let look = Look(s)
                let own = Look.agentInk(s)
                StateMark(look: look, tint: Look.agentTint(s))
                if look.loud {
                    line(s.detail ?? look.label, color: Theme.needs)
                } else if let c = s.command, !c.isEmpty {
                    line(c, mono: true, color: own ?? Theme.ink)
                } else {
                    line(s.detail ?? Look.label(s), color: own ?? Theme.ink)
                }
            } else {
                line("No session picked")
            }
            Spacer(minLength: 4)
            DetailsFold()
        }
        .controlSize(.small)
    }

    /// While the session works, its state and every line about it but a
    /// warning take its agent's colour, as its card does.
    private func details(_ s: Session) -> some View {
        let look = Look(s)
        let own = Look.agentInk(s)
        let meta = own ?? Theme.muted
        return WholeLines(spacing: 4) {
            HStack(spacing: 6) {
                StateMark(look: look, tint: Look.agentTint(s))
                Text(Look.label(s))
                    .font(Theme.font(12.5, .bold))
                    .foregroundStyle(look.loud ? Theme.needs : own ?? Theme.ink)
                Spacer(minLength: 4)
                // Icons, so a narrow sidebar never cuts their words short; the
                // words are on hover and for VoiceOver.
                Button { store.show(s.id, focus: true) } label: { Image(systemName: "keyboard") }
                    .help("Type to it")
                    .accessibilityLabel("Type to it")
                if let cwd = s.cwd {
                    Button { store.reveal(cwd) } label: { Image(systemName: "folder") }
                        .help("Show the folder in Finder")
                        .accessibilityLabel("Show the folder in Finder")
                }
                if store.canPark { ParkButton(session: s) }
                DetailsFold()
            }
            .controlSize(.small)
            line(s.detail, color: look.loud ? Theme.needs : own ?? Theme.ink)
            ControlLines(session: s)
            line(s.command, mono: true, color: meta)
            line([s.profile, s.branch].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "), mono: true, color: meta)
            line(s.cwd.map(Age.tilde), mono: true, color: meta)
            line(s.tokens?.breakdown, color: meta)
            ForEach((s.subagents ?? []).prefix(3)) { h in
                line("↳ " + [h.type ?? "Helper", h.detail].compactMap { $0 }.joined(separator: " · "),
                     color: h.needsYou == true ? Theme.needs : meta)
            }
            if let extra = s.subagents.map({ $0.count - 3 }), extra > 0 {
                line("and \(extra) more helpers", color: meta)
            }
            ForEach((s.warnings ?? []).prefix(2), id: \.self) { w in
                line("⚠︎ " + w, color: Theme.sunInk)
            }
            ForEach((s.tail ?? []).suffix(2), id: \.self) { t in
                line(t, mono: true)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    /// One line of the panel, cut off with its full text on hover.
    @ViewBuilder private func line(_ text: String?, mono: Bool = false, color: Color = Theme.muted) -> some View {
        if let text, !text.isEmpty {
            Text(text)
                .font(.system(size: mono ? 10.5 : 12, design: mono ? .monospaced : .rounded))
                .foregroundStyle(color)
                .lineLimit(1)
                .truncationMode(.middle)
                .help(text)
        }
    }
}

/// The details panel's lines, down from the top, as many as fit whole: the
/// first that would be cut off, and every line after it, is left out, so
/// the panel never shows half a line, however tall it is.
struct WholeLines: Layout {
    var spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? subviews.map { $0.sizeThatFits(.unspecified).width }.max() ?? 0
        let natural = heights(subviews, width).reduce(0, +) + spacing * CGFloat(max(subviews.count - 1, 0))
        return CGSize(width: width, height: min(natural, proposal.height ?? natural))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        var fits = true
        for (view, height) in zip(subviews, heights(subviews, bounds.width)) {
            fits = fits && y + height <= bounds.maxY + 0.5
            // A line left out goes far below, where the panel's clip hides it.
            view.place(at: CGPoint(x: bounds.minX, y: fits ? y : bounds.maxY + 1000),
                       proposal: ProposedViewSize(width: bounds.width, height: height))
            y += height + spacing
        }
    }

    private func heights(_ subviews: Subviews, _ width: CGFloat) -> [CGFloat] {
        subviews.map { $0.sizeThatFits(ProposedViewSize(width: width, height: nil)).height }
    }
}

/// The details panel's chevron, at the end of its first line: it minimizes
/// the panel to one line, or brings it back.
struct DetailsFold: View {
    @AppStorage(DetailsPanel.minimizedKey) private var minimized = false

    var body: some View {
        Button { withAnimation(.easeOut(duration: 0.15)) { minimized.toggle() } } label: {
            Image(systemName: minimized ? "chevron.up" : "chevron.down")
        }
        .help(minimized ? "Show the details" : "Minimize the details")
        .accessibilityLabel(minimized ? "Show the details" : "Minimize the details")
    }
}

/// The right-click menu.
private struct Actions: View {
    @EnvironmentObject private var store: Store
    let session: Session

    var body: some View {
        Button("Show and Type to It") { store.show(session.id, focus: true) }
        Button("Open in Ghostty") {
            store.show(session.id)
            store.openInGhostty()
        }
        Button("Rename…") { store.beginRename(session.id) }
        Divider()
        ControlItems(session: session)
        Divider()
        if let cwd = session.cwd {
            Button("Show Folder in Finder") { store.reveal(cwd) }
            Button("Copy Folder Path") { store.copy(cwd, what: "folder path") }
        }
        if let branch = session.branch, !branch.isEmpty {
            Button("Copy Branch Name") { store.copy(branch, what: "branch name") }
        }
        Button("Copy Session ID") { store.copy(session.id, what: "session ID") }
        Divider()
        Button("Close Session") { store.close(session) }
    }
}

/// What the sidebar puts first in the title bar, beside the traffic lights
/// and above the find field: the sun over the waves and the name Irie.
private struct TitleName: View {
    var body: some View {
        HStack(spacing: 3) {
            SunMark().frame(width: 14, height: 14)
            Text("Irie").font(.system(size: 13, weight: .heavy, design: .rounded))
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Irie")
        .accessibilityAddTraits(.isHeader)
        // The gap to the counts, which are an item of their own.
        .padding(.trailing, 6)
        .fixedSize()
    }
}

/// A count per state, in the title bar after Irie's name. A count shows the
/// first session, in list order, in that state. A parked session is out of
/// the way, so To verify leaves it out; it still counts when it needs you.
private struct Counts: View {
    @EnvironmentObject private var store: Store

    var body: some View {
        HStack(spacing: 6) {
            switch store.link {
            case .online:
                count([.failed, .needs, .stalled], mark: .needs, color: Theme.needs, label: "need you")
                count([.working], mark: .working, color: Theme.ink, label: "working")
                if !store.closeWhenDone {
                    count([.done], mark: .done, color: Look.doneColor, label: "to verify, not yet seen", parked: false)
                }
                if store.sessions.contains(where: { [.paused, .pausing].contains(Look($0)) }) {
                    count([.paused, .pausing], mark: .paused, color: Theme.paused, label: "paused")
                }
                if store.sessions.contains(where: { Look($0) == .frozen }) {
                    count([.frozen], mark: .frozen, color: Theme.frozen, label: "frozen")
                }
                if let t = store.trod {
                    trod(t)
                }
            case .offline:
                Text("offline").font(Theme.font(11, .bold)).foregroundStyle(Theme.needs)
            case .connecting:
                EmptyView()
            }
        }
        // The title bar sizes the item to its content; a point either side
        // keeps the capsules' edges whole.
        .padding(.horizontal, 1)
        .fixedSize()
    }

    /// The trod bot within its hours (DESIGN §4.10): a moon, in the accent
    /// while it steps in, muted while you are at the Mac or the plan's use
    /// stops it.
    private func trod(_ t: TrodShift) -> some View {
        let works = (t.away ?? false) && !(t.capped ?? false)
        var words = "Trod bot"
        if let u = t.until { words += " until " + u.formatted(date: .omitted, time: .shortened) }
        switch (t.capped ?? false, t.away ?? false) {
        case (true, _): words += ": your plan is as full as it may get, so it sends nothing on"
        case (false, true): words += ": you’re away, so it keeps your sessions going"
        default: words += ": it steps in once you’ve been away for a while"
        }
        if t.awake ?? false { words += ", and keeps the Mac awake" }
        let color = works ? Theme.accent : Theme.muted
        return Image(systemName: "moon.stars")
            .font(Theme.font(10, .bold))
            .foregroundStyle(color)
            .padding(.horizontal, 5)
            .padding(.vertical, 1.5)
            .background(Capsule().fill(color.opacity(0.12)))
            .overlay(Capsule().strokeBorder(color.opacity(0.4), lineWidth: 1))
            .help(words)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(words)
    }

    /// A count of the sessions in these states, with the first state's mark,
    /// which shows the first such session; without parked, the parked ones
    /// are left out. It is drawn as a chip rather than a button, so the title
    /// bar puts no bezel round it; VoiceOver still finds a button.
    private func count(_ looks: [Look], mark: Look, color: Color, label: String, parked: Bool = true) -> some View {
        let rows = store.sessions.filter { looks.contains(Look($0)) && (parked || !$0.isParked) }
        let go = { if let first = rows.first { store.show(first.id) } }
        return HStack(spacing: 3) {
            StateMark(look: mark, size: 10)
            // Two digits' width, so a tenth session doesn't push the others.
            Text("\(rows.count)").font(Theme.font(10.5, .bold)).monospacedDigit()
                .frame(minWidth: 12)
        }
        .foregroundStyle(color)
        .padding(.horizontal, 4)
        .padding(.vertical, 1.5)
        .background(Capsule().fill(color.opacity(rows.isEmpty ? 0.05 : 0.12)))
        .overlay(Capsule().strokeBorder(color.opacity(rows.isEmpty ? 0.15 : 0.4), lineWidth: 1))
        .opacity(rows.isEmpty ? 0.5 : 1)
        .contentShape(Capsule())
        .onTapGesture { go() }
        .help("\(rows.count) \(label)")
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(rows.count) \(label)")
        .accessibilityAddTraits(.isButton)
        .accessibilityAction { go() }
    }
}

/// "Find a session". ⌘F comes here; Esc clears it and goes back to the list.
private struct FindField: View {
    @EnvironmentObject private var store: Store
    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 11))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
            TextField("Find a session", text: $store.filter)
                .textFieldStyle(.plain)
                .font(Theme.font(12.5))
                .focused($focused)
                .onExitCommand {
                    store.filter = ""
                    store.focusSidebar += 1
                }
            Button { store.filter = "" } label: { Image(systemName: "xmark.circle.fill") }
                .buttonStyle(.plain)
                .foregroundStyle(Theme.muted)
                .opacity(store.filter.isEmpty ? 0 : 1)
                .disabled(store.filter.isEmpty)
                .help("Clear")
                .accessibilityLabel("Clear the search")
        }
        .padding(.horizontal, Theme.inset)
        .padding(.vertical, 4)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field)
            .shadow(color: Theme.shadow, radius: 2, y: 1))
        .overlay(
            RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                .strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: focused ? 1.5 : 1)
        )
        .padding(10)
        .onChange(of: store.focusFind) { focused = true }
    }
}

private struct Offline: View {
    let why: String
    let socket: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(why).font(Theme.font(13.5, .bold)).foregroundStyle(Theme.ink)
            Text("Start it in a terminal with `irie up`. The app connects by itself once it’s running.")
                .font(.system(size: 12)).foregroundStyle(Theme.muted)
            Text(Age.tilde(socket)).font(.system(size: 10.5, design: .monospaced)).foregroundStyle(Theme.muted)
                .lineLimit(1).truncationMode(.middle)
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

/// What an empty Irie says: its mark, its word, and where to start.
private struct Welcome: View {
    var body: some View {
        VStack(spacing: 10) {
            SunMark().frame(width: 56, height: 56)
            Text("Everything irie.").font(Theme.font(18, .bold)).foregroundStyle(Theme.ink)
            Text("No agents yet. Type a task below to start one.")
                .font(.system(size: 12.5)).foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .combine)
    }
}

private struct Hint: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.system(size: 12.5)).foregroundStyle(Theme.muted)
            .padding(14)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

enum Age {
    static func short(_ d: Date, now: Date) -> String {
        let s = max(0, Int(now.timeIntervalSince(d)))
        switch s {
        case ..<60: return "now"
        case ..<3600: return "\(s / 60)m"
        case ..<86400: return "\(s / 3600)h"
        default: return "\(s / 86400)d"
        }
    }

    /// "3 minutes ago", for VoiceOver: "3m" would be read as metres.
    static func spoken(_ d: Date, now: Date) -> String {
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .full
        return f.localizedString(for: d, relativeTo: now)
    }

    static func tilde(_ path: String) -> String {
        let home = NSHomeDirectory()
        return path.hasPrefix(home + "/") ? "~" + path.dropFirst(home.count) : path
    }
}
