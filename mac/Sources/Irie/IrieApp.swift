import AppKit
import SwiftUI

/// Starts the app, or with `--snapshot DIR` draws its window offscreen from
/// fixture sessions and exits (Snapshot.swift), or with `--record OUT.mp4
/// --reel FILE` films its window while a reel plays (Recording.swift).
@main
enum Launch {
    static func main() {
        Preview.boot()
        let args = CommandLine.arguments
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
        Window("Irie", id: "main") {
            ContentView()
                .environment(store)
                .frame(minWidth: 820, minHeight: 500)
                .task { store.start() }
        }
        .defaultSize(width: 1200, height: 760)
        .windowToolbarStyle(.unifiedCompact(showsTitle: Self.showsTitle))
        .commands {
            LearnCommands(store: store)
            IrieCommands(store: store, menu: store.menu)
            PluginCommands(store: store, menu: store.menu)
        }
        // Help > Learning Resources and Glossary (Learn.swift).
        Window("Learn", id: LearnWindow.id) {
            LearnWindow()
                .environment(store)
        }
        .defaultSize(width: 560, height: 640)
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
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self, weak store] event in
            guard let self, let store else { return event }
            let mods = event.modifierFlags.intersection([.command, .control, .option, .shift])
            let irie = MainActor.assumeIsolated { event.window != nil && event.window === self.window }
            if event.keyCode == 48, mods == .control || mods == [.control, .shift], irie { // Tab
                MainActor.assumeIsolated { store.step(mods.contains(.shift) ? -1 : 1) }
                return nil
            }
            if mods == .control, event.charactersIgnoringModifiers == "]" {
                MainActor.assumeIsolated { store.toggleFocus() }
                return nil
            }
            // In Irie's window ⌘W closes the selected session and ⇧⌘W the
            // window, even while the terminal has the keyboard. In any other
            // window, such as Settings or About, either closes that window:
            // the menu's ⌘W is Close Session.
            if event.charactersIgnoringModifiers?.lowercased() == "w", mods == .command || mods == [.command, .shift],
               let window = event.window {
                MainActor.assumeIsolated {
                    if !irie || mods.contains(.shift) {
                        window.performClose(nil)
                    } else {
                        // The cards picked, or the selected one (Tabs.swift).
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
/// is. The freeze dog watches the app from the moment it has launched.
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_: Notification) {
        NSApp.setActivationPolicy(.regular)
        AppearanceChoice.apply()
        if !CommandLine.arguments.contains("--background") {
            NSApp.activate(ignoringOtherApps: true)
        }
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
    @State private var keys = KeyRouter()
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id

    var body: some View {
        NavigationSplitView {
            // A new palette draws the sidebar afresh. The terminal area
            // follows the palette itself, so its terminal stays attached.
            let _ = Theme.use(theme)
            // At its narrowest the sidebar still fits the title bar's row
            // (sun, name and counts) beside the traffic lights; a row that
            // doesn't fit goes into the toolbar's overflow menu.
            // Red for done (ui.red_on_done) does the same: the cards draw
            // again only when their own row changes, and the colour is
            // Look's, which they don't watch.
            SidebarView()
                .id("\(theme) \(store.redOnDone)")
                .navigationSplitViewColumnWidth(min: 260, ideal: 290, max: 420)
        } detail: {
            TerminalArea()
                .toolbar {
                    // Room between the sidebar's edge and the title, which
                    // the title bar would otherwise set almost against it.
                    ToolbarItem(placement: .navigation) {
                        Color.clear.frame(width: 6, height: 1).accessibilityHidden(true)
                    }
                }
        }
        // The lines from the selected card to the terminal, over both columns.
        .overlayPreferenceValue(TabLinkKey.self) { TabLink(ends: $0) }
        .tint(Theme.accent)
        .sheet(isPresented: Bindable(store).newTaskSheet) {
            NewTaskSheet().environment(store)
        }
        .background(WindowReader { window in
            keys.window = window
            // A preview takes over the window of the build before (Preview.swift).
            PreviewState.shared.adopt(window, store: store)
        })
        .onAppear {
            keys.install(store)
            WindowCloseItem.install()
        }
        .onChange(of: theme) { store.sendTheme(theme) }
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
