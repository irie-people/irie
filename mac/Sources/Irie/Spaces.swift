import AppKit
import SwiftUI

// Spaces (DESIGN §4.1): named sets of cards that the sidebar shows one at a
// time, like a browser's spaces, each with its own Pinned and Parked. The
// chips under the list show them: a click or ⌃1–9 shows one, ⌃⌥1–9 sends
// the selected or picked cards to one, and a card dropped on a chip moves
// there. Every paired machine's group and Outside Irie show in every space,
// and the title bar's counts count them all; showing a card in another
// space takes the sidebar there.

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

    /// Shows a space: the card shown there last, else its first.
    func setSpace(_ key: String) {
        guard key != space else { return }
        if let id = selection { lastShown[space] = id }
        space = key
        UserDefaults.standard.set(key, forKey: "space")
        pickNone()
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
    /// takes its place.
    func sendToSpace(_ list: [Session], _ key: String) {
        let moving = list.filter { $0.peerName == nil && $0.spaceKey != key }
        guard !moving.isEmpty else { return }
        let ids = Set(moving.map(\.id))
        showNext(after: ids)
        pickNone()
        preview { rows in
            for i in rows.indices where ids.contains(rows[i].id) { rows[i].space = key }
        }
        let what = moving.count == 1 ? moving[0].name : "\(moving.count) sessions"
        Task {
            do {
                _ = try await client.call("space", SpaceArgs(sessions: moving.map(\.id), space: key), as: SpaceResult.self)
                say("Moved \(what) to \(spaceName(key)). ⌘Z brings \(moving.count == 1 ? "it" : "them") back.", error: false, seconds: 6, undo: true)
            } catch {
                preview { rows in
                    for i in rows.indices {
                        if let old = moving.first(where: { $0.id == rows[i].id }) { rows[i].space = old.space }
                    }
                }
                say("Couldn’t move \(what): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// A card dropped on a space's chip: it, or every card picked with it,
    /// goes there.
    func dropOnSpace(_ key: String) -> Bool {
        defer {
            dragging = nil
            dropMark = nil
        }
        guard let id = dragging, let s = sessions.first(where: { $0.id == id }) else { return false }
        sendToSpace(targets(for: s), key)
        return true
    }

    /// Makes a space and shows it with its name open to edit, taking cards
    /// there first when moving names some.
    func newSpace(moving: [Session] = []) {
        Task {
            do {
                let r = try await client.call("spaces", SpacesArgs(action: "new"), as: SpacesResult.self)
                spaces = r.spaces
                guard let id = r.space else { return }
                if !moving.isEmpty { sendToSpace(moving, id) }
                setSpace(id)
                renamingSpace = id
            } catch {
                say("Couldn’t make a space: \(error.localizedDescription)", error: true)
            }
        }
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
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 4) {
                ForEach(Array(store.spaces.enumerated()), id: \.element.id) { i, s in
                    SpaceChip(space: s, key: store.spaceKey(at: i), number: i + 1)
                }
                Button { store.newSpace() } label: {
                    Image(systemName: "plus")
                        .font(.system(size: 10, weight: .bold))
                        .frame(width: 22, height: 22)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .foregroundStyle(Theme.muted)
                .help("New space")
                .accessibilityLabel("New space")
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Spaces")
    }
}

/// One space's chip: a click shows it, and its menu names, moves or
/// removes it. A card dropped on it moves there.
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
        .onDrop(of: [.text], isTargeted: $targeted) { _ in store.dropOnSpace(key) }
        .contextMenu {
            Button("Rename…") { store.renamingSpace = space.id }
            if number > 1 { Button("Move Left") { store.moveSpace(space.id, by: -1) } }
            if number < store.spaces.count { Button("Move Right") { store.moveSpace(space.id, by: 1) } }
            if store.spaces.count > 1 {
                Divider()
                Button("Remove Space") { store.removeSpace(space.id) }
            }
        }
        .help(number <= 9 ? "\(space.name) (⌃\(number)). Drop a card here to move it; ⌃⌥\(number) sends the selected card." : space.name)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(space.name)
        .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction { store.setSpace(key) }
    }
}

/// A card's Move to Space menu, for it or every card picked with it.
@MainActor
struct SpaceMenu: View {
    @Environment(Store.self) private var store
    let list: [Session]

    var body: some View {
        if store.canSpaces, list.allSatisfy({ $0.peerName == nil }) {
            Menu("Move to Space") {
                ForEach(Array(store.spaces.enumerated()), id: \.element.id) { i, s in
                    let key = store.spaceKey(at: i)
                    Button(s.name) { store.sendToSpace(list, key) }
                        .disabled(list.allSatisfy { $0.spaceKey == key })
                }
                Divider()
                Button("New Space") { store.newSpace(moving: list) }
            }
        }
    }
}
