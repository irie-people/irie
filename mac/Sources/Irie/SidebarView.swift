import SwiftUI

/// The sidebar: a card per session, in groups. The Repo view is a tree, a
/// group per repo with its sessions in the order they started and each
/// session's helpers under it, so a state change only changes a card's mark
/// and colours, never the order (DESIGN §4.1). The State view groups the
/// sessions by what they need. A click shows that agent's terminal and gives
/// it the keyboard; the arrow keys walk the cards and keep the keyboard
/// here. The switch between the views, and the selected session's details,
/// sit above the task bar.
struct SidebarView: View {
    @EnvironmentObject private var store: Store
    @FocusState private var listFocused: Bool

    var body: some View {
        // One row of its own above the cards: Irie's mark, its name and the
        // counts sit in the title bar, beside the traffic lights.
        VStack(spacing: 0) {
            FindField()
            content
            if !store.sessions.isEmpty {
                ModeSwitch()
            }
            DetailsPanel()
            // The footer sits lighter than the list, with rounded top corners.
            TaskBar()
                .background(UnevenRoundedRectangle(topLeadingRadius: Theme.frameRadius, topTrailingRadius: Theme.frameRadius, style: .continuous)
                    .fill(Theme.card).shadow(color: Theme.shadow, radius: 3, y: -1))
        }
        .background(Theme.sidebar.ignoresSafeArea())
        .toolbar {
            ToolbarItem(placement: .automatic) { TitleBar() }
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
                                GroupHeader(group: g)
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
            .onKeyPress(.upArrow) {
                store.walk(-1)
                return .handled
            }
            .onKeyPress(.downArrow) {
                store.walk(1)
                return .handled
            }
            .onKeyPress(.return) {
                if let o = store.selectedOutside {
                    store.explainOutside(o)
                    return .handled
                }
                guard let id = store.selection else { return .ignored }
                store.show(id, focus: true)
                return .handled
            }
            .accessibilityLabel("Sessions")
        }
    }
}

/// One session, always two lines: its name, then what it needs from you (in
/// red), what it is doing, or how it ended, in plain words. The trailing
/// slot has a fixed width, so the name never re-wraps when the age ticks or
/// the pointer brings up the actions. The card is soft and rounded, on a
/// gentle shadow: tinted red while it needs you, edged in the accent while
/// selected.
private struct SessionCard: View {
    @EnvironmentObject private var store: Store
    let session: Session
    let selected: Bool
    let listFocused: Bool
    let now: Date
    @State private var hovering = false

    private var look: Look { Look(session) }

    var body: some View {
        HStack(alignment: .top, spacing: 9) {
            StateMark(look: look).padding(.top, 2)
            VStack(alignment: .leading, spacing: 2) {
                Text(session.name)
                    .font(Theme.font(13.5, look.quiet ? .semibold : .bold))
                    .foregroundStyle(look.quiet ? Theme.muted : Theme.ink)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Text(status)
                    .font(Theme.font(12, look.loud ? .semibold : .regular))
                    .foregroundStyle(look.loud ? Theme.needs : Theme.muted)
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
                .foregroundStyle(Theme.muted)
                .opacity(hovering ? 0 : 1)
                QuickActions(session: session)
                    .opacity(hovering ? 1 : 0)
                    .allowsHitTesting(hovering)
            }
            .frame(width: 66, alignment: .trailing)
        }
        .padding(.vertical, 8)
        .padding(.leading, 11)
        .padding(.trailing, 7)
        .background(shape.fill(fill).shadow(color: Theme.shadow, radius: 3, y: 1.5))
        .overlay(shape.strokeBorder(edge, lineWidth: selected ? 1.5 : 1))
        .contentShape(shape)
        .onHover { hovering = $0 }
        .onTapGesture { store.show(session.id, focus: true) }
        .contextMenu { Actions(session: session) }
        .help([session.name, session.detail, session.command].compactMap { $0 }.joined(separator: "\n"))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction { store.show(session.id, focus: true) }
        .accessibilityActions {
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

    /// The second line.
    private var status: String {
        switch look {
        case .starting: return "Starting…"
        case .done, .idle, .frozen, .paused, .pausing: return look.label
        default: return session.detail ?? look.label
        }
    }

    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: Theme.radius, style: .continuous) }

