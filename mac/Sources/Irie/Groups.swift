import AppKit
import SwiftUI

// Your groups (DESIGN §4.1): named sets of cards drawn together under a
// header that folds them, like a browser's tab groups, with a rail down the
// side of their cards; and groups inside groups, as folders hold folders,
// each a step further in with its own rail. A card's menu has New Group,
// which makes a group of it, or of every card picked with it, inside the
// group that holds them all, and opens the group's name to edit; Move to
// Group, which lists every group as a tree; and Move Out, one group out. A
// header's menu renames the group, folds it, puts it in a new group, moves
// it into another group or out of the one around it, ungroups it, which
// leaves what it held where it is, in the group around it, or closes its
// cards. A group's header drags: dropped on another group's header it goes
// inside that group, on a header's top edge just before it, on a card in
// that card's place, and on the strip under the list at the top of the
// list. A card dropped on a card of a group, or on its header, joins the
// group, and one dropped among the cards of no group leaves it. The List
// view draws each group where the first of its cards is in the core's one
// order, its groups' cards too; the Repo view groups the cards by repo
// instead. Groups are the core's (internal/core, groups.go), so the
// terminal sidebar draws them too, and every change of a card's or a
// group's place is a step that ⌘Z takes back.

/// One of your groups, as the core's header lists it.
struct GroupInfo: Codable, Equatable, Identifiable {
    var id: String
    var name: String
    /// The group it is in; nil or "" at the top of the list.
    var parent: String?

    /// The group it is in, nil at the top.
    var parentKey: String? { parent.flatMap { $0.isEmpty ? nil : $0 } }
}

extension Session {
    /// The group of yours it is in; nil for none.
    var groupKey: String? { group.flatMap { $0.isEmpty ? nil : $0 } }
}

struct GroupArgs: Encodable {
    var action: String?
    var group: String?
    var name: String?
    var sessions: [String]?
    var groups: [String]?
    /// Where move puts them: in this group, "" for the top of the list.
    var into: String?
    /// Where a drop put the cards: just before this card, or just after
    /// that one.
    var before: String?
    var after: String?

    enum CodingKeys: String, CodingKey {
        case action = "do"
        case group, name, sessions, groups, into, before, after
    }
}

struct GroupResult: Decodable {
    var groups: [GroupInfo]
    var group: String?
    var sessions: [String]?
    /// How many steps of the history it took: none when nothing moved, so
    /// ⌘Z has nothing of it to take back.
    var steps: Int?
}

/// One of your groups as the List view draws it: its header, then what it
/// holds in the core's one order, its cards and the groups inside it, each
/// where the first of its cards is.
struct GroupNode: Equatable, Identifiable {
    let id: String
    let name: String
    /// How many groups are around it: 0 at the top of the list.
    let depth: Int
    var items: [GroupItem]
    /// Every card within it, its groups' too, in the order drawn.
    var sessions: [Session]
}

/// What a group holds: a card, or a group inside it.
enum GroupItem: Equatable, Identifiable {
    case card(Session)
    case group(GroupNode)

    var id: String {
        switch self {
        case let .card(s): s.id
        case let .group(g): SessionGroup.userID(g.id)
        }
    }
}

/// Where on a group's header a held card or group would land: just before
/// the group, beside it, or inside it, last.
struct HeaderMark: Equatable {
    var group: String
    var before: Bool
}

/// A group in a menu of groups: the group, and how far in it is.
struct GroupMenuRow: Identifiable {
    let info: GroupInfo
    let depth: Int
    var id: String { info.id }

    /// Its name, indented by its depth.
    var label: String { String(repeating: "\u{2003}", count: depth) + info.name }
}

extension SessionGroup {
    /// The sidebar's id of your group id.
    static func userID(_ id: String) -> String { "group:" + id }

    /// The sidebar's group for node, one of your groups.
    static func of(_ node: GroupNode) -> SessionGroup {
        SessionGroup(id: userID(node.id), title: node.name, path: nil, sessions: node.sessions, userGroup: node.id, node: node)
    }
}

