import AppKit
import SwiftUI

/// `Irie --snapshot DIR` draws the window offscreen from fixture sessions,
/// never a core, in light and dark mode. It writes a PNG of each and prints
/// where the first card starts, then exits. It is how the views are checked
/// without a screen (docs/MAC.md, "Build and run").
@MainActor
enum Snapshot {
    static func run(into dir: String) -> Never {
        NSApplication.shared.setActivationPolicy(.accessory)
        // The terminal view, with the frame and the line from the selected
        // card, whichever view the app has saved, which a volatile domain
        // leaves untouched.
        UserDefaults.standard.setVolatileDomain(["showTerminal": true], forName: UserDefaults.argumentDomain)
        let store = Store(socketPath: "/nonexistent/irie-snapshot.sock")
        store.loadFixture(Fixture.sessions, selection: Fixture.selected)
        do {
            try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
            // Each palette in both modes at the default size, then the default
            // palette in the narrowest window and a wide one. The palette is
            // set in a volatile domain, so the app's saved choice is untouched.
            var shots: [(String, String, CGFloat, Bool, SidebarMode)] = []
            for p in Palette.all {
                shots.append(("\(p.id)-light", p.id, 1100, false, .repo))
                shots.append(("\(p.id)-dark", p.id, 1100, true, .repo))
            }
            shots.append(("state-light", Palette.standard.id, 1100, false, .state))
            shots.append(("state-dark", Palette.standard.id, 1100, true, .state))
            shots.append(("window-narrow", Palette.standard.id, 820, false, .repo))
            shots.append(("window-wide", Palette.standard.id, 1500, false, .repo))
            // The details at their default size, but for these two: minimized
            // to a line, and dragged as tall as the cards allow.
            let details: [String: [String: Any]] = [
                "details-minimized": [DetailsPanel.minimizedKey: true],
                "details-tall": [DetailsPanel.heightKey: 2000.0],
            ]
            shots.append(("details-minimized", Palette.standard.id, 1100, false, .repo))
            shots.append(("details-tall", Palette.standard.id, 1100, false, .repo))
            for (base, palette, width, dark, mode) in shots {
                // Set directly, so the app's saved view and folds are untouched.
                store.mode = mode
                store.folded = []
                var domain: [String: Any] = [Palette.defaultsKey: palette, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false]
                domain.merge(details[base] ?? [:]) { $1 }
                UserDefaults.standard.setVolatileDomain(domain, forName: UserDefaults.argumentDomain)
                Theme.use(palette)
                let (rep, titlebar) = window(ContentView().environmentObject(store), dark: dark, width: width, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                let top = firstCardTop(rep, below: titlebar)
                print("\(name): title bar \(fmt(titlebar)) pt, first card at \(top.map(fmt) ?? "?") pt")
            }
            // The new-task sheet, in the default palette.
            Theme.use(Palette.standard.id)
            for dark in [false, true] {
                let (rep, _) = window(NewTaskSheet().environmentObject(store), dark: dark, width: 560, height: 470)
                let name = "sheet-\(dark ? "dark" : "light").png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
        } catch {
            FileHandle.standardError.write(Data("snapshot: \(error.localizedDescription)\n".utf8))
            exit(1)
        }
        exit(0)
    }

    /// The window as the app draws it, at 2x, and its title bar's height in
    /// points. It is ordered in far off any screen, so it never shows.
    private static func window(_ root: some View, dark: Bool, width: CGFloat, height: CGFloat) -> (NSBitmapImageRep, CGFloat) {
        let w = NSWindow(contentRect: NSRect(x: -40000, y: -40000, width: width, height: height),
                         styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                         backing: .buffered, defer: false)
        w.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        w.toolbarStyle = IrieApp.toolbarStyle
        w.titleVisibility = IrieApp.showsTitle ? .visible : .hidden
        let host = NSHostingView(rootView: root)
        host.sceneBridgingOptions = [.toolbars, .title]
        w.contentView = host
        w.orderFrontRegardless()
        RunLoop.main.run(until: Date().addingTimeInterval(0.8))
        let frame = host.superview ?? host
        let rep = frame.bitmapImageRepForCachingDisplay(in: frame.bounds)!
        frame.cacheDisplay(in: frame.bounds, to: rep)
        let titlebar = w.frame.height - w.contentLayoutRect.height
        w.orderOut(nil)
        return (rep, titlebar)
    }

    /// Where the first card's top edge is, in points from the top of the
    /// window, read off the picture. Down the middle of the sidebar, below
    /// the title bar, the find field is the first thing that differs from
    /// the sidebar's ground, and the first card the second.
    private static func firstCardTop(_ rep: NSBitmapImageRep, below titlebar: CGFloat) -> CGFloat? {
        let scale = CGFloat(rep.pixelsHigh) / rep.size.height
        let x = Int(130 * scale)
        func px(_ y: Int) -> [Int] {
            var p = [Int](repeating: 0, count: 4)
            rep.getPixel(&p, atX: x, y: y)
            return p
        }
        var y = Int((titlebar + 1) * scale)
        let ground = px(y)
        var runs = 0, inRun = false
        while y < rep.pixelsHigh {
            let differs = zip(px(y), ground).prefix(3).contains { abs($0 - $1) > 6 }
            if differs, !inRun {
                runs += 1
                if runs == 2 { return CGFloat(y) / scale }
            }
            inRun = differs
            y += 1
        }
        return nil
    }

    private static func fmt(_ v: CGFloat) -> String { String(format: "%.0f", v) }
}

/// Sessions for the snapshot, in /home/alice. The first needs you, so it
/// marks where the list starts.
enum Fixture {
    static let selected = "s02docsguide"

    static var sessions: [Session] {
        let now = Date()
        func ago(_ minutes: Double) -> Date { now.addingTimeInterval(-minutes * 60) }
        let irie = "/home/alice/code/irie", bigup = "/home/alice/code/bigup"
        return [
            Session(id: "s01syncagent", name: "sync-agents", state: "needs_you", reason: "permission",
                    detail: "Asks: may it run rm -rf build?", command: "Bash · rm -rf build", created: ago(30), cwd: irie + "-sync-agents",
                    repo: irie, branch: "irie/sync-agents", profile: "claude",
                    tokens: Tokens(input: 1200, output: 3400, cacheRead: 40000, cacheWrite: 3600)),
            Session(id: selected, name: "docs-guide", state: "working", detail: "Running the tests", command: "Bash · go test ./...",
                    created: ago(26), cwd: irie + "-docs-guide", repo: irie, branch: "irie/docs-guide", profile: "claude",
                    subagents: [Subagent(agent: "sub:a1", type: "Explore", started: ago(3), detail: "Reading DESIGN.md", command: "Read · docs/DESIGN.md"),
                                Subagent(agent: "sub:a2", type: "Plan", started: ago(2))],
                    tokens: Tokens(input: 900, output: 2100, cacheRead: 21000, cacheWrite: 1800)),
            Session(id: "s03convoview", name: "conversation-view", state: "working", detail: "Editing ChatView.swift", command: "Edit · mac/Sources/Irie/ChatView.swift",
                    created: ago(20), cwd: irie + "-conversation-view", repo: irie, branch: "irie/conversation-view", profile: "claude"),
            Session(id: "s04warmlook0", name: "warm-look", state: "done", created: ago(14), cwd: irie + "-warm-look",
                    repo: irie, branch: "irie/warm-look", profile: "claude"),
            Session(id: "s05bigupm000", name: "bigup-m0", state: "working", detail: "Editing main.rs", command: "Edit · src/main.rs",
                    created: ago(12), cwd: bigup + "-bigup-m0", repo: bigup, branch: "irie/bigup-m0", profile: "claude"),
            Session(id: "s06scratch01", name: "summarise-the-notes", state: "idle", created: ago(9),
                    cwd: "/home/alice/.local/share/irie/scratch/summarise-the-notes", profile: "claude"),
            Session(id: "s07stalled01", name: "try-the-login", state: "stalled", reason: "stalled",
                    detail: "stalled: no hooks within 30s · Do you trust the files in this folder?", created: ago(6),
                    cwd: "/home/alice/scratch/try-the-login", profile: "claude"),
            Session(id: "s08exited01", name: "old-experiment", state: "exited", detail: "exited 1", exitCode: 1,
                    created: ago(4), cwd: "/home/alice/scratch/old-experiment", profile: "claude"),
            Session(id: "s09starting", name: "write-the-changelog", state: "starting", created: ago(0.2),
                    cwd: irie + "-write-the-changelog", repo: irie, branch: "irie/write-the-changelog", profile: "claude"),
        ]
    }
}
