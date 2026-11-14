import AppKit
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
        /// The selected session's name, while one of Irie's is selected
        /// rather than one outside it: the Session menu's header, and the
        /// session Close names.
        var name: String?
        var parked = false
        var canPark = false
        /// Pin, and the cards picked with ⌘ or ⇧ (Tabs.swift).
        var pinned = false
        var canPin = false
        /// Your groups (Groups.swift).
        var canGroup = false
        var picked = 0
        var frozen = false
        var canFreeze = false
        var promoted = false
        /// The first nine cards in the sidebar's order, which ⌘1 to ⌘9 show.
        var rows: [String] = []
        /// The spaces a selected card can go to, by name, and the one it is
        /// in, by its place among them (Spaces.swift).
        var spaces: [String] = []
        var space: Int?
        /// The session ⇧⌘T brings back.
        var reopen: String?
        var trodHand = false
        var canTrod = false
        var boosting = false
        var canBoost = false
        var boostAlways = false
        var canBoostAlways = false
        var vpn = false
        var lockdown = false
        var undo: String?
        var redo: String?
        var helperShown = false
        var showTerminal = false
        /// The right sidebar's tab on show; nil while it is closed.
        var sideTab: SideTab?
        var canPlugins = false
        /// The core's plugins, which the Plugins menu lists: a build
        /// without one has none of its items (DESIGN §4.17).
        var plugins: [PluginInfo] = []
        /// The core serves the vpn op: its build has the privacy plugin,
        /// whose VPN items show only then.
        var canVPN = false
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
        var f = MenuState.Facts()
        if let c = closable {
            f.name = c.name
            f.parked = c.isParked
            f.canPark = canPark
            f.pinned = c.isPinned
            f.canPin = canPin && c.peerName == nil
            f.frozen = c.isFrozen
            f.canFreeze = c.canFreeze
            f.promoted = c.isPromoted
            if c.peerName == nil {
                f.spaces = spaces.map(\.name)
                f.space = spaces.indices.first { spaceKey(at: $0) == c.spaceKey }
            }
        }
        f.picked = picked.count
        f.canGroup = canGroup && (picked.count > 1 || closable?.peerName == nil && closable != nil)
        f.rows = drawnOrder.prefix(9).map(\.name)
        f.reopen = closed.first?.name
        f.trodHand = trod?.hand == true
        f.canTrod = canTrod
        f.boosting = boosting
        f.canBoost = canBoost
        f.boostAlways = boostAlways
        f.canBoostAlways = canBoostAlways
        f.vpn = vpn != nil
        f.lockdown = vpn?.lockdown == true
        f.undo = undoLabel
        f.redo = redoLabel
        f.helperShown = helperShown
        f.showTerminal = showTerminal
        f.sideTab = side.open ? side.tab : nil
        f.canPlugins = canPlugins
        f.plugins = plugins
        f.canVPN = canVPN
        return f
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

/// How a menu names a session: whole, up to a length that keeps the menu
/// narrow, then cut off.
enum MenuText {
    static let longest = 32

    static func short(_ name: String) -> String {
        name.count > longest ? name.prefix(longest - 1).trimmingCharacters(in: .whitespaces) + "…" : name
    }

    static func quoted(_ name: String) -> String { "“" + short(name) + "”" }

    /// What ⌘W closes: the cards picked, or the selected one by name.
    static func close(_ f: MenuState.Facts) -> String {
        if f.picked > 1 { return "Close \(f.picked) Sessions" }
        return f.name.map { "Close " + quoted($0) } ?? "Close Session"
    }
}