extension Store {
    /// The List view's cards between Pinned and Parked: each of your groups
    /// where the first of its cards is, its groups' cards included, with
    /// its other cards after it and the groups inside it where the first of
    /// theirs is; and the cards of no group between your groups, in runs
    /// drawn without a header.
    func listGroups(_ here: [Session]) -> [SessionGroup] {
        final class Box {
            let id: String
            var items: [Entry] = []
            init(_ id: String) { self.id = id }
        }
        enum Entry {
            case card(Session)
            case box(Box)
        }
        var names: [String: String] = [:]
        for g in groups { names[g.id] = g.name }
        var boxes: [String: Box] = [:], top: [Entry] = []
        for s in here {
            let path = groupPath(s.groupKey)
            guard !path.isEmpty else {
                top.append(.card(s))
                continue
            }
            var parent: Box?
            for g in path {
                if let b = boxes[g.id] {
                    parent = b
                    continue
                }
                let b = Box(g.id)
                boxes[g.id] = b
                if let p = parent { p.items.append(.box(b)) } else { top.append(.box(b)) }
                parent = b
            }
            parent?.items.append(.card(s))
        }
        func node(_ b: Box, depth: Int) -> GroupNode {
            var items: [GroupItem] = [], cards: [Session] = []
            for e in b.items {
                switch e {
                case let .card(s):
                    items.append(.card(s))
                    cards.append(s)
                case let .box(sub):
                    let n = node(sub, depth: depth + 1)
                    items.append(.group(n))
                    cards += n.sessions
                }
            }
            return GroupNode(id: b.id, name: names[b.id] ?? "", depth: depth, items: items, sessions: cards)
        }
        var out: [SessionGroup] = [], run: [Session] = [], runs = 0
        func endRun() {
            guard !run.isEmpty else { return }
            out.append(SessionGroup(id: runs == 0 ? SessionGroup.list : "\(SessionGroup.list):\(runs)", title: "Sessions", path: nil, sessions: run))
            runs += 1
            run = []
        }
        for e in top {
            switch e {
            case let .card(s):
                run.append(s)
            case let .box(b):
                endRun()
                out.append(SessionGroup.of(node(b, depth: 0)))
            }
        }
        endRun()
        return out
    }

    /// Group id and the groups around it, the outermost first; none for nil
    /// or a group the header doesn't list.
    func groupPath(_ id: String?) -> [GroupInfo] {
        var out: [GroupInfo] = [], at = id
        while let g = at, let info = groups.first(where: { $0.id == g }), !out.contains(where: { $0.id == g }) {
            out.insert(info, at: 0)
            at = info.parentKey
        }
        return out
    }

    /// Group id's path of names, the outermost first: "Work › Irie".
    func groupTitle(_ id: String) -> String { groupPath(id).map(\.name).joined(separator: " › ") }

    /// The group group id is in; nil at the top of the list.
    func parentGroup(_ id: String) -> String? { groups.first { $0.id == id }?.parentKey }

    /// Whether group id is group anc or inside it, at any depth; nil, a card
    /// in no group, is inside none.
    func isWithin(_ id: String?, _ anc: String) -> Bool { groupPath(id).contains { $0.id == anc } }

    /// The cards of node the sidebar draws: without those in a folded group
    /// inside it.
    func shown(_ node: GroupNode) -> [Session] {
        node.items.flatMap { item -> [Session] in
            switch item {
            case let .card(s): return [s]
            case let .group(g): return isOpen(SessionGroup.of(g)) ? shown(g) : []
            }
        }
    }

