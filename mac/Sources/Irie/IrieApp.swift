import AppKit
import SwiftUI

/// Starts the app, or with `--snapshot DIR` draws its window out of sight
/// from fixture sessions and exits (Snapshot.swift), or with `--record
/// OUT.mp4 --reel FILE` films its window while a reel plays
/// (Recording.swift), or with `--away-helper` keeps a command's windows off
/// the desktops the user can see, for `irie away` (Away.swift).
@main
enum Launch {
    static func main() {
        Preview.boot()
        let args = CommandLine.arguments
        if args.contains("--away-helper") {
            MainActor.assumeIsolated { Away.run(args) }
        }
        if let i = args.firstIndex(of: "--snapshot") {
            let dir = i + 1 < args.count ? args[i + 1] : FileManager.default.currentDirectoryPath
            MainActor.assumeIsolated { Snapshot.run(into: dir) }
        }
        if let i = args.firstIndex(of: "--render-conversation") {
            let dir = i + 1 < args.count ? args[i + 1] : FileManager.default.currentDirectoryPath
            MainActor.assumeIsolated { ConversationSnapshot.run(into: dir) }
        }
        if args.contains("--bench") {
            MainActor.assumeIsolated { Bench.run() }
        }
        if let i = args.firstIndex(of: "--render-onboarding") {
            let dir = i + 1 < args.count ? args[i + 1] : FileManager.default.currentDirectoryPath
            MainActor.assumeIsolated { OnboardingSnapshot.run(into: dir) }
        }
        // Films the window while a reel plays, then exits (Recording.swift).
        if args.contains("--record") {
            MainActor.assumeIsolated { Recording.run(args) }
        }
        IrieApp.main()
    }
}

struct IrieApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    /// Held, not watched: the scene redraws nothing when the store changes,
    /// which is several times a second while agents work. The window's
    /// views watch it, and the menus watch only their own facts
    /// (MenuBar.swift).
    @State private var store = Store()

    /// The compact title bar: one short row for the traffic lights, the
    /// sidebar's counts and the shown session's name. The scene's
    /// windowToolbarStyle below says the same for SwiftUI.
    static let toolbarStyle: NSWindow.ToolbarStyle = .unifiedCompact
    /// The bar hides the window's own title: TerminalArea draws the shown
    /// session's name and subtitle as the toolbar's principal item.
    static let showsTitle = false

    var body: some Scene {
        Window("\(Brand.name)", id: "main") {
            ContentView()
                .environment(store)
                .frame(minWidth: 820, minHeight: 500)
                .task { store.start() }
        }
        .defaultSize(width: 1200, height: 760)
        .windowToolbarStyle(.unifiedCompact(showsTitle: Self.showsTitle))
        .commands {
            LearnCommands(store: store, menu: store.menu)
            IrieCommands(store: store, menu: store.menu)
            PluginCommands(store: store, menu: store.menu)
            #if !NOMARKETS
            MarketsCommands(menu: store.menu)
            #endif
            #if !NOMAIL
            MailCommands(menu: store.menu)
            #endif
        }
        // Help > Learning Resources and Glossary (Learn.swift).
        Window("Learn", id: LearnWindow.id) {
            LearnWindow()
                .environment(store)
        }
        .defaultSize(width: 560, height: 640)
        // View > Theme > Make a Theme… and View > Layout: themes and
        // layouts of your own (ThemeMaker.swift).
        Window("Make a Theme", id: ThemeMaker.windowID) {
            ThemeMaker()
                .environment(store)
        }
        .defaultSize(ThemeMaker.size)
        #if !NOMARKETS
        // Window › Markets (⇧⌘M) and :markets: the markets plugin's map
        // (Markets.swift). Its item shows only while the plugin is on, so
        // the one SwiftUI would add for the window goes.
        Window("Markets", id: MarketsWindow.id) {
            MarketsWindow()
                .environment(store)
        }
        .defaultSize(width: 1280, height: 820)
        .commandsRemoved()
        #endif
        #if !NOMAIL
        // Window › Mail (⇧⌘E): the mail plugin's accounts, list and message
        // side by side (MailViews.swift). As with Markets, its item shows
        // only while the plugin is on.
        Window("Mail", id: MailWindow.id) {
            MailWindow()
                .environment(store)
                .frame(minWidth: 760, minHeight: 420)
        }
        .defaultSize(width: 1080, height: 700)
        .commandsRemoved()
        #endif
        // Irie > Settings… (⌘,): every preference in one place (Settings.swift).
        Settings {
            SettingsView(model: store.settings)
                .environment(store)
        }
    }
}

