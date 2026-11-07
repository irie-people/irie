import AppKit
import SwiftUI

/// `Irie --bench` measures what one change to the store costs the window:
/// the main thread's CPU time for each kind of change, with 30 fixture
/// sessions drawn offscreen, never a core. It is the yardstick for "only
/// redraw what changed" (docs/proposals/optimisation.md, iteration 6): a
/// change that draws more than it shows costs here first. Like --snapshot,
/// the window is ordered in far off any screen and the app takes no focus.
@MainActor
enum Bench {
    /// The main thread's CPU time so far.
    private static func cpu() -> UInt64 { clock_gettime_nsec_np(CLOCK_THREAD_CPUTIME_ID) }

    static func run() -> Never {
        NSApplication.shared.setActivationPolicy(.accessory)
        UserDefaults.standard.setVolatileDomain(["showTerminal": true], forName: UserDefaults.argumentDomain)
        let store = Store(socketPath: "/nonexistent/irie-bench.sock")
        var rows: [Session] = []
        for i in 0..<30 {
            var s = Fixture.sessions[i % Fixture.sessions.count]
            s.id = String(format: "s%011dz", i)
            s.name = "Session \(i) " + s.name
            s.order = i
            s.parked = false
            rows.append(s)
        }
        store.loadFixture(rows, selection: rows[0].id)
        store.mode = .list
        store.folded = []
        let w = NSWindow(contentRect: NSRect(x: -40000, y: -40000, width: 1200, height: 900),
                         styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                         backing: .buffered, defer: false)
        w.toolbarStyle = IrieApp.toolbarStyle
        let host = NSHostingView(rootView: ContentView().environment(store))
        host.sceneBridgingOptions = [.toolbars, .title]
        w.contentView = host
        w.orderFrontRegardless()
        RunLoop.main.run(until: Date().addingTimeInterval(1.5))
        // One turn of the app: the run loop, where SwiftUI takes the
        // changes, then layout and drawing.
        func pass() {
            RunLoop.main.run(until: Date().addingTimeInterval(0.02))
            host.layoutSubtreeIfNeeded()
            w.displayIfNeeded()
        }
        // Each change's CPU, less what the same turns cost with no change.
        func measure(_ name: String, _ n: Int, _ change: (Int) -> Void) {
            let i0 = cpu()
            for _ in 0..<n { pass() }
            let idle = cpu() - i0
            let c0 = cpu()
            for k in 0..<n {
                change(k)
                pass()
            }
            let busy = cpu() - c0
            let ms = Double(Int64(busy) - Int64(idle)) / Double(n) / 1e6
            print(String(format: "%@: %.1f ms of the main thread per change", name, ms))
        }
        measure("warm-up", 10) { k in store.preview { r in r[k % r.count].detail = "Warming up \(k)" } }
        measure("one session's update", 40) { k in
            let i = (k * 7) % rows.count
            store.preview { r in r[i].detail = "Running step \(k)" }
        }
        measure("a click on a card", 40) { k in store.selection = rows[(k * 5 + 1) % rows.count].id }
        measure("a notice", 40) { k in store.notice = Store.Notice(text: "Notice \(k)", isError: true) }
        measure("every row changes", 20) { k in store.preview { r in for i in r.indices { r[i].detail = "All \(k)" } } }
        w.orderOut(nil)
        exit(0)
    }
}
