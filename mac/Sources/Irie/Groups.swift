import AppKit
import SwiftUI

// Your groups (DESIGN §4.1): named sets of cards drawn together under a
// header that folds them, like a browser's tab groups, with a rail down the
// side of their cards. A card's menu has New Group, which makes a group of
// it, or of every card picked with it, and opens the group's name to edit;
// Move to Group; and Remove from Group. A header's menu renames the group,
// folds it, ungroups it, which leaves its cards where they are, or closes
// its cards. A card dropped on a card of a group, or on its header, joins
// the group, and one dropped among the cards of no group leaves it. The
// List view draws each group where the first of its cards is in the core's
// one order; the Repo view groups the cards by repo instead. Groups are the
// core's (internal/core, groups.go), so the terminal sidebar draws them
// too, and every change of a card's group is a step that ⌘Z takes back.

/// One of your groups, as the core's header lists it.
struct GroupInfo: Codable, Equatable, Identifiable {
    var id: String
    var name: String
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
    /// Where a drop put the cards: just before this card, or just after
    /// that one.
    var before: String?
    var after: String?

    enum CodingKeys: String, CodingKey {
        case action = "do"
        case group, name, sessions, before, after
    }
}

struct GroupResult: Decodable {
    var groups: [GroupInfo]
    var group: String?
    var sessions: [String]?
}

extension SessionGroup {
    /// The sidebar's id of your group id.
    static func userID(_ id: String) -> String { "group:" + id }
}

extension Store {
    /// The List view's cards between Pinned and Parked: each of your groups
    /// where the first of its cards is, with its other cards after it, and
    /// the cards of no group between them, in runs drawn without a header.
    func listGroups(_ here: [Session]) -> [SessionGroup] {
        var names: [String: String] = [:]
        for g in groups { names[g.id] = g.name }
        var members: [String: [Session]] = [:]
        for s in here {
            if let g = s.groupKey, names[g] != nil { members[g, default: []].append(s) }
        }
        var out: [SessionGroup] = [], run: [Session] = [], seen: Set<String> = [], runs = 0
        func endRun() {
            guard !run.isEmpty else { return }
            out.append(SessionGroup(id: runs == 0 ? SessionGroup.list : "\(SessionGroup.list):\(runs)", title: "Sessions", path: nil, sessions: run))
            runs += 1
            run = []
        }
        for s in here {
            guard let g = s.groupKey, let name = names[g] else {
                run.append(s)
                continue
            }
            guard seen.insert(g).inserted else { continue }
            endRun()
            out.append(SessionGroup(id: SessionGroup.userID(g), title: name, path: nil, sessions: members[g] ?? [], userGroup: g))
        }
        endRun()
        return out
    }

    /// Your groups with a card of this Mac's in the space on show, in the
    /// order they were made: what Move to Group offers.
    var groupsHere: [GroupInfo] {
        let here = Set(sessions.filter { $0.peerName == nil && $0.spaceKey == space }.compactMap(\.groupKey))
        return groups.filter { here.contains($0.id) }
    }

    /// A group's name by its id.
    func groupName(_ id: String) -> String { groups.first { $0.id == id }?.name ?? "its group" }

    /// The cards a group action takes: this Mac's.
    private func mine(_ list: [Session]) -> [Session] { list.filter { $0.peerName == nil } }

    /// Makes a group of list, a card or every card picked with it, where
    /// the first of them is, and opens its name to edit. Your groups show
    /// in the List view, so the sidebar goes there.
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

    /// Puts list in group id: last in it, or where a drop put them.
    func addToGroup(_ list: [Session], _ id: String, before: String? = nil, after: String? = nil) {
        let cards = mine(list).filter { $0.groupKey != id || $0.isPinned || $0.isParked }
        guard canGroup, !cards.isEmpty else { return }
        pickNone()
        let what = cards.count == 1 ? cards[0].name : "\(cards.count) sessions"
        groupCall(GroupArgs(action: "add", group: id, sessions: cards.map(\.id), before: before, after: after), failed: "move \(what) to the group") { r in
            if before == nil, after == nil {
                self.say("Moved \(what) to \(self.groupName(id)). ⌘Z takes it back.", error: false, seconds: 6, undo: true)
            }
        }
    }