/// Catches the keys that walk the sidebar before SwiftTerm, which would send
/// them to tmux, and before AppKit's key-view loop, which takes Ctrl+Tab. The
/// keyboard stays where it was.
@MainActor
final class KeyRouter {
    private var monitor: Any?
    private var middle: Any?
    /// Modifiers going down and up, and clicks, for the look-ahead at keys
    /// (KeyPeek.swift).
    private var flags: Any?
    private var clicks: Any?
    /// A double ↓ on an agent's empty prompt (ContinueKey.swift).
    private let continueKey = ContinueKey()
    /// Irie's own window, where the sessions are (WindowReader).
    weak var window: NSWindow?

    func install(_ store: Store) {
        guard monitor == nil else { return }
        // A middle click on a card closes it, as on a browser's tab, while
        // Settings › Tabs lets a card close from under the pointer.
        middle = NSEvent.addLocalMonitorForEvents(matching: .otherMouseUp) { [weak self, weak store] event in
            guard let self, let store, event.buttonNumber == 2 else { return event }
            return MainActor.assumeIsolated {
                guard event.window != nil, event.window === self.window, CloseFromPointer.on,
                      let id = store.hovered, let s = store.sessions.first(where: { $0.id == id }) else { return event }
                store.close(s)
                return nil
            }
        }
        // A modifier held alone shows what it does (KeyPeek.swift); a click
        // or leaving the app puts that away.
        flags = NSEvent.addLocalMonitorForEvents(matching: .flagsChanged) { [weak self, weak store] event in
            guard let self, let store else { return event }
            MainActor.assumeIsolated {
                if event.window != nil, event.window === self.window {
                    KeyPeek.shared.flags(event, window: event.window, store: store)
                } else {
                    KeyPeek.shared.drop()
                }
            }
            return event
        }
        clicks = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]) { event in
            MainActor.assumeIsolated { KeyPeek.shared.drop() }
            return event
        }
        NotificationCenter.default.addObserver(forName: NSApplication.didResignActiveNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { KeyPeek.shared.drop() }
        }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self, weak store] event in
            guard let self, let store else { return event }
            let mods = event.modifierFlags.intersection([.command, .control, .option, .shift])
            let irie = MainActor.assumeIsolated { event.window != nil && event.window === self.window }
            // The look-ahead sees every key first and takes none: a key ends
            // what a held modifier showed, and Ctrl+X in Claude Code's
            // terminal shows the keys of its chords (KeyPeek.swift).
            if irie { MainActor.assumeIsolated { KeyPeek.shared.key(event, window: event.window, store: store) } }
            // First, so a ↓ it holds back goes ahead of any other key.
            if irie, MainActor.assumeIsolated({ self.continueKey.route(event, store: store) }) == nil {
                return nil
            }
            // The tree of keys with fn (KeyTree.swift): Fn+X, then each key
            // after with fn held, as Emacs's C-x C-s. A chord under way
            // takes its keys before anything else in the window sees them;
            // a key typed without fn drops it and goes on as it would have.
            if irie, MainActor.assumeIsolated({ KeyChord.shared.route(event, store: store) }) {
                return nil
            }
            // A code card's editor has its own keys while it has the
            // keyboard: find, save, open a file, go to a line, comment, and
            // ⌘W for its file rather than the session (CodeEditor.swift).
            if irie, MainActor.assumeIsolated({ (event.window?.firstResponder as? CodeTextView)?.command(event) == true }) {
                return nil
            }
            // Each modifier with Tab walks one level: ⌃Tab the sessions,
            // ⌥Tab the terminal and the panes, Fn+Tab the spaces.
            // Fn+Tab shows the next space and Fn+Shift+Tab the one before
            // (Spaces.swift), rather than typing a Tab. Only the fn flag
            // tells them from Tab, and only here: AppKit keeps fn out of an
            // app's own menu keys. With ⌃ or ⌥ as well, fn changes nothing.
            if event.keyCode == 48, mods.isEmpty || mods == .shift, irie, event.modifierFlags.contains(.function) {
                MainActor.assumeIsolated { store.stepSpace(mods.contains(.shift) ? -1 : 1) }
                return nil
            }
            // Fn+Z starts zen, the session on show alone, full screen, and
            // ends it (Zen.swift), from anywhere in the window, the terminal
            // too, which never gets the z. As with Fn+Tab, only the fn flag
            // tells it from z, and only here. The letter, not the key's
            // place, so it is Z on every keyboard layout.
            if irie, mods.isEmpty, event.modifierFlags.contains(.function), event.charactersIgnoringModifiers?.lowercased() == "z" {
                MainActor.assumeIsolated { store.toggleZen() }
                return nil
            }
            if event.keyCode == 48, mods == .control || mods == [.control, .shift], irie { // Tab
                MainActor.assumeIsolated { store.step(mods.contains(.shift) ? -1 : 1) }
                return nil
            }
            // ⌥Tab walks the keyboard from the terminal through the panes,
            // left to right, turning the rolodex to each, and ⌥⇧Tab back
            // (Rolodex.swift).
            if event.keyCode == 48, mods == .option || mods == [.option, .shift], irie {
                MainActor.assumeIsolated { store.cyclePanes(mods.contains(.shift) ? -1 : 1, in: event.window) }
                return nil
            }
            // ⌃1–9 shows a space, and ⌃⌥1–9 sends the selected or picked
            // cards to it (Spaces.swift).
            if irie, mods == .control || mods == [.control, .option], let c = event.charactersIgnoringModifiers,
               let n = Int(c), (1...9).contains(n), MainActor.assumeIsolated({ store.canSpaces }) {
                MainActor.assumeIsolated {
                    if mods.contains(.option) { store.sendToSpace(number: n) } else { store.showSpace(number: n) }
                }
                return nil
            }
            // ⌘1–9 shows that card. Go › Sessions lists only the cards
            // there are, so this takes the key past the last one too,
            // which the terminal would otherwise get.
            if irie, mods == .command, let c = event.charactersIgnoringModifiers, let n = Int(c), (1...9).contains(n) {
                MainActor.assumeIsolated { store.showRow(n) }
                return nil
            }
            // ⇧⌘H shows the selected session's helpers, in its Helpers card
            // beside the terminal, as View › Session Helpers does.
            if irie, mods == [.command, .shift], event.charactersIgnoringModifiers?.lowercased() == "h" {
                MainActor.assumeIsolated { store.toggleHelpers() }
                return nil
            }
            if mods == .control, event.charactersIgnoringModifiers == "]" {
                MainActor.assumeIsolated { store.toggleFocus() }
                return nil
            }
            // In Irie's window ⌘W closes the selected session and ⇧⌘W the
            // window, even while the terminal has the keyboard. In any other
            // window, such as Settings or About, either closes that window:
            // the menu's ⌘W closes a session (MenuBar.swift).
            if event.charactersIgnoringModifiers?.lowercased() == "w", mods == .command || mods == [.command, .shift],
               let window = event.window {
                MainActor.assumeIsolated {
                    if !irie || mods.contains(.shift) {
                        window.performClose(nil)
                    } else if !store.closeInCodeCard(window: window) {
                        // The cards picked, or the selected one (Tabs.swift);
                        // a code card with the keyboard closes its file.
                        store.closeKey()
                    }
                }
                return nil
            }
            return event
        }
    }
}

