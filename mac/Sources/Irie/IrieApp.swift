import AppKit
import SwiftUI

@main
struct IrieApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var store = Store()

    var body: some Scene {
        Window("Irie", id: "main") {
            ContentView()
                .environmentObject(store)
                .frame(minWidth: 820, minHeight: 500)
                .task { store.start() }
        }
        .defaultSize(width: 1200, height: 760)
        .commands {
            CommandGroup(replacing: .newItem) {
                Button("New Task") { NotificationCenter.default.post(name: .irieNewTask, object: nil) }
                    .keyboardShortcut("n")
            }
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
    @State private var filter = ""

    var body: some View {
        NavigationSplitView {
            SidebarView(filter: filter)
                .navigationSplitViewColumnWidth(min: 240, ideal: 290, max: 420)
        } detail: {
            TerminalArea()
        }
        .searchable(text: $filter, placement: .sidebar, prompt: "Find a session")
        .tint(Theme.deepGreen)
    }
}