    /// Your groups with a card of this Mac's in the space on show, in the
    /// order the List view draws them, each with the groups inside it just
    /// after it: what Move to Group offers.
    var groupTree: [GroupMenuRow] {
        var out: [GroupMenuRow] = []
        func walk(_ n: GroupNode) {
            if let info = groups.first(where: { $0.id == n.id }) { out.append(GroupMenuRow(info: info, depth: n.depth)) }
            for case let .group(sub) in n.items { walk(sub) }
        }
        let here = sessions.filter { $0.peerName == nil && $0.spaceKey == space && !$0.isParked && !$0.isPinned }
        for g in listGroups(here) { if let n = g.node { walk(n) } }
        // A group whose cards are all pinned or parked is drawn nowhere,
        // and still takes cards.
        let drawn = Set(out.map(\.id))
        let waiting = Set(sessions.filter { $0.peerName == nil && $0.spaceKey == space }.flatMap { groupPath($0.groupKey).map(\.id) })
        for g in groups where waiting.contains(g.id) && !drawn.contains(g.id) {
            out.append(GroupMenuRow(info: g, depth: groupPath(g.id).count - 1))
        }
        return out
    }

    /// A group's name by its id.
    func groupName(_ id: String) -> String { groups.first { $0.id == id }?.name ?? "its group" }

    /// The cards a group action takes: this Mac's.
    private func mine(_ list: [Session]) -> [Session] { list.filter { $0.peerName == nil } }

    /// The drawn group of the List view that holds card id, one of yours at
    /// the top of the list or a run of cards of none.
    private func section(of id: String) -> SessionGroup? {
        drawnGroups.first { g in (g.bare || g.userGroup != nil) && g.sessions.contains { $0.id == id } }
    }

    /// Makes a group of list, a card or every card picked with it, where
    /// the first of them is, inside the group that holds them all, and
    /// opens its name to edit. Your groups show in the List view, so the
    /// sidebar goes there.
    func newGroup(with list: [Session]) {
        let cards = mine(list)
        guard canGroup, !cards.isEmpty else { return }
        pickNone()
        if mode != .list { choose(.list) }
        groupCall(GroupArgs(action: "new", sessions: cards.map(\.id)), failed: "make a group") { r in
            guard let id = r.group else { return }
            self.folded.remove(SessionGroup.userID(id))
            self.renamingGroup = id
            let what = cards.count == 1 ? cards[0].name : "\(cards.count) sessions"
            self.say("Grouped \(what). Type the group’s name; ⌘Z takes it back.", error: false, seconds: 6, undo: true)
        }
    }

    /// File > New Group: the cards picked, or the selected one.
    func newGroupKey() {
        let p = pickedSessions
        newGroup(with: p.count > 1 ? p : closable.map { [$0] } ?? [])
    }

    /// Puts group id in a new group, made where it is, and opens the new
    /// group's name to edit.
    func wrapGroup(_ id: String) {
        guard canGroup else { return }
        let name = groupName(id)
        groupCall(GroupArgs(action: "new", groups: [id]), failed: "make a group around \(name)") { r in
            guard let g = r.group else { return }
            self.folded.remove(SessionGroup.userID(g))
            self.renamingGroup = g
            self.say("Put \(name) in a new group. Type its name; ⌘Z takes it back.", error: false, seconds: 6, undo: true)
        }
    }

    /// Puts list in group id: last in it, or where a drop put them.
    func addToGroup(_ list: [Session], _ id: String, before: String? = nil, after: String? = nil) {
        let cards = mine(list).filter { $0.groupKey != id || $0.isPinned || $0.isParked }
        guard canGroup, !cards.isEmpty else { return }
        pickNone()
        let what = cards.count == 1 ? cards[0].name : "\(cards.count) sessions"
        groupCall(GroupArgs(action: "add", group: id, sessions: cards.map(\.id), before: before, after: after), failed: "move \(what) to the group") { _ in
            if before == nil, after == nil {
                self.say("Moved \(what) to \(self.groupTitle(id)). ⌘Z takes it back.", error: false, seconds: 6, undo: true)
            }
        }
    }