/// The menu bar's commands. They hold the store only to act on it, and
/// watch the menu's facts alone. Like LearnCommands, they are not on the
/// main actor themselves: a menu reads and acts there, which run says.
///
/// Each menu holds what its name says, most used first: File starts and
/// closes sessions, as a browser's tabs; Session acts on the selected one
/// in the words and order of its card's menu, then on all of them; Go moves
/// between them; View says what the window shows. A plugin's own commands,
/// such as the VPN's, are in its part of the Plugins menu (Plugins.swift).
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
        }
        // Sessions close like tabs (Closing.swift), and Close names the one
        // ⌘W closes. These take the place of the window's own Close, which
        // SwiftUI gives ⌘W: the window's moves to ⇧⌘W. KeyRouter catches
        // both keys first, even while the terminal has the keyboard.
        CommandGroup(replacing: .saveItem) {
            Button(MenuText.close(f), action: run { store.closeKey() })
                .keyboardShortcut("w")
                .disabled(f.name == nil && f.picked < 2)
            // Several at once, as one step that ⌘Z takes back (Tabs.swift).
            Button("Close Other Sessions", action: run { if let s = store.closable { store.closeMany(store.others(than: s)) } })
                .disabled(f.name == nil)
            Button("Close Sessions Below", action: run { if let s = store.closable { store.closeMany(store.below(s)) } })
                .disabled(f.name == nil)
            Button("Close All Finished", action: run { store.closeFinished() })
            Button("Close Window", action: run { NSApp.keyWindow?.performClose(nil) })
                .keyboardShortcut("w", modifiers: [.command, .shift])
            Divider()
            Button(f.reopen.map { "Reopen " + MenuText.quoted($0) } ?? "Reopen Closed Session", action: run { store.reopenLast() })
                .keyboardShortcut("t", modifiers: [.command, .shift])
                .disabled(f.reopen == nil)
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
        CommandGroup(after: .sidebar) {
            // The conversation, or the live terminal in its place.
            Button(f.showTerminal ? "Show Conversation" : "Show Terminal", action: run { store.showTerminal.toggle() })
                .keyboardShortcut("t", modifiers: [.command, .option])
            // The right sidebar (RightSidebar.swift), with the key cmux and
            // VS Code give a right side bar, and its tabs, the one on show
            // ticked. Helpers opens on the selected session's helper, as
            // ⇧⌘H does (KeyRouter).
            Section("Right Sidebar") {
                Button(f.sideTab == nil ? "Show Right Sidebar" : "Hide Right Sidebar", action: run { store.toggleSide() })
                    .keyboardShortcut("b", modifiers: [.command, .option])
                ForEach(SideTab.built) { t in
                    Toggle(t.title, isOn: Binding(get: { f.sideTab == t }, set: { _ in
                        MainActor.assumeIsolated { t == .helpers ? store.toggleHelpers() : store.toggleSideTab(t) }
                    }))
                    .keyboardShortcut(t.key, modifiers: [.command, .option])
                }
            }
            // The palettes, yours after Irie's, Make a Theme, and the
            // window's layouts (Looks.swift). A section ends with a line of
            // its own.
            LookMenus(store: store, theme: $theme)
            Divider()
            Button("Bigger Text", action: run { store.setFontSize(store.fontSize + 1) })
                .keyboardShortcut("=")
            Button("Smaller Text", action: run { store.setFontSize(store.fontSize - 1) })
                .keyboardShortcut("-")
            Button("Actual Size", action: run { store.setFontSize(Store.defaultFontSize) })
                .keyboardShortcut("0")
        }
        // Lines between groups are Dividers: a Section draws a line before
        // and after itself, so one at a menu's top or bottom, or two side by
        // side, would draw a stray or a double line.
        CommandMenu("Session") {
            // The session the items act on, by name, as a line of its own.
            Text(f.name.map(MenuText.short) ?? "No Session Selected")
            SessionItems(store: store, f: f)
            Divider()
            // Gabriel's boost: your focus gets the machine first for a
            // while, busy or not; it ends by itself (DESIGN §4.11). While
            // he boosts always, one for a while has nothing to add.
            Button(f.boosting ? "Stop Boost" : "Boost Your Focus", action: run { store.setBoost(!store.boosting) })
                .disabled(!f.canBoost || f.boostAlways)
            // And whenever you're at the computer, ticked while it is on
            // (arch.gabriel.always).
            if f.canBoostAlways {
                Toggle("Always Boost Your Focus", isOn: Binding(get: { f.boostAlways }, set: { on in MainActor.assumeIsolated { store.setBoostAlways(on) } }))
            }
            // A run you start goes on while you watch, until no session has
            // work left or the plan is as full as it may get.
            Button(f.trodHand ? "Stop Trod Bot" : "Start Trod Bot Now", action: run { store.setTrod(store.trod?.hand != true) })
                .disabled(!f.canTrod)
        }
        // KeyRouter catches Ctrl+Tab, Ctrl+] and ⌘1 to ⌘9 first; the menu
        // shows them.
        CommandMenu("Go") {
            Button("Next Session", action: run { store.step(1) })
                .keyboardShortcut(.tab, modifiers: .control)
            Button("Previous Session", action: run { store.step(-1) })
                .keyboardShortcut(.tab, modifiers: [.control, .shift])
            // The first nine cards, by name, in the sidebar's order.
            Menu("Sessions") {
                ForEach(Array(f.rows.enumerated()), id: \.offset) { i, name in
                    Button(MenuText.short(name), action: run { store.showRow(i + 1) })
                        .keyboardShortcut(KeyEquivalent(Character(String(i + 1))), modifiers: .command)
                }
            }
            .disabled(f.rows.isEmpty)
            Button("Find a Session", action: run { store.focusFind += 1 })
                .keyboardShortcut("f")
            Divider()
            // The keyboard, between the session list and the message box,
            // or the terminal while it shows.
            Button("Switch Focus", action: run { store.toggleFocus() })
                .keyboardShortcut("]", modifiers: .control)
            Divider()
            // Step through the strip of the helper pane (HelperPane.swift).
            Button("Next Helper") { NotificationCenter.default.post(name: .irieHelperStep, object: 1) }
                .keyboardShortcut("]", modifiers: .command)
                .disabled(!f.helperShown)
            Button("Previous Helper") { NotificationCenter.default.post(name: .irieHelperStep, object: -1) }
                .keyboardShortcut("[", modifiers: .command)
                .disabled(!f.helperShown)
        }
    }
}

