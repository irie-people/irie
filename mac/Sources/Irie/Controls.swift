import AppKit
import SwiftUI
import UniformTypeIdentifiers

// Pause, Promote and drag to reorder (DESIGN §4.1, §4.3). Pause freezes a
// session's whole process tree at once, its helpers and everything they
// started, and Resume continues it. Promote puts a session at the top of the
// list and gives its process tree the machine first: the other sessions run
// in macOS's background state. The core keeps the one order both sidebars
// draw, and a card dragged elsewhere in its group goes where it is dropped;
// Move Up and Move Down do the same without a drag. Each change shows at
// once, and the core's frames confirm it.

struct FreezeArgs: Encodable {
    var session: String
}

struct FreezeResult: Decodable {
    var sessions: [String]
}

struct PriorityArgs: Encodable {
    var session: String
    var level: Int
}

struct ReorderArgs: Encodable {
    var session: String
    var before: String?
}

/// Where a dragged card would land: just above or just below the card under
/// the pointer.
struct DropMark: Equatable {
    var target: String
    var below: Bool
}

extension Session {
    var isFrozen: Bool { state == "frozen" }
    var isPromoted: Bool { (priority ?? 0) > 0 }
    /// Whether it runs, so that it can pause.
    var canFreeze: Bool { state != "exited" && state != "failed" }
}

