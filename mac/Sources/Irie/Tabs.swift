import AppKit
import SwiftUI

// Your own ways with the cards (DESIGN §4.1): pin one to the top, close
// several at once, pick several with ⌘ or ⇧ and act on them together, move
// one with the keys, and close one from under the pointer. Pinned is
// Parked's opposite: the Pinned group heads the list, and no close reaches a
// pinned card but your own of it alone. Every close of several is one
// `close` with `sessions`, one step of the history, so ⌘Z brings them all
// back.

extension Session {
    var isPinned: Bool { pinned ?? false }
}

extension Store {
    // MARK: Pin

    /// Pins a session, last in the Pinned group at the top, or unpins it,
    /// first among the rest, just under Pinned, as the core does (pin.go).
    /// The card moves at once, and a notice says where it went, with Undo.
    func setPinned(_ s: Session, _ on: Bool) {
        guard canPin, s.peerName == nil, s.isPinned != on else { return }
        let was = sessions
        withAnimation(.easeOut(duration: 0.18)) {
            preview { rows in Self.putPinned(&rows, s.id, on) }
        }
        Task { await pin(s, on, was: was) }
    }

    /// Asks the core to pin or unpin s, just before before when a drag put
    /// it there, and puts the rows back as they were if it can't.
    func pin(_ s: Session, _ on: Bool, before: String? = nil, was: [Session]) async {
        do {
            _ = try await client.call(on ? "pin" : "unpin", ParkArgs(session: s.id, before: before), as: Empty.self)
            say(on ? "Pinned \(s.name): it stays at the top, and no close but yours of it alone reaches it." : "Unpinned \(s.name).",
                error: false, seconds: 6, undo: true)
        } catch {
            preview { rows in
                for i in rows.indices {
                    if let old = was.first(where: { $0.id == rows[i].id }) {
                        rows[i].pinned = old.pinned
                        rows[i].parked = old.parked
                        rows[i].order = old.order
                    }
                }
            }
            say("Couldn’t \(on ? "pin" : "unpin") \(s.name): \(error.localizedDescription)", error: true)
        }
    }

    /// Pins (on) or unpins session id in rows, in the list's order, as the
    /// core does: a pin goes last, and unparks; an unpin goes first among
    /// this Mac's sessions that are neither pinned nor parked.
    static func putPinned(_ rows: inout [Session], _ id: String, _ on: Bool) {
        guard let i = rows.firstIndex(where: { $0.id == id }) else { return }
        var s = rows.remove(at: i)
        s.pinned = on
        if on { s.parked = false }
        let at = on ? rows.count
            : rows.firstIndex(where: { !$0.isPinned && !$0.isParked && $0.peerName == nil }) ?? rows.count
        rows.insert(s, at: at)
        for j in rows.indices { rows[j].order = j }
    }

    // MARK: Close many

    /// Every card but s that Close Others closes: this Mac's, neither
    /// pinned nor parked, in the drawn order.
    func others(than s: Session) -> [Session] {
        drawnGroups.filter { $0.id != SessionGroup.pinned && $0.id != SessionGroup.parked && $0.host == nil }
            .flatMap(\.sessions)
            .filter { $0.id != s.id && !$0.isPinned && !$0.isParked && $0.peerName == nil }
    }

    /// The cards below s in its group, which Close Below closes; none for a
    /// pinned card, or another machine's.
    func below(_ s: Session) -> [Session] {
        guard !s.isPinned, s.peerName == nil,
              let g = drawnGroups.first(where: { g in g.sessions.contains { $0.id == s.id } }),
              let i = g.sessions.firstIndex(where: { $0.id == s.id }) else { return [] }
        return Array(g.sessions[(i + 1)...]).filter { !$0.isPinned && $0.peerName == nil }
    }

    /// The cards a group header's Close Group closes: none for Pinned's or
    /// a paired machine's.
    func closable(in g: SessionGroup) -> [Session] {
        guard g.id != SessionGroup.pinned, g.host == nil else { return [] }
        return g.sessions.filter { !$0.isPinned && $0.peerName == nil }
    }

