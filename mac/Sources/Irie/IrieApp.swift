import AppKit
import SwiftUI

/// Starts the app, or with `--snapshot DIR` draws its window offscreen from
/// fixture sessions and exits (Snapshot.swift).
@main
enum Launch {
    static func main() {
        let args = CommandLine.arguments
        if let i = args.firstIndex(of: "--snapshot") {
            let dir = i + 1 < args.count ? args[i + 1] : FileManager.default.currentDirectoryPath
            MainActor.assumeIsolated { Snapshot.run(into: dir) }
        }
        if let i = args.firstIndex(of: "--render-conversation") {
            let dir = i + 1 < args.count ? args[i + 1] : FileManager.default.currentDirectoryPath
            MainActor.assumeIsolated { ConversationSnapshot.run(into: dir) }
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
                .environmentObject(store)
                .frame(minWidth: 820, minHeight: 500)
                .task { store.start() }
        }
        .defaultSize(width: 1200, height: 760)
        .windowToolbarStyle(.unifiedCompact(showsTitle: Self.showsTitle))
        .commands { IrieCommands(store: store, menu: store.menu) }
        // Irie > Settings… (⌘,): every preference in one place (Settings.swift).
        Settings {
            SettingsView(model: store.settings)
                .environmentObject(store)
        }
    }
}

/// Catches the keys that walk the sidebar before SwiftTerm, which would send
/// them to tmux, and before AppKit's key-view loop, which takes Ctrl+Tab. The
/// keyboard stays where it was.
@MainActor
final class KeyRouter {
    private var monitor: Any?
    /// Irie's own window, where the sessions are (WindowReader).
    weak var window: NSWindow?

    func install(_ store: Store) {
        guard monitor == nil else { return }
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
                    } else if let s = store.closable {
                        store.close(s)
                    } else {
                        NSSound.beep()
                    }
                }
                return nil
            }
            return event
        }
    }
}

/// Run from a plain binary the app still gets a Dock icon and the focus, and
/// it quits with its window. `--background`, which `tools/live.sh` passes when
/// it relaunches the app, leaves the focus where it is.
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_: Notification) {
        NSApp.setActivationPolicy(.regular)
        AppearanceChoice.apply()
        if !CommandLine.arguments.contains("--background") {
            NSApp.activate(ignoringOtherApps: true)
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_: NSApplication) -> Bool { true }
}

extension Notification.Name {
    /// Gives the task bar the keyboard.
    static let irieQuickTask = Notification.Name("irieQuickTask")
}

struct ContentView: View {
    @EnvironmentObject private var store: Store
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
            SidebarView()
                .id(theme)
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
        .sheet(isPresented: $store.newTaskSheet) {
            NewTaskSheet().environmentObject(store)
        }
        .background(WindowReader { keys.window = $0 })
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
