import AppKit
import SwiftUI
import UniformTypeIdentifiers

// Pause, Promote, Park and drag to reorder (DESIGN §4.1, §4.3). Pause
// freezes a session's whole process tree at once, its helpers and everything
// they started, and Resume continues it. Promote puts a session at the top of
// the list and gives its process tree the machine first: the other sessions
// run in macOS's background state. Park puts a session in the Parked group at
// the end of the list, folded until opened, so it goes out of sight and out
// of the To verify count; Unpark brings it back to its place. Parked or
// unparked from the card under the pointer, the card stays put until the
// pointer leaves it, so a second click on the same spot takes it back; a
// notice offers Undo too. The core keeps the one order both sidebars draw,
// and a card dragged elsewhere in its group goes where it is dropped;
// dropped across the Parked group's edge, it is parked or unparked there.
// Move Up and Move Down move one without a drag. Each change shows at once,
// and the core's frames confirm it.

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

struct ParkArgs: Encodable {
    var session: String
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
    var isParked: Bool { parked ?? false }
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

    /// Promotes a session to the top, which unparks it, or puts it back
    /// among the rest, where it stays. It shows at once.
    func setPromoted(_ s: Session, _ on: Bool) {
        if on { lingering[s.id] = nil }
        preview { rows in
            guard let i = rows.firstIndex(where: { $0.id == s.id }) else { return }
            rows[i].priority = on ? 1 : 0
            if on {
                rows[i].order = (rows.compactMap(\.order).min() ?? 0) - 1
                rows[i].parked = false
            }
        }
        Task {
            do {
                _ = try await client.call("priority", PriorityArgs(session: s.id, level: on ? 1 : 0), as: Empty.self)
                if on {
                    say("Promoted \(s.name): it has the CPU and disk first.", error: false)
                }
            } catch {
                preview { rows in
                    if let i = rows.firstIndex(where: { $0.id == s.id }) {
                        rows[i].priority = s.priority
                        rows[i].parked = s.parked
                    }
                }
                say("Couldn’t promote \(s.name): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Parks a session, which puts its card in the Parked group at the end
    /// of the list, or unparks it back to its place. It shows at once, and
    /// a notice says where it went, since a folded Parked hides it, with
    /// Undo. From the card under the pointer (fromPointer), the card stays
    /// where it is, drawn parked or not, until the pointer leaves it: what
    /// slid under the pointer, such as Parked's header, which folds, or the
    /// next card's Park, would take the click meant to take it back.
    func setParked(_ s: Session, _ on: Bool, fromPointer: Bool = false) {
        guard canPark, s.isParked != on else { return }
        if fromPointer, hovered == s.id {
            if let drawn = lingering[s.id] {
                // Back the way it is drawn.
                if drawn == on { lingering[s.id] = nil }
            } else {
                lingering[s.id] = s.isParked
            }
        } else {
            lingering[s.id] = nil
        }
        preview { rows in
            if let i = rows.firstIndex(where: { $0.id == s.id }) { rows[i].parked = on }
        }
        Task {
            do {
                _ = try await client.call(on ? "park" : "unpark", ParkArgs(session: s.id), as: Empty.self)
                say(on ? "Parked \(s.name), out of the way at the end of the list." : "Unparked \(s.name), back in its place.",
                    error: false, seconds: 6, undo: true)
            } catch {
                lingering[s.id] = nil
                preview { rows in
                    if let i = rows.firstIndex(where: { $0.id == s.id }) { rows[i].parked = s.parked }
                }
                say("Couldn’t \(on ? "park" : "unpark") \(s.name): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// The pointer came onto a card or left it. A card that lingers there
    /// takes its place once the pointer leaves it.
    func hover(_ id: String, _ inside: Bool) {
        if inside {
            hovered = id
            return
        }
        if hovered == id { hovered = nil }
        settle(id)
    }

    /// Puts a lingering card, or with none named every one, where it now
    /// belongs.
    func settle(_ id: String? = nil) {
        guard id.map({ lingering[$0] != nil }) ?? !lingering.isEmpty else { return }
        withAnimation(.easeOut(duration: 0.2)) {
            if let id { lingering[id] = nil } else { lingering = [:] }
        }
    }

    /// Parks or unparks the selected card: the sidebar's z key.
    func toggleParked() {
        guard let s = closable else { return }
        setParked(s, !s.isParked)
    }

    /// Folds the Parked group, or opens it: the sidebar's Z key.
    func foldParked() {
        if let g = drawnGroups.first(where: { $0.id == SessionGroup.parked }) { fold(g) }
    }

    /// The drawn group a session is in.
    private func group(of id: String) -> SessionGroup? {
        drawnGroups.first { g in g.sessions.contains { $0.id == id } }
    }

    /// Whether the held card can land on target: another card of its group,
    /// or one across the Parked group's edge. There it is parked, or comes
    /// back into its own group unparked, so in the Repo view only among its
    /// repo's cards.
    func canDrop(onto target: Session) -> Bool {
        guard let id = dragging, id != target.id, var held = sessions.first(where: { $0.id == id }) else { return false }
        guard held.isParked == drawnParked(target) || canPark else { return false }
        held.parked = drawnParked(target)
        return groupID(of: held) == groupID(of: target)
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
    /// Across the Parked group's edge it is parked or unparked as well.
    func reorder(_ id: String, onto target: Session) {
        guard let from = sessions.firstIndex(where: { $0.id == id }),
              let to = sessions.firstIndex(where: { $0.id == target.id }), from != to else { return }
        setParked(sessions[from], drawnParked(target))
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
            Image(systemName: frozen ? "play.fill" : "pause.fill").frame(width: 16, height: 18)
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
            Image(systemName: on ? "arrow.up.circle.fill" : "arrow.up.circle").frame(width: 16, height: 18)
        }
        .foregroundStyle(ink ? Theme.ink : on ? Theme.accentInk : Theme.muted)
        .help(on ? "Promoted: click to put it back among the rest" : "Promote: put it first, with the CPU and disk first")
        .accessibilityLabel(on ? "Stop promoting" : "Promote")
    }
}

/// Park, or Unpark, as an icon: on a hovered card and in the details panel.
struct ParkButton: View {
    @EnvironmentObject private var store: Store
    let session: Session
    /// Sized as the hovered card's other buttons are.
    var onCard = false

    var body: some View {
        let on = session.isParked
        Button { store.setParked(session, !on, fromPointer: onCard) } label: {
            Image(systemName: on ? "parkingsign.circle.fill" : "parkingsign.circle")
                .frame(width: onCard ? 16 : nil, height: onCard ? 18 : nil)
        }
        .help(on ? "Unpark: put it back in its place in the list" : "Park: put it out of the way, in Parked at the end of the list")
        .accessibilityLabel(on ? "Unpark" : "Park")
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
        if store.canPark {
            Button(session.isParked ? "Unpark" : "Park") { store.setParked(session, !session.isParked, fromPointer: true) }
        }
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

/// The trod bot's boot print on a card it sent on (DESIGN §4.10): how often
/// in this shift, bright for a while after each time, and a hop each time
/// it does, so you can watch it work.
struct TrodPrint: View {
    let trod: TrodRow
    let now: Date

    /// How long a boot print stays bright after the bot sent the session on.
    static let fresh: TimeInterval = 120

    private var bright: Bool { now.timeIntervalSince(trod.last) < Self.fresh }

    var body: some View {
        HStack(spacing: 1) {
            Image(systemName: "shoeprints.fill")
                .symbolEffect(.bounce, value: trod.last)
            if trod.sent > 1 {
                Text("\(trod.sent)").monospacedDigit()
            }
        }
        .font(.system(size: 10.5, weight: .bold))
        .foregroundStyle(bright ? Theme.accentInk : Theme.muted)
        .help(words)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(words)
    }

    private var words: String {
        let why: String
        switch trod.reason {
        case "question": why = "it asked you something, so the bot told it to decide"
        case "error": why = "an API error had stopped its turn"
        case "limit": why = "the usage limit that stopped it had reset"
        case "loose": why = "its work wasn’t through yet"
        default: why = trod.reason
        }
        let times = trod.sent == 1 ? "once" : "\(trod.sent) times"
        return "The trod bot sent it on \(times) this shift, last at \(trod.last.formatted(date: .omitted, time: .shortened)): \(why)."
    }
}

/// What the details panel says about a frozen, promoted or parked session,
/// in short lines with the whole story on hover: plainly what Irie does, and
/// that memory is not something it can set aside.
struct ControlLines: View {
    let session: Session

    var body: some View {
        if session.isParked {
            lineText("Parked: out of the way, still running",
                     more: "It waits in Parked at the end of the list and keeps running, and it still turns red when it needs you. Unpark puts it back in its place.")
        }
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

/// Lets a card be dragged to another place in its group, or across the
/// Parked group's edge, with a line where it would land.
struct Reorderable: ViewModifier {
    @EnvironmentObject private var store: Store
    let session: Session

    func body(content: Content) -> some View {
        content
            .onDrag {
                store.settle()
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
