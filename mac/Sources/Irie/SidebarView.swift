import SwiftUI

/// The sidebar: a card per session, in the order they started. A state
/// change only changes a card's mark and colours, never the order or a
/// card's height (DESIGN §4.1), so nothing moves under the pointer. A click
/// shows that agent's terminal and gives it the keyboard; the arrow keys
/// walk the cards and keep the keyboard here. The selected session's
/// details sit in a fixed panel above the task bar.
struct SidebarView: View {
    @EnvironmentObject private var store: Store
    @FocusState private var listFocused: Bool

    var body: some View {
        VStack(spacing: 0) {
            Header()
            FindField()
            content
            DetailsPanel()
            TaskBar()
        }
        .background(Theme.sidebar.ignoresSafeArea())
    }

    @ViewBuilder private var content: some View {
        let rows = store.drawnOrder
        if case let .offline(why) = store.link, store.sessions.isEmpty {
            Offline(why: why, socket: store.socketPath)
        } else if store.sessions.isEmpty {
            if store.link == .connecting {
                Hint(text: "Connecting to Irie…")
            } else {
                Welcome()
            }
        } else if rows.isEmpty {
            Hint(text: "No sessions match “\(store.filter)”.")
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    // One clock for every card's age.
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        LazyVStack(alignment: .leading, spacing: 6) {
                            ForEach(rows) { s in
                                SessionCard(session: s, selected: s.id == store.selection, listFocused: listFocused, now: ctx.date)
                                    .id(s.id)
                            }
                        }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 8)
                    }
                }
                .onChange(of: store.selection) {
                    if let id = store.selection { proxy.scrollTo(id) }
                }
            }
            .focusable()
            .focusEffectDisabled()
            .focused($listFocused)
            .onChange(of: store.focusSidebar) { listFocused = true }
            .onKeyPress(.upArrow) {
                store.step(-1, wrap: false)
                return .handled
            }
            .onKeyPress(.downArrow) {
                store.step(1, wrap: false)
                return .handled
            }
            .onKeyPress(.return) {
                guard let id = store.selection else { return .ignored }
                store.show(id, focus: true)
                return .handled
            }
            .accessibilityLabel("Sessions")
        }
    }
}

/// One session, always two lines: its name, then what it needs from you (in
/// red), what it is doing, or how it ended. The trailing slot has a fixed
/// width, so the name never re-wraps when the age ticks or the pointer
/// brings up the actions. The card is rounded and edged in its state's
/// colour, or in sun while selected.
private struct SessionCard: View {
    @EnvironmentObject private var store: Store
    let session: Session
    let selected: Bool
    let listFocused: Bool
    let now: Date
    @State private var hovering = false