/// Run from a plain binary the app still gets a Dock icon and the focus, and
/// it quits with its window. `--background`, which `tools/live.sh` and the
/// freeze dog pass when they open the app again, leaves the focus where it
/// is and puts the window back on the desktop it was on; `--away`, which
/// hot.sh passes its preview, puts it on a desktop the user cannot see
/// (WindowPlace, Desktops.swift). The freeze dog watches the app from the
/// moment it has launched.
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_: Notification) {
        NSApp.setActivationPolicy(.regular)
        AppearanceChoice.apply()
        // Before the window server first shows the window.
        WindowPlace.shared.start()
        if !WindowPlace.background && !WindowPlace.away {
            NSApp.activate(ignoringOtherApps: true)
        }
        // Zen as it was when the app last ran (Zen.swift).
        ZenResume.launch()
        // A relaunch can leave the sidebar folded: it comes back, unless zen
        // folded it (Looks.swift).
        WindowPanes.unfoldAfterLaunch()
        FreezeDog.start(socketPath: CorePaths.socket())
    }

    func applicationShouldTerminateAfterLastWindowClosed(_: NSApplication) -> Bool { true }
}

extension Notification.Name {
    /// Gives the task bar the keyboard.
    static let irieQuickTask = Notification.Name("irieQuickTask")
}