    private var fill: Color {
        if selected { return listFocused ? Theme.selection : Theme.selection.opacity(0.55) }
        if look.loud { return Theme.needsTint }
        return hovering ? Theme.hover : Theme.card
    }

    /// The accent round the selected card, faint while the list lacks the
    /// keyboard; red round one that needs you; a hairline round the rest.
    private var edge: Color {
        if selected { return listFocused ? Theme.accent : Theme.accent.opacity(0.5) }
        if hovering { return Theme.accent.opacity(0.45) }
        return look.loud ? Theme.needs.opacity(0.5) : Theme.edge
    }

    private var spoken: String {
        var parts = [session.name, look.label]
        if let d = session.detail, !d.isEmpty { parts.append(d) }
        parts.append("started " + Age.spoken(session.created, now: now))
        if let t = session.tokens { parts.append("\(t.total) tokens") }
        return parts.joined(separator: ", ")
    }
}

/// A group's header: its name and count, and a chevron that folds it. A
/// folded group says how many of its sessions need you, in red.
private struct GroupHeader: View {
    @EnvironmentObject private var store: Store
    let group: SessionGroup

    var body: some View {
        let open = store.isOpen(group)
        let needs = group.sessions.filter { Look($0).loud }.count
        let note = !open && needs > 0 && group.id != "state:needs" ? "\(needs) \(needs == 1 ? "needs" : "need") you" : nil
        Button { store.fold(group) } label: {
            HStack(spacing: 5) {
                Image(systemName: "chevron.right")
                    .font(.system(size: 8.5, weight: .heavy))
                    .rotationEffect(.degrees(open ? 90 : 0))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 10)
                Text(group.title)
                    .font(Theme.font(12, .heavy))
                    .foregroundStyle(group.id == "state:needs" ? Theme.needs : Theme.ink)
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
        .help(group.path.map(Age.tilde) ?? group.title)
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
    let session: Session

    var body: some View {
        let helpers = session.subagents ?? []
        if !helpers.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(helpers.prefix(3)) { h in
                    let loud = h.needsYou == true
                    HStack(spacing: 5) {
                        Text("↳").font(Theme.font(12)).foregroundStyle(Theme.muted)
                        Text(h.type ?? "Helper").font(Theme.font(12, .bold)).foregroundStyle(loud ? Theme.needs : Theme.ink)
                        if let d = h.detail, !d.isEmpty {
                            Text(d).font(Theme.font(12, loud ? .semibold : .regular)).foregroundStyle(loud ? Theme.needs : Theme.muted)
                        }
                    }
                    .lineLimit(1)
                    .help([h.type, h.detail, h.command].compactMap { $0 }.joined(separator: "\n"))
                }
                if helpers.count > 3 {
                    Text("and \(helpers.count - 3) more helpers").font(Theme.font(12)).foregroundStyle(Theme.muted)
                }
            }
            .padding(.leading, 34)
            .padding(.trailing, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
        }
    }
}

/// The switch between the Repo and State views, above the details.
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
                .help(m == .state ? "Group the sessions by what they need" : "Group the sessions by repo, with their helpers")
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .padding(2)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.panel))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        .frame(maxWidth: 190)
        .padding(.top, 5)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Group the sessions")
    }
}

/// What a hovered card offers. They sit in the card's fixed trailing slot;
/// VoiceOver gets them, and more, as the card's actions.
private struct QuickActions: View {
    @EnvironmentObject private var store: Store
    let session: Session

    var body: some View {
        HStack(spacing: 0) {
            Button { store.show(session.id, focus: true) } label: { Image(systemName: "keyboard").frame(width: 20, height: 18) }
                .help("Type to it")
            Button { if let cwd = session.cwd { store.reveal(cwd) } } label: { Image(systemName: "folder").frame(width: 20, height: 18) }
                .help("Show the folder in Finder")
                .disabled(session.cwd == nil)
            Menu {
                Actions(session: session)
            } label: {
                Image(systemName: "ellipsis")
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .frame(width: 22)
            .help("More")
        }
        .buttonStyle(.borderless)
        .font(.system(size: 11, weight: .semibold))
        .foregroundStyle(Theme.muted)
        .accessibilityHidden(true)
    }
}

/// The selected session's details, on a card of fixed height above the task
/// bar, edged in its state's colour: long text is cut off, and the full text
/// is on hover.
private struct DetailsPanel: View {
    @EnvironmentObject private var store: Store
    static let height: CGFloat = 150

