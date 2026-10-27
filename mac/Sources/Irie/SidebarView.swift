import SwiftUI

/// The sidebar: a card per session, grouped by what it needs from you. A
/// click shows that agent's terminal and gives it the keyboard; the arrow
/// keys walk the cards and keep the keyboard here. The selected card opens
/// up to show its details.
struct SidebarView: View {
    @EnvironmentObject private var store: Store
    @FocusState private var listFocused: Bool

    private var visible: [Session] {
        let f = store.filter.trimmingCharacters(in: .whitespaces).lowercased()
        return f.isEmpty ? store.sessions : store.sessions.filter { $0.name.lowercased().contains(f) }
    }

    var body: some View {
        VStack(spacing: 0) {
            Header()
            FindField()
            content
            TaskBar()
        }
        .background(Theme.sidebar.ignoresSafeArea())
    }

    @ViewBuilder private var content: some View {
        if case let .offline(why) = store.link, store.sessions.isEmpty {
            Offline(why: why, socket: store.socketPath)
        } else if store.sessions.isEmpty {
            Hint(text: store.link == .connecting ? "Connecting to Irie…" : "No sessions yet. Type a task below to start one.")
        } else if visible.isEmpty {
            Hint(text: "No sessions match “\(store.filter)”.")
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    // One clock for every card's age.
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        LazyVStack(alignment: .leading, spacing: 10) {
                            ForEach(Look.Group.allCases, id: \.self) { group in
                                let rows = visible.filter { Look($0).group == group }
                                if !rows.isEmpty {
                                    GroupSection(group: group, rows: rows, now: ctx.date)
                                }
                            }
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 6)
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
        }
    }
}

/// A group's header, which folds the group, and its cards.
private struct GroupSection: View {
    @EnvironmentObject private var store: Store
    let group: Look.Group
    let rows: [Session]
    let now: Date

    var body: some View {
        let open = !store.collapsed.contains(group)
        VStack(alignment: .leading, spacing: 2) {
            Button {
                if open { store.collapsed.insert(group) } else { store.collapsed.remove(group) }
            } label: {
                HStack(spacing: 5) {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 8, weight: .bold))
                        .rotationEffect(.degrees(open ? 90 : 0))
                    Text(group.title.uppercased()).font(.system(size: 10.5, weight: .bold)).tracking(0.8)
                    Text("\(rows.count)").font(.system(size: 10.5, weight: .semibold)).monospacedDigit().opacity(0.75)
                    Spacer()
                }
                .foregroundStyle(group == .needs ? Theme.needs : Color.secondary)
                .padding(.horizontal, 6)
                .padding(.vertical, 3)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(group.title), \(rows.count)")
            .accessibilityHint(open ? "Folds the group" : "Opens the group")
            if open {
                ForEach(rows) { s in
                    SessionCard(session: s, selected: s.id == store.selection, now: now).id(s.id)
                }
            }
        }
    }
}

/// One session. A single line; what it asks when it needs you, on a red
/// tint; its details while selected.
private struct SessionCard: View {
    @EnvironmentObject private var store: Store
    let session: Session
    let selected: Bool
    let now: Date
    @State private var hovering = false

    private var look: Look { Look(session) }
    private var quiet: Bool { look == .done || look == .idle || look == .ended }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                StateMark(look: look)
                Text(session.name)
                    .font(.system(size: 13, weight: quiet ? .regular : .semibold))
                    .foregroundStyle(look == .ended || look == .idle ? Color.secondary : Color.primary)
                    .lineLimit(2)
                Spacer(minLength: 4)
                if hovering {
                    QuickActions(session: session)
                } else {
                    Text([session.tokens.map { Tokens.short($0.total) }, Age.short(session.created, now: now)]
                        .compactMap { $0 }.joined(separator: " · "))
                        .font(.system(size: 11)).monospacedDigit()
                        .foregroundStyle(.tertiary)
                        .help(session.tokens?.breakdown ?? "")
                }
            }
            .frame(minHeight: 20)
            if look == .needs, let d = session.detail, !d.isEmpty {
                Text(d)
                    .font(.system(size: 11.5, weight: .medium))
                    .foregroundStyle(Theme.needs)
                    .lineLimit(selected ? 4 : 2)
                    .padding(.leading, 22)
            }
            if let helpers = session.subagents, !helpers.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(helpers) { HelperLine(helper: $0) }
                }
                .padding(.leading, 22)
            }
            if selected {
                Details(session: session).padding(.leading, 22).padding(.top, 2)
            }
        }
        .padding(.vertical, 7)
        .padding(.leading, 10)
        .padding(.trailing, 8)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(fill))
        .overlay(alignment: .leading) {
            if selected {
                Capsule().fill(Theme.sun).frame(width: 3).padding(.vertical, 7)
            }
        }
        .contentShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
        .onHover { hovering = $0 }
        .onTapGesture { store.show(session.id, focus: true) }
        .contextMenu { Actions(session: session) }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction { store.show(session.id, focus: true) }
    }

    private var fill: Color {
        if selected { return Theme.selection }
        if hovering { return Theme.hover }
        return look == .needs ? Theme.needsTint : .clear
    }

    private var accessibilityText: String {
        var parts = [session.name, StateMark.label(for: look)]
        if let d = session.detail, !d.isEmpty { parts.append(d) }
        return parts.joined(separator: ", ")
    }
}