    private var look: Look { Look(session) }
    private var quiet: Bool { look == .done || look == .idle || look == .ended }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            StateMark(look: look).padding(.top, 2)
            VStack(alignment: .leading, spacing: 2) {
                Text(session.name)
                    .font(.system(size: 13, weight: quiet ? .regular : .semibold, design: .rounded))
                    .foregroundStyle(look == .ended || look == .idle ? Theme.muted : Color.primary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Text(status)
                    .font(.system(size: 11.5, weight: look == .needs ? .medium : .regular))
                    .foregroundStyle(look == .needs ? Theme.needs : Theme.muted)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            ZStack(alignment: .topTrailing) {
                VStack(alignment: .trailing, spacing: 2) {
                    Text(Age.short(session.created, now: now))
                    Text(session.tokens.map { Tokens.short($0.total) } ?? " ")
                }
                .font(.system(size: 11)).monospacedDigit()
                .foregroundStyle(Theme.muted)
                .opacity(hovering ? 0 : 1)
                QuickActions(session: session)
                    .opacity(hovering ? 1 : 0)
                    .allowsHitTesting(hovering)
            }
            .frame(width: 66, alignment: .trailing)
        }
        .padding(.vertical, 6)
        .padding(.leading, 10)
        .padding(.trailing, 6)
        .background(shape.fill(fill))
        .overlay(shape.strokeBorder(edge, lineWidth: selected ? 1.5 : 1))
        .contentShape(shape)
        .onHover { hovering = $0 }
        .onTapGesture { store.show(session.id, focus: true) }
        .contextMenu { Actions(session: session) }
        .help([session.name, session.detail].compactMap { $0 }.joined(separator: "\n"))
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
        }
    }

    /// The second line.
    private var status: String {
        let helpers = session.subagents.map { $0.count == 1 ? "1 helper" : "\($0.count) helpers" }
        let what: String
        switch look {
        case .needs: what = session.detail ?? "Needs you"
        case .working: what = session.detail ?? "Working"
        case .starting: what = "Starting…"
        case .done: what = "Done"
        case .idle: what = "Idle"
        case .ended: what = session.detail ?? "Ended"
        }
        return [helpers, what].compactMap { $0 }.joined(separator: " · ")
    }

    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: Theme.radius, style: .continuous) }

    private var fill: Color {
        if selected { return listFocused ? Theme.selection : Theme.selection.opacity(0.45) }
        if look == .needs { return Theme.needsTint }
        return hovering ? Theme.hover : Theme.card
    }

    /// Sun round the selected card, faint while the list lacks the keyboard,
    /// and its state's colour round the rest.
    private var edge: Color {
        if selected { return listFocused ? Theme.sunEdge : Theme.sunEdge.opacity(0.5) }
        if hovering { return Theme.sunEdge.opacity(0.5) }
        return look.color?.opacity(look == .needs ? 0.6 : 0.35) ?? Theme.edge
    }

    private var spoken: String {
        var parts = [session.name, StateMark.label(for: look)]
        if let d = session.detail, !d.isEmpty { parts.append(d) }
        parts.append("started " + Age.spoken(session.created, now: now))
        if let t = session.tokens { parts.append("\(t.total) tokens") }
        return parts.joined(separator: ", ")
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
        Group {
            if let s = selected {
                details(s)
            } else {
                Text("Pick a session to see its details.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(Theme.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(shape.fill(Theme.panel))
        .overlay(shape.strokeBorder(selected.flatMap { Look($0).color }?.opacity(0.45) ?? Theme.edge, lineWidth: 1))
        .clipShape(shape)
        .padding(.horizontal, 10)
        .padding(.top, 6)
        .frame(height: Self.height)
    }

    private func details(_ s: Session) -> some View {
        let look = Look(s)
        return VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                StateMark(look: look)
                Text(StateMark.label(for: look))
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundStyle(look == .needs ? Theme.needs : Color.primary)
                Spacer(minLength: 4)
                Button("Type to it") { store.show(s.id, focus: true) }
                if let cwd = s.cwd {
                    Button("Show folder") { store.reveal(cwd) }
                }
            }
            .controlSize(.small)
            line(s.detail, color: look == .needs ? Theme.needs : Color.primary)
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
                .font(.system(size: mono ? 10.5 : 11.5, design: mono ? .monospaced : .default))
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
    }
}

/// The mark, the name and a count per state. A count shows the first
/// session, in list order, in that state.
private struct Header: View {
    @EnvironmentObject private var store: Store

    var body: some View {
        HStack(spacing: 7) {
            SunMark().frame(width: 22, height: 22)
            Text("Irie").font(.system(size: 18, weight: .heavy, design: .rounded))
            Spacer()
            switch store.link {
            case .online:
                count(.needs, color: Theme.needs, label: "need you")
                count(.working, color: Theme.work, label: "working")
                count(.done, color: Theme.done, label: "done")
            case .offline:
                Text("offline").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.needs)
            case .connecting:
                EmptyView()
            }
        }
        .padding(.horizontal, 14)
        .padding(.top, 10)
        .padding(.bottom, 8)
    }

    private func count(_ g: Look.Group, color: Color, label: String) -> some View {
        let rows = store.sessions.filter { Look($0).group == g }
        return Button {
            if let first = rows.first { store.show(first.id) }
        } label: {
            HStack(spacing: 4) {
                Circle().fill(color).frame(width: 6, height: 6)
                // Two digits' width, so a tenth session doesn't push the others.
                Text("\(rows.count)").font(.system(size: 11.5, weight: .bold, design: .rounded)).monospacedDigit()
                    .frame(minWidth: 15)
            }
            .foregroundStyle(color)
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(Capsule().fill(color.opacity(rows.isEmpty ? 0.06 : 0.14)))
            .overlay(Capsule().strokeBorder(color.opacity(rows.isEmpty ? 0.15 : 0.45), lineWidth: 1))
            .opacity(rows.isEmpty ? 0.5 : 1)
        }
        .buttonStyle(.plain)
        .disabled(rows.isEmpty)
        .help("\(rows.count) \(label)")
        .accessibilityLabel("\(rows.count) \(label)")
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
                .font(.system(size: 12.5))
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
        .padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field))
        .overlay(
            RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                .strokeBorder(focused ? Theme.sunEdge : Theme.edge, lineWidth: focused ? 1.5 : 1)
        )
        .padding(.horizontal, 10)
        .padding(.bottom, 6)
        .onChange(of: store.focusFind) { focused = true }
    }
}

/// Red when it needs you, an open ring while it works, a check when done.
/// The marks hold still: an endless animation in a list makes the whole
/// window lay out again on every frame.
struct StateMark: View {
    let look: Look

    var body: some View {
        ZStack {
            switch look {
            case .needs:
                Circle().fill(Theme.needs.opacity(0.22)).frame(width: 14, height: 14)
                Circle().fill(Theme.needs).frame(width: 8, height: 8)
            case .working:
                Circle().trim(from: 0, to: 0.72)
                    .stroke(Theme.work, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                    .frame(width: 11, height: 11)
                    .rotationEffect(.degrees(-90))
            case .starting:
                Circle().stroke(Theme.work, style: StrokeStyle(lineWidth: 1.5, dash: [2, 2]))
                    .frame(width: 11, height: 11)
            case .done:
                Image(systemName: "checkmark.circle.fill").font(.system(size: 12)).foregroundStyle(Theme.done)
            case .idle:
                Circle().stroke(Theme.muted, lineWidth: 1.5).frame(width: 8, height: 8)
            case .ended:
                Circle().fill(Theme.muted).frame(width: 5, height: 5)
            }
        }
        .frame(width: 14, height: 14)
        .accessibilityLabel(Self.label(for: look))
    }

    static func label(for look: Look) -> String {
        switch look {
        case .needs: return "Needs you"
        case .working: return "Working"
        case .starting: return "Starting"
        case .done: return "Done"
        case .idle: return "Idle"
        case .ended: return "Ended"
        }
    }
}

private struct Offline: View {
    let why: String
    let socket: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(why).font(.system(size: 13, weight: .semibold))
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
            Text("Everything irie.").font(.system(size: 17, weight: .semibold, design: .rounded))
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
