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
        IrieApp.main()
    }
}

struct IrieApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var store = Store()
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id

    /// The compact title bar: one short row for the traffic lights, the
    /// sidebar's counts and the shown session's name. The scene's
    /// windowToolbarStyle below says the same for SwiftUI.
    static let toolbarStyle: NSWindow.ToolbarStyle = .unifiedCompact

    var body: some Scene {
        Window("Irie", id: "main") {
            ContentView()
                .environmentObject(store)
                .frame(minWidth: 820, minHeight: 500)
                .task { store.start() }
        }
        .defaultSize(width: 1200, height: 760)
        .windowToolbarStyle(.unifiedCompact)
        .commands {
            SidebarCommands()
            CommandGroup(replacing: .newItem) {
                Button("New Task") { NotificationCenter.default.post(name: .irieNewTask, object: nil) }
                    .keyboardShortcut("n")
            }
            // Sessions close like tabs (Closing.swift). KeyRouter catches ⌘W
            // first; the window's own Close moves to ⇧⌘W.
            CommandGroup(after: .newItem) {
                Button("Close Session") { if let s = store.closable { store.close(s) } }
                    .keyboardShortcut("w")
                    .disabled(store.closable == nil)
                Button("Reopen Closed Session") { store.reopenLast() }
                    .keyboardShortcut("t", modifiers: [.command, .shift])
                    .disabled(store.closed.isEmpty)
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
            }
        }
    }
}

/// Catches the keys that walk the sidebar before SwiftTerm, which would send
/// them to tmux, and before AppKit's key-view loop, which takes Ctrl+Tab. The
/// keyboard stays where it was.
@MainActor
final class KeyRouter {
    private var monitor: Any?

    func install(_ store: Store) {
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak store] event in
            guard let store else { return event }
            let mods = event.modifierFlags.intersection([.command, .control, .option, .shift])
            if event.keyCode == 48, mods == .control || mods == [.control, .shift], // Tab
               event.window === NSApp.mainWindow {
                MainActor.assumeIsolated { store.step(mods.contains(.shift) ? -1 : 1) }
                return nil
            }
            if mods == .control, event.charactersIgnoringModifiers == "]" {
                MainActor.assumeIsolated { store.toggleFocus() }
                return nil
            }
            // In Irie's window ⌘W closes the selected session and ⇧⌘W the
            // window, even while the terminal has the keyboard. Other
            // windows, such as About, keep ⌘W.
            if event.charactersIgnoringModifiers?.lowercased() == "w", mods == .command || mods == [.command, .shift],
               let window = event.window, window === NSApp.mainWindow {
                MainActor.assumeIsolated {
                    if mods.contains(.shift) {
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
/// it quits with its window.
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_: Notification) {
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_: NSApplication) -> Bool { true }
}

extension Notification.Name {
    static let irieNewTask = Notification.Name("irieNewTask")
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
            SidebarView()
                .id(theme)
                .navigationSplitViewColumnWidth(min: 240, ideal: 290, max: 420)
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
        .tint(Theme.accent)
        .onAppear {
            keys.install(store)
            WindowCloseItem.install()
        }
    }
}
