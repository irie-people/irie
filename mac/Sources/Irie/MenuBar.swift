import Combine
import SwiftUI

/// What the menu bar's items say and whether they work: the few facts the
/// menus read, kept apart from the store. The store changes several times a
/// second while agents work, as each one's detail, tools and tokens move,
/// and menus that read it were rebuilt every time: an open menu flickered,
/// lost the item under the pointer or closed. These facts change only when
/// an item's title or state does (docs/MAC.md §4).
@MainActor
final class MenuState: ObservableObject {
    struct Facts: Equatable {
        /// A session of Irie's is selected, not one outside it.
        var selected = false
        var closable = false
        var parked = false
        var canPark = false
        var canReopen = false
        var undo: String?
        var redo: String?
        var helperShown = false
        var showTerminal = false
    }

    @Published private(set) var facts = Facts()

    /// Takes the facts as they are now; the menus change only if they did.
    func set(_ f: Facts) {
        if f != facts { facts = f }
    }
}

extension Store {
    /// The menu's facts as the store has them now.
    var menuFacts: MenuState.Facts {
        let c = closable
        return MenuState.Facts(
            selected: selection != nil && outsideSelection == nil,
            closable: c != nil, parked: c?.isParked == true, canPark: c != nil && canPark,
            canReopen: !closed.isEmpty, undo: undoLabel, redo: redoLabel,
            helperShown: helperShown, showTerminal: showTerminal)
    }
}

/// The menu bar's commands. They hold the store only to act on it, and
/// watch the menu's facts alone.
struct IrieCommands: Commands {
    let store: Store
    @ObservedObject var menu: MenuState
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id

    var body: some Commands {
        let f = menu.facts
        SidebarCommands()
        CommandGroup(replacing: .newItem) {
            Button("New Task…") { store.newTaskSheet = true }
                .keyboardShortcut("n")
            Button("Type a Task") { NotificationCenter.default.post(name: .irieQuickTask, object: nil) }
                .keyboardShortcut("n", modifiers: [.command, .option])
            Button("Rename Session…") { if let id = store.selection { store.beginRename(id) } }
                .disabled(!f.selected)
        }
        // Sessions close like tabs (Closing.swift). KeyRouter catches ⌘W
        // first; the window's own Close moves to ⇧⌘W.
        CommandGroup(after: .newItem) {
            Button("Close Session") { if let s = store.closable { store.close(s) } }
                .keyboardShortcut("w")
                .disabled(!f.closable)
            Button("Reopen Closed Session") { store.reopenLast() }
                .keyboardShortcut("t", modifiers: [.command, .shift])
                .disabled(!f.canReopen)
            // The sidebar's z key does it while the list has the keyboard.
            Button(f.parked ? "Unpark Session" : "Park Session") { store.toggleParked() }
                .disabled(!f.canPark)
        }
        // Undo and Redo step through the core's history, a park or a
        // close, and name the step; a text field with typing to take
        // back keeps ⌘Z for that (Undo.swift).
        CommandGroup(replacing: .undoRedo) {
            Button(f.undo.map { "Undo " + UndoText.title($0) } ?? "Undo") { store.undoKey() }
                .keyboardShortcut("z")
            Button(f.redo.map { "Redo " + UndoText.title($0) } ?? "Redo") { store.undoKey(redo: true) }
                .keyboardShortcut("z", modifiers: [.command, .shift])
        }
        // KeyRouter catches Ctrl+Tab and Ctrl+] first; the menu shows them.
        CommandMenu("Go") {
            Button("Next Session") { store.step(1) }
                .keyboardShortcut(.tab, modifiers: .control)
            Button("Previous Session") { store.step(-1) }
                .keyboardShortcut(.tab, modifiers: [.control, .shift])
            Button("Sidebar or Terminal") { store.toggleFocus() }
                .keyboardShortcut("]", modifiers: .control)
            Divider()
            ForEach(1..<10) { n in
                Button("Session \(n)") { store.showRow(n) }
                    .keyboardShortcut(KeyEquivalent(Character(String(n))), modifiers: .command)
            }
            Divider()
            Button("Find a Session") { store.focusFind += 1 }
                .keyboardShortcut("f")
            Divider()
            // Step through the strip of the helper pane (HelperPane.swift).
            Button("Next Helper") { NotificationCenter.default.post(name: .irieHelperStep, object: 1) }
                .keyboardShortcut("]", modifiers: .command)
                .disabled(!f.helperShown)
            Button("Previous Helper") { NotificationCenter.default.post(name: .irieHelperStep, object: -1) }
                .keyboardShortcut("[", modifiers: .command)
                .disabled(!f.helperShown)
        }
        CommandGroup(after: .toolbar) {
            Picker("Theme", selection: $theme) {
                ForEach(Palette.all) { p in Text(p.name).tag(p.id) }
            }
            Divider()
            Button("Bigger Text") { store.setFontSize(store.fontSize + 1) }
                .keyboardShortcut("=")
            Button("Smaller Text") { store.setFontSize(store.fontSize - 1) }
                .keyboardShortcut("-")
            Button("Actual Size") { store.setFontSize(Store.defaultFontSize) }
                .keyboardShortcut("0")
            Divider()
            // The conversation, or the live terminal on the right.
            Toggle("Terminal", isOn: Binding(get: { f.showTerminal }, set: { store.showTerminal = $0 }))
                .keyboardShortcut("t", modifiers: [.command, .option])
            // A helper beside the conversation (HelperPane.swift).
            Button(f.helperShown ? "Hide Helpers" : "Show Helpers") { store.toggleHelpers() }
                .keyboardShortcut("h", modifiers: [.command, .shift])
                .disabled(!f.selected)
        }
    }
}
