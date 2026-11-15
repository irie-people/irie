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
        // The terminal view, with the frame and the lines from the selected
        // card, whichever view the app has saved, which a volatile domain
        // leaves untouched.
        UserDefaults.standard.setVolatileDomain(["showTerminal": true], forName: UserDefaults.argumentDomain)
        let store = Store(socketPath: "/nonexistent/\(Brand.slug)-snapshot.sock")
        store.loadFixture(Fixture.sessions, selection: Fixture.selected)
        // Notes of the fixture's own, never the app's saved copy.
        store.notes.loadFixture(Fixture.notes)
        // A theme and layouts of the fixture's own, never the files in
        // the config folder (Looks.swift).
        LookBook.shared.useFixture()
        // Closed, whatever the app has saved; a fixture store saves nothing.
        store.side = SideState()
        store.rolodex.loadFixture(session: Fixture.selected, front: nil, open: false)
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
            shots.append(("list-light", Palette.standard.id, 1100, false, .list))
            shots.append(("list-dark", Palette.standard.id, 1100, true, .list))
            shots.append(("window-narrow", Palette.standard.id, 820, false, .repo))
            shots.append(("window-wide", Palette.standard.id, 1500, false, .repo))
            // The details at their default size, but for these two: minimized
            // to a line, and dragged as tall as the cards allow.
            let details: [String: [String: Any]] = [
                "details-minimized": [DetailsPanel.minimizedKey: true],
                "details-tall": [DetailsPanel.heightKey: 2000.0],
                "notes-folded": [NotesPanel.openKey: false],
                "notes-folded-dark": [NotesPanel.openKey: false],
            ]
            shots.append(("details-minimized", Palette.standard.id, 1100, false, .repo))
            shots.append(("details-tall", Palette.standard.id, 1100, false, .repo))
            shots.append(("notes-folded", Palette.standard.id, 1100, false, .repo))
            shots.append(("notes-folded-dark", Palette.standard.id, 1100, true, .repo))
            for (base, palette, width, dark, mode) in shots {
                // Set directly, so the app's saved view and folds are untouched.
                // Parked is open but in the List view, which shows it folded,
                // as it starts.
                store.mode = mode
                store.folded = []
                store.parkedOpen = mode != .list
                var domain: [String: Any] = [Palette.defaultsKey: palette, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false, NotesPanel.openKey: true, NotesPanel.draftKey: ""]
                domain.merge(details[base] ?? [:]) { $1 }
                UserDefaults.standard.setVolatileDomain(domain, forName: UserDefaults.argumentDomain)
                Theme.use(palette)
                let (rep, titlebar) = window(ContentView().environment(store), dark: dark, width: width, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                let top = firstCardTop(rep, below: titlebar)
                print("\(name): title bar \(fmt(titlebar)) pt, first card at \(top.map(fmt) ?? "?") pt")
            }
            // The selected session beside its teammates, as the terminal shows
            // an agent team, in a wide window and a narrow one.
            store.mode = .repo
            store.loadFixture(Fixture.sessions.map { s in
                var s = s
                if s.id == Fixture.selected { s.team = Fixture.team }
                return s
            }, selection: Fixture.selected)
            Theme.use(Palette.standard.id)
            for (base, width, dark) in [("team-split", 1100.0, false), ("team-split-dark", 1100.0, true), ("team-split-wide", 1500.0, false), ("team-split-narrow", 820.0, false)] {
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false, NotesPanel.openKey: true, NotesPanel.draftKey: ""], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: CGFloat(width), height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            store.loadFixture(Fixture.sessions, selection: Fixture.selected)

            // The right sidebar (RightSidebar.swift), inline after the
            // terminal: its Dock in both modes, Helpers and Review.
            for (base, tab, dark) in [("side-dock", SideTab.dock, false), ("side-dock-dark", SideTab.dock, true),
                                      ("side-helpers", SideTab.helpers, false), ("side-review", SideTab.review, false),
                                      ("side-notices", SideTab.notices, false), ("side-notices-dark", SideTab.notices, true),
                                      ("side-narrow", SideTab.review, false)] {
                // At its narrowest, with a count on every tab, the tabs
                // draw tighter and × stays in sight.
                let narrow = base == "side-narrow"
                store.side = SideState(open: true, width: narrow ? SideState.minWidth : SideState.startWidth, tab: tab)
                if narrow { store.noticesSeen = .distantPast }
                if tab == .notices || narrow {
                    // The notices kept, and an error on show at the top of
                    // the working area, clear of the task bar.
                    let now = Date()
                    store.notices = [
                        LoggedNotice(text: "Parked “api-fix”. It is at the end of the list, in Parked.", isError: false, at: now.addingTimeInterval(-3600)),
                        LoggedNotice(text: "Added to “site-copy”.", isError: false, at: now.addingTimeInterval(-900)),
                        LoggedNotice(text: "Couldn’t start the task: no space left on device", isError: true, at: now.addingTimeInterval(-60)),
                    ]
                    store.notice = Store.Notice(text: "Couldn’t start the task: no space left on device", isError: true)
                }
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false, NotesPanel.openKey: true, NotesPanel.draftKey: ""], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // Mail (MailViews.swift): its tab with the list and with a
            // message open, its window, and its two sheets.
            for (base, opening, dark) in [("side-mail", false, false), ("side-mail-dark", false, true), ("side-mail-reading", true, false)] {
                store.mail.loadFixture(opening: opening)
                store.side = SideState(open: true, width: SideState.startWidth, tab: .mail)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            for dark in [false, true] {
                store.mail.loadFixture(opening: true)
                let (rep, _) = window(MailWindow().environment(store), dark: dark, width: 1080, height: 700)
                let name = "mail-window-\(dark ? "dark" : "light").png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            do {
                let (add, _) = window(AddMailAccount().environment(store), dark: false, width: 480, height: 330)
                try add.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent("mail-add-account.png"))
                print("mail-add-account.png")
                store.mail.loadFixture(opening: true)
                store.mail.reply()
                if let c = store.mail.composing {
                    let (rep, _) = window(MailComposer(draft: c).environment(store), dark: false, width: 560, height: 520)
                    try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent("mail-compose.png"))
                    print("mail-compose.png")
                }
                store.mail.composing = nil
                store.mail.loadFixture()
            }
            store.side = SideState()
            store.notice = nil

            // The terminal popped out into Ghostty (PopOut.swift): the note
            // in its frame, in both modes. Set directly, so nothing is saved.
            store.poppedOut = PoppedOut(pid: 0, launched: .distantPast)
            for (base, dark) in [("popped-out", false), ("popped-out-dark", true)] {
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1100, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            store.poppedOut = nil

            // The panes beside the terminal (Rolodex.swift): a card of each
            // kind with one in front, in both modes and a narrower window, a
            // blank card, and two turns drawn still, forward and back.
            let panes: [(String, PaneKind?, Bool, CGFloat)] = [
                ("panes", .terminal, false, 1500), ("panes-dark", .terminal, true, 1500), ("panes-browser", .browser, false, 1500),
                ("panes-narrow", .terminal, false, 1100), ("panes-blank", nil, false, 1500), ("panes-mail", .mail, false, 1500),
            ]
            for (base, front, dark, width) in panes {
                store.rolodex.loadFixture(session: Fixture.selected, front: front)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: width, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // A turn tilts the flap in 3D, which only SwiftUI's own renderer
            // draws, so these are the wheel alone.
            for (base, forward, at, dark) in [("panes-turn-forward", true, 0.45, false), ("panes-turn-back", false, 0.35, false), ("panes-turn-forward-dark", true, 0.3, true)] {
                let size = CGSize(width: 560, height: 640)
                store.rolodex.loadFixture(session: Fixture.selected, front: .terminal)
                store.rolodex.freezeFixtureTurn(session: Fixture.selected, forward: forward, at: at, size: size)
                let view = RolodexView().environment(store).frame(width: size.width, height: size.height).padding(16)
                    .background(Theme.ground).environment(\.colorScheme, dark ? .dark : .light)
                var png: Data?
                (NSAppearance(named: dark ? .darkAqua : .aqua) ?? NSAppearance.currentDrawing()).performAsCurrentDrawingAppearance {
                    let r = ImageRenderer(content: view)
                    r.scale = 2
                    if let image = r.cgImage { png = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) }
                }
                if let png {
                    try png.write(to: URL(fileURLWithPath: dir).appendingPathComponent(base + ".png"))
                    print(base + ".png")
                }
            }
            store.rolodex.loadFixture(session: Fixture.selected, front: nil, open: false)

            // The new-task sheet, in the default palette.
            Theme.use(Palette.standard.id)
            for dark in [false, true] {
                let (rep, _) = window(NewTaskSheet().environment(store), dark: dark, width: 560, height: 470)
                let name = "sheet-\(dark ? "dark" : "light").png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // Each tab of the Settings window, from a fixture config.
            store.settings.loadFixture(try Wire.decoder.decode(ConfigResult.self, from: Data(Fixture.config.utf8)),
                                       plugins: try Wire.decoder.decode(PluginsResult.self, from: Data(Fixture.plugins.utf8)).plugins)
            for tab in SettingsView.Tab.allCases {
                for dark in [false, true] {
                    let page = SettingsView.page(tab, model: store.settings).environment(store).tint(Theme.accent)
                    let (rep, _) = window(page, dark: dark, width: SettingsView.size.width, height: SettingsView.size.height)
                    let name = "settings-\(tab.rawValue)-\(dark ? "dark" : "light").png"
                    try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                    print(name)
                }
            }
            // Make a Theme's two pages (ThemeMaker.swift): a new theme from
            // the default palette, and the fixture's layouts.
            for (page, dark) in [(ThemeMaker.Page.colours, false), (.colours, true), (.layouts, false)] {
                let (rep, _) = window(ThemeMaker(page: page).environment(store), dark: dark,
                                      width: ThemeMaker.size.width, height: ThemeMaker.size.height)
                let name = "make-theme-\(page.rawValue)-\(dark ? "dark" : "light").png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // The fixture's own theme being changed, every colour open.
            do {
                let (rep, _) = window(ThemeMaker(opening: .edit("harbour"), everyColour: true).environment(store), dark: false,
                                      width: ThemeMaker.size.width, height: ThemeMaker.size.height)
                let name = "make-theme-change-light.png"
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
    /// points. It has to be ordered in for SwiftUI to lay it out, and macOS
    /// brings any titled window ordered in onto a screen, however far off
    /// it was placed, so it is see-through, takes no clicks and goes to a
    /// desktop the user cannot see first (Desktops.swift): it never shows.
    private static func window(_ root: some View, dark: Bool, width: CGFloat, height: CGFloat) -> (NSBitmapImageRep, CGFloat) {
        let w = NSWindow(contentRect: NSRect(x: -40000, y: -40000, width: width, height: height),
                         styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                         backing: .buffered, defer: false)
        w.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        w.toolbarStyle = IrieApp.toolbarStyle
        w.titleVisibility = IrieApp.showsTitle ? .visible : .hidden
        Desktops.drawUnseen(w)
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
/// marks where the list starts; two are parked, one of them a finished
/// turn, which says Parked and is not counted to verify; one waits on its
/// helper and teammates.
enum Fixture {
    static let selected = "s02docsguide"

    /// The notepad's notes (NotesPanel.swift): two to do, one ticked off by
    /// hand, and one sent as a jot to the selected session.
    static let notes: [Note] = {
        let now = Date()
        return [
            Note(id: "n1", text: "Try the new parser on the big repo", created: now.addingTimeInterval(-86400)),
            Note(id: "n2", text: "Ask for a review of the login fix", done: true, created: now.addingTimeInterval(-7200),
                 sent: now.addingTimeInterval(-1800), session: selected),
            Note(id: "n3", text: "Write the release notes for 1.2, with the screenshots from the docs", created: now.addingTimeInterval(-3600)),
            Note(id: "n4", text: "Rename the settings tab", done: true, created: now.addingTimeInterval(-600)),
        ]
    }()

    /// The selected session's teammates, for the split.
    static let team = [
        Teammate(session: selected + "-k1", name: "security", color: "blue", type: "Explore", title: "Review the session code", working: true),
        Teammate(session: selected + "-k2", name: "performance", color: "green", title: "Measure the login path"),
        Teammate(session: selected + "-k3", name: "tests", color: "yellow", type: "general-purpose", working: true),
    ]

    /// The plugins op's answer, as `irie plugins` prints it on a Mac with
    /// Mullvad VPN and Mullvad Browser installed.
    static let plugins = #"""
    {"plugins": [{"name": "vpn", "title": "VPN", "on": true,
      "summary": "Your VPN in every client, connected, disconnected or moved from any task bar or the menu: Mullvad VPN, Tailscale, NetBird, Cloudflare WARP, NordVPN, Cisco Secure Client, GlobalProtect, FortiClient, the system's own VPNs and any other through commands of your own.",
      "parts": [
        {"name": "vpn", "title": "Mullvad VPN", "switch": "vpn.on", "on": true, "state": "ready", "setup": ":setup vpn", "detail": "VPN on: Malmö, Sweden (se-mma-wg-002), lockdown on, 41 days left.", "summary": "See the VPN in every client, and connect or disconnect it from a task bar or the menu, or move it where the VPN has places."}
      ]},
     {"name": "privacy", "title": "Privacy", "on": true, "setup": ":setup privacy",
      "summary": "Anonymous research over Mullvad and Tor, Mullvad Browser for agents and a dead man's switch for your family, with Bitwarden for agents to come.",
      "parts": [
        {"name": "anon", "title": "Anonymous research", "switch": "anon.on", "on": true, "state": "ready", "setup": ":setup anon", "detail": "+anon tasks leave through Mullvad and +tor ones through Tor, and both search through \#(Brand.name)'s SearXNG.", "summary": "Tasks typed with +anon or +tor research in compartments."},
        {"name": "browser", "title": "Mullvad Browser for agents", "switch": "browser.connector", "on": false, "state": "off", "setup": ":setup browser", "detail": "Mullvad Browser is installed. :setup browser checks it on a page and turns this on.", "summary": "Agents browse in a real Mullvad Browser without a window."},
        {"name": "vault", "title": "Bitwarden for agents", "on": false, "state": "held", "detail": "Not in \#(Brand.name) yet. A security review found ways an agent could get past your Touch ID and read secrets, so it stays out until they are fixed.", "summary": "An agent uses a login from your Bitwarden vault without ever seeing it."},
        {"name": "switch", "title": "Dead man's switch", "on": true, "state": "ready", "setup": ":setup switch", "detail": "The guide is ready: :setup switch, or Plugins › Dead man's switch › Set Up… in the Mac app.", "summary": "A guided session sets up what your family needs if you stop checking in."}
      ]}]}
    """#

    /// The config op's answer for a file with a profile, repos, the jot dog
    /// and Mikael's good of its own, as the core writes it (config.Tree).
    static let config = #"""
    {"path": "/home/alice/.config/\#(Brand.slug)/config.toml", "config": {
        "arch": {"mikael": {"on": true, "door": true, "good": ["Give credit where it's due.", "Be honest with clients, even when it costs me the job."]},
            "gabriel": {"on": true, "outside": true, "away": "5m", "word": "10m", "heavy": 50, "spare": ["zoom.us", "FaceTime", "Music", "Spotify"], "boost": "1h"}},
        "dog": {"land": {"\#(Brand.slug)": "live"}, "nudges": 2, "watch": true},
        "trod": {"on": true, "start": "23:00", "end": "07:00", "away": "15m", "questions": true, "nudges": 8, "retry": "15m", "within": "24h", "max_working": 4, "max_plan_use": 90, "keep_awake": true,
            "rules": ["Don't spend money you didn't make.", "Don't go over the plan's weekly allotment: never buy usage credits or turn on extra usage.", "Don't push, publish or send anything off this machine: no git push, releases, posts, emails or messages."],
            "guidelines": ["When you have to choose, take the option you would recommend, and say in your last message which you took and why.", "Finish the task you were given; note new ideas and extra work for the morning instead of starting them."]},
        "learn": {"agent_tips": true, "links": {"Team handbook": "https://example.com/handbook"}, "tutor": "explain", "visual": true},
        "local": {"context": 65536, "env": {}, "kv_cache": "q8_0", "memory_gib": 0, "ollama": "", "port": 11435, "url": ""},
        "profiles": {
            "claude": {"command": "claude", "effort": "max", "harness": "claude-code", "model": "claude-opus-5-5", "permission_mode": "auto"},
            "clean": {"command": "", "effort": "max", "harness": "claude-code", "model": "claude-opus-5-5", "permission_mode": "auto"},
            "haiku": {"command": "claude", "effort": "", "harness": "claude-code", "model": "claude-haiku-5-5", "permission_mode": "auto"},
            "local": {"command": "claude", "effort": "", "harness": "claude-code", "local": {"enforce": true, "min_context": 65536, "runtime": "ollama"}, "model": "", "permission_mode": ""},
            "sonnet": {"command": "claude", "effort": "medium", "harness": "claude-code", "model": "claude-sonnet-5-5", "permission_mode": "auto"}
        },
        "repos": {"aliases": {"\#(Brand.slug)": "~/code/\#(Brand.slug)", "notes": "~/notes"}, "roots": ["~/code"]},
        "route": {"accuracy": {"claude": {"hard": 0.9, "medium": 0.94, "small": 0.98}, "haiku": {"hard": 0.3, "medium": 0.7, "small": 0.92}, "local": {"hard": 0.05, "medium": 0.4, "small": 0.85}, "sonnet": {"hard": 0.65, "medium": 0.88, "small": 0.96}}, "cost": {"claude": 1, "haiku": 0.02, "local": 0, "sonnet": 0.25}, "delegate": true, "delegate_mode": "acceptEdits", "failover": true, "local_min_accuracy": 0.6, "min_accuracy": 0.85, "pressure": 80, "prior_weight": 10, "profiles": ["local", "haiku", "sonnet", "claude"]},
        "spawn": {"close_delay": "30s", "close_when_done": true, "default_cwd": "", "default_profile": "claude", "follow_up_delay": "8s", "follow_up_window": "2m", "hide_tmux": true, "ready_timeout": "30s", "teammates": "panes",
            "preferences": ["Keep any text or notification you send the user short enough to take in at a glance on a phone: the point or the ask first, in one plain sentence, then no more than three short lines, with steps numbered, one action each. Leave out background and asides; put anything longer on a page they can open on the phone, and send its link.", "Ask before you push."]},
        "ui": {"flip_key": "C-\\", "focus_key": "C-]", "ghostty": "auto", "max_sub_rows": 6, "red_on_done": false, "helper_style": "browser", "new_tabs": "last", "freeze_dog": "10s", "sidebar_width": 34, "theme": "rosewater", "view": "conversation"},
        "usage": {"status_line": true},
        "context": {"keep_days": 30},
        "worktree": {"base": "", "branch_prefix": "\#(Brand.slug)/", "copy": [".claude/settings.local.json", "CLAUDE.local.md"], "root": "", "submodules": true}
    }}
    """#

    /// Your groups, one inside another: Work holds Mac app and
    /// patent-research, Mac app holds conversation-view and Looks, and Looks
    /// holds warm-look.
    static let groups = [GroupInfo(id: "girie0000000", name: "Work"),
                         GroupInfo(id: "gmacapp00000", name: "Mac app", parent: "girie0000000"),
                         GroupInfo(id: "glooks000000", name: "Looks", parent: "gmacapp00000")]

    static var sessions: [Session] {
        let now = Date()
        func ago(_ minutes: Double) -> Date { now.addingTimeInterval(-minutes * 60) }
        let irie = "/home/alice/code/\(Brand.slug)", bigup = "/home/alice/code/bigup"
        // Each with the colour the core gives it, from the top of the list
        // down, so cards side by side stand apart (internal/core, hue.go).
        return [
            Session(id: "s01syncagent", name: "sync-agents", state: "needs_you", reason: "permission",
                    detail: "Asks: may it run rm -rf build?", command: "Bash · rm -rf build", created: ago(30), cwd: irie + "-sync-agents",
                    repo: irie, branch: "\(Brand.slug)/sync-agents", profile: "claude",
                    tokens: Tokens(input: 1200, output: 3400, cacheRead: 40000, cacheWrite: 3600), hue: 1),
            Session(id: selected, name: "docs-guide", state: "working", detail: "Running the tests", command: "Bash · go test ./...",
                    created: ago(26), cwd: irie + "-docs-guide", repo: irie, branch: "\(Brand.slug)/docs-guide", profile: "claude",
                    subagents: [Subagent(agent: "sub:a1", type: "Explore", started: ago(3), detail: "Reading DESIGN.md", command: "Read · docs/DESIGN.md"),
                                Subagent(agent: "sub:a2", type: "Plan", started: ago(2))],
                    tokens: Tokens(input: 900, output: 2100, cacheRead: 21000, cacheWrite: 1800), hue: 5),
            Session(id: "s03convoview", name: "conversation-view", state: "working", detail: "Editing ChatView.swift", command: "Edit · mac/Sources/Irie/ChatView.swift",
                    created: ago(20), cwd: irie + "-conversation-view", repo: irie, branch: "\(Brand.slug)/conversation-view", profile: "claude",
                    group: "gmacapp00000", trod: TrodRow(sent: 2, last: ago(0.5), reason: "error"), hue: 2),
            Session(id: "s04warmlook0", name: "warm-look", state: "done", created: ago(14), cwd: irie + "-warm-look",
                    repo: irie, branch: "\(Brand.slug)/warm-look", profile: "claude", group: "glooks000000",
                    trod: TrodRow(sent: 1, last: ago(9), reason: "question"), hue: 6,
                    // It woke cold three times today, which the card says
                    // (DESIGN §6.3).
                    rebuilds: Rebuilds(today: 3, todayUSD: 4.10, week: 5, weekUSD: 7.30, cause: "you", words: "$4.10 today: woke cold 3 times"),
                    cold: Cold(tokens: 310_000, usd: 2.48, words: "~$2.48 to wake")),
            // A finished turn whose helper and teammates still work: it waits
            // on them, as its Stop said.
            Session(id: "s10patents00", name: "patent-research", state: "idle",
                    detail: "Waiting on its work in the background · Find the seed patents",
                    created: ago(18), cwd: irie + "-patent-research", repo: irie, branch: "\(Brand.slug)/patent-research", profile: "claude",
                    subagents: [Subagent(agent: "sub:b1", type: "Explore", started: ago(5), detail: "Searching the web for expired patents")],
                    team: [Teammate(session: "s10patents00-1", name: "seed-finder", color: "blue", type: "general-purpose", title: "Reading the claims", working: true),
                           Teammate(session: "s10patents00-2", name: "expand-a", color: "green", type: "general-purpose", title: "Paging the citations", working: true)],
                    subs: Subs(helpers: 1, teammates: 2),
                    tokens: Tokens(input: 2100, output: 5200, cacheRead: 61000, cacheWrite: 4400), group: "girie0000000", hue: 4),
            Session(id: "s05bigupm000", name: "bigup-m0", state: "working", detail: "Editing main.rs", command: "Edit · src/main.rs",
                    created: ago(12), cwd: bigup + "-bigup-m0", repo: bigup, branch: "\(Brand.slug)/bigup-m0", profile: "claude", hue: 3),
            Session(id: "s06scratch01", name: "summarise-the-notes", state: "done", created: ago(9),
                    cwd: "/home/alice/.local/share/\(Brand.slug)/scratch/summarise-the-notes", profile: "claude", parked: true, hue: 2),
            Session(id: "s07stalled01", name: "try-the-login", state: "stalled", reason: "stalled",
                    detail: "stalled: no hooks within 30s · Do you trust the files in this folder?", created: ago(6),
                    cwd: "/home/alice/scratch/try-the-login", profile: "claude", hue: 1),
            Session(id: "s08exited01", name: "old-experiment", state: "exited", detail: "exited 1", exitCode: 1,
                    created: ago(4), cwd: "/home/alice/scratch/old-experiment", profile: "claude", parked: true, hue: 6),
            Session(id: "s09starting", name: "write-the-changelog", state: "starting", created: ago(0.2),
                    cwd: irie + "-write-the-changelog", repo: irie, branch: "\(Brand.slug)/write-the-changelog", profile: "claude", hue: 4),
        ]
    }
}
