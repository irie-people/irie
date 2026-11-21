import AppKit
import SwiftUI

// Drags in the session list (DESIGN §4.1; docs/MAC.md, "Drag to reorder"
// and "Your groups"). A card drags, with every card picked with it, and a
// group of yours drags by its header, as a chip with its name and count.
// One drop target lies over the whole list, in place of one on each card
// and header, so what a drag holds can land anywhere in the list and the
// list always shows where (DropModel.swift): a line, headed by a dot,
// between the two rows it would go between, drawn as far in as the group it
// would join; or the header of the group it would go inside, lit, with the
// group's rail. What is held fades meanwhile, so what moves shows. Held
// over a folded group's header for a moment, the group springs open, as a
// folder does in Finder, so the drop can aim inside it; held near the
// list's edge, the list scrolls (DragScroll.swift).
//
// Each card and header tells the list where it is (dropRow). The list's
// drop delegate (ListDrop) has the store aim the hold where the pointer is
// (Store.aim), and the store performs the drop (Store.perform): one
// reorder, pin, unpin, park, unpark or group move call to the core, which
// keeps the one order and the groups, and one step that ⌘Z takes back. A
// drop shows at once, ahead of the core's frames, which then confirm it.

/// Where a row of the list is, for the drop over the list.
struct DropRowFrame {
    let id: String
    let bounds: Anchor<CGRect>
}

struct DropRowFrames: PreferenceKey {
    static var defaultValue: [DropRowFrame] { [] }
    static func reduce(value: inout [DropRowFrame], nextValue: () -> [DropRowFrame]) { value += nextValue() }
}

/// The list's layout that the drop's line follows: its side inset, and
/// each group's step further in (GroupTree). A group's rail runs down at
/// `rail`, under its header's chevron, which the header sets that far in,
/// so its cards step in past it.
enum ListLayout {
    static let inset: CGFloat = 10
    static let indent: CGFloat = 14
    static let rail: CGFloat = 9
}

extension View {
    /// Tells the list where this row is, for drops: a card by its session's
    /// id, a header by its group's, the strip under the list as
    /// DropModel.end.
    func dropRow(_ id: String) -> some View {
        anchorPreference(key: DropRowFrames.self, value: .bounds) { [DropRowFrame(id: id, bounds: $0)] }
    }
}

/// The drop over the whole list: it aims the hold where the pointer is,
/// and performs the drop there.
@MainActor
struct ListDrop: DropDelegate {
    let store: Store

    func validateDrop(info: DropInfo) -> Bool { store.heldText != nil }

    func dropEntered(info: DropInfo) { store.aim(at: info.location) }

    func dropUpdated(info: DropInfo) -> DropProposal? {
        DropProposal(operation: store.aim(at: info.location) ? .move : .forbidden)
    }

    func dropExited(info: DropInfo) { store.aim(at: nil) }

    func performDrop(info: DropInfo) -> Bool {
        store.aim(at: info.location)
        guard let l = store.drop else {
            store.endDrag()
            return false
        }
        // What the drag holds, kept here: the hold may end before the
        // dropped item has been read.
        let group = store.draggingGroup, card = store.dragging, held = store.heldCards
        return store.carrying(info.itemProviders(for: [.text])) { self.store.perform(l, group: group, card: card, held: held) }
    }
}

/// Over the list: the line where what a drag holds would land, with a dot
/// at its head, as far in as the group it would join. It keeps the rows'
/// frames for the drop (ListDrop), and takes no clicks.
@MainActor
struct DropLine: View {
    @Environment(Store.self) private var store
    let frames: [DropRowFrame]

