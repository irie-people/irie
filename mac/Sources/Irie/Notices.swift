import SwiftUI

// Notices: what the app tells the user, such as an error or what a step did
// (Store.say). The one on show sits at the top of the working area, never
// over the task bar, which nothing ever covers. It goes by itself, its ×
// closes it at once, and the right sidebar's Notices tab keeps every one.

/// A notice as the Notices tab keeps it.
struct LoggedNotice: Codable, Identifiable, Equatable {
    var id = UUID()
    var text: String
    var isError: Bool
    var at = Date()

    /// How many the tab keeps: the newest.
    static let kept = 100
    private static let key = "noticeLog"

    /// Those kept from earlier launches, oldest first.
    static func saved() -> [LoggedNotice] {
        guard let d = UserDefaults.standard.data(forKey: key),
              let all = try? JSONDecoder().decode([LoggedNotice].self, from: d) else { return [] }
        return Array(all.suffix(kept))
    }

    static func save(_ all: [LoggedNotice]) {
        if let d = try? JSONEncoder().encode(Array(all.suffix(kept))) {
            UserDefaults.standard.set(d, forKey: key)
        }
    }

    /// When it came: the time today, the day and time before.
    var when: String {
        Calendar.current.isDateInToday(at)
            ? at.formatted(date: .omitted, time: .shortened)
            : at.formatted(.dateTime.month(.abbreviated).day().hour().minute())
    }
}

/// The notice on show, at the top of the working area (TerminalArea), never
/// over the task bar. A notice that reports a step the history can take
/// back, such as a park, offers Undo; one that added a jot to the task
/// before offers New task. The pointer resting on it holds it, and its ×
/// closes it.
@MainActor
struct NoticeToast: View {
    @Environment(Store.self) private var store

    var body: some View {
        if let n = store.notice {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(n.text)
                    .font(.system(size: 11.5, weight: .medium))
                    .foregroundStyle(n.isError ? Theme.needs : Theme.ink)
                    .lineLimit(4)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                if n.undo {
                    Button("Undo") {
                        store.dismissNotice()
                        store.undo()
                    }
                    .buttonStyle(.borderless)
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundStyle(Theme.accentInk)
                    .help("Take it back (⌘Z)")
                }
                if let apart = n.apart {
                    Button("New task") { store.startApart(apart) }
                        .buttonStyle(.borderless)
                        .font(.system(size: 11.5, weight: .semibold))
                        .foregroundStyle(Theme.accentInk)
                        .help("Take it back and start it as a task of its own")
                }
                Button { store.dismissNotice() } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 16, height: 16)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Close it; the right sidebar’s Notices tab keeps it")
                .accessibilityLabel("Close the notice")
            }
            .padding(.leading, Theme.inset)
            .padding(.trailing, 8)
            .padding(.vertical, 7)
            .background(
                RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                    .fill(Theme.field)
                    .shadow(radius: 3, y: 1)
                    .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                        .strokeBorder(n.isError ? Theme.needs.opacity(0.6) : Theme.edge, lineWidth: 1))
            )
            .frame(maxWidth: 380, alignment: .trailing)
            .onHover { store.noticeHeld = $0 }
            .transition(.opacity)
            .id(n.id)
            .accessibilityElement(children: .contain)
            .accessibilityAddTraits(.updatesFrequently)
        }
    }
}

/// The right sidebar's Notices tab: every notice the app showed, newest
/// first, the newest 100 kept across launches, since each one on show goes
/// by itself. Errors are in red, and the tab counts those it has not shown
/// yet.
@MainActor
struct NoticesTab: View {
    @Environment(Store.self) private var store

    var body: some View {
        Group {
            if store.notices.isEmpty {
                SideNote(title: "No notices", text: "What the app tells you, such as an error, shows at the top of the window for a few seconds, and stays here.")
            } else {
                VStack(spacing: 0) {
                    HStack {
                        Text(store.notices.count == 1 ? "1 notice" : "\(store.notices.count) notices")
                            .font(Theme.font(11.5, .medium))
                            .foregroundStyle(Theme.muted)
                        Spacer()
                        Button("Clear") { store.clearNotices() }
                            .buttonStyle(.borderless)
                            .font(Theme.font(11.5, .semibold))
                            .foregroundStyle(Theme.accentInk)
                            .help("Empty the list")
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 6) {
                            ForEach(Array(store.notices.reversed())) { n in row(n) }
                        }
                        .padding(.horizontal, 10)
                        .padding(.bottom, 10)
                    }
                }
            }
        }
        .onAppear { store.sawNotices() }
    }

    private func row(_ n: LoggedNotice) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(n.text)
                .font(Theme.font(12))
                .foregroundStyle(n.isError ? Theme.needs : Theme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            Text(n.when)
                .font(Theme.font(10.5))
                .foregroundStyle(Theme.muted)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.card))
        .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
            .strokeBorder(n.isError ? Theme.needs.opacity(0.45) : Theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
        .accessibilityLabel((n.isError ? "Error: " : "") + n.text + ", " + n.when)
    }
}
