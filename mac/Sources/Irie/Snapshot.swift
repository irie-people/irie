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
            // The title bar at its fullest: a session paused and one frozen,
            // the trod bot within its hours and the VPN on, beside the
            // sidebar at its narrowest, at 319 points and at its widest, and
            // reversed. Each draws the fullest look of the counts that fits,
            // and nothing goes into the bar's overflow menu (Counts).
            store.loadFixture(Fixture.sessions.map { s in
                var s = s
                if s.id == "s10patents00" { s.state = "paused" }
                if s.id == "s08exited01" { s.state = "frozen" }
                return s
            }, selection: Fixture.selected)
            store.loadTitleBarFixture()
            for (base, sidebar, reversed) in [("titlebar-260", 260.0, false), ("titlebar-319", 319.0, false), ("titlebar-420", 420.0, false),
                                              ("titlebar-reversed", 319.0, true)] {
                store.reversed = reversed
                WindowPanes.fixtureSidebar = CGFloat(sidebar)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false, NotesPanel.openKey: true, NotesPanel.draftKey: ""], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: false, width: 1100, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print("\(name): sidebar \(drawn.sidebar.map { fmt(CGFloat($0)) } ?? "?") pt, counts \(drawn.room.map { "\(fmt($0)) pt wide" } ?? "with no room read")")
            }
            WindowPanes.fixtureSidebar = nil
            store.reversed = false
            store.loadFixture(Fixture.sessions, selection: Fixture.selected)

            // The right sidebar (RightSidebar.swift), inline after the
            // terminal: your helpers in both modes, Review, then
            // Variorum's note in both modes and half set up, last, since
            // its tab shows only once Variorum is there.
            store.ownHelpers = Fixture.helpers
            for (base, tab, dark) in [("side-helpers", SideTab.helpers, false), ("side-helpers-dark", SideTab.helpers, true),
                                      ("side-review", SideTab.review, false),
                                      ("side-notices", SideTab.notices, false), ("side-notices-dark", SideTab.notices, true),
                                      ("side-narrow", SideTab.review, false),
                                      ("side-variorum", SideTab.variorum, false), ("side-variorum-dark", SideTab.variorum, true),
                                      ("side-variorum-setup", SideTab.variorum, false)] {
                // At its narrowest, with a count on every tab, the tabs
                // draw tighter and × stays in sight.
                let narrow = base == "side-narrow"
                store.side = SideState(open: true, width: narrow ? SideState.minWidth : SideState.startWidth, tab: tab)
                if narrow { store.noticesSeen = .distantPast }
                if tab == .variorum {
                    // No notice on show, and none unseen.
                    store.notice = nil
                    store.noticesSeen = Date()
                    store.variorum.useFixture(base == "side-variorum-setup" ? VariorumFixture.setup : VariorumFixture.ready,
                                              session: Fixture.selected, fresh: VariorumFixture.fresh, opened: VariorumFixture.opened)
                }
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
            // The question a close asks first when commits of a card's work
            // have not landed (Closing.swift): of one card, and of a close of
            // five, two of them not landed.
            let fix = NotLive(session: "s1", name: "api-fix", path: "/home/alice/worktrees/website/api-fix", branch: "\(Brand.slug)/api-fix", land: "live",
                              ahead: 2, commits: ["3f2a9c1 fix(api): retry a dropped connection", "b71e04d test(api): a dropped connection comes back"])
            let docs = NotLive(session: "s2", name: "Write the install guide", path: "/home/alice/worktrees/website/install-guide", branch: "\(Brand.slug)/install-guide",
                               land: "live", ahead: 5, commits: ["9e1d2aa docs: the install guide", "41c77b0 docs: the guide's pictures", "0aa1f3e docs: say what to do when it fails", "c2b8e19 docs: a first draft"])
            for (base, list, count) in [("close-not-live", [fix], 1), ("close-many-not-live", [fix, docs], 5)] {
                let alert = NotLiveConfirm.alert(list, closing: count)
                alert.layout()
                if let v = alert.window.contentView, let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) {
                    v.cacheDisplay(in: v.bounds, to: rep)
                    let name = base + ".png"
                    try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                    print(name)
                }
            }
            store.side = SideState()
            store.notice = nil

            // A plugin's pane (PluginPanes.swift): a web pane's page, and a pane
            // whose app isn't running, with the panes' tabs after Irie's.
            let withPanes = try Wire.decoder.decode(PluginsResult.self, from: Data(Fixture.plugins.utf8))
            store.plugins = withPanes.plugins
            store.bundles = withPanes.bundles ?? []
            // With the Bitwarden plugin on, each web pane's header has its key,
            // and a sign-in page has its banner while Bitwarden is locked.
            for (base, pane, down, dark, signIn) in [("side-pane", "invoices.invoices", false, false, false), ("side-pane-dark", "invoices.invoices", false, true, false),
                                                     ("side-pane-down", "bookings.bookings", true, false, false),
                                                     ("side-pane-bitwarden", "invoices.invoices", false, false, true), ("side-pane-bitwarden-dark", "invoices.invoices", false, true, true)] {
                PluginPaneFixture.down = down ? [pane] : []
                PluginPaneFixture.signIn = signIn ? [pane] : []
                store.side = SideState(open: true, width: SideState.startWidth, tab: .plugin, pane: pane)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            PluginPaneFixture.down = []
            PluginPaneFixture.signIn = []
            // Yoink's tab (YoinkViews.swift): its downloads as they go, in
            // both modes, a link's sizes to pick from, and what it lacks
            // while it lacks it.
            let yoinkPlugin = try Wire.decoder.decode(PluginInfo.self, from: Data(Fixture.yoinkPlugin.utf8))
            for (base, picking, dark, needs) in [("side-yoink", false, false, false), ("side-yoink-dark", false, true, false),
                                                 ("side-yoink-picking", true, false, false), ("side-yoink-needs", false, false, true)] {
                var plugin = yoinkPlugin
                if needs {
                    plugin.parts[0].state = "needs"
                    plugin.parts[0].detail = "It needs ffmpeg, to merge a video with its sound and to make mp3s: `brew install ffmpeg`."
                }
                store.plugins = withPanes.plugins + [plugin]
                store.yoink.loadFixture(picking: picking)
                store.side = SideState(open: true, width: SideState.startWidth, tab: .plugin, pane: "yoink.yoink")
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // The Chat tab (ChatViews.swift): the drafts waiting and each
            // account's channels in both modes, a channel open with the note
            // of text Discord hides, and the tab before any account.
            let chatPlugin = try Wire.decoder.decode(PluginInfo.self, from: Data(Fixture.chatPlugin.utf8))
            for (base, reading, empty, dark) in [("side-chat", false, false, false), ("side-chat-dark", false, false, true),
                                                 ("side-chat-reading", true, false, false), ("side-chat-empty", false, true, false)] {
                store.plugins = withPanes.plugins + [chatPlugin]
                store.chat.loadFixture(reading: reading, empty: empty)
                store.side = SideState(open: true, width: SideState.startWidth, tab: .plugin, pane: "chat.tab")
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // The Charters tab (Founding.swift): a search's passage in the
            // Constitution, found whatever its spelling, in both modes.
            let foundingPlugin = try Wire.decoder.decode(PluginInfo.self, from: Data(Fixture.foundingPlugin.utf8))
            for (base, dark) in [("side-founding", false), ("side-founding-dark", true)] {
                store.plugins = withPanes.plugins + [foundingPlugin]
                store.founding.loadFixture()
                store.side = SideState(open: true, width: SideState.startWidth, tab: .plugin, pane: "founding.founding")
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // Jammin's tab (JamminViews.swift): Spotify playing and Apple
            // Music paused, then Spotify waiting to be linked, each in both
            // modes; then the sheet that links Spotify, with a plain page
            // for Spotify's. Every name is made up, the artwork a gradient.
            let jamminPlugin = try Wire.decoder.decode(PluginInfo.self, from: Data(Fixture.jamminPlugin.utf8))
            for (base, scene, dark) in [("side-jammin", "spotify", false), ("side-jammin-dark", "spotify", true),
                                        ("side-jammin-music", "music", false), ("side-jammin-music-dark", "music", true),
                                        ("side-jammin-link", "spotify-link", false), ("side-jammin-link-dark", "spotify-link", true),
                                        ("side-jammin-files", "files", false), ("side-jammin-spotify-app-dark", "spotify-app", true)] {
                var plugin = jamminPlugin
                if scene == "spotify-link" {
                    plugin.parts[0].state = "needs"
                    plugin.parts[0].detail = "No Spotify app yet: make one at Spotify's dashboard, then Link Spotify in the Jammin tab takes its Client ID."
                }
                store.plugins = withPanes.plugins + [plugin]
                store.jammin.loadFixture(scene)
                store.side = SideState(open: true, width: SideState.startWidth, tab: .plugin, pane: "jammin.tab")
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 1000)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            for (base, step, dark) in [("jammin-link-app", 1, false), ("jammin-link-app-dark", 1, true), ("jammin-link-signin", 2, false)] {
                let sheet = JamminSignInSheet(fixture: true, step: step, clientID: step == 1 ? "0123456789abcdef0123456789abcdef" : "")
                let (rep, _) = window(sheet.environment(store), dark: dark, width: 840, height: 700)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            store.plugins = []
            store.bundles = []
            store.side = SideState()

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
            // blank card, the wheel folded down to its strip, and two turns
            // drawn still, forward and back.
            let panes: [(String, PaneKind?, Bool, CGFloat, Bool)] = [
                ("panes", .terminal, false, 1500, false), ("panes-dark", .terminal, true, 1500, false),
                ("panes-browser", .browser, false, 1500, false), ("panes-narrow", .terminal, false, 1100, false),
                ("panes-blank", nil, false, 1500, false), ("panes-mail", .mail, false, 1500, false),
                ("panes-folded", .browser, false, 1500, true), ("panes-folded-dark", .browser, true, 1500, true),
                // A session's own helpers and controls, once tabs of the right
                // sidebar.
                ("panes-helpers", .helpers, false, 1500, false), ("panes-dock", .dock, false, 1500, false),
            ]
            for (base, front, dark, width, folded) in panes {
                store.rolodex.loadFixture(session: Fixture.selected, front: front, folded: folded)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: width, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // A link clicked in the terminal (TerminalLinks.swift): a new
            // page comes up on a browser card at the wheel's right end; a
            // link to the page a card shows already brings that card forward
            // and adds none.
            for (base, link, dark) in [("panes-link", "https://example.com/docs", false), ("panes-link-dark", "https://example.com/docs", true),
                                       ("panes-link-again", "http://localhost:5173", false)] {
                store.rolodex.loadFixture(session: Fixture.selected, front: .terminal)
                store.showLink(link, in: Fixture.selected)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                let deck = store.rolodex.deck(Fixture.selected)
                print("\(name): \(deck.panes.count) cards, \(deck.frontPane?.label ?? "none") in front, keyboard on \(store.rolodex.keys ?? "the terminal")")
            }
            // Wider than fits beside the terminal, the panes go over it: on
            // top while a card has the keyboard, under the terminal while it
            // has it, and the band a drag over it shows.
            let overs: [(String, PaneKind, Bool, Bool, Bool)] = [
                ("panes-over", .browser, false, true, false), ("panes-over-dark", .terminal, true, true, false),
                ("panes-over-behind", .browser, false, false, false), ("panes-over-behind-dark", .terminal, true, false, false),
                ("panes-over-drag", .terminal, false, false, true), ("panes-over-drag-dark", .browser, true, true, true),
            ]
            for (base, front, dark, raised, drag) in overs {
                store.rolodex.loadFixture(session: Fixture.selected, front: front, over: true, share: 0.8, raised: raised,
                                          guide: drag ? RolodexAim(width: 860, place: .over) : nil)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // A code card (CodePane.swift): a file with the lines its agent
            // changed marked, in the app's own code colours in both modes,
            // and in GitHub's.
            CodeFiles.shared.fixture(Fixture.codeFolder, files: Fixture.codeFiles)
            for (base, dark, colours, tree) in [("panes-code", false, CodeThemes.standard, false), ("panes-code-dark", true, CodeThemes.standard, false),
                                                ("panes-code-github", false, "github", false), ("panes-code-tree", false, CodeThemes.standard, true)] {
                store.rolodex.loadFixture(session: Fixture.selected, front: .code)
                let doc = CodeDocs.shared.fixture(Fixture.codeFile, text: Fixture.code, theme: CodeThemes.theme(colours, dark: dark),
                                                  font: .monospacedSystemFont(ofSize: 12, weight: .regular))
                doc.markFixture(28...29)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact,
                                                         DetailsPanel.minimizedKey: false, CodeThemes.defaultsKey: colours, "codeTree": tree],
                                                        forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // A Markdown file made pretty (CodePane.swift): its editor and its
            // pretty view side by side, in both modes.
            for (base, dark) in [("panes-code-pretty", false), ("panes-code-pretty-dark", true)] {
                store.rolodex.loadFixture(session: Fixture.selected, front: .code)
                _ = CodeDocs.shared.fixture(Fixture.codeOther, text: Fixture.readme, theme: CodeThemes.theme(CodeThemes.standard, dark: dark),
                                            font: .monospacedSystemFont(ofSize: 12, weight: .regular))
                store.rolodex.show(file: Fixture.codeOther, on: "fixturecode", in: Fixture.selected)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact,
                                                         DetailsPanel.minimizedKey: false, CodeThemes.defaultsKey: CodeThemes.standard, "codeTree": false,
                                                         "codePretty": Fixture.codeOther],
                                                        forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            // A Vault card (VaultPane.swift): the notepad's note as the vault
            // keeps it, with the vault's notes beside it, in both modes, the
            // Notes panel marking it kept there; and the card with no vault.
            CodeFiles.shared.fixture(Fixture.vaultFolder, files: Fixture.vaultFiles)
            let kept = NotepadVault(path: Fixture.vaultNote, synced: Date(), problem: nil)
            // Then Reading view (VaultReading.swift): a note with what it
            // draws, alone and beside its editor, and the notepad's note
            // with its boxes to tick.
            let vaultShots: [(String, Bool, Bool, String, String)] = [
                ("panes-vault", false, true, Fixture.vaultNote, ""), ("panes-vault-dark", true, true, Fixture.vaultNote, ""),
                ("panes-vault-welcome", false, false, Fixture.vaultNote, ""),
                ("panes-vault-reading", false, true, Fixture.vaultLaunch, "r"), ("panes-vault-reading-dark", true, true, Fixture.vaultNote, "r"),
                ("panes-vault-both", false, true, Fixture.vaultLaunch, "b"), ("panes-vault-reading-kit", true, true, Fixture.vaultKit, "r"),
            ]
            for (base, dark, ready, note, look) in vaultShots {
                store.rolodex.loadFixture(session: Fixture.selected, front: .vault, note: note)
                store.notes.loadFixture(Fixture.notes, vault: ready ? kept : nil)
                store.vault.loadFixture(ready ? ObsidianResult(state: "ready", text: "", vault: Fixture.vaultFolder, name: "Notes", opened: true, notes: kept)
                    : ObsidianResult(state: "needs", text: "No vault yet."))
                _ = CodeDocs.shared.fixture(Fixture.vaultNote, text: Fixture.vaultText, theme: CodeThemes.theme(CodeThemes.standard, dark: dark),
                                            font: .monospacedSystemFont(ofSize: 12, weight: .regular))
                _ = CodeDocs.shared.fixture(Fixture.vaultLaunch, text: Fixture.vaultLaunchText, theme: CodeThemes.theme(CodeThemes.standard, dark: dark),
                                            font: .monospacedSystemFont(ofSize: 12, weight: .regular))
                _ = CodeDocs.shared.fixture(Fixture.vaultKit, text: Fixture.vaultKitText, theme: CodeThemes.theme(CodeThemes.standard, dark: dark),
                                            font: .monospacedSystemFont(ofSize: 12, weight: .regular))
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact,
                                                         DetailsPanel.minimizedKey: false, CodeThemes.defaultsKey: CodeThemes.standard, "vaultTree": true,
                                                         "vaultLooks": look.isEmpty ? "" : look + " " + note],
                                                        forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
                let name = base + ".png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                print(name)
            }
            store.notes.loadFixture(Fixture.notes)
            store.vault.loadFixture(nil)
            // A file's link clicked in the terminal: the code card comes to
            // the front with the file open at its line, and the keyboard.
            do {
                store.rolodex.loadFixture(session: Fixture.selected, front: .browser)
                let doc = CodeDocs.shared.fixture(Fixture.codeFile, text: Fixture.code, theme: CodeThemes.theme(CodeThemes.standard, dark: false),
                                                  font: .monospacedSystemFont(ofSize: 12, weight: .regular))
                doc.markFixture(28...29)
                store.open(LinkReply(in: "card", kind: "code", path: Fixture.codeFile, line: 28), from: Fixture.selected)
                UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact,
                                                         DetailsPanel.minimizedKey: false, CodeThemes.defaultsKey: CodeThemes.standard, "codeTree": false],
                                                        forName: UserDefaults.argumentDomain)
                let (rep, _) = window(ContentView().environment(store), dark: false, width: 1500, height: 720)
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent("panes-link-file.png"))
                let deck = store.rolodex.deck(Fixture.selected)
                print("panes-link-file.png: \(deck.panes.count) cards, \(deck.frontPane?.label ?? "none") in front, keyboard on \(store.rolodex.keys ?? "the terminal")")
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

            try zenShots(store, into: dir)
            try reversedShots(store, into: dir)

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
                                       plugins: withPanes.plugins, bundles: withPanes.bundles ?? [])
            for tab in SettingsView.Tab.allCases {
                for dark in [false, true] {
                    let page = SettingsView.page(tab, model: store.settings).environment(store).tint(Theme.accent)
                    let (rep, _) = window(page, dark: dark, width: SettingsView.size.width, height: SettingsView.size.height)
                    let name = "settings-\(tab.rawValue)-\(dark ? "dark" : "light").png"
                    try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
                    print(name)
                }
            }
            // Appearance as tall as the screen lets the window be, for the
            // sections under the fold, such as Links in the Terminal.
            do {
                let page = SettingsView.page(.appearance, model: store.settings).environment(store).tint(Theme.accent)
                let (rep, _) = window(page, dark: false, width: SettingsView.size.width, height: 2300)
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent("settings-appearance-tall.png"))
                print("settings-appearance-tall.png")
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

    /// Zen (Zen.swift): each layout at a laptop screen's size, as Fn+Z
    /// shows it, the list folded away and the toolbar gone, the session's
    /// card a bubble over its terminal's corner, and Keep Watch's other
    /// sessions with one that needs you.
    private static func zenShots(_ store: Store, into dir: String) throws {
        store.side = SideState()
        store.rolodex.loadFixture(session: Fixture.selected, front: nil, open: false)
        // The rail draws the cards the sidebar would: by repo, every group
        // open, Parked too.
        store.mode = .repo
        store.folded = []
        store.parkedOpen = true
        // And with the session's panes open, which zen keeps as it has them.
        let zens: [(String, ZenLayout, Bool, PaneKind?)] = [
            ("zen-centred", .centred, false, nil), ("zen-centred-dark", .centred, true, nil), ("zen-edge", .edge, false, nil),
            ("zen-edge-dark", .edge, true, nil), ("zen-watch", .watch, false, nil), ("zen-watch-dark", .watch, true, nil),
            ("zen-centred-panes", .centred, false, .helpers), ("zen-edge-panes-dark", .edge, true, .helpers),
        ]
        for (base, layout, dark, panes) in zens {
            if let panes {
                store.rolodex.loadFixture(session: Fixture.selected, front: panes)
            } else {
                store.rolodex.loadFixture(session: Fixture.selected, front: nil, open: false)
            }
            store.zen.showFixture(layout)
            UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
            Theme.use(Palette.standard.id)
            let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1512, height: 945, zen: true)
            let name = base + ".png"
            try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
            print("\(name): \(layout.title)")
        }
        // What zen offers, one thing at a time (ZenCalm.swift): a step of
        // its path, a change to try, and a rest with its breath, still.
        let rest = ZenOffer(id: "rest", kind: "rest", text: "You’ve been at it a while. Rest your eyes and follow the dot: breathe in as it rises, out as it falls.",
                            yes: "Back to It", no: "In 10 Minutes")
        let offers: [(String, Bool, ZenOffer)] = [
            ("zen-offer", false, ZenOffer(id: "eyes", kind: "learn", text: "While your agent works, rest your eyes: look at something about six metres away for twenty seconds.", yes: "Got It")),
            ("zen-try-dark", true, ZenOffer(id: "breaks", kind: "try", text: "Would you like a gentle reminder to rest every 50 minutes?", yes: "Try It", no: "Not Now")),
            ("zen-help", false, ZenOffer(id: "help", kind: "learn", text: "You matter more than the work. If things feel heavy, findahelpline.com lists free, confidential helplines near you.",
                                         yes: "Got It", link: "https://findahelpline.com/")),
            ("zen-rest", false, rest), ("zen-rest-dark", true, rest),
        ]
        store.rolodex.loadFixture(session: Fixture.selected, front: nil, open: false)
        for (base, dark, offer) in offers {
            store.zen.showFixture(.centred)
            store.zen.offer = offer
            Theme.use(Palette.standard.id)
            let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1512, height: 945, zen: true)
            let name = base + ".png"
            try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
            print("\(name): zen offers \(offer.id)")
        }
        store.zen.offer = nil
        // Your Zen, the graph explorer (ZenHistory.swift): a history that
        // goes on, comes back once, and goes on again.
        store.zen.fixtureVersions = zenVersionsFixture()
        defer { store.zen.fixtureVersions = [] }
        for dark in [false, true] {
            let (rep, _) = window(ZenHistory(seed: store.zen.fixtureVersions).environment(store), dark: dark, width: 660, height: 560)
            let name = dark ? "zen-history-dark.png" : "zen-history.png"
            try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
            print("\(name): your zen's versions")
        }
        // Off, and the first layout again for Settings' pictures.
        store.zen.showFixture(nil)
        store.zen.layout = .centred
        store.rolodex.loadFixture(session: Fixture.selected, front: nil, open: false)
    }

    /// Five versions of a zen, newest first, as the core lists them: the
    /// app's own settings first, a rest every 50 minutes, Edge to Edge for
    /// a day, back to the rests, then text a point larger.
    private static func zenVersionsFixture() -> [ZenVersion] {
        let start = Date().addingTimeInterval(-80 * 3600)
        let day = DateFormatter()
        day.dateFormat = "d MMM HH:mm"
        func zen(_ layout: String, breaks: Int, text: Int) -> ZenProfile {
            ZenProfile(layout: layout, columns: 100, textSize: text, fullScreen: true, gentle: true, breaks: breaks, night: true)
        }
        let steps: [(String, ZenProfile, TimeInterval)] = [
            ("Your zen as it was", zen("centred", breaks: 0, text: 0), 0),
            ("A rest every 50 minutes", zen("centred", breaks: 50, text: 0), 3 * 3600),
            ("Edge to Edge", zen("edge", breaks: 50, text: 0), 5 * 3600),
            ("Back to your zen of " + day.string(from: start.addingTimeInterval(3 * 3600)), zen("centred", breaks: 50, text: 0), 29 * 3600),
            ("Text a point larger", zen("centred", breaks: 50, text: 14), 77 * 3600),
        ]
        var out: [ZenVersion] = []
        var parent: String?
        for (i, (why, p, at)) in steps.enumerated() {
            let state = "sha512:" + String(repeating: String(p.layout.count + p.breaks + p.textSize, radix: 16), count: 64).prefix(128)
            let name = "sha512:" + String(repeating: String(format: "%02x", i + 10), count: 64)
            out.append(ZenVersion(name: name, parent: parent, created: start.addingTimeInterval(at), why: why, state: state, zen: p))
            parent = name
        }
        return out.reversed()
    }

    /// The layout reversed (Store.reversed): the sidebar on the right, the
    /// panes and the right sidebar on the left, the lines from the card
    /// running left into the terminal's frame; the panes folded, over the
    /// terminal, under it, and a drag of their edge; then, not reversed,
    /// the look-ahead at keys part way into Fn+X Fn+L (KeyTree.swift), and
    /// for fn, Claude Code's Ctrl+X and ⌃, and ⌘ in a code card.
    private static func reversedShots(_ store: Store, into dir: String) throws {
        store.mode = .repo
        store.folded = []
        store.parkedOpen = true
        defer {
            store.reversed = false
            store.side = SideState()
            store.rolodex.loadFixture(session: Fixture.selected, front: nil, open: false)
            KeyChord.shared.showFixture([])
            KeyPeek.shared.showFixture(nil)
            store.teach.loadFixture(shown: false, asked: nil, in: nil, result: nil)
        }
        let shots: [(String, Bool, Bool, @MainActor () -> Void)] = [
            ("reversed", false, true, {
                store.side = SideState(open: true, width: SideState.startWidth, tab: .review)
                store.rolodex.loadFixture(session: Fixture.selected, front: .terminal)
            }),
            ("reversed-dark", true, true, {
                store.side = SideState(open: true, width: SideState.startWidth, tab: .review)
                store.rolodex.loadFixture(session: Fixture.selected, front: .browser)
            }),
            ("reversed-folded", false, true, {
                store.side = SideState()
                store.rolodex.loadFixture(session: Fixture.selected, front: .terminal, folded: true)
            }),
            ("reversed-over", false, true, {
                store.side = SideState()
                store.rolodex.loadFixture(session: Fixture.selected, front: .browser, over: true, share: 0.8)
            }),
            ("reversed-over-behind", false, true, {
                store.side = SideState()
                store.rolodex.loadFixture(session: Fixture.selected, front: .terminal, over: true, share: 0.8, raised: false)
            }),
            ("reversed-over-drag", true, true, {
                store.side = SideState()
                store.rolodex.loadFixture(session: Fixture.selected, front: .terminal, over: true, share: 0.8, raised: false,
                                          guide: RolodexAim(width: 860, place: .over))
            }),
            ("keys-hint", false, false, {
                store.side = SideState()
                store.rolodex.loadFixture(session: Fixture.selected, front: nil, open: false)
                KeyChord.shared.showFixture(["x", "l"])
            }),
            ("keys-hint-dark", true, false, {
                KeyChord.shared.showFixture(["x"])
            }),
            // The look-ahead at keys (KeyPeek.swift): fn held alone, Ctrl+X
            // in Claude Code's terminal, ⌃ held there, and ⌘ in a code card.
            ("keys-peek-fn", false, false, {
                KeyChord.shared.showFixture([])
                KeyPeek.shared.showFixture(KeyCatalog.root())
            }),
            ("keys-peek-claude-x", true, false, {
                KeyPeek.shared.showFixture(KeyCatalog.claudeChord(store: store))
            }),
            ("keys-peek-claude-control", false, false, {
                KeyPeek.shared.showFixture(KeyCatalog.held(.control, place: .agent(claude: true), store: store))
            }),
            ("keys-peek-code", false, false, {
                KeyPeek.shared.showFixture(KeyCatalog.held(.command, place: .code, store: store))
            }),
            // The Teach tile (TeachTile.swift) at its place at the top
            // right, with a card of each of the catalog's pictures, and
            // as it opens, before anything is asked.
            ("teach-tile", false, false, {
                KeyPeek.shared.showFixture(nil)
                let name = store.sessions.first { $0.id == Fixture.selected }?.name ?? "docs guide"
                store.teach.loadFixture(shown: true, asked: Fixture.teachQuestion, in: (Fixture.selected, name), result: Fixture.teachCard)
            }),
            ("teach-tile-dark", true, false, {
                let name = store.sessions.first { $0.id == Fixture.selected }?.name ?? "docs guide"
                store.teach.loadFixture(shown: true, asked: Fixture.teachQuestion, in: (Fixture.selected, name), result: Fixture.teachCard)
            }),
            ("teach-tile-empty", false, false, {
                store.teach.loadFixture(shown: true, asked: nil, in: nil, result: nil,
                                        recent: [Fixture.teachQuestion, "What is a worktree?"], topics: Fixture.teachTopics)
            }),
            // With the panes open beside the terminal: over the middle of
            // the terminal still, over the panes where it must be.
            ("teach-tile-panes", false, false, {
                let name = store.sessions.first { $0.id == Fixture.selected }?.name ?? "docs guide"
                store.rolodex.loadFixture(session: Fixture.selected, front: .terminal)
                // A step of a tour: Your groups, the third topic of the guide.
                let groups = Fixture.teachTopics[2]
                store.teach.loadFixture(shown: true, asked: TeachTile.question(groups), in: (Fixture.selected, name), result: Fixture.teachCard,
                                        topics: Fixture.teachTopics, tour: groups)
            }),
        ]
        for (base, dark, reversed, set) in shots {
            store.reversed = reversed
            set()
            UserDefaults.standard.setVolatileDomain([Palette.defaultsKey: Palette.standard.id, "showTerminal": true, DetailsPanel.heightKey: DetailsPanel.compact,
                                                     DetailsPanel.minimizedKey: false], forName: UserDefaults.argumentDomain)
            Theme.use(Palette.standard.id)
            let (rep, _) = window(ContentView().environment(store), dark: dark, width: 1500, height: 720)
            let name = base + ".png"
            try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
            print("\(name): sidebar \(WindowPanes.sidebar.map { $0.shown ? "shown" : "folded" } ?? "not found")")
        }
    }

    /// The sidebar's width and the counts' room in the window drawn last.
    private static var drawn: (sidebar: Double?, room: CGFloat?) = (nil, nil)

    /// The window as the app draws it, at 2x, and its title bar's height in
    /// points. It has to be ordered in for SwiftUI to lay it out, and macOS
    /// brings any titled window ordered in onto a screen, however far off
    /// it was placed, so it is see-through, takes no clicks and goes to a
    /// desktop the user cannot see first (Desktops.swift): it never shows.
    private static func window(_ root: some View, dark: Bool, width: CGFloat, height: CGFloat, zen: Bool = false) -> (NSBitmapImageRep, CGFloat) {
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
        if zen {
            // As zen's full screen shows it: no window buttons, no toolbar,
            // and the sidebar folded away, once the window's first layout
            // has made them, which a busy machine takes a while over.
            for b in [NSWindow.ButtonType.closeButton, .miniaturizeButton, .zoomButton] { w.standardWindowButton(b)?.isHidden = true }
            let until = Date().addingTimeInterval(5)
            while (WindowPanes.window !== w || WindowPanes.sidebar == nil || w.toolbar == nil) && Date() < until {
                RunLoop.main.run(until: Date().addingTimeInterval(0.1))
            }
            w.toolbar?.isVisible = false
            WindowPanes.setSidebar(shown: false, width: nil)
        }
        RunLoop.main.run(until: Date().addingTimeInterval(0.8))
        if zen {
            print("  zen: sidebar \(WindowPanes.sidebar.map { $0.shown ? "shown" : "folded" } ?? "not found"), toolbar \(w.toolbar?.isVisible == true ? "shown" : "hidden")")
        }
        let frame = host.superview ?? host
        let rep = frame.bitmapImageRepForCachingDisplay(in: frame.bounds)!
        frame.cacheDisplay(in: frame.bounds, to: rep)
        // The sidebar's width and the counts' room as drawn, which the
        // window answers for only while it is on screen.
        drawn = (WindowPanes.sidebar?.width, TitleBarRoom.shared.room)
        // What the title bar had no room for, as it was drawn, after the
        // layout drawing it takes: a picture that loses an item to the
        // overflow menu says which, by its label or its width.
        let inBar = Set((w.toolbar?.visibleItems ?? []).map(\.itemIdentifier))
        let over = (w.toolbar?.items ?? []).filter { $0.view != nil && !inBar.contains($0.itemIdentifier) }
        if !over.isEmpty, w.toolbar?.isVisible == true {
            let named = over.map { $0.label.isEmpty ? "\(Int($0.view?.fittingSize.width ?? 0)) pt wide" : $0.label }
            print("  title bar: \(over.count) item\(over.count == 1 ? "" : "s") in its overflow menu: \(named.joined(separator: ", "))")
        }
        let titlebar = w.frame.height - w.contentLayoutRect.height
        w.orderOut(nil)
        // Its views go, so they no longer act on what the next pictures set.
        w.contentView = nil
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
    /// Your helpers (YourHelpers.swift): one at work with three items, and
    /// one whose plugin is off.
    static var helpers: [HelperInfo] {
        let since = ISO8601DateFormatter().string(from: Calendar.current.date(bySettingHour: 9, minute: 12, second: 0, of: Date()) ?? Date())
        return [
            HelperInfo(id: "inbox.listen", plugin: "inbox", title: "Texts to tasks", icon: "tray", on: true, runs: true, running: true,
                       since: since, state: "working", line: "Watching what you text yourself: 2 at work, 5 done today",
                       items: [
                           HelperItemInfo(title: "Tabs that join their panel", detail: "With a session since 15:01", state: "working", session: selected),
                           HelperItemInfo(title: "Upscaling video", detail: "Report ready: oversold, one tip worth keeping", state: "done",
                                          open: "/Users/alice/Documents/Inbox/upscaling.md"),
                           HelperItemInfo(title: "A reel from a private account", detail: "Couldn’t fetch it after 5 tries", state: "error"),
                       ],
                       open: "/Users/alice/Documents/Inbox"),
            HelperInfo(id: "backups.watch", plugin: "backups", title: "Backups", icon: "externaldrive", on: false, runs: true, running: false),
        ]
    }

    static let selected = "s02docsguide"

    /// The selected session's card of where it stands, being made again
    /// while the session works (docs/proposals/materialized-cards.md).
    static var standsCard: MaterializeResult {
        let made = ISO8601DateFormatter().string(from: Date().addingTimeInterval(-180))
        let json = """
        {"id": "session/\(selected)/status", "state": "refreshing", "by": "bonsai-2:9b", "made": "\(made)",
         "spec": {"title": "Writing the docs guide", "body": [
           {"type": "Text", "text": "It checks the guide against DESIGN.md before it runs the tests again."},
           {"type": "List", "items": [
             {"title": "Wrote the guide's outline", "detail": "docs/guide.md", "status": "ok"},
             {"title": "Linked each part to DESIGN", "status": "ok"},
             {"title": "One test fails", "detail": "TestGuideLinks", "status": "warn"}]},
           {"type": "Progress", "label": "Sections written", "value": 4, "total": 6}]}}
        """
        return try! Wire.decoder.decode(MaterializeResult.self, from: Data(json.utf8))
    }

    /// What the Teach tile is asked in the snapshots, and the card a local
    /// model made for it, with each of the catalog's pictures.
    static let teachQuestion = "How does a kept card know it is out of date?"

    /// What a project's docs teach, as the learn op gives it, for the tile's
    /// tour: a guide's topics, then a design's.
    static let teachTopics: [LearnTopic] = [
        LearnTopic(title: "A card per session, in a list or a tree", file: "docs/MAC.md", heading: "1. What it shows"),
        LearnTopic(title: "Cards that never move", file: "docs/MAC.md", heading: "1. What it shows"),
        LearnTopic(title: "Your groups", file: "docs/MAC.md", heading: "1. What it shows"),
        LearnTopic(title: "Spaces", file: "docs/MAC.md", heading: "1. What it shows"),
        LearnTopic(title: "Drag to reorder", file: "docs/MAC.md", heading: "1. What it shows"),
        LearnTopic(title: "Session", file: "docs/DESIGN.md", heading: "Terms"),
        LearnTopic(title: "Hold", file: "docs/DESIGN.md", heading: "Terms"),
    ]
    static var teachCard: MaterializeResult {
        let made = ISO8601DateFormatter().string(from: Date().addingTimeInterval(-120))
        let json = """
        {"id": "explain/\(selected)/3f1a", "state": "fresh", "by": "bonsai-2:9b", "made": "\(made)",
         "spec": {"title": "When a kept card is made again", "body": [
           {"type": "Flow", "items": [
             {"title": "A view asks", "detail": "by the card's name"},
             {"title": "The store answers", "detail": "with what it kept, at once"},
             {"title": "Out of date?", "detail": "inputs moved, or as old as Every"}]},
           {"type": "Diagram", "links": [
             {"from": "Cold", "to": "Fresh", "label": "first card"},
             {"from": "Fresh", "to": "Stale", "label": "inputs moved"},
             {"from": "Stale", "to": "Refreshing", "label": "a remake begins"},
             {"from": "Refreshing", "to": "Fresh", "label": "the new card"}]},
           {"type": "Compare", "sides": [
             {"title": "Inputs", "points": ["what the card reads", "a change puts it behind"]},
             {"title": "Recipe", "points": ["what makes the card", "a change leaves it cold"]}]},
           {"type": "Code", "language": "go", "text": "if r.Gap > 0 && age < r.Gap {\\n\\treturn false\\n}\\nreturn c.Inputs != r.Inputs"},
           {"type": "Text", "style": "caption", "text": "Gap comes first: it is the most a card can cost."}]}}
        """
        return try! Wire.decoder.decode(MaterializeResult.self, from: Data(json.utf8))
    }

    /// The code card's file, in the selected session's folder, and another
    /// it has open.
    static var codeFolder: String { "/home/alice/code/\(Brand.slug)-docs-guide" }
    static var codeFile: String { codeFolder + "/internal/session/store.go" }
    static var codeOther: String { codeFolder + "/README.md" }
    /// The files of its folder, for the tree.
    static let codeFiles = ["README.md", "go.mod", "main.go", "internal/session/store.go", "internal/session/store_test.go",
                            "internal/session/cookie.go", "internal/login/login.go", "internal/login/login_test.go",
                            "web/index.html", "web/app.js", "web/style.css", "docs/setup.md"]
    /// The fixture folder's README.md, made pretty in the code card's
    /// snapshots.
    static let readme = """
    # docs-guide

    Keeps a team's docs next to the code they describe, and says which ones fell behind.

    ## What it does

    - Finds the docs that name a function you changed
    - Opens each one beside the code, at the line
      - in the session's own code card
    - Marks the ones left behind, so **nothing goes stale**

    ## Try it

    ```sh
    go run . check ./...
    ```

    | Command | What it does |
    |---|---|
    | `check` | Lists the docs out of step with the code |
    | `open` | Shows a doc beside the code it names |

    > A doc that drifts is worse than none.

    ---

    Written for the fixture, about a made-up tool.
    """

    static let code = """
    package session

    import (
    \t"errors"
    \t"sync"
    \t"time"
    )

    // ErrClosed is returned once the store has been closed.
    var ErrClosed = errors.New("session store closed")

    // Store keeps each signed-in session until it expires.
    type Store struct {
    \tmu       sync.Mutex
    \tsessions map[string]Session
    \tttl      time.Duration
    \tclosed   bool
    }

    // Put saves a session and waits for the write to land, so a
    // sign-in that follows reads it back.
    func (s *Store) Put(id string, sess Session) error {
    \ts.mu.Lock()
    \tdefer s.mu.Unlock()
    \tif s.closed {
    \t\treturn ErrClosed
    \t}
    \tsess.Expires = time.Now().Add(s.ttl)
    \ts.sessions[id] = sess
    \treturn nil
    }

    // Get returns a session that has not expired.
    func (s *Store) Get(id string) (Session, bool) {
    \ts.mu.Lock()
    \tdefer s.mu.Unlock()
    \tsess, ok := s.sessions[id]
    \tif !ok || time.Now().After(sess.Expires) {
    \t\treturn Session{}, false
    \t}
    \treturn sess, true
    }

    """

    /// The notepad's notes (NotesPanel.swift): two to do, one ticked off by
    /// hand, and one sent as a jot to the selected session.
    /// A vault (VaultPane.swift): its folder, its notes, the notepad's note
    /// in it as the core writes it, and the notes that link to that.
    static var vaultFolder: String { "/home/alice/Notes" }
    static var vaultNote: String { vaultFolder + "/\(Brand.name) Notes.md" }
    static var vaultFiles: [String] {
        ["\(Brand.name) Notes.md", "Inbox.md", "Reading list.md", "Projects/Launch.md", "Projects/Roadmap.md", "Daily/2026-11-20.md", "Daily/2026-11-21.md",
         "Projects/Release kit.md", "Attachments/cover.png"]
    }
    static var vaultText: String {
        """
        %% \(Brand.name) keeps your notepad here, a task a note: tick, add or change one, and \(Brand.name)'s notepad follows within seconds, and back. %%

        - [ ] Try the new parser on the big repo ^i3k2d9a
        - [x] Ask for a review of the login fix ^ia7c2mx
        - [ ] Write the release notes for 1.2, with the screenshots from the [[Projects/Launch|launch]] docs ^iq7z2bt
        - [x] Rename the settings tab ^ip2m4kd

        ## Later
        Ideas that aren't tasks yet stay here, as written. #someday ==after 1.2==

        """
    }
    static let vaultLinked = ["Daily/2026-11-21.md", "Projects/Launch.md"]
    /// A note with what Reading view draws (VaultReading.swift): properties,
    /// links, a tag and a highlight, a callout that folds, tasks, a table,
    /// code, a picture and another note's section embedded.
    static var vaultLaunch: String { vaultFolder + "/Projects/Launch.md" }
    static var vaultLaunchText: String {
        """
        ---
        status: in progress
        tags: [launch, release]
        due: 2026-12-08
        ---
        # Launch

        The 1.2 release goes out with the [[Projects/Roadmap|roadmap]]'s first part. Notes from the [[Daily/2026-11-21]] stand-up are in #launch, and ==the screenshots come first==. %% a reminder only the editor shows %%

        > [!tip] Before the post
        > Check the links on **example.com** work, and that the [[Inbox]] is empty.

        > [!faq]- Why a week early?
        > The conference talk is on the 10th.

        - [x] Draft the release notes ^ia7c2mx
        - [ ] Record the demo
        	- [ ] Write the script
        - [ ] Post on the blog

        | Part | Owner | Done |
        | --- | --- | :-: |
        | Notes | alice | yes |
        | Demo | alice | no |

        ```swift
        let release = Release(version: "1.2", date: "2026-12-08")
        print(release.notes) // ready
        ```

        ![[Attachments/cover.png|240]]

        ![[Projects/Roadmap#Next]]
        """
    }
    /// A note of code, a picture and another note's section embedded.
    static var vaultKit: String { vaultFolder + "/Projects/Release kit.md" }
    static var vaultKitText: String {
        """
        ## The kit
        ```swift
        let release = Release(version: "1.2", date: "2026-12-08")
        print(release.notes) // ready
        ```

        ![[Attachments/cover.png|240]]

        ![[Projects/Roadmap#Next]]

        ![[Projects/Budget]]
        """
    }

    /// The notes Reading view embeds, by their path in the vault.
    static let vaultEmbeds: [String: String] = [
        "Projects/Roadmap.md": "# Roadmap\n\n## Next\nThe phone's own view of the vault, and a graph of the links.\n\n## Later\nCanvas files.\n",
    ]
    /// The picture Reading view embeds: a gradient drawn here, as no file
    /// is read.
    static let vaultImage: NSImage? = {
        let img = NSImage(size: NSSize(width: 480, height: 200))
        img.lockFocus()
        NSGradient(colors: [NSColor(calibratedRed: 0.36, green: 0.55, blue: 0.62, alpha: 1), NSColor(calibratedRed: 0.62, green: 0.48, blue: 0.66, alpha: 1)])?
            .draw(in: NSRect(x: 0, y: 0, width: 480, height: 200), angle: 20)
        let words = NSAttributedString(string: "\(Brand.name) 1.2", attributes: [.font: NSFont.systemFont(ofSize: 40, weight: .bold), .foregroundColor: NSColor.white])
        words.draw(at: NSPoint(x: 40, y: 76))
        img.unlockFocus()
        return img
    }()

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
      "summary": "Anonymous research over Mullvad and Tor, Mullvad Browser for agents and a dead man's switch for your family.",
      "parts": [
        {"name": "anon", "title": "Anonymous research", "switch": "anon.on", "on": true, "state": "ready", "setup": ":setup anon", "detail": "+anon tasks leave through Mullvad and +tor ones through Tor, and both search through \#(Brand.name)'s SearXNG.", "summary": "Tasks typed with +anon or +tor research in compartments."},
        {"name": "browser", "title": "Mullvad Browser for agents", "switch": "browser.connector", "on": false, "state": "off", "setup": ":setup browser", "detail": "Mullvad Browser is installed. :setup browser checks it on a page and turns this on.", "summary": "Agents browse in a real Mullvad Browser without a window."},
        {"name": "switch", "title": "Dead man's switch", "on": true, "state": "ready", "setup": ":setup switch", "detail": "The guide is ready: :setup switch, or Plugins › Dead man's switch › Set Up… in the Mac app.", "summary": "A guided session sets up what your family needs if you stop checking in."}
      ]},
      {"name": "writing", "title": "Writing", "on": true,
       "summary": "Agents that help with your writing in Variorum, Lillian Duarte's writing app, with notes and other versions beside your words, never over them.",
       "parts": [
        {"name": "variorum", "title": "Writing in Variorum", "on": true, "state": "ready", "setup": ":write", "detail": "Ready: :write, or Plugins › Writing in Variorum › Start Writing in Variorum… in the Mac app, starts a writing session.", "summary": "`:write` starts a session that works in Variorum through its door for AI."}
      ]},
      {"name": "bitwarden", "title": "Bitwarden", "on": true,
       "summary": "Your Bitwarden logins fill the sign-in pages of your panes in the right sidebar.",
       "parts": [
        {"name": "autofill", "title": "Fill sign-ins in panes", "on": true, "state": "ready", "summary": "A pane's sign-in page fills itself with the login your vault has for its address.",
         "detail": "Ready: unlock Bitwarden with a pane's key, or in the Plugins menu, and each pane's sign-in fills itself."},
        {"name": "vault", "title": "Bitwarden for agents", "on": false, "state": "held", "detail": "Not in \#(Brand.name) yet. A security review found ways an agent could get past your Touch ID and read secrets, so it stays out until they are fixed.", "summary": "An agent uses a login from your Bitwarden vault without ever seeing it."}
      ]},
      {"name": "bookings", "title": "Bookings", "on": true, "manifest": "/home/alice/.config/\#(Brand.slug)/plugins/bookings.toml",
       "summary": "The booking calendar, beside your sessions.",
       "parts": [
        {"name": "bookings", "title": "Bookings", "on": true, "state": "needs", "detail": "Nothing answers at 127.0.0.1:8090 yet. Start it: cd ~/code/bookings && make serve",
         "summary": "A tab in the Mac app's right sidebar that shows http://127.0.0.1:8090/, with logins of its own that last.",
         "pane": {"id": "bookings.bookings", "title": "Bookings", "icon": "calendar", "kind": "web", "address": "http://127.0.0.1:8090/", "origin": "http://127.0.0.1:8090", "start": "cd ~/code/bookings && make serve", "store": "bookings"}}
      ]},
      {"name": "invoices", "title": "Invoices", "on": true, "manifest": "/home/alice/.config/\#(Brand.slug)/plugins/invoices.toml",
       "summary": "The invoicing app on this Mac, beside your sessions.",
       "parts": [
        {"name": "invoices", "title": "Invoices", "on": true, "state": "ready", "detail": "Ready: its tab in the right sidebar shows http://127.0.0.1:8080/.",
         "summary": "A tab in the Mac app's right sidebar that shows http://127.0.0.1:8080/, with logins of its own that last.",
         "pane": {"id": "invoices.invoices", "title": "Invoices", "icon": "doc.text", "kind": "web", "address": "http://127.0.0.1:8080/", "origin": "http://127.0.0.1:8080", "start": "cd ~/code/invoices && make serve", "store": "invoices"}}
      ]}],
     "bundles": [{"name": "shop", "title": "Shop", "summary": "The apps that run the shop, with one switch for all of them.", "manifest": "/home/alice/.config/\#(Brand.slug)/plugins/shop.toml", "state": "on",
      "plugins": [
        {"name": "invoices", "plugin": "invoices", "title": "Invoices", "on": true, "state": "ready", "detail": "Ready: its tab in the right sidebar shows http://127.0.0.1:8080/."},
        {"name": "bookings", "plugin": "bookings", "title": "Bookings", "on": true, "state": "needs", "detail": "Nothing answers at 127.0.0.1:8090 yet. Start it: cd ~/code/bookings && make serve"},
        {"name": "writing.variorum", "plugin": "writing", "part": "variorum", "title": "Writing in Variorum", "on": true, "state": "ready", "detail": "Ready: :write starts a writing session. It has no switch of its own: it is on while the Writing plugin is."}
      ]}]}
    """#

    /// The Yoink plugin, on and ready, with its tab.
    static let yoinkPlugin = #"""
    {"name": "yoink", "title": "Yoink", "on": true,
     "summary": "Download a video, or its sound as an mp3, from a link to YouTube, Vimeo, Wikimedia Commons and the other sites yt-dlp knows, into your Downloads folder.",
     "parts": [
      {"name": "yoink", "title": "Video downloads", "on": true, "state": "ready",
       "detail": "Ready: yt-dlp 2026.10.26 through uvx, with ffmpeg; downloads land in ~/Downloads.",
       "summary": "Paste a link in the Yoink tab, or give it to :yoink in any task bar, and pick a size or the sound alone.",
       "pane": {"id": "yoink.yoink", "title": "Yoink", "icon": "arrow.down.circle", "kind": "yoink"}}
     ]}
    """#

    /// The chat plugin, on, with its tab on its Discord part.
    static let chatPlugin = #"""
    {"name": "chat", "title": "Chat", "on": true,
     "summary": "Your Discord servers and Slack workspaces beside your sessions, through bots of your own.",
     "parts": [
      {"name": "discord", "title": "Discord", "on": true, "state": "ready",
       "detail": "Ready: 1 account (acme). :chat, or Chat in the right sidebar, reads them.",
       "summary": "Your Discord servers, through a bot of yours.",
       "pane": {"id": "chat.tab", "title": "Chat", "icon": "bubble.left.and.bubble.right", "kind": "chat"}},
      {"name": "slack", "title": "Slack", "on": true, "state": "ready", "detail": "Ready: 1 account (team).", "summary": "Your Slack workspace, through an app of yours."},
      {"name": "agents", "title": "Chat for agents", "on": true, "state": "ready", "switch": "chat.agents", "detail": "Ready: agents write drafts that wait for you to send.", "summary": "Agents write drafts that wait for you to send."}
     ]}
    """#

    /// The founding plugin, on and ready, with its tab.
    static let foundingPlugin = #"""
    {"name": "founding", "title": "Charters", "on": true,
     "summary": "Thirty-eight texts of rights, word for word from the bodies that keep them and checked against other copies, from Magna Carta to India's Right to Information Act.",
     "parts": [
      {"name": "founding", "title": "The charters", "on": true, "state": "ready",
       "detail": "Ready: 38 texts in the Charters tab, and the founding-documents skill in every session.",
       "summary": "Read them in the Charters tab, search them whatever the spelling, and follow how each drew on the ones before it.",
       "pane": {"id": "founding.founding", "title": "Charters", "icon": "building.columns", "kind": "founding"}}
     ]}
    """#

    /// The Jammin plugin, on, its Spotify part ready, with its tab.
    static let jamminPlugin = #"""
    {"name": "jammin", "title": "Jammin", "on": true,
     "summary": "Your music beside your sessions: what plays on Spotify or in the Music app, its controls, your playlists and a search.",
     "parts": [
      {"name": "spotify", "title": "Spotify", "on": true, "state": "ready",
       "detail": "Ready: signed in to Spotify as alice, through your Spotify app.",
       "summary": "Spotify, through a Spotify app of yours.",
       "pane": {"id": "jammin.tab", "title": "Jammin", "icon": "radio", "kind": "jammin"}},
      {"name": "music", "title": "Apple Music", "on": true, "state": "ready",
       "detail": "Ready: the Music app on this Mac.",
       "summary": "The Music app on this Mac."}
     ]}
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
        "learn": {"agent_tips": true, "links": {"Team handbook": "https://example.com/handbook"}, "pictures": "tile", "tutor": "explain", "visual": true},
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
        "ui": {"flip_key": "C-\\", "focus_key": "C-]", "ghostty": "auto", "max_sub_rows": 6, "red_on_done": false, "helper_style": "browser", "new_tabs": "last", "terminal_links": "rolodex", "freeze_dog": "10s", "sidebar_width": 34, "theme": "rosewater", "view": "conversation"},
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
                    tokens: Tokens(input: 900, output: 2100, cacheRead: 21000, cacheWrite: 1800), hue: 5,
                    // Its Claude Code has a known advisory, which its card
                    // and its details say (DESIGN §4.21).
                    lock: LockRow(packages: [LockPackage(ecosystem: "npm", name: "@anthropic-ai/claude-code", version: "2.1.250", part: "Claude Code",
                                                         checked: true, advisories: 1, fixed: "2.1.251")],
                                  advisories: 1, words: "Claude Code 2.1.250: 1 advisory, fixed in 2.1.251")),
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
