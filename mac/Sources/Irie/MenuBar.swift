import Observation
import SwiftUI

/// What the menu bar's items say and whether they work: the few facts the
/// menus read, kept apart from the store. The store changes several times a
/// second while agents work, as each one's detail, tools and tokens move,
/// and menus that read it were rebuilt every time: an open menu flickered,
/// lost the item under the pointer or closed. These facts change only when
/// an item's title or state does (docs/MAC.md §4).
@MainActor
@Observable
final class MenuState {
    struct Facts: Equatable {
        /// A session of Irie's is selected, not one outside it.
        var selected = false
        var closable = false
        var parked = false
        var canPark = false
        /// Pin, and the cards picked with ⌘ or ⇧ (Tabs.swift).
        var pinned = false
        var canPin = false
        var picked = 0
        var canReopen = false
        var trodHand = false
        var canTrod = false
        var boosting = false
        var canBoost = false
        var vpn = false
        var lockdown = false
        var undo: String?
        var redo: String?
        var helperShown = false
        var showTerminal = false
        var sideOpen = false
        var canPlugins = false
        var plugins: [PluginInfo] = []
    }

    private(set) var facts = Facts()

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
            pinned: c?.isPinned == true, canPin: c != nil && canPin && c?.peerName == nil, picked: picked.count,
            canReopen: !closed.isEmpty, trodHand: trod?.hand == true, canTrod: canTrod,
            boosting: boosting, canBoost: canBoost, vpn: vpn != nil, lockdown: vpn?.lockdown == true,
            undo: undoLabel, redo: redoLabel, helperShown: helperShown, showTerminal: showTerminal,
            sideOpen: side.open, canPlugins: canPlugins, plugins: plugins)
    }

    /// Keeps the menu's facts up to date: whenever what they are read from
    /// changes, they are read again once the change is in, once for every
    /// change made in one turn of the run loop. A boost ends by the clock,
    /// so its end reads them again too.
    func watchMenu() {
        let f = withObservationTracking { menuFacts } onChange: { [weak self] in
            guard let store = self else { return }
            DispatchQueue.main.async {
                MainActor.assumeIsolated { store.watchMenu() }
            }
        }
        menu.set(f)
        if let until = boostUntil, until > Date() {
            DispatchQueue.main.asyncAfter(deadline: .now() + until.timeIntervalSinceNow + 0.1) { [weak self] in
                MainActor.assumeIsolated {
                    guard let self else { return }
                    self.menu.set(self.menuFacts)
                }
            }
        }
    }
}

/// The menu bar's commands. They hold the store only to act on it, and
/// watch the menu's facts alone. Like LearnCommands, they are not on the
/// main actor themselves: a menu reads and acts there, which run says.
struct IrieCommands: Commands {
    let store: Store
    let menu: MenuState
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id

    /// An action on the main actor, where SwiftUI runs a menu's actions.
    private func run(_ work: @escaping @MainActor () -> Void) -> () -> Void {
        { MainActor.assumeIsolated(work) }
    }