    /// Puts list in group into, nil for the top of the list: just before
    /// what holds card before, or after what holds after, else last.
    func moveCards(_ list: [Session], into: String?, before: String? = nil, after: String? = nil, notice: Bool = true) {
        let cards = mine(list).filter { !($0.groupKey == into && !$0.isPinned && !$0.isParked && before == nil && after == nil) }
        guard canGroup, !cards.isEmpty else { return }
        pickNone()
        let what = cards.count == 1 ? cards[0].name : "\(cards.count) sessions"
        let to = into.map(groupTitle) ?? "the top of the list"
        groupCall(GroupArgs(action: "move", sessions: cards.map(\.id), into: into ?? "", before: before, after: after), failed: "move \(what)") { r in
            guard notice else { return }
            if (r.steps ?? 0) == 0 {
                self.say("\(what) \(cards.count == 1 ? "is" : "are") there already.", error: false)
            } else {
                self.say("Moved \(what) to \(to). ⌘Z takes it back.", error: false, seconds: 6, undo: true)
            }
        }
    }

    /// Puts list at the top of the list, just after the group of yours it
    /// is drawn in.
    func moveToTop(_ list: [Session]) {
        let after = list.first.flatMap { section(of: $0.id) }.flatMap { $0.userGroup == nil ? nil : $0.sessions.last?.id }
        moveCards(list, into: nil, after: after)
    }

    /// Takes list out of their groups, each into the group around its group
    /// or none: just after the group it leaves, or where a drop put them.
    func removeFromGroup(_ list: [Session], before: String? = nil, after: String? = nil) {
        let cards = mine(list).filter { $0.groupKey != nil }
        guard canGroup, !cards.isEmpty else { return }
        pickNone()
        let what = cards.count == 1 ? cards[0].name : "\(cards.count) sessions"
        let from = Set(cards.compactMap(\.groupKey))
        let into = from.count == 1 ? from.first.flatMap(parentGroup) : nil
        groupCall(GroupArgs(action: "remove", sessions: cards.map(\.id), before: before, after: after), failed: "take \(what) out of the group") { _ in
            guard before == nil, after == nil else { return }
            let back = cards.count == 1 ? "it" : "them"
            if let into {
                self.say("Moved \(what) out, into \(self.groupTitle(into)). ⌘Z puts \(back) back.", error: false, seconds: 6, undo: true)
            } else {
                self.say("Took \(what) out of \(cards.count == 1 ? "its group" : "their groups"). ⌘Z puts \(back) back.", error: false, seconds: 6, undo: true)
            }
        }
    }

    /// Puts group id, with what it holds, in group into, nil for the top of
    /// the list: just before what holds card before, or after what holds
    /// after, else last in it.
    func moveGroup(_ id: String, into: String?, before: String? = nil, after: String? = nil) {
        guard canGroup else { return }
        let name = groupName(id)
        let to = into.map(groupTitle) ?? "the top of the list"
        groupCall(GroupArgs(action: "move", groups: [id], into: into ?? "", before: before, after: after), failed: "move \(name)") { r in
            if (r.steps ?? 0) == 0 {
                self.say("\(name) is there already.", error: false)
                return
            }
            if let into { self.folded.remove(SessionGroup.userID(into)) }
            self.say("Moved \(name) \(into == nil ? "to" : "into") \(to). ⌘Z takes it back.", error: false, seconds: 6, undo: true)
        }
    }

    /// Takes group id one group out: into the group around its group, just
    /// after that group.
    func moveOut(_ id: String) {
        guard let p = parentGroup(id) else { return }
        let after = drawnGroups.lazy.compactMap(\.node).compactMap { self.find(p, in: $0) }.first?.sessions.last?.id
        moveGroup(id, into: parentGroup(p), after: after)
    }

    /// Puts group id at the top of the list, just after the group of yours
    /// at the top that holds it.
    func moveGroupToTop(_ id: String) {
        let outer = groupPath(id).first?.id
        let after = outer == id ? nil : drawnGroups.first { $0.userGroup == outer }?.sessions.last?.id
        moveGroup(id, into: nil, after: after)
    }