    var body: some View {
        GeometryReader { proxy in
            // A card's helpers under it count with the card.
            let _ = store.noteFrames(Dictionary(frames.map { ($0.id, proxy[$0.bounds]) }, uniquingKeysWith: { $0.union($1) }))
            if let l = store.drop, let p = l.line, !l.same {
                let dot: CGFloat = 7
                let right = proxy.size.width - ListLayout.inset
                let from = p.x + dot + 3
                Circle().fill(Theme.accent)
                    .frame(width: dot, height: dot)
                    .position(x: p.x + dot / 2, y: p.y)
                if right > from {
                    Capsule().fill(Theme.accent)
                        .frame(width: right - from, height: 3)
                        .position(x: (from + right) / 2, y: p.y)
                }
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

/// A group of yours as it drags: a chip with its folder, name and count.
struct GroupChip: View {
    let name: String
    let count: Int

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "folder.fill")
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(Theme.muted)
            Text(name)
                .font(Theme.font(12, .heavy))
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
            Text("\(count)")
                .font(Theme.font(11.5, .bold)).monospacedDigit()
                .foregroundStyle(Theme.muted)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .background(Capsule().fill(Theme.card))
        .overlay(Capsule().strokeBorder(Theme.accent, lineWidth: 1))
    }
}

extension Store {
    // MARK: Drags

    /// Takes hold of card s, and every card picked with it, for a drag:
    /// what the drop carries.
    func beginDrag(card s: Session) -> NSItemProvider {
        heldCards = Set(targets(for: s).filter { $0.peerName == nil }.map(\.id))
        dragging = s.id
        draggingGroup = nil
        startDrag()
        return NSItemProvider(object: s.id as NSString)
    }

    /// Takes hold of group id by its header.
    func beginDrag(group id: String) -> NSItemProvider {
        heldCards = []
        dragging = nil
        draggingGroup = id
        startDrag()
        return NSItemProvider(object: ("group:" + id) as NSString)
    }

    private func startDrag() {
        drop = nil
        dragStart = NSEvent.mouseLocation
        dragGen += 1
        let gen = dragGen
        // Held near the list's edge, it scrolls the list (DragScroll.swift).
        NotificationCenter.default.post(name: .irieDragBegan, object: nil)
        // A drag let go elsewhere, or ended with Escape, drops nothing
        // here: the hold ends once the button is up. A drop here ends it
        // itself first, and a newer drag is left alone.
        Task { @MainActor in
            while NSEvent.pressedMouseButtons & 1 != 0 { try? await Task.sleep(for: .milliseconds(50)) }
            try? await Task.sleep(for: .seconds(1))
            if dragGen == gen { endDrag() }
        }
    }

    /// Lets go of what a drag held.
    func endDrag() {
        dragging = nil
        draggingGroup = nil
        heldCards = []
        drop = nil
        dragStart = nil
        spring?.cancel()
        spring = nil
    }

    /// The rows' frames in the list, from its overlay (DropLine).
    func noteFrames(_ frames: [String: CGRect]) { dropFrames = frames }

    /// Opens group id, by its sidebar id: Parked, or one of yours.
    func unfold(_ id: String) {
        if id == SessionGroup.parked {
            guard !parkedOpen else { return }
            parkedOpen = true
            UserDefaults.standard.set(true, forKey: "parkedOpen")
            return
        }
        guard folded.contains(id) else { return }
        folded.remove(id)
        UserDefaults.standard.set(Array(folded).sorted(), forKey: "foldedGroups")
    }

    /// What the drag holds, for the model; nil for nothing, or for a
    /// paired machine's card, which moves only there.
    private var hold: DropModel.Hold? {
        if let g = draggingGroup { return .group(g) }
        guard let id = dragging, let s = sessions.first(where: { $0.id == id }), s.peerName == nil else { return nil }
        return .cards(Array(heldCards), pinned: s.isPinned, parked: s.isParked, group: s.groupKey, repo: "repo:" + (s.repo ?? ""))
    }

    /// Your groups, each with the group it is in.
    private var dropTree: DropModel.Tree {
        DropModel.Tree(parent: Dictionary(groups.map { ($0.id, $0.parentKey ?? "") }, uniquingKeysWith: { a, _ in a }))
    }

    /// The list's rows as the sidebar draws them now, in order.
    func dropRows() -> [DropModel.Row] {
        var out: [DropModel.Row] = []
        let gs = drawnGroups
        let lastList = canGroup && mode == .list ? gs.last(where: { $0.bare || $0.userGroup != nil })?.id : nil
        func walk(_ node: GroupNode, _ chain: [String]) {
            for item in node.items {
                switch item {
                case let .card(s):
                    out.append(DropModel.Row(id: s.id, kind: .card, section: DropModel.list, chain: chain, sessions: [s.id]))
                case let .group(sub):
                    let g = SessionGroup.of(sub), open = isOpen(g)
                    out.append(DropModel.Row(id: g.id, kind: .header, section: DropModel.list, chain: open ? chain + [sub.id] : chain,
                                             group: sub.id, open: open, sessions: sub.sessions.map(\.id)))
                    if open { walk(sub, chain + [sub.id]) }
                }
            }
        }
        for g in gs {
            let section = g.bare || g.userGroup != nil ? DropModel.list : g.id
            let open = isOpen(g)
            if let node = g.node {
                out.append(DropModel.Row(id: g.id, kind: .header, section: section, chain: open ? [node.id] : [],
                                         group: node.id, open: open, sessions: g.sessions.map(\.id)))
                if open { walk(node, [node.id]) }
            } else {
                if !g.bare {
                    out.append(DropModel.Row(id: g.id, kind: .header, section: section, open: open, sessions: g.sessions.map(\.id)))
                }
                if open {
                    for s in g.sessions {
                        out.append(DropModel.Row(id: s.id, kind: .card, section: section, sessions: [s.id], peer: s.peerName != nil))
                    }
                }
            }
            if g.id == lastList { out.append(DropModel.Row(id: DropModel.end, kind: .end, section: DropModel.list)) }
        }
        return out
    }

    /// Where what the drag holds would land with the pointer at p in the
    /// list; nil where it can't.
    func landing(at p: CGPoint) -> DropModel.Landing? {
        guard let hold else { return nil }
        let travel = dragStart.map { NSEvent.mouseLocation.x - $0.x } ?? 0
        return DropModel.landing(at: p, travel: travel, rows: dropRows(), frames: dropFrames, hold: hold, tree: dropTree,
                                 rules: DropModel.Rules(canGroup: canGroup, canPin: canPin, canPark: canPark, listView: mode == .list),
                                 inset: ListLayout.inset, indent: ListLayout.indent)
    }

    /// Aims the drag at p in the list, or off it for nil: the list shows
    /// where the hold would land, and a folded group under the pointer
    /// springs open after a moment, as a folder does in Finder, with the
    /// drop then aimed first in it. It reports whether the hold can land
    /// there.
    @discardableResult
    func aim(at p: CGPoint?) -> Bool {
        let l = p.flatMap(landing(at:))
        guard l != drop else { return l != nil }
        drop = l
        spring?.cancel()
        spring = nil
        guard let l, let lit = l.lit, l.line == nil, let p else { return l != nil }
        spring = Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(600))
            guard !Task.isCancelled, drop == l else { return }
            unfold(lit)
            // Once the open group's rows are laid out, aim again.
            try? await Task.sleep(for: .milliseconds(120))
            guard !Task.isCancelled, drop == l else { return }
            drop = landing(at: p)
        }
        return true
    }