    var body: some Commands {
        let f = MainActor.assumeIsolated { menu.facts }
        SidebarCommands()
        CommandGroup(replacing: .newItem) {
            Button("New Task…", action: run { store.newTaskSheet = true })
                .keyboardShortcut("n")
            Button("Type a Task") { NotificationCenter.default.post(name: .irieQuickTask, object: nil) }
                .keyboardShortcut("n", modifiers: [.command, .option])
            // A guided session of Irie's own, as :setup switch starts
            // from any task bar (docs/proposals/extensions.md §2.2).
            Button("Set Up a Dead Man's Switch…", action: run { Task { _ = await store.input(":setup switch") } })
            // A writing session in Variorum, as :write starts
            // (docs/proposals/writers.md).
            Button("Write with Variorum…", action: run { Task { _ = await store.input(":write") } })
            Button("Rename Session…", action: run { if let id = store.selection { store.beginRename(id) } })
                .disabled(!f.selected)
        }
        // Sessions close like tabs (Closing.swift). KeyRouter catches ⌘W
        // first; the window's own Close moves to ⇧⌘W.
        CommandGroup(after: .newItem) {
            Button(f.picked > 1 ? "Close \(f.picked) Sessions" : "Close Session", action: run { store.closeKey() })
                .keyboardShortcut("w")
                .disabled(!f.closable && f.picked < 2)
            // Several at once, as one step that ⌘Z takes back (Tabs.swift).
            Button("Close Other Sessions", action: run { if let s = store.closable { store.closeMany(store.others(than: s)) } })
                .disabled(!f.closable)
            Button("Close Sessions Below", action: run { if let s = store.closable { store.closeMany(store.below(s)) } })
                .disabled(!f.closable)
            Button("Close All Finished", action: run { store.closeFinished() })
            Button("Reopen Closed Session", action: run { store.reopenLast() })
                .keyboardShortcut("t", modifiers: [.command, .shift])
                .disabled(!f.canReopen)
            // The sidebar's z key does it while the list has the keyboard.
            Button(f.parked ? "Unpark Session" : "Park Session", action: run { store.toggleParked() })
                .disabled(!f.canPark)
            Button(f.pinned ? "Unpin Session" : "Pin Session to the Top", action: run {
                if let s = store.closable { store.setPinned(s, !s.isPinned) }
            })
            .disabled(!f.canPin)
            // The keys move the selected card in its group; with ⇧, to its
            // top or bottom, without promoting it.
            Menu("Move Session") {
                Button("Up", action: run { store.moveSelected(by: -1) })
                    .keyboardShortcut(.upArrow, modifiers: [.command, .option])
                Button("Down", action: run { store.moveSelected(by: 1) })
                    .keyboardShortcut(.downArrow, modifiers: [.command, .option])
                Button("To the Top of Its Group", action: run { store.moveSelectedToEdge(top: true) })
                    .keyboardShortcut(.upArrow, modifiers: [.command, .option, .shift])
                Button("To the Bottom of Its Group", action: run { store.moveSelectedToEdge(top: false) })
                    .keyboardShortcut(.downArrow, modifiers: [.command, .option, .shift])
            }
            .disabled(!f.closable)
            Divider()
            // A run you start goes on while you watch, until no session
            // has work left or the plan is as full as it may get.
            Button(f.trodHand ? "Stop the Trod Bot" : "Start the Trod Bot Now", action: run { store.setTrod(store.trod?.hand != true) })
                .disabled(!f.canTrod)
            // Gabriel's boost: your focus gets the machine first for a
            // while, busy or not; it ends by itself (DESIGN §4.11).
            Button(f.boosting ? "Stop the Boost" : "Boost Your Focus", action: run { store.setBoost(!store.boosting) })
                .disabled(!f.canBoost)
            // Mullvad VPN, while vpn.on: the same words as :vpn in a
            // task bar (docs/proposals/extensions.md §4.1).
            Menu("VPN") {
                Button("Connect", action: run { Task { _ = await store.input(":vpn on") } })
                Button("Disconnect", action: run { Task { _ = await store.input(":vpn off") } })
                Divider()
                Button(f.lockdown ? "Turn Lockdown Mode Off" : "Turn Lockdown Mode On", action: run {
                    Task { _ = await store.input(store.vpn?.lockdown == true ? ":vpn lockdown off" : ":vpn lockdown on") }
                })
                Button("Account Time Left", action: run { Task { _ = await store.input(":vpn time") } })
            }
            .disabled(!f.vpn)
        }
        // Undo and Redo step through the core's history, a park or a
        // close, and name the step; a text field with typing to take
        // back keeps ⌘Z for that (Undo.swift).
        CommandGroup(replacing: .undoRedo) {
            Button(f.undo.map { "Undo " + UndoText.title($0) } ?? "Undo", action: run { store.undoKey() })
                .keyboardShortcut("z")
            Button(f.redo.map { "Redo " + UndoText.title($0) } ?? "Redo", action: run { store.undoKey(redo: true) })
                .keyboardShortcut("z", modifiers: [.command, .shift])
        }
        // KeyRouter catches Ctrl+Tab and Ctrl+] first; the menu shows them.
        CommandMenu("Go") {
            Button("Next Session", action: run { store.step(1) })
                .keyboardShortcut(.tab, modifiers: .control)
            Button("Previous Session", action: run { store.step(-1) })
                .keyboardShortcut(.tab, modifiers: [.control, .shift])
            Button("Sidebar or Terminal", action: run { store.toggleFocus() })
                .keyboardShortcut("]", modifiers: .control)
            Divider()
            ForEach(1..<10) { n in
                Button("Session \(n)", action: run { store.showRow(n) })
                    .keyboardShortcut(KeyEquivalent(Character(String(n))), modifiers: .command)
            }
            Divider()
            Button("Find a Session", action: run { store.focusFind += 1 })
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
            Button("Bigger Text", action: run { store.setFontSize(store.fontSize + 1) })
                .keyboardShortcut("=")
            Button("Smaller Text", action: run { store.setFontSize(store.fontSize - 1) })
                .keyboardShortcut("-")
            Button("Actual Size", action: run { store.setFontSize(Store.defaultFontSize) })
                .keyboardShortcut("0")
            Divider()
            // The conversation, or the live terminal on the right.
            Toggle("Terminal", isOn: Binding(get: { f.showTerminal }, set: { on in MainActor.assumeIsolated { store.showTerminal = on } }))
                .keyboardShortcut("t", modifiers: [.command, .option])
            // The session's helpers, in the right sidebar (HelperPane.swift).
            Button(f.helperShown ? "Hide Helpers" : "Show Helpers", action: run { store.toggleHelpers() })
                .keyboardShortcut("h", modifiers: [.command, .shift])
                .disabled(!f.selected)
            Divider()
            // The right sidebar and its tabs (RightSidebar.swift), with
            // the key cmux and VS Code give a right side bar.
            Button(f.sideOpen ? "Hide Right Sidebar" : "Show Right Sidebar", action: run { store.toggleSide() })
                .keyboardShortcut("b", modifiers: [.command, .option])
            ForEach(SideTab.built) { t in
                Button(t.title, action: run { store.toggleSideTab(t) })
                    .keyboardShortcut(t.key, modifiers: [.command, .option])
            }
        }
    }
}
