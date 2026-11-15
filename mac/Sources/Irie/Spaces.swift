import AppKit
import SwiftUI

// Spaces (DESIGN §4.1): named sets of cards that the sidebar shows one at a
// time, like a browser's spaces, each with its own Pinned and Parked. The
// chips under the list show them: a click or ⌃1–9 shows one, ⌃⌥1–9 sends
// the selected or picked cards to one, and a card dropped on a chip moves
// there, or a group of yours, whole. The + after them makes a space; while
// a drag holds a card or a group it reads New Space, and what is dropped on
// it goes to a new space, a group to one named after it. A group header's
// menu moves its group the same way (Move to Space). Every paired
// machine's group and Outside Irie show in every space, and the title
// bar's counts count them all; showing a card in another space takes the
// sidebar there.

/// One space, as the core's header lists it.
struct SpaceInfo: Codable, Equatable, Identifiable {
    var id: String
    var name: String
}

extension Session {
    /// The space it is in, as the sidebar keys it: "" for the first.
    var spaceKey: String { space ?? "" }
}

struct SpacesArgs: Encodable {
    var action: String?
    var space: String?
    var name: String?
    var before: String?

    enum CodingKeys: String, CodingKey {
        case action = "do"
        case space, name, before
    }
}

struct SpacesResult: Decodable {
    var spaces: [SpaceInfo]
    var space: String?
}

struct SpaceArgs: Encodable {
    var sessions: [String]
    /// Groups of yours that go whole, with what the core has in them then,
    /// their groups' cards too: a card that joined one a moment ago goes as
    /// well. The cards go in sessions too, for a core from before.
    var groups: [String]?
    var space: String
}

struct SpaceResult: Decodable {
    var sessions: [String]
}

extension Store {
    /// Whether the core keeps spaces.
    var canSpaces: Bool { !spaces.isEmpty }

    /// The key of the space at index i: "" for the first.
    func spaceKey(at i: Int) -> String { i == 0 ? "" : spaces[i].id }

    /// A space's name by its key.
    func spaceName(_ key: String) -> String {
        (key.isEmpty ? spaces.first : spaces.first { $0.id == key })?.name ?? "the first space"
    }

    /// Shows a space: the card shown there last, else its first; or, when
    /// showing is false, as show(_:) goes to a card of its own there, none.
    func setSpace(_ key: String, showing: Bool = true) {
        guard key != space else { return }
        if let id = selection { lastShown[space] = id }
        space = key
        UserDefaults.standard.set(key, forKey: "space")
        pickNone()
        guard showing else { return }
        let here = drawnOrder
        if let id = lastShown[key], here.contains(where: { $0.id == id }) {
            show(id)
        } else if let first = here.first {
            show(first.id)
        }
    }

    /// ⌃1–9: the space of that number.
    func showSpace(number n: Int) {
        guard spaces.indices.contains(n - 1) else {
            NSSound.beep()
            return
        }
        setSpace(spaceKey(at: n - 1))
    }

    /// ⌃⌥1–9: the cards picked, or the selected one, go to the space of that
    /// number.
    func sendToSpace(number n: Int) {
        guard spaces.indices.contains(n - 1) else {
            NSSound.beep()
            return
        }
        let p = pickedSessions
        sendToSpace(p.count > 1 ? p : closable.map { [$0] } ?? [], spaceKey(at: n - 1))
    }