    private var selected: Session? { store.sessions.first { $0.id == store.selection } }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
        let outside = store.selectedOutside
        let stateColor = outside == nil ? selected.flatMap { Look($0).color } : nil
        Group {
            if let o = outside {
                OutsideDetails(outside: o)
            } else if let s = selected {
                details(s)
            } else {
                Text("Pick a session to see its details.")
                    .font(Theme.font(12))
                    .foregroundStyle(Theme.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(shape.fill(Theme.panel))
        .overlay(shape.strokeBorder(stateColor?.opacity(0.45) ?? Theme.edge, lineWidth: 1))
        .clipShape(shape)
        .background(shape.fill(Theme.panel).shadow(color: Theme.shadow, radius: 3, y: 1.5))
        .padding(.horizontal, 10)
        .padding(.top, 6)
        .frame(height: Self.height)
    }

    private func details(_ s: Session) -> some View {
        let look = Look(s)
        return VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                StateMark(look: look)
                Text(look.label)
                    .font(Theme.font(12.5, .bold))
                    .foregroundStyle(look.loud ? Theme.needs : Theme.ink)
                Spacer(minLength: 4)
                Button("Type to it") { store.show(s.id, focus: true) }
                if let cwd = s.cwd {
                    Button("Show folder") { store.reveal(cwd) }
                }
            }
            .controlSize(.small)
            line(s.detail, color: look.loud ? Theme.needs : Theme.ink)
            line(s.command, mono: true)
            line([s.profile, s.branch].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "), mono: true)
            line(s.cwd.map(Age.tilde), mono: true)
            line(s.tokens?.breakdown)
            ForEach((s.subagents ?? []).prefix(3)) { h in
                line("↳ " + [h.type ?? "Helper", h.detail].compactMap { $0 }.joined(separator: " · "),
                     color: h.needsYou == true ? Theme.needs : Theme.muted)
            }
            if let extra = s.subagents.map({ $0.count - 3 }), extra > 0 {
                line("and \(extra) more helpers")
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

/// A count per state, in the title bar above the sidebar. A count shows the
/// first session, in list order, in that state.
/// What the sidebar puts in the title bar: the sun over the waves, the name
/// Irie and the counts, in one compact row beside the traffic lights.
private struct TitleBar: View {
    var body: some View {
        HStack(spacing: 6) {
            HStack(spacing: 3) {
                SunMark().frame(width: 14, height: 14)
                Text("Irie").font(.system(size: 13, weight: .heavy, design: .rounded))
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Irie")
            .accessibilityAddTraits(.isHeader)
            Counts()
        }
        // The title bar sizes the item to its content; a point either side
        // keeps the capsules' edges whole.
        .padding(.horizontal, 1)
        .fixedSize()
    }
}

private struct Counts: View {
    @EnvironmentObject private var store: Store

    var body: some View {
        HStack(spacing: 2) {
            switch store.link {
            case .online:
                count([.failed, .needs, .stalled], mark: .needs, color: Theme.needs, label: "need you")
                count([.working], mark: .working, color: Theme.ink, label: "working")
                count([.done], mark: .done, color: Theme.done, label: "done, not yet seen")
                if store.sessions.contains(where: { [.paused, .pausing].contains(Look($0)) }) {
                    count([.paused, .pausing], mark: .paused, color: Theme.paused, label: "paused")
                }
                if store.sessions.contains(where: { Look($0) == .frozen }) {
                    count([.frozen], mark: .frozen, color: Theme.frozen, label: "frozen")
                }
            case .offline:
                Text("offline").font(Theme.font(11, .bold)).foregroundStyle(Theme.needs)
            case .connecting:
                EmptyView()
            }
        }
    }

    /// A count of the sessions in these states, with the first state's mark,
    /// which shows the first such session. It is drawn as a chip rather than
    /// a button, so the title bar puts no bezel round it; VoiceOver still
    /// finds a button.
    private func count(_ looks: [Look], mark: Look, color: Color, label: String) -> some View {
        let rows = store.sessions.filter { looks.contains(Look($0)) }
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
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field)
            .shadow(color: Theme.shadow, radius: 2, y: 1))
        .overlay(
            RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                .strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: focused ? 1.5 : 1)
        )
        .padding(.horizontal, 10)
        .padding(.top, 2)
        .padding(.bottom, 4)
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