    /// Performs the drop where the drag aims: what it held, group id or
    /// card id with the cards held with it, goes there.
    func perform(_ l: DropModel.Landing, group: String?, card: String?, held: Set<String>) {
        guard !l.same else { return }
        // Out of sight, into a folded group, a notice says where it went.
        let hidden = l.lit != nil && l.line == nil
        if let g = group {
            guard case let .list(into, before, after) = l.place else { return }
            moveGroup(g, into: into, before: before, after: after, notice: hidden)
            return
        }
        guard let card, sessions.contains(where: { $0.id == card }) else { return }
        var cards = drawnGroups.flatMap(\.sessions).filter { held.contains($0.id) }
        if cards.isEmpty { cards = sessions.filter { held.contains($0.id) } }
        guard !cards.isEmpty else { return }
        pickNone()
        switch l.place {
        case let .pinned(before):
            place(cards, before: before) { $0.isPinned ? .reorder : .pin(true) }
        case let .parked(before):
            place(cards, before: before) { $0.isParked ? .reorder : .park(true) }
        case let .repo(before):
            place(cards, before: before) { $0.isPinned ? .pin(false) : $0.isParked ? .park(false) : .reorder }
        case let .list(into, before, after):
            if canGroup, cards.contains(where: { $0.groupKey != into }) {
                moveCards(cards, into: into, before: before, after: after, notice: hidden)
            } else {
                place(cards, before: before ?? following(after, in: into, passing: held)) { $0.isPinned ? .pin(false) : $0.isParked ? .park(false) : .reorder }
            }
        }
    }

    /// What placing a card takes: a reorder, or a pin or park, on or off.
    private enum Placing {
        case reorder
        case pin(Bool)
        case park(Bool)
    }