    /// Puts cards in a space, as one step that ⌘Z takes back. They leave the
    /// sidebar at once, and when the shown one goes, the next card here
    /// takes its place. With group, they are a group of yours, which goes
    /// whole, its groups with it; fresh says the space was just made for
    /// them.
    func sendToSpace(_ list: [Session], _ key: String, group: String? = nil, fresh: Bool = false) {
        let moving = list.filter { $0.peerName == nil && $0.spaceKey != key }
        guard !moving.isEmpty else { return }
        let ids = Set(moving.map(\.id))
        let was = sessions, wasGroups = groups
        showNext(after: ids)
        pickNone()
        previewSpace(moving, key)
        let what = group.map(groupName) ?? (moving.count == 1 ? moving[0].name : "\(moving.count) sessions")
        let it = group != nil || moving.count == 1 ? "it" : "them"
        let to = fresh ? "a new space" : spaceName(key)
        Task {
            do {
                let a = SpaceArgs(sessions: moving.map(\.id), groups: group.map { [$0] }, space: key)
                _ = try await client.call("space", a, as: SpaceResult.self)
                say("Moved \(what) to \(to). ⌘Z brings \(it) back.", error: false, seconds: 6, undo: true)
            } catch {
                groups = wasGroups
                preview { rows in
                    for i in rows.indices {
                        guard let old = was.first(where: { $0.id == rows[i].id }), ids.contains(old.id) else { continue }
                        rows[i].space = old.space
                        rows[i].group = old.group
                    }
                }
                say("Couldn’t move \(what): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Shows cards going to space key at once, as the core's space op will
    /// have them (spaces.go), so that the space they go to draws them in
    /// their groups from the first: a card leaves its group unless the
    /// whole group goes with it, which then comes out of a group around it
    /// that stays.
    private func previewSpace(_ moving: [Session], _ key: String) {
        let ids = Set(moving.map(\.id))
        // Whether every card of this Mac's in group g goes, or is there.
        func whole(_ g: String) -> Bool {
            !sessions.contains { $0.peerName == nil && !ids.contains($0.id) && $0.spaceKey != key && isWithin($0.groupKey, g) }
        }
        var leave: Set<String> = [], out: Set<String> = []
        for s in moving {
            guard let g = s.groupKey else { continue }
            guard whole(g) else {
                leave.insert(s.id)
                continue
            }
            var top = g
            while let p = parentGroup(top), whole(p) { top = p }
            if parentGroup(top) != nil { out.insert(top) }
        }
        for i in groups.indices where out.contains(groups[i].id) { groups[i].parent = "" }
        preview { rows in
            for i in rows.indices where ids.contains(rows[i].id) {
                rows[i].space = key
                if leave.contains(rows[i].id) { rows[i].group = nil }
            }
        }
    }

    /// Group id's cards of this Mac's in the space on show, those of its
    /// groups too, the pinned and parked ones among them: what goes with
    /// it to another space.
    func spaceCards(of id: String) -> [Session] {
        sessions.filter { $0.peerName == nil && $0.spaceKey == space && isWithin($0.groupKey, id) }
    }

    /// Sends group id, whole, to space key: its header's Move to Space, or
    /// a drop on a chip.
    func sendGroupToSpace(_ id: String, _ key: String) {
        sendToSpace(spaceCards(of: id), key, group: id)
    }

    /// Whether what the drag holds can go to space key, or to a new space
    /// for nil: a group of yours, or the cards held, of this Mac's, one of
    /// them in another space at least.
    func canDrop(onSpace key: String?) -> Bool {
        if let g = draggingGroup {
            return spaceCards(of: g).contains { key == nil || $0.spaceKey != key }
        }
        guard !heldCards.isEmpty else { return false }
        return sessions.contains { heldCards.contains($0.id) && $0.peerName == nil && (key == nil || $0.spaceKey != key) }
    }

    /// What a drag held, dropped on space key's chip, or on + for a new
    /// space (nil): group, a group of yours, goes whole; else the cards
    /// held, in the list's order.
    func dropOnSpace(_ key: String?, group: String?, held: Set<String>) {
        if let g = group {
            if let key { sendGroupToSpace(g, key) } else { newSpace(forGroup: g) }
            return
        }
        var cards = drawnGroups.flatMap(\.sessions).filter { held.contains($0.id) }
        if cards.isEmpty { cards = sessions.filter { held.contains($0.id) } }
        guard !cards.isEmpty else { return }
        if let key { sendToSpace(cards, key) } else { newSpace(moving: cards) }
    }

    /// Makes a space and shows it with its name open to edit, taking cards
    /// there first when moving names some. For a group of yours, it takes
    /// the group's name, and the group goes whole: the space is named
    /// already, so it shows without the name open.
    func newSpace(moving: [Session] = [], group: String? = nil) {
        let name = group.map(groupName)
        Task {
            do {
                let r = try await client.call("spaces", SpacesArgs(action: "new", name: name), as: SpacesResult.self)
                spaces = r.spaces
                guard let id = r.space else { return }
                if !moving.isEmpty { sendToSpace(moving, id, group: group, fresh: true) }
                setSpace(id)
                if group == nil { renamingSpace = id }
            } catch {
                say("Couldn’t make a space: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Group id, whole, to a new space named after it.
    func newSpace(forGroup id: String) {
        let cards = spaceCards(of: id)
        guard !cards.isEmpty else { return }
        newSpace(moving: cards, group: id)
    }

    func renameSpace(_ id: String, _ name: String) {
        renamingSpace = nil
        guard let old = spaces.first(where: { $0.id == id }), name.trimmingCharacters(in: .whitespaces) != old.name,
              !name.trimmingCharacters(in: .whitespaces).isEmpty else { return }
        spacesCall(SpacesArgs(action: "rename", space: id, name: name), failed: "rename the space")
    }

    /// Moves a space one place left (-1) or right (+1) among the chips.
    func moveSpace(_ id: String, by delta: Int) {
        guard let i = spaces.firstIndex(where: { $0.id == id }), spaces.indices.contains(i + delta) else { return }
        // Just before the space after where it goes, or last.
        let to = i + delta + (delta > 0 ? 1 : 0)
        let before = spaces.indices.contains(to) ? spaces[to].id : nil
        spacesCall(SpacesArgs(action: "move", space: id, before: before), failed: "move the space")
    }

    /// Removes a space; its cards go to the first space.
    func removeSpace(_ id: String) {
        if spaceKey(at: spaces.firstIndex(where: { $0.id == id }) ?? 0) == space { setSpace("") }
        spacesCall(SpacesArgs(action: "remove", space: id), failed: "remove the space")
    }

    private func spacesCall(_ a: SpacesArgs, failed what: String) {
        Task {
            do {
                spaces = try await client.call("spaces", a, as: SpacesResult.self).spaces
            } catch {
                say("Couldn’t \(what): \(error.localizedDescription)", error: true)
            }
        }
    }
}

/// The spaces under the list, a chip each, the one on show filled, and +.
@MainActor
struct SpaceBar: View {
    @Environment(Store.self) private var store

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 4) {
                    ForEach(Array(store.spaces.enumerated()), id: \.element.id) { i, s in
                        SpaceChip(space: s, key: store.spaceKey(at: i), number: i + 1)
                    }
                    NewSpaceChip()
                        .id(NewSpaceChip.id)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
            }
            // A drag that takes hold of a card or a group brings + into
            // sight, should the chips fill the bar.
            .onChange(of: store.heldText != nil) { _, holding in
                if holding { proxy.scrollTo(NewSpaceChip.id) }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Spaces")
    }
}

/// A drop on a space's chip, or on + for a new space (a nil key): it lights
/// the chip while what the drag holds would move there, and takes it there.
/// A chip lights for nothing that would stay where it is, as a group on
/// its own space's chip.
@MainActor
private struct SpaceDrop: DropDelegate {
    let store: Store
    let key: String?
    @Binding var targeted: Bool

    func validateDrop(info: DropInfo) -> Bool { store.heldText != nil && store.canDrop(onSpace: key) }

    func dropEntered(info: DropInfo) { targeted = true }

    func dropUpdated(info: DropInfo) -> DropProposal? { DropProposal(operation: .move) }

    func dropExited(info: DropInfo) { targeted = false }

    func performDrop(info: DropInfo) -> Bool {
        targeted = false
        // What the drag holds, kept here: the hold may end before the
        // dropped item has been read.
        let group = store.draggingGroup, held = store.heldCards, key = key
        return store.carrying(info.itemProviders(for: [.text])) { store.dropOnSpace(key, group: group, held: held) }
    }
}

/// The + after the chips: a click makes a space and opens its name. While a
/// drag holds a card or a group of yours, it reads New Space, and what is
/// dropped on it goes to a new space: a group to one named after it.
@MainActor
private struct NewSpaceChip: View {
    @Environment(Store.self) private var store
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var targeted = false

    /// Its id in the bar, to scroll it into sight.
    static let id = "space:new"

    var body: some View {
        let holding = store.canDrop(onSpace: nil)
        Button { store.newSpace() } label: {
            HStack(spacing: 4) {
                Image(systemName: "plus")
                    .font(.system(size: 10, weight: .bold))
                if holding {
                    Text("New Space")
                        .font(Theme.font(12, .medium))
                        .lineLimit(1)
                        .fixedSize()
                }
            }
            .padding(.horizontal, holding ? 9 : 0)
            .frame(minWidth: 22, minHeight: 22)
            .background(Capsule().fill(targeted ? Theme.hover : Color.clear))
            .overlay(Capsule().strokeBorder(targeted ? Theme.accent : holding ? Theme.muted.opacity(0.25) : Color.clear, lineWidth: 1))
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .foregroundStyle(targeted ? Theme.ink : Theme.muted)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.15), value: holding)
        .onDrop(of: [.text], delegate: SpaceDrop(store: store, key: nil, targeted: $targeted))
        .help("New space. Drop a card or a group here to give it a space of its own.")
        .accessibilityLabel("New space")
    }
}

/// One space's chip: a click shows it, and its menu names, moves or
/// removes it. A card dropped on it moves there, and a group of yours,
/// whole.
@MainActor
private struct SpaceChip: View {
    @Environment(Store.self) private var store
    let space: SpaceInfo
    let key: String
    let number: Int
    @State private var name = ""
    @State private var targeted = false
    @FocusState private var editing: Bool

    var body: some View {
        let on = store.space == key
        Group {
            if store.renamingSpace == space.id {
                TextField("Name", text: $name)
                    .textFieldStyle(.plain)
                    .font(Theme.font(12, .semibold))
                    .frame(width: 96)
                    .focused($editing)
                    .onAppear {
                        name = space.name
                        editing = true
                    }
                    .onSubmit { store.renameSpace(space.id, name) }
                    .onExitCommand { store.renamingSpace = nil }
                    .onChange(of: editing) {
                        if !editing && store.renamingSpace == space.id { store.renameSpace(space.id, name) }
                    }
            } else {
                Text(space.name)
                    .font(Theme.font(12, on ? .bold : .medium))
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 3)
        .foregroundStyle(on ? Theme.ink : Theme.muted)
        .background(Capsule().fill(on ? Theme.selection : targeted ? Theme.hover : Color.clear))
        .overlay(Capsule().strokeBorder(targeted ? Theme.accent : on ? Theme.accent.opacity(0.5) : Theme.muted.opacity(0.25), lineWidth: 1))
        .contentShape(Capsule())
        // One click shows it at once; Rename… is in its menu, and a new
        // space opens its name to edit by itself.
        .onTapGesture { store.setSpace(key) }
        .onDrop(of: [.text], delegate: SpaceDrop(store: store, key: key, targeted: $targeted))
        .contextMenu {
            Button("Rename…") { store.renamingSpace = space.id }
            if number > 1 { Button("Move Left") { store.moveSpace(space.id, by: -1) } }
            if number < store.spaces.count { Button("Move Right") { store.moveSpace(space.id, by: 1) } }
            if store.spaces.count > 1 {
                Divider()
                Button("Remove Space") { store.removeSpace(space.id) }
            }
        }
        .help(number <= 9 ? "\(space.name) (⌃\(number)). Drop a card or a group here to move it; ⌃⌥\(number) sends the selected card." : "\(space.name). Drop a card or a group here to move it.")
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(space.name)
        .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction { store.setSpace(key) }
    }
}

/// The spaces a card can go to, as items of its Move menu, for it or every
/// card picked with it: To Irie, To Steak, To New Space.
@MainActor
struct SpaceItems: View {
    @Environment(Store.self) private var store
    let list: [Session]

    /// Whether these cards can go to another space: another machine's
    /// can't.
    static func shown(_ store: Store, _ list: [Session]) -> Bool {
        store.canSpaces && list.allSatisfy { $0.peerName == nil }
    }

    var body: some View {
        if Self.shown(store, list) {
            ForEach(Array(store.spaces.enumerated()), id: \.element.id) { i, s in
                let key = store.spaceKey(at: i)
                Button("To " + s.name) { store.sendToSpace(list, key) }
                    .disabled(list.allSatisfy { $0.spaceKey == key })
            }
            Button("To New Space") { store.newSpace(moving: list) }
        }
    }
}

/// The spaces a group of yours can go to, whole, as items of its header's
/// Move to Space menu: every other space, then New Space, named after it.
@MainActor
struct GroupSpaceItems: View {
    @Environment(Store.self) private var store
    let id: String

    var body: some View {
        ForEach(Array(store.spaces.enumerated()), id: \.element.id) { i, s in
            let key = store.spaceKey(at: i)
            Button(s.name) { store.sendGroupToSpace(id, key) }
                .disabled(key == store.space)
        }
        Divider()
        Button("New Space") { store.newSpace(forGroup: id) }
    }
}