    /// Closes several sessions as one step of the history: the core closes
    /// them in one `close`, and ⌘Z brings them all back. When the shown one
    /// goes, the next card left takes its place, or the one before it.
    func closeMany(_ list: [Session]) {
        guard !list.isEmpty else {
            say("Nothing to close there. Pinned and parked cards stay.", error: false)
            return
        }
        if list.count == 1 {
            close(list[0])
            return
        }
        let ids = Set(list.map(\.id))
        showNext(after: ids)
        pickNone()
        Task {
            do {
                let r = try await client.call("close", CloseArgs(sessions: list.map(\.id)), as: CloseResult.self)
                say("Closed \(r.sessions.count) sessions. ⌘Z brings them all back.", error: false, seconds: 6, undo: true)
            } catch {
                say("Couldn’t close them all: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Close All Finished: the core closes every session whose work is
    /// through, but the pinned, the parked and another machine's.
    func closeFinished() {
        Task {
            do {
                let r = try await client.call("close", CloseArgs(finished: true), as: CloseResult.self)
                if r.sessions.isEmpty {
                    say("Nothing has finished to close. Pinned and parked sessions stay.", error: false)
                    return
                }
                if let id = selection, r.sessions.contains(id) { showNext(after: Set(r.sessions)) }
                say("Closed \(r.sessions.count) finished \(r.sessions.count == 1 ? "session" : "sessions"). ⌘Z brings them back.",
                    error: false, seconds: 6, undo: true)
            } catch {
                say("Couldn’t close the finished sessions: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// When the shown session is among ids, shows the next drawn card that
    /// isn't, or the one before it, as when a tab closes.
    private func showNext(after ids: Set<String>) {
        guard let id = selection, ids.contains(id) else { return }
        let order = drawnOrder
        guard let i = order.firstIndex(where: { $0.id == id }) else { return }
        if let next = order[i...].first(where: { !ids.contains($0.id) }) ?? order[..<i].last(where: { !ids.contains($0.id) }) {
            show(next.id)
        } else {
            selection = nil
        }
    }

    /// ⌘W: the cards picked, when more than one is, else the selected card.
    func closeKey() {
        let p = pickedSessions
        if p.count > 1 {
            closeMany(p)
        } else if let s = closable {
            close(s)
        } else {
            NSSound.beep()
        }
    }

    // MARK: Pick several

    /// The cards picked with ⌘ or ⇧, in the drawn order; empty while one
    /// card is simply selected.
    var pickedSessions: [Session] {
        picked.isEmpty ? [] : drawnGroups.flatMap(\.sessions).filter { picked.contains($0.id) }
    }

    /// The cards an action from s's menu acts on: every card picked, when s
    /// is one of several picked, else s alone.
    func targets(for s: Session) -> [Session] {
        let p = pickedSessions
        return p.count > 1 && p.contains(where: { $0.id == s.id }) ? p : [s]
    }

    /// A click on s with ⌘ (adding) or ⇧ (range) held: ⌘ picks it, or puts
    /// it back, and ⇧ picks every card from the one picked last, or the one
    /// shown, to it. The shown session stays shown. With one card or none
    /// left picked, it is a plain selection again.
    func pick(_ s: Session, range: Bool) {
        if picked.isEmpty, let sel = selection, outsideSelection == nil { picked = [sel] }
        let order = drawnOrder.map(\.id)
        if range, let a = (pickAnchor ?? selection).flatMap({ order.firstIndex(of: $0) }), let b = order.firstIndex(of: s.id) {
            picked.formUnion(order[min(a, b)...max(a, b)])
        } else if picked.contains(s.id) {
            picked.remove(s.id)
        } else {
            picked.insert(s.id)
        }
        pickAnchor = s.id
        if picked.count < 2 { pickNone() }
    }

    /// Puts the cards picked down: one card is selected again.
    func pickNone() {
        if !picked.isEmpty { picked = [] }
        pickAnchor = nil
    }

    /// Parks or pins every card in list, each as its own step.
    func setParked(_ list: [Session], _ on: Bool) {
        for s in list where s.isParked != on { setParked(s, on) }
        pickNone()
    }

    func setPinned(_ list: [Session], _ on: Bool) {
        for s in list where s.isPinned != on { setPinned(s, on) }
        pickNone()
    }

    // MARK: Move

    /// Moves a session to the top (true) or the bottom of its group, without
    /// promoting it, as a drag there would.
    func moveToEdge(_ s: Session, top: Bool) {
        guard let g = drawnGroups.first(where: { g in g.sessions.contains { $0.id == s.id } }),
              let target = top ? g.sessions.first : g.sessions.last, target.id != s.id else { return }
        reorder(s.id, onto: target)
    }

    /// The selected card, for the Move keys.
    func moveSelected(by delta: Int) {
        guard let s = closable else { return }
        move(s, by: delta)
    }

    func moveSelectedToEdge(top: Bool) {
        guard let s = closable else { return }
        moveToEdge(s, top: top)
    }
}

extension Store {
    // MARK: Sub tabs

    /// Hides a helper (its agent, sub:<id>) or a teammate ("team:" and its
    /// session) from its card: it keeps working, and the card's "hidden"
    /// line brings it back.
    func hideSub(_ session: String, _ key: String) {
        hiddenSubs[session, default: []].insert(key)
    }

    /// Brings back the helpers and teammates hidden from a card.
    func showSubs(_ session: String) {
        hiddenSubs[session] = nil
    }

    /// Stop on a helper's or teammate's row: the core turns the helper's
    /// tool calls away with word to end at once, or ends the teammate's
    /// terminal.
    func stopSub(_ s: Session, agent: String? = nil, teammate: String? = nil, name: String) {
        Task {
            do {
                _ = try await client.call("stop_sub", StopSubArgs(session: s.id, agent: agent, teammate: teammate), as: Empty.self)
                say(agent != nil ? "Stopped \(name): it ends at its next step." : "Stopped \(name).", error: false)
            } catch {
                say("Couldn’t stop \(name): \(error.localizedDescription)", error: true)
            }
        }
    }
}

/// The helper or teammate a Stop names.
struct StopSubArgs: Encodable {
    var session: String
    var agent: String?
    var teammate: String?
}

/// A helper's or teammate's row under its card: a × on hover hides it from
/// the card, where it keeps working, and its menu hides it or stops it.
@MainActor
struct SubRow<Content: View>: View {
    @Environment(Store.self) private var store
    let session: Session
    /// The helper's agent, sub:<id>, or "team:" and the teammate's session.
    let key: String
    let name: String
    @ViewBuilder var content: Content
    @State private var hovering = false

    private var teammate: String? { key.hasPrefix("team:") ? String(key.dropFirst(5)) : nil }

    var body: some View {
        HStack(spacing: 2) {
            content
            Button { store.hideSub(session.id, key) } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 8, weight: .heavy))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 14, height: 14)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .opacity(hovering ? 1 : 0)
            .allowsHitTesting(hovering)
            .help("Hide \(name) from the card. It keeps working; right-click it to stop it.")
            .accessibilityLabel("Hide \(name)")
        }
        .onHover { hovering = $0 }
        .contextMenu {
            Button("Hide From the Card") { store.hideSub(session.id, key) }
            if session.peerName == nil {
                Button(teammate == nil ? "Stop Helper" : "Stop Teammate") {
                    if let t = teammate {
                        store.stopSub(session, teammate: t, name: name)
                    } else {
                        store.stopSub(session, agent: key, name: name)
                    }
                }
            }
        }
    }
}

/// What a close of several returns: the sessions it closed.
struct CloseResult: Decodable {
    var sessions: [String]
}

/// The × that takes a card's mark while the pointer is on it, with
/// Settings › Tabs' "Close from under the pointer" on.
@MainActor
struct CloseMark: View {
    @Environment(Store.self) private var store
    let session: Session

    var body: some View {
        Button { store.close(session) } label: {
            Image(systemName: "xmark")
                .font(.system(size: 8.5, weight: .heavy))
                .foregroundStyle(Theme.muted)
                .frame(width: 15, height: 15)
                .background(Circle().fill(Theme.hover))
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .help("Close \(session.name) (⌘W). ⌘Z or ⇧⌘T brings it back.")
        .accessibilityLabel("Close \(session.name)")
    }
}

/// Settings › Tabs' switch for the × on a hovered card and the middle click.
enum CloseFromPointer {
    static let defaultsKey = "closeOnHover"
    static var on: Bool { UserDefaults.standard.object(forKey: defaultsKey) as? Bool ?? true }
}

/// A card's menu while several cards are picked: what acts on them all.
@MainActor
struct PickedActions: View {
    @Environment(Store.self) private var store
    let list: [Session]

    var body: some View {
        let n = list.count
        Button("Close \(n) Sessions") { store.closeMany(list) }
        if store.canPark {
            if list.contains(where: { !$0.isParked }) {
                Button("Park \(n) Sessions") { store.setParked(list, true) }
            } else {
                Button("Unpark \(n) Sessions") { store.setParked(list, false) }
            }
        }
        if store.canPin, list.allSatisfy({ $0.peerName == nil }) {
            if list.contains(where: { !$0.isPinned }) {
                Button("Pin \(n) Sessions") { store.setPinned(list, true) }
            } else {
                Button("Unpin \(n) Sessions") { store.setPinned(list, false) }
            }
        }
        Divider()
        Button("Deselect") { store.pickNone() }
    }
}

/// A card's close items: Close Others, Close Below and Close All Finished.
/// They read the rows only once chosen, so a card's menu never draws again
/// with every row's news.
@MainActor
struct CloseManyItems: View {
    @Environment(Store.self) private var store
    let session: Session

    var body: some View {
        Button("Close Others") { store.closeMany(store.others(than: session)) }
        Button("Close Below") { store.closeMany(store.below(session)) }
            .disabled(session.isPinned || session.peerName != nil)
        Button("Close All Finished") { store.closeFinished() }
    }
}

/// A group header's menu: fold it, and close its cards.
@MainActor
struct GroupActions: View {
    @Environment(Store.self) private var store
    let group: SessionGroup

    var body: some View {
        Button(store.isOpen(group) ? "Fold" : "Open") { store.fold(group) }
        let list = store.closable(in: group)
        if !list.isEmpty {
            Divider()
            Button(list.count == 1 ? "Close Its Session" : "Close Its \(list.count) Sessions") { store.closeMany(list) }
        }
    }
}