    /// Group id's node within node, or nil.
    private func find(_ id: String, in node: GroupNode) -> GroupNode? {
        if node.id == id { return node }
        for case let .group(sub) in node.items {
            if let n = find(id, in: sub) { return n }
        }
        return nil
    }

    func renameGroup(_ id: String, _ name: String) {
        if renamingGroup == id { renamingGroup = nil }
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        // A group gone meanwhile, as by ⌘Z, has nothing to rename.
        guard groups.contains(where: { $0.id == id }), !name.isEmpty, name != groupName(id) else { return }
        groupCall(GroupArgs(action: "rename", group: id, name: name), failed: "rename the group") { _ in }
    }

    /// Removes group id; its cards and groups stay where they are, in the
    /// group around it, or none.
    func ungroup(_ id: String) {
        let name = groupName(id)
        let around = parentGroup(id).map { " in \(groupTitle($0))" } ?? ""
        groupCall(GroupArgs(action: "ungroup", group: id), failed: "ungroup \(name)") { _ in
            self.say("Ungrouped \(name): what it held stays where it is\(around). ⌘Z groups it again.",
                     error: false, seconds: 6, undo: true)
        }
    }

    // MARK: Drags

    /// Whether what a drag holds can land on group g's header: just before
    /// it (before), or inside it. A group never goes inside itself.
    func canDropOnHeader(_ g: SessionGroup, before: Bool) -> Bool {
        guard canGroup, mode == .list, let id = g.userGroup else { return false }
        if let held = draggingGroup {
            // Inside the group it is in already, it would stay as it is.
            return held != id && !isWithin(before ? parentGroup(id) : id, held) && (before || parentGroup(held) != id)
        }
        guard let held = dragging, let s = sessions.first(where: { $0.id == held }) else { return false }
        return s.peerName == nil
    }

    /// What a drag holds dropped on group g's header: a card, or every card
    /// picked with it, or a group, goes just before the group, beside it
    /// (before), or last inside it.
    func dropOnHeader(_ g: SessionGroup, before: Bool) -> Bool {
        guard canDropOnHeader(g, before: before), let id = g.userGroup else { return false }
        let first = g.sessions.first?.id
        if let held = draggingGroup {
            if before { moveGroup(held, into: parentGroup(id), before: first) } else { moveGroup(held, into: id) }
            return true
        }
        guard let held = dragging, let s = sessions.first(where: { $0.id == held }) else { return false }
        if before {
            moveCards(targets(for: s), into: parentGroup(id), before: first, notice: false)
        } else {
            addToGroup(targets(for: s), id)
        }
        return true
    }

    /// Whether the held group can land on target, a card of the List view
    /// outside it: there it goes in target's place, in target's group.
    func canDropGroup(onto target: Session) -> Bool {
        guard canGroup, mode == .list, let held = draggingGroup else { return false }
        return target.peerName == nil && !target.isPinned && !target.isParked && !isWithin(target.groupKey, held)
    }

    /// Where the held group lands on target: below it when it comes from
    /// above, and above it when it comes from below.
    func markGroup(onto target: Session) -> DropMark? {
        guard canDropGroup(onto: target), let held = draggingGroup else { return nil }
        return DropMark(target: target.id, below: comesFromAbove(held, target))
    }

    /// The held group dropped on target: it goes in target's group, in
    /// target's place.
    func dropGroup(onto target: Session) -> Bool {
        defer { endDrag() }
        guard canDropGroup(onto: target), let held = draggingGroup else { return false }
        let above = comesFromAbove(held, target)
        moveGroup(held, into: target.groupKey, before: above ? nil : target.id, after: above ? target.id : nil)
        return true
    }