    /// Puts cards, in their order, just before session before, or last,
    /// each by what op says it takes. They show there at once; should the
    /// core refuse one, the cards go back as they were.
    private func place(_ cards: [Session], before: String?, _ op: (Session) -> Placing) {
        let was = sessions
        let ids = Set(cards.map(\.id))
        let ops = cards.map(op)
        preview { rows in
            var moved = rows.filter { ids.contains($0.id) }
            rows.removeAll { ids.contains($0.id) }
            for i in moved.indices {
                switch op(moved[i]) {
                case let .pin(on):
                    moved[i].pinned = on
                    if on { moved[i].parked = false }
                case let .park(on):
                    moved[i].parked = on
                    if on { moved[i].pinned = false }
                case .reorder:
                    break
                }
            }
            let at = before.flatMap { b in rows.firstIndex { $0.id == b } }
                ?? rows.lastIndex(where: { $0.peerName == nil }).map { $0 + 1 } ?? rows.count
            rows.insert(contentsOf: moved, at: at)
            for j in rows.indices { rows[j].order = j }
        }
        Task {
            for (c, op) in zip(cards, ops) {
                switch op {
                case .reorder:
                    do {
                        _ = try await client.call("reorder", ReorderArgs(session: c.id, before: before), as: Empty.self)
                    } catch {
                        preview { rows in
                            for i in rows.indices {
                                if let old = was.first(where: { $0.id == rows[i].id }) { rows[i].order = old.order }
                            }
                        }
                        say("Couldn’t move \(c.name): \(error.localizedDescription)", error: true)
                    }
                case let .pin(on):
                    await pin(c, on, before: before, was: was)
                case let .park(on):
                    await park(c, on, before: before, was: was)
                }
            }
        }
    }

    /// The session just after what of group into, nil for the top of the
    /// list, holds session after, in the list's order, passing over the
    /// sessions passing, which the drag holds: what a reorder lands before.
    /// Nil for none, which puts it last.
    private func following(_ after: String?, in into: String?, passing: Set<String>) -> String? {
        guard let after, let i = sessions.firstIndex(where: { $0.id == after }) else { return nil }
        var j = i
        if let child = child(of: sessions[i], in: into) {
            while j + 1 < sessions.count, isWithin(sessions[j + 1].groupKey, child) { j += 1 }
        }
        while j + 1 < sessions.count, passing.contains(sessions[j + 1].id) { j += 1 }
        return j + 1 < sessions.count ? sessions[j + 1].id : nil
    }

    /// The group of into's, nil for the top of the list, that holds s: nil
    /// when s is in into itself, or in none of its groups.
    func child(of s: Session, in into: String?) -> String? {
        let path = groupPath(s.groupKey).map(\.id)
        guard let into else { return path.first }
        guard let k = path.firstIndex(of: into), k + 1 < path.count else { return nil }
        return path[k + 1]
    }

    /// Shows a move or a join at once, ahead of the core: the sessions, or
    /// the groups' sessions, go where the drop put them, in group into, the
    /// groups themselves move with them, and a card that joins comes out
    /// of Pinned or Parked, as the core will have it.
    func previewPlace(_ a: GroupArgs) {
        let into = a.action == "add" ? a.group : a.into.flatMap { $0.isEmpty ? nil : $0 }
        var ids = a.sessions ?? []
        // A group's pinned or parked cards wait in Pinned or Parked, and
        // stay there.
        for g in a.groups ?? [] {
            ids += sessions.filter { $0.peerName == nil && !$0.isPinned && !$0.isParked && isWithin($0.groupKey, g) }.map(\.id)
        }
        let moving = Set(ids), joining = Set(a.sessions ?? [])
        guard !moving.isEmpty else { return }
        preview { rows in
            var moved = rows.filter { moving.contains($0.id) }
            rows.removeAll { moving.contains($0.id) }
            for i in moved.indices where joining.contains(moved[i].id) {
                moved[i].group = into
                moved[i].pinned = false
                moved[i].parked = false
            }
            // Where the core's spot puts them (groups.go): just before what
            // of into's holds before, else just after what holds after,
            // else last in into, else last.
            var at: Int?
            if let b = a.before, let i = rows.firstIndex(where: { $0.id == b }), into == nil || self.isWithin(rows[i].groupKey, into!) {
                at = self.child(of: rows[i], in: into).flatMap { c in rows.firstIndex { self.isWithin($0.groupKey, c) } } ?? i
            } else if let af = a.after, let i = rows.firstIndex(where: { $0.id == af }), into == nil || self.isWithin(rows[i].groupKey, into!) {
                at = (self.child(of: rows[i], in: into).flatMap { c in rows.lastIndex { self.isWithin($0.groupKey, c) } } ?? i) + 1
            } else if let into, let i = rows.lastIndex(where: { self.isWithin($0.groupKey, into) }) {
                at = i + 1
            }
            let idx = at ?? rows.lastIndex(where: { $0.peerName == nil }).map { $0 + 1 } ?? rows.count
            rows.insert(contentsOf: moved, at: idx)
            for j in rows.indices { rows[j].order = j }
        }
        for g in a.groups ?? [] {
            if let i = groups.firstIndex(where: { $0.id == g }) { groups[i].parent = into ?? "" }
        }
    }
}
