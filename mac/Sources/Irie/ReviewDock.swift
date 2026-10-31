import SwiftUI

// The review dock (DESIGN §4.5). Under close_when_done a finished session
// closes itself, so its card leaves the sidebar, and what is ready for you
// would otherwise vanish. The dock sits above the mode switch and lists
// the sessions that closed on their own, newest first: the work you asked
// for, ready to look at. One you closed yourself is done, so it is never
// here. A fresh one gets the done colour and a dot until you open it or it
// is a quarter of an hour old. A click reopens the session and shows it;
// × drops the row (it stays in history, ⇧⌘T).

extension Store {
    /// Reopens one closed session and shows it, as ⇧⌘T does for the last.
    func reopen(_ c: ClosedSession) {
        Task {
            do {
                let r = try await client.call("reopen", ReopenArgs(session: c.id), timeout: 600, as: ReopenResult.self)
                say("Reopened \(r.session.name).", error: false)
                show(r.session.id, focus: true)
            } catch {
                say("Couldn’t reopen \(c.name): \(error.localizedDescription)", error: true)
            }
        }
    }
}

struct ReviewDock: View {
    @EnvironmentObject private var store: Store
    /// Ids the user dropped or opened, newline-separated.
    @AppStorage("reviewDockSeen") private var seenRaw = ""
    @State private var open = true

    private static let fresh: TimeInterval = 15 * 60
    private static let keep: TimeInterval = 24 * 3600
    private static let rowLimit = 30

    private var seen: Set<String> { Set(seenRaw.split(separator: "\n").map(String.init)) }

    private func mark(_ id: String) {
        guard !seen.contains(id) else { return }
        // Bounded, so the defaults entry cannot grow without end.
        seenRaw = (seenRaw.split(separator: "\n").suffix(200) + [Substring(id)]).joined(separator: "\n")
    }

    var body: some View {
        if store.closeWhenDone {
            TimelineView(.periodic(from: .now, by: 30)) { ctx in
                let rows = store.closed
                    .filter { $0.auto == true && ctx.date.timeIntervalSince($0.closed) < Self.keep && !seen.contains($0.id) }
                    .prefix(Self.rowLimit)
                if !rows.isEmpty {
                    dock(Array(rows), now: ctx.date)
                }
            }
        }
    }

    private func isFresh(_ c: ClosedSession, _ now: Date) -> Bool { now.timeIntervalSince(c.closed) < Self.fresh }

    private func dock(_ rows: [ClosedSession], now: Date) -> some View {
        let fresh = rows.filter { isFresh($0, now) }.count
        return VStack(alignment: .leading, spacing: 0) {
            Button { withAnimation(.easeOut(duration: 0.15)) { open.toggle() } } label: {
                HStack(spacing: 6) {
                    Image(systemName: open ? "chevron.down" : "chevron.right")
                        .font(.system(size: 9, weight: .bold))
                        .frame(width: 10)
                    Text("Ready to review")
                        .font(Theme.font(12, .bold))
                    Text("\(rows.count)")
                        .font(Theme.font(11, .bold))
                        .foregroundStyle(fresh > 0 ? Theme.onAccent : Theme.muted)
                        .padding(.horizontal, 6)
                        .background(Capsule().fill(fresh > 0 ? Look.doneColor : Theme.panel))
                    Spacer(minLength: 0)
                }
                .foregroundStyle(Theme.ink)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .accessibilityLabel("Ready to review, \(rows.count)\(fresh > 0 ? ", \(fresh) new" : "")")
            .accessibilityHint(open ? "Hides the list" : "Shows the list")

            if open {
                ScrollView {
                    VStack(spacing: 2) {
                        ForEach(rows) { c in row(c, now: now) }
                    }
                    .padding(.horizontal, 8)
                    .padding(.bottom, 6)
                }
                .frame(maxHeight: 136)
            }
        }
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Theme.card)
            .shadow(color: Theme.shadow, radius: 2, y: 0.5))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous)
            .strokeBorder(fresh > 0 ? Look.doneColor.opacity(0.55) : Color.clear, lineWidth: 1))
        .padding(.horizontal, 10)
        // Clear of the footer's panel under it.
        .padding(.vertical, 6)
    }

    private func row(_ c: ClosedSession, now: Date) -> some View {
        let new = isFresh(c, now)
        return HStack(spacing: 6) {
            Button {
                mark(c.id)
                store.reopen(c)
            } label: {
                HStack(spacing: 6) {
                    StateMark(look: .done, size: 12)
                    Text(c.name)
                        .font(Theme.font(12, new ? .semibold : .regular))
                        .foregroundStyle(new ? Theme.ink : Theme.muted)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    Text(Age.short(c.closed, now: now))
                        .font(Theme.font(10.5))
                        .foregroundStyle(Theme.muted)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Reopen \(c.name) to review it")
            .accessibilityLabel("\(c.name), finished \(Age.spoken(c.closed, now: now))\(new ? ", new" : "")")
            .accessibilityHint("Reopens it")
            Button { mark(c.id) } label: {
                Image(systemName: "xmark").font(.system(size: 9, weight: .bold)).foregroundStyle(Theme.muted)
                    .frame(width: 16, height: 16)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Drop it from this list (it stays in history)")
            .accessibilityLabel("Drop \(c.name)")
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 3)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(new ? Look.doneColor.opacity(0.12) : Color.clear))
    }
}