extension Store {
    /// Pauses (freezes) or resumes a session. A pause shows at once.
    func setFrozen(_ s: Session, _ on: Bool) {
        if on {
            preview { rows in
                if let i = rows.firstIndex(where: { $0.id == s.id }) {
                    rows[i].state = "frozen"
                    rows[i].frozen = Date()
                }
            }
        }
        Task {
            do {
                _ = try await client.call(on ? "freeze" : "thaw", FreezeArgs(session: s.id), as: FreezeResult.self)
            } catch {
                preview { rows in
                    if let i = rows.firstIndex(where: { $0.id == s.id }) {
                        rows[i].state = s.state
                        rows[i].frozen = s.frozen
                    }
                }
                say("Couldn’t \(on ? "pause" : "resume") \(s.name): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Promotes a session to the top, or puts it back among the rest, where
    /// it stays. It shows at once.
    func setPromoted(_ s: Session, _ on: Bool) {
        preview { rows in
            guard let i = rows.firstIndex(where: { $0.id == s.id }) else { return }
            rows[i].priority = on ? 1 : 0
            if on { rows[i].order = (rows.compactMap(\.order).min() ?? 0) - 1 }
        }
        Task {
            do {
                _ = try await client.call("priority", PriorityArgs(session: s.id, level: on ? 1 : 0), as: Empty.self)
                if on {
                    say("Promoted \(s.name): it has the CPU and disk first.", error: false)
                }
            } catch {
                preview { rows in
                    if let i = rows.firstIndex(where: { $0.id == s.id }) { rows[i].priority = s.priority }
                }
                say("Couldn’t promote \(s.name): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// The drawn group a session is in.
    private func group(of id: String) -> SessionGroup? {
        drawnGroups.first { g in g.sessions.contains { $0.id == id } }
    }

    /// Whether the held card can land on target: another card of its group.
    func canDrop(onto target: Session) -> Bool {
        guard let id = dragging, id != target.id, let g = group(of: id) else { return false }
        return g.sessions.contains { $0.id == target.id }
    }

    /// Where the held card lands on target: in target's place, so below it
    /// when it comes from above and above it when it comes from below.
    func mark(onto target: Session) -> DropMark? {
        guard canDrop(onto: target), let id = dragging,
              let from = sessions.firstIndex(where: { $0.id == id }),
              let to = sessions.firstIndex(where: { $0.id == target.id }) else { return nil }
        return DropMark(target: target.id, below: from < to)
    }

    /// Puts session id in target's place, at once, and tells the core.
    func reorder(_ id: String, onto target: Session) {
        guard let from = sessions.firstIndex(where: { $0.id == id }),
              let to = sessions.firstIndex(where: { $0.id == target.id }), from != to else { return }
        var ids = sessions.map(\.id)
        let moving = ids.remove(at: from)
        guard let t = ids.firstIndex(of: target.id) else { return }
        let at = t + (from < to ? 1 : 0)
        ids.insert(moving, at: at)
        let before = at + 1 < ids.count ? ids[at + 1] : nil
        preview { rows in
            for i in rows.indices { rows[i].order = ids.firstIndex(of: rows[i].id) }
        }
        Task {
            do {
                _ = try await client.call("reorder", ReorderArgs(session: id, before: before), as: Empty.self)
            } catch {
                say("Couldn’t move it: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Moves a session one card up (-1) or down (+1) in its group, as a drag
    /// would.
    func move(_ s: Session, by delta: Int) {
        guard let g = group(of: s.id), let i = g.sessions.firstIndex(where: { $0.id == s.id }),
              g.sessions.indices.contains(i + delta) else { return }
        reorder(s.id, onto: g.sessions[i + delta])
    }

    /// Whether a session can move one card up (-1) or down (+1).
    func canMove(_ s: Session, by delta: Int) -> Bool {
        guard let g = group(of: s.id), let i = g.sessions.firstIndex(where: { $0.id == s.id }) else { return false }
        return g.sessions.indices.contains(i + delta)
    }
}

/// Pause or Resume, as an icon: on a hovered card and in the details panel.
struct PauseButton: View {
    @EnvironmentObject private var store: Store
    let session: Session

    var body: some View {
        let frozen = session.isFrozen
        Button { store.setFrozen(session, !frozen) } label: {
            Image(systemName: frozen ? "play.fill" : "pause.fill").frame(width: 20, height: 18)
        }
        .help(frozen ? "Resume: it and everything it started carry on" : "Pause: stop it, its helpers and everything they started, at once")
        .disabled(!frozen && !session.canFreeze)
        .accessibilityLabel(frozen ? "Resume" : "Pause")
    }
}

/// Promote, or put back, as an icon: on a hovered card and in the details
/// panel.
struct PromoteButton: View {
    @EnvironmentObject private var store: Store
    let session: Session
    /// In ink, where the accent's text colour falls short of 3:1, such as a
    /// selected working card's deep end; the filled arrow still shows it on.
    var ink = false

    var body: some View {
        let on = session.isPromoted
        Button { store.setPromoted(session, !on) } label: {
            Image(systemName: on ? "arrow.up.circle.fill" : "arrow.up.circle").frame(width: 20, height: 18)
        }
        .foregroundStyle(ink ? Theme.ink : on ? Theme.accentInk : Theme.muted)
        .help(on ? "Promoted: click to put it back among the rest" : "Promote: put it first, with the CPU and disk first")
        .accessibilityLabel(on ? "Stop promoting" : "Promote")
    }
}

/// The right-click menu's items for these.
struct ControlItems: View {
    @EnvironmentObject private var store: Store
    let session: Session

    var body: some View {
        if session.isFrozen {
            Button("Resume") { store.setFrozen(session, false) }
        } else {
            Button("Pause") { store.setFrozen(session, true) }.disabled(!session.canFreeze)
        }
        Button(session.isPromoted ? "Stop Promoting" : "Promote to the Top") { store.setPromoted(session, !session.isPromoted) }
        Button("Move Up") { store.move(session, by: -1) }.disabled(!store.canMove(session, by: -1))
        Button("Move Down") { store.move(session, by: 1) }.disabled(!store.canMove(session, by: 1))
    }
}

/// A promoted card's badge, after its name.
struct PromotedBadge: View {
    var body: some View {
        Image(systemName: "arrow.up.circle.fill")
            .font(.system(size: 11, weight: .bold))
            .foregroundStyle(Theme.accentInk)
            .help("Promoted: it has the CPU and disk first")
            .accessibilityHidden(true)
    }
}

/// What the details panel says about a frozen or promoted session, in
/// short lines with the whole story on hover: plainly what Irie does, and
/// that memory is not something it can set aside.
struct ControlLines: View {
    let session: Session

    var body: some View {
        if session.isFrozen {
            if session.freezing == true {
                lineText("Freezing at its next tool call",
                         more: "Everything it started has stopped. The agent itself started before Irie could pause it whole, so it stops at its next tool call.")
            } else {
                lineText("Paused" + (session.frozen.map { " since " + $0.formatted(date: .omitted, time: .shortened) } ?? ""),
                         more: "It, its helpers and everything they started are stopped. Resume carries them on.")
            }
        }
        if session.isPromoted {
            let more = "Promoted: it gets the CPU and disk first, and the other sessions run in the background. macOS gives no way to set memory aside for one process; the background ones are the first it slows."
            lineText("Promoted: CPU and disk first", more: more)
            lineText("macOS can’t set memory aside", more: more, color: Theme.muted)
        }
    }

    private func lineText(_ text: String, more: String, color: Color = Theme.ink) -> some View {
        Text(text)
            .font(Theme.font(12))
            .foregroundStyle(color)
            .lineLimit(1)
            .truncationMode(.tail)
            .help(more)
            .accessibilityLabel(more)
    }
}

/// Lets a card be dragged to another place in its group, with a line where
/// it would land.
struct Reorderable: ViewModifier {
    @EnvironmentObject private var store: Store
    let session: Session

    func body(content: Content) -> some View {
        content
            .onDrag {
                store.dragging = session.id
                return NSItemProvider(object: session.id as NSString)
            }
            .onDrop(of: [.text], delegate: CardDrop(store: store, target: session))
            .overlay(alignment: store.dropMark?.below == true ? .bottom : .top) {
                if store.dropMark?.target == session.id {
                    Capsule().fill(Theme.accent).frame(height: 3).padding(.horizontal, 6)
                        .offset(y: store.dropMark?.below == true ? 5 : -5)
                        .allowsHitTesting(false)
                }
            }
    }
}

@MainActor
private struct CardDrop: DropDelegate {
    let store: Store
    let target: Session

    func validateDrop(info: DropInfo) -> Bool { store.canDrop(onto: target) }

    func dropEntered(info: DropInfo) { store.dropMark = store.mark(onto: target) }

    func dropUpdated(info: DropInfo) -> DropProposal? {
        DropProposal(operation: store.canDrop(onto: target) ? .move : .forbidden)
    }

    func dropExited(info: DropInfo) {
        if store.dropMark?.target == target.id { store.dropMark = nil }
    }

    func performDrop(info: DropInfo) -> Bool {
        defer {
            store.dragging = nil
            store.dropMark = nil
        }
        guard let id = store.dragging, store.canDrop(onto: target) else { return false }
        store.reorder(id, onto: target)
        return true
    }
}