    /// Whether group id's cards are drawn above target.
    private func comesFromAbove(_ id: String, _ target: Session) -> Bool {
        let order = drawnGroups.flatMap(\.sessions)
        guard let t = order.firstIndex(where: { $0.id == target.id }),
              let g = order.firstIndex(where: { isWithin($0.groupKey, id) }) else { return false }
        return g < t
    }

    /// Whether what a drag holds can go to the top of the list, last.
    var canDropAtTop: Bool {
        guard canGroup, mode == .list else { return false }
        if draggingGroup != nil { return true }
        guard let held = dragging, let s = sessions.first(where: { $0.id == held }) else { return false }
        return s.peerName == nil
    }

    /// What a drag holds dropped on the strip under the List view's cards:
    /// it goes last at the top of the list, out of any group.
    func dropAtTop() -> Bool {
        guard canDropAtTop else { return false }
        if let held = draggingGroup {
            moveGroup(held, into: nil)
            return true
        }
        guard let held = dragging, let s = sessions.first(where: { $0.id == held }) else { return false }
        moveCards(targets(for: s), into: nil)
        return true
    }

    /// What a drop carries for what the drag holds: a card's id, or
    /// "group:" and a group's id; nil when it holds nothing.
    var heldText: String? { draggingGroup.map { "group:" + $0 } ?? dragging }

    /// Runs drop once the dropped item proves to be what the drag holds, so
    /// text from elsewhere, after a drag that ended without a drop, moves
    /// nothing. It reports whether the drop can be taken.
    func carrying(_ providers: [NSItemProvider], _ drop: @escaping @MainActor () -> Void) -> Bool {
        guard let held = heldText, let p = providers.first, p.canLoadObject(ofClass: NSString.self) else {
            endDrag()
            return false
        }
        _ = p.loadObject(ofClass: NSString.self) { text, _ in
            let ok = (text as? NSString).map { $0 as String } == held
            Task { @MainActor in
                if ok { drop() }
                self.endDrag()
            }
        }
        return true
    }

    /// Lets go of what a drag held.
    func endDrag() {
        dragging = nil
        draggingGroup = nil
        dropMark = nil
        headerMark = nil
    }