@MainActor
struct ContentView: View {
    @Environment(Store.self) private var store
    #if !NOMARKETS
    @Environment(\.openWindow) private var openWindow
    #endif
    @State private var keys = KeyRouter()
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id

    var body: some View {
        NavigationSplitView {
            // A new palette draws the sidebar afresh. The terminal area
            // follows the palette itself, so its terminal stays attached.
            // So does a theme of yours that changed, or one being made,
            // which the window wears meanwhile (Looks.swift).
            let _ = Theme.use(theme)
            // At its narrowest the sidebar still fits the title bar's row
            // (sun, name and counts) beside the traffic lights: the counts
            // draw tighter to fit the room the bar leaves them (Counts).
            // Red for done (ui.red_on_done) does the same: the cards draw
            // again only when their own row changes, and the colour is
            // Look's, which they don't watch.
            SidebarView()
                .id("\(theme) \(LookBook.shared.revision) \(store.redOnDone)")
                .environment(\.layoutDirection, .leftToRight)
                .navigationSplitViewColumnWidth(min: WindowPanes.fixtureSidebar ?? 260, ideal: WindowPanes.fixtureSidebar ?? 290,
                                                max: WindowPanes.fixtureSidebar ?? 420)
        } detail: {
            TerminalArea()
                .environment(\.layoutDirection, .leftToRight)
                .toolbar {
                    // Room between the sidebar's edge and the title, which
                    // the title bar would otherwise set almost against it.
                    ToolbarItem(placement: .navigation) {
                        Color.clear.frame(width: 6, height: 1).accessibilityHidden(true)
                    }
                }
        }
        // Reversed, the split view puts the sidebar on the right (Store.
        // reversed): right to left for the columns alone, each of which
        // reads left to right again inside. Zen folds the sidebar away and
        // lays out its own side, so the split view keeps to the layout's.
        .environment(\.layoutDirection, store.reversed ? .rightToLeft : .leftToRight)
        // The look-ahead at keys, over everything, like an alert
        // (KeyPeek.swift).
        .overlay { KeyPeekView(peek: KeyPeek.shared) }
        // The lines from the selected card to the terminal draw over both
        // columns, from views in the card and the frame (TabLink.swift).
        .tint(Theme.accent)
        .sheet(isPresented: Bindable(store).newTaskSheet) {
            NewTaskSheet().environment(store)
        }
        // View › Your Zen…: your zen's versions as a graph (ZenHistory.swift).
        .sheet(isPresented: Bindable(store.zen).historyShown) {
            ZenHistory().environment(store)
        }
        .background(WindowReader { window in
            keys.window = window
            WindowPanes.window = window
            // A drag of the sidebar's edge or the window's resizes the
            // terminal once, as it ends (ResizeHold.swift).
            ResizeHold.shared.watch(window)
            // The desktop it opens on and keeps (Desktops.swift).
            WindowPlace.shared.adopt(window)
            // A preview takes over the window of the build before (Preview.swift).
            PreviewState.shared.adopt(window, store: store)
            // The sidebar on its side, once the split view is there.
            DispatchQueue.main.async { WindowPanes.setReversed(store.reversed) }
            // Zen follows the window into full screen and out, and comes
            // back after a relaunch as it was (Zen.swift).
            store.watchZen(window)
            ZenResume.once { [weak window] in if let window { store.resumeZen(in: window) } }
        })
        .onAppear {
            keys.install(store)
            LookBook.shared.start(store)
        }
        .onChange(of: theme) { store.sendTheme(theme) }
        // Reversed, the sidebar goes to the right, and back (Store.reversed).
        .onChange(of: store.reversed) { _, on in WindowPanes.setReversed(on) }
        #if !NOMARKETS
        // :markets in a task bar opens the Markets window (Markets.swift).
        .onReceive(NotificationCenter.default.publisher(for: .irieMarkets)) { _ in openWindow(id: MarketsWindow.id) }
        #endif
    }
}

/// Hands over the window a view is in, once it is in one.
struct WindowReader: NSViewRepresentable {
    let found: (NSWindow) -> Void

    func makeNSView(context _: Context) -> NSView { Reader(found) }
    func updateNSView(_: NSView, context _: Context) {}

    final class Reader: NSView {
        let found: (NSWindow) -> Void

        init(_ found: @escaping (NSWindow) -> Void) {
            self.found = found
            super.init(frame: .zero)
        }

        required init?(coder _: NSCoder) { nil }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if let window { found(window) }
        }
    }
}