/// The Session menu's items for the selected session: its card's menu in
/// the same words and order (ControlItems), with the keys.
private struct SessionItems: View {
    let store: Store
    let f: MenuState.Facts

    private func run(_ work: @escaping @MainActor (Session) -> Void) -> () -> Void {
        { MainActor.assumeIsolated { if let s = store.closable { work(s) } } }
    }

    var body: some View {
        Button("Rename…", action: run { store.beginRename($0.id) })
            .disabled(f.name == nil)
        Button(f.frozen ? "Resume" : "Pause", action: run { store.setFrozen($0, !$0.isFrozen) })
            .disabled(f.name == nil || !f.frozen && !f.canFreeze)
        Button(f.promoted ? "Stop Promoting" : "Promote", action: run { store.setPromoted($0, !$0.isPromoted) })
            .disabled(f.name == nil)
        // The sidebar's z key does it while the list has the keyboard.
        Button(f.parked ? "Unpark" : "Park", action: run { store.setParked($0, !$0.isParked) })
            .disabled(!f.canPark)
        Button(f.pinned ? "Unpin" : "Pin", action: run { store.setPinned($0, !$0.isPinned) })
            .disabled(!f.canPin)
        // A group of the selected card, or of every card picked with it,
        // inside the group that holds them all (Groups.swift).
        Button(f.picked > 1 ? "New Group of \(f.picked) Sessions" : "New Group") { MainActor.assumeIsolated { store.newGroupKey() } }
            .disabled(!f.canGroup)
        // The keys move the selected card in its group; with ⇧, to its
        // top or bottom, without promoting it.
        Menu("Move") {
            Button("Up", action: run { store.move($0, by: -1) })
                .keyboardShortcut(.upArrow, modifiers: [.command, .option])
            Button("Down", action: run { store.move($0, by: 1) })
                .keyboardShortcut(.downArrow, modifiers: [.command, .option])
            Button("To Top of Group", action: run { store.moveToEdge($0, top: true) })
                .keyboardShortcut(.upArrow, modifiers: [.command, .option, .shift])
            Button("To Bottom of Group", action: run { store.moveToEdge($0, top: false) })
                .keyboardShortcut(.downArrow, modifiers: [.command, .option, .shift])
            if !f.spaces.isEmpty {
                Divider()
                ForEach(Array(f.spaces.enumerated()), id: \.offset) { i, name in
                    Button("To " + name, action: run { store.sendToSpace([$0], store.spaceKey(at: i)) })
                        .disabled(f.space == i)
                }
                Button("To New Space", action: run { store.newSpace(moving: [$0]) })
            }
        }
        .disabled(f.name == nil)
    }
}