    /// Asks the core, takes its groups at once, and draws the cards it
    /// moved in their new group ahead of its frames, which then confirm it.
    private func groupCall(_ a: GroupArgs, failed what: String, done: @escaping (GroupResult) -> Void) {
        Task {
            do {
                let r = try await client.call("group", a, as: GroupResult.self)
                groups = r.groups
                let moved = Set(r.sessions ?? [])
                let to: String? = switch a.action {
                case "new", "add": r.group
                case "move": a.into.flatMap { $0.isEmpty ? nil : $0 }
                default: nil
                }
                if !moved.isEmpty, a.action != "remove" {
                    preview { rows in
                        for i in rows.indices where moved.contains(rows[i].id) {
                            rows[i].group = to
                            if to != nil {
                                rows[i].pinned = false
                                rows[i].parked = false
                            }
                        }
                    }
                }
                done(r)
            } catch {
                say("Couldn’t \(what): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Whether a drop of the held card on target changes its group: in the
    /// List view, between two cards neither pinned nor parked, of
    /// different groups or of one and none.
    func dropRegroups(_ s: Session, onto target: Session) -> Bool {
        canGroup && mode == .list && s.peerName == nil && target.peerName == nil
            && !s.isPinned && !s.isParked && !target.isPinned && !target.isParked
            && s.groupKey != target.groupKey
    }

    /// The held card dropped in target's place, which puts it in target's
    /// group or in none: just after target when it came from above, else
    /// just before it. It shows there at once.
    func regroup(_ s: Session, onto target: Session, fromAbove: Bool, was: [Session]) {
        let before = fromAbove ? nil : target.id
        let after = fromAbove ? target.id : nil
        let to = target.groupKey
        preview { rows in
            if let i = rows.firstIndex(where: { $0.id == s.id }) { rows[i].group = to }
        }
        Task {
            do {
                let a = GroupArgs(action: "move", sessions: [s.id], into: to ?? "", before: before, after: after)
                groups = try await client.call("group", a, as: GroupResult.self).groups
            } catch {
                preview { rows in
                    for i in rows.indices {
                        guard let old = was.first(where: { $0.id == rows[i].id }) else { continue }
                        rows[i].order = old.order
                        rows[i].group = old.group
                    }
                }
                say("Couldn’t move \(s.name): \(error.localizedDescription)", error: true)
            }
        }
    }
}

/// A card's group items, for it or every card picked with it: New Group,
/// Move to Group, with every group as a tree, and Move Out of its group.
@MainActor
struct GroupMenu: View {
    @Environment(Store.self) private var store
    let list: [Session]

    var body: some View {
        if store.canGroup, list.allSatisfy({ $0.peerName == nil }) {
            let n = list.count
            let inGroup = list.contains { $0.groupKey != nil }
            Button(n == 1 ? "New Group" : "New Group of \(n) Sessions") { store.newGroup(with: list) }
            let tree = store.groupTree
            if !tree.isEmpty {
                Menu(n == 1 ? "Move to Group" : "Move \(n) to Group") {
                    if inGroup {
                        Button("Top of the List") { store.moveToTop(list) }
                        Divider()
                    }
                    ForEach(tree) { row in
                        Button(row.label) { store.addToGroup(list, row.id) }
                            .disabled(list.allSatisfy { $0.groupKey == row.id })
                    }
                }
            }
            if inGroup {
                let from = Set(list.compactMap(\.groupKey))
                if from.count == 1, let g = from.first {
                    Button(store.parentGroup(g) == nil ? "Remove from “\(store.groupName(g))”" : "Move Out of “\(store.groupName(g))”") {
                        store.removeFromGroup(list)
                    }
                } else {
                    Button("Move \(n) Out of Their Groups") { store.removeFromGroup(list) }
                }
            }
        }
    }
}

/// A group header's own items, for one of your groups: put it in a new
/// group, move it into another or out of the one around it.
@MainActor
struct GroupPlaceMenu: View {
    @Environment(Store.self) private var store
    let id: String

    var body: some View {
        let name = store.groupName(id)
        Button("Put “\(name)” in a New Group") { store.wrapGroup(id) }
        let parent = store.parentGroup(id)
        // Not itself, nor a group inside it, nor the one it is in.
        let others = store.groupTree.filter { !store.isWithin($0.id, id) && $0.id != parent }
        if parent != nil || !others.isEmpty {
            Menu("Move “\(name)” Into") {
                if parent != nil {
                    Button("Top of the List") { store.moveGroupToTop(id) }
                    if !others.isEmpty { Divider() }
                }
                ForEach(others) { row in
                    Button(row.label) { store.moveGroup(id, into: row.id) }
                }
            }
        }
        if let parent {
            Button("Move Out of “\(store.groupName(parent))”") { store.moveOut(id) }
        }
    }
}

/// A group header's name while it is open to edit: Return or a click
/// elsewhere renames the group, and Escape keeps its name.
@MainActor
struct GroupNameField: View {
    @Environment(Store.self) private var store
    let id: String
    let name: String
    @State private var text = ""
    @FocusState private var editing: Bool

    var body: some View {
        TextField("Group name", text: $text)
            .textFieldStyle(.plain)
            .font(Theme.font(12, .heavy))
            .focused($editing)
            // After the header has drawn it, so the field can take the
            // keyboard, as a card's name does.
            .task {
                text = name
                editing = true
            }
            .onSubmit { store.renameGroup(id, text) }
            .onExitCommand { store.renamingGroup = nil }
            .onChange(of: editing) {
                if !editing && store.renamingGroup == id { store.renameGroup(id, text) }
            }
            // A header that goes, as when its group does or the view
            // changes, gives the keys back.
            .onDisappear {
                if store.renamingGroup == id { store.renameGroup(id, text) }
            }
            .accessibilityLabel("New name for \(name)")
    }
}