    /// Takes list out of their groups: each just after the rest of its
    /// group, or where a drop put them.
    func removeFromGroup(_ list: [Session], before: String? = nil, after: String? = nil) {
        let cards = mine(list).filter { $0.groupKey != nil }
        guard canGroup, !cards.isEmpty else { return }
        pickNone()
        let what = cards.count == 1 ? cards[0].name : "\(cards.count) sessions"
        groupCall(GroupArgs(action: "remove", sessions: cards.map(\.id), before: before, after: after), failed: "take \(what) out of the group") { _ in
            if before == nil, after == nil {
                self.say("Took \(what) out of \(cards.count == 1 ? "its group" : "their groups"). ⌘Z puts \(cards.count == 1 ? "it" : "them") back.",
                         error: false, seconds: 6, undo: true)
            }
        }
    }

    func renameGroup(_ id: String, _ name: String) {
        renamingGroup = nil
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, name != groupName(id) else { return }
        groupCall(GroupArgs(action: "rename", group: id, name: name), failed: "rename the group") { _ in }
    }

    /// Removes group id; its cards stay where they are, in no group.
    func ungroup(_ id: String) {
        let name = groupName(id)
        groupCall(GroupArgs(action: "ungroup", group: id), failed: "ungroup \(name)") { r in
            let n = r.sessions?.count ?? 0
            self.say("Ungrouped \(name): \(n == 1 ? "its session stays" : "its \(n) sessions stay") where \(n == 1 ? "it is" : "they are"). ⌘Z groups them again.",
                     error: false, seconds: 6, undo: true)
        }
    }

    /// A card dropped on a group's header: it, or every card picked with
    /// it, goes last in that group.
    func dropOnGroup(_ g: SessionGroup) -> Bool {
        defer {
            dragging = nil
            dropMark = nil
        }
        guard let id = g.userGroup, let held = dragging, let s = sessions.first(where: { $0.id == held }) else { return false }
        addToGroup(targets(for: s), id)
        return true
    }

    /// Asks the core, takes its groups at once, and draws the cards it
    /// moved in their new group ahead of its frames, which then confirm it.
    private func groupCall(_ a: GroupArgs, failed what: String, done: @escaping (GroupResult) -> Void) {
        Task {
            do {
                let r = try await client.call("group", a, as: GroupResult.self)
                groups = r.groups
                let moved = Set(r.sessions ?? [])
                let to = a.action == "new" || a.action == "add" ? r.group : nil
                if !moved.isEmpty {
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
                let a = GroupArgs(action: to == nil ? "remove" : "add", group: to, sessions: [s.id], before: before, after: after)
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
/// Move to Group and Remove from Group.
@MainActor
struct GroupMenu: View {
    @Environment(Store.self) private var store
    let list: [Session]

    var body: some View {
        if store.canGroup, list.allSatisfy({ $0.peerName == nil }) {
            let n = list.count
            Button(n == 1 ? "New Group" : "New Group of \(n) Sessions") { store.newGroup(with: list) }
            let others = store.groupsHere.filter { g in !list.allSatisfy { $0.groupKey == g.id } }
            if !others.isEmpty {
                Menu(n == 1 ? "Move to Group" : "Move \(n) to Group") {
                    ForEach(others) { g in
                        Button(g.name) { store.addToGroup(list, g.id) }
                    }
                }
            }
            if list.contains(where: { $0.groupKey != nil }) {
                Button(n == 1 ? "Remove from Group" : "Remove \(n) from Their Groups") { store.removeFromGroup(list) }
            }
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
            .onAppear {
                text = name
                editing = true
            }
            .onSubmit { store.renameGroup(id, text) }
            .onExitCommand { store.renamingGroup = nil }
            .onChange(of: editing) {
                if !editing && store.renamingGroup == id { store.renameGroup(id, text) }
            }
            .accessibilityLabel("New name for \(name)")
    }
}
