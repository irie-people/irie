import SwiftUI

/// The sidebar: a compact row per session, grouped by what it needs from
/// you. Selecting a row shows that agent in the terminal; the selected row
/// opens up to show its details.
struct SidebarView: View {
    @EnvironmentObject private var store: Store
    let filter: String
    @State private var collapsed: Set<Look.Group> = []

    private var visible: [Session] {
        let f = filter.trimmingCharacters(in: .whitespaces).lowercased()
        return f.isEmpty ? store.sessions : store.sessions.filter { $0.name.lowercased().contains(f) }
    }

    var body: some View {
        VStack(spacing: 0) {
            Header()
            content
            Divider()
            TaskBar()
        }
    }

    @ViewBuilder private var content: some View {
        if case let .offline(why) = store.link, store.sessions.isEmpty {
            Offline(why: why, socket: store.socketPath)
        } else if store.sessions.isEmpty {
            Hint(text: store.link == .connecting ? "Connecting to Irie…" : "No sessions yet. Type a task below to start one.")
        } else if visible.isEmpty {
            Hint(text: "No sessions match “\(filter)”.")
        } else {
            List(selection: Binding(get: { store.selection }, set: { if let id = $0 { store.show(id) } })) {
                ForEach(Look.Group.allCases, id: \.self) { group in
                    let rows = visible.filter { Look($0).group == group }
                    if !rows.isEmpty {
                        Section(isExpanded: expansion(group)) {
                            ForEach(rows) { s in
                                SessionRow(session: s, selected: s.id == store.selection).tag(s.id)
                            }
                        } header: {
                            HStack {
                                Text(group.title)
                                Spacer()
                                Text("\(rows.count)").monospacedDigit()
                            }
                            .foregroundStyle(group == .needs ? Theme.needs : .secondary)
                        }
                    }
                }
            }
            .listStyle(.sidebar)
            .onKeyPress(.return) {
                guard let id = store.selection else { return .ignored }
                store.show(id, focus: true)
                return .handled
            }
        }
    }

    private func expansion(_ g: Look.Group) -> Binding<Bool> {
        Binding(get: { !collapsed.contains(g) }, set: { open in
            if open { collapsed.remove(g) } else { collapsed.insert(g) }
        })
    }
}

/// The mark, the name and the counts. A count jumps to that group's first
/// session.
private struct Header: View {
    @EnvironmentObject private var store: Store

    var body: some View {
        HStack(spacing: 7) {
            SunMark().frame(width: 22, height: 22)
            Text("Irie").font(.system(size: 18, weight: .heavy, design: .rounded))
            Spacer()
            if case .online = store.link {
                count(.needs, systemImage: "circle.fill", color: Theme.needs, label: "need you")
                count(.working, systemImage: "circle.dotted", color: Theme.work, label: "working")
                count(.done, systemImage: "checkmark", color: Theme.done, label: "done")
            } else if case .offline = store.link {
                Text("offline").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.needs)
            }
        }
        .padding(.horizontal, 14)
        .padding(.top, 6)
        .padding(.bottom, 8)
    }

    private func count(_ g: Look.Group, systemImage: String, color: Color, label: String) -> some View {
        let rows = store.sessions.filter { Look($0).group == g }
        return Button {
            if let first = rows.first { store.show(first.id) }
        } label: {
            HStack(spacing: 3) {
                Image(systemName: systemImage).font(.system(size: 8, weight: .bold))
                Text("\(rows.count)").font(.system(size: 12, weight: .bold)).monospacedDigit()
            }
            .foregroundStyle(color)
            .opacity(rows.isEmpty ? 0.4 : 1)
            .padding(.horizontal, 4)
            .padding(.vertical, 3)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(rows.isEmpty)
        .help("\(rows.count) \(label)")
    }
}

/// One session: a single line, plus what it asks when it needs you, plus its
/// details while selected.
private struct SessionRow: View {
    @EnvironmentObject private var store: Store
    let session: Session
    let selected: Bool

    private var look: Look { Look(session) }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 7) {
                StateMark(look: look)
                Text(session.name)
                    .font(.system(size: 13, weight: look == .done || look == .idle || look == .ended ? .regular : .medium))
                    .foregroundStyle(look == .ended || look == .idle ? .secondary : .primary)
                    .lineLimit(2)
                Spacer(minLength: 4)
                TimelineView(.periodic(from: .now, by: 30)) { ctx in
                    Text(Age.short(session.created, now: ctx.date))
                        .font(.system(size: 11)).monospacedDigit()
                        .foregroundStyle(.tertiary)
                }
            }
            if look == .needs, let d = session.detail, !d.isEmpty {
                Text(d)
                    .font(.system(size: 11.5))
                    .foregroundStyle(Theme.needs)
                    .lineLimit(selected ? 4 : 2)
                    .padding(.leading, 21)
            }
            if selected {
                Details(session: session).padding(.leading, 21)
            }
        }
        .padding(.vertical, 3)
        .contextMenu { Actions(session: session) }
        .help(session.detail ?? session.name)
    }
}

/// What a selected row adds: the current step, where it runs, warnings, the
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
                    .background(RoundedRectangle(cornerRadius: 6).fill(.quaternary))
            }
            HStack(spacing: 6) {
                Button("Type to it") { store.show(session.id, focus: true) }
                if let cwd = session.cwd {
                    Button("Show folder") { store.reveal(cwd) }
                }
            }
            .controlSize(.small)
            .padding(.top, 2)
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

/// Red when it needs you, an open ring while it works, a check when done.
/// The marks hold still: an endless animation in a list makes the whole
/// window lay out again on every frame.
struct StateMark: View {
    let look: Look

    var body: some View {
        ZStack {
            switch look {
            case .needs:
                Circle().fill(Theme.needs).frame(width: 8, height: 8)
                    .background(Circle().fill(Theme.needs.opacity(0.22)).frame(width: 14, height: 14))
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
                Circle().stroke(.secondary, lineWidth: 1.5).frame(width: 8, height: 8)
            case .ended:
                Circle().fill(.secondary).frame(width: 5, height: 5)
            }
        }
        .frame(width: 14, height: 14)
        .accessibilityLabel(label)
    }

    private var label: String {
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