/// A helper (subagent) under the card of the session that started it: red
/// when it needs you, blue while it works, with its current tool.
private struct HelperLine: View {
    let helper: Subagent

    private var needs: Bool { helper.needsYou == true }

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "arrow.turn.down.right")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.tertiary)
            Circle().fill(needs ? Theme.needs : Theme.work).frame(width: 6, height: 6)
            Text(helper.type ?? "Helper").font(.system(size: 11.5, weight: .medium))
            if let d = helper.detail, !d.isEmpty {
                Text(d)
                    .font(.system(size: 11))
                    .foregroundStyle(needs ? Theme.needs : Color.secondary)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Helper \(helper.type ?? ""), \(needs ? "needs you" : "working")\(helper.detail.map { ", " + $0 } ?? "")")
    }
}

/// What a hovered card offers without opening it.
private struct QuickActions: View {
    @EnvironmentObject private var store: Store
    let session: Session

    var body: some View {
        HStack(spacing: 2) {
            Button { store.show(session.id, focus: true) } label: { Image(systemName: "keyboard") }
                .help("Type to it")
                .accessibilityLabel("Type to it")
            if let cwd = session.cwd {
                Button { store.reveal(cwd) } label: { Image(systemName: "folder") }
                    .help("Show the folder in Finder")
                    .accessibilityLabel("Show the folder in Finder")
            }
            Menu {
                Actions(session: session)
            } label: {
                Image(systemName: "ellipsis")
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .fixedSize()
            .help("More")
            .accessibilityLabel("More actions")
        }
        .buttonStyle(.borderless)
        .font(.system(size: 11, weight: .semibold))
        .foregroundStyle(.secondary)
    }
}

/// What a selected card adds: the current step, where it runs, warnings, the
/// screen of a spawn that went wrong, and the buttons.
private struct Details: View {
    @EnvironmentObject private var store: Store
    let session: Session

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            if Look(session) != .needs, let d = session.detail, !d.isEmpty {
                Text(d).font(.system(size: 11.5)).foregroundStyle(.secondary).lineLimit(3)
            }
            let facts = [session.profile, session.branch].compactMap { $0 }.filter { !$0.isEmpty }
            if !facts.isEmpty {
                Text(facts.joined(separator: " · "))
                    .font(.system(size: 10.5, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.middle)
            }
            if let cwd = session.cwd {
                Text(Age.tilde(cwd))
                    .font(.system(size: 10.5, design: .monospaced))
                    .foregroundStyle(.tertiary)
                    .lineLimit(1).truncationMode(.middle)
            }
            if let t = session.tokens {
                Text(t.breakdown).font(.system(size: 10.5)).monospacedDigit().foregroundStyle(.secondary)
            }
            ForEach(session.warnings ?? [], id: \.self) { w in
                Label(w, systemImage: "exclamationmark.triangle.fill")
                    .font(.system(size: 11)).foregroundStyle(.orange)
            }
            if let tail = session.tail, !tail.isEmpty {
                Text(tail.suffix(6).joined(separator: "\n"))
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .padding(6)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: 6).fill(Color.primary.opacity(0.06)))
            }
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

/// The mark, the name and a count per group. A count shows that group's
/// first session.
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
                Text("\(rows.count)").font(.system(size: 11.5, weight: .bold)).monospacedDigit()
            }
            .foregroundStyle(color)
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(Capsule().fill(color.opacity(rows.isEmpty ? 0.06 : 0.14)))
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
            Image(systemName: "magnifyingglass").font(.system(size: 11)).foregroundStyle(.secondary)
            TextField("Find a session", text: $store.filter)
                .textFieldStyle(.plain)
                .font(.system(size: 12.5))
                .focused($focused)
                .onExitCommand {
                    store.filter = ""
                    store.focusSidebar += 1
                }
            if !store.filter.isEmpty {
                Button { store.filter = "" } label: { Image(systemName: "xmark.circle.fill") }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                    .help("Clear")
                    .accessibilityLabel("Clear the search")
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 5)
        .background(RoundedRectangle(cornerRadius: 7).fill(Color.primary.opacity(0.06)))
        .padding(.horizontal, 10)
        .padding(.bottom, 4)
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
                Circle().stroke(Color.secondary, lineWidth: 1.5).frame(width: 8, height: 8)
            case .ended:
                Circle().fill(Color.secondary).frame(width: 5, height: 5)
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
                .font(.system(size: 12)).foregroundStyle(.secondary)
            Text(Age.tilde(socket)).font(.system(size: 10.5, design: .monospaced)).foregroundStyle(.tertiary)
                .lineLimit(1).truncationMode(.middle)
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

private struct Hint: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.system(size: 12.5)).foregroundStyle(.secondary)
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

    static func tilde(_ path: String) -> String {
        let home = NSHomeDirectory()
        return path.hasPrefix(home + "/") ? "~" + path.dropFirst(home.count) : path
    }
}
