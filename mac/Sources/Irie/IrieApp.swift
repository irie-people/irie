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
            if event.keyCode == 48, mods == .control || mods == [.control, .shift] { // Tab
                MainActor.assumeIsolated { store.step(mods.contains(.shift) ? -1 : 1) }
                return nil
            }
            if mods == .control, event.charactersIgnoringModifiers == "]" {
                MainActor.assumeIsolated { store.toggleFocus() }
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

    var body: some View {
        NavigationSplitView {
            SidebarView()
                .navigationSplitViewColumnWidth(min: 240, ideal: 290, max: 420)
        } detail: {
            TerminalArea()
        }
        .tint(Theme.deepGreen)
        .onAppear { keys.install(store) }
    }
}
